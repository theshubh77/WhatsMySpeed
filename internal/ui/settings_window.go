package ui

import (
	"context"
	"fmt"
	"math"
	"sync"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/speedtest"
	"golang.org/x/sys/windows"
)

const (
	settingsClassName = "WhatsMySpeedDashboardWindow"
	sponsorURL        = "https://github.com/sponsors/theshubh77"

	baseWindowW = 500
	baseWindowH = 648
)

type controlAction int

const (
	actNone controlAction = iota
	actSpeedTestToggle
	actModeBoth
	actModeDownload
	actModeUpload
	actModeHide
	actLabelArrows
	actLabelUD
	actLabelWords
	actLayoutStackU
	actLayoutStackD
	actLayoutInline
	actLayoutCompact
	actUnitBytes
	actUnitBits
	actAutoStart
	actSponsor
)

type uiControl struct {
	action   controlAction
	bounds   rect
	label    string
	selected bool
	isSwitch bool
	isButton bool
}

type dashboardPalette struct {
	isLight     bool
	windowBG    uint32
	cardBG      uint32
	cardBorder  uint32
	pillBG      uint32
	pillHoverBG uint32
	brandCyan   uint32
	brandLime   uint32
	brandMint   uint32
	accentText  uint32
	textPrimary uint32
	textMuted   uint32
	trackBG     uint32
	tickColor   uint32
	switchOffBG uint32
	knobARGB    uint32
	badgeBG     uint32
}

func getDashboardPalette(isLight bool) dashboardPalette {
	if isLight {
		return dashboardPalette{
			isLight:     true,
			windowBG:    0xFFF4F7FA,
			cardBG:      0xFFFFFFFF,
			cardBorder:  0xFFE2E8F0,
			pillBG:      0xFFEDF2F7,
			pillHoverBG: 0xFFE2E8F0,
			brandCyan:   0xFF0891B2,
			brandLime:   0xFF65A30D,
			brandMint:   0xFF0D9488,
			accentText:  0xFFFFFFFF,
			textPrimary: 0xFF0F172A,
			textMuted:   0xFF64748B,
			trackBG:     0xFFE2E8F0,
			tickColor:   0xFFCBD5E1,
			switchOffBG: 0xFFCBD5E1,
			knobARGB:    0xFFFFFFFF,
			badgeBG:     0xFFEDF2F7,
		}
	}
	// Brand palette derived directly from WhatsMySpeed logo (#0B0F19, #22D3EE, #A3E635).
	return dashboardPalette{
		isLight:     false,
		windowBG:    0xFF0B0F19,
		cardBG:      0xFF121826,
		cardBorder:  0xFF1E293B,
		pillBG:      0xFF1A2233,
		pillHoverBG: 0xFF232E45,
		brandCyan:   0xFF22D3EE,
		brandLime:   0xFFA3E635,
		brandMint:   0xFF5EEAD4,
		accentText:  0xFF07131A,
		textPrimary: 0xFFF8FAFC,
		textMuted:   0xFF8E9BB0,
		trackBG:     0xFF1D273B,
		tickColor:   0xFF2E3D59,
		switchOffBG: 0xFF28354D,
		knobARGB:    0xFFFFFFFF,
		badgeBG:     0xFF131A29,
	}
}

// SettingsWindow implements the minimal, brand-styled WhatsMySpeed window with
// a speedometer gauge and pill-shaped taskbar configuration controls.
type SettingsWindow struct {
	app        *App
	hwnd       uintptr
	classReg   bool
	hIconSmall uintptr
	hIconBig   uintptr
	dpi        uint32
	clientW    int32
	clientH    int32
	hoveredAct controlAction
	tracking   bool
	controls   []uiControl

	// Offscreen DIB for flicker-free WM_PAINT.
	memDC     uintptr
	hBitmap   uintptr
	oldBitmap uintptr
	pBits     unsafe.Pointer
	gpBitmap  uintptr
	dibW      int32
	dibH      int32

	// Cached fonts & header icon.
	fontTitle      uintptr
	fontBody       uintptr
	fontSmall      uintptr
	fontCaps       uintptr
	fontGaugeHero  uintptr
	fontGaugeUnit  uintptr
	fontPillMetric uintptr
	logoBmp        uintptr
	logoBuf        []byte
	resDPI         uint32
	resLight       bool

	// Speed test runtime state.
	stMu       sync.Mutex
	stRunning  bool
	stCancel   context.CancelFunc
	stProgress speedtest.Progress
}

// NewSettingsWindow creates a SettingsWindow bound to app.
func NewSettingsWindow(app *App) *SettingsWindow {
	return &SettingsWindow{
		app: app,
		stProgress: speedtest.Progress{
			Phase: speedtest.PhaseIdle,
		},
	}
}

// Show displays and focuses the WhatsMySpeed window.
func (w *SettingsWindow) Show(startSpeedTest bool) {
	if w.hwnd == 0 {
		if err := w.create(); err != nil {
			return
		}
	}

	w.applyTitleBarTheme()
	_, _, _ = procShowWindow.Call(w.hwnd, swRestore)
	_, _, _ = procSetForegroundWindow.Call(w.hwnd)
	w.invalidate()

	if startSpeedTest {
		w.startSpeedTest()
	}
}

// RefreshAppearance forces a repaint and syncs title bar theme.
func (w *SettingsWindow) RefreshAppearance() {
	if w.hwnd == 0 {
		return
	}
	w.applyTitleBarTheme()
	if vis, _, _ := procIsWindowVisible.Call(w.hwnd); vis != 0 {
		w.invalidate()
	}
}

