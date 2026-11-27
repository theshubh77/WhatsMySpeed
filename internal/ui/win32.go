package ui

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wsPopup        = 0x80000000
	wsChild        = 0x40000000
	wsClipSiblings = 0x04000000
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsMaximize     = 0x01000000

	csVRedraw    = 0x0001
	csHRedraw    = 0x0002
	csDblClks    = 0x0008
	csDropShadow = 0x00020000

	wsExTopmost     = 0x00000008
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExLayered     = 0x00080000
	wsExLayoutRTL   = 0x00400000
	wsExNoActivate  = 0x08000000

	wmNull                  = 0x0000
	wmDestroy               = 0x0002
	wmSize                  = 0x0005
	wmActivate              = 0x0006
	wmKillFocus             = 0x0008
	wmPaint                 = 0x000F
	wmClose                 = 0x0010
	wmQueryEndSession       = 0x0011
	wmEraseBkgnd            = 0x0014
	wmEndSession            = 0x0016
	wmSettingChange         = 0x001A
	wmCancelMode            = 0x001F
	wmSetCursor             = 0x0020
	wmMouseActivate         = 0x0021
	wmContextMenu           = 0x007B
	wmDisplayChange         = 0x007E
	wmSetIcon               = 0x0080
	wmKeyDown               = 0x0100
	wmTimer                 = 0x0113
	wmMouseMove             = 0x0200
	wmLButtonDown           = 0x0201
	wmLButtonUp             = 0x0202
	wmLButtonDblClk         = 0x0203
	wmRButtonDown           = 0x0204
	wmRButtonUp             = 0x0205
	wmMButtonDown           = 0x0207
	wmPowerBroadcast        = 0x0218
	wmMouseLeave            = 0x02A3
	wmDpiChanged            = 0x02E0
	wmDpiChangedAfterParent = 0x02E3
	wmApp                   = 0x8000

	waInactive = 0

	vkReturn = 0x0D
	vkEscape = 0x1B
	vkSpace  = 0x20
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28

	maNoActivate = 3

	pbtApmResumeSuspend   = 0x0007
	pbtApmResumeAutomatic = 0x0012

	spiGetHighContrast = 0x0042
	hcfHighContrastOn  = 0x00000001
	colorWindow        = 5
	colorWindowText    = 8

	nullRegion = 1
	rgnError   = 0

	wmTrayCallback    = wmApp + 1
	wmSpeedTestUpdate = wmApp + 2
	wmSyncOverlay     = wmApp + 3
	wmShowDashboard   = wmApp + 4

	swHide         = 0
	swShowNormal   = 1
	swShowNoActive = 4
	swRestore      = 9

	iconSmall = 0
	iconBig   = 1

	hwndTop         = uintptr(0)  // (HWND)0
	hwndTopmost     = ^uintptr(0) // (HWND)-1
	hwndNoTopmost   = ^uintptr(1) // (HWND)-2
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040

	gwlStyle   = ^uintptr(15) // GWL_STYLE (-16)
	gwlExStyle = ^uintptr(19) // GWL_EXSTYLE (-20)

	gwHwndNext = 2

	monitorDefaultToNearest = 2

	smCxScreen    = 0
	smCyScreen    = 1
	smCxSmIcon    = 49
	smCySmIcon    = 50
	smCxMenuCheck = 71

	ulwAlpha   = 0x00000002
	acSrcOver  = 0x00
	acSrcAlpha = 0x01

	biRGB   = 0
	srcCopy = 0x00CC0020

	tmeLeave = 0x00000002

	abmGetState = 0x00000004
	absAutohide = 0x00000001

	dwmwaExtendedFrameBounds    = 9
	dwmwaCloaked                = 14
	dwmwaUseImmersiveDarkMode   = 20
	dwmwaWindowCornerPreference = 33
	dwmwaBorderColor            = 34
	dwmWcpRound                 = 2

	qunsBusy                 = 2
	qunsRunningD3DFullScreen = 3
	qunsPresentationMode     = 4

	winEventOutOfContext   = 0x0000
	winEventSkipOwnProcess = 0x0002

	eventSystemForeground     = 0x0003
	eventSystemCaptureEnd     = 0x0009
	eventObjectShow           = 0x8002
	eventObjectHide           = 0x8003
	eventObjectReorder        = 0x8004
	eventObjectLocationChange = 0x800B
	objidWindow               = 0

	gaParent = 1
	gaRoot   = 2

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	miimState   = 0x00000001
	miimID      = 0x00000002
	miimSubMenu = 0x00000004
	miimFType   = 0x00000100
	miimString  = 0x00000040
	miimData    = 0x00000020

	mftString     = 0x00000000
	mftOwnerDraw  = 0x00000100
	mftSeparator  = 0x00000800
	mftRadioCheck = 0x00000200

	colorMenu          = 4
	colorMenuText      = 7
	colorHighlight     = 13
	colorHighlightText = 14
	colorGrayText      = 17

	mfsChecked   = 0x00000008
	mfsUnchecked = 0x00000000
	mfsDefault   = 0x00001000

	tpmLeftAlign   = 0x0000
	tpmBottomAlign = 0x0020
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNonotify    = 0x0080

	dpiAwarenessPerMonitorAwareV2 = ^uintptr(3) // -4
	dpiAwarenessPerMonitorAware   = ^uintptr(2) // -3

	idcArrow = 32512
	idcHand  = 32649

	lfFaceSize = 32
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGdi32    = windows.NewLazySystemDLL("gdi32.dll")
	modShell32  = windows.NewLazySystemDLL("shell32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modDwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	modOle32    = windows.NewLazySystemDLL("ole32.dll")
	modOleAut32 = windows.NewLazySystemDLL("oleaut32.dll")

	procGetModuleHandleW              = modKernel32.NewProc("GetModuleHandleW")
	procRtlMoveMemory                 = modKernel32.NewProc("RtlMoveMemory")
	procSetProcessDpiAwarenessContext = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware           = modUser32.NewProc("SetProcessDPIAware")
	procRegisterClassExW             = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW              = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow                = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW               = modUser32.NewProc("DefWindowProcW")
	procGetMessageW                  = modUser32.NewProc("GetMessageW")
	procTranslateMessage             = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW             = modUser32.NewProc("DispatchMessageW")
	procPostMessageW                 = modUser32.NewProc("PostMessageW")
	procPostQuitMessage              = modUser32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW       = modUser32.NewProc("RegisterWindowMessageW")
	procLoadCursorW                  = modUser32.NewProc("LoadCursorW")
	procSetCursor                    = modUser32.NewProc("SetCursor")
	procShowWindow                   = modUser32.NewProc("ShowWindow")
	procIsWindow                     = modUser32.NewProc("IsWindow")
	procIsWindowVisible              = modUser32.NewProc("IsWindowVisible")
	procIsIconic                     = modUser32.NewProc("IsIconic")
	procSetWindowPos                 = modUser32.NewProc("SetWindowPos")
	procSetTimer                     = modUser32.NewProc("SetTimer")
	procKillTimer                    = modUser32.NewProc("KillTimer")
	procFindWindowW                  = modUser32.NewProc("FindWindowW")
	procFindWindowExW                = modUser32.NewProc("FindWindowExW")
	procGetWindowRect                = modUser32.NewProc("GetWindowRect")
	procGetClientRect                = modUser32.NewProc("GetClientRect")
	procAdjustWindowRectEx           = modUser32.NewProc("AdjustWindowRectEx")
	procAdjustWindowRectExForDpi     = modUser32.NewProc("AdjustWindowRectExForDpi")
	procGetClassNameW                = modUser32.NewProc("GetClassNameW")
	procGetForegroundWindow          = modUser32.NewProc("GetForegroundWindow")
	procSetForegroundWindow          = modUser32.NewProc("SetForegroundWindow")
	procAllowSetForegroundWindow     = modUser32.NewProc("AllowSetForegroundWindow")
	procGetTopWindow                 = modUser32.NewProc("GetTopWindow")
	procGetWindow                    = modUser32.NewProc("GetWindow")
	procSetParent                    = modUser32.NewProc("SetParent")
	procGetParent                    = modUser32.NewProc("GetParent")
	procGetAncestor                  = modUser32.NewProc("GetAncestor")
	procScreenToClient               = modUser32.NewProc("ScreenToClient")
	procGetWindowLongW               = modUser32.NewProc("GetWindowLongW")
	procSetWindowLongW               = modUser32.NewProc("SetWindowLongW")
	procGetWindowThreadProcessId     = modUser32.NewProc("GetWindowThreadProcessId")
	procSetWinEventHook              = modUser32.NewProc("SetWinEventHook")
	procUnhookWinEvent               = modUser32.NewProc("UnhookWinEvent")
	procMonitorFromWindow            = modUser32.NewProc("MonitorFromWindow")
	procMonitorFromPoint             = modUser32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW              = modUser32.NewProc("GetMonitorInfoW")
	procGetDpiForWindow              = modUser32.NewProc("GetDpiForWindow")
	procGetDpiForSystem              = modUser32.NewProc("GetDpiForSystem")
	procGetSystemMetrics             = modUser32.NewProc("GetSystemMetrics")
	procGetSystemMetricsForDpi       = modUser32.NewProc("GetSystemMetricsForDpi")
	procGetDC                        = modUser32.NewProc("GetDC")
	procReleaseDC                    = modUser32.NewProc("ReleaseDC")
	procBeginPaint                   = modUser32.NewProc("BeginPaint")
	procEndPaint                     = modUser32.NewProc("EndPaint")
	procInvalidateRect               = modUser32.NewProc("InvalidateRect")
	procUpdateLayeredWindow          = modUser32.NewProc("UpdateLayeredWindow")
	procTrackMouseEvent              = modUser32.NewProc("TrackMouseEvent")
	procGetCursorPos                 = modUser32.NewProc("GetCursorPos")
	procDestroyIcon                  = modUser32.NewProc("DestroyIcon")
	procSendMessageW                 = modUser32.NewProc("SendMessageW")
	procSystemParametersInfoW        = modUser32.NewProc("SystemParametersInfoW")
	procGetSysColor                  = modUser32.NewProc("GetSysColor")
	procGetWindowRgn                 = modUser32.NewProc("GetWindowRgn")
	procGetWindowRgnBox              = modUser32.NewProc("GetWindowRgnBox")

	procCreateCompatibleDC = modGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = modGdi32.NewProc("DeleteDC")
	procCreateDIBSection   = modGdi32.NewProc("CreateDIBSection")
	procSelectObject       = modGdi32.NewProc("SelectObject")
	procDeleteObject       = modGdi32.NewProc("DeleteObject")
	procBitBlt             = modGdi32.NewProc("BitBlt")
	procCreateRectRgn      = modGdi32.NewProc("CreateRectRgn")
	procRectInRegion       = modGdi32.NewProc("RectInRegion")

	procShellNotifyIconW             = modShell32.NewProc("Shell_NotifyIconW")
	procSHAppBarMessage              = modShell32.NewProc("SHAppBarMessage")
	procSHQueryUserNotificationState = modShell32.NewProc("SHQueryUserNotificationState")
	procShellExecuteW                = modShell32.NewProc("ShellExecuteW")

	procDwmGetWindowAttribute = modDwmapi.NewProc("DwmGetWindowAttribute")
	procDwmSetWindowAttribute = modDwmapi.NewProc("DwmSetWindowAttribute")

	procCoInitializeEx   = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize   = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")
	procSysFreeString    = modOleAut32.NewProc("SysFreeString")
	procSysStringLen     = modOleAut32.NewProc("SysStringLen")
)

