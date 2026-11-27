<p align="center">
  <img src="assets/app/logo.svg" alt="WhatsMySpeed Logo" width="88" />
</p>

<h1 align="center">WhatsMySpeed</h1>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.27+" />
  <img src="https://img.shields.io/badge/Platform-Windows%2011%20(x64)-0078D4?logo=windows&logoColor=white" alt="Windows 11 x64" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT" /></a>
</p>

**WhatsMySpeed** is a lightweight, native Windows 11 network speed monitor and speed test app built in pure Go and Win32 APIs (zero CGO). It sits cleanly on the Windows 11 taskbar beside the system tray overflow chevron (`^`) and matches Windows Light, Dark, and High Contrast themes.

## Features

- **Real-Time Taskbar Speed Meter:** Polls active network adapters every second via `GetIfTable2`, filtering out loopback and virtual filter shims to avoid double counting.
- **Bytes/s & Bits/s Formatting:** Scales automatically from `B/s` to `GB/s` or `bps` to `Gbps` using fixed-width columns so numbers stay steady.
- **Customizable Overlay & Quick Menu:**
  - **Display Modes:** `Show Both`, `Download Only`, `Upload Only`, `Hide Speed`
  - **Label Styles:** `Arrows` (vector icons), `U / D`, or `Up / Down`
  - **Layouts:** `Stacked (Upload Top)`, `Stacked (Download Top)`, `Inline`, and `Compact` across horizontal and vertical taskbars
  - **Context Menu:** Custom Windows 11 rounded popup menu from both the tray icon and taskbar meter
- **Native Hover Tooltip:** Native `tooltips_class32` hover popup with DPI-scaled text, bold vector direction arrows, and double-buffered live updates.
- **Built-In Speed Test & Dashboard:**
  - Open by launching `WhatsMySpeed.exe`, clicking the tray icon, double-clicking the taskbar meter, or selecting **Open WhatsMySpeed**.
  - Includes a 270-degree animated gauge running Ping (`8` probes), Download (`8s`), and Upload (`8s`) tests over `4` parallel streams against Cloudflare Edge.
  - Includes Light/Dark/System theme switching, live header speeds, and a `Start with Windows` toggle.
- **Native Windows 11 Behavior:** Supports Per-Monitor V2 DPI scaling, Explorer restarts, taskbar auto-hide, fullscreen hiding, third-party taskbar topmost fallback, single-instance focus, `--tray` silent startup, and local JSON config persistence.

## Project Structure

```text
WhatsMySpeed/
├── assets/                 # App icon (.ico, .png, .svg) and embedded Phosphor SVGs
├── cmd/whatsmyspeed/       # Entry point, single-instance guard, manifest & Win32 resources
├── installer/msix/         # Template MSIX manifest and Store tile assets
├── internal/
│   ├── config/             # JSON settings persistence and Windows startup (Run / StartupTask)
│   ├── network/            # Win32 GetIfTable2 monitor and unit formatters
│   ├── speedtest/          # Multi-stream Ping, Download, and Upload speed test engine
│   └── ui/                 # GDI+, overlay, native tooltip, popup menu, tray & dashboard UI
├── build.ps1               # Build standalone WhatsMySpeed.exe
└── build-msix.ps1          # Stage and package MSIX installer
```

## Build & Run

### Prerequisites

- Windows 11 (x64)
- Go 1.27+ (pure Go, `CGO_ENABLED=0`)

### Build executable

```powershell
.\build.ps1
.\WhatsMySpeed.exe
```

Or with `go build`:

```powershell
go build -trimpath -ldflags="-s -w -H=windowsgui" -o WhatsMySpeed.exe ./cmd/whatsmyspeed
.\WhatsMySpeed.exe
```

Use `--tray` to start silently in the taskbar and tray.

### Build MSIX package

```powershell
.\build-msix.ps1
```

Override Store identity flags cleanly via CLI when packaging a signed release:

```powershell
.\build-msix.ps1 -IdentityName "YourPublisher.WhatsMySpeed" -Publisher "CN=YOUR_PUBLISHER_ID" -PublisherDisplayName "Your Publisher Name" -Version "1.0.0.0"
```

### Run tests & checks

```powershell
go test -v -count=1 ./...
go vet ./...
```

## Configuration

Settings are saved locally at `%APPDATA%\WhatsMySpeed\config.json`:

```json
{
  "display_mode": "both",
  "speed_unit": "bytes",
  "label_style": "arrows",
  "layout": "stacked_up",
  "theme": "system",
  "start_with_windows": true
}
```

- `display_mode`: `"both"` (default), `"download"`, `"upload"`, `"hide"`
- `speed_unit`: `"bytes"` (default) or `"bits"`
- `label_style`: `"arrows"` (default), `"ud"`, `"words"`
- `layout`: `"stacked_up"` (default), `"stacked_down"`, `"inline"`, `"compact"`
- `theme`: `"system"` (default), `"dark"`, `"light"`
- `start_with_windows`: `true` (default) or `false`

See [PRIVACY.md](PRIVACY.md) for privacy details.

## Contributing

Pull requests and bug reports are welcome. Run `go test ./...` and `go vet ./...` before submitting changes.

## 🧑‍💻 Author

Created and maintained by [Shubham Bhamare](https://github.com/theshubh77).

If you find this project useful, consider giving the repository a ⭐!

## License & Credits

- Licensed under the [MIT License](LICENSE).
- App icon based on `andrsrxn:wifi-in-out` by [Andrés Raxón](https://github.com/andrsrxn/icons) (MIT License).
- UI and direction icons from [Phosphor Icons](https://phosphoricons.com/) (MIT License).
