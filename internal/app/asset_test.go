package app

import "testing"

// TestParseVersionSpec 验证 "[distro@]version" 解析。
// 纯函数, 表驱动。
func TestParseVersionSpec(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    VersionSpec
		wantErr bool
	}{
		{"纯大版本号默认 temurin", "21", VersionSpec{Distro: "temurin", Version: "21"}, false},
		{"完整版本号默认 temurin", "21.0.12+8", VersionSpec{Distro: "temurin", Version: "21.0.12+8"}, false},
		{"完整 release name 默认 temurin", "jdk-21.0.12+8", VersionSpec{Distro: "temurin", Version: "jdk-21.0.12+8"}, false},
		{"corretto@21", "corretto@21", VersionSpec{Distro: "corretto", Version: "21"}, false},
		{"microsoft@21.0.12+8", "microsoft@21.0.12+8", VersionSpec{Distro: "microsoft", Version: "21.0.12+8"}, false},
		{"带空格 trim", "  corretto@21  ", VersionSpec{Distro: "corretto", Version: "21"}, false},
		{"distro 带空格 trim", "  corretto @21", VersionSpec{Distro: "corretto", Version: "21"}, false},
		{"version 带空格 trim", "corretto@  21  ", VersionSpec{Distro: "corretto", Version: "21"}, false},
		{"空串报错", "", VersionSpec{}, true},
		{"纯空格报错", "   ", VersionSpec{}, true},
		{"@ 前空报错", "@21", VersionSpec{}, true},
		{"@ 后空报错", "corretto@", VersionSpec{}, true},
		{"@ 前后空报错", " @ ", VersionSpec{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersionSpec(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseVersionSpec(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("ParseVersionSpec(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// TestDefaultDistro 确保默认发行版标识与 provider.Default 约定一致。
// 此处只校验非空且为 temurin (provider 包另有测试校验常量同步)。
func TestDefaultDistro(t *testing.T) {
	if DefaultDistro != "temurin" {
		t.Errorf("DefaultDistro = %q, want %q", DefaultDistro, "temurin")
	}
}
