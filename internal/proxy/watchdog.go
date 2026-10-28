package proxy

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// errIdleTimeout is the cancel cause of an upstream request the idle watchdog
// ended.
var errIdleTimeout = errors.New("upstream idle timeout")

// watchdog ends an upstream exchange that goes silent. It is armed while the
// proxy waits on the upstream and disarmed while the proxy writes to the
// client, so a slow client never trips it. When it fires it cancels the
// upstream request, which unblocks the pending read. Safe for concurrent use.
type watchdog struct {
	timer  *time.Timer
	idle   time.Duration
	fired  atomic.Bool
	cancel context.CancelCauseFunc
}

// newWatchdog returns a disarmed watchdog that, once armed, cancels with
// errIdleTimeout after idle of silence.
func newWatchdog(idle time.Duration, cancel context.CancelCauseFunc) *watchdog {
	w := &watchdog{idle: idle, cancel: cancel}
	w.timer = time.AfterFunc(idle, w.fire)
	w.timer.Stop()
	return w
}

func (w *watchdog) fire() {
	w.fired.Store(true)
	w.cancel(errIdleTimeout)
}

// Arm starts a new idle period. After the watchdog fired it does nothing.
func (w *watchdog) Arm() {
	if !w.fired.Load() {
		w.timer.Reset(w.idle)
	}
}

// Disarm stops the idle period.
func (w *watchdog) Disarm() { w.timer.Stop() }

// Fired reports whether the watchdog cancelled the upstream request.
func (w *watchdog) Fired() bool { return w.fired.Load() }
