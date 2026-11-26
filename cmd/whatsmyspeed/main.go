// WhatsMySpeed is a lightweight, native Windows 11 taskbar network speed monitor.
// Author: Shubham Bhamare
package main

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/network"
	"github.com/WhatsMySpeed/WhatsMySpeed/internal/ui"
	"golang.org/x/sys/windows"
)

const singleInstanceMutexName = "Local\\WhatsMySpeed_SingleInstance_Mutex"

func acquireSingleInstance() (windows.Handle, bool) {
	namePtr, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		return 0, true
	}
	h, err := windows.CreateMutex(nil, false, namePtr)
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return 0, true
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			_ = windows.CloseHandle(h)
		}
		return 0, false
	}
	return h, true
}

func showErrorBox(title, msg string) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	msgBox := user32.NewProc("MessageBoxW")
	tPtr, _ := windows.UTF16PtrFromString(title)
	mPtr, _ := windows.UTF16PtrFromString(msg)
	_, _, _ = msgBox.Call(0, uintptr(unsafe.Pointer(mPtr)), uintptr(unsafe.Pointer(tPtr)), 0x10)
}

func startsInTray(args []string) bool {
	for _, a := range args {
		if strings.EqualFold(a, config.AutoStartArg) ||
			strings.EqualFold(a, "--minimized") ||
			strings.EqualFold(a, "--background") {
			return true
		}
	}
	return false
}

func main() {
	openOnLaunch := !startsInTray(os.Args[1:])

	mutex, ok := acquireSingleInstance()
	if !ok {
		ui.ActivateExistingInstance()
		return
	}
	if mutex != 0 {
		defer windows.CloseHandle(mutex)
	}

	cfgPath, _ := config.ConfigPath()
	cfg, _ := config.LoadAndSyncStartup(cfgPath)

	monitor := network.NewMonitor()
	app := ui.NewApp(cfg, cfgPath, monitor, openOnLaunch)
	if err := app.Run(); err != nil {
		showErrorBox(config.AppName, fmt.Sprintf("Failed to start %s:\n%v", config.AppName, err))
		os.Exit(1)
	}
}
