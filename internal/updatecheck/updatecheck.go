// Package updatecheck 在 jvm 启动时静默检查 GitHub 是否有新版本,
// 落后则打印一行提示 (运行 jvm upgrade 升级)。
//
// 设计要点:
//   - 24 小时节流: 每次启动先读上次检查时间戳 (~/.jvm/update-check.json),
//     未超过 24h 直接跳过, 不打 GitHub API, 避免拖慢每次启动。
//   - 全程静默: 网络失败、解析失败一律忽略, 绝不阻断主命令 (更新检查是
//     "锦上添花", 不能因为查不到版本而让 jvm use 等命令报错)。
//   - 只依赖 app 基础设施层 (版本号、HTTP、CompareVersions、LatestGitHubTag),
//     不依赖 upgrade 包 —— 更新检查只做 "查+比+提示", 不执行升级动作,
//     职责与 upgrade 的 "下载+替换 exe" 分离。
package updatecheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"jvm/internal/app"
	"jvm/internal/paths"
)

// githubRepo 是发布 jvm 的 GitHub 仓库 (owner/repo)。
// 与 internal/upgrade 的 githubRepo 保持一致; 此处独立声明, 避免跨包依赖。
const githubRepo = "BaixuanZhu/jvm"

// checkInterval 是两次更新检查之间的最小间隔 (24 小时)。
const checkInterval = 24 * time.Hour

// timestampFile 是上次检查时间戳的持久化路径 (~/.jvm/update-check.json)。
// 路径在包内自管, 不污染 paths 包 (paths 只管目录结构, 不该知道 "更新检查"
// 这个业务概念)。
var timestampFile = filepath.Join(paths.Root, "update-check.json")

// Run 执行一次静默更新检查。安全可并发调用, 任何错误均静默忽略。
//
// 应在 jvm 启动时 (main 的自举链路末尾) 调用, 不阻塞主命令。
func Run() {
	// recover 兜底: 即便内部逻辑 panic 也不影响主命令。
	defer func() { _ = recover() }()

	check(githubRepo, app.Version, checkInterval, time.Now(),
		app.LatestGitHubTag, fmt.Printf)
}

// tagFetcher 查询仓库的最新 tag (注入 app.LatestGitHubTag, 便于测试)。
type tagFetcher func(repo string) (string, error)

// printer 打印一行提示 (注入 fmt.Printf, 便于测试断言输出)。
type printer func(format string, args ...any) (int, error)

// check 是 Run 的可测核心: 串联节流→查询→比较→提示→写时间戳。
// 依赖 (tag 查询、当前时间、提示输出) 全部参数化, 使其可用假实现测试,
// 不打真实 GitHub API、不依赖真实时间。
func check(repo, current string, interval time.Duration, now time.Time,
	fetch tagFetcher, print printer) {

	if !shouldCheck(readLastCheck(), now, interval) {
		return
	}

	tag, err := fetch(repo)
	if err != nil {
		return // 网络问题不打扰用户
	}

	latest := stripV(tag)
	if app.CompareVersions(current, latest) < 0 {
		print("ℹ️  jvm 有新版本 %s (当前 %s), 运行 jvm upgrade 升级\n", tag, current)
	}
	writeLastCheck(now)
}

// shouldCheck 判断距上次检查是否已超过 interval, 是则应发起一次新检查。
// 纯函数, 便于表驱动测试。
func shouldCheck(last, now time.Time, interval time.Duration) bool {
	if last.IsZero() {
		return true // 从未检查过
	}
	return now.Sub(last) >= interval
}

// checkRecord 是时间戳文件的 JSON 结构。
type checkRecord struct {
	Last string `json:"last"` // RFC3339 时间戳
}

// readLastCheck 读取上次检查时间; 文件不存在/解析失败返回零值。
func readLastCheck() time.Time {
	data, err := os.ReadFile(timestampFile)
	if err != nil {
		return time.Time{}
	}
	var rec checkRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, rec.Last)
	if err != nil {
		return time.Time{}
	}
	return t
}

// writeLastCheck 把检查时间写入时间戳文件; 失败静默忽略。
func writeLastCheck(t time.Time) {
	rec := checkRecord{Last: t.UTC().Format(time.RFC3339)}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = os.WriteFile(timestampFile, data, 0o644)
}

// stripV 去掉版本号开头的 v/V 前缀, 如 "v0.6.1" → "0.6.1"。
func stripV(s string) string {
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		return s[1:]
	}
	return s
}
