// Package env 负责持久化环境变量 (JAVA_HOME 和 PATH), 写入注册表
//
//	HKCU\Environment
//
// 不调用 cmd.exe 或 setx (setx 会截断长 PATH, 是个老坑)。
// 写完后广播 WM_SETTINGCHANGE, 让新开的进程看到变化。
package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"jvm/internal/paths"
)

var (
	modAdvapi32 = syscall.NewLazyDLL("advapi32.dll")
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegOpenKeyEx           = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueEx        = modAdvapi32.NewProc("RegQueryValueExW")
	procRegSetValueEx          = modAdvapi32.NewProc("RegSetValueExW")
	procRegCloseKey            = modAdvapi32.NewProc("RegCloseKey")
	procSendMessageW           = modUser32.NewProc("SendMessageTimeoutW")
	procSetEnvironmentVariable = modKernel32.NewProc("SetEnvironmentVariableW")
)

const (
	HKEY_CURRENT_USER = 0x80000001
	KEY_READ          = 0x20019
	KEY_ALL_ACCESS    = 0xF003F
	REG_EXPAND_SZ     = 2

	HWND_BROADCAST   = 0xFFFF
	WM_SETTINGCHANGE = 0x001A
	SMTO_ABORTIFHUNG = 0x0002
)

// Persist 把一个值写到 HKCU\Environment (REG_EXPAND_SZ, 支持变量展开)。
// 同时设置当前进程的环境变量, 并广播 WM_SETTINGCHANGE 通知新进程。
func Persist(name, value string) error {
	var hKey syscall.Handle
	envKey, _ := syscall.UTF16PtrFromString("Environment")
	r1, _, e := procRegOpenKeyEx.Call(
		uintptr(HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(envKey)),
		0,
		uintptr(KEY_ALL_ACCESS),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r1 != 0 {
		return fmt.Errorf("打开注册表失败: %w", os.NewSyscallError("RegOpenKeyEx", e))
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	valueUTF16 := syscall.StringToUTF16(value)
	valueBytes := len(value) * 2
	nameUTF16, _ := syscall.UTF16PtrFromString(name)

	r1, _, e = procRegSetValueEx.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(nameUTF16)),
		0,
		uintptr(REG_EXPAND_SZ),
		uintptr(unsafe.Pointer(&valueUTF16[0])),
		uintptr(valueBytes+2), // 末尾 \0
	)
	if r1 != 0 {
		return fmt.Errorf("写注册表失败: %w", os.NewSyscallError("RegSetValueEx", e))
	}

	setCurrentProcessEnv(name, value)
	broadcastSettingChange()
	return nil
}

// setCurrentProcessEnv 设置当前进程的环境变量 (UTF16 接口)
func setCurrentProcessEnv(name, value string) {
	nameUTF16, _ := syscall.UTF16PtrFromString(name)
	valueUTF16, _ := syscall.UTF16PtrFromString(value)
	procSetEnvironmentVariable.Call(
		uintptr(unsafe.Pointer(nameUTF16)),
		uintptr(unsafe.Pointer(valueUTF16)),
	)
}

// broadcastSettingChange 广播 WM_SETTINGCHANGE, 通知 Explorer 等刷新环境
func broadcastSettingChange() {
	envUTF16, _ := syscall.UTF16PtrFromString("Environment")
	procSendMessageW.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_SETTINGCHANGE),
		0,
		uintptr(unsafe.Pointer(envUTF16)),
		SMTO_ABORTIFHUNG,
		5000,
		0,
	)
}

// ReadUserEnv 从 HKCU\Environment 读一个值。
// 导出以供 doctor 包诊断 JAVA_HOME/PATH 等持久化状态。
func ReadUserEnv(name string) (string, error) {
	return readUserEnv(name)
}

