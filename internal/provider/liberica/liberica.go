// Package liberica 是 BellSoft Liberica JDK 发行版的 provider 适配器。
//
// 数据源: BellSoft Product Discovery API (https://api.bell-sw.com/v1/liberica/releases)。
// 该 API 无"按 major 过滤"参数 (feature-version 已实测无效 → 400), 故查询策略为:
// 一次请求该架构的全部 GA JDK zip (约 160+ 条, 覆盖 feature 8~26), 客户端按
// featureVersion / latestInFeatureVersion / version 字段过滤。
//
// 校验: API 原生仅提供 sha1 (无 sha256, 亦无旁路 sha256 文件), 故 Liberica 用 SHA1
// 做下载完整性校验 (Asset.ChecksumAlgo = "sha1")。这是 BellSoft 官方提供的唯一校验值,
// 防损坏/截断足够; 非发行方防篡改签名 (那需 GPG, 不在 JDK 下载完整性范围内)。
//
// 下载直链: 不用 API 返回的 GitHub 域 downloadUrl (国内访问 GitHub 不稳), 改自拼
// BellSoft 官方 CDN https://download.bell-sw.com/java/{version}/{filename} (国内可直连,
// 与 GitHub release 同源同包)。
//
// 目标架构 (x64 / aarch64) 由 Configure 在启动期设置; API 参数 x64 用 arch=x86&bitness=64,
// aarch64 用 arch=arm&bitness=64 (文件名后缀分别是 windows-amd64 / windows-aarch64)。
package liberica

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"jvm/internal/app"
	"jvm/internal/provider"
)

const (
	// apiBase 是 BellSoft Liberica releases 端点
	apiBase = "https://api.bell-sw.com/v1/liberica/releases"

	// downloadBase 是 BellSoft 官方 CDN (稳定, 替代 API 返回的 GitHub 域)
	downloadBase = "https://download.bell-sw.com/java"

	// distroName 是发行版标识 (全字母, 满足 junction.splitDistro 的校验)
	distroName = "liberica"
)

// arch 是目标架构 (app.ArchX64 / app.ArchARM64)。
// 由 Configure 在启动期一次性设置 (经 provider.ConfigureAll 分发), 之后进程内只读。
var arch = app.ArchX64

// activeVersions 是 Available 展示用的活跃 LTS 版本。Liberica 实际覆盖更广 (8~26),
// 此处只列主流 LTS; Resolve 不受限于此, 任何 API 有的 major 都能装。
var activeVersions = []app.Release{
	{Major: 11, LTS: true},
	{Major: 17, LTS: true},
	{Major: 21, LTS: true},
	{Major: 25, LTS: true},
}

func init() {
	provider.Register(liberica{})
}

// liberica 是 Provider 适配器。嵌入 provider.Base 拿默认实现:
//   - ShortSemver: Liberica 版本号 (21.0.5+11) 已是干净形式, 透传即可
//   - ResolveReleaseName: 版本号即 release 标识, 透传即可
type liberica struct {
	provider.Base
}

// Name 返回发行版标识。
func (liberica) Name() string { return distroName }

// DisplayName 返回用户可见的发行版名。
func (liberica) DisplayName() string { return "BellSoft Liberica JDK" }

// Configure 实现 provider.Configurable: 设置目标架构。
// mirror 参数被忽略: Liberica 直连 download.bell-sw.com 官方 CDN, 无第三方镜像。
func (liberica) Configure(cfgArch, _ string) {
	if a := strings.TrimSpace(cfgArch); a != "" {
		if norm, ok := app.NormArch(a); ok {
			arch = norm
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  不支持的架构 %q (仅支持 x64 / aarch64), 回退 x64\n", a)
		}
	}
}

// Available 列出展示用的活跃 LTS 版本。
func (liberica) Available() ([]app.Release, error) {
	out := make([]app.Release, len(activeVersions))
	copy(out, activeVersions)
	return out, nil
}

// Resolve 按 VersionSpec 查单个版本。纯大版本号 → 最新 GA; 含 patch 的完整版本号 → 精确匹配。
func (l liberica) Resolve(spec app.VersionSpec) (*app.Asset, error) {
	v := strings.TrimSpace(spec.Version)
	majorStr := v
	if i := strings.IndexByte(v, '.'); i >= 0 {
		majorStr = v[:i]
	}
	major, err := app.ParseMajorVersion(majorStr)
	if err != nil {
		return nil, err
	}
	all, err := fetchReleases()
	if err != nil {
		return nil, err
	}
	if isPureMajor(v) {
		matched := filterLatestInFeature(all, major)
		if len(matched) == 0 {
			return nil, fmt.Errorf("Liberica 没有 windows/%s 的 JDK %d GA 包", arch, major)
		}
		return buildAsset(matched[0], major), nil
	}
	for _, r := range all {
		if r.GA && r.Version == v {
			return buildAsset(r, major), nil
		}
	}
	return nil, fmt.Errorf("Liberica 没有 windows/%s 的 %s 版本", arch, v)
}

