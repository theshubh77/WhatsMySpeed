package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/network"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/speedtest"
	"golang.org/x/sys/windows"
)

func TestWin32StructSizes(t *testing.T) {
	if got := unsafe.Sizeof(notifyIconDataW{}); got != 976 {
		t.Fatalf("sizeof(notifyIconDataW) = %d, want 976", got)
	}
	if got := unsafe.Sizeof(logFontW{}); got != 92 {
		t.Fatalf("sizeof(logFontW) = %d, want 92", got)
	}
	if got := unsafe.Sizeof(menuItemInfoW{}); got != 80 {
		t.Fatalf("sizeof(menuItemInfoW) = %d, want 80", got)
	}
	if got := unsafe.Sizeof(trackMouseEventStruct{}); got != 24 {
		t.Fatalf("sizeof(trackMouseEventStruct) = %d, want 24", got)
	}
	if got := unsafe.Sizeof(appBarData{}); got != 48 {
		t.Fatalf("sizeof(appBarData) = %d, want 48", got)
	}
	if got := unsafe.Sizeof(paintStruct{}); got != 72 {
		t.Fatalf("sizeof(paintStruct) = %d, want 72", got)
	}
}

func TestEmbeddedPhosphorIconsParseAndRasterize(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	iconPaths := []string{
		assets.IconArrowUpBold,
		assets.IconArrowDownBold,
		assets.IconGaugeBold,
		assets.IconSunBold,
		assets.IconMoonBold,
		assets.IconMonitorBold,
		assets.IconGitHubLogoBold,
		assets.IconHeartBold,
		assets.IconHeartFill,
	}

	for _, p := range iconPaths {
		bmp, buf, err := RasterizeSVGToBitmap(p, 24, 24, 0xFFFFFFFF)
		if err != nil {
			t.Fatalf("RasterizeSVGToBitmap(%q) failed: %v", p, err)
		}
		nonZero := 0
		for i := 3; i < len(buf); i += 4 {
			if buf[i] > 0 {
				nonZero++
			}
		}
		_, _, _ = procGdipDisposeImage.Call(bmp)
		if nonZero == 0 {
			t.Fatalf("RasterizeSVGToBitmap(%q) produced an empty image", p)
		}
	}

	hIcon, err := CreateTrayHICON(assets.IconGaugeBold, 96, 0xFFFFFFFF)
	if err != nil {
		t.Fatalf("CreateTrayHICON failed: %v", err)
	}
	_, _, _ = procDestroyIcon.Call(hIcon)

	appIcon, err := CreatePNGHICON(assets.AppLogoPNG, 32, 32)
	if err != nil {
		t.Fatalf("CreatePNGHICON failed: %v", err)
	}
	_, _, _ = procDestroyIcon.Call(appIcon)

	gradBmp, gradBuf, err := RasterizeSVGToGradientBitmap(assets.IconHeartFill, 20, 20, 0xFF22D3EE, 0xFFA3E635)
	if err != nil {
		t.Fatalf("RasterizeSVGToGradientBitmap failed: %v", err)
	}
	nonZeroGrad := 0
	for i := 3; i < len(gradBuf); i += 4 {
		if gradBuf[i] > 0 {
			nonZeroGrad++
		}
	}
	_, _, _ = procGdipDisposeImage.Call(gradBmp)
	if nonZeroGrad == 0 {
		t.Fatal("RasterizeSVGToGradientBitmap produced empty image")
	}
}

func TestResolveTaskbarFontName(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	font := ResolveTaskbarFontName()
	if font == "" {
		t.Fatal("ResolveTaskbarFontName returned empty string")
	}
	t.Logf("Resolved taskbar font: %s", font)
}

func TestComputeOverlayBoundsHorizontalAndVertical(t *testing.T) {
	// 1920x1080 Windows 11 bottom taskbar at 96 DPI.
	info := TaskbarInfo{
		Valid:       true,
		TaskbarRect: rect{Left: 0, Top: 1032, Right: 1920, Bottom: 1080},
		TrayRect:    rect{Left: 1700, Top: 1032, Right: 1920, Bottom: 1080},
		MonitorRect: rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		DPI:         96,
	}
	x, y, vis := ComputeOverlayBounds(info, 100, 38)
	if !vis {
		t.Fatal("expected overlay to be visible")
	}
	if x != 1596 || y != 1037 {
		t.Fatalf("got (%d, %d), want (1596, 1037)", x, y)
	}

	// Hidden when fullscreen app blocks monitor.
	info.FullscreenBlocking = true
	if _, _, vis := ComputeOverlayBounds(info, 100, 38); vis {
		t.Fatal("expected overlay hidden during fullscreen")
	}

	// Vertical taskbar fallback.
	vert := TaskbarInfo{
		Valid:       true,
		TaskbarRect: rect{Left: 0, Top: 0, Right: 60, Bottom: 1080},
		TrayRect:    rect{Left: 0, Top: 900, Right: 60, Bottom: 1080},
		MonitorRect: rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		DPI:         96,
	}
	vx, vy, vvis := ComputeOverlayBounds(vert, 50, 36)
	if !vvis {
		t.Fatal("expected vertical taskbar overlay visible")
	}
	if vx != 5 || vy != 860 {
		t.Fatalf("vertical got (%d, %d), want (5, 860)", vx, vy)
	}
}

func TestThemeColorsContrast(t *testing.T) {
	dark := ColorsForTheme(false)
	if dark.IsLight || dark.TextARGB != 0xFFFFFFFF {
		t.Fatalf("unexpected dark theme colors: %+v", dark)
	}
	light := ColorsForTheme(true)
	if !light.IsLight || light.TextARGB == 0xFFFFFFFF {
		t.Fatalf("unexpected light theme colors: %+v", light)
	}
}

func TestOverlayRenderProducesPremultipliedPixels(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	app := NewApp(config.Default(), "", network.NewMonitor(), false)
	app.fontFamily = ResolveTaskbarFontName()
	app.currentSample = network.SpeedSample{
		DownloadBytesPerSec: 12.5 * 1024 * 1024,
		UploadBytesPerSec:   1.24 * 1024,
	}
	app.rebuildDrawingResources(96, false, 1920, 48)
	defer app.cleanupResources()

	if app.gpFont == 0 {
		t.Fatal("expected non-zero GDI+ font")
	}
	if app.upBitmap == 0 || app.downBitmap == 0 {
		t.Fatal("expected non-zero arrow icon bitmaps")
	}
	if app.pBits == nil || app.metrics.width <= 0 || app.metrics.height <= 0 {
		t.Fatalf("invalid DIB/metrics: pBits=%v w=%d h=%d", app.pBits, app.metrics.width, app.metrics.height)
	}

	var gfx uintptr
	st, _, _ := procGdipGetImageGraphicsContext.Call(app.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		t.Fatalf("GdipGetImageGraphicsContext failed: %d", st)
	}
	_, _, _ = procGdipSetSmoothingMode.Call(gfx, gpSmoothingModeHighQuality)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)
	_, _, _ = procGdipGraphicsClear.Call(gfx, 0x01000000)

	app.drawOverlayContent(
		gfx,
		app.formatRate(app.currentSample.UploadBytesPerSec),
		app.formatRate(app.currentSample.DownloadBytesPerSec),
	)
	_, _, _ = procGdipDeleteGraphics.Call(gfx)

	pixels := unsafe.Slice((*byte)(app.pBits), int(app.dibW)*int(app.dibH)*4)
	brightPixels := 0
	for i := 0; i < len(pixels); i += 4 {
		b, g, r, a := pixels[i], pixels[i+1], pixels[i+2], pixels[i+3]
		if r > a || g > a || b > a {
			t.Fatalf("pixel %d not premultiplied: BGRA=(%d,%d,%d,%d)", i/4, b, g, r, a)
		}
		if a > 30 && r > 20 && g > 20 && b > 20 {
			brightPixels++
		}
	}
	if brightPixels == 0 {
		t.Fatal("rendered overlay contained no visible foreground pixels")
	}
	t.Logf("Overlay %dx%d rendered %d bright foreground pixels", app.metrics.width, app.metrics.height, brightPixels)
}

func TestBitsUnitFormattingAndColumnSizing(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	cfg := config.Default()
	cfg.SpeedUnit = config.UnitBits
	app := NewApp(cfg, "", network.NewMonitor(), false)
	app.fontFamily = ResolveTaskbarFontName()
	app.rebuildDrawingResources(96, false, 1920, 48)
	defer app.cleanupResources()

	got := app.formatRate(3750000) // 30 Mbps
	if got.Value != "30.0" || got.Unit != "Mbps" {
		t.Fatalf("got %+v, want 30.0 Mbps", got)
	}
	if app.metrics.unitWidth <= 0 || app.metrics.width <= 0 {
		t.Fatalf("unexpected bits metrics: %+v", app.metrics)
	}
}

