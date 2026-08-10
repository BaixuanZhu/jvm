// Package corretto 是 Amazon Corretto 发行版的 provider 适配器。
//
// 数据源: corretto-downloads GitHub 仓库的 latest_links/ 下两个 JSON:
//   - version-info.json: 大版本分类 (LTS / feature / EOL)
//   - indexmap_with_checksum.json: 全平台最新版下载链接 + SHA256
//
// Corretto 的 CloudFront CDN (corretto.aws) 国内可直连, 无需镜像, 故 MirrorURL 留空。
// 限制: CDN 只保留每个大版本的最新 patch, 历史 patch 直链返回 403, 不可下载
// (官方下载页同样只提供最新版)。故完整版本号 install 仅当恰好是当前最新 patch 时成功。
//
// 目标架构: Amazon Corretto 官方没有 Windows ARM64 构建 (21/25 下载页 Windows 仅
// x64, 已实测), 故 arch=aarch64 时所有查询/安装入口统一报 errNoWindowsARM64,
// 引导用户改用 temurin / microsoft。indexmap 结构按架构 map 化, 未来官方若新增
// Windows ARM64 分支, 只需放宽 checkArch 守卫。
package corretto

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"jvm/internal/app"
	"jvm/internal/provider"
)

const (
	// versionInfoURL 列出各大版本的 LTS/feature/EOL 分类
	versionInfoURL = "https://raw.githubusercontent.com/corretto/corretto-downloads/main/latest_links/version-info.json"

	// indexmapURL 是全平台最新版下载链接清单 (含 SHA256)
	indexmapURL = "https://raw.githubusercontent.com/corretto/corretto-downloads/main/latest_links/indexmap_with_checksum.json"

	// cdnBase 是 Corretto 官方 CDN, resource 路径拼到它后面即完整下载 URL
	cdnBase = "https://corretto.aws"

	// distroName 是发行版标识, 用作目录命名前缀和 CLI 的 distro@ 前缀
	distroName = "corretto"
)

func init() {
	provider.Register(corretto{})
}

// arch 是目标架构 (x64 / aarch64), 决定查询 indexmap 的哪个架构分支。
// 由 Configure 在启动期一次性设置 (经 provider.ConfigureAll 分发), 之后进程内只读。
var arch = app.ArchX64

// errNoWindowsARM64 是 arch=aarch64 时的统一错误。
// Amazon Corretto 官方没有 Windows ARM64 构建 (21/25 官方下载页 Windows 仅 x64),
// 明确报错并给出替代建议, 避免用户误以为装了原生 ARM64 JDK。
var errNoWindowsARM64 = errors.New(
	"Amazon Corretto 暂未提供 Windows ARM64 (aarch64) 构建。\n" +
		"   可改用 temurin 或 microsoft: jvm install 21  /  jvm install microsoft@21")

// checkArch 在目标架构不受支持时返回错误; 目前仅 x64 可用。
func checkArch() error {
	if arch == app.ArchARM64 {
		return errNoWindowsARM64
	}
	return nil
}

// corretto 是 Provider 适配器。嵌入 provider.Base 拿默认实现:
//   - ShortSemver: Corretto 版本号 (21.0.12.8.1) 已是干净形式, 透传即可
//   - ResolveReleaseName: 版本号即 release 标识, 透传即可
type corretto struct {
	provider.Base
}

// Name 返回发行版标识。
func (corretto) Name() string { return distroName }

// DisplayName 返回用户可见的发行版名。
func (corretto) DisplayName() string { return "Amazon Corretto" }

// Configure 实现 provider.Configurable: 设置目标架构 (x64 / aarch64)。
// mirror 参数被忽略: Corretto 直连 CloudFront CDN, 无镜像源。
// 非法 arch 警告并回退 x64; 空值保持默认。
func (corretto) Configure(cfgArch, _ string) {
	if a := strings.TrimSpace(cfgArch); a != "" {
		if norm, ok := app.NormArch(a); ok {
			arch = norm
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  不支持的架构 %q (仅支持 x64 / aarch64), 回退 x64\n", a)
		}
	}
}

