package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jvm/internal/app"
	"jvm/internal/junction"
	"jvm/internal/paths"
	"jvm/internal/provider"
)

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

		// -r / --refresh (可与 -a/-m/distro 组合)
		{"short -r", []string{"-r"}, AvailableOptions{Refresh: true}, false},
		{"long --refresh", []string{"--refresh"}, AvailableOptions{Refresh: true}, false},
		{"-r + -a", []string{"-r", "-a"}, AvailableOptions{All: true, Refresh: true}, false},
		{"-r + -m", []string{"-m", "17", "--refresh"}, AvailableOptions{Major: 17, Refresh: true}, false},
		{"-r + distro", []string{"corretto", "-r"}, AvailableOptions{Distro: "corretto", Refresh: true}, false},

		// --json / -j (可与全部其他选项组合)
		{"long --json", []string{"--json"}, AvailableOptions{JSON: true}, false},
		{"short -j", []string{"-j"}, AvailableOptions{JSON: true}, false},
		{"--json + -a + distro", []string{"corretto", "-a", "--json"}, AvailableOptions{Distro: "corretto", All: true, JSON: true}, false},
		{"--json + -r + -m", []string{"-j", "-r", "-m", "17"}, AvailableOptions{Major: 17, Refresh: true, JSON: true}, false},

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

// fakeProvider 注入 mock 数据, 供 availableTable/availableGroups 的并发测试。
// 嵌入 provider.Base 拿 ShortSemver/ResolveReleaseName 默认实现, 只 override 查询方法。
// 注册到全局 registry (跟随 shell/completion_test 的既有模式), 用唯一名避免冲突。
type fakeProvider struct {
	provider.Base
	name string
}

func (f fakeProvider) Name() string      { return f.name }
func (fakeProvider) DisplayName() string { return "Fake" }
func (fakeProvider) Available() ([]app.Release, error) {
	// 多个大版本 → 触发 availableTable/Groups 的多 goroutine 并发
	return []app.Release{{Major: 17, LTS: true}, {Major: 21, LTS: true}, {Major: 25, LTS: false}}, nil
}
func (fakeProvider) LatestPatch(major int) (*app.Asset, error) {
	return &app.Asset{ReleaseName: fmt.Sprintf("%d.0.1+1", major), Major: major}, nil
}
func (fakeProvider) Resolve(app.VersionSpec) (*app.Asset, error) { return nil, nil }
func (fakeProvider) ListVersions(major int) ([]*app.Asset, error) {
	return []*app.Asset{
		{ReleaseName: fmt.Sprintf("%d.0.1+1", major)},
		{ReleaseName: fmt.Sprintf("%d.0.0+1", major)},
	}, nil
}

// TestAvailableTableConcurrent 验证 availableTable 的并发 (sync.WaitGroup + goroutine
// 各写 rows[i]) 在 -race 下无数据竞争。此前这段并发逻辑零覆盖 (依赖真实 provider 网络,
// 无法单测); 注入 fake provider 提供 mock 数据后可离线跑。
// paths.Root 换到临时目录, availableTable 的缓存读写不落真实 ~/.jvm。
func TestAvailableTableConcurrent(t *testing.T) {
	withTempVersions(t)
	name := "fake-cmd-table-" + t.Name()
	provider.Register(fakeProvider{name: name})
	availableTable(AvailableOptions{}, name) // 并发查 LatestPatch; -race 下无竞争报告即通过
}

// TestAvailableGroupsConcurrent 验证 availableGroups 的并发 (goroutine 各写 groups[i])。
func TestAvailableGroupsConcurrent(t *testing.T) {
	withTempVersions(t)
	name := "fake-cmd-groups-" + t.Name()
	provider.Register(fakeProvider{name: name})
	availableGroups(AvailableOptions{All: true}, name)
}

// TestHome 验证 jvm home 输出 current 链接路径 (临时目录 + 真实 junction,
// 与 junction 包单测同款手法; 未选版本的 Fail 路径含 os.Exit, 不可单测)。
func TestHome(t *testing.T) {
	root := withTempVersions(t)
	target := filepath.Join(root, "versions", "temurin-21.0.8+7")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := junction.Create(paths.CurrentLink, target); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, Home)
	if got := strings.TrimSpace(out); got != paths.CurrentLink {
		t.Errorf("home 输出 = %q, 想 %q", got, paths.CurrentLink)
	}
}

