// 本文件定义跨 provider 共享的下载契约与版本号解析。
//
// app 包被几乎所有业务包依赖 (provider/jdk/cmd 等), 把发行版无关的
// Asset / Release / VersionSpec 放在这里, 可避免业务包之间的循环依赖。

package app

import (
	"fmt"
	"strings"
)

// Asset 是 provider 适配器产出的、jdk 层消费的下载契约。
// 各发行版 (Temurin/Corretto/Microsoft/...) 的适配器把各自 API 的返回
// 统一翻译成 *Asset, jdk 包据此完成下载/校验/解压, 对发行版细节无感。
type Asset struct {
	// Semver 是 provider 返回的原始 semver, 如 "21.0.5+11" / "21.0.12.8.1"。
	// 仅供展示, 不用作目录名 (目录名用 ReleaseName)。
	Semver string

	// Major 是大版本号, 如 21。用于 install 后提示 "运行 jvm use 21"。
	Major int

	// ZipURL 是官方下载直链。jdk 包用这个 URL 下载 zip。
	ZipURL string

	// MirrorURL 是国内镜像直链 (可选)。非空时 jdk 包走 "镜像优先, 失败回退官方"
	// 双源策略; 为空时直接走 ZipURL (适用于无国内镜像的发行版)。
	MirrorURL string

	// SHA256 是官方/镜像提供的校验和。为空时 jdk 包跳过校验 (各 provider 应尽量填)。
	SHA256 string

	// ReleaseName 是规整后的版本号, 用于本地目录命名 (不含 distro 前缀)。
	// 由 provider 的 ShortSemver 产出, 如 "21.0.5+11" / "21.0.12.8.1"。
	// jdk 包最终目录名是 "{Distro}-{ReleaseName}"。
	ReleaseName string

	// Distro 是发行版标识, 与 provider.Name() 一致, 如 "temurin" / "corretto"。
	Distro string
}

// Release 是一个大版本概要, 供 available 命令展示。
type Release struct {
	Major int  // 大版本号, 如 21
	LTS   bool // 是否长期支持版本
}

// VersionSpec 是用户输入解析后的版本指定, 跨 cmd/jdk/provider 共享。
type VersionSpec struct {
	// Distro 是发行版名, 如 "temurin" / "corretto"。
	// 无 distro@ 前缀时由 ParseVersionSpec 填默认值。
	Distro string

	// Version 是原始版本串, 如 "21" (大版本) / "21.0.12+8" (完整版本, 含 build 号)。
	// 由各 provider 自己解析 (ResolveReleaseName), 不在此预处理。
	Version string
}

// DefaultDistro 是无 distro@ 前缀时的默认发行版。
// 保持与 provider.Default 一致; 此处单独定义以避免 provider 包的反向依赖。
const DefaultDistro = "temurin"

// ParseVersionSpec 把用户输入解析为 VersionSpec。
//
// 输入格式: "[distro@]version", 例如:
//   - "21"                  → {Distro: "temurin", Version: "21"}
//   - "21.0.12+8"           → {Distro: "temurin", Version: "21.0.12+8"}
//   - "corretto@21"         → {Distro: "corretto", Version: "21"}
//   - "microsoft@21.0.12+8" → {Distro: "microsoft", Version: "21.0.12+8"}
//
// 无 @ 前缀时 Distro 取 DefaultDistro。空输入报错。
// 不校验 distro 是否已注册 (那是 provider.Get 的职责), 也不校验版本号格式
// (那是各 provider 的职责)。
//
// 纯函数, 便于表驱动测试。
func ParseVersionSpec(input string) (VersionSpec, error) {
	v := strings.TrimSpace(input)
	if v == "" {
		return VersionSpec{}, fmt.Errorf("版本号不能为空")
	}

	// 只认第一个 @ 作为 distro/version 分隔符, 允许版本号本身含 @ (实际不会)
	// at >= 0 表示存在 @; at == 0 (@21) 时 distro 为空, 由下方非空校验拦截
	if at := strings.IndexByte(v, '@'); at >= 0 {
		distro := strings.TrimSpace(v[:at])
		version := strings.TrimSpace(v[at+1:])
		if distro == "" || version == "" {
			return VersionSpec{}, fmt.Errorf("无效的版本指定 %q (格式: [distro@]version)", input)
		}
		return VersionSpec{Distro: distro, Version: version}, nil
	}

	return VersionSpec{Distro: DefaultDistro, Version: v}, nil
}
