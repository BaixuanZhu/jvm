package corretto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"jvm/internal/app"
)

// TestIsPureMajor 验证纯大版本号判定。
// 纯数字 ("21" / "8") 为 true; 含小数点 / 空串 / 非数字为 false。
func TestIsPureMajor(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"21", true},
		{"8", true},
		{"  17  ", true}, // 容许首尾空格
		// 以下应判 false
		{"21.0.12.8.1", false}, // 完整版本号
		{"21.0.12", false},     // 半截版本号
		{"", false},
		{"abc", false},
		{"21a", false},
	}
	for _, tt := range tests {
		if got := isPureMajor(tt.in); got != tt.want {
			t.Errorf("isPureMajor(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestMajorOf 验证从版本串提取大版本号。
func TestMajorOf(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"21.0.12.8.1", 21}, // JDK 9+ 四段格式
		{"8.502.07.1", 8},   // JDK 8 特殊格式
		{"17.0.20.8.1", 17},
		{"26.0.2.10.1", 26},
		{"21", 21}, // 纯大版本号
		{"abc", 0}, // 解析失败
		{"", 0},
	}
	for _, tt := range tests {
		if got := majorOf(tt.in); got != tt.want {
			t.Errorf("majorOf(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestJdkEntryVersion 验证从 indexmap resource 路径提取版本号。
// resource 格式: /downloads/resources/{version}/amazon-corretto-{version}-windows-x64-jdk.zip
func TestJdkEntryVersion(t *testing.T) {
	tests := []struct {
		name string
		res  string
		want string
	}{
		{"JDK 21 标准", "/downloads/resources/21.0.12.8.1/amazon-corretto-21.0.12.8.1-windows-x64-jdk.zip", "21.0.12.8.1"},
		{"JDK 8 特殊格式", "/downloads/resources/8.502.07.1/amazon-corretto-8.502.07.1-windows-x64-jdk.zip", "8.502.07.1"},
		{"JDK 17", "/downloads/resources/17.0.20.8.1/amazon-corretto-17.0.20.8.1-windows-x64-jdk.zip", "17.0.20.8.1"},
		{"路径过短", "/downloads/", ""},
		{"空串", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := jdkEntry{checksumEntry{Resource: tt.res}}
			if got := e.version(); got != tt.want {
				t.Errorf("version() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestToAsset 验证 jdkEntry → *app.Asset 翻译。
func TestToAsset(t *testing.T) {
	e := jdkEntry{checksumEntry{
		ChecksumSHA: "de9ad88fb2575a1aff4715b192014ba31edd6e411d243371f377cff0560e34bc",
		Resource:    "/downloads/resources/21.0.12.8.1/amazon-corretto-21.0.12.8.1-windows-x64-jdk.zip",
	}}
	a := e.toAsset()
	if a.Semver != "21.0.12.8.1" {
		t.Errorf("Semver = %q, want %q", a.Semver, "21.0.12.8.1")
	}
	if a.Major != 21 {
		t.Errorf("Major = %d, want 21", a.Major)
	}
	if a.ReleaseName != "21.0.12.8.1" {
		t.Errorf("ReleaseName = %q, want %q", a.ReleaseName, "21.0.12.8.1")
	}
	wantURL := "https://corretto.aws/downloads/resources/21.0.12.8.1/amazon-corretto-21.0.12.8.1-windows-x64-jdk.zip"
	if a.ZipURL != wantURL {
		t.Errorf("ZipURL = %q, want %q", a.ZipURL, wantURL)
	}
	if a.SHA256 != e.ChecksumSHA {
		t.Errorf("SHA256 = %q, want %q", a.SHA256, e.ChecksumSHA)
	}
	if a.Distro != "corretto" {
		t.Errorf("Distro = %q, want %q", a.Distro, "corretto")
	}
	if a.MirrorURL != "" {
		t.Errorf("MirrorURL = %q, 应为空 (Corretto 直连 CDN)", a.MirrorURL)
	}
}

// TestNameAndDisplayName 验证核心标识方法。
func TestNameAndDisplayName(t *testing.T) {
	p := corretto{}
	if p.Name() != "corretto" {
		t.Errorf("Name() = %q, want %q", p.Name(), "corretto")
	}
	if p.DisplayName() != "Amazon Corretto" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "Amazon Corretto")
	}
}

// TestConfigure 验证 Configure 设置目标架构 (含别名与非法值回退),
// 并在结束后恢复包级状态, 不污染其他测试。
func TestConfigure(t *testing.T) {
	origArch := arch
	t.Cleanup(func() { arch = origArch })

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"合法 x64", "x64", "x64"},
		{"合法 aarch64", "aarch64", "aarch64"},
		{"别名 arm64", "arm64", "aarch64"},
		{"非法回退 x64", "riscv", "x64"},
		{"空值保持默认", "", "x64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arch = "x64" // 每个子用例从已知状态开始
			corretto{}.Configure(tt.in, "https://ignored.mirror")
			if arch != tt.want {
				t.Errorf("Configure(%q) 后 arch = %q, want %q", tt.in, arch, tt.want)
			}
		})
	}
}

// TestCheckArch 验证架构守卫: x64 放行, aarch64 报统一错误并给出替代建议。
func TestCheckArch(t *testing.T) {
	origArch := arch
	t.Cleanup(func() { arch = origArch })

	arch = app.ArchX64
	if err := checkArch(); err != nil {
		t.Errorf("x64 下 checkArch() 应为 nil, got %v", err)
	}

	arch = app.ArchARM64
	err := checkArch()
	if !errors.Is(err, errNoWindowsARM64) {
		t.Fatalf("aarch64 下 checkArch() 应为 errNoWindowsARM64, got %v", err)
	}
	// 错误信息应引导用户改用有 ARM64 构建的发行版
	if !strings.Contains(err.Error(), "temurin") || !strings.Contains(err.Error(), "microsoft") {
		t.Errorf("错误信息应包含替代发行版建议, got: %v", err)
	}
}

// TestArchGuardBlocksAllEntries 验证 aarch64 下四个公开入口全部快速报错
// (守卫在网络请求之前生效, 故本测试无需 mock HTTP)。
func TestArchGuardBlocksAllEntries(t *testing.T) {
	origArch := arch
	t.Cleanup(func() { arch = origArch })
	arch = app.ArchARM64

	p := corretto{}
	if _, err := p.Available(); !errors.Is(err, errNoWindowsARM64) {
		t.Errorf("Available() 应报 errNoWindowsARM64, got %v", err)
	}
	if _, err := p.LatestPatch(21); !errors.Is(err, errNoWindowsARM64) {
		t.Errorf("LatestPatch() 应报 errNoWindowsARM64, got %v", err)
	}
	if _, err := p.ListVersions(21); !errors.Is(err, errNoWindowsARM64) {
		t.Errorf("ListVersions() 应报 errNoWindowsARM64, got %v", err)
	}
	if _, err := p.Resolve(app.VersionSpec{Distro: "corretto", Version: "21"}); !errors.Is(err, errNoWindowsARM64) {
		t.Errorf("Resolve() 应报 errNoWindowsARM64, got %v", err)
	}
}

// TestIndexMapArchBranches 验证 indexMap 按架构 map 化解析:
// x64 / (未来可能存在的) aarch64 分支都能按键索引, 缺失分支查找得零值。
func TestIndexMapArchBranches(t *testing.T) {
	fixture := `{
		"windows": {
			"x64": {"jdk": {"21": {"zip": {
				"checksum": "md5x64",
				"checksum_sha256": "sha256x64",
				"resource": "/downloads/resources/21.0.12.8.1/amazon-corretto-21.0.12.8.1-windows-x64-jdk.zip"
			}}}},
			"aarch64": {"jdk": {"21": {"zip": {
				"checksum": "md5arm",
				"checksum_sha256": "sha256arm",
				"resource": "/downloads/resources/21.0.12.8.1/amazon-corretto-21.0.12.8.1-windows-aarch64-jdk.zip"
			}}}}
		}
	}`
	var idx indexMap
	if err := json.Unmarshal([]byte(fixture), &idx); err != nil {
		t.Fatalf("解析 fixture 失败: %v", err)
	}
	if got := idx.Windows["x64"].JDK["21"].Zip.ChecksumSHA; got != "sha256x64" {
		t.Errorf("x64 分支 sha256 = %q, want sha256x64", got)
	}
	if got := idx.Windows["aarch64"].JDK["21"].Zip.Resource; !strings.HasSuffix(got, "windows-aarch64-jdk.zip") {
		t.Errorf("aarch64 分支 resource = %q, 应以 windows-aarch64-jdk.zip 结尾", got)
	}
	if _, ok := idx.Windows["riscv"]; ok {
		t.Error("不存在的架构分支应查找失败")
	}
}