func TestLabelStylesAndLayoutsRenderCleanly(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	labels := []config.LabelStyle{config.LabelArrows, config.LabelUD, config.LabelWords}
	layouts := []config.OverlayLayout{config.LayoutStackedUp, config.LayoutStackedDown, config.LayoutInline, config.LayoutCompact}

	for _, lbl := range labels {
		for _, lay := range layouts {
			cfg := config.Default()
			cfg.LabelStyle = lbl
			cfg.Layout = lay

			app := NewApp(cfg, "", network.NewMonitor(), false)
			app.fontFamily = ResolveTaskbarFontName()
			app.currentSample = network.SpeedSample{
				DownloadBytesPerSec: 12.5 * 1024 * 1024,
				UploadBytesPerSec:   2.4 * 1024 * 1024,
			}
			app.rebuildDrawingResources(96, false, 1920, 48)

			var gfx uintptr
			st, _, _ := procGdipGetImageGraphicsContext.Call(app.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
			if st != 0 || gfx == 0 {
				app.cleanupResources()
				t.Fatalf("GdipGetImageGraphicsContext(%s, %s) failed: %d", lbl, lay, st)
			}
			_, _, _ = procGdipGraphicsClear.Call(gfx, 0x01000000)
			app.drawOverlayContent(
				gfx,
				app.formatRate(app.currentSample.UploadBytesPerSec),
				app.formatRate(app.currentSample.DownloadBytesPerSec),
			)
			_, _, _ = procGdipDeleteGraphics.Call(gfx)

			pixels := unsafe.Slice((*byte)(app.pBits), int(app.dibW)*int(app.dibH)*4)
			fg := 0
			for i := 0; i < len(pixels); i += 4 {
				if pixels[i+3] > 20 {
					fg++
				}
			}
			app.cleanupResources()
			if fg == 0 {
				t.Fatalf("label=%s layout=%s rendered 0 foreground pixels", lbl, lay)
			}
		}
	}

	stackedApp := NewApp(config.Default(), "", network.NewMonitor(), false)
	stackedApp.fontFamily = ResolveTaskbarFontName()
	stackedMetrics := stackedApp.computeMetrics(96, 1920, 48)

	for _, tc := range []struct {
		name string
		mode config.DisplayMode
		lay  config.OverlayLayout
	}{
		{"inline", config.ModeBoth, config.LayoutInline},
		{"download_only", config.ModeDownload, config.LayoutStackedUp},
		{"upload_only", config.ModeUpload, config.LayoutStackedUp},
	} {
		cfg := config.Default()
		cfg.DisplayMode = tc.mode
		cfg.Layout = tc.lay
		singleApp := NewApp(cfg, "", network.NewMonitor(), false)
		singleApp.fontFamily = stackedApp.fontFamily
		m := singleApp.computeMetrics(96, 1920, 48)
		if m.fontPx <= stackedMetrics.fontPx || m.iconSize <= stackedMetrics.iconSize {
			t.Fatalf("%s metrics (%d font, %d icon) should be larger than stacked (%d font, %d icon)",
				tc.name, m.fontPx, m.iconSize, stackedMetrics.fontPx, stackedMetrics.iconSize)
		}
	}

	// Verify narrow Left & Right vertical taskbars adapt cleanly without horizontal overflow.
	for _, tbW := range []int32{62, 76} {
		for _, mode := range []config.DisplayMode{config.ModeBoth, config.ModeDownload, config.ModeUpload} {
			for _, lay := range layouts {
				for _, lbl := range labels {
					cfg := config.Default()
					cfg.DisplayMode = mode
					cfg.Layout = lay
					cfg.LabelStyle = lbl
					vApp := NewApp(cfg, "", network.NewMonitor(), false)
					vApp.fontFamily = stackedApp.fontFamily
					vApp.currentSample = network.SpeedSample{
						DownloadBytesPerSec: 125.4 * 1024 * 1024,
						UploadBytesPerSec:   42.8 * 1024 * 1024,
					}
					vApp.rebuildDrawingResources(96, false, tbW, 1080)
					if vApp.metrics.width > tbW {
						vApp.cleanupResources()
						t.Fatalf("vertical tbW=%d mode=%s lay=%s lbl=%s overflowed width %d",
							tbW, mode, lay, lbl, vApp.metrics.width)
					}
					if vApp.metrics.effectiveInline {
						vApp.cleanupResources()
						t.Fatalf("vertical tbW=%d mode=%s lay=%s should not keep side-by-side inline",
							tbW, mode, lay)
					}

					var gfx uintptr
					st, _, _ := procGdipGetImageGraphicsContext.Call(vApp.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
					if st != 0 || gfx == 0 {
						vApp.cleanupResources()
						t.Fatalf("vertical GdipGetImageGraphicsContext failed: %d", st)
					}
					_, _, _ = procGdipGraphicsClear.Call(gfx, 0x01000000)
					vApp.drawOverlayContent(
						gfx,
						vApp.formatRate(vApp.currentSample.UploadBytesPerSec),
						vApp.formatRate(vApp.currentSample.DownloadBytesPerSec),
					)
					_, _, _ = procGdipDeleteGraphics.Call(gfx)

					pixels := unsafe.Slice((*byte)(vApp.pBits), int(vApp.dibW)*int(vApp.dibH)*4)
					fg := 0
					for i := 0; i < len(pixels); i += 4 {
						b, g, r, a := pixels[i], pixels[i+1], pixels[i+2], pixels[i+3]
						if r > a || g > a || b > a {
							vApp.cleanupResources()
							t.Fatalf("vertical pixel not premultiplied: BGRA=(%d,%d,%d,%d)", b, g, r, a)
						}
						if a > 20 {
							fg++
						}
					}
					vApp.cleanupResources()
					if fg == 0 {
						t.Fatalf("vertical tbW=%d mode=%s lay=%s lbl=%s rendered 0 pixels", tbW, mode, lay, lbl)
					}
				}
			}
		}
	}

	// Verify Left & Right ComputeOverlayBounds keep overlay inside the vertical taskbar rect.
	for _, side := range []struct {
		name string
		tb   rect
		tray rect
	}{
		{"left", rect{Left: 0, Top: 0, Right: 62, Bottom: 1080}, rect{Left: 0, Top: 900, Right: 62, Bottom: 1080}},
		{"right", rect{Left: 1858, Top: 0, Right: 1920, Bottom: 1080}, rect{Left: 1858, Top: 900, Right: 1920, Bottom: 1080}},
	} {
		vx, vy, vis := ComputeOverlayBounds(TaskbarInfo{
			Valid:       true,
			TaskbarRect: side.tb,
			TrayRect:    side.tray,
			MonitorRect: rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
			DPI:         96,
		}, 58, 76)
		if !vis || vx < side.tb.Left || vx+58 > side.tb.Right || vy+76 > side.tray.Top {
			t.Fatalf("%s vertical bounds invalid: (%d,%d) vis=%v tb=%+v tray=%+v",
				side.name, vx, vy, vis, side.tb, side.tray)
		}
	}
}

func TestSettingsWindowSceneAndHitTest(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	app := NewApp(config.Default(), "", network.NewMonitor(), false)
	app.fontFamily = ResolveTaskbarFontName()
	app.currentSample = network.SpeedSample{
		DownloadBytesPerSec: 3_750_000,
		UploadBytesPerSec:   1_500_000,
		ActiveInterfaces:    1,
	}

	sw := NewSettingsWindow(app)
	sw.dpi = 96
	sw.clientW = baseWindowW
	sw.clientH = baseWindowH
	sw.stProgress = speedtest.Progress{
		Phase:        speedtest.PhaseDone,
		Percent:      100,
		PingMs:       18,
		JitterMs:     2,
		DownloadMbps: 142.6,
		UploadMbps:   68.4,
		Samples:      []float64{18, 46, 92, 118, 134, 141, 138, 145, 142, 140, 144, 142.6},
	}
	defer sw.releaseDrawResources()

	for _, isLight := range []bool{false, true} {
		sw.ensureDrawResources(96, isLight)
		if sw.gpBitmap == 0 {
			t.Fatalf("ensureDrawResources(light=%v) failed", isLight)
		}
		var gfx uintptr
		st, _, _ := procGdipGetImageGraphicsContext.Call(sw.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
		if st != 0 || gfx == 0 {
			t.Fatalf("GdipGetImageGraphicsContext failed: %d", st)
		}
		sw.renderScene(gfx, sw.clientW, sw.clientH, 96, isLight)
		_, _, _ = procGdipDeleteGraphics.Call(gfx)

		if outDir := os.Getenv("WHATSMYSPEED_PREVIEW_DIR"); outDir != "" {
			name := "dark.png"
			if isLight {
				name = "light.png"
			}
			pix := unsafe.Slice((*byte)(sw.pBits), int(sw.dibW)*int(sw.dibH)*4)
			img := image.NewNRGBA(image.Rect(0, 0, int(sw.dibW), int(sw.dibH)))
			for i := 0; i < len(pix); i += 4 {
				img.Pix[i+0] = pix[i+2]
				img.Pix[i+1] = pix[i+1]
				img.Pix[i+2] = pix[i+0]
				img.Pix[i+3] = 255
			}
			if f, err := os.Create(filepath.Join(outDir, name)); err == nil {
				_ = png.Encode(f, img)
				_ = f.Close()
			}
		}

		if len(sw.controls) < 19 {
			t.Fatalf("expected >=19 interactive controls, got %d", len(sw.controls))
		}
		// Verify every control hit-tests cleanly at its center.
		for _, c := range sw.controls {
			cx := (c.bounds.Left + c.bounds.Right) / 2
			cy := (c.bounds.Top + c.bounds.Bottom) / 2
			if got := sw.hitTest(cx, cy); got != c.action {
				t.Fatalf("hitTest(%d,%d) = %v, want %v", cx, cy, got, c.action)
			}
		}
	}
}

func TestGaugeCurveAndArcPath(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	prev := -1.0
	for _, mbps := range []float64{0, 5, 12, 30, 100, 500, 1000, 2000} {
		f := speedToGaugeFraction(mbps)
		if f < 0 || f > 1 || f < prev {
			t.Fatalf("speedToGaugeFraction(%v) = %v (prev=%v)", mbps, f, prev)
		}
		prev = f
	}

	for _, sweep := range []float64{10, 135, 270} {
		p, err := buildGaugeArcPath(250, 170, 94, 80, 135, sweep)
		if err != nil || p == 0 {
			t.Fatalf("buildGaugeArcPath(sweep=%v) failed: %v", sweep, err)
		}
		_, _, _ = procGdipDeletePath.Call(p)
	}
}

func TestIsShellOrFlyoutWindow(t *testing.T) {
	shellCases := []struct {
		className string
		exeName   string
	}{
		{"Progman", "explorer.exe"},
		{"WorkerW", "explorer.exe"},
		{"Shell_TrayWnd", ""},
		{"Shell_SecondaryTrayWnd", ""},
		{"Windows.UI.Core.CoreWindow", "SearchHost.exe"},
		{"XamlExplorerHostIslandWindow", ""},
		{"TopLevelWindowForOverflowXamlIsland", ""},
		{"MultitaskingViewFrame", ""},
		{"TaskSwitcherWnd", ""},
		{"ForegroundStaging", ""},
		{"ControlCenterWindow", ""},
		{"NotificationCenterWindow", ""},
		{"Shell_InputSwitchTopLevelWindow", ""},
		{"NotifyIconOverflowWindow", ""},
		{"TaskListThumbnailWnd", ""},
		{"WinUIDesktopWin32WindowClass", ""},
		{"Windows.UI.Composition.DesktopWindowContentBridge", ""},
		{"Windows.UI.Input.InputSite.WindowClass", ""},
		{"UnknownClass", "StartMenuExperienceHost.exe"},
		{"UnknownClass", "ShellExperienceHost.exe"},
		{"UnknownClass", "ShellHost.exe"},
		{"UnknownClass", "Explorer.EXE"},
		{"UnknownClass", "Widgets.exe"},
		{"UnknownClass", "WidgetBoard.exe"},
	}
	for _, tc := range shellCases {
		if !isShellOrFlyoutWindow(tc.className, tc.exeName) {
			t.Fatalf("isShellOrFlyoutWindow(%q, %q) = false, want true", tc.className, tc.exeName)
		}
	}

	appCases := []struct {
		className string
		exeName   string
	}{
		{"Chrome_WidgetWin_1", "chrome.exe"},
		{"MozillaWindowClass", "firefox.exe"},
		{"Qt5QWindowIcon", "vlc.exe"},
		{"ApplicationFrameWindow", "applicationframehost.exe"},
		{"UnrealWindow", "game.exe"},
	}
	for _, tc := range appCases {
		if isShellOrFlyoutWindow(tc.className, tc.exeName) {
			t.Fatalf("isShellOrFlyoutWindow(%q, %q) = true, want false", tc.className, tc.exeName)
		}
	}
}

func TestComputeOverlayBoundsVisibility(t *testing.T) {
	mon := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	tb := rect{Left: 0, Top: 1032, Right: 1920, Bottom: 1080}
	tray := rect{Left: 1700, Top: 1032, Right: 1912, Bottom: 1080}

	base := TaskbarInfo{
		Valid:       true,
		TaskbarHWND: 0x1234,
		MonitorRect: mon,
		TaskbarRect: tb,
		TrayRect:    tray,
		DPI:         96,
	}

	x, y, ok := ComputeOverlayBounds(base, 140, 36)
	if !ok {
		t.Fatal("ComputeOverlayBounds should show overlay on normal desktop taskbar")
	}
	if x+140 > tray.Left {
		t.Fatalf("overlay right %d overlaps tray left %d", x+140, tray.Left)
	}
	if y < tb.Top || y+36 > tb.Bottom {
		t.Fatalf("overlay Y [%d,%d] outside taskbar [%d,%d]", y, y+36, tb.Top, tb.Bottom)
	}

	fullscreen := base
	fullscreen.FullscreenBlocking = true
	if _, _, ok := ComputeOverlayBounds(fullscreen, 140, 36); ok {
		t.Fatal("ComputeOverlayBounds should hide overlay when FullscreenBlocking is true")
	}

	hiddenTaskbar := base
	hiddenTaskbar.AutoHide = true
	hiddenTaskbar.TaskbarHidden = true
	if _, _, ok := ComputeOverlayBounds(hiddenTaskbar, 140, 36); ok {
		t.Fatal("ComputeOverlayBounds should hide overlay when TaskbarHidden is true")
	}

	invalid := base
	invalid.Valid = false
	if _, _, ok := ComputeOverlayBounds(invalid, 140, 36); ok {
		t.Fatal("ComputeOverlayBounds should hide overlay when Valid is false")
	}
}

func TestChildLayeredWindowClientCoordinates(t *testing.T) {
	hInst := getModuleHandle()
	staticCls, _ := windows.UTF16PtrFromString("STATIC")
	title, _ := windows.UTF16PtrFromString("Test")

	parent, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticCls)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup),
		120, 140, 400, 48,
		0, 0, hInst, 0,
	)
	if parent == 0 {
		t.Skip("CreateWindowExW parent unavailable in this test station")
	}
	defer procDestroyWindow.Call(parent)

	child, _, _ := procCreateWindowExW.Call(
		uintptr(wsExToolWindow|wsExTopmost|wsExLayered|wsExNoActivate),
		uintptr(unsafe.Pointer(staticCls)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup),
		0, 0, 120, 40,
		0, 0, hInst, 0,
	)
	if child == 0 {
		t.Fatalf("CreateWindowExW child failed")
	}
	defer procDestroyWindow.Call(child)

	setWindowStyle(child, (getWindowStyle(child)&^wsPopup)|wsChild|wsClipSiblings)
	setWindowExStyle(child, getWindowExStyle(child)&^wsExTopmost)
	_, _, _ = procSetParent.Call(child, parent)
	if gotParent := getParentWindow(child); gotParent != parent {
		t.Fatalf("GetParent = 0x%x, want 0x%x", gotParent, parent)
	}

	cx, cy, ok := screenToClientPoint(parent, 170, 144)
	if !ok || cx != 50 || cy != 4 {
		t.Fatalf("screenToClientPoint(170,144) = (%d,%d,ok=%v), want (50,4,true)", cx, cy, ok)
	}

	_, _, _ = procSetWindowPos.Call(child, hwndTop, uintptr(cx), uintptr(cy), 120, 40, swpNoActivate|swpFrameChanged)
	r, rOK := getWindowRect(child)
	if !rOK || r.Left != 170 || r.Top != 144 || r.Right != 290 || r.Bottom != 184 {
		t.Fatalf("child rect = %+v (ok=%v), want {170 144 290 184}", r, rOK)
	}
}

