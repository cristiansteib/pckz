package ui

import (
	"sync"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// GTK must only be touched from the main thread. Background goroutines
// (sound server events, tray clicks, pactl calls) hand work over with post.

var (
	queueMu sync.Mutex
	queue   []func()
	drainFn glib.SourceFunc = func(uintptr) bool {
		queueMu.Lock()
		q := queue
		queue = nil
		queueMu.Unlock()
		for _, f := range q {
			f()
		}
		return false
	}
)

func post(f func()) {
	queueMu.Lock()
	queue = append(queue, f)
	schedule := len(queue) == 1
	queueMu.Unlock()
	if schedule {
		glib.IdleAdd(&drainFn, 0)
	}
}

// Signal handlers.
//
// puregotk allocates one C trampoline per distinct Go callback and never frees
// it, so creating a closure per widget would leak a trampoline every time a
// row is rebuilt. Instead each signal has a single shared handler that looks
// up what to do by the emitting widget's pointer.

var (
	rowActions   = map[uintptr]func(){}
	clickActions = map[uintptr]func(){}
	valueActions = map[uintptr]func(float64){}
	suppress     bool // set while the UI itself changes a slider

	onRowActivated = func(r adw.ActionRow) {
		if f := rowActions[r.GoPointer()]; f != nil {
			f()
		}
	}
	onClicked = func(b gtk.Button) {
		if f := clickActions[b.GoPointer()]; f != nil {
			f()
		}
	}
	onValueChanged = func(r gtk.Range) {
		if suppress {
			return
		}
		if f := valueActions[r.GoPointer()]; f != nil {
			f(r.GetValue())
		}
	}
)

func onRow(r *adw.ActionRow, f func()) {
	rowActions[r.GoPointer()] = f
	r.ConnectActivated(&onRowActivated)
}

func onClick(b *gtk.Button, f func()) {
	clickActions[b.GoPointer()] = f
	b.ConnectClicked(&onClicked)
}

func onValue(s *gtk.Scale, f func(float64)) {
	valueActions[s.GoPointer()] = f
	s.ConnectValueChanged(&onValueChanged)
}

func forget(ptrs ...uintptr) {
	for _, p := range ptrs {
		delete(rowActions, p)
		delete(clickActions, p)
		delete(valueActions, p)
	}
}

// setValue moves a slider without treating it as user input.
func setValue(s *gtk.Scale, v float64) {
	suppress = true
	s.SetValue(v)
	suppress = false
}
