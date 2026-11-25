package ui

import (
	"golang.org/x/sys/windows"
)

const (
	wsPopup = 0x80000000

	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
	wsExLayered    = 0x00080000
	wsExNoActivate = 0x08000000

	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmSettingChange = 0x001A
	wmContextMenu   = 0x007B
	wmDisplayChange = 0x007E
	wmTimer         = 0x0113
	wmMouseMove     = 0x0200
	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmMouseLeave    = 0x02A3
	wmDpiChanged    = 0x02E0
	wmApp           = 0x8000

	wmTrayCallback = wmApp + 1

	swHide         = 0
	swShowNoActive = 4

	hwndTopmost = ^uintptr(0) // (HWND)-1
	swpNoSize   = 0x0001
	swpNoMove   = 0x0002
	swpNoActivate = 0x0010

	gwHwndNext = 2

	monitorDefaultToNearest = 2

	smCxSmIcon = 49
	smCySmIcon = 50

	ulwAlpha   = 0x00000002
	acSrcOver  = 0x00
	acSrcAlpha = 0x01

	biRGB = 0

	tmeLeave = 0x00000002

	abmGetState = 0x00000004
	absAutohide = 0x00000001

	dwmwaCloaked = 14

	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	notifyIconVersion4 = 4

	miimState   = 0x00000001
	miimID      = 0x00000002
	miimFType   = 0x00000100
	miimString  = 0x00000040

	mftString     = 0x00000000
	mftSeparator  = 0x00000800
	mftRadioCheck = 0x00000200

	mfsChecked   = 0x00000008
	mfsUnchecked = 0x00000000

	tpmLeftAlign   = 0x0000
	tpmBottomAlign = 0x0020
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNonotify    = 0x0080

	dpiAwarenessPerMonitorAwareV2 = ^uintptr(3) // -4
	dpiAwarenessPerMonitorAware   = ^uintptr(2) // -3

	idcArrow = 32512

	lfFaceSize = 32
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGdi32    = windows.NewLazySystemDLL("gdi32.dll")
	modShell32  = windows.NewLazySystemDLL("shell32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modDwmapi   = windows.NewLazySystemDLL("dwmapi.dll")

	procGetModuleHandleW             = modKernel32.NewProc("GetModuleHandleW")
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
	procShowWindow                   = modUser32.NewProc("ShowWindow")
	procIsWindowVisible              = modUser32.NewProc("IsWindowVisible")
	procIsIconic                     = modUser32.NewProc("IsIconic")
	procSetWindowPos                 = modUser32.NewProc("SetWindowPos")
	procSetTimer                     = modUser32.NewProc("SetTimer")
	procKillTimer                    = modUser32.NewProc("KillTimer")
	procFindWindowW                  = modUser32.NewProc("FindWindowW")
	procFindWindowExW                = modUser32.NewProc("FindWindowExW")
	procGetWindowRect                = modUser32.NewProc("GetWindowRect")
	procGetClassNameW                = modUser32.NewProc("GetClassNameW")
	procGetForegroundWindow          = modUser32.NewProc("GetForegroundWindow")
	procSetForegroundWindow          = modUser32.NewProc("SetForegroundWindow")
	procGetTopWindow                 = modUser32.NewProc("GetTopWindow")
	procGetWindow                    = modUser32.NewProc("GetWindow")
	procMonitorFromWindow            = modUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW              = modUser32.NewProc("GetMonitorInfoW")
	procGetDpiForWindow              = modUser32.NewProc("GetDpiForWindow")
	procGetDpiForSystem              = modUser32.NewProc("GetDpiForSystem")
	procGetSystemMetrics             = modUser32.NewProc("GetSystemMetrics")
	procGetSystemMetricsForDpi       = modUser32.NewProc("GetSystemMetricsForDpi")
	procGetDC                        = modUser32.NewProc("GetDC")
	procReleaseDC                    = modUser32.NewProc("ReleaseDC")
	procUpdateLayeredWindow          = modUser32.NewProc("UpdateLayeredWindow")
	procTrackMouseEvent              = modUser32.NewProc("TrackMouseEvent")
	procGetCursorPos                 = modUser32.NewProc("GetCursorPos")
	procCreatePopupMenu              = modUser32.NewProc("CreatePopupMenu")
	procDestroyMenu                  = modUser32.NewProc("DestroyMenu")
	procInsertMenuItemW              = modUser32.NewProc("InsertMenuItemW")
	procTrackPopupMenuEx             = modUser32.NewProc("TrackPopupMenuEx")
	procDestroyIcon                  = modUser32.NewProc("DestroyIcon")

	procCreateCompatibleDC = modGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = modGdi32.NewProc("DeleteDC")
	procCreateDIBSection   = modGdi32.NewProc("CreateDIBSection")
	procSelectObject       = modGdi32.NewProc("SelectObject")
	procDeleteObject       = modGdi32.NewProc("DeleteObject")

	procShellNotifyIconW = modShell32.NewProc("Shell_NotifyIconW")
	procSHAppBarMessage  = modShell32.NewProc("SHAppBarMessage")

	procDwmGetWindowAttribute = modDwmapi.NewProc("DwmGetWindowAttribute")
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
