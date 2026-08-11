package liberica

import (
	"encoding/json"
	"testing"

	"jvm/internal/app"
)

func TestNameAndDisplayName(t *testing.T) {
	p := liberica{}
	if p.Name() != "liberica" {
		t.Errorf("Name = %q, want %q", p.Name(), "liberica")
	}
	if p.DisplayName() != "BellSoft Liberica JDK" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName(), "BellSoft Liberica JDK")
	}
}

func TestAPIArchParams(t *testing.T) {
	cases := []struct {
		in          string
		wantArch    string
		wantBitness string
	}{
		{app.ArchX64, "x86", "64"},
		{app.ArchARM64, "arm", "64"},
		{"unknown", "x86", "64"}, // 未知回退 x86/64
		{"", "x86", "64"},
	}
	for _, c := range cases {
		gotArch, gotBitness := apiArchParams(c.in)
		if gotArch != c.wantArch || gotBitness != c.wantBitness {
			t.Errorf("apiArchParams(%q) = (%q,%q), want (%q,%q)",
				c.in, gotArch, gotBitness, c.wantArch, c.wantBitness)
		}
	}
}

func TestIsPureMajor(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"21", true},
		{"21.0.5", false},
		{"21.0.5+11", false},
		{"", true},
	}
	for _, c := range cases {
		if got := isPureMajor(c.in); got != c.want {
			t.Errorf("isPureMajor(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDownloadURL(t *testing.T) {
	cases := []struct {
		version, filename, want string
	}{
		{"21.0.5+11", "bellsoft-jdk21.0.5+11-windows-amd64.zip",
			"https://download.bell-sw.com/java/21.0.5+11/bellsoft-jdk21.0.5+11-windows-amd64.zip"},
		{"25.0.4+9", "bellsoft-jdk25.0.4+9-windows-aarch64.zip",
			"https://download.bell-sw.com/java/25.0.4+9/bellsoft-jdk25.0.4+9-windows-aarch64.zip"},
	}
	for _, c := range cases {
		if got := downloadURL(c.version, c.filename); got != c.want {
			t.Errorf("downloadURL(%q,%q) = %q, want %q", c.version, c.filename, got, c.want)
		}
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
		{"别名 amd64", "amd64", "x64"},
		{"别名 arm64", "arm64", "aarch64"},
		{"带空格", "  aarch64  ", "aarch64"},
		{"非法回退 x64", "mips", "x64"},
		{"空值保持默认", "", "x64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arch = "x64" // 每个子用例从已知状态开始
			liberica{}.Configure(tt.in, "https://ignored.mirror")
			if arch != tt.want {
				t.Errorf("Configure(%q) 后 arch = %q, want %q", tt.in, arch, tt.want)
			}
		})
	}
}

func TestAvailable(t *testing.T) {
	p := liberica{}
	rels, err := p.Available()
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) == 0 {
		t.Fatal("Available 返回空")
	}
	for _, r := range rels {
		if r.Major == 0 {
			t.Errorf("Available 含零版本: %+v", r)
		}
		if !r.LTS {
			t.Errorf("Available 中 %d 应标记 LTS", r.Major)
		}
	}
}

// TestFilterLatestInFeature 验证按 feature + latestInFeature + GA 的过滤逻辑 (不触网)。
func TestFilterLatestInFeature(t *testing.T) {
	all := []libericaRelease{
		{Version: "21.0.5+11", FeatureVersion: 21, LatestInFeatureVersion: true, GA: true, SHA1: "aaa"},
		{Version: "21.0.4+9", FeatureVersion: 21, LatestInFeatureVersion: false, GA: true, SHA1: "bbb"},
		{Version: "21.0.1-ea+1", FeatureVersion: 21, LatestInFeatureVersion: false, GA: false, SHA1: "ccc"}, // EA 非 GA, 应排除
		{Version: "17.0.13+11", FeatureVersion: 17, LatestInFeatureVersion: true, GA: true, SHA1: "ddd"},
	}
	got := filterLatestInFeature(all, 21)
	if len(got) != 1 {
		t.Fatalf("feature 21 latest 数量 = %d, want 1", len(got))
	}
	if got[0].Version != "21.0.5+11" {
		t.Errorf("got %q, want 21.0.5+11", got[0].Version)
	}
	if g := filterLatestInFeature(all, 17); len(g) != 1 || g[0].Version != "17.0.13+11" {
		t.Errorf("feature 17 过滤错误: %+v", g)
	}
	if g := filterLatestInFeature(all, 99); len(g) != 0 {
		t.Errorf("feature 99 应为空, got %d", len(g))
	}
}

