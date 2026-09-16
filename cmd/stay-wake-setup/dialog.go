//go:build windows

package main

// Setup's window is a Windows task dialog (TaskDialogIndirect): the
// system's own dialog with command-link buttons, a progress bar and pages,
// so Setup needs no GUI toolkit. Task dialogs live in version 6 of the
// common controls, which setup.manifest - embedded through
// rsrc_windows_amd64.syso - asks for.

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

	tdnNavigated     = 1
	tdnButtonClicked = 2

	tdeContent = 0

	tdfUseHIconMain            = 0x0002
	tdfAllowDialogCancellation = 0x0008
	tdfUseCommandLinks         = 0x0010
	tdfShowMarqueeProgressBar  = 0x0400

	tdcbfCancelButton = 0x0008
	tdcbfCloseButton  = 0x0020

	idCancel = 2
	idClose  = 8

	tdErrorIcon = 0xFFFE // MAKEINTRESOURCE(-2)

	sOK    = 0
	sFalse = 1

	// The choices on the first page.
	choiceBackground = 101
	choiceInstant    = 102
	choiceUninstall  = 103

	// Which page is showing, passed to the callback as its reference data.
	pageChoose   = 0
	pageProgress = 1
	pageDone     = 2
)

const title = "StayWakeBlackScreen Setup"

var (
	// Set before the dialog opens and only read afterwards. The callback
	// is created in runDialog rather than here because dialogProc leads
	// back to it (through pack), which Go rejects as an init cycle.
	dialogCallback uintptr
	dialogOpts     options
	appIcon        uintptr

	// busy is only touched on the dialog's thread, in dialogProc.
	busy bool

	// failed is set by the worker and read once the dialog has closed.
	failed atomic.Bool

	// kept holds every page handed to Windows, so the memory its raw
	// pointers refer to stays alive while the dialog may still use it.
	keptMu sync.Mutex
	kept   []*packedPage
)

// page describes one page of the dialog.
type page struct {
	instruction, content string
	flags, commonButtons uint32
	buttons              []button
	errorIcon            bool
	kind                 uintptr
}

type button struct {
	id   int32
	text string // for a command link: "Title\nExplanation"
}

// packedPage is a TASKDIALOGCONFIG as Windows reads it, plus everything
// its raw pointers refer to.
type packedPage struct {
	buf  []byte
	refs [][]uint16
	btns []byte
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
	p.refs = append(p.refs, u)
	return uint64(uintptr(unsafe.Pointer(&u[0])))
}

// pack lays the page out as a TASKDIALOGCONFIG. The Windows headers
// declare it, and TASKDIALOG_BUTTON, with 1-byte packing, which a Go
// struct can't express, so the fields go in at their packed 64-bit
// offsets instead.
func (pg page) pack() *packedPage {
	p := &packedPage{buf: make([]byte, 160)}
	le := binary.LittleEndian
	flags := pg.flags

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
	le.PutUint32(p.buf[20:], flags)                 // dwFlags
	le.PutUint32(p.buf[24:], pg.commonButtons)      // dwCommonButtons
	le.PutUint64(p.buf[44:], p.str(pg.instruction)) // pszMainInstruction
	le.PutUint64(p.buf[52:], p.str(pg.content))     // pszContent
	if n := len(pg.buttons); n > 0 {
		p.btns = make([]byte, 12*n) // TASKDIALOG_BUTTON: int id, then the text pointer
		for i, b := range pg.buttons {
			le.PutUint32(p.btns[12*i:], uint32(b.id))
			le.PutUint64(p.btns[12*i+4:], p.str(b.text))
		}
		le.PutUint32(p.buf[60:], uint32(n))                                   // cButtons
		le.PutUint64(p.buf[64:], uint64(uintptr(unsafe.Pointer(&p.btns[0])))) // pButtons
	}
	le.PutUint64(p.buf[132:], p.str(fmt.Sprintf("Setup %s - installs for your account only, no administrator rights needed", version))) // pszFooter
	le.PutUint64(p.buf[140:], uint64(dialogCallback))                                                                                   // pfCallback
	le.PutUint64(p.buf[148:], uint64(pg.kind))                                                                                          // lpCallbackData

	keptMu.Lock()
	kept = append(kept, p)
	keptMu.Unlock()
	return p
}

func (p *packedPage) addr() uintptr { return uintptr(unsafe.Pointer(&p.buf[0])) }

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

	first := page{
		instruction: "How do you want to use StayWakeBlackScreen?",
		content: "It keeps your PC awake behind a black screen, without locking it. " +
			"Press Escape to end a black screen.\n\nAlready installed? Choosing again updates it.",
		flags:         tdfUseCommandLinks | tdfAllowDialogCancellation,
		commonButtons: tdcbfCancelButton,
		buttons: []button{
			{choiceBackground, "Idle guard\nRuns quietly in the notification area and blacks out the screen after a few minutes without input. Starts when you sign in."},
			{choiceInstant, "Instant black screen\nAdds StayWakeBlackScreen to the Start menu; opening it blacks out the screen right away."},
			{choiceUninstall, "Uninstall\nRemoves StayWakeBlackScreen together with its shortcuts and settings."},
		},
		kind: pageChoose,
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
	case tdnNavigated:
		busy = refData == pageProgress
		if busy {
			win32.SendMessage(hwnd, tdmSetProgressBarMarquee, 1, 0)
			win32.SendMessage(hwnd, tdmEnableButton, idClose, 0)
		}
	case tdnButtonClicked:
		switch wParam {
		case choiceBackground, choiceInstant, choiceUninstall:
			start(hwnd, int(wParam))
			return sFalse // keep the dialog open
		case idClose, idCancel:
			if busy {
				return sFalse
			}
		}
	}
	return sOK
}

// start switches to the progress page and runs the chosen action on a
// separate goroutine, so the dialog keeps painting meanwhile. The worker
// only talks to the dialog through SendMessage, which Windows hands to the
// dialog's thread.
func start(hwnd uintptr, choice int) {
	verb, action := "Installing", "background"
	switch choice {
	case choiceInstant:
		action = "instant"
	case choiceUninstall:
		verb, action = "Uninstalling", "uninstall"
	}
	navigate(hwnd, page{
		instruction:   verb + " StayWakeBlackScreen...",
		content:       "Getting started...",
		flags:         tdfShowMarqueeProgressBar,
		commonButtons: tdcbfCloseButton,
		kind:          pageProgress,
	})

	go func() {
		err := run(action, dialogOpts, func(step string) { setContent(hwnd, step) })
		failed.Store(err != nil)
		navigate(hwnd, resultPage(choice, err))
	}()
}

// resultPage is the last page: what happened, and what to do next.
func resultPage(choice int, err error) page {
	pg := page{commonButtons: tdcbfCloseButton, flags: tdfAllowDialogCancellation, kind: pageDone}
	switch {
	case err != nil:
		pg.errorIcon = true
		pg.instruction = "Setup didn't finish"
		pg.content = err.Error()
	case choice == choiceBackground:
		cfg, _ := config.Load()
		pg.instruction = "The idle guard is running"
		pg.content = fmt.Sprintf("Look for the monitor icon in the notification area. After %s without keyboard or mouse "+
			"input it blacks out the screen; press Escape to bring the screen back. It starts again whenever you sign in.\n\n"+
			"Right-click the icon to disable it, change its settings or exit.", minutes(cfg.IdleMinutes))
	case choice == choiceInstant:
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
