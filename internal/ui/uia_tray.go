package ui

import (
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clsctxInprocServer = 0x1
	coinitMultithreaded = 0x0

	uiaTreeScopeDescendants = 4

	uiaBoundingRectanglePropertyID = 30001
	uiaAutomationIdPropertyID      = 30011
	uiaClassNamePropertyID         = 30012
	uiaIsOffscreenPropertyID       = 30022
)

var (
	clsidCUIAutomation = windows.GUID{
		Data1: 0xff48dba4,
		Data2: 0x60ef,
		Data3: 0x4201,
		Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e},
	}
	iidIUIAutomation = windows.GUID{
		Data1: 0x30cbe57d,
		Data2: 0xd9d0,
		Data3: 0x452a,
		Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee},
	}
)

type trayCandidate struct {
	className    string
	automationID string
	bounds       rect
	offscreen    bool
}

type cachedUIATray struct {
	taskbarHWND uintptr
	tbRect      rect
	trayRect    rect
	valid       bool
	updatedAt   time.Time
}

type trayProbeCache struct {
	mu       sync.Mutex
	started  bool
	stopCh   chan struct{}
	wakeCh   chan struct{}
	notifyFn func()

	pendingHWND uintptr
	pendingTB   rect
	pendingDPI  uint32
	pendingIgn  uintptr
	inFlight    bool

	result cachedUIATray
}

var globalTrayProbe = &trayProbeCache{}

func uiaVtblCall(obj unsafe.Pointer, slot int) uintptr {
	vptr := *(*unsafe.Pointer)(obj)
	return (*(*[96]uintptr)(vptr))[slot]
}

func uiaRelease(obj unsafe.Pointer) {
	if obj != nil {
		_, _, _ = syscall.SyscallN(uiaVtblCall(obj, 2), uintptr(obj))
	}
}

func uiaBSTRToString(bstr *uint16) string {
	if bstr == nil {
		return ""
	}
	defer procSysFreeString.Call(uintptr(unsafe.Pointer(bstr)))
	if procSysStringLen.Find() != nil {
		return ""
	}
	n, _, _ := procSysStringLen.Call(uintptr(unsafe.Pointer(bstr)))
	if n == 0 || n > 4096 {
		return ""
	}
	slice := unsafe.Slice(bstr, int(n))
	return windows.UTF16ToString(slice)
}

func isSystemTrayUIAElement(className, automationID string, r, tb rect, dpi uint32, isRTL bool) bool {
	if r.Empty() || tb.Empty() {
		return false
	}
	// Element must actually intersect the taskbar rect.
	if r.Right <= tb.Left || r.Left >= tb.Right || r.Bottom <= tb.Top || r.Top >= tb.Bottom {
		return false
	}

	isTrayIdent := strings.HasPrefix(className, "SystemTray.") || strings.Contains(className, "SystemTray")
	if !isTrayIdent {
		switch strings.ToLower(automationID) {
		case "systemtrayicon",
			"showhiddenicons",
			"controlcenterbutton",
			"notificationcenterbutton",
			"clockbutton",
			"inputswitch",
			"touchkeyboard",
			"penmenu",
			"virtualtouchpad",
			"showdesktop",
			"showdesktoppipe",
			"copilot",
			"copilotbutton":
			isTrayIdent = true
		}
	}
	if !isTrayIdent {
		return false
	}

	isVertical := tb.Height() > tb.Width()
	if !isVertical {
		// Reject outer full-width taskbar containers so pinned app icons never push the overlay.
		maxElemW := tb.Width() / 2
		if limit := scaleDPIInt(420, dpi); limit < maxElemW && limit > 0 {
			maxElemW = limit
		}
		if r.Width() > maxElemW {
			return false
		}
		if isRTL {
			return r.Right <= tb.Left+tb.Width()/2
		}
		return r.Left >= tb.Left+tb.Width()/2
	}

	maxElemH := tb.Height() / 2
	if limit := scaleDPIInt(420, dpi); limit < maxElemH && limit > 0 {
		maxElemH = limit
	}
	if r.Height() > maxElemH {
		return false
	}
	return r.Top >= tb.Top+tb.Height()/2
}

func unionRect(a rect, aOK bool, b rect, bOK bool) (rect, bool) {
	switch {
	case !aOK && !bOK:
		return rect{}, false
	case !aOK:
		return b, true
	case !bOK:
		return a, true
	}
	if b.Left < a.Left {
		a.Left = b.Left
	}
	if b.Top < a.Top {
		a.Top = b.Top
	}
	if b.Right > a.Right {
		a.Right = b.Right
	}
	if b.Bottom > a.Bottom {
		a.Bottom = b.Bottom
	}
	return a, true
}

