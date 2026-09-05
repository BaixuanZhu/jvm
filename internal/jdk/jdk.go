// Package jdk 负责 JDK 的下载、校验、解压和安装 (发行版无关)。
//
// InstallVersion 按 VersionSpec 调 provider 适配器拿 Asset 元数据 (发行版细节
// 由适配器消化), 再交给 Install 完成取包 (缓存命中复用, 否则下载到缓存;
// 镜像优先, 无镜像则直连)、完整性校验 (SHA256/SHA1, 按 provider 提供)、
// 解压到数据面 versions/。安装包 zip 留存缓存目录供重装复用。
// DownloadFile 是通用带进度下载, 被 upgrade 包复用。
package jdk

import (
	"archive/zip"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"jvm/internal/app"
	"jvm/internal/paths"
	"jvm/internal/provider"
)

// safeVersionDir 校验解压出的顶层目录名是否安全 (只允许字母数字._+-)
var safeVersionDir = regexp.MustCompile(`^[A-Za-z0-9._+\-]+$`)

// InstallVersion 按 spec 查询版本并安装。便捷封装: 调 provider.Resolve 拿到
// 已填好的 Asset (CDN/镜像 URL 都在适配器内化), 然后交给 Install。
//
// spec.Version 的格式由各 provider 自己解析 (ResolveReleaseName), 这里不预处理。
// provider 通过 provider.Get(spec.Distro) 由调用方传入。
func InstallVersion(p provider.Provider, spec app.VersionSpec) error {
	fmt.Printf("🔍 正在查询 %s %s ...\n", p.DisplayName(), spec.Version)
	asset, err := p.Resolve(spec)
	if err != nil {
		return err
	}
	return Install(asset, p.Name())
}

// InstallLocal 从本地 zip 包安装 (jvm install <distro@版本> <zip文件>,
// 内网/代理下手动下载的场景)。不走网络、不做远程校验和校验 (用户对自己的
// 文件负责), 目录命名与远程安装完全一致: {VersionsDir}/{spec.Distro}-{spec.Version}。
func InstallLocal(zipPath string, spec app.VersionSpec) error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	if !safeVersionDir.MatchString(spec.Version) {
		return fmt.Errorf("版本号含不安全字符: %s", spec.Version)
	}
	finalName := spec.Distro + "-" + spec.Version

	target := filepath.Join(paths.VersionsDir, finalName)
	if info, _ := os.Stat(target); info != nil && info.IsDir() {
		fmt.Printf("⚠️  已安装 %s\n", finalName)
		fmt.Printf("   如需重装, 请先 jvm uninstall %s\n", finalName)
		return nil
	}

	info, err := os.Stat(zipPath)
	if err != nil || info.IsDir() {
		return fmt.Errorf("zip 文件不存在: %s", zipPath)
	}
	fmt.Printf("📦 从本地包安装 %s\n", finalName)
	fmt.Printf("   源: %s (%.1f MB, 不做远程校验和校验)\n", zipPath, float64(info.Size())/1024/1024)

	// 读顶层目录。与 Install 不同, 失败不删用户的源文件。
	topFolder, err := readTopFolder(zipPath)
	if err != nil {
		return err
	}
	if topFolder == "" {
		return fmt.Errorf("zip 内未找到顶层目录")
	}
	if !safeVersionDir.MatchString(topFolder) {
		return fmt.Errorf("zip 顶层目录名不安全, 已中止: %s", topFolder)
	}

	if err := extractAndPlace(zipPath, topFolder, finalName); err != nil {
		return err
	}

	fmt.Printf("\n✅ 安装完成: %s\n", finalName)
	fmt.Printf("   运行 `jvm use %s@%s` 来切换到这个版本\n", spec.Distro, spec.Version)
	return nil
}