// NotifySampleUpdated refreshes the live header readout when the window is visible.
func (w *SettingsWindow) NotifySampleUpdated() {
	if w.hwnd == 0 {
		return
	}
	if vis, _, _ := procIsWindowVisible.Call(w.hwnd); vis != 0 {
		w.invalidate()
	}
}

// CancelSpeedTest stops any active background speed test.
func (w *SettingsWindow) CancelSpeedTest() {
	w.stMu.Lock()
	cancel := w.stCancel
	w.stMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Destroy releases window handles and GDI/GDI+ resources.
func (w *SettingsWindow) Destroy() {
	w.CancelSpeedTest()
	w.releaseDrawResources()
	if w.hwnd != 0 {
		_, _, _ = procDestroyWindow.Call(w.hwnd)
		w.hwnd = 0
	}
	if w.hIconSmall != 0 {
		_, _, _ = procDestroyIcon.Call(w.hIconSmall)
		w.hIconSmall = 0
	}
	if w.hIconBig != 0 {
		_, _, _ = procDestroyIcon.Call(w.hIconBig)
		w.hIconBig = 0
	}
}

func (w *SettingsWindow) create() error {
	hInst := getModuleHandle()
	clsPtr, err := windows.UTF16PtrFromString(settingsClassName)
	if err != nil {
		return err
	}
	titlePtr, err := windows.UTF16PtrFromString(config.AppName)
	if err != nil {
		return err
	}

	if !w.classReg {
		hCursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		wcx := wndClassExW{
			CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			Style:         csHRedraw | csVRedraw,
			LpfnWndProc:   windows.NewCallback(settingsWndProc),
			HInstance:     hInst,
			HCursor:       hCursor,
			LpszClassName: clsPtr,
		}
		_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))
		w.classReg = true
	}

	dpi := w.app.currentDPI
	if dpi == 0 {
		dpi = getSystemDPI()
	}
	w.dpi = dpi

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)
	winW, winH := w.calcWindowSizeForClient(baseWindowW, baseWindowH, dpi, style)

	screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)
	posX := (int32(screenW) - winW) / 2
	posY := (int32(screenH) - winH) / 2
	if posX < 20 {
		posX = 20
	}
	if posY < 20 {
		posY = 20
	}

	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(clsPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		style,
		uintptr(posX),
		uintptr(posY),
		uintptr(winW),
		uintptr(winH),
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW(settings) failed: %w", callErr)
	}
	w.hwnd = hwnd

	w.hIconSmall, _ = CreatePNGHICON(assets.AppLogoPNG, scaleDPIInt(16, dpi), scaleDPIInt(16, dpi))
	w.hIconBig, _ = CreatePNGHICON(assets.AppLogoPNG, scaleDPIInt(32, dpi), scaleDPIInt(32, dpi))
	if w.hIconSmall == 0 {
		w.hIconSmall, _ = CreateSizedHICON(assets.IconGaugeBold, scaleDPIInt(16, dpi), scaleDPIInt(16, dpi), 0xFF22D3EE)
	}
	if w.hIconBig == 0 {
		w.hIconBig, _ = CreateSizedHICON(assets.IconGaugeBold, scaleDPIInt(32, dpi), scaleDPIInt(32, dpi), 0xFF22D3EE)
	}
	if w.hIconSmall != 0 {
		_, _, _ = procSendMessageW.Call(w.hwnd, wmSetIcon, iconSmall, w.hIconSmall)
	}
	if w.hIconBig != 0 {
		_, _, _ = procSendMessageW.Call(w.hwnd, wmSetIcon, iconBig, w.hIconBig)
	}

	var cr rect
	_, _, _ = procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&cr)))
	w.clientW = cr.Width()
	w.clientH = cr.Height()
	w.applyTitleBarTheme()
	return nil
}

func (w *SettingsWindow) calcWindowSizeForClient(baseClientW, baseClientH int32, dpi uint32, style uintptr) (int32, int32) {
	cw := scaleDPIInt(baseClientW, dpi)
	ch := scaleDPIInt(baseClientH, dpi)
	rc := rect{Left: 0, Top: 0, Right: cw, Bottom: ch}
	if procAdjustWindowRectExForDpi.Find() == nil {
		_, _, _ = procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0, uintptr(dpi))
	} else {
		_, _, _ = procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	}
	return rc.Width(), rc.Height()
}

func (w *SettingsWindow) applyTitleBarTheme() {
	if w.hwnd == 0 || procDwmSetWindowAttribute.Find() != nil {
		return
	}
	var useDark uint32
	if !DetectTaskbarLightTheme() {
		useDark = 1
	}
	_, _, _ = procDwmSetWindowAttribute.Call(
		w.hwnd,
		dwmwaUseImmersiveDarkMode,
		uintptr(unsafe.Pointer(&useDark)),
		4,
	)
}

func (w *SettingsWindow) invalidate() {
	if w.hwnd != 0 {
		_, _, _ = procInvalidateRect.Call(w.hwnd, 0, 0)
	}
}

func settingsWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	activeAppMu.Lock()
	app := activeApp
	activeAppMu.Unlock()

	if app == nil || app.settings == nil || app.settings.hwnd != hwnd {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}
	return app.settings.handleMessage(message, wParam, lParam)
}

