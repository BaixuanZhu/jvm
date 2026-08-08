package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallToProfileIdempotent 验证同一脚本多次写入只保留一个块。
// 这是 removeBlock 循环移除 + endMarkerFor ReplaceAll 的回归保证。
func TestInstallToProfileIdempotent(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.ps1")
	const marker = "# >>> test >>>"
	script := marker + "\nbody\n# <<< test <<<"

	// 写入 3 次, 应只保留 1 个块
	for i := 0; i < 3; i++ {
		if err := installToProfile(profile, script); err != nil {
			t.Fatalf("第 %d 次写入失败: %v", i+1, err)
		}
	}

	data, _ := os.ReadFile(profile)
	content := string(data)
	count := strings.Count(content, marker)
	if count != 1 {
		t.Errorf("幂等写入 3 次后应有 1 个块, 实际 %d 个", count)
	}
}

// TestInstallToProfileRemovesDuplicates 验证 profile 里已有多个同标记块时全部移除。
// 防止历史遗留 (手动 install + EnsureIntegration 各写一遍) 越积越多。
func TestInstallToProfileRemovesDuplicates(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.ps1")
	const marker = "# >>> test >>>"
	const endMarker = "# <<< test <<<"
	script := marker + "\nbody\n" + endMarker

	// 预置 3 个重复块 (模拟历史遗留)
	messy := "header\n" + script + "\n" + script + "\n" + script + "\nfooter\n"
	if err := os.WriteFile(profile, []byte(messy), 0o644); err != nil {
		t.Fatal(err)
	}

	// 再写一次, 应清掉所有旧块只留 1 个
	if err := installToProfile(profile, script); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	data, _ := os.ReadFile(profile)
	content := string(data)
	if count := strings.Count(content, marker); count != 1 {
		t.Errorf("应清理重复块只留 1 个, 实际 %d 个", count)
	}
	if !strings.Contains(content, "header") || !strings.Contains(content, "footer") {
		t.Errorf("清理重复块时误删了其他内容: %q", content)
	}
}

// TestInstallToProfileBlockIsolation 验证不同标记的块互不干扰。
// shell 集成块和补全块各自独立升级, 写补全块时不会误删 shell 集成块。
func TestInstallToProfileBlockIsolation(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.ps1")

	shellScript := "# >>> shell >>>\nfunction jvm { ... }\n# <<< shell <<<"
	completionScript := "# >>> completion >>>\nRegister-ArgumentCompleter\n# <<< completion <<<"

	// 先写 shell 集成块
	if err := installToProfile(profile, shellScript); err != nil {
		t.Fatal(err)
	}
	// 再写补全块
	if err := installToProfile(profile, completionScript); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(profile)
	content := string(data)

	// 两个块都应存在
	if !strings.Contains(content, "# >>> shell >>>") {
		t.Error("写补全块后 shell 集成块消失了")
	}
	if !strings.Contains(content, "# >>> completion >>>") {
		t.Error("补全块未写入")
	}

	// 再写一次补全块 (模拟升级), shell 集成块应保留, 补全块不重复
	if err := installToProfile(profile, completionScript); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(profile)
	content = string(data)
	if strings.Count(content, "# >>> completion >>>") != 1 {
		t.Error("补全块升级后应仍为 1 个")
	}
	if !strings.Contains(content, "# >>> shell >>>") {
		t.Error("补全块升级时误删了 shell 集成块")
	}
}

// TestInstallToProfileCreatesDir 验证目标目录不存在时自动创建。
func TestInstallToProfileCreatesDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "deep", "profile.ps1")
	script := "# >>> test >>>\nbody\n# <<< test <<<"

	if err := installToProfile(nested, script); err != nil {
		t.Fatalf("写入嵌套路径失败: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("文件未创建: %v", err)
	}
}

// TestInstallToProfileKeepsOtherContent 验证写入块时保留 profile 里的其他用户内容。
func TestInstallToProfileKeepsOtherContent(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.ps1")

	// 预置用户自定义内容
	existing := "Set-PSReadLineOption -PredictionMode History\n$alias = 'll'\n"
	if err := os.WriteFile(profile, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	script := "# >>> test >>>\nbody\n# <<< test <<<"
	if err := installToProfile(profile, script); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(profile)
	content := string(data)
	if !strings.Contains(content, "Set-PSReadLineOption") {
		t.Error("用户自定义内容被误删")
	}
	if !strings.Contains(content, "# >>> test >>>") {
		t.Error("脚本块未写入")
	}
}

// TestProfileScriptsAreASCII 验证写入 profile 的脚本内容全是 ASCII。
// 这是避免 PowerShell 5.1 中文系统 GBK 解码问题的根本保证 —— 脚本内容
// 不含非 ASCII 字节, 就不需要 BOM 适配。用 Unicode 检测工具扫描。
func TestProfileScriptsAreASCII(t *testing.T) {
	scripts := []struct {
		name    string
		content string
	}{
		{"psScript", psScript("C:/jvm/jvm.exe")},
		{"bashScript", bashScript("C:/jvm/jvm.exe")},
		{"psCompletionScript", psCompletionScript([]string{"temurin", "corretto", "microsoft"})},
		{"bashCompletionScript", bashCompletionScript([]string{"temurin", "corretto", "microsoft"})},
	}
	for _, s := range scripts {
		t.Run(s.name, func(t *testing.T) {
			for i, r := range s.content {
				if r > 127 {
					t.Errorf("%s 含非 ASCII 字符 %q (码点 U+%04X) 在位置 %d;\n"+
						"profile 脚本必须保持纯 ASCII 以避免 PowerShell 5.1 中文系统编码问题",
						s.name, string(r), r, i)
				}
			}
		})
	}
}
