//go:build windows

package launcher

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"track-tagger/internal/app"
	"track-tagger/internal/service"
)

var user = windows.NewLazySystemDLL("user32.dll")
var shell = windows.NewLazySystemDLL("shell32.dll")
var kernel = windows.NewLazySystemDLL("kernel32.dll")
var gdi = windows.NewLazySystemDLL("gdi32.dll")

func call(d *windows.LazyDLL, n string, args ...any) uintptr {
	a := make([]uintptr, len(args))
	var keep []*uint16
	for i, arg := range args {
		switch x := arg.(type) {
		case string:
			p := wide(x)
			keep = append(keep, p)
			a[i] = uintptr(unsafe.Pointer(p))
		case uintptr:
			a[i] = x
		case unsafe.Pointer:
			a[i] = uintptr(x)
		case int:
			a[i] = uintptr(x)
		case uint32:
			a[i] = uintptr(x)
		default:
			panic(fmt.Sprintf("unsupported Win32 argument %T", arg))
		}
	}
	v, _, _ := d.NewProc(n).Call(a...)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(args)
	return v
}
func wide(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }
func ptr(s string) string   { return s }

type point struct{ x, y int32 }
type msg struct {
	hwnd           uintptr
	message        uint32
	wparam, lparam uintptr
	time           uint32
	pt             point
	private        uint32
}
type rect struct{ left, top, right, bottom int32 }
type wc struct {
	size, style                        uint32
	proc                               uintptr
	clsExtra, wndExtra                 int32
	instance, icon, cursor, background uintptr
	menu, class                        *uint16
	smallIcon                          uintptr
}
type notifyIcon struct {
	size                uint32
	hwnd                uintptr
	id, flags, callback uint32
	icon                uintptr
	tip                 [128]uint16
	state, stateMask    uint32
	info                [256]uint16
	timeout             uint32
	title               [64]uint16
	infoFlags           uint32
	guid                windows.GUID
	balloon             uintptr
}
type browseInfo struct {
	owner, root     uintptr
	display         *uint16
	title           *uint16
	flags           uint32
	callback, param uintptr
	image           int32
}

const wake = 0x8001
const trayMessage = 0x8002

var win uintptr
var controls = map[int]uintptr{}
var config Settings
var directory string
var session *service.Session
var busy bool
var startCancel context.CancelFunc
var quitting bool
var status = "Stopped"
var queueMu sync.Mutex
var queue []func()
var taskbar uint32
var tray notifyIcon
var scale = 1.0
var scrollPosition, scrollMaximum int
var trayOK bool
var details bool
var appVersion = "dev"
var updates *updateController

func diagnosticDetails() string {
	mode := app.DryRun
	if checked(5) {
		mode = app.Copy
	}
	if checked(6) {
		mode = app.Write
	}
	updateStatus := "Not checked"
	if updates != nil {
		updateStatus = updates.message
	}
	return fmt.Sprintf("Track Tagger %s (windows/%s)\r\nStatus: %s\r\nMode: %s\r\nMusic folder: %s\r\nCopy destination: %s\r\nPolling: %v\r\nURL / details: %s\r\nUpdate status: %s\r\n\r\nRecent activity:\r\n%s", appVersion, runtime.GOARCH, status, mode, get(1), get(2), checked(7), get(21), updateStatus, get(22))
}

