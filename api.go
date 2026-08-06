package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Adoptium API 文档: https://api.adoptium.net/
// API 仍走官方 (轻量 JSON, 几 KB, 不慢);
// 大文件下载默认走国内镜像, 通过 mirrorDownloadURL 转换。
const apiBase = "https://api.adoptium.net/v3"

// downloadMirror 是默认的国内下载镜像 (清华 TUNA, Adoptium 全量镜像)
// 文件结构: {mirror}/{major}/jdk/x64/windows/{filename}
const downloadMirror = "https://mirrors.tuna.tsinghua.edu.cn/Adoptium"

// mirrorDownloadURL 把官方 GitHub 下载链接转成清华镜像链接
// 官方链接形如:
//
//	https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12%2B8/OpenJDK21U-jdk_x64_windows_hotspot_21.0.12_8.zip
//
// 只需要末尾的文件名, 拼到镜像站对应大版本目录下。
func mirrorDownloadURL(officialURL string, major int) string {
	// 取 URL 最后一段作为文件名
	idx := strings.LastIndexByte(officialURL, '/')
	if idx < 0 {
		return officialURL // 异常情况, 原样返回
	}
	filename := officialURL[idx+1:]
	// URL 解码文件名里的 %2B 等 (镜像站要的是真实字符)
	if decoded, err := url.QueryUnescape(filename); err == nil {
		filename = decoded
	}
	return fmt.Sprintf("%s/%d/jdk/x64/windows/%s", downloadMirror, major, filename)
}

// LTS 大版本号集合 (用于 available 命令展示)
var ltsVersions = map[int]bool{8: true, 11: true, 17: true, 21: true}

// assetInfo 表示一次查询返回的可用 JDK 资源
type assetInfo struct {
	Semver    string  // 例如 "21.0.5+11"
	Major     int     // 大版本号 21
	zipURL    string  // zip 下载地址
	sha256    string  // 校验和
	directory string  // 解压后的顶层目录名 (从 zipURL 推断)
}

// releaseRecord 对应 Adoptium API 单条 release 的 JSON 结构 (只取需要的字段)
type releaseRecord struct {
	VersionData struct {
		Semver string `json:"semver"`
		Major  int    `json:"major"`
	} `json:"version_data"`
	Binaries []struct {
		OS         string `json:"os"`
		Arch       string `json:"architecture"`
		ImageType  string `json:"image_type"`
		Package    struct {
			Name   string `json:"name"`
			Link   string `json:"link"`
			SHA256 string `json:"checksum"`
			Size   int64  `json:"size"`
		} `json:"package"`
	} `json:"binaries"`
}

// releaseResponse 对应 feature_releases 端点返回的数组
type releaseResponse []releaseRecord

// httpClient 复用连接, 设了较短超时避免卡死
// httpClient 用于 API 查询 (轻量请求, 60s 足够)
// 大文件下载用专门的 downloader, 不受此超时限制
var httpClient = &http.Client{Timeout: 60 * time.Second}

// downloadClient 用于下载大文件, 不设整体超时 (靠下载过程中的读超时)
var downloadClient = &http.Client{Timeout: 0}

