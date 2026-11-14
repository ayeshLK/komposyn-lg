param(
    [Parameter(Mandatory = $true)]
    [string] $ArchivePath,
    [Parameter(Mandatory = $true)]
    [string] $DestinationPath
)

$ErrorActionPreference = 'Stop'
# Callers provide a fresh destination directory. Omitting -Force keeps this
# compatible with Windows PowerShell's .NET Framework ZipFile overloads and
# avoids replacing anything if the precondition is violated.
Expand-Archive -LiteralPath $ArchivePath -DestinationPath $DestinationPath
