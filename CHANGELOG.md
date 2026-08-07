# Changelog

本项目所有重要变更均会记录在此文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

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

[Unreleased]: https://github.com/BaixuanZhu/jvm/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/BaixuanZhu/jvm/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/BaixuanZhu/jvm/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/BaixuanZhu/jvm/compare/v0.1.4...v0.2.0
[0.1.4]: https://github.com/BaixuanZhu/jvm/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/BaixuanZhu/jvm/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/BaixuanZhu/jvm/compare/v0.1.0...v0.1.2
[0.1.0]: https://github.com/BaixuanZhu/jvm/releases/tag/v0.1.0
