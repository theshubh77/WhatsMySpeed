package ui

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
)

const (
	trayIconID uint32 = 1

	cmdOpenWindow   uint32 = 1000
	cmdShowBoth     uint32 = 1001
	cmdDownloadOnly uint32 = 1002
	cmdUploadOnly   uint32 = 1003
	cmdHideSpeed    uint32 = 1004
	cmdUnitBytes    uint32 = 1005
	cmdUnitBits     uint32 = 1006
	cmdLabelArrows  uint32 = 1007
	cmdLabelUD      uint32 = 1008
	cmdLabelWords   uint32 = 1009
	cmdLayoutStackU uint32 = 1010
	cmdLayoutStackD uint32 = 1011
	cmdLayoutInline uint32 = 1012
	cmdLayoutComp   uint32 = 1013
	cmdRunSpeedTest uint32 = 1014
	cmdAutoStart    uint32 = 1015
	cmdThemeSystem  uint32 = 1016
	cmdThemeDark    uint32 = 1017
	cmdThemeLight   uint32 = 1018
	cmdExit         uint32 = 1019
)

type ownerMenuItem struct {
	id         uint32
	text       string
	isSep      bool
	isDefault  bool
	isRadio    bool
	checked    bool
	hasSubmenu bool
	width      uint32
	height     uint32
}

type menuPalette struct {
	bg        uint32
	text      uint32
	muted     uint32
	hoverBG   uint32
	hoverText uint32
	accent    uint32
	separator uint32
}

// TrayManager owns the Shell_NotifyIconW icon and popup context menu.
type TrayManager struct {
	hwnd        uintptr
	hIcon       uintptr
	dpi         uint32
	isLight     bool
	trayARGB    uint32
	added       bool
	lastTip     string
	menuItems   []ownerMenuItem
	menuDPI     uint32
	menuFont    string
	menuColors  menuPalette
}

// NewTrayManager creates a TrayManager bound to the message window hwnd.
func NewTrayManager(hwnd uintptr) *TrayManager {
	return &TrayManager{hwnd: hwnd}
}

func (t *TrayManager) buildNID(flags uint32, tip string) notifyIconDataW {
	var nid notifyIconDataW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UID = trayIconID
	nid.UFlags = flags
	nid.UCallbackMessage = wmTrayCallback
	nid.HIcon = t.hIcon
	if tip != "" {
		copyUTF16Fixed(nid.SzTip[:], tip)
	}
	return nid
}

// SyncIcon ensures the system tray icon is registered and sized for the current DPI.
func (t *TrayManager) SyncIcon(dpi uint32, colors ThemeColors, forceReadd bool) error {
	needNewIcon := t.hIcon == 0 || t.dpi != dpi || t.isLight != colors.IsLight || t.trayARGB != colors.TrayIconARGB
	var oldIcon uintptr

	if needNewIcon {
		w, h := getSmallIconMetrics(dpi)
		newIcon, err := CreatePNGHICON(assets.AppLogoPNG, w, h)
		if err != nil {
			newIcon, err = CreateTrayHICON(assets.IconGaugeBold, dpi, colors.TrayIconARGB)
			if err != nil {
				return err
			}
		}
		oldIcon = t.hIcon
		t.hIcon = newIcon
		t.dpi = dpi
		t.isLight = colors.IsLight
		t.trayARGB = colors.TrayIconARGB
	}

	if !t.added || forceReadd {
		tip := t.lastTip
		if tip == "" {
			tip = config.AppName
		}
		nid := t.buildNID(nifMessage|nifIcon|nifTip, tip)
		ok, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
		if ok != 0 {
			t.added = true
		} else {
			t.added = false
		}
	} else if needNewIcon {
		nid := t.buildNID(nifIcon, "")
		ok, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
		if ok == 0 {
			tip := t.lastTip
			if tip == "" {
				tip = config.AppName
			}
			addNid := t.buildNID(nifMessage|nifIcon|nifTip, tip)
			addOK, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&addNid)))
			t.added = addOK != 0
		}
	}

	if oldIcon != 0 {
		_, _, _ = procDestroyIcon.Call(oldIcon)
	}
	return nil
}

