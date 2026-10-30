// Copyright 2026 Ayesh Almeida
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openPrivateDiagnosticExportFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, errors.New("create private export")
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		_ = removeCreatedDiagnosticExportFile(path, info)
		return nil, errors.New("unsafe export destination")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = removeCreatedDiagnosticExportFile(path, info)
		return nil, errors.New("protect export destination")
	}
	return file, nil
}
