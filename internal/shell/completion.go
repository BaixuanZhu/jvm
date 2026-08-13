// shell 包内补全相关: 为 PowerShell 和 bash 生成 Tab 补全脚本。
//
// 补全能力:
//   - jvm <TAB>           子命令名
//   - jvm install <TAB>   distro@ 前缀 (远程版本太慢不补)
//   - jvm use <TAB>       本地已装版本 (读 ~/.jvm/versions, 零子进程)
//   - jvm available <TAB> distro 名
//
// 设计要点:
//   - 补全用独立的 profile 标记块 (completionMarker), 与 shell 集成块分离,
//     两者各自升级互不影响。
//   - distro 列表在 jvm 启动时从 provider 注册表读取并嵌入脚本, 新增发行版后
//     下次启动自动刷新 (EnsureIntegration 幂等重写)。
//   - 本地版本补全纯走文件系统 IO, 不调子进程, 毫秒级。
//   - 目录名 distro-version 反转为 distro@version, 旧的无前缀目录视为 temurin
//     (与 junction.ResolveVersion 的兼容逻辑一致)。

package shell

import (
	"strings"

	"jvm/internal/provider"
)

// completionMarker 是 profile 里补全块的独立标记 (与 shell 集成块分离)。
const completionMarker = "# >>> jvm completion >>>"

// completionEndMarker 是补全块的结束标记。
const completionEndMarker = "# <<< jvm completion <<<"

// distroNames 从 provider 注册表提取所有发行版名 (provider.All 已字典序排序)。
// 供补全脚本嵌入 distro@ 前缀和 available 参数补全。
func distroNames() []string {
	ps := provider.All()
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name())
	}
	return names
}

// psCompletionScript 返回 PowerShell 补全脚本。
// distros 在 jvm 启动时从注册表读取并嵌入, 新增发行版后自动刷新。
func psCompletionScript(distros []string) string {
	// PowerShell 数组语法: @('temurin','corretto','microsoft')
	quoted := make([]string, len(distros))
	for i, d := range distros {
		quoted[i] = "'" + d + "'"
	}
	arr := "@(" + strings.Join(quoted, ",") + ")"
	return completionMarker + `
# jvm Tab completion (PowerShell 5.1+ / 7+): subcommands, distro@ prefix, local versions.
$_jvmDistros = ` + arr + `

# Convert local version dir name (distro-version or bare version) to distro@version.
# Bare dirs (no distro prefix) are treated as temurin.
function _jvmLocalVersions {
    $dir = Join-Path $env:USERPROFILE '.jvm\versions'
    if (-not (Test-Path $dir)) { return @() }
    $out = @()
    foreach ($d in (Get-ChildItem -Directory -Path $dir -ErrorAction SilentlyContinue)) {
        $name = $d.Name
        $matched = $false
        foreach ($distro in $_jvmDistros) {
            $prefix = $distro + '-'
            if ($name.StartsWith($prefix)) {
                $out += ($distro + '@' + $name.Substring($prefix.Length))
                $matched = $true
                break
            }
        }
        if (-not $matched) { $out += ('temurin@' + $name) }
    }
    return $out
}

Register-ArgumentCompleter -Native -CommandName jvm -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $elements = $commandAst.CommandElements
    $completingSub = ($elements.Count -eq 1) -or ($elements[1].Extent.Text -eq $wordToComplete)
    if ($completingSub) {
        $cmds = 'install','use','pin','list','ls','available','uninstall','rm','current','doctor','init','upgrade','completion','version','help'
        $cmds | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
        return
    }
    $cmd = $elements[1].Extent.Text
    if ($cmd -eq 'install') {
        $_jvmDistros | ForEach-Object { "$_@" } | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    } elseif ($cmd -eq 'available') {
        $_jvmDistros | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    } elseif ($cmd -in @('use','pin','uninstall','rm')) {
        _jvmLocalVersions | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    }
}
` + completionEndMarker
}

// bashCompletionScript 返回 bash 补全脚本。
// distros 在 jvm 启动时从注册表读取并嵌入。
func bashCompletionScript(distros []string) string {
	// bash 空格分隔列表: "temurin corretto microsoft"
	distroStr := strings.Join(distros, " ")
	return completionMarker + `
# jvm Tab completion (bash / Git Bash): subcommands, distro@ prefix, local versions.
_jvm_distros="` + distroStr + `"
_jvm_commands="install use pin list ls available uninstall rm current doctor init upgrade completion version help"

# Convert local version dir name (distro-version or bare version) to distro@version.
# Bare dirs (no distro prefix) are treated as temurin.
_jvm_local_versions() {
    local dir="$HOME/.jvm/versions"
    [ -d "$dir" ] || return 0
    local name distro ver prefix matched
    for name in "$dir"/*/; do
        [ -d "$name" ] || continue        # skip literal if glob unmatched
        name="${name%/}"; name="${name##*/}"
        matched=0
        for distro in $_jvm_distros; do
            prefix="${distro}-"
            if [ "${name#"$prefix"}" != "$name" ]; then
                ver="${name#"$prefix"}"
                echo "${distro}@${ver}"
                matched=1
                break
            fi
        done
        [ "$matched" -eq 0 ] && echo "temurin@${name}"
    done
}

_jvm() {
    local cur cmd
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=($(compgen -W "$_jvm_commands" -- "$cur"))
        return 0
    fi
    cmd="${COMP_WORDS[1]}"
    case "$cmd" in
        install)
            local distros="" d
            for d in $_jvm_distros; do distros="$distros ${d}@"; done
            COMPREPLY=($(compgen -W "$distros" -- "$cur"))
            ;;
        available)
            COMPREPLY=($(compgen -W "$_jvm_distros" -- "$cur"))
            ;;
        use|pin|uninstall|rm)
            COMPREPLY=($(compgen -W "$(_jvm_local_versions)" -- "$cur"))
            ;;
    esac
    return 0
}
complete -F _jvm jvm
` + completionEndMarker
}
