// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const (
	issue48ChildEnv       = "KOMPOSYN_ISSUE48_REPL_CHILD"
	issue48ChildDataEnv   = "KOMPOSYN_ISSUE48_DATA_DIR"
	issue48ChildRootsEnv  = "KOMPOSYN_ISSUE48_ROOTS"
	issue48ChildOpenEnv   = "KOMPOSYN_ISSUE48_OPEN_GATE"
	issue48ChildStartEnv  = "KOMPOSYN_ISSUE48_START_GATE"
	issue48ChildNowEnv    = "KOMPOSYN_ISSUE48_START_NOW"
	issue48DetachedEnv    = "KOMPOSYN_ISSUE48_DETACHED_HELPER"
	issue48DetachedDirEnv = "KOMPOSYN_ISSUE48_DETACHED_DATA_DIR"
	issue48DetachedIDEnv  = "KOMPOSYN_ISSUE48_DETACHED_INSTANCE"
	issue48DetachedMark   = "KOMPOSYN_ISSUE48_DETACHED_CLOSED_MARKER"
)

// TestIssue48REPLChild re-enters the real REPL loop in an independently
// scheduled process. Gates let the parent force simultaneous Store.Open and
// collector acquisition without arbitrary startup sleeps.
func TestIssue48REPLChild(t *testing.T) {
	if os.Getenv(issue48ChildEnv) != "1" {
		return
	}
	dataDir := os.Getenv(issue48ChildDataEnv)
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "issue #48 child has no data directory")
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, "ISSUE48_CHILD_READY_TO_OPEN")
	if err := issue48WaitForGate(os.Getenv(issue48ChildOpenEnv)); err != nil {
		fmt.Fprintf(os.Stderr, "wait for database-open gate: %v\n", err)
		os.Exit(2)
	}
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "open child database: %v\n", err)
		os.Exit(2)
	}
	settings, err := loadSettings(filepath.Join(dataDir, "settings.json"), dataDir)
	if err != nil {
		_ = store.Close()
		fmt.Fprintf(os.Stderr, "load child settings: %v\n", err)
		os.Exit(2)
	}
	if !settings.OnboardingComplete {
		_ = store.Close()
		fmt.Fprintln(os.Stderr, "test child requires completed onboarding settings")
		os.Exit(2)
	}
	var roots []tokenlogger.CollectorRoot
	if encoded := os.Getenv(issue48ChildRootsEnv); encoded != "" {
		if err := json.Unmarshal([]byte(encoded), &roots); err != nil {
			_ = store.Close()
			fmt.Fprintf(os.Stderr, "decode synthetic roots: %v\n", err)
			os.Exit(2)
		}
	}
	fmt.Fprintln(os.Stdout, "ISSUE48_CHILD_READY_TO_START")
	if err := issue48WaitForGate(os.Getenv(issue48ChildStartEnv)); err != nil {
		_ = store.Close()
		fmt.Fprintf(os.Stderr, "wait for REPL-start gate: %v\n", err)
		os.Exit(2)
	}
	reader := &scannerLineReader{scanner: bufio.NewScanner(os.Stdin), output: os.Stdout}
	code := runInteractiveLoopWithRoots(reader, os.Stdout, os.Stderr, store, dataDir, settings, os.Getenv(issue48ChildNowEnv) == "1", roots)
	if err := store.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close child database: %v\n", err)
		if code == 0 {
			code = 2
		}
	}
	os.Exit(code)
}

// TestIssue48DetachedWorker re-enters the collector worker as a detached
// process. It is stopped cooperatively by the owning test through its instance
// lease; the test never signals a persisted PID.
func TestIssue48DetachedWorker(t *testing.T) {
	if os.Getenv(issue48DetachedEnv) != "1" {
		return
	}
	dataDir := os.Getenv(issue48DetachedDirEnv)
	instance := os.Getenv(issue48DetachedIDEnv)
	marker := os.Getenv(issue48DetachedMark)
	if dataDir == "" || instance == "" || marker == "" {
		os.Exit(2)
	}
	code := runWatchServe([]string{"--data-dir", dataDir, "--interval", "20ms", "--instance", instance}, io.Discard, context.Background())
	if err := os.WriteFile(marker, []byte("closed"), 0o600); err != nil {
		os.Exit(3)
	}
	os.Exit(code)
}

