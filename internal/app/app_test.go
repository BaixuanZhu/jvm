package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseMajorVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"21", 21, false},
		{"17", 17, false},
		{"11", 11, false},
		{" 17 ", 17, false}, // 前后空格应被 trim
		{"1", 1, false},
		{"0", 0, true},       // 非正整数
		{"-1", 0, true},      // 负数
		{"abc", 0, true},     // 非数字
		{"", 0, true},        // 空
		{"21.0.12", 0, true}, // 非整数 (小数)
	}
	for _, tt := range tests {
		got, err := ParseMajorVersion(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseMajorVersion(%q) 期望出错, 实际 err=nil got=%d", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMajorVersion(%q) 意料之外错误: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMajorVersion(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestUserAgent(t *testing.T) {
	ua := UserAgent()
	if !strings.Contains(ua, "jvm/") {
		t.Errorf("UserAgent() 不含 'jvm/': %q", ua)
	}
	if !strings.Contains(ua, Version) {
		t.Errorf("UserAgent() 不含 Version=%q: got %q", Version, ua)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1/0/1
	}{
		// 相等
		{"0.6.1", "0.6.1", 0},
		{"21.0.5+11", "21.0.5+11", 0},
		// 主版本号差异
		{"0.6.1", "0.7.0", -1},
		{"1.0.0", "0.9.9", 1},
		// 次版本号差异
		{"0.6.0", "0.6.1", -1},
		{"0.6.2", "0.6.1", 1},
		// 短版本补 0 比较: "1.0" == "1.0.0"
		{"1.0", "1.0.0", 0},
		{"1.0", "1.0.1", -1},
		// 含 build 号 (+N)
		{"21.0.5+11", "21.0.5+12", -1},
		// 非法段按 0
		{"1.x.0", "1.0.0", 0},
	}
	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		// 归一化: 只关心符号
		sign := 0
		if got < 0 {
			sign = -1
		} else if got > 0 {
			sign = 1
		}
		if sign != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, sign, tt.want)
		}
	}
}

func TestLatestGitHubTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "jvm/") {
			t.Errorf("请求未带正确 User-Agent: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v9.9.9"})
	}))
	defer srv.Close()

	tag, err := fetchTag(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchTag 失败: %v", err)
	}
	if tag != "v9.9.9" {
		t.Errorf("got tag %q, want v9.9.9", tag)
	}
}

func TestLatestGitHubTag_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := fetchTag(context.Background(), srv.URL); err == nil {
		t.Error("期望 500 响应返回错误, 实际 err=nil")
	}
}
