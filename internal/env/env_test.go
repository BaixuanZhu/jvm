package env

import (
	"path/filepath"
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

// TestFilterUserPath 覆盖 RemoveFromUserPath 的纯逻辑部分:
// 命中条目被移除 (大小写不敏感、Clean 后匹配), 其余条目与顺序保留, 无命中原样返回。
func TestFilterUserPath(t *testing.T) {
	remove := func(entries ...string) map[string]bool {
		m := make(map[string]bool, len(entries))
		for _, e := range entries {
			m[strings.ToLower(filepath.Clean(e))] = true
		}
		return m
	}
	tests := []struct {
		name   string
		in     string
		remove map[string]bool
		want   string
	}{
		{"移除中间条目", `C:\a;C:\jdk17\bin;C:\b`, remove(`C:\jdk17\bin`), `C:\a;C:\b`},
		{"大小写不敏感", `C:\a;C:\JDK17\BIN;C:\b`, remove(`c:\jdk17\bin`), `C:\a;C:\b`},
		{"斜杠形式等价", `C:\a;C:/jdk17/bin;C:\b`, remove(`C:\jdk17\bin`), `C:\a;C:\b`},
		{"移除多个", `C:\j1;C:\j2;C:\j3`, remove(`C:\j1`, `C:\j3`), `C:\j2`},
		{"无命中原样返回", `C:\a;C:\b`, remove(`C:\zz`), `C:\a;C:\b`},
		{"移除后为空", `C:\jdk17\bin`, remove(`C:\jdk17\bin`), ``},
		{"命中但带空格", `C:\a; C:\jdk17\bin ;C:\b`, remove(`C:\jdk17\bin`), `C:\a;C:\b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterUserPath(tt.in, tt.remove)
			if got != tt.want {
				t.Errorf("filterUserPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
