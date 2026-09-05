# jvm — Windows 上的 Java 版本管理器

> 🌐 **官网**：<https://baixuanzhu.github.io/jvm/>

一个类似 nvm-windows / jabba 的 JDK 版本管理工具，专为 Windows 设计。
支持 **Temurin / Corretto / Microsoft / Azul Zulu / BellSoft Liberica / Oracle GraalVM** 等多个发行版（另有 Temurin 早期访问变体 `temurin-ea`，跟踪尚未 GA 的预览版），提供双击安装包，无需管理员权限。

## 特性

- 📦 **一键安装**：`jvm install 21` 自动下载最新 GA 版本
- 🌐 **多发行版**：`jvm install corretto@21` / `jvm install zulu@21` / `jvm install liberica@21` / `jvm install graalvm@21` 切换发行版（省略前缀默认 temurin；`temurin-ea@28` 装 GA 前的预览版）
- 🎯 **精确版本**：`jvm install 21.0.12+8` 安装指定小版本（完整版本号精确匹配）
- 📁 **本地包安装**：`jvm install temurin@21.0.5+11 D:\jdk.zip` 从手动下载的 zip 纳管（内网/代理环境零网络）
- 💾 **下载缓存**：安装包自动留存，卸载后重装同版本免重新下载（`jvm cache` 查看/清理）
- 🔄 **秒级切换**：`jvm use 21` 通过 Windows junction 切换，立即生效
- ⚡ **当前终端即时生效**：自动注入 shell 函数，`use` 后当前窗口的 `java` 立刻变，无需重开
- 🔧 **自动配 PATH**：jvm 首次运行自动把自己加入 PATH + 安装 shell 集成，全程零配置
- 🚫 **免管理员权限**：junction 不需要提权
- 🔐 **自动校验**：SHA256 校验（Liberica 官方仅提供 SHA1，按发行版官方提供的算法校验），下载损坏会报错
- 🏠 **国内加速**：Temurin 优先走清华镜像，失败自动回退官方 CDN
- 🎈 **自动配 JAVA_HOME 和 PATH**：Maven / Gradle / IDE 都能识别
- 📌 **大版本取最新**：`jvm use 21` 自动切到该大版本的最新 patch
- ⬆️ **patch 一键升级**：`jvm update 21` 装最新 patch → 自动切换 → 清理旧版一步到位；`jvm update --all` 批量升级全部落后版本
- 🗂️ **数据目录可迁移**：`install_dir` 配置把 versions/ 挪到其他盘，C 盘不再吃紧
- 🔄 **自更新**：`jvm upgrade` 通过 GitHub Release 更新（需配置仓库）

## 安装

三种方式任选其一：

### 方式零：一键安装（推荐）

PowerShell 里执行一行命令即可（无需管理员权限）：

```powershell
iwr -useb "https://raw.githubusercontent.com/BaixuanZhu/jvm/main/install.ps1" | iex
```

脚本会自动下载最新 Release、校验 SHA256、解压到 `%LOCALAPPDATA%\Programs\jvm`，并运行一次 jvm 完成自举（注册用户 PATH + 注入 shell 集成）。装完**重开一次终端**即可使用。

想自定义安装目录，把脚本下到本地再传参：

```powershell
iwr -useb "https://raw.githubusercontent.com/BaixuanZhu/jvm/main/install.ps1" -OutFile install.ps1
.\install.ps1 -InstallDir "D:\tools\jvm"
```

> 国内访问 GitHub 慢？可设置 `$env:JVM_INSTALLER_MIRROR` 指向镜像下载前缀（末尾带斜杠），或改用下面的安装包 / 便携版方式。

### 方式一：安装包

