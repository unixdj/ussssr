// Copyright 2013 Vadim Vygonets
//
// This program is free software. It comes without any warranty, to
// the extent permitted by applicable law. You can redistribute it
// and/or modify it under the terms of the Do What The Fuck You Want
// To Public License, Version 2, as published by Sam Hocevar. See
// the LICENSE file or http://sam.zoy.org/wtfpl/ for more details.

package main

import "github.com/guelfey/go.dbus"

const (
	upDest   = "org.freedesktop.UPower"
	upPath   = "/org/freedesktop/UPower"
	upIface  = upDest
	upSignal = "NotifySleep" // XXX: or should we use "Sleeping" instead?
	upTest   = upIface + ".SuspendAllowed"
	upFilter = "type='signal',sender='" + upDest + "',interface='" +
		upIface + "',member=" + upSignal
)

type UPowerBackend bool

func NewUPowerBackend(conn *dbus.Connection) (Backend, error) {
	if r := <-conn.Object(upDest, upPath).Call(upTest, 0); r.Err != nil {
		return nil, r.Err
	}
	return UPowerBackend(false), nil
}

func (UPowerBackend) Name() string   { return "UPower" }
func (UPowerBackend) Filter() string { return upFilter }
func (UPowerBackend) Release() error { return nil }

func (UPowerBackend) Handle(sig dbus.Signal) (bool, error) {
	return sig.Path == upPath && sig.Name == upSignal+"."+upIface, nil
}
