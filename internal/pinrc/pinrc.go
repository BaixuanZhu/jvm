// Package pinrc 处理 .jvmrc 文件: 项目级 JDK 版本固定。
//
// 用途: 在项目根目录放一个 .jvmrc, 内容为版本号 (如 "21" 或 "corretto@21.0.12.8.1"),
// 之后 `jvm use` 无参数时自动读取它切换版本, 团队成员无需各自记版本号。
// `jvm pin` 负责写入该文件。
//
// 查找规则: 从当前目录逐级向上查找第一个 .jvmrc (与 .nvmrc / .ruby-version 一致,
// 支持 monorepo 子目录场景)。
//
// 文件格式: 一行版本号, 支持 # 注释行与空行。版本号格式与 CLI 一致
// (见 app.ParseVersionSpec), 内容原样交给版本解析链路, 本包不做语义校验。
package pinrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Filename 是项目级版本固定文件名。
const Filename = ".jvmrc"

// FindUp 从 startDir 起逐级向上查找 .jvmrc, 命中第一个即返回其原始内容与路径。
// 未找到返回 ("", "", false)。startDir 为空或不存在时直接当作未找到。
//
// 与 .nvmrc / .ruby-version 一致: 子目录里也会命中上层项目根的 .jvmrc。
func FindUp(startDir string) (content, foundPath string, found bool) {
	if startDir == "" {
		return "", "", false
	}
	dir := startDir
	for {
		p := filepath.Join(dir, Filename)
		if b, err := os.ReadFile(p); err == nil {
			return string(b), p, true
		}
		parent := filepath.Dir(dir)
		if parent == dir { // 到达文件系统根, 停止
			return "", "", false
		}
		dir = parent
	}
}

// Parse 解析 .jvmrc 内容: 取第一个非空、非 # 注释的行, 去掉首尾空白与 UTF-8 BOM,
// 返回版本号 (如 "corretto@21.0.12.8.1" 或 "21")。
// 内容全空或全是注释时返回错误。纯函数, 便于表驱动测试。
func Parse(content string) (string, error) {
	content = strings.TrimPrefix(content, "\uFEFF") // 容错 UTF-8 BOM (部分编辑器/PowerShell 会写)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line, nil
	}
	return "", fmt.Errorf(".jvmrc 内容为空或全是注释, 请写入版本号 (如 21 或 corretto@21)")
}

// Write 把 spec 写入 dir/.jvmrc (覆盖), 带一行注释说明用途。
// spec 应为合法的版本号 (调用方负责校验), 原样写入。
func Write(dir, spec string) error {
	content := "# jvm pin: 此目录使用的 JDK 版本, jvm use 无参时读取\n" + spec + "\n"
	return os.WriteFile(filepath.Join(dir, Filename), []byte(content), 0o644)
}
