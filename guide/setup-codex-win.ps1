# Codex 配置脚本 (Windows) - ExecutionPolicy 修复版
# 用法:
#   powershell -ExecutionPolicy Bypass -File setup-codex.ps1 -BaseUrl https://api.jucodex.com/v1 -ApiKey sk-xxxx
#   powershell -ExecutionPolicy Bypass -File setup-codex.ps1 -Show
#   powershell -ExecutionPolicy Bypass -File setup-codex.ps1 -Test -BaseUrl https://api.jucodex.com/v1 -ApiKey sk-xxxx

param(
    [string]$BaseUrl,
    [string]$ApiKey,
    [switch]$Test,
    [switch]$Show,
    [switch]$Help
)

# 支持一行命令预设变量
if (-not $BaseUrl -and (Test-Path Variable:url)) { $BaseUrl = $url }
if (-not $ApiKey -and (Test-Path Variable:key)) { $ApiKey = $key }

# 配置路径
$DefaultBaseUrl = "https://api.jucodex.com/v1"
$CodexConfigDir = "$env:USERPROFILE\.codex"
$CodexConfigFile = "$CodexConfigDir\config.toml"
$CodexAuthFile = "$CodexConfigDir\auth.json"

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO]" -ForegroundColor Blue -NoNewline
    Write-Host " $Message"
}

function Write-Success {
    param([string]$Message)
    Write-Host "[SUCCESS]" -ForegroundColor Green -NoNewline
    Write-Host " $Message"
}

function Write-Warn {
    param([string]$Message)
    Write-Host "[WARNING]" -ForegroundColor Yellow -NoNewline
    Write-Host " $Message"
}

function Write-Err {
    param([string]$Message)
    Write-Host "[ERROR]" -ForegroundColor Red -NoNewline
    Write-Host " $Message"
}

function Show-Help {
    Write-Host @"
Codex Configuration Script (Windows, ExecutionPolicy-fixed)

Usage:
  powershell -ExecutionPolicy Bypass -File setup-codex.ps1 [OPTIONS]

Options:
  -BaseUrl <URL>  Set base URL (default: $DefaultBaseUrl)
  -ApiKey <KEY>   Set API key
  -Test           Test API connection only (requires -BaseUrl and -ApiKey)
  -Show           Show current settings and exit
  -Help           Show this help message

Compatibility mode (this script writes BOTH files):
  1) ~/.codex/config.toml
  2) ~/.codex/auth.json
"@
}

function Resolve-CommandPath {
    param(
        [Parameter(Mandatory = $true)][string[]]$Names,
        [string[]]$HardcodedPaths = @()
    )

    foreach ($p in $HardcodedPaths) {
        if ([string]::IsNullOrWhiteSpace($p)) { continue }
        if (Test-Path $p) { return $p }
    }

    foreach ($n in $Names) {
        try {
            $cmd = Get-Command $n -ErrorAction SilentlyContinue
            if ($cmd -and $cmd.Source) { return $cmd.Source }
        } catch {}
    }

    return $null
}

function Get-NpmPath {
    $hardcoded = @(
        "$env:ProgramFiles\nodejs\npm.cmd",
        "$env:ProgramFiles(x86)\nodejs\npm.cmd"
    )
    return Resolve-CommandPath -Names @("npm.cmd", "npm.exe", "npm") -HardcodedPaths $hardcoded
}

function Get-CodexPath {
    # 优先 cmd/exe，避免 codex.ps1 受执行策略影响
    return Resolve-CommandPath -Names @("codex.cmd", "codex.exe", "codex")
}

function Test-Codex {
    try {
        $codexPath = Get-CodexPath
        if ($codexPath) {
            $ver = & $codexPath --version 2>$null
            if ($LASTEXITCODE -eq 0 -and $ver) {
                Write-Success "Codex is already installed: $ver"
                return $true
            }
            Write-Success "Codex command exists: $codexPath"
            return $true
        }
    }
    catch {}
    return $false
}

