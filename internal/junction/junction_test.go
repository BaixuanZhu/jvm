package junction

import (
	"os"
	"path/filepath"
	"testing"

	"jvm/internal/paths"
)

// 这些测试覆盖纯函数 SplitDistro / MajorOf / pureMajor / versionParts / semverLess。
// ResolveVersion 的规则: 纯大版本号 → 取最新 build; 完整版本号 (含 build 号) → 精确匹配。
// 不接受半截版本号 (跨发行版本号格式不一, 语义模糊)。junction 的 Create/Remove/ReadTarget 依赖 Windows syscall, 暂不测。

// TestSplitDistro 覆盖目录名 → (distro, version) 拆分。
// 新目录 {distro}-{version} 拆出前缀; 旧的无前缀目录 / 纯版本号输入默认 temurin。
func TestSplitDistro(t *testing.T) {
	tests := []struct {
		in          string
		wantDistro  string
		wantVersion string
	}{
		// 新式: {distro}-{version}
		{"temurin-21.0.5+11", "temurin", "21.0.5+11"},
		{"corretto-21.0.12.8.1", "corretto", "21.0.12.8.1"},
		{"microsoft-21.0.12", "microsoft", "21.0.12"},
		// 旧的无前缀目录 / 纯版本号输入 → 默认 temurin
		{"21.0.12+8", "temurin", "21.0.12+8"},
		{"jdk-21.0.12+8", "jdk", "21.0.12+8"}, // jdk 前缀也被拆 (迁移期残留, MigrateLegacyDirs 会清)
		{"8.0.502+7", "temurin", "8.0.502+7"},
		{"21", "temurin", "21"},
		// 边界
		{"", "temurin", ""},
		{"-21", "temurin", "-21"},           // 前缀为空 → 无 distro
		{"123-21", "temurin", "123-21"},     // 前缀含数字 → 无 distro
		{"temurin-", "temurin", "temurin-"}, // version 为空 → 无 distro
	}
	for _, tt := range tests {
		d, v := SplitDistro(tt.in)
		if d != tt.wantDistro || v != tt.wantVersion {
			t.Errorf("SplitDistro(%q) = (%q, %q), want (%q, %q)", tt.in, d, v, tt.wantDistro, tt.wantVersion)
		}
	}
}

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

		// {distro}-{version} 新目录命名
		{"temurin-21.0.5+11", 21},
		{"corretto-21.0.12.8.1", 21},
		{"microsoft-17.0.20", 17},

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
		got := MajorOf(tt.in)
		if got != tt.want {
			t.Errorf("MajorOf(%q) = %d, want %d", tt.in, got, tt.want)
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

// stripPrefix 去掉 distro- / jdk- 前缀, 让 "21.0.12+8" 能匹配 "temurin-21.0.12+8"
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
		// {distro}-{version} 新目录
		{"temurin-21.0.5+11", "21.0.5+11"},
		{"corretto-21.0.12.8.1", "21.0.12.8.1"},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripPrefix(tt.in)
		if got != tt.want {
			t.Errorf("stripPrefix(%q) = %q, want %q", tt.in, got, tt.want)
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
		// {distro}-{version} 新目录: 先剥 distro 再解析, distro 字母段不参与
		{"temurin-21.0.5+11", []int{21, 0, 5, 11}},
		{"corretto-8.0.502+7", []int{8, 0, 502, 7}},
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

		// {distro}-{version} 新目录: distro 前缀不影响比较
		{"temurin-21.0.5+11", "temurin-21.0.12+8", true},
		{"corretto-21.0.12.8.1", "corretto-21.0.11.10.1", false},
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
	// 模拟已装: 混合新目录 (带 distro 前缀) + 旧目录 (无前缀, 视为 temurin)
	// temurin: 新目录 temurin-21.0.5+11, 旧目录 21.0.12+8 / 17.0.20+8 / 8.0.502+7
	// corretto: corretto-21.0.12.8.1
	withTempVersions(t,
		"temurin-21.0.5+11", "21.0.12+8", "17.0.20+8", "8.0.502+7",
		"corretto-21.0.12.8.1",
	)

	tests := []struct {
		name    string
		distro  string
		input   string
		want    string
		wantErr bool
	}{
		// temurin: 旧无前缀目录 + 新目录都能命中 (向后兼容)
		{"temurin 纯大版本取最新 (命中旧目录)", "temurin", "21", "21.0.12+8", false},
		{"temurin JDK8 旧目录", "temurin", "8", "8.0.502+7", false},
		{"temurin 完整版本号 (旧目录)", "temurin", "21.0.12+8", "21.0.12+8", false},
		{"temurin 完整版本号 (新目录)", "temurin", "21.0.5+11", "temurin-21.0.5+11", false},
		{"temurin 半截 core (无 build 号) → 报错", "temurin", "21.0.12", "", true},

		// corretto: 只能命中 corretto- 前缀目录
		{"corretto 大版本取最新", "corretto", "21", "corretto-21.0.12.8.1", false},
		{"corretto 完整版本号", "corretto", "21.0.12.8.1", "corretto-21.0.12.8.1", false},

		// distro 过滤: temurin 查不到 corretto 的版本, 反之亦然
		{"corretto 查 JDK8 (未装)", "corretto", "8", "", true},
		{"不存在的 distro", "zulu", "21", "", true},

		// 不命中的情况
		{"temurin 不存在的版本", "temurin", "21.0.99+8", "", true},
		{"temurin 未安装的大版本", "temurin", "99", "", true},
		{"空串", "temurin", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveVersion(tt.distro, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveVersion(%q, %q) 期望报错, got %q", tt.distro, tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveVersion(%q, %q) 意外报错: %v", tt.distro, tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveVersion(%q, %q) = %q, want %q", tt.distro, tt.input, got, tt.want)
			}
		})
	}
}

