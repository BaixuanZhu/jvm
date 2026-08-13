//go:build windows

// 此文件覆盖 junction.Create/Remove 的真实 syscall 路径 (Windows reparse point)。
// 纯函数测试 (SplitDistro / MajorOf / semverLess 等) 在 junction_test.go, 跨平台;
// 此处只测 Windows 耦合的 Create/Remove, 故加 //go:build windows 约束,
// 非 Windows 平台 (如 CI 的 lint) 跳过编译。

package junction

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCreateRemoveRoundTrip 验证 Create → junction 有效 → Remove 往返。
// Create 的真实 syscall 路径 (FSCTL_SET_REPARSE_POINT) 此前只被集成测试间接覆盖,
// 是 1.0 稳定性的明显短板 (doctor.go 的 junction 判断也依赖它工作正确)。
func TestCreateRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "jdk-21")
	if err := os.MkdirAll(filepath.Join(target, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "current")

	// Create: link (不存在) → target (已存在目录)
	if err := Create(link, target); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	// junction 有效: os.Readlink 能解析 (doctor.go 也用此判断 junction 是否成立)
	if _, err := os.Readlink(link); err != nil {
		t.Errorf("Create 后 link 不是有效 junction (Readlink 失败): %v", err)
	}
	// 通过 link 能穿透访问 target 的内容 (junction 的核心功能)
	if _, err := os.Stat(filepath.Join(link, "bin")); err != nil {
		t.Errorf("通过 junction 访问 target 内容失败: %v", err)
	}

	// Remove: 只删 link 本身, target 目录不受影响
	if err := Remove(link); err != nil {
		t.Fatalf("Remove 失败: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("Remove 后 link 仍存在")
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("Remove 误删了 target 目录: %v", err)
	}
}

// TestCreateErrors 验证 Create 的前置校验: link 已存在 / target 不存在 都应报错,
// 不能静默建出错误状态或覆盖已有路径。
func TestCreateErrors(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	// link 路径已存在 → 必须报错 (绝不覆盖已有路径)
	existLink := filepath.Join(dir, "exists")
	if err := os.MkdirAll(existLink, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(existLink, target); err == nil {
		t.Error("link 已存在时 Create 应报错, 实际返回 nil")
	}

	// target 不存在 → 必须报错 (junction 指向虚无无意义)
	missingTarget := filepath.Join(dir, "missing")
	link2 := filepath.Join(dir, "link2")
	if err := Create(link2, missingTarget); err == nil {
		t.Error("target 不存在时 Create 应报错, 实际返回 nil")
		_ = Remove(link2) // 清理可能误建的空目录, 避免污染
	}
}
