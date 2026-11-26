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

	cmdShowBoth     uint32 = 1001
	cmdDownloadOnly uint32 = 1002
	cmdUploadOnly   uint32 = 1003
	cmdHideSpeed    uint32 = 1004
	cmdUnitBytes    uint32 = 1005
	cmdUnitBits     uint32 = 1006
	cmdAutoStart    uint32 = 1007
	cmdExit         uint32 = 1008
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

func insertMenuItem(hMenu uintptr, pos uint32, id uint32, text string, fType uint32, checked bool) error {
	var mii menuItemInfoW
	mii.CbSize = uint32(unsafe.Sizeof(mii))
	mii.FMask = miimFType | miimID | miimState
	mii.FType = fType
	mii.WID = id
	if checked {
		mii.FState = mfsChecked
	} else {
		mii.FState = mfsUnchecked
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

// ShowContextMenu displays the native Win32 popup menu at the current cursor position
// and returns the selected command ID (or 0 if dismissed).
func (t *TrayManager) ShowContextMenu(mode config.DisplayMode, unit config.SpeedUnit, autoStart bool) uint32 {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return 0
	}
	defer procDestroyMenu.Call(hMenu)

	_ = insertMenuItem(hMenu, 0, cmdShowBoth, "Show Both", mftString|mftRadioCheck, mode == config.ModeBoth)
	_ = insertMenuItem(hMenu, 1, cmdDownloadOnly, "Download Only", mftString|mftRadioCheck, mode == config.ModeDownload)
	_ = insertMenuItem(hMenu, 2, cmdUploadOnly, "Upload Only", mftString|mftRadioCheck, mode == config.ModeUpload)
	_ = insertMenuItem(hMenu, 3, cmdHideSpeed, "Hide Speed", mftString|mftRadioCheck, mode == config.ModeHide)
	_ = insertMenuItem(hMenu, 4, 0, "", mftSeparator, false)
	_ = insertMenuItem(hMenu, 5, cmdUnitBytes, "Bytes/s (KB/s, MB/s)", mftString|mftRadioCheck, unit == config.UnitBytes)
	_ = insertMenuItem(hMenu, 6, cmdUnitBits, "Bits/s (Kbps, Mbps)", mftString|mftRadioCheck, unit == config.UnitBits)
	_ = insertMenuItem(hMenu, 7, 0, "", mftSeparator, false)
	_ = insertMenuItem(hMenu, 8, cmdAutoStart, "Start with Windows", mftString, autoStart)
	_ = insertMenuItem(hMenu, 9, 0, "", mftSeparator, false)
	_ = insertMenuItem(hMenu, 10, cmdExit, "Exit", mftString, false)

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// Required by Win32 so clicking outside the popup menu reliably dismisses it.
	_, _, _ = procSetForegroundWindow.Call(t.hwnd)

	flags := uintptr(tpmLeftAlign | tpmBottomAlign | tpmRightButton | tpmReturnCmd | tpmNonotify)
	cmd, _, _ := procTrackPopupMenuEx.Call(hMenu, flags, uintptr(pt.X), uintptr(pt.Y), t.hwnd, 0)
	_, _, _ = procPostMessageW.Call(t.hwnd, wmNull, 0, 0)

	return uint32(cmd)
}
