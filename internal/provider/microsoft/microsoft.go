// Package microsoft 是 Microsoft Build of OpenJDK 发行版的 provider 适配器。
//
// 数据源: aka.ms 短链接重定向探测 (无 JSON API)。
//   - 大版本探测: aka.ms/download-jdk/microsoft-jdk-{major}-windows-{arch}.zip
//     → 301 到 aka.ms/download-jdk/microsoft-jdk-{fullversion}-windows-{arch}.zip
//     → 301 到 download.visualstudio.microsoft.com 的最终 CDN 直链
//   - 版本号从重定向 Location 的文件名里解析 (如 microsoft-jdk-21.0.12-windows-x64.zip → 21.0.12)
//   - SHA256 从旁路文件 microsoft-jdk-{fullversion}-windows-{arch}.zip.sha256sum.txt 获取
//     (内容格式: "<hash>  <filename>", 标准 sha256sum 输出)
//
// 仅支持 LTS 版本 (11/17/21/25) —— Microsoft 对非 LTS 版本支持窗口短, 此处不列。
// 直连 Akamai/VisualStudio CDN, 国内可直连, 无需镜像, MirrorURL 留空。
//
// 目标架构 (x64 / aarch64) 由 Configure 在启动期设置; 两个架构的短链与旁路
// 校验文件模式完全一致 (已实测 11/17/21/25 均有 aarch64 构建)。
//
// aka.ms 行为: 不存在的短链会 302 到 bing.com 搜索页, 据此判断版本是否存在。
package microsoft

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"jvm/internal/app"
	"jvm/internal/provider"
)

const (
	// akaBase 是 Microsoft JDK 短链根
	akaBase = "https://aka.ms/download-jdk"

	// filenameTpl 是 JDK zip 文件名模板: microsoft-jdk-{version}-windows-{arch}.zip
	// version 是 major ("21") 或 full ("21.0.12"); arch 是 x64 / aarch64
	//   - major: aka.ms 会 301 到含 full version 的 aka.ms 链接 (再 301 到 CDN)
	//   - full:  aka.ms 直接 301 到 CDN
	filenameTpl = "microsoft-jdk-%s-windows-%s.zip"

	// distroName 是发行版标识, 用作目录命名前缀和 CLI 的 distro@ 前缀
	distroName = "microsoft"
)

// arch 是目标架构 (x64 / aarch64), 用于拼接 aka.ms 短链与旁路校验文件名。
// 由 Configure 在启动期一次性设置 (经 provider.ConfigureAll 分发), 之后进程内只读。
var arch = app.ArchX64

// ltsReleases 是 Microsoft Build of OpenJDK 支持的 LTS 大版本。
// Microsoft 不提供"可用版本列表"API, 这里写死 LTS; 非 LTS 版本 (如 22/23/24)
// Microsoft 支持窗口短, 不列入。需新增 LTS 时改这里即可。
var ltsReleases = []int{11, 17, 21, 25}

func init() {
	provider.Register(microsoft{})
}

// microsoft 是 Provider 适配器。嵌入 provider.Base 拿默认实现:
//   - ShortSemver: Microsoft 版本号 (21.0.12) 已是干净形式, 透传即可
//   - ResolveReleaseName: 版本号即 release 标识, 透传即可
type microsoft struct {
	provider.Base
}

// Name 返回发行版标识。
func (microsoft) Name() string { return distroName }

// DisplayName 返回用户可见的发行版名。
func (microsoft) DisplayName() string { return "Microsoft Build of OpenJDK" }

// Configure 实现 provider.Configurable: 设置目标架构 (x64 / aarch64)。
// mirror 参数被忽略: Microsoft 直连 VisualStudio CDN, 无镜像源。
// 非法 arch 警告并回退 x64; 空值保持默认。
func (microsoft) Configure(cfgArch, _ string) {
	if a := strings.TrimSpace(cfgArch); a != "" {
		if norm, ok := app.NormArch(a); ok {
			arch = norm
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  不支持的架构 %q (仅支持 x64 / aarch64), 回退 x64\n", a)
		}
	}
}

