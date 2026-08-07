// Package cmd 实现各子命令的业务逻辑 (install/use/list/available/uninstall/current)。
//
// 这是命令编排层: 解析参数, 调用 paths/provider/jdk/junction/env 完成具体工作,
// 输出结果给用户。不含命令路由 (路由在 main 包)。
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"jvm/internal/app"
	"jvm/internal/env"
	"jvm/internal/jdk"
	"jvm/internal/junction"
	"jvm/internal/paths"
	"jvm/internal/provider"
)

// Install 处理 jvm install <版本号>
// 版本号支持: 21 / 21.0.12 / jdk-21.0.12+8 / corretto@21 / microsoft@21.0.12
func Install(arg string) {
	spec, err := app.ParseVersionSpec(arg)
	if err != nil {
		app.Fail(err.Error())
	}
	p, err := provider.Get(spec.Distro)
	if err != nil {
		app.Fail(err.Error())
	}
	if err := jdk.InstallVersion(p, spec); err != nil {
		app.Fail(err.Error())
	}
}

// Use 处理 jvm use <[distro@]版本号>
func Use(arg string) {
	if err := paths.EnsureDirs(); err != nil {
		app.Fail(err.Error())
	}

	spec, err := app.ParseVersionSpec(arg)
	if err != nil {
		app.Fail(err.Error())
	}
	dir, err := junction.ResolveVersion(spec.Distro, spec.Version)
	if err != nil {
		app.Fail(err.Error())
	}
	target := filepath.Join(paths.VersionsDir, dir)
	display := junction.DisplayName(dir) // 归一化为 {distro}-{version} 展示

	fmt.Printf("🔄 切换到 %s ...\n", display)
	if err := switchTo(target); err != nil {
		app.Fail(err.Error())
	}

	fmt.Printf("✅ 已切换到 %s\n", display)
	fmt.Println()
	fmt.Println("📌 已设置:")
	fmt.Printf("   JAVA_HOME = %s\n", paths.CurrentLink)
	fmt.Printf("   PATH 中已包含 %s\n", filepath.Join(paths.CurrentLink, "bin"))
	fmt.Println()
	fmt.Println("   集成了 shell 函数的终端 (PowerShell / Git Bash):")
	fmt.Println("   java -version 现在就是新版本。")
	fmt.Println("   未集成或老终端: 新开一个窗口即可。")
}

// switchTo 切换的核心: 删旧 junction → 建新 → 持久化 JAVA_HOME/PATH
func switchTo(targetDir string) error {
	if _, err := os.Lstat(paths.CurrentLink); err == nil {
		if err := junction.Remove(paths.CurrentLink); err != nil {
			return fmt.Errorf("无法移除旧的 current 链接: %w", err)
		}
	}
	if err := junction.Create(paths.CurrentLink, targetDir); err != nil {
		return err
	}
	if err := env.Persist("JAVA_HOME", paths.CurrentLink); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  设置 JAVA_HOME 失败: %v\n", err)
	}
	if err := env.EnsureCurrentInPath(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  设置 PATH 失败: %v\n", err)
	}
	return nil
}

// List 处理 jvm list
func List() {
	if err := paths.EnsureDirs(); err != nil {
		app.Fail(err.Error())
	}
	names, current := junction.ListLocal()
	if len(names) == 0 {
		fmt.Println("还没有安装任何版本。")
		fmt.Println("运行 jvm available 查看可安装版本, 然后 jvm install <版本号>。")
		return
	}
	fmt.Println("已安装的版本:")
	for _, n := range names {
		mark := "  "
		if n == current {
			mark = "→ "
		}
		// 显示名归一化: 旧的无前缀目录 (21.0.12+8) 补上 temurin- 前缀,
		// 与新装目录 (temurin-21.0.5+11) 显示一致; 实际目录名不变。
		fmt.Printf("  %s%s\n", mark, junction.DisplayName(n))
	}
}

