package shell

import (
	"strings"
	"testing"
)

func TestToMSYSPath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`D:\code\jvm\jvm.exe`, `/d/code/jvm/jvm.exe`},
		{`C:\Users\foo`, `/c/Users/foo`},     // 大写盘符转小写
		{`d:/foo/bar`, `/d/foo/bar`},         // 已是正斜杠 + 盘符
		{`/usr/local/bin`, `/usr/local/bin`}, // 无盘符, 原样
		{`relative\path`, `relative/path`},   // 相对路径, 仅转斜杠
	}
	for _, tt := range tests {
		if got := toMSYSPath(tt.in); got != tt.want {
			t.Errorf("toMSYSPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestShellLabel(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"powershell", "PowerShell"},
		{"pwsh", "PowerShell"},
		{"ps", "PowerShell"},
		{"bash", "bash"},
		{"sh", "bash"},
		{"git-bash", "bash"},
		{"unknown", "unknown"}, // 未识别原样返回
	}
	for _, tt := range tests {
		if got := shellLabel(tt.in); got != tt.want {
			t.Errorf("shellLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRemoveOldBlock(t *testing.T) {
	const marker = "# >>> jvm shell init >>>"
	const endMarker = "# <<< jvm shell init <<<"
	fullBlock := marker + "\nfunction jvm { ... }\n" + endMarker

	tests := []struct {
		name, in, want string
	}{
		{
			name: "无 marker 原样返回",
			in:   "alias ll='ls -l'\n",
			want: "alias ll='ls -l'\n",
		},
		{
			name: "完整块被移除",
			in:   "before\n\n" + fullBlock + "\nafter\n",
			want: "before\n\nafter\n",
		},
		{
			name: "只有块本身 (整段清空)",
			in:   fullBlock,
			want: "",
		},
		{
			name: "块在末尾 + 尾随换行",
			in:   "content\n" + fullBlock + "\n",
			want: "content\n",
		},
		{
			name: "只有开头 marker (无 endMarker) 原样返回",
			in:   "x\n" + marker + "\ny\n",
			want: "x\n" + marker + "\ny\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeOldBlock(tt.in)
			if got != tt.want {
				t.Errorf("removeOldBlock(%q) =\n %q\nwant:\n %q", tt.name, got, tt.want)
			}
			// 确保处理结果不含残留 marker
			if strings.Contains(got, marker) && tt.want != tt.in {
				t.Errorf("[%s] 结果仍含 marker", tt.name)
			}
		})
	}
}
