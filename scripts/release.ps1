<#
.SYNOPSIS
    Tags and pushes a new Orchestrator release to trigger the GitHub Actions
    Windows installer + auto-update release workflow.
.EXAMPLE
    .\scripts\release.ps1 1.0.0
    .\scripts\release.ps1 1.0.1 -DryRun
#>
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$Version,

    [switch]$DryRun
)

$ErrorActionPreference = "Stop"

$clean = $Version.Trim().TrimStart("v", "V")
if ($clean -notmatch '^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') {
    throw "Version must be valid semver (e.g. 1.0.0 or 1.0.1-beta.1), got: $Version"
}

$tag = "v$clean"
$numeric = ($clean -split '-')[0]

$status = git status --porcelain
if ($status) {
    throw "Working tree has uncommitted changes. Commit or stash them before releasing."
}

$existingTag = git tag -l $tag
if ($existingTag) {
    throw "Tag $tag already exists."
}

$wailsPath = Join-Path $PSScriptRoot "..\desktop\wails.json"
$cfg = Get-Content $wailsPath -Raw | ConvertFrom-Json
$cfg.info.productVersion = $numeric

if ($DryRun) {
    Write-Host "[DryRun] Would set desktop/wails.json productVersion=$numeric, commit, tag $tag, and push."
    exit 0
}

$json = ($cfg | ConvertTo-Json -Depth 10) + "`n"
[System.IO.File]::WriteAllText((Resolve-Path $wailsPath).Path, $json, [System.Text.UTF8Encoding]::new($false))
git add $wailsPath
if (git diff --cached --quiet; $LASTEXITCODE -ne 0) {
    git commit -m "chore(release): bump version to $tag"
}

git tag -a $tag -m "Release $tag"
$branch = (git rev-parse --abbrev-ref HEAD).Trim()
git push origin $branch
git push origin $tag

Write-Host ""
Write-Host "Pushed $tag to origin ($branch). GitHub Actions is now building the Windows installer and publishing latest.json."
Write-Host "Track progress at: https://github.com/lirikrexhepi/Orchestrator/actions"
