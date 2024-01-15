/*
 * Copyright (c) 2013, 2024 Vadim Vygonets <vadik@vygo.net>
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
	"syscall"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

const (
	sdDest       = "org.freedesktop.login1"
	sdPath       = "/org/freedesktop/login1"
	sdIface      = sdDest + ".Manager"
	sdSignal     = "PrepareForSleep"
	sdSigName    = sdIface + "." + sdSignal
	sdInhibit    = sdIface + ".Inhibit"
	sdMaxInhibit = sdIface + ".InhibitDelayMaxUSec"
	sdFilter     = "type='signal',interface='" + sdIface + "',member=" +
		sdSignal
)

type SystemdBackend struct {
	obj dbus.BusObject
	fd  int
}

var (
	ErrSDInhibit    = errors.New("invalid response from " + sdInhibit)
	ErrSDMaxInhibit = errors.New("invalid response from " + sdMaxInhibit)
)

func NewSystemdBackend(conn *dbus.Conn) Backend {
	be := SystemdBackend{
		obj: conn.Object(sdDest, sdPath),
		fd:  -1,
	}
	if be.inhibit() != nil {
		return nil
	}
	return &be
}

func (*SystemdBackend) Name() string   { return "systemd" }
func (*SystemdBackend) Filter() string { return sdFilter }

func (be *SystemdBackend) inhibit() error {
	if be.fd != -1 {
		logln("systemd.inhibit called before releasing old lock")
		// The fd is not trusted, better close it
		if err := be.Release(); err != nil {
			logln(err)
		}
		// Try to inhibit anyway
	}
	r := be.obj.Call(sdInhibit, 0,
		"sleep", "ussssr", "Lock screen", "delay")
	if r.Err != nil {
		return r.Err
	} else if len(r.Body) < 1 {
		return ErrSDInhibit
	}
	fd, ok := r.Body[0].(dbus.UnixFD)
	if !ok || fd < 0 {
		return ErrSDInhibit
	}
	be.fd = int(fd)
	syscall.CloseOnExec(be.fd)
	return nil
}

func (be *SystemdBackend) Handle(sig *dbus.Signal) (bool /* time.Duration, */, error) {
	if sig.Path != sdPath || sig.Name != sdSigName || len(sig.Body) < 1 {
		return false, ErrDBusSignal
	}
	var err error
	sleep, ok := sig.Body[0].(bool)
	if !ok {
		err = ErrDBusSignal
	} else if !sleep {
		err = be.inhibit()
	} else {
		// var v dbus.Variant
		v, ee := be.obj.GetProperty(sdMaxInhibit)
		vv, ok := v.Value().(int)
		debugln(vv, ok, ee)
	}
	return sleep, err
}

func (be *SystemdBackend) Release() error {
	var err error
	if be.fd != -1 {
		err = syscall.Close(be.fd)
		be.fd = -1
	} else {
		logln("systemd.Release called but no inhibit lock is held")
	}
	return err
}

func (be SystemdBackend) MaxDelay() (time.Duration, error) {
	vari, err := be.obj.GetProperty(sdMaxInhibit)
	if err != nil {
		return -1, err
	}
	v, ok := vari.Value().(uint64)
	if !ok {
		return -1, ErrSDMaxInhibit
	}
	const max = uint64((1<<63 - 1) / time.Microsecond)
	if v > max {
		v = max // 292 years is enough for anyone
	}
	return time.Duration(v) * time.Microsecond, nil
}
