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

//go:build !windows && !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package main

import (
	"errors"
	"os"
)

func secureLogDirectory(string) error {
	return errors.New("private log files are unsupported on this platform")
}
func inspectLogDirectory(string) (os.FileInfo, error) {
	return nil, errors.New("private log files are unsupported on this platform")
}
func inspectLogTarget(string) (bool, int64, error) {
	return false, 0, errors.New("private log files are unsupported on this platform")
}
func openPrivateLogFile(string, int, os.FileMode) (*os.File, error) {
	return nil, errors.New("private log files are unsupported on this platform")
}
func openReadOnlyLogFile(string) (*os.File, error) {
	return nil, errors.New("safe log inspection is unsupported on this platform")
}
func renameLogFile(string, string) error {
	return errors.New("private log files are unsupported on this platform")
}
func removeLogFile(string) error {
	return errors.New("private log files are unsupported on this platform")
}