// Install 下载并安装一个已查好的 Asset (发行版无关)。
//
// name 是发行版标识 (用作目录命名前缀和文案), 通常传 asset.Distro;
// 单独传参是为了让调用方在目录命名上不被 asset.Distro 绑死。
//
// 流程: 检查已装 → 取包 (缓存命中直接用, 否则下载到缓存; MirrorURL 非空走
// 双源, 否则直连 ZipURL) → SHA256 校验 → 解压 → 原子替换到
// {dataRoot}/versions/{name}-{ReleaseName}。安装包 zip 留在缓存目录
// ({dataRoot}/cache/{finalName}.zip), 卸载后重装同版本免重新下载。
func Install(asset *app.Asset, name string) error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}

	// 最终目录名: {distro}-{ReleaseName}。ReleaseName 由 provider 用 ShortSemver 产出,
	// 与 available/list/use/uninstall 完全对齐。name 默认取 asset.Distro。
	if name == "" {
		name = asset.Distro
	}
	finalName := name + "-" + asset.ReleaseName

	// 检查这个版本是否已装过 (按最终目录名精确判断)
	target := filepath.Join(paths.VersionsDir, finalName)
	if info, _ := os.Stat(target); info != nil && info.IsDir() {
		fmt.Printf("⚠️  已安装 %s\n", finalName)
		fmt.Printf("   如需重装, 请先 jvm uninstall %s\n", finalName)
		return nil
	}

	// 缓存文件以最终目录名命名: 镜像/官方双源内容一致 (校验和相同), 同键复用。
	cacheFile := filepath.Join(paths.CacheDir, finalName+".zip")

	if cacheHit(cacheFile, asset) {
		fmt.Printf("📦 将安装 %s %s\n", name, asset.Semver)
		fmt.Println("📦 命中下载缓存, 跳过下载:", filepath.Base(cacheFile))
	} else {
		sizeMB := remoteSizeMB(asset.ZipURL)
		fmt.Printf("📦 将安装 %s %s\n", name, asset.Semver)
		if sizeMB > 0 {
			fmt.Printf("   大小约 %.1f MB\n", sizeMB)
		}

		// 下载: Asset.MirrorURL 非空 → 镜像优先, 失败回退官方; 否则直连官方。
		// (镜像/CDN 直链的解析已由 provider 适配器内化填入 asset, 这里只消费。
		// 下载直接落到缓存路径, DownloadFile 的 .part→rename 语义保证只有完整
		// 下载成功后才会出现最终 zip 文件。)
		fmt.Print("🔗 解析下载地址...\n")
		if asset.MirrorURL != "" {
			if err := downloadWithFallback(cacheFile, asset.MirrorURL, asset.ZipURL); err != nil {
				return fmt.Errorf("下载失败: %w", err)
			}
		} else {
			fmt.Print("⬇️  下载 (无国内镜像, 直连官方源)...\n")
			if err := download(asset.ZipURL, cacheFile); err != nil {
				return fmt.Errorf("下载失败: %w", err)
			}
		}
		fmt.Println("✅ 下载完成")
	}
	zipPath := cacheFile

	// 从 zip 里读出顶层目录名 (不依赖预测, 更健壮)。
	// 走到这里的失败说明 zip 内容可疑 (下载损坏/缓存投毒), 一律删除缓存文件。
	topFolder, err := readTopFolder(zipPath)
	if err != nil {
		os.Remove(zipPath)
		return err
	}
	if topFolder == "" {
		os.Remove(zipPath)
		return fmt.Errorf("zip 内未找到顶层目录")
	}
	if !safeVersionDir.MatchString(topFolder) {
		os.Remove(zipPath)
		return fmt.Errorf("zip 顶层目录名不安全, 已中止: %s", topFolder)
	}

	// 完整性校验 (按 provider 提供的算法: sha256 / sha1; 为空则跳过)。
	// 不匹配同样删缓存 (可能是镜像文件损坏或源被换过)。
	algo := asset.ChecksumAlgo
	if algo == "" {
		algo = "sha256"
	}
	fmt.Printf("🔐 校验 %s... ", strings.ToUpper(algo))
	got, err := fileHash(zipPath, algo)
	if err != nil {
		os.Remove(zipPath)
		return err
	}
	if asset.Checksum != "" && got != asset.Checksum {
		os.Remove(zipPath)
		return fmt.Errorf("校验失败 (%s)\n   期望: %s\n   实际: %s", algo, asset.Checksum, got)
	}
	fmt.Println("通过")

	// 解压 (先解到临时目录, 成功后原子替换, 避免半解压状态 / 文件占用)。
	// 解压/落位失败保留缓存 zip (内容已校验过, 是好的, 重试不该重新下载)。
	if err := extractAndPlace(zipPath, topFolder, finalName); err != nil {
		return err
	}

	fmt.Printf("\n✅ 安装完成: %s\n", finalName)
	fmt.Printf("   运行 `jvm use %d` 来切换到这个版本\n", asset.Major)
	return nil
}