type issue48LineLog struct {
	mu      sync.Mutex
	lines   []string
	changed chan struct{}
}

func issue48NewLineLog() *issue48LineLog {
	return &issue48LineLog{changed: make(chan struct{}, 1)}
}

func (log *issue48LineLog) add(line string) {
	log.mu.Lock()
	log.lines = append(log.lines, line)
	log.mu.Unlock()
	select {
	case log.changed <- struct{}{}:
	default:
	}
}

func (log *issue48LineLog) text() string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return strings.Join(log.lines, "\n")
}

func (log *issue48LineLog) waitFor(t *testing.T, child *issue48REPLProcess, timeout time.Duration, contains string) string {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if output := log.text(); strings.Contains(output, contains) {
			return output
		}
		select {
		case <-log.changed:
		case <-child.done:
			if output := log.text(); strings.Contains(output, contains) {
				return output
			}
			t.Fatalf("REPL process exited before output %q; stdout=%q stderr=%q err=%v", contains, child.stdout.text(), child.stderr.text(), child.exitErr())
		case <-deadline.C:
			t.Fatalf("timed out waiting for %q; stdout=%q stderr=%q", contains, child.stdout.text(), child.stderr.text())
		}
	}
}

type issue48REPLProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *issue48LineLog
	stderr *issue48LineLog
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func (child *issue48REPLProcess) exitErr() error {
	child.mu.Lock()
	defer child.mu.Unlock()
	return child.err
}

func (child *issue48REPLProcess) send(line string) error {
	_, err := io.WriteString(child.stdin, line+"\n")
	return err
}

func (child *issue48REPLProcess) wait(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-child.done:
		if err := child.exitErr(); err != nil {
			t.Fatalf("REPL child exited unsuccessfully: %v; stdout=%q stderr=%q", err, child.stdout.text(), child.stderr.text())
		}
	case <-time.After(timeout):
		t.Fatalf("REPL child did not exit within %s; stdout=%q stderr=%q", timeout, child.stdout.text(), child.stderr.text())
	}
}

func (child *issue48REPLProcess) quit(t *testing.T) {
	t.Helper()
	select {
	case <-child.done:
		return
	default:
	}
	if err := child.send("/quit"); err != nil {
		// The process may already be completing after EOF; Wait below decides.
	}
	select {
	case <-child.done:
		if err := child.exitErr(); err != nil {
			t.Errorf("REPL child exit: %v; stdout=%q stderr=%q", err, child.stdout.text(), child.stderr.text())
		}
	case <-time.After(5 * time.Second):
		// This is the tracked child launched by this test, never a PID read from
		// collector metadata.
		_ = child.cmd.Process.Kill()
		<-child.done
		t.Errorf("tracked REPL child did not quit cooperatively; stdout=%q stderr=%q", child.stdout.text(), child.stderr.text())
	}
}

func issue48StartREPL(t *testing.T, dataDir, home string, roots []tokenlogger.CollectorRoot, openGate, startGate string) *issue48REPLProcess {
	t.Helper()
	return issue48StartREPLMode(t, dataDir, home, roots, openGate, startGate, false)
}

func issue48StartREPLMode(t *testing.T, dataDir, home string, roots []tokenlogger.CollectorRoot, openGate, startGate string, startNow bool) *issue48REPLProcess {
	t.Helper()
	encoded, err := json.Marshal(roots)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestIssue48REPLChild$")
	startNowValue := "0"
	if startNow {
		startNowValue = "1"
	}
	cmd.Env = append(os.Environ(),
		issue48ChildEnv+"=1",
		issue48ChildDataEnv+"="+dataDir,
		issue48ChildRootsEnv+"="+string(encoded),
		issue48ChildOpenEnv+"="+openGate,
		issue48ChildStartEnv+"="+startGate,
		issue48ChildNowEnv+"="+startNowValue,
		"TOKEN_LOGGER_DATA_DIR="+dataDir,
		"HOME="+home,
		"USERPROFILE="+home,
		"TOKEN_LOGGER_CODEX_DIR=",
		"TOKEN_LOGGER_CLAUDE_DIR=",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	child := &issue48REPLProcess{cmd: cmd, stdin: stdin, stdout: issue48NewLineLog(), stderr: issue48NewLineLog(), done: make(chan struct{})}
	go issue48ScanLines(stdout, child.stdout)
	go issue48ScanLines(stderr, child.stderr)
	go func() {
		err := cmd.Wait()
		child.mu.Lock()
		child.err = err
		child.mu.Unlock()
		close(child.done)
	}()
	t.Cleanup(func() { child.quit(t) })
	return child
}

func issue48ScanLines(reader io.Reader, log *issue48LineLog) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		log.add(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		log.add("scanner error: " + err.Error())
	}
}