type point struct {
	X int32
	Y int32
}

type size struct {
	Cx int32
	Cy int32
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func (r rect) Width() int32  { return r.Right - r.Left }
func (r rect) Height() int32 { return r.Bottom - r.Top }
func (r rect) Empty() bool   { return r.Width() <= 0 || r.Height() <= 0 }
func (r rect) Contains(x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
	_           uint32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

type blendFunction struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type bitmapInfo struct {
	BmiHeader bitmapInfoHeader
	BmiColors [1]uint32
}

type trackMouseEventStruct struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

type appBarData struct {
	CbSize           uint32
	_                uint32
	Hwnd             uintptr
	UCallbackMessage uint32
	UEdge            uint32
	Rc               rect
	LParam           uintptr
}

type highContrastW struct {
	CbSize            uint32
	DwFlags           uint32
	LpszDefaultScheme uintptr
}

type menuItemInfoW struct {
	CbSize        uint32
	FMask         uint32
	FType         uint32
	FState        uint32
	WID           uint32
	HSubMenu      uintptr
	HbmpChecked   uintptr
	HbmpUnchecked uintptr
	DwItemData    uintptr
	DwTypeData    *uint16
	Cch           uint32
	HbmpItem      uintptr
}

// notifyIconDataW matches Windows NOTIFYICONDATAW (976 bytes on amd64).
type notifyIconDataW struct {
	CbSize           uint32
	_                uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	_                uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type logFontW struct {
	LfHeight         int32
	LfWidth          int32
	LfEscapement     int32
	LfOrientation    int32
	LfWeight         int32
	LfItalic         byte
	LfUnderline      byte
	LfStrikeOut      byte
	LfCharSet        byte
	LfOutPrecision   byte
	LfClipPrecision  byte
	LfQuality        byte
	LfPitchAndFamily byte
	LfFaceName       [lfFaceSize]uint16
}

// EnableDPIAwareness configures Per-Monitor V2 DPI scaling for crisp rendering across monitors.
func EnableDPIAwareness() {
	if err := procSetProcessDpiAwarenessContext.Find(); err == nil {
		r1, _, _ := procSetProcessDpiAwarenessContext.Call(dpiAwarenessPerMonitorAwareV2)
		if r1 != 0 {
			return
		}
		r1, _, _ = procSetProcessDpiAwarenessContext.Call(dpiAwarenessPerMonitorAware)
		if r1 != 0 {
			return
		}
	}
	if err := procSetProcessDPIAware.Find(); err == nil {
		_, _, _ = procSetProcessDPIAware.Call()
	}
}

func getModuleHandle() uintptr {
	r1, _, _ := procGetModuleHandleW.Call(0)
	return r1
}

func getWindowDPI(hwnd uintptr) uint32 {
	if hwnd != 0 && procGetDpiForWindow.Find() == nil {
		if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi > 0 {
			return uint32(dpi)
		}
	}
	return getSystemDPI()
}

func getSystemDPI() uint32 {
	if procGetDpiForSystem.Find() == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
			return uint32(dpi)
		}
	}
	return 96
}

func getSmallIconMetrics(dpi uint32) (int32, int32) {
	if procGetSystemMetricsForDpi.Find() == nil {
		cx, _, _ := procGetSystemMetricsForDpi.Call(smCxSmIcon, uintptr(dpi))
		cy, _, _ := procGetSystemMetricsForDpi.Call(smCySmIcon, uintptr(dpi))
		if cx > 0 && cy > 0 {
			return int32(cx), int32(cy)
		}
	}
	cx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	cy, _, _ := procGetSystemMetrics.Call(smCySmIcon)
	if cx <= 0 {
		cx = 16
	}
	if cy <= 0 {
		cy = 16
	}
	return int32(cx), int32(cy)
}

func scaleDPIInt(px int32, dpi uint32) int32 {
	if dpi == 0 {
		dpi = 96
	}
	v := (int64(px)*int64(dpi) + 48) / 96
	if v < 1 && px > 0 {
		return 1
	}
	return int32(v)
}

func copyUTF16Fixed(dst []uint16, s string) {
	u16, err := windows.UTF16FromString(s)
	if err != nil {
		return
	}
	n := copy(dst, u16)
	if n > 0 && n == len(dst) {
		dst[len(dst)-1] = 0
	}
}

func loWord(v uintptr) uint16 { return uint16(v & 0xFFFF) }

func isWindowActuallyVisible(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	v, _, _ := procIsWindowVisible.Call(hwnd)
	return v != 0
}

func isWindowHandleValid(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	v, _, _ := procIsWindow.Call(hwnd)
	return v != 0
}

func isDescendantWindow(ancestor, hwnd uintptr) bool {
	if ancestor == 0 || hwnd == 0 {
		return false
	}
	if hwnd == ancestor {
		return true
	}
	cur := hwnd
	for i := 0; i < 12 && cur != 0; i++ {
		if cur == ancestor {
			return true
		}
		parent, _, _ := procGetAncestor.Call(cur, gaParent)
		if parent == 0 {
			parent = getParentWindow(cur)
		}
		cur = parent
	}
	return false
}

func monitorWorkAreaForWindow(hwnd uintptr) (rect, bool) {
	if hwnd == 0 {
		return rect{}, false
	}
	hMon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if hMon == 0 {
		return rect{}, false
	}
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); ok != 0 && !mi.RcWork.Empty() {
		return mi.RcWork, true
	}
	return rect{}, false
}

