package ui

import (
	"fmt"
	"math"
	"sync"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"golang.org/x/sys/windows"
)

const (
	tooltipClassName    = "WhatsMySpeedTooltipWindow"
	timerTooltipID      = 4
	tooltipHoverDelayMs = 450
)

type tooltipPalette struct {
	bg         uint32
	border     uint32
	titleColor uint32
	textColor  uint32
	downArrow  uint32
	upArrow    uint32
}

var (
	tooltipClassOnce sync.Once
	tooltipWndProcCB uintptr
)

type overlayTooltip struct {
	hwnd      uintptr
	memDC     uintptr
	hBitmap   uintptr
	oldBitmap uintptr
	pBits     unsafe.Pointer
	gpBitmap  uintptr
	dibW      int32
	dibH      int32
	visible   bool
	lastKey   string
	lastX     int32
	lastY     int32
}

func resolveTooltipPalette(isLight bool) tooltipPalette {
	if active, _, _ := DetectHighContrast(); active && procGetSysColor.Find() == nil {
		winCR, _, _ := procGetSysColor.Call(colorWindow)
		txtCR, _, _ := procGetSysColor.Call(colorWindowText)
		hiCR, _, _ := procGetSysColor.Call(colorHighlight)
		bg := colorrefToARGB(uint32(winCR))
		txt := colorrefToARGB(uint32(txtCR))
		accent := colorrefToARGB(uint32(hiCR))
		return tooltipPalette{
			bg:         bg,
			border:     txt,
			titleColor: txt,
			textColor:  txt,
			downArrow:  accent,
			upArrow:    txt,
		}
	}
	if isLight {
		return tooltipPalette{
			bg:         0xFAFFFFFF,
			border:     0xFFCBD5E1,
			titleColor: 0xFF475569,
			textColor:  0xFF0F172A,
			downArrow:  0xFF0E7490,
			upArrow:    0xFF4D7C0F,
		}
	}
	return tooltipPalette{
		bg:         0xF5121826,
		border:     0xFF334155,
		titleColor: 0xFF94A3B8,
		textColor:  0xFFF8FAFC,
		downArrow:  0xFF22D3EE,
		upArrow:    0xFFA3E635,
	}
}

func ensureTooltipClass() bool {
	tooltipClassOnce.Do(func() {
		tooltipWndProcCB = windows.NewCallback(tooltipWndProc)
		clsPtr, err := windows.UTF16PtrFromString(tooltipClassName)
		if err != nil {
			return
		}
		wcx := wndClassExW{
			CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			LpfnWndProc:   tooltipWndProcCB,
			HInstance:     getModuleHandle(),
			LpszClassName: clsPtr,
		}
		_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))
	})
	return tooltipWndProcCB != 0
}

func tooltipWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == wmMouseActivate {
		return maNoActivate
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return ret
}

func (tt *overlayTooltip) ensureWindow() bool {
	if tt.hwnd != 0 && isWindowHandleValid(tt.hwnd) {
		return true
	}
	tt.hwnd = 0
	if !ensureTooltipClass() {
		return false
	}
	clsPtr, err := windows.UTF16PtrFromString(tooltipClassName)
	if err != nil {
		return false
	}
	exStyle := uintptr(wsExToolWindow | wsExTopmost | wsExLayered | wsExTransparent | wsExNoActivate)
	hwnd, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(clsPtr)),
		0,
		uintptr(wsPopup),
		0, 0, 120, 48,
		0, 0, getModuleHandle(), 0,
	)
	if hwnd == 0 {
		return false
	}
	tt.hwnd = hwnd
	return true
}

