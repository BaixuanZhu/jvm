// 本文件实现 jvm available 查询结果的本地缓存 (~/.jvm/available-cache.json)。
//
// available 每次实时打全部 provider API (表格形态: Available + 每个 major 一个
// LatestPatch; -a 形态: 每个 major 一个 ListVersions 全量列表), 二次查询明显
// 偏慢。缓存把成功结果按条目留存, TTL 内复用:
//   - 表格条目按 distro 整表存 (Available + LatestPatch 的产物);
//   - 分组条目按 distro@major 逐组存 (--major 单组命中; -a 增量复用, 只重查
//     miss/过期的组, ListVersions 是最重的一档查询, 收益最大)。
//
// 每条目独立时间戳, 新旧不互相"传染"。只缓存全组成功的结果 (failed 行被 TTL
// 固化会让 ✗ 状态最长滞留一个周期); 目标架构参与键校验 (x64/aarch64 版本集合
// 不同, 不匹配整体 miss; mirror 只影响下载 URL 不影响版本列表, 不参与)。
// 读写任何失败静默降级直查 —— 缓存只加速, 绝不拦截。
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"jvm/internal/app"
	"jvm/internal/config"
	"jvm/internal/paths"
)

// availableCacheTTL 是单条缓存结果的有效期。available 的语义是"现在能装什么",
// 太长的 TTL 会让用户照缓存装到已下架的版本 (install 时 404), 10 分钟在
// "二次查询显著提速"与"结果足够新"之间取平衡。
const availableCacheTTL = 10 * time.Minute

// availableCachePath 返回缓存文件路径 (~/.jvm/available-cache.json, 控制面,
// 不随 install_dir 走)。函数而非 init 时算好的变量: 测试会整体换掉 paths.Root。
func availableCachePath() string {
	return filepath.Join(paths.Root, "available-cache.json")
}

// cachedRow / cachedTable 是 availableRow 的可序列化镜像 (原类型字段非导出,
// encoding/json 不序列化非导出字段, 故不复用)。
type cachedRow struct {
	Major  int    `json:"major"`
	LTS    bool   `json:"lts"`
	Latest string `json:"latest"`
}

type cachedTable struct {
	SavedAt string      `json:"saved_at"` // RFC3339, 条目级时间戳
	Rows    []cachedRow `json:"rows"`
}

// cachedGroup 是 versionGroup 的可序列化镜像。
type cachedGroup struct {
	SavedAt  string   `json:"saved_at"`
	Major    int      `json:"major"`
	LTS      bool     `json:"lts"`
	Versions []string `json:"versions"`
}

// availableCache 是缓存文件的 JSON 结构。Tables/Groups 各条目独立时间戳,
// 保存时 merge 保留其他条目。
type availableCache struct {
	Arch   string                 `json:"arch"`   // 写入时的目标架构 (规范值)
	Tables map[string]cachedTable `json:"tables"` // key: distro
	Groups map[string]cachedGroup `json:"groups"` // key: "distro@major"
}

// cacheArchKey 返回缓存键校验用的目标架构 (规范值), 与 provider 经 Configure
// 收到的值同源 (config 合并链: env > 文件 > 默认; 非法值 provider 侧回退 x64,
// 这里保持一致)。cmd 层不持有 main 已加载的 Config, 重新 Load 一次 (小文件,
// 开销可忽略)。
func cacheArchKey() string {
	a, ok := app.NormArch(config.Load().Arch)
	if !ok {
		return app.ArchX64
	}
	return a
}

// groupCacheKey 拼分组条目的键。
func groupCacheKey(distro string, major int) string {
	return fmt.Sprintf("%s@%d", distro, major)
}

// loadCacheFile 读缓存文件整体 (不做 TTL/arch 判定), 任何失败返回零值。
func loadCacheFile() availableCache {
	var c availableCache
	data, err := os.ReadFile(availableCachePath())
	if err != nil {
		return c
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c
	}
	return c
}

// cacheFresh 判断条目时间戳是否仍在 TTL 内。
func cacheFresh(savedAt string) bool {
	t, err := time.Parse(time.RFC3339, savedAt)
	if err != nil {
		return false
	}
	return time.Since(t) < availableCacheTTL
}

// mutateCache 在现有缓存文件上应用改动后落盘: 先整体读 (merge 保留其他条目),
// 架构变化时旧条目属于另一套版本集合、整体重置; 写失败静默。
func mutateCache(mutate func(c *availableCache)) {
	c := loadCacheFile()
	if key := cacheArchKey(); c.Arch != key {
		c = availableCache{Arch: key}
	}
	if c.Tables == nil {
		c.Tables = map[string]cachedTable{}
	}
	if c.Groups == nil {
		c.Groups = map[string]cachedGroup{}
	}
	mutate(&c)
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	// 父目录可能不存在 (available 可能是全新环境跑的第一条命令, ~/.jvm 尚未
	// 由其他命令创建), MkdirAll 兜底; 仍失败则静默放弃本条缓存。
	_ = os.MkdirAll(filepath.Dir(availableCachePath()), 0o755)
	_ = os.WriteFile(availableCachePath(), data, 0o644)
}

// loadTableCache 取某 distro 的表格条目 (availableTable 的缓存查询)。
// 命中条件: arch 匹配 + 条目存在且新鲜。
func loadTableCache(distro string) ([]availableRow, bool) {
	c := loadCacheFile()
	if c.Arch != cacheArchKey() {
		return nil, false
	}
	t, ok := c.Tables[distro]
	if !ok || !cacheFresh(t.SavedAt) {
		return nil, false
	}
	rows := make([]availableRow, 0, len(t.Rows))
	for _, r := range t.Rows {
		rows = append(rows, availableRow{major: r.Major, lts: r.LTS, latest: r.Latest})
	}
	return rows, true
}

// saveTableCache 覆盖写某 distro 的表格条目。调用方保证 rows 内无 failed 行。
func saveTableCache(distro string, rows []availableRow) {
	mutateCache(func(c *availableCache) {
		t := cachedTable{SavedAt: time.Now().UTC().Format(time.RFC3339)}
		for _, r := range rows {
			t.Rows = append(t.Rows, cachedRow{Major: r.major, LTS: r.lts, Latest: r.latest})
		}
		c.Tables[distro] = t
	})
}

// loadGroupCache 取某 distro@major 的分组条目 (availableGroups 的缓存查询)。
func loadGroupCache(distro string, major int) (versionGroup, bool) {
	c := loadCacheFile()
	if c.Arch != cacheArchKey() {
		return versionGroup{}, false
	}
	g, ok := c.Groups[groupCacheKey(distro, major)]
	if !ok || !cacheFresh(g.SavedAt) {
		return versionGroup{}, false
	}
	return versionGroup{major: g.Major, lts: g.LTS, versions: g.Versions}, true
}

// saveGroupCache 覆盖写某 distro@major 的分组条目。调用方保证 g 非 failed。
func saveGroupCache(distro string, g versionGroup) {
	mutateCache(func(c *availableCache) {
		c.Groups[groupCacheKey(distro, g.major)] = cachedGroup{
			SavedAt:  time.Now().UTC().Format(time.RFC3339),
			Major:    g.major,
			LTS:      g.lts,
			Versions: g.versions,
		}
	})
}

// cacheNoticeLine 是缓存命中时的一行说明 (表格/分组共用文案)。
func cacheNoticeLine() string {
	return fmt.Sprintf("⚡ %.0f 分钟内的缓存结果 (jvm available --refresh 强制刷新)", availableCacheTTL.Minutes())
}