func computeTrayUnionFromCandidates(candidates []trayCandidate, tb rect, dpi uint32, isRTL bool) (rect, bool) {
	var out rect
	var ok bool
	for _, c := range candidates {
		if c.offscreen {
			continue
		}
		if !isSystemTrayUIAElement(c.className, c.automationID, c.bounds, tb, dpi, isRTL) {
			continue
		}
		out, ok = unionRect(out, ok, c.bounds, true)
	}
	return out, ok
}

func probeSystemTrayViaUIA(uia unsafe.Pointer, taskbarHWND uintptr, tb rect, dpi uint32) (rect, bool) {
	if uia == nil || taskbarHWND == 0 || tb.Empty() {
		return rect{}, false
	}
	isRTL := (getWindowExStyle(taskbarHWND) & wsExLayoutRTL) != 0

	var tbElem unsafe.Pointer
	hr, _, _ := syscall.SyscallN(uiaVtblCall(uia, 6), uintptr(uia), taskbarHWND, uintptr(unsafe.Pointer(&tbElem))) // ElementFromHandle
	if int32(hr) < 0 || tbElem == nil {
		return rect{}, false
	}
	defer uiaRelease(tbElem)

	var trueCond unsafe.Pointer
	hr, _, _ = syscall.SyscallN(uiaVtblCall(uia, 21), uintptr(uia), uintptr(unsafe.Pointer(&trueCond))) // CreateTrueCondition
	if int32(hr) < 0 || trueCond == nil {
		return rect{}, false
	}
	defer uiaRelease(trueCond)

	var cacheReq unsafe.Pointer
	hr, _, _ = syscall.SyscallN(uiaVtblCall(uia, 20), uintptr(uia), uintptr(unsafe.Pointer(&cacheReq))) // CreateCacheRequest
	if int32(hr) < 0 || cacheReq == nil {
		return rect{}, false
	}
	defer uiaRelease(cacheReq)

	for _, propID := range []uintptr{
		uiaBoundingRectanglePropertyID,
		uiaAutomationIdPropertyID,
		uiaClassNamePropertyID,
		uiaIsOffscreenPropertyID,
	} {
		_, _, _ = syscall.SyscallN(uiaVtblCall(cacheReq, 3), uintptr(cacheReq), propID) // AddProperty
	}
	// Skip live control proxy creation for each descendant; cached properties alone suffice.
	_, _, _ = syscall.SyscallN(uiaVtblCall(cacheReq, 11), uintptr(cacheReq), 0) // put_AutomationElementMode(None)

	var arr unsafe.Pointer
	hr, _, _ = syscall.SyscallN(
		uiaVtblCall(tbElem, 8), // FindAllBuildCache
		uintptr(tbElem),
		uintptr(uiaTreeScopeDescendants),
		uintptr(trueCond),
		uintptr(cacheReq),
		uintptr(unsafe.Pointer(&arr)),
	)
	if int32(hr) < 0 || arr == nil {
		return rect{}, false
	}
	defer uiaRelease(arr)

	var length int32
	hr, _, _ = syscall.SyscallN(uiaVtblCall(arr, 3), uintptr(arr), uintptr(unsafe.Pointer(&length))) // get_Length
	if int32(hr) < 0 || length <= 0 {
		return rect{}, false
	}
	if length > 512 {
		length = 512
	}

	candidates := make([]trayCandidate, 0, 16)
	for i := int32(0); i < length; i++ {
		var elem unsafe.Pointer
		hr, _, _ = syscall.SyscallN(uiaVtblCall(arr, 4), uintptr(arr), uintptr(i), uintptr(unsafe.Pointer(&elem))) // GetElement
		if int32(hr) < 0 || elem == nil {
			continue
		}

		var offscreen int32
		hrOff, _, _ := syscall.SyscallN(uiaVtblCall(elem, 70), uintptr(elem), uintptr(unsafe.Pointer(&offscreen))) // CachedIsOffscreen
		if int32(hrOff) >= 0 && offscreen != 0 {
			uiaRelease(elem)
			continue
		}

		var bRect rect
		hrRect, _, _ := syscall.SyscallN(uiaVtblCall(elem, 75), uintptr(elem), uintptr(unsafe.Pointer(&bRect))) // CachedBoundingRectangle
		if int32(hrRect) < 0 || bRect.Empty() {
			uiaRelease(elem)
			continue
		}

		var clsBSTR, aidBSTR *uint16
		_, _, _ = syscall.SyscallN(uiaVtblCall(elem, 62), uintptr(elem), uintptr(unsafe.Pointer(&clsBSTR))) // CachedClassName
		_, _, _ = syscall.SyscallN(uiaVtblCall(elem, 61), uintptr(elem), uintptr(unsafe.Pointer(&aidBSTR))) // CachedAutomationId
		uiaRelease(elem)

		candidates = append(candidates, trayCandidate{
			className:    uiaBSTRToString(clsBSTR),
			automationID: uiaBSTRToString(aidBSTR),
			bounds:       bRect,
			offscreen:    offscreen != 0,
		})
	}

	return computeTrayUnionFromCandidates(candidates, tb, dpi, isRTL)
}

