package ui

import (
	"math"
	"sync"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"golang.org/x/sys/windows"
)

const (
	menuClassName = "WhatsMySpeedMenuWindow"

	timerMenuSubOpenID  = 20
	timerMenuSubCloseID = 21
	menuHoverDelayMs    = 110
)

type menuItemSpec struct {
	id        uint32
	text      string
	isSep     bool
	isDefault bool
	isRadio   bool
	checked   bool
	children  []menuItemSpec
}

type menuPalette struct {
	bg        uint32
	border    uint32
	text      uint32
	muted     uint32
	hoverBG   uint32
	hoverText uint32
	accent    uint32
	separator uint32
}

type menuLayout struct {
	width       int32
	height      int32
	padY        int32
	leftGutterW int32
	rightArrowW int32
	rowH        int32
	sepH        int32
	fontPx      int32
	itemTops    []int32
	itemBottoms []int32
}

var (
	menuCheckGlyphPoly = []gpPointI{
		{X: 8, Y: 54},
		{X: 37, Y: 84},
		{X: 92, Y: 18},
		{X: 82, Y: 9},
		{X: 37, Y: 65},
		{X: 18, Y: 45},
	}
	menuChevronGlyphPoly = []gpPointI{
		{X: 22, Y: 8},
		{X: 78, Y: 50},
		{X: 22, Y: 92},
		{X: 10, Y: 81},
		{X: 54, Y: 50},
		{X: 10, Y: 19},
	}

	menuClassOnce sync.Once
	menuWndProcCB uintptr

	activeMenuMu sync.Mutex
	activeMenu   *popupMenuSession
)

type popupMenuLevel struct {
	hwnd        uintptr
	items       []menuItemSpec
	layout      menuLayout
	hoveredIdx  int
	tracking    bool
	hasDWMRound bool

	memDC     uintptr
	hBitmap   uintptr
	oldBitmap uintptr
	gpBitmap  uintptr
}

type popupMenuSession struct {
	ownerHWND  uintptr
	dpi        uint32
	fontName   string
	pal        menuPalette
	main       *popupMenuLevel
	sub        *popupMenuLevel
	openSubIdx int
	chosenID   uint32
	done       bool
}

func resolveMenuPalette(cfg config.Config) menuPalette {
	if active, _, _ := DetectHighContrast(); active && procGetSysColor.Find() == nil {
		menuCR, _, _ := procGetSysColor.Call(colorMenu)
		txtCR, _, _ := procGetSysColor.Call(colorMenuText)
		hiCR, _, _ := procGetSysColor.Call(colorHighlight)
		hiTxtCR, _, _ := procGetSysColor.Call(colorHighlightText)
		grayCR, _, _ := procGetSysColor.Call(colorGrayText)
		sep := colorrefToARGB(uint32(grayCR))
		return menuPalette{
			bg:        colorrefToARGB(uint32(menuCR)),
			border:    sep,
			text:      colorrefToARGB(uint32(txtCR)),
			muted:     sep,
			hoverBG:   colorrefToARGB(uint32(hiCR)),
			hoverText: colorrefToARGB(uint32(hiTxtCR)),
			accent:    colorrefToARGB(uint32(hiTxtCR)),
			separator: sep,
		}
	}

	isLight := resolveThemeIsLight(cfg.Theme)

	if isLight {
		return menuPalette{
			bg:        0xFFF9FAFC,
			border:    0xFFE2E8F0,
			text:      0xFF111827,
			muted:     0xFF4B5563,
			hoverBG:   0xFFE5E7EB,
			hoverText: 0xFF111827,
			accent:    0xFF0E7490,
			separator: 0xFFE2E8F0,
		}
	}
	return menuPalette{
		bg:        0xFF1F2430,
		border:    0xFF334155,
		text:      0xFFF8FAFC,
		muted:     0xFFCBD5E1,
		hoverBG:   0xFF2E3748,
		hoverText: 0xFFFFFFFF,
		accent:    0xFF22D3EE,
		separator: 0xFF334155,
	}
}

