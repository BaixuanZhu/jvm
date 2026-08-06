package junction

import "testing"

// 这些测试覆盖纯函数 majorOf / normVersion / matchVersion。
// 重点回归: Temurin 旧式命名 (jdk8u502-b07) 此前因 jdk- 前缀硬编码而识别不了,
// 导致 jvm list 不显示、jvm use 8 匹配不到。
// junction 本身的 Create/Remove/ReadTarget 依赖 Windows syscall, 暂不测。

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

func TestNormVersion(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		// 去 jdk- / jdk 前缀, 转小写
		{"jdk-21.0.12+8", "21.0.12+8"},
		{"JDK-21.0.12+8", "21.0.12+8"},
		{"21.0.12+8", "21.0.12+8"},
		{"jdk8u502-b07", "8u502-b07"},
		{"JDK8U502-B07", "8u502-b07"},
		{"8u502-b07", "8u502-b07"},
		{"  jdk-21  ", "21"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normVersion(tt.in)
		if got != tt.want {
			t.Errorf("normVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCoreVersion(t *testing.T) {
	// 输入假定已 normVersion 归一化 (去前缀、小写)
	tests := []struct {
		in, want string
	}{
		{"21.0.5+11", "21.0.5"}, // 新式 +build
		{"21.0.12+8", "21.0.12"},
		{"21.0.12", "21.0.12"}, // 无 build
		{"8u502-b07", "8u502"}, // 旧式 -bNN
		{"8u402-b06", "8u402"},
		{"8u502", "8u502"}, // 无 build
		{"21", "21"},       // 纯大版本
	}
	for _, tt := range tests {
		got := coreVersion(tt.in)
		if got != tt.want {
			t.Errorf("coreVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMatchVersion(t *testing.T) {
	tests := []struct {
		dir, want string
		match     bool
	}{
		// JDK8 旧式命名 (核心回归点)。
		// matchVersion 只比大版本号, 所以 "8" / "8u502" 都能匹配 jdk8u502-b07。
		{"jdk8u502-b07", "8", true},
		{"jdk8u502-b07", "8u502", true},
		{"jdk8u502-b07", "8u502-b07", true},

		// JDK 9+ 新式命名
		{"jdk-21.0.12+8", "21", true},
		{"jdk-21.0.12+8", "21.0.12+8", true},
		{"jdk-17.0.13+11", "17", true},

		// 不匹配
		{"jdk-21.0.12+8", "17", false},
		{"jdk8u502-b07", "21", false},
		{"jdk-21.0.12+8", "abc", false}, // want 非法
	}
	for _, tt := range tests {
		got := matchVersion(tt.dir, tt.want)
		if got != tt.match {
			t.Errorf("matchVersion(%q, %q) = %v, want %v", tt.dir, tt.want, got, tt.match)
		}
	}
}

func TestVersionParts(t *testing.T) {
	tests := []struct {
		in   string
		want []int
	}{
		{"jdk-21.0.12+8", []int{21, 0, 12, 8}},
		{"jdk-17.0.13+11", []int{17, 0, 13, 11}},
		{"jdk8u502-b07", []int{8, 502, 7}},
		{"jdk8u402-b06", []int{8, 402, 6}},
		{"21.0.12+8", []int{21, 0, 12, 8}}, // 不带前缀
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
		// 新式命名: 修字符串排序 bug 的核心
		{"jdk-21.0.5+11", "jdk-21.0.12+8", true},  // 0.5 < 0.12 (字符串排序会判反)
		{"jdk-21.0.12+8", "jdk-21.0.5+11", false}, // 0.12 > 0.5
		{"jdk-21.0.12+7", "jdk-21.0.12+8", true},  // 同版本比 build
		{"jdk-21.0.12+8", "jdk-21.0.12+8", false}, // 相等不算小于

		// 旧式命名: update 号大的更新
		{"jdk8u402-b06", "jdk8u502-b07", true},  // 402 < 502
		{"jdk8u502-b07", "jdk8u402-b06", false}, // 502 > 402
		{"jdk8u502-b06", "jdk8u502-b07", true},  // 同 update 比 build

		// 大版本
		{"jdk-17.0.13+11", "jdk-21.0.12+8", true}, // 17 < 21
		{"jdk-21.0.12+8", "jdk-17.0.13+11", false},

		// 跨命名 (JDK8 vs JDK21): 按大版本号比, 不看字符串
		{"jdk8u502-b07", "jdk-21.0.12+8", true}, // 8 < 21
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
