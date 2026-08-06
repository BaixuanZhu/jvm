package adoptium

import "testing"

func TestShortSemver(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"21.0.5+11.0.LTS", "21.0.5+11"},
		{"21.0.12+8.0.LTS", "21.0.12+8"},
		{"17.0.13+11.0.LTS", "17.0.13+11"},
		{"23+36", "23+36"},         // 无 minor.security, 仅 major+build
		{"23+36.0.LTS", "23+36"},   // build 自带 .LTS 后缀
		{"23.0.1+11", "23.0.1+11"}, // 已是简短形式
		{"23.0.1", "23.0.1"},       // 无 build 号
		{"", ""},                   // 空串
	}
	for _, tt := range tests {
		if got := ShortSemver(tt.in); got != tt.want {
			t.Errorf("ShortSemver(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