// Available 列出所有可安装的大版本 (含 LTS 标记)。
// Corretto 的 version-info.json 把版本分为 LTS / feature / EOL 三类,
// 此处把 LTS 和 feature (当前在维护的非 LTS, 如 26) 都列为可安装, EOL 不列。
func (corretto) Available() ([]app.Release, error) {
	if err := checkArch(); err != nil {
		return nil, err
	}
	info, err := fetchVersionInfo()
	if err != nil {
		return nil, err
	}

	var result []app.Release
	for _, v := range info.SupportedLTSReleases {
		result = append(result, app.Release{Major: v, LTS: true})
	}
	for _, v := range info.SupportedFeatureReleases {
		result = append(result, app.Release{Major: v, LTS: false})
	}
	return result, nil
}

// Resolve 按 VersionSpec 查单个版本, 返回发行版无关的 Asset。
// 两种输入:
//   - 纯大版本号 → 从 indexmap 取该 major 的最新 patch
//   - 完整版本号 → 校验是否为 indexmap 中该 major 的当前版本 (Corretto CDN 只保留最新,
//     旧 patch 直链 403); 不匹配则报错引导
func (c corretto) Resolve(spec app.VersionSpec) (*app.Asset, error) {
	if err := checkArch(); err != nil {
		return nil, err
	}
	v := strings.TrimSpace(spec.Version)

	// 解析大版本号 (无论输入是纯大版本还是完整版本号, 都要先拿到 major)
	majorStr := v
	if i := strings.IndexByte(v, '.'); i >= 0 {
		majorStr = v[:i]
	}
	major, err := app.ParseMajorVersion(majorStr)
	if err != nil {
		return nil, err
	}

	// 从 indexmap 取该 major 当前最新版的 Windows/{arch}/jdk/zip 条目
	entry, err := fetchWindowsJDKEntry(major, arch)
	if err != nil {
		return nil, err
	}
	latestVersion := entry.version() // "21.0.12.8.1"

	// 纯大版本号 → 直接用最新版
	if isPureMajor(v) {
		return entry.toAsset(), nil
	}

	// 完整版本号 → 必须等于当前最新版 (Corretto CDN 不存历史 patch)
	if v != latestVersion {
		return nil, fmt.Errorf(
			"Corretto 仅提供每个大版本的最新版。当前 %d 的最新是 %s, 你输入的 %s 不可用。\n"+
				"用 jvm install corretto@%d 装最新版",
			major, latestVersion, v, major)
	}
	return entry.toAsset(), nil
}

// LatestPatch 返回指定大版本的最新版 (供 jvm available 表格用)。
// 直接复用 fetchWindowsJDKEntry, 比 Resolve 轻量 (不解析用户输入)。
func (c corretto) LatestPatch(major int) (*app.Asset, error) {
	if err := checkArch(); err != nil {
		return nil, err
	}
	entry, err := fetchWindowsJDKEntry(major, arch)
	if err != nil {
		return nil, err
	}
	return entry.toAsset(), nil
}

// ListVersions 返回指定大版本的全部子版本。
// Corretto CDN 每个大版本只保留最新 patch, 故每个 major 仅 1 条。
func (c corretto) ListVersions(major int) ([]*app.Asset, error) {
	if err := checkArch(); err != nil {
		return nil, err
	}
	entry, err := fetchWindowsJDKEntry(major, arch)
	if err != nil {
		return nil, err
	}
	return []*app.Asset{entry.toAsset()}, nil
}

// ---- 以下为包内私有实现 ----

// versionInfo 对应 version-info.json
type versionInfo struct {
	SupportedLTSReleases     []int `json:"supported_lts_releases"`
	SupportedFeatureReleases []int `json:"supported_feature_releases"`
	PreviewReleases          []int `json:"preview_releases"`
	EndOfLifeReleases        []int `json:"end_of_life_releases"`
}

