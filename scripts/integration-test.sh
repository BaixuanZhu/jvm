#!/usr/bin/env bash
# jvm 端到端集成测试: 跑真实 jvm.exe 验证 install/use/list/doctor/uninstall 等核心命令。
#
# 隔离机制 (CI / 本地都靠这两条, 不污染系统):
#   JVM_NO_BOOTSTRAP=1   关闭自举, 不写 shell profile / 用户 PATH
#   JVM_HOME=<临时目录>  把 ~/.jvm 重定向到临时目录, install 的 versions/current 全部隔离
#
# ⚠️ 注意: `jvm use` 仍会写 HKCU\Environment 注册表 (JAVA_HOME + 用户 PATH), 这与
#   paths.Root 无关、无法靠 JVM_HOME 重定向。doctor --fix 残留清理用例也会临时向
#   用户 PATH 追加一条假旧 JDK 再由 --fix 删除 (净效果为零)。CI runner 一次性,
#   污染可接受; 本地手动跑本脚本前请知悉 (会改本机注册表 JAVA_HOME / 用户 PATH)。
#
# 用法:
#   export JVM_HOME="$(mktemp -d)"   # 强烈建议, 隔离 install 副作用
#   export JVM_NO_BOOTSTRAP=1
#   make build
#   bash scripts/integration-test.sh
set -euo pipefail

ARCH="$(go env GOARCH)"
# 绝对路径: 后续 use --auto 等用例要 cd 进临时目录再执行, 相对路径会失效 (CI 实测踩过)
JVM="$(pwd)/dist/${ARCH}/jvm.exe"

if [ ! -f "$JVM" ]; then
    echo "✗ 找不到 $JVM, 请先 make build" >&2
    exit 1
fi

if [ -z "${JVM_HOME:-}" ]; then
    echo "⚠️  JVM_HOME 未设置, install 副作用会落到真实 ~/.jvm!" >&2
    echo "    建议: export JVM_HOME=\$(mktemp -d) 后再跑本脚本" >&2
fi

echo "## 集成测试 (jvm: $JVM, arch: $ARCH, JVM_HOME: ${JVM_HOME:-<默认 ~/.jvm>}) ##"
echo ""

LAST_OUT=""

# run <描述> <jvm args...>: 跑 jvm 命令, 输出存 LAST_OUT; 非 0 退出则打印输出并失败。
run() {
    local desc="$1"; shift
    echo "### $desc ###"
    if LAST_OUT="$("$JVM" "$@" 2>&1)"; then
        return 0
    fi
    local rc=$?
    echo "  ✗ 命令退出 $rc" >&2
    printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
    exit 1
}

# run_in <目录> <描述> <jvm args...>: run 的 cd 版 —— .jvmrc 相关用例须在特定目录执行。
run_in() {
    local dir="$1" desc="$2"; shift 2
    echo "### $desc ###"
    local rc=0
    LAST_OUT="$(cd "$dir" && "$JVM" "$@" 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ]; then
        echo "  ✗ 命令退出 $rc" >&2
        printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
        exit 1
    fi
}

# expect <描述> <期望子串>: 断言 LAST_OUT 含期望子串。
expect() {
	local desc="$1" needle="$2"
	if printf '%s' "$LAST_OUT" | grep -q -- "$needle"; then
		echo "  ✓ $desc"
	else
		echo "  ✗ $desc" >&2
		echo "    期望输出含: $needle" >&2
		printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
		exit 1
	fi
}

# expect_silent <描述>: 断言 LAST_OUT 为空 (use --auto 的 no-op 路径必须零输出,
# 否则每次开终端 prompt 前都会出现垃圾行)。
expect_silent() {
    local desc="$1"
    if [ -z "$LAST_OUT" ]; then
        echo "  ✓ $desc"
    else
        echo "  ✗ $desc (期望无输出)" >&2
        printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
        exit 1
    fi
}

