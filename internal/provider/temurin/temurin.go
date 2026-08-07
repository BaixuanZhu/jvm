// Package temurin 是 Eclipse Temurin (Adoptium) 发行版的 provider 适配器。
//
// 把 Adoptium API 的返回统一翻译成 provider 接口的 Asset 契约,
// 消化 "查询大版本/最新 GA/精确版本/release_name 解析/镜像 URL 拼接/
// CDN 直链解析" 等 Temurin 专属细节。上层 (cmd/jdk) 只认 provider.Provider。
//
// API 文档: https://api.adoptium.net/
// 元数据走官方 API (轻量 JSON, 几 KB); 大文件下载走镜像/CDN, 由 jdk 包处理。
package temurin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"jvm/internal/app"
	"jvm/internal/provider"
)

const (
	// apiBase 是 Adoptium API 根地址
	apiBase = "https://api.adoptium.net/v3"

	// pageLimit 是单次请求拉取的子版本条数。
	// Adoptium API 单页硬上限 50, 但每个大版本的 GA release 数量远低于此
	// (JDK 8 历史最长也才 20 条), 单次请求即拿全, 无需翻页。
	pageLimit = 50

	// distroName 是发行版标识, 用作目录命名前缀和 CLI 的 distro@ 前缀。
	distroName = "temurin"
)

// 以下两项是可配置的包级状态, 默认值保持原有行为 (清华镜像 + x64 架构)。
// 由 Configure() 在程序启动期一次性设置 (见 main.go), 之后进程内只读。
// 沿用 paths.Root / app.HTTPClient 的"包级共享状态"惯例。
var (
	// mirror 是默认的国内下载镜像 (清华 TUNA, Adoptium 全量镜像)
	// 文件结构: {mirror}/{major}/jdk/{arch}/windows/{filename}
	mirror = "https://mirrors.tuna.tsinghua.edu.cn/Adoptium"

	// arch 是目标架构, 用于 API 查询和镜像 URL 拼接
	arch = "x64"
)

func init() {
	provider.Register(temurin{})
}

// temurin 是 Provider 适配器。嵌入 provider.Base 拿默认实现,
// override 自己真正不同的方法 (ShortSemver/PageSize/ResolveReleaseName)。
type temurin struct {
	provider.Base
}

// Configure 在程序启动时设置架构和镜像源。非法 arch 回退 x64 并打印警告。
// 供 main 包加载完配置后调用; 不调用时使用默认值 (清华镜像 + x64)。
func Configure(cfgArch, cfgMirror string) {
	a := strings.TrimSpace(cfgArch)
	switch a {
	case "x64", "aarch64":
		arch = a
	case "":
		// 空值保持默认
	default:
		fmt.Fprintf(os.Stderr, "⚠️  不支持的架构 %q (仅支持 x64 / aarch64), 回退 x64\n", a)
	}
	if m := strings.TrimSpace(cfgMirror); m != "" {
		mirror = m
	}
}

// Name 返回发行版标识。
func (temurin) Name() string { return distroName }

// DisplayName 返回用户可见的发行版名。
func (temurin) DisplayName() string { return "Temurin (Adoptium)" }