// TestDisplayName 覆盖目录名 → 统一显示形式的归一化。
func TestDisplayName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		// 旧无前缀目录补 temurin-
		{"21.0.12+8", "temurin-21.0.12+8"},
		{"8.0.502+7", "temurin-8.0.502+7"},
		// 已带前缀的原样
		{"temurin-21.0.5+11", "temurin-21.0.5+11"},
		{"corretto-21.0.12.8.1", "corretto-21.0.12.8.1"},
	}
	for _, tt := range tests {
		got := DisplayName(tt.in)
		if got != tt.want {
			t.Errorf("DisplayName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestMigrateLegacyDirs 覆盖三种目录的处理:
//   - 遗留 jdk- 前缀目录 → 迁移到纯 semver (回归保护: SplitDistro 会把 jdk-21.0.12+8
//     拆成 ("jdk", "21.0.12+8"), 新格式跳过逻辑必须不误伤这种该迁移的目录)
//   - 纯 semver 旧目录 → 跳过 (已是规范)
//   - {distro}-{semver} 新目录 → 跳过 (新命名规范)
func TestMigrateLegacyDirs(t *testing.T) {
	withTempVersions(t,
		"jdk-21.0.12+8",        // 遗留: 应迁移成 21.0.12+8
		"jdk8u502-b07",         // 遗留: 应迁移成 8.0.502+7
		"17.0.20+8",            // 纯 semver: 跳过
		"temurin-11.0.32+9",    // 新命名: 跳过
		"corretto-21.0.12.8.1", // 新命名: 跳过
	)

	if err := MigrateLegacyDirs(); err != nil {
		t.Fatalf("MigrateLegacyDirs: %v", err)
	}

	entries, err := os.ReadDir(paths.VersionsDir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}

	// 迁移后应存在的目录
	for _, want := range []string{"21.0.12+8", "8.0.502+7", "17.0.20+8", "temurin-11.0.32+9", "corretto-21.0.12.8.1"} {
		if !got[want] {
			t.Errorf("迁移后缺少目录 %q", want)
		}
	}
	// 旧名应已不存在
	for _, gone := range []string{"jdk-21.0.12+8", "jdk8u502-b07"} {
		if got[gone] {
			t.Errorf("迁移后旧目录 %q 仍存在 (应已重命名)", gone)
		}
	}
}
