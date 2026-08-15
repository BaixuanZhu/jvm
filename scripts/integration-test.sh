#!/usr/bin/env bash
# jvm 端到端集成测试: 跑真实 jvm.exe 验证 install/use/list/doctor/uninstall 等核心命令。
#
# 隔离机制 (CI / 本地都靠这两条, 不污染系统):
#   JVM_NO_BOOTSTRAP=1   关闭自举, 不写 shell profile / 用户 PATH
#   JVM_HOME=<临时目录>  把 ~/.jvm 重定向到临时目录, install 的 versions/current 全部隔离
#
# ⚠️ 注意: `jvm use` 仍会写 HKCU\Environment 注册表 (JAVA_HOME + 用户 PATH), 这与
#   paths.Root 无关、无法靠 JVM_HOME 重定向。CI runner 一次性, 污染可接受; 本地手动跑
#   本脚本前请知悉 (会改本机注册表 JAVA_HOME / 用户 PATH)。
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
expect "version 输出版本号" "^jvm [0-9]"

run "2. install temurin@21 (SHA256)" install temurin@21
expect "temurin 安装完成" "安装完成"

run "3. list" list
expect "list 含 temurin-21" "temurin-21"

run "4. use temurin@21 (junction + 注册表)" use temurin@21
expect "use 切换成功" "已切换"

run "5. current + java -version 实跑" current
expect "current 显示当前版本" "当前版本"
expect "java -version 实跑成功" "version"

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

# --- jvm outdated: patch 升级检查 ---
# CI 刚装的版本可能就是最新 patch, "可升级/均为最新"两种输出都合法, 只断言必然
# 出现的表头与退出 0, 不断言具体分支 (防上游发新版导致用例抖动)。
run "11. outdated: patch 升级检查" outdated
expect "outdated 打印查询表头" "最新 patch"

# --- jvm use --auto: .jvmrc 自动切换状态机 ---
# 前置: current 为 temurin@21 (步骤 4 切的, 后续 install 不切)。
# 注意 NO_RC 须选一条向上都无 .jvmrc 的链 (runner 的 mktemp 目录满足)。
PROJ_RC="$(mktemp -d)"
NO_RC="$(mktemp -d)"
echo "corretto@21" > "$PROJ_RC/.jvmrc"

echo "### 12. use --auto: cd 进 .jvmrc 目录自动切换 ###"
rc=0
LAST_OUT="$(cd "$PROJ_RC" && "$JVM" use --auto 2>&1)" || rc=$?
if [ "$rc" -ne 0 ]; then
    echo "  ✗ use --auto 退出 $rc" >&2
    printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
    exit 1
fi
printf '%s\n' "$LAST_OUT" | grep -q "切换到 corretto" \
    && echo "  ✓ 自动切到 corretto" \
    || { echo "  ✗ 期望输出含\"切换到 corretto\":" >&2; printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2; exit 1; }

run "13. current 验证自动切换结果" current
expect "current 为 corretto" "corretto"

echo "### 14. use --auto: 离开 .jvmrc 目录恢复手动基线 ###"
rc=0
LAST_OUT="$(cd "$NO_RC" && "$JVM" use --auto 2>&1)" || rc=$?
if [ "$rc" -ne 0 ]; then
    echo "  ✗ use --auto 退出 $rc" >&2
    printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
    exit 1
fi
printf '%s\n' "$LAST_OUT" | grep -q "恢复 temurin" \
    && echo "  ✓ 恢复到 temurin" \
    || { echo "  ✗ 期望输出含\"恢复 temurin\":" >&2; printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2; exit 1; }

run "15. current 验证恢复" current
expect "current 回到 temurin" "temurin"

# 未安装版本: 一行警告但不失败 (exit 0), 不刷屏语义由调用方保证
PROJ_BAD="$(mktemp -d)"
echo "zulu@99" > "$PROJ_BAD/.jvmrc"
echo "### 16. use --auto: .jvmrc 指向未装版本 (警告不失败) ###"
rc=0
LAST_OUT="$(cd "$PROJ_BAD" && "$JVM" use --auto 2>&1)" || rc=$?
if [ "$rc" -ne 0 ]; then
    echo "  ✗ 警告路径不应失败 (退出 $rc)" >&2
    printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
    exit 1
fi
printf '%s\n' "$LAST_OUT" | grep -q "未安装" \
    && echo "  ✓ 如期提示未安装" \
    || { echo "  ✗ 期望输出含\"未安装\":" >&2; printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2; exit 1; }

# --- doctor --fix: junction 重建闭环 ---
# 用 cmd /c rmdir 删 junction (只删链接本身); 不能用 rm —— MSYS rm 会穿透
# junction 递归删除目标内容, 那是真实 JDK 目录。cygpath 归一成纯反斜杠路径,
# 避免 runner.temp 混合斜杠被 cmd 误解析。
echo "### 17. 删除 current junction, doctor 应报 ✗ ###"
cmd //c rmdir "$(cygpath -w "$JVM_HOME")\\current"
run "doctor 报告 current 链接异常" doctor
expect "current 链接 ✗" "尚未选定"

# --fix -y 会顺带补写 runner 的 shell profile (集成项原本关自举不写, 这里是有意
# 验证修复动作) 与注册表 JAVA_HOME/用户 PATH —— runner 一次性, 可接受。
run "18. doctor --fix -y 自动重建 junction" doctor --fix -y
expect "junction 已重建" "已重建"

run "19. current 重建后可用" current
expect "current 显示当前版本" "当前版本"

run "20. uninstall temurin@21 -y" uninstall temurin@21 -y
expect "uninstall 成功" "已卸载"

# ARM64 下载链路: amd64 runner 只验证到 install/校验/解压, 不跑 java -version
# (aarch64 二进制在 amd64 跑不了)。用 microsoft@17 避开上面 x64 已装的版本撞名。
echo "### 21. ARM64 install microsoft@17 (仅下载+校验+解压) ###"
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

run_fail "22. install 未知发行版" install nosuchdistro@21
expect "提示未知发行版" "未知"

run_fail "23. use 未安装的大版本" use temurin@99
expect "提示未安装" "没有安装"

run_fail "24. uninstall 不存在的版本" uninstall temurin@99 -y
expect "提示未找到" "没有"

echo ""
echo "--- 自更新 (jvm upgrade: 检查 GitHub Release) ---"
# 放最后: upgrade 在 dev build 版本号与 release 不同时会下载并替换 dist 下的 jvm.exe
# (replaceSelf 走 .bak 重命名, 处理运行中 exe)。runner 一次性, 替换不影响后续
# (本步骤已是最后一项)。本地单测难以覆盖 (需真实 Release + 替换二进制), 故放集成测试。
run "25. upgrade 检查最新版" upgrade
expect "upgrade 打印最新版本" "最新版本"

echo ""
echo "🎉 集成测试全部通过"
