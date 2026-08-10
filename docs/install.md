---
title: 安装指南
description: jvm 的三种安装方式：一键脚本、安装包、便携版，均无需管理员权限。
---

三种方式任选其一，全部**无需管理员权限**。

## 方式零：一键安装（推荐）

PowerShell 里执行一行命令：

```powershell
iwr -useb "https://raw.githubusercontent.com/BaixuanZhu/jvm/main/install.ps1" | iex
```

脚本会**自动检测系统架构（x64 / ARM64）**、下载匹配的最新 Release、校验 SHA256、解压到 `%LOCALAPPDATA%\Programs\jvm`，并运行一次 jvm 完成自举（注册用户 PATH + 注入 shell 集成）。装完**重开一次终端**即可使用。

想自定义安装目录，把脚本下到本地再传参：

```powershell
iwr -useb "https://raw.githubusercontent.com/BaixuanZhu/jvm/main/install.ps1" -OutFile install.ps1
.\install.ps1 -InstallDir "D:\tools\jvm"
```

> 国内访问 GitHub 慢？可设置 `$env:JVM_INSTALLER_MIRROR` 指向镜像下载前缀（末尾带斜杠），或改用下面的安装包 / 便携版方式。

## 方式一：安装包

从 [Releases]({{ site.github_releases }}) 按设备架构下载安装器，双击安装：

- x64 设备：`jvm-windows-amd64-setup.exe`（[直链]({{ site.download_setup }})）
- ARM64 设备：`jvm-windows-arm64-setup.exe`（[直链]({{ site.download_setup_arm64 }})）

安装器会：

1. 把 `jvm.exe` 放到 `%LOCALAPPDATA%\Programs\jvm`（用户级，无需管理员）
2. 运行一次 jvm，自动配置 PATH 和 shell 集成
3. 注册到「程序和功能」，可随时卸载

装完**重开一次终端**即可使用。

> ARM64 安装器的安装向导本身是 x86 界面（靠系统内置模拟运行），释放出的 `jvm.exe` 是 ARM64 原生二进制。

## 方式二：便携版

从 Releases 按设备架构下载便携 zip（`jvm-windows-amd64.zip` 或 `jvm-windows-arm64.zip`），解压出 `jvm.exe` 放到任意目录，运行一次（比如 `jvm version`）它会自动：

1. 把自己所在目录加入用户 PATH
2. 把 shell 集成函数静默写入 PowerShell `$PROFILE` 和 `~/.bashrc`

之后**重开一次终端**，`jvm` 命令随处可用。

> 自动集成是幂等的，重复运行不会堆积；不想用可手动删除 profile 里的 `jvm shell init` 标记块。

## 验证安装

```powershell
jvm version
jvm doctor   # 9 项环境诊断，全部 ✓ 即就绪
```

## 卸载

- 安装包：在「程序和功能」里卸载即可
- 便携版 / 脚本安装：删除安装目录与 `~/.jvm`，并从用户 PATH 中移除对应条目
