package ui

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"golang.org/x/sys/windows"
)

const (
	nativeTooltipClass  = "tooltips_class32"
	timerTooltipID      = 4
	tooltipHoverDelayMs = 450
)

var (
	initComctlTooltipOnce sync.Once

	tooltipSubclassOnce sync.Once
	tooltipSubclassCB   uintptr
	tooltipSubclassesMu sync.RWMutex
	tooltipSubclasses   = make(map[uintptr]*overlayTooltip)
)

type overlayTooltip struct {
	hwnd               uintptr
	ownerHWND          uintptr
	prevProc           uintptr
	hFont              uintptr
	fontDPI            uint32
	fontName           string
	upIconBmp          uintptr
	upIconBuf          []byte
	downIconBmp        uintptr
	downIconBuf        []byte
	iconDPI            uint32
	iconARGB           uint32
	memDC              uintptr
	memBmp             uintptr
	memOldBmp          uintptr
	memW               int32
	memH               int32
	canCustomDraw      bool
	customBoundsActive bool
	hasTargetBounds    bool
	targetX            int32
	targetY            int32
	targetW            int32
	targetH            int32
	holdDownW          int32
	holdUpW            int32
	visible            bool
	lastText           string
	lastDown           string
	lastUp             string
	lastTheme          config.ThemeMode
	lastHC             bool
	lastX              int32
	lastY              int32
	hasPos             bool
	dpi                uint32
}

func ensureCommonControlsTooltip() {
	initComctlTooltipOnce.Do(func() {
		if procInitCommonControlsEx.Find() != nil {
			return
		}
		icc := initCommonControlsExStruct{
			DwSize: uint32(unsafe.Sizeof(initCommonControlsExStruct{})),
			DwICC:  iccWin95Classes,
		}
		_, _, _ = procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	})
}

func ensureTooltipSubclassCallback() uintptr {
	tooltipSubclassOnce.Do(func() {
		tooltipSubclassCB = windows.NewCallback(tooltipSubclassProc)
	})
	return tooltipSubclassCB
}

func registerTooltipSubclass(tt *overlayTooltip) {
	if tt == nil || tt.hwnd == 0 {
		return
	}
	tooltipSubclassesMu.Lock()
	tooltipSubclasses[tt.hwnd] = tt
	tooltipSubclassesMu.Unlock()
}

func lookupTooltipSubclass(hwnd uintptr) *overlayTooltip {
	tooltipSubclassesMu.RLock()
	tt := tooltipSubclasses[hwnd]
	tooltipSubclassesMu.RUnlock()
	return tt
}

func unregisterTooltipSubclass(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	tooltipSubclassesMu.Lock()
	delete(tooltipSubclasses, hwnd)
	tooltipSubclassesMu.Unlock()
}

func tooltipSubclassProc(hwnd, message, wParam, lParam uintptr) uintptr {
	tt := lookupTooltipSubclass(hwnd)
	if tt == nil || tt.prevProc == 0 {
		ret, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
		return ret
	}
	prev := tt.prevProc

	switch uint32(message) {
	case wmNcDestroy:
		unregisterTooltipSubclass(hwnd)
		tt.prevProc = 0
		res, _, _ := procCallWindowProcW.Call(prev, hwnd, message, wParam, lParam)
		return res

	case wmEraseBkgnd:
		if tt.useCustomPaint() {
			return 1
		}

	case wmWindowPosChanging:
		if tt.useCustomPaint() {
			tt.applyWindowPosOverride(lParam)
		}

	case wmWindowPosChanged:
		res, _, _ := procCallWindowProcW.Call(prev, hwnd, message, wParam, lParam)
		if tt.useCustomPaint() {
			tt.clearStaleWindowRgn()
		}
		return res

	case wmThemeChanged:
		res, _, _ := procCallWindowProcW.Call(prev, hwnd, message, wParam, lParam)
		_, _, hc := resolveNativeTooltipThemeName(tt.lastTheme)
		tt.canCustomDraw = tt.probeCustomDrawSupport(hc)
		if tt.useCustomPaint() {
			tt.clearStaleWindowRgn()
			_, _, _ = procInvalidateRect.Call(hwnd, 0, 0)
		}
		return res

	case wmPaint:
		if tt.useCustomPaint() {
			var ps paintStruct
			hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			if hdc != 0 {
				tt.paintClientDC(hdc)
			}
			_, _, _ = procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			return 0
		}

	case wmPrintClient:
		if tt.useCustomPaint() && wParam != 0 {
			tt.paintClientDC(wParam)
			return 0
		}
	}

	res, _, _ := procCallWindowProcW.Call(prev, hwnd, message, wParam, lParam)
	return res
}

func (tt *overlayTooltip) useCustomPaint() bool {
	return tt != nil && tt.customBoundsActive && tt.canCustomDraw && !tt.lastHC && tt.hwnd != 0
}

