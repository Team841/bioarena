# Build and push bioarena to a field Pi.
#
# The common case is a code change with no asset change, so that is the default and it
# copies one file:
#
#   .\deploy-pi.ps1
#
# Add -Assets when static/ or templates/ changed, -Service when bioarena.service did.
# Neither is guessed: copying assets every time is most of the wall clock, and copying the
# service file needs a sudo move and a daemon-reload that are wasted when it has not
# changed.
#
#   .\deploy-pi.ps1 -Assets
#   .\deploy-pi.ps1 -Assets -Service
#   .\deploy-pi.ps1 -Target 10.2.100.5 -User admin
#   .\deploy-pi.ps1 -Panel 10.0.100.11        # an e-stop panel Pi instead
#
# Set up key authentication first or this asks for a password three times per deploy --
# see "Faster deploys" in the README.

param(
    [string]$Target = "10.0.100.5",
    [string]$User = "admin",
    [string]$Panel = "",
    [switch]$Assets,
    [switch]$Service,
    [switch]$SkipBuild,
    [ValidateSet("arm64", "arm")]
    [string]$Arch = "arm64"
)

$ErrorActionPreference = "Stop"
$started = Get-Date

function Invoke-Step {
    param([string]$Description, [scriptblock]$Action)

    Write-Host "==> $Description" -ForegroundColor Cyan
    & $Action
    if ($LASTEXITCODE -ne 0) {
        # Stop at the first failure. Continuing past a failed copy would restart the
        # service on whichever binary happened to be there, which looks like a successful
        # deploy of code that was never pushed.
        throw "$Description failed with exit code $LASTEXITCODE"
    }
}

if ($Panel -ne "") {
    $remote = "$User@$Panel"
    $binary = "estop-panel-pi"
    $directory = "/opt/estop-panel"
    $unit = "estop-panel"
    $package = "./cmd/estop-panel"
} else {
    $remote = "$User@$Target"
    $binary = "bioarena-pi"
    $directory = "/opt/bioarena"
    $unit = "bioarena"
    $package = "."
}

if (-not $SkipBuild) {
    $env:GOOS = "linux"
    $env:GOARCH = $Arch
    if ($Arch -eq "arm") {
        $env:GOARM = "7"
    } elseif (Test-Path Env:GOARM) {
        # A leftover GOARM from a previous 32-bit build is ignored by an arm64 build, but
        # clearing it keeps the environment honest about what was built.
        Remove-Item Env:GOARM
    }
    Invoke-Step "Building $binary for linux/$Arch" { go build -o $binary $package }
}

# Linux refuses to overwrite a running executable, and scp reports that as a bare
# "Failure", so the service comes down before the copy rather than after.
Invoke-Step "Stopping $unit" { ssh $remote "sudo systemctl stop $unit" }

Invoke-Step "Copying $binary" { scp $binary "${remote}:$directory/" }

if ($Assets) {
    Invoke-Step "Copying static and templates" { scp -r static templates "${remote}:$directory/" }
}

if ($Service) {
    $unitFile = if ($Panel -ne "") { "cmd/estop-panel/estop-panel.service" } else { "bioarena.service" }
    Invoke-Step "Copying $unit.service" { scp $unitFile "${remote}:~/" }
    Invoke-Step "Installing $unit.service" {
        ssh $remote "sudo mv ~/$unit.service /etc/systemd/system/ && sudo systemctl daemon-reload"
    }
}

# Ownership is deliberately left alone: the service reads these files and writes only what
# it creates itself. Chowning them to the service account breaks the next deploy, because
# scp overwrites by opening the existing file for writing.
Invoke-Step "Starting $unit" { ssh $remote "chmod +x $directory/$binary && sudo systemctl start $unit" }

Write-Host ""
ssh $remote "systemctl is-active $unit && systemctl show -p ActiveEnterTimestamp --value $unit"

$elapsed = [int]((Get-Date) - $started).TotalSeconds
Write-Host ""
Write-Host "Deployed in ${elapsed}s. Watch it with:" -ForegroundColor Green
Write-Host "  ssh $remote 'journalctl -u $unit -f'"
