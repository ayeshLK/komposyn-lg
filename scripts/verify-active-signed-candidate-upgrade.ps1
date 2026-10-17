# Copyright 2026 Ayesh Almeida
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

param(
    [string]$OldTag = 'v0.6.0',
    [Parameter(Mandatory=$true)][string]$CandidateDir
)

$ErrorActionPreference = 'Stop'
$repo = if ($env:KOMPOSYN_REPO) { $env:KOMPOSYN_REPO } else { 'ayeshLK/komposyn' }
$identity = '^https://github.com/ayeshLK/komposyn/.github/workflows/release.yml@refs/heads/master$'
$candidateIdentity = '^https://github.com/ayeshLK/komposyn-lg/.github/workflows/release.yml@refs/heads/master$'
$issuer = 'https://token.actions.githubusercontent.com'
foreach ($tool in @('gh', 'cosign')) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is required" }
}

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
$oldDir = Join-Path $temp 'old-release'
$installDir = Join-Path $temp 'bin'
$dataDir = Join-Path $temp 'data'
$sourceDir = Join-Path $temp 'source'
$binary = Join-Path $installDir 'komposyn-lg.exe'
$candidateDir = (Resolve-Path -LiteralPath $CandidateDir).Path
$target = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'windows_arm64' } else { 'windows_amd64' }
$candidateVersion = ((Get-Content (Join-Path $candidateDir 'SHA256SUMS') | Select-Object -First 1) -replace '^# version=v?','').Trim()
New-Item -ItemType Directory -Path $oldDir,$installDir,$dataDir,$sourceDir -Force | Out-Null
function Invoke-CLI([string[]]$Arguments) {
    $output = & $script:binary @Arguments
    if ($LASTEXITCODE -ne 0) { throw "komposyn-lg $($Arguments -join ' ') failed with exit $LASTEXITCODE`n$($output -join "`n")" }
    return $output
}
function Get-UsageSignature([string]$Json) {
    $summary = ($Json | ConvertFrom-Json).summary
    return ($summary | Select-Object request_count,input_tokens,cached_input_tokens,cache_write_input_tokens,output_tokens,reasoning_output_tokens,total_tokens | ConvertTo-Json -Compress)
}
try {
    $candidateAsset = "komposyn-lg_${candidateVersion}_${target}.zip"
    foreach ($name in @('SHA256SUMS','SHA256SUMS.sig','SHA256SUMS.pem',"$candidateAsset","$candidateAsset.sig","$candidateAsset.pem")) {
        if (-not (Test-Path (Join-Path $candidateDir $name))) { throw "missing signed candidate file $name" }
    }
    foreach ($signed in @('SHA256SUMS',$candidateAsset)) {
        & cosign verify-blob --signature (Join-Path $candidateDir "$signed.sig") --certificate (Join-Path $candidateDir "$signed.pem") --certificate-identity-regexp $candidateIdentity --certificate-oidc-issuer $issuer (Join-Path $candidateDir $signed)
        if ($LASTEXITCODE -ne 0) { throw "candidate signature rejected: $signed" }
    }
    $candidateHash = ((Get-Content (Join-Path $candidateDir 'SHA256SUMS') | Where-Object { $_ -match "  $([regex]::Escape($candidateAsset))$" }) -split '\s+')[0]
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $candidateDir $candidateAsset)).Hash.ToLowerInvariant() -ne $candidateHash.ToLowerInvariant()) { throw 'candidate checksum mismatch' }
    $oldAsset = "komposyn-lg_$($OldTag.TrimStart('v'))_${target}.zip"
    & gh release download $OldTag --repo $repo --dir $oldDir --pattern $oldAsset --pattern SHA256SUMS --pattern SHA256SUMS.sig --pattern SHA256SUMS.pem --pattern "$oldAsset.sig" --pattern "$oldAsset.pem"
    if ($LASTEXITCODE -ne 0) { throw "could not download signed historical release $OldTag" }
    $expected = ((Get-Content (Join-Path $oldDir 'SHA256SUMS') | Where-Object { $_ -match "  $([regex]::Escape($oldAsset))$" }) -split '\s+')[0]
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $oldDir $oldAsset)).Hash.ToLowerInvariant()
    if ($expected.ToLowerInvariant() -ne $actual) { throw "historical archive checksum mismatch: $OldTag" }
    & cosign verify-blob --signature (Join-Path $oldDir "$oldAsset.sig") --certificate (Join-Path $oldDir "$oldAsset.pem") --certificate-identity-regexp $identity --certificate-oidc-issuer $issuer (Join-Path $oldDir $oldAsset)
    if ($LASTEXITCODE -ne 0) { throw "historical archive signature rejected: $OldTag" }
    & cosign verify-blob --signature (Join-Path $oldDir 'SHA256SUMS.sig') --certificate (Join-Path $oldDir 'SHA256SUMS.pem') --certificate-identity-regexp $identity --certificate-oidc-issuer $issuer (Join-Path $oldDir 'SHA256SUMS')
    if ($LASTEXITCODE -ne 0) { throw "historical checksum manifest signature rejected: $OldTag" }
    Expand-Archive -LiteralPath (Join-Path $oldDir $oldAsset) -DestinationPath $oldDir
    Copy-Item -LiteralPath (Join-Path $oldDir 'komposyn-lg.exe') -Destination $binary

    $fixture = Join-Path $PSScriptRoot '..\plugins\library\token-logger\testdata\codex\normal.jsonl'
    Copy-Item -LiteralPath $fixture -Destination (Join-Path $sourceDir 'normal.jsonl')
    $env:TOKEN_LOGGER_DATA_DIR = $dataDir
    Invoke-CLI @('settings','--collector','off','--auto-start','off') | Out-Null
    Invoke-CLI @('watch','add',$sourceDir) | Out-Null
    Invoke-CLI @('import',(Join-Path $sourceDir 'normal.jsonl'),'--format','json') | Out-Null
    @'
{"protocol":"komposyn.token-logger","version":1,"id":"init","method":"initialize","params":{}}
{"protocol":"komposyn.token-logger","version":1,"id":"record","method":"record","params":{"event":{"event_id":"issue-46-active-id","schema_version":1,"event_type":"model.request","occurred_at":"2026-10-11T12:00:00Z","project_id":"issue-46-active-project","session_id":"issue-46-active-session","client":{"name":"issue-46-active"},"provider":"openai","requested_model":"gpt-test","usage":{"input_tokens":7,"output_tokens":3},"provenance":{"adapter":"fixture","source_id":"issue-46-active","source_record":"1"},"ingestion_key":"issue-46:active-id"},"project":{"id":"issue-46-active-project","root":"/issue-46-active-fixture","display_name":"Issue 46 active fixture"},"session":{"id":"issue-46-active-session","project_id":"issue-46-active-project","client":{"name":"issue-46-active"}}}}
{"protocol":"komposyn.token-logger","version":1,"id":"stop","method":"shutdown"}
'@ | & $binary harness | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'could not seed stable identities through protocol v1' }
    @'
import hashlib, sqlite3, sys
db = sqlite3.connect(sys.argv[1])
f = hashlib.sha256(b'issue-46-active-reviewed-diagnostic').hexdigest()
db.execute("INSERT INTO diagnostics(fingerprint,message,first_seen_at,last_seen_at,status) VALUES(?,?,?,?, 'reviewed')", (f,'legacy sanitized diagnostic','2026-10-11T12:00:00.000000000Z','2026-10-11T12:00:00.000000000Z'))
db.commit()
'@ | py -3 - (Join-Path $dataDir 'token-logger.db')
    if ($LASTEXITCODE -ne 0) { throw 'could not seed reviewed diagnostic state' }
    $before = Get-UsageSignature (Invoke-CLI @('usage','--format','json') | Out-String)
    Invoke-CLI @('watch','start','--interval','1s') | Out-Null
    $oldStatus = $null
    for ($attempt = 0; $attempt -lt 50; $attempt++) {
        $oldStatus = (Invoke-CLI @('watch','status','--format','json') | Out-String) | ConvertFrom-Json
        if ($oldStatus.state -eq 'running' -and $oldStatus.mode -eq 'background') { break }
        Start-Sleep -Milliseconds 100
    }
    if ($oldStatus.state -ne 'running' -or $oldStatus.mode -ne 'background' -or $oldStatus.pid -le 0) { throw "historical collector did not become active: $($oldStatus | ConvertTo-Json -Compress)" }
    $settingsDigest = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $dataDir 'settings.json')).Hash
    $oldPid = [int]$oldStatus.pid
    $oldInstance = [string]$oldStatus.instance_id

    $env:KOMPOSYN_VERSION = "v$candidateVersion"
    $env:KOMPOSYN_INSTALL_DIR = $installDir
    $env:KOMPOSYN_RELEASE_DIR = $candidateDir
    $installerOutput = & (Join-Path $PSScriptRoot 'install.ps1') 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { throw "signature-verifying installer failed: $installerOutput" }
    if ($installerOutput -notmatch 'restarted the background collector after upgrade') { throw "installer did not report background restart: $installerOutput" }
    $newStatus = (Invoke-CLI @('watch','status','--format','json') | Out-String) | ConvertFrom-Json
    if ($newStatus.state -ne 'running' -or $newStatus.mode -ne 'background' -or $newStatus.stale -or $newStatus.pid -le 0) { throw "upgraded collector is unhealthy: $($newStatus | ConvertTo-Json -Compress)" }
    if ($newStatus.pid -eq $oldPid -or $newStatus.instance_id -eq $oldInstance) { throw 'collector did not restart under a new process and lease identity' }
    if (Get-Process -Id $oldPid -ErrorAction SilentlyContinue) { throw 'historical collector process survived cooperative stop' }
    Get-Process -Id $newStatus.pid -ErrorAction Stop | Out-Null
    $after = Get-UsageSignature (Invoke-CLI @('usage','--format','json') | Out-String)
    if ($after -ne $before) { throw "usage totals changed during upgrade: before=$before after=$after" }
    $afterSettings = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $dataDir 'settings.json')).Hash
    if ($afterSettings -ne $settingsDigest) { throw 'settings or consent changed during upgrade' }
    if ((Invoke-CLI @('projects','list','--format','json') | Out-String) -notmatch 'issue-46-active-project') { throw 'project identity was not preserved' }
    if ((Invoke-CLI @('sessions','list','--project','issue-46-active-project','--format','json') | Out-String) -notmatch 'issue-46-active-session') { throw 'session identity was not preserved' }
    $reimport = (Invoke-CLI @('import',(Join-Path $sourceDir 'normal.jsonl'),'--format','json') | Out-String)
    if ($reimport -notmatch '"imported_events"\s*:\s*0' -or $reimport -notmatch '"duplicate_events"\s*:\s*0') { throw "source cursor was not preserved: $reimport" }
    if ((Invoke-CLI @('watch','list','--format','json') | Out-String) -notmatch [regex]::Escape($sourceDir)) { throw 'collector registry was not preserved' }
    @'
import sqlite3, sys
db=sqlite3.connect(f"file:{sys.argv[1]}?mode=ro",uri=True)
rows=db.execute("SELECT pid,instance_id,state,mode FROM collector_runtime").fetchall()
diagnostic=db.execute("SELECT status FROM diagnostics WHERE message='legacy sanitized diagnostic'").fetchone()
stopped=db.execute("SELECT COUNT(*) FROM collector_runs WHERE terminal_state='stopped'").fetchone()[0]
if len(rows)!=1 or rows[0] != (int(sys.argv[2]),sys.argv[3],'running','background'): raise SystemExit(f"unexpected lease owner: {rows!r}")
if diagnostic != ('reviewed',) or stopped < 1: raise SystemExit(f"history/diagnostic not retained: {diagnostic!r}, {stopped}")
'@ | py -3 - (Join-Path $dataDir 'token-logger.db') $newStatus.pid $newStatus.instance_id
    if ($LASTEXITCODE -ne 0) { throw 'lease owner or diagnostic/collector history validation failed' }
    $backup = Get-ChildItem -LiteralPath $dataDir -Filter '.komposyn-pre-migration-*.sqlite' | Select-Object -First 1
    if (-not $backup) { throw 'historical migration did not create a restorable database backup' }
    $backupDigest = (Get-FileHash -Algorithm SHA256 -LiteralPath $backup.FullName).Hash
    $restoreDir = Join-Path $temp 'restore'
    New-Item -ItemType Directory -Path $restoreDir | Out-Null
    Copy-Item -LiteralPath $backup.FullName -Destination (Join-Path $restoreDir 'token-logger.db')
    $env:TOKEN_LOGGER_DATA_DIR = $restoreDir
    $restored = & (Join-Path $oldDir 'komposyn-lg.exe') usage --format json | Out-String
    if ($LASTEXITCODE -ne 0 -or $restored -notmatch 'total_tokens') { throw 'historical executable could not read restored migration backup' }
    $env:TOKEN_LOGGER_DATA_DIR = $dataDir

    $env:TOKEN_LOGGER_DATA_DIR = $dataDir
    $env:KOMPOSYN_INSTALL_DIR = $installDir
    & (Join-Path $PSScriptRoot 'uninstall.ps1')
    if ($LASTEXITCODE -ne 0) { throw 'uninstall failed' }
    if ((Test-Path -LiteralPath $binary) -or -not (Test-Path (Join-Path $dataDir 'token-logger.db')) -or -not (Test-Path (Join-Path $dataDir 'settings.json'))) { throw 'uninstall did not preserve local data or remove the binary' }
    if (-not (Test-Path -LiteralPath $backup.FullName) -or (Get-FileHash -Algorithm SHA256 -LiteralPath $backup.FullName).Hash -ne $backupDigest) { throw 'uninstall removed or changed migration backup' }
    Write-Output "signed $OldTag to standalone v$candidateVersion active-collector upgrade passed on $target"
} finally {
    if ((Test-Path -LiteralPath $binary -PathType Leaf) -and (Test-Path -LiteralPath (Join-Path $dataDir 'token-logger.db'))) {
        $env:TOKEN_LOGGER_DATA_DIR = $dataDir
        try { & $binary watch stop --timeout 10s | Out-Null } catch { }
    }
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
