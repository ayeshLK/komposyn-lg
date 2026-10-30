// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateWindowsDiagnosticExportACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reviewed-evidence.json")
	file, err := openPrivateDiagnosticExportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("{}\n")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("export target is not regular: info=%v err=%v", info, err)
	}
	assertPrivateWindowsACL(t, path, false)
}