func issue48WaitForGate(path string) error {
	if path == "" {
		return nil
	}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for test gate %q", filepath.Base(path))
		}
	}
}

func issue48ReleaseGate(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func issue48Settings(t *testing.T, dataDir string, collectorEnabled, autoStart bool) {
	t.Helper()
	settings := defaultSettings(dataDir)
	settings.OnboardingComplete = true
	settings.CollectorEnabled = collectorEnabled
	settings.AutoStart = autoStart
	settings.PollIntervalMillis = 250
	settings.PollIntervalSeconds = 1
	if err := saveSettings(filepath.Join(dataDir, "settings.json"), settings); err != nil {
		t.Fatal(err)
	}
}

func issue48SyntheticRoot(t *testing.T) (string, tokenlogger.CollectorRoot) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "codex.jsonl")
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "providers", "codex", "normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, tokenlogger.CollectorRoot{Path: root, Required: true}
}

func issue48OpenStore(t *testing.T, dataDir string) *tokenlogger.Store {
	t.Helper()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close issue #48 test store: %v", err)
		}
	})
	return store
}

func issue48WaitStatus(t *testing.T, store *tokenlogger.Store, timeout time.Duration, predicate func(tokenlogger.CollectorRuntimeStatus) bool) tokenlogger.CollectorRuntimeStatus {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var last tokenlogger.CollectorRuntimeStatus
	for {
		status, err := store.CollectorRuntimeStatus(0)
		if err == nil {
			last = status
			if predicate(status) {
				return status
			}
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("timed out waiting for collector status; last=%+v", last)
		}
	}
}