func TestDashboardControlsAndConfigPersistence(t *testing.T) {
	if linkedinURL != "https://www.linkedin.com/in/theshubh77" {
		t.Fatalf("linkedinURL = %q, want https://www.linkedin.com/in/theshubh77", linkedinURL)
	}

	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	cfgPath := filepath.Join(t.TempDir(), "WhatsMySpeed", "config.json")
	app := NewApp(config.Default(), cfgPath, network.NewMonitor(), false)
	app.fontFamily = ResolveTaskbarFontName()
	win := NewSettingsWindow(app)
	app.settings = win
	defer win.releaseDrawResources()

	win.clientW = scaleDPIInt(baseWindowW, 96)
	win.clientH = scaleDPIInt(baseWindowH, 96)

	for _, isLight := range []bool{false, true} {
		win.ensureDrawResources(96, isLight)
		if win.gpBitmap == 0 {
			t.Fatalf("ensureDrawResources(isLight=%v) did not allocate DIB", isLight)
		}
		var gfx uintptr
		st, _, _ := procGdipGetImageGraphicsContext.Call(win.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
		if st != 0 || gfx == 0 {
			t.Fatalf("GdipGetImageGraphicsContext failed: status %d", st)
		}
		win.renderScene(gfx, win.clientW, win.clientH, 96, isLight)
		_, _, _ = procGdipDeleteGraphics.Call(gfx)
	}

	// Verify every interactive control exists in win.controls and hit-testing finds it.
	expectedActions := []controlAction{
		actThemeLight, actThemeDark, actThemeSystem,
		actSpeedTestToggle,
		actModeBoth, actModeDownload, actModeUpload, actModeHide,
		actLabelArrows, actLabelUD, actLabelWords,
		actLayoutStackU, actLayoutStackD, actLayoutInline, actLayoutCompact,
		actUnitBytes, actUnitBits,
		actAutoStart, actGitHub, actSponsor, actLinkedIn,
	}
	seen := make(map[controlAction]bool, len(win.controls))
	for _, c := range win.controls {
		if c.bounds.Width() <= 0 || c.bounds.Height() <= 0 {
			t.Fatalf("control action %d has empty bounds %+v", c.action, c.bounds)
		}
		cx := c.bounds.Left + c.bounds.Width()/2
		cy := c.bounds.Top + c.bounds.Height()/2
		if got := win.hitTest(cx, cy); got != c.action {
			t.Fatalf("hitTest(%d,%d) = %d, want %d", cx, cy, got, c.action)
		}
		seen[c.action] = true
	}
	for _, act := range expectedActions {
		if !seen[act] {
			t.Fatalf("expected interactive control action %d missing from dashboard", act)
		}
	}

	// Verify hover tooltips render cleanly for Light, Dark, and System theme buttons.
	expectedTips := map[controlAction]string{
		actThemeLight:  "Light Theme",
		actThemeDark:   "Dark Theme",
		actThemeSystem: "System Theme",
	}
	for act, wantTip := range expectedTips {
		if got := themeTooltipText(act); got != wantTip {
			t.Fatalf("themeTooltipText(%d) = %q, want %q", act, got, wantTip)
		}
		win.hoveredAct = act
		var gfx uintptr
		st, _, _ := procGdipGetImageGraphicsContext.Call(win.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
		if st != 0 || gfx == 0 {
			t.Fatalf("GdipGetImageGraphicsContext(tooltip %d) failed: %d", act, st)
		}
		win.renderScene(gfx, win.clientW, win.clientH, 96, false)
		_, _, _ = procGdipDeleteGraphics.Call(gfx)
	}
	win.hoveredAct = actNone

	// Exercise setting actions through handleAction and verify persistence in config.json.
	settingActions := []controlAction{
		actThemeLight,
		actThemeDark,
		actThemeSystem,
		actModeDownload,
		actModeUpload,
		actModeHide,
		actModeBoth,
		actLabelUD,
		actLabelWords,
		actLabelArrows,
		actLayoutStackD,
		actLayoutInline,
		actLayoutCompact,
		actLayoutStackU,
		actUnitBits,
		actUnitBytes,
	}
	for _, act := range settingActions {
		win.handleAction(act)
	}
	loaded, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatalf("LoadFrom(%q) failed: %v", cfgPath, err)
	}
	if loaded != app.cfg {
		t.Fatalf("persisted config %+v != app.cfg %+v", loaded, app.cfg)
	}
}

func TestGaugeStateAcrossPhases(t *testing.T) {
	if frac, val := targetGaugeState(speedtest.Progress{Phase: speedtest.PhaseDone, DownloadMbps: 250, UploadMbps: 120}); frac != 0 || val != 0 {
		t.Fatalf("PhaseDone targetGaugeState = (%v, %v), want (0, 0)", frac, val)
	}
	if frac, val := targetGaugeState(speedtest.Progress{Phase: speedtest.PhasePause, CompletedPhase: speedtest.PhaseDownload}); frac != 0 || val != 0 {
		t.Fatalf("PhasePause(after download) targetGaugeState = (%v, %v), want (0, 0)", frac, val)
	}
	if frac, val := targetGaugeState(speedtest.Progress{Phase: speedtest.PhaseDownload, DownloadMbps: 100}); frac <= 0 || val != 100 {
		t.Fatalf("PhaseDownload targetGaugeState = (%v, %v), want (>0, 100)", frac, val)
	}
}

func TestTrayBoundsFilteringAndUnion(t *testing.T) {
	tb := rect{Left: 0, Top: 1020, Right: 1920, Bottom: 1080} // 125% DPI (60px taskbar)
	dpi := uint32(120)

	// Simulate the exact Windows 11 reboot scenario:
	// Legacy TrayNotifyWnd only spans Clock+Battery (1745..1920), while XAML system tray
	// icons (Chevron ^, OneDrive, ENG/IN, ControlCenterButton) start at x=1490.
	win32NarrowTray := rect{Left: 1745, Top: 1020, Right: 1920, Bottom: 1080}

	candidates := []trayCandidate{
		// Left-side pinned app / full-width bridge must be ignored.
		{className: "Windows.UI.Composition.DesktopWindowContentBridge", automationID: "", bounds: tb},
		{className: "Taskbar.TaskListButtonAutomationPeer", automationID: "Appid: Chrome", bounds: rect{Left: 800, Top: 1020, Right: 850, Bottom: 1080}},
		{className: "SystemTray.ChevronIconView", automationID: "ShowHiddenIcons", bounds: rect{Left: 1490, Top: 1025, Right: 1522, Bottom: 1075}},
		{className: "SystemTray.IconView", automationID: "SystemTrayIcon", bounds: rect{Left: 1526, Top: 1025, Right: 1560, Bottom: 1075}},
		{className: "SystemTray.LanguageInnerButton", automationID: "InputSwitch", bounds: rect{Left: 1564, Top: 1025, Right: 1612, Bottom: 1075}},
		{className: "SystemTray.OmniButton", automationID: "ControlCenterButton", bounds: rect{Left: 1616, Top: 1025, Right: 1740, Bottom: 1075}},
		// Offscreen stale element must be ignored.
		{className: "SystemTray.NormalButton", automationID: "SystemTrayIcon", bounds: rect{Left: 1200, Top: 1025, Right: 1240, Bottom: 1075}, offscreen: true},
	}

	uxTray, uOK := computeTrayUnionFromCandidates(candidates, tb, dpi, false)
	if !uOK {
		t.Fatal("expected valid UIA tray union")
	}
	merged, mOK := unionRect(win32NarrowTray, true, uxTray, uOK)
	if !mOK || merged.Left != 1490 || merged.Right != 1920 {
		t.Fatalf("merged tray rect = %+v (ok=%v), want Left=1490 Right=1920", merged, mOK)
	}

	info := TaskbarInfo{
		Valid:       true,
		TaskbarRect: tb,
		TrayRect:    merged,
		MonitorRect: rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		DPI:         dpi,
	}
	x, y, vis := ComputeOverlayBounds(info, 110, 44)
	if !vis || x+110 > 1490 || y < tb.Top || y+44 > tb.Bottom {
		t.Fatalf("ComputeOverlayBounds overlapped tray or clipped: (%d,%d) vis=%v", x, y, vis)
	}

	// Simulate adding a new corner utility button (e.g., Touch Keyboard / Pen Menu / Copilot) to the tray.
	withExtraCornerIcon := append([]trayCandidate(nil), candidates...)
	withExtraCornerIcon = append(withExtraCornerIcon, trayCandidate{
		className:    "Windows.UI.Xaml.Controls.ContentPresenter",
		automationID: "TouchKeyboard",
		bounds:       rect{Left: 1448, Top: 1025, Right: 1486, Bottom: 1075},
	})
	expandedTray, exOK := computeTrayUnionFromCandidates(withExtraCornerIcon, tb, dpi, false)
	if !exOK || expandedTray.Left != 1448 {
		t.Fatalf("expanded tray union = %+v (ok=%v), want Left=1448", expandedTray, exOK)
	}
	exInfo := info
	exInfo.TrayRect = expandedTray
	x2, _, vis2 := ComputeOverlayBounds(exInfo, 110, 44)
	if !vis2 || x2+110 > 1448 {
		t.Fatalf("expanded ComputeOverlayBounds overlapped new corner icon: x2=%d", x2)
	}

	// Verify full-width DesktopWindowContentBridge is rejected by Win32 classifier too.
	if isWin32TrayDescendant("Windows.UI.Composition.DesktopWindowContentBridge", tb, tb, dpi, false) {
		t.Fatal("full-width DesktopWindowContentBridge must not be classified as tray child")
	}
	if !isWin32TrayDescendant("TrayInputIndicatorWClass", rect{Left: 1564, Top: 1025, Right: 1612, Bottom: 1075}, tb, dpi, false) {
		t.Fatal("TrayInputIndicatorWClass should be classified as tray child")
	}

	// Verify Right-to-Left (RTL) taskbar docks cleanly to the RIGHT of a left-side tray.
	rtlTray := []trayCandidate{
		{className: "SystemTray.ChevronIconView", automationID: "ShowHiddenIcons", bounds: rect{Left: 210, Top: 1025, Right: 244, Bottom: 1075}},
		{className: "SystemTray.OmniButton", automationID: "ControlCenterButton", bounds: rect{Left: 4, Top: 1025, Right: 206, Bottom: 1075}},
	}
	rtlRect, rtlOK := computeTrayUnionFromCandidates(rtlTray, tb, dpi, true)
	if !rtlOK || rtlRect.Right != 244 {
		t.Fatalf("RTL tray union = %+v (ok=%v), want Right=244", rtlRect, rtlOK)
	}
	rtlInfo := TaskbarInfo{
		Valid:       true,
		TaskbarRect: tb,
		TrayRect:    rtlRect,
		MonitorRect: rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
		DPI:         dpi,
		RTL:         true,
	}
	rx, _, rvis := ComputeOverlayBounds(rtlInfo, 110, 44)
	if !rvis || rx < rtlRect.Right {
		t.Fatalf("RTL ComputeOverlayBounds (%d) overlapped RTL tray Right (%d)", rx, rtlRect.Right)
	}
}

func TestThemeResolutionAndAccentLuminance(t *testing.T) {
	u64 := func(v uint64) *uint64 { return &v }

	// Standard Windows 11 Light/Dark/Custom taskbar rules.
	if got := resolveTaskbarIsLightFromSettings(u64(0), u64(1), nil, nil); got {
		t.Fatal("Windows Dark Taskbar + Light Apps should keep dark taskbar overlay (false)")
	}
	if got := resolveTaskbarIsLightFromSettings(u64(1), u64(1), nil, nil); !got {
		t.Fatal("Windows Light Taskbar should use light taskbar overlay (true)")
	}

	// "Show accent color on Start and taskbar" (ColorPrevalence=1) overrides SystemUsesLightTheme=1!
	darkBlueABGR := uint64(0xFFD77800) // Windows Blue #0078D7 in ABGR
	if got := resolveTaskbarIsLightFromSettings(u64(1), u64(1), u64(1), &darkBlueABGR); got {
		t.Fatal("Dark blue accent taskbar must switch overlay to white text (isLight=false)")
	}
	brightMintABGR := uint64(0xFF99F0A0) // Bright pastel light green
	if got := resolveTaskbarIsLightFromSettings(u64(0), u64(0), u64(1), &brightMintABGR); !got {
		t.Fatal("Bright pastel accent taskbar must switch overlay to dark text (isLight=true)")
	}

	if got := colorrefToARGB(0x00112233); got != 0xFF332211 {
		t.Fatalf("colorrefToARGB(0x00112233) = 0x%08x, want 0xFF332211", got)
	}
}

func TestIsShellOrFlyoutWindowThirdPartyWidgets(t *testing.T) {
	for _, exe := range []string{"wallpaper32.exe", "Lively.exe", "Rainmeter.exe", "TranslucentTB.exe", "RoundedTB.exe"} {
		if !isShellOrFlyoutWindow("GenericFullscreenWidget", exe) {
			t.Fatalf("expected third-party desktop/taskbar utility %q to be excluded from fullscreen blocking", exe)
		}
	}
}

func TestUIATrayCOMProbeDoesNotPanicOrFailInit(t *testing.T) {
	if procCoInitializeEx.Find() != nil || procCoCreateInstance.Find() != nil {
		t.Skip("OLE32 COM APIs unavailable")
	}
	hInst := getModuleHandle()
	cls, _ := windows.UTF16PtrFromString("STATIC")
	title, _ := windows.UTF16PtrFromString("WMS_UIA_Test")
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup),
		10, 10, 800, 48,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		t.Skip("headless window creation unavailable")
	}
	defer procDestroyWindow.Call(hwnd)

	hr, _, _ := procCoInitializeEx.Call(0, uintptr(coinitMultithreaded))
	if hr == 0 || hr == 1 {
		defer procCoUninitialize.Call()
	}
	var uia unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)),
		0,
		uintptr(clsctxInprocServer),
		uintptr(unsafe.Pointer(&iidIUIAutomation)),
		uintptr(unsafe.Pointer(&uia)),
	)
	if int32(hr) < 0 || uia == nil {
		t.Fatalf("CoCreateInstance(CUIAutomation) failed: 0x%08x", uint32(hr))
	}
	defer uiaRelease(uia)

	tb := rect{Left: 10, Top: 10, Right: 810, Bottom: 58}
	if _, ok := probeSystemTrayViaUIA(uia, hwnd, tb, 96); ok {
		t.Fatal("blank STATIC window should not report system tray elements")
	}
}

