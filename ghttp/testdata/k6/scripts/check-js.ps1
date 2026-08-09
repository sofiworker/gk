$ErrorActionPreference = 'Stop'
$node = Get-Command node -ErrorAction SilentlyContinue
if (-not $node) { throw 'node is required for JavaScript syntax validation' }
$files = Get-ChildItem -Path "$PSScriptRoot\..\lib", "$PSScriptRoot\..\scenarios" -Filter '*.js' -File
foreach ($file in $files) {
    & $node.Source --check $file.FullName
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
