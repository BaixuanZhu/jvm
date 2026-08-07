// Package doctor 实现 jvm doctor 命令: 全面诊断环境配置是否健康。
//
// 检查项 (每项输出 ✓ 通过 / ✗ 有问题, ✗ 附修复建议):
//   - 目录结构: ~/.jvm 和 ~/.jvm/versions 是否存在
//   - junction: current 链接是否存在且指向真实存在的版本目录
//   - JAVA_HOME: 注册表持久化值是否指向 ~/.jvm/current
//   - PATH 冲突: 是否有别的 java.exe 出现在 ~/.jvm/current/bin 之前 (会抢先)
//   - shell 集成: PowerShell 5.x/7+ 和 bash 的 profile 是否已注入
//   - current 的 java: ~/.jvm/current/bin/java.exe 是否存在
//
// 检查函数都接收显式参数 (路径/已读好的环境值), 不直接读全局状态 ——
// 这样可以用临时目录和注入值做表驱动测试, 不污染真实注册表/profile。
// Run() 负责从 paths/env/shell 取真实状态再分发给检查函数。
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jvm/internal/env"
	"jvm/internal/paths"
	"jvm/internal/shell"
)

// check 是单个检查项的结果。
type check struct {
	ok     bool   // true=通过, false=有问题
	name   string // 检查项名称
	detail string // 通过时是说明, 失败时是问题描述
	fix    string // 失败时的修复建议 (通过时为空)
}

// profileItem 是一个 shell profile 的检查输入。
type profileItem struct {
	label string // 给用户看的名称 (如 "PowerShell 5.x")
	path  string // profile 文件路径
}

// Run 执行全部检查并打印诊断报告。
func Run() {
	fmt.Println("🏥 jvm 环境诊断")
	fmt.Println(strings.Repeat("─", 40))

	// 从真实全局状态读取检查所需输入
	javaHome, _ := env.ReadUserEnv("JAVA_HOME")
	profiles := []profileItem{
		{"PowerShell 5.x", shell.PsProfilePath()},
		{"PowerShell 7+", shell.Ps7ProfilePath()},
		{"bash", shell.BashProfilePath()},
	}

	checks := []check{
		checkDirs(paths.Root, paths.VersionsDir),
		checkJunction(paths.CurrentLink),
		checkJavaHome(javaHome, paths.CurrentLink),
		checkPathConflict(os.Getenv("PATH"), paths.CurrentLink),
		checkShellIntegration(profiles),
		checkCurrentJava(paths.CurrentLink),
	}

	problems := 0
	for _, c := range checks {
		printCheck(c)
		if !c.ok {
			problems++
		}
	}

	fmt.Println(strings.Repeat("─", 40))
	switch problems {
	case 0:
		fmt.Println("✅ 所有检查通过, 环境配置正常。")
	default:
		fmt.Printf("⚠️  发现 %d 个问题 (见上方 ✗ 项的修复建议)。\n", problems)
	}
}

// printCheck 打印单个检查项结果
func printCheck(c check) {
	mark := "✓"
	if !c.ok {
		mark = "✗"
	}
	fmt.Printf("%s %s: %s\n", mark, c.name, c.detail)
	if c.fix != "" {
		fmt.Printf("    修复: %s\n", c.fix)
	}
}

// checkDirs 检查 root 和 versions 目录是否存在。
func checkDirs(root, versionsDir string) check {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return check{
			ok:     false,
			name:   "目录结构",
			detail: fmt.Sprintf("~/.jvm (%s) 不存在", root),
			fix:    "运行一次任意 jvm 命令 (如 jvm version) 会自动创建",
		}
	}
	if info, err := os.Stat(versionsDir); err != nil || !info.IsDir() {
		return check{
			ok:     false,
			name:   "目录结构",
			detail: fmt.Sprintf("版本目录 (%s) 不存在", versionsDir),
			fix:    "运行 jvm install <版本号> 安装一个 JDK",
		}
	}
	return check{ok: true, name: "目录结构", detail: "~/.jvm 和 versions 目录就绪"}
}

