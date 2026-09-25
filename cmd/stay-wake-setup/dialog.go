//go:build windows

package main

// Setup's window is a Windows task dialog (TaskDialogIndirect): the
// system's own dialog with radio buttons, push buttons, a progress bar and
// pages, so Setup needs no GUI toolkit. Task dialogs live in version 6 of
// the common controls, which setup.manifest - embedded through
// rsrc_windows_amd64.syso - asks for.
//
// Page one asks how StayWakeBlackScreen should be used (radio buttons,
// preselected from the current config) and offers Install/Update,
// Uninstall and Close. Nothing happens until one of them is clicked. A
// progress page follows, then a result page that stays until it's closed.

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"windows-stay-wake-black-screen/internal/config"
	"windows-stay-wake-black-screen/internal/tray"
	"windows-stay-wake-black-screen/internal/win32"
)

// comctl32 is loaded by bare name, not from an explicit System32 path, so
// the manifest can redirect it to version 6 - the one with task dialogs.
var (
	modComctl32            = windows.NewLazyDLL("comctl32.dll")
	procTaskDialogIndirect = modComctl32.NewProc("TaskDialogIndirect")
)

const (
	wmUser                   = 0x0400
	tdmNavigatePage          = wmUser + 101
	tdmSetProgressBarMarquee = wmUser + 107
	tdmSetElementText        = wmUser + 108
	tdmEnableButton          = wmUser + 111

	tdnCreated            = 0
	tdnNavigated          = 1
	tdnButtonClicked      = 2
	tdnRadioButtonClicked = 6

	tdeContent = 0

	tdfUseHIconMain            = 0x0002
	tdfAllowDialogCancellation = 0x0008
	tdfShowMarqueeProgressBar  = 0x0400

	tdErrorIcon = 0xFFFE // MAKEINTRESOURCE(-2)

	sOK    = 0
	sFalse = 1

	// idCancel is Close on every page, so Escape and the title bar's X,
	// which Windows reports as IDCANCEL, do exactly what Close does.
	idCancel        = 2
	buttonInstall   = 101
	buttonUninstall = 102

	radioBackground = 201
	radioInstant    = 202
)

// Which page is showing; the callback gets it as lpCallbackData.
const (
	pageChoose = iota
	pageProgress
	pageResult
)

const title = "StayWakeBlackScreen Setup"

var (
	// Set before the dialog opens and only read afterwards. The callback
	// is created in runDialog rather than here because dialogProc leads
	// back to it (through pack), which Go rejects as an init cycle.
	dialogCallback uintptr
	dialogOpts     options
	appIcon        uintptr

	// Only touched on the dialog's thread, in dialogProc and what it calls.
	busy     bool  // the progress page is showing
	selected int32 = radioBackground

	// failed is set by the worker and read once the dialog has closed.
	failed atomic.Bool

	// kept holds every page handed to Windows, so the memory its raw
	// pointers refer to stays alive while the dialog may still use it.
	keptMu sync.Mutex
	kept   []*packedPage
)

// page describes one page of the dialog. Every page also gets a Close
// button (idCancel).
type page struct {
	instruction, content string
	flags                uint32
	buttons, radios      []button
	defaultButton        int32
	defaultRadio         int32
	errorIcon            bool
	kind                 uintptr
}

type button struct {
	id   int32
	text string
}

// packedPage is a TASKDIALOGCONFIG as Windows reads it, plus everything
// its raw pointers refer to.
type packedPage struct {
	buf    []byte
	strs   [][]uint16
	arrays [][]byte
}

// str keeps s alive in p and returns its address as UTF-16, or 0 for "".
func (p *packedPage) str(s string) uint64 {
	if s == "" {
		return 0
	}
	u, err := windows.UTF16FromString(s)
	if err != nil {
		u, _ = windows.UTF16FromString("?")
	}
	p.strs = append(p.strs, u)
	return uint64(uintptr(unsafe.Pointer(&u[0])))
}

