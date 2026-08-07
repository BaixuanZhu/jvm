package config

import (
	"os"
	"path/filepath"
	"testing"

	"jvm/internal/paths"
)

// withTempRoot 把 paths.Root 临时指向一个临时目录并恢复。
// paths.Root 是包级 var, 测试期间替换它以隔离真实 ~/.jvm。
func withTempRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := paths.Root
	paths.Root = dir
	t.Cleanup(func() { paths.Root = orig })
	return dir
}

// setEnv 临时设置环境变量并在测试结束后恢复。
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	os.Setenv(key, value)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, orig)
		} else {
			os.Unsetenv(key)
		}
	})
}

// clearEnv 清掉 JVM_MIRROR / JVM_ARCH, 避免真实环境干扰测试。
func clearJVMEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"JVM_MIRROR", "JVM_ARCH"} {
		if v, had := os.LookupEnv(key); had {
			os.Unsetenv(key)
			t.Cleanup(func() { os.Setenv(key, v) })
		}
	}
}

func TestDefault(t *testing.T) {
	d := Default()
	if d.Mirror == "" {
		t.Error("默认 mirror 不应为空")
	}
	if d.Arch != "x64" {
		t.Errorf("默认 arch 应为 x64, got %q", d.Arch)
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	withTempRoot(t)
	clearJVMEnv(t)
	// 配置文件不存在 → 返回默认值
	cfg := Load()
	if cfg.Mirror != Default().Mirror {
		t.Errorf("缺失文件时 mirror 应为默认值, got %q", cfg.Mirror)
	}
	if cfg.Arch != "x64" {
		t.Errorf("缺失文件时 arch 应为 x64, got %q", cfg.Arch)
	}
}

func TestLoadConfigFilePresent(t *testing.T) {
	root := withTempRoot(t)
	clearJVMEnv(t)
	// 写一个合法的 TOML 配置
	content := `mirror = "https://my.mirror.example/Adoptium"
arch = "aarch64"
`
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load()
	if cfg.Mirror != "https://my.mirror.example/Adoptium" {
		t.Errorf("mirror 应取文件值, got %q", cfg.Mirror)
	}
	if cfg.Arch != "aarch64" {
		t.Errorf("arch 应取文件值, got %q", cfg.Arch)
	}
}

func TestLoadConfigFilePartial(t *testing.T) {
	root := withTempRoot(t)
	clearJVMEnv(t)
	// 文件只设了 mirror, arch 应回退默认
	content := `mirror = "https://only.mirror/Adoptium"
`
	os.WriteFile(filepath.Join(root, "config.toml"), []byte(content), 0o644)
	cfg := Load()
	if cfg.Mirror != "https://only.mirror/Adoptium" {
		t.Errorf("mirror 应取文件值, got %q", cfg.Mirror)
	}
	if cfg.Arch != "x64" {
		t.Errorf("arch 应回退默认 x64, got %q", cfg.Arch)
	}
}

func TestLoadConfigFileInvalid(t *testing.T) {
	root := withTempRoot(t)
	clearJVMEnv(t)
	// 非法 TOML → 打印警告 + 回退默认 (不 panic)
	os.WriteFile(filepath.Join(root, "config.toml"), []byte("this is = = not valid toml [[["), 0o644)
	cfg := Load()
	if cfg.Mirror != Default().Mirror {
		t.Errorf("非法文件应回退默认 mirror, got %q", cfg.Mirror)
	}
	if cfg.Arch != "x64" {
		t.Errorf("非法文件应回退默认 x64, got %q", cfg.Arch)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	root := withTempRoot(t)
	// 文件设了值
	content := `mirror = "https://file.mirror/Adoptium"
arch = "aarch64"
`
	os.WriteFile(filepath.Join(root, "config.toml"), []byte(content), 0o644)
	// 环境变量应覆盖文件
	setEnv(t, "JVM_MIRROR", "https://env.mirror/Adoptium")
	setEnv(t, "JVM_ARCH", "x64")

	cfg := Load()
	if cfg.Mirror != "https://env.mirror/Adoptium" {
		t.Errorf("环境变量应覆盖文件, mirror got %q", cfg.Mirror)
	}
	if cfg.Arch != "x64" {
		t.Errorf("环境变量应覆盖文件, arch got %q", cfg.Arch)
	}
}

func TestLoadEnvOnly(t *testing.T) {
	withTempRoot(t)
	clearJVMEnv(t)
	setEnv(t, "JVM_MIRROR", "https://env-only.mirror/Adoptium")
	cfg := Load()
	if cfg.Mirror != "https://env-only.mirror/Adoptium" {
		t.Errorf("无文件时环境变量应生效, got %q", cfg.Mirror)
	}
}

func TestLoadEmptyEnvIgnored(t *testing.T) {
	root := withTempRoot(t)
	clearJVMEnv(t)
	content := `mirror = "https://file.mirror/Adoptium"
`
	os.WriteFile(filepath.Join(root, "config.toml"), []byte(content), 0o644)
	// 空环境变量不应覆盖文件值
	setEnv(t, "JVM_MIRROR", "   ")

	cfg := Load()
	if cfg.Mirror != "https://file.mirror/Adoptium" {
		t.Errorf("空环境变量不应覆盖文件, mirror got %q", cfg.Mirror)
	}
}