func (tt *overlayTooltip) ensureDIB(w, h int32) bool {
	if tt.dibW == w && tt.dibH == h && tt.memDC != 0 && tt.gpBitmap != 0 {
		return true
	}
	tt.releaseDIB()
	if w <= 0 || h <= 0 {
		return false
	}
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return false
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
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
	hBmp, _, _ := procCreateDIBSection.Call(screenDC, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&pBits)), 0, 0)
	if hBmp == 0 || pBits == nil {
		_, _, _ = procDeleteDC.Call(memDC)
		return false
	}
	oldObj, _, _ := procSelectObject.Call(memDC, hBmp)
	var gpBmp uintptr
	st, _, _ := procGdipCreateBitmapFromScan0.Call(
		uintptr(w), uintptr(h), uintptr(w*4),
		gpPixelFormat32bppPARGB, uintptr(pBits), uintptr(unsafe.Pointer(&gpBmp)),
	)
	if st != 0 || gpBmp == 0 {
		_, _, _ = procSelectObject.Call(memDC, oldObj)
		_, _, _ = procDeleteObject.Call(hBmp)
		_, _, _ = procDeleteDC.Call(memDC)
		return false
	}

	tt.memDC = memDC
	tt.hBitmap = hBmp
	tt.oldBitmap = oldObj
	tt.pBits = pBits
	tt.gpBitmap = gpBmp
	tt.dibW = w
	tt.dibH = h
	return true
}

func (tt *overlayTooltip) releaseDIB() {
	if tt.gpBitmap != 0 {
		_, _, _ = procGdipDisposeImage.Call(tt.gpBitmap)
		tt.gpBitmap = 0
	}
	if tt.memDC != 0 {
		if tt.oldBitmap != 0 {
			_, _, _ = procSelectObject.Call(tt.memDC, tt.oldBitmap)
			tt.oldBitmap = 0
		}
		_, _, _ = procDeleteDC.Call(tt.memDC)
		tt.memDC = 0
	}
	if tt.hBitmap != 0 {
		_, _, _ = procDeleteObject.Call(tt.hBitmap)
		tt.hBitmap = 0
	}
	tt.pBits = nil
	tt.dibW = 0
	tt.dibH = 0
	tt.lastKey = ""
}

func (tt *overlayTooltip) Hide() {
	if tt == nil {
		return
	}
	if tt.visible && tt.hwnd != 0 {
		_, _, _ = procShowWindow.Call(tt.hwnd, swHide)
	}
	tt.visible = false
}

func (tt *overlayTooltip) Destroy() {
	if tt == nil {
		return
	}
	tt.Hide()
	tt.releaseDIB()
	if tt.hwnd != 0 {
		_, _, _ = procDestroyWindow.Call(tt.hwnd)
		tt.hwnd = 0
	}
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

func measureOverlayTooltip(title, downStr, upStr string, dpi uint32, fontName string) (tipW, tipH int32, titleH, speedH, dArrowW, dTxtW, uArrowW, uTxtW int32) {
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}
	padX := scaleDPIInt(12, dpi)
	padY := scaleDPIInt(8, dpi)
	rowGap := scaleDPIInt(3, dpi)
	arrowGap := scaleDPIInt(4, dpi)
	pairGap := scaleDPIInt(12, dpi)
	fontPx := scaleDPIInt(12, dpi)

	var gfx, bmp, fTitle, fBody, strFmt uintptr
	buf := make([]byte, 16)
	st, _, _ := procGdipCreateBitmapFromScan0.Call(2, 2, 8, gpPixelFormat32bppPARGB, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmp)))
	if st == 0 && bmp != 0 {
		defer procGdipDisposeImage.Call(bmp)
		st2, _, _ := procGdipGetImageGraphicsContext.Call(bmp, uintptr(unsafe.Pointer(&gfx)))
		if st2 == 0 && gfx != 0 {
			defer procGdipDeleteGraphics.Call(gfx)
			_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)
		}
	}
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}
	fTitle, _ = gpCreateFontPx(screenDC, fontName, fontPx, 600)
	if fTitle != 0 {
		defer procGdipDeleteFont.Call(fTitle)
	}
	fBody, _ = gpCreateFontPx(screenDC, fontName, fontPx, 600)
	if fBody != 0 {
		defer procGdipDeleteFont.Call(fBody)
	}
	strFmt, _ = gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if strFmt != 0 {
		defer procGdipDeleteStringFormat.Call(strFmt)
	}

	titleH = scaleDPIInt(16, dpi)
	speedH = scaleDPIInt(16, dpi)
	tW := scaleDPIInt(90, dpi)
	dArrowW = scaleDPIInt(8, dpi)
	uArrowW = scaleDPIInt(8, dpi)
	dTxtW = scaleDPIInt(52, dpi)
	uTxtW = scaleDPIInt(52, dpi)

	if gfx != 0 && strFmt != 0 {
		if fTitle != 0 {
			if fw, fh := gpMeasureString(gfx, fTitle, strFmt, title); fw > 0 {
				tW = int32(math.Ceil(float64(fw))) + scaleDPIInt(2, dpi)
				if int32(math.Ceil(float64(fh))) > titleH {
					titleH = int32(math.Ceil(float64(fh)))
				}
			}
		}
		if fBody != 0 {
			if fw, fh := gpMeasureString(gfx, fBody, strFmt, "↓"); fw > 0 {
				dArrowW = int32(math.Ceil(float64(fw)))
				if int32(math.Ceil(float64(fh))) > speedH {
					speedH = int32(math.Ceil(float64(fh)))
				}
			}
			if fw, _ := gpMeasureString(gfx, fBody, strFmt, downStr); fw > 0 {
				dTxtW = int32(math.Ceil(float64(fw))) + scaleDPIInt(2, dpi)
			}
			if fw, _ := gpMeasureString(gfx, fBody, strFmt, "↑"); fw > 0 {
				uArrowW = int32(math.Ceil(float64(fw)))
			}
			if fw, _ := gpMeasureString(gfx, fBody, strFmt, upStr); fw > 0 {
				uTxtW = int32(math.Ceil(float64(fw))) + scaleDPIInt(2, dpi)
			}
		}
	}

	row2W := dArrowW + arrowGap + dTxtW + pairGap + uArrowW + arrowGap + uTxtW
	contentW := tW
	if row2W > contentW {
		contentW = row2W
	}
	tipW = contentW + padX*2
	tipH = titleH + rowGap + speedH + padY*2
	return tipW, tipH, titleH, speedH, dArrowW, dTxtW, uArrowW, uTxtW
}

