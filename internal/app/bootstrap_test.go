package app

import (
	"syscall"
	"testing"
)

// TestIsUnderTemp 表驱动验证 Temp 路径判断 (输入均为规整形式,
// 长短名展开由 TestNormalizePathExpandsShortName 覆盖)。
func TestIsUnderTemp(t *testing.T) {
	temp := `C:\Users\tester\AppData\Local\Temp`
	tests := []struct {
		name string
		exe  string
		temp string
		want bool
	}{
		{"go run 临时二进制", `C:\Users\tester\AppData\Local\Temp\go-build123\b001\exe\jvm.exe`, temp, true},
		{"Temp 根下的 exe", `C:\Users\tester\AppData\Local\Temp\jvm.exe`, temp, true},
		{"exe 即 Temp 目录本身 (边界)", temp, temp, true},
		{"大小写不敏感", `c:\users\tester\appdata\local\temp\jvm.exe`, temp, true},
		{"正斜杠归一化", `C:/Users/tester/AppData/Local/Temp/jvm.exe`, temp, true},
		{"temp 参数带尾部分隔符", `C:\Users\tester\AppData\Local\Temp\jvm.exe`, temp + `\`, true},
		{"安装目录不受影响", `D:\Program Files\jvm\jvm.exe`, temp, false},
		{"LocalAppData 程序目录不受影响", `C:\Users\tester\AppData\Local\Programs\jvm\jvm.exe`, temp, false},
		{"前缀陷阱: Temp2 不是 Temp 的子目录", `C:\Users\tester\AppData\Local\Temp2\jvm.exe`, temp, false},
		{"开发目录不受影响", `D:\code\jvm\dist\amd64\jvm.exe`, temp, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnderTemp(tt.exe, tt.temp); got != tt.want {
				t.Errorf("isUnderTemp(%q, %q) = %v, want %v", tt.exe, tt.temp, got, tt.want)
			}
		})
	}
}

// TestNormalizePathExpandsShortName 用真实存在的路径验证 8.3 短名展开:
// t.TempDir() 一定存在, 其短名形式经 normalizePath 展开后应与长名归一化一致。
func TestNormalizePathExpandsShortName(t *testing.T) {
	dir := t.TempDir() // 真实存在, GetLongPathName 才能展开
	short, err := shortPathName(dir)
	if err != nil || short == dir {
		t.Skipf("无法生成短名形式 (系统可能未启用 8.3 短名): %v", err)
	}
	gotShort, gotLong := normalizePath(short), normalizePath(dir)
	if gotShort != gotLong {
		t.Errorf("短名 %q 与长名 %q 归一化后应一致, got %q vs %q", short, dir, gotShort, gotLong)
	}
}

// TestBootstrapEnabled 表驱动验证三层判定: env 开关 > ldflags var > Temp 守卫。
func TestBootstrapEnabled(t *testing.T) {
	orig := Bootstrap
	t.Cleanup(func() { Bootstrap = orig })

	temp := `C:\Users\tester\AppData\Local\Temp`
	tests := []struct {
		name      string
		bootstrap string // ldflags 注入值
		noBootEnv string // JVM_NO_BOOTSTRAP
		exe       string // 自身 exe 路径
		want      bool
	}{
		{"发行构建 + 安装目录", "on", "", `D:\Program Files\jvm\jvm.exe`, true},
		{"发行构建 + 开发目录 (go build 源码)", "on", "", `D:\code\jvm\jvm.exe`, true},
		{"开发构建 (ldflags off)", "off", "", `D:\code\jvm\dist\amd64\jvm.exe`, false},
		{"env 显式关闭", "on", "1", `D:\Program Files\jvm\jvm.exe`, false},
		{"env 空白视为未设置", "on", "   ", `D:\Program Files\jvm\jvm.exe`, true},
		{"go run 临时二进制被守卫拦截", "on", "", temp + `\go-build123\b001\exe\jvm.exe`, false},
		{"exe 定位失败不阻断", "on", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Bootstrap = tt.bootstrap
			if got := bootstrapEnabled(tt.exe, tt.noBootEnv, temp); got != tt.want {
				t.Errorf("bootstrapEnabled(%q, %q, temp) = %v, want %v (Bootstrap=%q)",
					tt.exe, tt.noBootEnv, got, tt.want, tt.bootstrap)
			}
		})
	}
}

// shortPathName 取路径的 8.3 短名形式 (测试辅助)。
func shortPathName(p string) (string, error) {
	p16, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, 4096)
	n, err := syscall.GetShortPathName(p16, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf[:n]), nil
}
