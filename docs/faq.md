---
title: 常见问题
description: jvm 使用中的常见问题与解答。
---

## 需要管理员权限吗？

**不需要。** junction 通过原生 syscall 创建（普通用户权限足够），注册表只写 `HKCU`（用户级），安装目录在 `%LOCALAPPDATA%` 下。

## 支持 CMD（cmd.exe）吗？

CMD **不支持** shell 自动集成（doskey 体验差）。jvm 本身在 CMD 里可用，但 `jvm use` 后当前 CMD 窗口的 `java` 不会立刻变——新开窗口即可。推荐用 PowerShell 或 Git Bash。

## 已装的 JDK 怎么升级？

```powershell
jvm outdated          # 先看哪些版本有新 patch
jvm update 21         # 升级 temurin 21：装最新 patch → 自动切换 → 清理旧目录
jvm update --all      # 或者一次升级全部落后的 (发行版, 大版本) 组
```

正在使用该组版本时升级会自动切换到新版；被进程占用删不掉的旧目录会跳过并提示稍后手动 `uninstall`。

## `jvm use` 后 java 没变？

1. 确认你重开过至少一次终端（让 PATH 和 shell 集成生效）
2. 跑 `jvm doctor` 做 14 项诊断，或直接 `jvm doctor --fix` 自动修复
3. 确认使用的是 PowerShell 或 bash（CMD 见上一条）

## 国内下载慢 / 失败？

- Temurin 默认走清华镜像，失败会自动回退官方 CDN
- 下载支持断点续传（3 次指数退避重试 + HTTP Range），中断后重跑 `jvm install` 会接着下；连接停滞（对端不再传数据）30 秒自动中断重试，不会无限挂起
- 内网 / 代理环境可用本地包安装：`jvm install temurin@21.0.5+11 D:\jdk.zip`（手动下载后纳管，零网络）
- 安装脚本本身拉不动 GitHub 时，设置 `$env:JVM_INSTALLER_MIRROR` 指向镜像前缀，或改用安装包 / 便携版

## 和 nvm-windows / jabba 有什么区别？

定位类似，jvm 的差异点：多发行版支持（`distro@` 语法）、当前终端即时生效、免管理员、国内镜像加速、项目级 `.jvmrc` 固定与自动切换、`jvm exec` 不动全局的一次性执行、`jvm outdated` / `jvm update` 升级检查与 patch 一键升级、`jvm doctor` 环境诊断与一键修复、下载缓存与本地包安装、Tab 智能补全。

## 不想让 cd 自动切版本，怎么关？

`~/.jvm/config.toml` 里写 `autoswitch = false`，或临时 `$env:JVM_AUTOSWITCH = "0"`。关掉后 `.jvmrc` 仍可用无参数 `jvm use` 显式读取。

## 会污染系统环境吗？

不会。只写 `HKCU\Environment`（用户级 PATH / JAVA_HOME），不碰 `HKLM`；JDK 全部放在 `~/.jvm/versions/` 下；shell 集成是 profile 里一段有标记的脚本块，可整段删除。

## 支持 Windows on ARM 吗？

**支持，且有 ARM64 原生版本。** ARM64 设备请下载 `jvm-windows-arm64-setup.exe`（或便携 zip；一键安装脚本会自动识别架构）。ARM64 版 jvm 默认下载 ARM64 原生 JDK：Microsoft / Zulu / Liberica 全部 LTS 都有 ARM64 构建，Temurin 部分版本有（如 21），Corretto 官方未发布 Windows ARM64 构建（选择时会明确报错并给出替代建议）。

也可以在 `~/.jvm/config.toml` 里设 `arch = "aarch64"`，或用 `$env:JVM_ARCH` 临时指定。

## 如何更新 jvm 自身？

```powershell
jvm upgrade
```

从 GitHub Release 拉取最新便携包，替换自身前做 SHA256 校验。jvm 启动时也会静默检查新版本（24h 节流），有更新会提示。
