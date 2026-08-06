package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// 本文件实现 jvm upgrade: 通过 GitHub Release 检查并更新 jvm 自身。
//
// 机制:
//  1. 调 GitHub API /repos/{owner}/{repo}/releases/latest 拿最新 release
//  2. 比对 tag_name (如 v0.2.0) 和当前 version 常量
//  3. 下载约定的 asset (jvm-windows-amd64.exe.zip), 解压出 jvm.exe
//  4. Windows 上不能覆盖运行中的 exe, 但可以重命名它:
//     把旧 exe 重命名为 .bak, 把新 exe 移到目标位置, 启动时清理 .bak
//
// 部署说明 (发 release 时):
//   - 在 GitHub 建 release, tag 用 v0.2.0 格式
//   - 上传 asset, 命名 jvm-windows-amd64.exe.zip, zip 里放单个 jvm.exe
//   - 修改下面 githubRepo 常量为你的 owner/repo

// githubRepo 是发布 jvm 的 GitHub 仓库 (owner/repo 格式)。
// 占位值, 建好仓库后改成你自己的, 例如 "zbxComputer/jvm"。
const githubRepo = "yourname/jvm"

// assetNamePrefix 是 release asset 的文件名前缀约定。
// 完整名 = 前缀 + GOOS-GOARCH + .exe.zip, 例如 jvm-windows-amd64.exe.zip
func expectedAssetName() string {
	return fmt.Sprintf("jvm-%s-%s.exe.zip", runtime.GOOS, runtime.GOARCH)
}

// githubRelease 对应 GitHub API releases/latest 的响应 (只取需要的字段)
type githubRelease struct {
	TagName string `json:"tag_name"` // 如 "v0.2.0"
	Name    string `json:"name"`     // release 标题
	HTMLURL string `json:"html_url"` // release 页面链接
	Assets  []struct {
		Name          string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size          int64  `json:"size"`
	} `json:"assets"`
}

// cmdUpgrade 处理 jvm upgrade: 检查并更新到最新版
func cmdUpgrade() {
	if githubRepo == "yourname/jvm" {
		fmt.Println("⚠️  自更新未配置。")
		fmt.Println("   请先在 selfupdate.go 里把 githubRepo 改成你的 owner/repo,")
		fmt.Println("   并在 GitHub 发布 release (tag 用 vX.Y.Z, 上传 jvm-windows-amd64.exe.zip)。")
		fmt.Println("   当前版本: jvm", version)
		return
	}

	fmt.Printf("🔍 正在检查 %s 的最新版本...\n", githubRepo)
	rel, err := fetchLatestGitHubRelease()
	if err != nil {
		fail("检查更新失败: " + err.Error())
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")
	fmt.Printf("   最新版本: %s (当前: %s)\n", latestVersion, version)

	if latestVersion == version {
		fmt.Println("✅ 已经是最新版本, 无需更新。")
		return
	}

	// 找匹配当前平台的 asset
	assetURL := ""
	want := expectedAssetName()
	for _, a := range rel.Assets {
		if a.Name == want {
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		fail(fmt.Sprintf("最新 release 里没有找到 asset %s。\n   请到 %s 手动下载。", want, rel.HTMLURL))
	}

	fmt.Printf("⬇️  下载 %s ...\n", want)
	tmpZip := filepath.Join(os.TempDir(), "jvm-upgrade-"+latestVersion+".zip")
	if err := downloadFile(assetURL, tmpZip); err != nil {
		fail("下载失败: " + err.Error())
	}
	defer os.Remove(tmpZip)

	fmt.Print("📂 解压中... ")
	tmpExe, err := extractSingleExe(tmpZip)
	if err != nil {
		fail("解压失败: " + err.Error())
	}
	defer os.Remove(tmpExe)
	fmt.Println("完成")

	// 替换当前运行的 exe
	fmt.Print("🔄 替换 jvm.exe ... ")
	if err := replaceSelf(tmpExe); err != nil {
		fail("替换失败: " + err.Error())
	}
	fmt.Println("完成")

	fmt.Printf("\n✅ 已更新到 %s\n", latestVersion)
	fmt.Println("   新版本将在下次运行 jvm 时生效。")
}

// fetchLatestGitHubRelease 调 GitHub API 拿最新 release
func fetchLatestGitHubRelease() (*githubRelease, error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}

	var rel githubRelease
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}
	return &rel, nil
}

// extractSingleExe 从 zip 里解压出单个 jvm.exe 到临时文件, 返回其路径
func extractSingleExe(zipPath string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	for _, f := range reader.File {
		name := filepath.Base(f.Name)
		if name != "jvm.exe" {
			continue
		}
		// 解压到临时文件
		out, err := os.CreateTemp("", "jvm-new-*.exe")
		if err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			out.Close()
			os.Remove(out.Name())
			return "", err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			os.Remove(out.Name())
			return "", err
		}
		rc.Close()
		out.Close()
		return out.Name(), nil
	}
	return "", fmt.Errorf("zip 里没有找到 jvm.exe")
}

// replaceSelf 用新 exe 替换当前运行的 jvm.exe
// Windows 标准做法: 重命名旧 exe 为 .bak (运行中的 exe 不能删/覆盖, 但能重命名),
// 再把新 exe 移到目标位置。启动时清理可能残留的 .bak。
func replaceSelf(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位当前 exe: %w", err)
	}
	currentExe, _ = filepath.Abs(currentExe)

	bakPath := currentExe + ".bak"
	// 先清理可能残留的旧 .bak (上次更新中断留下的)
	os.Remove(bakPath)

	// 1. 重命名当前 exe 为 .bak
	if err := os.Rename(currentExe, bakPath); err != nil {
		return fmt.Errorf("重命名旧 exe 失败 (文件可能被占用): %w", err)
	}

	// 2. 把新 exe 移到目标位置
	if err := os.Rename(newExePath, currentExe); err != nil {
		// 移动失败, 回滚: 把 .bak 改回去
		os.Rename(bakPath, currentExe)
		return fmt.Errorf("移动新 exe 失败: %w", err)
	}

	// 3. 尝试删除 .bak (Windows 上正在运行的文件可能删不掉, 忽略错误)
	os.Remove(bakPath)
	return nil
}

// userAgent 返回统一的 HTTP User-Agent 字符串
// 各处网络请求都应引用它, 保持版本号一致 (自更新、API 查询、下载)
func userAgent() string {
	return fmt.Sprintf("jvm/%s (windows java version manager)", version)
}

// 占位: 避免 time 包未使用的编译错误 (下载超时用 downloadClient, 此处预留)
var _ = time.Second
