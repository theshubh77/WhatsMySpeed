package ui

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/speedtest"
	"golang.org/x/sys/windows"
)

const (
	settingsClassName = "WhatsMySpeedDashboardWindow"
	sponsorURL        = "https://github.com/sponsors/theshubh77"
	repoURL           = "https://github.com/theshubh77/WhatsMySpeed"
	linkedinURL       = "https://www.linkedin.com/in/theshubh77"

	baseWindowW    = 626
	baseWindowH    = 756
	timerAnimID    = 10
	animIntervalMs = 16
)

type controlAction int

const (
	actNone controlAction = iota
	actSpeedTestToggle
	actThemeLight
	actThemeDark
	actThemeSystem
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
	actGitHub
	actSponsor
	actLinkedIn
)

type uiControl struct {
	action   controlAction
	bounds   rect
	label    string
	selected bool
	isSwitch bool
	isButton bool
	iconBmp  uintptr
	iconSize int32
}

type dashboardPalette struct {
	isLight     bool
	windowBG    uint32
	cardBG      uint32
	cardBorder  uint32
	faintBorder uint32
	pillBG      uint32
	pillHoverBG uint32
	brandCyan   uint32
	brandLime   uint32
	brandMint   uint32
	heartPink   uint32
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
			faintBorder: 0x180F172A,
			pillBG:      0xFFEDF2F7,
			pillHoverBG: 0xFFE2E8F0,
			brandCyan:   0xFF0891B2,
			brandLime:   0xFF65A30D,
			brandMint:   0xFF0D9488,
			heartPink:   0xFFDB2777,
			accentText:  0xFFFFFFFF,
			textPrimary: 0xFF0F172A,
			textMuted:   0xFF475569,
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
		faintBorder: 0x16FFFFFF,
		pillBG:      0xFF1A2233,
		pillHoverBG: 0xFF232E45,
		brandCyan:   0xFF22D3EE,
		brandLime:   0xFFA3E635,
		brandMint:   0xFF5EEAD4,
		heartPink:   0xFFF472B6,
		accentText:  0xFF07131A,
		textPrimary: 0xFFF8FAFC,
		textMuted:   0xFF94A3B8,
		trackBG:     0xFF1D273B,
		tickColor:   0xFF2E3D59,
		switchOffBG: 0xFF28354D,
		knobARGB:    0xFFFFFFFF,
		badgeBG:     0xFF131A29,
	}
}

// SettingsWindow implements the minimal, brand-styled WhatsMySpeed window with
// a speedometer gauge, theme switcher, GitHub link, and taskbar configuration pills.
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

	// Cached fonts & vector icons.
	fontTitle      uintptr
	fontBody       uintptr
	fontSmall      uintptr
	fontCaption    uintptr
	fontCaps       uintptr
	fontGaugeHero  uintptr
	fontGaugeUnit  uintptr
	fontPillMetric uintptr
	logoBmp        uintptr
	logoBuf        []byte
	sunBmp         uintptr
	sunBuf         []byte
	sunSelBmp      uintptr
	sunSelBuf      []byte
	moonBmp        uintptr
	moonBuf        []byte
	moonSelBmp     uintptr
	moonSelBuf     []byte
	monBmp         uintptr
	monBuf         []byte
	monSelBmp      uintptr
	monSelBuf      []byte
	githubBmp      uintptr
	githubBuf      []byte
	heartBmp       uintptr
	heartBuf       []byte
	madeHeartBmp   uintptr
	madeHeartBuf   []byte
	dlSmallBmp     uintptr
	dlSmallBuf     []byte
	ulSmallBmp     uintptr
	ulSmallBuf     []byte
	dlMetricBmp    uintptr
	dlMetricBuf    []byte
	ulMetricBmp    uintptr
	ulMetricBuf    []byte
	resDPI         uint32
	resLight       bool

	// Speed test runtime state & 60fps gauge smoothing.
	stMu          sync.Mutex
	stRunning     bool
	stCancel      context.CancelFunc
	stProgress    speedtest.Progress
	animTimerOn   bool
	animLastTick  time.Time
	animGaugeFrac float64
	animDisplay   float64
	pulsePhase    float64
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

