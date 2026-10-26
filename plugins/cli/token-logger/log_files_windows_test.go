// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateWindowsLogDirectoryAndFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private-logs")
	logger, err := newEventLogger(dir, "1.2.3", "0.9.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	logger.Close()
	if _, err := inspectLogDirectory(dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{logger.sink.active, logger.sink.lockPath} {
		if exists, _, err := inspectLogTarget(path); err != nil || !exists {
			t.Fatalf("private log target %s: exists=%t err=%v", path, exists, err)
		}
	}
}
