param(
    [ValidateSet('smoke','full')][string]$Profile = 'smoke',
    [string]$OutputDir = '',
    [string]$BaseUrl = '',
    [string]$Addr = '127.0.0.1:8080',
    [string]$StaticDir = '',
    [long]$MaxBodyBytes = 1048576,
    [string]$Secret = 'known-k6-secret',
    [int]$ShutdownSeconds = 5,
    [string]$Scenario = 'contract_smoke'
)

$ErrorActionPreference = 'Stop'
$moduleRoot = (Resolve-Path $PSScriptRoot).Path
if (-not $OutputDir) { $OutputDir = Join-Path $moduleRoot ("results\" + (Get-Date -Format 'yyyyMMdd-HHmmss')) }
$OutputDir = [IO.Path]::GetFullPath($OutputDir)
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
$binDir = Join-Path $OutputDir 'bin'; New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$serverExe = Join-Path $binDir 'server.exe'; $rawprobeExe = Join-Path $binDir 'rawprobe.exe'; $resourcesExe = Join-Path $binDir 'resources.exe'
$summary = [ordered]@{ profile=$Profile; scenario=$Scenario; started_at=(Get-Date).ToString('o'); stages=[ordered]@{}; cleanup=[ordered]@{}; exit_code=0 }
$serverProcess = $null
$shutdownFile = Join-Path $OutputDir 'server.shutdown'
$stageExitCode = 2

function Invoke-Stage([string]$Name, [scriptblock]$Action) {
    $start = Get-Date
    try { & $Action; $summary.stages[$Name] = @{ status='passed'; duration_ms=[int]((Get-Date)-$start).TotalMilliseconds } }
    catch { $summary.stages[$Name] = @{ status='failed'; duration_ms=[int]((Get-Date)-$start).TotalMilliseconds; error=$_.Exception.Message }; throw }
}

try {
    if ($MaxBodyBytes -le 0 -or $ShutdownSeconds -le 0 -or $Scenario -notmatch '^[A-Za-z0-9_-]+$') { throw 'invalid runner arguments' }
    $scenarioPath=Join-Path $moduleRoot ("scenarios\$Scenario.js"); if (-not (Test-Path $scenarioPath)) { throw "scenario not found: $Scenario" }
    $stageExitCode = 3
    Invoke-Stage 'build' {
        Push-Location $moduleRoot
        try { & go build -o $serverExe ./cmd/server *> (Join-Path $OutputDir 'build-server.log'); if ($LASTEXITCODE) { throw "server build exit $LASTEXITCODE" }; & go build -o $rawprobeExe ./cmd/rawprobe *> (Join-Path $OutputDir 'build-rawprobe.log'); if ($LASTEXITCODE) { throw "rawprobe build exit $LASTEXITCODE" }; & go build -o $resourcesExe ./cmd/resources *> (Join-Path $OutputDir 'build-resources.log'); if ($LASTEXITCODE) { throw "resources build exit $LASTEXITCODE" } } finally { Pop-Location }
    }
    $serverOut = Join-Path $OutputDir 'server.stdout.log'; $serverErr = Join-Path $OutputDir 'server.stderr.log'
    $serverArgs = @('-addr',$Addr,'-max-body-bytes',[string]$MaxBodyBytes,'-secret',$Secret,'-shutdown-timeout',("${ShutdownSeconds}s"),'-shutdown-file',$shutdownFile)
    if ($StaticDir) { $serverArgs += @('-static-dir',[IO.Path]::GetFullPath($StaticDir)) }
    Invoke-Stage 'server_start' { $script:serverProcess = Start-Process -FilePath $serverExe -ArgumentList $serverArgs -RedirectStandardOutput $serverOut -RedirectStandardError $serverErr -PassThru -WindowStyle Hidden }
    Invoke-Stage 'ready' {
        $deadline=(Get-Date).AddSeconds(15); $ready=''
        while ((Get-Date) -lt $deadline) { if ($serverProcess.HasExited) { throw "server exited $($serverProcess.ExitCode)" }; if (Test-Path $serverOut) { $ready=(Get-Content $serverOut | Where-Object { $_ -like 'READY http*' } | Select-Object -Last 1) }; if ($ready) { break }; Start-Sleep -Milliseconds 100 }
        if (-not $ready) { throw 'READY line not observed' }
        $script:readyUrl=$ready.Substring(6).TrimEnd('/')
        if ($BaseUrl -and $BaseUrl.TrimEnd('/') -ne $readyUrl) { throw "BaseUrl $BaseUrl does not match READY $readyUrl" }
        $script:effectiveUrl=$readyUrl
        $health=Invoke-WebRequest -UseBasicParsing -TimeoutSec 5 -Uri "$effectiveUrl/health"; if ($health.StatusCode -ne 200) { throw "health status $($health.StatusCode)" }
    }
    $stageExitCode = 6
    Invoke-Stage 'resources' { & $resourcesExe -output (Join-Path $OutputDir 'resources.json') *> (Join-Path $OutputDir 'resources.log'); if ($LASTEXITCODE) { throw "resources exit $LASTEXITCODE" } }
    $stageExitCode = 4
    Invoke-Stage 'k6' {
        $env:BASE_URL=$effectiveUrl; $env:PROFILE=$Profile
        & k6 run --summary-export (Join-Path $OutputDir 'k6-summary.json') $scenarioPath *> (Join-Path $OutputDir 'k6.log')
        if ($LASTEXITCODE) { throw "k6 exit $LASTEXITCODE" }
    }
    $stageExitCode = 5
    Invoke-Stage 'rawprobe' {
        $uri=[Uri]$readyUrl; $probeAddr="$($uri.Host):$($uri.Port)"
        & $rawprobeExe -addr $probeAddr -secret $Secret -health-url "$effectiveUrl/health" -metrics-url "$effectiveUrl/__test/metrics" -output (Join-Path $OutputDir 'rawprobe.json') *> (Join-Path $OutputDir 'rawprobe.log')
        if ($LASTEXITCODE) { throw "rawprobe exit $LASTEXITCODE" }
    }
} catch { $summary.exit_code = $stageExitCode; Write-Error $_ -ErrorAction Continue }
finally {
    if ($serverProcess -and -not $serverProcess.HasExited) {
		Set-Content -Path $shutdownFile -Value 'shutdown' -Encoding ASCII
		$summary.cleanup.shutdown_file = $shutdownFile
        $summary.cleanup.graceful_wait_ms = $ShutdownSeconds * 1000
        if (-not $serverProcess.WaitForExit($ShutdownSeconds * 1000)) {
            $summary.cleanup.forced = $true
            Stop-Process -Id $serverProcess.Id -Force
            if ($summary.exit_code -eq 0) { $summary.exit_code = 7 }
        } else { $summary.cleanup.forced = $false }
    }
    $summary.finished_at=(Get-Date).ToString('o'); $summary.server_url=$readyUrl
    $summary | ConvertTo-Json -Depth 8 | Set-Content -Encoding UTF8 (Join-Path $OutputDir 'summary.json')
}
exit [int]$summary.exit_code
