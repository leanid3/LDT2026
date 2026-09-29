$ErrorActionPreference = 'Stop'
$port = 3002
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$nextCli = Join-Path $repoRoot 'frontend\node_modules\next\dist\bin\next'

$listener = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
if ($listener) {
    $owner = Get-CimInstance Win32_Process -Filter "ProcessId = $($listener.OwningProcess)" -ErrorAction SilentlyContinue
    $isNextServer = $owner -and ($owner.CommandLine -match '(?i)frontend.*node_modules.*next.*start-server\.js')
    if ($isNextServer) {
        Write-Host "Frontend already running at http://localhost:$port/login" -ForegroundColor Green
        exit 0
    }
    throw "Port $port is already occupied by PID $($listener.OwningProcess). Close that process or report this message."
}

& node $nextCli dev --hostname localhost --port $port
exit $LASTEXITCODE