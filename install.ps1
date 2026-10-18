$ErrorActionPreference = "Stop"

# ===== 你的仓库 =====
$Repo = "1o7o/boom"
# =====================

$Bin  = "boom"
$Base = "https://v4.gh-proxy.org/https://github.com/$Repo/releases/latest/download"

$archRaw = $env:PROCESSOR_ARCHITEW6432
if ([string]::IsNullOrEmpty($archRaw)) { $archRaw = $env:PROCESSOR_ARCHITECTURE }

switch ($archRaw) {
    "AMD64" { $arch = "amd64" }
    "ARM64" { $arch = "arm64" }
    default { Write-Error "不支持的架构: $archRaw"; exit 1 }
}

$file = "${Bin}_windows_${arch}.exe"
$url  = "$Base/$file"

$destDir = Join-Path $env:LOCALAPPDATA "Programs\$Bin"
New-Item -ItemType Directory -Force -Path $destDir | Out-Null
$dest = Join-Path $destDir "$Bin.exe"

Write-Host "下载 $url"
Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing

Unblock-File -Path $dest -ErrorAction SilentlyContinue

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($null -eq $userPath) { $userPath = "" }
$parts = $userPath -split ';' | Where-Object { $_ -ne "" }
if ($parts -notcontains $destDir) {
    $newPath = ($parts + $destDir) -join ';'
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Host "已把 $destDir 加入用户 PATH（新开终端生效）"
}

Write-Host "✅ 已安装到 $dest"
Write-Host "新开一个终端，运行 $Bin 开始"
