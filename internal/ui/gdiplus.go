package ui

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	gpPixelFormat32bppPARGB = 0x000E200B

	gpSmoothingModeHighQuality           = 2
	gpInterpolationModeHighQualityBicubic = 7
	gpTextRenderingHintAntiAliasGridFit  = 3
	gpTextRenderingHintAntiAlias         = 4
	gpFillModeWinding                 = 1
	gpStringAlignmentNear             = 0
	gpStringAlignmentCenter           = 1
	gpStringAlignmentFar              = 2
	gpStringFormatFlagsNoWrap         = 0x00001000
	gpStringFormatFlagsNoClip         = 0x00004000
	gpStringFormatFlagsMeasureTrail   = 0x00000800
)

var (
	modGdiplus = windows.NewLazySystemDLL("gdiplus.dll")

	procGdiplusStartup                = modGdiplus.NewProc("GdiplusStartup")
	procGdiplusShutdown               = modGdiplus.NewProc("GdiplusShutdown")
	procGdipCreateBitmapFromScan0     = modGdiplus.NewProc("GdipCreateBitmapFromScan0")
	procGdipDisposeImage              = modGdiplus.NewProc("GdipDisposeImage")
	procGdipCreateHICONFromBitmap     = modGdiplus.NewProc("GdipCreateHICONFromBitmap")
	procGdipGetImageGraphicsContext   = modGdiplus.NewProc("GdipGetImageGraphicsContext")
	procGdipDeleteGraphics            = modGdiplus.NewProc("GdipDeleteGraphics")
	procGdipGraphicsClear             = modGdiplus.NewProc("GdipGraphicsClear")
	procGdipSetSmoothingMode          = modGdiplus.NewProc("GdipSetSmoothingMode")
	procGdipSetInterpolationMode      = modGdiplus.NewProc("GdipSetInterpolationMode")
	procGdipSetTextRenderingHint      = modGdiplus.NewProc("GdipSetTextRenderingHint")
	procGdipCreateSolidFill           = modGdiplus.NewProc("GdipCreateSolidFill")
	procGdipCreateLineBrushI          = modGdiplus.NewProc("GdipCreateLineBrushI")
	procGdipDeleteBrush               = modGdiplus.NewProc("GdipDeleteBrush")
	procGdipCreateFontFamilyFromName  = modGdiplus.NewProc("GdipCreateFontFamilyFromName")
	procGdipDeleteFontFamily          = modGdiplus.NewProc("GdipDeleteFontFamily")
	procGdipCreateFontFromLogfontW    = modGdiplus.NewProc("GdipCreateFontFromLogfontW")
	procGdipDeleteFont                = modGdiplus.NewProc("GdipDeleteFont")
	procGdipCreateStringFormat        = modGdiplus.NewProc("GdipCreateStringFormat")
	procGdipDeleteStringFormat        = modGdiplus.NewProc("GdipDeleteStringFormat")
	procGdipSetStringFormatAlign      = modGdiplus.NewProc("GdipSetStringFormatAlign")
	procGdipSetStringFormatLineAlign  = modGdiplus.NewProc("GdipSetStringFormatLineAlign")
	procGdipSetStringFormatFlags      = modGdiplus.NewProc("GdipSetStringFormatFlags")
	procGdipDrawString                = modGdiplus.NewProc("GdipDrawString")
	procGdipMeasureString             = modGdiplus.NewProc("GdipMeasureString")
	procGdipDrawImageRectI            = modGdiplus.NewProc("GdipDrawImageRectI")
	procGdipCreatePath                = modGdiplus.NewProc("GdipCreatePath")
	procGdipDeletePath                = modGdiplus.NewProc("GdipDeletePath")
	procGdipStartPathFigure           = modGdiplus.NewProc("GdipStartPathFigure")
	procGdipClosePathFigure           = modGdiplus.NewProc("GdipClosePathFigure")
	procGdipAddPathLineI              = modGdiplus.NewProc("GdipAddPathLineI")
	procGdipAddPathBeziersI           = modGdiplus.NewProc("GdipAddPathBeziersI")
	procGdipAddPathArcI               = modGdiplus.NewProc("GdipAddPathArcI")
	procGdipFillPath                  = modGdiplus.NewProc("GdipFillPath")
	procGdipCreateMatrix3I            = modGdiplus.NewProc("GdipCreateMatrix3I")
	procGdipDeleteMatrix              = modGdiplus.NewProc("GdipDeleteMatrix")
	procGdipTransformPath             = modGdiplus.NewProc("GdipTransformPath")
)