func measurePopupMenu(items []menuItemSpec, dpi uint32, fontName string) menuLayout {
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}
	ml := menuLayout{
		padY:        scaleDPIInt(4, dpi),
		leftGutterW: scaleDPIInt(34, dpi),
		rightArrowW: scaleDPIInt(28, dpi),
		rowH:        scaleDPIInt(30, dpi),
		sepH:        scaleDPIInt(9, dpi),
		fontPx:      scaleDPIInt(14, dpi),
		itemTops:    make([]int32, len(items)),
		itemBottoms: make([]int32, len(items)),
	}

	var gfx, bmp, fRegular, fBold, strFmt uintptr
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
	fRegular, _ = gpCreateFontPx(screenDC, fontName, ml.fontPx, 500)
	if fRegular != 0 {
		defer procGdipDeleteFont.Call(fRegular)
	}
	fBold, _ = gpCreateFontPx(screenDC, fontName, ml.fontPx, 700)
	if fBold != 0 {
		defer procGdipDeleteFont.Call(fBold)
	}
	strFmt, _ = gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if strFmt != 0 {
		defer procGdipDeleteStringFormat.Call(strFmt)
	}

	maxW := scaleDPIInt(188, dpi)
	curY := ml.padY
	for i, it := range items {
		ml.itemTops[i] = curY
		if it.isSep {
			curY += ml.sepH
			ml.itemBottoms[i] = curY
			continue
		}
		curY += ml.rowH
		ml.itemBottoms[i] = curY

		textW := scaleDPIInt(int32(len(it.text))*7, dpi)
		if gfx != 0 && strFmt != 0 {
			font := fRegular
			if it.isDefault && fBold != 0 {
				font = fBold
			}
			if font != 0 {
				if fw, _ := gpMeasureString(gfx, font, strFmt, it.text); fw > 0 {
					textW = int32(math.Ceil(float64(fw))) + scaleDPIInt(6, dpi)
				}
			}
		}
		rowW := ml.leftGutterW + textW + ml.rightArrowW
		if rowW > maxW {
			maxW = rowW
		}
	}
	ml.width = maxW
	ml.height = curY + ml.padY
	return ml
}

func computeMainMenuPos(anchor point, winW, winH int32, mon rect, dpi uint32) (int32, int32) {
	margin := scaleDPIInt(4, dpi)
	x := anchor.X
	y := anchor.Y - winH
	if y < mon.Top+margin {
		y = anchor.Y
	}
	if x+winW > mon.Right-margin {
		x = anchor.X - winW
	}
	if x < mon.Left+margin {
		x = mon.Left + margin
	}
	if x+winW > mon.Right-margin {
		x = mon.Right - margin - winW
	}
	if y+winH > mon.Bottom-margin {
		y = mon.Bottom - margin - winH
	}
	if y < mon.Top+margin {
		y = mon.Top + margin
	}
	return x, y
}

func computeSubmenuPos(parentWinRect, itemLocalRect rect, subW, subH int32, mon rect, padY int32, dpi uint32) (int32, int32) {
	margin := scaleDPIInt(4, dpi)
	overlap := scaleDPIInt(2, dpi)
	x := parentWinRect.Right - overlap
	if x+subW > mon.Right-margin {
		x = parentWinRect.Left - subW + overlap
	}
	if x < mon.Left+margin {
		x = mon.Left + margin
	}
	if x+subW > mon.Right-margin {
		x = mon.Right - margin - subW
	}

	y := parentWinRect.Top + itemLocalRect.Top - padY
	if y+subH > mon.Bottom-margin {
		y = mon.Bottom - margin - subH
	}
	if y < mon.Top+margin {
		y = mon.Top + margin
	}
	return x, y
}

