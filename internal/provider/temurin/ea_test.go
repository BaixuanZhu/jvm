package temurin

import (
	"encoding/json"
	"testing"
)

// EA feature_releases 响应样例 (精简自真实 API, 两条 release、各一个 Windows x64 包)
const eaFeatureReleasesJSON = `[
  {
    "release_name": "jdk-28+14-ea-beta",
    "version_data": {"semver": "28.0.0-ea.14-beta", "major": 28},
    "binaries": [{
      "architecture": "x64", "os": "windows", "jvm_impl": "hotspot",
      "package": {
        "name": "OpenJDK-jdk_x64_windows_hotspot_28_14-ea.zip",
        "link": "https://github.com/adoptium/temurin28-binaries/releases/download/jdk-28%2B14-ea-beta/OpenJDK-jdk_x64_windows_hotspot_28_14-ea.zip",
        "checksum": "9e962187865701b0fe8f7d4b74e20b5e690295f7fa828dbd5dc4972f4dff439e",
        "size": 160679146
      }
    }]
  },
  {
    "release_name": "jdk-28+13-ea-beta",
    "version_data": {"semver": "28.0.0-ea.13-beta", "major": 28},
    "binaries": [{
      "architecture": "x64", "os": "windows",
      "package": {
        "name": "OpenJDK-jdk_x64_windows_hotspot_28_13-ea.zip",
        "link": "https://github.com/adoptium/temurin28-binaries/releases/download/jdk-28%2B13-ea-beta/OpenJDK-jdk_x64_windows_hotspot_28_13-ea.zip",
        "checksum": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "size": 160100000
      }
    }]
  }
]`

// /info/release_names?release_type=ea 响应样例
const eaReleaseNamesJSON = `{"releases": [
  "jdk-28+14-ea-beta", "jdk-28+13-ea-beta", "jdk-28+5-ea-beta",
  "jdk-27+35-ea-beta", "jdk-27+29-ea-beta"
]}`

// TestParseReleaseNamesMajors 验证 EA 大版本归并: 去重、降序。
func TestParseReleaseNamesMajors(t *testing.T) {
	majors, err := parseReleaseNamesMajors([]byte(eaReleaseNamesJSON))
	if err != nil {
		t.Fatalf("parseReleaseNamesMajors 报错: %v", err)
	}
	want := []int{28, 27}
	if len(majors) != len(want) {
		t.Fatalf("majors = %v, want %v", majors, want)
	}
	for i, m := range want {
		if majors[i] != m {
			t.Errorf("majors[%d] = %d, want %d", i, majors[i], m)
		}
	}
}

func TestMajorOfReleaseName(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"jdk-28+14-ea-beta", 28},
		{"jdk-27+35-ea-beta", 27},
		{"jdk-25.0.1+13", 25}, // 容错 GA 形式
		{"28+14-ea-beta", 28}, // 无 jdk- 前缀
		{"", 0},
		{"jdk-ea-beta", 0},
		{"garbage", 0},
	}
	for _, tt := range tests {
		if got := majorOfReleaseName(tt.in); got != tt.want {
			t.Errorf("majorOfReleaseName(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestEAAssetFromRecord 验证 EA Asset 构造: Distro 为 temurin-ea、
// 不填 MirrorURL、ReleaseName 剥 jdk- 前缀。
func TestEAAssetFromRecord(t *testing.T) {
	var records releaseResponse
	if err := json.Unmarshal([]byte(eaFeatureReleasesJSON), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("fixture 应有 2 条记录, got %d", len(records))
	}

	a, err := eaAssetFromRecord(records[0], "测试")
	if err != nil {
		t.Fatalf("eaAssetFromRecord 报错: %v", err)
	}
	if a.Distro != "temurin-ea" {
		t.Errorf("Distro = %q, want temurin-ea", a.Distro)
	}
	if a.ReleaseName != "28+14-ea-beta" {
		t.Errorf("ReleaseName = %q, want 28+14-ea-beta", a.ReleaseName)
	}
	if a.MirrorURL != "" {
		t.Errorf("EA 不走镜像, MirrorURL 应为空, got %q", a.MirrorURL)
	}
	if a.Checksum != "9e962187865701b0fe8f7d4b74e20b5e690295f7fa828dbd5dc4972f4dff439e" {
		t.Errorf("Checksum = %q", a.Checksum)
	}
	if a.ZipURL == "" || a.Major != 28 {
		t.Errorf("ZipURL/Major 未填齐: %q / %d", a.ZipURL, a.Major)
	}
}

// TestEAResolveReleaseName 验证 EA 版本号规整。
func TestEAResolveReleaseName(t *testing.T) {
	e := ea{}
	if got, err := e.ResolveReleaseName("28+14-ea-beta"); err != nil || got != "jdk-28+14-ea-beta" {
		t.Errorf("补前缀: got (%q, %v)", got, err)
	}
	if got, err := e.ResolveReleaseName("jdk-28+14-ea-beta"); err != nil || got != "jdk-28+14-ea-beta" {
		t.Errorf("透传: got (%q, %v)", got, err)
	}
	if _, err := e.ResolveReleaseName("28"); err == nil {
		t.Error("缺 build 号应报错")
	}
}

// TestEANameAndDisplay 验证注册身份。
func TestEANameAndDisplay(t *testing.T) {
	e := ea{}
	if e.Name() != "temurin-ea" {
		t.Errorf("Name = %q", e.Name())
	}
	if e.DisplayName() == "" {
		t.Error("DisplayName 不应为空")
	}
}