// buttonArray lays bs out as TASKDIALOG_BUTTONs (1-byte packed: the int32
// id, then the text pointer) and returns the array's address.
func (p *packedPage) buttonArray(bs []button) uint64 {
	arr := make([]byte, 12*len(bs))
	for i, b := range bs {
		binary.LittleEndian.PutUint32(arr[12*i:], uint32(b.id))
		binary.LittleEndian.PutUint64(arr[12*i+4:], p.str(b.text))
	}
	p.arrays = append(p.arrays, arr)
	return uint64(uintptr(unsafe.Pointer(&arr[0])))
}

func (p *packedPage) addr() uintptr { return uintptr(unsafe.Pointer(&p.buf[0])) }

// pack lays the page out as a TASKDIALOGCONFIG. The Windows headers
// declare it with 1-byte packing, which a Go struct can't express, so the
// fields go in at their packed 64-bit offsets instead.
func (pg page) pack() *packedPage {
	p := &packedPage{buf: make([]byte, 160)}
	le := binary.LittleEndian

	flags := pg.flags | tdfAllowDialogCancellation
	buttons := append(append([]button{}, pg.buttons...), button{idCancel, "Close"})

	le.PutUint32(p.buf[0:], 160)                           // cbSize
	le.PutUint64(p.buf[12:], uint64(win32.ModuleHandle())) // hInstance
	le.PutUint64(p.buf[28:], p.str(title))                 // pszWindowTitle
	switch {
	case pg.errorIcon:
		le.PutUint64(p.buf[36:], tdErrorIcon) // pszMainIcon
	case appIcon != 0:
		flags |= tdfUseHIconMain
		le.PutUint64(p.buf[36:], uint64(appIcon)) // hMainIcon
	}
	le.PutUint32(p.buf[20:], flags)                    // dwFlags
	le.PutUint64(p.buf[44:], p.str(pg.instruction))    // pszMainInstruction
	le.PutUint64(p.buf[52:], p.str(pg.content))        // pszContent
	le.PutUint32(p.buf[60:], uint32(len(buttons)))     // cButtons
	le.PutUint64(p.buf[64:], p.buttonArray(buttons))   // pButtons
	le.PutUint32(p.buf[72:], uint32(pg.defaultButton)) // nDefaultButton
	if len(pg.radios) > 0 {
		le.PutUint32(p.buf[76:], uint32(len(pg.radios)))   // cRadioButtons
		le.PutUint64(p.buf[80:], p.buttonArray(pg.radios)) // pRadioButtons
		le.PutUint32(p.buf[88:], uint32(pg.defaultRadio))  // nDefaultRadioButton
	}
	le.PutUint64(p.buf[132:], p.str(fmt.Sprintf("Setup %s - installs for your account only, no administrator rights needed", version))) // pszFooter
	le.PutUint64(p.buf[140:], uint64(dialogCallback))                                                                                   // pfCallback
	le.PutUint64(p.buf[148:], uint64(pg.kind))                                                                                          // lpCallbackData

	keptMu.Lock()
	kept = append(kept, p)
	keptMu.Unlock()
	return p
}

// runDialog shows Setup's window and returns the process exit code.
func runDialog(o options) int {
	runtime.LockOSThread()
	if err := procTaskDialogIndirect.Find(); err != nil {
		messageBox("Setup can't show its window on this version of Windows:\n\n" + err.Error())
		return 1
	}

	dialogOpts = o
	dialogCallback = syscall.NewCallback(dialogProc)
	if icon, err := tray.EnabledIcon(); err == nil {
		appIcon = icon
		defer tray.DestroyIconHandle(icon)
	}
	// Preselect how it's used today, without creating anything on a PC
	// where it was never installed.
	if cfg, found := config.Existing(); found && !cfg.Autostart {
		selected = radioInstant
	}

	first := page{
		instruction: "How do you want to use StayWakeBlackScreen?",
		content: "It keeps your PC awake behind a black screen, without locking it. " +
			"Press Escape to end a black screen.",
		radios: []button{
			{radioBackground, "Idle guard: runs in the notification area and blacks out the screen after a few minutes without input; starts when you sign in"},
			{radioInstant, "Instant black screen: a Start menu entry that blacks out the screen as soon as you open it"},
		},
		defaultRadio:  selected,
		buttons:       []button{{buttonInstall, "Install/Update"}, {buttonUninstall, "Uninstall"}},
		defaultButton: buttonInstall,
		kind:          pageChoose,
	}.pack()

	if hr, _, _ := procTaskDialogIndirect.Call(first.addr(), 0, 0, 0); hr != 0 {
		messageBox(fmt.Sprintf("Setup couldn't open its window (error 0x%08X).", hr))
		return 1
	}
	if failed.Load() {
		return 1
	}
	return 0
}

