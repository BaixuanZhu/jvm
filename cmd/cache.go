package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"jvm/internal/app"
	"jvm/internal/paths"
)

// Cache 处理 jvm cache [clean]: 查看下载缓存 (安装包 zip 留存, 重装免下载)
// 或清空。缓存位于 {dataRoot}/cache, 随 install_dir 配置走。
func Cache(args []string) {
	switch {
	case len(args) == 0:
		listCache()
	case len(args) == 1 && args[0] == "clean":
		cleanCache()
	default:
		app.Fail("用法: jvm cache [clean]")
	}
}

// cacheEntry 是一条缓存记录 (zip 文件及其大小)。
type cacheEntry struct {
	name string
	size int64
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
		out = append(out, cacheEntry{e.Name(), info.Size()})
		total += info.Size()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, total
}

// listCache 打印缓存条目与合计, 空缓存给空态提示。
func listCache() {
	entries, total := listCacheEntries()
	fmt.Printf("📦 下载缓存 (%s):\n", paths.CacheDir)
	if len(entries) == 0 {
		fmt.Println("   (空。安装 JDK 后, 安装包会留在这里, 卸载重装免重新下载)")
		return
	}
	for _, e := range entries {
		fmt.Printf("   %-40s %8.1f MB\n", e.name, float64(e.size)/1024/1024)
	}
	fmt.Printf("   合计 %d 个文件, %.1f MB\n", len(entries), float64(total)/1024/1024)
	fmt.Println("清理: jvm cache clean")
}

// cleanCache 删除缓存里的 zip 与中断残留的 .zip.part, 保留目录本身。
func cleanCache() {
	entries, _ := os.ReadDir(paths.CacheDir)
	var freed int64
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".zip") && !strings.HasSuffix(name, ".zip.part")) {
			continue
		}
		if info, err := e.Info(); err == nil {
			freed += info.Size()
		}
		if err := os.Remove(filepath.Join(paths.CacheDir, name)); err != nil {
			fmt.Printf("⚠️  删除 %s 失败: %v\n", name, err)
			continue
		}
		removed++
	}
	fmt.Printf("🗑️  已清理 %d 个缓存文件, 释放 %.1f MB\n", removed, float64(freed)/1024/1024)
}