func (tt *overlayTooltip) installSubclass() {
	if tt == nil || tt.hwnd == 0 || tt.prevProc != 0 {
		return
	}
	if procSetWindowLongPtrW.Find() != nil || procCallWindowProcW.Find() != nil {
		return
	}
	cb := ensureTooltipSubclassCallback()
	if cb == 0 {
		return
	}
	registerTooltipSubclass(tt)
	prev, _, _ := procSetWindowLongPtrW.Call(tt.hwnd, gwlpWndProc, cb)
	if prev == 0 {
		unregisterTooltipSubclass(tt.hwnd)
		return
	}
	tt.prevProc = prev
}

func (tt *overlayTooltip) uninstallSubclass() {
	if tt == nil || tt.hwnd == 0 {
		return
	}
	unregisterTooltipSubclass(tt.hwnd)
	if tt.prevProc != 0 && procSetWindowLongPtrW.Find() == nil {
		_, _, _ = procSetWindowLongPtrW.Call(tt.hwnd, gwlpWndProc, tt.prevProc)
	}
	tt.prevProc = 0
}

func (tt *overlayTooltip) applyWindowPosOverride(lParam uintptr) bool {
	if tt == nil || !tt.hasTargetBounds || lParam == 0 || procRtlMoveMemory.Find() != nil {
		return false
	}
	var wp windowPos
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&wp)), lParam, unsafe.Sizeof(wp))
	changed := false
	if (wp.Flags&swpNoSize) == 0 && tt.targetW > 0 && tt.targetH > 0 {
		if wp.Cx != tt.targetW || wp.Cy != tt.targetH {
			wp.Cx = tt.targetW
			wp.Cy = tt.targetH
			changed = true
		}
	}
	if (wp.Flags & swpNoMove) == 0 {
		if wp.X != tt.targetX || wp.Y != tt.targetY {
			wp.X = tt.targetX
			wp.Y = tt.targetY
			changed = true
		}
	}
	if changed {
		_, _, _ = procRtlMoveMemory.Call(lParam, uintptr(unsafe.Pointer(&wp)), unsafe.Sizeof(wp))
	}
	return changed
}

func formatNativeOverlayTooltip(downStr, upStr string) string {
	// Pad around arrows slightly so ComCtrl sizes the native bubble generously enough
	// for crisp 16 DIP vector arrow icons in non-custom fallback modes.
	return fmt.Sprintf("%s\n↓   %s     ↑   %s", config.AppName, downStr, upStr)
}

func resolveNativeTooltipThemeName(mode config.ThemeMode) (subApp string, isDark bool, highContrast bool) {
	if active, _, _ := DetectHighContrast(); active {
		return "", false, true
	}
	if resolveThemeIsLight(mode) {
		return "Explorer", false, false
	}
	return "DarkMode_Explorer", true, false
}

func tooltipMetricSizes(dpi uint32) (fontPx, iconSize int32) {
	if dpi == 0 {
		dpi = 96
	}
	return scaleDPIInt(15, dpi), scaleDPIInt(16, dpi)
}

type tooltipLayout struct {
	fontPx  int32
	iconSz  int32
	padX    int32
	padY    int32
	lineGap int32
	iconGap int32
	pairGap int32
	h1      int32
	h2      int32
	appW    int32
	downW   int32
	upW     int32
	reqW    int32
	reqH    int32
}

func tooltipRateSample(rateStr string) string {
	idx := strings.LastIndexByte(rateStr, ' ')
	if idx < 0 || idx+1 >= len(rateStr) {
		return rateStr
	}
	unit := rateStr[idx+1:]
	if unit == "B/s" {
		unit = "KB/s"
	} else if unit == "bps" {
		unit = "Kbps"
	}
	return "88.8 " + unit
}

func (l *tooltipLayout) applyMinRateWidths(minDownW, minUpW int32) {
	if minDownW > l.downW {
		l.downW = minDownW
	}
	if minUpW > l.upW {
		l.upW = minUpW
	}
	row2W := l.iconSz + l.iconGap + l.downW + l.pairGap + l.iconSz + l.iconGap + l.upW
	innerW := l.appW
	if row2W > innerW {
		innerW = row2W
	}
	l.reqW = l.padX*2 + innerW
}