func toggleDetails() {
	details = !details
	for _, id := range []int{22, 13} {
		v := 0
		if details {
			v = 5
		}
		call(user, "ShowWindow", controls[id], v)
	}
	title := "Show details"
	if details {
		title = "Hide details"
	}
	set(15, title)
	var bounds rect
	call(user, "GetWindowRect", win, unsafe.Pointer(&bounds))
	height := int(650 * scale)
	if details {
		height = int(780 * scale)
	}
	available := int(call(user, "GetSystemMetrics", 1)) - 60
	if height > available {
		height = available
	}
	y := int(bounds.top)
	if y+height > available {
		y = available - height
	}
	if y < 0 {
		y = 0
	}
	call(user, "SetWindowPos", win, 0, int(bounds.left), y, int(bounds.right-bounds.left), height, 0x14)
	var client rect
	call(user, "GetClientRect", win, unsafe.Pointer(&client))
	content := 605
	if details {
		content = 730
	}
	scrollMaximum = int(float64(content)*scale) - int(client.bottom)
	if scrollMaximum < 0 {
		scrollMaximum = 0
	}
	scrollTo(0)
	call(user, "SetScrollRange", win, 1, 0, scrollMaximum, 1)
}

func scrollTo(position int) {
	if position < 0 {
		position = 0
	}
	if position > scrollMaximum {
		position = scrollMaximum
	}
	delta := scrollPosition - position
	scrollPosition = position
	call(user, "ScrollWindowEx", win, 0, delta, 0, 0, 0, 0, 7)
	call(user, "SetScrollPos", win, 1, position, 1)
}

