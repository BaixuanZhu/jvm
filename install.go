package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// install 下载并安装指定版本的 JDK
//
// version 支持三种写法:
//   - "21"          → 该大版本的最新 GA (走 fetchLatestAsset)
//   - "21.0.12"     → 精确小版本, 自动解析 build 号 (走 resolveReleaseName + fetchAssetByReleaseName)
//   - "jdk-21.0.12+8" → 完整 release name, 直接走 fetchAssetByReleaseName
//
// 流程:
//  1. 调 API 拿到 zip 地址和校验和
//  2. 下载到临时文件
//  3. SHA256 校验
//  4. 解压到 versionsDir (zip 内顶层目录原样保留, 例如 jdk-21.0.5+11)
//  5. 解压后从 zip 条目里读出真实顶层目录名, 作为已安装记录
func install(version string) error {
	if err := ensureDirs(); err != nil {
		return err
	}

	// 根据输入格式选择查询路径
	var asset *assetInfo
	isLatestMajor := false // 标记是否"大版本号取最新"场景 (决定 CDN 加速是否可用)
	v := strings.TrimSpace(version)
	if strings.ContainsAny(v, ".") || strings.HasPrefix(v, "jdk-") {
		// 精确版本: 先解析出完整 release_name, 再查
		releaseName, err := resolveReleaseName(v)
		if err != nil {
			return err
		}
		fmt.Printf("🔍 正在查询 Temurin %s ...\n", releaseName)
		asset, err = fetchAssetByReleaseName(releaseName)
		if err != nil {
			return err
		}
	} else {
		// 大版本号: 取最新 GA
		major, err := parseMajorVersion(v)
		if err != nil {
			return err
		}
		fmt.Printf("🔍 正在查询 Temurin JDK %d 的最新版本...\n", major)
		asset, err = fetchLatestAsset(major)
		if err != nil {
			return err
		}
		isLatestMajor = true // 这时 asset 就是该 major 最新版, CDN latest 链接匹配
	}

	// 检查这个版本是否已装过 (按目录名精确判断)
	target := filepath.Join(versionsDir, asset.directory)
	if info, _ := os.Stat(target); info != nil && info.IsDir() {
		fmt.Printf("⚠️  已安装 %s\n", asset.directory)
		fmt.Printf("   如需重装, 请先 jvm uninstall %s\n", asset.directory)
		return nil
	}

	sizeMB := remoteSizeMB(asset.zipURL)
	fmt.Printf("📦 将安装 Temurin %s\n", asset.Semver)
	if sizeMB > 0 {
		fmt.Printf("   大小约 %.1f MB\n", sizeMB)
	}

	// 1. 解析 CDN 直链 (仅"大版本取最新"场景, binary/latest 端点返回该 major 最新版)
	//    精确版本不能用 latest 端点 (文件名不同会下错), 直接用 asset.zipURL 兜底
	fmt.Print("🔗 解析下载地址...\n")
	cdnURL := asset.zipURL
	if isLatestMajor && asset.Major > 0 {
		if resolved, err := resolveCDNURL(asset.Major); err == nil {
			cdnURL = resolved
		}
	}

	// 2. 下载 (先试国内镜像, 失败回退官方链接)
	zipName := baseNameOfURL(asset.zipURL)
	zipPath := filepath.Join(jvmRoot, zipName)

	mirrorURL := mirrorDownloadURL(asset.zipURL, asset.Major)
	if err := downloadWithFallback(zipPath, mirrorURL, cdnURL); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	fmt.Println("✅ 下载完成")

	// 2. 先从 zip 里读出顶层目录名 (不依赖预测, 更健壮)
	topFolder, err := readTopFolder(zipPath)
	if err != nil {
		os.Remove(zipPath)
		return err
	}
	if topFolder == "" {
		os.Remove(zipPath)
		return fmt.Errorf("zip 内未找到顶层目录")
	}
	// 防御性校验: 顶层目录名必须是安全的标识符
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
	// asset.sha256 可能为空 (个别端点), 有则校验
	if asset.sha256 != "" && got != asset.sha256 {
		os.Remove(zipPath)
		return fmt.Errorf("校验失败\n   期望: %s\n   实际: %s", asset.sha256, got)
	}
	fmt.Println("通过")

	// 4. 解压 (先解到临时目录, 成功后原子替换, 避免半解压状态 / 文件占用)
	fmt.Print("📂 解压中... ")
	tmpExtract := filepath.Join(jvmRoot, ".tmp-extract-"+topFolder)
	os.RemoveAll(tmpExtract) // 清理可能残留的临时目录
	if err := unzipTo(zipPath, tmpExtract); err != nil {
		os.RemoveAll(tmpExtract)
		return fmt.Errorf("解压失败: %w", err)
	}
	// unzipTo 保留了 zip 内顶层目录, 所以真实内容在 tmpExtract/topFolder
	extractedDir := filepath.Join(tmpExtract, topFolder)
	finalDir := filepath.Join(versionsDir, topFolder)
	// 删除旧的目标目录 (重装场景)
	if _, err := os.Stat(finalDir); err == nil {
		os.RemoveAll(finalDir)
	}
	// 把解压出的顶层目录移到位
	if err := os.Rename(extractedDir, finalDir); err != nil {
		os.RemoveAll(tmpExtract)
		return fmt.Errorf("移动解压目录失败: %w", err)
	}
	os.RemoveAll(tmpExtract) // 清理空了的临时目录
	fmt.Println("完成")

	// 5. 清理 zip
	os.Remove(zipPath)

	// 6. 确认目标目录确实存在
	if _, err := os.Stat(filepath.Join(versionsDir, topFolder)); err != nil {
		return fmt.Errorf("解压后未找到 %s: %w", topFolder, err)
	}

	fmt.Printf("\n✅ 安装完成: %s\n", topFolder)
	fmt.Printf("   运行 `jvm use %d` 来切换到这个版本\n", asset.Major)
	return nil
}

// remoteSizeMB 用 HEAD 请求拿文件大小 (MB); 失败返回 0
func remoteSizeMB(url string) float64 {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := httpClient.Do(req)
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
// 这样国内用户默认走快的镜像, 镜像出问题时自动降级到官方
func downloadWithFallback(dest, mirrorURL, officialURL string) error {
	fmt.Print("⬇️  尝试国内镜像 (清华 TUNA)...\n")
	if err := downloadFile(mirrorURL, dest); err == nil {
		return nil
	}
	// 镜像失败, 清理半成品, 试官方
	if _, statErr := os.Stat(dest); statErr == nil {
		os.Remove(dest)
	}
	fmt.Print("⚠️  国内镜像失败, 回退到官方源 (可能较慢)...\n")
	return downloadFile(officialURL, dest)
}

// downloadFile 从 url 下载到本地路径, 带进度百分比显示
func downloadFile(url, dest string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent())

	resp, err := downloadClient.Do(req)
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

	// 如果知道总大小, 用进度 reader, 每收 5% 打印一次
	if total > 0 {
		pr := newProgressReader(resp.Body, total)
		defer pr.Close() // 透传关闭
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
	lastPrint int64 // 上次打印时的字节数
}

func newProgressReader(r io.ReadCloser, total int64) *progressReader {
	return &progressReader{reader: r, total: total}
}

// Close 透传给底层 ReadCloser
func (p *progressReader) Close() error { return p.reader.Close() }

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	p.received += int64(n)
	// 每收完 5% 或读完, 打印一次
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
	// 防 zip slip: 解压目标必须落在 dest 之内
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
