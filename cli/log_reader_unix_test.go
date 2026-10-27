// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLogReaderRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "klg.log")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := readManagedLogs(dir, logReadOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.Available || !result.ReadFailure || len(result.UnreadableFiles) != 1 || result.UnreadableFiles[0] != "klg.log" {
		t.Fatalf("FIFO was not rejected as an unsafe target: %+v", result)
	}
}
