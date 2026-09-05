// ea.go 是 Temurin 早期访问 (Early Access) 变体 provider, 与 GA temurin 同包:
// 复用 Adoptium API 的请求 plumbing (apiBase/arch/releaseRecord/assetFromRecord
// 的兄弟实现), 仅端点与镜像策略不同。
//
// 与 GA 的差异:
//   - 端点: feature_releases 的路径参数用 ea (GA 用 ga);
//     /v3/assets/latest 不支持 release_type, 不能用于 EA。
//   - 大版本列表: /info/release_names?release_type=ea 拿全部 EA release_name,
//     客户端归并出大版本 —— EA 大版本随时间漂移 (27 GA 后是 28/29...), 不能写死。
//   - 镜像: 清华镜像不同步 EA 构建, 一律直连 GitHub release 资产 (MirrorURL 留空)。
//   - release_name 形如 jdk-28+14-ea-beta: feature 版内无 patch 段,
//     build 号用 "+" 分隔, 固定 -ea-beta 后缀。
//
// 目录命名: temurin-ea-{版本} (如 temurin-ea-28+14-ea-beta)。distro 名带连字符,
// 依赖 junction.SplitDistro 的多段字母前缀贪心拆分。
package temurin

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"jvm/internal/app"
	"jvm/internal/provider"
)

// eaDistroName 是 EA 变体的发行版标识 (带连字符, 区别于 GA 的 "temurin")。
const eaDistroName = "temurin-ea"

func init() {
	provider.Register(ea{})
}

// ea 是 Temurin EA 适配器。嵌入 GA 的 temurin 拿默认实现与 Configure
// (共享包级 arch/mirror 状态; mirror 对 EA 无效 —— 资产不走镜像),
// override 端点相关的五个方法与 Name/DisplayName。
type ea struct {
	temurin
}

// Name 返回发行版标识。
func (ea) Name() string { return eaDistroName }

// DisplayName 返回用户可见的发行版名。
func (ea) DisplayName() string { return "Temurin (Adoptium) 早期访问" }

// Available 列出当前存在 EA 构建的大版本 (从 release_names 归并, 降序)。
// EA 只存在于尚未 GA 的大版本上 (已 GA 的查 EA 返回空), LTS 一律 false。
func (ea) Available() ([]app.Release, error) {
	u := fmt.Sprintf(
		"%s/info/release_names?architecture=%s&image_type=jdk&os=windows&release_type=ea&page_size=%d",
		apiBase, arch, pageLimit,
	)
	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询 EA 可用版本失败: %w", err)
	}
	majors, err := parseReleaseNamesMajors(body)
	if err != nil {
		return nil, err
	}
	if len(majors) == 0 {
		return nil, fmt.Errorf("当前没有 EA 构建 (windows/%s)", arch)
	}
	releases := make([]app.Release, 0, len(majors))
	for _, m := range majors {
		releases = append(releases, app.Release{Major: m, LTS: false})
	}
	return releases, nil
}

// Resolve 按 VersionSpec 查单个 EA 版本:
//   - 纯大版本号 → 该大版本最新 EA build
//   - 完整版本号 (如 28+14-ea-beta) → feature_releases 列表内按 release_name 精确匹配
//     (release_name 端点对 EA 名称的支持未验证, 列表匹配最稳)
func (e ea) Resolve(spec app.VersionSpec) (*app.Asset, error) {
	v := strings.TrimSpace(spec.Version)

	// 纯大版本号 → 最新 EA build; 其余按完整版本号在列表内精确匹配
	if isPureNumber(v) {
		major, err := app.ParseMajorVersion(v)
		if err != nil {
			return nil, err
		}
		return e.LatestPatch(major)
	}

	releaseName, err := e.ResolveReleaseName(v)
	if err != nil {
		return nil, err
	}
	records, err := fetchEAReleases(majorOfReleaseName(releaseName), pageLimit)
	if err != nil {
		return nil, err
	}
	want := strings.TrimPrefix(releaseName, "jdk-") // releaseNameOf 产出的短形式
	for _, r := range records {
		if releaseNameOf(r) == want {
			return eaAssetFromRecord(r, releaseName)
		}
	}
	return nil, fmt.Errorf("没有找到 %s 的 EA 版本 %q (windows/%s)", eaDistroName, v, arch)
}

