package jdk

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestFileHash 验证 fileHash 按算法分流 (sha256/sha1)、空与未知算法回退 sha256、
// 大小写/连字符容忍。用固定内容 "hello" 的已知哈希向量, 不触网。
func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	const wantSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	const wantSHA1 = "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"

	cases := []struct {
		algo string
		want string
	}{
		{"sha256", wantSHA256},
		{"sha1", wantSHA1},
		{"SHA-1", wantSHA1}, // 连字符 + 大小写容忍
		{"", wantSHA256},    // 空默认 sha256
		{"md5", wantSHA256}, // 未知算法回退 sha256
	}
	for _, c := range cases {
		got, err := fileHash(path, c.algo)
		if err != nil {
			t.Fatalf("fileHash(algo=%q) 出错: %v", c.algo, err)
		}
		if got != c.want {
			t.Errorf("fileHash(algo=%q) = %q, want %q", c.algo, got, c.want)
		}
	}
}

// TestDownloadFile_Retry 验证: 前两次返回 500, 第三次 200, 最终下载成功。
func TestDownloadFile_Retry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "hello jvm")
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	// WithRetries(2) → 最多尝试 3 次 (1 次初始 + 2 次重试)。
	if err := DownloadFile(srv.URL, dest, WithRetries(2)); err != nil {
		t.Fatalf("DownloadFile 失败: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello jvm" {
		t.Errorf("下载内容 = %q, want %q", data, "hello jvm")
	}
	if calls != 3 {
		t.Errorf("请求次数 = %d, want 3", calls)
	}
}

// TestDownloadFile_RetryExhausted 验证: 始终 500, 最终报错, 重试次数用尽。
func TestDownloadFile_RetryExhausted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	err := DownloadFile(srv.URL, dest, WithRetries(2))
	if err == nil {
		t.Fatal("期望持续 500 返回错误, 实际 nil")
	}
	if calls != 3 { // 1 初始 + 2 重试
		t.Errorf("请求次数 = %d, want 3", calls)
	}
}

// TestDownloadFile_Resume 验证断点续传: 预写 .part 前半, 服务端收到 Range 头,
// 返回 206 + 后半, 最终文件完整。
func TestDownloadFile_Resume(t *testing.T) {
	const full = "0123456789ABCDEF" // 16 字节
	var gotRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		// 支持 Range: 返回请求偏移之后的内容, 206。
		w.Header().Set("Content-Range", "bytes 8-15/16")
		w.WriteHeader(http.StatusPartialContent)
		io.WriteString(w, full[8:]) // 后 8 字节
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	// 预写 .part 前 8 字节, 模拟上次中断。
	if err := os.WriteFile(dest+".part", []byte(full[:8]), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DownloadFile(srv.URL, dest, WithResume(true)); err != nil {
		t.Fatalf("DownloadFile 失败: %v", err)
	}
	if gotRange != "bytes=8-" {
		t.Errorf("Range 头 = %q, want %q", gotRange, "bytes=8-")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != full {
		t.Errorf("续传后内容 = %q, want %q", data, full)
	}
	// .part 应被 rename 掉, 不再存在。
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part 应已被 rename, 仍存在: %v", err)
	}
}

// TestDownloadFile_NoResumeFallback 验证: 服务端忽略 Range 返回 200,
// 应从头覆盖下载。
func TestDownloadFile_NoResumeFallback(t *testing.T) {
	const full = "complete-file"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 忽略 Range, 返回完整内容。
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, full)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	// 预写脏 .part, 应被覆盖 (TRUNC)。
	if err := os.WriteFile(dest+".part", []byte("GARBAGE-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DownloadFile(srv.URL, dest, WithResume(true)); err != nil {
		t.Fatalf("DownloadFile 失败: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != full {
		t.Errorf("内容 = %q, want %q (应从头覆盖)", data, full)
	}
}

// TestDownloadFile_Unexpected206 验证: 未请求 Range (无 .part) 却收到 206,
// 应报错而非误当作续传写入 (防止服务端异常时数据错乱)。
func TestDownloadFile_Unexpected206(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不论有无 Range 头, 一律返回 206 (模拟配置异常的服务端)。
		w.WriteHeader(http.StatusPartialContent)
		io.WriteString(w, "partial")
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	// 不预写 .part → offset=0, 未请求 Range, 却收到 206 → 应报错。
	err := DownloadFile(srv.URL, dest, WithResume(true))
	if err == nil {
		t.Fatal("未请求 Range 却收到 206, 应报错")
	}
}
func TestDownloadFile_DefaultNoOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "plain")
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	if err := DownloadFile(srv.URL, dest); err != nil {
		t.Fatalf("DownloadFile 失败: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "plain") {
		t.Errorf("内容 = %q, want 含 'plain'", data)
	}
	// 非 resume 模式不应产生 .part 文件。
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Errorf("非续传模式不应产生 .part")
	}
}

// TestDownloadFile_InterruptThenResume 是最接近真实场景的续传集成测试:
// 服务端第一次请求中途 hijack 断开 (模拟网络中断), .part 保留部分数据;
// 第二次请求正常返回剩余部分, 最终文件完整。
// 这覆盖了 "真实中断 → 保留 .part → 再次调用续传 → 完整" 的完整链路。
func TestDownloadFile_InterruptThenResume(t *testing.T) {
	// 构造 256KB 内容 (足够大, 确保不会一次 read 就读完导致无法中断)。
	content := make([]byte, 256*1024)
	for i := range content {
		content[i] = byte(i)
	}

	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requestCount, 1)

		start := int64(0)
		if rng := r.Header.Get("Range"); strings.HasPrefix(rng, "bytes=") {
			fmt.Sscanf(rng, "bytes=%d-", &start)
		}

		if start > 0 {
			// 续传请求: 返回剩余部分, 206。
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(content[start:])
			return
		}

		// 全量请求: 第 1 次中途断开, 第 2 次正常。
		if n == 1 {
			w.WriteHeader(http.StatusOK)
			// 写前一半然后 hijack 断开。
			half := len(content) / 2
			w.Write(content[:half])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				conn.Close() // 强制 TCP 断开
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")

	// 第一次: 应因连接中断失败, 保留 .part (前半数据)。
	err := DownloadFile(srv.URL, dest, WithResume(true), WithRetries(0))
	if err == nil {
		t.Fatal("第一次下载应在连接断开时报错")
	}
	partInfo, err := os.Stat(dest + ".part")
	if err != nil {
		t.Fatalf("中断后 .part 应保留, 但读取失败: %v", err)
	}
	t.Logf("中断后 .part 大小: %d 字节 (期望约 %d)", partInfo.Size(), len(content)/2)
	if partInfo.Size() == 0 {
		t.Error(".part 为空, 中断前未写入任何数据")
	}

	// 第二次: 续传, 应从断点继续到完整。
	if err := DownloadFile(srv.URL, dest, WithResume(true), WithRetries(0)); err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(content) {
		t.Errorf("最终文件大小 = %d, want %d", len(data), len(content))
	}
	// 逐字节验证完整性。
	for i := range content {
		if data[i] != content[i] {
			t.Fatalf("字节 %d 不匹配: got %d, want %d (续传数据损坏)", i, data[i], content[i])
		}
	}
	t.Log("✅ 中断→续传后文件逐字节完整")

	// .part 应已被 rename 清理。
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Error("续传完成后 .part 应被 rename 清理")
	}
}
