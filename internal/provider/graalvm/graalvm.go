// Package graalvm 是 Oracle GraalVM 发行版的 provider 适配器。
//
// 数据源: download.oracle.com 官方 CDN (无 JSON API)。
//   - 归档直链: {cdn}/{major}/archive/graalvm-jdk-{version}_windows-x64_bin.zip
//     (version 形如 21.0.12 / 25.0.4.1, CPU 线无 +build 段)
//   - SHA256 旁路文件: 直链 + ".sha256" (内容为裸 hex, 首个空白分隔 token)
//   - 版本枚举: CPU 补丁号从探测起点起连续递增 (季度 +1), 顺序 HEAD 归档直链
//     直到首个 404 即该大版本最新 patch (Oracle 无目录列表, 亦无元数据 API)
//
// 仅覆盖 CPU LTS 线 (21/25)。17 及更早不在该 CDN 路径下 (404); 创新线
// (25i1/25i2/... 走 gds.oracle.com, 版本号方案不同) 不在本适配器范围。
//
// 官方无 Windows ARM64 构建 (仅 Linux/macOS 有 AArch64), arch=aarch64 时
// 各入口统一报错并建议改用 temurin/microsoft。
//
// 直连 Oracle CDN (国内可直连), 无镜像, MirrorURL 留空。
package graalvm

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"jvm/internal/app"
	"jvm/internal/provider"
)

const (
	// cdnBase 是 Oracle GraalVM 下载根
	cdnBase = "https://download.oracle.com/graalvm"

	// filenameTpl 是 JDK zip 文件名模板: graalvm-jdk-{version}_windows-{arch}_bin.zip
	// version 形如 21.0.12; 本发行版仅 x64 (aarch64 由 checkArch 拦截)
	filenameTpl = "graalvm-jdk-%s_windows-%s_bin.zip"

	// distroName 是发行版标识, 用作目录命名前缀和 CLI 的 distro@ 前缀
	distroName = "graalvm"
)

// arch 是目标架构, 由 Configure 在启动期设置。GraalVM 仅消费其合法性
// (跟全局配置对齐), 实际只支持 x64 —— aarch64 由 checkArch 统一拦截。
var arch = app.ArchX64

// cpuReleases 是 download.oracle.com/graalvm 路径下的 CPU LTS 大版本。
// Oracle 无可用版本列表 API, 这里写死; 26 等 LTS 发布后在此追加。
var cpuReleases = []int{21, 25}

// floors 是每个大版本的补丁探测起点 (该 major 在 archive 里保留的最早 patch 号:
// 21.0.8 起、25.0.1 起)。CPU 补丁号连续递增, 从起点顺序 HEAD 直到 404 即最新。
// Oracle 若跳号/调整清档会使起点失效 (scanLatest 报错), 届时维护此表。
var floors = map[int]int{21: 8, 25: 1}

// versionRe 校验用户输入的完整版本号: 2~4 段纯数字点分组 (21.0.12 / 25.0.4.1)。
var versionRe = regexp.MustCompile(`^\d+(\.\d+){1,3}$`)

// sidecarHexRe 校验 SHA256 旁路文件解析出的哈希 (64 位 hex)。
var sidecarHexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

func init() {
	provider.Register(graalvm{})
}

// graalvm 是 Provider 适配器。嵌入 provider.Base 拿默认实现:
//   - ShortSemver: GraalVM CPU 版本号 (21.0.12) 已是干净形式, 透传即可
//   - ResolveReleaseName: 版本号即 release 标识, 透传即可
type graalvm struct {
	provider.Base
}

// Name 返回发行版标识。
func (graalvm) Name() string { return distroName }

// DisplayName 返回用户可见的发行版名。
func (graalvm) DisplayName() string { return "GraalVM (Oracle)" }