// UpdateTooltip updates the tray hover tooltip when text changes.
func (t *TrayManager) UpdateTooltip(tip string) {
	if !t.added || tip == t.lastTip {
		return
	}
	nid := t.buildNID(nifTip, tip)
	ok, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
	if ok != 0 {
		t.lastTip = tip
		return
	}
	t.added = false
	t.lastTip = tip
	_ = t.SyncIcon(t.dpi, ThemeColors{IsLight: t.isLight, TrayIconARGB: t.trayARGB}, true)
}

// Remove deletes the system tray icon and frees its HICON resource.
func (t *TrayManager) Remove() {
	if t.added {
		nid := t.buildNID(0, "")
		_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		t.added = false
	}
	if t.hIcon != 0 {
		_, _, _ = procDestroyIcon.Call(t.hIcon)
		t.hIcon = 0
	}
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
)

func argbToCOLORREF(argb uint32) uint32 {
	r := (argb >> 16) & 0xFF
	g := (argb >> 8) & 0xFF
	b := argb & 0xFF
	return (b << 16) | (g << 8) | r
}

func resolveMenuPalette(cfg config.Config) menuPalette {
	if active, _, _ := DetectHighContrast(); active && procGetSysColor.Find() == nil {
		menuCR, _, _ := procGetSysColor.Call(colorMenu)
		txtCR, _, _ := procGetSysColor.Call(colorMenuText)
		hiCR, _, _ := procGetSysColor.Call(colorHighlight)
		hiTxtCR, _, _ := procGetSysColor.Call(colorHighlightText)
		grayCR, _, _ := procGetSysColor.Call(colorGrayText)
		return menuPalette{
			bg:        colorrefToARGB(uint32(menuCR)),
			text:      colorrefToARGB(uint32(txtCR)),
			muted:     colorrefToARGB(uint32(grayCR)),
			hoverBG:   colorrefToARGB(uint32(hiCR)),
			hoverText: colorrefToARGB(uint32(hiTxtCR)),
			accent:    colorrefToARGB(uint32(hiTxtCR)),
			separator: colorrefToARGB(uint32(grayCR)),
		}
	}

	isLight := DetectAppsLightTheme()
	if cfg.Theme == config.ThemeLight {
		isLight = true
	} else if cfg.Theme == config.ThemeDark {
		isLight = false
	}

	if isLight {
		return menuPalette{
			bg:        0xFFF9FAFC,
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
		text:      0xFFF8FAFC,
		muted:     0xFFCBD5E1,
		hoverBG:   0xFF2E3748,
		hoverText: 0xFFFFFFFF,
		accent:    0xFF22D3EE,
		separator: 0xFF334155,
	}
}

func insertOwnerMenuItem(hMenu uintptr, pos uint32, item ownerMenuItem, subMenu uintptr, idx int) error {
	var mii menuItemInfoW
	mii.CbSize = uint32(unsafe.Sizeof(mii))
	mii.FMask = miimFType | miimID | miimState | miimData
	mii.FType = mftOwnerDraw
	if item.isSep {
		mii.FType |= mftSeparator
	}
	if item.isRadio {
		mii.FType |= mftRadioCheck
	}
	mii.WID = item.id
	mii.FState = checkState(item.checked)
	mii.DwItemData = uintptr(idx + 1)

	if subMenu != 0 {
		mii.FMask |= miimSubMenu
		mii.HSubMenu = subMenu
	}

	ok, _, _ := procInsertMenuItemW.Call(hMenu, uintptr(pos), 1, uintptr(unsafe.Pointer(&mii)))
	if ok == 0 {
		return fmt.Errorf("InsertMenuItemW(%q) failed", item.text)
	}
	return nil
}

func checkState(checked bool) uint32 {
	if checked {
		return mfsChecked
	}
	return mfsUnchecked
}

func applyMenuBackgroundBrush(hMenu uintptr, bgBrush uintptr) {
	if hMenu == 0 || bgBrush == 0 || procSetMenuInfo.Find() != nil {
		return
	}
	mi := menuInfoW{
		CbSize:  uint32(unsafe.Sizeof(menuInfoW{})),
		FMask:   mimBackground | mimApplyToSubmenus,
		HbrBack: bgBrush,
	}
	_, _, _ = procSetMenuInfo.Call(hMenu, uintptr(unsafe.Pointer(&mi)))
}

func (t *TrayManager) measureAllMenuItems(dpi uint32, fontName string) {
	if len(t.menuItems) == 0 {
		return
	}
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}

	leftGutterW := scaleDPIInt(34, dpi)
	rightArrowW := scaleDPIInt(28, dpi)
	rowH := scaleDPIInt(30, dpi)
	sepH := scaleDPIInt(9, dpi)
	fontPx := scaleDPIInt(14, dpi)

	cxCheck := scaleDPIInt(15, dpi)
	if procGetSystemMetricsForDpi.Find() == nil {
		if v, _, _ := procGetSystemMetricsForDpi.Call(smCxMenuCheck, uintptr(dpi)); v > 0 {
			cxCheck = int32(v)
		}
	} else if v, _, _ := procGetSystemMetrics.Call(smCxMenuCheck); v > 0 {
		cxCheck = int32(v)
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
	fRegular, _ = gpCreateFontPx(screenDC, fontName, fontPx, 500)
	if fRegular != 0 {
		defer procGdipDeleteFont.Call(fRegular)
	}
	fBold, _ = gpCreateFontPx(screenDC, fontName, fontPx, 700)
	if fBold != 0 {
		defer procGdipDeleteFont.Call(fBold)
	}
	strFmt, _ = gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter)
	if strFmt != 0 {
		defer procGdipDeleteStringFormat.Call(strFmt)
	}

	minDesiredW := scaleDPIInt(170, dpi)
	for i := range t.menuItems {
		it := &t.menuItems[i]
		if it.isSep {
			w := minDesiredW - cxCheck
			if w < 60 {
				w = 60
			}
			it.width = uint32(w)
			it.height = uint32(sepH)
			continue
		}

		textW := scaleDPIInt(int32(len(it.text))*7, dpi)
		if gfx != 0 && strFmt != 0 {
			font := fRegular
			if it.isDefault && fBold != 0 {
				font = fBold
			}
			if font != 0 {
				if fw, _ := gpMeasureString(gfx, font, strFmt, it.text); fw > 0 {
					textW = int32(math.Ceil(float64(fw))) + scaleDPIInt(4, dpi)
				}
			}
		}

		totalW := leftGutterW + textW + scaleDPIInt(8, dpi) + rightArrowW
		if totalW < minDesiredW {
			totalW = minDesiredW
		}
		adjW := totalW - cxCheck
		if adjW < scaleDPIInt(80, dpi) {
			adjW = scaleDPIInt(80, dpi)
		}
		it.width = uint32(adjW)
		it.height = uint32(rowH)
	}
}