func TestSettingsWindowAdaptiveWorkAreaFit(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	// Laptop 1920x1080 at 150% DPI (144 DPI): unscaled window would exceed 1008px work area.
	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)
	sw := NewSettingsWindow(NewApp(config.Default(), "", network.NewMonitor(), false))
	work150 := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1008}
	winW, winH, posX, posY := sw.fitAndCenterInWorkArea(work150, 144, style)
	if winW > work150.Width() || winH > work150.Height() || posX < 0 || posY < 0 {
		t.Fatalf("150%% DPI window exceeded work area: size=(%d,%d) pos=(%d,%d)", winW, winH, posX, posY)
	}

	// Laptop 1366x768 at 100% DPI (96 DPI): must shrink proportionally inside 1366x720 work area.
	work768 := rect{Left: 0, Top: 0, Right: 1366, Bottom: 720}
	w768, h768, x768, y768 := sw.fitAndCenterInWorkArea(work768, 96, style)
	if w768 > work768.Width() || h768 > work768.Height() || x768 < 0 || y768 < 0 {
		t.Fatalf("1366x768 window exceeded work area: size=(%d,%d) pos=(%d,%d)", w768, h768, x768, y768)
	}

	// Verify scaled scene still keeps every interactive control inside bounds and hit-testable.
	sw.app.fontFamily = ResolveTaskbarFontName()
	sw.clientW = w768 - 16
	sw.clientH = h768 - 39
	effDPI := layoutDPIForClient(sw.clientW, sw.clientH, 96)
	sw.ensureDrawResources(effDPI, false)
	defer sw.releaseDrawResources()

	var gfx uintptr
	st, _, _ := procGdipGetImageGraphicsContext.Call(sw.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		t.Fatalf("GdipGetImageGraphicsContext failed: %d", st)
	}
	sw.renderScene(gfx, sw.clientW, sw.clientH, effDPI, false)
	_, _, _ = procGdipDeleteGraphics.Call(gfx)

	for _, c := range sw.controls {
		if c.bounds.Left < 0 || c.bounds.Top < 0 || c.bounds.Right > sw.clientW || c.bounds.Bottom > sw.clientH {
			t.Fatalf("control %d clipped outside shrunk client (%dx%d): %+v", c.action, sw.clientW, sw.clientH, c.bounds)
		}
		cx := (c.bounds.Left + c.bounds.Right) / 2
		cy := (c.bounds.Top + c.bounds.Bottom) / 2
		if got := sw.hitTest(cx, cy); got != c.action {
			t.Fatalf("shrunk hitTest(%d,%d) = %d, want %d", cx, cy, got, c.action)
		}
	}

	if got := friendlySpeedtestError("Get https://speed.cloudflare.com: dial tcp: lookup: no such host"); got != "Check internet" {
		t.Fatalf("friendlySpeedtestError(offline) = %q, want Check internet", got)
	}
	if got := friendlySpeedtestError("context deadline exceeded"); got != "Timed out" {
		t.Fatalf("friendlySpeedtestError(timeout) = %q, want Timed out", got)
	}
}

