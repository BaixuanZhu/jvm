// Package pinrc 处理项目级 JDK 版本固定文件 (.jvmrc 及生态兼容格式)。
//
// 用途: 在项目根目录放一个版本固定文件, 内容为版本号 (如 "21" 或
// "corretto@21.0.12.8.1"), 之后 `jvm use` 无参数时自动读取它切换版本,
// 团队成员无需各自记版本号。`jvm pin` 负责写入 .jvmrc。
//
// 除 jvm 原生的 .jvmrc 外, 还兼容读取团队里常见的外来格式 (团队成员
// 用 sdkman / asdf / mise 时提交到仓库的文件), 让 jvm 用户 cd 进任何
// 仓库都能自动切换:
//   - .java-version  纯版本号一行 (历史习惯, 与 .jvmrc 同构)
//   - .tool-versions asdf / mise (多行 "工具 版本", 取 java 行; 版本段可带
//     发行版前缀, 如 temurin-21.0.2+11)
//   - .sdkmanrc      sdkman (多行 "key=value", 取 java 行; 值带发行版标识
//     后缀, 如 21.0.2-tem, 映射为 jvm 的 distro@version)
//
// 查找规则: 从当前目录逐级向上 (与 .nvmrc / .ruby-version 一致, 支持
// monorepo 子目录场景); 每层目录内按 CandidateFiles 优先级检查, 命中
// 第一个能解析出 Java 版本的文件即止。解析不出可用版本的文件 (空文件 /
// 无 java 行 / 无法识别的发行版标识) 视为未命中, 继续找其余候选与上层。
//
// 版本号格式与 CLI 一致 (见 app.ParseVersionSpec), 解析出的 spec 原样交给
// 版本解析链路, 本包不做语义校验 (半截版本号的宽松匹配由 cmd 层按来源分流)。
// jvm pin 只写 .jvmrc, 不写外来格式。
package pinrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Filename 是 jvm 原生的项目级版本固定文件名 (pin 命令的写入目标)。
const Filename = ".jvmrc"

// CandidateFiles 是版本固定文件的候选清单, 按优先级排列 (每层目录内依次
// 检查, 命中第一个即止)。shell 集成脚本 (internal/shell) 的 rc 检测清单
// 也由本切片生成 —— 两处必须一致, 改动只动这里。
var CandidateFiles = []string{".jvmrc", ".java-version", ".tool-versions", ".sdkmanrc"}

// Source 标识命中的文件类型, 枚举值与 CandidateFiles 的下标一一对应。
// cmd 层按来源分流匹配语义: .jvmrc 保持严格版本匹配, 外来格式允许半截
// 版本号降级到大版本取组内最新 (sdkman/asdf 生态普遍不带 build 号)。
type Source int

const (
	SrcJVMRC        Source = iota // .jvmrc (jvm 原生)
	SrcJavaVersion                // .java-version
	SrcToolVersions               // .tool-versions (asdf / mise)
	SrcSDKMANrc                   // .sdkmanrc (sdkman)
)

