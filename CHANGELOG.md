# Changelog

本项目所有重要变更均会记录在此文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 修复

- 修复 JDK 下载在连接停滞（对端不断开但不再传数据）时无限挂起的问题：下载
  增加读停滞看门狗——距最后一次收到数据超过 30 秒即中断本次尝试并自动重试
  （断点续传，已下载部分不丢）。大文件下载不设整体超时（避免慢网络误杀），
  此前半死连接会让进度条原地卡死且永不报错；重试时也会显式打印
  `⚠️ 下载中断/停滞, Xs 后重试` 提示（原先静默退避）。

### 新增

- `jvm doctor` 新增 3 项检查（11 → 14 项）：**配置文件**（config.toml 存在但
  语法非法时显式报 ✗——此时 mirror/arch 等自定义项已静默回退默认，启动时的
  一次性警告容易错过）；**临时目录残留**（`~/.jvm` 下 `.tmp-extract-*` 解压
  半成品，安装中断遗留、白占磁盘，`doctor --fix` 自动清理）；**下载缓存**
  （信息性汇报 zip 数量/占用/未完成分片，占用大或含 `.part` 时附
  `jvm cache clean` 释放提示）。

- `jvm available` 查询结果本地缓存（`~/.jvm/available-cache.json`，10 分钟有效）：
  二次查询不再实时打全部发行版 API，大幅提速。表格形态按发行版整表缓存；
  `-a` / `--major` 分组形态按 `distro@major` 逐组缓存、增量复用（只重查缺失或
  过期的组，全量子版本列表这一最重查询收益最大）。查询失败的结果不落盘，
  目标架构变化（`arch` 配置）自动整体失效；`-r` / `--refresh` 强制绕过缓存
  直查。命中时输出会标注 `⚡ 缓存结果` 一行。

- `jvm update --all [-y]`：一条命令升级全部落后的 (发行版, 大版本) 组——并发
  检查与 `outdated` 同源，汇总打印各组升级计划后一次确认，逐组执行与单组
  `update` 完全相同的闭环（装新 → 条件切换 → 清理旧版）。某组失败不阻断
  其余组，末尾给出成功/未处理汇总；存在失败或跳过组时以非零码退出，供
  脚本感知。

- Tab 补全覆盖扩展：`jvm exec` 补本地已装版本（仅 `--` 之前的版本槽，之后让位
  给要执行的命令）；`available` 补 `-a` / `-m` 选项；`doctor` 补 `--fix` / `-y`；
  `init` / `completion` 补 `powershell` / `bash` 参数与 `--install`。`install`
  仅在版本槽补 `distro@` 前缀，本地 zip 路径槽让给 shell 默认文件补全。
  补全块 token 递增至 v5，升级后首次启动自动刷新老 profile 块。

## [0.13.0] - 2026-09-05

### 新增

- 新发行版 `temurin-ea`（Temurin 早期访问版）：跟踪尚未 GA 的大版本预览构建
  （如 `jvm install temurin-ea@28` / `jvm available temurin-ea`），EA 大版本
  列表实时从 Adoptium API 归并（随上游滚动，无需升级 jvm）。版本形如
  `28+14-ea-beta`，`outdated` / `update` 对 EA 组照常工作（build 级升级）。
  清华镜像不同步 EA 构建，下载直连 GitHub release 资产。

- 新发行版 `graalvm`（Oracle GraalVM，CPU LTS 线 21/25）：`jvm install
  graalvm@21` / `jvm available graalvm`。直连 Oracle 官方 CDN（国内可直连，
  无镜像），SHA256 走官方 `.sha256` 旁路校验；版本号形如 `21.0.12`。
  官方无 Windows ARM64 构建，`arch=aarch64` 下会明确提示并建议改用
  temurin/microsoft。GraalVM 创新线（25iN）与 CE 不在本版范围。

- 本地 zip 包安装：`jvm install <distro@版本> <zip文件路径>`（如
  `jvm install temurin@21.0.5+11 D:\downloads\jdk.zip`），适合内网/代理环境
  手动下载后纳管。不访问网络、不做远程校验和校验（本地文件由用户负责），
  安装后的目录命名与远程安装完全一致，`use`/`uninstall`/`outdated` 等
  命令照常工作。

- 下载缓存：安装包 zip 以 `{distro}-{版本}.zip` 留存 `~/.jvm/cache/`（随
  `install_dir` 走），卸载后重装同版本直接复用、不再重新下载（命中前先校验
  完整性，损坏/篡改自动删除重新下载）。新命令 `jvm cache` 查看缓存条目与
  占用，`jvm cache clean` 清空（含中断残留的 `.zip.part`）。