func drawPopupMenuLevel(gfx uintptr, lvl *popupMenuLevel, pal menuPalette, dpi uint32, fontName string, activeParentIdx int) {
	w := lvl.layout.width
	h := lvl.layout.height
	if w <= 0 || h <= 0 {
		return
	}
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}

	_, _, _ = procGdipSetSmoothingMode.Call(gfx, gpSmoothingModeHighQuality)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)

	if bBG, err := gpCreateSolidBrush(pal.bg); err == nil {
		gpFillRoundedRect(gfx, bBG, 0, 0, w, h, 0)
		_, _, _ = procGdipDeleteBrush.Call(bBG)
	}

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}
	fRegular, _ := gpCreateFontPx(screenDC, fontName, lvl.layout.fontPx, 500)
	if fRegular != 0 {
		defer procGdipDeleteFont.Call(fRegular)
	}
	fBold, _ := gpCreateFontPx(screenDC, fontName, lvl.layout.fontPx, 700)
	if fBold != 0 {
		defer procGdipDeleteFont.Call(fBold)
	}
	strFmt, _ := gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if strFmt != 0 {
		defer procGdipDeleteStringFormat.Call(strFmt)
	}

	for i, it := range lvl.items {
		top := lvl.layout.itemTops[i]
		bot := lvl.layout.itemBottoms[i]
		rowH := bot - top

		if it.isSep {
			sepX := scaleDPIInt(10, dpi)
			sepW := w - scaleDPIInt(20, dpi)
			sepY := top + rowH/2
			if sepW > 0 {
				if bSep, err := gpCreateSolidBrush(pal.separator); err == nil {
					gpFillRoundedRect(gfx, bSep, sepX, sepY, sepW, 1, 0)
					_, _, _ = procGdipDeleteBrush.Call(bSep)
				}
			}
			continue
		}

		highlighted := i == lvl.hoveredIdx || i == activeParentIdx
		if highlighted {
			padX := scaleDPIInt(4, dpi)
			padY := scaleDPIInt(1, dpi)
			if bHover, err := gpCreateSolidBrush(pal.hoverBG); err == nil {
				gpFillRoundedRect(gfx, bHover, padX, top+padY, w-padX*2, rowH-padY*2, scaleDPIInt(5, dpi))
				_, _, _ = procGdipDeleteBrush.Call(bHover)
			}
		}

		txtColor := pal.text
		chevColor := pal.muted
		if highlighted {
			txtColor = pal.hoverText
			chevColor = pal.hoverText
		}

		if it.checked {
			cx := lvl.layout.leftGutterW/2 + scaleDPIInt(1, dpi)
			cy := top + rowH/2
			if bIcon, err := gpCreateSolidBrush(pal.accent); err == nil {
				if it.isRadio {
					d := scaleDPIInt(8, dpi)
					_, _, _ = procGdipFillEllipseI.Call(
						gfx, bIcon,
						uintptr(cx-d/2), uintptr(cy-d/2),
						uintptr(d), uintptr(d),
					)
				} else {
					cw := scaleDPIInt(13, dpi)
					ch := scaleDPIInt(10, dpi)
					gpFillScaledPolygon(gfx, bIcon, menuCheckGlyphPoly, 100, cx-cw/2, cy-ch/2, cw, ch)
				}
				_, _, _ = procGdipDeleteBrush.Call(bIcon)
			}
		}

		font := fRegular
		if it.isDefault && fBold != 0 {
			font = fBold
		}
		if font != 0 && strFmt != 0 {
			if bTxt, bErr := gpCreateSolidBrush(txtColor); bErr == nil {
				gpDrawString(gfx, font, strFmt, bTxt, it.text, gpRectF{
					X:      float32(lvl.layout.leftGutterW),
					Y:      float32(top),
					Width:  float32(w - lvl.layout.leftGutterW - lvl.layout.rightArrowW),
					Height: float32(rowH),
				})
				_, _, _ = procGdipDeleteBrush.Call(bTxt)
			}
		}

		if len(it.children) > 0 {
			arrW := scaleDPIInt(7, dpi)
			arrH := scaleDPIInt(12, dpi)
			ax := w - lvl.layout.rightArrowW/2 - arrW/2 - scaleDPIInt(2, dpi)
			ay := top + (rowH-arrH)/2
			if bChev, err := gpCreateSolidBrush(chevColor); err == nil {
				gpFillScaledPolygon(gfx, bChev, menuChevronGlyphPoly, 100, ax, ay, arrW, arrH)
				_, _, _ = procGdipDeleteBrush.Call(bChev)
			}
		}
	}

	if !lvl.hasDWMRound {
		if bBorder, err := gpCreateSolidBrush(pal.border); err == nil {
			gpFillRoundedRect(gfx, bBorder, 0, 0, w, 1, 0)
			gpFillRoundedRect(gfx, bBorder, 0, h-1, w, 1, 0)
			gpFillRoundedRect(gfx, bBorder, 0, 0, 1, h, 0)
			gpFillRoundedRect(gfx, bBorder, w-1, 0, 1, h, 0)
			_, _, _ = procGdipDeleteBrush.Call(bBorder)
		}
	}
}

