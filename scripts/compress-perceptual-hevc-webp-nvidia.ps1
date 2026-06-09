[CmdletBinding(SupportsShouldProcess)]
param(
    [Parameter(Mandatory, Position = 0)]
    [string]$Folder,

    [switch]$DeleteOriginal,

    [ValidateRange(0, 100)]
    [int]$WebPQuality = 85,

    [ValidateRange(0, 100)]
    [double]$MinVmaf = 95,

    [string]$Ffmpeg = "ffmpeg",
    [string]$Ffprobe = "ffprobe",
    [string]$AbAv1 = "ab-av1",
    [string]$Cwebp = "cwebp",
    [string]$ExifTool = "exiftool",
    [string]$VmafModelDirectory
)

$ErrorActionPreference = "Stop"
$imageExtensions = @(".jpg", ".jpeg", ".png")
$videoExtensions = @(
    ".3gp", ".3gpp", ".avi", ".flv", ".insv", ".m2t", ".m2ts", ".m4v",
    ".mkv", ".mov", ".mp4", ".mpe", ".mpeg", ".mpg", ".mts", ".ts",
    ".webm", ".wmv"
)

function Assert-Command {
    param([string]$Command)

    if (-not (Get-Command $Command -ErrorAction SilentlyContinue)) {
        throw "Required command was not found: $Command"
    }
}

function Invoke-Native {
    param(
        [string]$Command,
        [string[]]$Arguments
    )

    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command failed with exit code $LASTEXITCODE"
    }
}

function Get-MetadataValue {
    param(
        [string]$Path,
        [string]$Tag
    )

    $value = & $ExifTool -s3 "-$Tag" $Path
    if ($LASTEXITCODE -ne 0) {
        throw "ExifTool failed while reading $Tag from $Path"
    }
    return ($value -join "`n").Trim()
}

function Copy-And-ValidateMetadata {
    param(
        [string]$Source,
        [string]$Destination,
        [string[]]$Tags,
        [switch]$QuickTime
    )

    $arguments = @("-overwrite_original", "-m")
    if ($QuickTime) {
        $arguments += @("-api", "QuickTimeUTC=1", "-api", "LargeFileSupport=1")
    }
    $arguments += @("-tagsFromFile", $Source, "-all:all", $Destination)
    Invoke-Native $ExifTool $arguments

    foreach ($tag in $Tags) {
        $sourceValue = Get-MetadataValue $Source $tag
        $destinationValue = Get-MetadataValue $Destination $tag
        if ($sourceValue -and -not $destinationValue) {
            throw "Metadata validation failed: output is missing $tag"
        }
    }
}