// readUserEnv 从 HKCU\Environment 读一个值
func readUserEnv(name string) (string, error) {
	var hKey syscall.Handle
	envKey, _ := syscall.UTF16PtrFromString("Environment")
	r1, _, e := procRegOpenKeyEx.Call(
		uintptr(HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(envKey)),
		0,
		uintptr(KEY_READ),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r1 != 0 {
		return "", os.NewSyscallError("RegOpenKeyEx", e)
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	nameUTF16, _ := syscall.UTF16PtrFromString(name)
	var bufLen uint32 = 32768
	buf := make([]uint16, bufLen)
	var valueType uint32
	r1, _, e = procRegQueryValueEx.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(nameUTF16)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
	)
	if r1 != 0 {
		return "", os.NewSyscallError("RegQueryValueEx", e)
	}
	return syscall.UTF16ToString(buf), nil
}

// splitPathEntries 拆分 PATH, 忽略空条目
func splitPathEntries(p string) []string {
	parts := strings.Split(p, ";")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// EnsureCurrentInPath 确保 ~/.jvm/current/bin 在用户 PATH 的最前面。
// 只在缺失时添加, 不会重复加 (可反复运行)。
func EnsureCurrentInPath() error {
	binPath := filepath.Join(paths.CurrentLink, "bin")
	userPath, err := readUserEnv("PATH")
	if err != nil {
		userPath = "" // 用户 PATH 可能不存在, 新建一个
	}

	entries := splitPathEntries(userPath)
	for _, e := range entries {
		if strings.EqualFold(filepath.Clean(e), filepath.Clean(binPath)) {
			return nil // 已经在了
		}
	}

	newPath := binPath
	if strings.TrimSpace(userPath) != "" {
		newPath = binPath + ";" + userPath
	}
	return Persist("PATH", newPath)
}

// RemoveFromUserPath 从注册表用户 PATH 中移除指定条目 (大小写不敏感、
// 路径规整后匹配), 供 doctor --fix 清理旧 JDK 残留路径。
// 没有命中条目时不动注册表。
func RemoveFromUserPath(entries []string) error {
	if len(entries) == 0 {
		return nil
	}
	remove := make(map[string]bool, len(entries))
	for _, e := range entries {
		remove[strings.ToLower(filepath.Clean(e))] = true
	}
	userPath, err := readUserEnv("PATH")
	if err != nil {
		return fmt.Errorf("读取用户 PATH 失败: %w", err)
	}
	newPath := filterUserPath(userPath, remove)
	if newPath == userPath {
		return nil
	}
	return Persist("PATH", newPath)
}

// filterUserPath 从 PATH 串中移除命中的条目, 保留其余条目与顺序。
// remove 的 key 须为 strings.ToLower(filepath.Clean(条目)) 形式
// (与 RemoveFromUserPath 构造方式一致)。纯函数, 便于表驱动测试。
func filterUserPath(userPath string, remove map[string]bool) string {
	parts := strings.Split(userPath, ";")
	out := make([]string, 0, len(parts))
	changed := false
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" && remove[strings.ToLower(filepath.Clean(trimmed))] {
			changed = true
			continue
		}
		out = append(out, p)
	}
	if !changed {
		return userPath
	}
	return strings.Join(out, ";")
}

// EnsureUserPath 确保 jvm.exe 自身所在目录在用户 PATH 里。
// 每次启动静默调用: 用 os.Executable() 拿 exe 目录, 若不在用户 PATH
// 就追加到末尾并持久化。首次运行自动注入, 之后用户移动 exe 也能自适应。
// 失败静默忽略, 不影响主命令。
func EnsureUserPath() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exePath)

	userPath, _ := readUserEnv("PATH")
	entries := splitPathEntries(userPath)
	for _, e := range entries {
		if strings.EqualFold(filepath.Clean(e), filepath.Clean(exeDir)) {
			return // 已经在 PATH 里
		}
	}

	// 追加到末尾 (jvm 自身优先级低, 不抢占 current/bin 的前置位置)
	newPath := userPath
	if strings.TrimSpace(newPath) != "" {
		newPath = newPath + ";" + exeDir
	} else {
		newPath = exeDir
	}
	_ = Persist("PATH", newPath)
}