func (l *popupMenuLevel) ensureDIB() bool {
	if l.memDC != 0 && l.gpBitmap != 0 {
		return true
	}
	w := l.layout.width
	h := l.layout.height
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

	l.memDC = memDC
	l.hBitmap = hBmp
	l.oldBitmap = oldObj
	l.gpBitmap = gpBmp
	return true
}

func (l *popupMenuLevel) releaseDIB() {
	if l.gpBitmap != 0 {
		_, _, _ = procGdipDisposeImage.Call(l.gpBitmap)
		l.gpBitmap = 0
	}
	if l.memDC != 0 {
		if l.oldBitmap != 0 {
			_, _, _ = procSelectObject.Call(l.memDC, l.oldBitmap)
			l.oldBitmap = 0
		}
		_, _, _ = procDeleteDC.Call(l.memDC)
		l.memDC = 0
	}
	if l.hBitmap != 0 {
		_, _, _ = procDeleteObject.Call(l.hBitmap)
		l.hBitmap = 0
	}
}

func (l *popupMenuLevel) destroy() {
	l.releaseDIB()
	if l.hwnd != 0 {
		_, _, _ = procDestroyWindow.Call(l.hwnd)
		l.hwnd = 0
	}
}

func (l *popupMenuLevel) hitTest(y int32) int {
	for i, it := range l.items {
		if !it.isSep && y >= l.layout.itemTops[i] && y < l.layout.itemBottoms[i] {
			return i
		}
	}
	return -1
}

func (l *popupMenuLevel) paint(hdc uintptr, pal menuPalette, dpi uint32, fontName string, activeParentIdx int) {
	if !l.ensureDIB() {
		return
	}
	var gfx uintptr
	st, _, _ := procGdipGetImageGraphicsContext.Call(l.gpBitmap, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		return
	}
	drawPopupMenuLevel(gfx, l, pal, dpi, fontName, activeParentIdx)
	_, _, _ = procGdipDeleteGraphics.Call(gfx)

	_, _, _ = procBitBlt.Call(
		hdc, 0, 0, uintptr(l.layout.width), uintptr(l.layout.height),
		l.memDC, 0, 0, srcCopy,
	)
}

func ensureMenuWindowClass() bool {
	menuClassOnce.Do(func() {
		menuWndProcCB = windows.NewCallback(popupMenuWndProc)
		clsPtr, err := windows.UTF16PtrFromString(menuClassName)
		if err != nil {
			return
		}
		hCursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		wcx := wndClassExW{
			CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			Style:         csHRedraw | csVRedraw | csDropShadow,
			LpfnWndProc:   menuWndProcCB,
			HInstance:     getModuleHandle(),
			HCursor:       hCursor,
			LpszClassName: clsPtr,
		}
		_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))
	})
	return menuWndProcCB != 0
}

func createPopupMenuLevel(items []menuItemSpec, dpi uint32, fontName string, pal menuPalette, x, y int32, noActivate bool) (*popupMenuLevel, error) {
	if !ensureMenuWindowClass() {
		return nil, windows.ERROR_INVALID_WINDOW_HANDLE
	}
	clsPtr, err := windows.UTF16PtrFromString(menuClassName)
	if err != nil {
		return nil, err
	}
	ml := measurePopupMenu(items, dpi, fontName)

	exStyle := uintptr(wsExToolWindow | wsExTopmost)
	if noActivate {
		exStyle |= wsExNoActivate
	}
	hwnd, _, callErr := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(clsPtr)),
		0,
		uintptr(wsPopup),
		uintptr(x), uintptr(y), uintptr(ml.width), uintptr(ml.height),
		0, 0, getModuleHandle(), 0,
	)
	if hwnd == 0 {
		return nil, callErr
	}

	hasRound := false
	if procDwmSetWindowAttribute.Find() == nil {
		corner := uint32(dwmWcpRound)
		hr, _, _ := procDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&corner)), 4)
		hasRound = hr == 0
		borderCR := argbToCOLORREF(pal.border)
		_, _, _ = procDwmSetWindowAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&borderCR)), 4)
	}

	return &popupMenuLevel{
		hwnd:        hwnd,
		items:       items,
		layout:      ml,
		hoveredIdx:  -1,
		hasDWMRound: hasRound,
	}, nil
}

