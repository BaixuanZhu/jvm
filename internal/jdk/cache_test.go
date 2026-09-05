package jdk

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"jvm/internal/app"
	"jvm/internal/paths"
)

// withTempJdkDirs 把 jdk 数据面路径 (Root/VersionsDir/CacheDir) 临时指向
// 临时目录并恢复, 隔离 Install 流程测试对真实 ~/.jvm 的写入。
func withTempJdkDirs(t *testing.T) {
	t.Helper()
	origRoot, origVer, origCache := paths.Root, paths.VersionsDir, paths.CacheDir
	base := t.TempDir()
	paths.Root = base
	paths.VersionsDir = filepath.Join(base, "versions")
	paths.CacheDir = filepath.Join(base, "cache")
	t.Cleanup(func() {
		paths.Root, paths.VersionsDir, paths.CacheDir = origRoot, origVer, origCache
	})
}

// writeTestZip 构造一个 zip: 内含 topFolder 顶层目录 (一个文件), 落在 path。
func writeTestZip(t *testing.T, path, topFolder string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(topFolder + "/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello jvm")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCacheHit 验证缓存命中判定: 存在且非空 + 校验和匹配 (无校验和时只看存在性)。
func TestCacheHit(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeTestZip(t, zipPath, "jdk-21")
	sum, err := fileHash(zipPath, "sha256")
	if err != nil {
		t.Fatal(err)
	}
	emptyPath := filepath.Join(dir, "empty.zip")
	os.WriteFile(emptyPath, nil, 0o644)

	tests := []struct {
		name  string
		file  string
		asset *app.Asset
		want  bool
	}{
		{"文件不存在", filepath.Join(dir, "missing.zip"), &app.Asset{}, false},
		{"空文件不算命中", emptyPath, &app.Asset{}, false},
		{"校验和匹配", zipPath, &app.Asset{Checksum: sum}, true},
		{"校验和不匹配", zipPath, &app.Asset{Checksum: "deadbeef"}, false},
		{"无校验和时存在即命中", zipPath, &app.Asset{}, true},
	}
	for _, tt := range tests {
		if got := cacheHit(tt.file, tt.asset); got != tt.want {
			t.Errorf("%s: cacheHit = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestInstallCacheReuse 端到端验证缓存复用: 第一次 Install 下载并留存 zip,
// 移除版本目录 (模拟卸载) 后重装, 不应再发任何 GET 请求。
func TestInstallCacheReuse(t *testing.T) {
	withTempJdkDirs(t)

	// 内存里构造合法 zip (顶层目录 jdk-21.0.8+15)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("jdk-21.0.8+15/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("hello"))
	zw.Close()
	zipBytes := buf.Bytes()
	sum := sha256.Sum256(zipBytes)

	var gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			atomic.AddInt32(&gets, 1)
			w.Write(zipBytes) // HEAD (remoteSizeMB) 落到默认 200 空体
		}
	}))
	defer srv.Close()

	asset := &app.Asset{
		Semver: "21.0.8+15", Major: 21, Distro: "fake",
		ReleaseName: "21.0.8+15",
		ZipURL:      srv.URL + "/jdk.zip",
		Checksum:    hex.EncodeToString(sum[:]),
	}

	if err := Install(asset, "fake"); err != nil {
		t.Fatalf("第一次安装失败: %v", err)
	}
	// 模拟卸载: 移除版本目录, 缓存 zip 保留
	if err := os.RemoveAll(filepath.Join(paths.VersionsDir, "fake-21.0.8+15")); err != nil {
		t.Fatal(err)
	}
	if err := Install(asset, "fake"); err != nil {
		t.Fatalf("第二次安装 (应走缓存) 失败: %v", err)
	}
	if n := atomic.LoadInt32(&gets); n != 1 {
		t.Errorf("重装应命中缓存零下载, 实际 GET 次数 = %d", n)
	}
	if _, err := os.Stat(filepath.Join(paths.CacheDir, "fake-21.0.8+15.zip")); err != nil {
		t.Errorf("安装后缓存 zip 应留存: %v", err)
	}
}

// TestInstallChecksumMismatchPurgesCache 验证校验失败会清除缓存文件,
// 下次安装重新下载 (缓存投毒/镜像损坏自愈)。
func TestInstallChecksumMismatchPurgesCache(t *testing.T) {
	withTempJdkDirs(t)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("jdk-17.0.12+7/readme.txt")
	w.Write([]byte("hello"))
	zw.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write(buf.Bytes())
		}
	}))
	defer srv.Close()

	asset := &app.Asset{
		Semver: "17.0.12+7", Major: 17, Distro: "fake",
		ReleaseName: "17.0.12+7",
		ZipURL:      srv.URL + "/jdk.zip",
		Checksum:    "0000000000000000000000000000000000000000000000000000000000000000",
	}
	if err := Install(asset, "fake"); err == nil {
		t.Fatal("校验和不匹配应报错")
	}
	cacheFile := filepath.Join(paths.CacheDir, "fake-17.0.12+7.zip")
	if _, err := os.Stat(cacheFile); !os.IsNotExist(err) {
		t.Errorf("校验失败后缓存文件应被删除, stat err = %v", err)
	}
}
