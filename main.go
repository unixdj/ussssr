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

// UPower/Systemd Screen Saving Sleep Reactor
package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"strconv"
	"time"
)

var conf = struct {
	cmd      []string
	delay    time.Duration
	bg       bool
	debug    bool
	logLevel int
}{
	logLevel: 1,
	delay:    defaultDelay,
}

// logging

func loglnAt(ll int, v ...interface{}) {
	if conf.logLevel >= ll {
		log.Println(v...)
	}
}

func logln(v ...interface{})   { loglnAt(1, v...) }
func debugln(v ...interface{}) { loglnAt(2, v...) }

// command line flags

type durFlag struct{ *time.Duration } // -d, -t

func (d durFlag) String() string {
	if d.Duration != nil {
		return d.Duration.String()
	}
	return ""
}

func (d durFlag) Set(s string) error {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		*d.Duration = time.Duration(f * float64(time.Second))
	} else if *d.Duration, err = time.ParseDuration(s); err != nil {
		return err
	}
	if *d.Duration < 0 {
		return errors.New("invalid duration")
	}
	return nil
}

func printHelp(long bool) {
	w := flag.CommandLine.Output()
	if long {
		io.WriteString(w,
			`USSSSR - UPower/Systemd Screen Saving Sleep Reactor

USSSSR runs a command when a UPower or systemd sleep (suspend,
hibernate) event is received on D-Bus.

When a sleep signal is received, if the command is not running,
it is started.  If a foreground command exits with status 0
before the timeout, or a background command is started, the sleep
inhibit lock is released after a delay to let the screen saver
engage before sleep.  Otherwise the lock is released immediately.

A screen saver that doesn't fork or exit until the screen is
unlocked (such as "slock") should be run in the background, a
command that exits immediately (such as "xset s activate" or
"xscreensaver -lock") in the foreground.

Delay can be specified in seconds (e.g., "0.5") or in any format
accepted by time.ParseDuration (e.g., "500ms").

`)
	}
	io.WriteString(w, "Usage:\n  ")
	io.WriteString(w, os.Args[0])
	io.WriteString(w, " [flags] command [argument ...]\n\nFlags:\n")
	flag.PrintDefaults()
}

func help(long bool) {
	flag.CommandLine.SetOutput(os.Stdout)
	printHelp(long)
	os.Exit(0)
}

func usage() {
	os.Stderr.WriteString("\n")
	printHelp(false)
}

func parseFlags() {
	flag.Var(durFlag{&conf.delay}, "d", "`delay` after command")
	flag.BoolVar(&conf.bg, "b", false, "run command in the background")
	flag.BoolVar(&conf.debug, "debug", false,
		"use debug backend (non-functional)")
	flag.BoolFunc("q", "quiet",
		func(string) error { conf.logLevel--; return nil })
	flag.BoolFunc("v", "verbose",
		func(string) error { conf.logLevel++; return nil })
	flag.BoolFunc("h", "short help",
		func(string) error { help(false); return nil })
	flag.BoolFunc("help", "long help",
		func(string) error { help(true); return nil })
	flag.Usage = usage
	flag.Parse()
	conf.cmd = flag.Args()
	if len(conf.cmd) == 0 {
		printHelp(false)
		os.Exit(2)
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("ussssr: ")
	parseFlags()
	loop()
}
