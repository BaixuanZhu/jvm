// Package paths 提供 jvm 的目录路径配置。
//
// jvm 的所有数据默认都在 ~/.jvm 下, 结构:
//
//	~/.jvm/
//	  versions/            已安装的 JDK (每个一个子目录, {distro}-{版本} 命名)
//	  cache/               下载缓存 (安装包 zip 留存, 重装免下载)
//	  current/             junction, 指向当前选中的版本
//	  config.toml          用户配置
//
// 目录分两层:
//   - 控制面 (Root): config.toml / current / auto-state。注册表 PATH/JAVA_HOME
//     指向 Root/current/bin, 永不迁移, 否则环境变量失效。
//   - 数据面 (dataRoot, 默认 = Root): versions/ / cache/ 等大体积目录。
//     config.toml 的 install_dir 键 (经 SetInstallDir) 可把它指到其他盘
//     (如 D:\jdks), 已装版本不自动搬迁。
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Root 是 jvm 的控制面根目录: ~/.jvm (或 JVM_HOME 指定的目录)。
var Root string

// dataRoot 是数据面根目录 (versions 等的父目录), 默认与 Root 相同,
// 经 SetInstallDir 重定向。
var dataRoot string

// VersionsDir 是所有已安装 JDK 的存放目录: {dataRoot}/versions
var VersionsDir string

// CacheDir 是下载缓存目录: {dataRoot}/cache。安装包 zip 以
// {distro}-{ReleaseName}.zip 命名留存于此, 卸载后重装同版本免重新下载;
// `jvm cache clean` 清空。
var CacheDir string

// CurrentLink 是 junction 路径, 始终指向当前选中的版本: ~/.jvm/current
var CurrentLink string

// AutoStateFile 记录 .jvmrc 自动切换前的手动版本目录名 (~/.jvm/auto-state):
// cd 进含 .jvmrc 的目录时自动切换前先记下当时的版本, 离开 (cd 到无 .jvmrc
// 的目录) 时据此恢复。显式 jvm use 会清掉它 (手动选择即新基线)。
var AutoStateFile string

func init() {
	Root = calcRoot()
	dataRoot = Root
	CurrentLink = filepath.Join(Root, "current")
	AutoStateFile = filepath.Join(Root, "auto-state")
	recomputeDataDirs()
}

// recomputeDataDirs 基于 dataRoot 重算数据面路径。
func recomputeDataDirs() {
	VersionsDir = filepath.Join(dataRoot, "versions")
	CacheDir = filepath.Join(dataRoot, "cache")
}

// SetInstallDir 把数据面目录 (versions/) 重定向到 dir, 控制面 (Root 下的
// config.toml / current / auto-state) 不动 —— 注册表 PATH/JAVA_HOME 指向的
// ~/.jvm/current/bin 依旧有效, 无需任何迁移。
//
// dir 为相对路径时以进程 cwd 解析为绝对路径; 空串为 no-op。需在命令执行前
// 调用 (main 在 config.Load 后立即调)。已装版本不会自动搬迁: 旧默认目录里的
// 内容切换后即从 jvm list 消失, 需手动搬到新目录。
func SetInstallDir(dir string) error {
	d := strings.TrimSpace(dir)
	if d == "" {
		return nil
	}
	abs, err := filepath.Abs(d)
	if err != nil {
		return fmt.Errorf("install_dir 无法解析为绝对路径: %w", err)
	}
	dataRoot = abs
	recomputeDataDirs()
	return nil
}

// calcRoot 返回 jvm 根目录: 优先 JVM_HOME 环境变量 (便于 CI / 集成测试把整个
// ~/.jvm 重定向到任意目录), 否则回退 ~/.jvm。
//
// JVM_HOME 直接当根目录用 (而非 $JVM_HOME/.jvm), 这样 CI 设 JVM_HOME=$tmp/jvm-ci
// 即把 versions/current 全部隔离到临时目录。paths.init() 先于所有消费包执行,
// 故 JVM_HOME 只要进程启动前进入环境变量即对全进程生效。
func calcRoot() string {
	if h := os.Getenv("JVM_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// 极端情况 (环境变量缺失), 无法继续, 直接 panic
		panic("无法获取用户目录: " + err.Error())
	}
	return filepath.Join(home, ".jvm")
}

// EnsureDirs 确保根目录、versions 和 cache 目录存在, 不存在则创建。
func EnsureDirs() error {
	for _, dir := range []string{VersionsDir, CacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", dir, err)
		}
	}
	return nil
}
