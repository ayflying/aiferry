[CmdletBinding()]
param(
  [string]$BuildHost = 'root@192.168.50.217',
  [string]$DeployDir = '/root/aiferry-dev',
  [string]$Image = 'ghcr.io/ayflying/aiferry',
  [string]$ComposeProject = 'aiferry-dev'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-External {
  param(
    [Parameter(Mandatory = $true)][string]$File,
    [Parameter(Mandatory = $true)][string[]]$Arguments
  )

  & $File @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "命令执行失败：$File $($Arguments -join ' ')"
  }
}

function Write-Utf8File {
  param([string]$Path, [string]$Content)
  [System.IO.File]::WriteAllText($Path, $Content, [System.Text.UTF8Encoding]::new($false))
}

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$status = & git status --porcelain
if ($LASTEXITCODE -ne 0) { throw '无法读取 Git 工作区状态。' }
if ($status) { throw '发布前必须提交或暂存之外的工作区改动，避免将无关文件带入版本构建。' }

$current = (Get-Content -Raw (Join-Path $root 'VERSION')).Trim()
if ($current -notmatch '^(\d+)\.(\d+)\.(\d+)$') {
  throw "VERSION 格式无效：$current"
}

$next = '{0}.{1}.{2}' -f $Matches[1], $Matches[2], ([int]$Matches[3] + 1)
Write-Utf8File (Join-Path $root 'VERSION') "$next`n"

$examplePath = Join-Path $root '.env.example'
$example = Get-Content -Raw $examplePath
$updatedExample = [regex]::Replace($example, '(?m)^AIFERRY_VERSION=.*$', "AIFERRY_VERSION=$next")
if ($updatedExample -eq $example) {
  throw '.env.example 缺少 AIFERRY_VERSION。'
}
Write-Utf8File $examplePath $updatedExample

Invoke-External git @('add', 'VERSION', '.env.example')
Invoke-External git @(
  'commit',
  '-m', "发布：AiFerry $next",
  '-m', "递增根目录 VERSION 至 $next，并同步镜像运行环境的示例版本。",
  '-m', '该发布将在远程构建服务器构建并推送同版本标签及 latest 到 GitHub Container Registry。'
)
Invoke-External git @('push', 'origin', 'main')

$revision = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw '无法读取发布提交。' }
$stage = "/tmp/aiferry-release-$next"
$archive = Join-Path ([System.IO.Path]::GetTempPath()) "aiferry-$next.tar.gz"

try {
  Invoke-External ssh @($BuildHost, "test ! -e '$stage' && mkdir -p '$stage'")
  Invoke-External git @('archive', '--format=tar.gz', "--output=$archive", 'HEAD')
  Invoke-External scp @($archive, "${BuildHost}:$stage/source.tar.gz")
  Invoke-External ssh @($BuildHost, "tar -xzf '$stage/source.tar.gz' -C '$stage' && rm -f '$stage/source.tar.gz'")
  Invoke-External ssh @($BuildHost, "cd '$stage' && sh hack/release-remote.sh '$next' '$Image' '$DeployDir' '$ComposeProject' '$revision'")
}
finally {
  if (Test-Path $archive) { Remove-Item -Force $archive }
  & ssh $BuildHost "rm -rf '$stage'" 2>$null
}
