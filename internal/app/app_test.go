package app

import (
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
		{"0", 0, true},     // 非正整数
		{"-1", 0, true},    // 负数
		{"abc", 0, true},   // 非数字
		{"", 0, true},      // 空
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
