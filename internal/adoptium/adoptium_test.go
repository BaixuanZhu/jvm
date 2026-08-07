package adoptium

import "testing"

func TestShortSemver(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"21.0.5+11.0.LTS", "21.0.5+11"},
		{"21.0.12+8.0.LTS", "21.0.12+8"},
		{"17.0.13+11.0.LTS", "17.0.13+11"},
		{"23+36", "23+36"},         // 无 minor.security, 仅 major+build
		{"23+36.0.LTS", "23+36"},   // build 自带 .LTS 后缀
		{"23.0.1+11", "23.0.1+11"}, // 已是简短形式
		{"23.0.1", "23.0.1"},       // 无 build 号
		{"", ""},                   // 空串
	}
	for _, tt := range tests {
		if got := ShortSemver(tt.in); got != tt.want {
			t.Errorf("ShortSemver(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestConfigureSaveRestore 确保 Configure 修改包级状态后恢复,
// 不污染其他测试 (arch/mirror 是包级 var)。
func TestConfigure(t *testing.T) {
	origArch, origMirror := arch, mirror
	t.Cleanup(func() { arch, mirror = origArch, origMirror })

	tests := []struct {
		name     string
		inArch   string
		inMirror string
		wantArch string
		wantMirr string
	}{
		{"合法 x64", "x64", "https://m1", "x64", "https://m1"},
		{"合法 aarch64", "aarch64", "https://m2", "aarch64", "https://m2"},
		{"带空格", "  x64  ", "  https://m3  ", "x64", "https://m3"},
		{"非法 arch 回退 x64", "arm32", "https://m4", "x64", "https://m4"},
		{"空 arch 保持默认", "", "https://m5", "x64", "https://m5"},
		{"空 mirror 不覆盖", "aarch64", "  ", "aarch64", origMirror},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 每个子用例从已知状态开始
			arch, mirror = "x64", origMirror
			Configure(tt.inArch, tt.inMirror)
			if arch != tt.wantArch {
				t.Errorf("arch = %q, want %q", arch, tt.wantArch)
			}
			if tt.wantMirr != origMirror && mirror != tt.wantMirr {
				t.Errorf("mirror = %q, want %q", mirror, tt.wantMirr)
			}
		})
	}
}

// TestMirrorDownloadURLWithArch 验证 MirrorDownloadURL 用当前 arch 拼接。
func TestMirrorDownloadURLWithArch(t *testing.T) {
	origArch, origMirror := arch, mirror
	t.Cleanup(func() { arch, mirror = origArch, origMirror })

	arch = "aarch64"
	mirror = "https://test.mirror/Adoptium"
	got := MirrorDownloadURL("https://github.com/x/OpenJDK21U-jdk_aarch64_windows_hotspot_21.0.12_8.zip", 21)
	want := "https://test.mirror/Adoptium/21/jdk/aarch64/windows/OpenJDK21U-jdk_aarch64_windows_hotspot_21.0.12_8.zip"
	if got != want {
		t.Errorf("MirrorDownloadURL (aarch64) = %q, want %q", got, want)
	}
}

// TestPageSize 验证分页上限常量合理 (Adoptium 单次最多 50 条)。
// 调用方据此判断 FetchAllAssets 结果是否可能被截断。
func TestPageSize(t *testing.T) {
	if PageSize <= 0 {
		t.Errorf("PageSize 应为正数, got %d", PageSize)
	}
	if PageSize != 50 {
		t.Errorf("PageSize 当前约定为 50 (与 Adoptium API 默认一致), got %d", PageSize)
	}
}
