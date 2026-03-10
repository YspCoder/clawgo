[CmdletBinding()]
param(
    [string]$Output = "build/clawgo-windows-amd64-slim.exe",
    [switch]$EmbedWebUI,
    [switch]$Compress
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$embedDir = Join-Path $repoRoot "cmd/workspace"
$workspaceDir = Join-Path $repoRoot "workspace"
$webuiDistDir = Join-Path $repoRoot "webui/dist"
$outputPath = Join-Path $repoRoot $Output

function Copy-DirectoryContents {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )

    if (Test-Path $Destination) {
        Remove-Item -Recurse -Force $Destination
    }
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    Copy-Item -Path (Join-Path $Source "*") -Destination $Destination -Recurse -Force
}

try {
    if (-not (Test-Path $workspaceDir)) {
        throw "Missing workspace source directory: $workspaceDir"
    }

    Copy-DirectoryContents -Source $workspaceDir -Destination $embedDir

    if ($EmbedWebUI) {
        if (-not (Test-Path $webuiDistDir)) {
            throw "EmbedWebUI was requested, but WebUI dist is missing: $webuiDistDir"
        }
        $embedWebuiDir = Join-Path $embedDir "webui"
        Copy-DirectoryContents -Source $webuiDistDir -Destination $embedWebuiDir
    }

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $outputPath) | Out-Null

    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"

    $version = "dev"
    $buildTime = [DateTimeOffset]::Now.ToString("yyyy-MM-ddTHH:mm:sszzz")
    $ldflags = "-X main.version=$version -X main.buildTime=$buildTime -s -w -buildid="
    $tags = "purego,netgo,osusergo"

    & go build -trimpath -buildvcs=false -tags $tags -ldflags $ldflags -o $outputPath ./cmd
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }

    if ($Compress) {
        $upx = Get-Command upx -ErrorAction SilentlyContinue
        if ($null -ne $upx) {
            & $upx.Source --best --lzma $outputPath | Out-Null
            if ($LASTEXITCODE -ne 0) {
                throw "upx failed with exit code $LASTEXITCODE"
            }
        } else {
            Write-Warning "Compress was requested, but upx was not found in PATH"
        }
    }

    $sizeMB = [Math]::Round((Get-Item $outputPath).Length / 1MB, 2)
    Write-Host "Build complete: $outputPath ($sizeMB MB)"
}
finally {
    if (Test-Path $embedDir) {
        Remove-Item -Recurse -Force $embedDir
    }
}
