package config

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	msixStartupTaskID = "WhatsMySpeedStartup"

	errInsufficientBuffer uint32 = 122

	roInitMultithreaded uint32  = 1
	sOK                 uintptr = 0
	sFalse              uintptr = 1

	asyncStatusStarted   uint32 = 0
	asyncStatusCompleted uint32 = 1

	startupTaskDisabledByUser  uint32 = 1
	startupTaskEnabled         uint32 = 2
	startupTaskDisabledPolicy  uint32 = 3
	startupTaskEnabledByPolicy uint32 = 4

	activationKindStartupTask uint32 = 1020
)

var (
	modKernel32AppModel = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentPkg   = modKernel32AppModel.NewProc("GetCurrentPackageFullName")

	modCombase                 = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize           = modCombase.NewProc("RoInitialize")
	procRoUninitialize         = modCombase.NewProc("RoUninitialize")
	procRoGetActivationFactory = modCombase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = modCombase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = modCombase.NewProc("WindowsDeleteString")

	iidIAsyncInfo = windows.GUID{
		Data1: 0x00000036,
		Data2: 0x0000,
		Data3: 0x0000,
		Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidIStartupTaskStatics = windows.GUID{
		Data1: 0xee5b60bd,
		Data2: 0xa148,
		Data3: 0x41a7,
		Data4: [8]byte{0xb2, 0x6e, 0xe8, 0xb8, 0x8a, 0x1e, 0x62, 0xf8},
	}
	iidIAppInstanceStatics = windows.GUID{
		Data1: 0x9d11e77f,
		Data2: 0x9ea6,
		Data3: 0x47af,
		Data4: [8]byte{0xa6, 0xec, 0x46, 0x78, 0x4c, 0x5b, 0xa2, 0x54},
	}
)

func vtblCall(obj unsafe.Pointer, slot int) uintptr {
	vptr := *(*unsafe.Pointer)(obj)
	return (*(*[16]uintptr)(vptr))[slot]
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		_, _, _ = syscall.SyscallN(vtblCall(obj, 2), uintptr(obj))
	}
}

func comQueryInterface(obj unsafe.Pointer, iid *windows.GUID) (unsafe.Pointer, error) {
	var out unsafe.Pointer
	hr, _, _ := syscall.SyscallN(vtblCall(obj, 0), uintptr(obj), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("QueryInterface failed: HRESULT 0x%08x", uint32(hr))
	}
	return out, nil
}

// IsPackaged reports whether the current process is running inside an MSIX / Microsoft Store package.
func IsPackaged() bool {
	if procGetCurrentPkg.Find() != nil {
		return false
	}
	var length uint32
	r1, _, _ := procGetCurrentPkg.Call(uintptr(unsafe.Pointer(&length)), 0)
	return r1 == 0 || uint32(r1) == errInsufficientBuffer
}

func runInWinRTMTA(fn func() error) error {
	if procRoInitialize.Find() != nil || procRoGetActivationFactory.Find() != nil || procWindowsCreateString.Find() != nil {
		return fmt.Errorf("WinRT combase APIs unavailable")
	}

	errCh := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hr, _, _ := procRoInitialize.Call(uintptr(roInitMultithreaded))
		shouldUninit := hr == sOK || hr == sFalse
		if int32(hr) < 0 && uint32(hr) != 0x80010106 { // RPC_E_CHANGED_MODE
			errCh <- fmt.Errorf("RoInitialize failed: HRESULT 0x%08x", uint32(hr))
			return
		}
		if shouldUninit {
			defer procRoUninitialize.Call()
		}
		errCh <- fn()
	}()
	return <-errCh
}

func newHString(s string) (uintptr, error) {
	u16, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	charLen := uint32(len(u16) - 1)
	var hstr uintptr
	hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u16[0])), uintptr(charLen), uintptr(unsafe.Pointer(&hstr)))
	if int32(hr) < 0 {
		return 0, fmt.Errorf("WindowsCreateString(%q) failed: HRESULT 0x%08x", s, uint32(hr))
	}
	return hstr, nil
}

