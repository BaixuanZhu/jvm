package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"jvm/internal/junction"
)

// === checkDirs ===

func TestCheckDirs(t *testing.T) {
	t.Run("两个目录都存在", func(t *testing.T) {
		root := t.TempDir()
		versions := filepath.Join(root, "versions")
		os.MkdirAll(versions, 0o755)
		c := checkDirs(root, versions)
		if !c.ok {
			t.Errorf("期望通过, got detail=%q", c.detail)
		}
	})
	t.Run("root 不存在", func(t *testing.T) {
		c := checkDirs(filepath.Join(t.TempDir(), "missing"), "whatever")
		if c.ok {
			t.Error("期望失败 (root 不存在)")
		}
		if !strings.Contains(c.detail, "不存在") {
			t.Errorf("detail 应提示不存在, got %q", c.detail)
		}
	})
	t.Run("versions 不存在", func(t *testing.T) {
		root := t.TempDir()
		c := checkDirs(root, filepath.Join(root, "versions"))
		if c.ok {
			t.Error("期望失败 (versions 不存在)")
		}
		if !strings.Contains(c.detail, "版本目录") {
			t.Errorf("detail 应提示版本目录, got %q", c.detail)
		}
	})
	t.Run("root 是文件不是目录", func(t *testing.T) {
		root := t.TempDir()
		filePath := filepath.Join(root, "notdir")
		os.WriteFile(filePath, []byte("x"), 0o644)
		c := checkDirs(filePath, "whatever")
		if c.ok {
			t.Error("期望失败 (root 是文件)")
		}
	})
}

// === checkJunction ===
//
// v0.4.1 修复: 之前用 os.Lstat 的 ModeSymlink 位判断"是否是链接", 但 Windows
// junction (reparse point) 在 Go 里不设置 ModeSymlink (那是给真 symlink 的),
// 导致 doctor 对所有 junction 误报"不是链接"。现在改用 os.Readlink 成功与否判断。
// 下面的测试覆盖各分支, 包括用真实 Windows junction 验证 (junction.Create 走原生
// syscall, 不需要管理员/开发者模式, 故可稳定复现)。

func TestCheckJunction(t *testing.T) {
	t.Run("链接不存在", func(t *testing.T) {
		c := checkJunction(filepath.Join(t.TempDir(), "nope"))
		if c.ok {
			t.Error("期望失败 (链接不存在)")
		}
		if !strings.Contains(c.detail, "尚未选定") {
			t.Errorf("detail 应提示未选定, got %q", c.detail)
		}
	})
	t.Run("普通目录 (非链接)", func(t *testing.T) {
		dir := t.TempDir()
		c := checkJunction(dir)
		if c.ok {
			t.Error("期望失败 (普通目录不是链接)")
		}
		if !strings.Contains(c.detail, "不是链接") {
			t.Errorf("detail 应提示不是链接, got %q", c.detail)
		}
	})
	t.Run("有效符号链接指向真实目录", func(t *testing.T) {
		// 真 symlink 需要管理员或开发者模式; 跳过以避免在受限环境失败。
		if skipSymlinkTest(t) {
			return
		}
		target := t.TempDir()
		link := filepath.Join(t.TempDir(), "current")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("无法创建符号链接 (可能未开启开发者模式): %v", err)
		}
		c := checkJunction(link)
		if !c.ok {
			t.Errorf("期望通过, got detail=%q", c.detail)
		}
	})
	t.Run("真实 Windows junction 指向真实目录", func(t *testing.T) {
		// junction (而非 symlink) 是 jvm 实际用的链接形式, 也是 v0.4.1 修复的回归点:
		// 之前 checkJunction 对所有 junction 都误报。junction.Create 走原生 syscall,
		// 不需要管理员/开发者模式, 可在普通用户态稳定运行。
		//
		// 注意: 用 ASCII 前缀的 os.MkdirTemp 而非 t.TempDir —— Go 会把子测试名
		// (含中文) 拼进 t.TempDir 路径, 而 Windows junction 的 reparse point 解析
		// 对含非 ANSI 字符的路径有兼容问题, 会污染测试。真实 ~/.jvm 路径无此问题。
		c := testJunctionCase(t, false)
		if !c.ok {
			t.Errorf("期望通过 (junction 是有效链接), got detail=%q", c.detail)
		}
	})
	t.Run("悬空 junction (目标已删)", func(t *testing.T) {
		c := testJunctionCase(t, true)
		if c.ok {
			t.Error("期望失败 (悬空 junction)")
		}
		if !strings.Contains(c.detail, "目标不存在") {
			t.Errorf("detail 应提示目标不存在, got %q", c.detail)
		}
	})
}

// testJunctionCase 建一个真实 Windows junction 测 checkJunction。
// dangle=true 时建好后删掉 target 让链接悬空。返回 checkJunction 的结果。
func testJunctionCase(t *testing.T, dangle bool) check {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("junction 测试仅 Windows")
	}
	target, err := os.MkdirTemp("", "jvm-tgt-*")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.MkdirTemp("", "jvm-lnk-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(target)
		os.RemoveAll(parent)
	})
	link := filepath.Join(parent, "current")
	if err := junction.Create(link, target); err != nil {
		t.Skipf("无法创建 junction: %v", err)
	}
	if dangle {
		os.RemoveAll(target)
	}
	return checkJunction(link)
}

// === checkJavaHome ===