func TestBuildAsset(t *testing.T) {
	r := libericaRelease{
		Version:        "21.0.5+11",
		FeatureVersion: 21,
		Filename:       "bellsoft-jdk21.0.5+11-windows-amd64.zip",
		SHA1:           "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
		GA:             true,
	}
	a := buildAsset(r, 21)
	if a.Semver != "21.0.5+11" {
		t.Errorf("Semver = %q", a.Semver)
	}
	if a.Major != 21 {
		t.Errorf("Major = %d", a.Major)
	}
	wantURL := "https://download.bell-sw.com/java/21.0.5+11/bellsoft-jdk21.0.5+11-windows-amd64.zip"
	if a.ZipURL != wantURL {
		t.Errorf("ZipURL = %q, want %q", a.ZipURL, wantURL)
	}
	if a.Checksum != "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d" {
		t.Errorf("Checksum = %q", a.Checksum)
	}
	if a.ChecksumAlgo != "sha1" {
		t.Errorf("ChecksumAlgo = %q, want sha1", a.ChecksumAlgo)
	}
	if a.ReleaseName != "21.0.5+11" {
		t.Errorf("ReleaseName = %q", a.ReleaseName)
	}
	if a.Distro != "liberica" {
		t.Errorf("Distro = %q", a.Distro)
	}
	if a.MirrorURL != "" {
		t.Errorf("MirrorURL = %q, 应为空 (直连 download.bell-sw.com)", a.MirrorURL)
	}
}

// TestParseReleaseJSON 用内联 fixture (真实 API 返回样本) 验证 JSON 解析 (不触网)。
func TestParseReleaseJSON(t *testing.T) {
	const fixture = `[{"bitness":64,"latestLTS":false,"updateVersion":4,"downloadUrl":"https://github.com/bell-sw/Liberica/releases/download/25.0.4+9/bellsoft-jdk25.0.4+9-windows-amd64.zip","latestInFeatureVersion":true,"LTS":true,"bundleType":"jdk","featureVersion":25,"packageType":"zip","FX":false,"GA":true,"architecture":"x86","latest":false,"extraVersion":0,"buildVersion":9,"EOL":false,"os":"windows","interimVersion":0,"version":"25.0.4+9","sha1":"9f107bdaffeff35e2cef7c3c915fb376d58c7038","filename":"bellsoft-jdk25.0.4+9-windows-amd64.zip","size":249112859,"patchVersion":0,"TCK":true,"updateType":"psu"}]`
	var list []libericaRelease
	if err := json.Unmarshal([]byte(fixture), &list); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("长度 = %d, want 1", len(list))
	}
	r := list[0]
	if r.Version != "25.0.4+9" {
		t.Errorf("Version = %q", r.Version)
	}
	if r.FeatureVersion != 25 {
		t.Errorf("FeatureVersion = %d", r.FeatureVersion)
	}
	if !r.GA {
		t.Errorf("GA = false, want true")
	}
	if !r.LTS {
		t.Errorf("LTS = false, want true")
	}
	if !r.LatestInFeatureVersion {
		t.Errorf("LatestInFeatureVersion = false, want true")
	}
	if r.SHA1 != "9f107bdaffeff35e2cef7c3c915fb376d58c7038" {
		t.Errorf("SHA1 = %q", r.SHA1)
	}
	if r.Filename != "bellsoft-jdk25.0.4+9-windows-amd64.zip" {
		t.Errorf("Filename = %q", r.Filename)
	}
}