func (w *SettingsWindow) handleMessage(message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmEraseBkgnd:
		return 1

	case wmSize:
		w.clientW = int32(loWord(lParam))
		w.clientH = int32(int16((lParam >> 16) & 0xFFFF))
		w.invalidate()
		return 0

	case wmDpiChanged:
		newDPI := uint32(loWord(wParam))
		if newDPI > 0 {
			w.dpi = newDPI
			style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)
			winW, winH := w.calcWindowSizeForClient(baseWindowW, baseWindowH, newDPI, style)
			_, _, _ = procSetWindowPos.Call(
				w.hwnd, 0, 0, 0,
				uintptr(winW), uintptr(winH),
				swpNoMove|swpNoZOrder|swpNoActivate,
			)
		}
		w.invalidate()
		return 0

	case wmMouseMove:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		act := w.hitTest(x, y)
		if act != w.hoveredAct {
			w.hoveredAct = act
			w.invalidate()
		}
		if !w.tracking {
			tme := trackMouseEventStruct{
				CbSize:    uint32(unsafe.Sizeof(trackMouseEventStruct{})),
				DwFlags:   tmeLeave,
				HwndTrack: w.hwnd,
			}
			ok, _, _ := procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			w.tracking = ok != 0
		}
		return 0

	case wmMouseLeave:
		w.tracking = false
		if w.hoveredAct != actNone {
			w.hoveredAct = actNone
			w.invalidate()
		}
		return 0

	case wmSetCursor:
		if loWord(lParam) == 1 && w.hoveredAct != actNone {
			hHand, _, _ := procLoadCursorW.Call(0, idcHand)
			if hHand != 0 {
				_, _, _ = procSetCursor.Call(hHand)
				return 1
			}
		}

	case wmLButtonUp:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		act := w.hitTest(x, y)
		if act != actNone {
			w.handleAction(act)
		}
		return 0

	case wmSpeedTestUpdate:
		w.invalidate()
		return 0

	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			w.paintClient(hdc)
			_, _, _ = procEndPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		return 0

	case wmClose:
		_, _, _ = procShowWindow.Call(w.hwnd, swHide)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(w.hwnd, uintptr(message), wParam, lParam)
	return ret
}

func (w *SettingsWindow) hitTest(x, y int32) controlAction {
	for _, c := range w.controls {
		if c.bounds.Contains(x, y) {
			return c.action
		}
	}
	return actNone
}

func (w *SettingsWindow) handleAction(act controlAction) {
	cfg := w.app.cfg
	switch act {
	case actSpeedTestToggle:
		w.stMu.Lock()
		running := w.stRunning
		w.stMu.Unlock()
		if running {
			w.CancelSpeedTest()
		} else {
			w.startSpeedTest()
		}
		return
	case actModeBoth:
		cfg.DisplayMode = config.ModeBoth
	case actModeDownload:
		cfg.DisplayMode = config.ModeDownload
	case actModeUpload:
		cfg.DisplayMode = config.ModeUpload
	case actModeHide:
		cfg.DisplayMode = config.ModeHide
	case actLabelArrows:
		cfg.LabelStyle = config.LabelArrows
	case actLabelUD:
		cfg.LabelStyle = config.LabelUD
	case actLabelWords:
		cfg.LabelStyle = config.LabelWords
	case actLayoutStackU:
		cfg.Layout = config.LayoutStackedUp
	case actLayoutStackD:
		cfg.Layout = config.LayoutStackedDown
	case actLayoutInline:
		cfg.Layout = config.LayoutInline
	case actLayoutCompact:
		cfg.Layout = config.LayoutCompact
	case actUnitBytes:
		cfg.SpeedUnit = config.UnitBytes
	case actUnitBits:
		cfg.SpeedUnit = config.UnitBits
	case actAutoStart:
		enabled, _ := config.IsAutoStartEnabled()
		_ = config.SetAutoStart(!enabled)
		w.invalidate()
		return
	case actSponsor:
		openExternalURL(w.hwnd, sponsorURL)
		return
	default:
		return
	}
	w.app.ApplyConfig(cfg)
}

func openExternalURL(hwnd uintptr, rawURL string) {
	verbPtr, _ := windows.UTF16PtrFromString("open")
	urlPtr, _ := windows.UTF16PtrFromString(rawURL)
	_, _, _ = procShellExecuteW.Call(
		hwnd,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(urlPtr)),
		0, 0, swShowNormal,
	)
}

