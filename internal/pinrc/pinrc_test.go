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

func TestParseToolVersions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string // 期望 spec; "" 表示期望未命中
	}{
		// java 行的各种版本形态
		{"bare major", "java 21\n", "21"},
		{"half version", "java 21.0.2\n", "21.0.2"},
		{"full version", "java 21.0.2+11\n", "21.0.2+11"},

		// 发行版前缀 (asdf 插件名): 拆成 distro@version
		{"temurin prefix", "java temurin-21.0.2+11\n", "temurin@21.0.2+11"},
		{"corretto prefix", "java corretto-21.0.12.8.1\n", "corretto@21.0.12.8.1"},
		{"zulu prefix", "java zulu-21.30.15\n", "zulu@21.30.15"},
		{"temurin-ea prefix 不被 temurin 截胡", "java temurin-ea-28+14-ea-beta\n", "temurin-ea@28+14-ea-beta"},
		{"未知前缀整段当版本", "java oracle-21\n", "oracle-21"},

		// 多工具 / 多版本 / 注释
		{"多工具取 java 行", "nodejs 20.11.0\njava 21\npython 3.12\n", "21"},
		{"java 行在后", "nodejs 20\ngradle 8.5\njava temurin-17.0.12+11\n", "temurin@17.0.12+11"},
		{"多版本并列取第一个", "java 21 17\n", "21"},
		{"注释与空行", "# asdf\n\njava 21\n", "21"},
		{"java 大写容错", "Java 21\n", "21"},

		// CRLF / BOM
		{"crlf", "java 21\r\n", "21"},
		{"utf8 bom", "\uFEFFjava 21\n", "21"},

		// 未命中: 无 java 行 / 只有工具名无版本
		{"无 java 行", "nodejs 20\npython 3.12\n", ""},
		{"空文件", "", ""},
		{"全注释", "# a\n# b\n", ""},
		{"java 无版本段", "java\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseToolVersions(tt.content)
			if tt.want == "" {
				if ok {
					t.Errorf("parseToolVersions(%q) 期望未命中, got %q", tt.content, got)
				}
				return
			}
			if !ok {
				t.Errorf("parseToolVersions(%q) 期望命中 %q, 实际未命中", tt.content, tt.want)
				return
			}
			if got != tt.want {
				t.Errorf("parseToolVersions(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestParseSDKMANrc(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string // 期望 spec; "" 表示期望未命中
	}{
		// 发行版标识后缀全量映射
		{"tem → temurin", "java=21.0.2-tem\n", "temurin@21.0.2"},
		{"amzn → corretto", "java=17.0.12-amzn\n", "corretto@17.0.12"},
		{"ms → microsoft", "java=21-ms\n", "microsoft@21"},
		{"zul → zulu", "java=21.0.2-zul\n", "zulu@21.0.2"},
		{"librca → liberica", "java=21.0.2-librca\n", "liberica@21.0.2"},
		{"graal → graalvm", "java=21.0.2-graal\n", "graalvm@21.0.2"},

		// 裸值 (无标识): 缺省发行版
		{"裸 major", "java=21\n", "21"},
		{"裸半截版本", "java=21.0.2\n", "21.0.2"},

		// 多行 / 注释 / 容错
		{"多 key 取 java 行", "# sdkman config\njava=21.0.2-tem\ngradle=8.5\n", "temurin@21.0.2"},
		{"key 大小写容错", "Java=21.0.2-tem\n", "temurin@21.0.2"},
		{"等号两侧空格", "java = 21.0.2-tem\n", "temurin@21.0.2"},
		{"crlf", "java=21.0.2-tem\r\n", "temurin@21.0.2"},
		{"utf8 bom", "\uFEFFjava=21.0.2-tem\n", "temurin@21.0.2"},

		// 未命中: 无 java 行 / 未知标识 / 空值
		{"无 java 行", "gradle=8.5\nmaven=3.9\n", ""},
		{"空文件", "", ""},
		{"未知标识 oracle", "java=21.0.2-oracle\n", ""},
		{"未知标识 open", "java=21.0.2-open\n", ""},
		{"只有标识无版本", "java=-tem\n", ""},
		{"java 空值", "java=\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSDKMANrc(tt.content)
			if tt.want == "" {
				if ok {
					t.Errorf("parseSDKMANrc(%q) 期望未命中, got %q", tt.content, got)
				}
				return
			}
			if !ok {
				t.Errorf("parseSDKMANrc(%q) 期望命中 %q, 实际未命中", tt.content, tt.want)
				return
			}
			if got != tt.want {
				t.Errorf("parseSDKMANrc(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

// TestFindUp 构造嵌套临时目录树, 验证向上查找与候选文件优先级行为。
func TestFindUp(t *testing.T) {
	// 树:
	//   root/                    (无 rc 文件)
	//     proj/.jvmrc            内容 "corretto@21"
	//       sub/.sdkmanrc        内容 "java=17.0.2-tem" (sub 层, 优先于上层 .jvmrc)
	//         leaf/.java-version 内容 "21"              (更近, 优先于 sub 的 .sdkmanrc)
	//       mixed/.jvmrc         空文件 (被跳过) + .tool-versions "java temurin-21.0.2+11"
	//     prio/.tool-versions    "java 21" + .sdkmanrc "java=17-amzn" (同层, 前者优先)
	//     empty/                 (完全无 rc 文件的子树)
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	sub := filepath.Join(proj, "sub")
	leaf := filepath.Join(sub, "leaf")
	mixed := filepath.Join(proj, "mixed")
	prio := filepath.Join(root, "prio")
	empty := filepath.Join(root, "empty")
	for _, d := range []string{proj, sub, leaf, mixed, prio, empty} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(proj, ".jvmrc"):          "corretto@21\n",
		filepath.Join(sub, ".sdkmanrc"):        "java=17.0.2-tem\n",
		filepath.Join(leaf, ".java-version"):   "21\n",
		filepath.Join(mixed, ".jvmrc"):         "", // 空 .jvmrc: 视为未命中, 落到下一候选
		filepath.Join(mixed, ".tool-versions"): "java temurin-21.0.2+11\n",
		filepath.Join(prio, ".tool-versions"):  "java 21\n",
		filepath.Join(prio, ".sdkmanrc"):       "java=17-amzn\n",
	}
	for p, content := range files {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name     string
		startDir string
		want     string // 期望解析出的 spec; "" 表示期望未找到
		wantSrc  Source
		wantDir  string // 期望命中文件所在目录
	}{
		{"从 leaf 向上命中最近的 .java-version", leaf, "21", SrcJavaVersion, leaf},
		{"从 sub 命中本层 .sdkmanrc (优先于上层 .jvmrc)", sub, "temurin@17.0.2", SrcSDKMANrc, sub},
		{"从 proj 直接命中 .jvmrc", proj, "corretto@21", SrcJVMRC, proj},
		{"空 .jvmrc 被跳过, 落到同层 .tool-versions", mixed, "temurin@21.0.2+11", SrcToolVersions, mixed},
		{"同层候选按优先级: .tool-versions 先于 .sdkmanrc", prio, "21", SrcToolVersions, prio},
		{"empty 子树无 rc 文件", empty, "", 0, ""},
		{"root 无 rc 文件", root, "", 0, ""},
		{"空 startDir 视为未找到", "", "", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, src, foundPath, found := FindUp(tt.startDir)
			if tt.want == "" {
				if found {
					t.Errorf("FindUp(%q) 期望未找到, got spec=%q path=%q", tt.startDir, spec, foundPath)
				}
				return
			}
			if !found {
				t.Errorf("FindUp(%q) 期望找到, 实际未找到", tt.startDir)
				return
			}
			if spec != tt.want {
				t.Errorf("FindUp(%q) spec = %q, want %q", tt.startDir, spec, tt.want)
			}
			if src != tt.wantSrc {
				t.Errorf("FindUp(%q) src = %v, want %v", tt.startDir, src, tt.wantSrc)
			}
			if filepath.Dir(foundPath) != tt.wantDir {
				t.Errorf("FindUp(%q) 命中目录 = %q, want %q", tt.startDir, filepath.Dir(foundPath), tt.wantDir)
			}
		})
	}
}

// TestCandidateFilesSourceOrder 验证 Source 枚举值与 CandidateFiles 下标对应
// (FindUp 的实现依赖这一点, shell 集成脚本的候选清单也由同一切片生成)。
func TestCandidateFilesSourceOrder(t *testing.T) {
	if len(CandidateFiles) != 4 {
		t.Fatalf("CandidateFiles 应有 4 个候选, got %d", len(CandidateFiles))
	}
	for i, name := range CandidateFiles {
		if Source(i) == SrcJVMRC && name != ".jvmrc" {
			t.Errorf("CandidateFiles[0] 应为 .jvmrc, got %s", name)
		}
	}
	if CandidateFiles[0] != ".jvmrc" || Filename != ".jvmrc" {
		t.Error("原生文件名应同为 .jvmrc (pin 写入目标与首选候选一致)")
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
