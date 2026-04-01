# Codex 配置脚本 (Windows)
# 用法:
#   powershell -ExecutionPolicy Bypass -File setup-codex.ps1 -BaseUrl https://api.jucode.cn -ApiKey sk-xxxx
#   powershell -ExecutionPolicy Bypass -File setup-codex.ps1 -Show
#   powershell -ExecutionPolicy Bypass -File setup-codex.ps1 -Test -BaseUrl https://api.jucode.cn -ApiKey sk-xxxx

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
$DefaultBaseUrl = "https://jucodex.com"
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

function Write-Warning {
    param([string]$Message)
    Write-Host "[WARNING]" -ForegroundColor Yellow -NoNewline
    Write-Host " $Message"
}

function Write-Error {
    param([string]$Message)
    Write-Host "[ERROR]" -ForegroundColor Red -NoNewline
    Write-Host " $Message"
}

function Show-Help {
    Write-Host @"
Codex Configuration Script (Windows)

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

function Test-Codex {
    try {
        $cmd = Get-Command codex -ErrorAction SilentlyContinue
        if ($cmd) {
            $ver = & codex --version 2>$null
            if ($LASTEXITCODE -eq 0 -and $ver) {
                Write-Success "Codex is already installed: $ver"
                return $true
            }
            Write-Success "Codex command exists"
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
                Write-Warning "Node.js version is too old: $nodeVersion (requires >= 18.0.0)"
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
        $ProgressPreference = 'SilentlyContinue'
        Invoke-WebRequest -Uri $nodeUrl -OutFile $installerPath -UseBasicParsing
        $ProgressPreference = 'Continue'

        if (-not (Test-Path $installerPath)) {
            Write-Error "Failed to download Node.js installer"
            return $false
        }

        Write-Info "Installing Node.js silently..."
        $installArgs = @('/i', "`"$installerPath`"", '/quiet', '/norestart', 'ADDLOCAL=ALL')
        $process = Start-Process -FilePath "msiexec.exe" -ArgumentList $installArgs -Wait -PassThru

        Remove-Item -Path $installerPath -Force -ErrorAction SilentlyContinue

        if ($process.ExitCode -ne 0) {
            Write-Error "Node.js installation failed with exit code: $($process.ExitCode)"
            return $false
        }

        $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
        Write-Success "Node.js installed successfully"
        return $true
    }
    catch {
        Write-Error "Failed to install Node.js: $($_.Exception.Message)"
        return $false
    }
}

function Install-Codex {
    Write-Info "Installing Codex CLI..."

    try {
        $npmVersion = & npm --version 2>$null
        if ($LASTEXITCODE -ne 0) {
            Write-Error "npm is not available. Please restart PowerShell after Node.js installation."
            return $false
        }

        Write-Info "npm version: $npmVersion"
        Write-Info "Running: npm install -g @openai/codex"
        & npm install -g @openai/codex 2>&1 | ForEach-Object { Write-Host $_ }

        if ($LASTEXITCODE -eq 0) {
            $ver = & codex --version 2>$null
            if ($LASTEXITCODE -eq 0 -and $ver) {
                Write-Success "Codex installed successfully: $ver"
            } else {
                Write-Success "Codex install command finished"
            }
            return $true
        }

        Write-Error "Failed to install Codex via npm"
        return $false
    }
    catch {
        Write-Error "Failed to install Codex: $($_.Exception.Message)"
        return $false
    }
}

function Ensure-Codex {
    Write-Info "Checking Codex installation..."

    if (Test-Codex) {
        return $true
    }

    Write-Warning "Codex is not installed"

    if (-not (Test-NodeJS)) {
        if (-not (Install-NodeJS)) {
            Write-Warning "Failed to install Node.js automatically"
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

    Write-Error "Invalid API key format. Allowed chars: letters, numbers, dot, underscore, hyphen."
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
            "Content-Type" = "application/json"
            "Authorization" = "Bearer $ApiKey"
        }

        $null = Invoke-RestMethod -Uri $uri -Method Get -Headers $headers -ErrorAction Stop
        Write-Success "API connection successful: $uri"
        return $true
    }
    catch {
        if ($_.Exception.Response -and $_.Exception.Response.StatusCode -eq 401) {
            Write-Error "API key authentication failed (401)."
        } elseif ($_.Exception.Message -like "*Unable to connect*" -or $_.Exception.Message -like "*could not be resolved*") {
            Write-Error "Cannot connect to API server. Check BaseUrl and network."
        } else {
            Write-Error "API test failed: $($_.Exception.Message)"
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

        # 同时写入环境变量，兼容依赖 env 的场景
        [Environment]::SetEnvironmentVariable("OPENAI_API_KEY", $ApiKey, [EnvironmentVariableTarget]::User)
        [Environment]::SetEnvironmentVariable("CODEX_API_KEY", $ApiKey, [EnvironmentVariableTarget]::User)
        $env:OPENAI_API_KEY = $ApiKey
        $env:CODEX_API_KEY = $ApiKey

        Write-Success "Environment variables OPENAI_API_KEY and CODEX_API_KEY set successfully"
        return $true
    }
    catch {
        Write-Error "Failed to create configuration: $($_.Exception.Message)"
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
        Ensure-Codex | Out-Null
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
                Write-Warning "API key is required"
            } elseif (-not (Test-ApiKey $ApiKey)) {
                $ApiKey = ""
            }
        }
    }

    if ([string]::IsNullOrWhiteSpace($BaseUrl) -or [string]::IsNullOrWhiteSpace($ApiKey)) {
        Write-Error "Both BaseUrl and ApiKey are required"
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

        $codexCmd = Get-Command codex -ErrorAction SilentlyContinue
        if ($codexCmd) {
            Write-Info "Run 'codex --version' or start codex in a new terminal to verify."
        }

        Write-Host ""
        Show-Settings
    } else {
        Write-Error "Failed to write settings"
    }
}

Main
