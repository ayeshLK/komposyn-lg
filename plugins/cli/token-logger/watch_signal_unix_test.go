// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package main

import (
	"os"
	"syscall"
	"testing"
)

func TestProductionWatchDispatchStopsOnInterruptAndTerminate(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			assertSignalStopsForegroundWatch(t, signal)
		})
	}
}
