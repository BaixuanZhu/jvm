// shell 包内补全相关: 为 PowerShell 和 bash 生成 Tab 补全脚本。
//
// 补全能力:
//   - jvm <TAB>                     子命令名
//   - jvm install <TAB>             distro@ 前缀 (远程版本太慢不补; 第二参数 zip 路径走默认文件补全)
//   - jvm use/pin/uninstall <TAB>   本地已装版本 (读 ~/.jvm/versions, 零子进程)
//   - jvm exec <TAB>                本地已装版本 (仅 -- 之前的版本槽, 之后让位给命令补全)
//   - jvm available <TAB>           distro 名 + -a/-m 选项
//   - jvm doctor <TAB>              --fix/-y 选项
//   - jvm init/completion <TAB>     powershell/bash 参数 + --install 选项
//
// 设计要点:
//   - 补全用独立的 profile 标记块 (completionMarker), 与 shell 集成块分离,
//     两者各自升级互不影响。
//   - 补全块内嵌 completionVersionToken, 走与集成块同款的版本化重写:
//     改补全脚本内容 (含新增发行版导致的 distro 列表变化) 须递增 token,
//     老用户的旧块才会被 EnsureIntegration 自动重写刷新。
//   - distro 列表在 jvm 启动时从 provider 注册表读取并嵌入脚本。
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

// completionVersionToken 是补全块的版本标记 (嵌在块内, marker 行之后)。
// 与集成块的 integrationVersionToken 同构: 老块没有 token (或 token 过期) 时
// 启动自举自动重写, 保证升级 jvm 后老用户能拿到新补全 (此前只按 marker 存在性
// 判断, 内容变更后老 profile 永不更新)。改补全脚本内容 (含新增发行版导致的
// distro 列表变化) 时同步递增此 token。
// v2: 子命令列表补 cache 及其 clean 参数补全。
// v3: 发行版列表新增 graalvm。
// v4: 发行版列表新增 temurin-ea; 本地版本目录反转改最长前缀匹配
//
//	(temurin-ea-* 不再被短名 temurin 抢匹配)。
//
// v5: 补全覆盖扩展: exec 版本参数 (-- 前) / available 与 doctor 选项 /
//
//	init 与 completion 参数; install 限版本槽, zip 路径槽让给默认文件补全。
const completionVersionToken = "# jvm-completion: v5"

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
	return completionMarker + "\n" + completionVersionToken + `
# jvm Tab completion (PowerShell 5.1+ / 7+): subcommands, distro@ prefix, local versions.
$_jvmDistros = ` + arr + `

# Convert local version dir name (distro-version or bare version) to distro@version.
# Bare dirs (no distro prefix) are treated as temurin.
# Longest-prefix match: temurin-ea-* must not be captured by the shorter "temurin".
function _jvmLocalVersions {
    $dir = Join-Path $env:USERPROFILE '.jvm\versions'
    if (-not (Test-Path $dir)) { return @() }
    $out = @()
    foreach ($d in (Get-ChildItem -Directory -Path $dir -ErrorAction SilentlyContinue)) {
        $name = $d.Name
        $best = ''
        foreach ($distro in $_jvmDistros) {
            $prefix = $distro + '-'
            if ($name.StartsWith($prefix) -and $prefix.Length -gt $best.Length) { $best = $prefix }
        }
        if ($best -ne '') {
            $out += ($best.Substring(0, $best.Length - 1) + '@' + $name.Substring($best.Length))
        } else {
            $out += ('temurin@' + $name)
        }
    }
    return $out
}

Register-ArgumentCompleter -Native -CommandName jvm -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $elements = $commandAst.CommandElements
    $completingSub = ($elements.Count -eq 1) -or ($elements[1].Extent.Text -eq $wordToComplete)
    if ($completingSub) {
        $cmds = 'install','use','pin','list','ls','available','outdated','update','uninstall','rm','current','home','exec','doctor','init','upgrade','completion','cache','version','help'
        $cmds | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
        return
    }
    $cmd = $elements[1].Extent.Text
    # Completing the first argument slot? Later slots (install's zip path) fall back to file completion.
    $completingArg1 = ($elements.Count -eq 2) -or ($elements[2].Extent.Text -eq $wordToComplete)
    if ($cmd -eq 'install') {
        # Only the version slot gets distro@ prefixes; the zip path slot uses default file completion.
        if ($completingArg1) {
            $_jvmDistros | ForEach-Object { "$_@" } | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
    } elseif ($cmd -eq 'available') {
        $cands = @('-a','--all','-m','--major') + $_jvmDistros
        $cands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    } elseif ($cmd -in @('use','pin','uninstall','rm')) {
        _jvmLocalVersions | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    } elseif ($cmd -eq 'exec') {
        # Complete versions only before --; past it the user types the command to run.
        $pastSep = $false
        foreach ($e in $elements) {
            if ($e.Extent.Text -eq '--') { $pastSep = $true; break }
        }
        if (-not $pastSep) {
            _jvmLocalVersions | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
    } elseif ($cmd -eq 'update') {
        # update only accepts major versions: trim local versions to distro@major, dedup
        _jvmLocalVersions | ForEach-Object {
            $p = $_.Split('@', 2)
            $p[0] + '@' + $p[1].Split('.')[0]
        } | Sort-Object -Unique | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    } elseif ($cmd -eq 'cache') {
        'clean' | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    } elseif ($cmd -eq 'doctor') {
        '--fix','-f','-y','--yes' | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    } elseif ($cmd -in @('init','completion')) {
        if ($completingArg1) {
            'powershell','bash' | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        } else {
            '--install','-i' | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
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
	return completionMarker + "\n" + completionVersionToken + `
