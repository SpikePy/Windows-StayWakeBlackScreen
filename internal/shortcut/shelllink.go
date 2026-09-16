//go:build windows

package shortcut

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Shortcuts are written through the shell's IShellLink COM object - the
// same .lnk files Explorer creates.
var (
	modOle32             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")
)

// The shell's class and interface IDs, all of the form
// {xxxxxxxx-0000-0000-C000-000000000046}.
var (
	clsidShellLink = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW  = windows.GUID{Data1: 0x000214F9, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile = windows.GUID{Data1: 0x0000010B, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	clsctxInprocServer = 0x1
	stgmRead           = 0x0
)

// comObject is any COM interface pointer: its first field is the vtable.
type comObject struct{ vtbl uintptr }

type iUnknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

// iShellLinkWVtbl and iPersistFileVtbl mirror those interfaces' methods.
// Only the ones called below matter by name, but the order must match the
// interface exactly - that order is what picks the function to call.
type iShellLinkWVtbl struct {
	iUnknownVtbl
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

type iPersistFileVtbl struct {
	iUnknownVtbl
	GetClassID    uintptr
	IsDirty       uintptr
	Load          uintptr
	Save          uintptr
	SaveCompleted uintptr
	GetCurFile    uintptr
}

func (o *comObject) release() {
	syscall.SyscallN((*iUnknownVtbl)(unsafe.Pointer(o.vtbl)).Release, uintptr(unsafe.Pointer(o)))
}

// comSetUp initialises COM for this thread and returns the matching
// tear-down. A thread already initialised in another mode is fine - it
// just isn't ours to tear down.
func comSetUp() (func(), error) {
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
	if err == nil {
		return windows.CoUninitialize, nil
	}
	if errors.Is(err, syscall.Errno(windows.RPC_E_CHANGED_MODE)) {
		return func() {}, nil
	}
	return func() {}, fmt.Errorf("CoInitializeEx: %w", err)
}

// openLink creates an IShellLinkW together with its IPersistFile, and
// returns both with a function releasing them.
func openLink() (link *comObject, vtbl *iShellLinkWVtbl, persist *comObject, pvtbl *iPersistFileVtbl, release func(), err error) {
	if hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link))); hr != 0 {
		return nil, nil, nil, nil, nil, fmt.Errorf("CoCreateInstance(ShellLink): 0x%08X", hr)
	}
	vtbl = (*iShellLinkWVtbl)(unsafe.Pointer(link.vtbl))
	if hr, _, _ := syscall.SyscallN(vtbl.QueryInterface,
		uintptr(unsafe.Pointer(link)), uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&persist))); hr != 0 {
		link.release()
		return nil, nil, nil, nil, nil, fmt.Errorf("IShellLink::QueryInterface(IPersistFile): 0x%08X", hr)
	}
	pvtbl = (*iPersistFileVtbl)(unsafe.Pointer(persist.vtbl))
	return link, vtbl, persist, pvtbl, func() { persist.release(); link.release() }, nil
}

// createShortcut writes a .lnk at lnkPath that runs target with args.
func createShortcut(lnkPath, target, args, description string) error {
	comDone, err := comSetUp()
	if err != nil {
		return err
	}
	defer comDone()

	link, vtbl, persist, pvtbl, release, err := openLink()
	if err != nil {
		return err
	}
	defer release()

	set := func(method uintptr, name, value string) error {
		p, err := windows.UTF16PtrFromString(value)
		if err != nil {
			return err
		}
		if hr, _, _ := syscall.SyscallN(method, uintptr(unsafe.Pointer(link)), uintptr(unsafe.Pointer(p))); hr != 0 {
			return fmt.Errorf("IShellLink::%s(%s): 0x%08X", name, value, hr)
		}
		return nil
	}
	for _, s := range []struct {
		method      uintptr
		name, value string
	}{
		{vtbl.SetPath, "SetPath", target},
		{vtbl.SetArguments, "SetArguments", args},
		{vtbl.SetWorkingDirectory, "SetWorkingDirectory", filepath.Dir(target)},
		{vtbl.SetDescription, "SetDescription", description},
	} {
		if err := set(s.method, s.name, s.value); err != nil {
			return err
		}
	}

	lnkPtr, err := windows.UTF16PtrFromString(lnkPath)
	if err != nil {
		return err
	}
	if hr, _, _ := syscall.SyscallN(pvtbl.Save,
		uintptr(unsafe.Pointer(persist)), uintptr(unsafe.Pointer(lnkPtr)), 1); hr != 0 {
		return fmt.Errorf("IPersistFile::Save(%s): 0x%08X", lnkPath, hr)
	}
	return nil
}

// shortcutTarget reads back the program a .lnk runs and its arguments.
func shortcutTarget(lnkPath string) (target, args string, err error) {
	comDone, err := comSetUp()
	if err != nil {
		return "", "", err
	}
	defer comDone()

	link, vtbl, persist, pvtbl, release, err := openLink()
	if err != nil {
		return "", "", err
	}
	defer release()

	lnkPtr, err := windows.UTF16PtrFromString(lnkPath)
	if err != nil {
		return "", "", err
	}
	if hr, _, _ := syscall.SyscallN(pvtbl.Load,
		uintptr(unsafe.Pointer(persist)), uintptr(unsafe.Pointer(lnkPtr)), stgmRead); hr != 0 {
		return "", "", fmt.Errorf("IPersistFile::Load(%s): 0x%08X", lnkPath, hr)
	}

	path := make([]uint16, windows.MAX_PATH)
	if hr, _, _ := syscall.SyscallN(vtbl.GetPath,
		uintptr(unsafe.Pointer(link)), uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)), 0, 0); hr != 0 {
		return "", "", fmt.Errorf("IShellLink::GetPath: 0x%08X", hr)
	}
	arguments := make([]uint16, 1024)
	if hr, _, _ := syscall.SyscallN(vtbl.GetArguments,
		uintptr(unsafe.Pointer(link)), uintptr(unsafe.Pointer(&arguments[0])), uintptr(len(arguments))); hr != 0 {
		return "", "", fmt.Errorf("IShellLink::GetArguments: 0x%08X", hr)
	}
	return windows.UTF16ToString(path), windows.UTF16ToString(arguments), nil
}
