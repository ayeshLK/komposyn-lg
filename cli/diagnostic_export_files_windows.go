// Copyright 2026 Ayesh Almeida
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func openPrivateDiagnosticExportFile(path string) (*os.File, error) {
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(path), windows.GENERIC_WRITE|windows.WRITE_DAC,
		0, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, errors.New("create private export")
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		file := os.NewFile(uintptr(handle), path)
		created, _ := file.Stat()
		_ = file.Close()
		_ = removeCreatedDiagnosticExportFile(path, created)
		return nil, errors.New("unsafe export destination")
	}
	file := os.NewFile(uintptr(handle), path)
	if err := secureLogFile(file); err != nil {
		created, _ := file.Stat()
		_ = file.Close()
		_ = removeCreatedDiagnosticExportFile(path, created)
		return nil, errors.New("protect export destination")
	}
	return file, nil
}
