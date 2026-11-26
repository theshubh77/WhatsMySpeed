package ui

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/network"
	"golang.org/x/sys/windows"
)

const (
	hostClassName    = "WhatsMySpeedHostWindow"
	overlayClassName = "WhatsMySpeedOverlayWindow"
	timerSampleID    = 1
	timerZOrderID    = 2
	timerReassertID  = 3
	sampleIntervalMs  = 1000
	zOrderIntervalMs  = 200
	reassertDelayMs   = 50
	overlayFontWeight = 700
)

type layoutMetrics struct {
	width      int32
	height     int32
	padX       int32
	indicatorW int32
	iconSize   int32
	iconGap    int32
	valueWidth int32
	unitGap    int32
	unitWidth  int32
	itemWidth  int32
	inlineGap  int32
	rowHeight  int32
	rowGap     int32
	cornerRad  int32
	fontPx     int32
}

// App manages the Win32 layered taskbar overlay, system tray icon, settings window, and 1-second refresh loop.
type App struct {
	cfgPath     string
	cfg         config.Config
	monitor     *network.Monitor
	openOnStart bool

	gdipToken         uintptr
	fontFamily        string
	hostHWND          uintptr
	hwnd              uintptr
	attachedParent    uintptr
	userClosing       bool
	wndProcCB         uintptr
	wmTaskbarCreated  uint32
	tray              *TrayManager
	settings          *SettingsWindow
	currentSample     network.SpeedSample
	isLightTheme      bool
	currentDPI        uint32
	lastTaskbarHeight int32
	isHovered         bool
	trackingLeave     bool
	isVisible         bool
	lastX             int32
	lastY             int32
	lastPaintX        int32
	lastPaintY        int32
	lastW             int32
	lastH             int32
	hasPos            bool

	winEventCB     uintptr
	foregroundHook uintptr
	explorerHook   uintptr
	explorerPID    uint32
	taskbarHWND    uintptr
	syncPending    atomic.Bool

	metrics layoutMetrics

	// Cached GDI / GDI+ drawing resources (rebuilt only on DPI/theme/mode/layout change).
	memDC      uintptr
	hBitmap    uintptr
	oldBitmap  uintptr
	pBits      unsafe.Pointer
	gpBitmap   uintptr
	dibW       int32
	dibH       int32
	gpFont     uintptr
	fmtFar     uintptr
	fmtNear    uintptr
	fmtCenter  uintptr
	textBrush  uintptr
	hoverBrush uintptr
	upBitmap   uintptr
	upBuf      []byte
	downBitmap uintptr
	downBuf    []byte
}

var (
	activeAppMu sync.Mutex
	activeApp   *App
)

// NewApp constructs the UI application with the provided configuration and network monitor.
func NewApp(cfg config.Config, cfgPath string, monitor *network.Monitor, openOnStart bool) *App {
	return &App{
		cfg:         cfg,
		cfgPath:     cfgPath,
		monitor:     monitor,
		openOnStart: openOnStart,
	}
}

// Run initializes Win32/GDI+, docks the overlay to the Windows 11 taskbar,
// and runs the message loop until Exit is selected.
func (a *App) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	EnableDPIAwareness()

	token, err := StartupGDIPlus()
	if err != nil {
		return err
	}
	a.gdipToken = token
	defer ShutdownGDIPlus(a.gdipToken)

	a.fontFamily = ResolveTaskbarFontName()

	msgTaskbar, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
	a.wmTaskbarCreated = uint32(msgTaskbar)

	activeAppMu.Lock()
	activeApp = a
	activeAppMu.Unlock()
	defer func() {
		activeAppMu.Lock()
		if activeApp == a {
			activeApp = nil
		}
		activeAppMu.Unlock()
	}()

	if err := a.createHostWindow(); err != nil {
		return err
	}
	if err := a.createWindow(); err != nil {
		_, _, _ = procDestroyWindow.Call(a.hostHWND)
		return err
	}
	defer a.cleanupResources()

	a.tray = NewTrayManager(a.hostHWND)
	defer a.tray.Remove()

	a.settings = NewSettingsWindow(a)
	defer a.settings.Destroy()

	a.refreshEnvironment(true)

	// Take initial baseline reading immediately.
	if s, err := a.monitor.Sample(); err == nil {
		a.currentSample = s
	}
	a.renderAndPosition()
	if a.openOnStart {
		a.OpenSettingsWindow(false)
	}

	a.ensureWinEventHooks(a.taskbarHWND)
	defer a.uninstallWinEventHooks()

	_, _, _ = procSetTimer.Call(a.hostHWND, timerSampleID, sampleIntervalMs, 0)
	defer procKillTimer.Call(a.hostHWND, timerSampleID)
	_, _, _ = procSetTimer.Call(a.hostHWND, timerZOrderID, zOrderIntervalMs, 0)
	defer procKillTimer.Call(a.hostHWND, timerZOrderID)
	defer procKillTimer.Call(a.hostHWND, timerReassertID)

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func (a *App) ensureWndProcCallback() uintptr {
	if a.wndProcCB == 0 {
		a.wndProcCB = windows.NewCallback(wndProc)
	}
	return a.wndProcCB
}

