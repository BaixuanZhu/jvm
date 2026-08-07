package corretto

import "testing"

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