// checkJunction 检查 current 链接是否有效。
//
// 判断"是否是链接"用 os.Readlink 是否成功为准 —— 不能用 os.Lstat 的 ModeSymlink 位:
// Windows junction (IO_REPARSE_TAG_MOUNT_POINT) 在 Go 里 Lstat 不设置 ModeSymlink
// (那是给真 symlink IO_REPARSE_TAG_SYMLINK 的), 而是设置 ModeIrregular。
// 但 os.Readlink 对 junction 和 symlink 都能正确解析 (Go 1.20+)。
func checkJunction(link string) check {
	if _, err := os.Lstat(link); err != nil {
		return check{
			ok:     false,
			name:   "current 链接",
			detail: "尚未选定任何版本 (current 不存在)",
			fix:    "运行 jvm use <版本号> 选择一个版本",
		}
	}
	target, err := os.Readlink(link)
	if err != nil || target == "" {
		// current 存在但 Readlink 失败 → 是普通目录而非链接 (旧版残留或手动建的)
		return check{
			ok:     false,
			name:   "current 链接",
			detail: "current 不是链接 (可能是普通目录)",
			fix:    "删除后重新 jvm use <版本号>",
		}
	}
	// 验证目标真实存在 (指向已被删除的版本目录 = 悬空链接)
	if _, err := os.Stat(link); err != nil {
		return check{
			ok:     false,
			name:   "current 链接",
			detail: fmt.Sprintf("current 指向的目标不存在 (%s)", filepath.Base(target)),
			fix:    "版本目录可能被手动删除, jvm use <版本号> 重新指向有效版本",
		}
	}
	return check{ok: true, name: "current 链接", detail: fmt.Sprintf("指向 %s", filepath.Base(target))}
}

// checkJavaHome 检查持久化的 JAVA_HOME 是否指向 current。
// javaHome 是已从注册表读出的值 (由 Run 读取后注入, 便于测试)。
func checkJavaHome(javaHome, currentLink string) check {
	if strings.TrimSpace(javaHome) == "" {
		return check{
			ok:     false,
			name:   "JAVA_HOME",
			detail: "注册表 HKCU\\Environment 未设置 JAVA_HOME",
			fix:    "运行 jvm use <版本号> 会自动设置",
		}
	}
	if !strings.EqualFold(filepath.Clean(javaHome), filepath.Clean(currentLink)) {
		return check{
			ok:     false,
			name:   "JAVA_HOME",
			detail: fmt.Sprintf("JAVA_HOME=%s (期望 %s)", javaHome, currentLink),
			fix:    "运行 jvm use <版本号> 会自动修正",
		}
	}
	return check{ok: true, name: "JAVA_HOME", detail: "指向 ~/.jvm/current"}
}

// checkPathConflict 检查 PATH 里是否有别的 java.exe 在 current/bin 之前。
// pathEnv 是已读好的 PATH 值 (便于测试), currentLink 是 current 目录路径。
func checkPathConflict(pathEnv, currentLink string) check {
	binPath := filepath.Join(currentLink, "bin")
	if pathEnv == "" {
		return check{ok: true, name: "PATH 冲突", detail: "PATH 为空, 无冲突"}
	}
	for _, entry := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// 先遇到 current/bin, 之后有无冲突都不影响 (current 优先)
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(binPath)) {
			return check{ok: true, name: "PATH 冲突", detail: "current/bin 在 PATH 中, 无抢先的 java"}
		}
		// 在遇到 current/bin 之前, 若某条目里有 java.exe, 就是抢先
		javaExe := filepath.Join(entry, "java.exe")
		if _, err := os.Stat(javaExe); err == nil {
			return check{
				ok:     false,
				name:   "PATH 冲突",
				detail: fmt.Sprintf("%s 里有 java.exe, 出现在 current/bin 之前", entry),
				fix:    "从 PATH 移除该目录, 或把它移到 current/bin 之后",
			}
		}
	}
	return check{ok: true, name: "PATH 冲突", detail: "PATH 中无抢先的 java"}
}

// checkShellIntegration 检查给定的 profile 列表是否已注入集成。
func checkShellIntegration(profiles []profileItem) check {
	var missing []string
	for _, p := range profiles {
		if !shell.ProfileHasIntegration(p.path) {
			missing = append(missing, p.label)
		}
	}
	if len(missing) == 0 {
		return check{ok: true, name: "shell 集成", detail: "PowerShell 5.x/7+ 和 bash profile 均已注入"}
	}
	return check{
		ok:     false,
		name:   "shell 集成",
		detail: fmt.Sprintf("未集成: %s", strings.Join(missing, ", ")),
		fix:    "重开终端自动补全, 或手动 jvm init <shell> --install",
	}
}

// checkCurrentJava 检查 current/bin/java.exe 是否存在。
func checkCurrentJava(currentLink string) check {
	javaExe := filepath.Join(currentLink, "bin", "java.exe")
	if _, err := os.Stat(javaExe); err != nil {
		return check{
			ok:     false,
			name:   "current 的 java",
			detail: "current/bin/java.exe 不存在",
			fix:    "当前指向的版本可能损坏, 重新 jvm install + jvm use",
		}
	}
	return check{ok: true, name: "current 的 java", detail: "java.exe 就绪"}
}
