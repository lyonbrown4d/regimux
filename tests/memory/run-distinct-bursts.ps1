param(
    [Parameter(Mandatory)][ValidateSet('postgres','mysql')][string]$Backend,
    [Parameter(Mandatory)][ValidatePattern('^[a-z0-9][a-z0-9-]{0,15}$')][string]$RunId,
    [ValidateRange(1,2048)][int]$Concurrency = 512,
    [ValidatePattern('(?-i)^[a-z0-9][a-z0-9-]*$')][string]$Project = 'regimux-memory-20261021'
)

$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$concurrencyCompose = Join-Path $repositoryRoot 'tests/concurrency/compose.yml'
. (Join-Path $repositoryRoot 'tests/concurrency/container-state.ps1')
$memoryCompose = Join-Path $PSScriptRoot 'compose.yml'
$reports = Join-Path $repositoryRoot 'tmp/memory-20261021'
$network = $Project
$oldReports = $env:REGIMUX_CONCURRENCY_REPORTS
$oldNetwork = $env:REGIMUX_CONCURRENCY_NETWORK
$env:REGIMUX_CONCURRENCY_REPORTS = $reports
$targets = @(
    'oci-manifest','oci-config','oci-layer','go-info','go-mod','go-zip',
    'npm-metadata','npm-tarball','pypi-simple','pypi-wheel',
    'maven-pom','maven-jar','dist-archive'
)
$services = @(
    "$project-regimux-$Backend-1", "$project-regimux-$Backend-peer-1",
    "$project-fixture-1", "$project-redis-1", "$project-$Backend-1",
    "$project-s3-1", "$project-s3-proxy-1"
)
$baseURLs = 'http://regimux:8080,http://regimux-peer:8080'

function Get-ContainerSamples([string[]]$ContainerNames) {
    $startInfo = [System.Diagnostics.ProcessStartInfo]::new('docker')
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    foreach ($argument in (@('stats','--no-stream','--format','{{json .}}') + $ContainerNames)) {
        $startInfo.ArgumentList.Add($argument)
    }
    $samplingProcess = [System.Diagnostics.Process]::new()
    $samplingProcess.StartInfo = $startInfo
    try {
        if (-not $samplingProcess.Start()) { throw 'Cannot start Docker resource sampler' }
        $outputTask = $samplingProcess.StandardOutput.ReadToEndAsync()
        $errorTask = $samplingProcess.StandardError.ReadToEndAsync()
        if (-not $samplingProcess.WaitForExit(10000)) {
            if (-not $samplingProcess.HasExited) { $samplingProcess.Kill($true) }
            $samplingProcess.WaitForExit()
            Write-Warning 'Docker resource sample timed out; preserving the missing sample.'
            return
        }
        if ($samplingProcess.ExitCode -ne 0) {
            Write-Warning $errorTask.GetAwaiter().GetResult()
            return
        }
        $outputTask.GetAwaiter().GetResult() -split '\r?\n' | Where-Object { $_.Length -gt 0 }
    } finally {
        $samplingProcess.Dispose()
    }
}

function Save-FinalState([string[]]$ContainerNames, [string]$Path) {
    foreach ($name in $ContainerNames) {
        $state = docker inspect --format '{"Name":{{json .Name}},"Image":{{json .Image}},"State":{{json .State}},"RestartCount":{{.RestartCount}}}' $name 2>$null
        if ($LASTEXITCODE -eq 0) {
            $state | Add-Content -LiteralPath $Path
        } else {
            @{ name = $name; error = 'container state unavailable' } | ConvertTo-Json -Compress | Add-Content -LiteralPath $Path
        }
    }
}

