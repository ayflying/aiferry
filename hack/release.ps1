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

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$status = & git status --porcelain
if ($LASTEXITCODE -ne 0) { throw '无法读取 Git 工作区状态。' }
if ($status) { throw '发布前必须提交或暂存之外的工作区改动，避免将无关文件带入版本构建。' }

$version = (Get-Content -Raw (Join-Path $root 'VERSION')).Trim()
if ($version -notmatch '^\d+\.\d+\.\d+$') {
  throw "VERSION 格式无效：$version"
}

Invoke-External git @('push', 'origin', 'main')

$revision = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw '无法读取发布提交。' }
$stage = "/tmp/aiferry-release-$version"
$archive = Join-Path ([System.IO.Path]::GetTempPath()) "aiferry-$version.tar.gz"

try {
  Invoke-External ssh @($BuildHost, "test ! -e '$stage' && mkdir -p '$stage'")
  Invoke-External git @('archive', '--format=tar.gz', "--output=$archive", 'HEAD')
  Invoke-External scp @($archive, "${BuildHost}:$stage/source.tar.gz")
  Invoke-External ssh @($BuildHost, "tar -xzf '$stage/source.tar.gz' -C '$stage' && rm -f '$stage/source.tar.gz'")
  Invoke-External ssh @($BuildHost, "cd '$stage' && sh hack/release-remote.sh '$version' '$Image' '$DeployDir' '$ComposeProject' '$revision'")
}
finally {
  if (Test-Path $archive) { Remove-Item -Force $archive }
  & ssh $BuildHost "rm -rf '$stage'" 2>$null
}

