// Package upgrade 通过 GitHub Release 检查并更新 jvm 自身。
//
// 机制: 调 GitHub API /repos/{owner}/{repo}/releases/latest 拿最新 release,
// 比对 tag_name 和当前版本, 下载约定的 asset (zip), 解压出 jvm.exe 并替换。
// Windows 不能覆盖运行中的 exe, 但能重命名: 旧 exe → .bak, 新 exe 移到位, 启动时清理。
//
// 部署说明 (发 release 时):
//   - tag 用 v0.2.0 格式
//   - 上传 asset: jvm-windows-amd64.exe.zip (zip 里放单个 jvm.exe)
package upgrade

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

	"jvm/internal/app"
	"jvm/internal/jdk"
)

// githubRepo 是发布 jvm 的 GitHub 仓库 (owner/repo 格式)。
// 建好仓库后改成你自己的; 未配置 (占位值) 时 Run 会给出提示。
const githubRepo = "BaixuanZhu/jvm"

// githubRelease 对应 GitHub API releases/latest 的响应 (只取需要的字段)
type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// expectedAssetName 返回当前平台约定的 release asset 文件名
// 完整名 = jvm-{GOOS}-{GOARCH}.exe.zip, 例如 jvm-windows-amd64.exe.zip
func expectedAssetName() string {
	return fmt.Sprintf("jvm-%s-%s.exe.zip", runtime.GOOS, runtime.GOARCH)
}

// Run 检查并更新到最新版 (供 cmd upgrade 调用)。
func Run() {
	fmt.Printf("🔍 正在检查 %s 的最新版本...\n", githubRepo)
	rel, err := fetchLatestGitHubRelease()
	if err != nil {
		app.Fail("检查更新失败: " + err.Error())
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")
	fmt.Printf("   最新版本: %s (当前: %s)\n", latestVersion, app.Version)

	if latestVersion == app.Version {
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
		app.Fail(fmt.Sprintf("最新 release 里没有找到 asset %s。\n   请到 %s 手动下载。", want, rel.HTMLURL))
	}

	fmt.Printf("⬇️  下载 %s ...\n", want)
	tmpZip := filepath.Join(os.TempDir(), "jvm-upgrade-"+latestVersion+".zip")
	if err := jdk.DownloadFile(assetURL, tmpZip); err != nil {
		app.Fail("下载失败: " + err.Error())
	}
	defer os.Remove(tmpZip)

	fmt.Print("📂 解压中... ")
	tmpExe, err := extractSingleExe(tmpZip)
	if err != nil {
		app.Fail("解压失败: " + err.Error())
	}
	defer os.Remove(tmpExe)
	fmt.Println("完成")

	fmt.Print("🔄 替换 jvm.exe ... ")
	if err := replaceSelf(tmpExe); err != nil {
		app.Fail("替换失败: " + err.Error())
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
	req.Header.Set("User-Agent", app.UserAgent())
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := app.HTTPClient.Do(req)
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
		if filepath.Base(f.Name) != "jvm.exe" {
			continue
		}
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

// replaceSelf 用新 exe 替换当前运行的 jvm.exe。
// Windows 标准做法: 重命名旧 exe 为 .bak (运行中的 exe 不能覆盖, 但能重命名),
// 再把新 exe 移到目标位置。
func replaceSelf(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位当前 exe: %w", err)
	}
	currentExe, _ = filepath.Abs(currentExe)

	bakPath := currentExe + ".bak"
	os.Remove(bakPath) // 清理可能残留的旧 .bak

	// 1. 重命名当前 exe 为 .bak
	if err := os.Rename(currentExe, bakPath); err != nil {
		return fmt.Errorf("重命名旧 exe 失败 (文件可能被占用): %w", err)
	}
	// 2. 把新 exe 移到目标位置
	if err := os.Rename(newExePath, currentExe); err != nil {
		os.Rename(bakPath, currentExe) // 回滚
		return fmt.Errorf("移动新 exe 失败: %w", err)
	}
	// 3. 尝试删除 .bak (可能删不掉, 忽略错误)
	os.Remove(bakPath)
	return nil
}