func monitorWorkAreaForPoint(pt point) (rect, bool) {
	packed := uintptr(uint32(pt.X)) | (uintptr(uint32(pt.Y)) << 32)
	hMon, _, _ := procMonitorFromPoint.Call(packed, monitorDefaultToNearest)
	if hMon == 0 {
		return rect{}, false
	}
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); ok != 0 && !mi.RcWork.Empty() {
		return mi.RcWork, true
	}
	return rect{}, false
}

func monitorBoundsForPoint(pt point) (rect, bool) {
	packed := uintptr(uint32(pt.X)) | (uintptr(uint32(pt.Y)) << 32)
	hMon, _, _ := procMonitorFromPoint.Call(packed, monitorDefaultToNearest)
	if hMon == 0 {
		return rect{}, false
	}
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); ok != 0 && !mi.RcMonitor.Empty() {
		return mi.RcMonitor, true
	}
	return rect{}, false
}

func monitorBoundsForWindow(hwnd uintptr) (rect, bool) {
	if hwnd == 0 {
		return rect{}, false
	}
	hMon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if hMon == 0 {
		return rect{}, false
	}
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); ok != 0 && !mi.RcMonitor.Empty() {
		return mi.RcMonitor, true
	}
	return rect{}, false
}

func argbToCOLORREF(argb uint32) uint32 {
	r := (argb >> 16) & 0xFF
	g := (argb >> 8) & 0xFF
	b := argb & 0xFF
	return (b << 16) | (g << 8) | r
}

