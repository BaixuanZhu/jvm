package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
