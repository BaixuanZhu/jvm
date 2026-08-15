package config

import (
	"os"
	"path/filepath"
	"runtime"
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

// clearEnv 清掉 JVM_MIRROR / JVM_ARCH / JVM_AUTOSWITCH, 避免真实环境干扰测试。
func clearJVMEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"JVM_MIRROR", "JVM_ARCH", "JVM_AUTOSWITCH"} {
		if v, had := os.LookupEnv(key); had {
			os.Unsetenv(key)
			t.Cleanup(func() { os.Setenv(key, v) })
		}
	}
}

// wantDefaultArch 独立推导"默认 arch 应为何值", 与被测的 defaultArch 互验映射:
// 期望值按 runtime.GOARCH 在测试侧重新映射一遍, 而不是直接调 defaultArch()
// (否则测试变成自己比自己, 失去意义)。同时保证 ARM64 机器上跑测试不会误失败。
func wantDefaultArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x64"
}

func TestDefault(t *testing.T) {
	d := Default()
	if d.Mirror == "" {
		t.Error("默认 mirror 不应为空")
	}
	if d.Arch != wantDefaultArch() {
		t.Errorf("默认 arch 应为 %s, got %q", wantDefaultArch(), d.Arch)
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
	if cfg.Arch != wantDefaultArch() {
		t.Errorf("缺失文件时 arch 应为默认值 %s, got %q", wantDefaultArch(), cfg.Arch)
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
	if cfg.Arch != wantDefaultArch() {
		t.Errorf("arch 应回退默认值 %s, got %q", wantDefaultArch(), cfg.Arch)
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
	if cfg.Arch != wantDefaultArch() {
		t.Errorf("非法文件应回退默认值 %s, got %q", wantDefaultArch(), cfg.Arch)
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

// === autoswitch (.jvmrc 目录自动切换) ===

func TestLoadAutoSwitchDefault(t *testing.T) {
	withTempRoot(t)
	clearJVMEnv(t)
	if cfg := Load(); !cfg.AutoSwitch {
		t.Error("autoswitch 默认应为 true")
	}
}

func TestLoadAutoSwitchFileFalse(t *testing.T) {
	root := withTempRoot(t)
	clearJVMEnv(t)
	os.WriteFile(filepath.Join(root, "config.toml"), []byte("autoswitch = false\n"), 0o644)
	if cfg := Load(); cfg.AutoSwitch {
		t.Error("文件显式 false 应关闭自动切换")
	}
}

func TestLoadAutoSwitchFileTrue(t *testing.T) {
	root := withTempRoot(t)
	clearJVMEnv(t)
	// 显式 true 与未设置 (默认 true) 结果一致, 验证 *bool 两条路径
	os.WriteFile(filepath.Join(root, "config.toml"), []byte("autoswitch = true\n"), 0o644)
	if cfg := Load(); !cfg.AutoSwitch {
		t.Error("文件显式 true 应开启")
	}
}

func TestLoadAutoSwitchEnvOverridesFile(t *testing.T) {
	root := withTempRoot(t)
	os.WriteFile(filepath.Join(root, "config.toml"), []byte("autoswitch = false\n"), 0o644)
	setEnv(t, "JVM_AUTOSWITCH", "1")
	if cfg := Load(); !cfg.AutoSwitch {
		t.Error("环境变量 1 应覆盖文件的 false")
	}
	setEnv(t, "JVM_AUTOSWITCH", "0")
	if cfg := Load(); cfg.AutoSwitch {
		t.Error("环境变量 0 应关闭")
	}
}

func TestLoadAutoSwitchEnvInvalidIgnored(t *testing.T) {
	withTempRoot(t)
	clearJVMEnv(t)
	setEnv(t, "JVM_AUTOSWITCH", "flase") // 拼错 → 忽略, 保持默认
	if cfg := Load(); !cfg.AutoSwitch {
		t.Error("非法布尔值应被忽略并保持默认 true")
	}
}