func issue48SeedSyntheticUsage(t *testing.T, dataDir, root string) {
	t.Helper()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	collector, err := tokenlogger.NewCollectorWithRoots(store, []tokenlogger.CollectorRoot{{Path: root, Required: true}}, time.Minute)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := collector.PollOnce(); err != nil {
		_ = store.Close()
		t.Fatalf("import synthetic fixture: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIssue48SimultaneousREPLAutoStartHasOneOwner(t *testing.T) {
	dataDir, home := t.TempDir(), t.TempDir()
	issue48Settings(t, dataDir, true, true)
	_, root := issue48SyntheticRoot(t)
	openGate, startGate := filepath.Join(dataDir, "open.gate"), filepath.Join(dataDir, "start.gate")
	first := issue48StartREPL(t, dataDir, home, []tokenlogger.CollectorRoot{root}, openGate, startGate)
	second := issue48StartREPL(t, dataDir, home, []tokenlogger.CollectorRoot{root}, openGate, startGate)
	first.stdout.waitFor(t, first, 10*time.Second, "ISSUE48_CHILD_READY_TO_OPEN")
	second.stdout.waitFor(t, second, 10*time.Second, "ISSUE48_CHILD_READY_TO_OPEN")
	issue48ReleaseGate(t, openGate)
	first.stdout.waitFor(t, first, 10*time.Second, "ISSUE48_CHILD_READY_TO_START")
	second.stdout.waitFor(t, second, 10*time.Second, "ISSUE48_CHILD_READY_TO_START")
	issue48ReleaseGate(t, startGate)
	first.stdout.waitFor(t, first, 10*time.Second, "komposyn-lg interactive")
	second.stdout.waitFor(t, second, 10*time.Second, "komposyn-lg interactive")
	if err := first.send("/status"); err != nil {
		t.Fatal(err)
	}
	if err := second.send("/status"); err != nil {
		t.Fatal(err)
	}
	first.stdout.waitFor(t, first, 10*time.Second, "collector lifecycle: state=running")
	second.stdout.waitFor(t, second, 10*time.Second, "collector lifecycle: state=running")
	firstStatus, secondStatus := issue48OpenStore(t, dataDir), issue48OpenStore(t, dataDir)
	active := issue48WaitStatus(t, firstStatus, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "running" })
	if active.Mode != "embedded" || (active.PID != first.cmd.Process.Pid && active.PID != second.cmd.Process.Pid) {
		t.Fatalf("one of the tracked REPL processes should own the embedded lease: status=%+v pids=(%d,%d)", active, first.cmd.Process.Pid, second.cmd.Process.Pid)
	}
	firstText, secondText := first.stdout.text(), second.stdout.text()
	owners := 0
	borrowers := 0
	for _, output := range []string{firstText, secondText} {
		if strings.Contains(output, "collector owned by this REPL session") {
			owners++
		}
		if strings.Contains(output, "embedded collector owned by another process") {
			borrowers++
		}
	}
	if owners != 1 || borrowers != 1 {
		t.Fatalf("simultaneous REPL startup should produce one owner and one borrower; owner=%d borrower=%d\nfirst=%s\nsecond=%s", owners, borrowers, firstText, secondText)
	}

	owner, borrower := first, second
	if active.PID == second.cmd.Process.Pid {
		owner, borrower = second, first
	}
	instance := active.InstanceID
	issue48WaitStatus(t, firstStatus, 10*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool {
		return status.State == "running" && status.LastCompletedAt != nil && status.Imported == 2
	})
	if err := borrower.send("/range 2026-10-01 2026-10-15"); err != nil {
		t.Fatal(err)
	}
	borrower.stdout.waitFor(t, borrower, 10*time.Second, "Range: 2026-10-01 to 2026-10-15")
	appendFile, err := os.OpenFile(filepath.Join(root.Path, "codex.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendFile.WriteString("\n" + lifecycleAppendRecord + "\n"); err != nil {
		_ = appendFile.Close()
		t.Fatalf("append synthetic usage record: %v", err)
	}
	if err := appendFile.Close(); err != nil {
		t.Fatal(err)
	}
	issue48WaitStatus(t, firstStatus, 10*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool {
		count, countErr := firstStatus.EventCount()
		return countErr == nil && count == 3 && status.Imported == 3
	})
	if err := borrower.send("/refresh"); err != nil {
		t.Fatal(err)
	}
	borrower.stdout.waitFor(t, borrower, 10*time.Second, "260")
	if count, countErr := firstStatus.EventCount(); countErr != nil || count != 3 {
		t.Fatalf("shared committed event count=%d err=%v, want 3", count, countErr)
	}
	if err := borrower.send("/quit"); err != nil {
		t.Fatal(err)
	}
	borrower.wait(t, 5*time.Second)
	status := issue48WaitStatus(t, firstStatus, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "running" })
	if status.InstanceID != instance || status.PID != owner.cmd.Process.Pid {
		t.Fatalf("closing borrower changed owner lease: before=%+v after=%+v", active, status)
	}

	// A fresh borrower remains open while the owner exits; it must stay usable
	// and observe the stopped state without silently taking ownership.
	third := issue48StartREPL(t, dataDir, home, []tokenlogger.CollectorRoot{root}, "", "")
	third.stdout.waitFor(t, third, 10*time.Second, "komposyn-lg interactive")
	if err := third.send("/status"); err != nil {
		t.Fatal(err)
	}
	third.stdout.waitFor(t, third, 10*time.Second, "collector lifecycle: state=running")
	if err := owner.send("/quit"); err != nil {
		t.Fatal(err)
	}
	owner.wait(t, 5*time.Second)
	stopped := issue48WaitStatus(t, firstStatus, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "stopped" })
	if stopped.InstanceID != instance {
		t.Fatalf("owner exit changed lease identity: got %+v, previous instance %q", stopped, instance)
	}
	if err := third.send("/status"); err != nil {
		t.Fatal(err)
	}
	third.stdout.waitFor(t, third, 10*time.Second, "collector lifecycle: state=stopped")
	if err := third.send("/refresh"); err != nil {
		t.Fatal(err)
	}
	third.stdout.waitFor(t, third, 10*time.Second, "Range:")
	stillStopped := issue48WaitStatus(t, secondStatus, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "stopped" })
	if stillStopped.InstanceID != instance {
		t.Fatalf("borrower silently took over after owner exit: %+v", stillStopped)
	}
	if err := third.send("/quit"); err != nil {
		t.Fatal(err)
	}
	third.wait(t, 5*time.Second)
}

