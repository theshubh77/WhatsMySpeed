<p align="center">
  <img src="assets/app/logo.svg" alt="WhatsMySpeed Logo" width="88" />
</p>

<h1 align="center">WhatsMySpeed</h1>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.27+" />
  <img src="https://img.shields.io/badge/Platform-Windows%2011%20(x64)-0078D4?logo=windows&logoColor=white" alt="Windows 11 x64" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT" /></a>
</p>

**WhatsMySpeed** is a lightweight, native Windows 11 network speed monitor and diagnostic speed test utility written in pure Go and Win32 APIs (zero CGO). It renders live download and upload throughput directly on the Windows 11 taskbar beside the system tray overflow chevron (`^`), adapting automatically to system themes, High Contrast, taskbar orientations, and Per-Monitor V2 DPI scaling.

## Features

- **Real-Time Taskbar Bandwidth Meter:**
  - Samples active, non-loopback network adapters every second via the Windows IP Helper API (`GetIfTable2`), filtering out NDIS/WFP filter shims and virtual switches to prevent double counting.
  - Supports **Bytes/s** (`B/s`, `KB/s`, `MB/s`, `GB/s`) and **Bits/s** (`bps`, `Kbps`, `Mbps`, `Gbps`) with fixed-width measured columns so values never jump horizontally.
- **Customizable Overlay & Quick Menu:**
  - **Display Modes:** `Show Both`, `Download Only`, `Upload Only`, `Hide Speed`
  - **Label Styles:** `Arrows` (embedded Phosphor vector icons), `U / D`, or `Up / Down`
  - **Layouts:** `Stacked (Upload Top)`, `Stacked (Download Top)`, `Inline`, and `Compact` (adapts to horizontal top/bottom and vertical left/right taskbars)
  - Custom theme-aware Windows 11 DWM-rounded popup context menu accessible from both the tray icon and taskbar overlay.
- **Native Taskbar Hover Tooltip:**
  - Backed by a genuine subclassed Win32 `tooltips_class32` window matching Windows 11 `DarkMode_Explorer`, `Explorer` (Light), and High Contrast visuals.
  - Renders enlarged vector direction icons and clear live download/upload values using offscreen DIB double-buffering, stable hover-session column widths, and automatic occlusion hiding for flicker-free updates.
- **Built-In Multi-Stream Speed Test & Dashboard:**
  - Opens on normal app launch, tray icon click/double-click, taskbar meter double-click, or **Open WhatsMySpeed** from the context menu.
  - Features a 270-degree animated speedometer gauge, live header throughput badge, theme-styled dashboard tooltips, and an end-to-end diagnostic pipeline against Cloudflare Edge: `8` ping probes (latency + jitter), `8s` Download, and `8s` Upload across `4` parallel streams with `2s` TCP slow-start warmup exclusion and `1.5s` inter-phase pauses (`Run Speed Test` / `Cancel` / `Test Again`).
  - Interactive pill controls and smooth animated switch/theme transitions (`Light`, `Dark`, `System`, and `Start with Windows`).
- **Native Windows 11 Integration & Persistence:**
  - Docks inside `Shell_TrayWnd` alongside `TrayNotifyWnd` with automatic topmost fallback when third-party taskbar tools clip child regions, maintaining proper Z-order above shell surfaces and hiding cleanly over fullscreen apps.
  - Responds live to display/DPI changes (`PerMonitorV2`), Explorer restarts, theme switches, taskbar resizing/auto-hide, and sleep/resume events.
  - Single-instance mutex prevents duplicate instances while signaling the active dashboard window; `--tray`, `--minimized`, and `--background` flags (plus MSIX `StartupTask` activation) launch silently to the tray.
  - Persists preferences in `%APPDATA%\WhatsMySpeed\config.json` and synchronizes startup state via `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` (standalone) or `Windows.ApplicationModel.StartupTask` (MSIX).

## Project Structure

```text
WhatsMySpeed/
├── .github/
│   └── FUNDING.yml                 # Optional GitHub Sponsors metadata
├── assets/
│   ├── app/                        # Application icon (.ico, .png, .svg)
│   ├── assets.go                   # Embedded app logo & Phosphor SVGs (//go:embed)
│   └── icons/                      # Official Phosphor SVG icons
├── cmd/
│   └── whatsmyspeed/
│       ├── app.manifest            # Per-Monitor V2 DPI & Common Controls v6 manifest
│       ├── app.rc                  # Windows executable icon & version resource script
│       ├── main.go                 # Entry point, single-instance mutex & launch flags
│       └── rsrc_windows_amd64.syso # Pre-compiled Windows resources linked by go build
├── internal/
│   ├── config/
│   │   ├── autostart.go            # Startup integration (HKCU Run & MSIX StartupTask)
│   │   ├── config.go               # Atomic JSON config persistence
│   │   ├── config_test.go          # Config & startup tests
│   │   ├── msix.go                 # WinRT StartupTask & package identity helpers
│   │   └── msix_test.go            # MSIX / WinRT activation tests
│   ├── network/
│   │   ├── format.go               # Unit scaling (B/s to GB/s and bps to Gbps)
│   │   ├── monitor.go              # Win32 GetIfTable2 throughput sampler
│   │   └── network_test.go         # Formatter & interface filtering tests
│   ├── speedtest/
│   │   ├── speedtest.go            # Multi-stream Ping, Download & Upload engine
│   │   └── speedtest_test.go       # Local mock server & cancellation tests
│   └── ui/
│       ├── gdiplus.go              # Pure-Go GDI+ flat API bindings
│       ├── icons.go                # SVG parser & DPI-aware GDI+ vector rasterizer
│       ├── menu.go                 # Custom Windows 11 DWM-rounded popup context menu
│       ├── overlay.go              # Layered taskbar overlay & Win32 message loop
│       ├── overlay_tooltip.go      # Subclassed double-buffered native Win32 tooltip
│       ├── settings_window.go      # Dashboard UI, speedometer gauge & speed test view
│       ├── taskbar.go              # Taskbar bounds, tray detection & fullscreen check
│       ├── theme.go                # System/Light/Dark & High Contrast theme helpers
│       ├── tray.go                 # Shell_NotifyIconW tray icon integration
│       ├── ui_test.go              # UI, Win32 struct, tooltip & animation tests
│       ├── uia_tray.go             # UI Automation tray overflow boundary probe
│       └── win32.go                # Win32, UxTheme & DWM syscall bindings
├── installer/
│   └── msix/
│       ├── AppxManifest.xml        # Template MSIX manifest (placeholder identity fields)
│       └── Assets/                 # Store & Start menu tile PNGs
├── build.ps1                       # Standalone console-less Windows x64 build script
├── build-msix.ps1                  # Automated MSIX staging & packaging script
├── go.mod
├── go.sum
├── LICENSE
├── PRIVACY.md
└── README.md
```

