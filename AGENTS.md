# AGENTS.md

## Project overview

`jvm` 是 Windows 上的 Java 版本管理器（类似 nvm-windows / jabba）。Go 编写的单二进制 CLI；通过 Windows junction 切换 JDK 版本，免管理员权限，自动配置 PATH/JAVA_HOME 与 shell 集成。支持多发行版（Temurin / Corretto / Microsoft Build of OpenJDK），CLI 用 `[distro@]version` 语法选择发行版（省略前缀默认 temurin）。**仅支持 Windows（x64 / ARM64）**。

## Setup commands

需 Go 1.26+ 与 GNU Make（Git Bash / MinGW / WSL 自带）。

- Build: `make build` → `dist/<arch>/jvm.exe`（`-trimpath -ldflags "-s -w"`；`GOARCH` 默认取本机 `go env GOARCH`，交叉编 ARM64 用 `make build GOARCH=arm64`）
- Run: `make run ARGS="version"`（先 build 再运行）
- Format / 静态检查: `make fmt` / `make vet`（即 `go fmt ./...` / `go vet ./...`）
- Deps: `make tidy`
- 安装包: `make installer` → `dist/jvm-windows-<arch>-setup.exe`（需 NSIS：`scoop install nsis` 或 `choco install nsis`）
- 便携 zip: `make release` → `dist/jvm-windows-<arch>.zip`（供 `jvm upgrade` 拉取）
- 全部发行资产: `make dist-all`（仅当前 `GOARCH`；双架构由 CI 分别传 `GOARCH=amd64/arm64` 各跑一遍）
- Clean: `make clean`

发布：打 tag（`git tag v0.1.0 && git push --tags`）触发 `.github/workflows/release.yml`，在 windows-latest 自动编译（amd64 + arm64 双架构）、打安装器 + 便携 zip 并发 GitHub Release（四 asset + checksums.txt）。ARM64 安装器 stub 为 x86，在 ARM64 Windows 上靠系统内置模拟运行，释放出的 jvm.exe 是 ARM64 原生。

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

命令路由在 `main.go`（纯 switch，无框架），各子命令实现在 `cmd` 包（编排层：解析参数 → 调底层包 → 输出），基础设施按职责拆在 `internal/`：

- `internal/provider/` — **Provider 抽象层**：核心接口（`Name`/`DisplayName`/`Available`/`Resolve`/`LatestPatch`/`ListVersions`）+ `Base` 基类（默认实现）+ 注册表（`Register`/`Get`/`All`）+ **`Configurable` 可选接口与 `ConfigureAll(arch, mirror)` 分发**（provider 实现 `Configurable` 即在启动时自动接收全局目标架构/镜像，main.go 无需改动；temurin 两者都用，corretto/microsoft 只用 arch）。各发行版适配器嵌入 `Base` 按需 override。新公共 provider 逻辑优先放这里或 `internal/app/`，而非塞进某个业务包。
- `internal/provider/temurin/` — Temurin (Adoptium) 适配器：Adoptium API 查大版本/最新 GA/精确版本，解析官方 CDN 直链与清华镜像 URL（轻量 JSON 走官方 API，大文件下载走镜像）。支持 x64 / aarch64；**注意上游 Windows ARM64 覆盖不全**（如 21 有、17/25 暂无，Adoptium API 对缺失组合返回 404），查询错误消息统一带 `(windows/{arch})` 上下文便于定位。
- `internal/provider/corretto/` — Amazon Corretto 适配器：corretto-downloads 仓库的 indexmap JSON（含 SHA256，按架构 map 化索引），直连 CloudFront CDN（无镜像）。**官方无 Windows ARM64 构建**：`arch=aarch64` 时各入口统一报 `errNoWindowsARM64` 并建议改用 temurin/microsoft。
- `internal/provider/microsoft/` — Microsoft Build of OpenJDK 适配器：aka.ms 短链重定向探测 + `.sha256sum.txt` 旁路校验，仅 LTS（11/17/21/25），直连 VisualStudio CDN（无镜像）。x64 / aarch64 均支持（短链文件名后缀参数化）。
- `internal/jdk/` — 下载、SHA256 校验、解压、原子替换到 `~/.jvm/versions/`（发行版无关，只认 `*app.Asset`）。
- `internal/junction/` — Windows junction 创建/删除/解析，用**原生 syscall**（`FSCTL_SET_REPARSE_POINT`），不调 `cmd.exe`（避免注入面）。
- `internal/env/` — 注册表读写（`HKCU\Environment`）+ 广播 `WM_SETTINGCHANGE`，持久化 `JAVA_HOME`/PATH/`jvm` 自身 PATH。
- `internal/shell/` — PowerShell/bash 集成脚本生成 + 写入 `$PROFILE`/`~/.bashrc`（幂等，缺失才补）；profile 路径走 `KnownFolderPath`（`FOLDERID_Documents`/`FOLDERID_Profile`），兼容 Documents 目录重定向。
- `internal/upgrade/` — 走 GitHub Release 自更新。
- `internal/updatecheck/` — 启动时静默检查 GitHub 新版本（24h 节流，落后才提示，失败永不阻断）；只依赖 `app`，不依赖 `upgrade`（检查提示与执行升级职责分离）。
- `internal/doctor/` — `jvm doctor` 环境诊断：目录结构 / junction / JAVA_HOME / PATH 冲突 / shell 集成 / current 的 java / java 版本（实跑 `java -version`）/ 版本目录完整性 / 注册表 PATH 残留 共 9 项，每项输出 ✓/✗ 并附修复建议。检查函数收显式参数、不读全局，便于表驱动测试。
- `internal/config/` — 用户配置加载（`~/.jvm/config.toml`）：当前覆盖 `mirror`（下载镜像，默认清华 TUNA，仅 temurin 消费）与 `arch`（目标架构，默认跟随 `runtime.GOARCH`：amd64 版 → `x64`，arm64 版 → `aarch64`；三个 provider 均消费 arch）。优先级：环境变量（`JVM_MIRROR`/`JVM_ARCH`）> 配置文件 > 默认值；配置文件缺失视为正常，解析失败警告后回退默认。
- `internal/paths/` — `~/.jvm` 下目录路径常量（`init()` 里基于 `os.UserHomeDir()` 计算）。
- `internal/app/` — **共享基础设施层**（版本号、`Fail`、版本解析、统一 HTTP client、`CompareVersions` 版本比较、`LatestGitHubTag` release 查询、`NormArch` 架构规范化（`x64`/`aarch64` 规范值 + `amd64`/`arm64` 别名）、`Asset`/`Release`/`VersionSpec` 跨 provider 下载契约）；存在目的是被几乎所有业务包依赖以**避免循环依赖**——新公共逻辑优先放这里，而非塞进某个业务包。