func (p *trayProbeCache) Start(notifyFn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notifyFn = notifyFn
	if p.started {
		return
	}
	p.started = true
	p.stopCh = make(chan struct{})
	p.wakeCh = make(chan struct{}, 1)
	go p.workerLoop(p.stopCh, p.wakeCh)
}

func (p *trayProbeCache) Stop() {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return
	}
	p.started = false
	close(p.stopCh)
	p.stopCh = nil
	p.wakeCh = nil
	p.notifyFn = nil
	p.result = cachedUIATray{}
	p.mu.Unlock()
}

func (p *trayProbeCache) Trigger(taskbarHWND uintptr, tb rect, dpi uint32, minAge time.Duration) {
	if taskbarHWND == 0 || tb.Empty() {
		return
	}
	p.mu.Lock()
	if !p.started || p.wakeCh == nil {
		p.mu.Unlock()
		return
	}
	if !p.inFlight && p.result.taskbarHWND == taskbarHWND && p.result.tbRect == tb && minAge > 0 && time.Since(p.result.updatedAt) < minAge {
		p.mu.Unlock()
		return
	}
	p.pendingHWND = taskbarHWND
	p.pendingTB = tb
	p.pendingDPI = dpi
	wakeCh := p.wakeCh
	p.mu.Unlock()

	select {
	case wakeCh <- struct{}{}:
	default:
	}
}

func (p *trayProbeCache) Snapshot(taskbarHWND uintptr, tb rect) (rect, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.result.valid || p.result.taskbarHWND != taskbarHWND || p.result.tbRect != tb {
		return rect{}, false
	}
	return p.result.trayRect, true
}

func (p *trayProbeCache) workerLoop(stopCh <-chan struct{}, wakeCh <-chan struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if procCoInitializeEx.Find() != nil || procCoCreateInstance.Find() != nil {
		return
	}
	hr, _, _ := procCoInitializeEx.Call(0, uintptr(coinitMultithreaded))
	shouldUninit := hr == 0 || hr == 1
	if int32(hr) < 0 && uint32(hr) != 0x80010106 {
		return
	}
	if shouldUninit {
		defer procCoUninitialize.Call()
	}

	var uia unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)),
		0,
		uintptr(clsctxInprocServer),
		uintptr(unsafe.Pointer(&iidIUIAutomation)),
		uintptr(unsafe.Pointer(&uia)),
	)
	if int32(hr) < 0 || uia == nil {
		return
	}
	defer uiaRelease(uia)

	for {
		select {
		case <-stopCh:
			return
		case <-wakeCh:
		}

		p.mu.Lock()
		hwnd := p.pendingHWND
		tb := p.pendingTB
		dpi := p.pendingDPI
		notify := p.notifyFn
		p.inFlight = true
		p.mu.Unlock()

		trayRect, ok := probeSystemTrayViaUIA(uia, hwnd, tb, dpi)

		p.mu.Lock()
		p.inFlight = false
		prev := p.result
		changed := !prev.valid && ok ||
			(prev.valid && !ok && prev.taskbarHWND == hwnd) ||
			(prev.valid && ok && (prev.taskbarHWND != hwnd || prev.tbRect != tb || prev.trayRect != trayRect))
		if ok || prev.taskbarHWND != hwnd || prev.tbRect != tb {
			p.result = cachedUIATray{
				taskbarHWND: hwnd,
				tbRect:      tb,
				trayRect:    trayRect,
				valid:       ok,
				updatedAt:   time.Now(),
			}
		} else {
			p.result.updatedAt = time.Now()
		}
		p.mu.Unlock()

		if changed && notify != nil {
			notify()
		}
	}
}
