/*
 * Copyright (c) 2013, 2022, 2024 Vadim Vygonets <vadik@vygo.net>
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package main

import (
	"errors"
	"log"
	"os/exec"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

const (
	defaultTimeout = 5 * time.Second        // default max inhibit time
	defaultDelay   = 500 * time.Millisecond // default delay after command
)

var ErrDBusSignal = errors.New("invalid D-Bus signal")

/*
Backend is the interface for backends.

The Handle method handles a received signal, returning a flag
indicating whether the signal is a sleep signal, and an error.
If the flag is true, the error must be nil.  For signals other
than sleep and wakeup, (false, ErrDBusSignal) must be returned.

The Release method is called after the sleep preparation is
complete, in order to release the sleep inhibit lock, if one is
taken.

The MaxInhibit method returns the current maximum inhibit delay.
If the query is not supported, the returned Duration must be -1.
*/
type Backend interface {
	Name() string                       // return backend name
	Filter() string                     // return string for DBus.AddMatch
	Handle(*dbus.Signal) (bool, error)  // handle signal
	Release() error                     // release sleep inhibit lock
	MaxInhibit() (time.Duration, error) // return maximum inhibit delay
}

// newBackend returns a Backend, or nil if none is available.
func newBackend(conn *dbus.Conn) Backend {
	var be Backend
	for _, f := range []func(*dbus.Conn) Backend{
		NewSystemdBackend,
		NewUPowerBackend,
	} {
		if be = f(conn); be != nil {
			break
		}
	}
	return be
}

// openConn initialises the D-Bus connection and returns backend
// and signal channel.
func openConn() (Backend, chan *dbus.Signal) {
	if conf.debug {
		debugln("using backend debug")
		return newDebugBackend()
	}

	conn, err := dbus.SystemBus()
	if err != nil {
		log.Fatalln("connect to D-Bus system bus:", err)
	}
	be := newBackend(conn)
	if be == nil {
		log.Fatalln("no backend available")
	}
	debugln("using backend", be.Name(), "with filter", be.Filter())

	const add = "org.freedesktop.DBus.AddMatch"
	if err := conn.BusObject().Call(add, 0, be.Filter()).Err; err != nil {
		log.Fatalln("add signal filter:", err)
	}
	sc := make(chan *dbus.Signal, 4)
	conn.Signal(sc)
	return be, sc
}

func wait(cmd *exec.Cmd, stopped chan<- error) {
	stopped <- cmd.Wait()
}

// _run starts the command, returning an error if it cannot be
// started.  If the error is nil, the wait status will be sent to
// stopped upon termination.
func _run(stopped chan<- error) error {
	cmd := exec.Command(conf.cmd[0], conf.cmd[1:]...)
	err := cmd.Start()
	if err == nil {
		go wait(cmd, stopped)
	}
	return err
}

var run = _run

// setTimeout sets *timeout according to the maximum inhibit
// delay max.  max is reduced by a safety margin of 1/16.  In
// background mode max is then capped to conf.delay.
func setTimeout(timeout *time.Duration, max time.Duration) {
	max -= max >> 4 // safety margin of 1/16 of maximum inhibit delay
	if conf.bg && max > conf.delay {
		max = conf.delay
	}
	if max != *timeout {
		*timeout = max
		debugln("timeout =", max)
	}
}

func updateTimeout(timeout *time.Duration, be Backend) {
	if max, err := be.MaxInhibit(); err != nil {
		logln(be.Name()+".MaxInhibit:", err)
	} else if max >= 0 {
		setTimeout(timeout, max)
	}
}