## Prerequisites

- **OS:** Windows 11 (x64)
- **Go:** Go 1.27+ (`CGO_ENABLED=0`, no GCC / CGO toolchain required)
- **Optional:** `x86_64-w64-mingw32-windres` if recompiling `rsrc_windows_amd64.syso` from `app.rc`; Windows SDK tools (or auto-fetched lightweight NuGet build tools via `build-msix.ps1`) for MSIX packaging

## Build & Run

### Using the PowerShell Build Script

```powershell
.\build.ps1
.\WhatsMySpeed.exe
```

### Using `go build` Directly

```powershell
go build -trimpath -ldflags="-s -w -H=windowsgui" -o WhatsMySpeed.exe ./cmd/whatsmyspeed
.\WhatsMySpeed.exe
```

> `-H=windowsgui` links a GUI subsystem binary without spawning a console window. Passing `--tray` starts silently minimized to the taskbar and notification area.

### Building an MSIX Package

```powershell
.\build-msix.ps1
```

For Store submission or signed distribution, override identity values strictly via CLI parameters rather than modifying tracked manifest templates:

```powershell
.\build-msix.ps1 -IdentityName "YourPublisher.WhatsMySpeed" -Publisher "CN=YOUR_PUBLISHER_ID" -PublisherDisplayName "Your Publisher Name" -Version "1.0.0.0"
```

### Running Tests & Static Checks

```powershell
go test -v -count=1 ./...
go vet ./...
```

## Configuration

Preferences are stored locally at:

```text
%APPDATA%\WhatsMySpeed\config.json
```

Example default configuration:

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
- `speed_unit`: `"bytes"` (`B/s` to `GB/s`, default) or `"bits"` (`bps` to `Gbps`)
- `label_style`: `"arrows"` (default), `"ud"`, or `"words"`
- `layout`: `"stacked_up"` (default), `"stacked_down"`, `"inline"`, or `"compact"`
- `theme`: `"system"` (default), `"dark"`, or `"light"`
- `start_with_windows`: `true` (default) or `false`

See [PRIVACY.md](PRIVACY.md) for details on local telemetry-free operation and Cloudflare Edge speed test requests.

## Contributing & Repository Hygiene

Pull requests and bug reports are welcome. Before submitting changes, run `go test ./...` and `go vet ./...`.

To keep the repository clean, reproducible, and secure, keep tracked vs. local-only files strictly separated via [.gitignore](.gitignore):

### Should Be Pushed (Tracked Source)
- Application source and unit tests (`cmd/`, `internal/`, `assets/`)
- Module dependencies and build/package templates (`go.mod`, `go.sum`, `build.ps1`, `build-msix.ps1`, `installer/msix/`)
- Public documentation and legal files (`README.md`, `LICENSE`, `PRIVACY.md`, `.gitignore`)

### Should Not Be Pushed (Local Only / Ignored)
- **Personal / local assistant or workflow instructions** (`AGENTS.md`, `agent.md`, `CLAUDE.md`, `GEMINI.md`, `.agent/`, `.claude/`, `.gemini/`, `.cursor/`, `.cursorrules`, `.windsurfrules`, `.clinerules`)
- **Publisher secrets, signing keys, and certificates** (`*.pfx`, `*.cer`, `*.pvk`, `*.snk`, and Partner Center publisher IDs; pass release IDs via `build-msix.ps1` flags instead of editing `AppxManifest.xml`)
- **Compiled binaries, installers, and fetched SDK tools** (`WhatsMySpeed.exe`, `*.msix`, `*.pri`, `/build/`, `/dist/`, `/tools/`)
- **Temporary logs and editor/OS metadata** (`*.log`, `*.tmp`, `.vscode/`, `.idea/`, `Thumbs.db`, `.DS_Store`)

## License & Acknowledgments

- Licensed under the [MIT License](LICENSE).
- App icon based on `andrsrxn:wifi-in-out` by [Andrés Raxón](https://github.com/andrsrxn/icons) (MIT License).
- Directional and UI icons from [Phosphor Icons](https://phosphoricons.com/) (MIT License).
