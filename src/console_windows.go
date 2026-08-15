//go:build windows

package main

import "syscall"

func init() {
	if p, err := syscall.LoadDLL("kernel32.dll"); err == nil {
		if f, err := p.FindProc("SetConsoleOutputCP"); err == nil {
			f.Call(65001)
		}
	}
}
