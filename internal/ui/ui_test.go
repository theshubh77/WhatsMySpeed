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

	app := NewApp(config.Default(), "", network.NewMonitor())
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
	app := NewApp(cfg, "", network.NewMonitor())
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

			app := NewApp(cfg, "", network.NewMonitor())
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

	app := NewApp(config.Default(), "", network.NewMonitor())
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
		DownloadMbps: 30.4,
		UploadMbps:   12.1,
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
