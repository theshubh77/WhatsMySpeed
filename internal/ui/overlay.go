package ui

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/network"
	"golang.org/x/sys/windows"
)

const (
	overlayClassName = "WhatsMySpeedOverlayWindow"
	timerSampleID    = 1
	sampleIntervalMs = 1000
)

type layoutMetrics struct {
	width       int32
	height      int32
	padX        int32
	iconSize    int32
	iconGap     int32
	valueWidth  int32
	unitGap     int32
	unitWidth   int32
	rowHeight   int32
	rowGap      int32
	cornerRad   int32
	fontPx      int32
}

// App manages the Win32 layered taskbar overlay, system tray icon, and 1-second refresh loop.
type App struct {
	cfgPath string
	cfg     config.Config
	monitor *network.Monitor

	gdipToken         uintptr
	fontFamily        string
	hwnd              uintptr
	wmTaskbarCreated  uint32
	tray              *TrayManager
	currentSample     network.SpeedSample
	isLightTheme      bool
	currentDPI        uint32
	lastTaskbarHeight int32
	isHovered         bool
	trackingLeave     bool
	isVisible         bool
	lastX             int32
	lastY             int32
	lastW             int32
	lastH             int32
	hasPos            bool

	metrics layoutMetrics

	// Cached GDI / GDI+ drawing resources (rebuilt only on DPI/theme/mode change).
	memDC       uintptr
	hBitmap     uintptr
	oldBitmap   uintptr
	pBits       unsafe.Pointer
	gpBitmap    uintptr
	dibW        int32
	dibH        int32
	gpFont      uintptr
	fmtFar      uintptr
	fmtNear     uintptr
	textBrush   uintptr
	hoverBrush  uintptr
	upBitmap    uintptr
	upBuf       []byte
	downBitmap  uintptr
	downBuf     []byte
}

var (
	activeAppMu sync.Mutex
	activeApp   *App
)

