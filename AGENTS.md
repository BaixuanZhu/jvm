# AGENTS.md

## Project overview

`jvm` 是 Windows 上的 Java 版本管理器（类似 nvm-windows / jabba）。Go 编写的单二进制 CLI；通过 Windows junction 切换 JDK 版本，免管理员权限，自动配置 PATH/JAVA_HOME 与 shell 集成。支持多发行版（Temurin / Corretto / Microsoft Build of OpenJDK / Azul Zulu / BellSoft Liberica），CLI 用 `[distro@]version` 语法选择发行版（省略前缀默认 temurin）。命令面：install / use（无参读 `.jvmrc`，集成钩子下 cd 自动切换）/ pin / uninstall / list / available / outdated（patch 升级检查）/ update（大版本 patch 一键升级：装新 → 条件切换 → 清理旧版，仅接受大版本号；`upgrade` 只管 jvm 自身更新，语义分离）/ current / home（打印当前 JAVA_HOME 路径）/ exec（指定版本一次性执行，不动全局）/ doctor（`--fix` 自动修复）/ init / completion / upgrade。**仅支持 Windows（x64 / ARM64）**。

## Setup commands

需 Go 1.26+ 与 GNU Make（Git Bash / MinGW / WSL 自带）。

- Build: `make build` → `dist/<arch>/jvm.exe`（`-trimpath -ldflags "-s -w"`；`GOARCH` 默认取本机 `go env GOARCH`，交叉编 ARM64 用 `make build GOARCH=arm64`）。**开发构建注入 `Bootstrap=off`，产物启动不做静默自举**；发行风味用 `make build-dist`（`installer`/`release`/`dist-all` 自动走它）
- Run: `make run ARGS="version"`（先 dev build 再运行，不污染环境）
- Format / 静态检查: `make fmt` / `make vet`（即 `go fmt ./...` / `go vet ./...`）
- 测试: `make test`（即 `go test ./...`）；竞态检测 `make test-race`（`go test -race ./...`）
- Deps: `make tidy`
- 安装包: `make installer` → `dist/jvm-windows-<arch>-setup.exe`（需 NSIS：`scoop install nsis` 或 `choco install nsis`）
- 便携 zip: `make release` → `dist/jvm-windows-<arch>.zip`（供 `jvm upgrade` 拉取）
- 全部发行资产: `make dist-all`（仅当前 `GOARCH`；双架构由 CI 分别传 `GOARCH=amd64/arm64` 各跑一遍）
- Clean: `make clean`

发布：打 tag（`git tag v0.1.0 && git push --tags`）触发 `.github/workflows/release.yml`，在 windows-latest **先跑单测 gate（红则中止发版）** 再自动编译（amd64 + arm64 双架构）、打安装器 + 便携 zip 并发 GitHub Release（四 asset + checksums.txt）。ARM64 安装器 stub 为 x86，在 ARM64 Windows 上靠系统内置模拟运行，释放出的 jvm.exe 是 ARM64 原生。（曾有发版后自动提交 winget manifest PR 的 job，因暂不发布 winget 已移除；将来要上 winget 可从 git 历史的 release.yml 找回。）

CI（`.github/workflows/ci.yml`）：push 到 main / PR 每次跑单元测试（`go vet` + `go test` + `go test -race` + `go build`，windows-latest）；真实集成测试（`scripts/integration-test.sh`，跑全 5 发行版的 install/use/list/doctor/uninstall + ARM64 下载链路 + `jvm pin` + `jvm available`（各 provider 的 Available 端点）+ `jvm outdated` + `jvm update`（旧 patch 升级 → 自动切换 → 清理旧目录 + 已是最新幂等）+ `jvm home` + `jvm exec`（bin 内解析 / 退出码传播 / 批处理分发 / 无参读 `.jvmrc` / 未装版本 fail-fast）+ 裸 `jvm use` 读 `.jvmrc` + `use --auto` 状态机（切换 / 恢复 / 未装警告 / 显式 use 清基线后离开静默 / `JVM_AUTOSWITCH=0` 禁用静默）+ `doctor --fix` junction 重建与注册表 PATH 残留清理闭环（造假旧 JDK 条目 → 检出 → 删除 → 验证不误删 current/bin）+ 负向用例（未知 distro / use 未装版本 / exec 未装版本 / uninstall 不存在）+ `jvm upgrade` 端到端）仅每周 schedule + 手动 `workflow_dispatch` 触发（下载 ~1GB JDK，重）。集成 job 用 `JVM_HOME` 指向 runner 临时目录隔离文件副作用、`JVM_NO_BOOTSTRAP=1` 关自举（`use` 仍写一次性 runner 的注册表，可接受）。本地手跑集成脚本前注意：`use` 会改本机注册表 JAVA_HOME/PATH、残留清理用例会临时改用户 PATH。**集成脚本在 Git Bash 下跑，传给 jvm.exe 的 `/c` 等 flag 必须写 `//c`（MSYS 会把单个 `/c` 转成 `C:\`），删 junction 用 `cmd //c rmdir`（`rm` 会穿透 junction 删目标内容）**。

