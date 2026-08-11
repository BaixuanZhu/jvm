---
title: 常见问题
description: jvm 使用中的常见问题与解答。
---

## 需要管理员权限吗？

**不需要。** junction 通过原生 syscall 创建（普通用户权限足够），注册表只写 `HKCU`（用户级），安装目录在 `%LOCALAPPDATA%` 下。

## 支持 CMD（cmd.exe）吗？

CMD **不支持** shell 自动集成（doskey 体验差）。jvm 本身在 CMD 里可用，但 `jvm use` 后当前 CMD 窗口的 `java` 不会立刻变——新开窗口即可。推荐用 PowerShell 或 Git Bash。

## `jvm use` 后 java 没变？

1. 确认你重开过至少一次终端（让 PATH 和 shell 集成生效）
2. 跑 `jvm doctor` 做 9 项诊断，按修复建议处理
3. 确认使用的是 PowerShell 或 bash（CMD 见上一条）

## 国内下载慢 / 失败？

- Temurin 默认走清华镜像，失败会自动回退官方 CDN
- 下载支持断点续传（3 次指数退避重试 + HTTP Range），中断后重跑 `jvm install` 会接着下
- 安装脚本本身拉不动 GitHub 时，设置 `$env:JVM_INSTALLER_MIRROR` 指向镜像前缀，或改用安装包 / 便携版

## 和 nvm-windows / jabba 有什么区别？

定位类似，jvm 的差异点：多发行版支持（`distro@` 语法）、当前终端即时生效、免管理员、国内镜像加速、`jvm doctor` 环境诊断、Tab 智能补全。

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
