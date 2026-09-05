package graalvm

import (
	"strings"
	"testing"

	"jvm/internal/app"
)

// withArch 保存/恢复包级 arch (Configure 测试与 aarch64 守卫测试共用)。
func withArch(t *testing.T, a string) {
	t.Helper()
	orig := arch
	arch = a
	t.Cleanup(func() { arch = orig })
}

func TestArchiveURL(t *testing.T) {
	tests := []struct {
		version, want string
	}{
		{"21.0.12", "https://download.oracle.com/graalvm/21/archive/graalvm-jdk-21.0.12_windows-x64_bin.zip"},
		{"25.0.4.1", "https://download.oracle.com/graalvm/25/archive/graalvm-jdk-25.0.4.1_windows-x64_bin.zip"},
	}
	for _, tt := range tests {
		if got := archiveURL(tt.version); got != tt.want {
			t.Errorf("archiveURL(%q) = %q, want %q", tt.version, got, tt.want)
		}
	}
}

func TestScanLatest(t *testing.T) {
	t.Run("正常: 起点存在, 顺延到首个缺口", func(t *testing.T) {
		exists := func(n int) bool { return n >= 8 && n <= 12 }
		got, err := scanLatest(8, exists)
		if err != nil || got != 12 {
			t.Errorf("scanLatest = (%d, %v), want (12, nil)", got, err)
		}
	})
	t.Run("起点即最新 (下一号缺失)", func(t *testing.T) {
		exists := func(n int) bool { return n == 1 }
		got, err := scanLatest(1, exists)
		if err != nil || got != 1 {
			t.Errorf("scanLatest = (%d, %v), want (1, nil)", got, err)
		}
	})
	t.Run("起点失效报错", func(t *testing.T) {
		exists := func(n int) bool { return false }
		if _, err := scanLatest(8, exists); err == nil {
			t.Error("起点不存在应报错")
		}
	})
	t.Run("恒真触发上限防御", func(t *testing.T) {
		exists := func(n int) bool { return true }
		if _, err := scanLatest(1, exists); err == nil {
			t.Error("恒真应触发上限报错")
		}
	})
}

func TestParseSidecar(t *testing.T) {
	const hash = "7560971e52b236ddba7a22e7a682a9a185e56a2f6ee9cd6c4ecbb800e9643990"
	tests := []struct {
		name, body, want string
		wantErr          bool
	}{
		{"裸 hex (Oracle 现行格式)", hash, hash, false},
		{"sha256sum 双字段格式容错", hash + "  graalvm-jdk-21.0.12_windows-x64_bin.zip", hash, false},
		{"大写归一为小写", strings.ToUpper(hash), hash, false},
		{"前导空白", "  \n" + hash, hash, false},
		{"非 hex 拒绝", "not-a-hash", "", true},
		{"长度不对拒绝", "abcd", "", true},
		{"空内容拒绝", "   ", "", true},
	}
	for _, tt := range tests {
		got, err := parseSidecar(tt.body)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: 应报错", tt.name)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s: parseSidecar = (%q, %v), want (%q, nil)", tt.name, got, err, tt.want)
		}
	}
}

func TestAvailable(t *testing.T) {
	withArch(t, app.ArchX64)
	releases, err := (graalvm{}).Available()
	if err != nil {
		t.Fatalf("Available 报错: %v", err)
	}
	if len(releases) != 2 || releases[0].Major != 21 || releases[1].Major != 25 {
		t.Errorf("Available = %v, want [{21 LTS} {25 LTS}]", releases)
	}
	if !releases[0].LTS || !releases[1].LTS {
		t.Error("CPU 线版本应标记 LTS")
	}
}

func TestResolveValidation(t *testing.T) {
	withArch(t, app.ArchX64)
	g := graalvm{}

	t.Run("不支持的大版本", func(t *testing.T) {
		if _, err := g.Resolve(app.VersionSpec{Distro: "graalvm", Version: "17"}); err == nil {
			t.Error("17 应报不支持")
		}
	})
	t.Run("版本号格式非法", func(t *testing.T) {
		// 21.0.5+11 是 Temurin 格式, GraalVM CPU 线无 +build 段
		if _, err := g.Resolve(app.VersionSpec{Distro: "graalvm", Version: "21.0.5+11"}); err == nil {
			t.Error("带 +build 的格式应报错")
		}
	})
	t.Run("aarch64 统一拦截", func(t *testing.T) {
		withArch(t, app.ArchARM64)
		defer withArch(t, app.ArchX64)
		if _, err := g.Resolve(app.VersionSpec{Distro: "graalvm", Version: "21"}); err == nil {
			t.Error("aarch64 应被拦截")
		}
		if _, err := g.LatestPatch(21); err == nil {
			t.Error("aarch64 应被拦截")
		}
		if _, err := (graalvm{}).Available(); err == nil {
			t.Error("aarch64 应被拦截")
		}
	})
}

func TestConfigure(t *testing.T) {
	withArch(t, app.ArchX64)
	t.Run("合法值", func(t *testing.T) {
		(graalvm{}).Configure("aarch64", "ignored")
		if arch != app.ArchARM64 {
			t.Errorf("arch = %q, want aarch64", arch)
		}
	})
	t.Run("别名归一", func(t *testing.T) {
		(graalvm{}).Configure("amd64", "")
		if arch != app.ArchX64 {
			t.Errorf("arch = %q, want x64", arch)
		}
	})
	t.Run("非法值回退 x64", func(t *testing.T) {
		(graalvm{}).Configure("arm32", "")
		if arch != app.ArchX64 {
			t.Errorf("arch = %q, want x64 (非法回退)", arch)
		}
	})
	t.Run("空值保持", func(t *testing.T) {
		before := arch
		(graalvm{}).Configure("", "")
		if arch != before {
			t.Error("空值不应改动 arch")
		}
	})
}
