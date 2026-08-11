// Package paths 提供 jvm 的目录路径配置。
//
// jvm 的所有数据都在 ~/.jvm 下, 结构:
//
//	~/.jvm/
//	  versions/            已安装的 JDK (每个一个子目录, 以纯 semver 命名)
//	    21.0.12+8/
//	  current/             junction, 指向当前选中的版本
//
// 这些路径在 init() 里基于用户主目录计算, 全局只读。
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// Root 是 jvm 的根目录: ~/.jvm
var Root string

// VersionsDir 是所有已安装 JDK 的存放目录: ~/.jvm/versions
var VersionsDir string

// CurrentLink 是 junction 路径, 始终指向当前选中的版本: ~/.jvm/current
var CurrentLink string

func init() {
	Root = calcRoot()
	VersionsDir = filepath.Join(Root, "versions")
	CurrentLink = filepath.Join(Root, "current")
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

// EnsureDirs 确保根目录和 versions 目录存在, 不存在则创建。
func EnsureDirs() error {
	if err := os.MkdirAll(VersionsDir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s: %w", VersionsDir, err)
	}
	return nil
}
