---
title: 更新日志
description: jvm 各版本变更记录。
layout: docs
---

## [0.11.0] - 2026-08-16

### 新增
- **`jvm exec <[distro@]版本> -- <命令>`**：用指定版本在子进程环境里执行命令，不动全局 current junction（如 `jvm exec 17 -- mvn test`）。无版本号时读 `.jvmrc` 再退到当前版本；批处理（mvn / gradlew 等 `.cmd`/`.bat`）自动经 `cmd.exe` 分发，子进程退出码原样传播。
- **`jvm outdated`**：检查已装版本 patch 更新，按（发行版, 大版本）分组并发查询，列出可升级行与升级命令。
- **`jvm doctor --fix`**：诊断报告后自动修复失败项——目录 / current 链接重建 / JAVA_HOME / shell 集成 / 用户 PATH 补全；注册表 PATH 里的旧 JDK 残留逐条确认后移除（`-y` 跳过）。新增第 10 项检查「用户 PATH」。
- **`.jvmrc` 目录自动切换**：shell 集成 v2 在 `.jvmrc` 变化时自动执行 `jvm use --auto`——cd 进含 `.jvmrc` 的目录自动切到该版本，cd 出去恢复切换前的手动版本。默认开启，`autoswitch = false`（或 `JVM_AUTOSWITCH=0`）关闭；老用户升级 jvm 后自动获得新钩子。

## [0.10.0] - 2026-08-13

### 新增
- **项目级版本固定 `.jvmrc`**：`jvm pin [版本]` 写入当前目录的 `.jvmrc`，`jvm use` 无参数时从当前目录逐级向上查找并读取（与 .nvmrc / .ruby-version 一致，支持 monorepo 子目录），团队成员无需各自记版本号。
- 新增 `make test-race` 竞态检测，CI 每次 push / PR 额外跑 `go test -race`。

## [0.9.0] - 2026-08-11

### 新增
- **新增 Azul Zulu (`zulu@`) 与 BellSoft Liberica (`liberica@`) 两个发行版 provider**：两者均支持 Windows x64 / ARM64。Zulu 走 Azul Metadata API 两步查询取官方 SHA256；Liberica 走 BellSoft Product Discovery API + 官方 SHA1。至此 jvm 覆盖五大主流 OpenJDK 发行版。
- 下载校验层支持多算法：`Asset` 契约由 `SHA256` 改为 `Checksum` + `ChecksumAlgo`（默认 sha256），各 provider 按发行版官方提供的哈希校验（Liberica 用 SHA1，其余 SHA256）。

## [0.8.0] - 2026-08-10

### 新增
- **jvm 自身发行 Windows ARM64 版本**：`jvm-windows-arm64-setup.exe` 与 `jvm-windows-arm64.zip`（安装向导界面为 x86 模拟运行，释放出的 jvm.exe 是 ARM64 原生）；一键安装脚本自动识别 x64 / ARM64。
- ARM64 版 jvm 默认下载 ARM64 (aarch64) 版 JDK（此前默认 x64，需手动配置）。
- Microsoft Build of OpenJDK 支持下载 Windows ARM64 构建（11/17/21/25 全部 LTS）。
- `arch` 配置接受别名：`amd64`→`x64`、`arm64`→`aarch64`。

### 变更
- Provider 层新增 `Configurable` 可选接口与统一配置分发：新增发行版适配器实现接口即自动接入目标架构/镜像配置。
- `arch=aarch64` 时使用 Corretto 会明确报错并建议改用 Temurin / Microsoft（Corretto 官方没有 Windows ARM64 构建；此前会静默下载 x64 版靠系统模拟运行）。

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
