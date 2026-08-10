---
title: 更新日志
description: jvm 各版本变更记录。
layout: docs
---

## [0.7.0] - 2026-08-08

### 新增
- 启动时静默检查 jvm 新版本（24 小时节流）：距上次检查超 24h 才查 GitHub Release，落后时提示运行 `jvm upgrade`，网络失败永不阻断主命令。
- JDK 下载支持瞬时错误重试（3 次指数退避）与 HTTP Range 断点续传：下载中断后下次 `jvm install` 从断点继续，无需重下完整 zip。
- `jvm doctor` 新增三项诊断：
  - `java 版本`：实跑 `java -version`，排除"文件存在但二进制损坏/缺 DLL"。
  - `版本目录完整性`：扫描 `~/.jvm/versions/` 各目录是否均有 `bin/java.exe`，标记解压中断的半成品。
  - `注册表 PATH 残留`：检查用户 PATH 里是否有非 current/bin 的旧 JDK 路径。

### 变更
- 导出 `junction.MajorOf` 供 doctor 复用，避免版本目录名解析逻辑重复。
- `app` 包新增 `CompareVersions` / `LatestGitHubTag` 共享基础设施，供更新检查使用（不引入新依赖）。

## [0.6.1] - 2026-08-08

### 修复
- shell 集成与 Tab 补全在 Documents 目录被重定向时完全不生效：profile 路径改用 `KnownFolderPath(FOLDERID_Documents)`，与 PowerShell 实际查找的 `$PROFILE` 永远一致。

## [0.6.0] - 2026-08-08

### 新增
- PowerShell 5.1/7+ 与 Git Bash 的 Tab 智能补全（`jvm <TAB>` 子命令、`jvm use <TAB>` 本地已装版本等），静默自举注入，零配置。
- `jvm doctor` 新增 Tab 补全状态检测项。

### 修复
- profile 标记块替换只替换首个标记的问题；重复块循环移除；profile 脚本改为纯 ASCII，避免 PowerShell 5.1 中文系统上 GBK 解码损坏。

---

更早版本请见仓库 [CHANGELOG.md]({{ site.github_repo }}/blob/main/CHANGELOG.md) 与 [Releases]({{ site.github_repo }}/releases) 页面。