func TestTaskbarRegionClippingGuard(t *testing.T) {
	hInst := getModuleHandle()
	staticCls, _ := windows.UTF16PtrFromString("STATIC")
	title, _ := windows.UTF16PtrFromString("WMS_Rgn_Test")
	tb, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticCls)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup),
		0, 0, 1000, 48,
		0, 0, hInst, 0,
	)
	if tb == 0 {
		t.Skip("CreateWindowExW unavailable in this test station")
	}
	defer procDestroyWindow.Call(tb)

	childRect := rect{Left: 650, Top: 4, Right: 750, Bottom: 44}
	// Normal taskbar without SetWindowRgn must not be flagged as clipped.
	if isTaskbarRegionClippingChild(tb, childRect) {
		t.Fatal("unclipped taskbar reported child as clipped")
	}

	// Simulate RoundedTB-style SetWindowRgn keeping only the rightmost tray segment [800..1000].
	hRgn, _, _ := procCreateRectRgn.Call(800, 0, 1000, 48)
	if hRgn == 0 {
		t.Fatal("CreateRectRgn failed")
	}
	setWindowRgn := modUser32.NewProc("SetWindowRgn")
	if ok, _, _ := setWindowRgn.Call(tb, hRgn, 0); ok == 0 {
		_, _, _ = procDeleteObject.Call(hRgn)
		t.Fatal("SetWindowRgn failed")
	}

	if !isTaskbarRegionClippingChild(tb, childRect) {
		t.Fatal("expected child outside SetWindowRgn [800..1000] to be detected as clipped")
	}
	if isTaskbarRegionClippingChild(tb, rect{Left: 820, Top: 4, Right: 920, Bottom: 44}) {
		t.Fatal("expected child inside SetWindowRgn [800..1000] not to be detected as clipped")
	}
}