try {
    New-Item -ItemType Directory -Path $reports -Force | Out-Null
    $env:REGIMUX_CONCURRENCY_NETWORK = $network
    foreach ($target in $targets) {
        $report = "memory-$RunId-$Backend-$target"
        $jsonPath = Join-Path $reports "$report.json"
        $logPath = Join-Path $reports "$report.log"
        $resourcesPath = Join-Path $reports "$report-resources.jsonl"
        $statePath = Join-Path $reports "$report-final-state.jsonl"
        if (Test-Path -LiteralPath $jsonPath) { throw "Report already exists: $jsonPath" }
        if (Test-Path -LiteralPath $logPath) { throw "Log already exists: $logPath" }
        if (Test-Path -LiteralPath $resourcesPath) { throw "Resource samples already exist: $resourcesPath" }
        if (Test-Path -LiteralPath $statePath) { throw "Container state already exists: $statePath" }

        $case = "$RunId-$Backend-distinct-$target"
        $wavePrefix = "$RunId-$Backend-$target"
        $client = "$project-$case"
        $sampleNames = $services + @($client)
        $composeArgs = @(
            'compose','-p',$Project,'-f',$concurrencyCompose,'-f',$memoryCompose,
            '--profile','clients','run','-d','--no-deps','--name',$client,
            '-e',"DISTINCT_CONCURRENCY=$Concurrency",
            '-e',"DISTINCT_TARGET=$target",
            '-e',"WAVE_PREFIX=$wavePrefix",
            '-e',"REPORT=$report",
            '-e',"BASE_URLS=$baseURLs",
            'client'
        )
        $start = [DateTimeOffset]::UtcNow
        Write-Output "Running ${target}: $Concurrency VUs across both RegiMux replicas"
        $containerId = & docker @composeArgs
        if ($LASTEXITCODE -ne 0) { throw "Cannot start distinct-key case $target" }

        $timedOut = $false
        do {
            $stamp = [DateTimeOffset]::UtcNow.ToString('o')
            $samples = @(Get-ContainerSamples -ContainerNames $sampleNames)
            if ($samples.Count -eq 0) {
                @{ time = $stamp; error = 'Docker resource sample unavailable' } | ConvertTo-Json -Compress | Add-Content -LiteralPath $resourcesPath
            }
            foreach ($sample in $samples) {
                @{ time = $stamp; stats = ($sample | ConvertFrom-Json) } | ConvertTo-Json -Depth 4 -Compress | Add-Content -LiteralPath $resourcesPath
            }
            $running = docker inspect --format '{{.State.Running}}' $client
            if ($LASTEXITCODE -ne 0) { throw "Cannot inspect distinct-key case $target" }
            if (([DateTimeOffset]::UtcNow - $start).TotalSeconds -gt 210) {
                docker stop --time 5 $client | Out-Null
                $timedOut = $true
                break
            }
            if ($running -eq 'true') { Start-Sleep -Seconds 1 }
        } while ($running -eq 'true')

        docker logs $client 2>&1 | Out-File -LiteralPath $logPath -Encoding utf8
        Save-FinalState -ContainerNames $sampleNames -Path $statePath
        $exitCode = docker inspect --format '{{.State.ExitCode}}' $client
        if ($LASTEXITCODE -ne 0) { throw "Cannot read exit status for $target" }
        docker rm $client | Out-Null
        if (-not (Test-Path -LiteralPath $jsonPath)) { throw "k6 did not write expected report: $jsonPath" }
        Write-Output "$target exit=$exitCode elapsed=$([Math]::Round(([DateTimeOffset]::UtcNow-$start).TotalSeconds,1))s"
        if ($timedOut) { throw "Distinct-key case exceeded harness deadline: $target; inspect retained reports" }
        if ($exitCode -ne '0') { throw "Distinct-key assertions failed: $target; inspect retained reports" }
        Assert-ContainerRunningWithoutRestart -ContainerNames $services
    }
    Write-Output "All 13 distinct-key bursts passed for $Backend; reports and resource samples: $reports"
} finally {
    if ($null -eq $oldReports) { Remove-Item Env:REGIMUX_CONCURRENCY_REPORTS -ErrorAction SilentlyContinue } else { $env:REGIMUX_CONCURRENCY_REPORTS = $oldReports }
    if ($null -eq $oldNetwork) { Remove-Item Env:REGIMUX_CONCURRENCY_NETWORK -ErrorAction SilentlyContinue } else { $env:REGIMUX_CONCURRENCY_NETWORK = $oldNetwork }
}