**发版前必须更新 `CHANGELOG.md`**：在 `## [Unreleased]` 下方新增 `## [<版本号>] - <YYYY-MM-DD>` 段落，按 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 分类（新增/变更/修复/移除）。CI 的 release workflow 用 awk 从该段落抽取 Release body（抽不到则 Release 页面无说明），漏写会导致 GitHub Release 页面信息缺失。文档/纯测试/重构等非用户可见变更不必记。

## Code style

- 格式与静态检查走 Makefile：`make fmt`、`make vet`；测试 `go test ./...`。无额外 linter 配置（无 `.golangci.yml` / `.editorconfig`）。
- 代码注释与用户可见的 CLI 文案均为**中文**；新增输出信息请保持中文。
- 常规 Go 惯例：包注释、导出符号注释、错误用 `fmt.Errorf("...: %w", err)` 包裹。

## Workflow

- **单人开发，直接提交 `main`**：不需要为每个特性另开分支，可直接在 `main` 上提交。仅在改动破坏性较大、想隔离试错、或并发推进多条线时才**推荐**（非强制）开 feat/fix 分支；合并用 `git merge --no-ff`（历史里已有 `Merge feat/...` 记录可循）。无论是否开分支，提交流程不变。
- 提交信息用 Conventional Commits（中文描述），历史风格参考：`feat(jdk): ...` / `fix(shell): ...` / `docs: ...` / `refactor: ...`；可选 scope 用对应包名。
- 发版：打 tag（`git tag vX.Y.Z && git push --tags`）→ CI 自动编 + 发 Release；详见下文 Setup / Hard constraints。

## Architecture

命令路由在 `main.go`（纯 switch，无框架），各子命令实现在 `cmd` 包（编排层：解析参数 → 调底层包 → 输出；`cmd.go` 主体 + 按命令分文件 `exec.go`/`outdated.go`/`auto.go`），基础设施按职责拆在 `internal/`：