func TestCustomPopupMenuLayoutAndRendering(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	fontName := ResolveTaskbarFontName()
	items := buildContextMenuItems(config.Default(), true)
	if len(items) < 10 {
		t.Fatalf("expected full context menu items, got %d", len(items))
	}

	ml := measurePopupMenu(items, 120, fontName)
	if ml.width < scaleDPIInt(188, 120) || ml.height <= ml.padY*2 {
		t.Fatalf("unexpected measured menu size: %dx%d", ml.width, ml.height)
	}

	mon := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	mx, my := computeMainMenuPos(point{X: 1850, Y: 1040}, ml.width, ml.height, mon, 120)
	if mx < mon.Left || mx+ml.width > mon.Right || my < mon.Top || my+ml.height > mon.Bottom {
		t.Fatalf("main menu pos (%d,%d) size %dx%d escaped monitor %+v", mx, my, ml.width, ml.height, mon)
	}

	// Verify submenu flips left cleanly near the right edge of the monitor.
	parentRect := rect{Left: mx, Top: my, Right: mx + ml.width, Bottom: my + ml.height}
	itemLocal := rect{Left: 0, Top: ml.itemTops[8], Right: ml.width, Bottom: ml.itemBottoms[8]}
	subLayout := measurePopupMenu(items[8].children, 120, fontName)
	sx, sy := computeSubmenuPos(parentRect, itemLocal, subLayout.width, subLayout.height, mon, ml.padY, 120)
	if sx >= parentRect.Left || sx < mon.Left || sy < mon.Top || sy+subLayout.height > mon.Bottom {
		t.Fatalf("expected submenu to flip left inside monitor, got (%d,%d) parent=%+v mon=%+v", sx, sy, parentRect, mon)
	}

	// Render Dark & Light palettes including a submenu item with chevron + selected pill.
	for _, mode := range []config.ThemeMode{config.ThemeDark, config.ThemeLight} {
		cfg := config.Default()
		cfg.Theme = mode
		pal := resolveMenuPalette(cfg)

		lvl := &popupMenuLevel{
			items:       items,
			layout:      ml,
			hoveredIdx:  8,
			hasDWMRound: true,
		}
		buf := make([]byte, int(ml.width)*int(ml.height)*4)
		var bmp uintptr
		st, _, _ := procGdipCreateBitmapFromScan0.Call(
			uintptr(ml.width), uintptr(ml.height), uintptr(ml.width*4),
			gpPixelFormat32bppPARGB, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmp)),
		)
		if st != 0 || bmp == 0 {
			t.Fatalf("GdipCreateBitmapFromScan0 failed: %d", st)
		}
		var gfx uintptr
		st, _, _ = procGdipGetImageGraphicsContext.Call(bmp, uintptr(unsafe.Pointer(&gfx)))
		if st != 0 || gfx == 0 {
			_, _, _ = procGdipDisposeImage.Call(bmp)
			t.Fatalf("GdipGetImageGraphicsContext failed: %d", st)
		}
		drawPopupMenuLevel(gfx, lvl, pal, 120, fontName, -1)
		_, _, _ = procGdipDeleteGraphics.Call(gfx)
		_, _, _ = procGdipDisposeImage.Call(bmp)

		// Check that only one crisp chevron sits in the right arrow zone on hovered submenu row 8.
		rowTop := ml.itemTops[8] + ml.rowH/3
		rowBot := ml.itemBottoms[8] - ml.rowH/3
		hoverR := byte((pal.hoverBG >> 16) & 0xFF)
		hoverG := byte((pal.hoverBG >> 8) & 0xFF)
		hoverB := byte(pal.hoverBG & 0xFF)
		// Rightmost interior strip of the hover pill must remain pure hover color (zero second OS arrow!).
		rightEdgeStart := ml.width - scaleDPIInt(9, 120)
		rightEdgeEnd := ml.width - scaleDPIInt(6, 120)
		for y := rowTop; y < rowBot; y++ {
			for x := rightEdgeStart; x < rightEdgeEnd; x++ {
				off := (int(y)*int(ml.width) + int(x)) * 4
				if buf[off] != hoverB || buf[off+1] != hoverG || buf[off+2] != hoverR {
					t.Fatalf("theme=%s unexpected pixel at right edge (%d,%d): got BGR(%d,%d,%d) want hover(%d,%d,%d)",
						mode, x, y, buf[off], buf[off+1], buf[off+2], hoverB, hoverG, hoverR)
				}
			}
		}
	}
}

func TestOverlayTooltipBoundsAndRendering(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	mon := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	cases := []struct {
		name    string
		overlay rect
		taskbar rect
		check   func(x, y, w, h int32) bool
	}{
		{
			name:    "bottom_taskbar",
			overlay: rect{Left: 1620, Top: 1036, Right: 1730, Bottom: 1076},
			taskbar: rect{Left: 0, Top: 1032, Right: 1920, Bottom: 1080},
			check:   func(x, y, w, h int32) bool { return y+h <= 1032 && x >= 0 && x+w <= 1920 },
		},
		{
			name:    "top_taskbar",
			overlay: rect{Left: 1620, Top: 4, Right: 1730, Bottom: 44},
			taskbar: rect{Left: 0, Top: 0, Right: 1920, Bottom: 48},
			check:   func(x, y, w, h int32) bool { return y >= 48 && x >= 0 && x+w <= 1920 },
		},
		{
			name:    "left_taskbar",
			overlay: rect{Left: 4, Top: 860, Right: 68, Bottom: 900},
			taskbar: rect{Left: 0, Top: 0, Right: 72, Bottom: 1080},
			check:   func(x, y, w, h int32) bool { return x >= 72 && y >= 0 && y+h <= 1080 },
		},
		{
			name:    "right_taskbar",
			overlay: rect{Left: 1852, Top: 860, Right: 1916, Bottom: 900},
			taskbar: rect{Left: 1848, Top: 0, Right: 1920, Bottom: 1080},
			check:   func(x, y, w, h int32) bool { return x+w <= 1848 && y >= 0 && y+h <= 1080 },
		},
	}

	fontName := ResolveTaskbarFontName()
	for _, isLight := range []bool{false, true} {
		pal := resolveTooltipPalette(isLight)
		tipW, tipH, titleH, speedH, dArrowW, dTxtW, uArrowW, uTxtW := measureOverlayTooltip(
			config.AppName, "24.8 MB/s", "6.2 MB/s", 120, fontName,
		)
		if tipW < 100 || tipH < 30 {
			t.Fatalf("unexpected tooltip size %dx%d", tipW, tipH)
		}

		for _, tc := range cases {
			x, y := computeTooltipBounds(tc.overlay, tc.taskbar, mon, uint32(tipW), uint32(tipH), 120)
			if !tc.check(x, y, tipW, tipH) {
				t.Fatalf("case=%s light=%v placed out of bounds: (%d,%d) %dx%d", tc.name, isLight, x, y, tipW, tipH)
			}
		}

		buf := make([]byte, int(tipW)*int(tipH)*4)
		var bmp uintptr
		st, _, _ := procGdipCreateBitmapFromScan0.Call(
			uintptr(tipW), uintptr(tipH), uintptr(tipW*4),
			gpPixelFormat32bppPARGB, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmp)),
		)
		if st != 0 || bmp == 0 {
			t.Fatalf("GdipCreateBitmapFromScan0(tooltip) failed: %d", st)
		}
		var gfx uintptr
		st, _, _ = procGdipGetImageGraphicsContext.Call(bmp, uintptr(unsafe.Pointer(&gfx)))
		if st != 0 || gfx == 0 {
			_, _, _ = procGdipDisposeImage.Call(bmp)
			t.Fatalf("GdipGetImageGraphicsContext(tooltip) failed: %d", st)
		}
		renderOverlayTooltip(gfx, tipW, tipH, titleH, speedH, dArrowW, dTxtW, uArrowW, uTxtW, config.AppName, "24.8 MB/s", "6.2 MB/s", pal, 120, fontName)
		_, _, _ = procGdipDeleteGraphics.Call(gfx)
		_, _, _ = procGdipDisposeImage.Call(bmp)

		opaqueCount := 0
		for i := 3; i < len(buf); i += 4 {
			if buf[i] > 180 {
				opaqueCount++
			}
		}
		if opaqueCount == 0 {
			t.Fatalf("light=%v tooltip rendered empty pixels", isLight)
		}
	}
}