func computeTooltipLayoutWithFont(gfx, font, strFmt uintptr, dpi uint32, downStr, upStr string) tooltipLayout {
	if dpi == 0 {
		dpi = 96
	}
	fontPx, iconSz := tooltipMetricSizes(dpi)
	padX := scaleDPIInt(14, dpi)
	padY := scaleDPIInt(10, dpi)
	lineGap := scaleDPIInt(5, dpi)
	iconGap := scaleDPIInt(5, dpi)
	pairGap := scaleDPIInt(14, dpi)

	aW, h1F := gpMeasureString(gfx, font, strFmt, config.AppName)
	dW, h2DF := gpMeasureString(gfx, font, strFmt, downStr)
	if s := tooltipRateSample(downStr); s != downStr {
		if sw, _ := gpMeasureString(gfx, font, strFmt, s); sw > dW {
			dW = sw
		}
	}
	uW, h2UF := gpMeasureString(gfx, font, strFmt, upStr)
	if s := tooltipRateSample(upStr); s != upStr {
		if sw, _ := gpMeasureString(gfx, font, strFmt, s); sw > uW {
			uW = sw
		}
	}

	h1 := int32(math.Ceil(float64(h1F)))
	if h1 < fontPx {
		h1 = fontPx
	}
	h2 := int32(math.Ceil(math.Max(float64(h2DF), float64(h2UF))))
	if h2 < iconSz {
		h2 = iconSz
	}

	slack := scaleDPIInt(2, dpi)
	layout := tooltipLayout{
		fontPx:  fontPx,
		iconSz:  iconSz,
		padX:    padX,
		padY:    padY,
		lineGap: lineGap,
		iconGap: iconGap,
		pairGap: pairGap,
		h1:      h1,
		h2:      h2,
		appW:    int32(math.Ceil(float64(aW))) + slack,
		downW:   int32(math.Ceil(float64(dW))) + slack,
		upW:     int32(math.Ceil(float64(uW))) + slack,
		reqH:    padY*2 + h1 + lineGap + h2,
	}
	layout.applyMinRateWidths(0, 0)
	return layout
}

func measureTooltipLayout(dpi uint32, fontName, downStr, upStr string) (tooltipLayout, bool) {
	if dpi == 0 {
		dpi = 96
	}
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}

	buf := make([]byte, 16)
	var bmp uintptr
	st, _, _ := procGdipCreateBitmapFromScan0.Call(2, 2, 8, gpPixelFormat32bppPARGB, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmp)))
	if st != 0 || bmp == 0 {
		return tooltipLayout{}, false
	}
	defer procGdipDisposeImage.Call(bmp)

	var gfx uintptr
	st, _, _ = procGdipGetImageGraphicsContext.Call(bmp, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		return tooltipLayout{}, false
	}
	defer procGdipDeleteGraphics.Call(gfx)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}
	fontPx, _ := tooltipMetricSizes(dpi)
	font, err := gpCreateFontPx(screenDC, fontName, fontPx, 500)
	if err != nil {
		return tooltipLayout{}, false
	}
	defer procGdipDeleteFont.Call(font)

	strFmt, err := gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if err != nil {
		return tooltipLayout{}, false
	}
	defer procGdipDeleteStringFormat.Call(strFmt)

	return computeTooltipLayoutWithFont(gfx, font, strFmt, dpi, downStr, upStr), true
}

func (tt *overlayTooltip) ensureFont(dpi uint32, fontName string) {
	if tt.hwnd == 0 {
		return
	}
	if dpi == 0 {
		dpi = 96
	}
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}
	if tt.hFont != 0 && tt.fontDPI == dpi && tt.fontName == fontName {
		return
	}

	fontPx, _ := tooltipMetricSizes(dpi)
	var lf logFontW
	lf.LfHeight = -fontPx
	lf.LfWeight = 500
	lf.LfCharSet = 1
	lf.LfQuality = 5 // CLEARTYPE_QUALITY
	copyUTF16Fixed(lf.LfFaceName[:], fontName)

	newFont, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
	if newFont != 0 {
		_, _, _ = procSendMessageW.Call(tt.hwnd, wmSetFont, newFont, 0)
		if tt.hFont != 0 {
			_, _, _ = procDeleteObject.Call(tt.hFont)
		}
		tt.hFont = newFont
		tt.fontDPI = dpi
		tt.fontName = fontName
	}

	margin := rect{
		Left:   scaleDPIInt(14, dpi),
		Top:    scaleDPIInt(10, dpi),
		Right:  scaleDPIInt(14, dpi),
		Bottom: scaleDPIInt(10, dpi),
	}
	_, _, _ = procSendMessageW.Call(tt.hwnd, ttmSetMargin, 0, uintptr(unsafe.Pointer(&margin)))
}

func (tt *overlayTooltip) releaseIcons() {
	if tt.upIconBmp != 0 {
		_, _, _ = procGdipDisposeImage.Call(tt.upIconBmp)
		tt.upIconBmp = 0
	}
	if tt.downIconBmp != 0 {
		_, _, _ = procGdipDisposeImage.Call(tt.downIconBmp)
		tt.downIconBmp = 0
	}
	tt.upIconBuf = nil
	tt.downIconBuf = nil
	tt.iconDPI = 0
	tt.iconARGB = 0
}

func (tt *overlayTooltip) ensureIcons(dpi uint32, argb uint32) {
	if dpi == 0 {
		dpi = 96
	}
	if tt.upIconBmp != 0 && tt.downIconBmp != 0 && tt.iconDPI == dpi && tt.iconARGB == argb {
		return
	}
	tt.releaseIcons()
	_, iconSz := tooltipMetricSizes(dpi)
	tt.downIconBmp, tt.downIconBuf, _ = RasterizeSVGToBitmap(assets.IconArrowDownBold, iconSz, iconSz, argb)
	tt.upIconBmp, tt.upIconBuf, _ = RasterizeSVGToBitmap(assets.IconArrowUpBold, iconSz, iconSz, argb)
	tt.iconDPI = dpi
	tt.iconARGB = argb
}

