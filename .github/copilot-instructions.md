# HLS Control - AI Coding Agent Instructions

## Project Overview

This is a Go-based HLS (HTTP Live Streaming) control server that receives UDP streams, converts them to HLS format using FFmpeg, and serves them via HTTP with real-time SSE (Server-Sent Events) notifications.

**Single-file architecture**: All logic is in `app/main.go` (~190 lines). This is intentional - keep it monolithic.

## Core Architecture

### Stream Processing Flow

1. **UDP Input** → FFmpeg conversion → **HLS Output** (m3u8 + .ts segments)
2. Each stream runs in its own goroutine with infinite retry loop
3. When stream ends/errors: cleanup segments, broadcast SSE event, restart immediately
4. FFmpeg parameters are tuned for low-latency: 4-second segments, 4-segment playlist

### Key Components

**EventManager** (SSE broadcasting):

-   Thread-safe pub/sub for stream events (`closed` type only currently)
-   Clients connect via `/events` endpoint, receive real-time updates
-   Pattern: `em.Broadcast(StreamEvent{channelId: index, eventType: "closed"})`

**startStream function**:

-   Infinite loop wrapper around FFmpeg process
-   Logs to both stdout and `logs/channel_N.log`
-   Auto-cleanup on stream end: removes `stream_N*.ts` and `stream_N.m3u8`
-   Uses `ffmpeg-go` library, NOT exec/Command

## Critical Conventions

### File Naming Patterns

-   HLS playlists: `streams/streamN.m3u8` (where N = channel index)
-   HLS segments: `streams/stream_N_%03d.ts` (3-digit counter)
-   Logs: `logs/channel_N.log`

### FFmpeg Configuration

Always use these exact flags in `startStream`:

```go
"c": "copy",              // No re-encoding
"f": "hls",
"hls_time": 4,            // 4-second segments
"hls_list_size": 4,       // Keep last 4 segments
"hls_flags": "delete_segments+append_list+program_date_time",
"hls_segment_filename": fmt.Sprintf("./streams/stream_%d", index) + "_%03d.ts",
```

### Concurrency Pattern

-   Each stream = 1 goroutine spawned in `main()` loop
-   EventManager uses RWMutex for client map access
-   SSE handler spawns goroutine for sending events (`go func()` in SSEHandler)

## Build & Run

**Build**: `./build.sh` creates static Linux binary in `dist/hls_control`

-   CGO disabled, cross-compile ready (GOOS/GOARCH set)

**Run**:

```bash
./hls_control -p 3002 -s 2220 -n 4 -i 192.168.1.100
```

-   `-p`: HTTP server port (default 3002)
-   `-s`: Starting UDP port (increments per stream)
-   `-n`: Number of concurrent streams
-   `-i`: UDP source IP (default localhost)

**Endpoints**:

-   `GET /streams/*` - Static file serving for HLS playlists/segments
-   `GET /events` - SSE endpoint for stream status

## Development Notes

-   **No tests exist** - manual testing with real UDP streams
-   **Error handling**: Errors logged, streams auto-restart (infinite loop)
-   **Dependencies**: Only `gin-gonic/gin` and `u2takey/ffmpeg-go` (+ transitive)
-   **FFmpeg required**: Must be installed on system (`ffmpeg` in PATH)
-   **Directory auto-creation**: `streams/` and `logs/` created if missing

## Common Tasks

**Adding new stream events**: Modify `StreamEvent` struct and add broadcast calls in `startStream`

**Changing HLS settings**: Edit KwArgs in `startStream` FFmpeg call - beware of latency/quality tradeoffs

**Debugging stream issues**: Check `logs/channel_N.log` for FFmpeg output and `error.log` for app-level errors
