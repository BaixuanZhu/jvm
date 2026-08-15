// 本文件实现 jvm exec 子命令: 用指定 JDK 版本执行命令, 不动全局状态。
//
// 与 jvm use 的区别: use 切换全局 current junction (影响所有终端),
// exec 只在子进程环境里注入 JAVA_HOME / PATH, 一次性生效 —— 适合
// 多版本并行测试 (jvm exec 17 -- mvn test) 和 CI 脚本。
package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"jvm/internal/app"
	"jvm/internal/junction"
	"jvm/internal/paths"
	"jvm/internal/pinrc"
)

// Exec 处理 jvm exec <[distro@]版本> -- <命令> [参数...]。
//
// `--` 分隔符必需: 版本号与命令之间没有结构歧义。无版本号时读 .jvmrc,
// 再退到当前 current 指向的版本。只解析本地已装版本, 不自动下载
// (保持快速且可预测, 未装时提示 jvm install)。
func Exec(args []string) {
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		app.Fail("用法: jvm exec <[distro@]版本> -- <命令> [参数...]\n" +
			"  示例:\n" +
			"    jvm exec 17 -- mvn test            用 JDK 17 跑测试 (不动全局版本)\n" +
			"    jvm exec corretto@21 -- java -version\n" +
			"    jvm exec -- mvn test                无版本号: 读 .jvmrc, 再退到当前版本")
	}
	specArgs, cmdArgs := args[:sep], args[sep+1:]
	if len(cmdArgs) == 0 {
		app.Fail("jvm exec 需要在 -- 之后给出要执行的命令")
	}

	if err := paths.EnsureDirs(); err != nil {
		app.Fail(err.Error())
	}
	dir := resolveExecDir(specArgs)

	// 执行前不打印任何信息, 不污染管道 (jvm exec -- java -version 的输出就是 java 的输出)
	if err := execWith(filepath.Join(paths.VersionsDir, dir), cmdArgs); err != nil {
		// 子进程退出码非零不是"错误", 原样传播给调用方 (脚本按 $? 判断)
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		app.Fail(err.Error())
	}
}

// resolveExecDir 把 exec 的版本参数解析到本地版本目录名。
// specArgs 为空时依次尝试 .jvmrc 和 current 指向的版本。
func resolveExecDir(specArgs []string) string {
	arg := ""
	switch len(specArgs) {
	case 0:
		arg = execSpecFromContext()
	case 1:
		arg = specArgs[0]
	default:
		app.Fail("jvm exec 最多接受一个版本参数 (格式: [distro@]版本)")
	}
	if arg == "" {
		app.Fail("没有可用的版本: 当前目录及上层无 .jvmrc, 也没有选中的版本。\n" +
			"  先 jvm install <版本号>, 或 jvm exec <版本> -- <命令>")
	}

	spec, err := app.ParseVersionSpec(arg)
	if err != nil {
		app.Fail(err.Error())
	}
	dir, err := junction.ResolveVersion(spec.Distro, spec.Version)
	if err != nil {
		app.Fail(err.Error() + "\n  先安装: jvm install " + spec.Distro + "@" + spec.Version)
	}
	return dir
}

// execSpecFromContext 返回无版本参数时的兜底 spec:
// 先读 .jvmrc (从当前目录向上), 没有则用 current 指向的版本。
// 都没有返回空串。
func execSpecFromContext() string {
	if cwd, err := os.Getwd(); err == nil {
		if content, _, found := pinrc.FindUp(cwd); found {
			if spec, err := pinrc.Parse(content); err == nil {
				return spec
			}
		}
	}
	if t := junction.ReadTarget(); t != "" {
		distro, ver := junction.SplitDistro(filepath.Base(t))
		return distro + "@" + ver
	}
	return ""
}

// execWith 在指定 JDK 版本的环境里执行命令。
// 环境只对子进程生效: JAVA_HOME 指向真实版本目录 (非 junction),
// 该版本的 bin 前插到 PATH 最前 (java/javac 优先于 current/bin)。
// 子进程退出码非零时返回包装了 *exec.ExitError 的错误, 由调用方决定传播方式。
func execWith(versionDir string, cmdArgs []string) error {
	exePath, err := lookPathIn(filepath.Join(versionDir, "bin"), cmdArgs[0])
	if err != nil {
		return fmt.Errorf("未找到命令 %s (PATH 与该版本 bin 里都没有)", cmdArgs[0])
	}

	var c *exec.Cmd
	// Windows: CreateProcess 不能直接执行批处理文件, 而 mvn / gradlew 全是
	// .cmd/.bat —— 批处理必须经 cmd.exe /c 分发, 否则报"不是有效的 Win32 应用程序"
	if ext := strings.ToLower(filepath.Ext(exePath)); ext == ".bat" || ext == ".cmd" {
		c = exec.Command("cmd.exe", append([]string{"/c", exePath}, cmdArgs[1:]...)...)
	} else {
		c = exec.Command(exePath, cmdArgs[1:]...)
	}
	c.Env = buildExecEnv(versionDir, os.Environ())
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("执行 %s 失败: %w", cmdArgs[0], err)
	}
	return nil
}

// lookPathIn 解析命令的实际路径: 先在版本 bin 里找 (java/javac 就该用
// 指定版本的那个, 不依赖系统 PATH), 找不到再回退系统 PATH (mvn 等外部工具)。
func lookPathIn(bin, name string) (string, error) {
	// 含路径分隔符的输入是显式路径, 不做 bin 内查找
	if strings.ContainsRune(name, os.PathSeparator) || strings.ContainsRune(name, '/') {
		return exec.LookPath(name)
	}
	// 候选: 原名 + 原名加 PATHEXT 各扩展 (.exe/.bat/.cmd/...)
	candidates := []string{filepath.Join(bin, name)}
	for _, ext := range pathExts() {
		if ext != "" {
			candidates = append(candidates, filepath.Join(bin, name+ext))
		}
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return exec.LookPath(name)
}

// pathExts 返回 Windows 可执行扩展列表 (PATHEXT, 分号分隔)。
// 环境变量缺失时用保守默认值。
func pathExts() []string {
	v := os.Getenv("PATHEXT")
	if v == "" {
		v = ".COM;.EXE;.BAT;.CMD"
	}
	return strings.Split(v, ";")
}

// buildExecEnv 构建子进程环境: 覆盖 JAVA_HOME, 版本 bin 前插到 PATH。
// 对传入的 env 切片做纯变换, 便于表驱动测试。
// 环境变量名在 Windows 上大小写不敏感, 用 EqualFold 匹配。
func buildExecEnv(versionDir string, env []string) []string {
	javaHome := versionDir
	bin := filepath.Join(javaHome, "bin")
	out := make([]string, 0, len(env)+2)
	hasHome, hasPath := false, false
	for _, e := range env {
		name, value, ok := strings.Cut(e, "=")
		if !ok {
			out = append(out, e)
			continue
		}
		switch {
		case strings.EqualFold(name, "JAVA_HOME"):
			out = append(out, "JAVA_HOME="+javaHome)
			hasHome = true
		case strings.EqualFold(name, "PATH"):
			out = append(out, "PATH="+bin+string(os.PathListSeparator)+value)
			hasPath = true
		default:
			out = append(out, e)
		}
	}
	if !hasHome {
		out = append(out, "JAVA_HOME="+javaHome)
	}
	if !hasPath {
		out = append(out, "PATH="+bin)
	}
	return out
}
