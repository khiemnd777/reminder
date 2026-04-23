$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Error "Docker is not installed or not in PATH."
}

if (-not (Test-Path ".env")) {
    Write-Error ".env not found. Copy .env.example to .env and fill in your values first."
}

docker compose up --build -d