// extractAndPlace 解压并原子落位: 先解到临时目录, 再把 zip 内顶层目录
// (topFolder, 发行版原始命名如 jdk-21.0.12+8) 重命名归一化为最终目录名
// ({distro}-{ReleaseName})。被远程 Install 与本地 InstallLocal 共用。
func extractAndPlace(zipPath, topFolder, finalName string) error {
	fmt.Print("📂 解压中... ")
	tmpExtract := filepath.Join(paths.Root, ".tmp-extract-"+topFolder)
	os.RemoveAll(tmpExtract)
	if err := unzipTo(zipPath, tmpExtract); err != nil {
		os.RemoveAll(tmpExtract)
		return fmt.Errorf("解压失败: %w", err)
	}
	extractedDir := filepath.Join(tmpExtract, topFolder) // zip 内原始目录名
	finalDir := filepath.Join(paths.VersionsDir, finalName)
	if _, err := os.Stat(finalDir); err == nil {
		os.RemoveAll(finalDir) // 重装场景
	}
	if err := os.Rename(extractedDir, finalDir); err != nil {
		os.RemoveAll(tmpExtract)
		return fmt.Errorf("移动解压目录失败: %w", err)
	}
	os.RemoveAll(tmpExtract)
	fmt.Println("完成")

	if _, err := os.Stat(finalDir); err != nil {
		return fmt.Errorf("解压后未找到 %s: %w", finalName, err)
	}
	return nil
}

// cacheHit 判断缓存文件是否可直接复用: 文件存在且非空 (DownloadFile 只在
// 完整下载成功后才 rename 出最终文件, 存在即完整), 且校验和匹配
// (asset 未提供校验和时跳过内容比对)。
func cacheHit(file string, asset *app.Asset) bool {
	info, err := os.Stat(file)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	if asset.Checksum == "" {
		return true
	}
	algo := asset.ChecksumAlgo
	if algo == "" {
		algo = "sha256"
	}
	got, err := fileHash(file, algo)
	return err == nil && got == asset.Checksum
}

// remoteSizeMB 用 HEAD 请求拿文件大小 (MB); 失败返回 0
func remoteSizeMB(url string) float64 {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", app.UserAgent())
	resp, err := app.HTTPClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.ContentLength > 0 {
		return float64(resp.ContentLength) / 1024 / 1024
	}
	return 0
}

// downloadWithFallback 优先用镜像源, 失败则回退到官方源。
// 续传模式下镜像→官方切换时保留 .part (两者内容一致, SHA256 相同, 可继续续传)。
func downloadWithFallback(dest, mirrorURL, officialURL string) error {
	fmt.Print("⬇️  尝试国内镜像...\n")
	if err := download(mirrorURL, dest); err == nil {
		return nil
	}
	fmt.Print("⚠️  国内镜像失败, 回退到官方源 (可能较慢)...\n")
	return download(officialURL, dest)
}

