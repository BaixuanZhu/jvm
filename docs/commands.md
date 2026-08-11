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
jvm install 21.0.12+8       # 安装 temurin 精确版本（完整版本号，含 build 号）
jvm use 21                  # 切换到 21（大版本号取最新 patch）
jvm use corretto@21         # 切换到 corretto 21
jvm uninstall 21            # 卸载（默认需确认，加 -y 跳过）
```

## 查询

```powershell
jvm list                    # 已安装版本（→ 标记当前）
jvm available               # 可安装的大版本（标记 LTS）
jvm available corretto      # 查看 corretto 可安装版本
jvm available -a            # 列出每个大版本的全部子版本
jvm available --major 21    # 只看 JDK 21 的全部子版本
jvm current                 # 当前版本（会实际执行 java -version）
jvm doctor                  # 诊断环境配置（9 项检查，附修复建议）
```

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

PowerShell 5.1/7+ 与 Git Bash 均支持智能补全：`jvm <TAB>` 补子命令、`jvm use <TAB>` 补本地已装版本、`jvm install <TAB>` 补发行版前缀。静默自举注入，零配置。
