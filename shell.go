package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 本文件实现 shell 集成, 让 `jvm use` 在当前终端立即生效。
//
// 根本约束: Go 子进程无法修改父 shell (PowerShell/bash) 的进程级环境变量。
// 解决办法: 提供一个 `jvm init <shell>` 命令, 输出一个 wrapper 函数脚本,
// 用户把它放进 shell 的启动配置 ($PROFILE / ~/.bashrc)。
// wrapper 让 `jvm` 变成 shell 函数, 执行 `jvm use` 后在当前会话刷新 PATH/JAVA_HOME,
// 这样当前终端立即生效, 不用重开窗口。
//
// 幂等: init --install 会检测 profile 里是否已注入, 避免重复。

// profileMarker 是写入 profile 的注入块标记, 用于幂等检测
const profileMarker = "# >>> jvm shell init >>>"

// cmdInitDispatch 解析 jvm init 的参数并分发
// 用法: jvm init <powershell|bash> [--install]
func cmdInitDispatch(args []string) {
	if len(args) < 1 {
		fail("用法: jvm init <powershell|bash> [--install]\n" +
			"  示例:\n" +
			"    jvm init powershell            打印脚本到屏幕\n" +
			"    jvm init powershell --install  自动写入 $PROFILE\n" +
			"    jvm init bash --install        自动写入 ~/.bashrc")
	}
	shell := args[0]
	doInstall := false
	for _, a := range args[1:] {
		if a == "--install" || a == "-i" {
			doInstall = true
		}
	}
	cmdInit(shell, doInstall)
}

// cmdInit 处理 jvm init <shell> [--install]
//   jvm init powershell         打印脚本到 stdout
//   jvm init powershell --install   写入 $PROFILE
//   jvm init bash               打印脚本到 stdout
//   jvm init bash --install     写入 ~/.bashrc
func cmdInit(shell string, doInstall bool) {
	script, profilePath, err := shellIntegration(shell)
	if err != nil {
		fail(err.Error())
	}

	if doInstall {
		if err := installToProfile(profilePath, script); err != nil {
			fail("写入 profile 失败: " + err.Error())
		}
		fmt.Printf("✅ 已写入 %s\n", profilePath)
		fmt.Printf("   重启 %s 或重新加载配置即可生效。\n", shellLabel(shell))
		fmt.Printf("   之后 `jvm use <版本>` 在当前终端立即生效。\n")
		return
	}

	// 不带 --install, 打印脚本并给出手动安装提示
	fmt.Println(script)
	fmt.Println(profileMarker + " (以上为自动生成, 不要手改)")
	fmt.Printf("\n💡 自动安装到 profile: jvm init %s --install\n", shell)
}

// shellIntegration 返回某个 shell 的集成脚本和对应 profile 路径
func shellIntegration(shell string) (script, profilePath string, err error) {
	exePath, _ := os.Executable() // jvm.exe 绝对路径, 硬编码进脚本使函数不依赖 PATH
	switch shell {
	case "powershell", "pwsh", "ps":
		return psScript(exePath), psProfilePath(), nil
	case "bash", "sh", "git-bash":
		return bashScript(exePath), bashProfilePath(), nil
	default:
		return "", "", fmt.Errorf("不支持的 shell: %s (支持: powershell, bash)", shell)
	}
}

// toMSYSPath 把 Windows 路径 (D:\code\jvm\jvm.exe) 转成 MSYS/Git Bash 路径 (/d/code/jvm/jvm.exe)
// bash 不认反斜杠, 必须用正斜杠 + 小写盘符 + 去掉冒号
func toMSYSPath(winPath string) string {
	p := filepath.ToSlash(winPath) // 反斜杠 → 正斜杠: D:/code/jvm/jvm.exe
	if len(p) >= 2 && p[1] == ':' {
		// D:/... → /d/...
		drive := strings.ToLower(string(p[0]))
		return "/" + drive + p[2:]
	}
	return p
}

// psProfilePath 返回 Windows PowerShell 5.x 的 profile 路径
// ($PROFILE CurrentUserCurrentHost, 兼容性最好, Win 自带)
func psProfilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
}

// ps7ProfilePath 返回 PowerShell 7+ 的 profile 路径
func ps7ProfilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
}