func (w *SettingsWindow) startSpeedTest() {
	w.stMu.Lock()
	if w.stRunning {
		w.stMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.stRunning = true
	w.stCancel = cancel
	w.stProgress = speedtest.Progress{
		Phase:   speedtest.PhasePing,
		Percent: 2,
	}
	w.stMu.Unlock()
	w.invalidate()

	go func() {
		res := speedtest.Run(ctx, speedtest.DefaultOptions(), func(p speedtest.Progress) {
			w.stMu.Lock()
			w.stProgress = p
			w.stMu.Unlock()
			if w.hwnd != 0 {
				_, _, _ = procPostMessageW.Call(w.hwnd, wmSpeedTestUpdate, 0, 0)
			}
		})

		w.stMu.Lock()
		w.stRunning = false
		w.stCancel = nil
		w.stProgress = res
		w.stMu.Unlock()
		if w.hwnd != 0 {
			_, _, _ = procPostMessageW.Call(w.hwnd, wmSpeedTestUpdate, 0, 0)
		}
	}()
}

func (w *SettingsWindow) ensureDrawResources(dpi uint32, isLight bool) {
	if dpi == 0 {
		dpi = 96
	}
	if w.fontTitle == 0 || w.resDPI != dpi || w.resLight != isLight {
		w.releaseFontAndIconResources()
		screenDC, _, _ := procGetDC.Call(0)
		if screenDC != 0 {
			defer procReleaseDC.Call(0, screenDC)
		}

		fam := w.app.fontFamily
		if fam == "" {
			fam = ResolveTaskbarFontName()
		}
		w.fontTitle, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(18, dpi), 700)
		w.fontBody, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(12, dpi), 600)
		w.fontSmall, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(11, dpi), 500)
		w.fontCaps, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(10, dpi), 700)
		w.fontGaugeHero, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(40, dpi), 700)
		w.fontGaugeUnit, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(13, dpi), 600)
		w.fontPillMetric, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(13, dpi), 700)

		iconPx := scaleDPIInt(28, dpi)
		w.logoBmp, w.logoBuf, _ = DecodePNGToBitmap(assets.AppLogoPNG, iconPx, iconPx)
		if w.logoBmp == 0 {
			pal := getDashboardPalette(isLight)
			w.logoBmp, w.logoBuf, _ = RasterizeSVGToBitmap(assets.IconGaugeBold, iconPx, iconPx, pal.brandCyan)
		}

		w.resDPI = dpi
		w.resLight = isLight
	}

	if w.clientW > 0 && w.clientH > 0 && (w.memDC == 0 || w.dibW != w.clientW || w.dibH != w.clientH) {
		w.releaseDIB()
		screenDC, _, _ := procGetDC.Call(0)
		if screenDC == 0 {
			return
		}
		defer procReleaseDC.Call(0, screenDC)

		memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
		if memDC == 0 {
			return
		}
		bmi := bitmapInfo{
			BmiHeader: bitmapInfoHeader{
				BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
				BiWidth:       w.clientW,
				BiHeight:      -w.clientH,
				BiPlanes:      1,
				BiBitCount:    32,
				BiCompression: biRGB,
			},
		}
		var pBits unsafe.Pointer
		hBmp, _, _ := procCreateDIBSection.Call(screenDC, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&pBits)), 0, 0)
		if hBmp == 0 || pBits == nil {
			_, _, _ = procDeleteDC.Call(memDC)
			return
		}
		oldObj, _, _ := procSelectObject.Call(memDC, hBmp)
		var gpBmp uintptr
		st, _, _ := procGdipCreateBitmapFromScan0.Call(
			uintptr(w.clientW), uintptr(w.clientH), uintptr(w.clientW*4),
			gpPixelFormat32bppPARGB, uintptr(pBits), uintptr(unsafe.Pointer(&gpBmp)),
		)
		if st != 0 || gpBmp == 0 {
			_, _, _ = procSelectObject.Call(memDC, oldObj)
			_, _, _ = procDeleteObject.Call(hBmp)
			_, _, _ = procDeleteDC.Call(memDC)
			return
		}
		w.memDC = memDC
		w.hBitmap = hBmp
		w.oldBitmap = oldObj
		w.pBits = pBits
		w.gpBitmap = gpBmp
		w.dibW = w.clientW
		w.dibH = w.clientH
	}
}

func (w *SettingsWindow) paintClient(hdc uintptr) {
	if w.clientW <= 0 || w.clientH <= 0 {
		return
	}
	dpi := w.dpi
	if dpi == 0 {
		dpi = 96
	}
	isLight := DetectTaskbarLightTheme()
	w.ensureDrawResources(dpi, isLight)
	if w.gpBitmap == 0 || w.memDC == 0 {
		return
	}

	var gfx uintptr
	st, _, _ := procGdipGetImageGraphicsContext.Call(w.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		return
	}

	w.renderScene(gfx, w.clientW, w.clientH, dpi, isLight)
	_, _, _ = procGdipDeleteGraphics.Call(gfx)

	_, _, _ = procBitBlt.Call(
		hdc, 0, 0, uintptr(w.clientW), uintptr(w.clientH),
		w.memDC, 0, 0, srcCopy,
	)
}

// speedToGaugeFraction maps Mbps onto a Speedtest-style non-linear gauge scale [0..1].
func speedToGaugeFraction(mbps float64) float64 {
	if mbps <= 0 {
		return 0
	}
	stops := []struct{ mbps, frac float64 }{
		{0, 0.00},
		{5, 0.16},
		{15, 0.34},
		{30, 0.50},
		{60, 0.65},
		{120, 0.78},
		{250, 0.88},
		{500, 0.95},
		{1000, 1.00},
	}
	if mbps >= stops[len(stops)-1].mbps {
		return 1.0
	}
	for i := 1; i < len(stops); i++ {
		if mbps <= stops[i].mbps {
			span := stops[i].mbps - stops[i-1].mbps
			t := (mbps - stops[i-1].mbps) / span
			return stops[i-1].frac + t*(stops[i].frac-stops[i-1].frac)
		}
	}
	return 1.0
}

