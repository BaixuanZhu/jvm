// 本文件实现 jvm use --auto: 由 shell 集成钩子 (PowerShell 包装 prompt /
// bash 挂 PROMPT_COMMAND) 在 cwd 或 .jvmrc 变化时调用, 自动切换/恢复版本。
// 用户不直接敲这个命令。
//
// 输出刻意精简 (no-op 路径零输出), 否则每次开终端 prompt 前都会冒垃圾行。
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jvm/internal/app"
	"jvm/internal/junction"
	"jvm/internal/paths"
	"jvm/internal/pinrc"
)

// autoAction 是 UseAuto 决策后的动作。
type autoAction int

const (
	autoNoop   autoAction = iota // 无事可做: 无 .jvmrc 且无待恢复状态, 或已在目标版本
	autoSwitch                   // 切到 target (.jvmrc 要求的版本)
	autoRevert                   // 离开 .jvmrc 目录, 恢复 state 记录的版本
	autoWarn                     // .jvmrc 有问题 (未装/解析失败), 打一行警告
)

// UseAuto 处理 jvm use --auto (shell 钩子调用)。
// enabled 为 false (config autoswitch = false) 时静默退出 —— 钩子仍留在
// profile 里但空转, 用户改配置即可开关, 无需重装集成。
//
// 语义:
//   - 目录里 (含上层) 有 .jvmrc: 切到该版本; 与当前相同则静默跳过;
//     未安装/解析失败打一行警告 (钩子只在 .jvmrc 路径变化时触发, 不刷屏)
//   - 没有 .jvmrc: 若发生过自动切换 (state 文件存在), 恢复切换前的版本并清 state
//   - 第一次自动切换前把当时的版本记进 state, 供恢复
func UseAuto(enabled bool) {
	if !enabled {
		return
	}
	if err := paths.EnsureDirs(); err != nil {
		return // 钩子路径失败静默, 不打断 prompt
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	// 解析 .jvmrc 要求的本地版本目录名; 失败留给 decideAuto 走警告分支
	content, foundPath, found := pinrc.FindUp(cwd)
	rcDir, warn := "", ""
	if found {
		rcDir, warn = resolveRcVersion(content, foundPath)
	}

	state := readAutoState()
	action, target := decideAuto(found, rcDir, currentDirName(), state)
	switch action {
	case autoNoop:
	case autoWarn:
		fmt.Printf("⚠️  %s\n", warn)
	case autoSwitch:
		// 第一次自动切换前记住手动版本, 供离开 .jvmrc 目录时恢复
		if state == "" {
			if cur := currentDirName(); cur != "" {
				if err := writeAutoState(cur); err != nil {
					fmt.Printf("⚠️  记录自动切换状态失败: %v\n", err)
				}
			}
		}
		switchQuietly(target, "📌 .jvmrc: 切换到 "+junction.DisplayName(target))
	case autoRevert:
		switchQuietly(target, "📌 离开 .jvmrc 目录: 恢复 "+junction.DisplayName(target))
		clearAutoState()
	}
}

// resolveRcVersion 把 .jvmrc 内容解析到本地版本目录名。
// 失败时返回空目录名和一行警告文案 (含原因与建议)。
func resolveRcVersion(content, foundPath string) (dir, warn string) {
	spec, err := pinrc.Parse(content)
	if err != nil {
		return "", foundPath + ": " + err.Error()
	}
	vs, err := app.ParseVersionSpec(spec)
	if err != nil {
		return "", foundPath + ": " + err.Error()
	}
	dir, err = junction.ResolveVersion(vs.Distro, vs.Version)
	if err != nil {
		return "", fmt.Sprintf(".jvmrc 要求 %s, 未安装。运行 jvm install %s", spec, spec)
	}
	return dir, ""
}

// decideAuto 是自动切换的纯决策函数 (不碰文件系统, 便于表驱动测试):
//   - !rcFound && state != ""  → autoRevert (target = state)
//   - !rcFound && state == ""  → autoNoop
//   - rcFound && rcDir == ""   → autoWarn (解析失败, 文案由调用方构造)
//   - rcFound && 版本不同       → autoSwitch (target = rcDir)
//   - rcFound && 版本相同       → autoNoop
//
// current 是当前 current 指向的版本目录名 (无则空串), 大小写不敏感比较。
func decideAuto(rcFound bool, rcDir, current, state string) (autoAction, string) {
	if !rcFound {
		if state != "" {
			return autoRevert, state
		}
		return autoNoop, ""
	}
	switch {
	case rcDir == "":
		return autoWarn, ""
	case current == "" || !strings.EqualFold(rcDir, current):
		return autoSwitch, rcDir
	default:
		return autoNoop, ""
	}
}

// switchQuietly 静默切换 (复用 switchTo), 成功打一行结果, 失败打警告不打断。
func switchQuietly(dir, message string) {
	if err := switchTo(filepath.Join(paths.VersionsDir, dir)); err != nil {
		fmt.Printf("⚠️  自动切换失败: %v\n", err)
		return
	}
	fmt.Println(message)
}

// currentDirName 返回 current 指向的版本目录名; 无则空串。
func currentDirName() string {
	if t := junction.ReadTarget(); t != "" {
		return filepath.Base(t)
	}
	return ""
}

// readAutoState 读自动切换状态 (自动切换前的手动版本目录名), 无则空串。
func readAutoState() string {
	return readAutoStateFile(paths.AutoStateFile)
}

// writeAutoState 记录自动切换前的手动版本目录名。
func writeAutoState(dir string) error {
	return writeAutoStateFile(paths.AutoStateFile, dir)
}

// clearAutoState 清除自动切换状态 (显式 jvm use 或恢复完成后调用)。
func clearAutoState() {
	_ = os.Remove(paths.AutoStateFile)
}

// readAutoStateFile / writeAutoStateFile 是 state 文件 IO 的显式路径版本,
// 便于测试。文件内容为单行版本目录名。
func readAutoStateFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeAutoStateFile(path, dir string) error {
	return os.WriteFile(path, []byte(dir), 0o644)
}
