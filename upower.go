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

import "github.com/guelfey/go.dbus"

const (
	upDest    = "org.freedesktop.UPower"
	upPath    = "/org/freedesktop/UPower"
	upIface   = upDest
	upSignal  = "NotifySleep" // XXX: or should we use "Sleeping" instead?
	upSigName = upIface + "." + upSignal
	upTest    = upIface + ".SuspendAllowed"
	upFilter  = "type='signal',interface='" + upIface + "',member=" +
		upSignal
)

type UPowerBackend struct{}

func NewUPowerBackend(conn *dbus.Conn) (Backend, error) {
	if r := conn.Object(upDest, upPath).Call(upTest, 0); r.Err != nil {
		return nil, r.Err
	}
	return UPowerBackend{}, nil
}

func (UPowerBackend) Name() string   { return "UPower" }
func (UPowerBackend) Filter() string { return upFilter }
func (UPowerBackend) Release() error { return nil }

func (UPowerBackend) Handle(sig *dbus.Signal) (bool, error) {
	return sig.Path == upPath && sig.Name == upSigName, nil
}
