package app

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Bootstrap 控制是否在启动时执行静默自举 (注册用户 PATH + shell 集成)。
// 源码默认 "on": 直接 go build / go install 产出的二进制行为与发行版一致。
// 本地开发构建由 Makefile 注入 -X jvm/internal/app.Bootstrap=off 关闭,
// 避免 dist/ 下的开发版污染系统环境 (与 Version 同为 ldflags 注入的 var)。
var Bootstrap = "on"

// BootstrapEnabled 报告本次运行是否应执行启动自举 (供 main 启动流程调用)。
func BootstrapEnabled() bool {
	exe, err := os.Executable()
	if err != nil {
		exe = "" // 定位失败不阻断, 按非 Temp 处理
	}
	return bootstrapEnabled(exe, os.Getenv("JVM_NO_BOOTSTRAP"), os.TempDir())
}

// bootstrapEnabled 是自举判定逻辑。任一条件命中即关闭:
//  1. JVM_NO_BOOTSTRAP 环境变量非空 (显式开关, 对任何构建生效)
//  2. 构建时经 ldflags 注入 Bootstrap=off (Makefile 的 make build / make run 开发产物)
//  3. 自身 exe 位于系统 Temp 目录下 (go run 的 go-build 临时二进制、Temp 里的
//     验证副本) —— 易失位置注册进 PATH 必然成为死链
//
// 收显式参数、不读全局 (Bootstrap var 除外, 与 Version 同惯例), 便于表驱动测试。
func bootstrapEnabled(exe, noBootEnv, tempDir string) bool {
	if strings.TrimSpace(noBootEnv) != "" {
		return false
	}
	if Bootstrap == "off" {
		return false
	}
	if exe == "" {
		return true
	}
	return !isUnderTemp(exe, tempDir)
}

// isUnderTemp 判断 exe 是否位于 tempDir 下 (tempDir 本身也算)。
// 纯路径比较, 两个参数先各自归一化; 收显式参数便于表驱动测试。
func isUnderTemp(exe, tempDir string) bool {
	e := normalizePath(exe)
	t := normalizePath(tempDir)
	return e == t || strings.HasPrefix(e, t+`\`)
}

// normalizePath 归一化 Windows 路径供比较: 尽力展开 8.3 短名 + Clean + 小写。
// 路径不存在时展开失败, 回退 Clean + 小写后的原值 (比较仍可能失配, 由调用方兜底)。
func normalizePath(p string) string {
	if lp := longPathName(p); lp != "" {
		p = lp
	}
	return strings.ToLower(filepath.Clean(p))
}

// longPathName 调 Win32 GetLongPathName 展开 8.3 短名 (如 ZBXCOM~1 → zbxComputer)。
// os.TempDir() 可能返回短名形式而 os.Executable() 返回长名 (或反之), 不展开
// 直接比较会失配。路径不存在等失败场景返回 "", 由调用方回退原值。
func longPathName(p string) string {
	p16, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return ""
	}
	buf := make([]uint16, 4096)
	n, err := syscall.GetLongPathName(p16, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}