func TestIssue48REPLProcessesKeepRangesAndNavigationIndependent(t *testing.T) {
	dataDir, home := t.TempDir(), t.TempDir()
	issue48Settings(t, dataDir, false, false)
	root, _ := issue48SyntheticRoot(t)
	issue48SeedSyntheticUsage(t, dataDir, root)
	first := issue48StartREPL(t, dataDir, home, nil, "", "")
	second := issue48StartREPL(t, dataDir, home, nil, "", "")
	first.stdout.waitFor(t, first, 10*time.Second, "komposyn-lg interactive")
	second.stdout.waitFor(t, second, 10*time.Second, "komposyn-lg interactive")

	if err := first.send("/range 2026-10-01 2026-10-15"); err != nil {
		t.Fatal(err)
	}
	first.stdout.waitFor(t, first, 10*time.Second, "Range: 2026-10-01 to 2026-10-15")
	if err := second.send("/range 2026-10-20 2026-10-22"); err != nil {
		t.Fatal(err)
	}
	second.stdout.waitFor(t, second, 10*time.Second, "Range: 2026-10-20 to 2026-10-22")

	if err := first.send("/projects"); err != nil {
		t.Fatal(err)
	}
	first.stdout.waitFor(t, first, 10*time.Second, "Sanitized Fixture Project")
	if err := first.send("/project 1"); err != nil {
		t.Fatal(err)
	}
	first.stdout.waitFor(t, first, 10*time.Second, "PROJECT: Sanitized Fixture Project")
	if err := second.send("/status"); err != nil {
		t.Fatal(err)
	}
	second.stdout.waitFor(t, second, 10*time.Second, "2026-10-20T00:00:00Z to 2026-10-22T00:00:00Z")
	secondOutput := second.stdout.text()
	if strings.Contains(secondOutput, "scope: komposyn / project:Sanitized Fixture Project") {
		t.Fatalf("navigation in first REPL leaked to second REPL: %s", secondOutput)
	}
	if !strings.Contains(secondOutput, "2026-10-20T00:00:00Z to 2026-10-22T00:00:00Z") {
		t.Fatalf("second REPL lost its own pinned range: %s", secondOutput)
	}
	if err := first.send("/status"); err != nil {
		t.Fatal(err)
	}
	first.stdout.waitFor(t, first, 10*time.Second, "2026-10-01T00:00:00Z to 2026-10-15T00:00:00Z")
	firstOutput := first.stdout.text()
	if !strings.Contains(firstOutput, "2026-10-01T00:00:00Z to 2026-10-15T00:00:00Z") {
		t.Fatalf("first REPL lost its own pinned range: %s", firstOutput)
	}
	if strings.Contains(firstOutput, "2026-10-20T00:00:00Z to 2026-10-22T00:00:00Z") {
		t.Fatalf("second REPL range leaked into first REPL: %s", firstOutput)
	}

	if err := first.send("/quit"); err != nil {
		t.Fatal(err)
	}
	if err := second.send("/quit"); err != nil {
		t.Fatal(err)
	}
	first.wait(t, 5*time.Second)
	second.wait(t, 5*time.Second)
}

