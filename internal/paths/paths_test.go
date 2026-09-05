package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCalcRootRespectsJVM_HOME 验证 JVM_HOME 优先: 设了就直接当根目录。
func TestCalcRootRespectsJVM_HOME(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JVM_HOME", dir)
	if got := calcRoot(); got != dir {
		t.Errorf("calcRoot() = %q, want %q (JVM_HOME 应被优先采用)", got, dir)
	}
}

// TestCalcRootFallsBackToHome 验证 JVM_HOME 为空时回退 ~/.jvm。
func TestCalcRootFallsBackToHome(t *testing.T) {
	t.Setenv("JVM_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("无法获取用户主目录: %v", err)
	}
	want := filepath.Join(home, ".jvm")
	if got := calcRoot(); got != want {
		t.Errorf("calcRoot() = %q, want %q (空 JVM_HOME 应回退 ~/.jvm)", got, want)
	}
}

// withSavedDirs 保存并恢复数据面路径变量, 让 SetInstallDir 的测试互不污染。
func withSavedDirs(t *testing.T) {
	t.Helper()
	origRoot, origData, origVer := Root, dataRoot, VersionsDir
	t.Cleanup(func() {
		Root, dataRoot, VersionsDir = origRoot, origData, origVer
	})
}

// TestSetInstallDir 验证 install_dir 重定向: 只动数据面 (versions),
// 控制面 (Root/CurrentLink/AutoStateFile) 不变。
func TestSetInstallDir(t *testing.T) {
	withSavedDirs(t)
	root, cur, state := Root, CurrentLink, AutoStateFile

	newRoot := filepath.Join(t.TempDir(), "jdks")
	if err := SetInstallDir(newRoot); err != nil {
		t.Fatalf("SetInstallDir(%q) 报错: %v", newRoot, err)
	}
	if VersionsDir != filepath.Join(newRoot, "versions") {
		t.Errorf("VersionsDir = %q, want %q", VersionsDir, filepath.Join(newRoot, "versions"))
	}
	if Root != root || CurrentLink != cur || AutoStateFile != state {
		t.Errorf("控制面不应被改动: Root=%q (want %q), CurrentLink=%q (want %q)", Root, root, CurrentLink, cur)
	}
}

// TestSetInstallDirRelative 验证相对路径按进程 cwd 解析为绝对路径。
func TestSetInstallDirRelative(t *testing.T) {
	withSavedDirs(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := SetInstallDir("rel-jdks"); err != nil {
		t.Fatalf("SetInstallDir 相对路径报错: %v", err)
	}
	want := filepath.Join(wd, "rel-jdks", "versions")
	if VersionsDir != want {
		t.Errorf("VersionsDir = %q, want %q", VersionsDir, want)
	}
}

// TestSetInstallDirEmptyNoop 验证空串是 no-op (保持当前数据面)。
func TestSetInstallDirEmptyNoop(t *testing.T) {
	withSavedDirs(t)
	before := VersionsDir
	if err := SetInstallDir("   "); err != nil {
		t.Fatalf("空白串应 no-op, 报错: %v", err)
	}
	if VersionsDir != before {
		t.Errorf("空白串不应改动 VersionsDir: %q → %q", before, VersionsDir)
	}
}