type gdiplusStartupInput struct {
	GdiplusVersion           uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

type gpRectF struct {
	X      float32
	Y      float32
	Width  float32
	Height float32
}

type gpRectI struct {
	X      int32
	Y      int32
	Width  int32
	Height int32
}

type gpPointI struct {
	X int32
	Y int32
}

// StartupGDIPlus initializes the Windows GDI+ subsystem and returns a shutdown token.
func StartupGDIPlus() (uintptr, error) {
	var token uintptr
	input := gdiplusStartupInput{GdiplusVersion: 1}
	status, _, _ := procGdiplusStartup.Call(
		uintptr(unsafe.Pointer(&token)),
		uintptr(unsafe.Pointer(&input)),
		0,
	)
	if status != 0 {
		return 0, fmt.Errorf("GdiplusStartup failed with status %d", status)
	}
	return token, nil
}

// ShutdownGDIPlus shuts down GDI+ for the given startup token.
func ShutdownGDIPlus(token uintptr) {
	if token != 0 {
		_, _, _ = procGdiplusShutdown.Call(token)
	}
}

func gpIsFontFamilyAvailable(name string) bool {
	u16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	var family uintptr
	status, _, _ := procGdipCreateFontFamilyFromName.Call(
		uintptr(unsafe.Pointer(u16)),
		0,
		uintptr(unsafe.Pointer(&family)),
	)
	if status != 0 || family == 0 {
		return false
	}
	_, _, _ = procGdipDeleteFontFamily.Call(family)
	return true
}

// ResolveTaskbarFontName selects the native Windows 11 Segoe UI Variable font when present,
// gracefully falling back to Segoe UI.
func ResolveTaskbarFontName() string {
	candidates := []string{
		"Segoe UI Variable Text",
		"Segoe UI Variable Small",
		"Segoe UI Variable",
		"Segoe UI",
	}
	for _, name := range candidates {
		if gpIsFontFamilyAvailable(name) {
			return name
		}
	}
	return "Segoe UI"
}

func gpCreateFontPx(hdc uintptr, family string, pixelHeight int32, weight int32) (uintptr, error) {
	var lf logFontW
	lf.LfHeight = -pixelHeight
	lf.LfWeight = weight
	lf.LfCharSet = 1 // DEFAULT_CHARSET
	lf.LfQuality = 4 // ANTIALIASED_QUALITY
	copyUTF16Fixed(lf.LfFaceName[:], family)

	var gpFont uintptr
	status, _, _ := procGdipCreateFontFromLogfontW.Call(
		hdc,
		uintptr(unsafe.Pointer(&lf)),
		uintptr(unsafe.Pointer(&gpFont)),
	)
	if status != 0 || gpFont == 0 {
		return 0, fmt.Errorf("GdipCreateFontFromLogfontW(%q) failed with status %d", family, status)
	}
	return gpFont, nil
}

func gpCreateSolidBrush(argb uint32) (uintptr, error) {
	var brush uintptr
	status, _, _ := procGdipCreateSolidFill.Call(uintptr(argb), uintptr(unsafe.Pointer(&brush)))
	if status != 0 || brush == 0 {
		return 0, fmt.Errorf("GdipCreateSolidFill failed with status %d", status)
	}
	return brush, nil
}

func gpCreateLinearBrushI(x1, y1, x2, y2 int32, color1, color2 uint32) (uintptr, error) {
	p1 := gpPointI{X: x1, Y: y1}
	p2 := gpPointI{X: x2, Y: y2}
	var brush uintptr
	// WrapModeTileFlipX (1) is required by GDI+ (WrapModeClamp is rejected by linear gradient brushes).
	status, _, _ := procGdipCreateLineBrushI.Call(
		uintptr(unsafe.Pointer(&p1)),
		uintptr(unsafe.Pointer(&p2)),
		uintptr(color1),
		uintptr(color2),
		1,
		uintptr(unsafe.Pointer(&brush)),
	)
	if status != 0 || brush == 0 {
		return 0, fmt.Errorf("GdipCreateLineBrushI failed with status %d", status)
	}
	return brush, nil
}

func gpCreateStringFormat(hAlign, vAlign int32) (uintptr, error) {
	var fmtHandle uintptr
	status, _, _ := procGdipCreateStringFormat.Call(0, 0, uintptr(unsafe.Pointer(&fmtHandle)))
	if status != 0 || fmtHandle == 0 {
		return 0, fmt.Errorf("GdipCreateStringFormat failed with status %d", status)
	}
	_, _, _ = procGdipSetStringFormatAlign.Call(fmtHandle, uintptr(hAlign))
	_, _, _ = procGdipSetStringFormatLineAlign.Call(fmtHandle, uintptr(vAlign))
	_, _, _ = procGdipSetStringFormatFlags.Call(
		fmtHandle,
		uintptr(gpStringFormatFlagsNoWrap|gpStringFormatFlagsNoClip),
	)
	return fmtHandle, nil
}

func gpMeasureString(graphics, font, strFmt uintptr, text string) (float32, float32) {
	u16, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return 0, 0
	}
	layout := gpRectF{Width: 1000, Height: 1000}
	var bounds gpRectF
	status, _, _ := procGdipMeasureString.Call(
		graphics,
		uintptr(unsafe.Pointer(u16)),
		^uintptr(0), // -1 = null-terminated
		font,
		uintptr(unsafe.Pointer(&layout)),
		strFmt,
		uintptr(unsafe.Pointer(&bounds)),
		0,
		0,
	)
	if status != 0 {
		return 0, 0
	}
	return bounds.Width, bounds.Height
}

