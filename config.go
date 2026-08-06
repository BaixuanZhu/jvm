package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// jvmRoot 返回 jvm 的根目录: ~/.jvm
// 所有版本都装在这里, 结构如下:
//
//	~/.jvm/
//	  versions/
//	    jdk-21.0.5+11/      <- 解压后的 JDK
//	    jdk-17.0.13+11/
//	  current/              <- junction, 指向当前选中的版本
//	    bin/java.exe ...    <- 通过 junction 访问
var jvmRoot string

// versionsDir 是所有已安装 JDK 的存放目录
var versionsDir string

// currentLink 是 junction 路径, 始终指向当前选中的版本
var currentLink string

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		panic("无法获取用户目录: " + err.Error())
	}
	jvmRoot = filepath.Join(home, ".jvm")
	versionsDir = filepath.Join(jvmRoot, "versions")
	currentLink = filepath.Join(jvmRoot, "current")
}

// ensureDirs 确保根目录和 versions 目录存在
func ensureDirs() error {
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s: %w", versionsDir, err)
	}
	return nil
}