// dialogProc is the task dialog's callback; Windows calls it on the
// dialog's own thread.
func dialogProc(hwnd, msg, wParam, lParam, refData uintptr) uintptr {
	switch msg {
	case tdnCreated, tdnNavigated:
		busy = refData == pageProgress
		if busy {
			win32.SendMessage(hwnd, tdmSetProgressBarMarquee, 1, 0)
			win32.SendMessage(hwnd, tdmEnableButton, idCancel, 0)
		}
	case tdnRadioButtonClicked:
		selected = int32(wParam)
	case tdnButtonClicked:
		switch wParam {
		case buttonInstall:
			start(hwnd, actionFor(selected))
			return sFalse // keep the dialog open
		case buttonUninstall:
			start(hwnd, "uninstall")
			return sFalse
		case idCancel:
			if busy {
				return sFalse // no closing halfway through
			}
		}
	}
	return sOK
}

func actionFor(radio int32) string {
	if radio == radioInstant {
		return "instant"
	}
	return "background"
}

// start switches to the progress page and runs action on a separate
// goroutine, so the dialog keeps painting meanwhile. The worker only
// talks to the dialog through SendMessage, which Windows hands to the
// dialog's thread.
func start(hwnd uintptr, action string) {
	verb := "Installing"
	if action == "uninstall" {
		verb = "Uninstalling"
	}
	navigate(hwnd, page{
		instruction:   verb + " StayWakeBlackScreen...",
		content:       "Getting started...",
		flags:         tdfShowMarqueeProgressBar,
		defaultButton: idCancel,
		kind:          pageProgress,
	})

	go func() {
		err := run(action, dialogOpts, func(step string) { setContent(hwnd, step) })
		failed.Store(err != nil)
		navigate(hwnd, resultPage(action, err))
	}()
}

// resultPage is the last page: what to do next, or what went wrong. It
// stays until it's closed.
func resultPage(action string, err error) page {
	pg := page{defaultButton: idCancel, kind: pageResult}
	switch {
	case err != nil:
		pg.instruction = "Setup didn't finish"
		pg.content = err.Error()
		pg.errorIcon = true
	case action == "background":
		cfg, _ := config.Load()
		pg.instruction = "The idle guard is running"
		pg.content = fmt.Sprintf("Look for the monitor icon in the notification area. After %s without keyboard or mouse "+
			"input it blacks out the screen; press Escape to bring the screen back. It starts again whenever you sign in. "+
			"Right-click the icon for Blackout, Disable, Configure and Exit. To black out the screen right away, open "+
			"StayWakeBlackScreen from the Start menu.", minutes(cfg.IdleMinutes))
	case action == "instant":
		pg.instruction = "StayWakeBlackScreen is in your Start menu"
		pg.content = "Open it whenever you want the screen black: press the Windows key, type StayWake and press Enter. " +
			"Press Escape to bring the screen back."
	default:
		pg.instruction = "StayWakeBlackScreen has been removed"
		pg.content = "Its program, shortcuts and settings are gone."
	}
	return pg
}

func minutes(n int) string {
	if n == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", n)
}

func navigate(hwnd uintptr, pg page) {
	win32.SendMessage(hwnd, tdmNavigatePage, 0, pg.pack().addr())
}

func setContent(hwnd uintptr, text string) {
	u, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	win32.SendMessage(hwnd, tdmSetElementText, tdeContent, uintptr(unsafe.Pointer(u)))
	runtime.KeepAlive(u)
}

func messageBox(text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(title)
	windows.MessageBox(0, t, c, windows.MB_OK|windows.MB_ICONERROR)
}
