# dropzone

A lightweight, cross-platform **system-tray applet** that watches for new
screenshots and screen recordings, compresses them, and uploads them to your
**Cloudflare R2** bucket automatically. After each upload a clean public URL is
copied to your clipboard (ShareX-style).

- **Screenshots** → compressed to **lossless WebP** → uploaded.
- **Recordings** → **original uploaded first** (fast availability) → re-encoded
  with **ffmpeg (H.265 by default)** → the R2 object is **replaced** with the
  smaller version.
- Watches your OS capture folders **and the clipboard** (so `Win+Shift+S` works).
- **Local files are never modified.**

## Requirements

- **Go 1.24+** and a **C compiler** to build (the tray library uses cgo).
  On Windows: [WinLibs](https://winlibs.com/) / MSYS2 / TDM-GCC. `CGO_ENABLED=1`.
- **ffmpeg** on your `PATH` for video compression (screenshots work without it).
  If ffmpeg is missing, recordings are still uploaded — just not compressed.

## Build

```sh
# from the repo root
go mod tidy
CGO_ENABLED=1 go build -o dropzone ./cmd/dropzone
# Windows (hide the console window for a tray app):
#   go build -ldflags "-H=windowsgui" -o dropzone.exe ./cmd/dropzone
```

## Configure

Run the interactive setup once:

```sh
./dropzone --setup
```

It stores non-secret settings in a JSON config and your **secret access key in
the OS keychain** (Windows Credential Manager / macOS Keychain / Linux Secret
Service). Config location:

| OS      | Path                                                        |
|---------|-------------------------------------------------------------|
| Windows | `%AppData%\dropzone\config.json`                                 |
| macOS   | `~/Library/Application Support/dropzone/config.json`             |
| Linux   | `~/.config/dropzone/config.json`                                 |

You'll be asked for:

- **Account ID** — your Cloudflare account ID.
- **Access Key ID** / **Secret Access Key** — an R2 API token, ideally scoped to
  just this one bucket (least privilege).
- **Bucket name**.
- **Public base URL** — your bucket's **custom domain** (e.g.
  `https://cdn.example.com`), used to build the shareable links.

Verify credentials any time with `./dropzone --verify`.

See [`config.example.json`](config.example.json) for all options.

### Config options

| Key | Meaning |
|-----|---------|
| `watchFolders` | Explicit folders to watch. Empty = auto-detect OS defaults. |
| `watchClipboard` | Also upload images copied to the clipboard. |
| `copyLinkToClipboard` | Copy the public URL to the clipboard after upload. |
| `keyPrefix` | Optional root prefix for all object keys. |
| `image.format` | `webp-lossless` (default) or `jpeg`. |
| `image.jpegQualityFallback` | JPEG quality if WebP fails / `format=jpeg`. |
| `video.codec` | `h265` (default), `h264`, or `av1`. |
| `video.crf` | Quality (lower = better). `0` = codec default (h265 28 / h264 23 / av1 30). |
| `video.preset` | ffmpeg preset (`medium` default; ignored for av1). |
| `video.ffmpegPath` | Explicit ffmpeg path (else looked up on `PATH`). |

## Run

Launch `dropzone` (double-click the built binary or run it from a terminal). A tray
icon appears with:

- **Status** — current activity (watching / uploading / paused).
- **Pause / Resume** — stop or resume processing.
- **Open config folder**.
- **Start at login** — toggle autostart (no admin needed).
- **Quit**.

## Object layout in R2

```
[keyPrefix/]images/2026/07/03/<ulid>.webp
[keyPrefix/]video/2026/07/03/<ulid>.mp4
```

Keys are date-organized and use a time-sortable ULID to avoid collisions.

## Notes / limitations

- **Detection is folder + clipboard based.** Default capture folders are
  auto-detected per OS but are user-relocatable; set `watchFolders` if yours
  differ (especially on Linux, where it varies by desktop).
- **Pause drops captures** taken while paused (local files are kept, so nothing
  is lost — they just aren't uploaded).
- **Linux tray** needs a StatusNotifierItem/AppIndicator host (GNOME needs the
  AppIndicator extension).