// download 是 jdk 包内部的统一下载入口: 3 次重试 + 断点续传。
// 收敛 downloadWithFallback 和直连两处的下载调用, 避免 options 散落。
func download(url, dest string) error {
	return DownloadFile(url, dest, WithRetries(3), WithResume(true))
}

// DownloadOption 配置 DownloadFile 的行为 (重试次数、是否断点续传)。
// 采用 functional options 模式, 让现有零参调用方 (如 upgrade 包) 无需改动。
type DownloadOption func(*downloadConfig)

type downloadConfig struct {
	retries int  // 瞬时错误重试次数 (0 = 不重试)
	resume  bool // 是否启用断点续传
}

// WithRetries 设置瞬时错误重试次数。
func WithRetries(n int) DownloadOption {
	return func(c *downloadConfig) { c.retries = n }
}

// WithResume 启用断点续传: 下载中断后保留 .part, 下次从断点继续。
func WithResume(b bool) DownloadOption {
	return func(c *downloadConfig) { c.resume = b }
}

// DownloadFile 从 url 下载到 dest, 带进度百分比显示。
// 导出以供 upgrade 包复用 (下载自更新 zip)。
//
// 续传 (resume=true) 时, 数据写入 dest+".part", 成功后原子 rename 为 dest,
// 失败保留 .part 供下次续传; 故调用方拿到的 dest 永远是完整文件, 无需感知续传。
// 续传依赖服务端支持 Range 请求 (响应 206); 不支持时自动回退全量下载。
func DownloadFile(url, dest string, opts ...DownloadOption) error {
	cfg := downloadConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	// resume 模式下用 .part 作临时文件; 非 resume 直接写 dest。
	// 重试循环: 瞬时错误 (网络/5xx) 最多 cfg.retries 次, 指数退避。
	var lastErr error
	for attempt := 0; attempt <= cfg.retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s, 4s...
			time.Sleep(backoff)
		}
		lastErr = downloadOnce(url, dest, cfg.resume)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// downloadOnce 执行一次下载尝试。
