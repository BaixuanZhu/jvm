package shell

import (
	"os"
	"path/filepath"
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
			got := removeBlock(tt.in, marker, endMarker)
			if got != tt.want {
				t.Errorf("removeBlock(%q) =\n %q\nwant:\n %q", tt.name, got, tt.want)
			}
			// 确保处理结果不含残留 marker
			if strings.Contains(got, marker) && tt.want != tt.in {
				t.Errorf("[%s] 结果仍含 marker", tt.name)
			}
		})
	}
}

// TestBlocksIndependent 验证 shell 集成块与补全块用不同标记, 可各自独立移除而不互相影响。
// 这是「两个关注点独立升级」的核心保证: 写补全块时绝不能误删 shell 集成块。
func TestBlocksIndependent(t *testing.T) {
	shellBlock := profileMarker + "\nfunction jvm { ... }\n" + endMarkerFor(profileMarker)
	completionBlock := completionMarker + "\nRegister-ArgumentCompleter\n" + completionEndMarker
	content := "header\n" + shellBlock + "\n" + completionBlock + "\nfooter\n"

	// 只移除补全块, shell 集成块应保留
	onlyShell := removeBlock(content, completionMarker, completionEndMarker)
	if !strings.Contains(onlyShell, profileMarker) {
		t.Error("移除补全块时误删了 shell 集成块")
	}
	if strings.Contains(onlyShell, completionMarker) {
		t.Error("补全块未被移除")
	}

	// 只移除 shell 集成块, 补全块应保留
	onlyCompletion := removeBlock(content, profileMarker, endMarkerFor(profileMarker))
	if !strings.Contains(onlyCompletion, completionMarker) {
		t.Error("移除 shell 集成块时误删了补全块")
	}
	if strings.Contains(onlyCompletion, profileMarker) {
		t.Error("shell 集成块未被移除")
	}
}

// TestEndMarkerFor 验证从起始标记推导结束标记。
// 标记首尾各有 ">>>", 两个都要翻成 "<<<"。
func TestEndMarkerFor(t *testing.T) {
	tests := []struct{ in, want string }{
		{profileMarker, "# <<< jvm shell init <<<"},
		{completionMarker, completionEndMarker}, // "# <<< jvm completion <<<"
	}
	for _, tt := range tests {
		got := endMarkerFor(tt.in)
		if got != tt.want {
			t.Errorf("endMarkerFor(%q) = %q, want %q", tt.in, got, tt.want)
		}
		// 必须不含残留的 >>>, 且含两个 <<<
		if strings.Contains(got, ">>>") {
			t.Errorf("结束标记不应含 >>>: %q", got)
		}
	}
}

// === v2: .jvmrc 自动切换钩子 + 集成块版本化 ===

// TestPsScriptAutoHook 验证 PowerShell 集成脚本含自动切换钩子的关键构件。
func TestPsScriptAutoHook(t *testing.T) {
	s := psScript(`C:\fake\jvm.exe`)
	for _, want := range []string{
		integrationVersionToken,
		"function global:prompt", // 包装 prompt (每次提示符前触发)
		"$global:__jvm_last_dir", // 目录缓存: 目录没变不找 .jvmrc
		"$global:__jvm_last_rc",  // rc 缓存: rc 没变不拉起 exe
		"jvm use --auto",         // 走 wrapper 函数, 会话 env 自动刷新
		"Test-Path -LiteralPath", // 逐级向上找 .jvmrc
		"try {",                  // 容错: 钩子失败绝不破坏 prompt
	} {
		if !strings.Contains(s, want) {
			t.Errorf("psScript 缺少 %q", want)
		}
	}
}

// TestBashScriptAutoHook 验证 bash 集成脚本含自动切换钩子的关键构件。
func TestBashScriptAutoHook(t *testing.T) {
	s := bashScript(`C:\fake\jvm.exe`)
	for _, want := range []string{
		integrationVersionToken,
		"__jvm_autoswitch()",
		"PROMPT_COMMAND", // 挂提示符钩子
		"jvm use --auto",
		"${__jvm_last_dir:-}",       // 缓存 + set -u 安全
		"*\";__jvm_autoswitch;\"*)", // case 守卫防重复追加
	} {
		if !strings.Contains(s, want) {
			t.Errorf("bashScript 缺少 %q", want)
		}
	}
}

// TestIntegrationScriptsASCII 验证两个集成脚本纯 ASCII:
// PowerShell 5.1 在中文系统上按 GBK 解码非 ASCII 字节会损坏语法。
func TestIntegrationScriptsASCII(t *testing.T) {
	for name, s := range map[string]string{
		"powershell": psScript(`C:\fake\jvm.exe`),
		"bash":       bashScript(`C:\fake\jvm.exe`),
	} {
		for i := 0; i < len(s); i++ {
			if s[i] > 127 {
				t.Errorf("%s 脚本第 %d 字节非 ASCII: %q", name, i, s[i])
				break
			}
		}
	}
}

// TestEnsureBlockVersionedRewrites 验证版本化重写: 老格式集成块 (无 token)
// 被换成新块, 补全块与用户自有内容不动; 已是当前版本则原样保留。
func TestEnsureBlockVersionedRewrites(t *testing.T) {
	oldBlock := profileMarker + "\nfunction jvm { old version }\n" + endMarkerFor(profileMarker)
	completionBlock := completionMarker + "\nRegister-ArgumentCompleter { old }\n" + completionEndMarker

	t.Run("老块重写为新版", func(t *testing.T) {
		profile := filepath.Join(t.TempDir(), "profile.ps1")
		os.WriteFile(profile, []byte("user stuff\n"+oldBlock+"\n"+completionBlock+"\n"), 0o644)

		ensureBlockVersioned(profile, psScript(`C:\fake\jvm.exe`))

		data, _ := os.ReadFile(profile)
		got := string(data)
		if !strings.Contains(got, integrationVersionToken) {
			t.Error("重写后应含版本 token")
		}
		if strings.Contains(got, "old version") {
			t.Error("老块内容应被移除")
		}
		if !strings.Contains(got, completionMarker) || !strings.Contains(got, "Register-ArgumentCompleter") {
			t.Error("补全块不应被动到")
		}
		if !strings.Contains(got, "user stuff") {
			t.Error("用户自有内容不应被动到")
		}
		if strings.Count(got, profileMarker) != 1 {
			t.Errorf("集成块 marker 应恰好一个, got %d", strings.Count(got, profileMarker))
		}
	})

	t.Run("已是当前版本不重写", func(t *testing.T) {
		profile := filepath.Join(t.TempDir(), "profile.ps1")
		current := psScript(`C:\fake\jvm.exe`)
		os.WriteFile(profile, []byte("user stuff\n"+current+"\n"), 0o644)
		before, _ := os.ReadFile(profile)

		ensureBlockVersioned(profile, current)

		after, _ := os.ReadFile(profile)
		if string(before) != string(after) {
			t.Error("已是当前版本的块不应被重写")
		}
	})

	t.Run("空 profile 直接写入", func(t *testing.T) {
		profile := filepath.Join(t.TempDir(), "profile.ps1")
		ensureBlockVersioned(profile, psScript(`C:\fake\jvm.exe`))
		data, _ := os.ReadFile(profile)
		if !strings.Contains(string(data), integrationVersionToken) {
			t.Error("应写入集成块")
		}
	})
}