# run_fail <描述> <jvm args...>: 跑 jvm 命令, 期望它失败 (退出非 0); 成功则测试失败。
# 失败输出仍存 LAST_OUT, 可用 expect 断言错误消息内容。供负向用例 (未知 distro 等)。
run_fail() {
    local desc="$1"; shift
    echo "### $desc ###"
    local rc=0
    LAST_OUT="$("$JVM" "$@" 2>&1)" || rc=$?
    if [ "$rc" -eq 0 ]; then
        echo "  ✗ 命令预期失败但成功了 (退出 0)" >&2
        printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
        exit 1
    fi
    echo "  ✓ 如期失败 (退出 $rc)"
}

run "1. version" version
# 版本号两种合法形态: git describe 语义版 (0.11.1-3-ge1da48e) 或无 tag 回退的
# 短哈希 (e1da48e), 首字符均落在 [0-9a-f]; CI 已拉全量+tag, 本地无 tag 也能过。
expect "version 输出版本号" "^jvm [0-9a-f]"

run "2. install temurin@21 (SHA256)" install temurin@21
expect "temurin 安装完成" "安装完成"

run "3. list" list
expect "list 含 temurin-21" "temurin-21"

run "4. use temurin@21 (junction + 注册表)" use temurin@21
expect "use 切换成功" "已切换"

run "5. current + java -version 实跑" current
expect "current 显示当前版本" "当前版本"
expect "java -version 实跑成功" "version"

# home 输出 current 链接路径 (单行, 与注册表 JAVA_HOME 同值)
run "5a. home: 输出 JAVA_HOME 路径" home
expect "home 指向 current" "current"

# 覆盖全部 5 个 provider 的下载 + 校验路径:
#   corretto/microsoft/zulu 走 SHA256, liberica 走 SHA1 (多算法校验真实用例)。
run "6a. install corretto@21 (SHA256, CloudFront)" install corretto@21
expect "corretto 安装完成" "安装完成"
run "6b. install microsoft@21 (SHA256, aka.ms 旁路)" install microsoft@21
expect "microsoft 安装完成" "安装完成"
run "6c. install zulu@21 (SHA256, Azul 详情端点)" install zulu@21
expect "zulu 安装完成" "安装完成"
run "6d. install liberica@21 (SHA1, BellSoft)" install liberica@21
expect "liberica 安装完成" "安装完成"

# doctor 软检查: 关自举后 shell 集成/补全两项必 ✗ (预期内), 只验版本目录完整性。
run "7. doctor (软检查)" doctor
expect "doctor 版本目录完整性通过" "版本目录完整性"

# --- jvm exec: 指定版本一次性执行, 不动全局 ---
# java 在版本 bin 内解析 (lookPathIn 优先版本 bin, 不依赖系统 PATH)
run "8. exec: 指定版本执行 java -version" exec temurin@21 -- java -version
expect "exec 输出 java 版本" "version"

# 退出码传播: 子进程退出 3 应原样传给调用方 (脚本按 $? 判断的前提)。
# 注意 //c 是 MSYS 转义: 单个 /c 会被 Git Bash 转成 "C:\" 传给原生 exe,
# cmd 就进了交互模式 (本地实测翻车过), CI 的 bash 同是 Git Bash。
echo "### 9. exec: 子进程退出码原样传播 ###"
rc=0
LAST_OUT="$("$JVM" exec temurin@21 -- cmd //c exit 3 2>&1)" || rc=$?
if [ "$rc" -ne 3 ]; then
    echo "  ✗ 预期退出码 3, 实际 $rc" >&2
    printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
    exit 1
fi
echo "  ✓ 退出码 3 如期传播"

# 批处理分发: .cmd 必须经 cmd.exe /c (Windows CreateProcess 不能直接跑批处理,
# mvn/gradlew 全是 .cmd) —— 真机才能验证的路径。放在 JVM_HOME 下避免 MSYS 路径转换。
printf '@echo off\r\necho BATCH-OK\r\n' > "$JVM_HOME/test-dispatch.cmd"
run "10. exec: 批处理分发 (.cmd 经 cmd.exe /c)" exec temurin@21 -- "$JVM_HOME/test-dispatch.cmd"
expect "批处理输出 BATCH-OK" "BATCH-OK"
rm -f "$JVM_HOME/test-dispatch.cmd"

