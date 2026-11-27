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
	RTL                bool
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
	if (getWindowExStyle(fg) & wsExTransparent) != 0 {
		return false
	}

	fgMon, _, _ := procMonitorFromWindow.Call(fg, monitorDefaultToNearest)
	if fgMon != hMon {
		return false
	}

	quns, hasQuns := queryUserNotificationState()
	if hasQuns && (quns == qunsRunningD3DFullScreen || quns == qunsPresentationMode) {
		return true
	}

	fgRect, ok := getWindowVisibleBounds(fg)
	if !ok {
		return false
	}
	if fgRect.Left > monRect.Left || fgRect.Top > monRect.Top ||
		fgRect.Right < monRect.Right || fgRect.Bottom < monRect.Bottom {
		return false
	}
	// If Windows keeps Shell_TrayWnd above fg in Z-order (e.g. third-party desktop widgets),
	// the taskbar is not actually covered by a fullscreen window.
	if taskbarHWND != 0 && isWindowAbove(taskbarHWND, fg) {
		return false
	}

	style := getWindowStyle(fg)
	if (style & wsCaption) == wsCaption {
		return hasQuns && quns == qunsBusy
	}
	return true
}

func findChildWindow(parent, childAfter uintptr) uintptr {
	hwnd, _, _ := procFindWindowExW.Call(parent, childAfter, 0, 0)
	return hwnd
}

func findPrimaryTaskbarWindow() uintptr {
	var firstMatch, explorerMatch uintptr
	cur := uintptr(0)
	for i := 0; i < 32; i++ {
		cur = findWindowEx(0, cur, "Shell_TrayWnd")
		if cur == 0 {
			break
		}
		if firstMatch == 0 {
			firstMatch = cur
		}
		if getProcessExeName(getWindowProcessID(cur)) == "explorer.exe" {
			if explorerMatch == 0 {
				explorerMatch = cur
			}
			if isWindowActuallyVisible(cur) {
				return cur
			}
		}
	}
	if explorerMatch != 0 {
		return explorerMatch
	}
	if firstMatch != 0 {
		return firstMatch
	}
	return findWindow("Shell_TrayWnd")
}

func isWin32TrayDescendant(cls string, r, tb rect, dpi uint32, isRTL bool) bool {
	if r.Empty() || tb.Empty() {
		return false
	}
	if r.Right <= tb.Left || r.Left >= tb.Right || r.Bottom <= tb.Top || r.Top >= tb.Bottom {
		return false
	}

	isVertical := tb.Height() > tb.Width()
	if !isVertical {
		if r.Width() >= (tb.Width()*3)/4 {
			return false
		}
		if isRTL {
			if r.Right >= tb.Right {
				return false
			}
		} else if r.Left <= tb.Left {
			return false
		}
		switch cls {
		case "TrayNotifyWnd",
			"TrayInputIndicatorWClass",
			"TrayClockWClass",
			"TrayButton",
			"TrayShowDesktopButtonWClass",
			"SystemTray_Main":
			return true
		case "ToolbarWindow32",
			"Windows.UI.Composition.DesktopWindowContentBridge",
			"Windows.UI.Input.InputSite.WindowClass":
			if isRTL {
				return r.Right <= tb.Left+tb.Width()/2 && r.Left <= tb.Left+scaleDPIInt(420, dpi)
			}
			return r.Left >= tb.Left+tb.Width()/2 && r.Right >= tb.Right-scaleDPIInt(420, dpi)
		default:
			return false
		}
	}

	if r.Top <= tb.Top || r.Height() >= (tb.Height()*3)/4 {
		return false
	}
	switch cls {
	case "TrayNotifyWnd",
		"TrayInputIndicatorWClass",
		"TrayClockWClass",
		"TrayButton",
		"TrayShowDesktopButtonWClass",
		"SystemTray_Main":
		return true
	case "ToolbarWindow32",
		"Windows.UI.Composition.DesktopWindowContentBridge",
		"Windows.UI.Input.InputSite.WindowClass":
		return r.Top >= tb.Top+tb.Height()/2 && r.Bottom >= tb.Bottom-scaleDPIInt(420, dpi)
	default:
		return false
	}
}