func TestCheckJavaHome(t *testing.T) {
	const current = `C:\Users\test\.jvm\current`
	tests := []struct {
		name     string
		javaHome string
		wantOK   bool
	}{
		{"空值", "", false},
		{"只有空格", "   ", false},
		{"完全匹配", current, true},
		{"大小写不同但路径相同", strings.ToLower(current), true},
		{"路径分隔符差异", `C:/Users/test/.jvm/current`, true},
		{"指向别处", `C:\Program Files\Java\jdk-17`, false},
		{"是 current 的子目录", current + `\bin`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := checkJavaHome(tt.javaHome, current)
			if c.ok != tt.wantOK {
				t.Errorf("checkJavaHome(%q) ok=%v, want %v (detail=%q)",
					tt.javaHome, c.ok, tt.wantOK, c.detail)
			}
		})
	}
}

// === checkPathConflict ===

func TestCheckPathConflict(t *testing.T) {
	// 准备一个含 java.exe 的目录模拟"抢先的 java"
	conflictDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(conflictDir, "java.exe"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	emptyDir := t.TempDir()

	// current/bin 用一个不存在的占位路径 (函数只比较路径, 不要求 current 真实存在)
	const currentLink = `C:\fake\.jvm\current`
	binPath := filepath.Join(currentLink, "bin")
	sep := string(os.PathListSeparator)

	tests := []struct {
		name    string
		pathEnv string
		wantOK  bool
	}{
		{"PATH 为空", "", true},
		{"current/bin 在最前", binPath + sep + conflictDir, true},
		{"current/bin 在中间", emptyDir + sep + binPath + sep + conflictDir, true},
		{"current/bin 在最后", emptyDir + sep + binPath, true},
		{"无 current/bin 但也无抢先 java", emptyDir + sep + emptyDir, true},
		{"有抢先的 java 在 current 之前", conflictDir + sep + binPath, false},
		{"抢先 java 单独存在 (无 current)", conflictDir, false},
		{"空条目被忽略", sep + sep + emptyDir + sep, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := checkPathConflict(tt.pathEnv, currentLink)
			if c.ok != tt.wantOK {
				t.Errorf("checkPathConflict ok=%v, want %v (detail=%q)",
					c.ok, tt.wantOK, c.detail)
			}
		})
	}
}

// === checkShellIntegration ===

func TestCheckShellIntegration(t *testing.T) {
	// 用 shell.profileMarker 写一个"已集成"的假 profile
	const marker = "# >>> jvm shell init >>>"
	integratedProfile := filepath.Join(t.TempDir(), "integrated.ps1")
	os.WriteFile(integratedProfile, []byte(marker+"\n# some script\n"), 0o644)
	plainProfile := filepath.Join(t.TempDir(), "plain.ps1")
	os.WriteFile(plainProfile, []byte("Write-Host hello\n"), 0o644)
	missingProfile := filepath.Join(t.TempDir(), "nope.ps1") // 不存在

	t.Run("全部已集成", func(t *testing.T) {
		c := checkShellIntegration([]profileItem{
			{"ps5", integratedProfile},
			{"bash", integratedProfile},
		})
		if !c.ok {
			t.Errorf("期望通过, got detail=%q", c.detail)
		}
	})
	t.Run("部分缺失", func(t *testing.T) {
		c := checkShellIntegration([]profileItem{
			{"ps5", integratedProfile},
			{"ps7", missingProfile},
			{"bash", plainProfile},
		})
		if c.ok {
			t.Error("期望失败 (部分未集成)")
		}
		// 应列出缺失的: ps7 和 bash
		if !strings.Contains(c.detail, "ps7") {
			t.Errorf("detail 应含 ps7, got %q", c.detail)
		}
		if !strings.Contains(c.detail, "bash") {
			t.Errorf("detail 应含 bash, got %q", c.detail)
		}
		// 不应误报已集成的 ps5
		if strings.Contains(c.detail, "ps5") {
			t.Errorf("detail 不应含已集成的 ps5, got %q", c.detail)
		}
	})
	t.Run("空列表 (视为全部通过)", func(t *testing.T) {
		c := checkShellIntegration(nil)
		if !c.ok {
			t.Error("空列表应通过")
		}
	})
}

// === checkCurrentJava ===

func TestCheckCurrentJava(t *testing.T) {
	t.Run("java.exe 存在", func(t *testing.T) {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, "bin"), 0o755)
		os.WriteFile(filepath.Join(root, "bin", "java.exe"), []byte("x"), 0o644)
		c := checkCurrentJava(root)
		if !c.ok {
			t.Errorf("期望通过, got detail=%q", c.detail)
		}
	})
	t.Run("java.exe 不存在", func(t *testing.T) {
		c := checkCurrentJava(t.TempDir())
		if c.ok {
			t.Error("期望失败 (无 java.exe)")
		}
	})
}

// skipSymlinkTest 在需要符号链接权限的环境 (Windows 非开发者模式) 跳过。
// 返回 true 表示应跳过。
func skipSymlinkTest(t *testing.T) bool {
	t.Helper()
	// Windows 上创建符号链接需管理员或开发者模式; 非 Windows 通常无限制。
	if runtime.GOOS != "windows" {
		return false
	}
	// 尝试创建一个临时符号链接探测权限; 失败则标记跳过。
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.MkdirAll(target, 0o755)
	link := filepath.Join(dir, "probe")
	if err := os.Symlink(target, link); err != nil {
		return true
	}
	os.Remove(link)
	return false
}
