package updatecheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShouldCheck(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	interval := 24 * time.Hour

	tests := []struct {
		name string
		last time.Time // 零值表示从未检查
		want bool
	}{
		{"从未检查 (零值)", time.Time{}, true},
		{"刚好到间隔", now.Add(-24 * time.Hour), true},
		{"略超间隔", now.Add(-(24*time.Hour + time.Minute)), true},
		{"差一分钟", now.Add(-(24*time.Hour - time.Minute)), false},
		{"刚检查过", now.Add(-time.Hour), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCheck(tt.last, now, interval); got != tt.want {
				t.Errorf("shouldCheck() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripV(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"v0.6.1", "0.6.1"},
		{"V0.6.1", "0.6.1"},
		{"0.6.1", "0.6.1"}, // 无前缀
		{"v", ""},          // 仅前缀
		{"", ""},           // 空
	}
	for _, tt := range tests {
		if got := stripV(tt.in); got != tt.want {
			t.Errorf("stripV(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReadWriteLastCheck(t *testing.T) {
	// 把 timestampFile 重定向到临时目录, 测完还原 (避免污染真实 ~/.jvm)。
	dir := t.TempDir()
	orig := timestampFile
	timestampFile = filepath.Join(dir, "update-check.json")
	defer func() { timestampFile = orig }()

	want := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	writeLastCheck(want)

	got := readLastCheck()
	if !got.Equal(want) {
		t.Errorf("readLastCheck() = %v, want %v", got, want)
	}
}

func TestReadLastCheck_MissingFile(t *testing.T) {
	dir := t.TempDir()
	orig := timestampFile
	timestampFile = filepath.Join(dir, "does-not-exist.json")
	defer func() { timestampFile = orig }()

	if got := readLastCheck(); !got.IsZero() {
		t.Errorf("文件不存在应返回零值, 实际 %v", got)
	}
}

func TestReadLastCheck_BadJSON(t *testing.T) {
	dir := t.TempDir()
	orig := timestampFile
	timestampFile = filepath.Join(dir, "update-check.json")
	defer func() { timestampFile = orig }()

	if err := os.WriteFile(timestampFile, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readLastCheck(); !got.IsZero() {
		t.Errorf("非法 JSON 应返回零值, 实际 %v", got)
	}
}

func TestReadLastCheck_BadTimestamp(t *testing.T) {
	dir := t.TempDir()
	orig := timestampFile
	timestampFile = filepath.Join(dir, "update-check.json")
	defer func() { timestampFile = orig }()

	rec := checkRecord{Last: "not-a-date"}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(timestampFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readLastCheck(); !got.IsZero() {
		t.Errorf("非法时间戳应返回零值, 实际 %v", got)
	}
}

// redirectTimestamp 把 timestampFile 重定向到临时目录, 返回还原函数。
// 所有测 check() 的子测试都应调用, 避免污染真实 ~/.jvm。
func redirectTimestamp(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	orig := timestampFile
	timestampFile = filepath.Join(dir, "update-check.json")
	return func() { timestampFile = orig }
}

func TestCheck(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	t.Run("有新版本则提示", func(t *testing.T) {
		defer redirectTimestamp(t)()
		var printed string
		fakeFetch := func(string) (string, error) { return "v9.9.9", nil }
		capturePrint := func(format string, args ...any) (int, error) {
			printed = fmt.Sprintf(format, args...)
			return len(printed), nil
		}
		check("any/repo", "0.1.0", 0, now, fakeFetch, capturePrint)
		if !strings.Contains(printed, "v9.9.9") {
			t.Errorf("应提示新版本 v9.9.9, got %q", printed)
		}
		if !strings.Contains(printed, "0.1.0") {
			t.Errorf("应含当前版本 0.1.0, got %q", printed)
		}
	})

	t.Run("已是最新则不提示", func(t *testing.T) {
		defer redirectTimestamp(t)()
		var printed string
		fakeFetch := func(string) (string, error) { return "v0.1.0", nil }
		capturePrint := func(format string, args ...any) (int, error) {
			printed = "should not print"
			return 0, nil
		}
		check("any/repo", "0.1.0", 0, now, fakeFetch, capturePrint)
		if printed != "" {
			t.Error("当前已是最新, 不应打印提示")
		}
	})

	t.Run("网络失败则跳过", func(t *testing.T) {
		defer redirectTimestamp(t)()
		var printed string
		fakeFetch := func(string) (string, error) { return "", fmt.Errorf("network error") }
		capturePrint := func(format string, args ...any) (int, error) {
			printed = "should not print"
			return 0, nil
		}
		check("any/repo", "0.1.0", 0, now, fakeFetch, capturePrint)
		if printed != "" {
			t.Error("网络失败不应打印提示")
		}
	})

	t.Run("节流命中则不打 API", func(t *testing.T) {
		defer redirectTimestamp(t)()
		// 预写时间戳: 刚检查过 (1 小时前), 节流间隔 24h → 应跳过。
		writeLastCheck(now.Add(-1 * time.Hour))
		fetchCalled := false
		fakeFetch := func(string) (string, error) {
			fetchCalled = true
			return "v9.9.9", nil
		}
		check("any/repo", "0.1.0", 24*time.Hour, now, fakeFetch, func(string, ...any) (int, error) { return 0, nil })
		if fetchCalled {
			t.Error("节流期内不应调用 fetch")
		}
	})

	t.Run("检查后写入时间戳", func(t *testing.T) {
		defer redirectTimestamp(t)()
		fakeFetch := func(string) (string, error) { return "v9.9.9", nil }
		check("any/repo", "0.1.0", 0, now, fakeFetch, func(string, ...any) (int, error) { return 0, nil })
		got := readLastCheck()
		if !got.Equal(now) {
			t.Errorf("检查后时间戳应为 %v, got %v", now, got)
		}
	})
}