- `internal/provider/` — **Provider 抽象层**：核心接口（`Name`/`DisplayName`/`Available`/`Resolve`/`LatestPatch`/`ListVersions`）+ `Base` 基类（默认实现）+ 注册表（`Register`/`Get`/`All`）+ **`Configurable` 可选接口与 `ConfigureAll(arch, mirror)` 分发**（provider 实现 `Configurable` 即在启动时自动接收全局目标架构/镜像，main.go 无需改动；temurin 两者都用，corretto/microsoft/zulu/liberica 只用 arch）。各发行版适配器嵌入 `Base` 按需 override。新公共 provider 逻辑优先放这里或 `internal/app/`，而非塞进某个业务包。
- `internal/provider/temurin/` — Temurin (Adoptium) 适配器：Adoptium API 查大版本/最新 GA/精确版本，解析官方 CDN 直链与清华镜像 URL（轻量 JSON 走官方 API，大文件下载走镜像）。支持 x64 / aarch64；**注意上游 Windows ARM64 覆盖不全**（如 21 有、17/25 暂无，Adoptium API 对缺失组合返回 404），查询错误消息统一带 `(windows/{arch})` 上下文便于定位。
- `internal/provider/corretto/` — Amazon Corretto 适配器：corretto-downloads 仓库的 indexmap JSON（含 SHA256，按架构 map 化索引），直连 CloudFront CDN（无镜像）。**官方无 Windows ARM64 构建**：`arch=aarch64` 时各入口统一报 `errNoWindowsARM64` 并建议改用 temurin/microsoft。
- `internal/provider/microsoft/` — Microsoft Build of OpenJDK 适配器：aka.ms 短链重定向探测 + `.sha256sum.txt` 旁路校验，仅 LTS（11/17/21/25），直连 VisualStudio CDN（无镜像）。x64 / aarch64 均支持（短链文件名后缀参数化）。
- `internal/provider/zulu/` — Azul Zulu 适配器：Azul Metadata API 两步查询（列表端点拿直链 + `package_uuid`，详情端点拿 `sha256_hash`），按文件名 `-ca-jdk` 过滤纯 JDK 变体（排除 fx/crac），直连 cdn.azul.com（无镜像）。x64 / aarch64 均支持（API `arch=arm64`，文件名 `win_aarch64`）。
- `internal/provider/liberica/` — BellSoft Liberica 适配器：BellSoft Product Discovery API 一次拉该架构全量 JDK zip（API 无 feature 过滤，客户端按 `latestInFeatureVersion`/`featureVersion` 过滤），**官方仅提供 sha1**（无 sha256）故用 SHA1 做完整性校验（`Asset.ChecksumAlgo="sha1"`），下载直链自拼 `download.bell-sw.com` 官方 CDN（替代 API 返回的 GitHub 域）。x64 / aarch64 均支持（API `arch=arm&bitness=64`）。
- `internal/jdk/` — 下载、完整性校验（SHA256/SHA1，按 provider 提供）、解压、原子替换到 `~/.jvm/versions/`（发行版无关，只认 `*app.Asset`）。
- `internal/junction/` — Windows junction 创建/删除/解析，用**原生 syscall**（`FSCTL_SET_REPARSE_POINT`），不调 `cmd.exe`（避免注入面）。
- `internal/env/` — 注册表读写（`HKCU\Environment`）+ 广播 `WM_SETTINGCHANGE`，持久化 `JAVA_HOME`/PATH/`jvm` 自身 PATH；`RemoveFromUserPath` 供 doctor --fix 清理旧 JDK 残留条目（大小写不敏感、`filepath.Clean` 后匹配，纯逻辑在 `filterUserPath` 供表测）。
- `internal/shell/` — PowerShell/bash 集成脚本生成 + 写入 `$PROFILE`/`~/.bashrc`（幂等，缺失才补）；profile 路径走 `KnownFolderPath`（`FOLDERID_Documents`/`FOLDERID_Profile`），兼容 Documents 目录重定向。集成脚本 v2 除 `jvm use` 会话刷新 wrapper 外还含 **`.jvmrc` 自动切换钩子**（PowerShell 包装 `prompt`、bash 挂 `PROMPT_COMMAND` 前插 + case 守卫防重复；双层缓存——目录/rc 未变零开销；脚本纯 ASCII 防 PS 5.1 GBK 解码损坏）。集成块与补全块各自内嵌版本 token（`jvm-integration: v2` / `jvm-completion: v1`），`EnsureIntegration` 经参数化的 `ensureBlockVersioned` 对缺块或缺当前 token 的老块自动重写升级；**改集成/补全脚本内容（含新增发行版导致的 distro 列表变化）必须递增对应 token**，否则老用户拿不到新脚本。
- `internal/upgrade/` — 走 GitHub Release 自更新。
- `internal/updatecheck/` — 启动时静默检查 GitHub 新版本（24h 节流，落后才提示，失败永不阻断）；只依赖 `app`，不依赖 `upgrade`（检查提示与执行升级职责分离）。
- `internal/doctor/` — `jvm doctor` 环境诊断：目录结构 / junction / JAVA_HOME / PATH 冲突 / shell 集成 / current 的 java / java 版本（实跑 `java -version`）/ 版本目录完整性 / 用户 PATH（注册表是否含 current/bin）/ 注册表 PATH 残留 共 10 项，每项输出 ✓/✗ 并附修复建议。检查函数收显式参数、不读全局，便于表驱动测试。`doctor --fix`（`Run(fix, assumeYes)`）对失败项执行修复映射：目录/junction 重建到最新已装（非空普通目录跳过）/JAVA_HOME/集成与补全注入/用户 PATH 补 current/bin 自动修，PATH 残留逐条 y/N 确认后经 `env.RemoveFromUserPath` 删除（`-y` 跳过），需重装/动系统 PATH 的项只保留建议；残留检测提取为 `ResidueEntries` 供诊断与修复同源。
- `internal/config/` — 用户配置加载（`~/.jvm/config.toml`）：覆盖 `mirror`（下载镜像，默认清华 TUNA，仅 temurin 消费）、`arch`（目标架构，默认跟随 `runtime.GOARCH`：amd64 版 → `x64`，arm64 版 → `aarch64`；五个 provider 均消费 arch）、`autoswitch`（`.jvmrc` 自动切换，默认 true；文件解析用 `fileConfig` 的 `*bool` 区分未设置与显式 false）、`install_dir`（数据目录重定向，默认空 = `~/.jvm`；把 `versions/` 挪到其他盘，控制面 config.toml/current/注册表 PATH 不动）。优先级：环境变量（`JVM_MIRROR`/`JVM_ARCH`/`JVM_AUTOSWITCH`/`JVM_INSTALL_DIR`，布尔走 `strconv.ParseBool`，非法值警告后忽略）> 配置文件 > 默认值；配置文件缺失视为正常，解析失败警告后回退默认。
- `internal/paths/` — `~/.jvm` 下目录路径（`init()` 里基于 `os.UserHomeDir()` 计算）。目录分两层：**控制面**（`Root`：config.toml/current junction/auto-state，注册表 PATH/JAVA_HOME 指向 `Root/current/bin`，永不迁移）与**数据面**（`dataRoot`，默认 = Root：`VersionsDir`；`SetInstallDir` 供 main 在 config.Load 后按 `install_dir` 键重定向，已装版本不自动搬迁，doctor 检测旧默认目录残留时给提示）。`JVM_HOME` 环境变量整体重定向 Root（CI 隔离用）。`AutoStateFile`（`~/.jvm/auto-state`）记录 `.jvmrc` 自动切换前的手动版本目录名，供离开 `.jvmrc` 目录时恢复，显式 `jvm use` 会清掉它。
- `internal/pinrc/` — 项目级版本固定文件 `.jvmrc` 的查找（从 cwd 逐级向上）、解析、写入；`jvm use` 无参与 `jvm exec` 无版本号时读取、`jvm pin` 写入、shell 自动切换钩子触发 `jvm use --auto` 时由 `cmd/auto.go` 消费。版本解析复用 `app.ParseVersionSpec` + `junction.ResolveVersion`，本包不做语义校验。
- `internal/app/` — **共享基础设施层**（版本号、`Fail`、版本解析、统一 HTTP client、`CompareVersions` 版本比较、`LatestGitHubTag` release 查询、`NormArch` 架构规范化（`x64`/`aarch64` 规范值 + `amd64`/`arm64` 别名）、`Asset`/`Release`/`VersionSpec` 跨 provider 下载契约）；存在目的是被几乎所有业务包依赖以**避免循环依赖**——新公共逻辑优先放这里，而非塞进某个业务包。