func (tt *overlayTooltip) ensureWindow(ownerHWND uintptr, mode config.ThemeMode, dpi uint32, fontName string) bool {
	if tt.hwnd != 0 {
		if isWindowHandleValid(tt.hwnd) && tt.ownerHWND == ownerHWND {
			return true
		}
		tt.Destroy()
	}
	if ownerHWND == 0 {
		return false
	}

	ensureCommonControlsTooltip()
	clsPtr, err := windows.UTF16PtrFromString(nativeTooltipClass)
	if err != nil {
		return false
	}

	cwUseDefault := ^uintptr(0x7FFFFFFF) // CW_USEDEFAULT (-2147483648)
	exStyle := uintptr(wsExTopmost | wsExToolWindow | wsExTransparent | wsExNoActivate)
	style := uintptr(wsPopup | ttsAlwaysTip | ttsNoPrefix)
	hwnd, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(clsPtr)),
		0,
		style,
		cwUseDefault, cwUseDefault, cwUseDefault, cwUseDefault,
		ownerHWND, 0, getModuleHandle(), 0,
	)
	if hwnd == 0 {
		return false
	}

	tt.hwnd = hwnd
	tt.ownerHWND = ownerHWND
	tt.applyTheme(mode)
	tt.ensureFont(dpi, fontName)

	initPtr, _ := windows.UTF16PtrFromString(config.AppName)
	ti := ttToolInfoW{
		CbSize:   ttToolInfoV2Size,
		UFlags:   ttfTrack | ttfAbsolute,
		Hwnd:     ownerHWND,
		UID:      ownerHWND,
		LpszText: initPtr,
	}
	added, _, _ := procSendMessageW.Call(hwnd, ttmAddToolW, 0, uintptr(unsafe.Pointer(&ti)))
	if added == 0 {
		tt.Destroy()
		return false
	}
	_, _, _ = procSendMessageW.Call(hwnd, ttmSetMaxTipWidth, 0, uintptr(scaleDPIInt(520, dpi)))
	tt.installSubclass()

	tt.lastText = config.AppName
	return true
}

func (tt *overlayTooltip) applyTheme(mode config.ThemeMode) {
	if tt.hwnd == 0 {
		return
	}
	subApp, isDark, hc := resolveNativeTooltipThemeName(mode)

	if procSetWindowTheme.Find() == nil {
		if hc {
			emptyPtr, _ := windows.UTF16PtrFromString("")
			_, _, _ = procSetWindowTheme.Call(tt.hwnd, uintptr(unsafe.Pointer(emptyPtr)), uintptr(unsafe.Pointer(emptyPtr)))
		} else {
			subPtr, _ := windows.UTF16PtrFromString(subApp)
			_, _, _ = procSetWindowTheme.Call(tt.hwnd, uintptr(unsafe.Pointer(subPtr)), 0)
		}
	}

	if procDwmSetWindowAttribute.Find() == nil {
		var darkVal uint32
		if isDark {
			darkVal = 1
		}
		_, _, _ = procDwmSetWindowAttribute.Call(tt.hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&darkVal)), 4)
		corner := uint32(dwmWcpRound)
		_, _, _ = procDwmSetWindowAttribute.Call(tt.hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&corner)), 4)
	}

	if hc && procGetSysColor.Find() == nil {
		bkCR, _, _ := procGetSysColor.Call(colorWindow)
		txtCR, _, _ := procGetSysColor.Call(colorWindowText)
		_, _, _ = procSendMessageW.Call(tt.hwnd, ttmSetTipBkColor, bkCR, 0)
		_, _, _ = procSendMessageW.Call(tt.hwnd, ttmSetTipTextClr, txtCR, 0)
	}

	tt.lastTheme = mode
	tt.lastHC = hc
	tt.canCustomDraw = tt.probeCustomDrawSupport(hc)
}

func (tt *overlayTooltip) openTooltipTheme() uintptr {
	if tt == nil || tt.hwnd == 0 {
		return 0
	}
	if procOpenThemeData.Find() != nil || procDrawThemeBackground.Find() != nil || procCloseThemeData.Find() != nil {
		return 0
	}
	clsPtr, err := windows.UTF16PtrFromString("TOOLTIP")
	if err != nil {
		return 0
	}
	hTheme, _, _ := procOpenThemeData.Call(tt.hwnd, uintptr(unsafe.Pointer(clsPtr)))
	return hTheme
}

func (tt *overlayTooltip) probeCustomDrawSupport(hc bool) bool {
	if hc {
		return false
	}
	hTheme := tt.openTooltipTheme()
	if hTheme == 0 {
		return false
	}
	_, _, _ = procCloseThemeData.Call(hTheme)
	return true
}

