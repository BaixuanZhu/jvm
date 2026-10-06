// uninstall_test.go 覆盖 uninstall 的参数解析纯函数 (删除编排路径写
// junction/注册表, 按项目惯例不设单测, 由集成脚本覆盖)。
package cmd

import (
	"strings"
	"testing"
)

func TestParseUninstallArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantArg    string
		wantAll    bool
		wantYes    bool
		wantErrSub string // 非空表示期望报错且文案包含该子串
	}{
		{"单个版本", []string{"21"}, "21", false, false, ""},
		{"完整版本", []string{"temurin@21.0.5+11"}, "temurin@21.0.5+11", false, false, ""},
		{"跳过确认", []string{"21", "-y"}, "21", false, true, ""},
		{"长 flag 跳过确认", []string{"--yes", "21"}, "21", false, true, ""},
		{"组卸载", []string{"temurin@21", "--all"}, "temurin@21", true, false, ""},
		{"组卸载短 flag", []string{"-a", "21", "-y"}, "21", true, true, ""},
		{"flag 在前", []string{"-y", "-a", "corretto@17"}, "corretto@17", true, true, ""},

		{"无参数", []string{}, "", false, false, "用法"},
		{"未知选项", []string{"21", "-x"}, "", false, false, "未识别的选项"},
		{"多余位置参数", []string{"21", "17"}, "", false, false, "未识别的参数"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arg, all, yes, err := parseUninstallArgs(tt.args)
			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("parseUninstallArgs(%v) 期望报错, got arg=%q all=%v yes=%v",
						tt.args, arg, all, yes)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Errorf("错误文案应含 %q, got %q", tt.wantErrSub, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUninstallArgs(%v) 意外报错: %v", tt.args, err)
			}
			if arg != tt.wantArg || all != tt.wantAll || yes != tt.wantYes {
				t.Errorf("parseUninstallArgs(%v) = (%q, %v, %v), want (%q, %v, %v)",
					tt.args, arg, all, yes, tt.wantArg, tt.wantAll, tt.wantYes)
			}
		})
	}
}