func deleteHString(hstr uintptr) {
	if hstr != 0 && procWindowsDeleteString.Find() == nil {
		_, _, _ = procWindowsDeleteString.Call(hstr)
	}
}

func getActivationFactory(className string, iid *windows.GUID) (unsafe.Pointer, error) {
	hClass, err := newHString(className)
	if err != nil {
		return nil, err
	}
	defer deleteHString(hClass)

	var factory unsafe.Pointer
	hr, _, _ := procRoGetActivationFactory.Call(hClass, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&factory)))
	if int32(hr) < 0 || factory == nil {
		return nil, fmt.Errorf("RoGetActivationFactory(%q) failed: HRESULT 0x%08x", className, uint32(hr))
	}
	return factory, nil
}

func awaitWinRTAsync(asyncOp unsafe.Pointer, outResultPtr unsafe.Pointer) error {
	infoObj, err := comQueryInterface(asyncOp, &iidIAsyncInfo)
	if err != nil {
		return err
	}
	defer comRelease(infoObj)

	deadline := time.Now().Add(8 * time.Second)
	var status uint32
	for {
		hr, _, _ := syscall.SyscallN(vtblCall(infoObj, 7), uintptr(infoObj), uintptr(unsafe.Pointer(&status))) // get_Status
		if int32(hr) < 0 {
			return fmt.Errorf("IAsyncInfo.get_Status failed: HRESULT 0x%08x", uint32(hr))
		}
		if status != asyncStatusStarted {
			break
		}
		if time.Now().After(deadline) {
			_, _, _ = syscall.SyscallN(vtblCall(infoObj, 9), uintptr(infoObj)) // Cancel
			return fmt.Errorf("WinRT async operation timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if status != asyncStatusCompleted {
		var errCode int32
		_, _, _ = syscall.SyscallN(vtblCall(infoObj, 8), uintptr(infoObj), uintptr(unsafe.Pointer(&errCode))) // get_ErrorCode
		return fmt.Errorf("WinRT async status %d (error 0x%08x)", status, uint32(errCode))
	}

	hr, _, _ := syscall.SyscallN(vtblCall(asyncOp, 8), uintptr(asyncOp), uintptr(outResultPtr)) // GetResults
	if int32(hr) < 0 {
		return fmt.Errorf("IAsyncOperation.GetResults failed: HRESULT 0x%08x", uint32(hr))
	}
	return nil
}

func getMSIXStartupTask(taskID string) (unsafe.Pointer, error) {
	statics, err := getActivationFactory("Windows.ApplicationModel.StartupTask", &iidIStartupTaskStatics)
	if err != nil {
		return nil, err
	}
	defer comRelease(statics)

	hTaskID, err := newHString(taskID)
	if err != nil {
		return nil, err
	}
	defer deleteHString(hTaskID)

	var asyncOp unsafe.Pointer
	hr, _, _ := syscall.SyscallN(vtblCall(statics, 7), uintptr(statics), hTaskID, uintptr(unsafe.Pointer(&asyncOp))) // GetAsync
	if int32(hr) < 0 || asyncOp == nil {
		return nil, fmt.Errorf("IStartupTaskStatics.GetAsync failed: HRESULT 0x%08x", uint32(hr))
	}
	defer comRelease(asyncOp)

	var taskObj unsafe.Pointer
	if err := awaitWinRTAsync(asyncOp, unsafe.Pointer(&taskObj)); err != nil {
		return nil, err
	}
	if taskObj == nil {
		return nil, fmt.Errorf("StartupTask(%q) returned nil", taskID)
	}
	return taskObj, nil
}

func isStartupStateEnabled(state uint32) bool {
	return state == startupTaskEnabled || state == startupTaskEnabledByPolicy
}

func isMSIXStartupEnabled() (bool, error) {
	var enabled bool
	err := runInWinRTMTA(func() error {
		taskObj, err := getMSIXStartupTask(msixStartupTaskID)
		if err != nil {
			return err
		}
		defer comRelease(taskObj)

		var state uint32
		hr, _, _ := syscall.SyscallN(vtblCall(taskObj, 8), uintptr(taskObj), uintptr(unsafe.Pointer(&state))) // get_State
		if int32(hr) < 0 {
			return fmt.Errorf("IStartupTask.get_State failed: HRESULT 0x%08x", uint32(hr))
		}
		enabled = isStartupStateEnabled(state)
		return nil
	})
	return enabled, err
}

func setMSIXStartupEnabled(enable bool) error {
	return runInWinRTMTA(func() error {
		taskObj, err := getMSIXStartupTask(msixStartupTaskID)
		if err != nil {
			return err
		}
		defer comRelease(taskObj)

		var curState uint32
		hr, _, _ := syscall.SyscallN(vtblCall(taskObj, 8), uintptr(taskObj), uintptr(unsafe.Pointer(&curState))) // get_State
		if int32(hr) < 0 {
			return fmt.Errorf("IStartupTask.get_State failed: HRESULT 0x%08x", uint32(hr))
		}

		if !enable {
			if !isStartupStateEnabled(curState) {
				return nil
			}
			hr, _, _ = syscall.SyscallN(vtblCall(taskObj, 7), uintptr(taskObj)) // Disable
			if int32(hr) < 0 {
				return fmt.Errorf("IStartupTask.Disable failed: HRESULT 0x%08x", uint32(hr))
			}
			return nil
		}

		if isStartupStateEnabled(curState) {
			return nil
		}
		if curState == startupTaskDisabledByUser || curState == startupTaskDisabledPolicy {
			return fmt.Errorf("startup task is disabled by user or policy (state=%d)", curState)
		}

		var asyncOp unsafe.Pointer
		hr, _, _ = syscall.SyscallN(vtblCall(taskObj, 6), uintptr(taskObj), uintptr(unsafe.Pointer(&asyncOp))) // RequestEnableAsync
		if int32(hr) < 0 || asyncOp == nil {
			return fmt.Errorf("IStartupTask.RequestEnableAsync failed: HRESULT 0x%08x", uint32(hr))
		}
		defer comRelease(asyncOp)

		var nextState uint32
		if err := awaitWinRTAsync(asyncOp, unsafe.Pointer(&nextState)); err != nil {
			return err
		}
		if !isStartupStateEnabled(nextState) {
			return fmt.Errorf("startup task did not enable (state=%d)", nextState)
		}
		return nil
	})
}

// IsPackagedStartupLaunch reports whether an MSIX-packaged instance was activated via Windows StartupTask.
func IsPackagedStartupLaunch() bool {
	if !IsPackaged() {
		return false
	}
	var isStartup bool
	_ = runInWinRTMTA(func() error {
		statics, err := getActivationFactory("Windows.ApplicationModel.AppInstance", &iidIAppInstanceStatics)
		if err != nil {
			return err
		}
		defer comRelease(statics)

		var activatedArgs unsafe.Pointer
		hr, _, _ := syscall.SyscallN(vtblCall(statics, 7), uintptr(statics), uintptr(unsafe.Pointer(&activatedArgs))) // GetActivatedEventArgs
		if int32(hr) < 0 || activatedArgs == nil {
			return fmt.Errorf("GetActivatedEventArgs failed: HRESULT 0x%08x", uint32(hr))
		}
		defer comRelease(activatedArgs)

		var kind uint32
		hr, _, _ = syscall.SyscallN(vtblCall(activatedArgs, 6), uintptr(activatedArgs), uintptr(unsafe.Pointer(&kind))) // get_Kind
		if int32(hr) == 0 && kind == activationKindStartupTask {
			isStartup = true
		}
		return nil
	})
	return isStartup
}
