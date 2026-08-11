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
JVM="./dist/${ARCH}/jvm.exe"

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

run "8. uninstall temurin@21 -y" uninstall temurin@21 -y
expect "uninstall 成功" "已卸载"

# ARM64 下载链路: amd64 runner 只验证到 install/校验/解压, 不跑 java -version
# (aarch64 二进制在 amd64 跑不了)。用 microsoft@17 避开上面 x64 已装的版本撞名。
echo "### 9. ARM64 install microsoft@17 (仅下载+校验+解压) ###"
if LAST_OUT="$(JVM_ARCH=aarch64 "$JVM" install microsoft@17 2>&1)"; then
    expect "ARM64 microsoft@17 安装完成" "安装完成"
else
    rc=$?
    echo "  ✗ ARM64 install 退出 $rc" >&2
    printf '%s\n' "$LAST_OUT" | sed 's/^/      /' >&2
    exit 1
fi

echo ""
echo "🎉 集成测试全部通过"