// resume=true 时探测 .part 已有大小, 发 Range 请求续传; 失败保留 .part。
func downloadOnce(url, dest string, resume bool) error {
	partPath := dest
	offset := int64(0)

	if resume {
		partPath = dest + ".part"
		if info, err := os.Stat(partPath); err == nil {
			offset = info.Size() // 已下载的部分, 从此处续
		}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", app.UserAgent())
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := app.DownloadClient.Do(req)
	if err != nil {
		return err // 网络错误 (可重试)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// 全量响应: 服务端不支持 Range 或首次请求。从头写, 丢弃已有 .part。
		offset = 0
	case resp.StatusCode == http.StatusPartialContent:
		// 续传响应, offset 保持。若之前不是续传请求却收到 206, 视为异常。
		if offset == 0 {
			return fmt.Errorf("意外的 206 响应 (未请求 Range)")
		}
	default:
		// 4xx 不重试 (客户端错误, 重试无意义); 5xx 可重试。
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// 打开文件: 续传时追加写, 全量时覆盖写。
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(partPath, flags, 0o644)
	if err != nil {
		return err
	}

	// 写入数据; 走 progressReader 显示进度。
	// 注意: out 必须在 rename 前 Close —— Windows 不允许 rename 仍打开的文件,
	// 故不用 defer, 而是显式关闭并处理错误。
	//
	// total (完整文件大小) 的确定优先级:
	//  1. 206 响应的 Content-Range 头 (最可靠, 如 "bytes 131072-262143/262144");
	//  2. offset + resp.ContentLength (200 响应或 ContentLength 已知时);
	//  3. 0 = 未知, 不显示进度百分比。
	total := resolveTotalSize(resp, offset)
	writeErr := func() error {
		defer out.Close()
		var body io.Reader = resp.Body
		if total > 0 {
			pr := newProgressReader(resp.Body, total, offset)
			defer pr.Close()
			body = pr
		}
		_, err := io.Copy(out, body)
		return err
	}()
	if writeErr != nil {
		return writeErr // 写入错误 (可重试, .part 已保留)
	}

	// 续传模式: 完成后原子 rename .part → dest (此时 out 已 Close)。
	if resume {
		if err := os.Rename(partPath, dest); err != nil {
			return err
		}
	}
	return nil
}

// resolveTotalSize 推算完整文件大小, 用于进度条显示。
//
// 206 续传响应里 resp.ContentLength 常为 -1 (服务端用 chunked 编码),
// 直接 offset + ContentLength 会算错 (出现 >100% 的进度条)。故优先从
// Content-Range 头解析完整大小; 解析失败回退到 offset + ContentLength;
// 仍未知 (返回 0) 则不显示百分比。
func resolveTotalSize(resp *http.Response, offset int64) int64 {
	// 206: 优先从 Content-Range 头解析完整大小。
	// 格式 "bytes 131072-262143/262144", 末段是完整文件大小。
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if i := strings.LastIndexByte(cr, '/'); i >= 0 {
			if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil && n > 0 {
				return n
			}
		}
	}
	// 回退: offset + 本次 ContentLength (200 响应 offset=0 时即 ContentLength)。
	if resp.ContentLength > 0 {
		return offset + resp.ContentLength
	}
	return 0 // 未知, 不显示进度
}

// progressReader 包装 io.ReadCloser, 定期打印下载进度。
// offset 是续传时已有的字节数 (用于把进度条对齐到整体进度)。
type progressReader struct {
	reader    io.ReadCloser
	total     int64
	received  int64
	lastPrint int64
}

func newProgressReader(r io.ReadCloser, total, offset int64) *progressReader {
	return &progressReader{reader: r, total: total, received: offset, lastPrint: offset}
}

func (p *progressReader) Close() error { return p.reader.Close() }

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	p.received += int64(n)
	step := p.total / 20
	if step == 0 {
		step = 1
	}
	if p.received-p.lastPrint >= step || (err != nil && p.received > p.lastPrint) {
		pct := p.received * 100 / p.total
		fmt.Printf("\r⬇️  下载中... %d%% (%.1f / %.1f MB)   ",
			pct, float64(p.received)/1024/1024, float64(p.total)/1024/1024)
		p.lastPrint = p.received
	}
	return n, err
}

// fileHash 计算文件的哈希 (十六进制小写), 按 algo 选择算法:
// "sha1" (含 "sha-1") 用 SHA-1, 其余 (空串/"sha256"/未知) 一律用 SHA-256。
// 流式读取, 不全量载入内存, 适合大 JDK zip。
func fileHash(path, algo string) (string, error) {
	var h hash.Hash
	switch strings.ToLower(strings.TrimSpace(algo)) {
	case "sha1", "sha-1":
		h = sha1.New()
	default: // "" / "sha256" / 未知 → 一律 SHA-256
		h = sha256.New()
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readTopFolder 打开 zip, 返回第一个条目路径里的顶层目录名 (不含 /)
func readTopFolder(zipPath string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	for _, f := range reader.File {
		if i := strings.IndexByte(f.Name, '/'); i > 0 {
			return f.Name[:i], nil
		}
	}
	return "", nil
}

// unzipTo 把 zip 解压到目标目录
func unzipTo(zipPath, dest string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, f := range reader.File {
		if err := extractZipFile(f, dest); err != nil {
			return err
		}
	}
	return nil
}

// extractZipFile 解压 zip 里的单个条目 (带 zip-slip 防护)
func extractZipFile(f *zip.File, dest string) error {
	name := filepath.FromSlash(f.Name)
	destPath := filepath.Join(dest, name)
	cleanDest := filepath.Clean(dest)
	if !strings.HasPrefix(filepath.Clean(destPath), cleanDest+string(os.PathSeparator)) {
		return fmt.Errorf("非法路径 (zip slip 拦截): %s", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(destPath, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}
