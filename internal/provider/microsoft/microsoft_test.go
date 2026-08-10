package microsoft

import (
	"errors"
	"fmt"
	"testing"
)

// TestExtractVersionFromFilename 验证从 aka.ms 最终 URL 路径解析版本号。
// 路径形如 microsoft-jdk-21.0.12-windows-x64.zip (可能带 CDN 前缀路径),
// 后缀按目标架构 (-windows-x64 / -windows-aarch64) 剥离。
func TestExtractVersionFromFilename(t *testing.T) {
	tests := []struct {
		name string
		path string
		arch string
		want string
	}{
		{"纯文件名", "microsoft-jdk-21.0.12-windows-x64.zip", "x64", "21.0.12"},
		{"带 CDN 前缀路径", "/download/pr/abc/def/microsoft-jdk-21.0.12-windows-x64.zip", "x64", "21.0.12"},
		{"JDK 11", "microsoft-jdk-11.0.32-windows-x64.zip", "x64", "11.0.32"},
		{"JDK 17", "microsoft-jdk-17.0.20-windows-x64.zip", "x64", "17.0.20"},
		{"JDK 25", "microsoft-jdk-25.0.4-windows-x64.zip", "x64", "25.0.4"},
		// aarch64 架构
		{"aarch64 纯文件名", "microsoft-jdk-21.0.12-windows-aarch64.zip", "aarch64", "21.0.12"},
		{"aarch64 带 CDN 前缀", "/download/pr/abc/def/microsoft-jdk-17.0.20-windows-aarch64.zip", "aarch64", "17.0.20"},
		{"aarch64 JDK 11", "microsoft-jdk-11.0.32-windows-aarch64.zip", "aarch64", "11.0.32"},
		// 边界 / 非法
		{"无前缀", "21.0.12-windows-x64.zip", "x64", ""},
		{"空串", "", "x64", ""},
		{"只有前缀", "microsoft-jdk-", "x64", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractVersionFromFilename(tt.path, tt.arch); got != tt.want {
				t.Errorf("extractVersionFromFilename(%q, %q) = %q, want %q", tt.path, tt.arch, got, tt.want)
			}
		})
	}
}

// TestConfigure 验证 Configure 设置目标架构 (含别名与非法值回退),
// 并在结束后恢复包级状态, 不污染其他测试。
func TestConfigure(t *testing.T) {
	origArch := arch
	t.Cleanup(func() { arch = origArch })

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"合法 x64", "x64", "x64"},
		{"合法 aarch64", "aarch64", "aarch64"},
		{"别名 amd64", "amd64", "x64"},
		{"别名 arm64", "arm64", "aarch64"},
		{"带空格", "  aarch64  ", "aarch64"},
		{"非法回退 x64", "riscv", "x64"},
		{"空值保持默认", "", "x64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arch = "x64" // 每个子用例从已知状态开始
			microsoft{}.Configure(tt.in, "https://ignored.mirror")
			if arch != tt.want {
				t.Errorf("Configure(%q) 后 arch = %q, want %q", tt.in, arch, tt.want)
			}
		})
	}
}

// TestIsSupportedLTS 验证 LTS 版本判定。
func TestIsSupportedLTS(t *testing.T) {
	supported := []int{11, 17, 21, 25}
	notSupported := []int{8, 9, 10, 12, 16, 18, 22, 23, 24, 26}
	for _, v := range supported {
		if !isSupportedLTS(v) {
			t.Errorf("isSupportedLTS(%d) = false, want true", v)
		}
	}
	for _, v := range notSupported {
		if isSupportedLTS(v) {
			t.Errorf("isSupportedLTS(%d) = true, want false", v)
		}
	}
}

// TestNameAndDisplayName 验证核心标识方法。
func TestNameAndDisplayName(t *testing.T) {
	p := microsoft{}
	if p.Name() != "microsoft" {
		t.Errorf("Name() = %q, want %q", p.Name(), "microsoft")
	}
	if p.DisplayName() != "Microsoft Build of OpenJDK" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "Microsoft Build of OpenJDK")
	}
}

// TestAvailable 验证写死的 LTS 列表 (Microsoft 无可用版本 API)。
func TestAvailable(t *testing.T) {
	p := microsoft{}
	releases, err := p.Available()
	if err != nil {
		t.Fatalf("Available() 意外报错: %v", err)
	}
	if len(releases) != len(ltsReleases) {
		t.Fatalf("Available() 返回 %d 个, want %d", len(releases), len(ltsReleases))
	}
	for _, r := range releases {
		if !r.LTS {
			t.Errorf("大版本 %d 应为 LTS", r.Major)
		}
	}
}

// TestErrVersionNotFoundIsComparable 验证哨兵错误能被 errors.Is 识别。
// 回归保护: resolveRedirect 里 http.Client 会把 CheckRedirect 返回的错误
// 包进 *url.Error, 必须用 errors.Is 而非 == 比较 (曾因此导致 bing.com
// 跳转的错误信息泄漏内部 URL)。
func TestErrVersionNotFoundIsComparable(t *testing.T) {
	// 哨兵自身
	if !errors.Is(errVersionNotFound, errVersionNotFound) {
		t.Error("errors.Is(errVersionNotFound, errVersionNotFound) 应为 true")
	}
	// 被 fmt.Errorf %w 包装后仍可识别 (模拟生产代码的包装路径)
	wrapped := fmt.Errorf("包装: %w", errVersionNotFound)
	if !errors.Is(wrapped, errVersionNotFound) {
		t.Error("被 %w 包装后 errors.Is 仍应识别哨兵错误")
	}
}
