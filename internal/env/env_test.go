package env

import (
	"strings"
	"testing"
)

// TestSplitPathEntries 覆盖 PATH 分号拆分逻辑。
// splitPathEntries 是 env 包 PATH 操作 (EnsureCurrentInPath/EnsureUserPath) 的基础,
// 行为要点: 按 ";" 拆分; 丢弃纯空白条目; 保留条目内部首尾空格 (不 Trim)。
func TestSplitPathEntries(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"空串", "", []string{}},
		{"只有分隔符", ";;;", []string{}},
		{"单条目", `C:\bin`, []string{`C:\bin`}},
		{"多条目", `C:\a;C:\b;C:\c`, []string{`C:\a`, `C:\b`, `C:\c`}},
		{"首尾有空条目", `;C:\bin;`, []string{`C:\bin`}},
		{"中间有空条目", `C:\a;;C:\b`, []string{`C:\a`, `C:\b`}},
		{"纯空白条目被丢弃", "   ;C:\\bin;\t", []string{`C:\bin`}},
		{"条目内部空格被保留", `C:\Program Files\x; C:\y `, []string{`C:\Program Files\x`, ` C:\y `}},
		{"尾部空白条目", `C:\bin;   `, []string{`C:\bin`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitPathEntries(tt.in)
			if len(got) != len(tt.want) {
				t.Errorf("splitPathEntries(%q) = %v (len %d), want %v (len %d)",
					tt.in, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitPathEntries(%q)[%d] = %q, want %q",
						tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestSplitPathEntriesRoundTrip 验证: 含 current/bin 的 PATH 拆分后能正确定位,
// 这是 EnsureCurrentInPath 幂等判断的前提。
func TestSplitPathEntriesRoundTrip(t *testing.T) {
	const binPath = `C:\Users\test\.jvm\current\bin`
	path := binPath + `;C:\other\bin` + ";"
	entries := splitPathEntries(path)
	found := false
	for _, e := range entries {
		if strings.EqualFold(strings.TrimSpace(e), binPath) {
			found = true
		}
	}
	if !found {
		t.Errorf("拆分后未找到 current/bin, entries=%v", entries)
	}
}
