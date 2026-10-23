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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const settingsLockHelperEnv = "KOMPOSYN_SETTINGS_LOCK_HELPER"

func TestSettingsLockProcessHelper(t *testing.T) {
	if os.Getenv(settingsLockHelperEnv) != "1" {
		return
	}
	path := os.Getenv("KOMPOSYN_SETTINGS_PATH")
	dataDir := os.Getenv("KOMPOSYN_SETTINGS_DATA_DIR")
	field := os.Getenv("KOMPOSYN_SETTINGS_FIELD")
	readyPath := os.Getenv("KOMPOSYN_SETTINGS_READY")
	gatePath := os.Getenv("KOMPOSYN_SETTINGS_GATE")
	attemptPath := os.Getenv("KOMPOSYN_SETTINGS_ATTEMPT")
	heldPath := os.Getenv("KOMPOSYN_SETTINGS_HELD")
	releasePath := os.Getenv("KOMPOSYN_SETTINGS_RELEASE")
	if path == "" || dataDir == "" || readyPath == "" || gatePath == "" || attemptPath == "" || heldPath == "" || releasePath == "" {
		t.Fatal("settings helper process received incomplete fixture paths")
	}
	if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, gatePath, 5*time.Second)
	if err := os.WriteFile(attemptPath, []byte("attempting"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := updateSettings(path, dataDir, func(settings *appSettings) error {
		// Hold the first process inside the locked mutation until the parent has
		// confirmed that the other process attempted its update.
		if err := os.WriteFile(heldPath, []byte("locked"), 0o600); err != nil {
			return err
		}
		waitForFile(t, releasePath, 5*time.Second)
		switch field {
		case "auto-start":
			settings.AutoStart = true
		case "poll-interval":
			settings.PollIntervalMillis = 11000
			settings.PollIntervalSeconds = 11
		default:
			return fmt.Errorf("unknown test field %q", field)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentSettingsUpdatesAcrossProcesses(t *testing.T) {
	dataDir := t.TempDir()
	settingsPath := filepath.Join(dataDir, "settings.json")
	if err := saveSettings(settingsPath, defaultSettings(dataDir)); err != nil {
		t.Fatal(err)
	}
	gatePath := filepath.Join(dataDir, "start")
	releasePath := filepath.Join(dataDir, "release")
	fields := []string{"auto-start", "poll-interval"}
	commands := make([]*exec.Cmd, 0, len(fields))
	outputs := make([]*bytes.Buffer, 0, len(fields))
	for i, field := range fields {
		readyPath := filepath.Join(dataDir, fmt.Sprintf("ready-%d", i))
		attemptPath := filepath.Join(dataDir, field+".attempt")
		heldPath := filepath.Join(dataDir, field+".held")
		output := new(bytes.Buffer)
		cmd := exec.Command(os.Args[0], "-test.run=^TestSettingsLockProcessHelper$")
		cmd.Env = append(os.Environ(),
			settingsLockHelperEnv+"=1",
			"KOMPOSYN_SETTINGS_PATH="+settingsPath,
			"KOMPOSYN_SETTINGS_DATA_DIR="+dataDir,
			"KOMPOSYN_SETTINGS_FIELD="+field,
			"KOMPOSYN_SETTINGS_READY="+readyPath,
			"KOMPOSYN_SETTINGS_GATE="+gatePath,
			"KOMPOSYN_SETTINGS_ATTEMPT="+attemptPath,
			"KOMPOSYN_SETTINGS_HELD="+heldPath,
			"KOMPOSYN_SETTINGS_RELEASE="+releasePath,
		)
		cmd.Stdout, cmd.Stderr = output, output
		if err := cmd.Start(); err != nil {
			for _, started := range commands {
				_ = started.Process.Kill()
			}
			t.Fatalf("start helper %s: %v", field, err)
		}
		commands = append(commands, cmd)
		outputs = append(outputs, output)
	}
	finished := make([]chan error, len(commands))
	for i, cmd := range commands {
		finished[i] = make(chan error, 1)
		go func(done chan<- error, cmd *exec.Cmd) { done <- cmd.Wait() }(finished[i], cmd)
	}
	defer func() {
		for _, cmd := range commands {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
			}
		}
	}()
	for i := range fields {
		waitForFile(t, filepath.Join(dataDir, fmt.Sprintf("ready-%d", i)), 5*time.Second)
	}
	if err := os.WriteFile(gatePath, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		waitForFile(t, filepath.Join(dataDir, field+".attempt"), 5*time.Second)
	}
	heldField := waitForOneFile(t, []string{
		filepath.Join(dataDir, "auto-start.held"),
		filepath.Join(dataDir, "poll-interval.held"),
	}, 5*time.Second)
	otherField := "auto-start"
	if heldField == "auto-start" {
		otherField = "poll-interval"
	}
	if _, err := os.Stat(filepath.Join(dataDir, otherField+".held")); err == nil {
		t.Fatalf("both settings mutations entered their critical sections concurrently")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, done := range finished {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("helper %s failed: %v\n%s", fields[i], err, outputs[i].String())
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("helper %s did not exit within timeout", fields[i])
		}
	}
	got, err := loadSettings(settingsPath, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoStart || got.PollIntervalMillis != 11000 || got.PollIntervalSeconds != 11 {
		t.Fatalf("independent updates lost: %+v", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(settingsPath + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("settings lock mode = %o, want 600", mode)
		}
	}
}

func waitForOneFile(t *testing.T, paths []string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				return strings.TrimSuffix(filepath.Base(path), ".held")
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for one of %v", paths)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConcurrentInteractiveOnboardingUsesLatestSettingsOnce(t *testing.T) {
	dataDir := t.TempDir()
	settingsPath := filepath.Join(dataDir, "settings.json")
	initial := defaultSettings(dataDir) // Both windows captured the same first-run snapshot.
	firstReader := &blockingAnswerReader{
		answers: []string{"y", "n"},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	firstDone := make(chan onboardingResult, 1)
	go func() {
		settings, startNow, cancelled, err := coordinateInteractiveOnboarding(firstReader, settingsPath, dataDir, initial, false)
		firstDone <- onboardingResult{settings: settings, startNow: startNow, cancelled: cancelled, err: err}
	}()
	waitChannel(t, firstReader.entered, "first onboarding prompt")

	secondReader := &countingAnswerReader{answer: "y"}
	secondStarted := make(chan struct{})
	secondDone := make(chan onboardingResult, 1)
	go func() {
		close(secondStarted)
		settings, startNow, cancelled, err := coordinateInteractiveOnboarding(secondReader, settingsPath, dataDir, initial, false)
		secondDone <- onboardingResult{settings: settings, startNow: startNow, cancelled: cancelled, err: err}
	}()
	waitChannel(t, secondStarted, "second onboarding process start")
	firstReader.releasePrompt()

	first := receiveOnboardingResult(t, firstDone, "first onboarding")
	second := receiveOnboardingResult(t, secondDone, "second onboarding")
	if first.err != nil || first.cancelled || !first.startNow {
		t.Fatalf("first onboarding result = %+v", first)
	}
	if firstReader.prompts.Load() != 2 {
		t.Fatalf("first reader received %d prompts, want both explicit onboarding questions", firstReader.prompts.Load())
	}
	if second.err != nil || second.cancelled || second.startNow {
		t.Fatalf("second onboarding result = %+v; it should reload the completed settings without starting a collector", second)
	}
	if secondReader.prompts.Load() != 0 {
		t.Fatalf("second reader received %d prompts after another REPL completed onboarding", secondReader.prompts.Load())
	}
	if !second.settings.OnboardingComplete || !second.settings.CollectorEnabled || second.settings.AutoStart {
		t.Fatalf("second REPL did not receive persisted explicit choices: %+v", second.settings)
	}
	persisted, err := loadSettings(settingsPath, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if persisted != second.settings {
		t.Fatalf("persisted settings = %+v; second REPL snapshot = %+v", persisted, second.settings)
	}
}

func TestConcurrentDiagnosticReviewRereadsRealPendingState(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "token-logger.db")
	firstStore, err := tokenlogger.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer firstStore.Close()
	secondStore, err := tokenlogger.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	diagnostic, err := firstStore.RecordDiagnostic("synthetic review prompt")
	if err != nil {
		t.Fatal(err)
	}

	firstReader := &blockingAnswerReader{
		answers: []string{"n"},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	firstOutput := new(bytes.Buffer)
	firstDone := make(chan reviewResult, 1)
	go func() {
		leave, err := reviewPendingDiagnostics(firstReader, firstStore, dataDir, firstOutput)
		firstDone <- reviewResult{leave: leave, err: err}
	}()
	waitChannel(t, firstReader.entered, "first diagnostic consent prompt")

	secondReader := &countingAnswerReader{answer: "y"}
	secondOutput := new(bytes.Buffer)
	secondStarted := make(chan struct{})
	secondDone := make(chan reviewResult, 1)
	go func() {
		close(secondStarted)
		leave, err := reviewPendingDiagnostics(secondReader, secondStore, dataDir, secondOutput)
		secondDone <- reviewResult{leave: leave, err: err}
	}()
	waitChannel(t, secondStarted, "second diagnostic review process start")
	firstReader.releasePrompt()

	first := receiveReviewResult(t, firstDone, "first diagnostic review")
	second := receiveReviewResult(t, secondDone, "second diagnostic review")
	if first.err != nil || first.leave || second.err != nil || second.leave {
		t.Fatalf("review results: first=%+v second=%+v", first, second)
	}
	if firstReader.prompts.Load() != 1 || secondReader.prompts.Load() != 0 {
		t.Fatalf("diagnostic prompts: first=%d second=%d; want one explicit prompt total", firstReader.prompts.Load(), secondReader.prompts.Load())
	}
	stored, err := secondStore.Diagnostic(diagnostic.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "skipped" {
		t.Fatalf("diagnostic status = %q, want explicit skipped choice", stored.Status)
	}
	pending, err := secondStore.PendingDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending diagnostics after review = %d, want 0", len(pending))
	}
}

type onboardingResult struct {
	settings  appSettings
	startNow  bool
	cancelled bool
	err       error
}

type reviewResult struct {
	leave bool
	err   error
}

type blockingAnswerReader struct {
	answers []string
	entered chan struct{}
	release chan struct{}
	prompts atomic.Int32
	once    sync.Once
}

func (reader *blockingAnswerReader) ReadLine(string) (string, error) {
	index := int(reader.prompts.Add(1)) - 1
	if index == 0 {
		close(reader.entered)
		<-reader.release
	}
	if index >= len(reader.answers) {
		return "", fmt.Errorf("unexpected prompt %d", index+1)
	}
	return reader.answers[index], nil
}
func (*blockingAnswerReader) DisableHistory()  {}
func (*blockingAnswerReader) EnableHistory()   {}
func (*blockingAnswerReader) IsTerminal() bool { return true }
func (reader *blockingAnswerReader) releasePrompt() {
	reader.once.Do(func() { close(reader.release) })
}

type countingAnswerReader struct {
	answer  string
	prompts atomic.Int32
}

func (reader *countingAnswerReader) ReadLine(string) (string, error) {
	reader.prompts.Add(1)
	return reader.answer, nil
}
func (*countingAnswerReader) DisableHistory()  {}
func (*countingAnswerReader) EnableHistory()   {}
func (*countingAnswerReader) IsTerminal() bool { return true }

func waitChannel(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func receiveOnboardingResult(t *testing.T, ch <-chan onboardingResult, description string) onboardingResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return onboardingResult{}
	}
}

func receiveReviewResult(t *testing.T, ch <-chan reviewResult, description string) reviewResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return reviewResult{}
	}
}

func TestSettingsStartupSnapshotRemainsIndependentFromPersistedUpdates(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "settings.json")
	initial := defaultSettings(dataDir)
	initial.OnboardingComplete = true
	if err := saveSettings(path, initial); err != nil {
		t.Fatal(err)
	}
	sessionSettings, err := loadSettings(path, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateSettings(path, dataDir, func(settings *appSettings) error {
		settings.AutoStart = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if sessionSettings.AutoStart {
		t.Fatal("persisted update mutated an existing REPL settings snapshot")
	}
	latest, err := loadSettings(path, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !latest.AutoStart {
		t.Fatal("new process settings read did not observe persisted value")
	}
}

func TestLocalFileLockHonorsWaitTimeout(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "held.lock")
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- withLocalFileLock(lockPath, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first process did not acquire local lock")
	}
	start := time.Now()
	err := withLocalFileLockTimeout(lockPath, 75*time.Millisecond, func() error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "timed out waiting") {
		t.Fatalf("lock wait error = %v, want timeout", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("lock timeout took too long: %s", time.Since(start))
	}
	close(release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lock owner did not finish after release")
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for file %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
