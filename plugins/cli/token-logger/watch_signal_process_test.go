// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const (
	issue56SignalHelperEnv = "KOMPOSYN_ISSUE56_SIGNAL_HELPER"
	issue56SignalReadyEnv  = "KOMPOSYN_ISSUE56_SIGNAL_READY"
	issue56SignalStdoutEnv = "KOMPOSYN_ISSUE56_SIGNAL_STDOUT"
	issue56SignalStderrEnv = "KOMPOSYN_ISSUE56_SIGNAL_STDERR"
	issue56SignalDataEnv   = "KOMPOSYN_ISSUE56_SIGNAL_DATA"
	issue56SignalLogEnv    = "KOMPOSYN_ISSUE56_SIGNAL_LOG"
	issue56SignalSourceEnv = "KOMPOSYN_ISSUE56_SIGNAL_SOURCE"
)

type signalReadyWriter struct {
	path string
	buf  bytes.Buffer
}

func (writer *signalReadyWriter) Write(value []byte) (int, error) {
	n, err := writer.buf.Write(value)
	if strings.Contains(writer.buf.String(), "watching ") {
		if markerErr := os.WriteFile(writer.path, []byte("ready"), 0o600); markerErr != nil {
			return n, markerErr
		}
	}
	return n, err
}

// This subprocess helper enters through runProcess and the production command
// dispatch, so the test exercises the same signal-aware logger wrapper as main.
func TestIssue56SignalWatchProcessHelper(t *testing.T) {
	if os.Getenv(issue56SignalHelperEnv) != "1" {
		return
	}
	for name, value := range map[string]string{
		"TOKEN_LOGGER_DATA_DIR": os.Getenv(issue56SignalDataEnv),
		"TOKEN_LOGGER_LOG_DIR":  os.Getenv(issue56SignalLogEnv),
	} {
		if err := os.Setenv(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	stdout := &signalReadyWriter{path: os.Getenv(issue56SignalReadyEnv)}
	var stderr bytes.Buffer
	code := runProcess([]string{
		"watch", "foreground", os.Getenv(issue56SignalSourceEnv),
		"--data-dir", os.Getenv(issue56SignalDataEnv), "--interval", "1h",
	}, strings.NewReader(""), stdout, &stderr)
	if err := os.WriteFile(os.Getenv(issue56SignalStdoutEnv), stdout.buf.Bytes(), 0o600); err != nil {
		t.Fatalf("save child stdout: %v", err)
	}
	if err := os.WriteFile(os.Getenv(issue56SignalStderrEnv), stderr.Bytes(), 0o600); err != nil {
		t.Fatalf("save child stderr: %v", err)
	}
	if code != 0 {
		os.Exit(code)
	}
}

func assertSignalStopsForegroundWatch(t *testing.T, signal os.Signal) {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	logDir := filepath.Join(dir, "logs")
	homeDir := filepath.Join(dir, "home")
	sourceDir := filepath.Join(dir, "empty-source")
	for _, path := range []string{dataDir, logDir, homeDir, sourceDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ready := filepath.Join(dir, "ready")
	stdoutPath := filepath.Join(dir, "stdout")
	stderrPath := filepath.Join(dir, "stderr")
	command := exec.Command(os.Args[0], "-test.run=^TestIssue56SignalWatchProcessHelper$")
	command.Env = environmentWith(
		issue56SignalHelperEnv+"=1",
		issue56SignalReadyEnv+"="+ready,
		issue56SignalStdoutEnv+"="+stdoutPath,
		issue56SignalStderrEnv+"="+stderrPath,
		issue56SignalDataEnv+"="+dataDir,
		issue56SignalLogEnv+"="+logDir,
		issue56SignalSourceEnv+"="+sourceDir,
		"HOME="+homeDir,
		"USERPROFILE="+homeDir,
	)
	var childOutput bytes.Buffer
	command.Stdout = &childOutput
	command.Stderr = &childOutput
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("foreground child did not become ready; child output=%q", childOutput.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Signal(signal); err != nil {
		t.Fatalf("send %v to foreground child: %v", signal, err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatalf("foreground child exited after %v: %v; output=%q", signal, err, childOutput.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("foreground child did not cooperatively stop after %v", signal)
	}
	stdout, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("read foreground stdout: %v; child output=%q", err, childOutput.String())
	}
	if !strings.Contains(string(stdout), "collector stopped: lifecycle=stopped") {
		t.Fatalf("foreground stop summary missing after %v: %s", signal, stdout)
	}
	if stderr, readErr := os.ReadFile(stderrPath); readErr != nil || len(stderr) != 0 {
		t.Fatalf("foreground stderr after %v = %q, err=%v", signal, stderr, readErr)
	}
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	status, statusErr := store.CollectorRuntimeStatus(time.Minute)
	closeErr := store.Close()
	if statusErr != nil || closeErr != nil {
		t.Fatalf("read stopped collector status: status err=%v close err=%v", statusErr, closeErr)
	}
	if status.State != "stopped" || status.StoppedAt == nil {
		t.Fatalf("signal left the collector lease unreleased: %+v", status)
	}
	logContents, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	records := decodeLogLines(t, logContents)
	seen := map[string]bool{}
	for _, record := range records {
		seen[record["msg"].(string)] = true
	}
	for _, expected := range []string{"process.started", "collector.started", "collector.stopped", "process.finished"} {
		if !seen[expected] {
			t.Errorf("flushed lifecycle log lacks %q after %v: %#v", expected, signal, records)
		}
	}
}
