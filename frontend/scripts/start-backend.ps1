$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$composeFile = Join-Path $repoRoot 'infra\docker-compose.yml'
$apiContainer = 'ldt2026-api'
$apiUrl = 'http://localhost:8080/readyz'
$dockerApp = Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\Docker Desktop.exe'

# Docker Desktop may be installed per-user and absent from PATH in a fresh terminal.
$dockerResources = Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\resources'
$dockerBin = Join-Path $dockerResources 'bin'
$dockerPlugins = Join-Path $dockerResources 'cli-plugins'
if ((Test-Path $dockerBin) -and (Test-Path $dockerPlugins)) {
    $env:PATH = "$dockerPlugins;$dockerBin;$env:PATH"
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw 'Docker CLI не найден. Проверь установку Docker Desktop.'
}

# Use cmd for the probe so PowerShell does not turn Docker stderr into a terminating error.
& $env:ComSpec /c 'docker info >NUL 2>&1'
if ($LASTEXITCODE -ne 0) {
    if (-not (Test-Path $dockerApp)) { throw 'Docker Engine не запущен, а Docker Desktop.exe не найден.' }
    Write-Host 'Запускаю Docker Desktop; это может занять пару минут...'
    Start-Process -FilePath $dockerApp
    $engineDeadline = (Get-Date).AddMinutes(3)
    do {
        Start-Sleep -Seconds 3
        & $env:ComSpec /c 'docker info >NUL 2>&1'
        if ($LASTEXITCODE -eq 0) { break }
    } while ((Get-Date) -lt $engineDeadline)
    if ($LASTEXITCODE -ne 0) { throw 'Docker Engine не запустился. Открой Docker Desktop и проверь сообщение об ошибке.' }
}

& docker compose -f $composeFile up -d
if ($LASTEXITCODE -ne 0) { throw 'Не удалось запустить инфраструктуру backend через Docker Compose.' }

$container = (& docker inspect $apiContainer 2>$null | ConvertFrom-Json -ErrorAction SilentlyContinue)
if (-not $container) {
    throw "Контейнер $apiContainer не найден. Backend API нужно один раз подготовить; см. docs/development.md."
}
if ($container[0].State.Status -ne 'running') {
    & docker start $apiContainer
    if ($LASTEXITCODE -ne 0) { throw "Не удалось запустить контейнер $apiContainer." }
}

Write-Host 'Ожидаю готовности backend API на http://localhost:8080 ...'
$deadline = (Get-Date).AddMinutes(3)
do {
    try {
        $response = Invoke-RestMethod -Uri $apiUrl -TimeoutSec 3
        if ($response.status -eq 'ready') {
            $pipelineContainers = @('ldt2026-relay', 'ldt2026-engine', 'ldt2026-mockworkers', 'ldt2026-rin-mock', 'ldt2026-rin-sync')
            $missingPipeline = @()
            foreach ($name in $pipelineContainers) {
                & $env:ComSpec /c "docker inspect $name >NUL 2>&1"
                if ($LASTEXITCODE -ne 0) {
                    $missingPipeline += $name
                    continue
                }
                $state = (& docker inspect --format '{{.State.Status}}' $name 2>$null)
                if ($state -ne 'running') {
                    & docker start $name
                    if ($LASTEXITCODE -ne 0) { throw "Не удалось запустить backend-сервис $name." }
                }
            }
            if ($missingPipeline.Count -gt 0) {
                Write-Warning ("Не найдены контейнеры конвейера: " + ($missingPipeline -join ', ') + ". Загрузка и просмотр данных доступны, но автоматическая обработка документов не запустится. См. docs/development.md, раздел «Полный конвейер локально».")
            } else {
                Write-Host 'Backend API и конвейер обработки документов готовы.' -ForegroundColor Green
            }
            Write-Host 'Запускаю frontend.' -ForegroundColor Green
            exit 0
        }
    } catch {
        Start-Sleep -Seconds 2
    }
} while ((Get-Date) -lt $deadline)
throw 'Backend API не стал готов за 3 минуты. Проверь логи: docker logs ldt2026-api'