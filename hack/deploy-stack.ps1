[CmdletBinding()]
# AiFerry 生产栈安全发布脚本。
#
# 背景（2026-10-29 事故复盘）：直接 PUT 栈更新时，Portainer 会先删除旧容器再创建新容器；
# 一旦提交内容有错（当时把 stack.env 写成了只有一行「=」），新容器起不来，站点整站 502，
# 中断约 1 小时 15 分。宿主机还叠加了 Docker 网络残留（failed to add bridge … to ipset: exist）。
#
# 本脚本把「失败」的代价压到秒级，做法是：
#   1. 发布前读取并备份当前 compose 与全部环境变量（只落本地，不打印值）
#   2. 严格校验环境变量非空名、compose 结构合法，避免把可预见错误带上线
#   3. 基线漂移检测：线上版本不是预期值就立即中止，不覆盖别人的改动
#   4. 发布后等待 healthz 达到目标 version；栈明确失败就立即回滚，不等超时
#   5. 回滚只重放备份内容，不依赖人工记忆
#
# 用法：
#   .\hack\deploy-stack.ps1 -Version 0.6.52 -BaselineVersion 0.6.49 -DryRun
#   .\hack\deploy-stack.ps1 -Version 0.6.52 -BaselineVersion 0.6.49
param(
  [Parameter(Mandatory = $true)][string]$Version,
  [Parameter(Mandatory = $true)][string]$BaselineVersion,
  [string]$StackId = '22',
  [string]$EndpointId = '10',
  [string]$HealthUrl = 'https://aiferry.yunloli.cn/healthz',
  [string]$ImageRepo = 'ghcr.io/ayflying/aiferry',
  [string]$BackupDir = '',
  [int]$VerifyTimeoutSeconds = 600,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "目标版本格式无效：$Version" }
if ($BaselineVersion -notmatch '^\d+\.\d+\.\d+$') { throw "基线版本格式无效：$BaselineVersion" }

$apiKey = [Environment]::GetEnvironmentVariable('PORTAINER_API_KEY', 'User')
$apiBase = [Environment]::GetEnvironmentVariable('PORTAINER_URL', 'User')
if (-not $apiKey) { throw '缺少用户级环境变量 PORTAINER_API_KEY。' }
if (-not $apiBase) { throw '缺少用户级环境变量 PORTAINER_URL。' }
$apiBase = $apiBase.TrimEnd('/')
if (-not $BackupDir) { $BackupDir = Join-Path ([IO.Path]::GetTempPath()) 'aiferry-stack-backup' }
if (-not (Test-Path $BackupDir)) { New-Item -ItemType Directory -Path $BackupDir -Force | Out-Null }

# 通过 curl 调 Portainer：Invoke-RestMethod 在这套环境里对慢响应更容易超时，
# 且需要拿到原始响应体判断成功与否。
function Invoke-Portainer {
  param(
    [Parameter(Mandatory = $true)][string]$Method,
    [Parameter(Mandatory = $true)][string]$Path,
    [string]$BodyFile = '',
    [int]$TimeoutSeconds = 600
  )
  $curlArgs = @('--max-time', "$TimeoutSeconds", '-sS', '-w', "`n%{http_code}", '-X', $Method,
    '-H', "X-API-Key: $apiKey")
  if ($BodyFile) { $curlArgs += @('-H', 'Content-Type: application/json', '--data-binary', "@$BodyFile") }
  $curlArgs += "$apiBase$Path"
  # Portainer 会间歇性无响应（2026-10-29 实测 40～120 秒超时），按退避重试。
  $attempt = 0
  $raw = $null
  while ($true) {
    $attempt++
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $raw = & curl.exe @curlArgs 2>$null
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $prev
    if ($exitCode -eq 0) { break }
    if ($attempt -ge 3) { throw "Portainer 请求失败（$Method $Path），curl 退出码 $exitCode，已重试 $attempt 次。" }
    Start-Sleep -Seconds (5 * $attempt)
  }
  $text = ($raw -join "`n")
  $index = $text.LastIndexOf("`n")
  $statusCode = 0
  if ($index -ge 0) {
    [int]::TryParse($text.Substring($index + 1).Trim(), [ref]$statusCode) | Out-Null
    $text = $text.Substring(0, $index)
  }
  return [pscustomobject]@{ StatusCode = $statusCode; Body = $text }
}