// NewApp constructs the UI application with the provided configuration and network monitor.
func NewApp(cfg config.Config, cfgPath string, monitor *network.Monitor) *App {
	return &App{
		cfg:     cfg,
		cfgPath: cfgPath,
		monitor: monitor,
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

	if err := a.createWindow(); err != nil {
		return err
	}
	defer a.cleanupResources()

	a.tray = NewTrayManager(a.hwnd)
	defer a.tray.Remove()

	a.refreshEnvironment(true)

	// Take initial baseline reading immediately.
	if s, err := a.monitor.Sample(); err == nil {
		a.currentSample = s
	}
	a.renderAndPosition()

	_, _, _ = procSetTimer.Call(a.hwnd, timerSampleID, sampleIntervalMs, 0)
	defer procKillTimer.Call(a.hwnd, timerSampleID)

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
		LpfnWndProc:   windows.NewCallback(wndProc),
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
	return nil
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	activeAppMu.Lock()
	app := activeApp
	activeAppMu.Unlock()

	if app == nil || app.hwnd != hwnd {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}
	return app.handleMessage(message, wParam, lParam)
}

func (a *App) handleMessage(message uint32, wParam, lParam uintptr) uintptr {
	if a.wmTaskbarCreated != 0 && message == a.wmTaskbarCreated {
		a.refreshEnvironment(true)
		a.renderAndPosition()
		return 0
	}

	switch message {
	case wmTimer:
		if wParam == timerSampleID {
			a.onSampleTick()
		}
		return 0

	case wmMouseMove:
		if !a.isHovered {
			a.isHovered = true
			a.paintLayered()
		}
		if !a.trackingLeave {
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

	case wmRButtonUp, wmContextMenu:
		a.openContextMenu()
		return 0

	case wmTrayCallback:
		ev := uint32(loWord(lParam))
		if ev == wmRButtonUp || ev == wmContextMenu || ev == wmLButtonUp {
			a.openContextMenu()
		}
		return 0

	case wmSettingChange, wmDisplayChange, wmDpiChanged:
		a.refreshEnvironment(false)
		a.renderAndPosition()
		return 0

	case wmClose:
		_, _, _ = procDestroyWindow.Call(a.hwnd)
		return 0

	case wmDestroy:
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(a.hwnd, uintptr(message), wParam, lParam)
	return ret
}

func (a *App) onSampleTick() {
	if s, err := a.monitor.Sample(); err == nil {
		a.currentSample = s
	}

	// Detect dynamic theme/DPI/taskbar changes cleanly.
	a.refreshEnvironment(false)
	a.updateTrayTooltip()
	a.renderAndPosition()
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

	if font, err := gpCreateFontPx(screenDC, a.fontFamily, a.metrics.fontPx, 500); err == nil {
		a.gpFont = font
	} else if font, err := gpCreateFontPx(screenDC, "Segoe UI", a.metrics.fontPx, 500); err == nil {
		a.gpFont = font
	}

	a.fmtFar, _ = gpCreateStringFormat(gpStringAlignmentFar, gpStringAlignmentCenter)
	a.fmtNear, _ = gpCreateStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
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
	m := layoutMetrics{
		padX:      scaleDPIInt(8, dpi),
		iconGap:   scaleDPIInt(4, dpi),
		unitGap:   scaleDPIInt(3, dpi),
		cornerRad: scaleDPIInt(5, dpi),
	}

	if a.cfg.DisplayMode == config.ModeBoth {
		m.fontPx = scaleDPIInt(12, dpi)
		m.iconSize = scaleDPIInt(10, dpi)
		m.rowHeight = scaleDPIInt(15, dpi)
		m.rowGap = scaleDPIInt(1, dpi)
		m.height = m.rowHeight*2 + m.rowGap + scaleDPIInt(8, dpi)
	} else {
		m.fontPx = scaleDPIInt(13, dpi)
		m.iconSize = scaleDPIInt(11, dpi)
		m.rowHeight = scaleDPIInt(18, dpi)
		m.rowGap = 0
		m.height = scaleDPIInt(38, dpi)
	}

	if taskbarHeight > 0 {
		maxH := taskbarHeight - scaleDPIInt(6, dpi)
		if maxH > 16 && m.height > maxH {
			m.height = maxH
		}
	}

	// Measure max numeric and unit widths so horizontal columns stay rock-steady.
	m.valueWidth, m.unitWidth = a.measureColumns(dpi, m.fontPx)
	m.width = m.padX + m.iconSize + m.iconGap + m.valueWidth + m.unitGap + m.unitWidth + m.padX
	return m
}

func (a *App) measureColumns(dpi uint32, fontPx int32) (int32, int32) {
	fallbackVal := scaleDPIInt(33, dpi)
	fallbackUnit := scaleDPIInt(30, dpi)

	buf := make([]byte, 16)
	var bmp uintptr
	st, _, _ := procGdipCreateBitmapFromScan0.Call(2, 2, 8, gpPixelFormat32bppPARGB, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmp)))
	if st != 0 || bmp == 0 {
		return fallbackVal, fallbackUnit
	}
	defer procGdipDisposeImage.Call(bmp)

	var gfx uintptr
	st, _, _ = procGdipGetImageGraphicsContext.Call(bmp, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		return fallbackVal, fallbackUnit
	}
	defer procGdipDeleteGraphics.Call(gfx)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}
	font, err := gpCreateFontPx(screenDC, a.fontFamily, fontPx, 500)
	if err != nil {
		return fallbackVal, fallbackUnit
	}
	defer procGdipDeleteFont.Call(font)

	strFmt, err := gpCreateStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if err != nil {
		return fallbackVal, fallbackUnit
	}
	defer procGdipDeleteStringFormat.Call(strFmt)

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
	return valW, unitW
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

func (a *App) renderAndPosition() {
	if a.cfg.DisplayMode == config.ModeHide {
		if a.isVisible {
			_, _, _ = procShowWindow.Call(a.hwnd, swHide)
			a.isVisible = false
		}
		return
	}

	info := QueryTaskbarInfo()
	x, y, shouldShow := ComputeOverlayBounds(info, a.metrics.width, a.metrics.height)
	if !shouldShow {
		if a.isVisible {
			_, _, _ = procShowWindow.Call(a.hwnd, swHide)
			a.isVisible = false
		}
		return
	}

	a.paintLayeredAt(x, y)

	needMove := !a.hasPos ||
		a.lastX != x ||
		a.lastY != y ||
		a.lastW != a.metrics.width ||
		a.lastH != a.metrics.height ||
		!isWindowAbove(a.hwnd, info.TaskbarHWND)

	if !a.isVisible {
		_, _, _ = procShowWindow.Call(a.hwnd, swShowNoActive)
		a.isVisible = true
		needMove = true
	}

	if needMove {
		_, _, _ = procSetWindowPos.Call(
			a.hwnd,
			hwndTopmost,
			uintptr(x),
			uintptr(y),
			uintptr(a.metrics.width),
			uintptr(a.metrics.height),
			swpNoActivate,
		)
	}

	a.lastX = x
	a.lastY = y
	a.lastW = a.metrics.width
	a.lastH = a.metrics.height
	a.hasPos = true
}

func (a *App) paintLayered() {
	if !a.isVisible || a.cfg.DisplayMode == config.ModeHide {
		return
	}
	a.paintLayeredAt(a.lastX, a.lastY)
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
	// Alpha=1 keeps the entire overlay rect hit-testable for right-clicks while remaining visually transparent.
	_, _, _ = procGdipGraphicsClear.Call(gfx, 0x01000000)

	if a.isHovered && a.hoverBrush != 0 {
		gpFillRoundedRect(gfx, a.hoverBrush, 0, 0, a.metrics.width, a.metrics.height, a.metrics.cornerRad)
	}

	down := a.formatRate(a.currentSample.DownloadBytesPerSec)
	up := a.formatRate(a.currentSample.UploadBytesPerSec)

	switch a.cfg.DisplayMode {
	case config.ModeBoth:
		totalRowsH := a.metrics.rowHeight*2 + a.metrics.rowGap
		topY := (a.metrics.height - totalRowsH) / 2
		a.drawSpeedRow(gfx, topY, a.upBitmap, up)
		a.drawSpeedRow(gfx, topY+a.metrics.rowHeight+a.metrics.rowGap, a.downBitmap, down)
	case config.ModeDownload:
		topY := (a.metrics.height - a.metrics.rowHeight) / 2
		a.drawSpeedRow(gfx, topY, a.downBitmap, down)
	case config.ModeUpload:
		topY := (a.metrics.height - a.metrics.rowHeight) / 2
		a.drawSpeedRow(gfx, topY, a.upBitmap, up)
	}

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

func (a *App) drawSpeedRow(gfx uintptr, rowY int32, iconBitmap uintptr, speed network.FormattedSpeed) {
	iconX := a.metrics.padX
	iconY := rowY + (a.metrics.rowHeight-a.metrics.iconSize)/2
	if iconBitmap != 0 {
		_, _, _ = procGdipDrawImageRectI.Call(
			gfx,
			iconBitmap,
			uintptr(iconX),
			uintptr(iconY),
			uintptr(a.metrics.iconSize),
			uintptr(a.metrics.iconSize),
		)
	}

	valX := float32(iconX + a.metrics.iconSize + a.metrics.iconGap)
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
	autoStart, _ := config.IsAutoStartEnabled()
	cmd := a.tray.ShowContextMenu(a.cfg.DisplayMode, a.cfg.SpeedUnit, autoStart)

	switch cmd {
	case cmdShowBoth:
		a.setDisplayMode(config.ModeBoth)
	case cmdDownloadOnly:
		a.setDisplayMode(config.ModeDownload)
	case cmdUploadOnly:
		a.setDisplayMode(config.ModeUpload)
	case cmdHideSpeed:
		a.setDisplayMode(config.ModeHide)
	case cmdUnitBytes:
		a.setSpeedUnit(config.UnitBytes)
	case cmdUnitBits:
		a.setSpeedUnit(config.UnitBits)
	case cmdAutoStart:
		_ = config.SetAutoStart(!autoStart)
	case cmdExit:
		_, _, _ = procPostMessageW.Call(a.hwnd, wmClose, 0, 0)
	}
}

func (a *App) setSpeedUnit(unit config.SpeedUnit) {
	if !config.ValidSpeedUnit(unit) || a.cfg.SpeedUnit == unit {
		return
	}
	a.cfg.SpeedUnit = unit
	if a.cfgPath != "" {
		_ = config.SaveTo(a.cfgPath, a.cfg)
	} else {
		_ = config.Save(a.cfg)
	}

	a.rebuildDrawingResources(a.currentDPI, a.isLightTheme, a.lastTaskbarHeight)
	a.hasPos = false
	a.updateTrayTooltip()
	a.renderAndPosition()
}

func (a *App) setDisplayMode(mode config.DisplayMode) {
	if !config.ValidDisplayMode(mode) || a.cfg.DisplayMode == mode {
		return
	}
	wasHidden := a.cfg.DisplayMode == config.ModeHide
	a.cfg.DisplayMode = mode
	if a.cfgPath != "" {
		_ = config.SaveTo(a.cfgPath, a.cfg)
	} else {
		_ = config.Save(a.cfg)
	}

	if wasHidden && mode != config.ModeHide {
		a.monitor.Reset()
		if s, err := a.monitor.Sample(); err == nil {
			a.currentSample = s
		}
	}

	a.rebuildDrawingResources(a.currentDPI, a.isLightTheme, a.lastTaskbarHeight)
	a.hasPos = false
	a.renderAndPosition()
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
