param(
    [Parameter(Mandatory)][ValidateSet('sqlite','postgres','mysql')][string]$Backend,
    [ValidateRange(1,2048)][int]$ColdVUs = 512,
    [ValidatePattern('^[a-z0-9][a-z0-9-]{0,15}$')][string]$RunId = 'run',
    [ValidatePattern('^[a-z0-9][a-z0-9-]*$')][string]$Project = 'regimux-concurrency-20261016',
    [string]$ReportsPath = 'tmp/concurrency-20261016/reports',
    [switch]$OnlyHot,
    [switch]$SampleOnly
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'container-state.ps1')
$compose = Join-Path $PSScriptRoot 'compose.yml'
$repositoryRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$reports = Join-Path $repositoryRoot $ReportsPath
New-Item -ItemType Directory -Path $reports -Force | Out-Null
$env:REGIMUX_CONCURRENCY_REPORTS = $reports
$project = $Project
$app = "$project-regimux-$Backend-1"
$names = @($app, "$project-fixture-1", "$project-redis-1")
$bases = 'http://regimux:8080'
if ($Backend -ne 'sqlite') {
    $names += @("$project-regimux-$Backend-peer-1", "$project-$Backend-1", "$project-s3-1")
    $bases += ',http://regimux-peer:8080'
}
$targets = @('oci-manifest','oci-config','oci-layer','go-info','go-mod','go-zip','npm-metadata','npm-tarball','pypi-simple','pypi-wheel','maven-pom','maven-jar','dist-archive')
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
if ($SampleOnly) {
    Get-ContainerSamples -ContainerNames $names
    return
}
function Run-Case([string]$Case, [string]$Mode, [int]$VUs, [string]$Target, [string]$Wave, [bool]$Mixed = $false) {
    if (Test-Path -LiteralPath "$reports/$Case.json") { throw "Report already exists: $Case" }
    $client = "$project-$Case"
    $start = [DateTimeOffset]::UtcNow
    $id = docker compose -p $project -f $compose --profile clients run -d --no-deps --name $client -e "MODE=$Mode" -e "CONCURRENCY=$VUs" -e "TARGET=$Target" -e "WAVE=$Wave" -e "BASE_URLS=$bases" -e 'DURATION=30s' -e "REPORT=$Case" -e "MIXED_METHODS=$([int]$Mixed)" client
    if ($LASTEXITCODE -ne 0) { throw "Cannot start $Case" }
    $sampleNames = $names + @($client)
    do {
        $stamp = [DateTimeOffset]::UtcNow.ToString('o')
        $samples = @(Get-ContainerSamples -ContainerNames $sampleNames)
        if ($samples.Count -eq 0) {
            @{time=$stamp; error='Docker resource sample unavailable'} | ConvertTo-Json -Compress | Add-Content -LiteralPath "$reports/$Case-resources.jsonl"
        }
        foreach ($sample in $samples) {
            @{time=$stamp; stats=($sample | ConvertFrom-Json)} | ConvertTo-Json -Depth 4 -Compress | Add-Content -LiteralPath "$reports/$Case-resources.jsonl"
        }
        $running = docker inspect --format '{{.State.Running}}' $client
        if (([DateTimeOffset]::UtcNow - $start).TotalSeconds -gt 200) { throw "Case exceeded harness deadline: $Case" }
    } while ($running -eq 'true')
    docker logs $client 2>&1 | Out-File -LiteralPath "$reports/$Case.log" -Encoding utf8
    foreach ($name in $sampleNames) {
        docker inspect --format '{"Name":{{json .Name}},"Image":{{json .Image}},"State":{{json .State}},"RestartCount":{{.RestartCount}}}' $name | Add-Content -LiteralPath "$reports/$Case-final-state.jsonl"
    }
    $code = docker inspect --format '{{.State.ExitCode}}' $client
    docker rm $client | Out-Null
    Write-Output "$Case exit=$code elapsed=$([Math]::Round(([DateTimeOffset]::UtcNow-$start).TotalSeconds,1))s"
    if ($code -ne '0') { throw "Load assertions failed: $Case; inspect retained reports" }
    Assert-ContainerRunningWithoutRestart -ContainerNames $names
}
$index = 0
foreach ($target in $(if ($OnlyHot) { @() } else { $targets })) {
    $index++
    $case = "$RunId-$Backend-cold-$target"
    Run-Case $case burst $ColdVUs $target "$RunId-$Backend-cold-$index"
}
Run-Case "$RunId-$Backend-hot-512" hot 512 all "$RunId-$Backend-hot"
Run-Case "$RunId-$Backend-hot-1024" hot 1024 all "$RunId-$Backend-hot"
Run-Case "$RunId-$Backend-mixed-512" burst 512 oci-manifest "$RunId-$Backend-mixed" $true
