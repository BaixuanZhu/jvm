# jvm — Windows 上的 Java 版本管理器

> 🌐 **官网**：<https://baixuanzhu.github.io/jvm/>

一个类似 nvm-windows / jabba 的 JDK 版本管理工具，专为 Windows 设计。
支持 **Temurin / Corretto / Microsoft Build of OpenJDK** 等多个发行版，提供双击安装包，无需管理员权限。

## 特性

- 📦 **一键安装**：`jvm install 21` 自动下载最新 GA 版本
- 🌐 **多发行版**：`jvm install corretto@21` / `jvm install microsoft@21` 切换发行版（省略前缀默认 temurin）
- 🎯 **精确版本**：`jvm install 21.0.12+8` 安装指定小版本（完整版本号精确匹配）
- 🔄 **秒级切换**：`jvm use 21` 通过 Windows junction 切换，立即生效
- ⚡ **当前终端即时生效**：自动注入 shell 函数，`use` 后当前窗口的 `java` 立刻变，无需重开
- 🔧 **自动配 PATH**：jvm 首次运行自动把自己加入 PATH + 安装 shell 集成，全程零配置
- 🚫 **免管理员权限**：junction 不需要提权
- 🔐 **自动校验**：SHA256 校验，下载损坏会报错
- 🏠 **国内加速**：Temurin 优先走清华镜像，失败自动回退官方 CDN
- 🎈 **自动配 JAVA_HOME 和 PATH**：Maven / Gradle / IDE 都能识别
- 📌 **大版本取最新**：`jvm use 21` 自动切到该大版本的最新 patch
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

从 [Releases](https://github.com/BaixuanZhu/jvm/releases) 下载 `jvm-windows-amd64-setup.exe`，双击安装。安装器会：

1. 把 `jvm.exe` 放到 `%LOCALAPPDATA%\Programs\jvm`（用户级，无需管理员）
2. 运行一次 jvm，自动配置 PATH 和 shell 集成
3. 注册到「程序和功能」，可随时卸载

装完**重开一次终端**即可使用。

### 方式二：便携版

从 Releases 下载 `jvm-windows-amd64.zip`，解压出 `jvm.exe` 放到任意目录，运行一次（比如 `jvm version`）它会自动：

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
jvm install 21.0.12+8       # 安装 temurin 精确版本 (完整版本号, 含 build 号)
jvm use 21                  # 切换到 21 (大版本号取最新)
jvm use corretto@21         # 切换到 corretto 21
jvm uninstall 21            # 卸载 (默认需确认, 加 -y 跳过)

# 查询
jvm list                    # 已安装版本 (→ 标记当前)
jvm available               # 可安装的大版本 (标记 LTS)
jvm available corretto      # 查看 corretto 可安装版本
jvm available -a            # 列出每个大版本的全部子版本
jvm available --major 21    # 只看 JDK 21 的全部子版本
jvm current                 # 当前版本 (会实际执行 java -version)
jvm doctor                  # 诊断环境配置 (PATH/junction/JAVA_HOME/shell 集成)

# Shell 集成 (当前终端立即生效)
jvm init powershell         # 打印 PowerShell 集成脚本
jvm init powershell --install   # 自动写入 $PROFILE
jvm init bash --install     # 自动写入 ~/.bashrc

# 其他
jvm upgrade                 # 检查并更新 jvm 自身 (需配置仓库)
jvm version                 # 显示 jvm 版本号
jvm help                    # 帮助
```

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
  versions/
    21.0.12+8/          ← 解压后的 JDK（以纯 semver 命名）
    17.0.20+8/
  current/              ← junction，指向当前选中的版本
    bin/java.exe ...    ← 通过 junction 访问
```

- **PATH** 永远指向 `~/.jvm/current/bin`（只配置一次）
- **JAVA_HOME** 永远指向 `~/.jvm/current`
- 切换版本 = 重建 `current` 这个 junction 指向
- 因为 PATH 没变，**任何新终端自动用新版本，无需刷新环境变量**

## 配置文件（可选）

jvm 默认用清华镜像源下载、安装 `x64` 架构的 JDK。如需更改，在 `~/.jvm/config.toml` 写：

```toml
# 下载镜像源 (默认清华 TUNA)。海外用户可改成官方或其他镜像。
mirror = "https://mirrors.tuna.tsinghua.edu.cn/Adoptium"

# 目标架构 (默认 x64)。Windows on ARM 设备用 aarch64。
arch = "aarch64"
```

也可用环境变量临时覆盖（优先级高于配置文件），适合一次性切换：

```powershell
$env:JVM_MIRROR = "https://your.mirror/Adoptium"
$env:JVM_ARCH = "aarch64"
jvm install 21
```

不配置则使用默认值，无需创建文件。

## 自更新配置（jvm upgrade）

`jvm upgrade` 通过 GitHub Release 更新 jvm 自身。使用前需配置：

1. 在 GitHub 建仓库，打 tag（如 `v0.2.0`，必须以 `v` 开头）并推送。CI 会自动编译、打包、发布 Release，产出两个 asset：
   - `jvm-windows-amd64-setup.exe`（安装器）
   - `jvm-windows-amd64.zip`（便携版，`jvm upgrade` 拉这个，zip 里放单个 `jvm.exe`）

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
| 下载源 | Temurin 清华镜像优先 → 官方 CDN 回退；Corretto/Microsoft 直连官方 CDN | 国内快，且官方兜底 |
| 发行版 | Temurin / Corretto / Microsoft Build of OpenJDK | 纯 zip 解压，无需 msi installer |

## 构建

需要 [Go 1.26+](https://go.dev/dl/) 和 GNU Make（Git Bash / MinGW / WSL 自带）。

```powershell
cd D:\code\jvm
make build       # 编译 -> dist/jvm.exe
make installer   # 打安装包 -> dist/jvm-windows-amd64-setup.exe (需 NSIS, scoop install nsis)
make release     # 打便携 zip -> dist/jvm-windows-amd64.zip (供 jvm upgrade)
make dist-all    # 同时产出安装器 + 便携 zip
```

> 打 tag（`git tag v0.1.0 && git push --tags`）会触发 GitHub Actions 自动编译并发 Release。

## 项目结构

| 文件 | 职责 |
|------|------|
| `main.go` | 命令行入口和路由 |
| `cmd/cmd.go` | 各子命令的实现 |
| `internal/provider/` | Provider 抽象层（接口 + 注册表 + Base 基类） |
| `internal/provider/temurin/` | Temurin (Adoptium) 适配器：API + 清华镜像 |
| `internal/provider/corretto/` | Amazon Corretto 适配器：indexmap JSON |
| `internal/provider/microsoft/` | Microsoft Build of OpenJDK 适配器：aka.ms 探测 |
| `internal/jdk/` | 下载、SHA256 校验、解压（原子替换） |
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
