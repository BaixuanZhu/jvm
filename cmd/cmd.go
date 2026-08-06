// Package cmd 实现各子命令的业务逻辑 (install/use/list/available/uninstall/current)。
//
// 这是命令编排层: 解析参数, 调用 paths/adoptium/jdk/junction/env 完成具体工作,
// 输出结果给用户。不含命令路由 (路由在 main 包)。
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"jvm/internal/adoptium"
	"jvm/internal/app"
	"jvm/internal/env"
	"jvm/internal/jdk"
	"jvm/internal/junction"
	"jvm/internal/paths"
)

// Install 处理 jvm install <版本号>
// 版本号支持: 21 (大版本最新) / 21.0.12 (精确小版本) / jdk-21.0.12+8 (完整名)
func Install(arg string) {
	if err := jdk.Install(arg); err != nil {
		app.Fail(err.Error())
	}
}

// Use 处理 jvm use <版本号>
func Use(arg string) {
	if err := paths.EnsureDirs(); err != nil {
		app.Fail(err.Error())
	}

	dir, err := junction.ResolveVersion(arg)
	if err != nil {
		app.Fail(err.Error())
	}
	target := filepath.Join(paths.VersionsDir, dir)

	fmt.Printf("🔄 切换到 %s ...\n", dir)
	if err := switchTo(target); err != nil {
		app.Fail(err.Error())
	}

	fmt.Printf("✅ 已切换到 %s\n", dir)
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
		fmt.Printf("  %s%s\n", mark, n)
	}
}

// availableRow 是表格里的一行: 每个大版本最新 GA 的简短版本号 + LTS 标记
type availableRow struct {
	major  int
	latest string // 简短版本号 "21.0.12+8"; 查询失败留空
	failed bool   // 该大版本查询是否失败
	lts    bool
}

// Available 处理 jvm available
// 并发查询每个大版本的最新 GA, 以表格形式列出大版本、最新版本号和 LTS 标记。
func Available() {
	fmt.Println("🔍 正在查询可安装的大版本 (并发获取最新版本号)...")
	releases, err := adoptium.FetchAvailableReleases()
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
		go func(i int, r adoptium.AvailableRelease) {
			defer wg.Done()
			row := availableRow{major: r.Major, lts: r.LTS}
			if asset, err := adoptium.FetchLatestAsset(r.Major); err == nil {
				row.latest = adoptium.ShortSemver(asset.Semver)
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

	printAvailableTable(rows)
	fmt.Println()
	fmt.Println("安装: jvm install <版本号>  例如: jvm install 21  或  jvm install 21.0.12")
}

// printAvailableTable 以对齐的 ASCII 表格形式打印可安装版本。
// 列宽按显示宽度 (runeWidth) 计算, 这样中文表头与 ASCII 版本号能正确对齐。
func printAvailableTable(rows []availableRow) {
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

	fmt.Println("可安装的大版本 (Temurin/Adoptium):")
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

// Uninstall 处理 jvm uninstall <版本号>
func Uninstall(arg string) {
	if err := paths.EnsureDirs(); err != nil {
		app.Fail(err.Error())
	}
	dir, err := junction.ResolveVersion(arg)
	if err != nil {
		app.Fail(err.Error())
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

// Current 处理 jvm current
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
	fmt.Println("java -version:")
	fmt.Printf("  在新终端运行: \"%s\" -version\n", javaBin)
}