func (a *App) createHostWindow() error {
	hInst := getModuleHandle()
	clsName, err := windows.UTF16PtrFromString(hostClassName)
	if err != nil {
		return err
	}
	winTitle, err := windows.UTF16PtrFromString(config.AppName + "Host")
	if err != nil {
		return err
	}

	wcx := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   a.ensureWndProcCallback(),
		HInstance:     hInst,
		LpszClassName: clsName,
	}
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))

	hwnd, _, callErr := procCreateWindowExW.Call(
		uintptr(wsExToolWindow|wsExNoActivate),
		uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(winTitle)),
		uintptr(wsPopup),
		0, 0, 0, 0,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW host failed: %w", callErr)
	}
	a.hostHWND = hwnd
	return nil
}

func (a *App) createWindow() error {
	hInst := getModuleHandle()
	clsName, err := windows.UTF16PtrFromString(overlayClassName)
	if err != nil {
		return err
	}
	winTitle, err := windows.UTF16PtrFromString(config.AppName)
	if err != nil {
		return err
	}

	hCursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wcx := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		Style:         csDblClks,
		LpfnWndProc:   a.ensureWndProcCallback(),
		HInstance:     hInst,
		HCursor:       hCursor,
		LpszClassName: clsName,
	}
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))

	exStyle := uintptr(wsExToolWindow | wsExTopmost | wsExLayered | wsExNoActivate)
	style := uintptr(wsPopup)

	hwnd, _, callErr := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(winTitle)),
		style,
		0, 0, 120, 40,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW failed: %w", callErr)
	}
	a.hwnd = hwnd
	a.attachedParent = 0
	return nil
}

func (a *App) ensureOverlayWindow() bool {
	if a.hwnd != 0 {
		return true
	}
	if err := a.createWindow(); err != nil {
		return false
	}
	return a.hwnd != 0
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	activeAppMu.Lock()
	app := activeApp
	activeAppMu.Unlock()

	if app == nil || (app.hwnd != hwnd && app.hostHWND != hwnd) {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}
	return app.handleMessage(hwnd, message, wParam, lParam)
}

func (a *App) handleMessage(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if a.wmTaskbarCreated != 0 && message == a.wmTaskbarCreated {
		a.refreshEnvironment(true)
		a.renderAndPosition()
		return 0
	}

	switch message {
	case wmTimer:
		switch wParam {
		case timerSampleID:
			a.onSampleTick()
		case timerZOrderID:
			a.syncPositionAndZOrder()
		case timerReassertID:
			_, _, _ = procKillTimer.Call(a.hostHWND, timerReassertID)
			a.syncPositionAndZOrder()
		}
		return 0

	case wmSyncOverlay:
		a.syncPending.Store(false)
		a.syncPositionAndZOrder()
		_, _, _ = procSetTimer.Call(a.hostHWND, timerReassertID, reassertDelayMs, 0)
		return 0

	case wmShowDashboard:
		a.OpenSettingsWindow(false)
		return 0

	case wmMouseMove:
		if !a.isHovered {
			a.isHovered = true
			a.paintLayered()
		}
		if !a.trackingLeave && a.hwnd != 0 {
			tme := trackMouseEventStruct{
				CbSize:    uint32(unsafe.Sizeof(trackMouseEventStruct{})),
				DwFlags:   tmeLeave,
				HwndTrack: a.hwnd,
			}
			ok, _, _ := procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			a.trackingLeave = ok != 0
		}
		return 0

	case wmMouseLeave:
		a.isHovered = false
		a.trackingLeave = false
		a.paintLayered()
		return 0

	case wmLButtonDblClk:
		a.OpenSettingsWindow(false)
		return 0

	case wmRButtonUp, wmContextMenu:
		a.openContextMenu()
		return 0

	case wmTrayCallback:
		ev := uint32(loWord(lParam))
		if ev == wmRButtonUp || ev == wmContextMenu {
			a.openContextMenu()
		} else if ev == wmLButtonUp || ev == wmLButtonDblClk {
			a.OpenSettingsWindow(false)
		}
		return 0

	case wmSettingChange, wmDisplayChange, wmDpiChanged:
		a.refreshEnvironment(false)
		a.renderAndPosition()
		if a.settings != nil {
			a.settings.RefreshAppearance()
		}
		return 0

	case wmClose:
		a.userClosing = true
		if a.settings != nil {
			a.settings.CancelSpeedTest()
		}
		if a.hwnd != 0 {
			_, _, _ = procDestroyWindow.Call(a.hwnd)
		}
		if a.hostHWND != 0 {
			_, _, _ = procDestroyWindow.Call(a.hostHWND)
		}
		return 0

	case wmDestroy:
		if hwnd == a.hwnd {
			a.hwnd = 0
			a.attachedParent = 0
			a.isVisible = false
			a.hasPos = false
			a.trackingLeave = false
			if a.userClosing && a.hostHWND == 0 {
				_, _, _ = procPostQuitMessage.Call(0)
			}
			return 0
		}
		if hwnd == a.hostHWND {
			a.hostHWND = 0
			_, _, _ = procPostQuitMessage.Call(0)
			return 0
		}
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return ret
}

func (a *App) onSampleTick() {
	if s, err := a.monitor.Sample(); err == nil {
		a.currentSample = s
	}

	a.refreshEnvironment(false)
	a.updateTrayTooltip()
	a.renderAndPosition()

	if a.settings != nil {
		a.settings.NotifySampleUpdated()
	}
}

func (a *App) formatRate(bytesPerSec float64) network.FormattedSpeed {
	if a.cfg.SpeedUnit == config.UnitBits {
		return network.FormatSpeedBits(bytesPerSec)
	}
	return network.FormatSpeed(bytesPerSec)
}

func (a *App) updateTrayTooltip() {
	if a.tray == nil {
		return
	}
	down := a.formatRate(a.currentSample.DownloadBytesPerSec)
	up := a.formatRate(a.currentSample.UploadBytesPerSec)
	tip := fmt.Sprintf("%s\n↓ %s   ↑ %s", config.AppName, down.String(), up.String())
	a.tray.UpdateTooltip(tip)
}

func (a *App) refreshEnvironment(forceTrayReadd bool) {
	info := QueryTaskbarInfo()
	dpi := info.DPI
	if dpi == 0 {
		dpi = getSystemDPI()
	}

	isLight := DetectTaskbarLightTheme()
	var tbHeight int32
	if info.Valid && !info.TaskbarRect.Empty() {
		tbHeight = info.TaskbarRect.Height()
	}

	needRebuild := a.gpFont == 0 ||
		a.currentDPI != dpi ||
		a.isLightTheme != isLight ||
		a.lastTaskbarHeight != tbHeight

	a.currentDPI = dpi
	a.isLightTheme = isLight
	a.lastTaskbarHeight = tbHeight

	if needRebuild {
		a.rebuildDrawingResources(dpi, isLight, tbHeight)
	}
	if a.tray != nil {
		_ = a.tray.SyncIcon(dpi, isLight, forceTrayReadd)
	}
}

func (a *App) rebuildDrawingResources(dpi uint32, isLight bool, taskbarHeight int32) {
	a.releaseRenderResources()

	a.metrics = a.computeMetrics(dpi, taskbarHeight)
	colors := ColorsForTheme(isLight)

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}

	if font, err := gpCreateFontPx(screenDC, a.fontFamily, a.metrics.fontPx, overlayFontWeight); err == nil {
		a.gpFont = font
	} else if font, err := gpCreateFontPx(screenDC, "Segoe UI", a.metrics.fontPx, overlayFontWeight); err == nil {
		a.gpFont = font
	}

	a.fmtFar, _ = gpCreateStringFormat(gpStringAlignmentFar, gpStringAlignmentCenter)
	a.fmtNear, _ = gpCreateStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	a.fmtCenter, _ = gpCreateStringFormat(gpStringAlignmentCenter, gpStringAlignmentCenter)
	a.textBrush, _ = gpCreateSolidBrush(colors.TextARGB)
	a.hoverBrush, _ = gpCreateSolidBrush(colors.HoverFillARGB)

	a.upBitmap, a.upBuf, _ = RasterizeSVGToBitmap(
		assets.IconArrowUpBold,
		a.metrics.iconSize,
		a.metrics.iconSize,
		colors.IconARGB,
	)
	a.downBitmap, a.downBuf, _ = RasterizeSVGToBitmap(
		assets.IconArrowDownBold,
		a.metrics.iconSize,
		a.metrics.iconSize,
		colors.IconARGB,
	)

	a.ensureDIB(a.metrics.width, a.metrics.height)
}

