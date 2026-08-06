package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// cmdInstall 处理 jvm install <版本号>
// 版本号支持: 21 (大版本最新) / 21.0.12 (精确小版本) / jdk-21.0.12+8 (完整名)
func cmdInstall(arg string) {
	if err := install(arg); err != nil {
		fail(err.Error())
	}
}

// cmdUse 处理 jvm use <版本号>
func cmdUse(arg string) {
	if err := ensureDirs(); err != nil {
		fail(err.Error())
	}

	// 支持两种输入: 大版本号 (21) 或完整目录名 (jdk-21.0.5+11)
	dir, err := resolveVersion(arg)
	if err != nil {
		fail(err.Error())
	}
	target := filepath.Join(versionsDir, dir)

	fmt.Printf("🔄 切换到 %s ...\n", dir)
	if err := switchTo(target); err != nil {
		fail(err.Error())
	}

	fmt.Printf("✅ 已切换到 %s\n", dir)
	fmt.Println()
	fmt.Println("📌 已设置:")
	fmt.Printf("   JAVA_HOME = %s\n", currentLink)
	fmt.Printf("   PATH 中已包含 %s\n", filepath.Join(currentLink, "bin"))
	fmt.Println()
	fmt.Println("   集成了 shell 函数的终端 (PowerShell / Git Bash):")
	fmt.Println("   java -version 现在就是新版本。")
	fmt.Println("   未集成或老终端: 新开一个窗口即可。")
}

// switchTo 是 cmdUse 调用的核心切换函数
// (放在 commands.go 因为它编排 junction + env)
func switchTo(targetDir string) error {
	// 1. 先删掉旧的 current junction
	if _, err := os.Lstat(currentLink); err == nil {
		if err := removeJunction(currentLink); err != nil {
			return fmt.Errorf("无法移除旧的 current 链接: %w", err)
		}
	}
	// 2. 创建新的 junction
	if err := createJunction(currentLink, targetDir); err != nil {
		return err
	}
	// 3. 持久化 JAVA_HOME
	if err := persistEnv("JAVA_HOME", currentLink); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  设置 JAVA_HOME 失败: %v\n", err)
	}
	// 4. 确保 current/bin 在 PATH 最前
	if err := ensureCurrentInPath(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  设置 PATH 失败: %v\n", err)
	}
	return nil
}

// cmdList 处理 jvm list
func cmdList() {
	if err := ensureDirs(); err != nil {
		fail(err.Error())
	}
	names, current := listLocalVersions()
	if len(names) == 0 {
		fmt.Println("还没有安装任何版本。")
		fmt.Println("运行 jvm available 查看可安装版本, 然后 jvm install <版本号>。")
		return
	}
	fmt.Println("已安装的版本:")
	for _, n := range names {
		mark := "  "
		if n == current {
			mark = "→ " // 标记当前版本
		}
		fmt.Printf("  %s%s\n", mark, n)
	}
}

// cmdAvailable 处理 jvm available
func cmdAvailable() {
	fmt.Println("🔍 正在查询可安装的大版本...")
	releases, err := fetchAvailableReleases()
	if err != nil {
		fail("查询失败: " + err.Error())
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

// cmdUninstall 处理 jvm uninstall <版本号>
func cmdUninstall(arg string) {
	if err := ensureDirs(); err != nil {
		fail(err.Error())
	}
	dir, err := resolveVersion(arg)
	if err != nil {
		fail(err.Error())
	}

	// 如果正在用这个版本, 先解除 current
	if t := readJunctionTarget(); t != "" && filepath.Base(t) == dir {
		fmt.Printf("⚠️  当前正在使用 %s, 先解除 current 链接...\n", dir)
		if err := removeJunction(currentLink); err != nil {
			fail("解除 current 失败: " + err.Error())
		}
	}

	target := filepath.Join(versionsDir, dir)
	fmt.Printf("🗑️  正在删除 %s ...\n", dir)
	if err := os.RemoveAll(target); err != nil {
		fail("删除失败: " + err.Error())
	}
	fmt.Printf("✅ 已卸载 %s\n", dir)
}

// cmdCurrent 处理 jvm current
func cmdCurrent() {
	t := readJunctionTarget()
	if t == "" {
		fmt.Println("当前没有选中任何版本。运行 jvm use <版本号> 来选择。")
		return
	}
	fmt.Printf("当前版本: %s\n", filepath.Base(t))
	// 顺便读一下 java -version, 直观验证
	javaBin := filepath.Join(currentLink, "bin", "java.exe")
	if info, _ := os.Stat(javaBin); info == nil {
		fmt.Println("(current 链接存在, 但 java.exe 未找到)")
		return
	}
	fmt.Println("java -version:")
	// 不在这里替用户执行 java.exe (避免引入子进程调用面);
	// 提示用户在新终端自行运行 java -version 验证
	fmt.Printf("  在新终端运行: \"%s\" -version\n", javaBin)
}
