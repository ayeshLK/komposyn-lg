// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func prepareSignalTestCommand(*exec.Cmd) {}

func sendSignalTestCommand(command *exec.Cmd, signal os.Signal) error {
	return command.Process.Signal(signal)
}

func TestProductionWatchDispatchStopsOnInterruptAndTerminate(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			assertSignalStopsForegroundWatch(t, signal)
		})
	}
}
