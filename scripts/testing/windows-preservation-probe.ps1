param(
    [Parameter(Mandatory = $true)]
    [string]$Output
)

$ErrorActionPreference = 'Stop'
$features = [System.Collections.Generic.List[object]]::new()
$root = Join-Path ([System.IO.Path]::GetTempPath()) ("aok-windows-preservation-" + [guid]::NewGuid().ToString('N'))
$volume = [System.IO.Path]::GetPathRoot($root)
$drive = [System.IO.DriveInfo]::new($volume)
$product = (Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion').ProductName
$version = [System.Environment]::OSVersion.Version
$osName = if ($product -match 'Windows 11' -or ($version.Major -eq 10 -and $version.Build -ge 22000)) { 'Windows 11' } else { $product }
$arch = if ([System.Environment]::Is64BitOperatingSystem -and $env:PROCESSOR_ARCHITECTURE -eq 'AMD64') { 'amd64' } else { $env:PROCESSOR_ARCHITECTURE.ToLowerInvariant() }

function Add-Feature([string]$Id, [scriptblock]$Probe) {
    try {
        $probeResult = & $Probe
        if ($probeResult -is [System.Collections.IDictionary] -and $probeResult.status -eq 'pending') {
            $features.Add(@{ id = $Id; status = 'pending'; detail = [string]$probeResult.detail })
        } else {
            $features.Add(@{ id = $Id; status = 'pass'; detail = 'native filesystem probe completed' })
        }
    } catch {
        $features.Add(@{ id = $Id; status = 'fail'; detail = $_.Exception.Message })
    }
}

try {
    New-Item -ItemType Directory -Path $root | Out-Null
    Add-Feature 'acl_privacy' {
        $dir = Join-Path $root 'acl'; New-Item -ItemType Directory -Path $dir | Out-Null
        $acl = Get-Acl $dir; $sid = [System.Security.Principal.WindowsIdentity]::GetCurrent().User
        $acl.SetAccessRuleProtection($true, $false)
        $rule = [System.Security.AccessControl.FileSystemAccessRule]::new($sid, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
        $acl.SetAccessRule($rule); Set-Acl -Path $dir -AclObject $acl
        $actual = Get-Acl $dir
        if ($actual.Access | Where-Object { $_.IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value -ne $sid.Value -and $_.AccessControlType -eq 'Allow' }) { throw 'unexpected allow ACE remains' }
    }
    Add-Feature 'reparse_points' {
        $target = Join-Path $root 'junction-target'; $link = Join-Path $root 'junction-link'
        New-Item -ItemType Directory -Path $target | Out-Null; New-Item -ItemType Junction -Path $link -Target $target | Out-Null
        if (-not ((Get-Item $link -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'junction was not identified as a reparse point' }
    }
    Add-Feature 'alternate_streams' {
        $file = Join-Path $root 'ads.txt'; [IO.File]::WriteAllText($file, 'base'); [IO.File]::WriteAllText($file + ':aok', 'stream')
        if ([IO.File]::ReadAllText($file + ':aok') -ne 'stream') { throw 'alternate stream readback mismatch' }
    }
    Add-Feature 'case_normalization' {
        $file = Join-Path $root 'CaseProbe.txt'; [IO.File]::WriteAllText($file, 'same-file')
        if ([IO.File]::ReadAllText((Join-Path $root 'caseprobe.TXT')) -ne 'same-file') { throw 'case-insensitive lookup mismatch' }
    }
    Add-Feature 'long_paths' {
        $base = Join-Path $root 'long'; New-Item -ItemType Directory -Path $base | Out-Null
        $part = ('x' * 80); $long = Join-Path $base ($part + '\' + $part + '\' + $part + '\probe.txt')
        $long = '\\?\' + [IO.Path]::GetFullPath($long); [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($long)) | Out-Null
        [IO.File]::WriteAllText($long, 'long-path'); if ([IO.File]::ReadAllText($long) -ne 'long-path') { throw 'long path readback mismatch' }
    }
    Add-Feature 'dirty_untracked_preservation' {
        return @{ status = 'pending'; detail = 'AOK delivery-preservation workflow has not been run against dirty and untracked repository files' }
    }
    Add-Feature 'extraction_traversal' {
        Add-Type -AssemblyName System.IO.Compression
        $archive = Join-Path $root 'traversal.zip'; $outside = Join-Path $root 'escape.txt'
        $stream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew)
        try { $zip = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create); try { $entry = $zip.CreateEntry('..\escape.txt'); $writer = [IO.StreamWriter]::new($entry.Open()); try { $writer.Write('must-not-escape') } finally { $writer.Dispose() } } finally { $zip.Dispose() } } finally { $stream.Dispose() }
        $base = [IO.Path]::GetFullPath((Join-Path $root 'extract')); New-Item -ItemType Directory -Path $base | Out-Null
        $zip = [IO.Compression.ZipFile]::OpenRead($archive)
        try { foreach ($entry in $zip.Entries) { $candidate = [IO.Path]::GetFullPath((Join-Path $base $entry.FullName)); $prefix = $base.TrimEnd('\') + '\'; if (-not $candidate.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) { continue }; [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $candidate, $true) } } finally { $zip.Dispose() }
        if (Test-Path -LiteralPath $outside) { throw 'archive traversal wrote outside extraction root' }
    }
    Add-Feature 'atomic_replace_lock' {
        $file = Join-Path $root 'atomic.txt'; $temp = Join-Path $root 'atomic.tmp'
        [IO.File]::WriteAllText($file, 'old'); [IO.File]::WriteAllText($temp, 'new')
        $lock = [IO.File]::Open($file, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        try { try { [IO.File]::Replace($temp, $file, $null); throw 'replace unexpectedly succeeded while locked' } catch [System.IO.IOException] {} } finally { $lock.Dispose() }
        [IO.File]::Replace($temp, $file, $null); if ([IO.File]::ReadAllText($file) -ne 'new') { throw 'atomic replacement readback mismatch' }
    }
    Add-Feature 'interruption_recovery' {
        return @{ status = 'pending'; detail = 'AOK delivery interruption and recovery workflow has not been exercised' }
    }
    Add-Feature 'restore_recovery' {
        return @{ status = 'pending'; detail = 'AOK delivery rollback and restore workflow has not been exercised' }
    }

    $scriptHash = (Get-FileHash -LiteralPath $PSCommandPath -Algorithm SHA256).Hash.ToLowerInvariant()
    $report = @{
        version = 'aok-windows-preservation/v1'
        producer = 'aok-windows-preservation-probe/v1'
        scriptSha256 = $scriptHash
        observedAt = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
        scope = @{ os = $osName; architecture = $arch; filesystem = $drive.DriveFormat; driveType = $drive.DriveType.ToString(); volume = $volume }
        features = @($features)
    }
    $json = $report | ConvertTo-Json -Depth 8
    [IO.File]::WriteAllText($Output, $json, [System.Text.UTF8Encoding]::new($false))
    Write-Output "Wrote probe report: $Output"
} finally {
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue }
}
