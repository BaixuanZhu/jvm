// 本文件实现 jvm outdated 子命令: 检查本地已装版本是否有新 patch 可升级。
//
// JDK 季度安全更新是刚需, 本命令扫 ~/.jvm/versions 全部 (发行版, 大版本) 组合,
// 并发查各 provider 的 LatestPatch, 列出落后行并给出升级命令。
// 升级不需要 --force: 不同 patch 是不同目录名, jvm install <distro>@<major>
// 会解析到新版本并列安装, 旧版用 jvm uninstall 清理。
package cmd

import (
	"fmt"
	"sync"

	"jvm/internal/app"
	"jvm/internal/junction"
	"jvm/internal/paths"
	"jvm/internal/provider"
)

// installedGroup 是一个 (发行版, 大版本) 组合: 本地该组语义最新的安装。
type installedGroup struct {
	distro   string // 发行版标识, 如 "temurin" (旧无前缀目录归为 temurin)
	major    int    // 大版本号
	localDir string // 本地最新版目录名 (如 "temurin-21.0.5+11"), 供展示
	localVer string // 本地最新版本号 (目录名去掉 distro 前缀)
}

// outdatedRow 是 outdated 输出的一行: 一组安装的最新 patch 查询结果。
type outdatedRow struct {
	group  installedGroup
	latest string // 远端最新版本号; 查询失败留空
	failed bool   // 查询是否失败
}

// Outdated 处理 jvm outdated: 列出本地已装版本哪些有新 patch。
func Outdated() {
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

	// 并发查每组的最新 GA (与 availableTable 同款 WaitGroup + 索引切片模式)
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

	printOutdated(rows)
}

// groupInstalled 把本地版本目录按 (发行版, 大版本) 分组, 每组保留语义最新的一个。
// 输入 names 应为 ListLocal 的降序结果 (组内首个即最新); 输出保持该顺序。
// 纯函数, 便于表驱动测试。
func groupInstalled(names []string) []installedGroup {
	var groups []installedGroup
	seen := map[string]bool{}
	for _, n := range names {
		distro, ver := junction.SplitDistro(n)
		major := junction.MajorOf(n)
		if major == 0 {
			continue // 非版本目录, 跳过
		}
		key := fmt.Sprintf("%s-%d", distro, major)
		if seen[key] {
			continue // 该组已取到更新的版本
		}
		seen[key] = true
		groups = append(groups, installedGroup{
			distro:   distro,
			major:    major,
			localDir: n,
			localVer: ver,
		})
	}
	return groups
}

// printOutdated 打印检查结果: 可升级 / 已最新 / 查询失败 分行展示。
func printOutdated(rows []outdatedRow) {
	var upgradable, failed []outdatedRow
	for _, r := range rows {
		switch {
		case r.failed:
			failed = append(failed, r)
		case app.CompareVersions(r.group.localVer, r.latest) < 0:
			upgradable = append(upgradable, r)
		}
	}

	if len(upgradable) == 0 && len(failed) == 0 {
		fmt.Printf("✅ 已安装的 %d 组版本均为最新 patch。\n", len(rows))
		return
	}

	if len(upgradable) > 0 {
		fmt.Println("可升级的版本:")
		for _, r := range upgradable {
			fmt.Printf("  %s@%d    %s → %s\n", r.group.distro, r.group.major, r.group.localVer, r.latest)
		}
		fmt.Println()
		fmt.Println("升级: jvm install <上面左列的 distro@大版本>   例如: jvm install " +
			upgradable[0].group.distro + "@" + itoa(upgradable[0].group.major))
		fmt.Println("      (新版本并列安装, 装好后 jvm use 切换, 旧版 jvm uninstall 清理)")
	}
	if len(failed) > 0 {
		fmt.Println()
		fmt.Println("⚠️  查询失败 (跳过):")
		for _, r := range failed {
			fmt.Printf("  %s@%d    检查 %s\n", r.group.distro, r.group.major, r.group.localVer)
		}
		fmt.Println("      可能是网络问题, 稍后重试或 jvm available <distro> 手动查看。")
	}
}