func (tt *overlayTooltip) Hide() {
	if tt == nil {
		return
	}
	if tt.visible && tt.hwnd != 0 && isWindowHandleValid(tt.hwnd) {
		ti := ttToolInfoW{
			CbSize: ttToolInfoV2Size,
			Hwnd:   tt.ownerHWND,
			UID:    tt.ownerHWND,
		}
		_, _, _ = procSendMessageW.Call(tt.hwnd, ttmTrackActivate, 0, uintptr(unsafe.Pointer(&ti)))
	}
	tt.visible = false
	tt.hasPos = false
	tt.holdDownW = 0
	tt.holdUpW = 0
}

func (tt *overlayTooltip) releasePaintBuffer() {
	if tt == nil {
		return
	}
	if tt.memDC != 0 {
		if tt.memOldBmp != 0 {
			_, _, _ = procSelectObject.Call(tt.memDC, tt.memOldBmp)
			tt.memOldBmp = 0
		}
		_, _, _ = procDeleteDC.Call(tt.memDC)
		tt.memDC = 0
	}
	if tt.memBmp != 0 {
		_, _, _ = procDeleteObject.Call(tt.memBmp)
		tt.memBmp = 0
	}
	tt.memW = 0
	tt.memH = 0
}

func (tt *overlayTooltip) ensurePaintBuffer(refHDC uintptr, w, h int32) bool {
	if tt == nil || refHDC == 0 || w <= 0 || h <= 0 {
		return false
	}
	if tt.memDC != 0 && tt.memBmp != 0 && tt.memW == w && tt.memH == h {
		return true
	}
	tt.releasePaintBuffer()

	memDC, _, _ := procCreateCompatibleDC.Call(refHDC)
	if memDC == 0 {
		return false
	}
	bmi := bitmapInfo{
		BmiHeader: bitmapInfoHeader{
			BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			BiWidth:       w,
			BiHeight:      -h,
			BiPlanes:      1,
			BiBitCount:    32,
			BiCompression: biRGB,
		},
	}
	var pBits unsafe.Pointer
	hBmp, _, _ := procCreateDIBSection.Call(refHDC, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&pBits)), 0, 0)
	if hBmp == 0 || pBits == nil {
		_, _, _ = procDeleteDC.Call(memDC)
		return false
	}
	oldBmp, _, _ := procSelectObject.Call(memDC, hBmp)
	tt.memDC = memDC
	tt.memBmp = hBmp
	tt.memOldBmp = oldBmp
	tt.memW = w
	tt.memH = h
	return true
}

func (tt *overlayTooltip) Destroy() {
	if tt == nil {
		return
	}
	tt.Hide()
	tt.uninstallSubclass()
	tt.releaseIcons()
	tt.releasePaintBuffer()
	if tt.hwnd != 0 {
		_, _, _ = procDestroyWindow.Call(tt.hwnd)
		tt.hwnd = 0
	}
	if tt.hFont != 0 {
		_, _, _ = procDeleteObject.Call(tt.hFont)
		tt.hFont = 0
	}
	tt.ownerHWND = 0
	tt.fontDPI = 0
	tt.fontName = ""
	tt.lastText = ""
	tt.canCustomDraw = false
	tt.customBoundsActive = false
	tt.hasTargetBounds = false
}

func computeTooltipBounds(overlayRect, taskbarRect, monRect rect, tipW, tipH, dpi uint32) (int32, int32) {
	w := int32(tipW)
	h := int32(tipH)
	gap := scaleDPIInt(6, dpi)
	margin := scaleDPIInt(6, dpi)

	x := overlayRect.Left + (overlayRect.Width()-w)/2
	y := overlayRect.Top - h - gap

	if !taskbarRect.Empty() {
		tbVertical := taskbarRect.Height() > taskbarRect.Width()
		if tbVertical {
			midMonX := monRect.Left + monRect.Width()/2
			y = overlayRect.Top + (overlayRect.Height()-h)/2
			if taskbarRect.Left < midMonX {
				anchorX := taskbarRect.Right
				if overlayRect.Right > anchorX {
					anchorX = overlayRect.Right
				}
				x = anchorX + gap
			} else {
				anchorX := taskbarRect.Left
				if overlayRect.Left < anchorX {
					anchorX = overlayRect.Left
				}
				x = anchorX - w - gap
			}
		} else {
			midMonY := monRect.Top + monRect.Height()/2
			if taskbarRect.Top < midMonY {
				anchorY := taskbarRect.Bottom
				if overlayRect.Bottom > anchorY {
					anchorY = overlayRect.Bottom
				}
				y = anchorY + gap
			} else {
				anchorY := taskbarRect.Top
				if overlayRect.Top < anchorY {
					anchorY = overlayRect.Top
				}
				y = anchorY - h - gap
			}
		}
	}

	if x+w > monRect.Right-margin {
		x = monRect.Right - margin - w
	}
	if x < monRect.Left+margin {
		x = monRect.Left + margin
	}
	if y+h > monRect.Bottom-margin {
		y = monRect.Bottom - margin - h
	}
	if y < monRect.Top+margin {
		y = monRect.Top + margin
	}
	return x, y
}

func packTrackPosition(x, y int32) uintptr {
	return uintptr(uint16(int16(x))) | (uintptr(uint16(int16(y))) << 16)
}

