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
  upload. Failed files are copied to the profile's `undone` directory.
- **FolderSync-aware watching:** Handles atomic file placement and hardlinks,
  and ignores hidden files/directories such as `.trashed-*`.
- **Fork publishing:** Multi-architecture images are published to
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
- Tags such as `v1.2.3` publish `1.2.3`, `1.2`, `1`, and `sha-...`.
- Pull requests run tests but do not publish images.

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
      - IUO_TASKS_FILE=/etc/immich-optimizer/config/tasks.yaml
    volumes:
      - /path/to/watch:/watch
      - /path/to/undone:/undone
      - ./optimizer-data:/data
      # Optional: Custom configuration
      - ./custom-config:/etc/immich-optimizer/config
    ports:
      - "8098:8098"
    restart: unless-stopped
```

Open `http://your-server:8098` to view the dashboard. Persist `/data` so
statistics survive container recreation. The dashboard records only successful
Immich uploads; failed/current jobs remain visible in the live log.

The dashboard does not currently provide authentication. Keep port `8098` on a
trusted network or place it behind an authenticated reverse proxy. When using a
custom Compose `user: "UID:GID"`, ensure that UID/GID can write to the host
directory mounted at `/data`.

### Multi-user profiles

Set `IUO_PROFILES_CONFIG` to a YAML block to run isolated watchers and Immich
API clients directly from Docker Compose. `IUO_IMMICH_URL` and
`IUO_TASKS_FILE` are shared. Each profile only defines its user, API key,
watch directory, and failure directory.

```yaml
environment:
  IUO_IMMICH_URL: http://immich-server:2283
  IUO_TASKS_FILE: /etc/immich-optimizer/bundled-configs/lossless/tasks.yaml
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

The bundled `storage-saver-amd-gpu` and `storage-saver-nvidia-gpu` task
profiles provide starting points. Confirm the container user can access
`/dev/dri` on VAAPI hosts; some systems require adding the host render group.

For lossless image optimization combined with HandBrake NVIDIA video
transcoding, use:

```yaml
IUO_TASKS_FILE: /etc/immich-optimizer/bundled-configs/mixed-lossless-images-nvidia-handbrake/tasks.yaml
```

`cjxl --lossless_jpeg=1` is specifically a reversible JPEG-to-JXL conversion.
It preserves the original JPEG bitstream inside the JXL file. For non-JPEG
images, the mixed profile uses `cjxl -d 0` for pixel-lossless conversion or
Caesium lossless optimization while preserving the original format.

For the same lossless image handling with phone-video rotation preserved as
display metadata and high-quality FFmpeg NVENC encoding, use:

```yaml
IUO_TASKS_FILE: /etc/immich-optimizer/bundled-configs/mixed-lossless-images-nvidia-ffmpeg/tasks.yaml
```

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

Three balanced NVIDIA/FFmpeg profiles are bundled for different image
compatibility and compression goals:

```yaml
# JPEG XL: strongest JPEG recompression, but limited support after download
IUO_TASKS_FILE: /etc/immich-optimizer/bundled-configs/balanced-jxl-nvidia-ffmpeg/tasks.yaml

# Standard JPEG: broadest compatibility
IUO_TASKS_FILE: /etc/immich-optimizer/bundled-configs/balanced-caesium-nvidia-ffmpeg/tasks.yaml

# AVIF: stronger compression with wider support than JPEG XL
IUO_TASKS_FILE: /etc/immich-optimizer/bundled-configs/balanced-avif-nvidia-ffmpeg/tasks.yaml

# Perceptual AV1: oavif SSIMULACRA2 images and ab-av1 VMAF videos
IUO_TASKS_FILE: /etc/immich-optimizer/bundled-configs/perceptual-av1/tasks.yaml
```

The JPEG XL profile uses distance `1.0`; the Caesium profile keeps JPEG/JPG as
standard JPEG at quality `85`; and the AVIF profile converts JPEG, PNG, and
WebP to AVIF at ImageMagick quality `65`. All three restore and validate photo
metadata after conversion and use the same orientation-preserving NVIDIA video
task. These image conversions are lossy and cannot reconstruct the original
files bit-for-bit.

The AVIF profile uses ImageMagick with Debian's `libheif-plugin-aomenc` AV1
still-image encoder, which is included in the published container.

The perceptual AV1 profile uses `oavif --score-tgt 85` for images and
`ab-av1 auto-encode --min-vmaf 95` for videos. It is CPU-heavy, but it searches
for an AV1 encode that meets a perceptual quality target instead of applying
one fixed quality number to every file. Videos are remuxed to MP4 with the
original audio and metadata after the VMAF-guided video stream is encoded.

### 🚀 Custom Image (GPU Acceleration, FFMPEG, etc.)

Hardware-accelerated video encoding (NVidia NVENC, Intel VAAPI, etc.) is **not included in the base image** because providing a one-size-fits-all solution is complex and leads to massive image fragmentation. Furthermore, there are some limitations with the upstream HandBrake base image not supporting `arm64` (see [jlesage/docker-handbrake#48](https://github.com/jlesage/docker-handbrake/issues/48)).

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
| `IUO_TASKS_FILE` | Path to tasks configuration | `tasks.yaml` |
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

The optimizer includes three pre-configured profiles:

### 🔒 Lossless Profile (Default)
```yaml
# Located at: config/lossless/tasks.yaml
# - Lossless JPEG-XL conversion for images
# - Caesium lossless compression
# - Passthrough for videos (no compression)
```

### ⚡ Lossy Profile
```yaml
# Located at: config/profile1/tasks.yaml  
# - Lossy JPEG-XL conversion (quality 75)
# - Caesium compression (quality 85)
# - HandBrake video compression
# - HEIC to JPEG-XL conversion
```

### 📤 Passthrough Profile
```yaml
# Located at: config/passthrough-all/tasks.yaml
# - No optimization, uploads files as-is
# - Useful for testing or when optimization is not desired
```

## 🛠️ Custom Configuration

Create a custom `tasks.yaml` file:

```yaml
tasks:
  - name: jpeg-xl-lossless
    command: cjxl --lossless_jpeg=1 {{.src_folder}}/{{.name}}.{{.extension}} {{.dst_folder}}/{{.name}}.jxl
    extensions:
      - jpeg
      - jpg
      - png
      
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