func TestParseJSONFlag(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    bool
		wantErr bool
	}{
		{"无参", nil, false, false},
		{"--json", []string{"--json"}, true, false},
		{"短名 -j", []string{"-j"}, true, false},
		{"重复不报错", []string{"--json", "-j"}, true, false},
		{"未知参数", []string{"--foo"}, false, true},
		{"位置参数", []string{"21"}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJSONFlag(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseJSONFlag(%v) 期望报错", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseJSONFlag(%v) 未预期错误: %v", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("parseJSONFlag(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// TestListJSON 验证 jvm list --json: 字段拆分 (distro/version/major/dir)、
// current 标记、旧无前缀目录归 temurin、空安装输出空数组。
func TestListJSON(t *testing.T) {
	root := withTempVersions(t)
	for _, d := range []string{"temurin-21.0.8+7", "temurin-17.0.12+8", "corretto-21.0.5+6", "21.0.12+8"} {
		if err := os.MkdirAll(filepath.Join(root, "versions", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := junction.Create(paths.CurrentLink, filepath.Join(root, "versions", "corretto-21.0.5+6")); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() { List([]string{"--json"}) })
	var got listOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, out)
	}
	if len(got.Installed) != 4 {
		t.Fatalf("应有 4 条, 实际 %d: %+v", len(got.Installed), got.Installed)
	}
	byDir := map[string]listEntry{}
	for _, e := range got.Installed {
		byDir[e.Dir] = e
	}
	// current 标记: 只有 corretto-21.0.5+6
	cur := 0
	for _, e := range got.Installed {
		if e.Current {
			cur++
			if e.Dir != "corretto-21.0.5+6" {
				t.Errorf("current 标错了: %+v", e)
			}
		}
	}
	if cur != 1 {
		t.Errorf("应恰有 1 条 current, 实际 %d", cur)
	}
	// 字段拆分
	if e := byDir["temurin-21.0.8+7"]; e.Distro != "temurin" || e.Version != "21.0.8+7" || e.Major != 21 {
		t.Errorf("temurin 条目 = %+v", e)
	}
	// 旧无前缀目录归 temurin (与 DisplayName 语义一致), dir 保留原始目录名
	if e := byDir["21.0.12+8"]; e.Distro != "temurin" || e.Version != "21.0.12+8" || e.Major != 21 {
		t.Errorf("旧目录条目 = %+v, 想归为 temurin", e)
	}
}

// TestListJSONEmpty 验证空安装时输出 {"installed":[]} 而非人类提示语。
func TestListJSONEmpty(t *testing.T) {
	withTempVersions(t)
	out := captureStdout(t, func() { List([]string{"--json"}) })
	if got := strings.TrimSpace(out); got != `{"installed":[]}` {
		t.Errorf("空安装输出 = %q, 想 {\"installed\":[]}", got)
	}
}

// TestAvailableTableJSON 验证表格形态 --json: 顶层 distro/display_name + 行字段,
// 且不输出进度行与安装提示 (输出整份可被 JSON 解析)。
func TestAvailableTableJSON(t *testing.T) {
	withTempVersions(t)
	name := "fake-json-table-" + t.Name()
	provider.Register(fakeProvider{name: name})

	out := captureStdout(t, func() { Available(AvailableOptions{Distro: name, JSON: true}) })
	if strings.Contains(out, "正在查询") || strings.Contains(out, "安装:") {
		t.Errorf("--json 不应输出进度行或提示:\n%s", out)
	}
	var got tableOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, out)
	}
	if got.Distro != name || got.DisplayName != "Fake" {
		t.Errorf("顶层 = %+v", got)
	}
	if len(got.Rows) != 3 {
		t.Fatalf("fakeProvider 有 3 个大版本, 实际 %d: %+v", len(got.Rows), got.Rows)
	}
	// Available 顺序 17/21/25, 输出应倒序 (新在前)
	if got.Rows[0].Major != 25 || got.Rows[0].Latest != "25.0.1+1" || got.Rows[0].Failed {
		t.Errorf("首行 = %+v, 想 25 组最新", got.Rows[0])
	}
	if !got.Rows[1].LTS || got.Rows[1].Major != 21 {
		t.Errorf("次行 = %+v, 想 21 LTS", got.Rows[1])
	}
}

// TestAvailableGroupsJSON 验证分组形态 --json (-a): 组结构与 versions 数组。
func TestAvailableGroupsJSON(t *testing.T) {
	withTempVersions(t)
	name := "fake-json-groups-" + t.Name()
	provider.Register(fakeProvider{name: name})

	out := captureStdout(t, func() { Available(AvailableOptions{Distro: name, All: true, JSON: true}) })
	var got groupsOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, out)
	}
	if got.Distro != name || len(got.Groups) != 3 {
		t.Errorf("分组结果 = %s (distro=%s, %d 组)", out, got.Distro, len(got.Groups))
	}
	// fakeProvider.ListVersions 每组返回两条, 降序 (新在前)
	for _, g := range got.Groups {
		if g.Failed || len(g.Versions) != 2 || g.Versions[0] != fmt.Sprintf("%d.0.1+1", g.Major) {
			t.Errorf("组 = %+v", g)
		}
	}
}