// Available 列出所有可安装的大版本。
// Microsoft 无可用版本列表 API, 返回写死的 LTS 版本集合。
func (microsoft) Available() ([]app.Release, error) {
	result := make([]app.Release, 0, len(ltsReleases))
	for _, v := range ltsReleases {
		result = append(result, app.Release{Major: v, LTS: true})
	}
	return result, nil
}

// Resolve 按 VersionSpec 查单个版本, 返回发行版无关的 Asset。
// 两种输入:
//   - 纯大版本号 → aka.ms 探测拿当前最新版本号 + 最终 CDN 直链 + SHA256
//   - 完整版本号 (如 21.0.12) → 直接拼 aka.ms 链接探测, 拿最终 CDN 直链 + SHA256
//     (aka.ms 只保留每个大版本的最新版, 旧版本会 302 到 bing.com, 据此报错)
func (m microsoft) Resolve(spec app.VersionSpec) (*app.Asset, error) {
	v := strings.TrimSpace(spec.Version)

	// 解析大版本号 (用于 Major 字段和错误提示)
	majorStr := v
	if i := strings.IndexByte(v, '.'); i >= 0 {
		majorStr = v[:i]
	}
	major, err := app.ParseMajorVersion(majorStr)
	if err != nil {
		return nil, err
	}
	if !isSupportedLTS(major) {
		return nil, fmt.Errorf(
			"Microsoft Build of OpenJDK 仅支持 LTS 版本 %v, 不支持大版本 %d",
			ltsReleases, major)
	}

	// 探测 aka.ms: 大版本号会被 301 到含 full version 的链接, 完整版本号直接 301 到 CDN
	return m.probe(v, major)
}

// LatestPatch 返回指定大版本的最新版 (供 jvm available 表格用)。
func (m microsoft) LatestPatch(major int) (*app.Asset, error) {
	if !isSupportedLTS(major) {
		return nil, fmt.Errorf(
			"Microsoft Build of OpenJDK 仅支持 LTS 版本 %v, 不支持大版本 %d",
			ltsReleases, major)
	}
	return m.probe(strconv.Itoa(major), major)
}

// ListVersions 返回指定大版本的全部子版本。
// aka.ms 只指向最新版, 无法枚举历史, 故每个 major 仅返回最新 1 条。
func (m microsoft) ListVersions(major int) ([]*app.Asset, error) {
	a, err := m.LatestPatch(major)
	if err != nil {
		return nil, err
	}
	return []*app.Asset{a}, nil
}

// ---- 以下为包内私有实现 ----

// probe 探测 aka.ms 短链, 解析版本号/直链/SHA256。
// input 是用户给的版本串 (大版本号 "21" 或完整版本号 "21.0.12");
// major 用于填充 Asset.Major 字段。
func (m microsoft) probe(input string, major int) (*app.Asset, error) {
	// 第一步: 跟随 aka.ms 重定向链, 拿到最终直链和解析出的完整版本号
	finalURL, fullVersion, err := resolveRedirect(input, arch)
	if err != nil {
		return nil, err
	}

	// 第二步: 拉取 SHA256 旁路文件
	sha256, err := fetchSHA256(fullVersion, arch)
	if err != nil {
		// SHA256 拉取失败阻断安装: jdk 包的完整性校验依赖它, 缺失等于无校验。
		return nil, err
	}

	return &app.Asset{
		Semver:      fullVersion,
		Major:       major,
		ZipURL:      finalURL,
		SHA256:      sha256,
		ReleaseName: fullVersion,
		Distro:      distroName,
		// MirrorURL 留空: 直连 VisualStudio CDN
	}, nil
}

