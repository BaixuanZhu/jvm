// Package shell 实现 PowerShell 和 bash 的 shell 集成, 让 jvm use 在当前终端立即生效。
//
// 根本约束: Go 子进程无法修改父 shell 的进程级环境变量。解决办法是注入一个
// shell wrapper 函数: 调用真正的 jvm.exe 后, 若是 use 命令就在当前会话刷新
// JAVA_HOME / PATH。
//
// EnsureIntegration 在 jvm 启动时静默调用, 把集成函数写入 PowerShell $PROFILE
// (5.x 和 7+) 和 ~/.bashrc, 幂等。用户无需手动配置。
package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jvm/internal/app"
)

// profileMarker 是写入 profile 的注入块标记, 用于幂等检测
const profileMarker = "# >>> jvm shell init >>>"

// EnsureIntegration 静默确保 shell 集成与 Tab 补全已安装到 PowerShell 和 bash 的 profile。
// 幂等 (已注入跳过)、静默 (正常无输出)、容错 (失败不中断)。
//
// shell 集成和补全是两个独立的关注点, 各用独立的 profile 标记块, 互不影响升级。
//
// 注意: 幂等检测以 marker 是否存在为准, 不比较内容。这意味着补全脚本内容变更后
// (如新增发行版、修 bug), 已有 profile 不会自动刷新 —— 用户需手动删掉 marker 块
// 或运行 `jvm completion <shell> --install` 强制重写。这与 shell 集成块的行为一致。
// distro 列表在生成补全脚本时从 provider 注册表读取并嵌入。
func EnsureIntegration() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	distros := distroNames()
	// PowerShell 5.x + 7+ 两个 profile 都写
	for _, p := range []string{psProfilePath(), ps7ProfilePath()} {
		if !profileHasIntegration(p) {
			_ = installToProfile(p, psScript(exePath))
		}
		if !completionHasIntegration(p) {
			_ = installToProfile(p, psCompletionScript(distros))
		}
	}
	// Git Bash
	bp := bashProfilePath()
	if !profileHasIntegration(bp) {
		_ = installToProfile(bp, bashScript(exePath))
	}
	if !completionHasIntegration(bp) {
		_ = installToProfile(bp, bashCompletionScript(distros))
	}
}

// completionHasIntegration 检测某个 profile 文件是否已注入 jvm 补全块。
func completionHasIntegration(profilePath string) bool {
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), completionMarker)
}

// CompletionHasIntegration 检测某个 profile 文件是否已注入 jvm 补全块。
// 导出以供 doctor 包诊断补全状态。
func CompletionHasIntegration(profilePath string) bool {
	return completionHasIntegration(profilePath)
}