func copyRectFromLParam(lParam uintptr) (rect, bool) {
	if lParam == 0 || procRtlMoveMemory.Find() != nil {
		return rect{}, false
	}
	var r rect
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&r)), lParam, unsafe.Sizeof(r))
	return r, true
}

func isTaskbarRegionClippingChild(taskbarHWND uintptr, localChild rect) bool {
	if taskbarHWND == 0 || localChild.Empty() || procGetWindowRgnBox.Find() != nil {
		return false
	}
	var rgnBox rect
	boxType, _, _ := procGetWindowRgnBox.Call(taskbarHWND, uintptr(unsafe.Pointer(&rgnBox)))
	if boxType == rgnError {
		return false
	}
	if boxType == nullRegion {
		return true
	}
	if procCreateRectRgn.Find() == nil && procGetWindowRgn.Find() == nil && procRectInRegion.Find() == nil {
		hRgn, _, _ := procCreateRectRgn.Call(0, 0, 0, 0)
		if hRgn != 0 {
			defer procDeleteObject.Call(hRgn)
			rType, _, _ := procGetWindowRgn.Call(taskbarHWND, hRgn)
			if rType != rgnError {
				inRgn, _, _ := procRectInRegion.Call(hRgn, uintptr(unsafe.Pointer(&localChild)))
				return inRgn == 0
			}
		}
	}
	return localChild.Right <= rgnBox.Left || localChild.Left >= rgnBox.Right ||
		localChild.Bottom <= rgnBox.Top || localChild.Top >= rgnBox.Bottom
}