function Test-NodeJS {
    try {
        $nodeVersion = & node --version 2>$null
        if ($LASTEXITCODE -eq 0 -and $nodeVersion) {
            if ($nodeVersion -match 'v?(\d+)\.(\d+)\.(\d+)') {
                $major = [int]$Matches[1]
                if ($major -ge 18) {
                    Write-Success "Node.js is installed: $nodeVersion"
                    return $true
                }
                Write-Warn "Node.js version is too old: $nodeVersion (requires >= 18.0.0)"
            }
        }
    }
    catch {}
    return $false
}

function Install-NodeJS {
    Write-Info "Node.js is not installed or version is too old. Installing Node.js..."

    $arch = if ([Environment]::Is64BitOperatingSystem) { "x64" } else { "x86" }
    $nodeVersion = "22.9.0"
    $nodeUrl = "https://nodejs.org/dist/v$nodeVersion/node-v$nodeVersion-$arch.msi"
    $installerPath = "$env:TEMP\node-installer.msi"

    try {
        Write-Info "Downloading Node.js installer from: $nodeUrl"
        $oldProgress = $ProgressPreference
        $ProgressPreference = 'SilentlyContinue'
        Invoke-WebRequest -Uri $nodeUrl -OutFile $installerPath -UseBasicParsing
        $ProgressPreference = $oldProgress

        if (-not (Test-Path $installerPath)) {
            Write-Err "Failed to download Node.js installer"
            return $false
        }

        Write-Info "Installing Node.js silently..."
        $installArgs = @('/i', "`"$installerPath`"", '/quiet', '/norestart', 'ADDLOCAL=ALL')
        $process = Start-Process -FilePath "msiexec.exe" -ArgumentList $installArgs -Wait -PassThru

        Remove-Item -Path $installerPath -Force -ErrorAction SilentlyContinue

        if ($process.ExitCode -ne 0) {
            Write-Err "Node.js installation failed with exit code: $($process.ExitCode)"
            return $false
        }

        # 刷新当前进程 PATH（避免新装后当前窗口找不到命令）
        $machinePath = [System.Environment]::GetEnvironmentVariable("Path", "Machine")
        $userPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
        $env:Path = "$machinePath;$userPath"

        Write-Success "Node.js installed successfully"
        return $true
    }
    catch {
        Write-Err "Failed to install Node.js: $($_.Exception.Message)"
        return $false
    }
}

function Install-Codex {
    Write-Info "Installing Codex CLI..."

    try {
        $npmPath = Get-NpmPath
        if (-not $npmPath) {
            Write-Err "npm is not available. Please restart PowerShell after Node.js installation."
            return $false
        }

        Write-Info "Using npm executable: $npmPath"
        $npmVersion = & $npmPath --version 2>$null
        if ($LASTEXITCODE -ne 0 -or -not $npmVersion) {
            Write-Err "npm is found but not runnable."
            return $false
        }

        Write-Info "npm version: $npmVersion"
        Write-Info "Running: `"$npmPath`" install -g @openai/codex"

        # 关键：使用 npm.cmd，规避 npm.ps1 执行策略限制
        & $npmPath install -g @openai/codex 2>&1 | ForEach-Object { Write-Host $_ }

        if ($LASTEXITCODE -eq 0) {
            $codexPath = Get-CodexPath
            if ($codexPath) {
                $ver = & $codexPath --version 2>$null
                if ($LASTEXITCODE -eq 0 -and $ver) {
                    Write-Success "Codex installed successfully: $ver"
                } else {
                    Write-Success "Codex installed, command path: $codexPath"
                }
            } else {
                Write-Success "Codex install command finished"
            }
            return $true
        }

        Write-Err "Failed to install Codex via npm"
        return $false
    }
    catch {
        Write-Err "Failed to install Codex: $($_.Exception.Message)"
        return $false
    }
}

function Ensure-Codex {
    Write-Info "Checking Codex installation..."

    if (Test-Codex) {
        return $true
    }

    Write-Warn "Codex is not installed"

    if (-not (Test-NodeJS)) {
        if (-not (Install-NodeJS)) {
            Write-Warn "Failed to install Node.js automatically"
            Write-Info "Please install Node.js manually from: https://nodejs.org/"
            return $false
        }
    }

    return (Install-Codex)
}

function New-SettingsDirectory {
    if (-not (Test-Path $CodexConfigDir)) {
        New-Item -ItemType Directory -Path $CodexConfigDir -Force | Out-Null
        Write-Info "Created Codex configuration directory: $CodexConfigDir"
    }
}

function Backup-Settings {
    $timestamp = Get-Date -Format "yyyyMMdd_HHmmss"

    if (Test-Path $CodexConfigFile) {
        $backupFile = "$CodexConfigFile.backup.$timestamp"
        Copy-Item -Path $CodexConfigFile -Destination $backupFile
        Write-Info "Backed up existing config to: $backupFile"
    }

    if (Test-Path $CodexAuthFile) {
        $backupAuthFile = "$CodexAuthFile.backup.$timestamp"
        Copy-Item -Path $CodexAuthFile -Destination $backupAuthFile
        Write-Info "Backed up existing auth to: $backupAuthFile"
    }
}

function Test-ApiKey {
    param([string]$ApiKey)

    if ($ApiKey -match '^[A-Za-z0-9._-]+$') {
        return $true
    }

    Write-Err "Invalid API key format. Allowed chars: letters, numbers, dot, underscore, hyphen."
    return $false
}

function Test-ApiConnection {
    param(
        [string]$BaseUrl,
        [string]$ApiKey
    )

    try {
        $uri = "$($BaseUrl.TrimEnd('/'))/health"
        $headers = @{
            "Content-Type"  = "application/json"
            "Authorization" = "Bearer $ApiKey"
        }

        $null = Invoke-RestMethod -Uri $uri -Method Get -Headers $headers -ErrorAction Stop
        Write-Success "API connection successful: $uri"
        return $true
    }
    catch {
        if ($_.Exception.Response -and $_.Exception.Response.StatusCode -eq 401) {
            Write-Err "API key authentication failed (401)."
        } elseif ($_.Exception.Message -like "*Unable to connect*" -or $_.Exception.Message -like "*could not be resolved*") {
            Write-Err "Cannot connect to API server. Check BaseUrl and network."
        } else {
            Write-Err "API test failed: $($_.Exception.Message)"
        }
        return $false
    }
}

function New-Settings {
    param(
        [string]$BaseUrl,
        [string]$ApiKey
    )

    $config = @"
model_provider = "OpenAI"
model = "gpt-5.4"
model_reasoning_effort = "high"
disable_response_storage = true

[model_providers.OpenAI]
name = "OpenAI"
base_url = "$BaseUrl"
wire_api = "responses"
requires_openai_auth = true
"@

    $auth = @"
{
  "auth_mode": "apikey",
  "OPENAI_API_KEY": "$ApiKey"
}
"@

    try {
        Set-Content -Path $CodexConfigFile -Value $config -Encoding UTF8
        Write-Success "Codex configuration written to: $CodexConfigFile"

        Set-Content -Path $CodexAuthFile -Value $auth -Encoding UTF8
        Write-Success "Codex auth written to: $CodexAuthFile"

        [Environment]::SetEnvironmentVariable("OPENAI_API_KEY", $ApiKey, [EnvironmentVariableTarget]::User)
        [Environment]::SetEnvironmentVariable("CODEX_API_KEY", $ApiKey, [EnvironmentVariableTarget]::User)
        $env:OPENAI_API_KEY = $ApiKey
        $env:CODEX_API_KEY = $ApiKey

        Write-Success "Environment variables OPENAI_API_KEY and CODEX_API_KEY set successfully"
        return $true
    }
    catch {
        Write-Err "Failed to create configuration: $($_.Exception.Message)"
        return $false
    }
}

function Show-Settings {
    if (Test-Path $CodexConfigFile) {
        Write-Host ""
        Write-Info "Current Codex config:"
        Write-Host "----------------------------------------"
        Get-Content $CodexConfigFile
        Write-Host "----------------------------------------"
    } else {
        Write-Info "No existing config file found: $CodexConfigFile"
    }

    if (Test-Path $CodexAuthFile) {
        Write-Host ""
        Write-Info "Current Codex auth:"
        Write-Host "----------------------------------------"
        Get-Content $CodexAuthFile | ForEach-Object {
            if ($_ -match '"OPENAI_API_KEY"\s*:\s*"(.+)"') {
                $k = $Matches[1]
                if ($k.Length -gt 12) {
                    '  "OPENAI_API_KEY": "' + $k.Substring(0, 8) + '...' + $k.Substring($k.Length - 4) + '"'
                } else {
                    '  "OPENAI_API_KEY": "' + $k.Substring(0, [Math]::Min(4, $k.Length)) + '..."'
                }
            } else {
                $_
            }
        }
        Write-Host "----------------------------------------"
    } else {
        Write-Info "No existing auth file found: $CodexAuthFile"
    }
}

function Main {
    Write-Info "Codex Configuration Script"
    Write-Host "======================================="
    Write-Host ""

    if ($Help) {
        Show-Help
        return
    }

    if ($Show) {
        Show-Settings
        return
    }

    # 仅测试时不安装
    if (-not $Test) {
        if (-not (Ensure-Codex)) {
            Write-Err "Codex installation check failed. Aborting configuration write."
            return
        }
        Write-Host ""
    }

    if (-not $BaseUrl -and -not $ApiKey) {
        Write-Info "Interactive setup mode"
        Write-Host ""

        $inputUrl = Read-Host "Enter Base URL [$DefaultBaseUrl]"
        if ([string]::IsNullOrWhiteSpace($inputUrl)) {
            $BaseUrl = $DefaultBaseUrl
        } else {
            $BaseUrl = $inputUrl
        }

        while ([string]::IsNullOrWhiteSpace($ApiKey)) {
            $ApiKey = Read-Host "Enter your API key"
            if ([string]::IsNullOrWhiteSpace($ApiKey)) {
                Write-Warn "API key is required"
            } elseif (-not (Test-ApiKey $ApiKey)) {
                $ApiKey = ""
            }
        }
    }

    if ([string]::IsNullOrWhiteSpace($BaseUrl) -or [string]::IsNullOrWhiteSpace($ApiKey)) {
        Write-Err "Both BaseUrl and ApiKey are required"
        Write-Info "Use -Help for usage information"
        return
    }

    if (-not (Test-ApiKey $ApiKey)) {
        return
    }

    $BaseUrl = $BaseUrl.TrimEnd('/')

    Write-Info "Configuration:"
    Write-Info "  Base URL: $BaseUrl"
    $maskedKey = if ($ApiKey.Length -gt 12) { "$($ApiKey.Substring(0, 8))...$($ApiKey.Substring($ApiKey.Length - 4))" } else { "$($ApiKey.Substring(0, [Math]::Min(4, $ApiKey.Length)))..." }
    Write-Info "  API Key: $maskedKey"
    Write-Host ""

    if ($Test) {
        Test-ApiConnection -BaseUrl $BaseUrl -ApiKey $ApiKey | Out-Null
        return
    }

    New-SettingsDirectory
    Backup-Settings

    if (New-Settings -BaseUrl $BaseUrl -ApiKey $ApiKey) {
        Write-Host ""
        Write-Success "Compatibility mode files generated."
        Write-Info "  - $CodexConfigFile"
        Write-Info "  - $CodexAuthFile"

        $codexPath = Get-CodexPath
        if ($codexPath) {
            Write-Info "Run `"$codexPath --version`" or start codex in a new terminal to verify."
        }

        Write-Host ""
        Show-Settings
    } else {
        Write-Err "Failed to write settings"
    }
}

Main