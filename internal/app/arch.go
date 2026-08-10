package app

import "strings"

// JDK 目标架构的规范值 (canonical values)。
// config 的 arch 配置项、各 provider 内部的架构参数统一使用这两个值;
// 各 provider 自行把规范值映射到自家 API/URL 的命名约定
// (恰好 Temurin/Corretto/Microsoft 三家的 Windows 命名都与规范值一致)。
const (
	ArchX64   = "x64"     // 64 位 x86 (amd64)
	ArchARM64 = "aarch64" // 64 位 ARM (arm64)
)

// NormArch 把用户输入的架构字符串规范化为 ArchX64 / ArchARM64。
// 接受常见别名 (amd64 → x64, arm64 → aarch64), 大小写不敏感;
// 无法识别时返回 ("", false)。纯函数, 便于表驱动测试。
func NormArch(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ArchX64, "amd64":
		return ArchX64, true
	case ArchARM64, "arm64":
		return ArchARM64, true
	}
	return "", false
}