运行模型：`~/.jvm/current` 是指向当前选中版本的 junction；PATH 永远指向 `~/.jvm/current/bin`，切换版本 = 重建 junction，故新终端无需刷新环境变量即生效。当前终端即时生效靠 shell 集成函数在 `jvm use` 后于会话内刷新 `JAVA_HOME`/PATH（子进程改不了父 shell 环境，靠 wrapper 函数绕过）。`.jvmrc` 自动切换叠加在此模型上：shell 钩子在 rc 变化时调 `jvm use --auto`（`cmd/auto.go`，纯决策 `decideAuto` + `auto-state` 状态机），cd 进含 `.jvmrc` 的目录自动切、离开恢复手动基线；`jvm exec` 则完全绕开 junction，只在子进程 env 里注入指定版本的 JAVA_HOME/bin（批处理经 `cmd.exe /c` 分发）。

## Hard constraints

- **仅 Windows（x64 / ARM64）**：大量依赖 junction、注册表、Windows syscall，不可移植到 *nix。jvm 自身发行 amd64 / arm64 双架构产物；ARM64 JDK 下载经 `arch = "aarch64"` 配置支持（arm64 版二进制默认即 aarch64，见 `internal/config`）。
- **Go 1.26+**（见 `go.mod`）。
- **版本号通过 ldflags 单一注入**：`internal/app/app.go` 的 `Version` 是 `var`（默认值 `0.1.0-dev`），构建时由 Makefile 的 `-X jvm/internal/app.Version=$(VERSION)` 覆盖（`VERSION` 取自 `git describe`）；CI 从 tag 推导并传给 `make build`。**禁止**在源码里手写版本字面量、禁止让 `Makefile` 与 `app.Version` 各维护一份。
- 自更新仓库地址硬编码在 `internal/upgrade/upgrade.go` 的 `githubRepo` 常量，fork 后需改。
- junction 必须用原生 syscall 实现，**禁止**改为 `cmd /c mklink` 等子进程方式（引入命令注入面）。
- PATH 持久化必须走注册表 + `WM_SETTINGCHANGE`，**禁止**用 `setx`（会截断长 PATH）。

## Security considerations

本工具下载二进制并修改系统 PATH/注册表，属安全敏感项目：

