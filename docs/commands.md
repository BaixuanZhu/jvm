---
title: 命令参考
description: jvm 全部子命令与用法示例。
---

## 版本管理

```powershell
jvm install 21              # 安装 temurin JDK 21 最新版（默认发行版）
jvm install corretto@21     # 安装 corretto JDK 21 最新版
jvm install microsoft@21    # 安装 microsoft JDK 21 最新版
jvm install zulu@21         # 安装 zulu JDK 21 最新版
jvm install liberica@21     # 安装 liberica JDK 21 最新版
jvm install graalvm@21      # 安装 Oracle GraalVM 21（CPU LTS 线）
jvm install temurin-ea@28   # 安装 temurin 28 早期访问版（GA 前预览）
jvm install 21.0.12+8       # 安装 temurin 精确版本（完整版本号，含 build 号）
jvm install temurin@21.0.5+11 D:\jdk.zip   # 从本地 zip 安装（内网/手动下载，零网络）
jvm use 21                  # 切换到 21（大版本号取最新 patch）
jvm use corretto@21         # 切换到 corretto 21
jvm uninstall 21            # 卸载（默认需确认，加 -y 跳过）
```

本地 zip 安装不访问网络、不做远程校验和校验（本地文件由你负责），安装后的目录命名与远程安装完全一致，`use` / `uninstall` / `outdated` 等命令照常工作。

## 升级

```powershell
jvm outdated                # 检查已装版本的 patch 更新，列出可升级行
jvm update 21               # 升级 temurin 21：装最新 patch → 条件切换 → 清理旧版
jvm update corretto@21 -y   # 升级指定发行版的大版本，跳过确认
jvm update --all            # 一条命令升级全部落后的 (发行版, 大版本) 组
```

`update` 只接受大版本号（patch 全自动）：正在使用该组版本时自动切换到新版；被进程占用删不掉的旧目录会跳过并提示稍后手动 `uninstall`。`update --all` 与 `outdated` 同源检查，汇总各组计划后**一次确认**，某组失败不阻断其余组。`jvm upgrade` 是另一回事——它只更新 jvm 自身。

## 项目版本固定（.jvmrc）

在项目根目录放一个 `.jvmrc`（内容一行版本号，如 `21` 或 `corretto@21`），团队成员无需各自记版本号：

```powershell
jvm pin 21                  # 把版本号写入当前目录 .jvmrc
jvm pin corretto@21
jvm use                     # 无参数：从当前目录逐级向上找 .jvmrc 并切换
```

配合 shell 集成 v2，cd 进含 `.jvmrc` 的目录会**自动切换**，cd 出去恢复原版本（详见[配置与原理]({{ '/docs/config/' | relative_url }})）。不支持自动切换的场景（如 CMD）用无参数 `jvm use` 即可。

## 一次性执行（不动全局）

```powershell
jvm exec 17 -- mvn test         # 用 JDK 17 跑测试，不影响全局 current 版本
jvm exec corretto@21 -- java -version
jvm exec -- mvn test            # 无版本号：读 .jvmrc，再退到当前版本
```

只解析本地已装版本（未装会提示 `jvm install`）；`java`/`javac` 优先在该版本 bin 内解析；mvn / gradlew 等批处理自动经 `cmd.exe` 分发；子进程退出码原样传播（`$LASTEXITCODE` / `$?` 可用）。

## 查询与诊断

```powershell
jvm list                    # 已安装版本（→ 标记当前）
jvm available               # 可安装的大版本（标记 LTS；结果缓存 10 分钟）
jvm available corretto      # 查看 corretto 可安装版本
jvm available -a            # 列出每个大版本的全部子版本
jvm available --major 21    # 只看 JDK 21 的全部子版本
jvm available --refresh     # 强制绕过缓存直查（-r 同效）
jvm current                 # 当前版本（会实际执行 java -version）
jvm home                    # 打印当前 JAVA_HOME 路径（单行，供 CI/IDE 引用）
jvm outdated                # 检查已装版本 patch 更新，提示升级命令
jvm doctor                  # 诊断环境配置（14 项检查，附修复建议）
jvm doctor --fix            # 自动修复失败项（残留清理逐条确认，-y 跳过）
jvm cache                   # 查看下载缓存条目与磁盘占用
jvm cache clean             # 清空下载缓存（含中断残留的 .part 分片）
```

`available` 的查询结果按条目本地缓存 10 分钟（`~/.jvm/available-cache.json`），二次查询不再实时打全部发行版 API；命中时输出会标注 `⚡ 缓存结果`。目标架构（`arch` 配置）变化时缓存自动整体失效。

## Shell 集成

首次运行已自动安装，以下命令供手动管理 / 诊断：

```powershell
jvm init powershell             # 打印 PowerShell 集成脚本
jvm init powershell --install   # 自动写入 $PROFILE
jvm init bash --install         # 自动写入 ~/.bashrc
jvm completion powershell --install   # 重装 Tab 补全
```

## 其他

```powershell
jvm upgrade                 # 检查并更新 jvm 自身
jvm version                 # 显示 jvm 版本号
jvm help                    # 帮助
```

## Tab 补全

PowerShell 5.1/7+ 与 Git Bash 均支持智能补全：`jvm <TAB>` 补子命令、`jvm use <TAB>` 补本地已装版本、`jvm install <TAB>` 补发行版前缀（本地 zip 安装时路径槽让给 shell 默认文件补全）、`jvm exec <TAB>` 补已装版本（`--` 之后让位给要执行的命令）、`jvm update <TAB>` 补 `distro@大版本`、`jvm available <TAB>` 补 `-a` / `-m` / `-r` 选项、`jvm doctor <TAB>` 补 `--fix` / `-y`。静默自举注入，零配置；补全内容有版本 token，升级 jvm 后自动刷新。
