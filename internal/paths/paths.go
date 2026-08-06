// Package paths 提供 jvm 的目录路径配置。
//
// jvm 的所有数据都在 ~/.jvm 下, 结构:
//
//	~/.jvm/
//	  versions/            已安装的 JDK (每个一个子目录)
//	    jdk-21.0.12+8/
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
	home, err := os.UserHomeDir()
	if err != nil {
		// 极端情况 (环境变量缺失), 无法继续, 直接 panic
		panic("无法获取用户目录: " + err.Error())
	}
	Root = filepath.Join(home, ".jvm")
	VersionsDir = filepath.Join(Root, "versions")
	CurrentLink = filepath.Join(Root, "current")
}

// EnsureDirs 确保根目录和 versions 目录存在, 不存在则创建。
func EnsureDirs() error {
	if err := os.MkdirAll(VersionsDir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s: %w", VersionsDir, err)
	}
	return nil
}