func (a *App) computeMetrics(dpi uint32, taskbarHeight int32) layoutMetrics {
	isCompact := a.cfg.Layout == config.LayoutCompact
	isInline := a.cfg.Layout == config.LayoutInline && a.cfg.DisplayMode == config.ModeBoth

	m := layoutMetrics{
		padX:      scaleDPIInt(8, dpi),
		iconGap:   scaleDPIInt(4, dpi),
		unitGap:   scaleDPIInt(3, dpi),
		inlineGap: scaleDPIInt(10, dpi),
		cornerRad: scaleDPIInt(5, dpi),
	}
	if isCompact {
		m.padX = scaleDPIInt(6, dpi)
		m.iconGap = scaleDPIInt(3, dpi)
		m.unitGap = scaleDPIInt(2, dpi)
	}

	switch {
	case a.cfg.DisplayMode != config.ModeBoth || isInline:
		m.fontPx = scaleDPIInt(16, dpi)
		m.iconSize = scaleDPIInt(17, dpi)
		m.rowHeight = scaleDPIInt(26, dpi)
		m.rowGap = 0
		m.height = scaleDPIInt(38, dpi)
		m.iconGap = scaleDPIInt(5, dpi)
		m.unitGap = scaleDPIInt(4, dpi)
		m.inlineGap = scaleDPIInt(12, dpi)
		if isCompact {
			m.fontPx = scaleDPIInt(14, dpi)
			m.iconSize = scaleDPIInt(15, dpi)
			m.rowHeight = scaleDPIInt(22, dpi)
			m.height = scaleDPIInt(34, dpi)
			m.iconGap = scaleDPIInt(3, dpi)
			m.unitGap = scaleDPIInt(2, dpi)
		}
	case isCompact:
		m.fontPx = scaleDPIInt(11, dpi)
		m.iconSize = scaleDPIInt(12, dpi)
		m.rowHeight = scaleDPIInt(13, dpi)
		m.rowGap = 0
		m.height = m.rowHeight*2 + scaleDPIInt(6, dpi)
	default:
		m.fontPx = scaleDPIInt(12, dpi)
		m.iconSize = scaleDPIInt(13, dpi)
		m.rowHeight = scaleDPIInt(15, dpi)
		m.rowGap = scaleDPIInt(1, dpi)
		m.height = m.rowHeight*2 + m.rowGap + scaleDPIInt(8, dpi)
	}

	if taskbarHeight > 0 {
		maxH := taskbarHeight - scaleDPIInt(6, dpi)
		if maxH > 16 && m.height > maxH {
			m.height = maxH
		}
	}
	if a.cfg.DisplayMode != config.ModeBoth || isInline {
		if m.rowHeight > m.height {
			m.rowHeight = m.height
		}
		if maxIcon := m.height - scaleDPIInt(4, dpi); maxIcon > 0 && m.iconSize > maxIcon {
			m.iconSize = maxIcon
		}
	}

	m.indicatorW, m.valueWidth, m.unitWidth = a.measureColumns(dpi, m.fontPx, m.iconSize)
	m.itemWidth = m.indicatorW + m.iconGap + m.valueWidth + m.unitGap + m.unitWidth

	if isInline {
		m.width = m.padX + m.itemWidth + m.inlineGap + m.itemWidth + m.padX
	} else {
		m.width = m.padX + m.itemWidth + m.padX
	}
	return m
}