- 所有 JDK 下载必须**完整性校验**（SHA256 或 SHA1，按发行版官方提供），损坏/篡改即报错中止（见 `internal/jdk` 的 `fileHash`）。Liberica 官方仅提供 sha1，故用 SHA1；其余发行版用 SHA256。
- junction 创建走原生 syscall，**无 shell 子进程**，杜绝命令注入。
- 自更新（`jvm upgrade`）从 GitHub Release 拉取，靠精确 asset 名匹配（`expectedAssetName`），替换自身前校验。
- 注册表只写 `HKCU`（用户级），无需管理员；不写 `HKLM`。
- 无密钥/凭证处理；网络仅连 Adoptium API、corretto-downloads 仓库、aka.ms/VisualStudio CDN、Azul Metadata API/cdn.azul.com、BellSoft API/download.bell-sw.com、清华镜像、GitHub API/CDN。

## Known gotchas

- **测试覆盖**：纯函数表驱动单测（`app` / `asset` / `junction` / `provider` 及各 provider 子包 / `cmd` / `config` / `doctor` / `jdk` / `shell` / `upgrade` / `updatecheck` / `pinrc` 包），覆盖版本解析、distro@ 语法、URL/版本号提取、路径转换、注册表、profile 块移除、下载重试/断点续传（httptest 模拟）、更新检查节流、doctor 各项诊断（临时目录 + 注入值隔离）、`exec` 的 env 构建与批处理分发（临时目录实跑）、自动切换决策 `decideAuto`/state 文件 IO、集成/补全块版本化重写 `ensureBlockVersioned`、`update` 的参数解析与升级计划 `parseUpdateArgs`/`planUpdate`、`filterUserPath`/`ResidueEntries`/`junctionFixPlan` 等逻辑；junction 创建/删除有 Windows syscall 单测（`junction_windows_test.go`，Create→Remove 往返 + 错误路径），`cmd` 的 `availableTable`/`availableGroups`/`Outdated()`/`Home()`（临时版本目录 + 真实 junction / 可控 fake provider 全流程）并发有 mock 单测（`-race` 验证）。写注册表/junction 的编排路径（`Use`/`UseAuto` 切换、`Update` 升级编排、`doctor --fix` 修复动作）不设单测以免污染开发机，由集成脚本覆盖（`use --auto` 状态机、`doctor --fix` junction 重建、`update` 闭环已在 `scripts/integration-test.sh`）。仍无自动化测试、靠集成测试或人工验证的：provider 适配器对真实 API 的解析、`jvm upgrade` 真实下载替换自身、shell 自动切换钩子的真机行为（开发时用 stub 方式人工验证过 PS/bash 各触发点，无自动化）。
- **CMD（cmd.exe）不支持** shell 自动集成与 `.jvmrc` 自动切换（doskey 体验差），仅 PowerShell 与 bash；CMD 用户需重开窗口，或用 `jvm exec`/`jvm use` 显式操作。
- `make installer` 依赖 NSIS；release workflow 用 `choco install nsis` 后需显式把 `C:\Program Files (x86)\NSIS` 写入 `GITHUB_PATH`（choco 不自动刷新 job PATH）。
- `jvm` 启动自举（把自身目录加入用户 PATH + 补全 shell 集成，幂等）**默认仅发行构建执行**。三层关闭机制（见 `internal/app/bootstrap.go` 的 `BootstrapEnabled`）：`make build`/`make run` 的开发产物经 ldflags 注入 `Bootstrap=off`；`JVM_NO_BOOTSTRAP` 环境变量非空；自身 exe 位于系统 Temp 目录（覆盖 `go run` 的 `Temp\go-build*\b001\exe` 临时二进制）。`make build-dist`/`installer`/`release` 产物与直接 `go build` 源码的二进制保持自举。
  - 开发调试放心跑 `./dist/<arch>/jvm.exe ...`，不会污染全局 `jvm`。
  - 历史污染清理：编辑 `$PROFILE` / `~/.bashrc` 删除指向开发路径的 `jvm` wrapper；检查注册表 `HKCU\Environment\PATH` 残留的开发/Temp 目录条目，清理后广播 `WM_SETTINGCHANGE`（或重新登录）。
- 构建产物输出到 `dist/`（已在 `.gitignore`）；不要把二进制提交进仓库。
- **版本目录命名无 arch 维度**：`~/.jvm/versions/` 目录名是 `{distro}-{ReleaseName}`（如 `temurin-21.0.5+11`），不含目标架构。同一台机器上先 `arch=x64` 再 `arch=aarch64` 装同一发行版同一版本会撞名（相同版本号 → 判定"已安装"跳过；不同 patch → 两个目录但 `use` 只匹配其一）。属已知限制：跨架构并存是极边缘场景，而目录名加 arch 会连锁影响 junction 匹配 / `MigrateLegacyDirs` / doctor，故不做。
