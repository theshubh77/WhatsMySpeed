package config

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestIsPackagedUnpackagedReturnsFalse(t *testing.T) {
	if IsPackaged() {
		t.Skip("test process itself is packaged")
	}
	if IsPackagedStartupLaunch() {
		t.Fatal("IsPackagedStartupLaunch should be false when unpackaged")
	}
}

func TestWinRTStartupAndAppInstanceFactoriesReachable(t *testing.T) {
	err := runInWinRTMTA(func() error {
		startupStatics, err := getActivationFactory("Windows.ApplicationModel.StartupTask", &iidIStartupTaskStatics)
		if err != nil {
			return err
		}
		comRelease(startupStatics)

		appInstStatics, err := getActivationFactory("Windows.ApplicationModel.AppInstance", &iidIAppInstanceStatics)
		if err != nil {
			return err
		}
		defer comRelease(appInstStatics)

		// Verify GetActivatedEventArgs vtable slot cleanly returns an expected non-crashing HRESULT.
		var args unsafe.Pointer
		hr, _, _ := syscall.SyscallN(vtblCall(appInstStatics, 7), uintptr(appInstStatics), uintptr(unsafe.Pointer(&args)))
		if args != nil {
			comRelease(args)
		}
		t.Logf("Unpackaged GetActivatedEventArgs hr=0x%08x", uint32(hr))
		return nil
	})
	if err != nil {
		t.Fatalf("WinRT factory activation failed: %v", err)
	}
}