func (a *App) measureColumns(dpi uint32, fontPx int32, iconSize int32) (int32, int32, int32) {
	fallbackInd := iconSize
	fallbackVal := scaleDPIInt(33, dpi)
	fallbackUnit := scaleDPIInt(30, dpi)

	buf := make([]byte, 16)
	var bmp uintptr
	st, _, _ := procGdipCreateBitmapFromScan0.Call(2, 2, 8, gpPixelFormat32bppPARGB, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmp)))
	if st != 0 || bmp == 0 {
		return fallbackInd, fallbackVal, fallbackUnit
	}
	defer procGdipDisposeImage.Call(bmp)

	var gfx uintptr
	st, _, _ = procGdipGetImageGraphicsContext.Call(bmp, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		return fallbackInd, fallbackVal, fallbackUnit
	}
	defer procGdipDeleteGraphics.Call(gfx)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}
	font, err := gpCreateFontPx(screenDC, a.fontFamily, fontPx, overlayFontWeight)
	if err != nil {
		return fallbackInd, fallbackVal, fallbackUnit
	}
	defer procGdipDeleteFont.Call(font)

	strFmt, err := gpCreateStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if err != nil {
		return fallbackInd, fallbackVal, fallbackUnit
	}
	defer procGdipDeleteStringFormat.Call(strFmt)

	indW := iconSize
	switch a.cfg.LabelStyle {
	case config.LabelUD:
		var maxW float32
		for _, s := range []string{"U", "D"} {
			w, _ := gpMeasureString(gfx, font, strFmt, s)
			if w > maxW {
				maxW = w
			}
		}
		indW = int32(math.Ceil(float64(maxW))) + scaleDPIInt(2, dpi)
	case config.LabelWords:
		var maxW float32
		for _, s := range []string{"Up", "Down"} {
			w, _ := gpMeasureString(gfx, font, strFmt, s)
			if w > maxW {
				maxW = w
			}
		}
		indW = int32(math.Ceil(float64(maxW))) + scaleDPIInt(2, dpi)
	}

	var maxValW float32
	for _, sample := range []string{"999.9", "1023", "88.8", "8.88"} {
		w, _ := gpMeasureString(gfx, font, strFmt, sample)
		if w > maxValW {
			maxValW = w
		}
	}

	unitLabels := []string{"B/s", "KB/s", "MB/s", "GB/s"}
	if a.cfg.SpeedUnit == config.UnitBits {
		unitLabels = []string{"bps", "Kbps", "Mbps", "Gbps"}
	}

	var maxUnitW float32
	for _, unit := range unitLabels {
		w, _ := gpMeasureString(gfx, font, strFmt, unit)
		if w > maxUnitW {
			maxUnitW = w
		}
	}

	valW := int32(math.Ceil(float64(maxValW))) + scaleDPIInt(2, dpi)
	unitW := int32(math.Ceil(float64(maxUnitW))) + scaleDPIInt(2, dpi)
	if valW < fallbackVal {
		valW = fallbackVal
	}
	if unitW < fallbackUnit {
		unitW = fallbackUnit
	}
	return indW, valW, unitW
}

func (a *App) ensureDIB(width, height int32) {
	if a.memDC != 0 && a.dibW == width && a.dibH == height {
		return
	}
	a.releaseDIB()

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
			BiWidth:       width,
			BiHeight:      -height, // top-down DIB
			BiPlanes:      1,
			BiBitCount:    32,
			BiCompression: biRGB,
		},
	}

	var pBits unsafe.Pointer
	hBitmap, _, _ := procCreateDIBSection.Call(
		screenDC,
		uintptr(unsafe.Pointer(&bmi)),
		0,
		uintptr(unsafe.Pointer(&pBits)),
		0,
		0,
	)
	if hBitmap == 0 || pBits == nil {
		_, _, _ = procDeleteDC.Call(memDC)
		return
	}

	oldObj, _, _ := procSelectObject.Call(memDC, hBitmap)

	var gpBitmap uintptr
	st, _, _ := procGdipCreateBitmapFromScan0.Call(
		uintptr(width),
		uintptr(height),
		uintptr(width*4),
		gpPixelFormat32bppPARGB,
		uintptr(pBits),
		uintptr(unsafe.Pointer(&gpBitmap)),
	)
	if st != 0 || gpBitmap == 0 {
		_, _, _ = procSelectObject.Call(memDC, oldObj)
		_, _, _ = procDeleteObject.Call(hBitmap)
		_, _, _ = procDeleteDC.Call(memDC)
		return
	}

	a.memDC = memDC
	a.hBitmap = hBitmap
	a.oldBitmap = oldObj
	a.pBits = pBits
	a.gpBitmap = gpBitmap
	a.dibW = width
	a.dibH = height
}

