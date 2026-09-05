// 本文件实现 jvm update 子命令: 把某个 (发行版, 大版本) 组收敛到最新 patch,
// 或用 --all 一次收敛全部落后组。
//
// outdated 只负责检查并提示, 本命令补全闭环: 确认 → 安装最新 patch (组内已有
// 则跳过下载) → (当前正在使用该组版本时) 切换 current → 删除组内全部旧 patch
// 目录。与 install 的分工: install 并列安装不动现状, update 一步收敛到最新。
// 只接受大版本号 —— patch 号写死在命令里天然会过时, 由 outdated 的提示引导用户
// 永远用大版本号调用; --all 批量形态与单组共用同一执行闭环 (buildUpdatePlan →
// printUpdatePlan → execUpdatePlan), 某组失败不阻断其余。
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

// planStatus 是升级计划的三种结局, 决定编排层走幂等出口还是执行升级。
type planStatus int

const (
	planUpgrade    planStatus = iota // 需要安装最新版或清理旧目录
	planLocalNewer                   // 本地比远端新 (镜像延迟等), 不降级
	planUpToDate                     // 组级已最新且无旧目录, 纯幂等
)

// updatePlan 是一个 (发行版, 大版本) 组的完整升级计划。
type updatePlan struct {
	distro          string
	major           int
	group           updateGroup // 组内全部目录与本地最新版本号
	latest          string      // 远端最新 patch (ReleaseName)
	newDir          string      // 最新版的目标目录名
	toDelete        []string    // 待删除的组内旧目录
	latestInstalled bool        // 最新版已在组内 (跳过下载, 只清理)
	needSwitch      bool        // current 正指向组内待删目录 (打印计划用)
	status          planStatus
}