// buildGaugeArcPath constructs a closed rounded-cap ring arc around (cx, cy) using pure integer Beziers.
func buildGaugeArcPath(cx, cy, rOuter, rInner, startDeg, sweepDeg float64) (uintptr, error) {
	if sweepDeg <= 0 {
		return 0, nil
	}
	if sweepDeg > 359.5 {
		sweepDeg = 359.5
	}

	var gpPath uintptr
	st, _, _ := procGdipCreatePath.Call(gpFillModeWinding, uintptr(unsafe.Pointer(&gpPath)))
	if st != 0 || gpPath == 0 {
		return 0, fmt.Errorf("GdipCreatePath failed with status %d", st)
	}

	const scale = 64.0
	sc := func(v float64) int32 { return int32(math.Round(v * scale)) }

	addArcBeziers := func(acx, acy, r, aStartDeg, aEndDeg float64) {
		total := aEndDeg - aStartDeg
		segs := int(math.Ceil(math.Abs(total) / 45.0))
		if segs < 1 {
			segs = 1
		}
		step := total / float64(segs)
		for i := 0; i < segs; i++ {
			a0 := (aStartDeg + float64(i)*step) * math.Pi / 180.0
			a1 := (aStartDeg + float64(i+1)*step) * math.Pi / 180.0
			d := a1 - a0
			k := (4.0 / 3.0) * math.Tan(d/4.0)

			p0x, p0y := acx+r*math.Cos(a0), acy+r*math.Sin(a0)
			c1x, c1y := p0x-k*r*math.Sin(a0), p0y+k*r*math.Cos(a0)
			p3x, p3y := acx+r*math.Cos(a1), acy+r*math.Sin(a1)
			c2x, c2y := p3x+k*r*math.Sin(a1), p3y-k*r*math.Cos(a1)

			pts := [4]gpPointI{
				{sc(p0x), sc(p0y)},
				{sc(c1x), sc(c1y)},
				{sc(c2x), sc(c2y)},
				{sc(p3x), sc(p3y)},
			}
			_, _, _ = procGdipAddPathBeziersI.Call(gpPath, uintptr(unsafe.Pointer(&pts[0])), 4)
		}
	}

	endDeg := startDeg + sweepDeg
	rMid := (rOuter + rInner) / 2.0
	rCap := (rOuter - rInner) / 2.0

	_, _, _ = procGdipStartPathFigure.Call(gpPath)
	// Outer arc clockwise
	addArcBeziers(cx, cy, rOuter, startDeg, endDeg)
	// End cap semicircle
	endRad := endDeg * math.Pi / 180.0
	addArcBeziers(cx+rMid*math.Cos(endRad), cy+rMid*math.Sin(endRad), rCap, endDeg, endDeg+180.0)
	// Inner arc counter-clockwise back to start
	addArcBeziers(cx, cy, rInner, endDeg, startDeg)
	// Start cap semicircle
	startRad := startDeg * math.Pi / 180.0
	addArcBeziers(cx+rMid*math.Cos(startRad), cy+rMid*math.Sin(startRad), rCap, startDeg+180.0, startDeg+360.0)
	_, _, _ = procGdipClosePathFigure.Call(gpPath)

	// Scale subpixel path back down to 1:1 device pixels using integer matrix.
	boundW := int32(math.Ceil(cx+rOuter)) + 16
	boundH := int32(math.Ceil(cy+rOuter)) + 16
	srcRect := gpRectI{X: 0, Y: 0, Width: boundW * int32(scale), Height: boundH * int32(scale)}
	dstPlg := [3]gpPointI{
		{X: 0, Y: 0},
		{X: boundW, Y: 0},
		{X: 0, Y: boundH},
	}
	var matrix uintptr
	st, _, _ = procGdipCreateMatrix3I.Call(
		uintptr(unsafe.Pointer(&srcRect)),
		uintptr(unsafe.Pointer(&dstPlg[0])),
		uintptr(unsafe.Pointer(&matrix)),
	)
	if st == 0 && matrix != 0 {
		_, _, _ = procGdipTransformPath.Call(gpPath, matrix)
		_, _, _ = procGdipDeleteMatrix.Call(matrix)
	}
	return gpPath, nil
}