func drawNativeTooltipContentWithHold(gfx uintptr, cr rect, dpi uint32, fontName, downStr, upStr string, textARGB uint32, downBmp, upBmp uintptr, holdDownW, holdUpW int32) {
	if cr.Width() <= 0 || cr.Height() <= 0 {
		return
	}
	if dpi == 0 {
		dpi = 96
	}
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}
	fontPx, _ := tooltipMetricSizes(dpi)

	_, _, _ = procGdipSetSmoothingMode.Call(gfx, gpSmoothingModeHighQuality)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}
	font, err := gpCreateFontPx(screenDC, fontName, fontPx, 500)
	if err != nil {
		return
	}
	defer procGdipDeleteFont.Call(font)

	strFmt, err := gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if err != nil {
		return
	}
	defer procGdipDeleteStringFormat.Call(strFmt)

	txtBrush, err := gpCreateSolidBrush(textARGB)
	if err != nil {
		return
	}
	defer procGdipDeleteBrush.Call(txtBrush)

	layout := computeTooltipLayoutWithFont(gfx, font, strFmt, dpi, downStr, upStr)
	layout.applyMinRateWidths(holdDownW, holdUpW)
	totalH := layout.h1 + layout.lineGap + layout.h2
	startY := cr.Top + (cr.Height()-totalH)/2
	if startY < cr.Top+layout.padY {
		startY = cr.Top + layout.padY
	}
	leftX := cr.Left + layout.padX
	maxTitleW := cr.Width() - layout.padX*2
	if maxTitleW < layout.appW {
		maxTitleW = layout.appW
	}

	gpDrawString(gfx, font, strFmt, txtBrush, config.AppName, gpRectF{
		X:      float32(leftX),
		Y:      float32(startY),
		Width:  float32(maxTitleW),
		Height: float32(layout.h1),
	})

	row2Y := startY + layout.h1 + layout.lineGap
	curX := leftX
	if downBmp != 0 {
		iy := row2Y + (layout.h2-layout.iconSz)/2
		_, _, _ = procGdipDrawImageRectI.Call(gfx, downBmp, uintptr(curX), uintptr(iy), uintptr(layout.iconSz), uintptr(layout.iconSz))
	}
	curX += layout.iconSz + layout.iconGap
	gpDrawString(gfx, font, strFmt, txtBrush, downStr, gpRectF{
		X:      float32(curX),
		Y:      float32(row2Y),
		Width:  float32(layout.downW),
		Height: float32(layout.h2),
	})

	curX += layout.downW + layout.pairGap
	if upBmp != 0 {
		iy := row2Y + (layout.h2-layout.iconSz)/2
		_, _, _ = procGdipDrawImageRectI.Call(gfx, upBmp, uintptr(curX), uintptr(iy), uintptr(layout.iconSz), uintptr(layout.iconSz))
	}
	curX += layout.iconSz + layout.iconGap
	gpDrawString(gfx, font, strFmt, txtBrush, upStr, gpRectF{
		X:      float32(curX),
		Y:      float32(row2Y),
		Width:  float32(layout.upW),
		Height: float32(layout.h2),
	})
}

func drawNativeTooltipContent(gfx uintptr, cr rect, dpi uint32, fontName, downStr, upStr string, textARGB uint32, downBmp, upBmp uintptr) {
	drawNativeTooltipContentWithHold(gfx, cr, dpi, fontName, downStr, upStr, textARGB, downBmp, upBmp, 0, 0)
}

func tooltipNonClientInsets(hwnd uintptr, dpi uint32) (int32, int32) {
	if hwnd == 0 {
		return 0, 0
	}
	wr, wOk := getWindowRect(hwnd)
	var cr rect
	if wOk && !wr.Empty() {
		if r1, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr))); r1 != 0 && !cr.Empty() {
			ncW := wr.Width() - cr.Width()
			ncH := wr.Height() - cr.Height()
			if ncW < 0 {
				ncW = 0
			}
			if ncH < 0 {
				ncH = 0
			}
			return ncW, ncH
		}
	}
	if procAdjustWindowRectExForDpi.Find() == nil {
		if dpi == 0 {
			dpi = 96
		}
		adj := rect{}
		st := getWindowStyle(hwnd)
		ex := getWindowExStyle(hwnd)
		if ok, _, _ := procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&adj)), uintptr(st), 0, uintptr(ex), uintptr(dpi)); ok != 0 {
			ncW, ncH := adj.Width(), adj.Height()
			if ncW < 0 {
				ncW = 0
			}
			if ncH < 0 {
				ncH = 0
			}
			return ncW, ncH
		}
	}
	return 0, 0
}

func (tt *overlayTooltip) clearStaleWindowRgn() bool {
	if tt == nil || tt.hwnd == 0 || procGetWindowRgnBox.Find() != nil || procSetWindowRgn.Find() != nil {
		return false
	}
	var rgn rect
	rType, _, _ := procGetWindowRgnBox.Call(tt.hwnd, uintptr(unsafe.Pointer(&rgn)))
	if rType == rgnError {
		return false
	}
	_, _, _ = procSetWindowRgn.Call(tt.hwnd, 0, 0)
	return true
}