function Complete-Output {
    param(
        [System.IO.FileInfo]$Source,
        [string]$Destination
    )

    $output = Get-Item -LiteralPath $Destination
    if ($output.Length -ge $Source.Length) {
        Remove-Item -LiteralPath $Destination -Force
        Write-Host "Kept original; output was not smaller: $($Source.Name)"
        return
    }

    $saved = $Source.Length - $output.Length
    $reduction = 100 * $saved / $Source.Length
    Write-Host ("Created {0}: {1:N2} MB -> {2:N2} MB ({3:N1}% saved)" -f `
        $output.Name, ($Source.Length / 1MB), ($output.Length / 1MB), $reduction)

    if ($DeleteOriginal -and $PSCmdlet.ShouldProcess($Source.FullName, "Delete original after verified conversion")) {
        Remove-Item -LiteralPath $Source.FullName -Force
    }
}

function Convert-Image {
    param([System.IO.FileInfo]$Source)

    $destination = Join-Path $Source.DirectoryName ($Source.BaseName + ".webp")
    if (Test-Path -LiteralPath $destination) {
        Write-Host "Skipping existing output: $destination"
        return
    }

    try {
        Invoke-Native $Cwebp @(
            "-quiet", "-q", "$WebPQuality", "-m", "6", "-metadata", "all",
            $Source.FullName, "-o", $destination
        )
        Copy-And-ValidateMetadata $Source.FullName $destination @("GPSPosition", "Model", "DateTimeOriginal")
        Complete-Output $Source $destination
    }
    catch {
        Remove-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
        throw
    }
}

function Get-VmafArguments {
    param([System.IO.FileInfo]$Source)

    if (-not $VmafModelDirectory) {
        return @()
    }

    $probe = & $Ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of json $Source.FullName |
        ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or -not $probe.streams) {
        throw "ffprobe could not determine dimensions for $($Source.FullName)"
    }

    $model = if ($probe.streams[0].width -gt 2560 -and $probe.streams[0].height -gt 1440) {
        "vmaf_4k_v0.6.1.json"
    }
    else {
        "vmaf_v0.6.1.json"
    }
    $modelPath = Join-Path $VmafModelDirectory $model
    if (-not (Test-Path -LiteralPath $modelPath)) {
        throw "VMAF model not found: $modelPath"
    }
    return @("--vmaf", "model=path=$modelPath")
}

function Find-NvencQp {
    param(
        [System.IO.FileInfo]$Source,
        [string[]]$VmafArguments
    )

    $low = 1
    $high = 40
    $bestQp = $null
    $bestPercent = $null
    $fallbackQp = $null
    $fallbackScore = -1.0
    $fallbackPercent = $null
    while ($low -le $high) {
        $qp = [Math]::Floor(($low + $high) / 2)
        Write-Host "Testing NVENC QP $qp against VMAF $MinVmaf"
        $arguments = @(
            "sample-encode",
            "--input", $Source.FullName,
            "--encoder", "hevc_nvenc",
            "--crf", "$cq",
            "--pix-format", "yuv420p",
            "--preset", "p7",
            "--min-samples", "3",
            "--sample-every", "8m",
            "--sample-duration", "12s",
            "--enc", "rc=constqp",
            "--enc", "qp=$qp",
            "--enc", "spatial_aq=1",
            "--enc", "rc-lookahead=32",
            "--enc-input", "noautorotate"
        ) + $VmafArguments
        $sampleOutput = (& $AbAv1 @arguments 2>&1 | ForEach-Object { "$_" })
        $sampleOutput | ForEach-Object { Write-Host $_ }
        if ($LASTEXITCODE -ne 0) {
            throw "ab-av1 sample-encode failed with exit code $LASTEXITCODE"
        }
        $scoreMatches = [regex]::Matches(($sampleOutput -join "`n"), 'VMAF\s+([0-9]+(?:\.[0-9]+)?)')
        if ($scoreMatches.Count -eq 0) {
            throw "Unable to read VMAF score for NVENC QP $qp"
        }
        $score = [double]$scoreMatches[$scoreMatches.Count - 1].Groups[1].Value
        $percentMatches = [regex]::Matches(($sampleOutput -join "`n"), 'predicted video stream size.*\(([0-9]+(?:\.[0-9]+)?)%\)')
        if ($percentMatches.Count -eq 0) {
            throw "Unable to read predicted size for NVENC QP $qp"
        }
        $percent = [double]$percentMatches[$percentMatches.Count - 1].Groups[1].Value
        if ($score -gt $fallbackScore) {
            $fallbackQp = $qp
            $fallbackScore = $score
            $fallbackPercent = $percent
        }
        if ($score -ge $MinVmaf) {
            $bestQp = $qp
            $bestPercent = $percent
            $low = $qp + 1
        }
        else {
            $high = $qp - 1
        }
    }

    if ($null -eq $bestQp) {
        $bestQp = $fallbackQp
        $bestPercent = $fallbackPercent
        Write-Warning "NVENC could not meet VMAF $MinVmaf; using best measured QP $bestQp at VMAF $fallbackScore"
    }
    return [pscustomobject]@{ Qp = $bestQp; Percent = $bestPercent }
}

