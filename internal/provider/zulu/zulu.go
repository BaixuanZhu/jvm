// Package zulu 是 Azul Zulu Build of OpenJDK 发行版的 provider 适配器。
//
// 数据源: Azul Metadata API (https://api.azul.com/metadata/v1), 两步查询:
//   - 列表端点 /zulu/packages/?java_version=...&os=windows&arch=... 拿下载直链 +
//     package_uuid + 版本号 (列表对象不含哈希)。
//   - 详情端点 /zulu/packages/{package_uuid}/ 拿官方 sha256_hash (含 md5_hash/size)。
//
// 列表同 major 会返回 jdk / fx(JavaFX) / crac 多变体, 用文件名含 "-ca-jdk" 过滤纯 JDK:
// 纯 jdk 是 "-ca-jdk", fx 是 "-ca-fx-jdk", crac 是 "-ca-crac-jdk", 后两者不含 "-ca-jdk"
// 子串, 故 strings.Contains(name, "-ca-jdk") 精确匹配纯 jdk。
//
// 版本号: java_version 数组 [21,0,12] + openjdk_build_number(8) → "21.0.12+8"。
//
// 目标架构 (x64 / aarch64) 由 Configure 在启动期设置; API 查询参数 arch 用
// "x64" / "arm64" (注意: 内部值 aarch64 在 Azul API 里叫 arm64, 文件名后缀则是
// win_aarch64)。直连 cdn.azul.com, 国内可直连, 无镜像源。
package zulu

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"jvm/internal/app"
	"jvm/internal/provider"
)

const (
	// apiBase 是 Azul Metadata API 根
	apiBase = "https://api.azul.com/metadata/v1"

	// distroName 是发行版标识, 用作目录命名前缀和 CLI 的 distro@ 前缀 (全字母)
	distroName = "zulu"
)

// arch 是目标架构 (app.ArchX64 / app.ArchARM64), 决定 API 查询的 arch 参数。
// 由 Configure 在启动期一次性设置 (经 provider.ConfigureAll 分发), 之后进程内只读。
var arch = app.ArchX64

// activeVersions 是 Available 展示用的活跃 LTS 版本。Zulu 实际覆盖 8/11/17/21/25 等,
// 此处只列主流 LTS; Resolve 不受限于此, 任何 API 有的 major 都能装。
var activeVersions = []app.Release{
	{Major: 11, LTS: true},
	{Major: 17, LTS: true},
	{Major: 21, LTS: true},
	{Major: 25, LTS: true},
}

func init() {
	provider.Register(zulu{})
}

// zulu 是 Provider 适配器。嵌入 provider.Base 拿默认实现:
//   - ShortSemver: Zulu 版本号 (21.0.12+8) 已是干净形式, 透传即可
//   - ResolveReleaseName: 版本号即 release 标识, 透传即可
type zulu struct {
	provider.Base
}

// Name 返回发行版标识。
func (zulu) Name() string { return distroName }

// DisplayName 返回用户可见的发行版名。
func (zulu) DisplayName() string { return "Azul Zulu OpenJDK" }

// Configure 实现 provider.Configurable: 设置目标架构。
// mirror 参数被忽略: Zulu 直连 cdn.azul.com, 无镜像源。
// 非法 arch 警告并回退 x64; 空值保持当前。
func (zulu) Configure(cfgArch, _ string) {
	if a := strings.TrimSpace(cfgArch); a != "" {
		if norm, ok := app.NormArch(a); ok {
			arch = norm
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  不支持的架构 %q (仅支持 x64 / aarch64), 回退 x64\n", a)
		}
	}
}

// Available 列出展示用的活跃 LTS 版本。
func (zulu) Available() ([]app.Release, error) {
	out := make([]app.Release, len(activeVersions))
	copy(out, activeVersions)
	return out, nil
}

// Resolve 按 VersionSpec 查单个版本。纯大版本号 → 最新 GA; 含 patch 的完整版本号 → 精确匹配。
func (z zulu) Resolve(spec app.VersionSpec) (*app.Asset, error) {
	v := strings.TrimSpace(spec.Version)
	majorStr := v
	if i := strings.IndexByte(v, '.'); i >= 0 {
		majorStr = v[:i]
	}
	major, err := app.ParseMajorVersion(majorStr)
	if err != nil {
		return nil, err
	}
	if isPureMajor(v) {
		return fetchLatest(major)
	}
	return fetchByExact(v, major)
}

// LatestPatch 返回指定大版本的最新 GA 版本。
func (z zulu) LatestPatch(major int) (*app.Asset, error) {
	return fetchLatest(major)
}

// ListVersions 返回指定大版本的全部 GA patch (按版本降序)。
// 仅用于 available -a 展示, 不查 sha256 (install 走 Resolve 才查详情端点)。
func (z zulu) ListVersions(major int) ([]*app.Asset, error) {
	pkgs, err := fetchPackages(major, false)
	if err != nil {
		return nil, err
	}
	sort.Slice(pkgs, func(i, j int) bool {
		return compareJavaVersion(pkgs[i].JavaVersion, pkgs[j].JavaVersion) > 0
	})
	out := make([]*app.Asset, 0, len(pkgs))
	for _, p := range pkgs {
		s := semverFromZulu(p)
		out = append(out, &app.Asset{
			Semver:      s,
			Major:       major,
			ZipURL:      p.DownloadURL,
			ReleaseName: s,
			Distro:      distroName,
		})
	}
	return out, nil
}

// ---- 以下为包内私有实现 ----