func (s *popupMenuSession) levelForHWND(hwnd uintptr) (*popupMenuLevel, bool) {
	if s.main != nil && hwnd == s.main.hwnd {
		return s.main, true
	}
	if s.sub != nil && hwnd == s.sub.hwnd {
		return s.sub, false
	}
	return nil, false
}

func (s *popupMenuSession) isOwnMenuHWND(hwnd uintptr) bool {
	return (s.main != nil && hwnd == s.main.hwnd) || (s.sub != nil && hwnd == s.sub.hwnd)
}

func (s *popupMenuSession) invalidateMain() {
	if s.main != nil && s.main.hwnd != 0 {
		_, _, _ = procInvalidateRect.Call(s.main.hwnd, 0, 0)
	}
}

func (s *popupMenuSession) closeSubmenu() {
	if s.main != nil && s.main.hwnd != 0 {
		_, _, _ = procKillTimer.Call(s.main.hwnd, timerMenuSubOpenID)
		_, _, _ = procKillTimer.Call(s.main.hwnd, timerMenuSubCloseID)
	}
	if s.sub != nil {
		s.sub.destroy()
		s.sub = nil
	}
	if s.openSubIdx != -1 {
		s.openSubIdx = -1
		s.invalidateMain()
	}
}

func (s *popupMenuSession) openSubmenuForIdx(idx int, selectFirst bool) {
	if s.main == nil || idx < 0 || idx >= len(s.main.items) {
		return
	}
	spec := s.main.items[idx]
	if len(spec.children) == 0 {
		return
	}
	if s.sub != nil && s.openSubIdx == idx {
		if selectFirst {
			s.moveSelection(s.sub, 1)
		}
		return
	}
	s.closeSubmenu()

	parentRect, ok := getWindowRect(s.main.hwnd)
	if !ok {
		return
	}
	subLayout := measurePopupMenu(spec.children, s.dpi, s.fontName)
	mon, ok := monitorBoundsForWindow(s.main.hwnd)
	if !ok {
		mon = rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	}
	itemLocal := rect{
		Left:   0,
		Top:    s.main.layout.itemTops[idx],
		Right:  s.main.layout.width,
		Bottom: s.main.layout.itemBottoms[idx],
	}
	sx, sy := computeSubmenuPos(parentRect, itemLocal, subLayout.width, subLayout.height, mon, s.main.layout.padY, s.dpi)
	sub, err := createPopupMenuLevel(spec.children, s.dpi, s.fontName, s.pal, sx, sy, true)
	if err != nil {
		return
	}
	s.sub = sub
	s.openSubIdx = idx
	if selectFirst {
		for i, ch := range sub.items {
			if !ch.isSep {
				sub.hoveredIdx = i
				break
			}
		}
	}
	s.invalidateMain()
	_, _, _ = procSetWindowPos.Call(sub.hwnd, hwndTopmost, uintptr(sx), uintptr(sy), uintptr(sub.layout.width), uintptr(sub.layout.height), swpNoActivate|swpShowWindow)
}

func (s *popupMenuSession) moveSelection(lvl *popupMenuLevel, dir int) {
	if lvl == nil || len(lvl.items) == 0 {
		return
	}
	start := lvl.hoveredIdx
	n := len(lvl.items)
	for step := 1; step <= n; step++ {
		idx := (start + dir*step + n*2) % n
		if start < 0 && dir < 0 {
			idx = (n - step + n) % n
		} else if start < 0 && dir > 0 {
			idx = (step - 1) % n
		}
		if !lvl.items[idx].isSep {
			lvl.hoveredIdx = idx
			if lvl.hwnd != 0 {
				_, _, _ = procInvalidateRect.Call(lvl.hwnd, 0, 0)
			}
			return
		}
	}
}

