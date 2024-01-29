/*
 * Copyright (c) 2024 Vadim Vygonets <vadik@vygo.net>
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
	"os"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

/*
DebugBackend is a debug backend receiving commands from stdin.
When -debug flag is given, no dbus connection is opened;
instead, newDebugBackend returns a *DebugBackend and replaces the
run function with one that simulates execution.

DebugBackend reads commands from stdin, simulating events:

    s  Sleep signal received
    w  Wakeup signal received
    e  Running command exits with status 0
    k  Running command killed

'e' and 'k' are no-ops if no command is running.
*/
type DebugBackend struct {
	cmd       chan byte
	sc        chan *dbus.Signal
	stopped   chan<- error
	start     chan chan<- error
	inhibited bool
}

func newDebugBackend() (Backend, chan *dbus.Signal) {
	be := &DebugBackend{
		cmd:   make(chan byte),
		sc:    make(chan *dbus.Signal),
		start: make(chan chan<- error),
	}
	go be.read()
	go be.loop()
	be.inhibit()
	run = be.run
	return be, be.sc
}

func (be *DebugBackend) read() {
	var buf [16]byte
	for {
		n, err := os.Stdin.Read(buf[:])
		if err != nil {
			log.Fatalln("read failed:", err)
		}
		for _, v := range buf[:n] {
			switch v {
			case 's', 'w', 'e', 'k':
				be.cmd <- v
			}
		}
	}
}

var (
	debugSleepSignal  = dbus.Signal{Name: "sleep"}
	debugWakeupSignal = dbus.Signal{Name: "wakeup"}
	ErrDebugKilled    = errors.New("killed")
)

func (be *DebugBackend) loop() {
	for {
		select {
		case b := <-be.cmd:
			switch b {
			case 's':
				be.sc <- &debugSleepSignal
			case 'w':
				be.sc <- &debugWakeupSignal
			case 'e':
				if be.stopped != nil {
					be.stopped <- nil
					be.stopped = nil
				}
			case 'k':
				if be.stopped != nil {
					be.stopped <- ErrDebugKilled
					be.stopped = nil
				}
			}
		case be.stopped = <-be.start:
		}
	}
}

func (*DebugBackend) Name() string   { return "debug" }
func (*DebugBackend) Filter() string { return "none" }

func (be *DebugBackend) inhibit() {
	if be.inhibited {
		logln("wakeup received while sleep inhibited")
	}
	debugln("inhibit sleep")
	be.inhibited = true
}

func (be *DebugBackend) Handle(sig *dbus.Signal) (bool, error) {
	sleep := sig.Name == "sleep"
	if !sleep {
		be.inhibit()
	}
	return sleep, nil
}

func (be *DebugBackend) Release() error {
	be.inhibited = false
	return nil
}

func (be *DebugBackend) run(stopped chan<- error) error {
	be.start <- stopped
	return nil
}

func (DebugBackend) MaxInhibit() (time.Duration, error) {
	return -1, nil
}
