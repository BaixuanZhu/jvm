package cmd

import (
	"reflect"
	"testing"
)

func TestParseUpdateArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantArg   string
		wantYes   bool
		wantError bool
	}{
		{name: "无参数报错", args: nil, wantError: true},
		{name: "仅 -y 无版本报错", args: []string{"-y"}, wantError: true},
		{name: "只有版本", args: []string{"21"}, wantArg: "21"},
		{name: "带 distro 前缀", args: []string{"corretto@21"}, wantArg: "corretto@21"},
		{name: "版本 + -y", args: []string{"21", "-y"}, wantArg: "21", wantYes: true},
		{name: "--yes 在前", args: []string{"--yes", "corretto@21"}, wantArg: "corretto@21", wantYes: true},
		{name: "两个版本参数报错", args: []string{"21", "17"}, wantError: true},
		{name: "未知 flag 报错", args: []string{"21", "-f"}, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arg, yes, err := parseUpdateArgs(tc.args)
			if tc.wantError {
				if err == nil {
					t.Fatalf("期望报错, 实际 arg=%q yes=%v", arg, yes)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外报错: %v", err)
			}
			if arg != tc.wantArg || yes != tc.wantYes {
				t.Errorf("arg=%q yes=%v, 想 arg=%q yes=%v", arg, yes, tc.wantArg, tc.wantYes)
			}
		})
	}
}

func TestPlanUpdate(t *testing.T) {
	// 模拟 ListLocal 的降序输出: 多 distro 多 major 混合 + 旧无前缀目录
	names := []string{
		"temurin-21.0.8+7",
		"temurin-21.0.5+11", // 同组旧 patch
		"temurin-17.0.12+8", // 同 distro 不同 major, 应排除
		"corretto-21.0.12.8.1",
		"21.0.2+13", // 旧无前缀目录, 归为 temurin 21 组
	}

	g, err := planUpdate(names, "temurin", 21)
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	wantDirs := []string{"temurin-21.0.8+7", "temurin-21.0.5+11", "21.0.2+13"}
	if !reflect.DeepEqual(g.dirs, wantDirs) {
		t.Errorf("dirs = %v, 想 %v", g.dirs, wantDirs)
	}
	if g.latestVer != "21.0.8+7" {
		t.Errorf("latestVer = %q, 想 \"21.0.8+7\" (降序输入组内首个即最新)", g.latestVer)
	}

	// 组不存在: 报错并引导
	if _, err := planUpdate(names, "zulu", 21); err == nil {
		t.Error("zulu 21 未安装, 应报错")
	}
	if _, err := planUpdate(names, "temurin", 25); err == nil {
		t.Error("temurin 25 未安装, 应报错")
	}

	// corretto 的四段式版本号组
	gc, err := planUpdate(names, "corretto", 21)
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if gc.latestVer != "21.0.12.8.1" {
		t.Errorf("latestVer = %q, 想 \"21.0.12.8.1\"", gc.latestVer)
	}
}