// InitDispatch 解析 jvm init 的参数并分发 (供手动 init 命令调用)。
// 用法: jvm init <powershell|bash> [--install]
func InitDispatch(args []string) {
	if len(args) < 1 {
		app.Fail("用法: jvm init <powershell|bash> [--install]\n" +
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

// InitCompletionDispatch 解析 jvm completion 的参数并分发 (供手动 completion 命令调用)。
// 用法: jvm completion <powershell|bash> [--install]
func InitCompletionDispatch(args []string) {
	if len(args) < 1 {
		app.Fail("用法: jvm completion <powershell|bash> [--install]\n" +
			"  示例:\n" +
			"    jvm completion powershell            打印补全脚本到屏幕\n" +
			"    jvm completion powershell --install  自动写入 $PROFILE\n" +
			"    jvm completion bash --install        自动写入 ~/.bashrc")
	}
	sh := args[0]
	doInstall := false
	for _, a := range args[1:] {
		if a == "--install" || a == "-i" {
			doInstall = true
		}
	}
	script, profilePath, err := completionScriptFor(sh)
	if err != nil {
		app.Fail(err.Error())
	}
	if doInstall {
		if err := installToProfile(profilePath, script); err != nil {
			app.Fail("写入 profile 失败: " + err.Error())
		}
		fmt.Printf("✅ 已写入 %s\n", profilePath)
		fmt.Printf("   重启 %s 或重新加载配置即可生效。\n", shellLabel(sh))
		fmt.Printf("   之后输入 jvm 后按 Tab 即可补全命令。\n")
		return
	}
	fmt.Println(script)
	fmt.Printf("\n💡 自动安装到 profile: jvm completion %s --install\n", sh)
}

// completionScriptFor 返回某个 shell 的补全脚本和对应 profile 路径
func completionScriptFor(sh string) (script, profilePath string, err error) {
	distros := distroNames()
	switch sh {
	case "powershell", "pwsh", "ps":
		return psCompletionScript(distros), psProfilePath(), nil
	case "bash", "sh", "git-bash":
		return bashCompletionScript(distros), bashProfilePath(), nil
	default:
		return "", "", fmt.Errorf("不支持的 shell: %s (支持: powershell, bash)", sh)
	}
}

// cmdInit 处理 jvm init <shell> [--install]
func cmdInit(shell string, doInstall bool) {
	script, profilePath, err := shellIntegration(shell)
	if err != nil {
		app.Fail(err.Error())
	}

	if doInstall {
		if err := installToProfile(profilePath, script); err != nil {
			app.Fail("写入 profile 失败: " + err.Error())
		}
		fmt.Printf("✅ 已写入 %s\n", profilePath)
		fmt.Printf("   重启 %s 或重新加载配置即可生效。\n", shellLabel(shell))
		fmt.Printf("   之后 `jvm use <版本>` 在当前终端立即生效。\n")
		return
	}

	fmt.Println(script)
	fmt.Println(profileMarker + " (以上为自动生成, 不要手改)")
	fmt.Printf("\n💡 自动安装到 profile: jvm init %s --install\n", shell)
}

// shellIntegration 返回某个 shell 的集成脚本和对应 profile 路径
func shellIntegration(shell string) (script, profilePath string, err error) {
	exePath, _ := os.Executable()
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
func toMSYSPath(winPath string) string {
	p := filepath.ToSlash(winPath)
	if len(p) >= 2 && p[1] == ':' {
		drive := strings.ToLower(string(p[0]))
		return "/" + drive + p[2:]
	}
	return p
}

// PsProfilePath 返回 Windows PowerShell 5.x 的 profile 路径。
// 导出以供 doctor 包检测集成状态。
func PsProfilePath() string { return psProfilePath() }

// Ps7ProfilePath 返回 PowerShell 7+ 的 profile 路径。
func Ps7ProfilePath() string { return ps7ProfilePath() }

// BashProfilePath 返回 bash profile 路径 (~/.bashrc)。
func BashProfilePath() string { return bashProfilePath() }

// psProfilePath 返回 Windows PowerShell 5.x 的 profile 路径
func psProfilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
}

// ps7ProfilePath 返回 PowerShell 7+ 的 profile 路径
func ps7ProfilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
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

// ProfileHasIntegration 检测某个 profile 文件是否已注入 jvm 集成块。
// 导出以供 doctor 包诊断 shell 集成状态。
func ProfileHasIntegration(profilePath string) bool {
	return profileHasIntegration(profilePath)
}

// profileHasIntegration 检测某个 profile 文件是否已注入 jvm 集成块
func profileHasIntegration(profilePath string) bool {
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), profileMarker)
}

// psScript 返回 PowerShell 集成脚本, exePath 硬编码进脚本使函数不依赖 PATH
func psScript(exePath string) string {
	return profileMarker + `
# jvm shell integration: make ` + "`jvm use`" + ` take effect in the current terminal
function jvm {
    & '` + exePath + `' @args
    # after ` + "`use`" + `, refresh JAVA_HOME / PATH in the current session
    if ($args.Count -gt 0 -and $args[0] -eq 'use') {
        $env:JAVA_HOME = Join-Path $env:USERPROFILE ".jvm\current"
        $bin = Join-Path $env:JAVA_HOME "bin"
        $env:PATH = ($env:PATH -split ';' | Where-Object { $_ -and $_ -ne $bin }) -join ';'
        $env:PATH = "$bin;$env:PATH"
    }
}
# <<< jvm shell init <<<`
}

// bashScript 返回 bash 集成脚本, exePath 硬编码 (转 MSYS 路径)
func bashScript(exePath string) string {
	bashPath := toMSYSPath(exePath)
	return profileMarker + `
# jvm shell integration: make ` + "`jvm use`" + ` take effect in the current terminal
jvm() {
    command "` + bashPath + `" "$@"
    if [ "$1" = "use" ]; then
        export JAVA_HOME="$HOME/.jvm/current"
        local bin="$JAVA_HOME/bin"
        export PATH="$bin:$(echo "$PATH" | tr ':' '\n' | grep -v "^${bin}$" | tr '\n' ':' | sed 's/:$//')"
    fi
}
# <<< jvm shell init <<<`
}

// installToProfile 把脚本写入 profile, 幂等 (已注入则替换为新版本)。
// 幂等判断依据是脚本自身的起始标记: 脚本必须以形如 "# >>> ... >>>" 的标记开头,
// 结束标记为对应的 "# <<< ... <<<"。不同关注点 (shell 集成 / 补全) 用各自的标记,
// 互不干扰 —— 写补全块时不会误删 shell 集成块, 反之亦然。
//
// 脚本内容保持纯 ASCII (注释用英文): 避免 PowerShell 5.1 在中文系统上用 GBK 解码
// 非 ASCII 字节导致语法损坏。不在写入端做 BOM 适配 —— 内容本身不触发编码问题,
// 比 BOM 机制更简单可靠 (BOM 还会干扰 bash 等工具)。
func installToProfile(profilePath, script string) error {
	if dir := filepath.Dir(profilePath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
	}
	existing := ""
	if data, err := os.ReadFile(profilePath); err == nil {
		existing = string(data)
	}
	// 从脚本首行提取起始标记, 据此判断并移除同标记的旧块
	startMarker := firstLine(script)
	existing = removeBlock(existing, startMarker, endMarkerFor(startMarker))
	newContent := existing
	if newContent != "" && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += "\n" + script + "\n"
	return os.WriteFile(profilePath, []byte(newContent), 0o644)
}

// firstLine 返回字符串的第一行 (用于提取脚本块的起始标记)
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// endMarkerFor 从起始标记 "# >>> name >>>" 推导结束标记 "# <<< name <<<"。
// 用 ReplaceAll 而非 Replace(n=1): 标记首尾各有一个 ">>>", 两个都要翻成 "<<<"。
func endMarkerFor(startMarker string) string {
	return strings.ReplaceAll(startMarker, ">>>", "<<<")
}

// removeBlock 从 content 里移除所有 [startMarker ... endMarker] 这一段 (含标记本身)。
// 循环移除直至无残留: 防止历史遗留的重复块 (如手动 install + EnsureIntegration 各写一遍)
// 在幂等升级时越积越多。startMarker 不存在则原样返回。供 installToProfile 做幂等替换。
func removeBlock(content, startMarker, endMarker string) string {
	for {
		start := strings.Index(content, startMarker)
		if start < 0 {
			return content
		}
		end := strings.Index(content[start:], endMarker)
		if end < 0 {
			return content // 只有开头标记无结束标记, 无法安全移除, 原样返回
		}
		end += start + len(endMarker)
		rest := content[end:]
		rest = strings.TrimPrefix(rest, "\r\n")
		rest = strings.TrimPrefix(rest, "\n")
		content = content[:start] + rest
	}
}
