package cmd

import (
	"strings"
	"testing"
)

func TestGroupInstalled(t *testing.T) {
	// 模拟 ListLocal 的降序输出: 多 distro 多 major 混合 + 旧无前缀目录
	names := []string{
		"temurin-21.0.8+7",
		"temurin-21.0.5+11", // 同组旧 patch, 应被跳过
		"temurin-17.0.12+8", // temurin 17 组
		"corretto-21.0.12.8.1",
		"corretto-21.0.9.8.1", // corretto 21 组旧 patch
		"25.0.1+12",           // 旧无前缀目录, 归为 temurin
	}
	got := groupInstalled(names)
	want := []installedGroup{
		{distro: "temurin", major: 21, localDir: "temurin-21.0.8+7", localVer: "21.0.8+7"},
		{distro: "temurin", major: 17, localDir: "temurin-17.0.12+8", localVer: "17.0.12+8"},
		{distro: "corretto", major: 21, localDir: "corretto-21.0.12.8.1", localVer: "21.0.12.8.1"},
		{distro: "temurin", major: 25, localDir: "25.0.1+12", localVer: "25.0.1+12"},
	}
	if len(got) != len(want) {
		t.Fatalf("组数 = %d, 想 %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("组 %d = %+v, 想 %+v", i, got[i], want[i])
		}
	}
}

func TestGroupInstalledSkipsNonVersions(t *testing.T) {
	got := groupInstalled([]string{"21.0.5+11", ".tmp-extract-xxx", "not-a-version"})
	if len(got) != 1 {
		t.Fatalf("组数 = %d, 想 1 (非版本目录应跳过): %+v", len(got), got)
	}
	if got[0].major != 21 || got[0].distro != "temurin" {
		t.Errorf("组 = %+v", got[0])
	}
}

func TestPrintOutdatedAllCurrent(t *testing.T) {
	rows := []outdatedRow{
		{group: installedGroup{distro: "temurin", major: 21, localVer: "21.0.8+7"}, latest: "21.0.8+7"},
		{group: installedGroup{distro: "corretto", major: 17, localVer: "17.0.12.8.1"}, latest: "17.0.12.8.1"},
	}
	out := captureStdout(t, func() { printOutdated(rows) })
	if !strings.Contains(out, "均为最新") {
		t.Errorf("输出 = %q, 想包含\"均为最新\"", out)
	}
	if strings.Contains(out, "可升级") {
		t.Errorf("不应出现可升级: %q", out)
	}
}

func TestPrintOutdatedLocalNewerThanRemote(t *testing.T) {
	// 本地比远端新 (镜像延迟等), 视为最新而不是"负升级"
	rows := []outdatedRow{
		{group: installedGroup{distro: "temurin", major: 21, localVer: "21.0.9+1"}, latest: "21.0.8+7"},
	}
	out := captureStdout(t, func() { printOutdated(rows) })
	if !strings.Contains(out, "均为最新") {
		t.Errorf("输出 = %q, 本地更新应视为最新", out)
	}
}

func TestPrintOutdatedMixed(t *testing.T) {
	rows := []outdatedRow{
		{group: installedGroup{distro: "temurin", major: 21, localVer: "21.0.5+11"}, latest: "21.0.8+7"},
		{group: installedGroup{distro: "corretto", major: 21, localVer: "21.0.12.8.1"}, latest: "21.0.12.8.1"},
		{group: installedGroup{distro: "zulu", major: 17, localVer: "17.0.10+7"}, failed: true},
	}
	out := captureStdout(t, func() { printOutdated(rows) })
	for _, want := range []string{
		"可升级的版本",
		"temurin@21    21.0.5+11 → 21.0.8+7",
		"jvm install temurin@21",
		"查询失败",
		"zulu@17",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
	// 已最新的 corretto 不应出现"可升级"行
	if strings.Contains(out, "corretto@21") {
		t.Errorf("已最新的组不应列出: %q", out)
	}
}
