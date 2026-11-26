package ui

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/assets"
)

const svgSubpixelScale = 64.0

type pathSegmentKind int

const (
	segMove pathSegmentKind = iota
	segLine
	segCubic
	segClose
)

type pathSegment struct {
	kind pathSegmentKind
	pts  [3]gpPointI
}

type parsedSVG struct {
	viewSize float64
	figures  [][]pathSegment
}

func parseEmbeddedSVG(assetPath string) (*parsedSVG, error) {
	raw, err := assets.Icons.ReadFile(assetPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded svg %q: %w", assetPath, err)
	}

	var doc struct {
		ViewBox string `xml:"viewBox,attr"`
		Paths   []struct {
			D string `xml:"d,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal svg %q: %w", assetPath, err)
	}
	if len(doc.Paths) == 0 {
		return nil, fmt.Errorf("svg %q contains no <path> elements", assetPath)
	}

	viewSize := 256.0
	if doc.ViewBox != "" {
		fields := strings.Fields(strings.ReplaceAll(doc.ViewBox, ",", " "))
		if len(fields) == 4 {
			if w, err := strconv.ParseFloat(fields[2], 64); err == nil && w > 0 {
				viewSize = w
			}
		}
	}

	var figures [][]pathSegment
	for _, p := range doc.Paths {
		segs, err := parseSVGPathData(p.D)
		if err != nil {
			return nil, fmt.Errorf("parse path in %q: %w", assetPath, err)
		}
		figures = append(figures, segs...)
	}
	return &parsedSVG{viewSize: viewSize, figures: figures}, nil
}

func scaleCoord(v float64) int32 {
	return int32(math.Round(v * svgSubpixelScale))
}

func parseSVGPathData(d string) ([][]pathSegment, error) {
	toks := tokenizeSVGPath(d)
	var figures [][]pathSegment
	var curFig []pathSegment

	var curX, curY float64
	var startX, startY float64
	var lastCtrlX, lastCtrlY float64
	var lastCmd byte

	flushFig := func() {
		if len(curFig) > 0 {
			figures = append(figures, curFig)
			curFig = nil
		}
	}

	i := 0
	readNum := func() (float64, error) {
		if i >= len(toks) {
			return 0, fmt.Errorf("unexpected end of SVG path data")
		}
		v, err := strconv.ParseFloat(toks[i], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid SVG number %q: %w", toks[i], err)
		}
		i++
		return v, nil
	}

	readFlag := func() (bool, error) {
		if i >= len(toks) {
			return false, fmt.Errorf("unexpected end of SVG arc flags")
		}
		v := toks[i]
		i++
		return v != "0", nil
	}

	isCmd := func(s string) bool {
		return len(s) == 1 && unicode.IsLetter(rune(s[0]))
	}

	var cmd byte
	for i < len(toks) {
		if isCmd(toks[i]) {
			cmd = toks[i][0]
			i++
		} else if cmd == 0 {
			return nil, fmt.Errorf("expected path command, got %q", toks[i])
		} else if cmd == 'M' {
			cmd = 'L'
		} else if cmd == 'm' {
			cmd = 'l'
		}

		switch cmd {
		case 'M', 'm':
			x, err := readNum()
			if err != nil {
				return nil, err
			}
			y, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'm' {
				x += curX
				y += curY
			}
			flushFig()
			curX, curY = x, y
			startX, startY = x, y
			curFig = append(curFig, pathSegment{
				kind: segMove,
				pts:  [3]gpPointI{{scaleCoord(x), scaleCoord(y)}},
			})
			lastCmd = cmd
			// Subsequent coordinate pairs are implicit L/l.
			for i < len(toks) && !isCmd(toks[i]) {
				nx, err := readNum()
				if err != nil {
					return nil, err
				}
				ny, err := readNum()
				if err != nil {
					return nil, err
				}
				if cmd == 'm' {
					nx += curX
					ny += curY
				}
				curFig = append(curFig, pathSegment{
					kind: segLine,
					pts:  [3]gpPointI{{scaleCoord(curX), scaleCoord(curY)}, {scaleCoord(nx), scaleCoord(ny)}},
				})
				curX, curY = nx, ny
				lastCmd = 'L'
			}

		case 'L', 'l':
			x, err := readNum()
			if err != nil {
				return nil, err
			}
			y, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'l' {
				x += curX
				y += curY
			}
			curFig = append(curFig, pathSegment{
				kind: segLine,
				pts:  [3]gpPointI{{scaleCoord(curX), scaleCoord(curY)}, {scaleCoord(x), scaleCoord(y)}},
			})
			curX, curY = x, y
			lastCmd = cmd

		case 'H', 'h':
			x, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'h' {
				x += curX
			}
			curFig = append(curFig, pathSegment{
				kind: segLine,
				pts:  [3]gpPointI{{scaleCoord(curX), scaleCoord(curY)}, {scaleCoord(x), scaleCoord(curY)}},
			})
			curX = x
			lastCmd = cmd

		case 'V', 'v':
			y, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'v' {
				y += curY
			}
			curFig = append(curFig, pathSegment{
				kind: segLine,
				pts:  [3]gpPointI{{scaleCoord(curX), scaleCoord(curY)}, {scaleCoord(curX), scaleCoord(y)}},
			})
			curY = y
			lastCmd = cmd

		case 'C', 'c':
			x1, err := readNum()
			if err != nil {
				return nil, err
			}
			y1, err := readNum()
			if err != nil {
				return nil, err
			}
			x2, err := readNum()
			if err != nil {
				return nil, err
			}
			y2, err := readNum()
			if err != nil {
				return nil, err
			}
			x, err := readNum()
			if err != nil {
				return nil, err
			}
			y, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'c' {
				x1 += curX
				y1 += curY
				x2 += curX
				y2 += curY
				x += curX
				y += curY
			}
			curFig = append(curFig, pathSegment{
				kind: segCubic,
				pts: [3]gpPointI{
					{scaleCoord(x1), scaleCoord(y1)},
					{scaleCoord(x2), scaleCoord(y2)},
					{scaleCoord(x), scaleCoord(y)},
				},
			})
			lastCtrlX, lastCtrlY = x2, y2
			curX, curY = x, y
			lastCmd = cmd

		case 'S', 's':
			x1, y1 := curX, curY
			if lastCmd == 'C' || lastCmd == 'c' || lastCmd == 'S' || lastCmd == 's' {
				x1 = 2*curX - lastCtrlX
				y1 = 2*curY - lastCtrlY
			}
			x2, err := readNum()
			if err != nil {
				return nil, err
			}
			y2, err := readNum()
			if err != nil {
				return nil, err
			}
			x, err := readNum()
			if err != nil {
				return nil, err
			}
			y, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 's' {
				x2 += curX
				y2 += curY
				x += curX
				y += curY
			}
			curFig = append(curFig, pathSegment{
				kind: segCubic,
				pts: [3]gpPointI{
					{scaleCoord(x1), scaleCoord(y1)},
					{scaleCoord(x2), scaleCoord(y2)},
					{scaleCoord(x), scaleCoord(y)},
				},
			})
			lastCtrlX, lastCtrlY = x2, y2
			curX, curY = x, y
			lastCmd = cmd

		case 'Q', 'q':
			qx, err := readNum()
			if err != nil {
				return nil, err
			}
			qy, err := readNum()
			if err != nil {
				return nil, err
			}
			x, err := readNum()
			if err != nil {
				return nil, err
			}
			y, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'q' {
				qx += curX
				qy += curY
				x += curX
				y += curY
			}
			c1x := curX + (2.0/3.0)*(qx-curX)
			c1y := curY + (2.0/3.0)*(qy-curY)
			c2x := x + (2.0/3.0)*(qx-x)
			c2y := y + (2.0/3.0)*(qy-y)
			curFig = append(curFig, pathSegment{
				kind: segCubic,
				pts: [3]gpPointI{
					{scaleCoord(c1x), scaleCoord(c1y)},
					{scaleCoord(c2x), scaleCoord(c2y)},
					{scaleCoord(x), scaleCoord(y)},
				},
			})
			lastCtrlX, lastCtrlY = qx, qy
			curX, curY = x, y
			lastCmd = cmd

		case 'T', 't':
			qx, qy := curX, curY
			if lastCmd == 'Q' || lastCmd == 'q' || lastCmd == 'T' || lastCmd == 't' {
				qx = 2*curX - lastCtrlX
				qy = 2*curY - lastCtrlY
			}
			x, err := readNum()
			if err != nil {
				return nil, err
			}
			y, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 't' {
				x += curX
				y += curY
			}
			c1x := curX + (2.0/3.0)*(qx-curX)
			c1y := curY + (2.0/3.0)*(qy-curY)
			c2x := x + (2.0/3.0)*(qx-x)
			c2y := y + (2.0/3.0)*(qy-y)
			curFig = append(curFig, pathSegment{
				kind: segCubic,
				pts: [3]gpPointI{
					{scaleCoord(c1x), scaleCoord(c1y)},
					{scaleCoord(c2x), scaleCoord(c2y)},
					{scaleCoord(x), scaleCoord(y)},
				},
			})
			lastCtrlX, lastCtrlY = qx, qy
			curX, curY = x, y
			lastCmd = cmd

		case 'A', 'a':
			rx, err := readNum()
			if err != nil {
				return nil, err
			}
			ry, err := readNum()
			if err != nil {
				return nil, err
			}
			xRot, err := readNum()
			if err != nil {
				return nil, err
			}
			largeArc, err := readFlag()
			if err != nil {
				return nil, err
			}
			sweep, err := readFlag()
			if err != nil {
				return nil, err
			}
			x, err := readNum()
			if err != nil {
				return nil, err
			}
			y, err := readNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'a' {
				x += curX
				y += curY
			}
			beziers := arcToCubicBeziers(curX, curY, rx, ry, xRot, largeArc, sweep, x, y)
			if len(beziers) == 0 {
				curFig = append(curFig, pathSegment{
					kind: segLine,
					pts:  [3]gpPointI{{scaleCoord(curX), scaleCoord(curY)}, {scaleCoord(x), scaleCoord(y)}},
				})
			} else {
				for _, b := range beziers {
					curFig = append(curFig, pathSegment{
						kind: segCubic,
						pts: [3]gpPointI{
							{scaleCoord(b[0]), scaleCoord(b[1])},
							{scaleCoord(b[2]), scaleCoord(b[3])},
							{scaleCoord(b[4]), scaleCoord(b[5])},
						},
					})
				}
			}
			curX, curY = x, y
			lastCmd = cmd

		case 'Z', 'z':
			curFig = append(curFig, pathSegment{kind: segClose})
			curX, curY = startX, startY
			flushFig()
			lastCmd = cmd

		default:
			return nil, fmt.Errorf("unsupported SVG path command %q", string(cmd))
		}
	}
	flushFig()
	return figures, nil
}

// tokenizeSVGPath splits SVG path data into commands and numeric tokens, handling
// compact negative signs (e.g. "0-3.43"), packed decimals (".14.5"), and arc flags.
func tokenizeSVGPath(d string) []string {
	var tokens []string
	n := len(d)
	i := 0
	var activeCmd byte
	arcParamIdx := 0

	skipSep := func() {
		for i < n && (d[i] == ' ' || d[i] == '\t' || d[i] == '\n' || d[i] == '\r' || d[i] == ',') {
			i++
		}
	}

	for {
		skipSep()
		if i >= n {
			break
		}
		ch := d[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			tokens = append(tokens, string(ch))
			activeCmd = ch
			arcParamIdx = 0
			i++
			continue
		}

		// In SVG A/a commands, parameter indices 3 and 4 (0-based) are single-digit 0/1 flags.
		if (activeCmd == 'A' || activeCmd == 'a') && (arcParamIdx%7 == 3 || arcParamIdx%7 == 4) {
			if ch == '0' || ch == '1' {
				tokens = append(tokens, string(ch))
				arcParamIdx++
				i++
				continue
			}
		}

		start := i
		if ch == '+' || ch == '-' {
			i++
		}
		seenDot := false
		seenExp := false
		for i < n {
			c := d[i]
			if c >= '0' && c <= '9' {
				i++
				continue
			}
			if c == '.' && !seenDot && !seenExp {
				seenDot = true
				i++
				continue
			}
			if (c == 'e' || c == 'E') && !seenExp {
				seenExp = true
				i++
				if i < n && (d[i] == '+' || d[i] == '-') {
					i++
				}
				continue
			}
			break
		}
		if i == start {
			i++
			continue
		}
		tokens = append(tokens, d[start:i])
		if activeCmd == 'A' || activeCmd == 'a' {
			arcParamIdx++
		}
	}
	return tokens
}

// arcToCubicBeziers converts an SVG elliptical arc (W3C SVG 1.1 F.6.5/F.6.6)
// into cubic Bezier curves [x1, y1, x2, y2, x, y].
func arcToCubicBeziers(x1, y1, rx, ry, angleDeg float64, largeArc, sweep bool, x2, y2 float64) [][6]float64 {
	if (x1 == x2 && y1 == y2) || rx == 0 || ry == 0 {
		return nil
	}
	rx = math.Abs(rx)
	ry = math.Abs(ry)

	phi := angleDeg * math.Pi / 180.0
	cosPhi := math.Cos(phi)
	sinPhi := math.Sin(phi)

	dx2 := (x1 - x2) / 2.0
	dy2 := (y1 - y2) / 2.0
	x1p := cosPhi*dx2 + sinPhi*dy2
	y1p := -sinPhi*dx2 + cosPhi*dy2

	rxSq := rx * rx
	rySq := ry * ry
	x1pSq := x1p * x1p
	y1pSq := y1p * y1p

	lambda := x1pSq/rxSq + y1pSq/rySq
	if lambda > 1.0 {
		scale := math.Sqrt(lambda)
		rx *= scale
		ry *= scale
		rxSq = rx * rx
		rySq = ry * ry
	}

	num := math.Max(0, rxSq*rySq-rxSq*y1pSq-rySq*x1pSq)
	den := rxSq*y1pSq + rySq*x1pSq
	if den == 0 {
		return nil
	}
	factor := math.Sqrt(num / den)
	if largeArc == sweep {
		factor = -factor
	}
	cxp := factor * (rx * y1p / ry)
	cyp := factor * (-ry * x1p / rx)

	cx := cosPhi*cxp - sinPhi*cyp + (x1+x2)/2.0
	cy := sinPhi*cxp + cosPhi*cyp + (y1+y2)/2.0

	vecAngle := func(ux, uy, vx, vy float64) float64 {
		dot := ux*vx + uy*vy
		lenProd := math.Hypot(ux, uy) * math.Hypot(vx, vy)
		if lenProd == 0 {
			return 0
		}
		cosVal := math.Max(-1.0, math.Min(1.0, dot/lenProd))
		ang := math.Acos(cosVal)
		if ux*vy-uy*vx < 0 {
			ang = -ang
		}
		return ang
	}

	theta1 := vecAngle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dTheta := vecAngle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dTheta > 0 {
		dTheta -= 2 * math.Pi
	} else if sweep && dTheta < 0 {
		dTheta += 2 * math.Pi
	}

	segs := int(math.Ceil(math.Abs(dTheta) / (math.Pi / 2.0)))
	if segs < 1 {
		segs = 1
	}
	delta := dTheta / float64(segs)
	alpha := (4.0 / 3.0) * math.Tan(delta/4.0)

	out := make([][6]float64, 0, segs)
	for s := 0; s < segs; s++ {
		t1 := theta1 + float64(s)*delta
		t2 := theta1 + float64(s+1)*delta

		cos1, sin1 := math.Cos(t1), math.Sin(t1)
		cos2, sin2 := math.Cos(t2), math.Sin(t2)

		p1x := cos1 - alpha*sin1
		p1y := sin1 + alpha*cos1
		p2x := cos2 + alpha*sin2
		p2y := sin2 - alpha*cos2

		mapPt := func(ux, uy float64) (float64, float64) {
			ex := rx * ux
			ey := ry * uy
			return cx + cosPhi*ex - sinPhi*ey, cy + sinPhi*ex + cosPhi*ey
		}

		c1x, c1y := mapPt(p1x, p1y)
		c2x, c2y := mapPt(p2x, p2y)
		ex, ey := mapPt(cos2, sin2)
		out = append(out, [6]float64{c1x, c1y, c2x, c2y, ex, ey})
	}
	return out
}

func (svg *parsedSVG) buildGpPath(destX, destY, destW, destH int32) (uintptr, error) {
	var gpPath uintptr
	st, _, _ := procGdipCreatePath.Call(gpFillModeWinding, uintptr(unsafe.Pointer(&gpPath)))
	if st != 0 || gpPath == 0 {
		return 0, fmt.Errorf("GdipCreatePath failed with status %d", st)
	}

	for _, fig := range svg.figures {
		_, _, _ = procGdipStartPathFigure.Call(gpPath)
		var startPt, lastPt gpPointI
		for _, seg := range fig {
			switch seg.kind {
			case segMove:
				startPt = seg.pts[0]
				lastPt = seg.pts[0]
			case segLine:
				_, _, _ = procGdipAddPathLineI.Call(
					gpPath,
					uintptr(seg.pts[0].X),
					uintptr(seg.pts[0].Y),
					uintptr(seg.pts[1].X),
					uintptr(seg.pts[1].Y),
				)
				lastPt = seg.pts[1]
			case segCubic:
				pts := [4]gpPointI{lastPt, seg.pts[0], seg.pts[1], seg.pts[2]}
				_, _, _ = procGdipAddPathBeziersI.Call(
					gpPath,
					uintptr(unsafe.Pointer(&pts[0])),
					4,
				)
				lastPt = seg.pts[2]
			case segClose:
				_, _, _ = procGdipClosePathFigure.Call(gpPath)
				lastPt = startPt
			}
		}
	}

	srcSize := scaleCoord(svg.viewSize)
	srcRect := gpRectI{X: 0, Y: 0, Width: srcSize, Height: srcSize}
	dstPlg := [3]gpPointI{
		{X: destX, Y: destY},
		{X: destX + destW, Y: destY},
		{X: destX, Y: destY + destH},
	}

	var matrix uintptr
	st, _, _ = procGdipCreateMatrix3I.Call(
		uintptr(unsafe.Pointer(&srcRect)),
		uintptr(unsafe.Pointer(&dstPlg[0])),
		uintptr(unsafe.Pointer(&matrix)),
	)
	if st != 0 || matrix == 0 {
		_, _, _ = procGdipDeletePath.Call(gpPath)
		return 0, fmt.Errorf("GdipCreateMatrix3I failed with status %d", st)
	}
	defer procGdipDeleteMatrix.Call(matrix)

	_, _, _ = procGdipTransformPath.Call(gpPath, matrix)
	return gpPath, nil
}

// RasterizeSVGToBitmap renders an embedded Phosphor SVG into a new GDI+ 32bppPARGB bitmap
// of size width x height with the given ARGB foreground color. Caller owns the bitmap via
// GdipDisposeImage.
func RasterizeSVGToBitmap(assetPath string, width, height int32, argb uint32) (uintptr, []byte, error) {
	svg, err := parseEmbeddedSVG(assetPath)
	if err != nil {
		return 0, nil, err
	}

	buf := make([]byte, int(width)*int(height)*4)
	var gpBitmap uintptr
	st, _, _ := procGdipCreateBitmapFromScan0.Call(
		uintptr(width),
		uintptr(height),
		uintptr(width*4),
		gpPixelFormat32bppPARGB,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&gpBitmap)),
	)
	if st != 0 || gpBitmap == 0 {
		return 0, nil, fmt.Errorf("GdipCreateBitmapFromScan0 failed with status %d", st)
	}

	var graphics uintptr
	st, _, _ = procGdipGetImageGraphicsContext.Call(gpBitmap, uintptr(unsafe.Pointer(&graphics)))
	if st != 0 || graphics == 0 {
		_, _, _ = procGdipDisposeImage.Call(gpBitmap)
		return 0, nil, fmt.Errorf("GdipGetImageGraphicsContext failed with status %d", st)
	}
	defer procGdipDeleteGraphics.Call(graphics)

	_, _, _ = procGdipGraphicsClear.Call(graphics, 0x00000000)
	_, _, _ = procGdipSetSmoothingMode.Call(graphics, gpSmoothingModeHighQuality)

	brush, err := gpCreateSolidBrush(argb)
	if err != nil {
		_, _, _ = procGdipDisposeImage.Call(gpBitmap)
		return 0, nil, err
	}
	defer procGdipDeleteBrush.Call(brush)

	gpPath, err := svg.buildGpPath(0, 0, width, height)
	if err != nil {
		_, _, _ = procGdipDisposeImage.Call(gpBitmap)
		return 0, nil, err
	}
	defer procGdipDeletePath.Call(gpPath)

	_, _, _ = procGdipFillPath.Call(graphics, brush, gpPath)
	return gpBitmap, buf, nil
}

// CreateTrayHICON rasterizes an embedded Phosphor SVG into a native Win32 HICON sized
// for the system tray at the given DPI. Caller must destroy the returned HICON with DestroyIcon.
func CreateTrayHICON(assetPath string, dpi uint32, argb uint32) (uintptr, error) {
	w, h := getSmallIconMetrics(dpi)
	return CreateSizedHICON(assetPath, w, h, argb)
}

// CreateSizedHICON rasterizes an embedded Phosphor SVG into an HICON of exact pixel dimensions.
// Caller must destroy the returned HICON with DestroyIcon.
func CreateSizedHICON(assetPath string, width, height int32, argb uint32) (uintptr, error) {
	gpBitmap, buf, err := RasterizeSVGToBitmap(assetPath, width, height, argb)
	if err != nil {
		return 0, err
	}
	defer procGdipDisposeImage.Call(gpBitmap)
	_ = buf // Keep backing pixel buffer alive until GdipCreateHICONFromBitmap completes.

	var hIcon uintptr
	st, _, _ := procGdipCreateHICONFromBitmap.Call(gpBitmap, uintptr(unsafe.Pointer(&hIcon)))
	if st != 0 || hIcon == 0 {
		return 0, fmt.Errorf("GdipCreateHICONFromBitmap failed with status %d", st)
	}
	return hIcon, nil
}
