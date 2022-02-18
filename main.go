/*
 * Copyright (c) 2013, 2022 Vadim Vygonets <vadik@vygo.net>
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

// UPower/Systemd Screen Saving Sleep Reactor
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"os/exec"
	"time"

	dbus "github.com/guelfey/go.dbus"
)

const (
	logPref  = "ussssr:"
	addMatch = "org.freedesktop.DBus.AddMatch"
)

type (
	Backend interface {
		Name() string
		Filter() string
		Handle(*dbus.Signal) (act bool, err error)
		Release() error
	}
	startReq struct {
		started, finished chan error
	}
)

var (
	debug bool
	start = make(chan startReq)
)

func debugln(v ...interface{}) {
	if debug {
		log.Println(v...)
	}
}

func runLoop(nowait bool, args []string) {
	var (
		running  bool
		finished chan error
		stop     = make(chan error, 1)
	)
	for {
		select {
		case req := <-start:
			if running {
				req.started <- errors.New("exec: already running")
				req.finished <- nil
				break
			}

			cmd := exec.Command(args[0], args[1:]...)
			if err := cmd.Start(); err != nil {
				req.started <- err
				req.finished <- nil
				break
			}
			running = true
			req.started <- nil

			go func() { stop <- cmd.Wait() }()

			if nowait {
				req.finished <- nil
				break
			}
			finished = req.finished
		case err := <-stop:
			if !running {
				log.Println(logPref, "wait: not running")
				break
			}
			running = false
			if ee, ok := err.(*exec.ExitError); ok {
				// log and swallow non-zero exit status
				log.Println(logPref, "wait:", ee)
				err = nil
			}
			if finished != nil {
				finished <- err
				finished = nil
			}
		}
	}
}

func run(finished chan error) error {
	started := make(chan error, 1)
	start <- startReq{started, finished}
	return <-started
}

func parseFlags() (bool, []string) {
	var nowait = flag.Bool("b", false, "run command in the background")
	flag.BoolVar(&debug, "d", false, "debug")
	flag.Usage = func() {
		os.Stderr.WriteString(
			`USSSSR - UPower/Systemd Screen Saving Sleep Reactor

USSSSR listens to sleep (suspend, hibernate) events broadcast
by UPower or systemd on D-Bus and reacts to them by running
a command which presumably activates a screen saver.

If the command is a screen saver that doesn't fork and doesn't
exit until the screen is unlocked (such as "slock"), the flag -b
should be used.  Commands that activate the screen saver and
exit immediately (such as in case of "xset s activate" or
"xscreensaver -lock") would be better without it.

Usage: ` + os.Args[0] + ` [-b] [-d] COMMAND [ARGS...]
`)
		flag.PrintDefaults()
	}

	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}
	return *nowait, flag.Args()
}

func newBackend(conn *dbus.Conn) Backend {
	for _, f := range []func(*dbus.Conn) (Backend, error){
		NewSystemdBackend,
		NewUPowerBackend,
	} {
		if be, err := f(conn); err == nil {
			debugln(logPref, "using backend", be.Name(),
				"with filter", be.Filter())
			return be
		}
	}
	log.Fatalln(logPref, "No backend found, exiting")
	// NOTREACHED
	return nil
}

func main() {
	nowait, args := parseFlags()

	conn, err := dbus.SystemBus()
	if err != nil {
		log.Fatalln(logPref, "connect to D-Bus system bus:", err)
	}
	defer conn.Close()

	be := newBackend(conn)
	if r := conn.BusObject().Call(addMatch, 0, be.Filter()); r.Err != nil {
		log.Fatalln(logPref, "add signal filter:", r.Err)
	}

	go runLoop(nowait, args)

	sc := make(chan *dbus.Signal, 5)
	conn.Signal(sc)

	for sig := range sc {
		debugln(logPref, "signal received:", sig)
		act, err := be.Handle(sig)
		if err != nil {
			log.Println(logPref, err)
		}
		if act {
			finished := make(chan error, 1)
			if err = run(finished); err != nil {
				log.Println(logPref, err)
			} else {
				debugln(logPref, "running command")
			}

			select {
			case <-finished:
				debugln(logPref, "command finished")
				// After running a command, sleep for a bit
				// to let the screen saver engage
				time.Sleep(time.Second / 2)
			case <-time.After(3 * time.Second):
				log.Println(logPref,
					"command timed out, consider using -b")
				go func() { <-finished }()
			}

			if err = be.Release(); err != nil {
				log.Println(logPref, be.Name()+":", err)
			}
		}
	}
}
