// Package jdk 负责 JDK 的下载、校验、解压和安装 (发行版无关)。
//
// InstallVersion 按 VersionSpec 调 provider 适配器拿 Asset 元数据 (发行版细节
// 由适配器消化), 再交给 Install 完成下载 (镜像优先, 无镜像则直连官方)、SHA256
// 校验、解压到 ~/.jvm/versions。DownloadFile 是通用带进度下载, 被 upgrade 包复用。
package jdk

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// Install 下载并安装一个已查好的 Asset (发行版无关)。
//
// name 是发行版标识 (用作目录命名前缀和文案), 通常传 asset.Distro;
// 单独传参是为了让调用方在目录命名上不被 asset.Distro 绑死。
//
// 流程: 检查已装 → 下载 (MirrorURL 非空走双源, 否则直连 ZipURL) →
//
//	SHA256 校验 → 解压 → 原子替换到 ~/.jvm/versions/{name}-{ReleaseName}。
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

	sizeMB := remoteSizeMB(asset.ZipURL)
	fmt.Printf("📦 将安装 %s %s\n", name, asset.Semver)
	if sizeMB > 0 {
		fmt.Printf("   大小约 %.1f MB\n", sizeMB)
	}

	// 1. 下载: Asset.MirrorURL 非空 → 镜像优先, 失败回退官方; 否则直连官方。
	//    (镜像/CDN 直链的解析已由 provider 适配器内化填入 asset, 这里只消费。)
	zipName := baseNameOfURL(asset.ZipURL)
	zipPath := filepath.Join(paths.Root, zipName)
	fmt.Print("🔗 解析下载地址...\n")
	if asset.MirrorURL != "" {
		if err := downloadWithFallback(zipPath, asset.MirrorURL, asset.ZipURL); err != nil {
			return fmt.Errorf("下载失败: %w", err)
		}
	} else {
		fmt.Print("⬇️  下载 (无国内镜像, 直连官方源)...\n")
		if err := DownloadFile(asset.ZipURL, zipPath); err != nil {
			return fmt.Errorf("下载失败: %w", err)
		}
	}
	fmt.Println("✅ 下载完成")

	// 2. 从 zip 里读出顶层目录名 (不依赖预测, 更健壮)
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

	// 3. SHA256 校验
	fmt.Print("🔐 校验 SHA256... ")
	got, err := fileSHA256(zipPath)
	if err != nil {
		return err
	}
	if asset.SHA256 != "" && got != asset.SHA256 {
		os.Remove(zipPath)
		return fmt.Errorf("校验失败\n   期望: %s\n   实际: %s", asset.SHA256, got)
	}
	fmt.Println("通过")

	// 4. 解压 (先解到临时目录, 成功后原子替换, 避免半解压状态 / 文件占用)
	//    zip 内顶层目录名 (topFolder) 是发行版原始命名 (如 jdk-21.0.12+8),
	//    与我们想要的最终目录名 ({distro}-{ReleaseName}) 不一致, 解压后重命名归一化。
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

	// 5. 清理 zip 并确认目标目录存在
	os.Remove(zipPath)
	if _, err := os.Stat(finalDir); err != nil {
		return fmt.Errorf("解压后未找到 %s: %w", finalName, err)
	}

	fmt.Printf("\n✅ 安装完成: %s\n", finalName)
	fmt.Printf("   运行 `jvm use %d` 来切换到这个版本\n", asset.Major)
	return nil
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

// downloadWithFallback 优先用镜像源, 失败则回退到官方源
func downloadWithFallback(dest, mirrorURL, officialURL string) error {
	fmt.Print("⬇️  尝试国内镜像...\n")
	if err := DownloadFile(mirrorURL, dest); err == nil {
		return nil
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		os.Remove(dest)
	}
	fmt.Print("⚠️  国内镜像失败, 回退到官方源 (可能较慢)...\n")
	return DownloadFile(officialURL, dest)
}

// DownloadFile 从 url 下载到本地路径, 带进度百分比显示。
// 导出以供 upgrade 包复用 (下载自更新 zip)。
func DownloadFile(url, dest string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", app.UserAgent())

	resp, err := app.DownloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	total := resp.ContentLength
	if total > 0 {
		pr := newProgressReader(resp.Body, total)
		defer pr.Close()
		_, err = io.Copy(out, pr)
		return err
	}
	_, err = io.Copy(out, resp.Body)
	return err
}

// progressReader 包装 io.ReadCloser, 定期打印下载进度
type progressReader struct {
	reader    io.ReadCloser
	total     int64
	received  int64
	lastPrint int64
}

func newProgressReader(r io.ReadCloser, total int64) *progressReader {
	return &progressReader{reader: r, total: total}
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

// fileSHA256 计算文件的 SHA256 (十六进制小写)
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
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

// baseNameOfURL 取 URL 最后一段作为文件名
func baseNameOfURL(u string) string {
	if i := strings.LastIndexByte(u, '/'); i >= 0 {
		return u[i+1:]
	}
	return u
}
