// Package config 负责加载 jvm 的用户配置 (~/.jvm/config.toml)。
//
// 配置项:
//   - mirror:     下载镜像源 (默认清华 TUNA)
//   - arch:       目标架构 (默认跟随当前二进制: amd64 版 → x64, arm64 版 → aarch64)
//   - autoswitch: .jvmrc 目录自动切换 (默认开启; cd 进含 .jvmrc 的目录自动
//     切到该版本, 离开时恢复)
//   - install_dir: 数据目录 (versions/) 的安装位置 (默认空 = ~/.jvm, 可指到
//     其他盘; 控制面 config.toml/current 不随迁移)
//
// 优先级: 环境变量 (JVM_MIRROR / JVM_ARCH / JVM_AUTOSWITCH / JVM_INSTALL_DIR)
// > 配置文件 > 默认值。
// 配置文件缺失视为正常 (返回默认值); 解析失败打印警告并回退默认。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"jvm/internal/paths"
)

// Config 是 jvm 的用户配置。
type Config struct {
	Mirror     string `toml:"mirror"`      // 下载镜像源 URL
	Arch       string `toml:"arch"`        // 目标架构: x64 / aarch64
	AutoSwitch bool   `toml:"autoswitch"`  // .jvmrc 目录自动切换开关
	InstallDir string `toml:"install_dir"` // 数据目录重定向 (空 = 默认 ~/.jvm)
}

// fileConfig 是配置文件的解析目标。bool 用指针以区分"未设置" (nil,
// 保持默认) 与显式 false; mirror/arch/install_dir 沿用非空判断。
type fileConfig struct {
	Mirror     string `toml:"mirror"`
	Arch       string `toml:"arch"`
	AutoSwitch *bool  `toml:"autoswitch"`
	InstallDir string `toml:"install_dir"`
}

// Default 返回默认配置。
func Default() Config {
	return Config{
		Mirror:     "https://mirrors.tuna.tsinghua.edu.cn/Adoptium",
		Arch:       defaultArch(),
		AutoSwitch: true,
	}
}

// defaultArch 返回当前二进制对应的默认 JDK 目标架构:
// ARM64 版 jvm 默认下载 ARM64 (aarch64) JDK, 其余 (amd64) 默认 x64。
// 注意: 六个 provider (temurin/corretto/microsoft/zulu/liberica/graalvm) 均消费该 arch 值。
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

// ValidateFile 检查配置文件是否可解析 (供 doctor 诊断)。
// 文件不存在视为正常 (返回 nil, 未使用自定义配置); 存在但 TOML 语法非法
// 返回原始解析错误。
func ValidateFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var fileCfg fileConfig
	return toml.Unmarshal(data, &fileCfg)
}

// Load 读取并合并配置: 默认值 ← 配置文件 ← 环境变量。
// 配置文件缺失视为正常; 解析失败打印警告并回退默认。
func Load() Config {
	cfg := Default()

	// 1. 读配置文件 (缺失则跳过, 用默认值)
	path := configPath()
	if data, err := os.ReadFile(path); err == nil {
		var fileCfg fileConfig
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
			if fileCfg.AutoSwitch != nil {
				cfg.AutoSwitch = *fileCfg.AutoSwitch
			}
			if strings.TrimSpace(fileCfg.InstallDir) != "" {
				cfg.InstallDir = fileCfg.InstallDir
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
	if v := strings.TrimSpace(os.Getenv("JVM_AUTOSWITCH")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.AutoSwitch = b
		} else {
			fmt.Fprintf(os.Stderr, "⚠️  JVM_AUTOSWITCH=%q 不是布尔值 (1/0/true/false), 已忽略\n", v)
		}
	}
	if v := strings.TrimSpace(os.Getenv("JVM_INSTALL_DIR")); v != "" {
		cfg.InstallDir = v
	}

	return cfg
}
