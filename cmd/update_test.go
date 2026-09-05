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
		wantAll   bool
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
		{name: "--all", args: []string{"--all"}, wantAll: true},
		{name: "-a 短形态", args: []string{"-a"}, wantAll: true},
		{name: "--all -y", args: []string{"--all", "-y"}, wantAll: true, wantYes: true},
		{name: "-y --all 顺序无关", args: []string{"-y", "--all"}, wantAll: true, wantYes: true},
		{name: "--all 与版本参数互斥报错", args: []string{"--all", "21"}, wantError: true},
		{name: "版本在前 --all 在后同样互斥", args: []string{"21", "--all"}, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arg, all, yes, err := parseUpdateArgs(tc.args)
			if tc.wantError {
				if err == nil {
					t.Fatalf("期望报错, 实际 arg=%q all=%v yes=%v", arg, all, yes)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外报错: %v", err)
			}
			if arg != tc.wantArg || all != tc.wantAll || yes != tc.wantYes {
				t.Errorf("arg=%q all=%v yes=%v, 想 arg=%q all=%v yes=%v", arg, all, yes, tc.wantArg, tc.wantAll, tc.wantYes)
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

func TestBuildUpdatePlan(t *testing.T) {
	// 组内最新 21.0.5+11, 远端 21.0.8+7: 落后, 需升级
	group := updateGroup{dirs: []string{"temurin-21.0.5+11", "21.0.2+13"}, latestVer: "21.0.5+11"}

	t.Run("落后: 计划安装新版并删旧目录", func(t *testing.T) {
		p := buildUpdatePlan(group, "temurin", 21, "21.0.8+7", "")
		if p.status != planUpgrade {
			t.Fatalf("status = %v, 想 planUpgrade", p.status)
		}
		if p.newDir != "temurin-21.0.8+7" {
			t.Errorf("newDir = %q, 想 temurin-21.0.8+7", p.newDir)
		}
		if !reflect.DeepEqual(p.toDelete, group.dirs) {
			t.Errorf("toDelete = %v, 想 %v", p.toDelete, group.dirs)
		}
		if p.latestInstalled {
			t.Error("latestInstalled 应为 false")
		}
	})

	t.Run("current 指向组内旧目录: needSwitch", func(t *testing.T) {
		p := buildUpdatePlan(group, "temurin", 21, "21.0.8+7", "21.0.2+13")
		if !p.needSwitch {
			t.Error("current 在待删目录里, needSwitch 应为 true")
		}
		// current 指向组外目录: 不切换
		p2 := buildUpdatePlan(group, "temurin", 21, "21.0.8+7", "corretto-21.0.12.8.1")
		if p2.needSwitch {
			t.Error("current 在组外, needSwitch 应为 false")
		}
	})

	t.Run("本地比远端新: 不降级", func(t *testing.T) {
		p := buildUpdatePlan(group, "temurin", 21, "21.0.4+3", "")
		if p.status != planLocalNewer {
			t.Errorf("status = %v, 想 planLocalNewer", p.status)
		}
	})

	t.Run("组级已最新且无旧目录: 纯幂等", func(t *testing.T) {
		g := updateGroup{dirs: []string{"temurin-21.0.8+7"}, latestVer: "21.0.8+7"}
		p := buildUpdatePlan(g, "temurin", 21, "21.0.8+7", "temurin-21.0.8+7")
		if p.status != planUpToDate {
			t.Errorf("status = %v, 想 planUpToDate", p.status)
		}
	})

	t.Run("最新版已装但组内仍有旧目录: 只清理", func(t *testing.T) {
		g := updateGroup{dirs: []string{"temurin-21.0.8+7", "temurin-21.0.5+11"}, latestVer: "21.0.8+7"}
		p := buildUpdatePlan(g, "temurin", 21, "21.0.8+7", "")
		if p.status != planUpgrade {
			t.Fatalf("status = %v, 想 planUpgrade (清理路径)", p.status)
		}
		if !p.latestInstalled {
			t.Error("latestInstalled 应为 true")
		}
		if !reflect.DeepEqual(p.toDelete, []string{"temurin-21.0.5+11"}) {
			t.Errorf("toDelete = %v, 想 [temurin-21.0.5+11]", p.toDelete)
		}
	})

	t.Run("同版本旧无前缀目录: 走安装路径换规范名", func(t *testing.T) {
		// 旧无前缀目录 21.0.8+7 与规范名目录同版本时名字不同, 不算 latestInstalled
		g := updateGroup{dirs: []string{"21.0.8+7"}, latestVer: "21.0.8+7"}
		p := buildUpdatePlan(g, "temurin", 21, "21.0.8+7", "")
		if p.status != planUpgrade || p.latestInstalled {
			t.Errorf("status=%v latestInstalled=%v, 想 planUpgrade + false (换规范名目录)", p.status, p.latestInstalled)
		}
	})
}
