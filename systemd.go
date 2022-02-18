/*
 * Copyright (c) 2013 Vadim Vygonets <vadik@vygo.net>
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
	"syscall"

	"github.com/guelfey/go.dbus"
)

const (
	sdDest    = "org.freedesktop.login1"
	sdPath    = "/org/freedesktop/login1"
	sdIface   = sdDest + ".Manager"
	sdSignal  = "PrepareForSleep"
	sdSigName = sdIface + "." + sdSignal
	sdInhibit = sdIface + ".Inhibit"
	sdFilter  = "type='signal',interface='" + sdIface + "',member=" +
		sdSignal
)

type SystemdBackend struct {
	obj *dbus.Object
	fd  int
}

func NewSystemdBackend(conn *dbus.Conn) (Backend, error) {
	be := SystemdBackend{
		obj: conn.Object(sdDest, sdPath),
		fd:  -1,
	}
	if err := be.inhibit(); err != nil {
		return nil, err
	}
	return &be, nil
}

func (*SystemdBackend) Name() string   { return "systemd" }
func (*SystemdBackend) Filter() string { return sdFilter }

func (be *SystemdBackend) inhibit() error {
	if be.fd != -1 {
		log.Println(logPref, "(*SystemdBackend).inhibit()"+
			" called twice without closing FD")
		// The fd is not trusted, better close it
		if err := be.Release(); err != nil {
			log.Println(logPref, err)
			// Try to inhibit anyway
		}
	}
	r := be.obj.Call(sdInhibit, 0,
		"sleep", "ussssr", "Lock screen", "delay")
	if r.Err != nil {
		return r.Err
	}
	if fd, ok := r.Body[0].(dbus.UnixFD); ok && fd >= 0 {
		be.fd = int(fd)
		syscall.CloseOnExec(be.fd)
		return nil
	}
	return errors.New(sdInhibit + "() returned an invalid value")
}

func (be *SystemdBackend) Handle(sig *dbus.Signal) (bool, error) {
	if sig.Path != sdPath || sig.Name != sdSigName || len(sig.Body) < 1 {
		return false, nil
	}
	b, ok := sig.Body[0].(bool)
	if ok && !b {
		return b, be.inhibit()
	}
	return b, nil
}

func (be *SystemdBackend) Release() (err error) {
	if be.fd != -1 {
		err = syscall.Close(be.fd)
		be.fd = -1
	}
	return
}
