// 本文件实现 jvm update 子命令: 把某个 (发行版, 大版本) 组收敛到最新 patch。
//
// outdated 只负责检查并提示, 本命令补全闭环: 确认 → 安装最新 patch (组内已有
// 则跳过下载) → (当前正在使用该组版本时) 切换 current → 删除组内全部旧 patch
// 目录。与 install 的分工: install 并列安装不动现状, update 一步收敛到最新。
// 只接受大版本号 —— patch 号写死在命令里天然会过时, 由 outdated 的提示引导用户
// 永远用大版本号调用。
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"jvm/internal/app"
	"jvm/internal/jdk"
	"jvm/internal/junction"
	"jvm/internal/paths"
	"jvm/internal/provider"
)

// updateGroup 是一个 (发行版, 大版本) 组的本地安装状态, 供计划升级使用。
type updateGroup struct {
	dirs      []string // 组内全部目录名 (含旧无前缀目录, SplitDistro 已归组)
	latestVer string   // 组内语义最新的版本号 (目录名去 distro 前缀)
}

// Update 处理 jvm update <[distro@]大版本> [-y|--yes]。
// 流程: 查最新 patch → 打印计划并确认 → 安装 (组内已有最新版则跳过下载) →
// 条件切换 → 删除旧目录。
func Update(args []string) {
	arg, assumeYes, err := parseUpdateArgs(args)
	if err != nil {
		app.Fail(err.Error())
	}
	spec, err := app.ParseVersionSpec(arg)
	if err != nil {
		app.Fail(err.Error())
	}
	major, err := app.ParseMajorVersion(spec.Version)
	if err != nil {
		app.Fail("update 只支持大版本号 (例如: jvm update 21  或  jvm update corretto@21),\n" +
			"  要安装指定 patch 请用 jvm install <完整版本号>")
	}
	p, err := provider.Get(spec.Distro)
	if err != nil {
		app.Fail(err.Error())
	}
	if err := paths.EnsureDirs(); err != nil {
		app.Fail(err.Error())
	}

	names, _ := junction.ListLocal()
	group, err := planUpdate(names, spec.Distro, major)
	if err != nil {
		app.Fail(err.Error())
	}

	fmt.Printf("🔍 正在查询 %s %d 的最新 patch...\n", spec.Distro, major)
	asset, err := p.LatestPatch(major)
	if err != nil {
		app.Fail(err.Error())
	}
	latest := asset.ReleaseName

	// 本地比远端新 (镜像延迟等): 绝不降级, 视为最新退出。
	if app.CompareVersions(group.latestVer, latest) > 0 {
		fmt.Printf("✅ %s@%d 已是最新 patch (%s, 比远端 %s 新)\n", spec.Distro, major, group.latestVer, latest)
		return
	}

	newDir := spec.Distro + "-" + latest
	var toDelete []string
	latestInstalled := false
	for _, d := range group.dirs {
		if d == newDir {
			latestInstalled = true
		} else {
			toDelete = append(toDelete, d)
		}
	}
	// 组级已最新且无旧目录可清理: 纯幂等路径, 无事可做。
	// (按目录名而非版本号判断 latestInstalled: 旧无前缀目录与规范名目录同版本
	// 时名字不同, 走安装路径换成规范名目录反而顺带完成迁移。)
	if latestInstalled && len(toDelete) == 0 {
		fmt.Printf("✅ %s@%d 已是最新 patch (%s)\n", spec.Distro, major, latest)
		return
	}

	// 切换条件: current 正指向组内目录 (即将被删, 不切则 junction 悬空);
	// current 在别的组时不动用户的全局选择, 与 install 不自动切换的语义一致。
	current := ""
	if t := junction.ReadTarget(); t != "" {
		current = filepath.Base(t)
	}
	needSwitch := false
	for _, d := range toDelete {
		if d == current {
			needSwitch = true
		}
	}

	// 打印计划并确认: 一次确认覆盖安装/切换/删除全流程, 且放在下载前 ——
	// 装完再问会让确认在沉没成本面前形同虚设。
	if latestInstalled {
		fmt.Printf("%s@%d 已是最新 patch (%s), 组内有旧版本待清理:\n", spec.Distro, major, latest)
	} else {
		fmt.Printf("将升级 %s@%d:  %s → %s\n", spec.Distro, major, group.latestVer, latest)
		fmt.Printf("  安装  %s\n", newDir)
	}
	for _, d := range toDelete {
		fmt.Printf("  删除  %s\n", d)
	}
	if needSwitch {
		fmt.Println("  (当前正在使用该组版本, 自动切换到最新版)")
	}
	if !assumeYes && !confirm("确定? [y/N] ") {
		fmt.Println("已取消。")
		return
	}

	// 安装最新 patch: 走 Resolve 正规链路 (与 jvm install <精确版本> 完全相同,
	// LatestPatch 是轻量查询不保证填全下载字段, 故不直接拿它的 Asset 下载)。
	// 最新版已在组内 (outdated 信息过期或并列安装过) 则跳过下载直接清理。
	if latestInstalled {
		fmt.Println("📦 最新版已安装, 跳过下载, 直接清理旧版本")
	} else if err := jdk.InstallVersion(p, app.VersionSpec{Distro: spec.Distro, Version: latest}); err != nil {
		app.Fail(err.Error())
	}

	if needSwitch {
		fmt.Printf("🔄 当前正在使用旧版本, 切换到 %s ...\n", newDir)
		if err := switchTo(filepath.Join(paths.VersionsDir, newDir)); err != nil {
			app.Fail("切换失败 (新版已装好, 可手动 jvm use " + itoa(major) + "): " + err.Error())
		}
		clearAutoState() // 旧基线目录即将删除, 待恢复状态必须一并清掉
	}

	// 删除旧目录: Windows 下可能被运行中的 java 进程 (Gradle daemon / IDE) 占用,
	// 删不掉的跳过并警告, 不让整个升级失败 (同 upgrade.CleanupStaleBak 的宽容思路)。
	var failed []string
	for _, d := range toDelete {
		fmt.Printf("🗑️  正在删除 %s ...\n", d)
		if err := os.RemoveAll(filepath.Join(paths.VersionsDir, d)); err != nil {
			fmt.Printf("⚠️  删除 %s 失败 (可能被进程占用)\n", d)
			failed = append(failed, d)
		}
	}

	fmt.Println()
	if latestInstalled {
		fmt.Printf("✅ %s@%d 已是最新 patch (%s), 并清理了 %d 个旧版本\n",
			spec.Distro, major, latest, len(toDelete)-len(failed))
	} else {
		fmt.Printf("✅ %s@%d 已升级到 %s\n", spec.Distro, major, latest)
		if n := len(toDelete) - len(failed); n > 0 {
			fmt.Printf("   已清理 %d 个旧版本\n", n)
		}
	}
	if len(failed) > 0 {
		fmt.Printf("⚠️  %d 个旧版本未能删除, 稍后可 jvm uninstall 手动清理\n", len(failed))
	} else if len(toDelete) > 0 {
		fmt.Printf("   提示: 若项目 .jvmrc 钉的是完整版本号, 请同步更新 (建议钉大版本号, 如 %s@%d)\n",
			spec.Distro, major)
	}
}

