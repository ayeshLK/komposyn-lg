function Resolve-KomposynWindowsArchitecture {
    param(
        [Parameter(Mandatory=$true)][string]$RuntimeArchitecture,
        [Parameter(Mandatory=$true)][string]$SystemArchitecture
    )

    if ($RuntimeArchitecture -eq 'Arm64' -or $SystemArchitecture -eq 'ARM64') {
        return 'arm64'
    }
    return 'amd64'
}

function Get-KomposynWindowsArchitecture {
    $runtimeArchitecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    $systemArchitecture = ''
    try {
        $systemArchitecture = (Get-ItemProperty -LiteralPath 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment' -Name PROCESSOR_ARCHITECTURE -ErrorAction Stop).PROCESSOR_ARCHITECTURE
    } catch {
        # OSArchitecture is still sufficient when the system environment key is unavailable.
    }
    return Resolve-KomposynWindowsArchitecture -RuntimeArchitecture $runtimeArchitecture -SystemArchitecture $systemArchitecture
}
