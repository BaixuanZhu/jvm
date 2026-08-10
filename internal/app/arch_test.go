package app

import "testing"

func TestNormArch(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"x64", ArchX64, true},
		{"amd64", ArchX64, true}, // Go 命名别名
		{"X64", ArchX64, true},   // 大小写不敏感
		{" x64 ", ArchX64, true}, // 前后空格 trim
		{"aarch64", ArchARM64, true},
		{"arm64", ArchARM64, true}, // Go 命名别名
		{"ARM64", ArchARM64, true},
		{"x86", "", false},   // 32 位不支持
		{"arm", "", false},   // 32 位 ARM 不支持
		{"", "", false},      // 空
		{"riscv", "", false}, // 未知架构
	}
	for _, tt := range tests {
		got, ok := NormArch(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("NormArch(%q) = (%q, %v), 期望 (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