// renderScene draws the minimal brand-colored dashboard onto gfx and populates interactive controls.
func (w *SettingsWindow) renderScene(gfx uintptr, width, height int32, dpi uint32, isLight bool) {
	pal := getDashboardPalette(isLight)
	s := func(px int32) int32 { return scaleDPIInt(px, dpi) }

	_, _, _ = procGdipSetSmoothingMode.Call(gfx, gpSmoothingModeHighQuality)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)
	_, _, _ = procGdipGraphicsClear.Call(gfx, uintptr(pal.windowBG))

	w.controls = w.controls[:0]

	fmtNear, _ := gpCreateStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	fmtCenter, _ := gpCreateStringFormat(gpStringAlignmentCenter, gpStringAlignmentCenter)
	defer func() {
		if fmtNear != 0 {
			_, _, _ = procGdipDeleteStringFormat.Call(fmtNear)
		}
		if fmtCenter != 0 {
			_, _, _ = procGdipDeleteStringFormat.Call(fmtCenter)
		}
	}()

	bCard, _ := gpCreateSolidBrush(pal.cardBG)
	bBorder, _ := gpCreateSolidBrush(pal.cardBorder)
	bPill, _ := gpCreateSolidBrush(pal.pillBG)
	bPillHover, _ := gpCreateSolidBrush(pal.pillHoverBG)
	bCyan, _ := gpCreateSolidBrush(pal.brandCyan)
	bLime, _ := gpCreateSolidBrush(pal.brandLime)
	bMint, _ := gpCreateSolidBrush(pal.brandMint)
	bText, _ := gpCreateSolidBrush(pal.textPrimary)
	bMuted, _ := gpCreateSolidBrush(pal.textMuted)
	bAccentTxt, _ := gpCreateSolidBrush(pal.accentText)
	bTrack, _ := gpCreateSolidBrush(pal.trackBG)
	bTick, _ := gpCreateSolidBrush(pal.tickColor)
	bBadge, _ := gpCreateSolidBrush(pal.badgeBG)
	bSwitchOff, _ := gpCreateSolidBrush(pal.switchOffBG)
	bKnob, _ := gpCreateSolidBrush(pal.knobARGB)
	defer func() {
		for _, b := range []uintptr{bCard, bBorder, bPill, bPillHover, bCyan, bLime, bMint, bText, bMuted, bAccentTxt, bTrack, bTick, bBadge, bSwitchOff, bKnob} {
			if b != 0 {
				_, _, _ = procGdipDeleteBrush.Call(b)
			}
		}
	}()

	drawCard := func(x, y, cw, ch, r int32) {
		gpFillRoundedRect(gfx, bBorder, x, y, cw, ch, r)
		gpFillRoundedRect(gfx, bCard, x+1, y+1, cw-2, ch-2, r-1)
	}

	// All buttons and option controls render as full capsule pills (radius = height/2).
	drawPill := func(c uiControl) {
		r := c.bounds.Height() / 2
		if c.selected && c.isButton {
			gradBrush, err := gpCreateLinearBrushI(
				c.bounds.Left, c.bounds.Top,
				c.bounds.Right, c.bounds.Bottom,
				pal.brandCyan, pal.brandLime,
			)
			if err == nil {
				gpFillRoundedRect(gfx, gradBrush, c.bounds.Left, c.bounds.Top, c.bounds.Width(), c.bounds.Height(), r)
				_, _, _ = procGdipDeleteBrush.Call(gradBrush)
			} else {
				gpFillRoundedRect(gfx, bCyan, c.bounds.Left, c.bounds.Top, c.bounds.Width(), c.bounds.Height(), r)
			}
			gpDrawString(gfx, w.fontBody, fmtCenter, bAccentTxt, c.label, gpRectF{
				X: float32(c.bounds.Left), Y: float32(c.bounds.Top),
				Width: float32(c.bounds.Width()), Height: float32(c.bounds.Height()),
			})
			return
		}

		brush := bPill
		txtBrush := bText
		if c.selected {
			brush = bCyan
			txtBrush = bAccentTxt
		} else if w.hoveredAct == c.action {
			brush = bPillHover
		}
		gpFillRoundedRect(gfx, brush, c.bounds.Left, c.bounds.Top, c.bounds.Width(), c.bounds.Height(), r)
		gpDrawString(gfx, w.fontSmall, fmtCenter, txtBrush, c.label, gpRectF{
			X: float32(c.bounds.Left), Y: float32(c.bounds.Top),
			Width: float32(c.bounds.Width()), Height: float32(c.bounds.Height()),
		})
	}

	marginX := s(16)
	cardW := width - marginX*2

	// 1. Minimal Header (Logo + App Name + Live Speed Pill)
	hdrY := s(14)
	iconSz := s(28)
	if w.logoBmp != 0 {
		_, _, _ = procGdipDrawImageRectI.Call(gfx, w.logoBmp, uintptr(marginX), uintptr(hdrY+s(2)), uintptr(iconSz), uintptr(iconSz))
	}
	gpDrawString(gfx, w.fontTitle, fmtNear, bText, config.AppName, gpRectF{
		X: float32(marginX + iconSz + s(10)), Y: float32(hdrY), Width: float32(s(170)), Height: float32(s(32)),
	})

	liveBadgeW := s(212)
	liveBadgeH := s(30)
	liveBadgeX := width - marginX - liveBadgeW
	liveBadgeY := hdrY + s(1)
	gpFillRoundedRect(gfx, bBadge, liveBadgeX, liveBadgeY, liveBadgeW, liveBadgeH, liveBadgeH/2)

	dlLive := w.app.formatRate(w.app.currentSample.DownloadBytesPerSec).String()
	ulLive := w.app.formatRate(w.app.currentSample.UploadBytesPerSec).String()
	halfBadgeW := (liveBadgeW - s(12)) / 2
	gpDrawString(gfx, w.fontSmall, fmtCenter, bCyan, "↓ "+dlLive, gpRectF{
		X: float32(liveBadgeX + s(6)), Y: float32(liveBadgeY), Width: float32(halfBadgeW), Height: float32(liveBadgeH),
	})
	gpDrawString(gfx, w.fontSmall, fmtCenter, bLime, "↑ "+ulLive, gpRectF{
		X: float32(liveBadgeX + s(6) + halfBadgeW), Y: float32(liveBadgeY), Width: float32(halfBadgeW), Height: float32(liveBadgeH),
	})

	// 2. Big Speedometer Gauge Card
	stCardY := s(60)
	stCardH := s(298)
	drawCard(marginX, stCardY, cardW, stCardH, s(16))

	w.stMu.Lock()
	stProg := w.stProgress
	stRunning := w.stRunning
	w.stMu.Unlock()

	gaugeCX := float64(width) / 2.0
	gaugeCY := float64(stCardY + s(112))
	rOuter := float64(s(94))
	rInner := float64(s(80))

	// 270-degree background gauge track (135° to 405°)
	if trackPath, err := buildGaugeArcPath(gaugeCX, gaugeCY, rOuter, rInner, 135.0, 270.0); err == nil && trackPath != 0 {
		_, _, _ = procGdipFillPath.Call(gfx, bTrack, trackPath)
		_, _, _ = procGdipDeletePath.Call(trackPath)
	}

	// Subtle radial tick dots along the inner arc
	tickR := rInner - float64(s(9))
	tickSize := s(4)
	for i := 0; i <= 8; i++ {
		ang := (135.0 + float64(i)*270.0/8.0) * math.Pi / 180.0
		tx := int32(math.Round(gaugeCX+tickR*math.Cos(ang))) - tickSize/2
		ty := int32(math.Round(gaugeCY+tickR*math.Sin(ang))) - tickSize/2
		gpFillRoundedRect(gfx, bTick, tx, ty, tickSize, tickSize, tickSize/2)
	}

	// Determine active gauge sweep & hero center readout
	phaseLabel := "READY"
	heroValue := "—"
	heroUnit := "Mbps"
	var gaugeFrac float64

	switch stProg.Phase {
	case speedtest.PhasePing:
		phaseLabel = "PING"
		if stProg.PingMs > 0 {
			heroValue = fmt.Sprintf("%.0f", stProg.PingMs)
		}
		heroUnit = "ms"
		gaugeFrac = float64(stProg.Percent) / 15.0 * 0.25
	case speedtest.PhaseDownload:
		phaseLabel = "DOWNLOAD"
		heroValue = fmt.Sprintf("%.1f", stProg.DownloadMbps)
		heroUnit = "Mbps"
		gaugeFrac = speedToGaugeFraction(stProg.DownloadMbps)
	case speedtest.PhaseUpload:
		phaseLabel = "UPLOAD"
		heroValue = fmt.Sprintf("%.1f", stProg.UploadMbps)
		heroUnit = "Mbps"
		gaugeFrac = speedToGaugeFraction(stProg.UploadMbps)
	case speedtest.PhaseDone:
		phaseLabel = "DOWNLOAD"
		heroValue = fmt.Sprintf("%.1f", stProg.DownloadMbps)
		heroUnit = "Mbps"
		gaugeFrac = speedToGaugeFraction(stProg.DownloadMbps)
	case speedtest.PhaseCancelled:
		phaseLabel = "CANCELLED"
	case speedtest.PhaseError:
		phaseLabel = "OFFLINE"
		heroUnit = ""
	}

	if gaugeFrac > 0 {
		if activePath, err := buildGaugeArcPath(gaugeCX, gaugeCY, rOuter, rInner, 135.0, gaugeFrac*270.0); err == nil && activePath != 0 {
			x0 := int32(math.Round(gaugeCX - rOuter))
			y0 := int32(math.Round(gaugeCY - rOuter))
			x1 := int32(math.Round(gaugeCX + rOuter))
			y1 := int32(math.Round(gaugeCY + rOuter))
			gradBrush, gErr := gpCreateLinearBrushI(x0, y0, x1, y1, pal.brandCyan, pal.brandLime)
			if gErr == nil {
				_, _, _ = procGdipFillPath.Call(gfx, gradBrush, activePath)
				_, _, _ = procGdipDeleteBrush.Call(gradBrush)
			} else {
				_, _, _ = procGdipFillPath.Call(gfx, bCyan, activePath)
			}
			_, _, _ = procGdipDeletePath.Call(activePath)
		}
	}

	// Center Hero Typography inside Gauge
	gpDrawString(gfx, w.fontCaps, fmtCenter, bMuted, phaseLabel, gpRectF{
		X: float32(gaugeCX) - float32(s(70)), Y: float32(gaugeCY) - float32(s(44)),
		Width: float32(s(140)), Height: float32(s(16)),
	})
	gpDrawString(gfx, w.fontGaugeHero, fmtCenter, bText, heroValue, gpRectF{
		X: float32(gaugeCX) - float32(s(84)), Y: float32(gaugeCY) - float32(s(22)),
		Width: float32(s(168)), Height: float32(s(44)),
	})
	if heroUnit != "" {
		gpDrawString(gfx, w.fontGaugeUnit, fmtCenter, bMuted, heroUnit, gpRectF{
			X: float32(gaugeCX) - float32(s(60)), Y: float32(gaugeCY) + float32(s(24)),
			Width: float32(s(120)), Height: float32(s(18)),
		})
	}

	// Three compact result pills directly beneath the gauge
	metricY := stCardY + s(202)
	metricH := s(32)
	metricGap := s(8)
	metricPad := s(22)
	metricW := (cardW - metricPad*2 - metricGap*2) / 3

	pingTxt := "Ping  —"
	if stProg.PingMs > 0 {
		pingTxt = fmt.Sprintf("Ping  %.0f ms", stProg.PingMs)
	}
	dlTxt := "↓  —"
	if stProg.DownloadMbps > 0 || stProg.Phase == speedtest.PhaseDone {
		dlTxt = fmt.Sprintf("↓  %.1f Mbps", stProg.DownloadMbps)
	}
	ulTxt := "↑  —"
	if stProg.UploadMbps > 0 || stProg.Phase == speedtest.PhaseDone {
		ulTxt = fmt.Sprintf("↑  %.1f Mbps", stProg.UploadMbps)
	}

	metricPills := []struct {
		text  string
		brush uintptr
	}{
		{pingTxt, bMint},
		{dlTxt, bCyan},
		{ulTxt, bLime},
	}
	for i, mp := range metricPills {
		px := marginX + metricPad + int32(i)*(metricW+metricGap)
		gpFillRoundedRect(gfx, bPill, px, metricY, metricW, metricH, metricH/2)
		gpDrawString(gfx, w.fontPillMetric, fmtCenter, mp.brush, mp.text, gpRectF{
			X: float32(px + s(4)), Y: float32(metricY), Width: float32(metricW - s(8)), Height: float32(metricH),
		})
	}

	// Centered capsule Start / Cancel / Test Again button
	btnW := s(166)
	btnH := s(38)
	btnX := (width - btnW) / 2
	btnY := stCardY + s(246)
	btnLabel := "Start Speed Test"
	if stRunning {
		btnLabel = "Cancel"
	} else if stProg.Phase == speedtest.PhaseDone {
		btnLabel = "Test Again"
	}
	stBtn := uiControl{
		action:   actSpeedTestToggle,
		bounds:   rect{Left: btnX, Top: btnY, Right: btnX + btnW, Bottom: btnY + btnH},
		label:    btnLabel,
		selected: !stRunning,
		isButton: true,
	}
	w.controls = append(w.controls, stBtn)
	drawPill(stBtn)

	// 3. Minimal Settings Card (4 horizontal label + capsule pill rows)
	appCardY := stCardY + stCardH + s(14)
	appCardH := s(196)
	drawCard(marginX, appCardY, cardW, appCardH, s(16))

	cfg := w.app.cfg
	labelColW := s(56)
	pillStartX := marginX + s(16) + labelColW
	pillAreaW := cardW - s(32) - labelColW
	pillH := s(32)

	drawInlinePillRow := func(y int32, title string, items []uiControl) {
		gpDrawString(gfx, w.fontSmall, fmtNear, bMuted, title, gpRectF{
			X: float32(marginX + s(16)), Y: float32(y), Width: float32(labelColW - s(4)), Height: float32(pillH),
		})
		gap := s(6)
		n := int32(len(items))
		pw := (pillAreaW - gap*(n-1)) / n
		for i := range items {
			px := pillStartX + int32(i)*(pw+gap)
			items[i].bounds = rect{Left: px, Top: y, Right: px + pw, Bottom: y + pillH}
			w.controls = append(w.controls, items[i])
			drawPill(items[i])
		}
	}

	rowStep := s(44)
	row0Y := appCardY + s(16)
	drawInlinePillRow(row0Y, "Show", []uiControl{
		{action: actModeBoth, label: "Both", selected: cfg.DisplayMode == config.ModeBoth},
		{action: actModeDownload, label: "Download", selected: cfg.DisplayMode == config.ModeDownload},
		{action: actModeUpload, label: "Upload", selected: cfg.DisplayMode == config.ModeUpload},
		{action: actModeHide, label: "Hide", selected: cfg.DisplayMode == config.ModeHide},
	})
	drawInlinePillRow(row0Y+rowStep, "Label", []uiControl{
		{action: actLabelArrows, label: "Arrows", selected: cfg.LabelStyle == config.LabelArrows},
		{action: actLabelUD, label: "U / D", selected: cfg.LabelStyle == config.LabelUD},
		{action: actLabelWords, label: "Up / Down", selected: cfg.LabelStyle == config.LabelWords},
	})
	drawInlinePillRow(row0Y+rowStep*2, "Layout", []uiControl{
		{action: actLayoutStackU, label: "Up / Down", selected: cfg.Layout == config.LayoutStackedUp},
		{action: actLayoutStackD, label: "Down / Up", selected: cfg.Layout == config.LayoutStackedDown},
		{action: actLayoutInline, label: "Inline", selected: cfg.Layout == config.LayoutInline},
		{action: actLayoutCompact, label: "Compact", selected: cfg.Layout == config.LayoutCompact},
	})
	drawInlinePillRow(row0Y+rowStep*3, "Unit", []uiControl{
		{action: actUnitBytes, label: "Bytes/s (MB/s)", selected: cfg.SpeedUnit == config.UnitBytes},
		{action: actUnitBits, label: "Bits/s (Mbps)", selected: cfg.SpeedUnit == config.UnitBits},
	})

	// 4. Clean Minimal Footer Row (Start with Windows switch + Sponsor pill)
	footY := appCardY + appCardH + s(14)
	footH := s(54)
	drawCard(marginX, footY, cardW, footH, s(16))

	autoStart, _ := config.IsAutoStartEnabled()
	swW := s(40)
	swH := s(22)
	swX := marginX + s(16)
	swY := footY + (footH-swH)/2

	w.controls = append(w.controls, uiControl{
		action:   actAutoStart,
		bounds:   rect{Left: swX, Top: swY, Right: swX + s(185), Bottom: swY + swH},
		isSwitch: true,
		selected: autoStart,
	})

	trackBrush := bSwitchOff
	knobX := swX + s(3)
	if autoStart {
		trackBrush = bCyan
		knobX = swX + swW - s(19)
	}
	gpFillRoundedRect(gfx, trackBrush, swX, swY, swW, swH, swH/2)
	gpFillRoundedRect(gfx, bKnob, knobX, swY+s(3), s(16), s(16), s(8))
	gpDrawString(gfx, w.fontSmall, fmtNear, bText, "Start with Windows", gpRectF{
		X: float32(swX + swW + s(10)), Y: float32(footY), Width: float32(s(140)), Height: float32(footH),
	})

	spW := s(164)
	spH := s(32)
	spX := marginX + cardW - s(14) - spW
	spY := footY + (footH-spH)/2
	spCtrl := uiControl{
		action: actSponsor,
		bounds: rect{Left: spX, Top: spY, Right: spX + spW, Bottom: spY + spH},
		label:  "♥ Sponsor @theshubh77",
	}
	w.controls = append(w.controls, spCtrl)
	drawPill(spCtrl)
}

