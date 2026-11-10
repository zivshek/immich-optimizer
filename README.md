# Immich Optimizer

[![Release](https://img.shields.io/github/v/release/zivshek/immich-optimizer)](https://github.com/zivshek/immich-optimizer/releases)
[![Docker](https://img.shields.io/badge/docker-ghcr.io-blue)](https://ghcr.io/zivshek/immich-optimizer)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zivshek/immich-optimizer)](https://golang.org/)
[![License](https://img.shields.io/github/license/zivshek/immich-optimizer)](LICENSE)

A file optimization service that automatically processes and uploads media files to [Immich](https://immich.app/). This tool watches for new files in a directory, applies configurable optimization tasks, and uploads the optimized results to your Immich instance.

Files and directories whose names begin with `.` are ignored.

## Fork Differences

This fork extends the original
[`miguelangel-nubla/immich-optimizer`](https://github.com/miguelangel-nubla/immich-optimizer)
for server-side processing **before** assets are uploaded to Immich.

- **Multi-user ingestion:** Assign separate watched and failure directories to
  different Immich users/API keys while sharing one optimizer container.
- **Compose and Dockge friendly profiles:** Define profiles inline with
  `IUO_PROFILES_CONFIG`, without mounting a separate profiles file.
- **GPU processing:** Includes NVIDIA NVENC and AMD/Intel VAAPI Compose
  examples and bundled processing profiles.
- **Mixed optimization profiles:** Supports lossless image optimization
  together with high-quality NVIDIA FFmpeg or HandBrake video transcoding.
- **Phone-video preservation:** The NVIDIA FFmpeg profile preserves stored
  dimensions and display rotation, restores writable GPS/camera/date metadata,
  and validates critical metadata before upload.
- **Safer processing:** Originals are deleted only after a successful Immich
  upload. If optimization fails, the original is uploaded instead. Files are
  copied to the profile's `undone` directory only when uploading also fails.
- **Image savings threshold:** Optimized images are uploaded only when they
  reduce file size by at least `15%`; otherwise the original image is uploaded.
- **FolderSync-aware watching:** Handles atomic file placement and hardlinks,
  and ignores hidden files/directories such as `.trashed-*`.
- **Fork publishing:** A lightweight multi-architecture standard image and an
  AMD64 perceptual-quality image are published to
  `ghcr.io/zivshek/immich-optimizer`.
- **Dashboard and history:** Provides an embedded dashboard with live logs,
  processed counts, size totals, space savings, reduction percentage, and
  SQLite-backed upload history.

When using FolderSync, configure it not to resync unchanged source files after
the optimizer removes a successfully uploaded landing-zone file. Optimized
assets differ from the originals, so Immich Mobile may display a local original
and optimized cloud asset separately while both exist.

## ✨ Features

- **📁 File Watching**: Automatically monitors directories for new media files
- **🔄 Configurable Processing**: Support for multiple optimization profiles
- **📸 Image Optimization**:
  - Lossless JPEG-XL conversion
  - Caesium compression
  - Format-specific optimization
- **🎥 Video Optimization**: HandBrake integration for video compression
- **🚀 Multi-Architecture**: Native support for AMD64 and ARM64
- **🔒 Secure**: Runs as non-root user with proper file permissions
- **⚡ Performance**: Concurrent processing with configurable limits
- **📊 Monitoring**: Built-in health checks and structured logging
- **🐳 Docker Ready**: Production-ready container images

## 📦 Installation

See [ARCHITECTURE.md](ARCHITECTURE.md) to understand the whole picture.

### Docker (Recommended)

```bash
# Pull the latest image
docker pull ghcr.io/zivshek/immich-optimizer:latest

# Run with lossless optimization
docker run -d \
  --name immich-optimizer \
  -v /path/to/watch:/watch \
  -v /path/to/undone:/undone \
  -e IUO_IMMICH_URL=http://your-immich-instance:2283 \
  -e IUO_IMMICH_API_KEY=your-api-key \
  ghcr.io/zivshek/immich-optimizer:latest
```

Images are published automatically by GitHub Actions:

- Every push to `main` publishes `latest` and a `sha-...` tag.
- Every push to `main` also publishes the AMD64-only `perceptual` and
  `perceptual-sha-...` tags.
- Tags such as `v1.2.3` publish `1.2.3`, `1.2`, `1`, and `sha-...`.
- Pull requests run tests but do not publish images.

Use `ghcr.io/zivshek/immich-optimizer:latest` for standard profiles. Use
`ghcr.io/zivshek/immich-optimizer:perceptual` for profiles that require
`fssimu2`, `ab-av1`, VMAF, or the dedicated NVENC-enabled FFmpeg build. Keeping
the expensive perceptual toolchain in a separate AMD64 image makes normal
multi-architecture publishes much faster.

After the first workflow publish, set the package visibility to **Public** in
the package settings if anonymous `docker pull` access is desired.

### Docker Compose

```yaml
services:
  immich-optimizer:
    image: ghcr.io/zivshek/immich-optimizer:latest
    container_name: immich-optimizer
    environment:
      - IUO_IMMICH_URL=http://immich-server:2283
      - IUO_IMMICH_API_KEY=your-api-key
      - IUO_WATCH_DIR=/watch
    volumes:
      - /path/to/watch:/watch
      - /path/to/undone:/undone
      - ./optimizer-data:/data
      # Optional: dashboard-selectable custom profiles
      - ./custom_profiles:/custom_profiles
    ports:
      - "8098:8098"
    restart: unless-stopped
```

Open `http://your-server:8098` to view the dashboard. Persist `/data` so
statistics survive container recreation. The dashboard records only successful
Immich uploads in its totals. Recent Jobs shows successful and failed attempts
in pages of 10; failed rows are red and do not affect statistics. Individual
history rows can be deleted from the dashboard.

The dashboard lists every `tasks.yaml` found below
`/etc/immich-optimizer/bundled-configs` and `/custom_profiles`. Each ingestion
profile has independent image and video config dropdowns, so their tasks can be
mixed without creating another combined config. Perceptual choices are
single-purpose configs. These include `standard/av1-crf28`,
`perceptual/webp`, `perceptual/avif`, `perceptual/jxl`, `perceptual/av1`, and
`perceptual/hevc`. Enable **Use NVIDIA** to make supported video configs use
NVENC; both AV1 configs intentionally remain SVT-AV1. A config appears only in
the dropdowns matching the extensions it declares. Custom entries appear as
`custom/<folder>`. Recently selected configs appear first, followed by the
remaining folder names alphabetically. Changes apply only to jobs started
after changing a dashboard control, and persist in the SQLite database
across container restarts.
The per-profile **Drop APAC** toggle controls newer iPhone spatial-audio videos.
By default APAC tracks are preserved by uploading the original file instead of
transcoding. When **Drop APAC** is enabled, bundled video profiles keep the
primary compatibility audio track and transcode/remux without the Apple APAC
spatial-audio track.
`IUO_TASKS_FILE` initializes both selections when no dashboard selection has
been saved. A custom config may contain only image tasks or only video tasks;
its scripts remain relative to the folder containing its `tasks.yaml`.
Saved combined image/video selections migrate to independent choices automatically.
Legacy standard config folders remain loadable for existing `IUO_TASKS_FILE`
paths, but are hidden from dashboard choices. Legacy perceptual paths are
rewritten during startup. A single-purpose bundled startup config uses passthrough for
the other media type until you select a config for it.

The dashboard does not currently provide authentication. Keep port `8098` on a
trusted network or place it behind an authenticated reverse proxy. When using a
custom Compose `user: "UID:GID"`, ensure that UID/GID can write to the host
directory mounted at `/data`.

### Multi-user profiles

Set `IUO_PROFILES_CONFIG` to a YAML block to run isolated watchers and Immich
API clients directly from Docker Compose. `IUO_IMMICH_URL` and the startup
fallback task config are shared. Each profile only defines its user, API key,
watch directory, and failure directory.

```yaml
environment:
  IUO_IMMICH_URL: http://immich-server:2283
  IUO_PROFILES_CONFIG: |
    profiles:
      - user: alice
        api_key: ${ALICE_IMMICH_API_KEY}
        watch_dir: /inbox/alice
        undone_dir: /undone/alice
      - user: bob
        api_key: ${BOB_IMMICH_API_KEY}
        watch_dir: /inbox/bob
        undone_dir: /undone/bob
```

Start the included example:

```bash
docker compose -f compose.multi-user.yaml up -d
```

Profile paths must not overlap. The original single-user settings,
`IUO_PROFILES_FILE`, and per-profile environment-variable format remain
supported when `IUO_PROFILES_CONFIG` is not set.

### GPU passthrough

Processing commands automatically have access to devices exposed to the
container. Use the included Compose override matching the host GPU:

```bash
# AMD or Intel VAAPI
docker compose -f compose.multi-user.yaml -f compose.gpu-vaapi.yaml up -d

# NVIDIA Container Toolkit
docker compose -f compose.multi-user.yaml -f compose.gpu-nvidia.yaml up -d
```

Choose image and video processing independently in the dashboard. For example:

| Goal | Image config | Video config |
| --- | --- | --- |
| Lossless photos, unchanged videos (default) | `standard/lossless` | `standard/passthrough-videos` |
| Lossless photos, NVIDIA HEVC with FFmpeg | `standard/lossless` | `standard/hevc-nvidia-ffmpeg` |
| Lossless photos, NVIDIA HEVC with HandBrake | `standard/lossless` | `standard/hevc-nvidia-handbrake` |
| Lossless photos, AMD/Intel VAAPI video | `standard/lossless` | `standard/hevc-vaapi` |

The NVIDIA FFmpeg video config preserves stored frame dimensions and display
rotation. Confirm that the container user can access `/dev/dri` for VAAPI;
some systems require adding the host render group.

This profile disables FFmpeg autorotation, preserves the stored frame
dimensions and source display rotation, applies no scale or crop filters, and
copies audio without re-encoding. It outputs MP4 and uses ExifTool to restore
writable GPS, camera, capture-date, and rotation metadata from the source.
Proprietary timed metadata streams that FFmpeg cannot remux are omitted. For
example, stored
`3840x2160` frames with a `-90` degree display rotation remain `3840x2160`
frames with the rotation metadata preserved.

The profile uses broadly compatible 8-bit HEVC NVENC settings because forcing
10-bit output or Temporal AQ can cause otherwise NVENC-capable GPUs to reject
the encode. Test GPU access and encoder initialization inside the container:

```bash
docker exec immich-optimizer nvidia-smi
docker exec immich-optimizer ffmpeg -hide_banner -f lavfi \
  -i color=size=1280x720:rate=30 -t 1 -c:v hevc_nvenc -f null -
```

Image configs have names independent of video codecs and GPUs:

| Image config | Behavior |
| --- | --- |
| `standard/lossless` | Reversible JPEG-to-JXL; supported SDR raster images use pixel-lossless JXL; detected HDR and other formats retain originals |
| `standard/lossless-caesium` | Caesium lossless optimization, retaining source formats |
| `standard/balanced-jxl` | JPEG-to-JXL at distance 1; supported non-JPEG images use lossless JXL |
| `standard/balanced-caesium` | JPEG quality 85; PNG/WebP lossless optimization |
| `standard/balanced-avif` | JPEG/PNG/WebP to AVIF at quality 65 |
| `standard/jxl-q75` | Legacy quality-75 image recipe, including HEIC conversion |
| `standard/jxl-resize` | Resize to fit 4096×4096 and encode JXL at quality 80 |
| `standard/passthrough-images` | Upload original images |
| `perceptual/webp`, `perceptual/avif`, `perceptual/jxl` | Search for image quality meeting the Image Score |

Standard video choices are `standard/av1-crf28`, `standard/hevc-nvidia-ffmpeg`,
`standard/hevc-nvidia-handbrake`, `standard/hevc-nvidia-fast`,
`standard/hevc-vaapi`, `standard/handbrake` (the legacy imported preset),
and `standard/passthrough-videos`. Perceptual video choices are
`perceptual/av1` and `perceptual/hevc`.

Standard choices work in the `:latest` image. Perceptual choices require the
`:perceptual` image. The dashboard disables quality controls unused by the
selected bundled config. Custom configs may still use those controls.

Standard source folders live under `config/standard`; perceptual sources live
under `config/perceptual`. Old combined standard folders are compatibility
recipes, not additional dashboard choices. Their image and video selections
migrate separately, including the former `mixed-lossless-images-*`,
`balanced-*-nvidia-ffmpeg`, `storage-saver-*-gpu`, `profile1`, and
`passthrough-all` names.

Metadata-copying profiles enable ExifTool's `ExtractEmbedded` option so tags
stored inside embedded media data are considered during copying and validation.

The AVIF profile uses ImageMagick with Debian's `libheif-plugin-aomenc` AV1
still-image encoder, which is included in the published container.

The dashboard's per-profile **Image Score** defaults to SSIMULACRA2 `85` and
applies to perceptual image configs. The perceptual AVIF image config searches
ImageMagick AVIF qualities with `fssimu2`; the perceptual WebP image config
searches `cwebp` qualities with `fssimu2`; and the perceptual JXL image config
searches `cjxl` qualities with `fssimu2`. All select the smallest output
meeting the selected score. The perceptual AV1 video config uses
`ab-av1 auto-encode` with the dashboard's per-profile **Video Score**, which
defaults to VMAF `95`, and SVT-AV1. It is
CPU-heavy, but it searches for an AV1 encode that meets a perceptual quality
target instead of applying one fixed quality number to every file.

JPEG XL supports both lossless and lossy compression. `perceptual/jxl` uses
lossy encoding and the Image Score controls its quality/size tradeoff. For
lossless images, select `standard/lossless` instead. It uses
`cjxl --lossless_jpeg=1` for JPEG and verifies byte-for-byte JPEG reconstruction;
supported SDR non-JPEG images use `-d 0`. It does not use the perceptual score.
Detected HDR images retain their originals, preserving HDR without lossy AVIF
conversion. Floating-point EXR/PFM and other unsupported formats also pass
through. Pixel-lossless JXL encoding uses `-d 0` (equivalently `-q 100`). Lossless JPEG reconstruction
alone does not guarantee that viewers render an embedded HDR gain map.

`perceptual/jxl` detects HDR-specific gain-map, headroom, and PQ/HLG metadata
before encoding. HDR sources use the sibling `perceptual/avif` script: supported
Ultra HDR JPEGs become AVIF with a validated lossless gain map, and unsupported
HDR sources retain their original format. SDR sources continue to become JXL.
This preserves HDR rather than flattening a gain-map JPEG to SDR; it does not
produce native HDR JXL. The two profile folders must remain siblings in custom
installations. The normal image savings threshold still applies.

The standard `av1-crf28` video config performs one SVT-AV1 encode at preset
`6`, without VMAF sampling. Its CRF comes from the dashboard's per-profile
**Video CRF** control and defaults to `28`. Lower values retain more quality
and produce larger files; higher values save more space. Changes apply to
future jobs, so you can adjust the value before processing a particular video
or batch. The profile is substantially faster and uses less CPU time than
perceptual auto-encode, while retaining high visual quality for typical phone
videos. It preserves source frame orientation, copies audio and chapters, and
restores and validates embedded metadata.

HDR preservation in these two profiles is conservative. `perceptual/avif`
converts supported Ultra HDR JPEGs to AVIF using libavif's native gain-map
conversion. The gain map is encoded losslessly; the selected SSIMULACRA2 score
measures decoded SDR-base PNGs, not reconstructed HDR quality. This avoids
passing gain-map AVIF containers directly to the metric decoder. The output gain map
is decoded and checked before and after metadata restoration. Unsupported JPEG
gain maps fall back to the original. HDR detected in other source formats
retains its original format; SDR photos use ImageMagick AVIF compression.
Generic JUMBF/C2PA metadata, portrait/auxiliary images, HEIC/HEIF containers,
and high bit depth alone do not mark a photo as HDR. Only HDR-specific gain-map,
headroom, or PQ/HLG transfer metadata selects HDR preservation. This requires
rebuilding the perceptual container image; custom installs need libavif 1.3.0 with `AVIF_LIBXML2` enabled and `avifenc`/`avifdec`
on PATH. Preserving HDR in an AVIF does not guarantee HDR rendering in Immich's
previews or every client. `standard/av1-crf28`
encodes PQ/HLG as 10-bit AV1 without tone mapping and checks color signalling
and static HDR metadata after encoding. Dolby Vision/HDR10+ inputs and outputs
that fail validation fall back to uploading the original. HDR probing scans
video frame metadata and adds processing time. Existing AV1 passthrough and
APAC preservation remain in effect.

The perceptual image builds `fssimu2` with a baseline CPU target and Zig
`ReleaseSafe` checks so cached build artifacts do not require the builder's CPU
features and internal traps retain diagnostics. Named `[smoke]` build checks
report the failing command and its exit code; `132` indicates SIGILL and can
mean incompatible CPU instructions or an internal tool trap.

Processing diagnostics for `perceptual/avif` and `standard/av1-crf28` include
`[avif]`/`[av1]` stage markers and explicit failure stages. Image logs list the
metadata tag names that triggered HDR handling, gain-map validation results,
and scorer exit codes. Video logs show input/output color signalling and
expected/actual values when validation fails. Successful HDR image encodes
print summaries; encoder diagnostics are printed in full on an encoding error.

The perceptual AV1 config samples at least five six-second sections, spaced
every two minutes, before selecting the full encode settings.

The perceptual HEVC video config uses `ab-av1` with `libx265`, VMAF `95`, 8-bit
`yuv420p`, and `hvc1` MP4 output. It generally saves less space than AV1, but
playback and sharing compatibility is much better across older clients.

When **Use NVIDIA** is enabled with `perceptual/hevc`, it replaces CPU
`libx265` with `hevc_nvenc`. It is much faster, though files may be larger than
CPU HEVC at the same VMAF target. VMAF analysis still runs on the CPU.
It searches NVENC constant-QP values for VMAF `95`; when a difficult source
cannot reach that target even at the highest-quality tested QP, it logs a
warning and uses the best measured result. If the selected result is projected
to save less than `20%`, the task fails before the full encode so the file can
be retried manually with a different profile.

### Windows folder compressor

[`scripts/compress-perceptual-hevc-webp-nvidia.ps1`](scripts/compress-perceptual-hevc-webp-nvidia.ps1)
provides the same WebP and VMAF-guided NVIDIA HEVC workflow for a local Windows
folder. It processes only files directly inside the folder, converts JPEG/PNG
to `filename.webp`, converts videos to `filename-hbed.mp4`, and skips existing
WebP files and videos whose names already end in `-hbed`.

It requires `ab-av1`, `cwebp`, ExifTool, and an FFmpeg build containing both
`libvmaf` and `hevc_nvenc` on `PATH`. Run it from PowerShell:

```powershell
.\scripts\compress-perceptual-hevc-webp-nvidia.ps1 "D:\Camera"

# Delete originals only after verified metadata-preserving smaller outputs
.\scripts\compress-perceptual-hevc-webp-nvidia.ps1 "D:\Camera" -DeleteOriginal
```

Use `-WhatIf` with `-DeleteOriginal` to preview deletions. Optional
`-VmafModelDirectory` can point to a folder containing `vmaf_v0.6.1.json` and
`vmaf_4k_v0.6.1.json`.

### 🚀 Custom Image (Additional Tools, Drivers, etc.)

The standard image includes FFmpeg, HandBrakeCLI, and the tools required by the
standard bundled profiles. Hardware encoding still requires compatible host
drivers and GPU passthrough. Use a custom image when a profile needs additional
tools, codecs, or driver-specific packages. There are also limitations with the
upstream HandBrake base image not supporting `arm64` (see
[jlesage/docker-handbrake#48](https://github.com/jlesage/docker-handbrake/issues/48)).

Instead of using the pre-built image, you can use your own Dockerfile, I provide a example [Dockerfile.custom](Dockerfile.custom) (should already have GPU acceleration working) as a starting point to bundle the latest **Immich Optimizer** binary directly **INTO your own specialized container environment**. This approach allows you to install **any additional packages or specific versions** (e.g. CUDA, specialized ffmpeg builds, specific driver versions, or custom tools) required for your specific hardware/workflow.

This is also a great alternative for users who want to **rely solely on `ffmpeg`** for video optimization without the overhead or specific requirements of the HandBrake base image. Or just want to run immich-optimizer binary directly without any container at all.

The following script downloads the latest **Immich Optimizer** binary from the GitHub releases page and installs it:

```Dockerfile
ARG IMMICH_OPTIMIZER_REPO=zivshek/immich-optimizer
RUN set -eux; \
    LATEST_TAG=$(curl -s https://api.github.com/repos/$IMMICH_OPTIMIZER_REPO/releases/latest | jq -r '.tag_name'); \
    case "$TARGETPLATFORM" in \
    "linux/amd64") ARCH=x86_64 ;; \
    "linux/arm64") ARCH=arm64 ;; \
    *) echo "Platform $TARGETPLATFORM not supported"; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/immich-optimizer.tar.gz \
    "https://github.com/$IMMICH_OPTIMIZER_REPO/releases/download/${LATEST_TAG}/immich-optimizer_Linux_${ARCH}.tar.gz"; \
    tar xzf /tmp/immich-optimizer.tar.gz -C /usr/local/bin immich-optimizer; \
    rm /tmp/immich-optimizer.tar.gz; \
    chmod +x /usr/local/bin/immich-optimizer
```

## ⚙️ Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `IUO_IMMICH_URL` | Immich server URL (required) | - |
| `IUO_IMMICH_API_KEY` | Immich API key (required) | - |
| `IUO_WATCH_DIR` | Directory to watch for files | `/watch` |
| `IUO_UNDONE_DIR` | Directory for files that failed processing/upload | `/undone` |
| `IUO_TASKS_FILE` | Startup fallback task configuration | `/etc/immich-optimizer/bundled-configs/standard/lossless/tasks.yaml` in the published images |
| `IUO_BUNDLED_CONFIGS_DIR` | Root directory scanned for dashboard-selectable task configurations | `/etc/immich-optimizer/bundled-configs` |
| `IUO_CUSTOM_PROFILES_DIR` | Root directory scanned for dashboard-selectable custom profiles | `/custom_profiles` |
| `IUO_PROFILES` | Comma-separated profile names configured through environment variables | - |
| `IUO_PROFILES_CONFIG` | Inline YAML profile list for Docker Compose and Dockge | - |
| `IUO_PROFILES_FILE` | Path to multi-user profiles configuration; enables profile mode | - |
| `IUO_DASHBOARD_ADDRESS` | Dashboard HTTP listen address | `:8098` |
| `IUO_STATS_DATABASE` | SQLite statistics database path | `/data/immich-optimizer.db` |

### Command Line Options

```bash
immich-optimizer [options]

Options:
  -immich_url string     Immich server URL
  -immich_api_key string Immich API key  
  -watch_dir string      Directory to watch (default "/watch")
  -undone_dir string     Directory for failed files (default "/undone")
  -tasks_file string     Tasks configuration file (default "tasks.yaml")
  -dashboard_address string Dashboard HTTP listen address (default ":8098")
  -stats_database string SQLite statistics database path (default "/data/immich-optimizer.db")
  -profiles_config string Inline YAML multi-user profiles configuration
  -profiles_file string  Multi-user profiles configuration file
  -version               Show version information
```

## 📋 Optimization Profiles

Image and video configs are selected independently in the dashboard. Use
`standard/lossless` for lossless images and `standard/passthrough-videos` for
unchanged videos. Choose a video encoder separately when video compression is
wanted. The tables above describe the available choices.

`IUO_TASKS_FILE` supplies the startup fallback. For the default
`standard/lossless/tasks.yaml`, the dashboard initializes lossless image
processing and video passthrough. Existing saved selections take precedence.

## 🛠️ Custom Configuration

Create a custom `tasks.yaml` file:

```yaml
tasks:
  - name: jpeg-xl-lossless
    command: cjxl --lossless_jpeg=1 {{.src_folder}}/{{.name}}.{{.extension}} {{.dst_folder}}/{{.name}}.jxl
    extensions:
      - jpeg
      - jpg
      
  - name: video-compress
    command: HandBrakeCLI -i {{.src_folder}}/{{.name}}.{{.extension}} -o {{.dst_folder}}/{{.name}}.mp4 --preset="Fast 1080p30"
    extensions:
      - avi
      - mkv
      - mov
      
  - name: passthrough
    command: ""  # Empty command passes file through unchanged
    extensions:
      - webp
      - avif
```

### Template Variables

Available in task commands:

- `{{.src_folder}}` - Source directory path
- `{{.dst_folder}}` - Destination directory path  
- `{{.name}}` - Filename without extension
- `{{.extension}}` - File extension without dot

## 🔧 Troubleshooting

### Common Issues

**Connection Refused**
```bash
# Check Immich URL and network connectivity
curl -I http://your-immich-instance:2283/api/server-info
```

**Permission Denied**
```bash
# Ensure watch directory is accessible
ls -la /path/to/watch
# Fix permissions if needed
chmod 755 /path/to/watch
```

**Task Failures**
```bash
# Check if required tools are installed
docker exec immich-optimizer which cjxl
docker exec immich-optimizer which caesiumclt
```

### Debug Mode

Enable verbose logging by setting log level:

```bash
# For binary
export LOG_LEVEL=debug
immich-optimizer

# For Docker
docker run -e LOG_LEVEL=debug ...
```

### Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [Immich](https://immich.app/) - The amazing self-hosted photo and video management solution
- [JPEG XL](https://jpegxl.info/) - Next-generation image compression
- [Caesium](https://saerasoft.com/caesium/) - Image compression tool
- [HandBrake](https://handbrake.fr/) - Video transcoder

## 📞 Support

- 🐛 [Report Issues](https://github.com/miguelangel-nubla/immich-optimizer/issues)
- 📖 [Documentation](https://github.com/miguelangel-nubla/immich-optimizer/wiki)