// availableRow 是表格里的一行: 每个大版本最新 GA 的简短版本号 + LTS 标记
type availableRow struct {
	major  int
	latest string // 简短版本号 "21.0.12+8"; 查询失败留空
	failed bool   // 该大版本查询是否失败
	lts    bool
}

// AvailableOptions 是 jvm available 的选项 (由 ParseAvailableArgs 解析)。
type AvailableOptions struct {
	Distro string // 可选: 指定发行版 (空 = 默认 temurin)
	All    bool   // -a/--all: 列出每个大版本的全部子版本
	Major  int    // -m/--major: 仅列出该大版本的全部子版本 (0 表示未指定)
}

// ParseAvailableArgs 解析 jvm available 的命令行参数。
// 支持:
//
//	[distro]                位置参数: 指定发行版 (如 corretto), 空 = 默认 temurin
//	-a / --all              列出所有大版本的全部子版本
//	-m <N> / --major <N>    仅列出大版本 N 的全部子版本
//	--major=<N>             同上 (等号形式)
//
// -a 与 --major 互斥。distro 位置参数最多一个, 多余报错。
// 纯函数, 便于表驱动测试。
func ParseAvailableArgs(args []string) (AvailableOptions, error) {
	var opts AvailableOptions
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-a" || arg == "--all":
			if opts.Major > 0 {
				return opts, fmt.Errorf("-a 与 --major 不能同时使用")
			}
			opts.All = true
		case arg == "-m" || arg == "--major":
			if opts.All {
				return opts, fmt.Errorf("-a 与 --major 不能同时使用")
			}
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s 需要一个大版本号参数", arg)
			}
			i++
			m, err := app.ParseMajorVersion(args[i])
			if err != nil {
				return opts, fmt.Errorf("无效的大版本号 %q: %w", args[i], err)
			}
			opts.Major = m
		case strings.HasPrefix(arg, "--major="):
			if opts.All {
				return opts, fmt.Errorf("-a 与 --major 不能同时使用")
			}
			val := strings.TrimPrefix(arg, "--major=")
			m, err := app.ParseMajorVersion(val)
			if err != nil {
				return opts, fmt.Errorf("无效的大版本号 %q: %w", val, err)
			}
			opts.Major = m
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("未识别的选项: %s (可用: -a / --all / -m <N> / --major <N>)", arg)
		default:
			// 位置参数: 第一个当 distro, 多余报错
			if opts.Distro != "" {
				return opts, fmt.Errorf("未识别的参数: %s (available 最多接受一个发行版名位置参数)", arg)
			}
			opts.Distro = arg
		}
		i++
	}
	return opts, nil
}

// versionGroup 是分组输出里一个大版本的全部子版本 (已规整, 降序)。
type versionGroup struct {
	major    int
	lts      bool
	versions []string
	failed   bool // 查询失败时为 true, versions 为空
}

// Available 处理 jvm available [distro] [-a | --major <N>]。
// 无 flag 时以表格列出每个大版本的最新 GA; -a/--major 时按大版本分组列出全部子版本。
// distro 位置参数指定发行版 (空 = 默认 temurin)。
func Available(opts AvailableOptions) {
	distro := opts.Distro
	if distro == "" {
		distro = provider.Default
	}
	if opts.All || opts.Major > 0 {
		availableGroups(opts, distro)
		return
	}
	availableTable(distro)
}

