package cmd

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout 在 fn 执行期间捕获 os.Stdout (execWith 直通 os.Stdout, 测试用管道截获)
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}

// writeBat 在 dir 下写一个批处理文件并返回路径
func writeBat(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("@echo off\r\n"+content), 0o644); err != nil {
		t.Fatalf("写批处理失败: %v", err)
	}
}

func TestBuildExecEnv(t *testing.T) {
	versionDir := filepath.Join("C:", "jvm", "versions", "temurin-21.0.5+11")
	got := buildExecEnv(versionDir, []string{
		"PATH=C:\\Windows;C:\\Tools",
		"JAVA_HOME=C:\\Users\\x\\.jvm\\current",
		"FOO=bar",
	})
	want := []string{
		"PATH=" + versionDir + "\\bin;C:\\Windows;C:\\Tools",
		"JAVA_HOME=" + versionDir,
		"FOO=bar",
	}
	if len(got) != len(want) {
		t.Fatalf("环境条目数 = %d, 想 %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("条目 %d = %q, 想 %q", i, got[i], want[i])
		}
	}
}

func TestBuildExecEnvMissingEntries(t *testing.T) {
	// env 里没有 JAVA_HOME/PATH 时要补上 (极端构造, 保险分支)
	got := buildExecEnv(`D:\v`, []string{"FOO=bar"})
	joined := strings.Join(got, "\n")
	for _, want := range []string{"JAVA_HOME=D:\\v", "PATH=D:\\v\\bin", "FOO=bar"} {
		if !strings.Contains(joined, want) {
			t.Errorf("缺少条目 %q, 实际: %q", want, joined)
		}
	}
}

func TestBuildExecEnvCaseInsensitive(t *testing.T) {
	// Windows 环境变量名大小写不敏感, "Path=" 也要被覆盖
	got := buildExecEnv(`D:\v`, []string{"Path=C:\\Windows"})
	if got[0] != "PATH=D:\\v\\bin;C:\\Windows" {
		t.Errorf("Path 覆盖结果 = %q", got[0])
	}
}

func TestExecWithRunsCommand(t *testing.T) {
	dir := t.TempDir()
	out := captureStdout(t, func() {
		if err := execWith(dir, []string{"cmd.exe", "/c", "echo", "hello-exec"}); err != nil {
			t.Errorf("execWith: %v", err)
		}
	})
	if !strings.Contains(out, "hello-exec") {
		t.Errorf("输出 = %q, 想包含 hello-exec", out)
	}
}

func TestExecWithInjectsJavaHome(t *testing.T) {
	dir := t.TempDir()
	bat := filepath.Join(dir, "showhome.bat")
	writeBat(t, bat, "echo %JAVA_HOME%")
	out := captureStdout(t, func() {
		// 显式路径形式, 不走 bin 内查找
		if err := execWith(dir, []string{bat}); err != nil {
			t.Errorf("execWith: %v", err)
		}
	})
	if got := strings.TrimSpace(out); !strings.EqualFold(got, dir) {
		t.Errorf("JAVA_HOME = %q, 想 %q", got, dir)
	}
}

func TestExecWithExitCode(t *testing.T) {
	dir := t.TempDir()
	err := execWith(dir, []string{"cmd.exe", "/c", "exit", "3"})
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("想 *exec.ExitError, 实际: %v", err)
	}
	if ee.ExitCode() != 3 {
		t.Errorf("退出码 = %d, 想 3", ee.ExitCode())
	}
}

func TestExecWithBatchDispatch(t *testing.T) {
	// mvn/gradlew 场景: 批处理放在版本 bin 里, 用裸名调用
	// 验证 lookPathIn 在 bin 内命中 .bat 且经 cmd.exe /c 分发
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBat(t, filepath.Join(dir, "bin", "fakebuild.bat"), "echo from-batch\r\nexit /b 5")

	out := captureStdout(t, func() {
		err := execWith(dir, []string{"fakebuild"})
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 5 {
			t.Errorf("想退出码 5 的 ExitError, 实际: %v", err)
		}
	})
	if !strings.Contains(out, "from-batch") {
		t.Errorf("输出 = %q, 想包含 from-batch", out)
	}
}

func TestExecWithCommandNotFound(t *testing.T) {
	dir := t.TempDir()
	err := execWith(dir, []string{"definitely-not-a-command-xyz"})
	if err == nil || !strings.Contains(err.Error(), "未找到命令") {
		t.Errorf("想\"未找到命令\"错误, 实际: %v", err)
	}
}

func TestLookPathInPrefersVersionBin(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBat(t, filepath.Join(bin, "mytool.bat"), "echo x")

	got, err := lookPathIn(bin, "mytool")
	if err != nil {
		t.Fatalf("lookPathIn: %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(bin, "mytool.bat")) {
		t.Errorf("解析到 %q, 想 %q", got, filepath.Join(bin, "mytool.bat"))
	}
}

func TestLookPathInFallsBackToSystemPath(t *testing.T) {
	// cmd.exe 不在版本 bin 里, 应回退系统 PATH
	got, err := lookPathIn(t.TempDir(), "cmd.exe")
	if err != nil {
		t.Fatalf("lookPathIn: %v", err)
	}
	if !strings.EqualFold(filepath.Base(got), "cmd.exe") {
		t.Errorf("解析到 %q", got)
	}
}