// HandleMenuMeasureItem populates owner-drawn menu item dimensions on WM_MEASUREITEM.
func (t *TrayManager) HandleMenuMeasureItem(lParam uintptr) bool {
	mis, ok := copyMeasureItemFromLParam(lParam)
	if !ok || mis.CtlType != odtMenu || mis.ItemData == 0 {
		return false
	}
	idx := int(mis.ItemData) - 1
	if idx < 0 || idx >= len(t.menuItems) {
		return false
	}
	item := t.menuItems[idx]
	mis.ItemWidth = item.width
	mis.ItemHeight = item.height
	return writeMeasureItemToLParam(lParam, mis)
}

// HandleMenuDrawItem renders an owner-drawn context menu row with larger crisp icons on WM_DRAWITEM.
func (t *TrayManager) HandleMenuDrawItem(lParam uintptr) bool {
	dis, ok := copyDrawItemFromLParam(lParam)
	if !ok || dis.CtlType != odtMenu || dis.HDC == 0 || dis.ItemData == 0 {
		return false
	}
	idx := int(dis.ItemData) - 1
	if idx < 0 || idx >= len(t.menuItems) {
		return false
	}
	item := t.menuItems[idx]

	var gfx uintptr
	st, _, _ := procGdipCreateFromHDC.Call(dis.HDC, uintptr(unsafe.Pointer(&gfx)))
	if st != 0 || gfx == 0 {
		return false
	}
	defer procGdipDeleteGraphics.Call(gfx)

	dpi := t.menuDPI
	if dpi == 0 {
		dpi = 96
	}
	t.drawOwnerMenuItem(gfx, dis.RcItem, dpi, item, (dis.ItemState&odsSelected) != 0, (dis.ItemState&odsChecked) != 0)
	// Block user32 from painting its tiny legacy submenu arrow after WM_DRAWITEM.
	_, _, _ = procExcludeClipRect.Call(dis.HDC, uintptr(dis.RcItem.Left), uintptr(dis.RcItem.Top), uintptr(dis.RcItem.Right), uintptr(dis.RcItem.Bottom))
	return true
}

