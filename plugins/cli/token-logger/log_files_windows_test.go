// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestPrivateWindowsLogDirectoryAndFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private-logs")
	logger, err := newEventLogger(dir, "1.2.3", "0.9.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	logger.sink.maxFile = 512
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	logger.Close()
	archiveLine := []byte(fmt.Sprintf("{\"format_version\":1,\"padding\":%q}\n", strings.Repeat("x", 300)))
	if err := logger.sink.appendBatch([][]byte{archiveLine}); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectLogDirectory(dir); err != nil {
		t.Fatal(err)
	}
	assertPrivateWindowsACL(t, dir, true)
	for _, path := range []string{logger.sink.active, logger.sink.lockPath, logger.sink.archivePath(1)} {
		if exists, _, err := inspectLogTarget(path); err != nil || !exists {
			t.Fatalf("private log target %s: exists=%t err=%v", path, exists, err)
		}
		assertPrivateWindowsACL(t, path, false)
	}
}

func assertPrivateWindowsACL(t *testing.T, path string, directory bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read security descriptor for %s: %v", path, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatalf("read descriptor control for %s: %v", path, err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("%s DACL is not protected from inherited access", path)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("read DACL for %s: %v", path, err)
	}
	if dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("%s DACL has %v ACEs; want one current-user ACE", path, dacl)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatalf("read access ACE for %s: %v", path, err)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		t.Fatalf("%s ACE type=%d, want allow ACE", path, ace.Header.AceType)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current user SID: %v", err)
	}
	aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !aceSID.Equals(user.User.Sid) {
		t.Fatalf("%s grants access to SID %s, want only current user %s", path, aceSID.String(), user.User.Sid.String())
	}
	if ace.Mask != 0x1F01FF {
		t.Fatalf("%s access mask=%#x, want file-all-access for current user", path, ace.Mask)
	}
	if ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
		t.Fatalf("%s ACE is inherited despite protected DACL", path)
	}
	if directory && ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE {
		t.Fatalf("%s current-user ACE flags=%#x, want protected child inheritance", path, ace.Header.AceFlags)
	}
}