function Read-Health {
  param([int]$TimeoutSeconds = 15)
  $prev = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  $raw = & curl.exe --max-time $TimeoutSeconds -sS $HealthUrl 2>$null
  $exitCode = $LASTEXITCODE
  $ErrorActionPreference = $prev
  if ($exitCode -ne 0 -or -not $raw) { return $null }
  try { return (($raw -join '') | ConvertFrom-Json) } catch { return $null }
}

# 环境变量必须是「非空名 + 任意值」：空名会被写成一行「=」，compose 直接拒绝启动。
# 这条规则就是 2026-10-29 事故的直接原因，必须挡住。
function Assert-EnvEntries {
  param($Entries)
  $list = @()
  if ($null -ne $Entries) { $list = @($Entries) }
  if ($list.Count -eq 0) { throw '栈环境变量为空，拒绝发布：空 env 会覆盖 stack.env，导致应用缺少数据库与主密钥配置。' }
  $index = 0
  foreach ($item in $list) {
    $index++
    if (-not ($item.PSObject.Properties.Name -contains 'name')) { throw "第 $index 项环境变量缺少 name 字段，拒绝发布。" }
    if ([string]$item.name -notmatch '^[A-Za-z_][A-Za-z0-9_]*$') { throw "第 $index 项环境变量名为空或非法，拒绝发布。" }
  }
  return $list
}

# compose 结构校验：只允许改镜像标签，其它形态异常一律拒绝。
function Assert-Compose {
  param([string]$Compose)
  if (-not $Compose) { throw '栈 compose 内容为空。' }
  $lines = $Compose -split "`n"
  if (@($lines | Where-Object { $_ -match '^services:\s*$' }).Count -lt 1) { throw 'compose 缺少 services 段。' }
  $imageLines = @($lines | Where-Object { $_ -match '^\s*image:\s*\S' })
  if ($imageLines.Count -ne 1) { throw "compose 中 image 行数异常（$($imageLines.Count)），拒绝发布。" }
  if (@($lines | Where-Object { $_ -match '^=' }).Count -gt 0) { throw 'compose 内出现孤立的等号行。' }
  if ($imageLines[0] -notmatch ([regex]::Escape($ImageRepo) + ':[0-9]+\.[0-9]+\.[0-9]+')) {
    throw "compose 镜像不是 $ImageRepo 的固定版本标签：$($imageLines[0].Trim())"
  }
}

# 读取栈部署详情：部署中的条目可能没有 Message 字段，必须按属性存在性判断，
# 否则 StrictMode 会直接抛异常，把「回滚」这一步跳过去（2026-10-29 实际踩到）。
function Write-DeploymentDetail {
  param($Stack)
  foreach ($item in @($Stack.DeploymentStatus)) {
    $text = ''
    if ($item.PSObject.Properties.Name -contains 'Message') { $text = [string]$item.Message }
    if ($text) { Write-Warning "部署详情：$text" }
  }
}

$live = Read-Health
if (-not $live) { throw "无法读取线上健康状态（$HealthUrl），发布前必须先确认可用性。" }
Write-Host "线上基线：version=$($live.version) revision=$($live.revision)"
if ($live.version -ne $BaselineVersion) {
  throw "基线漂移：线上是 $($live.version)，脚本要求 $BaselineVersion。已中止，避免覆盖他人发布。"
}

$stackResp = Invoke-Portainer -Method GET -Path "/api/stacks/$StackId" -TimeoutSeconds 120
if ($stackResp.StatusCode -ne 200) { throw "读取栈 $StackId 失败，HTTP $($stackResp.StatusCode)。" }
$stack = $stackResp.Body | ConvertFrom-Json
$env = Assert-EnvEntries -Entries $stack.Env
$envNames = ($env | ForEach-Object { [string]$_.name }) -join ','
Write-Host "栈 $StackId：环境变量 $($env.Count) 项（$envNames）"

$fileResp = Invoke-Portainer -Method GET -Path "/api/stacks/$StackId/file" -TimeoutSeconds 120
if ($fileResp.StatusCode -ne 200) { throw "读取栈文件失败，HTTP $($fileResp.StatusCode)。" }
$compose = ($fileResp.Body | ConvertFrom-Json).StackFileContent
Assert-Compose -Compose $compose
$imageLines = @(($compose -split "`n") | Where-Object { $_ -match '^\s*image:\s*\S' })
$currentImage = $imageLines[0].Trim()
Write-Host "当前镜像：$currentImage"

