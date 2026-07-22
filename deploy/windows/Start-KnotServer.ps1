[CmdletBinding()]
param()

$module = Join-Path $PSScriptRoot "KnotServer.psm1"
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Import-Module $module -Force
Start-KnotServer -RepositoryRoot $repositoryRoot