func getWindowStyle(hwnd uintptr) uint32 {
	if hwnd == 0 {
		return 0
	}
	v, _, _ := procGetWindowLongW.Call(hwnd, gwlStyle)
	return uint32(v)
}

func getWindowExStyle(hwnd uintptr) uint32 {
	if hwnd == 0 {
		return 0
	}
	v, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
	return uint32(v)
}

func setWindowStyle(hwnd uintptr, style uint32) {
	if hwnd != 0 {
		_, _, _ = procSetWindowLongW.Call(hwnd, gwlStyle, uintptr(style))
	}
}

func setWindowExStyle(hwnd uintptr, exStyle uint32) {
	if hwnd != 0 {
		_, _, _ = procSetWindowLongW.Call(hwnd, gwlExStyle, uintptr(exStyle))
	}
}

func getParentWindow(hwnd uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	p, _, _ := procGetParent.Call(hwnd)
	return p
}

func getTopChildWindow(parent uintptr) uintptr {
	if parent == 0 {
		return 0
	}
	c, _, _ := procGetTopWindow.Call(parent)
	return c
}

func screenToClientPoint(parent uintptr, x, y int32) (int32, int32, bool) {
	if parent == 0 {
		return x, y, false
	}
	pt := point{X: x, Y: y}
	ok, _, _ := procScreenToClient.Call(parent, uintptr(unsafe.Pointer(&pt)))
	return pt.X, pt.Y, ok != 0
}