// availableTable 是默认的表格输出 (每个大版本最新 GA)。
func availableTable(distro string) {
	p, err := provider.Get(distro)
	if err != nil {
		app.Fail(err.Error())
	}
	fmt.Printf("🔍 正在查询 %s 可安装的大版本 (并发获取最新版本号)...\n", p.DisplayName())
	releases, err := p.Available()
	if err != nil {
		app.Fail("查询失败: " + err.Error())
	}
	if len(releases) == 0 {
		fmt.Println("没有查询到可用版本。")
		return
	}

	// 并发查每个大版本的最新 GA 版本号
	rows := make([]availableRow, len(releases))
	var wg sync.WaitGroup
	for i, r := range releases {
		wg.Add(1)
		go func(i int, r app.Release) {
			defer wg.Done()
			row := availableRow{major: r.Major, lts: r.LTS}
			// LatestPatch 只取该 major 最新一条, 比 Resolve 轻量 (不解析用户输入/不内化 CDN)
			if asset, e := p.LatestPatch(r.Major); e == nil {
				row.latest = asset.ReleaseName
			} else {
				row.failed = true
			}
			rows[i] = row
		}(i, r)
	}
	wg.Wait()

	// 表格按 major 倒序展示 (新版本在前)
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}

	printAvailableTable(rows, p.DisplayName())
	fmt.Println()
	fmt.Printf("安装: jvm install %s@<版本号>  例如: jvm install %s@21  或  jvm install %s@21.0.12\n",
		p.Name(), p.Name(), p.Name())
	fmt.Printf("查看全部子版本: jvm available %s -a  或  jvm available %s --major 21\n", p.Name(), p.Name())
}

// availableGroups 按 -a / --major 分组列出全部子版本。
func availableGroups(opts AvailableOptions, distro string) {
	p, err := provider.Get(distro)
	if err != nil {
		app.Fail(err.Error())
	}
	fmt.Printf("🔍 正在查询 %s 可安装的子版本 (可能稍慢)...\n", p.DisplayName())

	// 取大版本列表 + LTS 标记 (单次 API; --major 场景也用它拿 LTS 标记)
	releases, err := p.Available()
	if err != nil {
		app.Fail("查询可用大版本失败: " + err.Error())
	}

	ltsOf := map[int]bool{}
	for _, r := range releases {
		ltsOf[r.Major] = r.LTS
	}

	// 确定要查哪些大版本
	var majors []int
	if opts.Major > 0 {
		if _, ok := ltsOf[opts.Major]; !ok {
			app.Fail(fmt.Sprintf("没有大版本 %d。运行 jvm available 查看可安装的大版本", opts.Major))
		}
		majors = []int{opts.Major}
	} else {
		for _, r := range releases {
			majors = append(majors, r.Major)
		}
	}
	// 降序 (新版本在前)
	sortIntsDesc(majors)

	// 并发取每个大版本的全部子版本 (provider 自己负责拉全量, 不再有截断提示)
	groups := make([]versionGroup, len(majors))
	var wg sync.WaitGroup
	for i, m := range majors {
		wg.Add(1)
		go func(i, m int) {
			defer wg.Done()
			g := versionGroup{major: m, lts: ltsOf[m]}
			if assets, e := p.ListVersions(m); e == nil {
				g.versions = make([]string, 0, len(assets))
				for _, a := range assets {
					g.versions = append(g.versions, a.ReleaseName)
				}
			} else {
				g.failed = true
			}
			groups[i] = g
		}(i, m)
	}
	wg.Wait()

	printAvailableGroups(groups, p.DisplayName())
	fmt.Println()
	fmt.Printf("安装: jvm install %s@<版本号>  例如: jvm install %s@21.0.10+7\n", p.Name(), p.Name())
}

// sortIntsDesc 原地把切片降序排序 (仅用于一组 major 号, 避免引入 sort 到调用点)。
func sortIntsDesc(a []int) {
	sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
}

