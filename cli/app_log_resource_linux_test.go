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

//go:build linux

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

// TestIssue56ProductionLoggerResourceComparison is an opt-in, serialized
// comparison of the actual foreground collector and production eventLogger.
// Run with ISSUE56_RESOURCE_EVIDENCE=1; it is intentionally not a CI benchmark.
func TestIssue56ProductionLoggerResourceComparison(t *testing.T) {
	if os.Getenv("ISSUE56_RESOURCE_EVIDENCE") != "1" {
		t.Skip("set ISSUE56_RESOURCE_EVIDENCE=1 to run the long resource comparison")
	}
	if os.Getenv("ISSUE56_RESOURCE_WORKER") == "1" {
		issue56ResourceWorker(t)
		return
	}
	root := t.TempDir()
	goodSources := issue56ResourceSources(t, filepath.Join(root, "sources"), 96)
	badSources := make([]string, len(goodSources))
	for index := range goodSources {
		badSources[index] = filepath.Join(root, "missing", strconv.Itoa(index)+".jsonl")
	}
	const warmup = 3 * time.Second
	const measured = 30 * time.Second
	const repeats = 3

	var results []issue56ResourceResult
	for repetition := 0; repetition < repeats; repetition++ {
		for _, scenario := range []struct {
			name string
			root []string
		}{{"ordinary", goodSources}, {"repeated-failure", badSources}} {
			modes := []bool{true, false}
			if repetition%2 == 1 {
				modes[0], modes[1] = modes[1], modes[0]
			}
			for _, enabled := range modes {
				result := issue56RunResourceWorker(t, root, scenario.name, scenario.root, enabled, warmup, measured)
				result.Repetition = repetition + 1
				results = append(results, result)
			}
		}
	}
	encoded, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	output := os.Getenv("ISSUE56_RESOURCE_OUTPUT")
	if output == "" {
		fmt.Printf("ISSUE56_RESOURCE_RESULTS=%s\n", encoded)
	} else if err := os.WriteFile(output, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func issue56ResourceWorker(t *testing.T) {
	t.Helper()
	parseDuration := func(name string) time.Duration {
		value, err := time.ParseDuration(os.Getenv(name))
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		return value
	}
	sources := strings.Split(os.Getenv("ISSUE56_RESOURCE_SOURCES"), "\n")
	if len(sources) == 0 || sources[0] == "" {
		t.Fatal("worker received no synthetic source paths")
	}
	result := runIssue56ResourceSample(t, os.Getenv("ISSUE56_RESOURCE_ROOT"), os.Getenv("ISSUE56_RESOURCE_SCENARIO"), sources, os.Getenv("ISSUE56_RESOURCE_LOGGING") == "true", parseDuration("ISSUE56_RESOURCE_WARMUP"), parseDuration("ISSUE56_RESOURCE_DURATION"))
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("ISSUE56_RESOURCE_SAMPLE_OUTPUT"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func issue56RunResourceWorker(t *testing.T, root, scenario string, sources []string, logging bool, warmup, measured time.Duration) issue56ResourceResult {
	t.Helper()
	output := filepath.Join(root, fmt.Sprintf("sample-%s-%t-%d.json", scenario, logging, time.Now().UnixNano()))
	cmd := exec.Command(os.Args[0], "-test.run=^TestIssue56ProductionLoggerResourceComparison$")
	cmd.Env = issue56ReplaceEnv(os.Environ(), map[string]string{
		"ISSUE56_RESOURCE_EVIDENCE":      "1",
		"ISSUE56_RESOURCE_WORKER":        "1",
		"ISSUE56_RESOURCE_ROOT":          root,
		"ISSUE56_RESOURCE_SCENARIO":      scenario,
		"ISSUE56_RESOURCE_SOURCES":       strings.Join(sources, "\n"),
		"ISSUE56_RESOURCE_LOGGING":       strconv.FormatBool(logging),
		"ISSUE56_RESOURCE_WARMUP":        warmup.String(),
		"ISSUE56_RESOURCE_DURATION":      measured.String(),
		"ISSUE56_RESOURCE_SAMPLE_OUTPUT": output,
	})
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resource worker %s logging=%t failed: %v\n%s", scenario, logging, err, combined)
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var result issue56ResourceResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("decode resource worker output: %v", err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	return result
}

func issue56ReplaceEnv(environment []string, replacements map[string]string) []string {
	result := make([]string, 0, len(environment)+len(replacements))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := replacements[name]; !replaced {
			result = append(result, entry)
		}
	}
	for name, value := range replacements {
		result = append(result, name+"="+value)
	}
	return result
}

type issue56ResourceResult struct {
	Scenario        string        `json:"scenario"`
	Logging         bool          `json:"logging"`
	Repetition      int           `json:"repetition"`
	Warmup          time.Duration `json:"warmup_ns"`
	Measurement     time.Duration `json:"measurement_ns"`
	Shutdown        time.Duration `json:"shutdown_ns"`
	LoggerShutdown  time.Duration `json:"logger_shutdown_ns"`
	CPUSeconds      float64       `json:"cpu_seconds"`
	RSSStartBytes   uint64        `json:"rss_start_bytes"`
	RSSEndBytes     uint64        `json:"rss_end_bytes"`
	RSSPeakSampled  uint64        `json:"rss_peak_sampled_bytes"`
	HWMStartBytes   uint64        `json:"hwm_start_bytes"`
	HWMEndBytes     uint64        `json:"hwm_end_bytes"`
	IOReadDelta     uint64        `json:"io_read_bytes_delta"`
	IOWriteDelta    uint64        `json:"io_write_bytes_delta"`
	AllocDelta      uint64        `json:"go_alloc_bytes_delta"`
	HeapEnd         uint64        `json:"go_heap_alloc_bytes_end"`
	LogBytes        int64         `json:"retained_log_bytes"`
	Suppressed      uint64        `json:"suppressed_count"`
	Dropped         uint64        `json:"dropped_count"`
	LogEventCount   int           `json:"log_event_count"`
	FailureObserved bool          `json:"failure_observed"`
	Unchanged       int           `json:"unchanged_source_observations"`
	Imported        int64         `json:"imported_records"`
	ExitCode        int           `json:"collector_exit_code"`
}

func runIssue56ResourceSample(t *testing.T, root, scenario string, sources []string, logging bool, warmup, measured time.Duration) issue56ResourceResult {
	t.Helper()
	dataDir := filepath.Join(root, fmt.Sprintf("%s-%t-%d", scenario, logging, time.Now().UnixNano()))
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var logger applicationLog = discardApplicationLog{}
	var eventLog *eventLogger
	if logging {
		var err error
		eventLog, err = newEventLogger(dataDir, cliVersion, tokenlogger.CoreVersion(), os.Getpid(), "00112233445566778899aabbccddeeff")
		if err != nil {
			t.Fatal(err)
		}
		logger = eventLog
	}
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	args := append([]string{"foreground"}, sources...)
	args = append(args, "--interval", "100ms", "--data-dir", dataDir)
	stderr := &lifecycleBuffer{}
	go func() { done <- runWatchWithContextAndLogger(args, io.Discard, stderr, ctx, logger) }()
	started := time.Now()
	var status tokenlogger.CollectorRuntimeStatus
	for time.Since(started) < 15*time.Second {
		status, err = store.CollectorRuntimeStatus(time.Second)
		if err == nil && status.LastCompletedAt != nil && (scenario != "ordinary" || status.Outcomes.UnchangedSnapshot > 0) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.LastCompletedAt == nil || (scenario == "ordinary" && status.Outcomes.UnchangedSnapshot == 0) {
		cancel()
		_ = store.Close()
		t.Fatalf("%s collector did not complete warmup poll: %+v err=%v", scenario, status, err)
	}
	time.Sleep(warmup)
	status, err = store.CollectorRuntimeStatus(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	beforeCPU, err := issue56ProcCPU()
	if err != nil {
		t.Fatal(err)
	}
	beforeMemory, err := issue56ProcMemory()
	if err != nil {
		t.Fatal(err)
	}
	beforeIO, err := issue56ProcIO()
	if err != nil {
		t.Fatal(err)
	}
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	unchangedBefore := status.Outcomes.UnchangedSnapshot
	result := issue56ResourceResult{Scenario: scenario, Logging: logging, Warmup: warmup, Measurement: measured, RSSStartBytes: beforeMemory.rss, HWMStartBytes: beforeMemory.hwm}
	result.RSSPeakSampled = beforeMemory.rss
	stopSampling := make(chan struct{})
	sampled := make(chan uint64, 1)
	go func() {
		peak := beforeMemory.rss
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSampling:
				sampled <- peak
				return
			case <-ticker.C:
				memory, readErr := issue56ProcMemory()
				if readErr == nil && memory.rss > peak {
					peak = memory.rss
				}
			}
		}
	}()
	time.Sleep(measured)
	close(stopSampling)
	result.RSSPeakSampled = <-sampled
	afterCPU, err := issue56ProcCPU()
	if err != nil {
		t.Fatal(err)
	}
	afterMemory, err := issue56ProcMemory()
	if err != nil {
		t.Fatal(err)
	}
	afterIO, err := issue56ProcIO()
	if err != nil {
		t.Fatal(err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	result.CPUSeconds = afterCPU - beforeCPU
	result.RSSEndBytes, result.HWMEndBytes = afterMemory.rss, afterMemory.hwm
	result.IOReadDelta, result.IOWriteDelta = subtractCounter(afterIO.read, beforeIO.read), subtractCounter(afterIO.write, beforeIO.write)
	result.AllocDelta, result.HeapEnd = after.TotalAlloc-before.TotalAlloc, after.HeapAlloc
	finalStatus, _ := store.CollectorRuntimeStatus(time.Second)
	result.Unchanged, result.Imported = finalStatus.Outcomes.UnchangedSnapshot-unchangedBefore, finalStatus.Imported
	result.FailureObserved = finalStatus.State == "degraded" || finalStatus.State == "failed" || finalStatus.DiscoveryFailures > 0 || finalStatus.Outcomes.FailedSource > 0
	shutdownStart := time.Now()
	cancel()
	select {
	case result.ExitCode = <-done:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("collector did not stop after cancellation")
	}
	result.Shutdown = time.Since(shutdownStart)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if eventLog != nil {
		loggerShutdownStart := time.Now()
		eventLog.Close()
		result.LoggerShutdown = time.Since(loggerShutdownStart)
		result.Suppressed = sumIssue56LogCounts(t, eventLog.sink.active, appLogEventsSuppressed.String())
		result.Dropped = sumIssue56LogCounts(t, eventLog.sink.active, appLogEventsDropped.String())
		paths := []string{eventLog.sink.active}
		for index := 1; index <= eventLog.sink.archiveN; index++ {
			paths = append(paths, eventLog.sink.archivePath(index))
		}
		for _, path := range paths {
			if info, err := os.Stat(path); err == nil {
				result.LogBytes += info.Size()
				contents, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				result.LogEventCount += strings.Count(string(contents), "\n")
			}
		}
	}
	if result.ExitCode != 0 {
		t.Fatalf("collector exit code=%d", result.ExitCode)
	}
	if scenario == "repeated-failure" && !result.FailureObserved {
		t.Fatalf("failure workload did not expose a failed source/discovery state: %+v", result)
	}
	if scenario == "repeated-failure" && logging && (result.LogBytes == 0 || result.LogEventCount > 64 || result.Dropped != 0) {
		t.Fatalf("failure workload did not keep logged evidence bounded and complete: %+v", result)
	}
	if scenario == "ordinary" && (result.Imported != int64(len(sources)*2) || result.Unchanged == 0 || result.Suppressed != 0) {
		t.Fatalf("ordinary workload did not remain a clean imported/unchanged collection: %+v", result)
	}
	if scenario == "ordinary" && stderr.String() != "" {
		t.Fatalf("ordinary workload emitted collector errors: %s", stderr.String())
	}
	return result
}

func issue56ResourceSources(t *testing.T, dir string, count int) []string {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "providers", "codex", "normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sources := make([]string, 0, count)
	for index := 0; index < count; index++ {
		contents := strings.ReplaceAll(string(fixture), "codex-session-fixture", fmt.Sprintf("resource-session-%03d", index))
		contents = strings.ReplaceAll(contents, "project-fixture", fmt.Sprintf("resource-project-%03d", index))
		contents = strings.ReplaceAll(contents, "token-count-", fmt.Sprintf("resource-%03d-token-count-", index))
		path := filepath.Join(dir, fmt.Sprintf("session-%03d.jsonl", index))
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	return sources
}

func sumIssue56LogCounts(t *testing.T, path, message string) uint64 {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var count uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("invalid JSONL record: %v", err)
		}
		if record["msg"] == message {
			if value, ok := record["count"].(float64); ok {
				count += uint64(value)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return count
}

type issue56Memory struct{ rss, hwm uint64 }

func issue56ProcMemory() (issue56Memory, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return issue56Memory{}, err
	}
	var result issue56Memory
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			continue
		}
		switch fields[0] {
		case "VmRSS:":
			result.rss = value * 1024
		case "VmHWM:":
			result.hwm = value * 1024
		}
	}
	return result, nil
}

func issue56ProcCPU() (float64, error) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0, fmt.Errorf("short /proc/self/stat")
	}
	user, err := strconv.ParseUint(fields[13], 10, 64)
	if err != nil {
		return 0, err
	}
	system, err := strconv.ParseUint(fields[14], 10, 64)
	if err != nil {
		return 0, err
	}
	return float64(user+system) / float64(100), nil
}

type issue56IO struct{ read, write uint64 }

func issue56ProcIO() (issue56IO, error) {
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		return issue56IO{}, err
	}
	var result issue56IO
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			continue
		}
		switch fields[0] {
		case "read_bytes:":
			result.read = value
		case "write_bytes:":
			result.write = value
		}
	}
	return result, nil
}

func subtractCounter(after, before uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}
