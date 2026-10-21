function Assert-ContainerRunningWithoutRestart([string[]]$ContainerNames) {
    foreach ($containerName in $ContainerNames) {
        $json = docker inspect --format '{"Running":{{json .State.Running}},"OOMKilled":{{json .State.OOMKilled}},"RestartCount":{{.RestartCount}}}' $containerName
        if ($LASTEXITCODE -ne 0) { throw "Cannot inspect acceptance container: $containerName" }
        $containerState = $json | ConvertFrom-Json
        if (-not $containerState.Running -or $containerState.OOMKilled -or $containerState.RestartCount -ne 0) {
            throw "Acceptance container stopped, was OOM-killed, or restarted: $containerName; state=$json"
        }
    }
}
