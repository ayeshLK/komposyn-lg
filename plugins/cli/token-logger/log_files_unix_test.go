// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogFilesHavePrivateUnixPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	logger, err := newEventLogger(dir, "1.2.3", "0.9.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	logger.Close()
	for _, path := range []string{dir, logger.sink.active, logger.sink.lockPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o700)
		if !info.IsDir() {
			want = 0o600
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s permissions=%#o, want %#o", path, got, want)
		}
	}
}