func TestIssue48SeparateDataDirectoriesHaveIndependentCollectorLeases(t *testing.T) {
	home := t.TempDir()
	firstDir, secondDir := t.TempDir(), t.TempDir()
	issue48Settings(t, firstDir, true, true)
	issue48Settings(t, secondDir, true, true)
	_, root := issue48SyntheticRoot(t)
	first := issue48StartREPL(t, firstDir, home, []tokenlogger.CollectorRoot{root}, "", "")
	second := issue48StartREPL(t, secondDir, home, []tokenlogger.CollectorRoot{root}, "", "")
	first.stdout.waitFor(t, first, 10*time.Second, "komposyn-lg interactive")
	second.stdout.waitFor(t, second, 10*time.Second, "komposyn-lg interactive")
	firstStore, secondStore := issue48OpenStore(t, firstDir), issue48OpenStore(t, secondDir)
	if err := first.send("/status"); err != nil {
		t.Fatal(err)
	}
	if err := second.send("/status"); err != nil {
		t.Fatal(err)
	}
	first.stdout.waitFor(t, first, 10*time.Second, "collector lifecycle: state=running")
	second.stdout.waitFor(t, second, 10*time.Second, "collector lifecycle: state=running")
	left := issue48WaitStatus(t, firstStore, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "running" })
	right := issue48WaitStatus(t, secondStore, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "running" })
	if left.PID != first.cmd.Process.Pid || right.PID != second.cmd.Process.Pid || left.InstanceID == right.InstanceID {
		t.Fatalf("separate data directories did not acquire independent leases: left=%+v right=%+v", left, right)
	}
	if err := first.send("/quit"); err != nil {
		t.Fatal(err)
	}
	first.wait(t, 5*time.Second)
	other := issue48WaitStatus(t, secondStore, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "running" })
	if other.InstanceID != right.InstanceID || other.PID != second.cmd.Process.Pid {
		t.Fatalf("stopping first directory affected the second lease: before=%+v after=%+v", right, other)
	}
	if err := second.send("/quit"); err != nil {
		t.Fatal(err)
	}
	second.wait(t, 5*time.Second)
}

func TestIssue48CrashedOwnerIsVisibleAndRecoversExplicitly(t *testing.T) {
	dataDir, home := t.TempDir(), t.TempDir()
	issue48Settings(t, dataDir, true, false)
	_, root := issue48SyntheticRoot(t)
	owner := issue48StartREPLMode(t, dataDir, home, []tokenlogger.CollectorRoot{root}, "", "", true)
	owner.stdout.waitFor(t, owner, 10*time.Second, "komposyn-lg interactive")
	store := issue48OpenStore(t, dataDir)
	active := issue48WaitStatus(t, store, 10*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "running" })
	if active.PID != owner.cmd.Process.Pid {
		t.Fatalf("unexpected owner pid: status=%+v child=%d", active, owner.cmd.Process.Pid)
	}
	// Simulate a crash only by killing the tracked process created by this test.
	if err := owner.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill tracked test owner: %v", err)
	}
	select {
	case <-owner.done:
	case <-time.After(5 * time.Second):
		t.Fatal("tracked owner process did not exit after kill")
	}
	issue48WaitStatus(t, store, 22*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool {
		refreshed, err := store.CollectorRuntimeStatus(staleWindow(status.Interval))
		return err == nil && refreshed.State == "stale" && refreshed.Stale
	})
	stale, err := store.CollectorRuntimeStatus(15 * time.Second)
	if err != nil {
		t.Fatalf("read stale lease status: %v", err)
	}
	if !stale.Stale || stale.InstanceID != active.InstanceID {
		t.Fatalf("crashed owner was not represented as the same stale lease: before=%+v after=%+v", active, stale)
	}
	observer := issue48StartREPL(t, dataDir, home, nil, "", "")
	observer.stdout.waitFor(t, observer, 10*time.Second, "komposyn-lg interactive")
	if err := observer.send("/status"); err != nil {
		t.Fatal(err)
	}
	observer.stdout.waitFor(t, observer, 10*time.Second, "collector lifecycle: state=stale")
	if err := observer.send("/quit"); err != nil {
		t.Fatal(err)
	}
	observer.wait(t, 5*time.Second)
	recovery := issue48StartREPLMode(t, dataDir, home, []tokenlogger.CollectorRoot{root}, "", "", true)
	recovery.stdout.waitFor(t, recovery, 10*time.Second, "komposyn-lg interactive")
	recovered := issue48WaitStatus(t, store, 10*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "running" })
	if recovered.PID != recovery.cmd.Process.Pid || recovered.InstanceID == active.InstanceID || !recovered.RecoveredFromStale {
		t.Fatalf("new explicitly started session did not recover the stale lease: %+v", recovered)
	}
	if err := recovery.send("/quit"); err != nil {
		t.Fatal(err)
	}
	recovery.wait(t, 5*time.Second)
}