// Configure 实现 provider.Configurable: 设置目标架构 (x64 / aarch64)。
// mirror 参数被忽略: Oracle GraalVM 直连官方 CDN, 无镜像源。
// 非法 arch 警告并回退 x64; 空值保持默认。
func (graalvm) Configure(cfgArch, _ string) {
	if a := strings.TrimSpace(cfgArch); a != "" {
		if norm, ok := app.NormArch(a); ok {
			arch = norm
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  不支持的架构 %q (仅支持 x64 / aarch64), 回退 x64\n", a)
		}
	}
}

// errNoWindowsARM64 是 arch=aarch64 时的统一错误。
var errNoWindowsARM64 = errors.New(
	"Oracle GraalVM 官方未提供 Windows ARM64 (aarch64) 构建 (仅 Linux/macOS 有 AArch64)。\n" +
		"   可改用 temurin 或 microsoft: jvm install 21  /  jvm install microsoft@21")

// checkArch 在目标架构不受支持时返回错误; 目前仅 x64 可用。
func checkArch() error {
	if arch == app.ArchARM64 {
		return errNoWindowsARM64
	}
	return nil
}

// Available 列出所有可安装的大版本。
// Oracle 无可用版本列表 API, 返回写死的 CPU LTS 集合。
func (graalvm) Available() ([]app.Release, error) {
	if err := checkArch(); err != nil {
		return nil, err
	}
	result := make([]app.Release, 0, len(cpuReleases))
	for _, v := range cpuReleases {
		result = append(result, app.Release{Major: v, LTS: true})
	}
	return result, nil
}

// Resolve 按 VersionSpec 查单个版本, 返回发行版无关的 Asset。
// 两种输入:
//   - 纯大版本号 → 探测最新 patch 后按完整版本处理
//   - 完整版本号 (如 21.0.12) → 拼 archive 直链 + 拉 SHA256 旁路
func (g graalvm) Resolve(spec app.VersionSpec) (*app.Asset, error) {
	if err := checkArch(); err != nil {
		return nil, err
	}
	v := strings.TrimSpace(spec.Version)

	majorStr := v
	if i := strings.IndexByte(v, '.'); i >= 0 {
		majorStr = v[:i]
	}
	major, err := app.ParseMajorVersion(majorStr)
	if err != nil {
		return nil, err
	}
	if !supported(major) {
		return nil, fmt.Errorf(
			"GraalVM (Oracle) 仅提供 CPU LTS 版本 %v, 不支持大版本 %d (17 及更早不在 Oracle CDN)",
			cpuReleases, major)
	}

	// 纯大版本号 → 探测最新 patch (LatestPatch 已含 arch/major 守卫)
	if v == strconv.Itoa(major) {
		a, err := g.LatestPatch(major)
		if err != nil {
			return nil, err
		}
		return fetchAsset(a.ReleaseName, major)
	}

	if !versionRe.MatchString(v) {
		return nil, fmt.Errorf("无效的 GraalVM 版本号 %q (形如 21.0.12 / 25.0.4.1)", v)
	}
	if !probeExists(archiveURL(v)) {
		return nil, fmt.Errorf("GraalVM 没有 %s 版本 (archive 自 %d.0.%d 起保留)",
			v, major, floors[major])
	}
	return fetchAsset(v, major)
}

// LatestPatch 返回指定大版本的最新 CPU patch。
// 轻量查询: 只填 ReleaseName/Distro/Major, 不拉校验和 (下载走 Resolve 正规链路)。
func (graalvm) LatestPatch(major int) (*app.Asset, error) {
	if err := checkArch(); err != nil {
		return nil, err
	}
	if !supported(major) {
		return nil, fmt.Errorf(
			"GraalVM (Oracle) 仅提供 CPU LTS 版本 %v, 不支持大版本 %d", cpuReleases, major)
	}
	patch, err := scanLatest(floors[major], func(n int) bool {
		return probeExists(archiveURL(fmt.Sprintf("%d.0.%d", major, n)))
	})
	if err != nil {
		return nil, err
	}
	version := fmt.Sprintf("%d.0.%d", major, patch)
	return &app.Asset{Semver: version, Major: major, ReleaseName: version, Distro: distroName}, nil
}