func (a *App) attachToTaskbar(taskbarHWND uintptr) {
	if a.hwnd == 0 {
		return
	}
	if taskbarHWND == 0 {
		if a.attachedParent != 0 {
			_, _, _ = procSetParent.Call(a.hwnd, 0)
			setWindowStyle(a.hwnd, (getWindowStyle(a.hwnd)&^wsChild)|wsPopup)
			setWindowExStyle(a.hwnd, getWindowExStyle(a.hwnd)|wsExTopmost)
			_, _, _ = procSetWindowPos.Call(
				a.hwnd,
				hwndTopmost,
				0, 0, 0, 0,
				swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged,
			)
			a.attachedParent = 0
			a.hasPos = false
		}
		return
	}
	if a.attachedParent == taskbarHWND && getParentWindow(a.hwnd) == taskbarHWND {
		return
	}

	// Attach as a layered child of Shell_TrayWnd so the overlay inherits the taskbar's
	// own DWM Z-order band across Quick Settings (Battery/Speaker/Wi-Fi) and Alt+Tab.
	setWindowStyle(a.hwnd, (getWindowStyle(a.hwnd)&^wsPopup)|wsChild|wsClipSiblings)
	setWindowExStyle(a.hwnd, getWindowExStyle(a.hwnd)&^wsExTopmost)
	_, _, _ = procSetParent.Call(a.hwnd, taskbarHWND)
	if getParentWindow(a.hwnd) == taskbarHWND {
		_, _, _ = procSetWindowPos.Call(
			a.hwnd,
			hwndTop,
			0, 0, 0, 0,
			swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged,
		)
		a.attachedParent = taskbarHWND
		a.hasPos = false
		return
	}

	// Fallback cleanly to an independent topmost popup if SetParent is denied.
	_, _, _ = procSetParent.Call(a.hwnd, 0)
	setWindowStyle(a.hwnd, (getWindowStyle(a.hwnd)&^wsChild)|wsPopup)
	setWindowExStyle(a.hwnd, getWindowExStyle(a.hwnd)|wsExTopmost)
	_, _, _ = procSetWindowPos.Call(
		a.hwnd,
		hwndTopmost,
		0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged,
	)
	a.attachedParent = 0
	a.hasPos = false
}

func (a *App) renderAndPosition() {
	a.positionOverlay(true)
}

func (a *App) syncPositionAndZOrder() {
	a.positionOverlay(false)
}

func (a *App) positionOverlay(repaint bool) {
	if a.cfg.DisplayMode == config.ModeHide {
		if a.hwnd != 0 && (a.isVisible || isWindowActuallyVisible(a.hwnd)) {
			_, _, _ = procShowWindow.Call(a.hwnd, swHide)
			a.isVisible = false
		}
		return
	}

	if !a.ensureOverlayWindow() {
		return
	}

	info := QueryTaskbarInfo()
	a.ensureWinEventHooks(info.TaskbarHWND)
	a.taskbarHWND = info.TaskbarHWND

	x, y, shouldShow := ComputeOverlayBounds(info, a.metrics.width, a.metrics.height)
	if !shouldShow {
		if a.isVisible || isWindowActuallyVisible(a.hwnd) {
			_, _, _ = procShowWindow.Call(a.hwnd, swHide)
			a.isVisible = false
		}
		return
	}

	a.attachToTaskbar(info.TaskbarHWND)

	paintX, paintY := x, y
	zAfter := hwndTopmost
	aboveTarget := info.TaskbarHWND == 0 || isWindowAbove(a.hwnd, info.TaskbarHWND)
	if a.attachedParent != 0 {
		if cx, cy, ok := screenToClientPoint(a.attachedParent, x, y); ok {
			paintX, paintY = cx, cy
		} else {
			paintX = x - info.TaskbarRect.Left
			paintY = y - info.TaskbarRect.Top
		}
		zAfter = hwndTop
		aboveTarget = getTopChildWindow(a.attachedParent) == a.hwnd
	}

	winVisible := isWindowActuallyVisible(a.hwnd)
	posChanged := !a.hasPos ||
		a.lastX != x ||
		a.lastY != y ||
		a.lastPaintX != paintX ||
		a.lastPaintY != paintY ||
		a.lastW != a.metrics.width ||
		a.lastH != a.metrics.height

	if repaint || !a.isVisible || !winVisible || posChanged {
		a.paintLayeredAt(paintX, paintY)
	}

	if !a.isVisible || !winVisible {
		_, _, _ = procShowWindow.Call(a.hwnd, swShowNoActive)
		a.isVisible = true
		posChanged = true
	}

	if posChanged {
		flags := uintptr(swpNoActivate)
		if !winVisible {
			flags |= swpShowWindow
		}
		_, _, _ = procSetWindowPos.Call(
			a.hwnd,
			zAfter,
			uintptr(paintX),
			uintptr(paintY),
			uintptr(a.metrics.width),
			uintptr(a.metrics.height),
			flags,
		)
	} else if !aboveTarget {
		_, _, _ = procSetWindowPos.Call(
			a.hwnd,
			zAfter,
			0, 0, 0, 0,
			swpNoMove|swpNoSize|swpNoActivate,
		)
	}

	if a.attachedParent == 0 && (posChanged || !aboveTarget) && info.TaskbarHWND != 0 && !isWindowAbove(a.hwnd, info.TaskbarHWND) {
		_, _, _ = procSetWindowPos.Call(
			a.hwnd,
			hwndNoTopmost,
			0, 0, 0, 0,
			swpNoMove|swpNoSize|swpNoActivate,
		)
		_, _, _ = procSetWindowPos.Call(
			a.hwnd,
			hwndTopmost,
			uintptr(x),
			uintptr(y),
			uintptr(a.metrics.width),
			uintptr(a.metrics.height),
			swpNoActivate|swpShowWindow,
		)
	}

	a.lastX = x
	a.lastY = y
	a.lastPaintX = paintX
	a.lastPaintY = paintY
	a.lastW = a.metrics.width
	a.lastH = a.metrics.height
	a.hasPos = true
}

