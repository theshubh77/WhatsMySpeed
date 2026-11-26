package ui

import (
	"context"
	"fmt"
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

	baseWindowW = 540
	baseWindowH = 668
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
	isLight       bool
	windowBG      uint32
	cardBG        uint32
	cardBorder    uint32
	pillBG        uint32
	pillHoverBG   uint32
	accent        uint32
	accentHover   uint32
	accentText    uint32
	downloadColor uint32
	uploadColor   uint32
	pingColor     uint32
	textPrimary   uint32
	textMuted     uint32
	trackBG       uint32
	switchOffBG   uint32
	knobARGB      uint32
	badgeBG       uint32
}

func getDashboardPalette(isLight bool) dashboardPalette {
	if isLight {
		return dashboardPalette{
			isLight:       true,
			windowBG:      0xFFF3F5F8,
			cardBG:        0xFFFFFFFF,
			cardBorder:    0xFFE1E5EC,
			pillBG:        0xFFF0F2F6,
			pillHoverBG:   0xFFE4E8F0,
			accent:        0xFF0067C0,
			accentHover:   0xFF005AA8,
			accentText:    0xFFFFFFFF,
			downloadColor: 0xFF0067C0,
			uploadColor:   0xFF6B4EFF,
			pingColor:     0xFF0F8B5F,
			textPrimary:   0xFF1A1D24,
			textMuted:     0xFF5D6575,
			trackBG:       0xFFE3E7EE,
			switchOffBG:   0xFFC0C6D2,
			knobARGB:      0xFFFFFFFF,
			badgeBG:       0xFFEBF0F7,
		}
	}
	return dashboardPalette{
		isLight:       false,
		windowBG:      0xFF17181C,
		cardBG:        0xFF22242A,
		cardBorder:    0xFF333640,
		pillBG:        0xFF2C2F37,
		pillHoverBG:   0xFF363A45,
		accent:        0xFF4CC2FF,
		accentHover:   0xFF38B4F2,
		accentText:    0xFF08141E,
		downloadColor: 0xFF4CC2FF,
		uploadColor:   0xFFA78BFA,
		pingColor:     0xFF4ADE80,
		textPrimary:   0xFFF4F6FA,
		textMuted:     0xFF9EA6B5,
		trackBG:       0xFF2F333E,
		switchOffBG:   0xFF454A57,
		knobARGB:      0xFFFFFFFF,
		badgeBG:       0xFF2A2D36,
	}
}

// SettingsWindow implements the native Windows 11 WhatsMySpeed dashboard with
// taskbar customization controls and a built-in multi-stream Speed Test.
type SettingsWindow struct {
	app         *App
	hwnd        uintptr
	classReg    bool
	hIconSmall  uintptr
	hIconBig    uintptr
	dpi         uint32
	clientW     int32
	clientH     int32
	hoveredAct  controlAction
	tracking    bool
	controls    []uiControl

	// Offscreen DIB for flicker-free WM_PAINT.
	memDC     uintptr
	hBitmap   uintptr
	oldBitmap uintptr
	pBits     unsafe.Pointer
	gpBitmap  uintptr
	dibW      int32
	dibH      int32

	// Cached fonts & header icon.
	fontTitle   uintptr
	fontSection uintptr
	fontBody    uintptr
	fontSmall   uintptr
	fontMetric  uintptr
	gaugeBmp    uintptr
	gaugeBuf    []byte
	resDPI      uint32
	resLight    bool

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
			Phase:       speedtest.PhaseIdle,
			ServerLabel: "Cloudflare Edge",
			StatusText:  "Ready to test your connection",
		},
	}
}

// Show displays and focuses the WhatsMySpeed settings and speedtest window.
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

// RefreshAppearance forces a repaint and updates title bar theme / DPI sizing.
func (w *SettingsWindow) RefreshAppearance() {
	if w.hwnd == 0 {
		return
	}
	w.applyTitleBarTheme()
	if vis, _, _ := procIsWindowVisible.Call(w.hwnd); vis != 0 {
		w.invalidate()
	}
}

