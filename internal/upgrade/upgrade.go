// Package upgrade 通过 GitHub Release 检查并更新 jvm 自身。
//
// 机制: 调 GitHub API /repos/{owner}/{repo}/releases/latest 拿最新 release,
// 比对 tag_name 和当前版本, 下载约定的 asset (zip), 解压出 jvm.exe 并替换。
// Windows 不能覆盖运行中的 exe, 但能重命名: 旧 exe → .bak, 新 exe 移到位;
// .bak 若因旧进程占用没删掉, 由 CleanupStaleBak 在下次启动时清理。
//
// 部署说明 (发 release 时):
//   - tag 用 v0.2.0 格式
//   - 上传 asset: jvm-windows-amd64.zip (zip 里放单个 jvm.exe)
package upgrade

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
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
// 完整名 = jvm-{GOOS}-{GOARCH}.zip, 例如 jvm-windows-amd64.zip
func expectedAssetName() string {
	return fmt.Sprintf("jvm-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
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
	if err := jdk.DownloadFile(assetURL, tmpZip, jdk.WithRetries(3)); err != nil {
		app.Fail("下载失败: " + err.Error())
	}
	defer os.Remove(tmpZip)

	// SHA256 校验: 从 release 的 checksums.txt asset 里取出期望值, 与本地下载文件比对。
	// 旧版 release 若没有 checksums.txt, 打印警告后继续 (不阻断向后兼容)。
	if expectedHash, ok := fetchExpectedChecksum(rel, want); ok {
		fmt.Print("🔐 校验 SHA256... ")
		got, err := fileSHA256(tmpZip)
		if err != nil {
			app.Fail("计算下载文件 SHA256 失败: " + err.Error())
		}
		if got != expectedHash {
			os.Remove(tmpZip)
			app.Fail(fmt.Sprintf("校验失败\n   期望: %s\n   实际: %s", expectedHash, got))
		}
		fmt.Println("通过")
	} else {
		fmt.Println("⚠️  该 release 未提供 checksums.txt, 跳过完整性校验。")
	}

	fmt.Print("📂 解压中... ")
	// 解压到当前 exe 同目录, 避免跨盘符 rename 失败 (os.Rename 走 MoveFileEx, 不能跨卷)
	selfExe, err := os.Executable()
	if err != nil {
		app.Fail("定位当前 exe 失败: " + err.Error())
	}
	selfDir := filepath.Dir(selfExe)
	tmpExe, err := extractSingleExe(tmpZip, selfDir)
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

// extractSingleExe 从 zip 里解压出单个 jvm.exe 到 dir 目录下的临时文件, 返回其路径。
// 把临时文件落在 dir (通常是当前 exe 同目录) 是为了保证后续 os.Rename 不跨卷 ——
// Windows 的 MoveFileEx 跨卷时会退化成复制+删除, 对运行中的 exe 会失败。
func extractSingleExe(zipPath, dir string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	for _, f := range reader.File {
		if filepath.Base(f.Name) != "jvm.exe" {
			continue
		}
		out, err := os.CreateTemp(dir, "jvm-new-*.exe")
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

// fetchExpectedChecksum 从 release 的 checksums.txt asset 里解析出指定文件的期望 SHA256。
// checksums.txt 采用 GNU coreutils sha256sum 格式: "<hash>  <filename>"。
// 找不到 checksums.txt asset 或文件名不匹配时返回 ("", false)。
func fetchExpectedChecksum(rel *githubRelease, filename string) (string, bool) {
	var checksumURL string
	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			checksumURL = a.BrowserDownloadURL
			break
		}
	}
	if checksumURL == "" {
		return "", false
	}

	tmpChecksum := filepath.Join(os.TempDir(), "jvm-checksums.txt")
	if err := jdk.DownloadFile(checksumURL, tmpChecksum, jdk.WithRetries(3)); err != nil {
		return "", false
	}
	defer os.Remove(tmpChecksum)

	data, err := os.ReadFile(tmpChecksum)
	if err != nil {
		return "", false
	}
	return parseChecksum(string(data), filename)
}

// parseChecksum 从 sha256sum 格式文本里找出 filename 对应的 hash。
// GNU coreutils 有两种格式:
//
//	"<hash>  <filename>"  text 模式 (两个空格, 无星号)
//	"<hash> *<filename>"  binary 模式 (一个空格 + 前导星号)
//
// Windows Git Bash 自带的 sha256sum 默认走 binary 模式 (输出 *前缀),
// 故这里要剥掉文件名前导 '*' 再比较。纯函数, 便于表驱动测试。
func parseChecksum(text, filename string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 拆成 hash 和文件名两部分
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		hash, name := fields[0], fields[1]
		name = strings.TrimPrefix(name, "*") // 兼容 binary 模式的 *前缀
		if name == filename {
			return hash, true
		}
	}
	return "", false
}

// fileSHA256 计算文件的 SHA256 (十六进制小写)。
// 与 internal/jdk 包内的同名私有函数逻辑一致, 此处独立实现避免改 jdk 公共 API。
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

// CleanupStaleBak 清理可能残留的 jvm.exe.bak。
//
// replaceSelf 在升级时把旧 exe 重命名为 .bak, 替换成功后立即尝试删除;
// 但旧 exe 仍在运行时删不掉 (Windows 文件锁), 故 .bak 可能残留。
// 本函数在 jvm 每次启动时调用, 清掉上次升级遗留的 .bak, 兑现包注释的"启动时清理"承诺。
// 失败静默忽略 (.bak 仍被占用或权限不足不阻断主流程)。
func CleanupStaleBak() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	cleanupBakAt(exePath)
}

// cleanupBakAt 清理 exePath 对应的 .bak 文件。纯路径操作, 便于表驱动测试。
// 返回是否实际删除了文件 (不存在视为无需清理)。
func cleanupBakAt(exePath string) bool {
	bakPath := exePath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		return false // 不存在, 无需清理
	}
	return os.Remove(bakPath) == nil
}