func winEventProc(hWinEventHook, event, hwnd, idObject, idChild, idEventThread, dwmsEventTime uintptr) uintptr {
	activeAppMu.Lock()
	app := activeApp
	activeAppMu.Unlock()
	if app == nil || app.hostHWND == 0 {
		return 0
	}

	ev := uint32(event)
	if ev >= eventObjectReorder {
		if (ev != eventObjectReorder && ev != eventObjectLocationChange) ||
			int32(idObject) != objidWindow || int32(idChild) != 0 {
			return 0
		}
		if ev == eventObjectLocationChange && app.taskbarHWND != 0 && hwnd != app.taskbarHWND {
			return 0
		}
	}

	if app.syncPending.CompareAndSwap(false, true) {
		_, _, _ = procPostMessageW.Call(app.hostHWND, wmSyncOverlay, 0, 0)
	}
	return 0
}

func (a *App) ensureWinEventHooks(taskbarHWND uintptr) {
	if procSetWinEventHook.Find() != nil {
		return
	}
	if a.winEventCB == 0 {
		a.winEventCB = windows.NewCallback(winEventProc)
	}

	flags := uintptr(winEventOutOfContext | winEventSkipOwnProcess)
	if a.foregroundHook == 0 {
		h, _, _ := procSetWinEventHook.Call(
			eventSystemForeground,
			eventSystemCaptureEnd,
			0,
			a.winEventCB,
			0,
			0,
			flags,
		)
		a.foregroundHook = h
	}

	if taskbarHWND != 0 && (a.explorerHook == 0 || taskbarHWND != a.taskbarHWND) {
		pid := getWindowProcessID(taskbarHWND)
		if pid != 0 && (a.explorerHook == 0 || pid != a.explorerPID) {
			if a.explorerHook != 0 {
				_, _, _ = procUnhookWinEvent.Call(a.explorerHook)
				a.explorerHook = 0
			}
			h, _, _ := procSetWinEventHook.Call(
				eventObjectReorder,
				eventObjectLocationChange,
				0,
				a.winEventCB,
				uintptr(pid),
				0,
				flags,
			)
			a.explorerHook = h
			a.explorerPID = pid
		}
	}
}

func (a *App) uninstallWinEventHooks() {
	if a.foregroundHook != 0 {
		_, _, _ = procUnhookWinEvent.Call(a.foregroundHook)
		a.foregroundHook = 0
	}
	if a.explorerHook != 0 {
		_, _, _ = procUnhookWinEvent.Call(a.explorerHook)
		a.explorerHook = 0
	}
	a.explorerPID = 0
}

func (a *App) paintLayered() {
	if a.hwnd == 0 || !a.isVisible || a.cfg.DisplayMode == config.ModeHide {
		return
	}
	a.paintLayeredAt(a.lastPaintX, a.lastPaintY)
}

func (a *App) paintLayeredAt(x, y int32) {
	if a.memDC == 0 || a.gpBitmap == 0 || a.gpFont == 0 {
		return
	}

	var gfx uintptr
	st, _, _ := procGdipGetImageGraphicsContext.Call(a.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		return
	}

	_, _, _ = procGdipSetSmoothingMode.Call(gfx, gpSmoothingModeHighQuality)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)
	// Alpha=1 keeps the entire overlay rect hit-testable for clicks while remaining visually transparent.
	_, _, _ = procGdipGraphicsClear.Call(gfx, 0x01000000)

	if a.isHovered && a.hoverBrush != 0 {
		gpFillRoundedRect(gfx, a.hoverBrush, 0, 0, a.metrics.width, a.metrics.height, a.metrics.cornerRad)
	}

	down := a.formatRate(a.currentSample.DownloadBytesPerSec)
	up := a.formatRate(a.currentSample.UploadBytesPerSec)

	a.drawOverlayContent(gfx, up, down)

	// Deleting Graphics commits all GDI+ drawing into the Scan0 DIB buffer before UpdateLayeredWindow.
	_, _, _ = procGdipDeleteGraphics.Call(gfx)

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return
	}
	defer procReleaseDC.Call(0, screenDC)

	ptDst := point{X: x, Y: y}
	winSz := size{Cx: a.metrics.width, Cy: a.metrics.height}
	ptSrc := point{X: 0, Y: 0}
	blend := blendFunction{
		BlendOp:             acSrcOver,
		SourceConstantAlpha: 255,
		AlphaFormat:         acSrcAlpha,
	}

	_, _, _ = procUpdateLayeredWindow.Call(
		a.hwnd,
		screenDC,
		uintptr(unsafe.Pointer(&ptDst)),
		uintptr(unsafe.Pointer(&winSz)),
		a.memDC,
		uintptr(unsafe.Pointer(&ptSrc)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		ulwAlpha,
	)
}

