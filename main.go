// Command jvm 是 Windows 上的 Java 版本管理器 (类似 nvm-windows / jabba)。
// 支持 Temurin / Corretto / Microsoft Build of OpenJDK 等发行版。
//
// 用法:
//
//	jvm install 21
//	jvm use 21
//	jvm list
//
// 本文件只做命令路由; 各命令实现在 cmd 包, 基础设施在 internal/ 各包。
package main

import (
	"fmt"
	"os"

	"jvm/cmd"
	"jvm/internal/app"
	"jvm/internal/config"
	"jvm/internal/doctor"
	"jvm/internal/env"
	"jvm/internal/junction"
	_ "jvm/internal/provider/corretto"  // 注册 Amazon Corretto 适配器 (init 副作用)
	_ "jvm/internal/provider/microsoft" // 注册 Microsoft Build of OpenJDK 适配器 (init 副作用)
	"jvm/internal/provider/temurin"
	"jvm/internal/shell"
	"jvm/internal/upgrade"
)

func main() {
	// 加载用户配置 (~/.jvm/config.toml 或环境变量), 配置镜像源和架构。
	// 必须在任何网络请求前完成, 故置于自举链路最前面。
	cfg := config.Load()
	temurin.Configure(cfg.Arch, cfg.Mirror)

	// 静默自举: 把 jvm 自身目录加入 PATH + 安装 shell 集成 (首次运行, 幂等)
	// 这样用户无需任何手动配置, 重开终端后 jvm use 即在当前终端即时生效
	env.EnsureUserPath()
	shell.EnsureIntegration()
	// 把旧版 jvm 遗留的版本目录迁移到新的纯 semver 命名 (幂等, 首次升级时触发)
	if err := junction.MigrateLegacyDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  版本目录迁移失败: %v\n", err)
	}
	// 清理上次 upgrade 残留的 .bak (旧进程占用时删不掉, 下次启动再清)
	upgrade.CleanupStaleBak()

	if len(os.Args) < 2 {
		usage()
		os.Exit(0)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "install":
		if len(args) < 1 {
			app.Fail("用法: jvm install <[distro@]版本号>  例如: jvm install 21  或  jvm install corretto@21")
		}
		cmd.Install(args[0])
	case "use":
		if len(args) < 1 {
			app.Fail("用法: jvm use <[distro@]版本号>  例如: jvm use 21  或  jvm use corretto@21")
		}
		cmd.Use(args[0])
	case "list", "ls":
		cmd.List()
	case "available":
		opts, err := cmd.ParseAvailableArgs(os.Args[2:])
		if err != nil {
			app.Fail(err.Error())
		}
		cmd.Available(opts)
	case "uninstall", "rm":
		cmd.Uninstall(args)
	case "current":
		cmd.Current()
	case "doctor":
		doctor.Run()
	case "init":
		shell.InitDispatch(args)
	case "completion":
		shell.InitCompletionDispatch(args)
	case "upgrade":
		upgrade.Run()
	case "version", "-v", "--version":
		fmt.Println("jvm", app.Version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Printf("未知命令: %s\n\n", command)
		usage()
		os.Exit(1)
	}
}

// usage 打印帮助信息
func usage() {
	fmt.Print(`jvm - Windows 上的 Java 版本管理器

用法:
  jvm <命令> [参数]

发行版与版本号: [distro@]version
  省略 distro@ 默认 temurin。支持: temurin / corretto / microsoft
  版本号两种形式: 大版本号 (如 21) 取最新; 完整版本号精确匹配 (格式因发行版而异,
  运行 jvm available <distro> 查看实际格式)。
  示例:
    21                  temurin JDK 21 最新版
    corretto@21         corretto JDK 21 最新版
    microsoft@21        microsoft JDK 21 最新版
    corretto@21.0.12.8.1  corretto 指定版本 (完整版本号)

命令:
  install <[distro@]版本>  安装 JDK (例如: jvm install 21  或  jvm install corretto@21)
  use <[distro@]版本>      切换到指定版本 (大版本号取最新, 完整版本号精确匹配)
  list                     列出本地已安装的版本
  available [distro] [...] 列出可安装版本 (-a 全部子版本, --major 指定大版本)
  uninstall <[distro@]版本> 卸载指定版本 (默认需确认, 加 -y 跳过)
  current                  显示当前正在使用的版本
  doctor                   诊断环境配置 (目录/junction/PATH/JAVA_HOME/shell 集成)
  init <shell>             打印/安装 shell 集成脚本 (通常自动完成, 无需手动)
  completion <shell>       打印/安装 shell Tab 补全脚本 (通常自动完成, 无需手动)
  upgrade                  检查并更新 jvm 自身 (通过 GitHub Release)
  version                  显示 jvm 版本号
  help                     显示此帮助信息

示例:
  jvm install 21              # 安装 temurin JDK 21 最新版
  jvm use 21                  # 切换到 21
  jvm install corretto@21     # 安装 corretto JDK 21
  jvm install microsoft@21    # 安装 microsoft JDK 21
  jvm available corretto      # 查看 corretto 可安装版本

切换原理:
  使用 Windows junction (目录联接), PATH 永远指向 ~/.jvm/current/bin,
  切换版本只需重建 junction, 任何新开的终端立即生效, 无需管理员权限。

`)
}