- 配置文件新增 `install_dir` 键（环境变量 `JVM_INSTALL_DIR` 同效）：把 JDK 安装
  数据目录（`versions/`）重定向到其他盘（如 `D:\jdks`），适合 C 盘空间紧张的
  场景。控制面（`config.toml`、`current` junction、注册表 PATH/JAVA_HOME）
  留在 `~/.jvm` 不动，无需任何迁移即生效；已装版本需手动搬到新目录，
  `jvm doctor` 检测到旧默认目录仍有版本时会给出搬迁提示。

## [0.12.0] - 2026-09-05

### 新增

- `jvm update <[distro@]大版本> [-y]`：patch 升级一步到位——安装该大版本最新 patch
  （组内已有最新版则跳过下载直接进入清理）、（当前正在使用该组版本时）自动切换
  current、清理组内全部旧 patch 目录（被进程占用删不掉的跳过并提示稍后手动
  `uninstall`）。仅接受大版本号；`jvm upgrade` 仍只负责 jvm 自身更新，两者语义
  分离。`jvm outdated` 的升级提示同步改为引导 `jvm update`。

- `jvm home`：打印当前 JAVA_HOME 路径（`~/.jvm/current`，单行无装饰），与注册表
  持久化的值一致且切换版本后不变，供 CI 脚本 / IDE 配置直接引用。

### 修复

- Tab 补全的子命令列表补齐 `outdated` / `exec`（引入补全时即遗漏），并新增
  `update` / `home` 的补全；`jvm update <TAB>` 补全本地已装版本的
  `distro@大版本` 形式（update 仅接受大版本号）。

### 变更

- 补全块引入版本 token（`jvm-completion: v1`）并纳入启动自举的版本化重写：
  老用户 profile 里只有 marker 的旧补全块，下次运行 jvm 时自动重写为新内容
  （与集成块 v2 同机制），覆盖 `jvm upgrade` / 安装器 / 手动换便携 zip 全部
  更新途径。此前补全块只按 marker 存在性判断，内容变更后老用户需手动执行
  `jvm completion <shell> --install` 才能刷新。

## [0.11.1] - 2026-09-04

### 修复

- 修复 Temurin 2026-07 CPU 起四段式版本号（如 `25.0.4.1+1`）下 `jvm available`
  显示错误版本号（`25.0.4+101`）、照此 `jvm install` 报「API 返回 404」的问题。
  根因是 Adoptium 的 semver 字段仍编码为三段（`25.0.4+101.0.LTS`，第 4 段被编码进
  build 号），从 semver 反推的短版本号无法反查 API；现改为直接采用 API 顶层
  `release_name` 字段（剥 `jdk-` 前缀），保证显示的版本号可安装。影响
  `jvm available` / `available -a` 展示与 `jvm outdated` 的远端版本号，
  老版本号（三段式）行为不变。

### 移除

- 移除发版后自动向 `microsoft/winget-pkgs` 提交 manifest 更新 PR 的 CI job（0.10.0
  引入，因暂不发布 winget 而移除；将来要上 winget 可从 git 历史找回 workflow 配置）。

## [0.11.0] - 2026-08-16

### 新增

- `jvm exec <[distro@]版本> -- <命令>`：用指定版本在子进程环境里执行命令，不动全局
  current junction（一次性多版本测试与 CI 脚本场景，如 `jvm exec 17 -- mvn test`）。
  无版本号时读 `.jvmrc` 再退到当前版本；只解析本地已装版本，不自动下载；
  `java`/`javac` 优先在该版本 bin 内解析（不依赖系统 PATH），批处理
  （`.bat`/`.cmd`，如 mvn/gradlew）经 `cmd.exe /c` 分发，子进程退出码原样传播。
- `jvm outdated`：检查已装版本 patch 更新——按（发行版, 大版本）分组并发查各
  provider 的 `LatestPatch`，列出可升级行（如 `temurin@21  21.0.5+11 → 21.0.8+7`）
  并提示升级命令（不同 patch 为不同目录名，`jvm install` 并列安装不冲突，无需
  重装参数）。
