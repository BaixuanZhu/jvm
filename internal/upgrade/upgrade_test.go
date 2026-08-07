package upgrade

import (
	"runtime"
	"strings"
	"testing"
)

func TestExpectedAssetName(t *testing.T) {
	got := expectedAssetName()
	want := "jvm-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
	if got != want {
		t.Errorf("expectedAssetName() = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, ".zip") {
		t.Errorf("expectedAssetName() 应以 .zip 结尾: %q", got)
	}
}

func TestParseChecksum(t *testing.T) {
	const text = `0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  jvm-windows-amd64.zip
fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210  jvm-windows-arm64.zip
deadbeef  other-file.txt
`
	tests := []struct {
		name     string
		filename string
		wantHash string
		wantOK   bool
	}{
		{"标准双空格格式", "jvm-windows-amd64.zip", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
		{"匹配另一个 asset", "jvm-windows-arm64.zip", "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210", true},
		{"文件名不存在", "jvm-linux-amd64.zip", "", false},
		{"空文件名", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, ok := parseChecksum(text, tt.filename)
			if hash != tt.wantHash || ok != tt.wantOK {
				t.Errorf("parseChecksum(%q) = (%q, %v), want (%q, %v)",
					tt.filename, hash, ok, tt.wantHash, tt.wantOK)
			}
		})
	}

	// 边界: 单空格分隔也应兼容
	t.Run("单空格分隔", func(t *testing.T) {
		hash, ok := parseChecksum("abc123 jvm-windows-amd64.zip", "jvm-windows-amd64.zip")
		if !ok || hash != "abc123" {
			t.Errorf("单空格分隔解析失败: hash=%q ok=%v", hash, ok)
		}
	})

	// 边界: 空文本
	t.Run("空文本", func(t *testing.T) {
		if _, ok := parseChecksum("", "jvm-windows-amd64.zip"); ok {
			t.Error("空文本应返回 ok=false")
		}
	})
}