// resolveRedirect 跟随 aka.ms 重定向链, 返回最终 CDN 直链和解析出的完整版本号。
// targetArch 决定短链文件名 (-windows-x64 / -windows-aarch64)。
//
// 重定向链 (有效版本):
//   - 大版本号输入: aka.ms/...21... → 301 → aka.ms/...21.0.12... → 301 → visualstudio CDN
//   - 完整版本输入: aka.ms/...21.0.12... → 301 → visualstudio CDN
//
// 无效版本: aka.ms → 302 → bing.com (据此报错)
func resolveRedirect(version, targetArch string) (finalURL, fullVersion string, err error) {
	shortURL := fmt.Sprintf("%s/%s", akaBase, fmt.Sprintf(filenameTpl, version, targetArch))

	client := &http.Client{
		Timeout: 30 * 1e9, // 30s
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 限制最多跟随 5 次重定向 (防御异常循环)
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			// 遇到 bing.com 跳转 = 版本不存在 (aka.ms 默认行为)
			if strings.Contains(req.URL.Host, "bing.com") {
				return errVersionNotFound
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", shortURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", app.UserAgent())

	resp, err := client.Do(req)
	if err != nil {
		// 区分"版本不存在"(bing 跳转) 和真正的网络错误。
		// http.Client 把 CheckRedirect 的错误包进 *url.Error, 用 errors.Is 识别。
		if errors.Is(err, errVersionNotFound) {
			return "", "", fmt.Errorf("Microsoft 没有 %s 版本 (aka.ms 仅保留每个大版本的最新版)", version)
		}
		return "", "", fmt.Errorf("探测 Microsoft 版本失败: %w", err)
	}
	// 只需要 resp.Request.URL (最终重定向地址), 立即关闭 body 不下载 zip 内容。
	// 用 GET 而非 HEAD: aka.ms 对 HEAD 的行为不稳定, GET 跟随重定向最可靠。
	resp.Body.Close()

	// 从最终 URL 解析完整版本号
	finalURL = resp.Request.URL.String()
	fullVersion = extractVersionFromFilename(resp.Request.URL.Path, targetArch)
	if fullVersion == "" {
		return "", "", fmt.Errorf("无法从 %s 解析版本号", finalURL)
	}
	return finalURL, fullVersion, nil
}

// fetchSHA256 拉取旁路校验文件, 解析出 SHA256 哈希。
// 文件内容格式: "<hash>  <filename>" (标准 sha256sum 输出)
func fetchSHA256(fullVersion, targetArch string) (string, error) {
	shaURL := fmt.Sprintf("%s/%s", akaBase,
		fmt.Sprintf(filenameTpl, fullVersion, targetArch)+".sha256sum.txt")

	body, err := app.HTTPGetText(shaURL)
	if err != nil {
		return "", fmt.Errorf("获取 Microsoft SHA256 失败: %w", err)
	}
	// 格式: "bf27a5d6...  microsoft-jdk-21.0.12-windows-x64.zip"
	fields := strings.Fields(strings.TrimSpace(string(body)))
	if len(fields) < 1 {
		return "", fmt.Errorf("Microsoft SHA256 文件内容为空")
	}
	return fields[0], nil
}

// extractVersionFromFilename 从文件名里提取版本号。
// targetArch 决定剥离的后缀: "microsoft-jdk-21.0.12-windows-x64.zip" → "21.0.12"
func extractVersionFromFilename(path, targetArch string) string {
	base := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		base = path[i+1:]
	}
	// 去前缀 "microsoft-jdk-" 和后缀 "-windows-{arch}.zip[...]"
	const prefix = "microsoft-jdk-"
	suffix := "-windows-" + targetArch
	if !strings.HasPrefix(base, prefix) {
		return ""
	}
	base = base[len(prefix):]
	if i := strings.Index(base, suffix); i >= 0 {
		base = base[:i]
	}
	return base
}

// isSupportedLTS 判断 major 是否在支持的 LTS 列表里。
func isSupportedLTS(major int) bool {
	for _, v := range ltsReleases {
		if v == major {
			return true
		}
	}
	return false
}

// errVersionNotFound 是 CheckRedirect 内部用的哨兵错误,
// 表示 aka.ms 把短链跳到了 bing.com (即版本不存在)。
var errVersionNotFound = errors.New("version not found (redirected to bing.com)")