# pin + exec 无版本参数: .jvmrc 写入与 "读 .jvmrc → 兜底 current" 的解析链路
# (execSpecFromContext 真实 pinrc.FindUp + ResolveVersion, 单测覆盖不到的编排)。
# 用 pin 建 rc; 当前 current 是 temurin (步骤 4), 若 exec 误用了 current 而非 rc,
# 输出会含 temurin 而非 corretto —— 用 %JAVA_HOME% 回显甄别 (env 只注入给子进程)。
PROJ_EXEC="$(mktemp -d)"
run_in "$PROJ_EXEC" "11. pin: 写入 .jvmrc" pin corretto@21
expect "pin 已写入" "已写入"
run_in "$PROJ_EXEC" "12. exec: 无版本参数读 .jvmrc" exec -- cmd //c echo %JAVA_HOME%
expect "exec 解析到 .jvmrc 的 corretto 版本" "corretto"

run_fail "13. exec: 未安装版本 fail-fast" exec zulu@99 -- java -version
expect "提示先安装" "先安装"

# --- available / outdated: 真实 API 查询 ---
# available 打各 provider 的 Available 端点 (install 只覆盖 Resolve, outdated 只覆盖
# LatestPatch, 这个端点各发行版独立), 与 outdated 一样只断言稳定表头防上游抖动。
run "14. available: 查询可安装大版本 (真实 API)" available
expect "available 打印大版本表" "可安装的大版本"

# CI 刚装的版本可能就是最新 patch, "可升级/均为最新"两种输出都合法, 只断言必然
# 出现的表头与退出 0, 不断言具体分支 (防上游发新版导致用例抖动)。
run "15. outdated: patch 升级检查" outdated
expect "outdated 打印查询表头" "最新 patch"

# --- jvm update: patch 升级闭环 (装新 → 切换 → 清理旧版) ---
# 步骤 2 已装 21 的最新 patch, 这里补装一个旧 patch 并切过去, 再 update 回来。
# 覆盖 "最新版已装 → Install 幂等跳过下载, 直接切换 + 删旧" 的编排路径
# (全新下载链路由步骤 2 的 install 覆盖, 免去重复下载 ~200MB)。
# 21.0.2+13 是 Adoptium 历史 GA, release_name 端点长期可查。
run "15a. install temurin@21.0.2+13 (旧 patch, 供 update 升级)" install temurin@21.0.2+13
expect "旧 patch 安装完成" "安装完成"

run "15b. use 切到旧 patch" use temurin@21.0.2+13
expect "已切到旧 patch" "已切换"

run "15c. update 21 -y: 最新版已装 → 跳过下载, 切换并清理旧版" update 21 -y
expect "update 跳过下载" "跳过下载"
expect "update 清理旧版本" "清理了"

run "15d. current 验证已切到新版" current
expect "current 为 temurin" "temurin"

echo "### 15e. 旧 patch 目录应已被 update 清理 ###"
if [ -d "$JVM_HOME/versions/temurin-21.0.2+13" ]; then
    echo "  ✗ temurin-21.0.2+13 仍存在, update 未清理旧目录" >&2
    exit 1
fi
echo "  ✓ 旧目录已清理"

run "15f. update 幂等: 已是最新" update 21 -y
expect "提示已是最新" "已是最新"

# --- 下载缓存: 卸载重装免重新下载 ---
# 15a 装过的 temurin-21.0.2+13 已被 update 清掉, 但缓存 zip 仍留存:
# 重装同版本应命中缓存 (零下载), 再验证 cache 列表与 clean 闭环。
run "15g. install temurin@21.0.2+13 重装 (命中缓存)" install temurin@21.0.2+13
expect "提示命中下载缓存" "命中下载缓存"

run "15h. cache 列表含旧 patch zip" cache
expect "列表非空" "temurin-21.0.2+13.zip"