- `jvm doctor --fix`：诊断报告后对失败项执行自动修复——目录结构 / current 链接
  重建到最新已装版本（current 为非空普通目录时跳过，不删用户数据）/ JAVA_HOME /
  shell 集成与补全注入 / 用户 PATH 补 current/bin；注册表 PATH 里的旧 JDK 残留
  逐条 y/N 确认后移除（`doctor --fix -y` 跳过确认）。同时新增第 10 项检查
  「用户 PATH」（注册表用户 PATH 是否含 current/bin，原来只查进程 PATH）。
- `.jvmrc` 目录自动切换：shell 集成脚本 v2（PowerShell 包装 prompt / bash 挂
  PROMPT_COMMAND，双层缓存，目录与 rc 未变时零开销）在 `.jvmrc` 变化时自动执行
  `jvm use --auto` —— cd 进含 `.jvmrc` 的目录自动切到该版本，cd 出去恢复切换前的
  手动版本（`~/.jvm/auto-state` 记基线，显式 `jvm use` 即新基线会清掉）。
  默认开启，`config.toml` 设 `autoswitch = false`（或 `JVM_AUTOSWITCH=0`）关闭；
  集成块带版本 token，老用户升级 jvm 后由启动自举自动重写 profile 拿到新钩子。

## [0.10.0] - 2026-08-13

### 新增
- 项目级版本固定 `.jvmrc`：`jvm pin [版本]` 写入当前目录的 `.jvmrc`，
  `jvm use` 无参数时从当前目录逐级向上查找并读取（与 .nvmrc / .ruby-version
  一致，支持 monorepo 子目录）。文件格式为一行 `[distro@]version`（支持 `#`
  注释）；版本解析链路完全复用现有 `ParseVersionSpec` + `ResolveVersion`，
  指定版本未安装时报错提示 `jvm install`（不自动下载）。新增 `internal/pinrc`
  包；为把目录名转成 distro@version，导出 `junction.SplitDistro`。
- 新增竞态检测：`make test-race`（`go test -race ./...`）本地可用，CI 每次
  push/PR 在单元测试后额外跑一遍 race，锁定 `cmd` 并发查询与 `provider` 注册表
  等路径的正确性。
- 发版后自动向 `microsoft/winget-pkgs` 提交 winget manifest 更新 PR（用官方
  wingetcreate，内部自动 fork + 提 PR，双架构 x64/arm64 的 setup.exe）。需在仓库
  Secrets 配置 `WINGET_PAT`；首次需手动提交 `BaixuanZhu.jvm` 初始 manifest。

## [0.9.0] - 2026-08-11

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

## [0.5.0] - 2026-08-07

### 新增
- Provider 抽象层 (`internal/provider/`): 统一各发行版的接口与注册表, 为后续扩展铺路。
- CLI `[distro@]version` 语法: 指定发行版 (如 `jvm install corretto@21`), 省略前缀默认 temurin。
- Amazon Corretto (`corretto@`) 与 Microsoft Build of OpenJDK (`microsoft@`) 两个发行版 provider。
- PowerShell 一键安装脚本 `install.ps1`: 一行命令下载安装, 自动检测系统架构。

### 变更
- 版本目录命名改为 `{distro}-{version}` (如 `temurin-21.0.12+8`); 旧的无前缀目录视为
  temurin, 向后兼容。
- 简化版本号输入语义: 只保留大版本号取最新 + 完整版本号精确匹配两种形式。

### 修复
- `install.ps1` 在 `iwr | iex` 管道下解析失败: 去掉输出里的 BOM 并改用英文, 避免管道编码问题。
- `MigrateLegacyDirs` 误把新的 `{distro}-{version}` 目录当成无法识别而跳过。

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

[Unreleased]: https://github.com/BaixuanZhu/jvm/compare/v0.11.1...HEAD
[0.11.1]: https://github.com/BaixuanZhu/jvm/compare/v0.11.0...v0.11.1
[0.11.0]: https://github.com/BaixuanZhu/jvm/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/BaixuanZhu/jvm/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/BaixuanZhu/jvm/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/BaixuanZhu/jvm/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/BaixuanZhu/jvm/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/BaixuanZhu/jvm/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/BaixuanZhu/jvm/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/BaixuanZhu/jvm/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/BaixuanZhu/jvm/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/BaixuanZhu/jvm/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/BaixuanZhu/jvm/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/BaixuanZhu/jvm/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/BaixuanZhu/jvm/compare/v0.1.4...v0.2.0
[0.1.4]: https://github.com/BaixuanZhu/jvm/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/BaixuanZhu/jvm/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/BaixuanZhu/jvm/compare/v0.1.0...v0.1.2
[0.1.0]: https://github.com/BaixuanZhu/jvm/releases/tag/v0.1.0
