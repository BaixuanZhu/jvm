#requires -Version 5.1
<#
.SYNOPSIS
    jvm 一键安装脚本 (PowerShell)。
.DESCRIPTION
    下载最新 Release 的便携 zip, 校验 SHA256, 解压到用户目录, 运行一次 jvm.exe
    触发其内置自举 (注册用户 PATH + 注入 PowerShell/bash shell 集成)。
    行为对齐 NSIS 安装器 (installer/jvm.nsi), 仅 Windows x64。

    用法:
      一键安装 (用全部默认值):
        iwr -useb "https://raw.githubusercontent.com/BaixuanZhu/jvm/main/install.ps1" | iex
      本地传参:
        .\install.ps1 -InstallDir "D:\tools\jvm"

    环境变量:
      JVM_INSTALLER_MIRROR  指向镜像/自建源的下载前缀, 末尾带斜杠。
                            例如国内可设为 ghproxy 类前缀。默认走 GitHub Release。
      HTTPS_PROXY / HTTP_PROXY  PowerShell 原生代理, 透传给 Invoke-WebRequest。
#>
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\jvm')
)

# 始终启用 TLS 1.2, 兼容老系统默认只开 TLS 1.0 的情形。
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocol]::Tls12
} catch {
    # PS 7+ 走 HttpClient, 不依赖此设置, 忽略即可。
}

$ErrorActionPreference = 'Stop'

# -----------------------------------------------------------------------------
# 辅助输出: 中文 + 颜色
# -----------------------------------------------------------------------------
function Write-Info { param([string]$Msg) Write-Host $Msg -ForegroundColor Cyan }
function Write-Ok { param([string]$Msg) Write-Host $Msg -ForegroundColor Green }
function Write-Warn { param([string]$Msg) Write-Host $Msg -ForegroundColor Yellow }
function Write-Err { param([string]$Msg) Write-Host $Msg -ForegroundColor Red }

# -----------------------------------------------------------------------------
# 下载一个 URL 到本地文件 (兼容 PS 5.1, 用 -UseBasicParsing 避免 IE 引擎依赖)。
# -----------------------------------------------------------------------------
function Save-Url {
    param([string]$Url, [string]$Destination)
    # 5.1 的 IWR 不认 -ResponseHeadersVariable 等, 用最朴素参数集。
    Invoke-WebRequest -Uri $Url -OutFile $Destination -UseBasicParsing -ErrorAction Stop
}

# -----------------------------------------------------------------------------
# 解析 GNU coreutils sha256sum 文本, 兼容两种格式:
#   "<hash>  <filename>"  text 模式 (两个空格, 无星号)
#   "<hash> *<filename>"  binary 模式 (一个空格 + 前导星号)
# 与 internal/upgrade/upgrade.go 的 parseChecksum 逻辑一一对应。
# 返回找到的 hash (小写 hex), 找不到返回 $null。
# -----------------------------------------------------------------------------
function Get-ExpectedChecksum {
    param([string]$ChecksumText, [string]$Filename)
    foreach ($line in ($ChecksumText -split "`n")) {
        $line = $line.Trim()
        if (-not $line) { continue }
        $fields = $line -split '\s+'
        if ($fields.Count -ne 2) { continue }
        $hash = $fields[0]
        $name = $fields[1] -replace '^\*', ''
        if ($name -eq $Filename) { return $hash.ToLower() }
    }
    return $null
}