// parseUpdateArgs 解析 jvm update 的命令行参数: 一个版本位置参数 + 可选 -y/--yes。
// 纯函数, 便于表驱动测试。
func parseUpdateArgs(args []string) (versionArg string, assumeYes bool, err error) {
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			assumeYes = true
		default:
			if versionArg != "" {
				return "", false, fmt.Errorf("未识别的参数: %s (用法: jvm update <[distro@]大版本> [-y|--yes])", a)
			}
			versionArg = a
		}
	}
	if versionArg == "" {
		return "", false, fmt.Errorf("用法: jvm update <[distro@]大版本> [-y|--yes]")
	}
	return versionArg, assumeYes, nil
}

// planUpdate 从 ListLocal 的结果里取出 (distro, major) 组的完整状态:
// 组内全部目录 + 语义最新的版本号。组不存在时返回错误 (update 只用于升级已装
// 版本, 引导走 install)。输入 names 应为 ListLocal 的降序输出 (组内首个即最新)。
// 纯函数, 便于表驱动测试。
func planUpdate(names []string, distro string, major int) (updateGroup, error) {
	var g updateGroup
	for _, n := range names {
		if d, _ := junction.SplitDistro(n); d != distro || junction.MajorOf(n) != major {
			continue
		}
		g.dirs = append(g.dirs, n)
	}
	if len(g.dirs) == 0 {
		return g, fmt.Errorf("没有安装 %s 的大版本 %d, 无可升级。运行 jvm list 查看已安装版本", distro, major)
	}
	_, g.latestVer = junction.SplitDistro(g.dirs[0])
	return g, nil
}