$targetImage = "${ImageRepo}:$Version"
$newCompose = [regex]::Replace($compose, ('(?m)^(\s*image:\s*)' + [regex]::Escape($ImageRepo) + ':[^\s]+'), ('${1}' + $targetImage))

# 备份：compose 与 env 都落盘，回滚时原样重放。
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$backupPayload = Join-Path $BackupDir "payload-$stamp.json"
$rollbackBody = @{ stackFileContent = $compose; env = $env; prune = $false; pullImage = $false } | ConvertTo-Json -Depth 20
[IO.File]::WriteAllText($backupPayload, $rollbackBody, (New-Object Text.UTF8Encoding($false)))
Write-Host "已备份当前栈到 $backupPayload"

if ($currentImage -eq "image: $targetImage") {
  Write-Host '栈已经是目标镜像，无需更新。'
  exit 0
}

if ($DryRun) {
  Write-Host '[DryRun] 校验通过：环境变量与 compose 结构合法，基线一致。'
  Write-Host "[DryRun] 将把 $currentImage 改为 image: $targetImage"
  Write-Host "[DryRun] 发布后按 $HealthUrl 的 version=$Version 判定，失败自动回滚。"
  exit 0
}

$deployPayload = Join-Path $BackupDir "deploy-$stamp.json"
$deployBody = @{ stackFileContent = $newCompose; env = $env; prune = $false; pullImage = $true } | ConvertTo-Json -Depth 20
[IO.File]::WriteAllText($deployPayload, $deployBody, (New-Object Text.UTF8Encoding($false)))

Write-Host "开始发布 $Version ..."
$putResp = Invoke-Portainer -Method PUT -Path "/api/stacks/$StackId`?endpointId=$EndpointId" -BodyFile $deployPayload -TimeoutSeconds 900
Write-Host "栈更新提交：HTTP $($putResp.StatusCode)"

# 宿主机拉镜像可能很慢（2026-10-29 实测数分钟），因此按栈状态区分「仍在部署」与「已失败」：
# 仍在部署就继续等，明确失败立即回滚，避免空等把中断时间拖长。
$deadline = (Get-Date).AddSeconds($VerifyTimeoutSeconds)
$healthy = $false
$seen = $null
while ((Get-Date) -lt $deadline) {
  $seen = Read-Health
  if ($seen -and $seen.status -eq 'ok' -and $seen.version -eq $Version) { $healthy = $true; break }
  $probe = Invoke-Portainer -Method GET -Path "/api/stacks/$StackId" -TimeoutSeconds 90
  if ($probe.StatusCode -eq 200) {
    $state = $probe.Body | ConvertFrom-Json
    if ([int]$state.Status -eq 4) { break }
  }
  Start-Sleep -Seconds 3
}

if ($healthy) {
  Write-Host "发布成功：version=$($seen.version) revision=$($seen.revision)"
  exit 0
}

Write-Warning "发布后未达到 version=$Version，开始自动回滚。"
$statusResp = Invoke-Portainer -Method GET -Path "/api/stacks/$StackId" -TimeoutSeconds 120
if ($statusResp.StatusCode -eq 200) { Write-DeploymentDetail -Stack ($statusResp.Body | ConvertFrom-Json) }

$rollbackResp = Invoke-Portainer -Method PUT -Path "/api/stacks/$StackId`?endpointId=$EndpointId" -BodyFile $backupPayload -TimeoutSeconds 900
Write-Host "回滚提交：HTTP $($rollbackResp.StatusCode)"

$deadline = (Get-Date).AddSeconds($VerifyTimeoutSeconds)
while ((Get-Date) -lt $deadline) {
  $seen = Read-Health
  if ($seen -and $seen.status -eq 'ok' -and $seen.version -eq $BaselineVersion) {
    Write-Host "已回滚到 $BaselineVersion：revision=$($seen.revision)"
    throw "发布 $Version 失败，已回滚到 $BaselineVersion，请检查部署详情后再试。"
  }
  Start-Sleep -Seconds 3
}
throw "发布 $Version 失败，自动回滚后仍未恢复到 $BaselineVersion：需要人工介入。"