# =============================================================================
# 主流程
# =============================================================================
try {
    Write-Info '==> jvm 一键安装'

    # ---- 1. 环境校验 ---------------------------------------------------------
    if (-not [System.Environment]::Is64BitOperatingSystem) {
        throw '当前系统不是 64 位 Windows。jvm 仅支持 Windows x64。'
    }

    # ---- 2. 解析下载源 -------------------------------------------------------
    $base = if ($env:JVM_INSTALLER_MIRROR) { $env:JVM_INSTALLER_MIRROR.TrimEnd('/') + '/' } else { 'https://github.com/BaixuanZhu/jvm/releases/latest/download/' }
    $zipName = 'jvm-windows-amd64.zip'
    $checksumName = 'checksums.txt'
    $zipUrl = $base + $zipName
    $checksumUrl = $base + $checksumName
    Write-Info "下载源: $base"

    # ---- 3. 准备工作目录 -----------------------------------------------------
    $workDir = Join-Path $env:TEMP ("jvm-install-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $workDir -Force | Out-Null
    $zipPath = Join-Path $workDir $zipName
    $checksumPath = Join-Path $workDir $checksumName

    try {
        # ---- 4. 下载 zip -----------------------------------------------------
        Write-Info "下载 $zipName ..."
        Save-Url -Url $zipUrl -Destination $zipPath
        Write-Ok "下载完成: $zipPath"

        # ---- 5. 校验 SHA256 (与 jvm upgrade 共用 checksums.txt) --------------
        $expected = $null
        $hasChecksum = $false
        try {
            Save-Url -Url $checksumUrl -Destination $checksumPath
            $checksumText = Get-Content -Path $checksumPath -Raw
            $expected = Get-ExpectedChecksum -ChecksumText $checksumText -Filename $zipName
            $hasChecksum = $true
        } catch {
            # Release 没有 checksums.txt (旧版本/镜像缺失), 仅警告不阻断, 与 upgrade.go 向后兼容语义一致。
            Write-Warn "未取到 $checksumName, 跳过 SHA256 校验 (继续安装)。"
        }

        if ($hasChecksum) {
            if (-not $expected) {
                throw "$checksumName 里没有找到 $zipName 的条目, 校验中止。"
            }
            $actual = (Get-FileHash -Algorithm SHA256 -Path $zipPath).Hash.ToLower()
            Write-Info "期望 SHA256: $expected"
            Write-Info "实际 SHA256: $actual"
            if ($actual -ne $expected) {
                throw "SHA256 校验失败, 下载文件可能损坏或被篡改, 已中止安装。"
            }
            Write-Ok 'SHA256 校验通过'
        }

        # ---- 6. 安装目录 + 处理旧 jvm.exe (升级场景) -------------------------
        Write-Info "安装目录: $InstallDir"
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $exePath = Join-Path $InstallDir 'jvm.exe'
        if (Test-Path $exePath) {
            # 运行中的 exe 不能覆盖但能改名 (复刻 upgrade.go replaceSelf 的 .bak 策略)。
            $bak = Join-Path $InstallDir 'jvm.exe.bak'
            if (Test-Path $bak) { Remove-Item $bak -Force }
            try {
                Rename-Item -Path $exePath -NewName 'jvm.exe.bak' -Force
                Write-Warn '检测到旧版 jvm.exe, 已备份为 jvm.exe.bak。'
            } catch {
                throw "无法替换现有的 jvm.exe (可能正在运行): $($_.Exception.Message)。请关闭所有 jvm 进程后重试。"
            }
        }

        # ---- 7. 解压 zip 里的 jvm.exe (单文件, 直接 ExtractToFile) -----------
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        try {
            $archive = [System.IO.Compression.ZipFile]::OpenRead($zipPath)
            try {
                $entry = $archive.Entries | Where-Object { $_.Name -eq 'jvm.exe' } | Select-Object -First 1
                if (-not $entry) {
                    throw "zip 里找不到 jvm.exe, 安装包结构异常。"
                }
                [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $exePath, $true)
            } finally {
                $archive.Dispose()
            }
        } catch {
            throw "解压失败: $($_.Exception.Message)"
        }
        Write-Ok "已解压: $exePath"

        # ---- 8. 记录安装位置 (与 installer/jvm.nsi 的 HKCU\Software\jvm\InstallDir 一致,
        #         便于 jvm upgrade 复用同目录语义)。不写「程序和功能」卸载项 ——
        #         脚本安装无 uninstaller, 写了反而误导用户。 ---------------------
        try {
            New-Item -Path 'HKCU:\Software\jvm' -Force | Out-Null
            Set-ItemProperty -Path 'HKCU:\Software\jvm' -Name 'InstallDir' -Value $InstallDir
        } catch {
            # 注册表写失败不致命, jvm 自举仍会靠 os.Executable() 推导目录。
            Write-Warn "写入注册表 HKCU\Software\jvm\InstallDir 失败 (不影响安装): $($_.Exception.Message)"
        }

        # ---- 9. 运行一次 jvm.exe 触发自举 (注册用户 PATH + 注入 shell 集成) --
        # 这是与 NSIS SecConfig (installer/jvm.nsi:80-86) 等价的一步。
        Write-Info '触发自举 (配置 PATH 和 shell 集成) ...'
        try {
            $versionOutput = & $exePath version 2>&1 | Out-String
            $versionLine = ($versionOutput -split "`n" | Where-Object { $_ -match '\S' } | Select-Object -First 1)
        } catch {
            # 自举失败不回滚已解压的 exe, 用户可手动跑一次 jvm version 排查。
            Write-Warn "首次运行 jvm.exe 失败 (PATH/shell 集成可能未完成): $($_.Exception.Message)"
        }

        # ---- 10. 完成 --------------------------------------------------------
        Write-Ok ''
        Write-Ok '✅ 安装完成!'
        if ($versionLine) { Write-Ok "   版本: $($versionLine.Trim())" }
        Write-Ok "   路径: $exePath"
        Write-Info ''
        Write-Info '下一步:'
        Write-Info '  1. 重开一次终端 (让 PATH 和 shell 集成生效)'
        Write-Info '  2. jvm install 21      # 装 JDK'
        Write-Info '  3. jvm use 21          # 切换到它'
        Write-Info ''
        Write-Info '卸载: 删除整个安装目录即可 (用户数据在 ~/.jvm, 不会被删)。'
    } finally {
        # 清理临时目录 (无论成功失败)。
        if (Test-Path $workDir) {
            Remove-Item -Path $workDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
} catch {
    # 友好打印 + 重新抛出, 保证 .\install.ps1 直接运行时退出码非 0。
    # iwr | iex 模式下 throw 不会关闭会话, 只显示错误记录。
    Write-Err ''
    Write-Err "❌ 安装失败: $($_.Exception.Message)"
    throw
}
