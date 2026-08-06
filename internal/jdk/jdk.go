// Package jdk 负责 JDK 的下载、校验、解压和安装。
//
// Install 根据 version (大版本号或精确版本) 调 adoptium 包拿资源元数据,
// 下载 zip (先镜像后官方), SHA256 校验, 解压到 ~/.jvm/versions。
// DownloadFile 是通用的带进度下载, 被 upgrade 包复用。
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

	"jvm/internal/adoptium"
	"jvm/internal/app"
	"jvm/internal/paths"
)

// safeVersionDir 校验解压出的顶层目录名是否安全 (只允许字母数字._+-)
var safeVersionDir = regexp.MustCompile(`^[A-Za-z0-9._+\-]+$`)

// Install 下载并安装指定版本的 JDK。
//
// version 支持三种写法:
//   - "21"            → 该大版本的最新 GA
//   - "21.0.12"       → 精确小版本, 自动解析 build 号
//   - "jdk-21.0.12+8" → 完整 release name
func Install(version string) error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}

	// 根据输入格式选择查询路径
	var asset *adoptium.AssetInfo
	isLatestMajor := false // 标记"大版本取最新"场景 (决定 CDN 加速是否可用)
	v := strings.TrimSpace(version)
	if strings.ContainsAny(v, ".") || strings.HasPrefix(v, "jdk-") {
		releaseName, err := adoptium.ResolveReleaseName(v)
		if err != nil {
			return err
		}
		fmt.Printf("🔍 正在查询 Temurin %s ...\n", releaseName)
		asset, err = adoptium.FetchAssetByReleaseName(releaseName)
		if err != nil {
			return err
		}
	} else {
		major, err := app.ParseMajorVersion(v)
		if err != nil {
			return err
		}
		fmt.Printf("🔍 正在查询 Temurin JDK %d 的最新版本...\n", major)
		asset, err = adoptium.FetchLatestAsset(major)
		if err != nil {
			return err
		}
		isLatestMajor = true
	}

	// 最终目录名采用 ShortSemver 形式 (X.Y.Z+N), 与 available/list/use/uninstall 完全对齐
	finalName := adoptium.ShortSemver(asset.Semver)

	// 检查这个版本是否已装过 (按最终目录名精确判断)
	target := filepath.Join(paths.VersionsDir, finalName)
	if info, _ := os.Stat(target); info != nil && info.IsDir() {
		fmt.Printf("⚠️  已安装 %s\n", finalName)
		fmt.Printf("   如需重装, 请先 jvm uninstall %s\n", finalName)
		return nil
	}

	sizeMB := remoteSizeMB(asset.ZipURL)
	fmt.Printf("📦 将安装 Temurin %s\n", asset.Semver)
	if sizeMB > 0 {
		fmt.Printf("   大小约 %.1f MB\n", sizeMB)
	}

	// 1. 解析 CDN 直链 (仅"大版本取最新"场景; 精确版本会下错)
	fmt.Print("🔗 解析下载地址...\n")
	cdnURL := asset.ZipURL
	if isLatestMajor && asset.Major > 0 {
		if resolved, err := adoptium.ResolveCDNURL(asset.Major); err == nil {
			cdnURL = resolved
		}
	}

	// 2. 下载 (先试国内镜像, 失败回退官方链接)
	zipName := baseNameOfURL(asset.ZipURL)
	zipPath := filepath.Join(paths.Root, zipName)
	mirrorURL := adoptium.MirrorDownloadURL(asset.ZipURL, asset.Major)
	if err := downloadWithFallback(zipPath, mirrorURL, cdnURL); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	fmt.Println("✅ 下载完成")

	// 3. 从 zip 里读出顶层目录名 (不依赖预测, 更健壮)
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

	// 4. SHA256 校验
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

	// 5. 解压 (先解到临时目录, 成功后原子替换, 避免半解压状态 / 文件占用)
	//    zip 内顶层目录名 (topFolder) 是 Adoptium 原始命名 (jdk-21.0.12+8 / jdk8u502-b07),
	//    与我们想要的最终目录名 (finalName) 不一致, 解压后重命名归一化。
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

	// 6. 清理 zip 并确认目标目录存在
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
	fmt.Print("⬇️  尝试国内镜像 (清华 TUNA)...\n")
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
