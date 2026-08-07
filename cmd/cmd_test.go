package cmd

import "testing"

// TestParseAvailableArgs 覆盖 jvm available 的 flag 解析。
// Available 的默认表格 / 分组输出依赖网络, 不在单测范围; 这里只测纯函数解析逻辑。
func TestParseAvailableArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    AvailableOptions
		wantErr bool
	}{
		// 空 args → 默认选项 (走表格输出)
		{"empty", nil, AvailableOptions{}, false},
		{"empty slice", []string{}, AvailableOptions{}, false},

		// -a / --all
		{"short -a", []string{"-a"}, AvailableOptions{All: true}, false},
		{"long --all", []string{"--all"}, AvailableOptions{All: true}, false},

		// -m <N> / --major <N> / --major=<N>
		{"short -m", []string{"-m", "17"}, AvailableOptions{Major: 17}, false},
		{"long --major", []string{"--major", "21"}, AvailableOptions{Major: 21}, false},
		{"--major= form", []string{"--major=8"}, AvailableOptions{Major: 8}, false},

		// 互斥: -a 与 -m 同给报错 (两种顺序)
		{"-a then -m", []string{"-a", "-m", "17"}, AvailableOptions{}, true},
		{"-m then -a", []string{"-m", "17", "-a"}, AvailableOptions{}, true},
		{"-a then --major=", []string{"-a", "--major=17"}, AvailableOptions{}, true},

		// -m / --major 缺参数
		{"-m no arg", []string{"-m"}, AvailableOptions{}, true},
		{"--major no arg", []string{"--major"}, AvailableOptions{}, true},

		// 非法 major
		{"-m non-numeric", []string{"-m", "abc"}, AvailableOptions{}, true},
		{"-m zero", []string{"-m", "0"}, AvailableOptions{}, true},
		{"-m negative", []string{"-m", "-1"}, AvailableOptions{}, true},
		{"--major= empty", []string{"--major="}, AvailableOptions{}, true},

		// 未识别 flag / 多余位置参数
		{"unknown flag", []string{"-x"}, AvailableOptions{}, true},
		{"unknown long flag", []string{"--foo"}, AvailableOptions{}, true},

		// 位置参数: 第一个当 distro (合法), 第二个多余 (报错)
		{"distro positional", []string{"corretto"}, AvailableOptions{Distro: "corretto"}, false},
		{"distro + flag", []string{"corretto", "-a"}, AvailableOptions{Distro: "corretto", All: true}, false},
		{"flag + distro", []string{"-a", "corretto"}, AvailableOptions{Distro: "corretto", All: true}, false},
		{"two positionals", []string{"corretto", "temurin"}, AvailableOptions{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAvailableArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseAvailableArgs(%v) 期望报错, 实际 nil", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAvailableArgs(%v) 未预期错误: %v", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("ParseAvailableArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}