// Update 处理 jvm update <[distro@]大版本> [-y|--yes] 与 jvm update --all [-y|--yes]。
// 单组流程: 查最新 patch → 打印计划并确认 → 安装 (组内已有最新版则跳过下载) →
// 条件切换 → 删除旧目录; --all 见 updateAll。
func Update(args []string) {
	arg, all, assumeYes, err := parseUpdateArgs(args)
	if err != nil {
		app.Fail(err.Error())
	}
	if all {
		updateAll(assumeYes)
		return
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

	plan := buildUpdatePlan(group, spec.Distro, major, asset.ReleaseName, currentDir())
	switch plan.status {
	case planLocalNewer:
		fmt.Printf("✅ %s@%d 已是最新 patch (%s, 比远端 %s 新)\n", plan.distro, plan.major, plan.group.latestVer, plan.latest)
		return
	case planUpToDate:
		fmt.Printf("✅ %s@%d 已是最新 patch (%s)\n", plan.distro, plan.major, plan.latest)
		return
	}

	// 打印计划并确认: 一次确认覆盖安装/切换/删除全流程, 且放在下载前 ——
	// 装完再问会让确认在沉没成本面前形同虚设。
	printUpdatePlan(plan)
	if !assumeYes && !confirm("确定? [y/N] ") {
		fmt.Println("已取消。")
		return
	}

	failedDirs, err := execUpdatePlan(plan, p)
	if err != nil {
		app.Fail(err.Error())
	}
	printUpdateResult(plan, failedDirs)
}

// updateAll 处理 jvm update --all [-y|--yes]: 并发检查全部 (发行版, 大版本) 组
// (与 outdated 同款数据链路), 汇总打印落后组的升级计划, 一次确认后逐组执行
// (与单组同一闭环)。某组失败不阻断其余, 末尾汇总; 存在失败/跳过组时以非零码
// 退出 (供脚本感知)。
func updateAll(assumeYes bool) {
	if err := paths.EnsureDirs(); err != nil {
		app.Fail(err.Error())
	}
	names, _ := junction.ListLocal()
	if len(names) == 0 {
		fmt.Println("还没有安装任何版本。")
		fmt.Println("运行 jvm available 查看可安装版本, 然后 jvm install <版本号>。")
		return
	}
	groups := groupInstalled(names)
	fmt.Printf("🔍 正在查询 %d 组已装版本的最新 patch (并发)...\n", len(groups))

	// 并发查每组的最新 GA (与 Outdated 同款 WaitGroup + 索引切片模式)
	rows := make([]outdatedRow, len(groups))
	var wg sync.WaitGroup
	for i, g := range groups {
		wg.Add(1)
		go func(i int, g installedGroup) {
			defer wg.Done()
			row := outdatedRow{group: g}
			if p, err := provider.Get(g.distro); err == nil {
				if asset, e := p.LatestPatch(g.major); e == nil {
					row.latest = asset.ReleaseName
				} else {
					row.failed = true
				}
			} else {
				row.failed = true // 目录名解析出未注册的 distro, 极少见
			}
			rows[i] = row
		}(i, g)
	}
	wg.Wait()

	// 分拣: 落后组进升级计划, 查询失败组跳过并在末尾汇总提示。
	var upgradable, failedRows []outdatedRow
	for _, r := range rows {
		switch {
		case r.failed:
			failedRows = append(failedRows, r)
		case app.CompareVersions(r.group.localVer, r.latest) < 0:
			upgradable = append(upgradable, r)
		}
	}
	if len(upgradable) == 0 {
		reportSkippedGroups(failedRows)
		fmt.Printf("✅ 已安装的 %d 组版本均为最新 patch。\n", len(rows)-len(failedRows))
		return
	}

	// 汇总打印全部落后组的计划, 一次确认覆盖全部 (同样放在下载前)。
	fmt.Printf("\n发现 %d 组可升级:\n\n", len(upgradable))
	cur := currentDir()
	var plans []updatePlan
	var provs []provider.Provider
	for _, r := range upgradable {
		p, err := provider.Get(r.group.distro)
		if err != nil {
			continue // 目录名与注册表赛跑的极小概率, 该组按跳过处理
		}
		group, err := planUpdate(names, r.group.distro, r.group.major)
		if err != nil {
			continue // 组来自同一份 names, 理论不发生
		}
		plan := buildUpdatePlan(group, r.group.distro, r.group.major, r.latest, cur)
		plans = append(plans, plan)
		provs = append(provs, p)
		printUpdatePlan(plan)
		fmt.Println()
	}
	if !assumeYes && !confirm("确定升级以上全部? [y/N] ") {
		fmt.Println("已取消。")
		return
	}

	// 逐组执行: 失败只记录不阻断 (其余组与该组互不影响), 末尾统一汇总。
	okCount := 0
	var unhandled []string
	for i, plan := range plans {
		failedDirs, err := execUpdatePlan(plan, provs[i])
		if err != nil {
			fmt.Printf("⚠️  %s@%d 升级失败: %v\n\n", plan.distro, plan.major, err)
			unhandled = append(unhandled, fmt.Sprintf("%s@%d (升级失败)", plan.distro, plan.major))
			continue
		}
		okCount++
		fmt.Printf("✅ %s@%d 已升级到 %s\n", plan.distro, plan.major, plan.latest)
		if n := len(plan.toDelete) - len(failedDirs); n > 0 {
			fmt.Printf("   已清理 %d 个旧版本\n", n)
		}
		if len(failedDirs) > 0 {
			fmt.Printf("⚠️  %d 个旧版本未能删除, 稍后可 jvm uninstall 手动清理\n", len(failedDirs))
		}
		fmt.Println()
	}
	for _, r := range failedRows {
		unhandled = append(unhandled, fmt.Sprintf("%s@%d (查询失败, 跳过)", r.group.distro, r.group.major))
	}

	fmt.Printf("📦 升级完成: 成功 %d 组", okCount)
	if len(unhandled) > 0 {
		fmt.Printf(", 未处理 %d 组 (%s)\n", len(unhandled), strings.Join(unhandled, ", "))
		os.Exit(1) // 有未处理组, 非零退出供脚本感知
	}
	fmt.Println()
}

// reportSkippedGroups 打印查询失败被跳过的组 (outdated 的 failed 文案风格)。
func reportSkippedGroups(failedRows []outdatedRow) {
	if len(failedRows) == 0 {
		return
	}
	fmt.Println("⚠️  查询失败 (跳过):")
	for _, r := range failedRows {
		fmt.Printf("  %s@%d    检查 %s\n", r.group.distro, r.group.major, r.group.localVer)
	}
	fmt.Println()
}

// buildUpdatePlan 由组状态与远端最新版计算升级计划: 本地比远端新则不降级,
// 最新版已在组内且无旧目录则纯幂等, 其余进入升级/清理路径。纯函数, 便于表驱动
// 测试; currentDir 是编排层读好的 current 指向目录名 (仅用于打印计划时的切换
// 提示, 执行时 execUpdatePlan 会按实时 current 重新判定)。
func buildUpdatePlan(group updateGroup, distro string, major int, latest, currentDir string) updatePlan {
	p := updatePlan{distro: distro, major: major, group: group, latest: latest}
	// 本地比远端新 (镜像延迟等): 绝不降级, 视为最新。
	if app.CompareVersions(group.latestVer, latest) > 0 {
		p.status = planLocalNewer
		return p
	}
	p.newDir = distro + "-" + latest
	for _, d := range group.dirs {
		if d == p.newDir {
			p.latestInstalled = true
		} else {
			p.toDelete = append(p.toDelete, d)
		}
	}
	// 组级已最新且无旧目录可清理: 纯幂等路径, 无事可做。
	// (按目录名而非版本号判断 latestInstalled: 旧无前缀目录与规范名目录同版本
	// 时名字不同, 走安装路径换成规范名目录反而顺带完成迁移。)
	if p.latestInstalled && len(p.toDelete) == 0 {
		p.status = planUpToDate
		return p
	}
	for _, d := range p.toDelete {
		if d == currentDir {
			p.needSwitch = true
		}
	}
	p.status = planUpgrade
	return p
}

// printUpdatePlan 打印单组升级计划 (安装/删除/切换明细), 单组与 --all 汇总共用。
func printUpdatePlan(p updatePlan) {
	if p.latestInstalled {
		fmt.Printf("%s@%d 已是最新 patch (%s), 组内有旧版本待清理:\n", p.distro, p.major, p.latest)
	} else {
		fmt.Printf("将升级 %s@%d:  %s → %s\n", p.distro, p.major, p.group.latestVer, p.latest)
		fmt.Printf("  安装  %s\n", p.newDir)
	}
	for _, d := range p.toDelete {
		fmt.Printf("  删除  %s\n", d)
	}
	if p.needSwitch {
		fmt.Println("  (当前正在使用该组版本, 自动切换到最新版)")
	}
}

// execUpdatePlan 执行单组升级: 安装最新 patch (走 Resolve 正规链路, 与
// jvm install <精确版本> 完全相同 —— LatestPatch 是轻量查询不保证填全下载字段)
// → 条件切换 → 删旧目录。返回删除失败的目录列表 (宽容收集, 不算升级失败)。
func execUpdatePlan(p updatePlan, prov provider.Provider) ([]string, error) {
	// 最新版已在组内 (outdated 信息过期或并列安装过) 则跳过下载直接清理。
	if p.latestInstalled {
		fmt.Println("📦 最新版已安装, 跳过下载, 直接清理旧版本")
	} else if err := jdk.InstallVersion(prov, app.VersionSpec{Distro: p.distro, Version: p.latest}); err != nil {
		return nil, err
	}

	// 切换条件执行时按实时 current 判定: current 正指向组内待删目录 (即将被删,
	// 不切则 junction 悬空); current 在别的组时不动用户的全局选择, 与 install
	// 不自动切换的语义一致。(--all 逐组执行时前序组的切换只在组内移动 current,
	// 不影响本组判定。)
	current := ""
	if t := junction.ReadTarget(); t != "" {
		current = filepath.Base(t)
	}
	for _, d := range p.toDelete {
		if d == current {
			fmt.Printf("🔄 当前正在使用旧版本, 切换到 %s ...\n", p.newDir)
			if err := switchTo(filepath.Join(paths.VersionsDir, p.newDir)); err != nil {
				return nil, fmt.Errorf("切换失败 (新版已装好, 可手动 jvm use %d): %w", p.major, err)
			}
			clearAutoState() // 旧基线目录即将删除, 待恢复状态必须一并清掉
			break
		}
	}

	// 删除旧目录: Windows 下可能被运行中的 java 进程 (Gradle daemon / IDE) 占用,
	// 删不掉的跳过并警告, 不让整个升级失败 (同 upgrade.CleanupStaleBak 的宽容思路)。
	var failed []string
	for _, d := range p.toDelete {
		fmt.Printf("🗑️  正在删除 %s ...\n", d)
		if err := os.RemoveAll(filepath.Join(paths.VersionsDir, d)); err != nil {
			fmt.Printf("⚠️  删除 %s 失败 (可能被进程占用)\n", d)
			failed = append(failed, d)
		}
	}
	return failed, nil
}

// printUpdateResult 打印单组升级的收尾汇总 (单组路径专用; --all 用组行内联汇总)。
func printUpdateResult(p updatePlan, failed []string) {
	fmt.Println()
	if p.latestInstalled {
		fmt.Printf("✅ %s@%d 已是最新 patch (%s), 并清理了 %d 个旧版本\n",
			p.distro, p.major, p.latest, len(p.toDelete)-len(failed))
	} else {
		fmt.Printf("✅ %s@%d 已升级到 %s\n", p.distro, p.major, p.latest)
		if n := len(p.toDelete) - len(failed); n > 0 {
			fmt.Printf("   已清理 %d 个旧版本\n", n)
		}
	}
	if len(failed) > 0 {
		fmt.Printf("⚠️  %d 个旧版本未能删除, 稍后可 jvm uninstall 手动清理\n", len(failed))
	} else if len(p.toDelete) > 0 {
		fmt.Printf("   提示: 若项目 .jvmrc 钉的是完整版本号, 请同步更新 (建议钉大版本号, 如 %s@%d)\n",
			p.distro, p.major)
	}
}

// currentDir 返回 current junction 指向的版本目录名, 未指向任何版本返回空串。
func currentDir() string {
	if t := junction.ReadTarget(); t != "" {
		return filepath.Base(t)
	}
	return ""
}

// parseUpdateArgs 解析 jvm update 的命令行参数: --all 或一个版本位置参数,
// 均可与 -y/--yes 组合; --all 与版本参数互斥。纯函数, 便于表驱动测试。
func parseUpdateArgs(args []string) (versionArg string, all, assumeYes bool, err error) {
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			assumeYes = true
		case "--all", "-a":
			all = true
		default:
			if versionArg != "" {
				return "", false, false, fmt.Errorf("未识别的参数: %s (用法: jvm update <[distro@]大版本> [-y|--yes] 或 jvm update --all)", a)
			}
			versionArg = a
		}
	}
	if all && versionArg != "" {
		return "", false, false, fmt.Errorf("--all 不能与版本参数同时使用 (用法: jvm update --all [-y|--yes])")
	}
	if versionArg == "" && !all {
		return "", false, false, fmt.Errorf("用法: jvm update <[distro@]大版本> [-y|--yes] 或 jvm update --all")
	}
	return versionArg, all, assumeYes, nil
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
