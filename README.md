# WhatsMySpeed

A lightweight, native Windows 11 real-time internet speed monitor written in pure Go and Win32 APIs. **WhatsMySpeed** docks directly onto the Windows 11 taskbar to the left of the system tray overflow chevron (`^`), blending seamlessly with native Light and Dark taskbar themes.

## Features

- **Real-Time Speed Monitoring:** Polls active, non-loopback network interfaces every second using the Windows IP Helper API (`GetIfTable2`), filtering out NDIS/WFP filter shims to avoid double-counting traffic.
- **Jitter-Free Smart Formatting:** Automatically scales across **`Bytes/s`** (`B/s`, `KB/s`, `MB/s`, `GB/s`) or **`Bits/s`** (`bps`, `Kbps`, `Mbps`, `Gbps` — matching ISP plans, speed tests, and Task Manager) using fixed-width columns so numbers never jump horizontally as throughput changes.
- **Native Windows 11 Integration:**
  - Borderless, transparent, click-responsive layered window (`WS_EX_TOOLWINDOW | WS_EX_TOPMOST | WS_EX_LAYERED | WS_EX_NOACTIVATE`) with zero focus stealing.
  - Automatically docks beside `TrayNotifyWnd` (just left of the `^` chevron) and reacts to taskbar resize, DPI scaling, display changes, Explorer restarts, auto-hide, and fullscreen apps.
  - Automatically detects Windows 11 Light/Dark taskbar theme (`SystemUsesLightTheme` / `AppsUseLightTheme`) and renders anti-aliased `Segoe UI Variable Text` (falling back to `Segoe UI`).
- **Phosphor Icons Embedded:** Bundles official [Phosphor Icons](https://phosphoricons.com/) (`arrow-up`, `arrow-down`, and `gauge`) directly into the standalone `.exe` via `//go:embed` and renders them crisply with GDI+ at any DPI.
- **Configurable Display Modes, Units & Autostart:**
  - `Show Both` (Upload stacked over Download)
  - `Download Only`
  - `Upload Only`
  - `Hide Speed` (keeps running in the system tray)
  - `Bytes/s (KB/s, MB/s)` or `Bits/s (Kbps, Mbps)` unit toggle
  - `Start with Windows` (`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`)
  - Persists preferences in `%APPDATA%\WhatsMySpeed\config.json`

## Project Structure

```text
WhatsMySpeed/
├── assets/
│   ├── assets.go               # Embedded Phosphor SVG assets (//go:embed)
│   └── icons/                  # Official Phosphor SVG icons
├── cmd/
│   └── whatsmyspeed/
│       └── main.go             # Application entry point & single-instance guard
├── internal/
│   ├── config/
│   │   ├── autostart.go        # Windows startup registry management
│   │   └── config.go           # JSON settings persistence (%APPDATA%\WhatsMySpeed\config.json)
│   ├── network/
│   │   ├── format.go           # Smart unit formatting (B/s - GB/s)
│   │   └── monitor.go          # Win32 GetIfTable2 bandwidth sampler
│   └── ui/
│       ├── gdiplus.go          # Pure-Go GDI+ flat API bindings
│       ├── icons.go            # SVG path parser & GDI+ vector rasterizer
│       ├── overlay.go          # Layered taskbar overlay & render loop
│       ├── taskbar.go          # Shell_TrayWnd / TrayNotifyWnd positioning & fullscreen detection
│       ├── theme.go            # Windows 11 Light/Dark taskbar theme detection
│       ├── tray.go             # Shell_NotifyIconW system tray & context menu
│       └── win32.go            # Win32 syscalls & DPI awareness
├── build.ps1                   # Standalone Windows GUI build script
├── go.mod
├── LICENSE
└── README.md
```

## Prerequisites

- **OS:** Windows 11 (x64)
- **Go:** Go 1.22+ (pure Go, no CGO / GCC required)

## Build & Run

### Using the build script (PowerShell)

```powershell
.\build.ps1
.\WhatsMySpeed.exe
```

### Using `go build` directly

```powershell
go build -trimpath -ldflags="-s -w -H=windowsgui" -o WhatsMySpeed.exe ./cmd/whatsmyspeed
.\WhatsMySpeed.exe
```

> `-H=windowsgui` builds a native GUI subsystem executable so no console window appears on launch.

### Running Tests

```powershell
go test -v ./...
```

## Configuration

Settings are automatically saved to:

```text
%APPDATA%\WhatsMySpeed\config.json
```

Example:

```json
{
  "display_mode": "both",
  "speed_unit": "bytes"
}
```

- Valid values for `display_mode`: `"both"`, `"download"`, `"upload"`, `"hide"`.
- Valid values for `speed_unit`: `"bytes"` (`B/s`–`GB/s`, default) or `"bits"` (`bps`–`Gbps`).

## Contributing

Contributions, bug reports, and pull requests are welcome! Please run `go test ./...` before submitting changes.

## License

This project is open source and licensed under the [MIT License](LICENSE).
