package ui

import (
	"fmt"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"golang.org/x/sys/windows"
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
	cmdExit         uint32 = 1016
)

// TrayManager owns the Shell_NotifyIconW icon and popup context menu.
type TrayManager struct {
	hwnd    uintptr
	hIcon   uintptr
	dpi     uint32
	isLight bool
	added   bool
	lastTip string
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

// SyncIcon ensures the system tray icon is registered and themed for the current DPI and taskbar mode.
func (t *TrayManager) SyncIcon(dpi uint32, isLight bool, forceReadd bool) error {
	needNewIcon := t.hIcon == 0 || t.dpi != dpi || t.isLight != isLight
	var oldIcon uintptr

	if needNewIcon {
		colors := ColorsForTheme(isLight)
		newIcon, err := CreateTrayHICON(assets.IconGaugeBold, dpi, colors.TrayIconARGB)
		if err != nil {
			return err
		}
		oldIcon = t.hIcon
		t.hIcon = newIcon
		t.dpi = dpi
		t.isLight = isLight
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
		}
	} else if needNewIcon {
		nid := t.buildNID(nifIcon, "")
		_, _, _ = procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
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
	t.lastTip = tip
	nid := t.buildNID(nifTip, tip)
	_, _, _ = procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
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

func insertMenuItem(hMenu uintptr, pos uint32, id uint32, text string, fType uint32, state uint32, subMenu uintptr) error {
	var mii menuItemInfoW
	mii.CbSize = uint32(unsafe.Sizeof(mii))
	mii.FMask = miimFType | miimID | miimState
	mii.FType = fType
	mii.WID = id
	mii.FState = state

	if subMenu != 0 {
		mii.FMask |= miimSubMenu
		mii.HSubMenu = subMenu
	}
	if fType != mftSeparator {
		mii.FMask |= miimString
		u16, err := windows.UTF16PtrFromString(text)
		if err != nil {
			return fmt.Errorf("encode menu item %q: %w", text, err)
		}
		mii.DwTypeData = u16
	}

	ok, _, _ := procInsertMenuItemW.Call(hMenu, uintptr(pos), 1, uintptr(unsafe.Pointer(&mii)))
	if ok == 0 {
		return fmt.Errorf("InsertMenuItemW(%q) failed", text)
	}
	return nil
}

func checkState(checked bool) uint32 {
	if checked {
		return mfsChecked
	}
	return mfsUnchecked
}

// ShowContextMenu displays the native Win32 popup menu at the current cursor position
// and returns the selected command ID (or 0 if dismissed).
func (t *TrayManager) ShowContextMenu(cfg config.Config, autoStart bool) uint32 {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return 0
	}
	defer procDestroyMenu.Call(hMenu)

	labelSub, _, _ := procCreatePopupMenu.Call()
	if labelSub != 0 {
		_ = insertMenuItem(labelSub, 0, cmdLabelArrows, "Arrows (↑ ↓)", mftString|mftRadioCheck, checkState(cfg.LabelStyle == config.LabelArrows), 0)
		_ = insertMenuItem(labelSub, 1, cmdLabelUD, "U / D", mftString|mftRadioCheck, checkState(cfg.LabelStyle == config.LabelUD), 0)
		_ = insertMenuItem(labelSub, 2, cmdLabelWords, "Up / Down", mftString|mftRadioCheck, checkState(cfg.LabelStyle == config.LabelWords), 0)
	}

	layoutSub, _, _ := procCreatePopupMenu.Call()
	if layoutSub != 0 {
		_ = insertMenuItem(layoutSub, 0, cmdLayoutStackU, "Stacked (Up / Down)", mftString|mftRadioCheck, checkState(cfg.Layout == config.LayoutStackedUp), 0)
		_ = insertMenuItem(layoutSub, 1, cmdLayoutStackD, "Stacked (Down / Up)", mftString|mftRadioCheck, checkState(cfg.Layout == config.LayoutStackedDown), 0)
		_ = insertMenuItem(layoutSub, 2, cmdLayoutInline, "Inline (Side by Side)", mftString|mftRadioCheck, checkState(cfg.Layout == config.LayoutInline), 0)
		_ = insertMenuItem(layoutSub, 3, cmdLayoutComp, "Compact", mftString|mftRadioCheck, checkState(cfg.Layout == config.LayoutCompact), 0)
	}

	unitSub, _, _ := procCreatePopupMenu.Call()
	if unitSub != 0 {
		_ = insertMenuItem(unitSub, 0, cmdUnitBytes, "Bytes/s (KB/s, MB/s)", mftString|mftRadioCheck, checkState(cfg.SpeedUnit == config.UnitBytes), 0)
		_ = insertMenuItem(unitSub, 1, cmdUnitBits, "Bits/s (Kbps, Mbps)", mftString|mftRadioCheck, checkState(cfg.SpeedUnit == config.UnitBits), 0)
	}

	pos := uint32(0)
	add := func(id uint32, text string, fType, state uint32, sub uintptr) {
		_ = insertMenuItem(hMenu, pos, id, text, fType, state, sub)
		pos++
	}

	add(cmdOpenWindow, "Open WhatsMySpeed", mftString, mfsDefault, 0)
	add(cmdRunSpeedTest, "Run Speed Test...", mftString, mfsUnchecked, 0)
	add(0, "", mftSeparator, mfsUnchecked, 0)
	add(cmdShowBoth, "Show Both", mftString|mftRadioCheck, checkState(cfg.DisplayMode == config.ModeBoth), 0)
	add(cmdDownloadOnly, "Download Only", mftString|mftRadioCheck, checkState(cfg.DisplayMode == config.ModeDownload), 0)
	add(cmdUploadOnly, "Upload Only", mftString|mftRadioCheck, checkState(cfg.DisplayMode == config.ModeUpload), 0)
	add(cmdHideSpeed, "Hide Speed", mftString|mftRadioCheck, checkState(cfg.DisplayMode == config.ModeHide), 0)
	add(0, "", mftSeparator, mfsUnchecked, 0)
	if labelSub != 0 {
		add(0, "Label Style", mftString, mfsUnchecked, labelSub)
	}
	if layoutSub != 0 {
		add(0, "Layout", mftString, mfsUnchecked, layoutSub)
	}
	if unitSub != 0 {
		add(0, "Speed Unit", mftString, mfsUnchecked, unitSub)
	}
	add(0, "", mftSeparator, mfsUnchecked, 0)
	add(cmdAutoStart, "Start with Windows", mftString, checkState(autoStart), 0)
	add(0, "", mftSeparator, mfsUnchecked, 0)
	add(cmdExit, "Exit", mftString, mfsUnchecked, 0)

	_, _, _ = procSetMenuDefaultItem.Call(hMenu, 0, 1)

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// Required by Win32 so clicking outside the popup menu reliably dismisses it.
	_, _, _ = procSetForegroundWindow.Call(t.hwnd)

	flags := uintptr(tpmLeftAlign | tpmBottomAlign | tpmRightButton | tpmReturnCmd | tpmNonotify)
	cmd, _, _ := procTrackPopupMenuEx.Call(hMenu, flags, uintptr(pt.X), uintptr(pt.Y), t.hwnd, 0)
	_, _, _ = procPostMessageW.Call(t.hwnd, wmNull, 0, 0)

	return uint32(cmd)
}
