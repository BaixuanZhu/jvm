---
title: 发行版
description: jvm 支持的 JDK 发行版与 distro@version 语法说明。
---

jvm 用 `[distro@]version` 语法选择发行版，**省略前缀默认 Temurin**。

| 发行版 | 前缀 | 数据源 | 下载通道 | Windows ARM64 | 备注 |
|--------|------|--------|----------|---------------|------|
| Temurin (Adoptium) | `temurin`（可省略） | Adoptium API | 清华镜像优先 → 官方 CDN 回退 | ⚠️ 部分版本（如 21 有、17/25 暂无） | 默认推荐 |
| Amazon Corretto | `corretto` | corretto-downloads indexmap | 直连 CloudFront CDN | ❌ 官方未发布 | 附带官方 SHA256 清单 |
| Microsoft Build of OpenJDK | `microsoft` | aka.ms 探测 | 直连 VisualStudio CDN | ✅ 全部 LTS | 仅 LTS 版本 |

ARM64 设备上 jvm 默认下载 ARM64 (aarch64) 版 JDK；选择 Corretto 时会明确报错并建议改用 Temurin / Microsoft。也可在 `~/.jvm/config.toml` 用 `arch` 显式指定目标架构（见配置页）。

## 用法示例

```powershell
jvm install 21               # temurin（默认）
jvm install corretto@21      # corretto
jvm install microsoft@21     # microsoft（仅 LTS）

jvm available corretto       # 查看 corretto 有哪些大版本
jvm use corretto@21          # 切换到 corretto 21
```

## 版本号写法

- **大版本**：`21` —— 自动取该大版本的最新 GA / 最新已装 patch
- **精确版本**：`21.0.12+8` —— 完整版本号（含 build 号）精确匹配

已安装的多个发行版互不干扰，`jvm use` 时带上前缀即可精确切换。

## 为什么只支持这三家？

三者都提供**纯 zip 分发包**，解压即用，无需跑 msi 安装器——这是 jvm 能免管理员权限的关键。后续如有同样以 zip 分发的发行版，可以通过 Provider 抽象层扩展。