func gpDrawString(graphics, font, strFmt, brush uintptr, text string, bounds gpRectF) {
	u16, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	_, _, _ = procGdipDrawString.Call(
		graphics,
		uintptr(unsafe.Pointer(u16)),
		^uintptr(0),
		font,
		uintptr(unsafe.Pointer(&bounds)),
		strFmt,
		brush,
	)
}

func gpFillRoundedRect(graphics, brush uintptr, x, y, w, h, r int32) {
	if w <= 0 || h <= 0 {
		return
	}
	if r <= 0 {
		var path uintptr
		if st, _, _ := procGdipCreatePath.Call(gpFillModeWinding, uintptr(unsafe.Pointer(&path))); st != 0 {
			return
		}
		defer procGdipDeletePath.Call(path)
		_, _, _ = procGdipAddPathLineI.Call(path, uintptr(x), uintptr(y), uintptr(x+w), uintptr(y))
		_, _, _ = procGdipAddPathLineI.Call(path, uintptr(x+w), uintptr(y), uintptr(x+w), uintptr(y+h))
		_, _, _ = procGdipAddPathLineI.Call(path, uintptr(x+w), uintptr(y+h), uintptr(x), uintptr(y+h))
		_, _, _ = procGdipClosePathFigure.Call(path)
		_, _, _ = procGdipFillPath.Call(graphics, brush, path)
		return
	}

	d := r * 2
	if d > w {
		d = w
	}
	if d > h {
		d = h
	}

	var path uintptr
	if st, _, _ := procGdipCreatePath.Call(gpFillModeWinding, uintptr(unsafe.Pointer(&path))); st != 0 {
		return
	}
	defer procGdipDeletePath.Call(path)

	_, _, _ = procGdipStartPathFigure.Call(path)
	// Note: GdipAddPathArcI takes float32 angles by value on x64 only in the last 2 parameters.
	// On Win64 ABI those two float32s would go in XMM registers, so instead draw rounded corners
	// via cubic Bezier approximations using pure integer GdipAddPathBeziersI (kappa = 0.55228475).
	r = d / 2
	k := (r*5523 + 5000) / 10000
	x0, y0, x1, y1 := x, y, x+w, y+h

	addCorner := func(p0, c1, c2, p3 gpPointI) {
		pts := [4]gpPointI{p0, c1, c2, p3}
		_, _, _ = procGdipAddPathBeziersI.Call(path, uintptr(unsafe.Pointer(&pts[0])), 4)
	}

	// Top-left corner -> top edge -> top-right corner
	addCorner(
		gpPointI{x0, y0 + r},
		gpPointI{x0, y0 + r - k},
		gpPointI{x0 + r - k, y0},
		gpPointI{x0 + r, y0},
	)
	if x1-r > x0+r {
		_, _, _ = procGdipAddPathLineI.Call(path, uintptr(x0+r), uintptr(y0), uintptr(x1-r), uintptr(y0))
	}
	addCorner(
		gpPointI{x1 - r, y0},
		gpPointI{x1 - r + k, y0},
		gpPointI{x1, y0 + r - k},
		gpPointI{x1, y0 + r},
	)
	if y1-r > y0+r {
		_, _, _ = procGdipAddPathLineI.Call(path, uintptr(x1), uintptr(y0+r), uintptr(x1), uintptr(y1-r))
	}
	addCorner(
		gpPointI{x1, y1 - r},
		gpPointI{x1, y1 - r + k},
		gpPointI{x1 - r + k, y1},
		gpPointI{x1 - r, y1},
	)
	if x1-r > x0+r {
		_, _, _ = procGdipAddPathLineI.Call(path, uintptr(x1-r), uintptr(y1), uintptr(x0+r), uintptr(y1))
	}
	addCorner(
		gpPointI{x0 + r, y1},
		gpPointI{x0 + r - k, y1},
		gpPointI{x0, y1 - r + k},
		gpPointI{x0, y1 - r},
	)
	_, _, _ = procGdipClosePathFigure.Call(path)
	_, _, _ = procGdipFillPath.Call(graphics, brush, path)
}