run "15i. uninstall 临时版本" uninstall temurin@21.0.2+13 -y
expect "已卸载" "已卸载"

run "15j. cache clean 清空" cache clean
expect "clean 释放提示" "已清理"

run "15k. cache 列表为空" cache
expect "空缓存提示" "空。"

# --- jvm use --auto: .jvmrc 自动切换状态机 ---
# 前置: current 为 temurin@21 (步骤 4 切的, 后续 install 不切)。
# 注意 NO_RC 须选一条向上都无 .jvmrc 的链 (runner 的 mktemp 目录满足)。
PROJ_RC="$(mktemp -d)"
NO_RC="$(mktemp -d)"
run_in "$PROJ_RC" "16. pin: 写入 .jvmrc" pin corretto@21
expect "pin 已写入" "已写入"

run_in "$PROJ_RC" "17. use --auto: cd 进 .jvmrc 目录自动切换" use --auto
expect "自动切到 corretto" "切换到 corretto"

run "18. current 验证自动切换结果" current
expect "current 为 corretto" "corretto"

run_in "$NO_RC" "19. use --auto: 离开 .jvmrc 目录恢复手动基线" use --auto
expect "恢复到 temurin" "恢复 temurin"

run "20. current 验证恢复" current
expect "current 回到 temurin" "temurin"

# 未安装版本: 一行警告但不失败 (exit 0), 不刷屏语义由调用方保证
PROJ_BAD="$(mktemp -d)"
echo "zulu@99" > "$PROJ_BAD/.jvmrc"
run_in "$PROJ_BAD" "21. use --auto: .jvmrc 指向未装版本 (警告不失败)" use --auto
expect "如期提示未安装" "未安装"

# 禁用开关端到端: JVM_AUTOSWITCH=0 → config → use --auto 静默不切。
# 此刻 current=temurin 且无待恢复状态, 禁用路径若有任何输出/切换都会被下面两条抓到。
echo "### 22. use --auto: JVM_AUTOSWITCH=0 禁用 (静默不切换) ###"
rc=0
LAST_OUT="$(cd "$PROJ_RC" && JVM_AUTOSWITCH=0 "$JVM" use --auto 2>&1)" || rc=$?
if [ "$rc" -ne 0 ]; then
    echo "  ✗ 禁用路径不应失败 (退出 $rc)" >&2
    printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
    exit 1
fi
expect_silent "禁用时零输出"

run "23. current 验证未被切换" current
expect "current 仍为 temurin" "temurin"

# 显式 use = 新手动基线: 先重新进入 rc 目录产生待恢复状态, 再显式裸 use (读 .jvmrc,
# 覆盖无参 use 的 pinrc 链路) —— 成功后必须清掉 auto-state, 之后离开 rc 目录应静默。
run_in "$PROJ_RC" "24. use --auto: 重新进入重建待恢复状态" use --auto
expect "自动切到 corretto" "切换到 corretto"

run_in "$PROJ_RC" "25. use 无参: 读 .jvmrc 切换 (显式 = 新手动基线)" use
expect "读取 .jvmrc" "读取"
expect "已切换到 corretto" "已切换"

run_in "$NO_RC" "26. use --auto: 显式切换后离开, 无基线可恢复 (静默)" use --auto
expect_silent "无输出 (state 已被显式 use 清掉)"

run "27. current 验证未被误恢复" current
expect "current 仍为 corretto" "corretto"

# --- doctor --fix: junction 重建闭环 ---
# 用 cmd /c rmdir 删 junction (只删链接本身); 不能用 rm —— MSYS rm 会穿透
# junction 递归删除目标内容, 那是真实 JDK 目录。cygpath 归一成纯反斜杠路径,
# 避免 runner.temp 混合斜杠被 cmd 误解析。
echo "### 28. 删除 current junction, doctor 应报 ✗ ###"
cmd //c rmdir "$(cygpath -w "$JVM_HOME")\\current"
run "doctor 报告 current 链接异常" doctor
expect "current 链接 ✗" "尚未选定"

