// Package app 提供跨包共享的基础设施: 版本号、错误处理、版本号解析、
// HTTP User-Agent 和复用的 HTTP 客户端。
//
// 这些符号被几乎所有业务包依赖 (adoptium/jdk/upgrade/cmd 等),
// 集中在此处可避免业务包之间的循环依赖。
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version 是 jvm 自身的版本号。
// 构建时通过 ldflags 注入 (见 Makefile: -X jvm/internal/app.Version=...);
// 本地 go run / go build 未注入时使用此默认值。
var Version = "0.1.0-dev"

// Fail 打印错误信息到 stderr 并以退出码 1 退出。
// 供各命令处理函数在遇到不可恢复错误时调用。
func Fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

// ParseMajorVersion 把用户输入解析为整数大版本号 (如 "21" -> 21)。
// 非正整数或非数字会返回错误。
func ParseMajorVersion(s string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("无效的版本号: %s (请输入大版本号, 例如 21、17、11)", s)
	}
	return v, nil
}

// UserAgent 返回统一的 HTTP User-Agent 字符串。
// 各处网络请求 (Adoptium API、GitHub API、文件下载) 都应引用它, 保持版本号一致。
func UserAgent() string {
	return fmt.Sprintf("jvm/%s (windows java version manager)", Version)
}

// HTTPClient 用于 API 查询 (轻量 JSON 请求, 60s 超时足够)。
// 复用连接, 避免每次查询都建立新连接。
var HTTPClient = &http.Client{Timeout: 60 * time.Second}

// DownloadClient 用于下载大文件 (JDK zip), 不设整体超时。
// 超时控制交给下载过程中的读超时, 避免大文件下载被误杀。
var DownloadClient = &http.Client{Timeout: 0}

// HTTPGetJSON 发 GET 请求并返回 body (供各 provider 查询轻量 JSON 元数据)。
// 统一 User-Agent 和 Accept 头, 非 200 报错。
func HTTPGetJSON(u string) ([]byte, error) {
	return httpGet(u, "application/json")
}

// HTTPGetText 发 GET 请求并返回 body (供各 provider 拉取纯文本元数据,
// 如 Microsoft 的 .sha256sum.txt 校验文件)。
func HTTPGetText(u string) ([]byte, error) {
	return httpGet(u, "text/plain")
}

// httpGet 是 HTTPGetJSON/HTTPGetText 的共享实现。
func httpGet(u, accept string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("Accept", accept)

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API 返回 %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// CompareVersions 按语义版本比较两个版本号, 返回 -1/0/1 (a<b / a==b / a>b)。
//
// 输入应是干净的版本号 (不含 v 前缀、distro 前缀), 如 "0.6.1" / "21.0.5+11";
// 调用方负责预处理 (剥 "v"/"V" 前缀)。按非数字字符分段, 逐段 Atoi 比较,
// 短的版本后续段视为 0。非数字段按 0 处理, 不打断比较。
//
// 纯函数, 便于表驱动测试。
//
// 注: junction 包另有 semverLess, 但它的输入是版本目录名 (含 distro- / jdk- 前缀,
// 内部先剥前缀), 语义与用途不同, 故保留独立实现; 本函数供纯版本号场景 (如自更新
// 比对) 使用。
func CompareVersions(a, b string) int {
	pa := versionSegments(a)
	pb := versionSegments(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionSegments 把版本号按非数字字符拆成数字段切片, 如 "0.6.1" → [0,6,1]。
// 非法段按 0 处理。纯函数。
func versionSegments(s string) []int {
	var parts []int
	for _, seg := range strings.FieldsFunc(s, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		if seg == "" {
			continue
		}
		n, err := strconv.Atoi(seg)
		if err != nil {
			n = 0
		}
		parts = append(parts, n)
	}
	return parts
}

// LatestGitHubTag 查询 GitHub 仓库的最新 release tag_name (如 "v0.6.1")。
// 用短超时 context (5s) 避免拖慢调用方 (如启动时的静默更新检查)。
// repo 是 "owner/repo" 格式。返回的 tag 保留 "v" 前缀, 由调用方决定是否剥离。
func LatestGitHubTag(repo string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	return fetchTag(ctx, u)
}

// fetchTag 是 LatestGitHubTag 的核心: GET 目标 URL, 解析 tag_name。
// 拆出来便于用 httptest 测试 (避免硬编码 api.github.com)。
func fetchTag(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}

	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}
	return body.TagName, nil
}