func collectWin32TrayRect(shellTray, ignoreHWND uintptr, tb rect, dpi uint32, isRTL bool) (rect, bool) {
	var out rect
	var ok bool
	var walk func(parent uintptr, depth int)
	walk = func(parent uintptr, depth int) {
		if depth > 4 || parent == 0 {
			return
		}
		child := uintptr(0)
		for i := 0; i < 64; i++ {
			child = findChildWindow(parent, child)
			if child == 0 {
				break
			}
			if child == ignoreHWND || !isWindowActuallyVisible(child) {
				continue
			}
			r, rOK := getWindowRect(child)
			if rOK {
				cls := getWindowClassName(child)
				if isWin32TrayDescendant(cls, r, tb, dpi, isRTL) {
					out, ok = unionRect(out, ok, r, true)
				}
			}
			walk(child, depth+1)
		}
	}
	walk(shellTray, 0)

	if !ok {
		trayNotify := findWindowEx(shellTray, 0, "TrayNotifyWnd")
		if r, rOK := getWindowRect(trayNotify); rOK && !r.Empty() {
			return r, true
		}
	}
	return out, ok
}

// QueryTaskbarInfo reads live Shell_TrayWnd and system tray bounds from Win32 + cached UIA.
func QueryTaskbarInfo() TaskbarInfo {
	return QueryTaskbarInfoWithExclusion(0)
}

// QueryTaskbarInfoWithExclusion queries live taskbar geometry while excluding the overlay HWND itself.
func QueryTaskbarInfoWithExclusion(ignoreHWND uintptr) TaskbarInfo {
	shellTray := findPrimaryTaskbarWindow()
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

	isRTL := (getWindowExStyle(shellTray) & wsExLayoutRTL) != 0
	win32TrayRect, win32OK := collectWin32TrayRect(shellTray, ignoreHWND, tbRect, dpi, isRTL)
	uiaTrayRect, uiaOK := globalTrayProbe.Snapshot(shellTray, tbRect)
	trayRect, _ := unionRect(win32TrayRect, win32OK, uiaTrayRect, uiaOK)

	autoHide := isTaskbarAutoHide()
	hidden := tbVisible == 0
	if autoHide && !monRect.Empty() {
		edgeTol := scaleDPIInt(4, dpi)
		if tbRect.Width() >= tbRect.Height() {
			if tbRect.Top >= monRect.Bottom-edgeTol || tbRect.Bottom <= monRect.Top+edgeTol {
				hidden = true
			}
		} else {
			if tbRect.Right <= monRect.Left+edgeTol || tbRect.Left >= monRect.Right-edgeTol {
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
		RTL:                isRTL,
		AutoHide:           autoHide,
		TaskbarHidden:      hidden,
		FullscreenBlocking: fullscreen,
	}
}

// ComputeOverlayBounds calculates the docked screen position immediately to the left
// of the Windows 11 system tray chevron (TrayNotifyWnd / UIA tray icons, or right of it on RTL),
// or centered above it for vertical taskbars.
func ComputeOverlayBounds(info TaskbarInfo, overlayW, overlayH int32) (x, y int32, visible bool) {
	if !info.Valid || info.TaskbarHidden || info.FullscreenBlocking || info.TaskbarRect.Empty() {
		return 0, 0, false
	}

	tb := info.TaskbarRect
	gap := scaleDPIInt(4, info.DPI)

	if tb.Width() >= tb.Height() {
		// Standard horizontal taskbar (bottom or top).
		y = tb.Top + (tb.Height()-overlayH)/2

		if info.RTL {
			minX := tb.Left + gap
			if !info.TrayRect.Empty() && info.TrayRect.Right < tb.Right {
				minX = info.TrayRect.Right + gap
				x = minX
			} else {
				fallbackTrayWidth := scaleDPIInt(240, info.DPI)
				x = tb.Left + fallbackTrayWidth + gap
			}
			if x < minX {
				x = minX
			}
			if x+overlayW > tb.Right-gap {
				x = tb.Right - overlayW - gap
			}
			if x < tb.Left {
				x = tb.Left
			}
			return x, y, true
		}

		maxX := tb.Right - overlayW - gap
		if !info.TrayRect.Empty() && info.TrayRect.Left > tb.Left {
			maxX = info.TrayRect.Left - overlayW - gap
			x = maxX
		} else {
			fallbackTrayWidth := scaleDPIInt(240, info.DPI)
			x = tb.Right - fallbackTrayWidth - overlayW - gap
		}

		if x > maxX {
			x = maxX
		}
		if x < tb.Left+gap {
			x = tb.Left + gap
		}
		if x+overlayW > tb.Right {
			x = tb.Right - overlayW
			if x < tb.Left {
				x = tb.Left
			}
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
		y = tb.Bottom - overlayH - scaleDPIInt(200, info.DPI)
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
