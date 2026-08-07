// Package adoptium 封装 Adoptium (Temurin) API 客户端。
//
// 提供: 查询大版本列表、查最新 GA、按精确版本/release_name 查询、
// 解析官方 CDN 直链和国内镜像 URL。下载本身由 jdk 包完成。
//
// API 文档: https://api.adoptium.net/
// API 走官方 (轻量 JSON, 几 KB); 大文件下载走镜像/CDN, 由调用方处理。
package adoptium

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"jvm/internal/app"
)

const (
	// apiBase 是 Adoptium API 根地址
	apiBase = "https://api.adoptium.net/v3"

	// PageSize 是 FetchAllAssets 单次请求的子版本上限。
	// 当某大版本的 GA 子版本数恰好等于此值, 说明可能被截断 (实际可能更多)。
	// 调用方可据此提示用户用 --major <N> 单独查看完整列表。
	PageSize = 50
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

// AssetInfo 表示一次查询返回的可用 JDK 资源
type AssetInfo struct {
	Semver string // 例如 "21.0.5+11"
	Major  int    // 大版本号 21
	ZipURL string // zip 下载地址
	SHA256 string // 校验和
}

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

// AvailableRelease 表示一个大版本的概要信息
type AvailableRelease struct {
	Major int
	LTS   bool
}

// assetFromRecord 从一条 release 记录里提取当前架构 (Windows/{arch}) 的 zip 资源
// 两个端点 (feature_releases / release_name) 共用这个逻辑
func assetFromRecord(r releaseRecord, hint string) (*AssetInfo, error) {
	if len(r.Binaries) == 0 {
		return nil, fmt.Errorf("%s 没有 Windows/%s 的 zip 包", hint, arch)
	}
	for _, b := range r.Binaries {
		if b.Package.Link != "" {
			return &AssetInfo{
				Semver: r.VersionData.Semver,
				Major:  r.VersionData.Major,
				ZipURL: b.Package.Link,
				SHA256: b.Package.SHA256,
			}, nil
		}
	}
	return nil, fmt.Errorf("%s 没有可下载的 zip 包", hint)
}

// FetchLatestAsset 查询指定大版本的最新 GA 版本, 返回当前架构的 zip 资源。
func FetchLatestAsset(major int) (*AssetInfo, error) {
	u := fmt.Sprintf(
		"%s/assets/feature_releases/%d/ga?architecture=%s&os=windows&image_type=jdk&heap_size=normal&vendor=eclipse",
		apiBase, major, arch,
	)
	body, err := httpGetJSON(u)
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

// FetchAllAssets 查询指定大版本的全部 GA 子版本 (page_size=PageSize 取全量),
// 返回顺序与 API 一致 (最新在前)。供 jvm available -a / --major 使用。
//
// 注意: 当返回条数恰好等于 PageSize, 说明可能被截断 (该大版本实际可能更多)。
// 调用方应用 len(result) == PageSize 自行判断并提示用户。
func FetchAllAssets(major int) ([]*AssetInfo, error) {
	u := fmt.Sprintf(
		"%s/assets/feature_releases/%d/ga?architecture=%s&os=windows&image_type=jdk&heap_size=normal&vendor=eclipse&page_size=%d",
		apiBase, major, arch, PageSize,
	)
	body, err := httpGetJSON(u)
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

	assets := make([]*AssetInfo, 0, len(releases))
	for _, r := range releases {
		a, err := assetFromRecord(r, fmt.Sprintf("大版本 %d", major))
		if err != nil {
			continue // 单条记录缺 Windows/x64 zip 就跳过, 不影响其余
		}
		assets = append(assets, a)
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("大版本 %d 没有可下载的 zip 包", major)
	}
	return assets, nil
}

// FetchAvailableReleases 列出所有可安装的大版本。
func FetchAvailableReleases() ([]AvailableRelease, error) {
	u := apiBase + "/info/available_releases"
	body, err := httpGetJSON(u)
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

	result := make([]AvailableRelease, 0, len(resp.AvailableReleases))
	for _, v := range resp.AvailableReleases {
		result = append(result, AvailableRelease{Major: v, LTS: ltsSet[v]})
	}
	return result, nil
}

// ShortSemver 把 Adoptium API 返回的 semver (如 "21.0.5+11.0.LTS") 规整为
// 简短易读的形式 "21.0.5+11"。没有 build 号时只返回 core 部分。
func ShortSemver(semver string) string {
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

// FetchAssetByReleaseName 用 release_name 端点查指定 release 的资源。
// releaseName 格式: "jdk-21.0.12+8" (带 jdk- 前缀和 +build 后缀)
// 注意: 该端点返回单个对象 (不是数组)
func FetchAssetByReleaseName(releaseName string) (*AssetInfo, error) {
	u := fmt.Sprintf(
		"%s/assets/release_name/eclipse/%s?architecture=%s&os=windows&image_type=jdk&heap_size=normal",
		apiBase, url.PathEscape(releaseName), arch,
	)
	body, err := httpGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询版本 %s 失败: %w", releaseName, err)
	}

	var r releaseRecord // release_name 端点返回单个对象
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析 API 响应失败: %w", err)
	}
	return assetFromRecord(r, releaseName)
}

// ResolveReleaseName 把用户输入 (如 "21.0.12") 解析成完整 release_name (如 "jdk-21.0.12+8")。
// 用户通常不知道 build 号, 所以先列出该 major 的所有 GA release, 找匹配的最新 build。
//
// 输入支持: "21.0.12" / "21.0.12+8" / "jdk-21.0.12+8"
func ResolveReleaseName(version string) (string, error) {
	v := strings.TrimSpace(version)
	if strings.HasPrefix(v, "jdk-") {
		return v, nil // 已经是完整 release_name
	}

	// 解析出 major
	majorStr := v
	if i := strings.IndexAny(v, ".+"); i >= 0 {
		majorStr = v[:i]
	}
	major, err := app.ParseMajorVersion(majorStr)
	if err != nil {
		return "", err
	}

	u := fmt.Sprintf(
		"%s/assets/feature_releases/%d/ga?architecture=%s&os=windows&image_type=jdk&heap_size=normal&vendor=eclipse",
		apiBase, major, arch,
	)
	body, err := httpGetJSON(u)
	if err != nil {
		return "", fmt.Errorf("查询大版本 %d 的 release 列表失败: %w", major, err)
	}
	var releases releaseResponse
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("解析 release 列表失败: %w", err)
	}

	// 要匹配的 "minor.security" (去掉 major 和 build)
	wantSuffix := ""
	if i := strings.IndexByte(v, '.'); i >= 0 {
		wantSuffix = v[i:] // ".0.12" 或 ".0.12+8"
		if plus := strings.IndexByte(wantSuffix, '+'); plus >= 0 {
			wantSuffix = wantSuffix[:plus]
		}
	}

	for _, r := range releases {
		semver := r.VersionData.Semver // "21.0.12+8.0.LTS"
		core := semver
		if plus := strings.IndexByte(core, '+'); plus >= 0 {
			core = core[:plus]
		}
		coreSuffix := ""
		if i := strings.IndexByte(core, '.'); i >= 0 {
			coreSuffix = core[i:]
		}
		if coreSuffix == wantSuffix {
			build := ""
			if plus := strings.IndexByte(semver, '+'); plus >= 0 {
				rest := semver[plus+1:] // "8.0.LTS"
				if dot := strings.IndexByte(rest, '.'); dot >= 0 {
					build = rest[:dot]
				}
			}
			return fmt.Sprintf("jdk-%s+%s", core, build), nil
		}
	}
	return "", fmt.Errorf("没有找到匹配 %s 的 release", version)
}

// ResolveCDNURL 用官方 binary 重定向端点拿到真正的 CDN 直链。
// 该端点会 302 到 release-assets.githubusercontent.com (Azure CDN), 速度快。
// 只跟随一次重定向拿 Location, 不下载内容。
//
// 端点: /v3/binary/latest/{major}/ga/windows/x64/jdk/hotspot/normal/eclipse
func ResolveCDNURL(major int) (string, error) {
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

// MirrorDownloadURL 把官方 GitHub 下载链接转成清华镜像链接。
// 只取末尾文件名, 拼到镜像站对应大版本目录下。
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

// httpGetJSON 发 GET 请求并返回 body
func httpGetJSON(u string) ([]byte, error) {
	if _, err := url.Parse(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", app.UserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := app.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API 返回 %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
