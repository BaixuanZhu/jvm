// Package cmd 实现各子命令的业务逻辑 (install/use/list/available/uninstall/current)。
//
// 这是命令编排层: 解析参数, 调用 paths/adoptium/jdk/junction/env 完成具体工作,
// 输出结果给用户。不含命令路由 (路由在 main 包)。
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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

// Available 处理 jvm available
func Available() {
	fmt.Println("🔍 正在查询可安装的大版本...")
	releases, err := adoptium.FetchAvailableReleases()
	if err != nil {
		app.Fail("查询失败: " + err.Error())
	}
	if len(releases) == 0 {
		fmt.Println("没有查询到可用版本。")
		return
	}
	fmt.Println("可安装的大版本 (Temurin/Adoptium):")
	fmt.Println()
	for _, r := range releases {
		tag := ""
		if r.LTS {
			tag = "  [LTS]"
		}
		fmt.Printf("  JDK %d%s\n", r.Major, tag)
	}
	fmt.Println()
	fmt.Println("安装: jvm install <版本号>  例如: jvm install 21")
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
