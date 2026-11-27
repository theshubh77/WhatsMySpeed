package ui

import (
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

// TrayManager owns the Shell_NotifyIconW icon and popup context menu.
type TrayManager struct {
	hwnd     uintptr
	hIcon    uintptr
	dpi      uint32
	isLight  bool
	trayARGB uint32
	added    bool
	lastTip  string
	menuFont string
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
		t.added = ok != 0
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

// DismissMenu closes any open context menu immediately.
func (t *TrayManager) DismissMenu() {
	dismissActivePopupMenu()
}

// Remove deletes the system tray icon and frees its HICON resource.
func (t *TrayManager) Remove() {
	t.DismissMenu()
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

func buildContextMenuItems(cfg config.Config, autoStart bool) []menuItemSpec {
	return []menuItemSpec{
		{id: cmdOpenWindow, text: "Open WhatsMySpeed", isDefault: true},
		{id: cmdRunSpeedTest, text: "Run Speed Test..."},
		{isSep: true},
		{id: cmdShowBoth, text: "Show Both", isRadio: true, checked: cfg.DisplayMode == config.ModeBoth},
		{id: cmdDownloadOnly, text: "Download Only", isRadio: true, checked: cfg.DisplayMode == config.ModeDownload},
		{id: cmdUploadOnly, text: "Upload Only", isRadio: true, checked: cfg.DisplayMode == config.ModeUpload},
		{id: cmdHideSpeed, text: "Hide Speed", isRadio: true, checked: cfg.DisplayMode == config.ModeHide},
		{isSep: true},
		{
			text: "Label Style",
			children: []menuItemSpec{
				{id: cmdLabelArrows, text: "Arrows (↑ ↓)", isRadio: true, checked: cfg.LabelStyle == config.LabelArrows},
				{id: cmdLabelUD, text: "U / D", isRadio: true, checked: cfg.LabelStyle == config.LabelUD},
				{id: cmdLabelWords, text: "Up / Down", isRadio: true, checked: cfg.LabelStyle == config.LabelWords},
			},
		},
		{
			text: "Layout",
			children: []menuItemSpec{
				{id: cmdLayoutStackU, text: "Stacked (Up / Down)", isRadio: true, checked: cfg.Layout == config.LayoutStackedUp},
				{id: cmdLayoutStackD, text: "Stacked (Down / Up)", isRadio: true, checked: cfg.Layout == config.LayoutStackedDown},
				{id: cmdLayoutInline, text: "Inline (Side by Side)", isRadio: true, checked: cfg.Layout == config.LayoutInline},
				{id: cmdLayoutComp, text: "Compact", isRadio: true, checked: cfg.Layout == config.LayoutCompact},
			},
		},
		{
			text: "Speed Unit",
			children: []menuItemSpec{
				{id: cmdUnitBytes, text: "Bytes/s (KB/s, MB/s)", isRadio: true, checked: cfg.SpeedUnit == config.UnitBytes},
				{id: cmdUnitBits, text: "Bits/s (Kbps, Mbps)", isRadio: true, checked: cfg.SpeedUnit == config.UnitBits},
			},
		},
		{
			text: "Theme",
			children: []menuItemSpec{
				{id: cmdThemeSystem, text: "System (Auto)", isRadio: true, checked: cfg.Theme == config.ThemeSystem},
				{id: cmdThemeDark, text: "Dark", isRadio: true, checked: cfg.Theme == config.ThemeDark},
				{id: cmdThemeLight, text: "Light", isRadio: true, checked: cfg.Theme == config.ThemeLight},
			},
		},
		{isSep: true},
		{id: cmdAutoStart, text: "Start with Windows", checked: autoStart},
		{isSep: true},
		{id: cmdExit, text: "Exit"},
	}
}

// ShowContextMenu displays the custom DPI-scaled popup menu at the cursor position
// and returns the selected command ID (or 0 if dismissed).
func (t *TrayManager) ShowContextMenu(cfg config.Config, autoStart bool) uint32 {
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
	if t.menuFont == "" {
		t.menuFont = ResolveTaskbarFontName()
	}

	items := buildContextMenuItems(cfg, autoStart)
	pal := resolveMenuPalette(cfg)

	// Foreground host first so SetForegroundWindow on the popup menu always succeeds.
	if t.hwnd != 0 {
		_, _, _ = procSetForegroundWindow.Call(t.hwnd)
	}
	return showCustomPopupMenu(t.hwnd, pt, dpi, t.menuFont, pal, items)
}