// ListVersions 返回指定大版本的全部子版本。
// archive 无目录列表, 无法枚举历史 patch, 仅返回最新 1 条 (microsoft 先例)。
func (g graalvm) ListVersions(major int) ([]*app.Asset, error) {
	a, err := g.LatestPatch(major)
	if err != nil {
		return nil, err
	}
	return []*app.Asset{a}, nil
}

// ---- 以下为包内私有实现 ----

// fetchAsset 拉取 SHA256 旁路文件, 组装完整 Asset。
func fetchAsset(version string, major int) (*app.Asset, error) {
	zipURL := archiveURL(version)
	sum, err := fetchSHA256(zipURL)
	if err != nil {
		return nil, err
	}
	return &app.Asset{
		Semver:      version,
		Major:       major,
		ZipURL:      zipURL,
		Checksum:    sum,
		ReleaseName: version,
		Distro:      distroName,
		// MirrorURL 留空: 直连 Oracle CDN
	}, nil
}

// archiveURL 拼归档直链: {cdn}/{major}/archive/graalvm-jdk-{version}_windows-x64_bin.zip
// major 从 version 首段提取 (21.0.12 → 21)。
func archiveURL(version string) string {
	major := version
	if i := strings.IndexByte(version, '.'); i >= 0 {
		major = version[:i]
	}
	return fmt.Sprintf("%s/%s/archive/%s", cdnBase, major, fmt.Sprintf(filenameTpl, version, app.ArchX64))
}

// fetchSHA256 拉取 zip 的 .sha256 旁路文件并解析哈希。
// Oracle 旁路内容是裸 hex (实测 text/plain 64 字节), 解析按"首个空白分隔
// token"容错, 兼容将来变成 "<hash>  <filename>" 的 sha256sum 格式。
func fetchSHA256(zipURL string) (string, error) {
	body, err := app.HTTPGetText(zipURL + ".sha256")
	if err != nil {
		return "", fmt.Errorf("获取 GraalVM SHA256 失败: %w", err)
	}
	sum, err := parseSidecar(string(body))
	if err != nil {
		return "", fmt.Errorf("GraalVM SHA256 文件格式异常: %w", err)
	}
	return sum, nil
}

// parseSidecar 从旁路文件内容解析 SHA256 哈希。纯函数, 便于表测。
func parseSidecar(body string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(body))
	if len(fields) < 1 || !sidecarHexRe.MatchString(fields[0]) {
		return "", fmt.Errorf("不是 64 位 hex 哈希")
	}
	return strings.ToLower(fields[0]), nil
}

// probeExists 用 HEAD 探测 URL 是否存在 (200 才算)。网络错误视为不存在,
// 由调用方决定如何呈现 (LatestPatch 的探测序列把网络故障当成 404 处理,
// 会在起点校验处暴露)。
func probeExists(url string) bool {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", app.UserAgent())
	resp, err := app.HTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// scanLatest 从 floor 起顺序探测, 返回最后一个存在的补丁号 (patch 连续
// 递增是 Oracle CPU 发布的既有规律: 21.0.8→12、25.0.1→4)。
// floor 本身不存在视为探测起点失效 (Oracle 跳号/清档), 报错提示升级 jvm。
// exists 抽象成参数便于表测。
func scanLatest(floor int, exists func(int) bool) (int, error) {
	if !exists(floor) {
		return 0, fmt.Errorf(
			"探测起点 patch %d 不存在 (Oracle 可能调整了发布节奏), 请升级 jvm 到新版", floor)
	}
	n := floor
	for i := 0; i < 64; i++ { // 上限防御: exists 异常恒真时不无限循环
		if !exists(n + 1) {
			return n, nil
		}
		n++
	}
	return 0, fmt.Errorf("补丁探测超过 64 次上限, 异常")
}

// supported 判断 major 是否在 CPU LTS 列表里。
func supported(major int) bool {
	for _, v := range cpuReleases {
		if v == major {
			return true
		}
	}
	return false
}