func renderOverlayTooltip(gfx uintptr, w, h, titleH, speedH, dArrowW, dTxtW, uArrowW, uTxtW int32, title, downStr, upStr string, pal tooltipPalette, dpi uint32, fontName string) {
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}
	padX := scaleDPIInt(12, dpi)
	padY := scaleDPIInt(8, dpi)
	rowGap := scaleDPIInt(3, dpi)
	arrowGap := scaleDPIInt(4, dpi)
	pairGap := scaleDPIInt(12, dpi)
	fontPx := scaleDPIInt(12, dpi)
	cornerR := scaleDPIInt(8, dpi)

	_, _, _ = procGdipSetSmoothingMode.Call(gfx, gpSmoothingModeHighQuality)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)
	_, _, _ = procGdipGraphicsClear.Call(gfx, 0x00000000)

	if bBorder, err := gpCreateSolidBrush(pal.border); err == nil {
		gpFillRoundedRect(gfx, bBorder, 0, 0, w, h, cornerR)
		_, _, _ = procGdipDeleteBrush.Call(bBorder)
	}
	if bBG, err := gpCreateSolidBrush(pal.bg); err == nil {
		innerR := cornerR - 1
		if innerR < 0 {
			innerR = 0
		}
		gpFillRoundedRect(gfx, bBG, 1, 1, w-2, h-2, innerR)
		_, _, _ = procGdipDeleteBrush.Call(bBG)
	}

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}
	fTitle, _ := gpCreateFontPx(screenDC, fontName, fontPx, 600)
	if fTitle != 0 {
		defer procGdipDeleteFont.Call(fTitle)
	}
	fArrow, _ := gpCreateFontPx(screenDC, fontName, fontPx, 700)
	if fArrow != 0 {
		defer procGdipDeleteFont.Call(fArrow)
	}
	fBody, _ := gpCreateFontPx(screenDC, fontName, fontPx, 500)
	if fBody != 0 {
		defer procGdipDeleteFont.Call(fBody)
	}
	strFmt, _ := gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if strFmt != 0 {
		defer procGdipDeleteStringFormat.Call(strFmt)
	}
	if strFmt == 0 {
		return
	}

	if fTitle != 0 {
		if bTitle, err := gpCreateSolidBrush(pal.titleColor); err == nil {
			gpDrawString(gfx, fTitle, strFmt, bTitle, title, gpRectF{
				X: float32(padX), Y: float32(padY),
				Width: float32(w - padX*2), Height: float32(titleH),
			})
			_, _, _ = procGdipDeleteBrush.Call(bTitle)
		}
	}

	row2Y := float32(padY + titleH + rowGap)
	curX := padX
	if fArrow != 0 {
		if bDown, err := gpCreateSolidBrush(pal.downArrow); err == nil {
			gpDrawString(gfx, fArrow, strFmt, bDown, "↓", gpRectF{
				X: float32(curX), Y: row2Y, Width: float32(dArrowW + 2), Height: float32(speedH),
			})
			_, _, _ = procGdipDeleteBrush.Call(bDown)
		}
	}
	curX += dArrowW + arrowGap
	if fBody != 0 {
		if bTxt, err := gpCreateSolidBrush(pal.textColor); err == nil {
			gpDrawString(gfx, fBody, strFmt, bTxt, downStr, gpRectF{
				X: float32(curX), Y: row2Y, Width: float32(dTxtW + 2), Height: float32(speedH),
			})
			curX += dTxtW + pairGap
			if fArrow != 0 {
				if bUp, err := gpCreateSolidBrush(pal.upArrow); err == nil {
					gpDrawString(gfx, fArrow, strFmt, bUp, "↑", gpRectF{
						X: float32(curX), Y: row2Y, Width: float32(uArrowW + 2), Height: float32(speedH),
					})
					_, _, _ = procGdipDeleteBrush.Call(bUp)
				}
			}
			curX += uArrowW + arrowGap
			gpDrawString(gfx, fBody, strFmt, bTxt, upStr, gpRectF{
				X: float32(curX), Y: row2Y, Width: float32(uTxtW + 2), Height: float32(speedH),
			})
			_, _, _ = procGdipDeleteBrush.Call(bTxt)
		}
	}
}