func post(f func()) {
	queueMu.Lock()
	queue = append(queue, f)
	queueMu.Unlock()
	call(user, "PostMessageW", win, wake, 0, 0)
}
func set(id int, s string) { call(user, "SetWindowTextW", controls[id], ptr(s)) }
func get(id int) string {
	n := call(user, "GetWindowTextLengthW", controls[id])
	b := make([]uint16, n+1)
	call(user, "GetWindowTextW", controls[id], unsafe.Pointer(&b[0]), n+1)
	return windows.UTF16ToString(b)
}
func checked(id int) bool { return call(user, "SendMessageW", controls[id], 0xf0, 0, 0) == 1 }
func check(id int, b bool) {
	v := uintptr(0)
	if b {
		v = 1
	}
	call(user, "SendMessageW", controls[id], 0xf1, v, 0)
}
func box(s string) { call(user, "MessageBoxW", win, ptr(s), ptr("Track Tagger"), 0x40) }
func refresh() {
	set(20, status)
	active := session != nil || busy || updates != nil && updates.installing
	for _, id := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		v := uintptr(1)
		if active {
			v = 0
		}
		call(user, "EnableWindow", controls[id], v)
	}
	for _, id := range []int{2, 8} {
		v := 0
		if !active && checked(5) {
			v = 1
		}
		call(user, "EnableWindow", controls[id], v)
	}
	v := uintptr(0)
	if (session != nil || busy) && (!busy || session == nil) {
		v = 1
	}
	call(user, "EnableWindow", controls[11], v)
	v = 0
	if session != nil {
		v = 1
	}
	call(user, "EnableWindow", controls[12], v)
	refreshUpdates()
}
func logline(s string) {
	post(func() {
		old := get(22)
		if len(old) > 24000 {
			old = old[len(old)-18000:]
			if i := strings.Index(old, "\n"); i >= 0 {
				old = old[i+1:]
			}
		}
		set(22, old+strings.ReplaceAll(s, "\n", "\r\n")+"\r\n")
		call(user, "SendMessageW", controls[22], 0xb1, ^uintptr(0), ^uintptr(0))
		call(user, "SendMessageW", controls[22], 0xb7, 0, 0)
	})
}
func openBrowser() {
	if session != nil {
		if call(shell, "ShellExecuteW", win, ptr("open"), ptr(session.URL()), 0, 0, 1) <= 32 {
			box("Could not open your browser. Copy the displayed local URL into your browser.")
		}
	}
}
func readSettings() {
	config.Source = strings.TrimSpace(get(1))
	config.Destination = strings.TrimSpace(get(2))
	config.Mode = app.DryRun
	if checked(5) {
		config.Mode = app.Copy
	}
	if checked(6) {
		config.Mode = app.Write
	}
	config.Poll = checked(7)
	config.AutoCheckUpdates = checked(42)
}
func start() {
	if busy || session != nil || updates != nil && updates.installing {
		return
	}
	readSettings()
	if e := config.Validate(); e != nil {
		box(e.Error())
		return
	}
	if e := Save(directory, config); e != nil {
		box("Cannot save settings: " + e.Error())
		return
	}
	busy = true
	status = "Starting"
	refresh()
	s := config
	if s.Mode != app.Copy {
		s.Destination = ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	startCancel = cancel
	go func() {
		started, e := service.Start(ctx, service.Options{Options: app.Options{Source: s.Source, Destination: s.Destination, State: directory, Mode: s.Mode, Settle: 2 * time.Minute}, Poll: s.UsePolling(), FallbackPort: true}, logline)
		post(func() {
			busy = false
			if e != nil {
				if ctx.Err() != nil {
					status = "Stopped"
					set(21, "Startup canceled. Choose settings and click Start when ready.")
				} else {
					status = "Error"
					set(21, "Could not start: "+e.Error()+". Check folders and permissions, then try Start again.")
				}
				refresh()
				if quitting {
					call(user, "DestroyWindow", win)
				}
				return
			}
			session = started
			status = "Running"
			set(21, started.URL())
			refresh()
			if quitting || ctx.Err() != nil {
				stop(quitting)
			} else {
				openBrowser()
			}
			go func() {
				<-started.Done()
				post(func() {
					if session != started {
						return
					}
					session = nil
					busy = false
					status = "Stopped"
					if e := started.Err(); e != nil {
						status = "Error"
						set(21, e.Error())
					}
					refresh()
					if quitting {
						call(user, "DestroyWindow", win)
					}
				})
			}()
		})
	}()
}
func stop(quit bool) {
	if quit && updates != nil && updates.busy {
		updates.CancelThen(func() { stop(true) })
		return
	}
	if quit {
		quitting = true
		if session == nil && !busy {
			readSettings()
			if e := Save(directory, config); e != nil {
				box("Cannot save settings: " + e.Error())
			}
		}
	}
	if session == nil {
		if busy && startCancel != nil {
			startCancel()
		}
		if !busy && quit {
			call(user, "DestroyWindow", win)
		}
		return
	}
	if busy {
		return
	}
	busy = true
	status = "Stopping"
	refresh()
	s := session
	go s.Stop()
}
func show() { call(user, "ShowWindow", win, 9); call(user, "SetForegroundWindow", win) }
func choose(id int) {
	var display [260]uint16
	bi := browseInfo{owner: win, display: &display[0], title: wide("Choose a folder"), flags: 0x41}
	p := call(shell, "SHBrowseForFolderW", unsafe.Pointer(&bi))
	if p == 0 {
		return
	}
	defer call(windows.NewLazySystemDLL("ole32.dll"), "CoTaskMemFree", p)
	var path [32768]uint16
	if call(shell, "SHGetPathFromIDListEx", p, unsafe.Pointer(&path[0]), 32768, 0) != 0 {
		set(id, windows.UTF16ToString(path[:]))
		if id == 1 && !config.PollOverride {
			config.Source = get(1)
			check(7, config.UsePolling())
		}
	}
}
func addTray() {
	tray = notifyIcon{hwnd: win, id: 1, flags: 7, callback: trayMessage, icon: call(user, "LoadIconW", call(kernel, "GetModuleHandleW", 0), 1)}
	tray.size = uint32(unsafe.Sizeof(tray))
	copy(tray.tip[:], windows.StringToUTF16("Track Tagger"))
	trayOK = call(shell, "Shell_NotifyIconW", 0, unsafe.Pointer(&tray)) != 0
}
func proc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0x10:
		if !trayOK {
			box("The notification-area icon is unavailable. Use Quit in this window to stop safely and exit.")
			return 0
		}
		if !config.CloseExplained {
			box("Track Tagger will remain in the notification area. Use its tray menu to Show, Stop, or Quit. Closing this window does not stop processing.")
			config.CloseExplained = true
			_ = Save(directory, config)
		}
		call(user, "ShowWindow", h, 0)
		return 0
	case 2:
		call(shell, "Shell_NotifyIconW", 2, unsafe.Pointer(&tray))
		call(user, "PostQuitMessage", 0)
		return 0
	case 0x115:
		position := scrollPosition
		switch w & 0xffff {
		case 0:
			position -= 30
		case 1:
			position += 30
		case 2:
			position -= 180
		case 3:
			position += 180
		case 4, 5:
			position = int((w >> 16) & 0xffff)
		}
		scrollTo(position)
		return 0
	case 0x20a:
		scrollTo(scrollPosition - int(int16(w>>16))/4)
		return 0
	case wake:
		queueMu.Lock()
		q := queue
		queue = nil
		queueMu.Unlock()
		for _, f := range q {
			f()
		}
		return 0
	case trayMessage:
		if uint32(l) == 0x203 {
			show()
		}
		if uint32(l) == 0x205 {
			menu := call(user, "CreatePopupMenu")
			for _, x := range []struct {
				id   uintptr
				text string
			}{{101, "Show Track Tagger"}, {102, "Open browser"}, {103, "Stop"}, {104, "Quit"}} {
				call(user, "AppendMenuW", menu, 0, x.id, ptr(x.text))
			}
			var p point
			call(user, "GetCursorPos", unsafe.Pointer(&p))
			call(user, "SetForegroundWindow", h)
			id := call(user, "TrackPopupMenu", menu, 0x100, uintptr(p.x), uintptr(p.y), 0, h, 0)
			call(user, "DestroyMenu", menu)
			switch id {
			case 101:
				show()
			case 102:
				openBrowser()
			case 103:
				stop(false)
			case 104:
				stop(true)
			}
		}
		return 0
	case 0x111:
		id := int(w & 0xffff)
		if id == 1 && uint32(w>>16) == 0x200 && !config.PollOverride {
			config.Source = get(1)
			check(7, config.UsePolling())
		}
		switch id {
		case 3:
			choose(1)
		case 8:
			choose(2)
		case 4, 5, 6:
			for _, i := range []int{4, 5, 6} {
				check(i, i == id)
			}
			label := "Start preview"
			if id == 5 {
				label = "Start copying eligible recordings"
			}
			if id == 6 {
				label = "Start updating originals"
			}
			set(10, label)
			refresh()
		case 7:
			config.PollOverride = true
		case 10:
			start()
		case 11:
			stop(false)
		case 12:
			openBrowser()
		case 14:
			stop(true)
		case 15:
			toggleDetails()
		case 41:
			if updates != nil {
				updates.Check()
			}
		case 42:
			config.AutoCheckUpdates = checked(42)
			if e := Save(directory, config); e != nil {
				set(43, "Could not save update preference: "+e.Error())
			}
		case 44:
			openReleaseNotes()
		case 45:
			confirmInstallUpdate()
		case 13:
			if call(user, "OpenClipboard", h) != 0 {
				call(user, "EmptyClipboard")
				text := diagnosticDetails()
				b := windows.StringToUTF16(text)
				mem := call(kernel, "GlobalAlloc", 0x42, uintptr(len(b)*2))
				p := call(kernel, "GlobalLock", mem)
				if p == 0 {
					call(kernel, "GlobalFree", mem)
					call(user, "CloseClipboard")
					break
				}
				call(windows.NewLazySystemDLL("ntdll.dll"), "RtlMoveMemory", p, unsafe.Pointer(&b[0]), uintptr(len(b)*2))
				call(kernel, "GlobalUnlock", mem)
				call(user, "SetClipboardData", 13, mem)
				call(user, "CloseClipboard")
			}
		}
		return 0
	}
	if m == taskbar {
		addTray()
		return 0
	}
	return call(user, "DefWindowProcW", h, uintptr(m), w, l)
}
func Run(version string) {
	appVersion = version
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	call(user, "SetProcessDPIAware")
	call(windows.NewLazySystemDLL("ole32.dll"), "CoInitializeEx", 0, 2)
	defer call(windows.NewLazySystemDLL("ole32.dll"), "CoUninitialize")
	var configErr error
	directory, configErr = ConfigDir()
	if configErr != nil {
		box("Cannot find your user configuration folder: " + configErr.Error())
		return
	}
	var e error
	config, e = Load(directory)
	if e != nil {
		config = Settings{Mode: app.DryRun, AutoCheckUpdates: true}
	}
	sid := "user"
	if token, err := windows.OpenCurrentProcessToken(); err == nil {
		if u, err := token.GetTokenUser(); err == nil {
			sid = u.User.Sid.String()
		}
		token.Close()
	}
	mutex, err := windows.CreateMutex(nil, false, wide(`Local\TrackTaggerLauncher-`+sid))
	if err == windows.ERROR_ALREADY_EXISTS {
		if mutex != 0 {
			defer windows.CloseHandle(mutex)
		}
		var h uintptr
		for i := 0; i < 30; i++ {
			h = call(user, "FindWindowW", ptr("TrackTaggerLauncher"), 0)
			if h != 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if h == 0 {
			box("Track Tagger is already starting. Try opening it again shortly.")
			return
		}
		call(user, "ShowWindow", h, 9)
		call(user, "SetForegroundWindow", h)
		return
	}
	if err != nil {
		box("Cannot establish the single-instance lock: " + err.Error())
		return
	}
	if mutex != 0 {
		defer windows.CloseHandle(mutex)
	}
	instance := call(kernel, "GetModuleHandleW", 0)
	class := wc{proc: syscall.NewCallback(proc), instance: instance, cursor: call(user, "LoadCursorW", 0, 32512), background: 16, class: wide("TrackTaggerLauncher")}
	class.icon = call(user, "LoadIconW", instance, 1)
	class.smallIcon = class.icon
	class.size = uint32(unsafe.Sizeof(class))
	call(user, "RegisterClassExW", unsafe.Pointer(&class))
	dc := call(user, "GetDC", 0)
	scale = float64(call(gdi, "GetDeviceCaps", dc, 88)) / 96
	call(user, "ReleaseDC", 0, dc)
	px := func(n int) uintptr { return uintptr(float64(n) * scale) }
	height := px(650)
	screen := call(user, "GetSystemMetrics", 1)
	if height > screen-60 {
		height = screen - 60
	}
	win = call(user, "CreateWindowExW", 0, ptr("TrackTaggerLauncher"), ptr("Track Tagger"), 0xea0000, 0x80000000, 0x80000000, px(760), height, 0, 0, instance, 0)
	if win == 0 {
		box("Cannot create the Track Tagger window. Restart Windows and try again.")
		return
	}
	font := call(gdi, "CreateFontW", uintptr(int32(-int32(16*scale))), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, ptr("Segoe UI"))
	defer call(gdi, "DeleteObject", font)
	add := func(id int, kind, text string, style uintptr, x, y, width, height int) {
		if id < 40 {
			y += 80
		}
		h := call(user, "CreateWindowExW", 0, ptr(kind), ptr(text), 0x50000000|style, px(x), px(y), px(width), px(height), win, uintptr(id), instance, 0)
		controls[id] = h
		call(user, "SendMessageW", h, 0x30, font, 1)
	}
	add(40, "STATIC", "Version "+appVersion, 0, 20, 15, 165, 25)
	add(41, "BUTTON", "Check for updates", 0x10000, 190, 10, 165, 30)
	add(42, "BUTTON", "Check automatically at startup", 0x10003, 370, 12, 350, 27)
	check(42, config.AutoCheckUpdates)
	add(43, "STATIC", "Updates are installed only when you choose Install update.", 0, 20, 48, 430, 38)
	add(44, "BUTTON", "Release notes", 0x10000, 460, 48, 125, 30)
	add(45, "BUTTON", "Install update", 0x10000, 595, 48, 125, 30)
	add(30, "STATIC", "Music folder", 0, 20, 15, 650, 22)
	add(1, "EDIT", config.Source, 0x810080, 20, 40, 590, 27)
	add(3, "BUTTON", "Browse…", 0x10000, 620, 40, 100, 28)
	add(31, "STATIC", "Processing mode — choose carefully before starting", 0, 20, 85, 700, 22)
	add(4, "BUTTON", "Preview — review without changing music", 0x10009, 20, 112, 700, 26)
	add(5, "BUTTON", "Copy to another folder — sources stay unchanged", 0x10009, 20, 142, 700, 26)
	add(6, "BUTTON", "Update originals — change metadata in the music folder", 0x10009, 20, 172, 700, 26)
	add(32, "STATIC", "Copy destination", 0, 20, 211, 690, 22)
	add(2, "EDIT", config.Destination, 0x810080, 20, 236, 590, 27)
	add(8, "BUTTON", "Browse…", 0x10000, 620, 236, 100, 28)
	add(7, "BUTTON", "Use polling (network shares and mapped drives)", 0x10003, 20, 278, 700, 26)
	check(7, config.UsePolling())
	id := 4
	if config.Mode == app.Copy {
		id = 5
	}
	if config.Mode == app.Write {
		id = 6
	}
	check(id, true)
	add(33, "STATIC", "Copy and Update automatically process eligible recordings after they settle.\r\nAmbiguous recordings wait for review. Start is required every time you open this app.", 0, 20, 313, 700, 44)
	label := "Start preview"
	if id == 5 {
		label = "Start copying eligible recordings"
	}
	if id == 6 {
		label = "Start updating originals"
	}
	add(10, "BUTTON", label, 0x10001, 20, 372, 350, 32)
	add(11, "BUTTON", "Stop", 0x10000, 380, 372, 90, 32)
	add(12, "BUTTON", "Open browser", 0x10000, 480, 372, 140, 32)
	add(14, "BUTTON", "Quit", 0x10000, 630, 372, 90, 32)
	add(20, "STATIC", "Stopped", 0, 20, 418, 700, 24)
	add(21, "EDIT", "Choose folders and click Start.", 0x808080, 20, 447, 700, 36)
	add(15, "BUTTON", "Show details", 0x10000, 20, 492, 140, 28)
	add(22, "EDIT", "", 0xa10844, 20, 530, 700, 65)
	add(13, "BUTTON", "Copy details", 0x10000, 580, 603, 140, 28)
	call(user, "ShowWindow", controls[22], 0)
	call(user, "ShowWindow", controls[13], 0)
	var client rect
	call(user, "GetClientRect", win, unsafe.Pointer(&client))
	scrollMaximum = int(px(605)) - int(client.bottom)
	if scrollMaximum < 0 {
		scrollMaximum = 0
	}
	call(user, "SetScrollRange", win, 1, 0, scrollMaximum, 1)
	taskbar = uint32(call(user, "RegisterWindowMessageW", ptr("TaskbarCreated")))
	addTray()
	initializeUpdates()
	refresh()
	call(user, "ShowWindow", win, 1)
	call(user, "UpdateWindow", win)
	if config.AutoCheckUpdates {
		updates.Check()
	}
	if e != nil {
		box(fmt.Sprint("Settings could not be loaded; Preview selected. ", e))
	}
	var message msg
	for {
		r := call(user, "GetMessageW", unsafe.Pointer(&message), 0, 0, 0)
		if r == 0 || r == ^uintptr(0) {
			break
		}
		if call(user, "IsDialogMessageW", win, unsafe.Pointer(&message)) == 0 {
			call(user, "TranslateMessage", unsafe.Pointer(&message))
			call(user, "DispatchMessageW", unsafe.Pointer(&message))
		}
	}
}
