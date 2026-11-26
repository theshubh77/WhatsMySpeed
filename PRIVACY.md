# Privacy Policy - WhatsMySpeed

Last updated: November 26, 2026

**WhatsMySpeed** is an open-source, privacy-first Windows 11 network speed monitor.

## 1. Local Speed Monitoring
- Live upload and download counters on the taskbar are read strictly locally from the Windows IP Helper API (`GetIfTable2`).
- **No traffic contents, URLs, DNS queries, browsing history, or packet payloads are ever inspected, recorded, or transmitted.**

## 2. Optional Built-In Speed Test
- Only when you explicitly click **Run Speed Test** (or **Test Again**), WhatsMySpeed performs HTTPS latency, download, and upload throughput tests against **Cloudflare Edge** (`https://speed.cloudflare.com/__down` and `/__up`).
- Your IP address is seen solely by Cloudflare Edge endpoints as part of handling those standard HTTPS test requests. No account, device identifier, or user data is sent or retained by the developer.

## 3. Local Settings Only
- Display preferences (`display_mode`, `speed_unit`, `label_style`, `layout`, `theme`, `start_with_windows`) are stored strictly locally on your device inside `%APPDATA%\WhatsMySpeed\config.json` (or inside your Windows app package container when installed from the Microsoft Store).
- Startup registration uses either the standard Windows `StartupTask` API (Microsoft Store version) or `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` (standalone version) and can be disabled at any time in Settings, the tray menu, or Windows Settings -> Apps -> Startup.

## 4. Telemetry, Ads & Third-Party Sharing
- **WhatsMySpeed** contains **no analytics, no telemetry, no crash reporters, no advertisements, and no third-party tracking SDKs**.
- **No personal data is collected, sold, or shared.**

## 5. Contact
- Author: **Shubham Bhamare** ([LinkedIn](https://www.linkedin.com/in/theshubh77))
- Source & Issue Tracker: [github.com/theshubh77/WhatsMySpeed](https://www.github.com/theshubh77/WhatsMySpeed)
