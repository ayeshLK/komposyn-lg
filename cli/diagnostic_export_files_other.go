// Copyright 2026 Ayesh Almeida
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build !windows && !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package main

import (
	"errors"
	"os"
)

func openPrivateDiagnosticExportFile(string) (*os.File, error) {
	return nil, errors.New("private diagnostic exports are unsupported on this platform")
}