// fetchLatestAsset 查询指定大版本的最新 GA 版本, 返回 Windows/x64 的 zip 资源
//
// API: GET /binary/latest/{feature_version}/ga/windows/x64/jdk/hotspot/normal/eclipse
// 这个端点直接 302 到 zip, 但我们改用 assets 端点拿元数据 (含校验和)
func fetchLatestAsset(major int) (*assetInfo, error) {
	// 用 feature_releases 端点, 拿到带 checksum 的元数据
	u := fmt.Sprintf(
		"%s/assets/feature_releases/%d/ga?architecture=x64&os=windows&image_type=jdk&heap_size=normal&vendor=eclipse",
		apiBase, major,
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

// assetFromRecord 从一条 release 记录里提取 Windows/x64 的 zip 资源
// 两个端点 (feature_releases / release_name) 共用这个逻辑
func assetFromRecord(r releaseRecord, hint string) (*assetInfo, error) {
	if len(r.Binaries) == 0 {
		return nil, fmt.Errorf("%s 没有 Windows/x64 的 zip 包", hint)
	}
	for _, b := range r.Binaries {
		if b.Package.Link != "" {
			pkgName := b.Package.Name
			// zip 解压后的顶层目录名通常是 pkgName 去掉 .zip 后缀
			directory := strings.TrimSuffix(pkgName, ".zip")
			return &assetInfo{
				Semver:    r.VersionData.Semver,
				Major:     r.VersionData.Major,
				zipURL:    b.Package.Link,
				sha256:    b.Package.SHA256,
				directory: directory,
			}, nil
		}
	}
	return nil, fmt.Errorf("%s 没有可下载的 zip 包", hint)
}

// availableRelease 表示一个大版本的概要信息
type availableRelease struct {
	Major    int
	LTS      bool
}

// fetchAvailableReleases 列出所有可安装的大版本
func fetchAvailableReleases() ([]availableRelease, error) {
	u := apiBase + "/info/available_releases"
	body, err := httpGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询可用版本失败: %w", err)
	}

	var resp struct {
		AvailableReleases   []int `json:"available_releases"`
		LTSReleases         []int `json:"most_recent_lts_releases"`
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

	result := make([]availableRelease, 0, len(resp.AvailableReleases))
	for _, v := range resp.AvailableReleases {
		result = append(result, availableRelease{Major: v, LTS: ltsSet[v]})
	}
	return result, nil
}

// fetchAssetByReleaseName 用 release_name 端点查指定 release 的资源
// releaseName 格式: "jdk-21.0.12+8" (带 jdk- 前缀和 +build 后缀)
// 端点: /v3/assets/release_name/{vendor}/{release_name}
// 注意: 该端点返回单个对象 (不是数组)
func fetchAssetByReleaseName(releaseName string) (*assetInfo, error) {
	u := fmt.Sprintf(
		"%s/assets/release_name/eclipse/%s?architecture=x64&os=windows&image_type=jdk&heap_size=normal",
		apiBase, url.PathEscape(releaseName),
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

// resolveReleaseName 把用户输入 (如 "21.0.12") 解析成完整 release_name (如 "jdk-21.0.12+8")
// 思路: 用户通常不知道 build 号 (+8), 所以先列出该 major 的所有 GA release,
// 找到 minor.security 匹配的最新 build。
//
// 输入支持的格式:
//   - "21.0.12"        → 匹配 major=21, minor=0, security=12 的最新 build
//   - "21.0.12+8"      → 精确匹配
//   - "jdk-21.0.12+8"  → 原样使用
func resolveReleaseName(version string) (string, error) {
	v := strings.TrimSpace(version)

	// 已经是完整 release_name, 直接返回
	if strings.HasPrefix(v, "jdk-") {
		return v, nil
	}

	// 解析出 major (用于查 feature_releases)
	// 输入形如 "21.0.12" 或 "21.0.12+8", major 是第一个点之前的部分
	majorStr := v
	if i := strings.IndexAny(v, ".+"); i >= 0 {
		majorStr = v[:i]
	}
	major, err := parseMajorVersion(majorStr)
	if err != nil {
		return "", err
	}

	// 列出该 major 的所有 GA release, 找匹配的
	u := fmt.Sprintf(
		"%s/assets/feature_releases/%d/ga?architecture=x64&os=windows&image_type=jdk&heap_size=normal&vendor=eclipse",
		apiBase, major,
	)
	body, err := httpGetJSON(u)
	if err != nil {
		return "", fmt.Errorf("查询大版本 %d 的 release 列表失败: %w", major, err)
	}
	var releases releaseResponse
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("解析 release 列表失败: %w", err)
	}

	// 把用户输入标准化为要匹配的 "minor.security" (去掉 major 和 build)
	// "21.0.12" → 要匹配 ".0.12"; "21.0.12+8" → 要匹配 ".0.12"
	wantSuffix := ""
	if i := strings.IndexByte(v, '.'); i >= 0 {
		wantSuffix = v[i:] // ".0.12" 或 ".0.12+8"
		// 去掉用户可能带的 build 号, 只匹配到 security
		if plus := strings.IndexByte(wantSuffix, '+'); plus >= 0 {
			wantSuffix = wantSuffix[:plus]
		}
	}

	for _, r := range releases {
		// r.VersionData.Semver 形如 "21.0.12+8.0.LTS", 从中取 release_name 风格
		// 直接用 semver 去掉 optional 后缀比较
		semver := r.VersionData.Semver // "21.0.12+8.0.LTS"
		// 提取 "minor.security" 部分: 去掉 major 前缀, 去掉 build 后
		core := semver
		// 去掉 + 后面的内容
		if plus := strings.IndexByte(core, '+'); plus >= 0 {
			core = core[:plus]
		}
		// core 现在是 "21.0.12", 去掉 major 前缀得到 ".0.12"
		coreSuffix := ""
		if i := strings.IndexByte(core, '.'); i >= 0 {
			coreSuffix = core[i:]
		}
		if coreSuffix == wantSuffix {
			// 匹配到了, 构造完整 release_name
			// build 号从 semver 提取: "21.0.12+8.0.LTS" → build=8
			build := ""
			if plus := strings.IndexByte(semver, '+'); plus >= 0 {
				rest := semver[plus+1:] // "8.0.LTS"
				if dot := strings.IndexByte(rest, '.'); dot >= 0 {
					build = rest[:dot] // "8"
				}
			}
			return fmt.Sprintf("jdk-%s+%s", core, build), nil
		}
	}
	return "", fmt.Errorf("没有找到匹配 %s 的 release", version)
}

// resolveCDNURL 用官方 binary 重定向端点拿到真正的 CDN 直链。
// 这个端点会 302 到 release-assets.githubusercontent.com (Azure CDN),
// 速度远快于直接访问 github.com releases 链接。
// 我们只跟随一次重定向拿地址, 不下载内容。
//
// 端点格式: /v3/binary/latest/{major}/ga/windows/x64/jdk/hotspot/normal/eclipse
func resolveCDNURL(major int) (string, error) {
	endpoint := fmt.Sprintf(
		"%s/binary/latest/%d/ga/windows/x64/jdk/hotspot/normal/eclipse",
		apiBase, major,
	)
	// 用一个不跟随重定向的 client, 只看 Location 头
	noRedirectClient := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // 不自动跟随
		},
	}
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent())

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 期望 302; Location 就是 CDN 直链
	if loc := resp.Header.Get("Location"); loc != "" {
		return loc, nil
	}
	// 极端情况: 没重定向, 直接返回端点本身 (可能就是直链)
	return endpoint, nil
}

// httpGetJSON 发 GET 请求并返回 body
func httpGetJSON(u string) ([]byte, error) {
	// 校验 URL, 防止无效输入
	if _, err := url.Parse(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	// Adoptium 要求带 User-Agent, 否则可能被拒
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API 返回 %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