// Available 列出所有可安装的大版本 (Adoptium /info/available_releases)。
func (temurin) Available() ([]app.Release, error) {
	u := apiBase + "/info/available_releases"
	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询可用版本失败: %w", err)
	}

	var resp struct {
		AvailableReleases    []int `json:"available_releases"`
		LTSReleases          []int `json:"most_recent_lts_releases"`
		AvailableLTSReleases []int `json:"available_lts_releases"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析可用版本响应失败: %w", err)
	}

	ltsSet := map[int]bool{}
	for _, v := range resp.AvailableLTSReleases {
		ltsSet[v] = true
	}
	for _, v := range resp.LTSReleases {
		ltsSet[v] = true
	}

	result := make([]app.Release, 0, len(resp.AvailableReleases))
	for _, v := range resp.AvailableReleases {
		result = append(result, app.Release{Major: v, LTS: ltsSet[v]})
	}
	return result, nil
}

// Resolve 按 VersionSpec 查单个版本, 返回发行版无关的 Asset。
// 内部按 spec.Version 格式分流:
//   - 含 "." 或 "jdk-" 前缀 → 精确版本/release_name 查询
//   - 纯大版本号           → 该大版本最新 GA, 并内化 CDN 直链解析
//
// CDN 直链解析 (ResolveCDNURL) 仅在"大版本取最新"场景生效:
// /binary/latest/{major} 端点只能拿到最新版的 CDN 直链, 精确版本会下错。
// (原 jdk.go:86 的 isLatestMajor 语义内化至此。)
func (t temurin) Resolve(spec app.VersionSpec) (*app.Asset, error) {
	v := strings.TrimSpace(spec.Version)

	var asset *app.Asset
	var err error
	isLatestMajor := false
	if strings.ContainsAny(v, ".") || strings.HasPrefix(v, "jdk-") {
		var releaseName string
		releaseName, err = t.ResolveReleaseName(v)
		if err != nil {
			return nil, err
		}
		asset, err = fetchAssetByReleaseName(releaseName)
	} else {
		var major int
		major, err = app.ParseMajorVersion(v)
		if err != nil {
			return nil, err
		}
		asset, err = fetchLatestAsset(major)
		isLatestMajor = true
	}
	if err != nil {
		return nil, err
	}

	// 规整 ReleaseName (用作目录命名, 不含 distro 前缀)
	asset.ReleaseName = t.ShortSemver(asset.Semver)

	// 内化 CDN 直链解析: 仅"大版本取最新"场景, 用 /binary/latest 拿 Azure CDN 直链提速。
	// 解析失败时静默回退到 asset.ZipURL (官方 GitHub releases 链接)。
	if isLatestMajor && asset.Major > 0 {
		if cdn, e := t.ResolveCDNURL(asset.Major); e == nil && cdn != "" {
			asset.ZipURL = cdn
		}
	}

	return asset, nil
}

// ResolveCDNURL 用官方 binary 重定向端点拿到真正的 CDN 直链 (Azure CDN, 速度快)。
// 该端点会 302 到 release-assets.githubusercontent.com。只跟随一次重定向拿
// Location, 不下载内容。
//
// 端点: /v3/binary/latest/{major}/ga/windows/{arch}/jdk/hotspot/normal/eclipse
//
// 实现 provider.CDNResolver 可选接口。
func (temurin) ResolveCDNURL(major int) (string, error) {
	endpoint := fmt.Sprintf(
		"%s/binary/latest/%d/ga/windows/%s/jdk/hotspot/normal/eclipse",
		apiBase, major, arch,
	)
	noRedirectClient := &http.Client{
		Timeout: 30 * 1e9, // 30s
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // 不自动跟随
		},
	}
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", app.UserAgent())

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if loc := resp.Header.Get("Location"); loc != "" {
		return loc, nil
	}
	return endpoint, nil
}

// ListVersions 查询指定大版本的全部 GA 子版本, 返回顺序与 API 一致 (最新在前)。
// 供 jvm available -a / --major 使用。
//
// 单次请求即拿全: Adoptium API 单页硬上限 pageLimit (50), 但每个大版本的 GA
// release 数量远低于此 (JDK 8 历史最长也才 20 条), 无需翻页。page 参数不可用
// (page>=1 返回 404), 故只发一次请求。
func (t temurin) ListVersions(major int) ([]*app.Asset, error) {
	u := fmt.Sprintf(
		"%s/assets/feature_releases/%d/ga?architecture=%s&os=windows&image_type=jdk&heap_size=normal&vendor=eclipse&page_size=%d",
		apiBase, major, arch, pageLimit,
	)
	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询版本失败: %w", err)
	}

	var releases releaseResponse
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 API 响应失败: %w", err)
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("没有找到大版本 %d 的 GA 版本", major)
	}

	assets := make([]*app.Asset, 0, len(releases))
	for _, r := range releases {
		a, err := assetFromRecord(r, fmt.Sprintf("大版本 %d", major))
		if err != nil {
			continue // 单条记录缺 Windows/x64 zip 就跳过, 不影响其余
		}
		a.ReleaseName = t.ShortSemver(a.Semver)
		assets = append(assets, a)
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("大版本 %d 没有可下载的 zip 包", major)
	}
	return assets, nil
}

// LatestPatch 返回指定大版本的最新 GA 版本 (供 jvm available 表格用)。
// 比 Resolve 轻量: 只取 feature_releases 端点第一页第一条, 不解析用户输入、
// 不内化 CDN (表格只展示版本号, 不下载)。
func (t temurin) LatestPatch(major int) (*app.Asset, error) {
	asset, err := fetchLatestAsset(major)
	if err != nil {
		return nil, err
	}
	asset.ReleaseName = t.ShortSemver(asset.Semver)
	return asset, nil
}

// ShortSemver 把 Adoptium API 返回的 semver (如 "21.0.5+11.0.LTS") 规整为
// 简短易读的形式 "21.0.5+11"。没有 build 号时只返回 core 部分。
// override Base 默认 (透传), 因为 Temurin 的 semver 带 ".LTS" 后缀需剥离。
func (temurin) ShortSemver(semver string) string {
	core := semver
	build := ""
	if plus := strings.IndexByte(semver, '+'); plus >= 0 {
		core = semver[:plus]
		rest := semver[plus+1:] // 例如 "11.0.LTS"
		if dot := strings.IndexByte(rest, '.'); dot >= 0 {
			build = rest[:dot]
		} else {
			build = rest
		}
	}
	if build != "" {
		return core + "+" + build
	}
	return core
}

// ResolveReleaseName 把用户输入的完整版本号标准化成 Adoptium release_name。
// 砍掉半截 core 匹配后, 用户输完整版本号 (含 build 号) 或纯大版本号两种形式:
//   - "jdk-21.0.12+8" → 透传 (已是完整 release_name)
//   - "21.0.12+8"     → 补 "jdk-" 前缀
//
// 无 build 号的半截形式 (如 "21.0.12") 报错 —— 不同发行版本号格式不一
// (Temurin 21.0.5+11 / Corretto 21.0.12.8.1), 半截形式语义模糊; 要装指定
// patch 就输完整版本号, 或用大版本号取最新。
//
// override Base 默认 (透传): Temurin release_name 带 "jdk-" 前缀需补上。
// 不再查 API 反推 build —— install 精确版本路径因此少一次网络请求。
func (t temurin) ResolveReleaseName(version string) (string, error) {
	v := strings.TrimSpace(version)
	if strings.HasPrefix(v, "jdk-") {
		return v, nil // 已是完整 release_name
	}
	// 完整版本号必须含 build 号 (X.Y.Z+N)
	if !strings.Contains(v, "+") {
		return "", fmt.Errorf("版本号 %q 缺少 build 号。用大版本号 (如 21) 取最新, 或完整版本号 (如 21.0.12+8)", v)
	}
	return "jdk-" + v, nil
}

// MirrorDownloadURL 把官方 GitHub 下载链接转成清华镜像链接。
// 只取末尾文件名, 拼到镜像站对应大版本目录下。
// 文件结构: {mirror}/{major}/jdk/{arch}/windows/{filename}
func MirrorDownloadURL(officialURL string, major int) string {
	idx := strings.LastIndexByte(officialURL, '/')
	if idx < 0 {
		return officialURL
	}
	filename := officialURL[idx+1:]
	if decoded, err := url.QueryUnescape(filename); err == nil {
		filename = decoded
	}
	return fmt.Sprintf("%s/%d/jdk/%s/windows/%s", mirror, major, arch, filename)
}

// ---- 以下为包内私有实现 (从旧 adoptium 包原样搬迁, 仅返回类型改为 *app.Asset) ----

// releaseRecord 对应 Adoptium API 单条 release 的 JSON 结构 (只取需要的字段)
type releaseRecord struct {
	VersionData struct {
		Semver string `json:"semver"`
		Major  int    `json:"major"`
	} `json:"version_data"`
	Binaries []struct {
		Package struct {
			Name   string `json:"name"`
			Link   string `json:"link"`
			SHA256 string `json:"checksum"`
			Size   int64  `json:"size"`
		} `json:"package"`
	} `json:"binaries"`
}

// releaseResponse 对应 feature_releases 端点返回的数组
type releaseResponse []releaseRecord

// assetFromRecord 从一条 release 记录里提取当前架构 (Windows/{arch}) 的 zip 资源,
// 翻译成发行版无关的 *app.Asset (填齐 Distro/MirrorURL/ReleaseName 外的其他字段;
// ReleaseName 由调用方在拿到 Semver 后用 ShortSemver 补填)。
// 两个端点 (feature_releases / release_name) 共用这个逻辑
func assetFromRecord(r releaseRecord, hint string) (*app.Asset, error) {
	if len(r.Binaries) == 0 {
		return nil, fmt.Errorf("%s 没有 Windows/%s 的 zip 包", hint, arch)
	}
	for _, b := range r.Binaries {
		if b.Package.Link != "" {
			return &app.Asset{
				Semver:    r.VersionData.Semver,
				Major:     r.VersionData.Major,
				ZipURL:    b.Package.Link,
				SHA256:    b.Package.SHA256,
				Distro:    distroName,
				MirrorURL: MirrorDownloadURL(b.Package.Link, r.VersionData.Major),
			}, nil
		}
	}
	return nil, fmt.Errorf("%s 没有可下载的 zip 包", hint)
}

// fetchLatestAsset 查询指定大版本的最新 GA 版本, 返回当前架构的 zip 资源。
func fetchLatestAsset(major int) (*app.Asset, error) {
	u := fmt.Sprintf(
		"%s/assets/feature_releases/%d/ga?architecture=%s&os=windows&image_type=jdk&heap_size=normal&vendor=eclipse",
		apiBase, major, arch,
	)
	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询版本失败: %w", err)
	}

	var releases releaseResponse
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 API 响应失败: %w", err)
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("没有找到大版本 %d 的 GA 版本", major)
	}
	return assetFromRecord(releases[0], fmt.Sprintf("大版本 %d", major))
}

// fetchAssetByReleaseName 用 release_name 端点查指定 release 的资源。
// releaseName 格式: "jdk-21.0.12+8" (带 jdk- 前缀和 +build 后缀)
// 注意: 该端点返回单个对象 (不是数组)
func fetchAssetByReleaseName(releaseName string) (*app.Asset, error) {
	u := fmt.Sprintf(
		"%s/assets/release_name/eclipse/%s?architecture=%s&os=windows&image_type=jdk&heap_size=normal",
		apiBase, url.PathEscape(releaseName), arch,
	)
	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询版本 %s 失败: %w", releaseName, err)
	}

	var r releaseRecord // release_name 端点返回单个对象
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析 API 响应失败: %w", err)
	}
	return assetFromRecord(r, releaseName)
}