func getWindowProcessID(hwnd uintptr) uint32 {
	if hwnd == 0 {
		return 0
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func getProcessExeName(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	var buf [512]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil || size == 0 {
		return ""
	}
	full := windows.UTF16ToString(buf[:size])
	if idx := strings.LastIndexAny(full, `\/`); idx >= 0 {
		full = full[idx+1:]
	}
	return strings.ToLower(full)
}

func getWindowVisibleBounds(hwnd uintptr) (rect, bool) {
	if hwnd == 0 {
		return rect{}, false
	}
	if procDwmGetWindowAttribute.Find() == nil {
		var r rect
		hr, _, _ := procDwmGetWindowAttribute.Call(
			hwnd,
			dwmwaExtendedFrameBounds,
			uintptr(unsafe.Pointer(&r)),
			unsafe.Sizeof(r),
		)
		if hr == 0 && !r.Empty() {
			return r, true
		}
	}
	return getWindowRect(hwnd)
}

func queryUserNotificationState() (uint32, bool) {
	if procSHQueryUserNotificationState.Find() != nil {
		return 0, false
	}
	var state uint32
	hr, _, _ := procSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state)))
	if hr != 0 {
		return 0, false
	}
	return state, true
}

// isShellOrFlyoutWindow reports whether a window belongs to the Windows desktop,
// taskbar, Alt+Tab switcher, Start/Search, tray overflow, or shell flyouts that
// should never hide the taskbar speed meter.
func isShellOrFlyoutWindow(className, exeName string) bool {
	switch className {
	case "Progman",
		"WorkerW",
		"Shell_TrayWnd",
		"Shell_SecondaryTrayWnd",
		"Windows.UI.Core.CoreWindow",
		"XamlExplorerHostIslandWindow",
		"TopLevelWindowForOverflowXamlIsland",
		"ControlCenterWindow",
		"NotificationCenterWindow",
		"Shell_InputSwitchTopLevelWindow",
		"MultitaskingViewFrame",
		"TaskSwitcherWnd",
		"ForegroundStaging",
		"NotifyIconOverflowWindow",
		"TaskListThumbnailWnd",
		"WinUIDesktopWin32WindowClass",
		"Windows.UI.Composition.DesktopWindowContentBridge",
		"Windows.UI.Input.InputSite.WindowClass":
		return true
	}

	switch strings.ToLower(exeName) {
	case "explorer.exe",
		"startmenuexperiencehost.exe",
		"searchhost.exe",
		"searchapp.exe",
		"shellexperiencehost.exe",
		"shellhost.exe",
		"textinputhost.exe",
		"lockapp.exe",
		"widgets.exe",
		"widgetboard.exe",
		"wallpaper32.exe",
		"wallpaper64.exe",
		"webwallpaper32.exe",
		"lively.exe",
		"rainmeter.exe",
		"translucenttb.exe",
		"roundedtb.exe",
		"f.lux.exe",
		"twinkle tray.exe",
		"monitorian.exe":
		return true
	}
	return false
}
