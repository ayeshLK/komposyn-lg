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

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func secureLogDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("create log directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe log directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return errors.New("protect log directory")
	}
	return nil
}

func inspectLogDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
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
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, 0, errors.New("unsafe log file target")
	}
	return true, info.Size(), nil
}

func openPrivateLogFile(path string, flags int, mode os.FileMode) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode.Perm()))
	if err != nil {
		return nil, errors.New("open safe log file")
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("unsafe log file")
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		_ = file.Close()
		return nil, errors.New("protect log file")
	}
	if flags&os.O_APPEND != 0 {
		if _, err := file.Seek(0, 2); err != nil {
			_ = file.Close()
			return nil, errors.New("seek log file")
		}
	}
	return file, nil
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
