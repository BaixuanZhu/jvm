// rc_test.go 覆盖 rc.go 的宽松版本匹配 (外来 rc 格式的半截版本号降级)。
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jvm/internal/app"
)

// TestResolveVersionLoose 用临时版本目录验证宽松匹配的三个分支:
// 原样命中 / 降级到大版本组内最新 / 彻底失败返回原样错误。
func TestResolveVersionLoose(t *testing.T) {
	root := withTempVersions(t)
	// 已装: temurin 21 组两个 patch + corretto 17 组一个
	for _, d := range []string{"temurin-21.0.5+11", "temurin-21.0.2+8", "corretto-17.0.12.8.1"} {
		if err := os.MkdirAll(filepath.Join(root, "versions", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name    string
		spec    string
		want    string // 期望目录名; "" 表示期望报错
		wantErr string // 期望错误文案包含的关键字 (want 非空时忽略)
	}{
		// 原样命中: 完整版本号 (含 build 号) 不需要降级
		{"完整版本号原样命中", "temurin@21.0.5+11", "temurin-21.0.5+11", ""},
		// 纯大版本号: ResolveVersion 的 major 分支直接给组内最新
		{"纯大版本号取组内最新", "temurin@21", "temurin-21.0.5+11", ""},
		// 半截版本号 (sdkman/asdf 生态常态): 降级为大版本组内最新
		{"半截版本降级到组内最新", "temurin@21.0.2", "temurin-21.0.5+11", ""},
		{"sdkman 四段式降级", "corretto@17.0.12", "corretto-17.0.12.8.1", ""},
		// 彻底失败: 组/发行版不存在, 降级也无济于事, 返回原样错误
		{"组不存在", "temurin@25.0.1", "", "没有找到"},
		{"发行版不存在", "zulu@21.0.2", "", "没有安装"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := app.ParseVersionSpec(tt.spec)
			if err != nil {
				t.Fatalf("ParseVersionSpec(%q): %v", tt.spec, err)
			}
			got, err := resolveVersionLoose(spec)
			if tt.want != "" {
				if err != nil {
					t.Fatalf("resolveVersionLoose(%q) 意外报错: %v", tt.spec, err)
				}
				if got != tt.want {
					t.Errorf("resolveVersionLoose(%q) = %q, want %q", tt.spec, got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("resolveVersionLoose(%q) 期望报错, got %q", tt.spec, got)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("错误文案应含 %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
