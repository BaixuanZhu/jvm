package temurin

import (
	"encoding/json"
	"testing"
)

// TestShortSemver 验证 Temurin semver 规整 (剥离 ".LTS" 后缀等)。
// ShortSemver 现在是 temurin 结构体方法 (实现 provider.Provider 接口)。
func TestShortSemver(t *testing.T) {
	p := temurin{}
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
		// 四段式新版本号 (2026-07 CPU 起): semver 仍编码为三段, 第 4 段被编码进
		// build 号 —— 本函数产出的 "25.0.4+101" 是无法反查 API 的失真形式
		// (真实 release_name 是 jdk-25.0.4.1+1), 故 ShortSemver 仅作 fallback,
		// 正常路径优先用 API 顶层 release_name 字段 (见 releaseNameOf)。
		{"25.0.4+101.0.LTS", "25.0.4+101"},
		{"25.0.4.1+1", "25.0.4.1+1"}, // 四段式已是简短形式, 透传
	}
	for _, tt := range tests {
		if got := p.ShortSemver(tt.in); got != tt.want {
			t.Errorf("ShortSemver(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestAssetFromRecordReleaseName 验证 Asset.ReleaseName 优先取 API 顶层
// release_name 字段 (剥 jdk- 前缀), 修复 2026-07 CPU 起四段式版本号
// (jdk-25.0.4.1+1) 下从 semver 反推出 "25.0.4+101" 导致 install 404 的问题。
//
// 用真实 API 响应的精简 JSON 反序列化构造 releaseRecord, 同时覆盖 json tag
// 与解析逻辑; 并断言往返一致 —— 显示的 ReleaseName 经 ResolveReleaseName
// 补回 jdk- 前缀后应原样还原 API 的 release_name, 即 "available 显示的
// 版本号 → install 输入" 能命中。纯本地测试, 不发网络请求。
func TestAssetFromRecordReleaseName(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string // 期望的 ReleaseName (available 显示 / 目录命名用)
	}{
		{
			"四段式新格式 (2026-07 CPU 起)",
			`{"release_name": "jdk-25.0.4.1+1",
			  "version_data": {"semver": "25.0.4+101.0.LTS", "major": 25},
			  "binaries": [{"package": {
			    "name": "OpenJDK25U-jdk_x64_windows_hotspot_25.0.4.1_1.zip",
			    "link": "https://github.com/adoptium/temurin25-binaries/releases/download/jdk-25.0.4.1%2B1/OpenJDK25U-jdk_x64_windows_hotspot_25.0.4.1_1.zip",
			    "checksum": "abc123", "size": 210000000}}]}`,
			"25.0.4.1+1",
		},
		{
			"三段式老格式",
			`{"release_name": "jdk-21.0.5+11",
			  "version_data": {"semver": "21.0.5+11.0.LTS", "major": 21},
			  "binaries": [{"package": {
			    "link": "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.5%2B11/OpenJDK21U-jdk_x64_windows_hotspot_21.0.5_11.zip",
			    "checksum": "def456", "size": 190000000}}]}`,
			"21.0.5+11",
		},
		{
			"release_name 字段缺失回退 ShortSemver",
			`{"version_data": {"semver": "21.0.5+11.0.LTS", "major": 21},
			  "binaries": [{"package": {
			    "link": "https://github.com/adoptium/temurin21-binaries/releases/download/x.zip",
			    "checksum": "def456", "size": 1}}]}`,
			"21.0.5+11",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r releaseRecord
			if err := json.Unmarshal([]byte(tt.json), &r); err != nil {
				t.Fatalf("JSON 反序列化失败: %v", err)
			}
			a, err := assetFromRecord(r, tt.name)
			if err != nil {
				t.Fatalf("assetFromRecord 意外报错: %v", err)
			}
			if a.ReleaseName != tt.want {
				t.Errorf("ReleaseName = %q, want %q", a.ReleaseName, tt.want)
			}
			// 往返一致性: ReleaseName 补回 jdk- 前缀应还原 API 的 release_name
			if r.ReleaseName != "" {
				got, err := temurin{}.ResolveReleaseName(a.ReleaseName)
				if err != nil {
					t.Fatalf("ResolveReleaseName(%q) 意外报错: %v", a.ReleaseName, err)
				}
				if got != r.ReleaseName {
					t.Errorf("往返不一致: ResolveReleaseName(%q) = %q, want %q", a.ReleaseName, got, r.ReleaseName)
				}
			}
		})
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
		{"别名 amd64", "amd64", "https://m1", "x64", "https://m1"},
		{"别名 arm64", "arm64", "https://m2", "aarch64", "https://m2"},
		{"带空格", "  x64  ", "  https://m3  ", "x64", "https://m3"},
		{"非法 arch 回退 x64", "arm32", "https://m4", "x64", "https://m4"},
		{"空 arch 保持默认", "", "https://m5", "x64", "https://m5"},
		{"空 mirror 不覆盖", "aarch64", "  ", "aarch64", origMirror},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 每个子用例从已知状态开始
			arch, mirror = "x64", origMirror
			temurin{}.Configure(tt.inArch, tt.inMirror)
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

// (PageSize 测试已移除: PageSize 方法被砍, ListVersions 改为内部循环翻页拉全量)

// TestNameAndDisplayName 验证核心标识方法。
func TestNameAndDisplayName(t *testing.T) {
	p := temurin{}
	if p.Name() != "temurin" {
		t.Errorf("Name() = %q, want %q", p.Name(), "temurin")
	}
	if p.DisplayName() != "Temurin (Adoptium)" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "Temurin (Adoptium)")
	}
}

// TestResolveReleaseName 验证版本号 → Adoptium release_name 的标准化。
// 砍掉半截 core 匹配后: 完整版本号 (含 build 号) 补 jdk- 前缀;
// 半截形式 (无 build 号) 报错, 引导用户用大版本号取最新或输完整版本号。
// 纯函数, 不发网络请求 (旧实现会查 feature_releases API 反推 build, 已删)。
func TestResolveReleaseName(t *testing.T) {
	p := temurin{}
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		// 已是完整 release_name → 透传
		{"完整 release_name 透传", "jdk-21.0.12+8", "jdk-21.0.12+8", false},
		// 含 build 号 → 补 jdk- 前缀 (尊重用户 build 号, 不再 API 反查)
		{"补 jdk- 前缀", "21.0.12+8", "jdk-21.0.12+8", false},
		{"带空格 trim", "  17.0.13+11  ", "jdk-17.0.13+11", false},
		// 半截 core (无 build 号) → 报错
		{"半截 core 报错", "21.0.12", "", true},
		{"半截 core 带空格报错", "  21.0.5  ", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.ResolveReleaseName(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveReleaseName(%q) 期望报错, got %q", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveReleaseName(%q) 意外报错: %v", tt.in, err)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveReleaseName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
