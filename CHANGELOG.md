# Changelog

本项目所有重要变更均会记录在此文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 新增
- 新增 Azul Zulu (`zulu@`) 与 BellSoft Liberica (`liberica@`) 两个发行版 provider；
  两者均支持 Windows x64 / ARM64 (Zulu 走 Azul Metadata API 两步查询取官方 sha256，
  Liberica 走 BellSoft Product Discovery API + 官方 sha1)。
- 下载校验层支持多算法: `Asset` 契约由 `SHA256` 改为 `Checksum` + `ChecksumAlgo`
  (默认 sha256), 各 provider 按发行版官方提供的哈希校验 (Liberica 用 SHA1, 其余 SHA256);
  `internal/jdk` 的 `fileSHA256` 泛化为 `fileHash(path, algo)`。
- paths 支持 `JVM_HOME` 环境变量重定向根目录 (CI / 集成测试隔离文件副作用用); 新增
  CI workflow (`.github/workflows/ci.yml`): push/PR 每次跑单元测试, 每周 schedule +
  手动 `workflow_dispatch` 跑真实集成测试 (`scripts/integration-test.sh`, 全 5 发行版
  install/use/list/doctor/uninstall); Makefile 新增 `test` target。
- 本地开发构建不再污染环境: `make build` / `make run` 产物经 ldflags 注入
  `Bootstrap=off`, 启动时不写用户 PATH / shell profile (发行风味用
  `make build-dist`, `installer`/`release`/`dist-all` 自动走它); 任何构建可用
  `JVM_NO_BOOTSTRAP=1` 强制关闭自举; 自身位于系统 Temp 目录 (如 `go run` 的
  临时二进制) 时亦自动跳过自举。

## [0.8.0] - 2026-08-10

### 新增
- 发行产物新增 windows/arm64 架构: CI 打 tag 时同时产出
  `jvm-windows-arm64-setup.exe` 与 `jvm-windows-arm64.zip`
  (安装器 stub 为 x86, 在 ARM64 Windows 上靠系统内置模拟运行,
  释放出的 jvm.exe 为 ARM64 原生)。本地交叉编译用 `make build GOARCH=arm64`,
  构建产物改为按架构分目录存放 (`dist/<arch>/jvm.exe`)。
- `install.ps1` 一键安装脚本自动检测系统架构 (x64 / ARM64),
  下载匹配架构的便携 zip。
- ARM64 版 jvm 默认下载 ARM64 (aarch64) 的 JDK (此前默认 x64, 需手动配置)。
- Microsoft Build of OpenJDK 支持下载 Windows ARM64 (aarch64) 构建
  (11/17/21/25 全部 LTS; aka.ms 短链与 SHA256 旁路校验模式与 x64 一致)。
- `arch` 配置接受别名: `amd64`→`x64`、`arm64`→`aarch64` (统一走
  `app.NormArch` 规范化, 各 provider 行为一致)。

### 变更
- Provider 层新增 `Configurable` 可选接口与 `provider.ConfigureAll` 统一分发
  全局下载配置 (目标架构/镜像): provider 实现该接口即自动接入,
  main.go 不再显式认识具体发行版适配器。
- `arch=aarch64` 时使用 Corretto 会明确报错并建议改用 temurin / microsoft
  (Amazon Corretto 官方没有 Windows ARM64 构建; 此前会静默下载 x64 版
  靠系统模拟运行, 用户无感知)。

## [0.7.0] - 2026-08-08

### 新增
- 启动时静默检查 jvm 新版本 (24 小时节流): 距上次检查超 24h 才查 GitHub
  Release, 落后时提示运行 `jvm upgrade`, 网络失败永不阻断主命令。
- JDK 下载支持瞬时错误重试 (3 次指数退避) 与 HTTP Range 断点续传: 下载中断
  后下次 `jvm install` 从断点继续, 无需重下完整 zip。续传细节 (.part 文件)
  封装在下载层内部, 编排层与自更新流程无感知。