func (a *App) drawOverlayContent(gfx uintptr, up, down network.FormattedSpeed) {
	switch a.cfg.DisplayMode {
	case config.ModeBoth:
		switch a.cfg.Layout {
		case config.LayoutInline:
			rowY := (a.metrics.height - a.metrics.rowHeight) / 2
			leftX := a.metrics.padX
			rightX := leftX + a.metrics.itemWidth + a.metrics.inlineGap
			a.drawSpeedItem(gfx, leftX, rowY, true, up)
			a.drawSpeedItem(gfx, rightX, rowY, false, down)
		case config.LayoutStackedDown:
			totalRowsH := a.metrics.rowHeight*2 + a.metrics.rowGap
			topY := (a.metrics.height - totalRowsH) / 2
			a.drawSpeedItem(gfx, a.metrics.padX, topY, false, down)
			a.drawSpeedItem(gfx, a.metrics.padX, topY+a.metrics.rowHeight+a.metrics.rowGap, true, up)
		default: // LayoutStackedUp, LayoutCompact
			totalRowsH := a.metrics.rowHeight*2 + a.metrics.rowGap
			topY := (a.metrics.height - totalRowsH) / 2
			a.drawSpeedItem(gfx, a.metrics.padX, topY, true, up)
			a.drawSpeedItem(gfx, a.metrics.padX, topY+a.metrics.rowHeight+a.metrics.rowGap, false, down)
		}
	case config.ModeDownload:
		topY := (a.metrics.height - a.metrics.rowHeight) / 2
		a.drawSpeedItem(gfx, a.metrics.padX, topY, false, down)
	case config.ModeUpload:
		topY := (a.metrics.height - a.metrics.rowHeight) / 2
		a.drawSpeedItem(gfx, a.metrics.padX, topY, true, up)
	}
}

func (a *App) directionLabel(isUpload bool) string {
	switch a.cfg.LabelStyle {
	case config.LabelUD:
		if isUpload {
			return "U"
		}
		return "D"
	case config.LabelWords:
		if isUpload {
			return "Up"
		}
		return "Down"
	default:
		return ""
	}
}

func (a *App) drawSpeedItem(gfx uintptr, startX, rowY int32, isUpload bool, speed network.FormattedSpeed) {
	if a.cfg.LabelStyle == config.LabelArrows {
		bmp := a.downBitmap
		if isUpload {
			bmp = a.upBitmap
		}
		if bmp != 0 {
			iconY := rowY + (a.metrics.rowHeight-a.metrics.iconSize)/2
			_, _, _ = procGdipDrawImageRectI.Call(
				gfx,
				bmp,
				uintptr(startX),
				uintptr(iconY),
				uintptr(a.metrics.iconSize),
				uintptr(a.metrics.iconSize),
			)
		}
	} else if a.textBrush != 0 {
		label := a.directionLabel(isUpload)
		fmtHandle := a.fmtNear
		if a.cfg.LabelStyle == config.LabelUD {
			fmtHandle = a.fmtCenter
		}
		if fmtHandle != 0 {
			gpDrawString(gfx, a.gpFont, fmtHandle, a.textBrush, label, gpRectF{
				X: float32(startX), Y: float32(rowY),
				Width: float32(a.metrics.indicatorW), Height: float32(a.metrics.rowHeight),
			})
		}
	}

	valX := float32(startX + a.metrics.indicatorW + a.metrics.iconGap)
	valW := float32(a.metrics.valueWidth)
	unitX := valX + valW + float32(a.metrics.unitGap)
	unitW := float32(a.metrics.unitWidth)
	rowF := float32(rowY)
	rowHF := float32(a.metrics.rowHeight)

	if a.textBrush != 0 {
		if a.fmtFar != 0 {
			gpDrawString(gfx, a.gpFont, a.fmtFar, a.textBrush, speed.Value, gpRectF{
				X: valX, Y: rowF, Width: valW, Height: rowHF,
			})
		}
		if a.fmtNear != 0 {
			gpDrawString(gfx, a.gpFont, a.fmtNear, a.textBrush, speed.Unit, gpRectF{
				X: unitX, Y: rowF, Width: unitW, Height: rowHF,
			})
		}
	}
}

func (a *App) openContextMenu() {
	if a.tray == nil {
		return
	}
	cmd := a.tray.ShowContextMenu(a.cfg, a.cfg.StartWithWindows)

	next := a.cfg
	switch cmd {
	case cmdOpenWindow:
		a.OpenSettingsWindow(false)
		return
	case cmdRunSpeedTest:
		a.OpenSettingsWindow(true)
		return
	case cmdShowBoth:
		next.DisplayMode = config.ModeBoth
	case cmdDownloadOnly:
		next.DisplayMode = config.ModeDownload
	case cmdUploadOnly:
		next.DisplayMode = config.ModeUpload
	case cmdHideSpeed:
		next.DisplayMode = config.ModeHide
	case cmdUnitBytes:
		next.SpeedUnit = config.UnitBytes
	case cmdUnitBits:
		next.SpeedUnit = config.UnitBits
	case cmdLabelArrows:
		next.LabelStyle = config.LabelArrows
	case cmdLabelUD:
		next.LabelStyle = config.LabelUD
	case cmdLabelWords:
		next.LabelStyle = config.LabelWords
	case cmdLayoutStackU:
		next.Layout = config.LayoutStackedUp
	case cmdLayoutStackD:
		next.Layout = config.LayoutStackedDown
	case cmdLayoutInline:
		next.Layout = config.LayoutInline
	case cmdLayoutComp:
		next.Layout = config.LayoutCompact
	case cmdThemeSystem:
		next.Theme = config.ThemeSystem
	case cmdThemeDark:
		next.Theme = config.ThemeDark
	case cmdThemeLight:
		next.Theme = config.ThemeLight
	case cmdAutoStart:
		next.StartWithWindows = !a.cfg.StartWithWindows
	case cmdExit:
		target := a.hostHWND
		if target == 0 {
			target = a.hwnd
		}
		_, _, _ = procPostMessageW.Call(target, wmClose, 0, 0)
		return
	default:
		return
	}

	a.ApplyConfig(next)
}