# --fix -y 会顺带补写 runner 的 shell profile (集成项原本关自举不写, 这里是有意
# 验证修复动作) 与注册表 JAVA_HOME/用户 PATH —— runner 一次性, 可接受。
run "29. doctor --fix -y 自动重建 junction" doctor --fix -y
expect "junction 已重建" "已重建"

run "30. current 重建后可用" current
expect "current 显示当前版本" "当前版本"

# --- doctor --fix: 注册表 PATH 残留清理 ---
# 造一个含 java.exe 的假旧 JDK, 把它的 bin 目录追加进用户 PATH (HKCU), 验证三件事:
#   1) doctor 检出残留;  2) --fix -y 删除;  3) 其余条目 (current/bin) 不被误删。
# 注意注入的必须是 bin 目录 —— ResidueEntries 判定残留的标准是 "PATH 条目内直接
# 存在 java.exe" (PATH 条目本就是 bin 目录), 注入 JDK 根目录会 stat 不到而不算残留
# (本地实测踩过)。
# 写注册表用 PowerShell (Get/Set 'User' 目标); 注意 .NET 会把 REG_EXPAND_SZ 展开
# 后以 REG_SZ 写回 —— runner 一次性环境可接受。假目录放 JVM_HOME 下随临时目录清理。
FAKE_JDK="$JVM_HOME/fake-old-jdk"
mkdir -p "$FAKE_JDK/bin"
touch "$FAKE_JDK/bin/java.exe"
FAKE_WIN="$(cygpath -w "$FAKE_JDK/bin")"
echo "### 31. 向用户 PATH 注入假旧 JDK, doctor 应报残留 ###"
powershell -NoProfile -Command "\$old=[Environment]::GetEnvironmentVariable('PATH','User'); [Environment]::SetEnvironmentVariable('PATH', \$old + ';' + '$FAKE_WIN', 'User')"
run "doctor 报告注册表 PATH 残留" doctor
expect "检出旧 JDK 残留" "里有旧 JDK"

run "32. doctor --fix -y 清理残留条目" doctor --fix -y
expect "残留已移除" "已移除"

run "33. doctor: 残留清理后复查" doctor
expect "无旧 JDK 残留" "无旧 JDK 残留"
expect "current/bin 未被误删" "current/bin 已在用户 PATH"

run "34. uninstall temurin@21 -y" uninstall temurin@21 -y
expect "uninstall 成功" "已卸载"

# ARM64 下载链路: amd64 runner 只验证到 install/校验/解压, 不跑 java -version
# (aarch64 二进制在 amd64 跑不了)。用 microsoft@17 避开上面 x64 已装的版本撞名。
echo "### 35. ARM64 install microsoft@17 (仅下载+校验+解压) ###"
if LAST_OUT="$(JVM_ARCH=aarch64 "$JVM" install microsoft@17 2>&1)"; then
    expect "ARM64 microsoft@17 安装完成" "安装完成"
else
    rc=$?
    echo "  ✗ ARM64 install 退出 $rc" >&2
    printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
    exit 1
fi

echo ""
echo "--- 负向用例 (命令应拒绝并退出非 0) ---"

run_fail "36. install 未知发行版" install nosuchdistro@21
expect "提示未知发行版" "未知"

run_fail "37. use 未安装的大版本" use temurin@99
expect "提示未安装" "没有安装"

run_fail "38. uninstall 不存在的版本" uninstall temurin@99 -y
expect "提示未找到" "没有"

echo ""
echo "--- 自更新 (jvm upgrade: 检查 GitHub Release) ---"
# 放最后: upgrade 在 dev build 版本号与 release 不同时会下载并替换 dist 下的 jvm.exe
# (replaceSelf 走 .bak 重命名, 处理运行中 exe)。runner 一次性, 替换不影响后续
# (本步骤已是最后一项)。本地单测难以覆盖 (需真实 Release + 替换二进制), 故放集成测试。
run "39. upgrade 检查最新版" upgrade
expect "upgrade 打印最新版本" "最新版本"

echo ""
echo "🎉 集成测试全部通过"