func (tt *overlayTooltip) renderTooltipSurface(targetDC, hTheme uintptr, cr rect, txtARGB uint32) bool {
	if hTheme != 0 {
		_, _, _ = procDrawThemeBackground.Call(hTheme, targetDC, ttpStandard, 0, uintptr(unsafe.Pointer(&cr)), 0)
	}
	var gfx uintptr
	st, _, _ := procGdipCreateFromHDC.Call(targetDC, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		return false
	}
	if hTheme == 0 {
		pal := resolveTooltipPaletteForMode(tt.lastTheme)
		if bBG, err := gpCreateSolidBrush(pal.bg); err == nil {
			gpFillRoundedRect(gfx, bBG, cr.Left, cr.Top, cr.Width(), cr.Height(), 0)
			_, _, _ = procGdipDeleteBrush.Call(bBG)
		}
		txtARGB = pal.textColor
	}
	tt.ensureIcons(tt.dpi, txtARGB)
	drawNativeTooltipContentWithHold(gfx, cr, tt.dpi, tt.fontName, tt.lastDown, tt.lastUp, txtARGB, tt.downIconBmp, tt.upIconBmp, tt.holdDownW, tt.holdUpW)
	_, _, _ = procGdipDeleteGraphics.Call(gfx)
	return true
}

func (tt *overlayTooltip) resolveCustomTextARGB(hTheme uintptr) uint32 {
	txtARGB := uint32(0xFFFFFFFF)
	if resolveThemeIsLight(tt.lastTheme) {
		txtARGB = 0xFF1F1F1F
	}
	if hTheme != 0 && procGetThemeColor.Find() == nil {
		var tc uint32
		if hr, _, _ := procGetThemeColor.Call(hTheme, ttpStandard, 0, tmtTextColor, uintptr(unsafe.Pointer(&tc))); hr == 0 {
			txtARGB = colorrefToARGB(tc)
		}
	}
	return txtARGB
}

func (tt *overlayTooltip) paintClientDC(dstHDC uintptr) bool {
	if tt == nil || tt.hwnd == 0 || dstHDC == 0 {
		return false
	}
	var cr rect
	if r1, _, _ := procGetClientRect.Call(tt.hwnd, uintptr(unsafe.Pointer(&cr))); r1 == 0 || cr.Empty() {
		return false
	}

	hTheme := tt.openTooltipTheme()
	if hTheme != 0 {
		defer procCloseThemeData.Call(hTheme)
	}
	txtARGB := tt.resolveCustomTextARGB(hTheme)

	cw, ch := cr.Width(), cr.Height()
	if tt.ensurePaintBuffer(dstHDC, cw, ch) {
		if !tt.renderTooltipSurface(tt.memDC, hTheme, cr, txtARGB) {
			return false
		}
		_, _, _ = procBitBlt.Call(dstHDC, 0, 0, uintptr(cw), uintptr(ch), tt.memDC, 0, 0, srcCopy)
		return true
	}
	return tt.renderTooltipSurface(dstHDC, hTheme, cr, txtARGB)
}

// HandleNotify serves as the fallback path when the tooltip HWND is not subclassed.
func (tt *overlayTooltip) HandleNotify(lParam uintptr) (handled bool, ret uintptr) {
	if tt == nil || tt.hwnd == 0 || tt.prevProc != 0 {
		return false, 0
	}
	nm, ok := copyNMCustomDrawFromLParam(lParam)
	if !ok || nm.Hdr.HwndFrom != tt.hwnd || nm.Hdr.Code != nmCustomDrawCode || nm.DwDrawStage != cddsPrePaint {
		return false, 0
	}
	if _, _, hc := resolveNativeTooltipThemeName(tt.lastTheme); hc {
		return false, 0
	}
	hTheme := tt.openTooltipTheme()
	if hTheme == 0 {
		return false, 0
	}
	_, _, _ = procCloseThemeData.Call(hTheme)
	if !tt.paintClientDC(nm.Hdc) {
		return false, 0
	}
	return true, cdrfSkipDefault
}