运行模型：`~/.jvm/current` 是指向当前选中版本的 junction；PATH 永远指向 `~/.jvm/current/bin`，切换版本 = 重建 junction，故新终端无需刷新环境变量即生效。当前终端即时生效靠 shell 集成函数在 `jvm use` 后于会话内刷新 `JAVA_HOME`/PATH（子进程改不了父 shell 环境，靠 wrapper 函数绕过）。

## Hard constraints

- **仅 Windows（x64 / ARM64）**：大量依赖 junction、注册表、Windows syscall，不可移植到 *nix。jvm 自身发行 amd64 / arm64 双架构产物；ARM64 JDK 下载经 `arch = "aarch64"` 配置支持（arm64 版二进制默认即 aarch64，见 `internal/config`）。
- **Go 1.26+**（见 `go.mod`）。
- **版本号通过 ldflags 单一注入**：`internal/app/app.go` 的 `Version` 是 `var`（默认值 `0.1.0-dev`），构建时由 Makefile 的 `-X jvm/internal/app.Version=$(VERSION)` 覆盖（`VERSION` 取自 `git describe`）；CI 从 tag 推导并传给 `make build`。**禁止**在源码里手写版本字面量、禁止让 `Makefile` 与 `app.Version` 各维护一份。
- 自更新仓库地址硬编码在 `internal/upgrade/upgrade.go` 的 `githubRepo` 常量，fork 后需改。
- junction 必须用原生 syscall 实现，**禁止**改为 `cmd /c mklink` 等子进程方式（引入命令注入面）。
- PATH 持久化必须走注册表 + `WM_SETTINGCHANGE`，**禁止**用 `setx`（会截断长 PATH）。

## Security considerations

本工具下载二进制并修改系统 PATH/注册表，属安全敏感项目：

- 所有 JDK 下载必须 **SHA256 校验**，损坏/篡改即报错中止（见 `internal/jdk` 的 `fileSHA256`）。
- junction 创建走原生 syscall，**无 shell 子进程**，杜绝命令注入。
- 自更新（`jvm upgrade`）从 GitHub Release 拉取，靠精确 asset 名匹配（`expectedAssetName`），替换自身前校验。
- 注册表只写 `HKCU`（用户级），无需管理员；不写 `HKLM`。
- 无密钥/凭证处理；网络仅连 Adoptium API、corretto-downloads 仓库、aka.ms/VisualStudio CDN、清华镜像、GitHub API/CDN。

## Known gotchas

- **测试覆盖**：纯函数表驱动单测（`app` / `asset` / `junction` / `provider` 及各 provider 子包 / `cmd` / `config` / `doctor` / `jdk` / `shell` / `upgrade` / `updatecheck` 包），覆盖版本解析、distro@ 语法、URL/版本号提取、路径转换、注册表、profile 块移除、下载重试/断点续传（httptest 模拟）、更新检查节流、doctor 各项诊断（临时目录 + 注入值隔离）等逻辑；junction 创建/真实网络请求等 Windows 耦合路径暂无单测，改动时注意人工验证。
- **CMD（cmd.exe）不支持** shell 自动集成（doskey 体验差），仅 PowerShell 与 bash；CMD 用户需重开窗口。
- `make installer` 依赖 NSIS；release workflow 用 `choco install nsis` 后需显式把 `C:\Program Files (x86)\NSIS` 写入 `GITHUB_PATH`（choco 不自动刷新 job PATH）。
- `jvm` 每次启动**静默自举**：把自身目录加入用户 PATH 并补全 shell 集成（幂等）——调试时留意首次运行的副作用。
- 构建产物输出到 `dist/`（已在 `.gitignore`）；不要把二进制提交进仓库。
- **版本目录命名无 arch 维度**：`~/.jvm/versions/` 目录名是 `{distro}-{ReleaseName}`（如 `temurin-21.0.5+11`），不含目标架构。同一台机器上先 `arch=x64` 再 `arch=aarch64` 装同一发行版同一版本会撞名（相同版本号 → 判定"已安装"跳过；不同 patch → 两个目录但 `use` 只匹配其一）。属已知限制：跨架构并存是极边缘场景，而目录名加 arch 会连锁影响 junction 匹配 / `MigrateLegacyDirs` / doctor，故不做。