func (t *TrayManager) drawOwnerMenuItem(gfx uintptr, rc rect, dpi uint32, item ownerMenuItem, selected, checked bool) {
	w := rc.Width()
	h := rc.Height()
	if w <= 0 || h <= 0 {
		return
	}
	pal := t.menuColors
	if pal.bg == 0 {
		pal = resolveMenuPalette(config.Default())
	}

	_, _, _ = procGdipSetSmoothingMode.Call(gfx, gpSmoothingModeHighQuality)
	_, _, _ = procGdipSetTextRenderingHint.Call(gfx, gpTextRenderingHintAntiAliasGridFit)

	if bBG, err := gpCreateSolidBrush(pal.bg); err == nil {
		gpFillRoundedRect(gfx, bBG, rc.Left, rc.Top, w, h, 0)
		_, _, _ = procGdipDeleteBrush.Call(bBG)
	}

	if item.isSep {
		sepX := rc.Left + scaleDPIInt(10, dpi)
		sepW := w - scaleDPIInt(20, dpi)
		sepY := rc.Top + h/2
		if sepW > 0 {
			if bSep, err := gpCreateSolidBrush(pal.separator); err == nil {
				gpFillRoundedRect(gfx, bSep, sepX, sepY, sepW, 1, 0)
				_, _, _ = procGdipDeleteBrush.Call(bSep)
			}
		}
		return
	}

	if selected {
		padX := scaleDPIInt(4, dpi)
		padY := scaleDPIInt(1, dpi)
		if bHover, err := gpCreateSolidBrush(pal.hoverBG); err == nil {
			gpFillRoundedRect(gfx, bHover, rc.Left+padX, rc.Top+padY, w-padX*2, h-padY*2, scaleDPIInt(5, dpi))
			_, _, _ = procGdipDeleteBrush.Call(bHover)
		}
	}

	leftGutterW := scaleDPIInt(34, dpi)
	rightArrowW := scaleDPIInt(28, dpi)
	txtColor := pal.text
	iconColor := pal.muted
	if selected {
		txtColor = pal.hoverText
		iconColor = pal.hoverText
	}
	if item.checked || checked {
		iconColor = pal.accent
	}

	// Left gutter check or radio dot, rendered larger and crisper than legacy Win32 menu glyphs.
	if item.checked || checked {
		cx := rc.Left + leftGutterW/2 + scaleDPIInt(1, dpi)
		cy := rc.Top + h/2
		if bIcon, err := gpCreateSolidBrush(iconColor); err == nil {
			if item.isRadio {
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

	fontName := t.menuFont
	if fontName == "" {
		fontName = ResolveTaskbarFontName()
	}
	weight := int32(500)
	if item.isDefault {
		weight = 700
	}
	fontPx := scaleDPIInt(14, dpi)
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		defer procReleaseDC.Call(0, screenDC)
	}
	if gpFont, err := gpCreateFontPx(screenDC, fontName, fontPx, weight); err == nil {
		defer procGdipDeleteFont.Call(gpFont)
		if strFmt, sErr := gpCreateTypographicStringFormat(gpStringAlignmentNear, gpStringAlignmentCenter); sErr == nil {
			defer procGdipDeleteStringFormat.Call(strFmt)
			if bTxt, bErr := gpCreateSolidBrush(txtColor); bErr == nil {
				gpDrawString(gfx, gpFont, strFmt, bTxt, item.text, gpRectF{
					X:      float32(rc.Left + leftGutterW),
					Y:      float32(rc.Top),
					Width:  float32(w - leftGutterW - rightArrowW),
					Height: float32(h),
				})
				_, _, _ = procGdipDeleteBrush.Call(bTxt)
			}
		}
	}

	if item.hasSubmenu {
		arrW := scaleDPIInt(7, dpi)
		arrH := scaleDPIInt(12, dpi)
		ax := rc.Right - rightArrowW/2 - scaleDPIInt(1, dpi) - arrW/2
		ay := rc.Top + (h-arrH)/2
		chevColor := pal.muted
		if selected {
			chevColor = pal.hoverText
		}
		if bChev, err := gpCreateSolidBrush(chevColor); err == nil {
			gpFillScaledPolygon(gfx, bChev, menuChevronGlyphPoly, 100, ax, ay, arrW, arrH)
			_, _, _ = procGdipDeleteBrush.Call(bChev)
		}
	}
}

// ShowContextMenu displays the native Win32 popup menu at the current cursor position
// and returns the selected command ID (or 0 if dismissed).
func (t *TrayManager) ShowContextMenu(cfg config.Config, autoStart bool) uint32 {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return 0
	}
	defer procDestroyMenu.Call(hMenu)

	t.menuItems = t.menuItems[:0]
	defer func() {
		t.menuItems = nil
	}()

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	dpi := t.dpi
	if dpi == 0 {
		dpi = getSystemDPI()
	}
	if tb := findPrimaryTaskbarWindow(); tb != 0 {
		if tbDPI := getWindowDPI(tb); tbDPI > 0 {
			dpi = tbDPI
		}
	}
	t.menuDPI = dpi
	if t.menuFont == "" {
		t.menuFont = ResolveTaskbarFontName()
	}
	t.menuColors = resolveMenuPalette(cfg)

	addTo := func(targetMenu uintptr, pos *uint32, item ownerMenuItem, sub uintptr) {
		idx := len(t.menuItems)
		t.menuItems = append(t.menuItems, item)
		_ = insertOwnerMenuItem(targetMenu, *pos, item, sub, idx)
		*pos++
	}

	labelSub, _, _ := procCreatePopupMenu.Call()
	if labelSub != 0 {
		p := uint32(0)
		addTo(labelSub, &p, ownerMenuItem{id: cmdLabelArrows, text: "Arrows (↑ ↓)", isRadio: true, checked: cfg.LabelStyle == config.LabelArrows}, 0)
		addTo(labelSub, &p, ownerMenuItem{id: cmdLabelUD, text: "U / D", isRadio: true, checked: cfg.LabelStyle == config.LabelUD}, 0)
		addTo(labelSub, &p, ownerMenuItem{id: cmdLabelWords, text: "Up / Down", isRadio: true, checked: cfg.LabelStyle == config.LabelWords}, 0)
	}

	layoutSub, _, _ := procCreatePopupMenu.Call()
	if layoutSub != 0 {
		p := uint32(0)
		addTo(layoutSub, &p, ownerMenuItem{id: cmdLayoutStackU, text: "Stacked (Up / Down)", isRadio: true, checked: cfg.Layout == config.LayoutStackedUp}, 0)
		addTo(layoutSub, &p, ownerMenuItem{id: cmdLayoutStackD, text: "Stacked (Down / Up)", isRadio: true, checked: cfg.Layout == config.LayoutStackedDown}, 0)
		addTo(layoutSub, &p, ownerMenuItem{id: cmdLayoutInline, text: "Inline (Side by Side)", isRadio: true, checked: cfg.Layout == config.LayoutInline}, 0)
		addTo(layoutSub, &p, ownerMenuItem{id: cmdLayoutComp, text: "Compact", isRadio: true, checked: cfg.Layout == config.LayoutCompact}, 0)
	}

	unitSub, _, _ := procCreatePopupMenu.Call()
	if unitSub != 0 {
		p := uint32(0)
		addTo(unitSub, &p, ownerMenuItem{id: cmdUnitBytes, text: "Bytes/s (KB/s, MB/s)", isRadio: true, checked: cfg.SpeedUnit == config.UnitBytes}, 0)
		addTo(unitSub, &p, ownerMenuItem{id: cmdUnitBits, text: "Bits/s (Kbps, Mbps)", isRadio: true, checked: cfg.SpeedUnit == config.UnitBits}, 0)
	}

	themeSub, _, _ := procCreatePopupMenu.Call()
	if themeSub != 0 {
		p := uint32(0)
		addTo(themeSub, &p, ownerMenuItem{id: cmdThemeSystem, text: "System (Auto)", isRadio: true, checked: cfg.Theme == config.ThemeSystem}, 0)
		addTo(themeSub, &p, ownerMenuItem{id: cmdThemeDark, text: "Dark", isRadio: true, checked: cfg.Theme == config.ThemeDark}, 0)
		addTo(themeSub, &p, ownerMenuItem{id: cmdThemeLight, text: "Light", isRadio: true, checked: cfg.Theme == config.ThemeLight}, 0)
	}

	pos := uint32(0)
	addMain := func(item ownerMenuItem, sub uintptr) {
		addTo(hMenu, &pos, item, sub)
	}

	addMain(ownerMenuItem{id: cmdOpenWindow, text: "Open WhatsMySpeed", isDefault: true}, 0)
	addMain(ownerMenuItem{id: cmdRunSpeedTest, text: "Run Speed Test..."}, 0)
	addMain(ownerMenuItem{isSep: true}, 0)
	addMain(ownerMenuItem{id: cmdShowBoth, text: "Show Both", isRadio: true, checked: cfg.DisplayMode == config.ModeBoth}, 0)
	addMain(ownerMenuItem{id: cmdDownloadOnly, text: "Download Only", isRadio: true, checked: cfg.DisplayMode == config.ModeDownload}, 0)
	addMain(ownerMenuItem{id: cmdUploadOnly, text: "Upload Only", isRadio: true, checked: cfg.DisplayMode == config.ModeUpload}, 0)
	addMain(ownerMenuItem{id: cmdHideSpeed, text: "Hide Speed", isRadio: true, checked: cfg.DisplayMode == config.ModeHide}, 0)
	addMain(ownerMenuItem{isSep: true}, 0)
	if labelSub != 0 {
		addMain(ownerMenuItem{text: "Label Style", hasSubmenu: true}, labelSub)
	}
	if layoutSub != 0 {
		addMain(ownerMenuItem{text: "Layout", hasSubmenu: true}, layoutSub)
	}
	if unitSub != 0 {
		addMain(ownerMenuItem{text: "Speed Unit", hasSubmenu: true}, unitSub)
	}
	if themeSub != 0 {
		addMain(ownerMenuItem{text: "Theme", hasSubmenu: true}, themeSub)
	}
	addMain(ownerMenuItem{isSep: true}, 0)
	addMain(ownerMenuItem{id: cmdAutoStart, text: "Start with Windows", checked: autoStart}, 0)
	addMain(ownerMenuItem{isSep: true}, 0)
	addMain(ownerMenuItem{id: cmdExit, text: "Exit"}, 0)

	t.measureAllMenuItems(dpi, t.menuFont)

	if procCreateSolidBrush.Find() == nil {
		bgBrush, _, _ := procCreateSolidBrush.Call(uintptr(argbToCOLORREF(t.menuColors.bg)))
		if bgBrush != 0 {
			defer procDeleteObject.Call(bgBrush)
			applyMenuBackgroundBrush(hMenu, bgBrush)
		}
	}

	_, _, _ = procSetMenuDefaultItem.Call(hMenu, 0, 1)

	// Required by Win32 so clicking outside the popup menu reliably dismisses it.
	_, _, _ = procSetForegroundWindow.Call(t.hwnd)

	flags := uintptr(tpmLeftAlign | tpmBottomAlign | tpmRightButton | tpmReturnCmd | tpmNonotify)
	cmd, _, _ := procTrackPopupMenuEx.Call(hMenu, flags, uintptr(pt.X), uintptr(pt.Y), t.hwnd, 0)
	_, _, _ = procPostMessageW.Call(t.hwnd, wmNull, 0, 0)

	return uint32(cmd)
}