- `jvm doctor` 新增三项诊断:
  - `java 版本`: 实跑 `java -version`, 排除 "文件存在但二进制损坏/缺 DLL"。
  - `版本目录完整性`: 扫描 `~/.jvm/versions/` 各目录是否均有 `bin/java.exe`,
    标记解压中断的半成品。
  - `注册表 PATH 残留`: 检查用户 PATH 里是否有非 current/bin 的旧 JDK 路径。

### 变更
- 导出 `junction.MajorOf` 供 doctor 复用, 避免版本目录名解析逻辑重复。
- `app` 包新增 `CompareVersions` / `LatestGitHubTag` 共享基础设施,
  供更新检查使用 (不引入新依赖)。

## [0.6.1] - 2026-08-08

### 修复
- shell 集成与 Tab 补全在 Documents 目录被重定向时完全不生效: profile 路径
  用 `KnownFolderPath(FOLDERID_Documents)` 替代 `os.UserHomeDir()/Documents`。
  用户的 Documents 可能被重定向到其他盘 (OneDrive 接管 / 手动移动 / 企业 GPO),
  原实现拼出的路径与 PowerShell 实际查找的 `$PROFILE` 不一致。新实现与
  `[Environment]::GetFolderPath('MyDocuments')` 同源, 保证路径永远一致。
  (bash profile 用 `FOLDERID_Profile`, 不受 Documents 重定向影响。)

## [0.6.0] - 2026-08-08

### 新增
- PowerShell 5.1/7+ 与 Git Bash 的 Tab 智能补全 (`jvm <TAB>` 子命令、
  `jvm install <TAB>` distro@ 前缀、`jvm use <TAB>` 本地已装版本、
  `jvm available <TAB>` distro 名)。静默自举注入 profile, 用户零配置。
  提供 `jvm completion <shell> [--install]` 命令供手动重装/诊断。
  distro 列表从 provider 注册表运行时嵌入, 新增发行版自动刷新。
- `jvm doctor` 新增 Tab 补全状态检测项。

### 修复
- `installToProfile` 的 `endMarkerFor` 只替换首个 `>>>`, 改为 `ReplaceAll`
  (标记首尾各有一个 `>>>`, 两个都要翻成 `<<<`)。
- `removeBlock` 只移除首个匹配块, 改为循环移除所有重复块 (防止历史遗留
  的重复块越积越多)。
- profile 脚本内容改为纯 ASCII (注释英文): 避免 PowerShell 5.1 在中文系统
  上用 GBK 解码非 ASCII 字节导致语法损坏。不在写入端做 BOM 适配。

## [0.4.1] - 2026-08-07

### 修复
- `jvm upgrade` 的 SHA256 校验一直被静默跳过: `checksums.txt` 解析器没兼容
  GNU coreutils binary 模式的 `*` 前缀 (Windows Git Bash 的 `sha256sum` 默认
  就是这种格式 `<hash> *<file>`), 导致文件名匹配失败、降级到"跳过校验"。
- `jvm doctor` 对所有 Windows junction 误报"current 不是链接 (可能是普通目录)":
  之前用 `os.Lstat` 的 `ModeSymlink` 位判断, 但 Windows junction (reparse point)
  在 Go 里不设置 `ModeSymlink` (那是给真 symlink 的), 改用 `os.Readlink` 成功与否判断。

## [0.4.0] - 2026-08-07

### 新增
- 配置文件支持 (`~/.jvm/config.toml`): 可配置镜像源 (`mirror`) 和目标架构
  (`arch`, 支持 `x64` / `aarch64`, 服务 Windows on ARM 设备)。环境变量
  `JVM_MIRROR` / `JVM_ARCH` 可临时覆盖 (优先级高于文件)。
- `jvm use <X.Y.Z>` 支持少 build 号匹配: 如 `jvm use 21.0.12` 自动命中
  `21.0.12+8`, 不必记 build 号 (与 `install` 的输入习惯对齐)。