// printAvailableGroups 按大版本分组打印全部子版本, 每组最新版标记 ← 最新。
func printAvailableGroups(groups []versionGroup, displayName string) {
	fmt.Printf("可安装的大版本 (%s):\n", displayName)
	fmt.Println()
	for _, g := range groups {
		header := fmt.Sprintf("JDK %d", g.major)
		if g.lts {
			header += " (LTS)"
		}
		fmt.Println(header + ":")
		if g.failed {
			fmt.Println("  (查询失败)")
			fmt.Println()
			continue
		}
		if len(g.versions) == 0 {
			fmt.Println("  (无)")
			fmt.Println()
			continue
		}
		for i, v := range g.versions {
			if i == 0 {
				fmt.Printf("  %s  ← 最新\n", v)
			} else {
				fmt.Printf("  %s\n", v)
			}
		}
		fmt.Printf("  (共 %d 个)\n", len(g.versions))
		fmt.Println()
	}
}

// printAvailableTable 以对齐的 ASCII 表格形式打印可安装版本。
// 列宽按显示宽度 (runeWidth) 计算, 这样中文表头与 ASCII 版本号能正确对齐。
func printAvailableTable(rows []availableRow, displayName string) {
	// 三列的表头标题
	const (
		majorTitle  = "大版本"
		latestTitle = "最新版本"
		ltsTitle    = "类型"
	)

	// 列宽 = max(所有单元格显示宽度, 表头显示宽度)
	majorW := runeWidth(majorTitle)
	latestW := runeWidth(latestTitle)
	ltsW := runeWidth(ltsTitle)
	for _, r := range rows {
		if w := runeWidth(itoa(r.major)); w > majorW {
			majorW = w
		}
		latest := r.latest
		if latest == "" {
			latest = "N/A"
		}
		if w := runeWidth(latest); w > latestW {
			latestW = w
		}
		lts := ""
		if r.lts {
			lts = "LTS"
		}
		if w := runeWidth(lts); w > ltsW {
			ltsW = w
		}
	}

	// 用 + - | 画框, 兼容性最好 (任何控制台/字体都不会乱码)
	border := func(left, mid, right string) string {
		return left + strings.Repeat("-", majorW+2) + mid +
			strings.Repeat("-", latestW+2) + mid +
			strings.Repeat("-", ltsW+2) + right
	}
	borderTop := border("+", "+", "+")
	borderMid := border("+", "+", "+")
	borderBot := border("+", "+", "+")

	fmt.Printf("可安装的大版本 (%s):\n", displayName)
	fmt.Println()
	fmt.Println(borderTop)
	// 表头行: 大版本 (靠右) | 最新版本 (靠左) | 类型 (居中)
	fmt.Printf("| %s | %s | %s |\n",
		padLeft(majorTitle, majorW),
		padRight(latestTitle, latestW),
		padCenter(ltsTitle, ltsW),
	)
	fmt.Println(borderMid)
	for _, r := range rows {
		latest := r.latest
		if latest == "" {
			latest = "N/A"
		}
		lts := ""
		if r.lts {
			lts = "LTS"
		}
		fmt.Printf("| %s | %s | %s |\n",
			padLeft(itoa(r.major), majorW),
			padRight(latest, latestW),
			padCenter(lts, ltsW),
		)
	}
	fmt.Println(borderBot)
}

// itoa 是 strconv.Itoa 的别名, 避免引入 strconv (保持 cmd 包 import 精简)
func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// runeWidth 返回字符串的显示宽度 (大多数中日韩字符占 2 列, 其余占 1 列)。
// 供表格对齐使用 —— 这里的版本号与中文文案混排时不能只数 rune。
func runeWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWide(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// isWide 粗略判断一个 rune 是否在等宽终端里占 2 列 (CJK 范围)。
func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x303E, // CJK Radicals / 标点
		r >= 0x3040 && r <= 0x33BF, // 平假名/片假名/CJK
		r >= 0x3400 && r <= 0x4DBF, // CJK Ext A
		r >= 0x4E00 && r <= 0x9FFF, // CJK 统一汉字
		r >= 0xA000 && r <= 0xA4CF, // 彝文
		r >= 0xAC00 && r <= 0xD7A3, // 韩文音节
		r >= 0xF900 && r <= 0xFAFF, // CJK 兼容汉字
		r >= 0xFE30 && r <= 0xFE4F, // CJK 兼容标点
		r >= 0xFF00 && r <= 0xFF60, // 全角字符
		r >= 0xFFE0 && r <= 0xFFE6: // 全角符号
		return true
	}
	return false
}

