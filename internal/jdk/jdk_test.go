package jdk

import "testing"

func TestBaseNameOfURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://example.com/path/jdk-21.zip", "jdk-21.zip"},
		{"https://github.com/owner/repo/releases/download/v1/jvm.exe", "jvm.exe"},
		{"http://localhost:8080/a/b/c.tar.gz", "c.tar.gz"},
		{"https://example.com/", ""}, // 末尾斜杠 → 空串
		{"noslash.txt", "noslash.txt"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := baseNameOfURL(tt.in); got != tt.want {
			t.Errorf("baseNameOfURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
