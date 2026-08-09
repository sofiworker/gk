$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$files = Get-ChildItem (Join-Path $root 'lib'), (Join-Path $root 'scenarios') -Filter '*.js' -File
$names = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
foreach ($file in $files) {
    $source = Get-Content -Raw $file.FullName
    foreach ($match in [regex]::Matches($source, '[''\"](?<name>[^''\"]+)[''\"]\s*:\s*(?:\([^)]*\)|[A-Za-z_$][A-Za-z0-9_$]*)\s*=>')) {
        [void]$names.Add($match.Groups['name'].Value)
    }
}
$scenarioCount = (Get-ChildItem (Join-Path $root 'scenarios') -Filter '*.js' -File).Count
[pscustomobject]@{ scenarios = $scenarioCount; unique_check_names = $names.Count; names = @($names | Sort-Object) } | ConvertTo-Json -Depth 4