// LatestPatch 返回指定大版本的最新 GA 版本。
func (l liberica) LatestPatch(major int) (*app.Asset, error) {
	all, err := fetchReleases()
	if err != nil {
		return nil, err
	}
	matched := filterLatestInFeature(all, major)
	if len(matched) == 0 {
		return nil, fmt.Errorf("Liberica 没有 windows/%s 的 JDK %d GA 包", arch, major)
	}
	return buildAsset(matched[0], major), nil
}

// ListVersions 返回指定大版本的全部 GA patch (按版本降序)。
// BellSoft API 一次返回 sha1, 故此处 Asset 含完整校验信息 (区别于 Zulu 的展示用空校验)。
func (l liberica) ListVersions(major int) ([]*app.Asset, error) {
	all, err := fetchReleases()
	if err != nil {
		return nil, err
	}
	var matched []libericaRelease
	for _, r := range all {
		if r.FeatureVersion == major && r.GA {
			matched = append(matched, r)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return app.CompareVersions(matched[i].Version, matched[j].Version) > 0
	})
	out := make([]*app.Asset, 0, len(matched))
	for _, r := range matched {
		out = append(out, buildAsset(r, major))
	}
	return out, nil
}

// ---- 以下为包内私有实现 ----

// libericaRelease 是 BellSoft API 返回的单条 release 对象。
// 只声明消费的字段, 其余 (EOL/TCK/updateType 等) 忽略。
type libericaRelease struct {
	Version                string `json:"version"`
	FeatureVersion         int    `json:"featureVersion"`
	BuildVersion           int    `json:"buildVersion"`
	GA                     bool   `json:"GA"`
	LTS                    bool   `json:"LTS"`
	Latest                 bool   `json:"latest"`
	LatestInFeatureVersion bool   `json:"latestInFeatureVersion"`
	LatestLTS              bool   `json:"latestLTS"`
	BundleType             string `json:"bundleType"`
	PackageType            string `json:"packageType"`
	FX                     bool   `json:"FX"`
	SHA1                   string `json:"sha1"`
	Filename               string `json:"filename"`
	DownloadURL            string `json:"downloadUrl"`
	Size                   int64  `json:"size"`
	Architecture           string `json:"architecture"`
	Bitness                int    `json:"bitness"`
	OS                     string `json:"os"`
}

// apiArchParams 把内部架构值翻译成 BellSoft API 的 arch / bitness 参数。
// x64 → (x86, 64); aarch64 → (arm, 64); 其余 (含未知) 一律 x86/64。
func apiArchParams(a string) (apiArch, apiBitness string) {
	if a == app.ArchARM64 {
		return "arm", "64"
	}
	return "x86", "64"
}

// isPureMajor 判断输入是否纯大版本号 (不含 ".", 即没有 patch)。
func isPureMajor(v string) bool {
	return !strings.ContainsRune(v, '.')
}

// downloadURL 自拼 BellSoft 官方 CDN 直链 (比 API 返回的 GitHub 域稳定)。
func downloadURL(version, filename string) string {
	return fmt.Sprintf("%s/%s/%s", downloadBase, version, filename)
}

// buildAsset 翻译成发行版无关的 *app.Asset。校验用官方 sha1。
func buildAsset(r libericaRelease, major int) *app.Asset {
	return &app.Asset{
		Semver:       r.Version,
		Major:        major,
		ZipURL:       downloadURL(r.Version, r.Filename),
		Checksum:     r.SHA1,
		ChecksumAlgo: "sha1",
		ReleaseName:  r.Version,
		Distro:       distroName,
		// MirrorURL 留空: 直连 download.bell-sw.com
	}
}

// filterLatestInFeature 过滤出指定 feature 的最新 GA patch。
// BellSoft 每个 feature 恰好一条 latestInFeatureVersion=true (GA 中)。
func filterLatestInFeature(all []libericaRelease, major int) []libericaRelease {
	var out []libericaRelease
	for _, r := range all {
		if r.FeatureVersion == major && r.LatestInFeatureVersion && r.GA {
			out = append(out, r)
		}
	}
	return out
}

// fetchReleases 请求该架构的全部 JDK zip release (含 GA 与 EA), 客户端按需过滤。
func fetchReleases() ([]libericaRelease, error) {
	a, b := apiArchParams(arch)
	u := fmt.Sprintf("%s?os=windows&arch=%s&bitness=%s&bundle-type=jdk&package-type=zip&output=json",
		apiBase, a, b)

	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询 Liberica 版本失败 (windows/%s): %w", arch, err)
	}
	var all []libericaRelease
	if err := json.Unmarshal(body, &all); err != nil {
		return nil, fmt.Errorf("解析 Liberica 版本列表失败: %w", err)
	}
	return all, nil
}