从 [Releases](https://github.com/BaixuanZhu/jvm/releases) 按设备架构下载安装器（x64 设备用 `jvm-windows-amd64-setup.exe`，ARM64 设备用 `jvm-windows-arm64-setup.exe`），双击安装。安装器会：

1. 把 `jvm.exe` 放到 `%LOCALAPPDATA%\Programs\jvm`（用户级，无需管理员）
2. 运行一次 jvm，自动配置 PATH 和 shell 集成
3. 注册到「程序和功能」，可随时卸载

装完**重开一次终端**即可使用。

### 方式二：便携版

从 Releases 按设备架构下载便携 zip（`jvm-windows-amd64.zip` 或 `jvm-windows-arm64.zip`），解压出 `jvm.exe` 放到任意目录，运行一次（比如 `jvm version`）它会自动：

1. 把自己所在目录加入用户 PATH
2. 把 shell 集成函数静默写入 PowerShell `$PROFILE` 和 `~/.bashrc`

之后**重开一次终端**，`jvm` 命令随处可用，且 `jvm use` 后当前窗口的 `java` 立刻变化，无需任何手动配置。

> 自动集成是幂等的，重复运行不会堆积；如果你不想用自动集成，可手动删除 profile 里的 `jvm shell init` 标记块。

## 快速开始

```powershell
# 1. 装一个 JDK
jvm install 21

# 2. 切换到它
jvm use 21

# 完成! java -version 现在就是新版本
```

> 首次运行 jvm 后，**重开一次终端**让 PATH 和 shell 集成生效。之后就一劳永逸了。

## 命令一览

```powershell
# 版本管理
jvm install 21              # 安装 temurin JDK 21 最新版 (默认发行版)
jvm install corretto@21     # 安装 corretto JDK 21 最新版
jvm install microsoft@21    # 安装 microsoft JDK 21 最新版
jvm install zulu@21         # 安装 zulu JDK 21 最新版
jvm install liberica@21     # 安装 liberica JDK 21 最新版
jvm install graalvm@21      # 安装 Oracle GraalVM 21 (CPU LTS 线)
jvm install temurin-ea@28   # 安装 temurin 28 早期访问版 (GA 前预览)
jvm install 21.0.12+8       # 安装 temurin 精确版本 (完整版本号, 含 build 号)
jvm install temurin@21.0.5+11 D:\jdk.zip   # 从本地 zip 安装 (内网/手动下载, 零网络)
jvm use 21                  # 切换到 21 (大版本号取最新)
jvm use corretto@21         # 切换到 corretto 21
jvm use                     # 无参则读当前目录的 .jvmrc 切换版本
jvm pin corretto@21         # 固定此目录用 corretto 21 (写入 .jvmrc)
jvm pin                     # 把当前版本写入 .jvmrc (无参用 current)
jvm uninstall 21            # 卸载 (默认需确认, 加 -y 跳过)
jvm exec 17 -- mvn test     # 用 JDK 17 执行命令 (不动全局版本; 无版本号则读 .jvmrc)
jvm update 21 -y            # 升级 21 到最新 patch: 装新 → 切换 → 清理旧版
jvm update --all -y         # 批量升级全部落后版本组 (一次确认, 失败不阻断其余)

# 查询
jvm list                    # 已安装版本 (→ 标记当前)
jvm available               # 可安装的大版本 (标记 LTS; 结果本地缓存 10 分钟)
jvm available corretto      # 查看 corretto 可安装版本
jvm available -a            # 列出每个大版本的全部子版本
jvm available --major 21    # 只看 JDK 21 的全部子版本
jvm available -r            # 绕过缓存强制刷新直查
jvm current                 # 当前版本 (会实际执行 java -version)
jvm home                    # 打印当前 JAVA_HOME 路径 (供脚本/IDE 引用)
jvm outdated                # 检查已安装版本是否有新 patch 可升级
jvm cache                   # 查看下载缓存 (安装包留存, 重装免下载)
jvm cache clean             # 清空下载缓存
jvm doctor                  # 诊断环境配置 (14 项: PATH/junction/JAVA_HOME/集成/残留...)
jvm doctor --fix            # 诊断 + 自动修复可修项 (PATH 残留删除前逐条确认, -y 跳过)

# Shell 集成 (当前终端立即生效)
jvm init powershell         # 打印 PowerShell 集成脚本
jvm init powershell --install   # 自动写入 $PROFILE
jvm init bash --install     # 自动写入 ~/.bashrc

# 其他
jvm upgrade                 # 检查并更新 jvm 自身 (需配置仓库)
jvm version                 # 显示 jvm 版本号
jvm help                    # 帮助
```

## 项目级版本（.jvmrc）

在项目根目录放一个 `.jvmrc` 文件，写上版本号，团队成员（或未来的你）在该目录运行 `jvm use`（不带参数）即可切到指定版本，无需各自记版本号：

```powershell
# 固定当前项目用 corretto 21 (写入当前目录的 .jvmrc)
jvm pin corretto@21

# 或手动创建: 项目根目录建 .jvmrc, 内容为一行版本号
#   21                    # temurin JDK 21 最新
#   corretto@21.0.12.8.1  # 指定发行版 + 完整版本

# 之后在该项目任意子目录运行 (不带参数即可)
jvm use
```

文件格式：一行 `[distro@]version`（与命令行语法一致），支持 `#` 注释行。`jvm use` 无参时会从当前目录**逐级向上**查找最近的 `.jvmrc`，所以子目录也会命中项目根的配置（支持 monorepo）。

> `jvm pin` 只写文件，不切换版本；想立即生效再敲一次 `jvm use`。指定的版本需要先 `jvm install` 安装，`use` 读到未安装的版本会报错提示安装（不会自动下载）。

### cd 自动切换

装了新版集成脚本后（升级 jvm 即自动重写 profile 获得），**cd 进含 `.jvmrc` 的目录会自动切到该版本，cd 出去自动恢复**你之前手动选的版本：

```powershell
jvm use 21          # 手动选定 21
cd D:\proj\legacy   # 该目录有 .jvmrc (内容 8) → 自动切到 8
cd ~                # 离开项目 → 自动恢复 21
```

- 目录和 `.jvmrc` 没变时零开销（双层缓存，不会每次按回车都拉起 jvm）
- `.jvmrc` 指定的版本未安装时提示一行 `jvm install` 建议，不刷屏
- 手动 `jvm use` 永远优先：显式切换后即成为新的恢复基线
- 不喜欢此行为？在 `~/.jvm/config.toml` 设 `autoswitch = false`（或临时 `JVM_AUTOSWITCH=0`）关闭

## 关于「当前终端立即生效」

jvm 首次运行时会**自动**把 shell 集成函数写入 PowerShell `$PROFILE`（5.x 和 7+ 都写）和 `~/.bashrc`。重开一次终端后：

- `jvm use 21` 执行后，**当前窗口**的 `java -version` 立刻变成 21
- 不用重开窗口，不用手动 `init`

原理：集成函数在调用真正的 `jvm.exe` 后，若是 `use` 命令就在当前 shell 会话刷新 `JAVA_HOME` 和 `PATH`。

> - 自动集成是**幂等**的，jvm 每次启动静默检查，缺失才补。
> - **CMD (cmd.exe)** 暂不支持自动集成（doskey 体验差）。CMD 用户请新开窗口，或手动运行 `jvm init` 查看脚本。
> - 若你想手动管理集成，仍可用 `jvm init powershell` / `jvm init bash` 打印脚本。

## 工作原理

```
~/.jvm/
  versions/               ← 已安装的 JDK (每个一个子目录, {distro}-{版本} 命名)
    temurin-21.0.12+8/
    corretto-21.0.12.8.1/
  cache/                  ← 下载缓存 (安装包 zip 留存, 卸载重装免重新下载)
  current/                ← junction，指向当前选中的版本
    bin/java.exe ...      ← 通过 junction 访问
  config.toml             ← 用户配置 (可选)
```

- **PATH** 永远指向 `~/.jvm/current/bin`（只配置一次）
- **JAVA_HOME** 永远指向 `~/.jvm/current`
- 切换版本 = 重建 `current` 这个 junction 指向
- 因为 PATH 没变，**任何新终端自动用新版本，无需刷新环境变量**

## 配置文件（可选）

jvm 默认用清华镜像源下载 JDK，目标架构跟随 jvm 二进制本身（x64 版下 `x64` JDK，ARM64 版下 `aarch64` JDK）。如需更改，在 `~/.jvm/config.toml` 写：

```toml
# 下载镜像源 (默认清华 TUNA, 仅 Temurin 走镜像)。海外用户可改成官方或其他镜像。
mirror = "https://mirrors.tuna.tsinghua.edu.cn/Adoptium"

# 目标架构 (默认跟随 jvm 二进制架构; 也接受别名 amd64/arm64)。
arch = "aarch64"

# .jvmrc 目录自动切换 (默认开启; cd 进含 .jvmrc 的目录自动切版本, 离开恢复)。
autoswitch = true

# JDK 数据目录重定向 (默认空 = ~/.jvm)。把 versions/ 和 cache/ 挪到其他盘,
# 适合 C 盘空间紧张的场景。控制面 (config.toml/current/注册表) 留在 ~/.jvm
# 不动, 无需迁移即生效; 已装版本需手动搬到新目录, jvm doctor 会给出提示。
# install_dir = "D:\\jdks"
```

> `jvm available` 的查询结果会本地缓存 10 分钟（`~/.jvm/available-cache.json`），
> 二次查询免打各发行版 API；加 `-r` / `--refresh` 强制直查。

各发行版的 Windows ARM64 (aarch64) 支持情况：

| 发行版 | Windows ARM64 构建 | 说明 |
|---|---|---|
| Temurin | ⚠️ 部分版本有 | Adoptium 官方 Windows ARM64 覆盖不全（如 21 有、17/25 暂无，以 API 实际返回为准）；缺失版本查询时报错会带 `(windows/aarch64)` 上下文 |
| Temurin EA | ⚠️ 同 Temurin | 早期访问版走同一 Adoptium API，ARM64 覆盖随上游滚动 |
| Microsoft | ✅ 有 | 11 / 17 / 21 / 25 全部 LTS 均有 ARM64 构建 |
| Zulu | ✅ 有 | Azul 提供 Windows ARM64 构建（API `arch=arm64`） |
| Liberica | ✅ 有 | BellSoft 提供 Windows ARM64 构建（API `arch=arm&bitness=64`） |
| Corretto | ❌ 没有 | 官方未发布 Windows ARM64 构建；`arch=aarch64` 时安装/查询会明确报错并建议改用 Temurin / Microsoft / Zulu / Liberica |
| GraalVM | ❌ 没有 | Oracle 官方无 Windows ARM64 构建；`arch=aarch64` 时会明确报错并建议改用 Temurin / Microsoft |

也可用环境变量临时覆盖（优先级高于配置文件），适合一次性切换：

```powershell
$env:JVM_MIRROR = "https://your.mirror/Adoptium"
$env:JVM_ARCH = "aarch64"
$env:JVM_AUTOSWITCH = "0"    # 临时关闭 .jvmrc 自动切换
$env:JVM_INSTALL_DIR = "D:\jdks"   # 临时重定向数据目录
jvm install 21
```

不配置则使用默认值，无需创建文件。

## 自更新配置（jvm upgrade）

`jvm upgrade` 通过 GitHub Release 更新 jvm 自身。使用前需配置：

1. 在 GitHub 建仓库，打 tag（如 `v0.2.0`，必须以 `v` 开头）并推送。CI 会自动编译、打包、发布 Release，按架构产出四类 asset：
   - `jvm-windows-amd64-setup.exe` / `jvm-windows-arm64-setup.exe`（安装器）
   - `jvm-windows-amd64.zip` / `jvm-windows-arm64.zip`（便携版，`jvm upgrade` 按当前二进制架构精确匹配拉取，zip 里放单个 `jvm.exe`）

2. `internal/upgrade/upgrade.go` 里的常量已设为本仓库：
   ```go
   const githubRepo = "BaixuanZhu/jvm"
   ```

3. 重新编译，即可用 `jvm upgrade` 检查并更新。

未配置时运行 `jvm upgrade` 会给出提示，不会出错。

## 设计决策

| 问题 | 选择 | 原因 |
|------|------|------|
| 切换机制 | Windows junction (reparse point) | 免管理员权限、新终端也生效 |
| junction 创建 | 原生 `FSCTL_SET_REPARSE_POINT` (syscall) | 不调用 cmd.exe，无注入面 |
| 当前终端生效 | 启动时自动注入 shell wrapper 函数 | 子进程改不了父 shell 环境，靠函数在会话内刷新 PATH |
| PATH 注入 | 注册表 `HKCU\Environment` + 广播 WM_SETTINGCHANGE | 不用 setx（会截断长 PATH） |
| 下载源 | Temurin 清华镜像优先 → 官方 CDN 回退；Corretto/Microsoft/Zulu/Liberica/GraalVM 直连各自官方 CDN；Temurin EA 直连 GitHub release 资产（镜像不同步） | 国内快，且官方兜底 |
| 发行版 | Temurin / Corretto / Microsoft / Azul Zulu / BellSoft Liberica / Oracle GraalVM / Temurin EA | 纯 zip 解压，无需 msi installer |

## 构建

需要 [Go 1.26+](https://go.dev/dl/) 和 GNU Make（Git Bash / MinGW / WSL 自带）。

```powershell
cd D:\code\jvm
make build       # 编译 -> dist/<arch>/jvm.exe (默认本机架构; 开发构建, 启动不写 PATH/profile)
make build-dist  # 发行风味构建 (保持启动自举, 供打包/验证安装流程用)
make installer   # 打安装包 -> dist/jvm-windows-<arch>-setup.exe (需 NSIS, scoop install nsis)
make release     # 打便携 zip -> dist/jvm-windows-<arch>.zip (供 jvm upgrade)
make dist-all    # 同时产出安装器 + 便携 zip

# 交叉编 ARM64: 上面任意命令加 GOARCH=arm64, 如
make dist-all GOARCH=arm64
```

> 开发构建（`make build` / `make run`）的二进制启动时**不会**把自身写进用户 PATH / shell profile，放心在开发目录里直接运行；任何构建都可用 `JVM_NO_BOOTSTRAP=1` 强制关闭自举。

> 打 tag（`git tag v0.1.0 && git push --tags`）会触发 GitHub Actions 自动编译并发 Release。

## 项目结构

| 文件 | 职责 |
|------|------|
| `main.go` | 命令行入口和路由 |
| `cmd/cmd.go` | 各子命令的实现 |
| `internal/provider/` | Provider 抽象层（接口 + 注册表 + Base 基类 + Configurable 配置分发） |
| `internal/provider/temurin/` | Temurin (Adoptium) 适配器：API + 清华镜像（x64 / aarch64）；同包内含 `temurin-ea` 早期访问变体（EA 直连 GitHub 资产，无镜像） |
| `internal/provider/corretto/` | Amazon Corretto 适配器：indexmap JSON（仅 x64，官方无 ARM64 构建） |
| `internal/provider/microsoft/` | Microsoft Build of OpenJDK 适配器：aka.ms 探测（x64 / aarch64） |
| `internal/provider/zulu/` | Azul Zulu 适配器：Metadata API 两步查询 + SHA256（x64 / aarch64） |
| `internal/provider/liberica/` | BellSoft Liberica 适配器：Product Discovery API + SHA1（x64 / aarch64） |
| `internal/provider/graalvm/` | Oracle GraalVM 适配器：CDN 顺序探测 + `.sha256` 旁路（仅 x64，LTS 21/25） |
| `internal/jdk/` | 下载、完整性校验（SHA256/SHA1）、解压（原子替换） |
| `internal/junction/` | junction 创建/删除/解析 (原生 syscall) |
| `internal/env/` | 注册表读写、JAVA_HOME/PATH/jvm 自身 PATH 持久化 |
| `internal/shell/` | PowerShell/bash 集成脚本生成 + profile 写入 |
| `internal/upgrade/` | GitHub Release 检查 + 自更新 |
| `internal/paths/` | 目录路径配置 |
| `internal/app/` | 共享层：版本号、错误退出、HTTP client |
| `installer/jvm.nsi` | NSIS 安装包脚本 |
| `Makefile` | 构建/打包目标 |

## 许可

[MIT](LICENSE) © 2026 BaixuanZhu