// padRight 用空格把 s 补齐到显示宽度 w (按显示列数, 而非 rune 数)
func padRight(s string, w int) string {
	pad := w - runeWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// padLeft 用空格把 s 左侧补齐到显示宽度 w
func padLeft(s string, w int) string {
	pad := w - runeWidth(s)
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

// padCenter 把 s 在宽度 w 里居中 (左 padding 比 right 少一个空格以处理奇数)
func padCenter(s string, w int) string {
	pad := w - runeWidth(s)
	if pad <= 0 {
		return s
	}
	left := pad / 2
	right := pad - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// Uninstall 处理 jvm uninstall <版本号> [-y|--yes]
// 默认会在删除前要求确认, 加 -y/--yes 可跳过 (便于脚本调用)。
func Uninstall(args []string) {
	if len(args) == 0 {
		app.Fail("用法: jvm uninstall <版本号> [-y|--yes]")
	}

	assumeYes := false
	var versionArg string
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			assumeYes = true
		default:
			if versionArg == "" {
				versionArg = a
			}
		}
	}
	if versionArg == "" {
		app.Fail("用法: jvm uninstall <[distro@]版本号> [-y|--yes]")
	}

	if err := paths.EnsureDirs(); err != nil {
		app.Fail(err.Error())
	}
	spec, err := app.ParseVersionSpec(versionArg)
	if err != nil {
		app.Fail(err.Error())
	}
	dir, err := junction.ResolveVersion(spec.Distro, spec.Version)
	if err != nil {
		app.Fail(err.Error())
	}

	// 删除前确认 (除非 -y)
	if !assumeYes {
		fmt.Printf("将永久删除 ~/.jvm/versions/%s, 确定? [y/N] ", dir)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("已取消。")
			return
		}
	}

	// 如果正在用这个版本, 先解除 current
	if t := junction.ReadTarget(); t != "" && filepath.Base(t) == dir {
		fmt.Printf("⚠️  当前正在使用 %s, 先解除 current 链接...\n", dir)
		if err := junction.Remove(paths.CurrentLink); err != nil {
			app.Fail("解除 current 失败: " + err.Error())
		}
	}

	target := filepath.Join(paths.VersionsDir, dir)
	fmt.Printf("🗑️  正在删除 %s ...\n", dir)
	if err := os.RemoveAll(target); err != nil {
		app.Fail("删除失败: " + err.Error())
	}
	fmt.Printf("✅ 已卸载 %s\n", dir)
}

// Current 处理 jvm current: 显示当前版本并实际执行 java -version 验证。
func Current() {
	t := junction.ReadTarget()
	if t == "" {
		fmt.Println("当前没有选中任何版本。运行 jvm use <版本号> 来选择。")
		return
	}
	fmt.Printf("当前版本: %s\n", filepath.Base(t))
	javaBin := filepath.Join(paths.CurrentLink, "bin", "java.exe")
	if info, _ := os.Stat(javaBin); info == nil {
		fmt.Println("(current 链接存在, 但 java.exe 未找到)")
		return
	}

	// 实际跑一次 java -version (输出走 stderr), 5 秒超时避免卡死
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, javaBin, "-version").CombinedOutput()
	if err != nil {
		fmt.Printf("⚠️  执行 java -version 失败: %v\n", err)
		fmt.Printf("   可在新终端手动运行: \"%s\" -version\n", javaBin)
		return
	}
	fmt.Println("java -version:")
	// 缩进输出, 保持整洁
	for _, line := range strings.Split(strings.TrimRight(string(out), "\r\n"), "\n") {
		fmt.Printf("  %s\n", line)
	}
}
