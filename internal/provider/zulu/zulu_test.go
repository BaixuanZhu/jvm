package zulu

import (
	"encoding/json"
	"testing"

	"jvm/internal/app"
)

func TestNameAndDisplayName(t *testing.T) {
	p := zulu{}
	if p.Name() != "zulu" {
		t.Errorf("Name = %q, want %q", p.Name(), "zulu")
	}
	if p.DisplayName() != "Azul Zulu OpenJDK" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName(), "Azul Zulu OpenJDK")
	}
}

func TestAPIArch(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{app.ArchX64, "x64"},
		{app.ArchARM64, "arm64"},
		{"unknown", "x64"}, // 未知回退 x64
		{"", "x64"},
	}
	for _, c := range cases {
		if got := apiArch(c.in); got != c.want {
			t.Errorf("apiArch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsPlainJDK(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"zulu21.52.15-ca-jdk21.0.12-win_x64.zip", true},
		{"zulu21.52.15-ca-jdk21.0.12-win_aarch64.zip", true},
		{"zulu21.52.15-ca-fx-jdk21.0.12-win_x64.zip", false},   // JavaFX 变体
		{"zulu21.50.19-ca-crac-jdk21.0.11-win_x64.zip", false}, // CRAC 变体
		{"zulu8.78.0.21-ca-jdk8.0.422-win_x64.zip", true},
	}
	for _, c := range cases {
		if got := isPlainJDK(c.name); got != c.want {
			t.Errorf("isPlainJDK(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsPureMajor(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"21", true},
		{"21.0.12", false},
		{"21.0.12+8", false},
		{"", true},
	}
	for _, c := range cases {
		if got := isPureMajor(c.in); got != c.want {
			t.Errorf("isPureMajor(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSemverFromZulu(t *testing.T) {
	cases := []struct {
		java  []int
		build int
		want  string
	}{
		{[]int{21, 0, 12}, 8, "21.0.12+8"},
		{[]int{21, 0, 12}, 0, "21.0.12"}, // 无 build 号
		{[]int{17, 0, 13}, 11, "17.0.13+11"},
		{[]int{8, 0, 422}, 6, "8.0.422+6"},
		{[]int{25}, 0, "25"},
	}
	for _, c := range cases {
		p := zuluPackage{JavaVersion: c.java, OpenJDKBuildNumber: c.build}
		if got := semverFromZulu(p); got != c.want {
			t.Errorf("semverFromZulu(java=%v build=%d) = %q, want %q", c.java, c.build, got, c.want)
		}
	}
}

func TestCompareJavaVersion(t *testing.T) {
	cases := []struct {
		a, b []int
		want int // 只看符号: 正 a>b / 负 a<b / 0 相等
	}{
		{[]int{21, 0, 12}, []int{21, 0, 11}, 1},
		{[]int{21, 0, 11}, []int{21, 0, 12}, -1},
		{[]int{21, 0, 12}, []int{21, 0, 12}, 0},
		{[]int{21, 0}, []int{21, 0, 12}, -1}, // 公共前缀相等, 短的小
		{[]int{22}, []int{21}, 1},
	}
	for _, c := range cases {
		got := compareJavaVersion(c.a, c.b)
		if (c.want == 0 && got != 0) || (c.want > 0 && got <= 0) || (c.want < 0 && got >= 0) {
			t.Errorf("compareJavaVersion(%v, %v) = %d, want sign %d", c.a, c.b, got, c.want)
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
			zulu{}.Configure(tt.in, "https://ignored.mirror")
			if arch != tt.want {
				t.Errorf("Configure(%q) 后 arch = %q, want %q", tt.in, arch, tt.want)
			}
		})
	}
}

func TestAvailable(t *testing.T) {
	p := zulu{}
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

// TestParsePackageJSON 用内联 fixture 验证列表/详情 JSON 解析 (不触网)。
func TestParsePackageJSON(t *testing.T) {
	const listJSON = `[{"availability_type":"CA","distro_version":[21,52,15,0],"download_url":"https://cdn.azul.com/zulu/bin/zulu21.52.15-ca-jdk21.0.12-win_x64.zip","java_version":[21,0,12],"latest":true,"name":"zulu21.52.15-ca-jdk21.0.12-win_x64.zip","openjdk_build_number":8,"package_uuid":"b712429b-594d-4b19-a765-b80c76f0170e","product":"zulu"}]`
	var list []zuluPackage
	if err := json.Unmarshal([]byte(listJSON), &list); err != nil {
		t.Fatalf("解析列表 JSON 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("列表长度 = %d, want 1", len(list))
	}
	p := list[0]
	if p.DownloadURL != "https://cdn.azul.com/zulu/bin/zulu21.52.15-ca-jdk21.0.12-win_x64.zip" {
		t.Errorf("DownloadURL = %q", p.DownloadURL)
	}
	if p.PackageUUID != "b712429b-594d-4b19-a765-b80c76f0170e" {
		t.Errorf("PackageUUID = %q", p.PackageUUID)
	}
	if got := semverFromZulu(p); got != "21.0.12+8" {
		t.Errorf("semver = %q, want 21.0.12+8", got)
	}
	if !isPlainJDK(p.Name) {
		t.Errorf("isPlainJDK(%q) = false, want true", p.Name)
	}

	// 详情 JSON (验证嵌入字段 + sha256_hash)
	const detailJSON = `{"sha256_hash":"abc123def","download_url":"https://cdn.azul.com/zulu/bin/x.zip","name":"x","package_uuid":"uuid1","java_version":[21,0,12],"openjdk_build_number":8}`
	var d zuluPackageDetail
	if err := json.Unmarshal([]byte(detailJSON), &d); err != nil {
		t.Fatalf("解析详情 JSON 失败: %v", err)
	}
	if d.SHA256Hash != "abc123def" {
		t.Errorf("SHA256Hash = %q, want abc123def", d.SHA256Hash)
	}
	if d.OpenJDKBuildNumber != 8 { // 嵌入的列表字段也能解析
		t.Errorf("嵌入 OpenJDKBuildNumber = %d, want 8", d.OpenJDKBuildNumber)
	}
}