func (s *popupMenuSession) dismiss() {
	if s.done {
		return
	}
	s.done = true
	s.closeSubmenu()
	if s.main != nil {
		if s.main.hwnd != 0 {
			_, _, _ = procPostMessageW.Call(s.main.hwnd, wmNull, 0, 0)
		}
	}
}

func popupMenuWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	activeMenuMu.Lock()
	sess := activeMenu
	activeMenuMu.Unlock()

	if sess == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}
	lvl, isMain := sess.levelForHWND(hwnd)
	if lvl == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return ret
	}

	switch message {
	case wmMouseActivate:
		if !isMain {
			return maNoActivate
		}

	case wmEraseBkgnd:
		return 1

	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			activeParent := -1
			if isMain {
				activeParent = sess.openSubIdx
			}
			lvl.paint(hdc, sess.pal, sess.dpi, sess.fontName, activeParent)
			_, _, _ = procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		return 0

	case wmMouseMove:
		if !lvl.tracking {
			tme := trackMouseEventStruct{
				CbSize:    uint32(unsafe.Sizeof(trackMouseEventStruct{})),
				DwFlags:   tmeLeave,
				HwndTrack: hwnd,
			}
			ok, _, _ := procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			lvl.tracking = ok != 0
		}
		if !isMain && sess.main != nil && sess.main.hwnd != 0 {
			_, _, _ = procKillTimer.Call(sess.main.hwnd, timerMenuSubCloseID)
		}
		y := int32(int16((lParam >> 16) & 0xFFFF))
		idx := lvl.hitTest(y)
		if idx != lvl.hoveredIdx {
			lvl.hoveredIdx = idx
			_, _, _ = procInvalidateRect.Call(hwnd, 0, 0)
			if isMain {
				_, _, _ = procKillTimer.Call(sess.main.hwnd, timerMenuSubOpenID)
				_, _, _ = procKillTimer.Call(sess.main.hwnd, timerMenuSubCloseID)
				if idx >= 0 && len(lvl.items[idx].children) > 0 {
					if sess.openSubIdx != idx {
						_, _, _ = procSetTimer.Call(sess.main.hwnd, timerMenuSubOpenID, menuHoverDelayMs, 0)
					}
				} else if sess.sub != nil {
					_, _, _ = procSetTimer.Call(sess.main.hwnd, timerMenuSubCloseID, menuHoverDelayMs, 0)
				}
			}
		}
		return 0

	case wmMouseLeave:
		lvl.tracking = false
		if lvl.hoveredIdx != -1 {
			lvl.hoveredIdx = -1
			_, _, _ = procInvalidateRect.Call(hwnd, 0, 0)
		}
		if isMain && sess.sub != nil {
			_, _, _ = procKillTimer.Call(sess.main.hwnd, timerMenuSubOpenID)
		}
		return 0

	case wmTimer:
		if isMain {
			switch wParam {
			case timerMenuSubOpenID:
				_, _, _ = procKillTimer.Call(sess.main.hwnd, timerMenuSubOpenID)
				if lvl.hoveredIdx >= 0 && len(lvl.items[lvl.hoveredIdx].children) > 0 {
					sess.openSubmenuForIdx(lvl.hoveredIdx, false)
				}
			case timerMenuSubCloseID:
				_, _, _ = procKillTimer.Call(sess.main.hwnd, timerMenuSubCloseID)
				if sess.sub != nil && sess.sub.hoveredIdx < 0 && lvl.hoveredIdx != sess.openSubIdx {
					sess.closeSubmenu()
				}
			}
		}
		return 0

	case wmLButtonDown, wmRButtonDown:
		y := int32(int16((lParam >> 16) & 0xFFFF))
		idx := lvl.hitTest(y)
		if isMain && idx >= 0 && len(lvl.items[idx].children) > 0 {
			sess.openSubmenuForIdx(idx, false)
		}
		return 0

	case wmLButtonUp, wmRButtonUp:
		y := int32(int16((lParam >> 16) & 0xFFFF))
		idx := lvl.hitTest(y)
		if idx >= 0 {
			spec := lvl.items[idx]
			if len(spec.children) > 0 {
				if isMain {
					sess.openSubmenuForIdx(idx, false)
				}
				return 0
			}
			if !spec.isSep && spec.id != 0 {
				sess.chosenID = spec.id
				sess.dismiss()
			}
		}
		return 0

	case wmKeyDown:
		switch wParam {
		case vkEscape:
			if sess.sub != nil {
				sess.closeSubmenu()
			} else {
				sess.dismiss()
			}
		case vkLeft:
			if sess.sub != nil {
				sess.closeSubmenu()
			}
		case vkRight:
			if sess.sub == nil && sess.main.hoveredIdx >= 0 && len(sess.main.items[sess.main.hoveredIdx].children) > 0 {
				sess.openSubmenuForIdx(sess.main.hoveredIdx, true)
			}
		case vkUp:
			target := sess.main
			if sess.sub != nil {
				target = sess.sub
			}
			sess.moveSelection(target, -1)
		case vkDown:
			target := sess.main
			if sess.sub != nil {
				target = sess.sub
			}
			sess.moveSelection(target, 1)
		case vkReturn, vkSpace:
			target := sess.main
			targetIsMain := true
			if sess.sub != nil {
				target = sess.sub
				targetIsMain = false
			}
			if target.hoveredIdx >= 0 && target.hoveredIdx < len(target.items) {
				spec := target.items[target.hoveredIdx]
				if len(spec.children) > 0 {
					if targetIsMain {
						sess.openSubmenuForIdx(target.hoveredIdx, true)
					}
				} else if !spec.isSep && spec.id != 0 {
					sess.chosenID = spec.id
					sess.dismiss()
				}
			}
		}
		return 0

	case wmActivate:
		if isMain && loWord(wParam) == waInactive && !sess.isOwnMenuHWND(lParam) {
			sess.dismiss()
		}
		return 0

	case wmKillFocus:
		if isMain && !sess.isOwnMenuHWND(wParam) {
			sess.dismiss()
		}
		return 0

	case wmCancelMode:
		sess.dismiss()
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return ret
}

