# Installs gander, a version manager for the Goose compiler, on Windows. In PowerShell:
#
#     irm https://github.com/bgourlie/gander/releases/latest/download/install.ps1 | iex
#
# It downloads gander for this machine from gander's latest GitHub release, checks it
# against the release's SHA256SUMS, and runs "gander self install", which puts gander.exe
# and goose.exe in %LOCALAPPDATA%\gander\bin and adds that folder to your PATH.
# Arguments go to "gander self install":
#
#     & ([scriptblock]::Create((irm .../install.ps1))) --no-modify-path
#
# GANDER_HOME changes the folder; GANDER_VERSION installs that release instead of the
# latest.
#
# irm | iex runs this in your own PowerShell session, so it never calls exit, which
# would close the window, and it changes nothing in the session but PATH. It is all one
# script block, so a download cut short runs nothing.

& {
    $ErrorActionPreference = 'Stop'
    # Invoke-WebRequest's progress bar slows downloads severalfold (deno_install#218).
    $ProgressPreference = 'SilentlyContinue'
    $repo = 'https://github.com/bgourlie/gander'
    $asset = 'gander-windows-x64.exe'

    # Goose ships only an x64 compiler. Windows on Arm runs x64 programs from Windows 11
    # (build 22000) on. A 32-bit PowerShell on 64-bit Windows reports x86, with the real
    # architecture in PROCESSOR_ARCHITEW6432.
    $arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    $build = [Environment]::OSVersion.Version.Build
    if (-not ($arch -eq 'AMD64' -or ($arch -eq 'ARM64' -and $build -ge 22000))) {
        throw "gander: there's no build for Windows $arch (build $build), because Goose ships no compiler for it. Goose runs on x64 Windows, and on Arm from Windows 11."
    }

    $root = $env:GANDER_HOME
    if (-not $root -and $env:LOCALAPPDATA) { $root = Join-Path $env:LOCALAPPDATA 'gander' }
    if ($root -notmatch '^([A-Za-z]:\\|\\\\)') {
        throw "gander: set GANDER_HOME to a writable absolute folder"
    }
    $bin = Join-Path $root 'bin'
    # Into gander's own folder, where it will be installed.
    $file = Join-Path $bin ".gander-install-$PID.exe"
    $sums = Join-Path $bin ".gander-install-$PID.sums"
    # A failed install takes back the folders it made, so they don't look like an install.
    $madeRoot = -not (Test-Path -LiteralPath $root)
    $madeBin = -not (Test-Path -LiteralPath $bin)

    # Get-Download tries three times when the network fails or GitHub answers 408, 429 or
    # 5xx, as install.sh's curl --retry 3 does, but not on another error such as 404. The
    # Response is looked up, since reading a missing property fails under Set-StrictMode.
    function Get-Download([string]$url, [string]$dest) {
        for ($try = 1; ; $try++) {
            try {
                Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $dest
                return
            } catch {
                $status = 0
                $response = $_.Exception.PSObject.Properties['Response']
                if ($response -and $response.Value) { $status = [int]$response.Value.StatusCode }
                $passing = $status -eq 0 -or $status -eq 408 -or $status -eq 429 -or $status -ge 500
                if ($try -ge 3 -or -not $passing) {
                    throw "gander: can't download ${url}: $($_.Exception.Message)"
                }
                Start-Sleep -Seconds $try
            }
        }
    }

    $ok = $false
    try {
        New-Item -ItemType Directory -Force -Path $bin | Out-Null

        $tag = $env:GANDER_VERSION
        if (-not $tag) {
            # The tag is read from where releases/latest redirects, so the binary and
            # SHA256SUMS come from the same release even if one is published meanwhile.
            $req = [System.Net.HttpWebRequest]::Create("$repo/releases/latest")
            $req.AllowAutoRedirect = $false
            # Left open, its connection can go to the first download after GitHub has
            # closed it: "The connection was closed unexpectedly".
            $req.KeepAlive = $false
            try {
                $resp = $req.GetResponse()
            } catch {
                throw "gander: can't reach $repo/releases/latest: $($_.Exception.Message)"
            }
            $location = $resp.Headers['Location']
            $resp.Close()
            if ($location -notmatch '/releases/tag/([^/]+)$') {
                throw "gander: can't find gander's latest release at $repo/releases"
            }
            $tag = $Matches[1]
        }
        Write-Host "Downloading gander $tag for windows-x64"
        Get-Download "$repo/releases/download/$tag/$asset" $file
        Get-Download "$repo/releases/download/$tag/SHA256SUMS" $sums

        $want = $null
        foreach ($line in Get-Content -LiteralPath $sums) {
            $f = -split $line
            if ($f.Count -eq 2 -and ($f[1] -eq $asset -or $f[1] -eq "*$asset")) { $want = $f[0] }
        }
        if (-not $want) { throw "gander: release $tag's SHA256SUMS has no line for $asset" }
        # Antivirus may delete a new program as soon as it's written.
        if (-not (Test-Path -LiteralPath $file)) {
            throw "gander: $file is gone after downloading it; antivirus may have removed it"
        }
        $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $file).Hash
        if ($got -ne $want) {
            throw "gander: the download of $asset doesn't match release $tag's SHA256SUMS (expected $want, got $got); try again"
        }

        & $file self install @args
        if ($LASTEXITCODE -ne 0) { throw "gander: gander self install failed; see above" }
        $ok = $true
    } finally {
        Remove-Item -LiteralPath $file, $sums -Force -ErrorAction SilentlyContinue
        if (-not $ok) {
            # Directory.Delete refuses a folder that isn't empty, so these keep anything
            # gander put in them; Remove-Item would ask.
            if ($madeBin) { try { [System.IO.Directory]::Delete($bin) } catch {} }
            if ($madeRoot) { try { [System.IO.Directory]::Delete($root) } catch {} }
        }
    }

    # "gander self install" changed PATH for new terminals; this one is already open.
    # Every form Go's flag package reads as --no-modify-path, case aside.
    if (-not ($args -match '^--?no-modify-path(=(1|t|true))?$') -and ($env:Path -split ';') -notcontains $bin) {
        $env:Path = "$bin;$env:Path"
        Write-Host "gander and goose work in this window too."
    }
} @args