// zuluPackage 是列表端点返回的 package 对象 (字段比详情端点少, 不含哈希)。
type zuluPackage struct {
	AvailabilityType   string `json:"availability_type"`
	DistroVersion      []int  `json:"distro_version"`
	DownloadURL        string `json:"download_url"`
	JavaVersion        []int  `json:"java_version"`
	Latest             bool   `json:"latest"`
	Name               string `json:"name"`
	OpenJDKBuildNumber int    `json:"openjdk_build_number"`
	PackageUUID        string `json:"package_uuid"`
	Product            string `json:"product"`
}

// zuluPackageDetail 是详情端点返回的对象, 在列表对象基础上补 sha256_hash。
type zuluPackageDetail struct {
	zuluPackage
	SHA256Hash string `json:"sha256_hash"`
}

// apiArch 把内部架构值翻译成 Azul API 的 arch 查询参数。
// 内部 aarch64 在 Azul API 里叫 "arm64"; 其余 (含未知) 一律 x64。
func apiArch(a string) string {
	if a == app.ArchARM64 {
		return "arm64"
	}
	return "x64"
}

// isPlainJDK 判断文件名是否纯 JDK 变体 (排除 fx / crac)。
func isPlainJDK(name string) bool {
	return strings.Contains(name, "-ca-jdk")
}

// isPureMajor 判断输入是否纯大版本号 (不含 ".", 即没有 patch)。
func isPureMajor(v string) bool {
	return !strings.ContainsRune(v, '.')
}

// semverFromZulu 把 java_version 数组 + openjdk_build_number 拼成 semver。
// [21,0,12] + build 8 → "21.0.12+8"。
func semverFromZulu(p zuluPackage) string {
	parts := make([]string, len(p.JavaVersion))
	for i, n := range p.JavaVersion {
		parts[i] = strconv.Itoa(n)
	}
	s := strings.Join(parts, ".")
	if p.OpenJDKBuildNumber > 0 {
		s += fmt.Sprintf("+%d", p.OpenJDKBuildNumber)
	}
	return s
}

// compareJavaVersion 按 [major,minor,patch...] 数组逐段比较, 返回 负/0/正。
func compareJavaVersion(a, b []int) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) == len(b) {
		return 0
	}
	if len(a) < len(b) {
		return -1
	}
	return 1
}

// fetchPackages 查询列表端点, 返回过滤后的纯 JDK 变体包列表。
// latestOnly=true 只取最新 patch。
func fetchPackages(major int, latestOnly bool) ([]zuluPackage, error) {
	latest := "false"
	if latestOnly {
		latest = "true"
	}
	u := fmt.Sprintf(
		"%s/zulu/packages/?java_version=%d&os=windows&arch=%s&archive_type=zip&java_package_type=jdk&release_status=GA&availability_types=CA&javafx_bundled=false&latest=%s",
		apiBase, major, apiArch(arch), latest)

	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("查询 Zulu 版本失败 (windows/%s): %w", arch, err)
	}
	var all []zuluPackage
	if err := json.Unmarshal(body, &all); err != nil {
		return nil, fmt.Errorf("解析 Zulu 版本列表失败: %w", err)
	}

	out := make([]zuluPackage, 0, len(all))
	for _, p := range all {
		if isPlainJDK(p.Name) {
			out = append(out, p)
		}
	}
	return out, nil
}

// fetchLatest 取指定大版本的最新 GA 包, 含 sha256。
func fetchLatest(major int) (*app.Asset, error) {
	pkgs, err := fetchPackages(major, true)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("Zulu 没有 windows/%s 的 JDK %d GA 包", arch, major)
	}
	return buildAsset(pkgs[0], major)
}

// fetchByExact 在指定大版本的全部 patch 里精确匹配完整版本号, 含 sha256。
func fetchByExact(full string, major int) (*app.Asset, error) {
	pkgs, err := fetchPackages(major, false)
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		if semverFromZulu(p) == full {
			return buildAsset(p, major)
		}
	}
	return nil, fmt.Errorf("Zulu 没有 windows/%s 的 %s 版本", arch, full)
}

// buildAsset 翻译成发行版无关的 *app.Asset, 查详情端点拿 sha256_hash。
func buildAsset(p zuluPackage, major int) (*app.Asset, error) {
	sha, err := fetchSHA256(p.PackageUUID)
	if err != nil {
		return nil, err
	}
	s := semverFromZulu(p)
	return &app.Asset{
		Semver:      s,
		Major:       major,
		ZipURL:      p.DownloadURL,
		Checksum:    sha,
		ReleaseName: s,
		Distro:      distroName,
		// MirrorURL 留空: 直连 cdn.azul.com
	}, nil
}

// fetchSHA256 查详情端点拿官方 sha256_hash。
func fetchSHA256(pkgUUID string) (string, error) {
	if pkgUUID == "" {
		return "", fmt.Errorf("Zulu package_uuid 为空, 无法查 sha256")
	}
	u := fmt.Sprintf("%s/zulu/packages/%s/", apiBase, pkgUUID)
	body, err := app.HTTPGetJSON(u)
	if err != nil {
		return "", fmt.Errorf("获取 Zulu SHA256 失败: %w", err)
	}
	var d zuluPackageDetail
	if err := json.Unmarshal(body, &d); err != nil {
		return "", fmt.Errorf("解析 Zulu 详情失败: %w", err)
	}
	if d.SHA256Hash == "" {
		return "", fmt.Errorf("Zulu 详情未返回 sha256_hash")
	}
	return d.SHA256Hash, nil
}