func showCustomPopupMenu(ownerHWND uintptr, anchor point, dpi uint32, fontName string, pal menuPalette, items []menuItemSpec) uint32 {
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}
	mon, ok := monitorBoundsForPoint(anchor)
	if !ok {
		mon = rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	}
	mainLayout := measurePopupMenu(items, dpi, fontName)
	mx, my := computeMainMenuPos(anchor, mainLayout.width, mainLayout.height, mon, dpi)

	sess := &popupMenuSession{
		ownerHWND:  ownerHWND,
		dpi:        dpi,
		fontName:   fontName,
		pal:        pal,
		openSubIdx: -1,
	}

	mainLvl, err := createPopupMenuLevel(items, dpi, fontName, pal, mx, my, false)
	if err != nil {
		return 0
	}
	sess.main = mainLvl

	activeMenuMu.Lock()
	prev := activeMenu
	activeMenu = sess
	activeMenuMu.Unlock()
	if prev != nil {
		prev.dismiss()
	}

	defer func() {
		sess.closeSubmenu()
		if sess.main != nil {
			sess.main.destroy()
		}
		activeMenuMu.Lock()
		if activeMenu == sess {
			activeMenu = nil
		}
		activeMenuMu.Unlock()
	}()

	_, _, _ = procSetForegroundWindow.Call(mainLvl.hwnd)
	_, _, _ = procSetWindowPos.Call(mainLvl.hwnd, hwndTopmost, uintptr(mx), uintptr(my), uintptr(mainLvl.layout.width), uintptr(mainLvl.layout.height), swpShowWindow)
	_, _, _ = procInvalidateRect.Call(mainLvl.hwnd, 0, 0)

	var m msg
	for !sess.done {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		r := int32(ret)
		if r <= 0 {
			if r == 0 {
				_, _, _ = procPostQuitMessage.Call(m.WParam)
			}
			sess.dismiss()
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return sess.chosenID
}

func dismissActivePopupMenu() {
	activeMenuMu.Lock()
	sess := activeMenu
	activeMenuMu.Unlock()
	if sess != nil {
		sess.dismiss()
	}
}
