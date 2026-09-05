// Command jvm 是 Windows 上的 Java 版本管理器 (类似 nvm-windows / jabba)。
// 支持 Temurin / Corretto / Microsoft / Zulu / Liberica 等发行版。
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
	"jvm/internal/paths"
	"jvm/internal/provider"
	_ "jvm/internal/provider/corretto"  // 注册 Amazon Corretto 适配器 (init 副作用)
	_ "jvm/internal/provider/liberica"  // 注册 BellSoft Liberica 适配器 (init 副作用)
	_ "jvm/internal/provider/microsoft" // 注册 Microsoft Build of OpenJDK 适配器 (init 副作用)
	_ "jvm/internal/provider/temurin"   // 注册 Temurin (Adoptium) 适配器 (init 副作用)
	_ "jvm/internal/provider/zulu"      // 注册 Azul Zulu 适配器 (init 副作用)
	"jvm/internal/shell"
	"jvm/internal/updatecheck"
	"jvm/internal/upgrade"
)

func main() {
	// 加载用户配置 (~/.jvm/config.toml 或环境变量), 把目标架构/镜像分发给
	// 所有实现了 provider.Configurable 的适配器 (temurin/corretto/microsoft/zulu/liberica)。
	// 必须在任何网络请求前完成, 故置于自举链路最前面。
	cfg := config.Load()
	provider.ConfigureAll(cfg.Arch, cfg.Mirror)

	// install_dir 配置把数据面 (versions/) 重定向到其他盘。必须先于所有读
	// paths.VersionsDir 的启动逻辑 (MigrateLegacyDirs 等) 与命令执行。
	if cfg.InstallDir != "" {
		if err := paths.SetInstallDir(cfg.InstallDir); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  install_dir 配置无效, 使用默认目录: %v\n", err)
		}
	}

	// 静默自举: 把 jvm 自身目录加入 PATH + 安装 shell 集成 (首次运行, 幂等)
	// 这样用户无需任何手动配置, 重开终端后 jvm use 即在当前终端即时生效。
	// 跳过场景 (见 app.BootstrapEnabled): 开发构建 (make build/run 注入 off)、
	// JVM_NO_BOOTSTRAP 非空、自身位于系统 Temp 目录 (go run 临时二进制)。
	if app.BootstrapEnabled() {
		env.EnsureUserPath()
		shell.EnsureIntegration()
	}
	// 把旧版 jvm 遗留的版本目录迁移到新的纯 semver 命名 (幂等, 首次升级时触发)
	if err := junction.MigrateLegacyDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  版本目录迁移失败: %v\n", err)
	}
	// 清理上次 upgrade 残留的 .bak (旧进程占用时删不掉, 下次启动再清)
	upgrade.CleanupStaleBak()
	// 静默检查 jvm 新版本 (24h 节流, 有新版才提示, 失败永不阻断)
	updatecheck.Run()

	if len(os.Args) < 2 {
		usage()
		os.Exit(0)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "install":
		if len(args) < 1 {
			app.Fail("用法: jvm install <[distro@]版本号> [zip文件]\n" +
				"  远程安装: jvm install 21  或  jvm install corretto@21\n" +
				"  本地包:   jvm install temurin@21.0.5+11 D:\\downloads\\jdk.zip")
		}
		cmd.Install(args)
	case "use":
		// --auto 由 shell 集成钩子调用 (cd 进/离开 .jvmrc 目录时), 用户不直接敲
		if len(args) >= 1 && args[0] == "--auto" {
			cmd.UseAuto(cfg.AutoSwitch)
			return
		}
		arg := ""
		if len(args) >= 1 {
			arg = args[0]
		}
		cmd.Use(arg)
	case "pin":
		arg := ""
		if len(args) >= 1 {
			arg = args[0]
		}
		cmd.Pin(arg)
	case "list", "ls":
		cmd.List()
	case "cache":
		cmd.Cache(args)
	case "available":
		opts, err := cmd.ParseAvailableArgs(os.Args[2:])
		if err != nil {
			app.Fail(err.Error())
		}
		cmd.Available(opts)
	case "outdated":
		cmd.Outdated()
	case "update":
		cmd.Update(args)
	case "uninstall", "rm":
		cmd.Uninstall(args)
	case "current":
		cmd.Current()
	case "home":
		cmd.Home()
	case "exec":
		cmd.Exec(args)
	case "doctor":
		fix, assumeYes := false, false
		for _, a := range args {
			switch a {
			case "--fix", "-f":
				fix = true
			case "-y", "--yes":
				assumeYes = true
			}
		}
		doctor.Run(fix, assumeYes, cfg.InstallDir)
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
  省略 distro@ 默认 temurin。支持: temurin / corretto / microsoft / zulu / liberica
  版本号两种形式: 大版本号 (如 21) 取最新; 完整版本号精确匹配 (格式因发行版而异,
  运行 jvm available <distro> 查看实际格式)。
  示例:
    21                  temurin JDK 21 最新版
    corretto@21         corretto JDK 21 最新版
    microsoft@21        microsoft JDK 21 最新版
    corretto@21.0.12.8.1  corretto 指定版本 (完整版本号)

命令:
  install <[distro@]版本>  安装 JDK (例如: jvm install 21  或  jvm install corretto@21)
                           本地 zip 包: jvm install temurin@21.0.5+11 D:\downloads\jdk.zip
                           (内网/代理下手动下载的场景, 不走网络与远程校验)
  use <[distro@]版本>      切换到指定版本 (无参则读 .jvmrc; 大版本号取最新, 完整版本号精确)
                           cd 进含 .jvmrc 的目录时自动切换 (config.toml 设 autoswitch=false 关闭)
  pin [版本]               固定当前目录的 JDK 版本到 .jvmrc (无参用当前版本)
  list                     列出本地已安装的版本
  cache [clean]            查看/清空下载缓存 (安装包留存, 卸载重装免重新下载)
  available [distro] [...] 列出可安装版本 (-a 全部子版本, --major 指定大版本)
  outdated                 检查已安装版本是否有新 patch 可升级
  update <[distro@]大版本> [-y]  升级该大版本到最新 patch: 装新 → 切换 → 清理旧版
                           (仅接受大版本号; 当前正在使用该组版本时自动切换, -y 跳过确认)
  uninstall <[distro@]版本> 卸载指定版本 (默认需确认, 加 -y 跳过)
  current                  显示当前正在使用的版本
  home                     打印当前 JAVA_HOME 路径 (~/.jvm/current, 供脚本/IDE 引用)
  exec <版本> -- <命令>    用指定版本执行命令, 不动全局 (例如: jvm exec 17 -- mvn test)
  doctor [--fix]           诊断环境配置 (--fix 自动修复可修项, 残留清理前逐条确认)
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
  jvm pin corretto@21         # 固定此目录用 corretto 21 (写入 .jvmrc)
  jvm use                     # 无参时读 .jvmrc 切换版本

切换原理:
  使用 Windows junction (目录联接), PATH 永远指向 ~/.jvm/current/bin,
  切换版本只需重建 junction, 任何新开的终端立即生效, 无需管理员权限。

`)
}
