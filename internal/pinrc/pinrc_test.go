package pinrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		// 单行版本号
		{"bare major", "21\n", "21", false},
		{"distro@version", "corretto@21.0.12.8.1\n", "corretto@21.0.12.8.1", false},
		{"full version", "21.0.12+8\n", "21.0.12+8", false},
		{"no trailing newline", "21", "21", false},

		// 注释 + 空行: 取第一个有效行
		{"comment then version", "# 项目 JDK\n21\n", "21", false},
		{"blank lines then version", "\n\n21\n", "21", false},
		{"version after multiple comments", "# a\n# b\n21\n# c\n", "21", false},

		// CRLF / 前后空白
		{"crlf line ending", "21\r\n", "21", false},
		{"leading spaces", "   21\n", "21", false},
		{"trailing spaces", "21   \n", "21", false},

		// UTF-8 BOM (部分编辑器 / PowerShell 会写)
		{"utf8 bom", "\uFEFF21\n", "21", false},

		// 错误: 空 / 全注释
		{"empty", "", "", true},
		{"only newlines", "\n\n\n", "", true},
		{"only comments", "# a\n# b\n", "", true},
		{"bom then comment only", "\uFEFF# just a comment\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse(%q) 期望报错, got %q", tt.content, got)
				}
				return
			}
			if err != nil {
				t.Errorf("Parse(%q) 意外报错: %v", tt.content, err)
				return
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

// TestFindUp 构造嵌套临时目录树, 验证向上查找行为 (子目录命中上层项目根)。
func TestFindUp(t *testing.T) {
	// 树:
	//   root/                 (无 .jvmrc)
	//     proj/.jvmrc         内容 "corretto@21"
	//       sub/              (从这里向上找应命中 proj)
	//         leaf/.jvmrc     内容 "17" (更近, 从 leaf 找命中这个)
	//     empty/              (完全无 .jvmrc 的子树)
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	sub := filepath.Join(proj, "sub")
	leaf := filepath.Join(sub, "leaf")
	empty := filepath.Join(root, "empty")
	for _, d := range []string{proj, sub, leaf, empty} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(proj, Filename), []byte("corretto@21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leaf, Filename), []byte("17\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		startDir string
		want     string // 期望解析出的版本号; "" 表示期望未找到
		wantDir  string // 期望命中文件所在目录
	}{
		{"从 leaf 向上找命中最近的", leaf, "17", leaf},
		{"从 sub 向上找命中 proj", sub, "corretto@21", proj},
		{"从 proj 直接命中", proj, "corretto@21", proj},
		{"empty 子树无 .jvmrc", empty, "", ""},
		{"root 无 .jvmrc", root, "", ""},
		{"空 startDir 视为未找到", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, foundPath, found := FindUp(tt.startDir)
			if tt.want == "" {
				if found {
					t.Errorf("FindUp(%q) 期望未找到, got content=%q path=%q", tt.startDir, content, foundPath)
				}
				return
			}
			if !found {
				t.Errorf("FindUp(%q) 期望找到, 实际未找到", tt.startDir)
				return
			}
			got, err := Parse(content)
			if err != nil {
				t.Errorf("FindUp(%q) 命中内容解析失败: %v", tt.startDir, err)
				return
			}
			if got != tt.want {
				t.Errorf("FindUp(%q) 解析 = %q, want %q", tt.startDir, got, tt.want)
			}
			if tt.wantDir != "" && filepath.Dir(foundPath) != tt.wantDir {
				t.Errorf("FindUp(%q) 命中目录 = %q, want %q", tt.startDir, filepath.Dir(foundPath), tt.wantDir)
			}
		})
	}
}

// TestWrite 验证 Write 写出的文件能被 Parse 正确读回 (往返一致性)。
func TestWrite(t *testing.T) {
	dir := t.TempDir()
	spec := "corretto@21.0.12.8.1"
	if err := Write(dir, spec); err != nil {
		t.Fatalf("Write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, Filename))
	if err != nil {
		t.Fatalf("读回 .jvmrc: %v", err)
	}
	got, err := Parse(string(b))
	if err != nil {
		t.Fatalf("Parse 写出的内容: %v", err)
	}
	if got != spec {
		t.Errorf("往返 = %q, want %q", got, spec)
	}
	// 文件首行应为注释 (说明用途, 用户 cat 时能看懂)
	if !strings.HasPrefix(string(b), "#") {
		t.Errorf("写出的 .jvmrc 应以注释行开头, got: %q", string(b))
	}
}