/*
loop initialises D-Bus connection and a Backend and runs the
event loop.

The event loop reacts to sleep and wakeup D-Bus signals, command
termination (exited or killed) and release timer expiring,
tracking state represented by two boolean variables:
  - running, indicating whether the command is running;
  - locked, indicating whether the release timer is running.

After sleep signal is received, the command is run if it's not
already running, and the release timer is started, stopped and
restarted as needed.  When the release timer expires, the backend
Release method is called, allowing the system to sleep.

The systemd backend takes a sleep inhibit lock at start and when
wakeup signal is received.  Any old lock held is released prior
to that.  If inhibiting fails, no state transition or action is
performed.  Its Release method releases the lock.

The UPower backend doesn't support wakeup signals and inhibiting
sleep.  Its Release method is a no-op.

State transitions and actions.  Empty: no action beyond state
change; "-": event does not occur in state.
  R=running, L=locked, T=true, f=false.
  +-----------------------+---------+-----------------------+
  |                       |         | initial state (R,L)   |
  |                       | state   +-----+-----+-----+-----+
  | event received        | change  | f,f | f,T | T,f | T,T |
  +-----------------------+---------+-----+-----+-----+-----+
  | sleep, exec ok        | R=T L=T | [a] | [a] | -   | -   |
  | sleep, exec failed    |     L=T | [b] | [b] | -   | -   |
  | sleep (no exec)       |     L=T | -   | -   | [b] |     |
  | wakeup, inhibit ok    |     L=f |     | [c] |     | [c] |
  | release timer expired |     L=f | -   | [d] | -   | [d] |
  | command terminated    | R=f     | -   | -   |     | [e] |
  +-----------------------+---------+-----+-----+-----+-----+
  [a] set release timer to timeout and deadline to now+timeout.
  [b] set release timer to expire immediately.
  [c] stop release timer.
  [d] release sleep inhibit lock.
  [e] in foreground mode, set release timer: if exit 0,
      to delay or until deadline, whichever is earlier;
      if exit non-zero or killed, to expire immediately.
*/
func loop() {
	be, sc := openConn()
	var (
		locked  bool                       // sleep actively inhibited
		running bool                       // command is running
		start   time.Time                  // command start time
		stopped = make(chan error)         // command status channel
		timeout = conf.delay               // inhibit release timeout
		release = time.NewTimer(time.Hour) // inhibit release timer
	)
	release.Stop()

	// The the effective timeout is capped to the maximum
	// inhibit delay minus a safety margin to account for
	// code runtime.  It is initially set according to the
	// default inhibit delay.  With systemd backend the
	// current maximum inhibit delay is queried and timeout
	// is adjusted after executing the command.
	setTimeout(&timeout, defaultTimeout)

	for {
		select {
		case sig := <-sc:
			debugln("signal received:", sig)
			if sleep, err := be.Handle(sig); err != nil {
				// wake-up signal but Inhibit failed,
				// or unknown signal
				logln(be.Name()+".Handle:", err)
				break
			} else if !sleep {
				debugln("wakeup")
				// Wake-up signal means that the old sleep
				// inhibit lock was released and a new one
				// taken.  If the release timer if running,
				// stop it to avoid releasing the new lock.
				if locked {
					if !release.Stop() {
						<-release.C
					}
					locked = false
				}
				break
			}

			// handling sleep signal
			if running {
				logln("exec: already running")
				// if previous timeouts/delays are active,
				// keep waiting, otherwise release immediately
				if !locked {
					locked = true
					release.Reset(0)
				}
				break
			}

			if !conf.bg {
				start = time.Now()
			}
			if locked && !release.Stop() {
				<-release.C
			}
			locked = true
			debugln("running command")
			if err := run(stopped); err != nil {
				// execution failed, release immediately
				logln(err)
				release.Reset(0)
				break
			}
			running = true
			// release after timeout
			updateTimeout(&timeout, be)
			release.Reset(timeout)

		case <-release.C:
			locked = false
			if running && !conf.bg {
				logln("command timed out, consider using -b")
			}
			debugln("releasing inhibit lock")
			if err := be.Release(); err != nil {
				logln(be.Name()+".Release:", err)
			}

		case err := <-stopped:
			running = false
			if err != nil {
				logln("wait:", err)
			}
			debugln("command finished")
			if locked && !conf.bg {
				// foreground, finished before timeout
				if !release.Stop() {
					<-release.C
				}
				// if command exited with status 0, release
				// after delay or at deadline, whichever is
				// earlier; otherwise release immediately.
				delay := time.Duration(0)
				if err == nil {
					delay = timeout - time.Since(start)
					if delay > conf.delay {
						delay = conf.delay
					}
				}
				release.Reset(delay)
			}
		}
	}
}