func (tt *overlayTooltip) ShowOrUpdate(ownerHWND uintptr, overlayRect, taskbarRect, monRect rect, dpi uint32, themeMode config.ThemeMode, fontName, downStr, upStr string) {
	if !tt.ensureWindow(ownerHWND, themeMode, dpi, fontName) {
		return
	}
	if dpi == 0 {
		dpi = 96
	}
	dpiChanged := tt.dpi != 0 && tt.dpi != dpi
	tt.dpi = dpi

	hcNow, _, _ := DetectHighContrast()
	themeChanged := tt.lastTheme != themeMode || tt.lastHC != hcNow
	if themeChanged {
		tt.applyTheme(themeMode)
	}
	tt.ensureFont(dpi, fontName)

	wasVisible := tt.visible
	if !wasVisible || themeChanged || dpiChanged {
		tt.holdDownW = 0
		tt.holdUpW = 0
	}

	prevDown, prevUp := tt.lastDown, tt.lastUp
	tt.lastDown = downStr
	tt.lastUp = upStr
	rateChanged := downStr != prevDown || upStr != prevUp

	layout, hasLayout := measureTooltipLayout(dpi, fontName, downStr, upStr)
	if hasLayout {
		if layout.downW > tt.holdDownW {
			tt.holdDownW = layout.downW
		}
		if layout.upW > tt.holdUpW {
			tt.holdUpW = layout.upW
		}
		layout.applyMinRateWidths(tt.holdDownW, tt.holdUpW)
	}
	prevCustomBounds := tt.customBoundsActive
	useCustomBounds := !hcNow && tt.canCustomDraw && hasLayout
	tt.customBoundsActive = useCustomBounds

	text := formatNativeOverlayTooltip(downStr, upStr)
	textChanged := text != tt.lastText
	// Only sync ComCtrl's native text string on initial show or when falling back to ComCtrl's
	// own High Contrast / non-themed rendering. Updating tool text every 1s under custom painting
	// makes ComCtrl recalculate GDI sizes and trigger unnecessary layout passes.
	if (textChanged && (!wasVisible || !useCustomBounds)) || (prevCustomBounds && !useCustomBounds) {
		txtPtr, err := windows.UTF16PtrFromString(text)
		if err == nil {
			ti := ttToolInfoW{
				CbSize:   ttToolInfoV2Size,
				Hwnd:     tt.ownerHWND,
				UID:      tt.ownerHWND,
				LpszText: txtPtr,
			}
			_, _, _ = procSendMessageW.Call(tt.hwnd, ttmUpdateTipTextW, 0, uintptr(unsafe.Pointer(&ti)))
			tt.lastText = text
		}
	}

	if useCustomBounds {
		ncW, ncH := tooltipNonClientInsets(tt.hwnd, dpi)
		targetW := layout.reqW + ncW
		targetH := layout.reqH + ncH
		x, y := computeTooltipBounds(overlayRect, taskbarRect, monRect, uint32(targetW), uint32(targetH), dpi)

		tt.targetX = x
		tt.targetY = y
		tt.targetW = targetW
		tt.targetH = targetH
		tt.hasTargetBounds = true

		if !wasVisible {
			_, _, _ = procSendMessageW.Call(tt.hwnd, ttmTrackPosition, 0, packTrackPosition(x, y))
			ti := ttToolInfoW{
				CbSize: ttToolInfoV2Size,
				Hwnd:   tt.ownerHWND,
				UID:    tt.ownerHWND,
			}
			_, _, _ = procSendMessageW.Call(tt.hwnd, ttmTrackActivate, 1, uintptr(unsafe.Pointer(&ti)))
			tt.visible = true
		}

		cleared := tt.clearStaleWindowRgn()
		curWR, curOk := getWindowRect(tt.hwnd)
		sizeDiffers := !curOk || curWR.Width() != targetW || curWR.Height() != targetH
		posDiffers := !curOk || curWR.Left != x || curWR.Top != y
		if sizeDiffers || posDiffers || prevCustomBounds != useCustomBounds {
			_, _, _ = procSetWindowPos.Call(tt.hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(targetW), uintptr(targetH), swpNoActivate)
			if tt.clearStaleWindowRgn() {
				cleared = true
			}
		}
		if !wasVisible || themeChanged || dpiChanged || cleared || sizeDiffers || posDiffers || rateChanged || prevCustomBounds != useCustomBounds {
			_, _, _ = procInvalidateRect.Call(tt.hwnd, 0, 0)
		}
		tt.lastX = x
		tt.lastY = y
		tt.hasPos = true
		return
	}

	tt.hasTargetBounds = false
	if !wasVisible {
		provX, provY := overlayRect.Left, overlayRect.Top-scaleDPIInt(56, dpi)
		if tt.hasPos {
			provX, provY = tt.lastX, tt.lastY
		}
		_, _, _ = procSendMessageW.Call(tt.hwnd, ttmTrackPosition, 0, packTrackPosition(provX, provY))
		ti := ttToolInfoW{
			CbSize: ttToolInfoV2Size,
			Hwnd:   tt.ownerHWND,
			UID:    tt.ownerHWND,
		}
		_, _, _ = procSendMessageW.Call(tt.hwnd, ttmTrackActivate, 1, uintptr(unsafe.Pointer(&ti)))
		tt.visible = true
	}

	if wr, ok := getWindowRect(tt.hwnd); ok && !wr.Empty() {
		x, y := computeTooltipBounds(overlayRect, taskbarRect, monRect, uint32(wr.Width()), uint32(wr.Height()), dpi)
		if !tt.hasPos || x != tt.lastX || y != tt.lastY || prevCustomBounds != useCustomBounds {
			_, _, _ = procSendMessageW.Call(tt.hwnd, ttmTrackPosition, 0, packTrackPosition(x, y))
		}
		tt.lastX = x
		tt.lastY = y
		tt.hasPos = true
	}
	_, _, _ = procSetWindowPos.Call(tt.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}
