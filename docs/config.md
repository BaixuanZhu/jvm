---
title: 配置与原理
description: jvm 的配置文件、目录结构与切换原理。
---

## 工作原理

```
~/.jvm/
  versions/
    temurin-21.0.12+8/  ← 解压后的 JDK（{distro}-{版本} 命名）
    corretto-21.0.12.8.1/
  current/              ← junction，指向当前选中的版本
    bin/java.exe ...
  auto-state            ← .jvmrc 自动切换前的手动版本记录（可选存在）
```

- **PATH** 永远指向 `~/.jvm/current/bin`（只配置一次）
- **JAVA_HOME** 永远指向 `~/.jvm/current`
- 切换版本 = 重建 `current` 这个 junction 的指向
- 因为 PATH 没变，**任何新终端自动用新版本，无需刷新环境变量**

当前终端即时生效则靠 shell 集成函数：`jvm use` 调用真正的 `jvm.exe` 后，由 wrapper 函数在会话内刷新 `JAVA_HOME` 和 `PATH`（子进程改不了父 shell 环境，靠函数绕过）。

## .jvmrc 自动切换

shell 集成 v2 在每次出提示符前检查当前目录链上的 `.jvmrc`（双层缓存，目录与 rc 未变时零开销）：

- cd 进含 `.jvmrc` 的目录 → 自动执行 `jvm use --auto` 切到该版本，切换前的手动版本记入 `~/.jvm/auto-state`
- 在多个 `.jvmrc` 项目间移动 → 逐个切换，基线不变
- cd 到无 `.jvmrc` 的目录 → 自动恢复切换前的手动版本
- 显式 `jvm use <版本>` = 新的手动基线（清掉待恢复状态），不会被自动恢复覆盖
- `.jvmrc` 指向未安装版本 → 一行警告提示 `jvm install`，不刷屏

仅 PowerShell 与 Git Bash 支持自动切换（CMD 用户用无参数 `jvm use` 显式读取）。

## 配置文件（可选）

jvm 默认用清华镜像源下载 JDK，**目标架构跟随 jvm 二进制本身**（x64 版下 `x64` JDK，ARM64 版下 `aarch64` JDK），`.jvmrc` 自动切换默认开启。如需更改，在 `~/.jvm/config.toml` 写：

```toml
# 下载镜像源（默认清华 TUNA，仅 Temurin 走镜像）。海外用户可改成官方或其他镜像。
mirror = "https://mirrors.tuna.tsinghua.edu.cn/Adoptium"

# 目标架构（默认跟随 jvm 二进制架构；也接受别名 amd64 / arm64）。
arch = "aarch64"

# .jvmrc 目录自动切换（默认 true，关掉后 cd 不再自动切换）。
autoswitch = false
```

各发行版的 Windows ARM64 构建支持：

| 发行版 | Windows ARM64 | 说明 |
|--------|---------------|------|
| Temurin | ⚠️ 部分版本 | Adoptium 官方覆盖不全（如 21 有、17/25 暂无），以实际查询为准 |
| Microsoft | ✅ 全部 LTS | 11 / 17 / 21 / 25 均有 ARM64 构建 |
| Zulu | ✅ LTS | 11 / 17 / 21 / 25 均有 ARM64 构建 |
| Liberica | ✅ 全部 LTS | 全架构覆盖 |
| Corretto | ❌ 没有 | 官方未发布；`arch=aarch64` 时会明确报错并建议改用 Temurin / Microsoft / Zulu / Liberica |

也可用环境变量临时覆盖（优先级高于配置文件）：

```powershell
$env:JVM_MIRROR = "https://your.mirror/Adoptium"
$env:JVM_ARCH = "aarch64"
$env:JVM_AUTOSWITCH = "0"     # 临时关闭 .jvmrc 自动切换
jvm install 21
```

优先级：**环境变量 > 配置文件 > 默认值**。不配置则全部用默认值，无需创建文件。

## 设计决策

| 问题 | 选择 | 原因 |
|------|------|------|
| 切换机制 | Windows junction (reparse point) | 免管理员权限、新终端也生效 |
| junction 创建 | 原生 `FSCTL_SET_REPARSE_POINT` (syscall) | 不调用 cmd.exe，无注入面 |
| 当前终端生效 | 启动时自动注入 shell wrapper 函数 | 子进程改不了父 shell 环境 |
| .jvmrc 自动切换 | 包装 `prompt` (PS) / 挂 `PROMPT_COMMAND` (bash) + 双层缓存 | 出提示符前自然触发，目录与 rc 未变时零开销 |
| PATH 持久化 | 注册表 `HKCU\Environment` + 广播 `WM_SETTINGCHANGE` | 不用 setx（会截断长 PATH） |
| 下载源 | Temurin 清华镜像优先 → 官方 CDN 回退 | 国内快，且官方兜底 |
| 安全校验 | 下载强制按官方哈希校验（SHA256 / SHA1） | 损坏/篡改即报错中止 |