// LatestPatch 返回指定大版本的最新 EA build (feature_releases 第一条)。
func (ea) LatestPatch(major int) (*app.Asset, error) {
	records, err := fetchEAReleases(major, 1)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("没有找到大版本 %d 的 EA 构建 (windows/%s, 已 GA 的版本无 EA)", major, arch)
	}
	return eaAssetFromRecord(records[0], fmt.Sprintf("大版本 %d", major))
}

// ListVersions 返回指定大版本的全部 EA build (最新在前)。
func (ea) ListVersions(major int) ([]*app.Asset, error) {
	records, err := fetchEAReleases(major, pageLimit)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("没有找到大版本 %d 的 EA 构建 (windows/%s)", major, arch)
	}
	assets := make([]*app.Asset, 0, len(records))
	for _, r := range records {
		if a, err := eaAssetFromRecord(r, fmt.Sprintf("大版本 %d", major)); err == nil {
			assets = append(assets, a)
		}
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("大版本 %d (windows/%s) 没有可下载的 zip 包", major, arch)
	}
	return assets, nil
}

// ResolveReleaseName 把用户输入规整为 EA release_name。
// EA 版本形如 "28+14-ea-beta" (无 patch 段但必有 +build), 补 "jdk-" 前缀
// 后即 release_name; 纯大版本号走不到这里 (Resolve 已分流)。
func (ea) ResolveReleaseName(version string) (string, error) {
	v := strings.TrimSpace(version)
	if strings.HasPrefix(v, "jdk-") {
		return v, nil
	}
	if !strings.Contains(v, "+") {
		return "", fmt.Errorf("EA 版本号 %q 缺少 build 号 (形如 28+14-ea-beta)。用大版本号 (如 28) 取最新", v)
	}
	return "jdk-" + v, nil
}

// ---- 以下为包内私有实现 ----

// fetchEAReleases 查询指定大版本的 EA release 列表 (sort_order=DESC, 最新在前)。
func fetchEAReleases(major, pageSize int) (releaseResponse, error) {
	u := fmt.Sprintf(
		"%s/assets/feature_releases/%d/ea?architecture=%s&os=windows&image_type=jdk&heap_size=normal&vendor=eclipse&page_size=%d&sort_order=DESC",
		apiBase, major, arch, pageSize,
	)
	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询大版本 %d 的 EA 版本 (windows/%s) 失败: %w", major, arch, err)
	}
	var releases releaseResponse
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 API 响应失败: %w", err)
	}
	return releases, nil
}

// eaAssetFromRecord 从一条 EA release 记录构造 Asset。与 GA 的
// assetFromRecord 差异: Distro 为 temurin-ea, 且不填 MirrorURL
// (清华镜像不同步 EA, 直连 GitHub release 资产)。
func eaAssetFromRecord(r releaseRecord, hint string) (*app.Asset, error) {
	if len(r.Binaries) == 0 {
		return nil, fmt.Errorf("%s 没有 Windows/%s 的 zip 包", hint, arch)
	}
	for _, b := range r.Binaries {
		if b.Package.Link != "" {
			return &app.Asset{
				Semver:      r.VersionData.Semver,
				Major:       r.VersionData.Major,
				ZipURL:      b.Package.Link,
				Checksum:    b.Package.SHA256,
				Distro:      eaDistroName,
				ReleaseName: releaseNameOf(r),
			}, nil
		}
	}
	return nil, fmt.Errorf("%s 没有可下载的 zip 包", hint)
}

// parseReleaseNamesMajors 从 /info/release_names?release_type=ea 的响应里
// 归并出大版本列表 (去重、降序)。release_name 形如 "jdk-28+14-ea-beta",
// 大版本取 "jdk-" 前缀后的开头连续数字。纯函数, 便于表测。
func parseReleaseNamesMajors(body []byte) ([]int, error) {
	var resp struct {
		Releases []string `json:"releases"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 EA 版本列表响应失败: %w", err)
	}
	seen := map[int]bool{}
	var majors []int
	for _, name := range resp.Releases {
		m := majorOfReleaseName(name)
		if m > 0 && !seen[m] {
			seen[m] = true
			majors = append(majors, m)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(majors)))
	return majors, nil
}

// majorOfReleaseName 从 release_name (如 "jdk-28+14-ea-beta") 解析大版本,
// 失败返回 0。纯函数。
func majorOfReleaseName(name string) int {
	name = strings.TrimPrefix(strings.TrimSpace(name), "jdk-")
	end := strings.IndexFunc(name, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(name)
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(name[:end])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// isPureNumber 判断 s 是否全为数字 (纯大版本号输入)。
func isPureNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