func TestToggleSwitchConcentricityAndSymmetry(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	dpis := []uint32{96, 120, 144, 168, 192}
	for _, dpi := range dpis {
		swW, swH, pad := toggleSwitchGeometry(dpi)
		if swH%2 != 0 {
			t.Fatalf("dpi=%d: toggle height %d must be even", dpi, swH)
		}
		if swH-2*pad <= 0 {
			t.Fatalf("dpi=%d: invalid knob diameter for h=%d pad=%d", dpi, swH, pad)
		}

		for _, isLight := range []bool{false, true} {
			pal := getDashboardPalette(isLight)
			for _, on := range []bool{false, true} {
				track := pal.switchOffBG
				if on {
					track = pal.brandCyan
				}
				bmp, buf, err := renderToggleSwitchBitmap(swW, swH, pad, track, pal.knobARGB, on)
				if err != nil || bmp == 0 {
					t.Fatalf("dpi=%d light=%v on=%v render failed: %v", dpi, isLight, on, err)
				}

				// 1. Vertical symmetry across all rows and columns of the toggle sprite.
				for y := int32(0); y < swH/2; y++ {
					yMirror := swH - 1 - y
					for x := int32(0); x < swW; x++ {
						i1 := (int(y)*int(swW) + int(x)) * 4
						i2 := (int(yMirror)*int(swW) + int(x)) * 4
						if buf[i1] != buf[i2] || buf[i1+1] != buf[i2+1] || buf[i1+2] != buf[i2+2] || buf[i1+3] != buf[i2+3] {
							_, _, _ = procGdipDisposeImage.Call(bmp)
							t.Fatalf("dpi=%d light=%v on=%v vertical asymmetry at (%d,%d) vs (%d,%d): %v != %v",
								dpi, isLight, on, x, y, x, yMirror, buf[i1:i1+4], buf[i2:i2+4])
						}
					}
				}

				// 2. Knob interior & AA edge horizontal symmetry within the solid cap interior disk.
				capStart := int32(0)
				if on {
					capStart = swW - swH
				}
				rSolidSq := float64(swH/2-1) * float64(swH/2-1)
				for y := int32(0); y < swH; y++ {
					dy := float64(y) + 0.5 - float64(swH)/2.0
					for dx := int32(0); dx < swH/2; dx++ {
						rx := float64(dx) + 0.5 - float64(swH)/2.0
						if rx*rx+dy*dy > rSolidSq {
							continue
						}
						x1 := capStart + dx
						x2 := capStart + swH - 1 - dx
						i1 := (int(y)*int(swW) + int(x1)) * 4
						i2 := (int(y)*int(swW) + int(x2)) * 4
						if buf[i1] != buf[i2] || buf[i1+1] != buf[i2+1] || buf[i1+2] != buf[i2+2] || buf[i1+3] != buf[i2+3] {
							_, _, _ = procGdipDisposeImage.Call(bmp)
							t.Fatalf("dpi=%d light=%v on=%v knob horizontal asymmetry at (%d,%d) vs (%d,%d): %v != %v",
								dpi, isLight, on, x1, y, x2, y, buf[i1:i1+4], buf[i2:i2+4])
						}
					}
				}

				// 3. Cardinal padding check: top, bottom, and cap-side borders must all equal pad.
				cx := capStart + swH/2
				cy := swH / 2
				isKnobSolid := func(x, y int32) bool {
					idx := (int(y)*int(swW) + int(x)) * 4
					return buf[idx+2] == buf[idx+1] && buf[idx+1] == buf[idx] && buf[idx+3] == 255
				}
				topBorderPx := int32(0)
				for y := int32(0); y < cy && !isKnobSolid(cx, y); y++ {
					topBorderPx++
				}
				botBorderPx := int32(0)
				for y := swH - 1; y >= cy && !isKnobSolid(cx, y); y-- {
					botBorderPx++
				}
				sideBorderPx := int32(0)
				if on {
					for x := swW - 1; x >= cx && !isKnobSolid(x, cy); x-- {
						sideBorderPx++
					}
				} else {
					for x := int32(0); x < cx && !isKnobSolid(x, cy); x++ {
						sideBorderPx++
					}
				}
				if topBorderPx != pad || botBorderPx != pad || sideBorderPx != pad {
					_, _, _ = procGdipDisposeImage.Call(bmp)
					t.Fatalf("dpi=%d light=%v on=%v uneven borders top=%d bot=%d side=%d, want %d",
						dpi, isLight, on, topBorderPx, botBorderPx, sideBorderPx, pad)
				}

				// 4. Opposite state reflection symmetry: ON mirrored horizontally matches OFF bit-for-bit.
				if on {
					offBmp, offBuf, offErr := renderToggleSwitchBitmap(swW, swH, pad, track, pal.knobARGB, false)
					if offErr != nil || offBmp == 0 {
						_, _, _ = procGdipDisposeImage.Call(bmp)
						t.Fatalf("dpi=%d light=%v off reflection render failed: %v", dpi, isLight, offErr)
					}
					for y := int32(0); y < swH; y++ {
						for x := int32(0); x < swW; x++ {
							i1 := (int(y)*int(swW) + int(x)) * 4
							i2 := (int(y)*int(swW) + int(swW-1-x)) * 4
							if buf[i1] != offBuf[i2] || buf[i1+1] != offBuf[i2+1] || buf[i1+2] != offBuf[i2+2] || buf[i1+3] != offBuf[i2+3] {
								_, _, _ = procGdipDisposeImage.Call(offBmp)
								_, _, _ = procGdipDisposeImage.Call(bmp)
								t.Fatalf("dpi=%d light=%v ON/OFF reflection mismatch at (%d,%d)", dpi, isLight, x, y)
							}
						}
					}
					_, _, _ = procGdipDisposeImage.Call(offBmp)
				}

				_, _, _ = procGdipDisposeImage.Call(bmp)
			}
		}
	}

	// 4. Composite ON toggle through SettingsWindow.renderScene at 125% DPI (120) and verify canvas symmetry.
	app := NewApp(config.Default(), "", network.NewMonitor(), false)
	app.cfg.StartWithWindows = true
	app.fontFamily = ResolveTaskbarFontName()
	sw := NewSettingsWindow(app)
	sw.dpi = 120
	sw.clientW = scaleDPIInt(baseWindowW, 120)
	sw.clientH = scaleDPIInt(baseWindowH, 120)
	defer sw.releaseDrawResources()

	sw.ensureDrawResources(120, false)
	var gfx uintptr
	st, _, _ := procGdipGetImageGraphicsContext.Call(sw.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		t.Fatalf("GdipGetImageGraphicsContext failed: %d", st)
	}
	sw.renderScene(gfx, sw.clientW, sw.clientH, 120, false)
	_, _, _ = procGdipDeleteGraphics.Call(gfx)

	var swBounds rect
	found := false
	for _, c := range sw.controls {
		if c.action == actAutoStart {
			swBounds = c.bounds
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("actAutoStart control missing after renderScene")
	}
	swW, swH, _ := toggleSwitchGeometry(120)
	pix := unsafe.Slice((*byte)(sw.pBits), int(sw.dibW)*int(sw.dibH)*4)
	for y := int32(0); y < swH/2; y++ {
		yTop := swBounds.Top + y
		yBot := swBounds.Top + swH - 1 - y
		for x := int32(0); x < swW; x++ {
			px := swBounds.Left + x
			iTop := (int(yTop)*int(sw.dibW) + int(px)) * 4
			iBot := (int(yBot)*int(sw.dibW) + int(px)) * 4
			if pix[iTop] != pix[iBot] || pix[iTop+1] != pix[iBot+1] || pix[iTop+2] != pix[iBot+2] {
				t.Fatalf("composited canvas vertical asymmetry at row offset %d, col %d: top=%v bot=%v",
					y, x, pix[iTop:iTop+3], pix[iBot:iBot+3])
			}
		}
	}
}

func TestToggleSwitchAnimationCurvesAndFrames(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	// 1. Easing curve boundaries, strict monotonicity, and smooth deceleration (fast start, soft landing).
	if got := cubicBezierEase(0, 0.2, 0.0, 0.0, 1.0); got != 0 {
		t.Fatalf("cubicBezierEase(0) = %v, want 0", got)
	}
	if got := cubicBezierEase(1, 0.2, 0.0, 0.0, 1.0); got != 1 {
		t.Fatalf("cubicBezierEase(1) = %v, want 1", got)
	}
	prev := 0.0
	for i := 1; i <= 20; i++ {
		u := float64(i) / 20.0
		v := cubicBezierEase(u, 0.2, 0.0, 0.0, 1.0)
		if v < prev || v > 1.0 {
			t.Fatalf("cubicBezierEase(%v) = %v out of order (prev=%v)", u, v, prev)
		}
		prev = v
	}
	if mid := cubicBezierEase(0.5, 0.2, 0.0, 0.0, 1.0); mid <= 0.7 {
		t.Fatalf("cubicBezierEase(0.5) = %v, expected smooth ease-out > 0.7", mid)
	}

	// 2. Color crossfade boundaries.
	pal := getDashboardPalette(false)
	if got := lerpARGB(pal.switchOffBG, pal.brandCyan, 0); got != pal.switchOffBG {
		t.Fatalf("lerpARGB(t=0) = 0x%08X, want 0x%08X", got, pal.switchOffBG)
	}
	if got := lerpARGB(pal.switchOffBG, pal.brandCyan, 1); got != pal.brandCyan {
		t.Fatalf("lerpARGB(t=1) = 0x%08X, want 0x%08X", got, pal.brandCyan)
	}
	if midCol := lerpARGB(pal.switchOffBG, pal.brandCyan, 0.5); midCol == pal.switchOffBG || midCol == pal.brandCyan {
		t.Fatalf("lerpARGB(t=0.5) = 0x%08X did not blend", midCol)
	}

	// 3. Intermediate animation frames remain vertically symmetric and advance knob centroid monotonically.
	swW, swH, pad := toggleSwitchGeometry(120)
	buf := make([]byte, int(swW)*int(swH)*4)
	prevKnobX := -1.0
	for _, pos := range []float64{0.0, 0.2, 0.5, 0.85, 1.0} {
		col := lerpARGB(pal.switchOffBG, pal.brandCyan, pos)
		renderToggleSwitchPixels(buf, swW, swH, pad, col, pal.knobARGB, pos)

		for y := int32(0); y < swH/2; y++ {
			yBot := swH - 1 - y
			for x := int32(0); x < swW; x++ {
				i1 := (int(y)*int(swW) + int(x)) * 4
				i2 := (int(yBot)*int(swW) + int(x)) * 4
				if buf[i1] != buf[i2] || buf[i1+1] != buf[i2+1] || buf[i1+2] != buf[i2+2] || buf[i1+3] != buf[i2+3] {
					t.Fatalf("pos=%.2f vertical asymmetry at (%d,%d)", pos, x, y)
				}
			}
		}

		// Compute solid white knob centroid X.
		sumX, count := 0.0, 0.0
		for y := pad; y < swH-pad; y++ {
			for x := int32(0); x < swW; x++ {
				idx := (int(y)*int(swW) + int(x)) * 4
				if buf[idx] == 255 && buf[idx+1] == 255 && buf[idx+2] == 255 && buf[idx+3] == 255 {
					sumX += float64(x)
					count++
				}
			}
		}
		if count == 0 {
			t.Fatalf("pos=%.2f rendered no solid knob pixels", pos)
		}
		cx := sumX / count
		if cx <= prevKnobX {
			t.Fatalf("pos=%.2f knob centroid X=%.2f did not advance past %.2f", pos, cx, prevKnobX)
		}
		prevKnobX = cx
	}

	// 4. Mid-flight renderScene compositing via SettingsWindow.
	app := NewApp(config.Default(), "", network.NewMonitor(), false)
	app.fontFamily = ResolveTaskbarFontName()
	sw := NewSettingsWindow(app)
	sw.dpi = 120
	sw.clientW = scaleDPIInt(baseWindowW, 120)
	sw.clientH = scaleDPIInt(baseWindowH, 120)
	defer sw.releaseDrawResources()

	sw.ensureDrawResources(120, false)
	sw.switchFrom = 0.0
	sw.switchTarget = 1.0
	sw.switchPos = 0.5
	sw.switchAnimating = true

	var gfx uintptr
	st, _, _ := procGdipGetImageGraphicsContext.Call(sw.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		t.Fatalf("GdipGetImageGraphicsContext failed: %d", st)
	}
	sw.renderScene(gfx, sw.clientW, sw.clientH, 120, false)
	_, _, _ = procGdipDeleteGraphics.Call(gfx)

	var swBounds rect
	for _, c := range sw.controls {
		if c.action == actAutoStart {
			swBounds = c.bounds
			break
		}
	}
	pix := unsafe.Slice((*byte)(sw.pBits), int(sw.dibW)*int(sw.dibH)*4)
	for y := int32(0); y < swH/2; y++ {
		yTop := swBounds.Top + y
		yBot := swBounds.Top + swH - 1 - y
		for x := int32(0); x < swW; x++ {
			px := swBounds.Left + x
			iTop := (int(yTop)*int(sw.dibW) + int(px)) * 4
			iBot := (int(yBot)*int(sw.dibW) + int(px)) * 4
			if pix[iTop] != pix[iBot] || pix[iTop+1] != pix[iBot+1] || pix[iTop+2] != pix[iBot+2] {
				t.Fatalf("animated canvas vertical asymmetry at (%d,%d)", x, y)
			}
		}
	}
}

func TestThemeSwitcherAnimationCurvesAndFrames(t *testing.T) {
	token, err := StartupGDIPlus()
	if err != nil {
		t.Fatalf("StartupGDIPlus failed: %v", err)
	}
	defer ShutdownGDIPlus(token)

	if themeIdxFromMode(config.ThemeLight) != 0 || themeIdxFromMode(config.ThemeDark) != 1 || themeIdxFromMode(config.ThemeSystem) != 2 {
		t.Fatalf("unexpected themeIdxFromMode mapping")
	}

	// Verify sliding capsule advances monotonically across 0 -> 1 -> 2 and maintains vertical symmetry.
	pal := getDashboardPalette(false)
	segW := scaleDPIInt(34, 120)
	h := (scaleDPIInt(38, 120) - scaleDPIInt(6, 120)) &^ 1
	areaW := segW * 3
	buf := make([]byte, int(areaW)*int(h)*4)

	prevCentroidX := -1.0
	for _, pos := range []float64{0.0, 0.5, 1.0, 1.5, 2.0} {
		renderThemeSlidePixels(buf, areaW, h, segW, -1, pal.pillHoverBG, pal.brandCyan, pos, 0.5)
		for y := int32(0); y < h/2; y++ {
			yBot := h - 1 - y
			for x := int32(0); x < areaW; x++ {
				i1 := (int(y)*int(areaW) + int(x)) * 4
				i2 := (int(yBot)*int(areaW) + int(x)) * 4
				if buf[i1] != buf[i2] || buf[i1+1] != buf[i2+1] || buf[i1+2] != buf[i2+2] || buf[i1+3] != buf[i2+3] {
					t.Fatalf("theme slide pos=%.2f vertical asymmetry at (%d,%d)", pos, x, y)
				}
			}
		}
		sumX, count := 0.0, 0.0
		for y := int32(0); y < h; y++ {
			for x := int32(0); x < areaW; x++ {
				idx := (int(y)*int(areaW) + int(x)) * 4
				if buf[idx+3] == 255 {
					sumX += float64(x)
					count++
				}
			}
		}
		if count == 0 {
			t.Fatalf("theme slide pos=%.2f rendered no solid pixels", pos)
		}
		cx := sumX / count
		if cx <= prevCentroidX {
			t.Fatalf("theme slide pos=%.2f centroid X=%.2f did not advance past %.2f", pos, cx, prevCentroidX)
		}
		prevCentroidX = cx
	}

	// Render SettingsWindow with an active mid-flight theme animation (Dark -> Light).
	app := NewApp(config.Default(), "", network.NewMonitor(), false)
	app.cfg.Theme = config.ThemeLight
	app.fontFamily = ResolveTaskbarFontName()
	sw := NewSettingsWindow(app)
	sw.dpi = 120
	sw.clientW = scaleDPIInt(baseWindowW, 120)
	sw.clientH = scaleDPIInt(baseWindowH, 120)
	defer sw.releaseDrawResources()

	sw.ensureDrawResources(120, true)
	sw.themeFrom = 1.0
	sw.themeTarget = 0.0
	sw.themePos = 0.5
	sw.themeAnimating = true

	var gfx uintptr
	st, _, _ := procGdipGetImageGraphicsContext.Call(sw.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		t.Fatalf("GdipGetImageGraphicsContext failed: %d", st)
	}
	sw.renderScene(gfx, sw.clientW, sw.clientH, 120, true)
	_, _, _ = procGdipDeleteGraphics.Call(gfx)

	var lightCtrl, darkCtrl uiControl
	for _, c := range sw.controls {
		if c.action == actThemeLight {
			lightCtrl = c
		} else if c.action == actThemeDark {
			darkCtrl = c
		}
	}
	if lightCtrl.bounds.Empty() || darkCtrl.bounds.Empty() {
		t.Fatalf("expected theme controls in rendered scene")
	}
	// At themePos=0.5, midpoint X between Light and Dark segments must contain cyan pixels.
	midX := (lightCtrl.bounds.Right + darkCtrl.bounds.Left) / 2
	midY := (lightCtrl.bounds.Top + lightCtrl.bounds.Bottom) / 2
	pix := unsafe.Slice((*byte)(sw.pBits), int(sw.dibW)*int(sw.dibH)*4)
	off := (int(midY)*int(sw.dibW) + int(midX)) * 4
	gotCyan := uint32(pix[off+2])<<16 | uint32(pix[off+1])<<8 | uint32(pix[off]) | 0xFF000000
	wantCyan := getDashboardPalette(true).brandCyan
	if gotCyan != wantCyan {
		t.Fatalf("mid-flight theme capsule pixel at (%d,%d) = 0x%08X, want 0x%08X", midX, midY, gotCyan, wantCyan)
	}
}