// ensureShellIntegration 静默确保 shell 集成已安装到 PowerShell 和 bash 的 profile。
// 每次启动调用: 检测 $PROFILE (PS5/7) 和 ~/.bashrc 是否已注入, 没有就写入。
// 这样用户无需手动 jvm init, 首次运行后重开终端, jvm use 即在当前终端即时生效。
// 设计: 幂等 (已注入跳过)、静默 (正常无输出)、容错 (失败不中断主命令)。
func ensureShellIntegration() {
	exePath, err := os.Executable()
	if err != nil {
		return // 拿不到 exe 路径就没法生成可靠脚本, 跳过
	}
	// PowerShell 5.x + 7+ 两个 profile 都写 (用户可能用任一版本)
	for _, p := range []string{psProfilePath(), ps7ProfilePath()} {
		if !profileHasIntegration(p) {
			_ = installToProfile(p, psScript(exePath))
		}
	}
	// Git Bash
	bp := bashProfilePath()
	if !profileHasIntegration(bp) {
		_ = installToProfile(bp, bashScript(exePath))
	}
}

// profileHasIntegration 检测某个 profile 文件是否已注入 jvm 集成块
func profileHasIntegration(profilePath string) bool {
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), profileMarker)
}

// bashProfilePath 返回 bash profile 路径 (~/.bashrc)
func bashProfilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".bashrc")
}

// shellLabel 返回给用户看的 shell 名称
func shellLabel(shell string) string {
	switch shell {
	case "powershell", "pwsh", "ps":
		return "PowerShell"
	case "bash", "sh", "git-bash":
		return "bash"
	}
	return shell
}

// psScript 返回 PowerShell 集成脚本, exePath 是 jvm.exe 的绝对路径 (硬编码进脚本,
// 使函数不依赖 PATH 就能定位 jvm.exe)
func psScript(exePath string) string {
	return profileMarker + `
# jvm shell 集成: 让 jvm use 在当前终端立即生效
function jvm {
    & '` + exePath + `' @args
    # use 命令后刷新当前会话的 JAVA_HOME / PATH
    if ($args.Count -gt 0 -and $args[0] -eq 'use') {
        $env:JAVA_HOME = Join-Path $env:USERPROFILE ".jvm\current"
        # 把 current\bin 放到 PATH 最前面 (幂等: 先移除已有的, 再前置)
        $bin = Join-Path $env:JAVA_HOME "bin"
        $env:PATH = ($env:PATH -split ';' | Where-Object { $_ -and $_ -ne $bin }) -join ';'
        $env:PATH = "$bin;$env:PATH"
    }
}
# <<< jvm shell init <<<`
}

// bashScript 返回 bash 集成脚本, exePath 是 jvm.exe 的绝对路径 (硬编码进脚本)
func bashScript(exePath string) string {
	// bash (MSYS/Git Bash) 不认 Windows 反斜杠路径, 要转成 /c/... 风格
	bashPath := toMSYSPath(exePath)
	return profileMarker + `
# jvm shell 集成: 让 jvm use 在当前终端立即生效
jvm() {
    command "` + bashPath + `" "$@"
    # use 命令后刷新当前会话的 JAVA_HOME / PATH
    if [ "$1" = "use" ]; then
        export JAVA_HOME="$HOME/.jvm/current"
        local bin="$JAVA_HOME/bin"
        # 幂等: 先移除已有的 current/bin, 再前置
        export PATH="$bin:$(echo "$PATH" | tr ':' '\n' | grep -v "^${bin}$" | tr '\n' ':' | sed 's/:$//')"
    fi
}
# <<< jvm shell init <<<`
}

// installToProfile 把脚本写入 profile, 幂等 (已注入则跳过)
func installToProfile(profilePath, script string) error {
	// 确保父目录存在 (PowerShell 的 Documents\WindowsPowerShell 可能不存在)
	if dir := filepath.Dir(profilePath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
	}

	// 读现有内容, 检测是否已注入
	existing := ""
	if data, err := os.ReadFile(profilePath); err == nil {
		existing = string(data)
	}
	if strings.Contains(existing, profileMarker) {
		// 已注入, 替换为新版本 (删旧块, 加新块)
		existing = removeOldBlock(existing)
	}

	// 追加新脚本 (前后空行分隔)
	newContent := existing
	if newContent != "" && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += "\n" + script + "\n"

	return os.WriteFile(profilePath, []byte(newContent), 0o644)
}

// removeOldBlock 从 profile 内容里移除旧的 jvm 注入块
// 块范围: profileMarker 到 "</<< jvm shell init <<<"
func removeOldBlock(content string) string {
	start := strings.Index(content, profileMarker)
	if start < 0 {
		return content
	}
	endMarker := "# <<< jvm shell init <<<"
	end := strings.Index(content[start:], endMarker)
	if end < 0 {
		return content
	}
	end += start + len(endMarker)
	// 移除 [start, end) 及其后紧跟的一个换行
	rest := content[end:]
	rest = strings.TrimPrefix(rest, "\r\n")
	rest = strings.TrimPrefix(rest, "\n")
	return content[:start] + rest
}
