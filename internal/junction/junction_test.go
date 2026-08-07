package junction

import (
	"os"
	"path/filepath"
	"testing"

	"jvm/internal/paths"
)

// 这些测试覆盖纯函数 majorOf / pureMajor / versionParts / semverLess / versionCore。
// ResolveVersion 的规则: 纯大版本号 → 取最新 build; 完整版本号 → 精确匹配;
// 少 build 号 core → 前缀匹配取最新。junction 的 Create/Remove/ReadTarget 依赖 Windows syscall, 暂不测。

func TestMajorOf(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		// 新式 (JDK 9+): jdk-{ver}+{build}
		{"jdk-21.0.12+8", 21},
		{"jdk-17.0.13+11", 17},
		{"jdk-11.0.25+9", 11},
		{"21.0.12+8", 21}, // 不带 jdk- 前缀也能解
		{"jdk-21", 21},

		// 旧式 (JDK 8 及更早): jdk{N}u{update}-b{build}
		{"jdk8u502-b07", 8},
		{"jdk8u402-b06", 8},
		{"jdk7u80-b15", 7},
		{"8u502-b07", 8}, // 不带 jdk 前缀也能解

		// 大小写 / 空格容错
		{"JDK-21.0.12+8", 21},
		{"  jdk-21.0.12+8  ", 21},

		// 解析失败的非法输入
		{"", 0},
		{"jdk-", 0},
		{"jdk", 0},
		{"readme.txt", 0},
		{".tmp-extract-foo", 0},
		{"current", 0},
		{"abc", 0},
	}
	for _, tt := range tests {
		got := majorOf(tt.in)
		if got != tt.want {
			t.Errorf("majorOf(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// pureMajor 是 ResolveVersion 判断"是否走大版本取最新"的唯一依据:
// 只有纯数字 ("8" / "21") 才算, 含点/u/+ 一律不算。
func TestPureMajor(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"8", 8, true},
		{"21", 21, true},
		{"  17  ", 17, true}, // 容许首尾空格
		// 以下都应判 ok=false (不当作大版本号)
		{"8u502", 0, false},
		{"21.0.5", 0, false},
		{"21.0.5+11", 0, false},
		{"jdk-21.0.12+8", 0, false},
		{"jdk8u502-b07", 0, false},
		{"", 0, false},
		{"abc", 0, false},
		{"0", 0, false}, // 非正整数
		{"-1", 0, false},
	}
	for _, tt := range tests {
		got, ok := pureMajor(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("pureMajor(%q) = (%d, %v), want (%d, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// stripPrefix 让带不带 jdk- 前缀的版本号能互相匹配 (与 install/available 对齐)
func TestStripPrefix(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"jdk-21.0.12+8", "21.0.12+8"},
		{"21.0.12+8", "21.0.12+8"},     // 不带前缀不变
		{"JDK-21.0.12+8", "21.0.12+8"}, // 大小写
		{"jdk8u502-b07", "8u502-b07"},
		{"8u502-b07", "8u502-b07"},
		{"  jdk-21  ", "21"},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripPrefix(tt.in)
		if got != tt.want {
			t.Errorf("stripPrefix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestVersionCore(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"21.0.12+8", "21.0.12"},         // 标准: 去 build
		{"jdk-21.0.12+8", "21.0.12"},     // 带前缀
		{"21.0.12", "21.0.12"},           // 无 build: 原样
		{"JDK-17.0.20+8", "17.0.20"},     // 大小写前缀
		{"8.0.502+7", "8.0.502"},         // JDK8 形式
		{"  jdk-21.0.12+8  ", "21.0.12"}, // 带空格
		{"21", "21"},                     // 仅大版本号
		{"", ""},
	}
	for _, tt := range tests {
		got := versionCore(tt.in)
		if got != tt.want {
			t.Errorf("versionCore(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestVersionParts(t *testing.T) {
	tests := []struct {
		in   string
		want []int
	}{
		// 新规范: 目录名采用纯 semver 形式 X.Y.Z+N
		{"21.0.12+8", []int{21, 0, 12, 8}},
		{"17.0.13+11", []int{17, 0, 13, 11}},
		{"8.0.502+7", []int{8, 0, 502, 7}},
		{"8.0.402+6", []int{8, 0, 402, 6}},
		// 容错: 带 jdk- 前缀的历史输入也能正确解析
		{"jdk-21.0.12+8", []int{21, 0, 12, 8}},
	}
	for _, tt := range tests {
		got := versionParts(tt.in)
		if !intSliceEqual(got, tt.want) {
			t.Errorf("versionParts(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestSemverLess(t *testing.T) {
	tests := []struct {
		a, b string
		less bool
	}{
		// 纯 semver 命名: 修字符串排序 bug 的核心
		{"21.0.5+11", "21.0.12+8", true},  // 0.5 < 0.12 (字符串排序会判反)
		{"21.0.12+8", "21.0.5+11", false}, // 0.12 > 0.5
		{"21.0.12+7", "21.0.12+8", true},  // 同版本比 build
		{"21.0.12+8", "21.0.12+8", false}, // 相等不算小于

		// JDK8 的纯 semver 形式: update 号大的更新
		{"8.0.402+6", "8.0.502+7", true},  // 402 < 502
		{"8.0.502+7", "8.0.402+6", false}, // 502 > 402
		{"8.0.502+6", "8.0.502+7", true},  // 同 update 比 build

		// 大版本
		{"17.0.13+11", "21.0.12+8", true}, // 17 < 21
		{"21.0.12+8", "17.0.13+11", false},

		// 跨大版本 (JDK8 vs JDK21): 按大版本号比, 不看字符串
		{"8.0.502+7", "21.0.12+8", true}, // 8 < 21
	}
	for _, tt := range tests {
		got := semverLess(tt.a, tt.b)
		if got != tt.less {
			t.Errorf("semverLess(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.less)
		}
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLegacyToNewName 覆盖旧版 jvm 遗留目录名 → 新纯 semver 形式的映射。
// MigrateLegacyDirs 靠它把旧目录就地 rename, 使升级后 list/use/uninstall 一致。
func TestLegacyToNewName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		// 新式遗留: 去掉 jdk- 前缀
		{"jdk-21.0.12+8", "21.0.12+8"},
		{"jdk-17.0.13+11", "17.0.13+11"},

		// 旧式遗留 (JDK8): jdk{N}u{U}-b{B} → {N}.0.{U}+{B}, build 去前导零
		{"jdk8u502-b07", "8.0.502+7"},
		{"jdk8u402-b06", "8.0.402+6"},
		{"jdk7u80-b15", "7.0.80+15"},

		// 已经是新规范 → 原样 (返回 "" 表示无需迁移)
		{"21.0.12+8", ""},
		{"8.0.502+7", ""},

		// 无法识别 → ""
		{"", ""},
		{"readme.txt", ""},
		{"OpenJDK21U-jdk_x64_windows_hotspot_21.0.12_8", ""}, // zip 文件名片段, 非版本目录
		{".tmp-extract-foo", ""},
	}
	for _, tt := range tests {
		got := legacyToNewName(tt.in)
		if got != tt.want {
			t.Errorf("legacyToNewName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// withTempVersions 临时把 paths.VersionsDir 指向一个含给定版本目录的临时目录。
// 返回恢复函数。隔离真实 ~/.jvm/versions。
func withTempVersions(t *testing.T, versions ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, v := range versions {
		os.MkdirAll(filepath.Join(dir, v), 0o755)
	}
	orig := paths.VersionsDir
	paths.VersionsDir = dir
	t.Cleanup(func() { paths.VersionsDir = orig })
}

func TestResolveVersion(t *testing.T) {
	// 模拟已装: 21.0.12+8, 21.0.5+11, 17.0.20+8, 8.0.502+7
	withTempVersions(t, "21.0.12+8", "21.0.5+11", "17.0.20+8", "8.0.502+7")

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		// 路径 1: 纯大版本号取最新
		{"纯大版本号取最新", "21", "21.0.12+8", false},
		{"纯大版本号 JDK8", "8", "8.0.502+7", false},

		// 路径 2: 完整版本号精确匹配
		{"完整版本号", "21.0.12+8", "21.0.12+8", false},
		{"带 jdk- 前缀", "jdk-21.0.12+8", "21.0.12+8", false},

		// 路径 3: 少 build 号前缀匹配 (本次新增)
		{"少 build 号命中最新", "21.0.12", "21.0.12+8", false},
		{"少 build 号带前缀", "jdk-17.0.20", "17.0.20+8", false},

		// 不命中的情况
		{"不存在的精确版本", "21.0.99+8", "", true},
		{"不存在的 core", "21.0.99", "", true},
		{"不完整小版本", "21.0", "", true},
		{"未安装的大版本", "99", "", true},
		{"空串", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveVersion(%q) 期望报错, got %q", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveVersion(%q) 意外报错: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
