// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import (
	"os"
	"testing"
)

func TestProductionWatchDispatchStopsOnInterrupt(t *testing.T) {
	assertSignalStopsForegroundWatch(t, os.Interrupt)
}