### 变更
- `jvm upgrade` 的 `.bak` 残留现在在 jvm 启动时自动清理, 兑现了既有注释承诺。
- `jvm available -a` 某大版本子版本数达上限 (50) 时提示可能未完整显示,
  引导用 `--major <N>` 单独查看。

### 修复
- 移除 `adoptium` 包里镜像 URL 和架构的硬编码, 改为可配置包级状态。

## [0.3.1] - 2026-08-07

### 变更
- 升级 release workflow 依赖的 GitHub Actions 到最新主版本, 消除
  Node.js 20 已弃用的警告:
  `actions/checkout@v5→v7`、`actions/setup-go@v6→v7`、
  `softprops/action-gh-release@v2→v3`。

## [0.3.0] - 2026-08-07

### 新增
- `jvm doctor` 命令: 全面诊断环境配置 (目录结构、current junction、JAVA_HOME、
  PATH 冲突、shell 集成、current 的 java.exe), 每项输出 ✓/✗ 并附修复建议。
- `jvm current` 现在实际执行 `java -version` 并打印输出 (5 秒超时), 而非仅
  提示路径让用户手动验证。
- `jvm uninstall` 删除前增加交互确认 `[y/N]`, 加 `-y`/`--yes` 可跳过 (便于脚本)。
- `jvm upgrade` 下载后增加 SHA256 完整性校验, 与 release 同步发布的
  `checksums.txt` 比对; 旧版 release 无 checksums 时警告但不阻断。
- 新增 MIT 开源协议 (LICENSE)。
- 补充 doctor / env / upgrade 包的表驱动单测。

### 变更
- `.github/workflows/release.yml` 发版时额外生成 `checksums.txt` asset;
  Release body 改从 CHANGELOG.md 抽取对应版本段落填充。

## [0.2.0] - 2026-08-06

### 新增
- `jvm available -a` / `--major <N>` 列出全部子版本。

### 变更
- 版本目录改用纯 semver 命名 (如 `21.0.12+8`), 与 available/use/uninstall 对齐。

## [0.1.4] - 2026-08-06

### 新增
- `use`/`uninstall` 接受不带 `jdk-` 前缀的版本号, 与 install/available 对齐。
- 纯大版本号取该版本下最新 build; 支持省略 build 号精确匹配。

### 变更
- 简化版本匹配规则: 纯大版本号取最新, 其余要求完整目录名。

### 修复
- 兼容 JDK8 旧式目录命名 (`jdk8u502-b07`) 的识别与切换。

## [0.1.3] - 2026-08-06

### 修复
- `upgrade` 解压临时 exe 落在 jvm.exe 同目录, 避免跨盘符 rename 失败。

## [0.1.2] - 2026-08-06

### 新增
- `available` 表格输出, 显示最新版本号与 LTS 标记。
- 构建时通过 ldflags 注入版本号, 源码不再手写版本字面量。
- 纯函数表驱动单测 + AGENTS.md。

## [0.1.0] - 2026-08-06

首个可用版本。

### 新增
- 核心命令: install / use / list / available / uninstall / current / init / upgrade / version。
- Windows junction 切换 (原生 syscall, 免管理员)。
- 注册表持久化 JAVA_HOME/PATH + WM_SETTINGCHANGE 广播。
- PowerShell / bash shell 集成 (当前终端即时生效)。
- 清华镜像优先 + 官方 CDN 回退下载。
- SHA256 校验、zip-slip 防护、原子解压。
- NSIS 安装包 + GitHub Actions 自动发布。

[Unreleased]: https://github.com/BaixuanZhu/jvm/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/BaixuanZhu/jvm/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/BaixuanZhu/jvm/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/BaixuanZhu/jvm/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/BaixuanZhu/jvm/compare/v0.1.4...v0.2.0
[0.1.4]: https://github.com/BaixuanZhu/jvm/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/BaixuanZhu/jvm/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/BaixuanZhu/jvm/compare/v0.1.0...v0.1.2
[0.1.0]: https://github.com/BaixuanZhu/jvm/releases/tag/v0.1.0