// ApplyConfig persists updated user preferences and refreshes the taskbar overlay, tray tooltip, and settings UI.
func (a *App) ApplyConfig(next config.Config) {
	if !config.ValidDisplayMode(next.DisplayMode) {
		next.DisplayMode = a.cfg.DisplayMode
	}
	if !config.ValidSpeedUnit(next.SpeedUnit) {
		next.SpeedUnit = a.cfg.SpeedUnit
	}
	if !config.ValidLabelStyle(next.LabelStyle) {
		next.LabelStyle = a.cfg.LabelStyle
	}
	if !config.ValidLayout(next.Layout) {
		next.Layout = a.cfg.Layout
	}
	if !config.ValidThemeMode(next.Theme) {
		next.Theme = a.cfg.Theme
	}
	if next == a.cfg {
		return
	}

	if next.StartWithWindows != a.cfg.StartWithWindows {
		if err := config.SetAutoStart(next.StartWithWindows); err != nil {
			if actual, qErr := config.IsAutoStartEnabled(); qErr == nil {
				next.StartWithWindows = actual
			} else {
				next.StartWithWindows = a.cfg.StartWithWindows
			}
		}
		if next == a.cfg {
			return
		}
	}

	wasHidden := a.cfg.DisplayMode == config.ModeHide
	a.cfg = next
	if a.cfgPath != "" {
		_ = config.SaveTo(a.cfgPath, a.cfg)
	} else {
		_ = config.Save(a.cfg)
	}

	if wasHidden && a.cfg.DisplayMode != config.ModeHide {
		a.monitor.Reset()
		if s, err := a.monitor.Sample(); err == nil {
			a.currentSample = s
		}
	}

	a.rebuildDrawingResources(a.currentDPI, a.isLightTheme, a.lastTaskbarHeight)
	a.hasPos = false
	a.updateTrayTooltip()
	a.renderAndPosition()

	if a.settings != nil {
		a.settings.RefreshAppearance()
	}
}

// ActivateExistingInstance signals an already-running WhatsMySpeed instance to show and focus its dashboard window.
func ActivateExistingInstance() bool {
	for attempt := 0; attempt < 10; attempt++ {
		hwnd := findWindow(hostClassName)
		if hwnd != 0 {
			if pid := getWindowProcessID(hwnd); pid != 0 {
				_, _, _ = procAllowSetForegroundWindow.Call(uintptr(pid))
			}
			_, _, _ = procPostMessageW.Call(hwnd, wmShowDashboard, 0, 0)
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// OpenSettingsWindow opens and focuses the WhatsMySpeed settings and speedtest window.
func (a *App) OpenSettingsWindow(startSpeedTest bool) {
	if a.settings == nil {
		return
	}
	a.settings.Show(startSpeedTest)
}

func (a *App) releaseDIB() {
	if a.gpBitmap != 0 {
		_, _, _ = procGdipDisposeImage.Call(a.gpBitmap)
		a.gpBitmap = 0
	}
	if a.memDC != 0 {
		if a.oldBitmap != 0 {
			_, _, _ = procSelectObject.Call(a.memDC, a.oldBitmap)
			a.oldBitmap = 0
		}
		_, _, _ = procDeleteDC.Call(a.memDC)
		a.memDC = 0
	}
	if a.hBitmap != 0 {
		_, _, _ = procDeleteObject.Call(a.hBitmap)
		a.hBitmap = 0
	}
	a.pBits = nil
	a.dibW = 0
	a.dibH = 0
}

func (a *App) releaseRenderResources() {
	a.releaseDIB()
	if a.gpFont != 0 {
		_, _, _ = procGdipDeleteFont.Call(a.gpFont)
		a.gpFont = 0
	}
	if a.fmtFar != 0 {
		_, _, _ = procGdipDeleteStringFormat.Call(a.fmtFar)
		a.fmtFar = 0
	}
	if a.fmtNear != 0 {
		_, _, _ = procGdipDeleteStringFormat.Call(a.fmtNear)
		a.fmtNear = 0
	}
	if a.fmtCenter != 0 {
		_, _, _ = procGdipDeleteStringFormat.Call(a.fmtCenter)
		a.fmtCenter = 0
	}
	if a.textBrush != 0 {
		_, _, _ = procGdipDeleteBrush.Call(a.textBrush)
		a.textBrush = 0
	}
	if a.hoverBrush != 0 {
		_, _, _ = procGdipDeleteBrush.Call(a.hoverBrush)
		a.hoverBrush = 0
	}
	if a.upBitmap != 0 {
		_, _, _ = procGdipDisposeImage.Call(a.upBitmap)
		a.upBitmap = 0
	}
	if a.downBitmap != 0 {
		_, _, _ = procGdipDisposeImage.Call(a.downBitmap)
		a.downBitmap = 0
	}
	a.upBuf = nil
	a.downBuf = nil
}

func (a *App) cleanupResources() {
	a.releaseRenderResources()
}
