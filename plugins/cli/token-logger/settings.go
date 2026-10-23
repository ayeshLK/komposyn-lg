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

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const settingsLockTimeout = 2 * time.Minute
const settingsLockRetryInterval = 25 * time.Millisecond

// appSettings stores explicit local collector preferences. It contains no
// provider credentials.
type appSettings struct {
	Version             int    `json:"version"`
	OnboardingComplete  bool   `json:"onboarding_complete"`
	CollectorEnabled    bool   `json:"collector_enabled"`
	AutoStart           bool   `json:"auto_start"`
	DataDir             string `json:"data_dir"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	PollIntervalMillis  int64  `json:"poll_interval_millis"`
	FailureMode         string `json:"failure_mode"`
}

// DefaultSettings returns conservative first-run settings. Auto-start is
// deliberately disabled until separately approved by the user.
func defaultSettings(dataDir string) appSettings {
	return appSettings{Version: 1, DataDir: dataDir, PollIntervalSeconds: 5, PollIntervalMillis: 5000, FailureMode: "non-blocking"}
}

// LoadSettings loads settings or returns defaults when the file is absent.
func loadSettings(path, dataDir string) (appSettings, error) {
	settings := defaultSettings(dataDir)
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return appSettings{}, fmt.Errorf("read settings: %w", err)
	}
	// Leave the newer field unset while decoding an existing file so a legacy
	// poll_interval_seconds value is not shadowed by the current default.
	settings.PollIntervalMillis = 0
	if err := json.Unmarshal(contents, &settings); err != nil {
		return appSettings{}, fmt.Errorf("parse settings: %w", err)
	}
	if settings.Version == 0 {
		settings.Version = 1
	}
	if settings.DataDir == "" {
		settings.DataDir = dataDir
	}
	if settings.PollIntervalSeconds <= 0 {
		settings.PollIntervalSeconds = 5
	}
	if settings.PollIntervalMillis <= 0 {
		settings.PollIntervalMillis = int64(settings.PollIntervalSeconds) * 1000
	}
	if settings.FailureMode == "" {
		settings.FailureMode = "non-blocking"
	}
	return settings, nil
}

// SaveSettings writes settings with user-only permissions where supported.
func saveSettings(path string, settings appSettings) error {
	if settings.Version == 0 {
		settings.Version = 1
	}
	if settings.PollIntervalSeconds <= 0 {
		settings.PollIntervalSeconds = int((settings.PollIntervalMillis + 999) / 1000)
		if settings.PollIntervalSeconds <= 0 {
			settings.PollIntervalSeconds = 5
		}
	}
	if settings.PollIntervalMillis <= 0 {
		settings.PollIntervalMillis = int64(settings.PollIntervalSeconds) * 1000
	}
	if settings.FailureMode == "" {
		settings.FailureMode = "non-blocking"
	}
	contents, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary settings file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary settings file: %w", err)
	}
	if _, err := temporary.Write(append(contents, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write settings: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("flush settings: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary settings file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}

// withLocalFileLock serializes one local coordination operation across
// processes. Lock files remain in place so a waiting process and a later
// process cannot accidentally lock different inodes. The operating system
// releases the lock if its owner exits unexpectedly.
func withLocalFileLock(lockPath string, fn func() error) error {
	return withLocalFileLockTimeout(lockPath, settingsLockTimeout, fn)
}

func withLocalFileLockTimeout(lockPath string, timeout time.Duration, fn func() error) error {
	if fn == nil {
		return errors.New("file lock callback is nil")
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	if err := lockFile.Chmod(0o600); err != nil {
		_ = lockFile.Close()
		return fmt.Errorf("protect lock file: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		locked, lockErr := tryLockSettingsFile(lockFile)
		if lockErr != nil {
			_ = lockFile.Close()
			return fmt.Errorf("lock settings file: %w", lockErr)
		}
		if locked {
			break
		}
		if !time.Now().Before(deadline) {
			_ = lockFile.Close()
			return fmt.Errorf("timed out waiting for local file lock after %s", timeout)
		}
		time.Sleep(settingsLockRetryInterval)
	}

	var callbackErr, unlockErr, closeErr error
	func() {
		defer func() {
			unlockErr = unlockSettingsFile(lockFile)
			closeErr = lockFile.Close()
		}()
		callbackErr = fn()
	}()
	if callbackErr != nil {
		return callbackErr
	}
	if unlockErr != nil {
		return fmt.Errorf("unlock settings file: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close settings lock: %w", closeErr)
	}
	return nil
}

// withSettingsFileLock serializes settings read-modify-write operations across
// local processes. The lock file lives beside the settings file and is kept in
// place to ensure all processes use the same operating-system lock.
func withSettingsFileLock(settingsPath string, fn func() error) error {
	return withLocalFileLock(settingsPath+".lock", fn)
}

// updateSettings applies one settings mutation while holding the cross-process
// lock, and starts from the latest persisted value rather than a session's
// potentially stale startup snapshot.
func updateSettings(path, dataDir string, mutate func(*appSettings) error) (appSettings, error) {
	if mutate == nil {
		return appSettings{}, errors.New("settings mutation is nil")
	}
	var updated appSettings
	err := withSettingsFileLock(path, func() error {
		current, err := loadSettings(path, dataDir)
		if err != nil {
			return err
		}
		if err := mutate(&current); err != nil {
			return err
		}
		if err := saveSettings(path, current); err != nil {
			return err
		}
		updated = current
		return nil
	})
	return updated, err
}

// CollectorPollInterval returns the exact persisted polling duration while
// retaining compatibility with settings written before millisecond storage.
func (settings appSettings) collectorPollInterval() time.Duration {
	if settings.PollIntervalMillis > 0 {
		return time.Duration(settings.PollIntervalMillis) * time.Millisecond
	}
	if settings.PollIntervalSeconds > 0 {
		return time.Duration(settings.PollIntervalSeconds) * time.Second
	}
	return 5 * time.Second
}