func (w *SettingsWindow) isWindowLightTheme() bool {
	switch w.app.cfg.Theme {
	case config.ThemeLight:
		return true
	case config.ThemeDark:
		return false
	default:
		return DetectTaskbarLightTheme()
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
	w.stopAnimTimer()
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
	if !w.isWindowLightTheme() {
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
		w.startAnimTimer()
		w.invalidate()
		return 0

	case wmTimer:
		if wParam == timerAnimID {
			w.stepAnimation()
		}
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
		w.stopAnimTimer()
		w.CancelSpeedTest()
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
	case actThemeLight:
		cfg.Theme = config.ThemeLight
	case actThemeDark:
		cfg.Theme = config.ThemeDark
	case actThemeSystem:
		cfg.Theme = config.ThemeSystem
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
	case actGitHub:
		openExternalURL(w.hwnd, repoURL)
		return
	case actSponsor:
		openExternalURL(w.hwnd, sponsorURL)
		return
	case actLinkedIn:
		openExternalURL(w.hwnd, linkedinURL)
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

func (w *SettingsWindow) startAnimTimer() {
	if w.hwnd == 0 {
		return
	}
	if !w.animTimerOn {
		w.animLastTick = time.Now()
		w.animTimerOn = true
		_, _, _ = procSetTimer.Call(w.hwnd, timerAnimID, animIntervalMs, 0)
	}
}

func (w *SettingsWindow) stopAnimTimer() {
	if w.animTimerOn {
		if w.hwnd != 0 {
			_, _, _ = procKillTimer.Call(w.hwnd, timerAnimID)
		}
		w.animTimerOn = false
	}
}

func targetGaugeState(p speedtest.Progress) (frac float64, val float64) {
	switch p.Phase {
	case speedtest.PhasePing:
		t := float64(p.Percent) / 10.0
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		return 0.06 + t*0.14, p.PingMs
	case speedtest.PhasePause:
		switch p.CompletedPhase {
		case speedtest.PhasePing:
			return 0, p.PingMs
		case speedtest.PhaseDownload:
			return 0, 0
		case speedtest.PhaseUpload:
			return speedToGaugeFraction(p.UploadMbps), p.UploadMbps
		default:
			return 0, 0
		}
	case speedtest.PhaseDownload:
		return speedToGaugeFraction(p.DownloadMbps), p.DownloadMbps
	case speedtest.PhaseUpload:
		return speedToGaugeFraction(p.UploadMbps), p.UploadMbps
	case speedtest.PhaseDone:
		return 0, 0
	default:
		return 0, 0
	}
}

func (w *SettingsWindow) stepAnimation() {
	now := time.Now()
	dt := now.Sub(w.animLastTick).Seconds()
	if dt < 0.004 {
		dt = 0.004
	} else if dt > 0.05 {
		dt = 0.05
	}
	w.animLastTick = now

	w.stMu.Lock()
	p := w.stProgress
	running := w.stRunning
	w.stMu.Unlock()

	targetFrac, targetVal := targetGaugeState(p)
	tau := 0.10
	if p.Phase == speedtest.PhasePause {
		tau = 0.13
	}
	alpha := 1.0 - math.Exp(-dt/tau)
	w.animGaugeFrac += (targetFrac - w.animGaugeFrac) * alpha
	w.animDisplay += (targetVal - w.animDisplay) * alpha
	w.pulsePhase += dt * 4.2

	if !running && math.Abs(targetFrac-w.animGaugeFrac) < 0.002 && math.Abs(targetVal-w.animDisplay) < 0.05 {
		w.animGaugeFrac = targetFrac
		w.animDisplay = targetVal
		w.stopAnimTimer()
	}
	w.invalidate()
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
	w.animGaugeFrac = 0
	w.animDisplay = 0
	w.pulsePhase = 0
	w.startAnimTimer()
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
		w.fontTitle, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(24, dpi), 700)
		w.fontBody, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(15, dpi), 600)
		w.fontSmall, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(14, dpi), 600)
		w.fontCaption, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(15, dpi), 500)
		w.fontCaps, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(13, dpi), 700)
		w.fontGaugeHero, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(52, dpi), 700)
		w.fontGaugeUnit, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(16, dpi), 600)
		w.fontPillMetric, _ = gpCreateFontPx(screenDC, fam, scaleDPIInt(15, dpi), 700)

		pal := getDashboardPalette(isLight)

		logoPx := scaleDPIInt(42, dpi)
		w.logoBmp, w.logoBuf, _ = DecodePNGToBitmap(assets.AppLogoPNG, logoPx, logoPx)
		if w.logoBmp == 0 {
			w.logoBmp, w.logoBuf, _ = RasterizeSVGToBitmap(assets.IconGaugeBold, logoPx, logoPx, pal.brandCyan)
		}

		thPx := scaleDPIInt(18, dpi)
		w.sunBmp, w.sunBuf, _ = RasterizeSVGToBitmap(assets.IconSunBold, thPx, thPx, pal.textMuted)
		w.sunSelBmp, w.sunSelBuf, _ = RasterizeSVGToBitmap(assets.IconSunBold, thPx, thPx, pal.accentText)
		w.moonBmp, w.moonBuf, _ = RasterizeSVGToBitmap(assets.IconMoonBold, thPx, thPx, pal.textMuted)
		w.moonSelBmp, w.moonSelBuf, _ = RasterizeSVGToBitmap(assets.IconMoonBold, thPx, thPx, pal.accentText)
		w.monBmp, w.monBuf, _ = RasterizeSVGToBitmap(assets.IconMonitorBold, thPx, thPx, pal.textMuted)
		w.monSelBmp, w.monSelBuf, _ = RasterizeSVGToBitmap(assets.IconMonitorBold, thPx, thPx, pal.accentText)

		ghPx := scaleDPIInt(18, dpi)
		w.githubBmp, w.githubBuf, _ = RasterizeSVGToBitmap(assets.IconGitHubLogoBold, ghPx, ghPx, pal.textPrimary)
		htPx := scaleDPIInt(17, dpi)
		w.heartBmp, w.heartBuf, _ = RasterizeSVGToBitmap(assets.IconHeartBold, htPx, htPx, pal.heartPink)
		mhPx := scaleDPIInt(19, dpi)
		w.madeHeartBmp, w.madeHeartBuf, _ = RasterizeSVGToGradientBitmap(assets.IconHeartFill, mhPx, mhPx, pal.brandCyan, pal.brandLime)

		smallArrowPx := scaleDPIInt(15, dpi)
		w.dlSmallBmp, w.dlSmallBuf, _ = RasterizeSVGToBitmap(assets.IconArrowDownBold, smallArrowPx, smallArrowPx, pal.brandCyan)
		w.ulSmallBmp, w.ulSmallBuf, _ = RasterizeSVGToBitmap(assets.IconArrowUpBold, smallArrowPx, smallArrowPx, pal.brandLime)

		metricArrowPx := scaleDPIInt(18, dpi)
		w.dlMetricBmp, w.dlMetricBuf, _ = RasterizeSVGToBitmap(assets.IconArrowDownBold, metricArrowPx, metricArrowPx, pal.brandCyan)
		w.ulMetricBmp, w.ulMetricBuf, _ = RasterizeSVGToBitmap(assets.IconArrowUpBold, metricArrowPx, metricArrowPx, pal.brandLime)

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
	isLight := w.isWindowLightTheme()
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
	addArcBeziers(cx, cy, rOuter, startDeg, endDeg)
	endRad := endDeg * math.Pi / 180.0
	addArcBeziers(cx+rMid*math.Cos(endRad), cy+rMid*math.Sin(endRad), rCap, endDeg, endDeg+180.0)
	addArcBeziers(cx, cy, rInner, endDeg, startDeg)
	startRad := startDeg * math.Pi / 180.0
	addArcBeziers(cx+rMid*math.Cos(startRad), cy+rMid*math.Sin(startRad), rCap, startDeg+180.0, startDeg+360.0)
	_, _, _ = procGdipClosePathFigure.Call(gpPath)

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
	fmtTypo, _ := gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	defer func() {
		if fmtNear != 0 {
			_, _, _ = procGdipDeleteStringFormat.Call(fmtNear)
		}
		if fmtCenter != 0 {
			_, _, _ = procGdipDeleteStringFormat.Call(fmtCenter)
		}
		if fmtTypo != 0 {
			_, _, _ = procGdipDeleteStringFormat.Call(fmtTypo)
		}
	}()

	bCard, _ := gpCreateSolidBrush(pal.cardBG)
	bBorder, _ := gpCreateSolidBrush(pal.cardBorder)
	bFaintBorder, _ := gpCreateSolidBrush(pal.faintBorder)
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
		for _, b := range []uintptr{bCard, bBorder, bFaintBorder, bPill, bPillHover, bCyan, bLime, bMint, bText, bMuted, bAccentTxt, bTrack, bTick, bBadge, bSwitchOff, bKnob} {
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

		if c.iconBmp != 0 && c.label != "" {
			// Icon + text capsule (e.g. GitHub, Sponsor)
			iconSz := c.iconSize
			gap := s(8)
			totalIconArea := iconSz + gap
			startX := c.bounds.Left + s(16)
			iconY := c.bounds.Top + (c.bounds.Height()-iconSz)/2
			_, _, _ = procGdipDrawImageRectI.Call(gfx, c.iconBmp, uintptr(startX), uintptr(iconY), uintptr(iconSz), uintptr(iconSz))
			gpDrawString(gfx, w.fontSmall, fmtNear, txtBrush, c.label, gpRectF{
				X: float32(startX + totalIconArea), Y: float32(c.bounds.Top),
				Width: float32(c.bounds.Width() - s(24) - totalIconArea), Height: float32(c.bounds.Height()),
			})
			return
		}

		if c.iconBmp != 0 {
			iconSz := c.iconSize
			ix := c.bounds.Left + (c.bounds.Width()-iconSz)/2
			iy := c.bounds.Top + (c.bounds.Height()-iconSz)/2
			_, _, _ = procGdipDrawImageRectI.Call(gfx, c.iconBmp, uintptr(ix), uintptr(iy), uintptr(iconSz), uintptr(iconSz))
			return
		}

		gpDrawString(gfx, w.fontSmall, fmtCenter, txtBrush, c.label, gpRectF{
			X: float32(c.bounds.Left), Y: float32(c.bounds.Top),
			Width: float32(c.bounds.Width()), Height: float32(c.bounds.Height()),
		})
	}

	marginX := s(18)
	cardW := width - marginX*2

	// 1. Minimal Header (Big Logo + App Name + Live Speed Pill + Light/Dark/System Theme Capsule)
	hdrY := s(14)
	iconSz := s(42)
	if w.logoBmp != 0 {
		_, _, _ = procGdipDrawImageRectI.Call(gfx, w.logoBmp, uintptr(marginX), uintptr(hdrY), uintptr(iconSz), uintptr(iconSz))
	}
	gpDrawString(gfx, w.fontTitle, fmtNear, bText, config.AppName, gpRectF{
		X: float32(marginX + iconSz + s(12)), Y: float32(hdrY), Width: float32(s(190)), Height: float32(iconSz),
	})

	// Theme 3-icon capsule on far right
	themeSegW := s(34)
	themeCapPad := s(4)
	themeCapW := themeSegW*3 + themeCapPad*2
	themeCapH := s(38)
	themeCapX := width - marginX - themeCapW
	themeCapY := hdrY + (iconSz-themeCapH)/2
	gpFillRoundedRect(gfx, bBadge, themeCapX, themeCapY, themeCapW, themeCapH, themeCapH/2)

	thIconSz := s(18)
	themeItems := []struct {
		mode   config.ThemeMode
		action controlAction
		bmp    uintptr
		selBmp uintptr
	}{
		{config.ThemeLight, actThemeLight, w.sunBmp, w.sunSelBmp},
		{config.ThemeDark, actThemeDark, w.moonBmp, w.moonSelBmp},
		{config.ThemeSystem, actThemeSystem, w.monBmp, w.monSelBmp},
	}
	for i, ti := range themeItems {
		sx := themeCapX + themeCapPad + int32(i)*themeSegW
		sy := themeCapY + s(3)
		sh := themeCapH - s(6)
		selected := w.app.cfg.Theme == ti.mode
		bmp := ti.bmp
		if selected {
			bmp = ti.selBmp
		}
		ctrl := uiControl{
			action:   ti.action,
			bounds:   rect{Left: sx, Top: sy, Right: sx + themeSegW, Bottom: sy + sh},
			selected: selected,
			iconBmp:  bmp,
			iconSize: thIconSz,
		}
		w.controls = append(w.controls, ctrl)
		if selected {
			gpFillRoundedRect(gfx, bCyan, sx, sy, themeSegW, sh, sh/2)
		} else if w.hoveredAct == ti.action {
			gpFillRoundedRect(gfx, bPillHover, sx, sy, themeSegW, sh, sh/2)
		}
		if bmp != 0 {
			ix := sx + (themeSegW-thIconSz)/2
			iy := sy + (sh-thIconSz)/2
			_, _, _ = procGdipDrawImageRectI.Call(gfx, bmp, uintptr(ix), uintptr(iy), uintptr(thIconSz), uintptr(thIconSz))
		}
	}

	// Live speed pill immediately left of the theme switcher
	drawCenteredIconText := func(bx, by, bw, bh int32, iconBmp uintptr, iconSz, gap int32, font, brush uintptr, text string) {
		tw, _ := gpMeasureString(gfx, font, fmtTypo, text)
		textW := int32(math.Ceil(float64(tw)))
		totalW := textW
		if iconBmp != 0 {
			totalW += iconSz + gap
		}
		startX := bx + (bw-totalW)/2
		if startX < bx+s(4) {
			startX = bx + s(4)
		}
		if iconBmp != 0 {
			iy := by + (bh-iconSz)/2
			_, _, _ = procGdipDrawImageRectI.Call(gfx, iconBmp, uintptr(startX), uintptr(iy), uintptr(iconSz), uintptr(iconSz))
			startX += iconSz + gap
		}
		gpDrawString(gfx, font, fmtTypo, brush, text, gpRectF{
			X: float32(startX), Y: float32(by), Width: float32(textW + s(4)), Height: float32(bh),
		})
	}

	liveBadgeW := s(228)
	liveBadgeH := s(38)
	liveBadgeX := themeCapX - s(10) - liveBadgeW
	liveBadgeY := themeCapY
	gpFillRoundedRect(gfx, bBadge, liveBadgeX, liveBadgeY, liveBadgeW, liveBadgeH, liveBadgeH/2)

	dlLive := w.app.formatRate(w.app.currentSample.DownloadBytesPerSec).String()
	ulLive := w.app.formatRate(w.app.currentSample.UploadBytesPerSec).String()
	halfBadgeW := (liveBadgeW - s(12)) / 2
	smallArrowSz := s(15)
	drawCenteredIconText(liveBadgeX+s(6), liveBadgeY, halfBadgeW, liveBadgeH, w.dlSmallBmp, smallArrowSz, s(4), w.fontSmall, bCyan, dlLive)
	drawCenteredIconText(liveBadgeX+s(6)+halfBadgeW, liveBadgeY, halfBadgeW, liveBadgeH, w.ulSmallBmp, smallArrowSz, s(4), w.fontSmall, bLime, ulLive)

	// 2. Big Speedometer Gauge Card
	stCardY := s(68)
	stCardH := s(330)
	drawCard(marginX, stCardY, cardW, stCardH, s(16))

	w.stMu.Lock()
	stProg := w.stProgress
	stRunning := w.stRunning
	w.stMu.Unlock()

	gaugeCX := float64(width) / 2.0
	gaugeCY := float64(stCardY + s(118))
	rOuter := float64(s(106))
	rInner := float64(s(89))

	if trackPath, err := buildGaugeArcPath(gaugeCX, gaugeCY, rOuter, rInner, 135.0, 270.0); err == nil && trackPath != 0 {
		_, _, _ = procGdipFillPath.Call(gfx, bTrack, trackPath)
		_, _, _ = procGdipDeletePath.Call(trackPath)
	}

	tickR := rInner - float64(s(10))
	tickSize := s(4)
	for i := 0; i <= 8; i++ {
		ang := (135.0 + float64(i)*270.0/8.0) * math.Pi / 180.0
		tx := int32(math.Round(gaugeCX+tickR*math.Cos(ang))) - tickSize/2
		ty := int32(math.Round(gaugeCY+tickR*math.Sin(ang))) - tickSize/2
		gpFillRoundedRect(gfx, bTick, tx, ty, tickSize, tickSize, tickSize/2)
	}

	rawFrac, rawVal := targetGaugeState(stProg)
	gaugeFrac := rawFrac
	displayVal := rawVal
	if w.animTimerOn {
		gaugeFrac = w.animGaugeFrac
		displayVal = w.animDisplay
	}
	if gaugeFrac < 0 {
		gaugeFrac = 0
	} else if gaugeFrac > 1 {
		gaugeFrac = 1
	}

	phaseLabel := "READY"
	heroValue := "—"
	heroUnit := "Mbps"

	switch stProg.Phase {
	case speedtest.PhasePing:
		phaseLabel = "PING"
		if displayVal > 0 {
			heroValue = fmt.Sprintf("%.0f", displayVal)
		}
		heroUnit = "ms"
	case speedtest.PhasePause:
		switch stProg.CompletedPhase {
		case speedtest.PhasePing:
			phaseLabel = "PING"
			if displayVal > 0 {
				heroValue = fmt.Sprintf("%.0f", displayVal)
			}
			heroUnit = "ms"
		case speedtest.PhaseDownload:
			phaseLabel = "PREPARING"
			heroValue = "—"
			heroUnit = "Mbps"
		case speedtest.PhaseUpload:
			phaseLabel = "UPLOAD"
			heroValue = fmt.Sprintf("%.1f", displayVal)
			heroUnit = "Mbps"
		default:
			phaseLabel = "PREPARING"
		}
	case speedtest.PhaseDownload:
		phaseLabel = "DOWNLOAD"
		heroValue = fmt.Sprintf("%.1f", displayVal)
		heroUnit = "Mbps"
	case speedtest.PhaseUpload:
		phaseLabel = "UPLOAD"
		heroValue = fmt.Sprintf("%.1f", displayVal)
		heroUnit = "Mbps"
	case speedtest.PhaseDone:
		phaseLabel = "COMPLETED"
		heroValue = "—"
		heroUnit = "Mbps"
	case speedtest.PhaseCancelled:
		phaseLabel = "CANCELLED"
	case speedtest.PhaseError:
		phaseLabel = "FAILED"
		heroUnit = ""
	}

	if gaugeFrac > 0.002 {
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

	gpDrawString(gfx, w.fontCaps, fmtCenter, bMuted, phaseLabel, gpRectF{
		X: float32(gaugeCX) - float32(s(80)), Y: float32(gaugeCY) - float32(s(54)),
		Width: float32(s(160)), Height: float32(s(20)),
	})
	gpDrawString(gfx, w.fontGaugeHero, fmtCenter, bText, heroValue, gpRectF{
		X: float32(gaugeCX) - float32(s(100)), Y: float32(gaugeCY) - float32(s(28)),
		Width: float32(s(200)), Height: float32(s(56)),
	})
	if heroUnit != "" {
		gpDrawString(gfx, w.fontGaugeUnit, fmtCenter, bMuted, heroUnit, gpRectF{
			X: float32(gaugeCX) - float32(s(70)), Y: float32(gaugeCY) + float32(s(30)),
			Width: float32(s(140)), Height: float32(s(22)),
		})
	}

	metricY := stCardY + s(220)
	metricH := s(38)
	metricGap := s(10)
	metricPad := s(20)
	metricW := (cardW - metricPad*2 - metricGap*2) / 3

	pingTxt := "Ping  —"
	if stProg.PingMs > 0 {
		pingTxt = fmt.Sprintf("Ping  %.0f ms", stProg.PingMs)
	}
	dlTxt := "—"
	if stProg.DownloadMbps > 0 || stProg.Phase == speedtest.PhaseDone || stProg.Phase == speedtest.PhaseUpload || (stProg.Phase == speedtest.PhasePause && stProg.CompletedPhase != speedtest.PhasePing) {
		dlTxt = fmt.Sprintf("%.1f Mbps", stProg.DownloadMbps)
	}
	ulTxt := "—"
	if stProg.UploadMbps > 0 || stProg.Phase == speedtest.PhaseDone {
		ulTxt = fmt.Sprintf("%.1f Mbps", stProg.UploadMbps)
	}

	metricArrowSz := s(18)
	metricPills := []struct {
		text    string
		brush   uintptr
		iconBmp uintptr
	}{
		{pingTxt, bMint, 0},
		{dlTxt, bCyan, w.dlMetricBmp},
		{ulTxt, bLime, w.ulMetricBmp},
	}
	for i, mp := range metricPills {
		px := marginX + metricPad + int32(i)*(metricW+metricGap)
		gpFillRoundedRect(gfx, bPill, px, metricY, metricW, metricH, metricH/2)
		if mp.iconBmp != 0 {
			drawCenteredIconText(px, metricY, metricW, metricH, mp.iconBmp, metricArrowSz, s(6), w.fontPillMetric, mp.brush, mp.text)
		} else {
			gpDrawString(gfx, w.fontPillMetric, fmtCenter, mp.brush, mp.text, gpRectF{
				X: float32(px + s(4)), Y: float32(metricY), Width: float32(metricW - s(8)), Height: float32(metricH),
			})
		}
	}

	btnW := s(200)
	btnH := s(44)
	btnX := (width - btnW) / 2
	btnY := stCardY + s(270)
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
	appCardY := stCardY + stCardH + s(12)
	appCardH := s(224)
	drawCard(marginX, appCardY, cardW, appCardH, s(16))

	cfg := w.app.cfg
	labelColW := s(68)
	pillStartX := marginX + s(16) + labelColW
	pillAreaW := cardW - s(32) - labelColW
	pillH := s(38)

	drawInlinePillRow := func(y int32, title string, items []uiControl) {
		gpDrawString(gfx, w.fontSmall, fmtNear, bMuted, title, gpRectF{
			X: float32(marginX + s(16)), Y: float32(y), Width: float32(labelColW - s(4)), Height: float32(pillH),
		})
		gap := s(8)
		n := int32(len(items))
		pw := (pillAreaW - gap*(n-1)) / n
		for i := range items {
			px := pillStartX + int32(i)*(pw+gap)
			items[i].bounds = rect{Left: px, Top: y, Right: px + pw, Bottom: y + pillH}
			w.controls = append(w.controls, items[i])
			drawPill(items[i])
		}
	}

	rowStep := s(50)
	row0Y := appCardY + s(18)
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

	// 4. Clean Footer Row (Start with Windows + GitHub + Sponsor, faint divider, and Made with <heart> by Shubham Bhamare)
	footY := appCardY + appCardH + s(12)
	footTopH := s(56)
	footBotH := s(38)
	footH := footTopH + footBotH
	drawCard(marginX, footY, cardW, footH, s(16))

	autoStart, _ := config.IsAutoStartEnabled()
	swW := s(48)
	swH := s(26)
	swX := marginX + s(16)
	swY := footY + (footTopH-swH)/2

	w.controls = append(w.controls, uiControl{
		action:   actAutoStart,
		bounds:   rect{Left: swX, Top: swY, Right: swX + s(220), Bottom: swY + swH},
		isSwitch: true,
		selected: autoStart,
	})

	trackBrush := bSwitchOff
	knobX := swX + s(3)
	if autoStart {
		trackBrush = bCyan
		knobX = swX + swW - s(23)
	}
	gpFillRoundedRect(gfx, trackBrush, swX, swY, swW, swH, swH/2)
	gpFillRoundedRect(gfx, bKnob, knobX, swY+s(3), s(20), s(20), s(10))
	gpDrawString(gfx, w.fontSmall, fmtNear, bText, "Start with Windows", gpRectF{
		X: float32(swX + swW + s(12)), Y: float32(footY), Width: float32(s(175)), Height: float32(footTopH),
	})

	pillFootH := s(38)
	pillFootY := footY + (footTopH-pillFootH)/2

	spW := s(128)
	spX := marginX + cardW - s(14) - spW
	spCtrl := uiControl{
		action:   actSponsor,
		bounds:   rect{Left: spX, Top: pillFootY, Right: spX + spW, Bottom: pillFootY + pillFootH},
		label:    "Sponsor",
		iconBmp:  w.heartBmp,
		iconSize: s(17),
	}
	w.controls = append(w.controls, spCtrl)
	drawPill(spCtrl)

	ghW := s(116)
	ghX := spX - s(8) - ghW
	ghCtrl := uiControl{
		action:   actGitHub,
		bounds:   rect{Left: ghX, Top: pillFootY, Right: ghX + ghW, Bottom: pillFootY + pillFootH},
		label:    "GitHub",
		iconBmp:  w.githubBmp,
		iconSize: s(18),
	}
	w.controls = append(w.controls, ghCtrl)
	drawPill(ghCtrl)

	// Faint footer divider and centered "Made with [gradient heart] by Shubham Bhamare" line
	divX := marginX + s(16)
	divY := footY + footTopH
	divW := cardW - s(32)
	gpFillRoundedRect(gfx, bFaintBorder, divX, divY, divW, 1, 0)

	partMade := "Made with"
	partBy := "by"
	partAuthor := "Shubham Bhamare"
	wMadeF, _ := gpMeasureString(gfx, w.fontCaption, fmtTypo, partMade)
	wByF, _ := gpMeasureString(gfx, w.fontCaption, fmtTypo, partBy)
	wAuthF, _ := gpMeasureString(gfx, w.fontCaption, fmtTypo, partAuthor)

	wMade := int32(math.Ceil(float64(wMadeF)))
	wBy := int32(math.Ceil(float64(wByF)))
	wAuth := int32(math.Ceil(float64(wAuthF)))
	mhSz := s(19)
	spGap := s(6)

	totalCreditW := wMade + spGap + mhSz + spGap + wBy + spGap + wAuth
	curX := (width - totalCreditW) / 2
	creditY := divY + 1
	creditH := footBotH - 1

	gpDrawString(gfx, w.fontCaption, fmtTypo, bMuted, partMade, gpRectF{
		X: float32(curX), Y: float32(creditY), Width: float32(wMade + s(4)), Height: float32(creditH),
	})
	curX += wMade + spGap

	if w.madeHeartBmp != 0 {
		hy := creditY + (creditH-mhSz)/2
		_, _, _ = procGdipDrawImageRectI.Call(gfx, w.madeHeartBmp, uintptr(curX), uintptr(hy), uintptr(mhSz), uintptr(mhSz))
	}
	curX += mhSz + spGap

	gpDrawString(gfx, w.fontCaption, fmtTypo, bMuted, partBy, gpRectF{
		X: float32(curX), Y: float32(creditY), Width: float32(wBy + s(4)), Height: float32(creditH),
	})
	curX += wBy + spGap

	linkBounds := rect{Left: curX, Top: creditY, Right: curX + wAuth + s(2), Bottom: creditY + creditH}
	w.controls = append(w.controls, uiControl{
		action: actLinkedIn,
		bounds: linkBounds,
		label:  partAuthor,
	})
	gpDrawString(gfx, w.fontCaption, fmtTypo, bCyan, partAuthor, gpRectF{
		X: float32(curX), Y: float32(creditY), Width: float32(wAuth + s(4)), Height: float32(creditH),
	})
	if w.hoveredAct == actLinkedIn {
		ulY := creditY + creditH - s(9)
		gpFillRoundedRect(gfx, bCyan, curX, ulY, wAuth, 1, 0)
	} else {
		ulY := creditY + creditH - s(9)
		gpFillRoundedRect(gfx, bFaintBorder, curX, ulY, wAuth, 1, 0)
	}
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
		&w.fontTitle, &w.fontBody, &w.fontSmall, &w.fontCaption, &w.fontCaps,
		&w.fontGaugeHero, &w.fontGaugeUnit, &w.fontPillMetric,
	} {
		if *f != 0 {
			_, _, _ = procGdipDeleteFont.Call(*f)
			*f = 0
		}
	}
	for _, b := range []*uintptr{
		&w.logoBmp, &w.sunBmp, &w.sunSelBmp,
		&w.moonBmp, &w.moonSelBmp, &w.monBmp, &w.monSelBmp,
		&w.githubBmp, &w.heartBmp, &w.madeHeartBmp,
		&w.dlSmallBmp, &w.ulSmallBmp, &w.dlMetricBmp, &w.ulMetricBmp,
	} {
		if *b != 0 {
			_, _, _ = procGdipDisposeImage.Call(*b)
			*b = 0
		}
	}
	w.logoBuf = nil
	w.sunBuf, w.sunSelBuf = nil, nil
	w.moonBuf, w.moonSelBuf = nil, nil
	w.monBuf, w.monSelBuf = nil, nil
	w.githubBuf, w.heartBuf, w.madeHeartBuf = nil, nil, nil
	w.dlSmallBuf, w.ulSmallBuf = nil, nil
	w.dlMetricBuf, w.ulMetricBuf = nil, nil
}

func (w *SettingsWindow) releaseDrawResources() {
	w.releaseDIB()
	w.releaseFontAndIconResources()
}
