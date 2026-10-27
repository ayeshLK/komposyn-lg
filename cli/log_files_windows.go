// Copyright 2026 Ayesh Almeida
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func secureLogDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("create log directory")
	}
	if _, err := inspectLogDirectory(path); err != nil {
		return err
	}
	dacl, err := privateLogACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		return errors.New("protect log directory")
	}
	return nil
}

func inspectLogDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return nil, errors.New("unsafe log directory")
	}
	attrs, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return nil, errors.New("unsafe log directory")
	}
	return info, nil
}

func inspectLogTarget(path string) (bool, int64, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, errors.New("inspect log file")
	}
	attrs, attrErr := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	if attrErr != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || !info.Mode().IsRegular() {
		return false, 0, errors.New("unsafe log file target")
	}
	return true, info.Size(), nil
}

func openPrivateLogFile(path string, flags int, _ os.FileMode) (*os.File, error) {
	access := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE | windows.WRITE_DAC)
	creation := uint32(windows.OPEN_EXISTING)
	if flags&os.O_CREATE != 0 {
		creation = windows.OPEN_ALWAYS
	}
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(path), access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, creation,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, errors.New("open safe log file")
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("unsafe log file")
	}
	file := os.NewFile(uintptr(handle), path)
	if err := secureLogFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	if flags&os.O_APPEND != 0 {
		if _, err := file.Seek(0, 2); err != nil {
			_ = file.Close()
			return nil, errors.New("seek log file")
		}
	}
	return file, nil
}

// openReadOnlyLogFile allows concurrent append and rename while protecting the
// path itself from reparse-point traversal. It never changes the file ACL.
func openReadOnlyLogFile(path string) (*os.File, error) {
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(path), windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("unsafe log file")
	}
	file := os.NewFile(uintptr(handle), path)
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("unsafe log file")
	}
	return file, nil
}

func privateLogACL() (*windows.ACL, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, errors.New("resolve current user for log ACL")
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, errors.New("create private log ACL")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return nil, errors.New("read private log ACL")
	}
	return dacl, nil
}

func secureLogFile(file *os.File) error {
	dacl, err := privateLogACL()
	if err != nil {
		return err
	}
	if err := windows.SetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		return errors.New("protect log file")
	}
	return nil
}

func renameLogFile(source, target string) error {
	if exists, _, err := inspectLogTarget(source); err != nil {
		return err
	} else if !exists {
		return errors.New("missing log rotation source")
	}
	if exists, _, err := inspectLogTarget(target); err != nil {
		return err
	} else if exists {
		return errors.New("log rotation target already exists")
	}
	if err := os.Rename(source, target); err != nil {
		return errors.New("rotate log file")
	}
	return nil
}

func removeLogFile(path string) error {
	if exists, _, err := inspectLogTarget(path); err != nil {
		return err
	} else if !exists {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return errors.New("remove oldest log archive")
	}
	return nil
}
