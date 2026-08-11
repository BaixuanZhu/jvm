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