// checksumEntry 对应 indexmap 里的单个资源条目
type checksumEntry struct {
	Checksum    string `json:"checksum"`        // MD5
	ChecksumSHA string `json:"checksum_sha256"` // SHA256
	Resource    string `json:"resource"`        // "/downloads/resources/21.0.12.8.1/amazon-corretto-..."
}

// indexMap 对应 indexmap_with_checksum.json。
// windows 下按架构名 ("x64" / "aarch64" ...) 索引: 用 map 而非硬编码字段,
// 未来官方新增 Windows 架构分支时结构无需变动 (目前官方仅有 x64)。
type indexMap struct {
	Windows map[string]struct { // key 是架构名 ("x64" / "aarch64")
		JDK map[string]struct { // key 是 major ("8" / "11" / "21" ...)
			Zip checksumEntry
		} `json:"jdk"`
	} `json:"windows"`
}

// jdkEntry 是从 indexmap 里解析出的单个 JDK zip 条目, 附带便捷方法
type jdkEntry struct {
	checksumEntry
}

// version 从 resource 路径里提取版本号。
// resource: "/downloads/resources/21.0.12.8.1/amazon-corretto-21.0.12.8.1-windows-x64-jdk.zip"
// 按 "/" 分割后第三段 (index 2) 是版本目录。
func (e jdkEntry) version() string {
	parts := strings.Split(strings.Trim(e.Resource, "/"), "/")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// toAsset 翻译成发行版无关的 *app.Asset
func (e jdkEntry) toAsset() *app.Asset {
	ver := e.version()
	return &app.Asset{
		Semver:      ver,
		Major:       majorOf(ver),
		ZipURL:      cdnBase + e.Resource,
		SHA256:      e.ChecksumSHA,
		ReleaseName: ver,
		Distro:      distroName,
		// MirrorURL 留空: Corretto CDN 国内可直连, 无镜像
	}
}

// fetchVersionInfo 拉取 version-info.json
func fetchVersionInfo() (*versionInfo, error) {
	body, err := app.HTTPGetJSON(versionInfoURL)
	if err != nil {
		return nil, fmt.Errorf("查询 Corretto 可用版本失败: %w", err)
	}
	var info versionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("解析 Corretto 版本信息失败: %w", err)
	}
	return &info, nil
}

// fetchWindowsJDKEntry 从 indexmap 里取 windows/{targetArch}/jdk/{major}/zip 条目
func fetchWindowsJDKEntry(major int, targetArch string) (jdkEntry, error) {
	body, err := app.HTTPGetJSON(indexmapURL)
	if err != nil {
		return jdkEntry{}, fmt.Errorf("查询 Corretto 版本列表失败: %w", err)
	}
	var idx indexMap
	if err := json.Unmarshal(body, &idx); err != nil {
		return jdkEntry{}, fmt.Errorf("解析 Corretto indexmap 失败: %w", err)
	}

	archBranch, ok := idx.Windows[targetArch]
	if !ok {
		return jdkEntry{}, fmt.Errorf("Corretto 暂未提供 Windows/%s 的 JDK 构建", targetArch)
	}
	entry, ok := archBranch.JDK[fmt.Sprintf("%d", major)]
	if !ok {
		return jdkEntry{}, fmt.Errorf("没有找到 Corretto 大版本 %d 的 Windows/%s JDK", major, targetArch)
	}
	if entry.Zip.Resource == "" {
		return jdkEntry{}, fmt.Errorf("Corretto 大版本 %d 的 Windows/%s JDK 资源为空", major, targetArch)
	}
	return jdkEntry{entry.Zip}, nil
}

// isPureMajor 判断是否纯数字大版本号 ("21" / "8"), 含小数点不算。
func isPureMajor(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

// majorOf 从版本串开头解析大版本号 ("21.0.12.8.1" → 21), 解析失败返回 0。
func majorOf(v string) int {
	majorStr := v
	if i := strings.IndexByte(v, '.'); i >= 0 {
		majorStr = v[:i]
	}
	m, _ := app.ParseMajorVersion(majorStr)
	return m
}
