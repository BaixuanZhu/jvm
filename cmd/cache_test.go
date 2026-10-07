package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jvm/internal/paths"
)

// withTempCache 把 paths.CacheDir 临时指向临时目录并恢复。
func withTempCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := paths.CacheDir
	paths.CacheDir = dir
	t.Cleanup(func() { paths.CacheDir = orig })
	return dir
}

func TestCacheList(t *testing.T) {
	dir := withTempCache(t)
	os.WriteFile(filepath.Join(dir, "zulu-17.0.12+7.zip"), bytes.Repeat([]byte("x"), 2048), 0o644)
	os.WriteFile(filepath.Join(dir, "temurin-21.0.8+15.zip"), bytes.Repeat([]byte("y"), 4096), 0o644)
	// 非 zip 条目不参与列表
	os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o644)

	out := captureStdout(t, func() { Cache(nil) })
	for _, want := range []string{"temurin-21.0.8+15.zip", "zulu-17.0.12+7.zip", "2 个文件"} {
		if !strings.Contains(out, want) {
			t.Errorf("jvm cache 输出应含 %q:\n%s", want, out)
		}
	}
}

func TestCacheListEmpty(t *testing.T) {
	withTempCache(t)
	out := captureStdout(t, func() { Cache([]string{}) })
	if !strings.Contains(out, "空") {
		t.Errorf("空缓存应提示为空, got:\n%s", out)
	}
}

func TestCacheClean(t *testing.T) {
	dir := withTempCache(t)
	zipFile := filepath.Join(dir, "temurin-21.0.8+15.zip")
	partFile := filepath.Join(dir, "temurin-22.zip.part")
	keepFile := filepath.Join(dir, "stray.txt")
	os.WriteFile(zipFile, bytes.Repeat([]byte("x"), 1024), 0o644)
	os.WriteFile(partFile, []byte("x"), 0o644)
	os.WriteFile(keepFile, []byte("keep"), 0o644)

	out := captureStdout(t, func() { Cache([]string{"clean"}) })
	if !strings.Contains(out, "2 个缓存文件") {
		t.Errorf("clean 应清掉 zip 与 .part 两个文件:\n%s", out)
	}
	for gone, name := range map[string]string{
		zipFile:  "zip",
		partFile: "part",
	} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s 应被删除", name)
		}
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Errorf("非 zip 文件不应被清理: %v", err)
	}
	// 目录本身保留
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("缓存目录应保留")
	}
}

// setAge 把文件的修改时间拨到 age 前 (mtime 是 --older-than 的判定依据)。
func setAge(t *testing.T, path string, age time.Duration) {
	t.Helper()
	past := time.Now().Add(-age)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("拨 %s 的 mtime 失败: %v", path, err)
	}
}

func TestParseAge(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"1d", 24 * time.Hour, false},
		{"30d", 720 * time.Hour, false},
		{"365d", 365 * 24 * time.Hour, false},
		{" 7d ", 168 * time.Hour, false}, // 首尾空白容忍
		{"", 0, true},                    // 空
		{"30", 0, true},                  // 缺单位
		{"d", 0, true},                   // 缺天数
		{"0d", 0, true},                  // 非正整数
		{"-5d", 0, true},
		{"7w", 0, true},  // 只支持天
		{"7h", 0, true},  // 只支持天
		{"30D", 0, true}, // 大写不支持
		{"abc", 0, true},
		{"36600d", 0, true}, // 超上限 (36500)
		{"36500d", 36500 * 24 * time.Hour, false},
	}
	for _, tt := range tests {
		got, err := parseAge(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseAge(%q) 期望报错, 实际 %v", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAge(%q) 未预期错误: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseAge(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseCacheCleanArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAge    time.Duration
		wantFilter bool
		wantErr    bool
		errSub     string // 期望错误文案含的子串 (wantErr 时校验)
	}{
		{"无参 = 全清", nil, 0, false, false, ""},
		{"空切片", []string{}, 0, false, false, ""},
		{"--older-than 7d", []string{"--older-than", "7d"}, 168 * time.Hour, true, false, ""},
		{"--older-than=30d", []string{"--older-than=30d"}, 720 * time.Hour, true, false, ""},
		{"缺值", []string{"--older-than"}, 0, false, true, "需要一个时限参数"},
		{"非法值", []string{"--older-than", "7w"}, 0, false, true, "<N>d"},
		{"重复指定", []string{"--older-than", "7d", "--older-than", "8d"}, 0, false, true, "只能指定一次"},
		{"未知 flag", []string{"--foo"}, 0, false, true, "未识别的参数"},
		{"位置参数", []string{"7d"}, 0, false, true, "未识别的参数"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age, filtered, err := parseCacheCleanArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseCacheCleanArgs(%v) 期望报错", tt.args)
				}
				if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("错误 %q 应含 %q", err.Error(), tt.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCacheCleanArgs(%v) 未预期错误: %v", tt.args, err)
			}
			if age != tt.wantAge || filtered != tt.wantFilter {
				t.Errorf("parseCacheCleanArgs(%v) = (%v, %v), want (%v, %v)",
					tt.args, age, filtered, tt.wantAge, tt.wantFilter)
			}
		})
	}
}

// TestCacheCleanOlderThan 验证按龄清理: 只删过期文件 (zip 与 .part 同样适用),
// 未到期与非 zip 文件保留。
func TestCacheCleanOlderThan(t *testing.T) {
	dir := withTempCache(t)
	oldZip := filepath.Join(dir, "temurin-20.0.1+1.zip")
	oldPart := filepath.Join(dir, "temurin-21.zip.part")
	newZip := filepath.Join(dir, "temurin-21.0.8+15.zip")
	for _, f := range []string{oldZip, oldPart, newZip} {
		if err := os.WriteFile(f, bytes.Repeat([]byte("x"), 1024), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setAge(t, oldZip, 40*24*time.Hour)
	setAge(t, oldPart, 40*24*time.Hour)
	// newZip 保持当前 mtime

	out := captureStdout(t, func() { Cache([]string{"clean", "--older-than", "30d"}) })
	for _, want := range []string{"2 个缓存文件", "早于 30 天前", "保留 1 个"} {
		if !strings.Contains(out, want) {
			t.Errorf("按龄清理输出应含 %q:\n%s", want, out)
		}
	}
	for gone, name := range map[string]string{
		oldZip:  "过期的 zip",
		oldPart: "过期的 part",
	} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s 应被删除", name)
		}
	}
	if _, err := os.Stat(newZip); err != nil {
		t.Errorf("未到期的 zip 不应被清理: %v", err)
	}
}

// TestCacheListShowsDate 验证列表带最后修改日期列 (供决定 --older-than 阈值参考)。
func TestCacheListShowsDate(t *testing.T) {
	dir := withTempCache(t)
	if err := os.WriteFile(filepath.Join(dir, "temurin-21.0.8+15.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { Cache(nil) })
	want := time.Now().Format("2006-01-02")
	if !strings.Contains(out, want) {
		t.Errorf("列表应含今天日期 %q:\n%s", want, out)
	}
}
