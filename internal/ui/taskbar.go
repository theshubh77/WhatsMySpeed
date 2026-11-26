package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// TaskbarInfo describes the current Windows 11 taskbar and system tray geometry.
type TaskbarInfo struct {
	Valid              bool
	TaskbarHWND        uintptr
	TaskbarRect        rect
	TrayRect           rect
	MonitorRect        rect
	DPI                uint32
	AutoHide           bool
	TaskbarHidden      bool
	FullscreenBlocking bool
}

func findWindow(className string) uintptr {
	u16, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(u16)), 0)
	return hwnd
}

func findWindowEx(parent, childAfter uintptr, className string) uintptr {
	u16, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowExW.Call(parent, childAfter, uintptr(unsafe.Pointer(u16)), 0)
	return hwnd
}

func getWindowRect(hwnd uintptr) (rect, bool) {
	var r rect
	if hwnd == 0 {
		return r, false
	}
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

func getWindowClassName(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	var buf [128]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func isWindowCloaked(hwnd uintptr) bool {
	if hwnd == 0 || procDwmGetWindowAttribute.Find() != nil {
		return false
	}
	var cloaked uint32
	hr, _, _ := procDwmGetWindowAttribute.Call(
		hwnd,
		dwmwaCloaked,
		uintptr(unsafe.Pointer(&cloaked)),
		unsafe.Sizeof(cloaked),
	)
	return hr == 0 && cloaked != 0
}

func isTaskbarAutoHide() bool {
	var abd appBarData
	abd.CbSize = uint32(unsafe.Sizeof(abd))
	state, _, _ := procSHAppBarMessage.Call(abmGetState, uintptr(unsafe.Pointer(&abd)))
	return (state & absAutohide) != 0
}

func isFullscreenWindowOnMonitor(hMon uintptr, monRect rect, taskbarHWND uintptr) bool {
	if hMon == 0 || monRect.Empty() {
		return false
	}
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 || fg == taskbarHWND {
		return false
	}
	if vis, _, _ := procIsWindowVisible.Call(fg); vis == 0 {
		return false
	}
	if iconic, _, _ := procIsIconic.Call(fg); iconic != 0 {
		return false
	}
	if isWindowCloaked(fg) {
		return false
	}

	pid := getWindowProcessID(fg)
	if pid != 0 && pid == windows.GetCurrentProcessId() {
		return false
	}

	cls := getWindowClassName(fg)
	exe := getProcessExeName(pid)
	if isShellOrFlyoutWindow(cls, exe) {
		return false
	}

	fgMon, _, _ := procMonitorFromWindow.Call(fg, monitorDefaultToNearest)
	if fgMon != hMon {
		return false
	}

	if quns, ok := queryUserNotificationState(); ok {
		if quns == qunsRunningD3DFullScreen || quns == qunsPresentationMode {
			return true
		}
	}

	fgRect, ok := getWindowVisibleBounds(fg)
	if !ok {
		return false
	}
	if fgRect.Left > monRect.Left || fgRect.Top > monRect.Top ||
		fgRect.Right < monRect.Right || fgRect.Bottom < monRect.Bottom {
		return false
	}

	style := getWindowStyle(fg)
	if (style & wsCaption) == wsCaption {
		quns, ok := queryUserNotificationState()
		return ok && quns == qunsBusy
	}
	return true
}

// QueryTaskbarInfo reads live Shell_TrayWnd and TrayNotifyWnd bounds from Win32.
func QueryTaskbarInfo() TaskbarInfo {
	shellTray := findWindow("Shell_TrayWnd")
	if shellTray == 0 {
		return TaskbarInfo{DPI: getSystemDPI()}
	}

	tbVisible, _, _ := procIsWindowVisible.Call(shellTray)
	tbRect, ok := getWindowRect(shellTray)
	if !ok || tbRect.Empty() {
		return TaskbarInfo{TaskbarHWND: shellTray, DPI: getWindowDPI(shellTray)}
	}

	dpi := getWindowDPI(shellTray)
	hMon, _, _ := procMonitorFromWindow.Call(shellTray, monitorDefaultToNearest)
	monRect := tbRect
	if hMon != 0 {
		var mi monitorInfo
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); ok != 0 && !mi.RcMonitor.Empty() {
			monRect = mi.RcMonitor
		}
	}

	trayNotify := findWindowEx(shellTray, 0, "TrayNotifyWnd")
	trayRect, _ := getWindowRect(trayNotify)

	autoHide := isTaskbarAutoHide()
	hidden := tbVisible == 0
	if autoHide && !monRect.Empty() {
		if tbRect.Width() >= tbRect.Height() {
			if tbRect.Top >= monRect.Bottom-2 || tbRect.Bottom <= monRect.Top+2 {
				hidden = true
			}
		} else {
			if tbRect.Right <= monRect.Left+2 || tbRect.Left >= monRect.Right-2 {
				hidden = true
			}
		}
	}

	fullscreen := isFullscreenWindowOnMonitor(hMon, monRect, shellTray)

	return TaskbarInfo{
		Valid:              true,
		TaskbarHWND:        shellTray,
		TaskbarRect:        tbRect,
		TrayRect:           trayRect,
		MonitorRect:        monRect,
		DPI:                dpi,
		AutoHide:           autoHide,
		TaskbarHidden:      hidden,
		FullscreenBlocking: fullscreen,
	}
}

// ComputeOverlayBounds calculates the docked screen position immediately to the left
// of the Windows 11 system tray chevron (TrayNotifyWnd), or centered above it for vertical taskbars.
func ComputeOverlayBounds(info TaskbarInfo, overlayW, overlayH int32) (x, y int32, visible bool) {
	if !info.Valid || info.TaskbarHidden || info.FullscreenBlocking || info.TaskbarRect.Empty() {
		return 0, 0, false
	}

	tb := info.TaskbarRect
	gap := scaleDPIInt(4, info.DPI)

	if tb.Width() >= tb.Height() {
		// Standard horizontal taskbar (bottom or top).
		y = tb.Top + (tb.Height()-overlayH)/2

		if !info.TrayRect.Empty() && info.TrayRect.Left > tb.Left {
			x = info.TrayRect.Left - overlayW - gap
		} else {
			fallbackTrayWidth := scaleDPIInt(140, info.DPI)
			x = tb.Right - fallbackTrayWidth - overlayW - gap
		}

		if x < tb.Left+gap {
			x = tb.Left + gap
		}
		if x+overlayW > tb.Right-gap {
			x = tb.Right - overlayW - gap
		}
		return x, y, true
	}

	// Vertical taskbar (left or right edge).
	x = tb.Left + (tb.Width()-overlayW)/2
	if x+overlayW > tb.Right {
		x = tb.Right - overlayW
	}
	if x < tb.Left {
		x = tb.Left
	}
	if !info.TrayRect.Empty() && info.TrayRect.Top > tb.Top {
		y = info.TrayRect.Top - overlayH - gap
	} else {
		y = tb.Bottom - overlayH - scaleDPIInt(120, info.DPI)
	}
	if y+overlayH > tb.Bottom-gap {
		y = tb.Bottom - overlayH - gap
	}
	if y < tb.Top+gap {
		y = tb.Top + gap
	}
	return x, y, true
}

// isWindowAbove checks whether targetHWND appears before refHWND in the top-level Z-order.
func isWindowAbove(targetHWND, refHWND uintptr) bool {
	if targetHWND == 0 || refHWND == 0 {
		return true
	}
	cur, _, _ := procGetTopWindow.Call(0)
	for cur != 0 {
		if cur == targetHWND {
			return true
		}
		if cur == refHWND {
			return false
		}
		cur, _, _ = procGetWindow.Call(cur, gwHwndNext)
	}
	return false
}
