// Package provider 定义 JDK 发行版的抽象解析层。
//
// 每个发行版 (Temurin/Corretto/Microsoft/...) 的 URL、API 形态、返回格式
// 各不相同, 由各自包内的适配器 (temurin/corretto/microsoft) 实现统一接口,
// 把差异消化在适配器内部, 上层 (cmd/jdk) 对发行版细节无感。
//
// 设计要点:
//   - Provider 接口含 Name/DisplayName/LatestPatch/Available/Resolve/
//     ListVersions 六个方法, 都是上层必需的基本属性与查询能力。
//   - ShortSemver/ResolveReleaseName 走嵌入 Base 基类的默认实现 (多数 provider
//     透传即可), 适配器只需 override 真正不同的方法。这是 Go 社区惯例
//     (database/sql/driver、fs.FS 的扩展都这么组织)。
//   - 可选能力拆成独立接口, 上层按需类型断言, 不污染核心接口:
//     Configurable (接收全局下载配置: 目标架构/镜像) 由 ConfigureAll 统一分发。
//   - 注册表用 init() 自注册, 新增 provider 只需在 main.go 空白导入。
package provider

import (
	"fmt"
	"sort"
	"sync"

	"jvm/internal/app"
)

// Provider 是每个发行版适配器必须实现的核心接口。
//
// Name + DisplayName 是标识/展示; LatestPatch/Available/Resolve/ListVersions
// 是查询能力。ShortSemver/ResolveReleaseName 走嵌入 Base 基类的默认实现,
// 适配器只需 override 真正不同的方法。
type Provider interface {
	// Name 返回发行版标识, 如 "temurin" / "corretto"。
	// 用作目录命名前缀、CLI 的 distro@ 前缀、provider.Get 的 key。
	Name() string

	// DisplayName 返回用户可见的发行版名, 如 "Temurin (Adoptium)" / "Amazon Corretto"。
	// 供 jvm install/available 等文案使用。适配器应给出友好名。
	DisplayName() string

	// Available 列出该发行版所有可安装的大版本 (供 jvm available 展示)。
	Available() ([]app.Release, error)

	// LatestPatch 返回指定大版本的最新 GA 版本 (供 jvm available 表格用)。
	// 比 Resolve 轻量: 不解析用户输入、不内化 CDN, 只取该 major 的最新一条。
	LatestPatch(major int) (*app.Asset, error)

	// Resolve 按 VersionSpec 查单个版本, 返回发行版无关的 Asset 契约。
	// 内部负责: 按 spec.Version 格式分流 (大版本最新 / 精确版本 / release_name),
	//           解析镜像 URL、校验和 (SHA256/SHA1)、规整 ReleaseName。
	Resolve(spec app.VersionSpec) (*app.Asset, error)

	// ListVersions 返回指定大版本的全部子版本 (供 jvm available -a 展示)。
	// provider 自己负责拉全量 (必要时循环翻页), 上层假定返回的就是完整列表。
	// 某些发行版 (如 Corretto/Microsoft) 每个 major 只维护最新 patch, 可只返回单条。
	ListVersions(major int) ([]*app.Asset, error)
}

// Base 提供 Provider 接口核心方法以外的可覆盖默认实现 (透传/常量)。
// 适配器嵌入 Base 后, 只需 override 真正不同的方法。
//
// 用结构体而非空接口是因为 Go 没有"接口默认实现", 嵌入结构体能让适配器
// 一行 type Corretto struct{ provider.Base } 就拿到所有默认值。
//
// Name/DisplayName/LatestPatch/Available/Resolve/ListVersions 不在 Base 提供
// 默认 (它们没有合理的通用值或需各自实现), 各 provider 必须自己实现。
type Base struct{}

// ShortSemver 规整 semver 为目录命名用的版本号 (不含 distro 前缀)。
// 默认原样返回 (多数发行版的 semver 已是干净形式); Temurin override
// 以剥离 ".LTS" 后缀。
func (Base) ShortSemver(semver string) string { return semver }

// ResolveReleaseName 把用户输入的完整版本号标准化成各发行版的 release 标识。
// 默认透传 (Corretto/Microsoft 版本号即标识); Temurin override 以补 "jdk-" 前缀
// (把 "21.0.12+8" 标准化成 Adoptium release_name "jdk-21.0.12+8")。
// 不接受半截版本号 (无 build 号), 由上层 Resolve 引导用大版本号取最新。
func (Base) ResolveReleaseName(version string) (string, error) { return version, nil }

// Configurable 是可选接口: provider 实现它来接收全局下载配置
// (目标架构 arch / 下载镜像 mirror, 均为 config 包加载后的最终值)。
//
// 实现方按需取用参数: 如 temurin 两者都用, corretto/microsoft 只用 arch
// (它们没有镜像源)。arch 的合法值见 app.NormArch; 实现方收到非法值时应
// 警告并回退自身默认, 不应 panic。
//
// 未实现该接口的 provider 视为不关心全局下载配置。
type Configurable interface {
	Configure(arch, mirror string)
}

// ConfigureAll 把全局下载配置分发给所有实现了 Configurable 的 provider。
// main 启动时调用一次; 新增 provider 只要实现 Configurable 即自动接入,
// 无需改动 main.go。
func ConfigureAll(arch, mirror string) {
	for _, p := range All() {
		if c, ok := p.(Configurable); ok {
			c.Configure(arch, mirror)
		}
	}
}

// Default 是无 distro@ 前缀时的默认发行版名。
const Default = app.DefaultDistro

// registry 持有所有已注册的 provider, 按 Name() 索引。
var (
	registryMu sync.RWMutex
	registry   = map[string]Provider{}
)

// Register 把一个 provider 加入注册表。供各 provider 包的 init() 调用。
// 同名重复注册视为编程错误, 直接 panic (通常是 main.go 空白导入重复)。
func Register(p Provider) {
	name := p.Name()
	if name == "" {
		panic("provider: Register 收到 Name() 为空的 provider")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic("provider: 重复注册 " + name)
	}
	registry[name] = p
}

// Get 按名字取 provider。不存在时返回错误并列出已注册的可用项。
func Get(name string) (Provider, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if p, ok := registry[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("未知的发行版 %q (可用: %s)", name, namesJoined())
}

// All 返回所有已注册的 provider, 按 Name() 字典序排序。
// 供 jvm available (无参数时展示默认) / help 文案使用。
func All() []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ps := make([]Provider, 0, len(registry))
	for _, p := range registry {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name() < ps[j].Name() })
	return ps
}

// namesJoined 返回所有已注册 provider 名的逗号分隔列表 (供错误消息)。
func namesJoined() string {
	ps := All()
	if len(ps) == 0 {
		return "(无)"
	}
	out := ""
	for i, p := range ps {
		if i > 0 {
			out += ", "
		}
		out += p.Name()
	}
	return out
}
