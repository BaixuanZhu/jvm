// Package config 负责加载 jvm 的用户配置 (~/.jvm/config.toml)。
//
// 配置项 (首期覆盖):
//   - mirror: 下载镜像源 (默认清华 TUNA)
//   - arch:   目标架构 (默认跟随当前二进制: amd64 版 → x64, arm64 版 → aarch64)
//
// 优先级: 环境变量 (JVM_MIRROR / JVM_ARCH) > 配置文件 > 默认值。
// 配置文件缺失视为正常 (返回默认值); 解析失败打印警告并回退默认。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"jvm/internal/paths"
)

// Config 是 jvm 的用户配置。
type Config struct {
	Mirror string `toml:"mirror"` // 下载镜像源 URL
	Arch   string `toml:"arch"`   // 目标架构: x64 / aarch64
}

// Default 返回默认配置。
func Default() Config {
	return Config{
		Mirror: "https://mirrors.tuna.tsinghua.edu.cn/Adoptium",
		Arch:   defaultArch(),
	}
}

// defaultArch 返回当前二进制对应的默认 JDK 目标架构:
// ARM64 版 jvm 默认下载 ARM64 (aarch64) JDK, 其余 (amd64) 默认 x64。
// 注意: 目前仅 temurin provider 消费该值, corretto / microsoft 仍固定 x64。
func defaultArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x64"
}

// configPath 返回配置文件路径: ~/.jvm/config.toml
func configPath() string {
	return filepath.Join(paths.Root, "config.toml")
}

// Load 读取并合并配置: 默认值 ← 配置文件 ← 环境变量。
// 配置文件缺失视为正常; 解析失败打印警告并回退默认。
func Load() Config {
	cfg := Default()

	// 1. 读配置文件 (缺失则跳过, 用默认值)
	path := configPath()
	if data, err := os.ReadFile(path); err == nil {
		var fileCfg Config
		if err := toml.Unmarshal(data, &fileCfg); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  解析 %s 失败, 使用默认配置: %v\n", path, err)
		} else {
			// 只在文件里显式设了非空值时才覆盖
			if strings.TrimSpace(fileCfg.Mirror) != "" {
				cfg.Mirror = fileCfg.Mirror
			}
			if strings.TrimSpace(fileCfg.Arch) != "" {
				cfg.Arch = fileCfg.Arch
			}
		}
	}

	// 2. 环境变量覆盖 (优先级最高)
	if v := strings.TrimSpace(os.Getenv("JVM_MIRROR")); v != "" {
		cfg.Mirror = v
	}
	if v := strings.TrimSpace(os.Getenv("JVM_ARCH")); v != "" {
		cfg.Arch = v
	}

	return cfg
}