# jvm Tab completion (bash / Git Bash): subcommands, distro@ prefix, local versions.
_jvm_distros="` + distroStr + `"
_jvm_commands="install use pin list ls available outdated update uninstall rm current home exec doctor init upgrade completion cache version help"

# Convert local version dir name (distro-version or bare version) to distro@version.
# Bare dirs (no distro prefix) are treated as temurin.
# Longest-prefix match: temurin-ea-* must not be captured by the shorter "temurin".
_jvm_local_versions() {
    local dir="$HOME/.jvm/versions"
    [ -d "$dir" ] || return 0
    local name distro prefix best
    for name in "$dir"/*/; do
        [ -d "$name" ] || continue        # skip literal if glob unmatched
        name="${name%/}"; name="${name##*/}"
        best=""
        for distro in $_jvm_distros; do
            prefix="${distro}-"
            if [ "${name#"$prefix"}" != "$name" ] && [ "${#prefix}" -gt "${#best}" ]; then
                best="$prefix"
            fi
        done
        if [ -n "$best" ]; then
            echo "${best%-}@${name#"$best"}"
        else
            echo "temurin@${name}"
        fi
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
    # First argument slot? Later slots (install's zip path) fall back to file completion.
    local arg1=0
    [ "$COMP_CWORD" -eq 2 ] && arg1=1
    case "$cmd" in
        install)
            # Only the version slot gets distro@ prefixes; the zip path slot uses file completion.
            if [ "$arg1" -eq 1 ]; then
                local distros="" d
                for d in $_jvm_distros; do distros="$distros ${d}@"; done
                COMPREPLY=($(compgen -W "$distros" -- "$cur"))
            fi
            ;;
        available)
            COMPREPLY=($(compgen -W "-a --all -m --major $_jvm_distros" -- "$cur"))
            ;;
        exec)
            # Complete versions only before --; past it the user types the command to run.
            local i past_sep=0
            for ((i=2; i<COMP_CWORD; i++)); do
                [ "${COMP_WORDS[i]}" = "--" ] && { past_sep=1; break; }
            done
            if [ "$past_sep" -eq 0 ]; then
                COMPREPLY=($(compgen -W "$(_jvm_local_versions)" -- "$cur"))
            fi
            ;;
        doctor)
            COMPREPLY=($(compgen -W "--fix -f -y --yes" -- "$cur"))
            ;;
        init|completion)
            if [ "$arg1" -eq 1 ]; then
                COMPREPLY=($(compgen -W "powershell bash" -- "$cur"))
            else
                COMPREPLY=($(compgen -W "--install -i" -- "$cur"))
            fi
            ;;
        use|pin|uninstall|rm)
            COMPREPLY=($(compgen -W "$(_jvm_local_versions)" -- "$cur"))
            ;;
        update)
            # update only accepts major versions: trim local versions to distro@major, dedup
            local v d m majors=""
            for v in $(_jvm_local_versions); do
                d="${v%%@*}"; v="${v#*@}"
                v="${d}@${v%%.*}"
                case " $majors " in *" $v "*) ;; *) majors="$majors $v" ;; esac
            done
            COMPREPLY=($(compgen -W "$majors" -- "$cur"))
            ;;
        cache)
            COMPREPLY=($(compgen -W "clean" -- "$cur"))
            ;;
    esac
    return 0
}
complete -F _jvm jvm
` + completionEndMarker
}