// NotifySampleUpdated refreshes live speed metrics when the dashboard window is open.
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
		w.hIconSmall, _ = CreateSizedHICON(assets.IconGaugeBold, scaleDPIInt(16, dpi), scaleDPIInt(16, dpi), 0xFF4CC2FF)
	}
	if w.hIconBig == 0 {
		w.hIconBig, _ = CreateSizedHICON(assets.IconGaugeBold, scaleDPIInt(32, dpi), scaleDPIInt(32, dpi), 0xFF4CC2FF)
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
		Phase:       speedtest.PhasePing,
		Percent:     2,
		ServerLabel: "Cloudflare Edge",
		StatusText:  "Connecting & measuring latency...",
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
		w.fontTitle, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(20, dpi), 700)
		w.fontSection, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(14, dpi), 600)
		w.fontBody, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(12, dpi), 500)
		w.fontSmall, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(11, dpi), 400)
		w.fontMetric, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(18, dpi), 700)

		iconPx := scaleDPIInt(32, dpi)
		w.gaugeBmp, w.gaugeBuf, _ = DecodePNGToBitmap(assets.AppLogoPNG, iconPx, iconPx)
		if w.gaugeBmp == 0 {
			pal := getDashboardPalette(isLight)
			w.gaugeBmp, w.gaugeBuf, _ = RasterizeSVGToBitmap(assets.IconGaugeBold, iconPx, iconPx, pal.accent)
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

// renderScene draws the full WhatsMySpeed dashboard onto gfx and populates interactive hit-test regions.
func (w *SettingsWindow) renderScene(gfx uintptr, width, height int32, dpi uint32, isLight bool) {
	pal := getDashboardPalette(isLight)
	s := func(px int32) int32 { return scaleDPIInt(px, dpi) }

	_, _, _ = procGdipSetSmoothingMode.Call(gfx, gpSmoothingModeHighQuality)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)
	_, _, _ = procGdipGraphicsClear.Call(gfx, uintptr(pal.windowBG))

	w.controls = w.controls[:0]

	fmtNear, _ := gpCreateStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	fmtCenter, _ := gpCreateStringFormat(gpStringAlignmentCenter, gpStringAlignmentCenter)
	fmtFar, _ := gpCreateStringFormat(gpStringAlignmentFar, gpStringAlignmentCenter)
	defer func() {
		if fmtNear != 0 {
			_, _, _ = procGdipDeleteStringFormat.Call(fmtNear)
		}
		if fmtCenter != 0 {
			_, _, _ = procGdipDeleteStringFormat.Call(fmtCenter)
		}
		if fmtFar != 0 {
			_, _, _ = procGdipDeleteStringFormat.Call(fmtFar)
		}
	}()

	bCard, _ := gpCreateSolidBrush(pal.cardBG)
	bBorder, _ := gpCreateSolidBrush(pal.cardBorder)
	bPill, _ := gpCreateSolidBrush(pal.pillBG)
	bPillHover, _ := gpCreateSolidBrush(pal.pillHoverBG)
	bAccent, _ := gpCreateSolidBrush(pal.accent)
	bAccentHover, _ := gpCreateSolidBrush(pal.accentHover)
	bText, _ := gpCreateSolidBrush(pal.textPrimary)
	bMuted, _ := gpCreateSolidBrush(pal.textMuted)
	bAccentTxt, _ := gpCreateSolidBrush(pal.accentText)
	bTrack, _ := gpCreateSolidBrush(pal.trackBG)
	bBadge, _ := gpCreateSolidBrush(pal.badgeBG)
	bDl, _ := gpCreateSolidBrush(pal.downloadColor)
	bUl, _ := gpCreateSolidBrush(pal.uploadColor)
	bPing, _ := gpCreateSolidBrush(pal.pingColor)
	bSwitchOff, _ := gpCreateSolidBrush(pal.switchOffBG)
	bKnob, _ := gpCreateSolidBrush(pal.knobARGB)
	defer func() {
		for _, b := range []uintptr{bCard, bBorder, bPill, bPillHover, bAccent, bAccentHover, bText, bMuted, bAccentTxt, bTrack, bBadge, bDl, bUl, bPing, bSwitchOff, bKnob} {
			if b != 0 {
				_, _, _ = procGdipDeleteBrush.Call(b)
			}
		}
	}()

	drawCard := func(x, y, cw, ch, r int32) {
		gpFillRoundedRect(gfx, bBorder, x, y, cw, ch, r)
		gpFillRoundedRect(gfx, bCard, x+1, y+1, cw-2, ch-2, r-1)
	}

	drawPill := func(c uiControl) {
		brush := bPill
		txtBrush := bText
		if c.selected {
			brush = bAccent
			txtBrush = bAccentTxt
			if w.hoveredAct == c.action && c.isButton {
				brush = bAccentHover
			}
		} else if w.hoveredAct == c.action {
			brush = bPillHover
		}
		gpFillRoundedRect(gfx, brush, c.bounds.Left, c.bounds.Top, c.bounds.Width(), c.bounds.Height(), s(6))
		font := w.fontBody
		if c.isButton {
			font = w.fontSection
		}
		gpDrawString(gfx, font, fmtCenter, txtBrush, c.label, gpRectF{
			X: float32(c.bounds.Left), Y: float32(c.bounds.Top),
			Width: float32(c.bounds.Width()), Height: float32(c.bounds.Height()),
		})
	}

	marginX := s(18)
	cardW := width - marginX*2

	// 1. Header
	hdrY := s(16)
	iconSz := s(32)
	if w.gaugeBmp != 0 {
		_, _, _ = procGdipDrawImageRectI.Call(gfx, w.gaugeBmp, uintptr(marginX), uintptr(hdrY+s(4)), uintptr(iconSz), uintptr(iconSz))
	}
	titleX := float32(marginX + iconSz + s(12))
	gpDrawString(gfx, w.fontTitle, fmtNear, bText, config.AppName, gpRectF{
		X: titleX, Y: float32(hdrY), Width: float32(s(200)), Height: float32(s(26)),
	})
	nicSub := fmt.Sprintf("Windows 11 Speed Meter  •  %d active NIC", w.app.currentSample.ActiveInterfaces)
	if w.app.currentSample.ActiveInterfaces != 1 {
		nicSub = fmt.Sprintf("Windows 11 Speed Meter  •  %d active NICs", w.app.currentSample.ActiveInterfaces)
	}
	gpDrawString(gfx, w.fontSmall, fmtNear, bMuted, nicSub, gpRectF{
		X: titleX, Y: float32(hdrY + s(27)), Width: float32(s(260)), Height: float32(s(16)),
	})

	// Live speed summary badge on header right
	badgeW := s(200)
	badgeH := s(44)
	badgeX := width - marginX - badgeW
	badgeY := hdrY + s(2)
	gpFillRoundedRect(gfx, bBadge, badgeX, badgeY, badgeW, badgeH, s(8))
	dlLive := w.app.formatRate(w.app.currentSample.DownloadBytesPerSec).String()
	ulLive := w.app.formatRate(w.app.currentSample.UploadBytesPerSec).String()
	gpDrawString(gfx, w.fontBody, fmtCenter, bDl, "↓ "+dlLive, gpRectF{
		X: float32(badgeX + s(6)), Y: float32(badgeY + s(4)), Width: float32(badgeW - s(12)), Height: float32(s(18)),
	})
	gpDrawString(gfx, w.fontBody, fmtCenter, bUl, "↑ "+ulLive, gpRectF{
		X: float32(badgeX + s(6)), Y: float32(badgeY + s(22)), Width: float32(badgeW - s(12)), Height: float32(s(18)),
	})

	// 2. Speed Test Card
	stCardY := s(76)
	stCardH := s(178)
	drawCard(marginX, stCardY, cardW, stCardH, s(10))

	w.stMu.Lock()
	stProg := w.stProgress
	stRunning := w.stRunning
	w.stMu.Unlock()

	gpDrawString(gfx, w.fontSection, fmtNear, bText, "Speed Test", gpRectF{
		X: float32(marginX + s(14)), Y: float32(stCardY + s(12)), Width: float32(s(150)), Height: float32(s(22)),
	})
	srvLabel := stProg.ServerLabel
	if srvLabel == "" {
		srvLabel = "Cloudflare Edge"
	}
	gpDrawString(gfx, w.fontSmall, fmtFar, bMuted, srvLabel, gpRectF{
		X: float32(marginX + cardW - s(210)), Y: float32(stCardY + s(14)), Width: float32(s(196)), Height: float32(s(18)),
	})

	// Three metric tiles: Ping, Download, Upload
	tileGap := s(10)
	tileW := (cardW - s(28) - tileGap*2) / 3
	tileH := s(66)
	tileY := stCardY + s(44)
	tileStartX := marginX + s(14)

	pingStr := "—"
	if stProg.PingMs > 0 {
		pingStr = fmt.Sprintf("%.0f ms", stProg.PingMs)
	}
	dlStr := "—"
	if stProg.DownloadMbps > 0 || stProg.Phase == speedtest.PhaseDone {
		dlStr = fmt.Sprintf("%.1f Mbps", stProg.DownloadMbps)
	}
	ulStr := "—"
	if stProg.UploadMbps > 0 || stProg.Phase == speedtest.PhaseDone {
		ulStr = fmt.Sprintf("%.1f Mbps", stProg.UploadMbps)
	}

	tiles := []struct {
		title string
		val   string
		brush uintptr
	}{
		{"PING", pingStr, bPing},
		{"DOWNLOAD", dlStr, bDl},
		{"UPLOAD", ulStr, bUl},
	}
	for idx, tl := range tiles {
		tx := tileStartX + int32(idx)*(tileW+tileGap)
		gpFillRoundedRect(gfx, bPill, tx, tileY, tileW, tileH, s(8))
		gpDrawString(gfx, w.fontSmall, fmtCenter, bMuted, tl.title, gpRectF{
			X: float32(tx), Y: float32(tileY + s(8)), Width: float32(tileW), Height: float32(s(16)),
		})
		gpDrawString(gfx, w.fontMetric, fmtCenter, tl.brush, tl.val, gpRectF{
			X: float32(tx + s(4)), Y: float32(tileY + s(28)), Width: float32(tileW - s(8)), Height: float32(s(28)),
		})
	}

	// Progress bar + Start/Cancel button
	btnW := s(138)
	btnH := s(36)
	btnX := marginX + cardW - s(14) - btnW
	btnY := stCardY + s(124)

	statusW := btnX - (marginX + s(14)) - s(14)
	statusText := stProg.StatusText
	if stProg.Phase == speedtest.PhaseError && stProg.Error != "" {
		statusText = stProg.Error
	}
	gpDrawString(gfx, w.fontSmall, fmtNear, bMuted, statusText, gpRectF{
		X: float32(marginX + s(14)), Y: float32(btnY + s(2)), Width: float32(statusW), Height: float32(s(18)),
	})

	barY := btnY + s(24)
	barH := s(8)
	gpFillRoundedRect(gfx, bTrack, marginX+s(14), barY, statusW, barH, s(4))
	if stProg.Percent > 0 {
		fillW := (statusW * int32(stProg.Percent)) / 100
		if fillW < barH {
			fillW = barH
		}
		gpFillRoundedRect(gfx, bAccent, marginX+s(14), barY, fillW, barH, s(4))
	}

	btnLabel := "Start Speed Test"
	if stRunning {
		btnLabel = "Cancel Test"
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

	// 3. Taskbar Appearance Card
	appCardY := stCardY + stCardH + s(14)
	appCardH := s(296)
	drawCard(marginX, appCardY, cardW, appCardH, s(10))

	gpDrawString(gfx, w.fontSection, fmtNear, bText, "Taskbar Appearance", gpRectF{
		X: float32(marginX + s(14)), Y: float32(appCardY + s(12)), Width: float32(s(220)), Height: float32(s(22)),
	})

	cfg := w.app.cfg
	rowStartX := marginX + s(14)
	rowInnerW := cardW - s(28)

	drawPillRow := func(y int32, label string, items []uiControl) {
		gpDrawString(gfx, w.fontSmall, fmtNear, bMuted, label, gpRectF{
			X: float32(rowStartX), Y: float32(y), Width: float32(rowInnerW), Height: float32(s(16)),
		})
		gap := s(8)
		n := int32(len(items))
		pw := (rowInnerW - gap*(n-1)) / n
		ph := s(32)
		py := y + s(20)
		for i := range items {
			px := rowStartX + int32(i)*(pw+gap)
			items[i].bounds = rect{Left: px, Top: py, Right: px + pw, Bottom: py + ph}
			w.controls = append(w.controls, items[i])
			drawPill(items[i])
		}
	}

	drawPillRow(appCardY+s(44), "Display Mode", []uiControl{
		{action: actModeBoth, label: "Show Both", selected: cfg.DisplayMode == config.ModeBoth},
		{action: actModeDownload, label: "Download Only", selected: cfg.DisplayMode == config.ModeDownload},
		{action: actModeUpload, label: "Upload Only", selected: cfg.DisplayMode == config.ModeUpload},
		{action: actModeHide, label: "Hide Speed", selected: cfg.DisplayMode == config.ModeHide},
	})

	drawPillRow(appCardY+s(106), "Label Style", []uiControl{
		{action: actLabelArrows, label: "Arrows (↑ ↓)", selected: cfg.LabelStyle == config.LabelArrows},
		{action: actLabelUD, label: "U / D", selected: cfg.LabelStyle == config.LabelUD},
		{action: actLabelWords, label: "Up / Down", selected: cfg.LabelStyle == config.LabelWords},
	})

	drawPillRow(appCardY+s(168), "Taskbar Layout", []uiControl{
		{action: actLayoutStackU, label: "Stacked (↑/↓)", selected: cfg.Layout == config.LayoutStackedUp},
		{action: actLayoutStackD, label: "Stacked (↓/↑)", selected: cfg.Layout == config.LayoutStackedDown},
		{action: actLayoutInline, label: "Inline", selected: cfg.Layout == config.LayoutInline},
		{action: actLayoutCompact, label: "Compact", selected: cfg.Layout == config.LayoutCompact},
	})

	drawPillRow(appCardY+s(230), "Speed Unit", []uiControl{
		{action: actUnitBytes, label: "Bytes/s (KB/s, MB/s)", selected: cfg.SpeedUnit == config.UnitBytes},
		{action: actUnitBits, label: "Bits/s (Kbps, Mbps)", selected: cfg.SpeedUnit == config.UnitBits},
	})

	// 4. Footer: Start with Windows & Sponsor
	footY := appCardY + appCardH + s(14)
	footH := s(72)
	drawCard(marginX, footY, cardW, footH, s(10))

	autoStart, _ := config.IsAutoStartEnabled()
	swW := s(44)
	swH := s(24)
	swX := marginX + s(14)
	swY := footY + s(14)

	swCtrl := uiControl{
		action:   actAutoStart,
		bounds:   rect{Left: swX, Top: swY, Right: swX + s(210), Bottom: swY + swH},
		isSwitch: true,
		selected: autoStart,
	}
	w.controls = append(w.controls, swCtrl)

	trackBrush := bSwitchOff
	knobX := swX + s(3)
	if autoStart {
		trackBrush = bAccent
		knobX = swX + swW - s(21)
	}
	gpFillRoundedRect(gfx, trackBrush, swX, swY, swW, swH, swH/2)
	gpFillRoundedRect(gfx, bKnob, knobX, swY+s(3), s(18), s(18), s(9))
	gpDrawString(gfx, w.fontBody, fmtNear, bText, "Start with Windows", gpRectF{
		X: float32(swX + swW + s(10)), Y: float32(swY), Width: float32(s(160)), Height: float32(swH),
	})
	gpDrawString(gfx, w.fontSmall, fmtNear, bMuted, `Config: %APPDATA%\WhatsMySpeed\config.json`, gpRectF{
		X: float32(swX), Y: float32(swY + swH + s(6)), Width: float32(s(300)), Height: float32(s(16)),
	})

	spW := s(168)
	spH := s(34)
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
	for _, f := range []*uintptr{&w.fontTitle, &w.fontSection, &w.fontBody, &w.fontSmall, &w.fontMetric} {
		if *f != 0 {
			_, _, _ = procGdipDeleteFont.Call(*f)
			*f = 0
		}
	}
	if w.gaugeBmp != 0 {
		_, _, _ = procGdipDisposeImage.Call(w.gaugeBmp)
		w.gaugeBmp = 0
	}
	w.gaugeBuf = nil
}

func (w *SettingsWindow) releaseDrawResources() {
	w.releaseDIB()
	w.releaseFontAndIconResources()
}
