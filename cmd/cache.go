package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"jvm/internal/app"
	"jvm/internal/paths"
)

// maxAgeDays 是 --older-than 允许的最大天数 (100 年), 防止换算 Duration 溢出。
const maxAgeDays = 36500

// Cache 处理 jvm cache [clean [--older-than <Nd>]]: 查看下载缓存 (安装包 zip
// 留存, 重装免下载) 或清理。缓存位于 {dataRoot}/cache, 随 install_dir 配置走。
func Cache(args []string) {
	switch {
	case len(args) == 0:
		listCache()
	case args[0] == "clean":
		maxAge, filtered, err := parseCacheCleanArgs(args[1:])
		if err != nil {
			app.Fail(err.Error())
		}
		cleanCache(maxAge, filtered)
	default:
		app.Fail("用法: jvm cache [clean [--older-than <Nd>]]")
	}
}

// parseAge 解析按龄参数 "<N>d": N 为正整数天数, 如 "30d" → 720h。
// 只支持天粒度 —— 缓存清理场景足够, 文件 mtime 的更细粒度没有意义。
// 纯函数, 便于表驱动测试。
func parseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "d") {
		return 0, fmt.Errorf("无效的时限 %q (应为 <N>d 形式, 如 30d)", s)
	}
	n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("无效的时限 %q (N 需为正整数天数, 如 30d)", s)
	}
	if n > maxAgeDays {
		return 0, fmt.Errorf("时限 %q 过大 (上限 %dd)", s, maxAgeDays)
	}
	return time.Duration(n) * 24 * time.Hour, nil
}

// parseCacheCleanArgs 解析 jvm cache clean 的参数。
// 支持 --older-than <Nd> 与 --older-than=<Nd>: 只清理修改时间早于 N 天前的
// 缓存文件; 未指定时 filtered 为 false (无条件全清)。
// 纯函数, 便于表驱动测试。
func parseCacheCleanArgs(args []string) (maxAge time.Duration, filtered bool, err error) {
	i := 0
	for i < len(args) {
		arg := args[i]
		val := ""
		switch {
		case arg == "--older-than":
			if i+1 >= len(args) {
				return 0, false, fmt.Errorf("--older-than 需要一个时限参数 (如 30d)")
			}
			i++
			val = args[i]
		case strings.HasPrefix(arg, "--older-than="):
			val = strings.TrimPrefix(arg, "--older-than=")
		default:
			return 0, false, fmt.Errorf("未识别的参数: %s (可用: --older-than <Nd>)", arg)
		}
		if filtered {
			return 0, false, fmt.Errorf("--older-than 只能指定一次")
		}
		d, e := parseAge(val)
		if e != nil {
			return 0, false, e
		}
		maxAge, filtered = d, true
		i++
	}
	return maxAge, filtered, nil
}

// cacheEntry 是一条缓存记录 (zip 文件、大小与最后修改时间)。
type cacheEntry struct {
	name    string
	size    int64
	modTime time.Time
}

// listCacheEntries 扫描缓存目录, 返回 .zip 文件 (按名字典序) 与总字节数。
// 目录不存在视为空。
func listCacheEntries() ([]cacheEntry, int64) {
	entries, err := os.ReadDir(paths.CacheDir)
	if err != nil {
		return nil, 0
	}
	var out []cacheEntry
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, cacheEntry{e.Name(), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, total
}

// listCache 打印缓存条目与合计 (附最后修改日期, 供决定 --older-than 阈值参考),
// 空缓存给空态提示。
func listCache() {
	entries, total := listCacheEntries()
	fmt.Printf("📦 下载缓存 (%s):\n", paths.CacheDir)
	if len(entries) == 0 {
		fmt.Println("   (空。安装 JDK 后, 安装包会留在这里, 卸载重装免重新下载)")
		return
	}
	for _, e := range entries {
		fmt.Printf("   %-40s %8.1f MB  %s\n", e.name, float64(e.size)/1024/1024, e.modTime.Format("2006-01-02"))
	}
	fmt.Printf("   合计 %d 个文件, %.1f MB\n", len(entries), float64(total)/1024/1024)
	fmt.Println("清理: jvm cache clean                    (全部)")
	fmt.Println("      jvm cache clean --older-than 30d   (仅 30 天前的)")
}

// cleanCache 删除缓存里的 zip 与中断残留的 .zip.part, 保留目录本身。
// filtered 为 true 时只删修改时间早于 maxAge 前的文件 (.part 同样适用 ——
// 中断的续传分片按龄清理正合适); false 时无条件全清。删除失败宽容继续
// (Windows 文件被占用场景)。
func cleanCache(maxAge time.Duration, filtered bool) {
	cutoff := time.Now().Add(-maxAge)
	entries, _ := os.ReadDir(paths.CacheDir)
	var freed int64
	removed, kept := 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".zip") && !strings.HasSuffix(name, ".zip.part")) {
			continue
		}
		info, infoErr := e.Info()
		if filtered && (infoErr != nil || !info.ModTime().Before(cutoff)) {
			// 读不到 mtime 时按未到期处理 (保守保留, 下次再清)
			kept++
			continue
		}
		if infoErr == nil {
			freed += info.Size()
		}
		if err := os.Remove(filepath.Join(paths.CacheDir, name)); err != nil {
			fmt.Printf("⚠️  删除 %s 失败: %v\n", name, err)
			continue
		}
		removed++
	}
	if !filtered {
		fmt.Printf("🗑️  已清理 %d 个缓存文件, 释放 %.1f MB\n", removed, float64(freed)/1024/1024)
		return
	}
	days := int(maxAge.Hours() / 24)
	msg := fmt.Sprintf("🗑️  已清理 %d 个缓存文件 (修改时间早于 %d 天前), 释放 %.1f MB",
		removed, days, float64(freed)/1024/1024)
	if kept > 0 {
		msg += fmt.Sprintf("; 保留 %d 个 (%d 天内)", kept, days)
	}
	fmt.Println(msg)
}
