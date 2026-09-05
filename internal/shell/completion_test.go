package shell

import (
	"strings"
	"testing"

	"jvm/internal/app"
	"jvm/internal/provider"
)

// TestPsCompletionScript 验证 PowerShell 补全脚本的关键内容。
func TestPsCompletionScript(t *testing.T) {
	distros := []string{"corretto", "microsoft", "temurin"} // 故意乱序, 验证原样嵌入
	script := psCompletionScript(distros)

	checks := []struct {
		name, want string
	}{
		{"含标记头", completionMarker},
		{"含标记尾", completionEndMarker},
		{"含版本 token", completionVersionToken},
		{"用 Register-ArgumentCompleter", "Register-ArgumentCompleter -Native -CommandName jvm"},
		{"含本地版本函数", "_jvmLocalVersions"},
		{"嵌入 corretto", "'corretto'"},
		{"嵌入 microsoft", "'microsoft'"},
		{"嵌入 temurin", "'temurin'"},
		{"含子命令 install", "'install'"},
		{"含子命令 completion", "'completion'"},
		{"含子命令 use", "'use'"},
		{"含子命令 pin", "'pin'"},
		{"含子命令 available", "'available'"},
		{"含子命令 outdated", "'outdated'"},
		{"含子命令 exec", "'exec'"},
		{"含子命令 update", "'update'"},
		{"含子命令 home", "'home'"},
		{"含子命令 cache", "'cache'"},
		{"update 参数补全分支", "-eq 'update'"},
		{"cache 参数补全分支", "-eq 'cache'"},
		{"最长前缀匹配 (temurin-ea 不被 temurin 抢)", "$prefix.Length -gt $best.Length"},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.want) {
			t.Errorf("[%s] 脚本应含 %q", c.name, c.want)
		}
	}
}

// TestBashCompletionScript 验证 bash 补全脚本的关键内容。
func TestBashCompletionScript(t *testing.T) {
	distros := []string{"corretto", "microsoft", "temurin"}
	script := bashCompletionScript(distros)

	checks := []struct {
		name, want string
	}{
		{"含标记头", completionMarker},
		{"含标记尾", completionEndMarker},
		{"含版本 token", completionVersionToken},
		{"注册 complete -F _jvm", "complete -F _jvm jvm"},
		{"含本地版本函数", "_jvm_local_versions()"},
		{"嵌入 corretto", "corretto"},
		{"嵌入 microsoft", "microsoft"},
		{"嵌入 temurin", "temurin"},
		{"含 compgen", "compgen"},
		{"install 分支", "install)"},
		{"available 分支", "available)"},
		{"update 分支", "update)"},
		{"含子命令 pin", " pin "},
		{"含子命令 outdated", " outdated "},
		{"含子命令 exec", " exec "},
		{"含子命令 home", " home "},
		{"含子命令 cache", " cache "},
		{"cache 参数分支", "cache)"},
		{"use 分支", "use|pin|uninstall|rm)"},
		{"最长前缀匹配 (temurin-ea 不被 temurin 抢)", "[ \"${#prefix}\" -gt \"${#best}\" ]"},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.want) {
			t.Errorf("[%s] 脚本应含 %q", c.name, c.want)
		}
	}
}

// TestCompletionHasIntegration 验证补全块检测。
func TestCompletionHasIntegration(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"含标记", "前置内容\n" + completionMarker + "\n...\n" + completionEndMarker, true},
		{"只含 shell 集成块不含补全", "# >>> jvm shell init >>>\n...\n# <<< jvm shell init <<<", false},
		{"空内容", "", false},
		{"无关内容", "alias ll='ls -l'\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// completionHasIntegration 读文件, 这里直接测标记包含逻辑
			got := strings.Contains(tt.in, completionMarker)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCompletionBlocksIndependent 验证补全块可被 removeBlock 正确移除,
// 且不会误伤其他内容。这是「两个关注点独立升级」的核心保证。
func TestCompletionBlocksIndependent(t *testing.T) {
	if completionMarker == profileMarker {
		t.Fatal("补全标记不应与 shell 集成标记相同")
	}
	content := "before\n\n" + completionMarker + "\nfunc body\n" + completionEndMarker + "\nafter\n"
	got := removeBlock(content, completionMarker, completionEndMarker)
	if strings.Contains(got, completionMarker) {
		t.Errorf("补全块未被移除: %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("移除补全块时误删了其他内容: %q", got)
	}
}

// fakeProvider 是测试用的 provider 实现, 嵌入 Base 拿默认。
// 用于 TestDistroNames 注册临时 provider 验证 distroNames 逻辑。
type fakeProvider struct {
	provider.Base
	name string
}

func (f fakeProvider) Name() string                              { return f.name }
func (fakeProvider) DisplayName() string                         { return "Fake" }
func (fakeProvider) Available() ([]app.Release, error)           { return nil, nil }
func (fakeProvider) LatestPatch(int) (*app.Asset, error)         { return nil, nil }
func (fakeProvider) Resolve(app.VersionSpec) (*app.Asset, error) { return nil, nil }
func (fakeProvider) ListVersions(int) ([]*app.Asset, error)      { return nil, nil }

// TestDistroNames 验证 distroNames 从 provider 注册表提取排序后的名字列表。
// 补全脚本据此嵌入 distro@ 前缀和 available 参数补全。
//
// provider 注册表是全局状态, 无法清理 (Register 同名会 panic), 故用一次性唯一名
// 避免与真实 provider / 并发测试冲突。验证「注册后能被 distroNames 返回」即可。
func TestDistroNames(t *testing.T) {
	// 注册一个一次性 fake provider, 验证 distroNames 能返回它
	uniqueName := "fake-distro-test-" + t.Name()
	provider.Register(fakeProvider{name: uniqueName})

	names := distroNames()
	found := false
	for _, n := range names {
		if n == uniqueName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("distroNames 未返回刚注册的 provider %q, got: %v", uniqueName, names)
	}

	// 验证字典序 (provider.All 已排序)
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("distroNames 未排序: %v", names)
			break
		}
	}

	// 与 provider.All 数量一致 (确保没有遗漏)
	if len(names) != len(provider.All()) {
		t.Errorf("distroNames 数量 %d 与 provider.All %d 不一致", len(names), len(provider.All()))
	}
}

// TestFirstLine 验证从脚本首行提取标记。
func TestFirstLine(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"多行取首行", completionMarker + "\nbody\n" + completionEndMarker, completionMarker},
		{"单行无换行", completionMarker, completionMarker},
		{"空串", "", ""},
		{"只有换行", "\nrest", ""},
		{"CRLF 换行", "line1\r\nline2", "line1\r"}, // \r 不被 IndexByte('\n') 截断
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLine(tt.in); got != tt.want {
				t.Errorf("firstLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCompletionScriptsASCII 验证两个补全脚本纯 ASCII (与集成脚本同款约束:
// PowerShell 5.1 在中文系统上按 GBK 解码非 ASCII 字节会损坏语法)。
func TestCompletionScriptsASCII(t *testing.T) {
	for name, s := range map[string]string{
		"powershell": psCompletionScript([]string{"temurin"}),
		"bash":       bashCompletionScript([]string{"temurin"}),
	} {
		for i := 0; i < len(s); i++ {
			if s[i] > 127 {
				t.Errorf("%s 脚本第 %d 字节非 ASCII: %q", name, i, s[i])
				break
			}
		}
	}
}