func TestIssue48DetachedCollectorOutlivesBorrowingREPL(t *testing.T) {
	dataDir, home := t.TempDir(), t.TempDir()
	issue48Settings(t, dataDir, true, true)
	rootPath, root := issue48SyntheticRoot(t)
	store := issue48OpenStore(t, dataDir)
	if _, err := store.RegisterCollectorSource(rootPath, true); err != nil {
		t.Fatal(err)
	}
	instance, err := tokenlogger.NewCollectorInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	closedMarker := filepath.Join(dataDir, "detached-worker-closed")
	t.Setenv(issue48DetachedEnv, "1")
	t.Setenv(issue48DetachedDirEnv, dataDir)
	t.Setenv(issue48DetachedIDEnv, instance)
	t.Setenv(issue48DetachedMark, closedMarker)
	stop := registerDetachedStopCleanup(t, store, instance, closedMarker)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := startDetachedCollector(executable, []string{"-test.run=^TestIssue48DetachedWorker$"}); err != nil {
		t.Fatal(err)
	}
	background := issue48WaitStatus(t, store, 15*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool {
		count, countErr := store.EventCount()
		return countErr == nil && status.State == "running" && status.Mode == "background" && count == 2
	})
	if background.PID == 0 || background.InstanceID != instance {
		t.Fatalf("detached test worker did not publish its owned background lease: %+v", background)
	}

	borrower := issue48StartREPL(t, dataDir, home, []tokenlogger.CollectorRoot{root}, "", "")
	borrower.stdout.waitFor(t, borrower, 10*time.Second, "komposyn-lg interactive")
	if err := borrower.send("/status"); err != nil {
		t.Fatal(err)
	}
	borrower.stdout.waitFor(t, borrower, 10*time.Second, "collector lifecycle: state=running; mode=background")
	if err := borrower.send("/quit"); err != nil {
		t.Fatal(err)
	}
	borrower.wait(t, 5*time.Second)
	stillRunning := issue48WaitStatus(t, store, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "running" })
	if stillRunning.InstanceID != instance || stillRunning.PID != background.PID || stillRunning.Mode != "background" {
		t.Fatalf("closing REPL changed detached collector state: before=%+v after=%+v", background, stillRunning)
	}
	stop()
	stopped := issue48WaitStatus(t, store, 5*time.Second, func(status tokenlogger.CollectorRuntimeStatus) bool { return status.State == "stopped" })
	if stopped.InstanceID != instance {
		t.Fatalf("detached collector stopped with an unexpected instance: %+v", stopped)
	}
}

func TestIssue48StoreOpenSimultaneousInitialization(t *testing.T) {
	dataDir, home := t.TempDir(), t.TempDir()
	issue48Settings(t, dataDir, false, false)
	openGate, startGate := filepath.Join(dataDir, "open.gate"), filepath.Join(dataDir, "start.gate")
	first := issue48StartREPL(t, dataDir, home, nil, openGate, startGate)
	second := issue48StartREPL(t, dataDir, home, nil, openGate, startGate)
	// Both children wait immediately before opening a not-yet-created database.
	// Releasing one shared gate exercises core's coordinated initialization.
	first.stdout.waitFor(t, first, 10*time.Second, "ISSUE48_CHILD_READY_TO_OPEN")
	second.stdout.waitFor(t, second, 10*time.Second, "ISSUE48_CHILD_READY_TO_OPEN")
	issue48ReleaseGate(t, openGate)
	first.stdout.waitFor(t, first, 15*time.Second, "ISSUE48_CHILD_READY_TO_START")
	second.stdout.waitFor(t, second, 15*time.Second, "ISSUE48_CHILD_READY_TO_START")
	issue48ReleaseGate(t, startGate)
	first.stdout.waitFor(t, first, 10*time.Second, "komposyn-lg interactive")
	second.stdout.waitFor(t, second, 10*time.Second, "komposyn-lg interactive")
	if err := first.send("/quit"); err != nil {
		t.Fatal(err)
	}
	if err := second.send("/quit"); err != nil {
		t.Fatal(err)
	}
	first.wait(t, 5*time.Second)
	second.wait(t, 5*time.Second)
	if _, err := os.Stat(filepath.Join(dataDir, "token-logger.db")); err != nil {
		t.Fatalf("simultaneous Store.Open did not initialize a database: %v", err)
	}
}
