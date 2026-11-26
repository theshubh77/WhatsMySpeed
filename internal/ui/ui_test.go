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
		assets.IconArrowsDownUpBold,
		assets.IconArrowUp,
		assets.IconArrowDown,
		assets.IconGauge,
		assets.IconArrowsDownUp,
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
	app.rebuildDrawingResources(96, false, 48)
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
	app.rebuildDrawingResources(96, false, 48)
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
			app.rebuildDrawingResources(96, false, 48)

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
