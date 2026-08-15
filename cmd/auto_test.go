package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// === decideAuto (纯决策) ===

func TestDecideAuto(t *testing.T) {
	tests := []struct {
		name       string
		rcFound    bool
		rcDir      string
		current    string
		state      string
		wantAction autoAction
		wantTarget string
	}{
		{"无 rc 无状态", false, "", "", "", autoNoop, ""},
		{"无 rc 有状态 → 恢复", false, "", "", "temurin-21.0.5+11", autoRevert, "temurin-21.0.5+11"},
		{"rc 解析失败 → 警告", true, "", "temurin-21", "", autoWarn, ""},
		{"rc 与当前相同 → no-op", true, "temurin-17.0.1+9", "temurin-17.0.1+9", "", autoNoop, ""},
		{"rc 与当前相同 (大小写) → no-op", true, "temurin-17.0.1+9", "TEMURIN-17.0.1+9", "", autoNoop, ""},
		{"rc 不同 → 切换", true, "corretto-17.0.12.8.1", "temurin-21.0.5+11", "", autoSwitch, "corretto-17.0.12.8.1"},
		{"无 current → 切换", true, "temurin-17.0.1+9", "", "", autoSwitch, "temurin-17.0.1+9"},
		{"rc 不同且已有状态 → 仍切换 (状态保留)", true, "temurin-19", "temurin-17", "temurin-21", autoSwitch, "temurin-19"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, target := decideAuto(tt.rcFound, tt.rcDir, tt.current, tt.state)
			if action != tt.wantAction {
				t.Errorf("action = %v, want %v", action, tt.wantAction)
			}
			if target != tt.wantTarget {
				t.Errorf("target = %q, want %q", target, tt.wantTarget)
			}
		})
	}
}

// === state 文件 IO ===

func TestAutoStateFileIO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-state")

	if got := readAutoStateFile(path); got != "" {
		t.Errorf("文件不存在应返回空串, got %q", got)
	}
	if err := writeAutoStateFile(path, "temurin-21.0.5+11"); err != nil {
		t.Fatalf("写入: %v", err)
	}
	if got := readAutoStateFile(path); got != "temurin-21.0.5+11" {
		t.Errorf("读回 = %q", got)
	}
	// 带首尾空白容错 (手编/换行)
	os.WriteFile(path, []byte("  temurin-21.0.5+11\r\n"), 0o644)
	if got := readAutoStateFile(path); got != "temurin-21.0.5+11" {
		t.Errorf("TrimSpace 后读回 = %q", got)
	}
	// 清除后再读为空
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := readAutoStateFile(path); got != "" {
		t.Errorf("清除后应返回空串, got %q", got)
	}
}

// === UseAuto 关闭时静默 ===

func TestUseAutoDisabled(t *testing.T) {
	out := captureStdout(t, func() { UseAuto(false) })
	if out != "" {
		t.Errorf("autoswitch 关闭时应零输出, got %q", out)
	}
}

// === resolveRcVersion 解析失败路径 (不碰本地版本目录) ===

func TestResolveRcVersionParseError(t *testing.T) {
	t.Run("内容为空", func(t *testing.T) {
		dir, warn := resolveRcVersion("", `D:\proj\.jvmrc`)
		if dir != "" {
			t.Errorf("解析失败 dir 应为空, got %q", dir)
		}
		if !strings.Contains(warn, "内容为空") {
			t.Errorf("warn 应说明内容为空, got %q", warn)
		}
	})
	t.Run("非法 spec", func(t *testing.T) {
		dir, warn := resolveRcVersion("corretto@\n", `D:\proj\.jvmrc`)
		if dir != "" {
			t.Errorf("解析失败 dir 应为空, got %q", dir)
		}
		if warn == "" {
			t.Error("非法 spec 应有警告")
		}
	})
}