function Convert-Video {
    param([System.IO.FileInfo]$Source)

    $destination = Join-Path $Source.DirectoryName ($Source.BaseName + "-hbed.mp4")
    if (Test-Path -LiteralPath $destination) {
        Write-Host "Skipping existing output: $destination"
        return
    }

    $workDirectory = Join-Path $env:TEMP ("hbed-" + [guid]::NewGuid().ToString("N"))
    $videoOnly = Join-Path $workDirectory "video-only-hevc-nvenc.mp4"
    $cacheDirectory = Join-Path $workDirectory "cache"
    New-Item -ItemType Directory -Path $cacheDirectory -Force | Out-Null

    $oldCache = $env:XDG_CACHE_HOME
    $oldTemp = $env:AB_AV1_TEMP_DIR
    $env:XDG_CACHE_HOME = $cacheDirectory
    $env:AB_AV1_TEMP_DIR = $workDirectory

    try {
        $vmafArguments = Get-VmafArguments $Source
        $selection = Find-NvencQp $Source $vmafArguments
        $qp = $selection.Qp
        Write-Host "Selected NVENC QP $qp"
        if ($selection.Percent -gt 80) {
            throw "Projected output is $($selection.Percent)% of original; less than 20% savings, refusing encode"
        }

        Invoke-Native $Ffmpeg @(
            "-hide_banner", "-y", "-noautorotate",
            "-i", $Source.FullName,
            "-map", "0:v:0",
            "-an",
            "-c:v", "hevc_nvenc",
            "-preset", "p7",
            "-pix_fmt", "yuv420p",
            "-rc", "constqp",
            "-qp", "$qp",
            "-spatial_aq", "1",
            "-rc-lookahead", "32",
            $videoOnly
        )

        Invoke-Native $Ffmpeg @(
            "-hide_banner", "-y", "-noautorotate",
            "-i", $videoOnly,
            "-i", $Source.FullName,
            "-map", "0:v:0",
            "-map", "1:a?",
            "-map_metadata", "1",
            "-map_chapters", "1",
            "-c", "copy",
            "-tag:v", "hvc1",
            "-movflags", "+faststart+use_metadata_tags",
            $destination
        )

        Copy-And-ValidateMetadata $Source.FullName $destination `
            @("Rotation", "GPSCoordinates", "Model", "CreateDate") -QuickTime
        Complete-Output $Source $destination
    }
    catch {
        Remove-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
        throw
    }
    finally {
        $env:XDG_CACHE_HOME = $oldCache
        $env:AB_AV1_TEMP_DIR = $oldTemp
        Remove-Item -LiteralPath $workDirectory -Recurse -Force -ErrorAction SilentlyContinue
    }
}

$resolvedFolder = (Resolve-Path -LiteralPath $Folder).Path
if (-not (Test-Path -LiteralPath $resolvedFolder -PathType Container)) {
    throw "Folder not found: $Folder"
}

foreach ($command in @($Ffmpeg, $Ffprobe, $AbAv1, $Cwebp, $ExifTool)) {
    Assert-Command $command
}

$resolvedFfmpeg = (Get-Command $Ffmpeg).Source
$ffmpegDirectory = Split-Path -Parent $resolvedFfmpeg
if (($env:PATH -split [IO.Path]::PathSeparator) -notcontains $ffmpegDirectory) {
    $env:PATH = $ffmpegDirectory + [IO.Path]::PathSeparator + $env:PATH
}

$filters = & $Ffmpeg -hide_banner -filters 2>&1
if ($filters -notmatch "libvmaf") {
    throw "The selected FFmpeg build does not include libvmaf."
}
$encoders = & $Ffmpeg -hide_banner -encoders 2>&1
if ($encoders -notmatch "hevc_nvenc") {
    throw "The selected FFmpeg build does not include hevc_nvenc."
}

$files = Get-ChildItem -LiteralPath $resolvedFolder -File
$eligibleImages = @($files | Where-Object { $imageExtensions -contains $_.Extension.ToLowerInvariant() }).Count
$eligibleVideos = @($files | Where-Object {
        ($videoExtensions -contains $_.Extension.ToLowerInvariant()) -and
        -not $_.BaseName.EndsWith("-hbed", [StringComparison]::OrdinalIgnoreCase)
    }).Count
Write-Host "Folder: $resolvedFolder"
Write-Host "Eligible images: $eligibleImages; eligible videos: $eligibleVideos; delete originals: $DeleteOriginal"

foreach ($file in $files) {
    $extension = $file.Extension.ToLowerInvariant()
    try {
        if ($imageExtensions -contains $extension) {
            Convert-Image $file
        }
        elseif (($videoExtensions -contains $extension) -and -not $file.BaseName.EndsWith("-hbed", [StringComparison]::OrdinalIgnoreCase)) {
            Convert-Video $file
        }
    }
    catch {
        Write-Error "Failed processing $($file.FullName): $_" -ErrorAction Continue
    }
}