func (tt *overlayTooltip) ShowOrUpdate(overlayRect, taskbarRect, monRect rect, dpi uint32, isLight bool, fontName, downStr, upStr string) {
	if !tt.ensureWindow() {
		return
	}
	if dpi == 0 {
		dpi = 96
	}
	pal := resolveTooltipPalette(isLight)
	title := config.AppName
	key := fmt.Sprintf("%d|%v|%08X|%s|%s", dpi, isLight, pal.bg, downStr, upStr)

	tipW, tipH, titleH, speedH, dArrowW, dTxtW, uArrowW, uTxtW := measureOverlayTooltip(title, downStr, upStr, dpi, fontName)
	needDraw := !tt.visible || tt.lastKey != key || tt.dibW != tipW || tt.dibH != tipH
	if needDraw {
		if !tt.ensureDIB(tipW, tipH) {
			return
		}
		var gfx uintptr
		st, _, _ := procGdipGetImageGraphicsContext.Call(tt.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
		if st != 0 || gfx == 0 {
			return
		}
		renderOverlayTooltip(gfx, tipW, tipH, titleH, speedH, dArrowW, dTxtW, uArrowW, uTxtW, title, downStr, upStr, pal, dpi, fontName)
		_, _, _ = procGdipDeleteGraphics.Call(gfx)
		tt.lastKey = key
	}

	x, y := computeTooltipBounds(overlayRect, taskbarRect, monRect, uint32(tipW), uint32(tipH), dpi)
	if needDraw || x != tt.lastX || y != tt.lastY {
		dstPt := point{X: x, Y: y}
		wndSize := size{Cx: tipW, Cy: tipH}
		srcPt := point{X: 0, Y: 0}
		blend := blendFunction{
			BlendOp:             acSrcOver,
			SourceConstantAlpha: 255,
			AlphaFormat:         acSrcAlpha,
		}
		_, _, _ = procUpdateLayeredWindow.Call(
			tt.hwnd,
			0,
			uintptr(unsafe.Pointer(&dstPt)),
			uintptr(unsafe.Pointer(&wndSize)),
			tt.memDC,
			uintptr(unsafe.Pointer(&srcPt)),
			0,
			uintptr(unsafe.Pointer(&blend)),
			ulwAlpha,
		)
		tt.lastX = x
		tt.lastY = y
	}
	flags := uintptr(swpNoMove | swpNoSize | swpNoActivate)
	if !tt.visible {
		flags |= swpShowWindow
	}
	_, _, _ = procSetWindowPos.Call(tt.hwnd, hwndTopmost, 0, 0, 0, 0, flags)
	tt.visible = true
}