func (w *SettingsWindow) releaseDIB() {
	if w.gpBitmap != 0 {
		_, _, _ = procGdipDisposeImage.Call(w.gpBitmap)
		w.gpBitmap = 0
	}
	if w.memDC != 0 {
		if w.oldBitmap != 0 {
			_, _, _ = procSelectObject.Call(w.memDC, w.oldBitmap)
			w.oldBitmap = 0
		}
		_, _, _ = procDeleteDC.Call(w.memDC)
		w.memDC = 0
	}
	if w.hBitmap != 0 {
		_, _, _ = procDeleteObject.Call(w.hBitmap)
		w.hBitmap = 0
	}
	w.pBits = nil
	w.dibW = 0
	w.dibH = 0
}

func (w *SettingsWindow) releaseFontAndIconResources() {
	for _, f := range []*uintptr{
		&w.fontTitle, &w.fontBody, &w.fontSmall, &w.fontCaps,
		&w.fontGaugeHero, &w.fontGaugeUnit, &w.fontPillMetric,
	} {
		if *f != 0 {
			_, _, _ = procGdipDeleteFont.Call(*f)
			*f = 0
		}
	}
	if w.logoBmp != 0 {
		_, _, _ = procGdipDisposeImage.Call(w.logoBmp)
		w.logoBmp = 0
	}
	w.logoBuf = nil
}

func (w *SettingsWindow) releaseDrawResources() {
	w.releaseDIB()
	w.releaseFontAndIconResources()
}
