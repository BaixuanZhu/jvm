// Package app 提供跨包共享的基础设施: 版本号、错误处理、版本号解析、
// HTTP User-Agent 和复用的 HTTP 客户端。
//
// 这些符号被几乎所有业务包依赖 (adoptium/jdk/upgrade/cmd 等),
// 集中在此处可避免业务包之间的循环依赖。
package app

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version 是 jvm 自身的版本号
const Version = "0.1.0"

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