// FindUp 从 startDir 起逐级向上查找版本固定文件: 每层目录内按
// CandidateFiles 优先级检查, 命中第一个能解析出 Java 版本的文件即返回
// 归一化后的 "[distro@]version" spec、来源类型与文件完整路径。
// 未找到返回 found=false。startDir 为空时直接当作未找到。
//
// 与 .nvmrc / .ruby-version 一致: 子目录里也会命中上层项目根的文件。
func FindUp(startDir string) (spec string, src Source, foundPath string, found bool) {
	if startDir == "" {
		return "", 0, "", false
	}
	dir := startDir
	for {
		for i, name := range CandidateFiles {
			p := filepath.Join(dir, name)
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if s, ok := parseFile(Source(i), string(b)); ok {
				return s, Source(i), p, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir { // 到达文件系统根, 停止
			return "", 0, "", false
		}
		dir = parent
	}
}

// parseFile 按文件类型分发到对应解析器。ok=false 表示该文件不含可用的
// Java 版本信息 (空 / 全注释 / 无 java 行 / 无法识别的发行版标识),
// 调用方应继续尝试其余候选。纯函数, 便于表驱动测试。
func parseFile(src Source, content string) (string, bool) {
	switch src {
	case SrcToolVersions:
		return parseToolVersions(content)
	case SrcSDKMANrc:
		return parseSDKMANrc(content)
	default: // .jvmrc / .java-version: 单行版本号, 同构解析
		s, err := Parse(content)
		return s, err == nil
	}
}

// Parse 解析 .jvmrc / .java-version 这类单行版本文件的内容: 取第一个
// 非空、非 # 注释的行, 去掉首尾空白与 UTF-8 BOM, 返回版本号
// (如 "corretto@21.0.12.8.1" 或 "21")。
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
	return "", fmt.Errorf("内容为空或全是注释, 请写入版本号 (如 21 或 corretto@21)")
}

// asdfDistros 是 .tool-versions 版本段可能带的发行版前缀 (asdf 插件名,
// 与 jvm 的 distro 名一致)。按长度降序尝试: temurin-ea 必须先于 temurin,
// 否则短前缀会把 "temurin-ea-28+14" 错截成版本 "ea-28+14"。
var asdfDistros = []string{"temurin-ea", "graalvm", "microsoft", "liberica", "corretto", "temurin", "zulu"}

// parseToolVersions 解析 .tool-versions (asdf / mise): 多行 "工具 版本",
// 取 java 行的第一个版本段。版本段可带发行版前缀 ("temurin-21.0.2+11" →
// "temurin@21.0.2+11"), 前缀须是 jvm 已知发行版, 否则整段按版本号对待
// (发行版缺省 temurin)。文件里没有 java 行 → 未命中 (非 Java 项目不挡
// 上层/其余候选)。纯函数, 便于表驱动测试。
func parseToolVersions(content string) (string, bool) {
	content = strings.TrimPrefix(content, "\uFEFF")
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "java") {
			continue
		}
		ver := fields[1] // 多版本并列 (如 "java 21 17") 取第一个
		for _, d := range asdfDistros {
			if p := d + "-"; strings.HasPrefix(ver, p) && len(ver) > len(p) {
				return d + "@" + ver[len(p):], true
			}
		}
		return ver, true // 无已知前缀, 整段当版本号
	}
	return "", false
}

// sdkmanDistros 把 sdkman 的 java 发行版标识后缀映射到 jvm 的发行版名
// (sdkman 生态里版本值形如 "21.0.2-tem")。
var sdkmanDistros = map[string]string{
	"tem":    "temurin",
	"amzn":   "corretto",
	"ms":     "microsoft",
	"zul":    "zulu",
	"librca": "liberica",
	"graal":  "graalvm",
}

// parseSDKMANrc 解析 .sdkmanrc (sdkman): 多行 "key=value", 取 java 行。
// 值尾部的发行版标识按 sdkmanDistros 映射 ("21.0.2-tem" → "temurin@21.0.2");
// 无标识的裸值 (如 "21") 按缺省发行版处理。带 jvm 不认识的标识 (如
// -oracle / -open, jvm 没有对应发行版) → 未命中, 不做猜测切换。
// 文件里没有 java 行 → 未命中。纯函数, 便于表驱动测试。
func parseSDKMANrc(content string) (string, bool) {
	content = strings.TrimPrefix(content, "\uFEFF")
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "java") {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		// 发行版标识在最后一个 "-" 之后 (sdkman 约定)
		if dash := strings.LastIndexByte(v, '-'); dash >= 0 {
			ver, id := v[:dash], v[dash+1:]
			if d, known := sdkmanDistros[id]; known && ver != "" {
				return d + "@" + ver, true
			}
			return "", false // 未知标识或有值无版本: jvm 无法满足
		}
		return v, true // 裸值, 缺省发行版
	}
	return "", false
}

// Write 把 spec 写入 dir/.jvmrc (覆盖), 带一行注释说明用途。
// spec 应为合法的版本号 (调用方负责校验), 原样写入。
// 只写 jvm 原生格式, 不写外来格式 (它们归各自的工具管)。
func Write(dir, spec string) error {
	content := "# jvm pin: 此目录使用的 JDK 版本, jvm use 无参时读取\n" + spec + "\n"
	return os.WriteFile(filepath.Join(dir, Filename), []byte(content), 0o644)
}
