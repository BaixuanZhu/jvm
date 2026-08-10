package provider

import (
	"strings"
	"testing"

	"jvm/internal/app"
)

// fakeProvider 是测试用的 provider 实现, 嵌入 Base 拿默认。
type fakeProvider struct {
	Base
	name string
}

func (f fakeProvider) Name() string                              { return f.name }
func (fakeProvider) DisplayName() string                         { return "Fake" }
func (fakeProvider) Available() ([]app.Release, error)           { return nil, nil }
func (fakeProvider) LatestPatch(int) (*app.Asset, error)         { return nil, nil }
func (fakeProvider) Resolve(app.VersionSpec) (*app.Asset, error) { return nil, nil }
func (fakeProvider) ListVersions(int) ([]*app.Asset, error)      { return nil, nil }

// TestRegisterGet 验证注册表的注册、查询、错误消息。
// 注意: 注册表是全局状态, 用一次性 name 避免与其他测试/temurin init 冲突。
func TestRegisterGet(t *testing.T) {
	name := "fake-provider-test-1"
	Register(fakeProvider{name: name})

	got, err := Get(name)
	if err != nil {
		t.Fatalf("Get(%q) 返回错误: %v", name, err)
	}
	if got.Name() != name {
		t.Errorf("Get(%q).Name() = %q, want %q", name, got.Name(), name)
	}
	if got.DisplayName() != "Fake" {
		t.Errorf("Get(%q).DisplayName() = %q, want %q", name, got.DisplayName(), "Fake")
	}
}

// TestGetUnknown 验证未知发行版的错误消息列出可用项。
func TestGetUnknown(t *testing.T) {
	_, err := Get("definitely-not-registered-xyz")
	if err == nil {
		t.Fatal("Get(未注册名) 应返回错误")
	}
	if !strings.Contains(err.Error(), "definitely-not-registered-xyz") {
		t.Errorf("错误消息应包含查询名, got: %v", err)
	}
	if !strings.Contains(err.Error(), "可用:") {
		t.Errorf("错误消息应列出可用项, got: %v", err)
	}
}

// TestAll 验证 All 返回已注册 provider (至少含测试注册的 + temurin init 注册的)。
func TestAll(t *testing.T) {
	name := "fake-provider-test-2"
	Register(fakeProvider{name: name})

	all := All()
	found := false
	for _, p := range all {
		if p.Name() == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("All() 未包含已注册的 %q", name)
	}
}

// TestRegisterDuplicate 验证同名重复注册 panic。
func TestRegisterDuplicate(t *testing.T) {
	name := "fake-provider-dup"
	Register(fakeProvider{name: name})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("重复注册应 panic")
		}
	}()
	Register(fakeProvider{name: name})
}

// TestBaseDefaults 验证 Base 基类的默认实现 (ShortSemver/ResolveReleaseName 透传)。
func TestBaseDefaults(t *testing.T) {
	b := Base{}
	if got := b.ShortSemver("21.0.5+11"); got != "21.0.5+11" {
		t.Errorf("Base.ShortSemver 应透传, got %q", got)
	}
	// ResolveReleaseName 默认透传
	got, err := b.ResolveReleaseName("21.0.12")
	if err != nil || got != "21.0.12" {
		t.Errorf("Base.ResolveReleaseName 应透传, got %q err %v", got, err)
	}
}

// configurableFake 是实现 Configurable 的测试 provider, 记录收到的配置。
type configurableFake struct {
	fakeProvider
	gotArch, gotMirror string
	called             bool
}

func (c *configurableFake) Configure(arch, mirror string) {
	c.gotArch, c.gotMirror = arch, mirror
	c.called = true
}

// TestConfigureAll 验证 ConfigureAll 只向实现了 Configurable 的 provider 分发配置,
// 未实现的 (fakeProvider) 不报错、不受影响。
func TestConfigureAll(t *testing.T) {
	fake := &configurableFake{fakeProvider: fakeProvider{name: "fake-provider-cfg"}}
	Register(fake)

	ConfigureAll("aarch64", "https://mirror.example/Adoptium")

	if !fake.called {
		t.Fatal("实现 Configurable 的 provider 应收到 Configure 调用")
	}
	if fake.gotArch != "aarch64" || fake.gotMirror != "https://mirror.example/Adoptium" {
		t.Errorf("收到的配置 = (%q, %q), 期望 (aarch64, https://mirror.example/Adoptium)",
			fake.gotArch, fake.gotMirror)
	}
}
