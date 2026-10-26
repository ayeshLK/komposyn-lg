// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const (
	issue56ChildModeEnv = "KOMPOSYN_ISSUE56_CHILD_MODE"
	issue56ChildDirEnv  = "KOMPOSYN_ISSUE56_CHILD_DIR"
	issue56ChildIDEnv   = "KOMPOSYN_ISSUE56_CHILD_ID"
	issue56ChildCapEnv  = "KOMPOSYN_ISSUE56_CHILD_CAP"
	issue56ChildCount   = "KOMPOSYN_ISSUE56_CHILD_COUNT"
	issue56AppModeEnv   = "KOMPOSYN_ISSUE56_APP_MODE"
	issue56AppReadyEnv  = "KOMPOSYN_ISSUE56_APP_READY"
	issue56AppGateEnv   = "KOMPOSYN_ISSUE56_APP_GATE"
	issue56AppDataEnv   = "KOMPOSYN_ISSUE56_APP_DATA"
	issue56AppLogEnv    = "KOMPOSYN_ISSUE56_APP_LOG"
)

func TestIssue56LogChildHelper(t *testing.T) {
	mode := os.Getenv(issue56ChildModeEnv)
	if mode == "" {
		return
	}
	dir := os.Getenv(issue56ChildDirEnv)
	sink, err := newJSONLLogSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	if capValue := os.Getenv(issue56ChildCapEnv); capValue != "" {
		sink.maxFile, err = strconv.ParseInt(capValue, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
	}
	switch mode {
	case "rotate":
		writerID, err := strconv.Atoi(os.Getenv(issue56ChildIDEnv))
		if err != nil {
			t.Fatal(err)
		}
		count, err := strconv.Atoi(os.Getenv(issue56ChildCount))
		if err != nil {
			t.Fatal(err)
		}
		droppedForLockContention := 0
		for index := 0; index < count; index++ {
			line := []byte(fmt.Sprintf("{\"format_version\":1,\"writer\":%d,\"sequence\":%d}\n", writerID, index))
			if err := sink.appendBatch([][]byte{line}); err != nil {
				if err.Error() == "log lock wait exceeded" {
					droppedForLockContention++
					continue
				}
				t.Fatalf("append process record: %v", err)
			}
		}
		if droppedForLockContention > 0 {
			fmt.Fprintf(os.Stderr, "dropped %d synthetic records after bounded lock wait\n", droppedForLockContention)
		}
	case "lock-owner":
		lock, err := openPrivateLogFile(sink.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			locked, err := tryLockSettingsFile(lock)
			if err != nil {
				t.Fatal(err)
			}
			if locked {
				fmt.Println("LOCKED")
				select {}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("could not acquire test log lock")
	case "process":
		if err := os.Setenv("TOKEN_LOGGER_LOG_DIR", dir); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := runProcess([]string{"version"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("runProcess code=%d stderr=%q", code, stderr.String())
		}
		if !strings.HasPrefix(stdout.String(), "komposyn-lg ") {
			t.Fatalf("version output = %q", stdout.String())
		}
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

func TestLogDirectoryResolutionAndUnavailableHome(t *testing.T) {
	got, err := resolveLogDirectory("relative/logs", func() (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("relative/logs")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("relative override resolved to %q; want %q", got, want)
	}
	if _, err := resolveLogDirectory("", func() (string, error) { return "", fmt.Errorf("synthetic home error") }); err == nil {
		t.Fatal("missing home was accepted without an override")
	}
	if got, err := resolveLogDirectory("", func() (string, error) { return "/synthetic/home", nil }); err != nil || got != filepath.Join("/synthetic/home", ".komposyn", "token-logger") {
		t.Fatalf("default log directory = %q, %v", got, err)
	}
	if got, err := resolveLogDirectory("chosen", func() (string, error) { return "", fmt.Errorf("must not be called") }); err != nil || !strings.HasSuffix(got, filepath.Join("chosen")) {
		t.Fatalf("explicit override did not bypass home lookup: %q, %v", got, err)
	}
}

func TestProcessLoggingStartsBeforeDatabaseAndDoesNotChangeStdout(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "private logs")
	t.Setenv("TOKEN_LOGGER_LOG_DIR", logDir)
	var stdout, stderr bytes.Buffer
	if code := runProcess([]string{"version"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("runProcess code=%d stderr=%q", code, stderr.String())
	}
	if stdout.String() != versionOutput()+"\n" {
		t.Fatalf("stdout changed: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(logDir, "token-logger.db")); !os.IsNotExist(err) {
		t.Fatalf("short command unexpectedly opened a database: stat err=%v", err)
	}
	contents, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	records := decodeLogLines(t, contents)
	if len(records) != 2 || records[0]["msg"] != "process.started" || records[1]["msg"] != "process.finished" {
		t.Fatalf("process records = %#v", records)
	}
	if records[0]["format_version"] != float64(logFormatVersion) || records[0]["core_version"] != tokenlogger.CoreVersion() || records[0]["cli_version"] != cliVersion || records[0]["run_id"] == "" {
		t.Fatalf("missing version/run metadata: %#v", records[0])
	}
}

func TestProcessLoggingPreservesProtocolAndResourceMachineOutput(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	t.Setenv("TOKEN_LOGGER_LOG_DIR", logDir)
	dataDir := filepath.Join(t.TempDir(), "data")
	for _, format := range []string{"json", "csv"} {
		args := []string{"projects", "list", "--data-dir", dataDir, "--format", format}
		var baseline, logged bytes.Buffer
		if code := runWithInput(args, strings.NewReader(""), &baseline, io.Discard); code != 0 {
			t.Fatalf("baseline %s command exit=%d", format, code)
		}
		if code := runProcess(args, strings.NewReader(""), &logged, io.Discard); code != 0 {
			t.Fatalf("logged %s command exit=%d", format, code)
		}
		if !bytes.Equal(baseline.Bytes(), logged.Bytes()) {
			t.Fatalf("logging changed %s output:\nbaseline=%q\nlogged=%q", format, baseline.Bytes(), logged.Bytes())
		}
	}
	protocolDataDir := filepath.Join(t.TempDir(), "protocol-data")
	var input bytes.Buffer
	requests := []map[string]any{
		{"protocol": protocolName, "version": protocolVersion, "id": "init", "method": "initialize", "params": map[string]any{"data_dir": protocolDataDir}},
		{"protocol": protocolName, "version": protocolVersion, "id": "health", "method": "health"},
		{"protocol": protocolName, "version": protocolVersion, "id": "shutdown", "method": "shutdown"},
	}
	for _, request := range requests {
		if err := json.NewEncoder(&input).Encode(request); err != nil {
			t.Fatal(err)
		}
	}
	var baseline, logged bytes.Buffer
	if code := runWithInput([]string{"harness"}, bytes.NewReader(input.Bytes()), &baseline, io.Discard); code != 0 {
		t.Fatalf("baseline harness exit=%d", code)
	}
	if code := runProcess([]string{"harness"}, bytes.NewReader(input.Bytes()), &logged, io.Discard); code != 0 {
		t.Fatalf("logged harness exit=%d", code)
	}
	if !bytes.Equal(baseline.Bytes(), logged.Bytes()) {
		t.Fatalf("logging changed protocol-v1 stdout:\nbaseline=%q\nlogged=%q", baseline.Bytes(), logged.Bytes())
	}
}

func TestProcessLoggingOmitsArgumentsAndRawFallbackErrors(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	t.Setenv("TOKEN_LOGGER_LOG_DIR", logDir)
	marker := "private-marker-project-identity"
	var stdout, stderr bytes.Buffer
	if code := runProcess([]string{marker, "--token", marker}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("unknown command exit=%d", code)
	}
	contents, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte(marker)) || bytes.Contains(contents, []byte("--token")) {
		t.Fatalf("uncontrolled command arguments were logged: %s", contents)
	}
	records := decodeLogLines(t, contents)
	if len(records) != 3 || records[1]["msg"] != "command.failed" || records[1]["exit_code"] != float64(1) || records[2]["msg"] != "process.finished" {
		t.Fatalf("failed command lifecycle records = %#v", records)
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOKEN_LOGGER_LOG_DIR", blocked)
	stdout.Reset()
	stderr.Reset()
	if code := runProcess([]string{"version"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("log failure changed command result: code=%d stderr=%q", code, stderr.String())
	}
	if stdout.String() != versionOutput()+"\n" || stderr.String() != "warning: local troubleshooting log unavailable\n" {
		t.Fatalf("unexpected fallback output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestLogRepeatSuppressionAndValidJSONL(t *testing.T) {
	logger, err := newEventLogger(t.TempDir(), "v1.2.3", "v0.9.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	logger.Event(appLogCollectorFailed, appLogCodeCollectorPoll, appCommandWatch, 2)
	for range 100 {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorPoll, appCommandWatch, 2)
	}
	logger.Close()
	contents, err := os.ReadFile(logger.sink.active)
	if err != nil {
		t.Fatal(err)
	}
	records := decodeLogLines(t, contents)
	if len(records) != 2 || records[0]["msg"] != "collector.failed" || records[1]["msg"] != "logging.events_suppressed" || records[1]["count"] != float64(100) {
		t.Fatalf("repeat suppression records = %#v", records)
	}
	if records[0]["level"] != "WARN" || records[0]["code"] != "collector_poll" {
		t.Fatalf("event severity/code = %#v", records[0])
	}
	if len(logger.last) > maxLogKeyState || len(logger.suppressed) > maxLogKeyState {
		t.Fatalf("repeat-suppression state grew beyond its bound: last=%d suppressed=%d", len(logger.last), len(logger.suppressed))
	}
	loggedAt, err := time.Parse(time.RFC3339Nano, records[0]["time"].(string))
	if err != nil || loggedAt.Location() != time.UTC {
		t.Fatalf("timestamp is not valid UTC: %v, %v", records[0]["time"], err)
	}
}

func newUnstartedEventLogger(t *testing.T) *eventLogger {
	t.Helper()
	sink, err := newJSONLLogSink(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := &eventLogger{
		sink: sink, cliVersion: "1.2.3", coreVersion: "0.9.0", pid: os.Getpid(),
		runID: "00112233445566778899aabbccddeeff", encodeBuffer: bytes.Buffer{},
		suppressed: make(map[logKey]uint64), last: make(map[logKey]time.Time),
	}
	logger.encoder = newLogEncoder(&logger.encodeBuffer)
	return logger
}

func TestFailedSuppressionSummaryWriteRetainsCountsForRecovery(t *testing.T) {
	logger := newUnstartedEventLogger(t)
	counts := map[logKey]uint64{
		{event: appLogCollectorFailed, code: appLogCodeCollectorPoll, command: appCommandWatch}:    100,
		{event: appLogCollectorFailed, code: appLogCodeCollectorStore, command: appCommandWatch}:   2500,
		{event: appLogCommandFailed, code: appLogCodeSettingsLoad, command: appCommandInteractive}: 17,
	}
	for key, count := range counts {
		logger.suppressed[key] = count
	}
	logger.sink.writeFile = func(_ *os.File, _ []byte) (int, error) {
		return 0, errors.New("synthetic zero-byte write failure")
	}
	logger.flushSummaries()
	if len(logger.suppressed) != len(counts) {
		t.Fatalf("zero-byte failure lost suppression keys: got %d, want %d", len(logger.suppressed), len(counts))
	}
	for key, want := range counts {
		if got := logger.suppressed[key]; got != want {
			t.Errorf("suppression count for %+v after failure = %d, want %d", key, got, want)
		}
	}
	logger.sink.writeFile = nil
	logger.flushSummaries()
	assertSuppressionRecords(t, logger.sink.active, counts)
}

func TestPartialSummaryBatchRetriesOnlyUnwrittenSuffix(t *testing.T) {
	logger := newUnstartedEventLogger(t)
	counts := map[logKey]uint64{
		{event: appLogCollectorFailed, code: appLogCodeCollectorPoll, command: appCommandWatch}:    101,
		{event: appLogCollectorFailed, code: appLogCodeCollectorStore, command: appCommandWatch}:   202,
		{event: appLogCommandFailed, code: appLogCodeSettingsLoad, command: appCommandInteractive}: 303,
	}
	for key, count := range counts {
		logger.suppressed[key] = count
	}
	calls := 0
	logger.sink.writeFile = func(file *os.File, line []byte) (int, error) {
		calls++
		if calls == 2 {
			n, err := file.Write(line[:len(line)/2])
			if err != nil {
				return n, err
			}
			return n, errors.New("synthetic partial write failure")
		}
		return file.Write(line)
	}
	logger.flushSummaries()
	if calls != 2 {
		t.Fatalf("failure injection reached %d writes, want failure on second", calls)
	}
	if len(logger.suppressed) != len(counts)-1 {
		t.Fatalf("partial batch retained %d summaries, want only unwritten suffix of %d", len(logger.suppressed), len(counts)-1)
	}
	contents, err := os.ReadFile(logger.sink.active)
	if err != nil {
		t.Fatal(err)
	}
	firstBatch := decodeLogLines(t, contents)
	if len(firstBatch) != 1 || firstBatch[0]["msg"] != appLogEventsSuppressed.String() {
		t.Fatalf("partial batch committed unexpected records: %#v", firstBatch)
	}
	logger.sink.writeFile = nil
	logger.flushSummaries()
	assertSuppressionRecords(t, logger.sink.active, counts)
}

func TestFailedTruncationAccountsUncertainSummaryWithoutReplayingPrefix(t *testing.T) {
	logger := newUnstartedEventLogger(t)
	counts := map[logKey]uint64{
		{event: appLogCollectorFailed, code: appLogCodeCollectorPoll, command: appCommandWatch}:    101,
		{event: appLogCollectorFailed, code: appLogCodeCollectorStore, command: appCommandWatch}:   202,
		{event: appLogCommandFailed, code: appLogCodeSettingsLoad, command: appCommandInteractive}: 303,
	}
	for key, count := range counts {
		logger.suppressed[key] = count
	}
	calls := 0
	logger.sink.writeFile = func(file *os.File, line []byte) (int, error) {
		calls++
		if calls == 2 {
			n, err := file.Write(line[:len(line)/2])
			if err != nil {
				return n, err
			}
			return n, errors.New("synthetic partial summary write")
		}
		return file.Write(line)
	}
	logger.sink.truncateFile = func(*os.File, int64) error {
		return errors.New("synthetic truncation failure")
	}
	logger.flushSummaries()
	if calls != 2 || len(logger.suppressed) != 1 {
		t.Fatalf("uncertain summary outcome: writes=%d pending=%v", calls, logger.suppressed)
	}
	contents, err := os.ReadFile(logger.sink.active)
	if err != nil {
		t.Fatal(err)
	}
	firstLine, _, foundLine := bytes.Cut(contents, []byte("\n"))
	if !foundLine {
		t.Fatalf("known successful prefix was not written: %q", contents)
	}
	firstRecord := decodeLogLines(t, append(firstLine, '\n'))[0]
	prefixKey := recordKeyFromLog(firstRecord)
	remainingKey := firstLogKey(logger.suppressed)
	uncertainKey := logKey{}
	for key := range counts {
		if (key.code.String() == prefixKey.code && key.command.String() == prefixKey.command) || key == remainingKey {
			continue
		}
		uncertainKey = key
	}
	if logger.dropped.Load() != counts[uncertainKey] {
		t.Fatalf("uncertain summary lost-evidence count=%d, want %d for %+v", logger.dropped.Load(), counts[uncertainKey], uncertainKey)
	}
	logger.sink.writeFile = nil
	logger.sink.truncateFile = nil
	logger.flushSummaries()
	contents, err = os.ReadFile(logger.sink.active)
	if err != nil {
		t.Fatal(err)
	}
	records := decodeLogLines(t, contents)
	if len(records) != 3 {
		t.Fatalf("recovered uncertain batch has %d records, want prefix, suffix, dropped summary: %#v", len(records), records)
	}
	suppressed := make(map[recordKey]uint64)
	droppedCount := uint64(0)
	for _, record := range records {
		switch record["msg"] {
		case appLogEventsSuppressed.String():
			suppressed[recordKeyFromLog(record)] = uint64(record["count"].(float64))
		case appLogEventsDropped.String():
			droppedCount = uint64(record["count"].(float64))
		default:
			t.Fatalf("unexpected recovered record: %#v", record)
		}
	}
	wantSuppressed := map[recordKey]uint64{
		prefixKey:                      counts[findLogKey(counts, prefixKey)],
		recordKeyFromKey(remainingKey): counts[remainingKey],
	}
	if !reflect.DeepEqual(suppressed, wantSuppressed) || droppedCount != counts[uncertainKey] {
		t.Fatalf("recovered suppression=%v dropped=%d, want suppression=%v dropped=%d", suppressed, droppedCount, wantSuppressed, counts[uncertainKey])
	}
	if logger.dropped.Load() != 0 {
		t.Fatalf("successfully written dropped summary left pending count %d", logger.dropped.Load())
	}
}

type recordKey struct {
	code    string
	command string
}

func recordKeyFromLog(record map[string]any) recordKey {
	return recordKey{code: record["code"].(string), command: record["command"].(string)}
}

func recordKeyFromKey(key logKey) recordKey {
	return recordKey{code: key.code.String(), command: key.command.String()}
}

func firstLogKey(values map[logKey]uint64) logKey {
	for key := range values {
		return key
	}
	return logKey{}
}

func findLogKey(values map[logKey]uint64, wanted recordKey) logKey {
	for key := range values {
		if recordKeyFromKey(key) == wanted {
			return key
		}
	}
	return logKey{}
}

func assertSuppressionRecords(t *testing.T, path string, want map[logKey]uint64) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	records := decodeLogLines(t, contents)
	if len(records) != len(want) {
		t.Fatalf("suppression records = %d, want %d: %#v", len(records), len(want), records)
	}
	type recordKey struct {
		code    string
		command string
	}
	wantRecords := make(map[recordKey]uint64, len(want))
	for key, count := range want {
		wantRecords[recordKey{code: key.code.String(), command: key.command.String()}] = count
	}
	got := make(map[recordKey]uint64, len(records))
	for _, record := range records {
		if record["msg"] != appLogEventsSuppressed.String() {
			t.Fatalf("unexpected summary record: %#v", record)
		}
		key := recordKey{code: record["code"].(string), command: record["command"].(string)}
		got[key] = uint64(record["count"].(float64))
	}
	if !reflect.DeepEqual(got, wantRecords) {
		t.Fatalf("recovered suppression counts = %#v, want %#v", got, wantRecords)
	}
}

func TestLogMetadataRejectsUncontrolledUnicodeAndBoundsOversize(t *testing.T) {
	marker := "private-project-marker"
	logger, err := newEventLogger(t.TempDir(), "1.2.3-"+strings.Repeat("a", 70), "1.2.3-λ-"+marker, os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	logger.Close()
	contents, err := os.ReadFile(logger.sink.active)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte(marker)) {
		t.Fatalf("uncontrolled Unicode metadata reached the log: %s", contents)
	}
	records := decodeLogLines(t, contents)
	if len(records) != 1 || records[0]["cli_version"] != "unknown" || records[0]["core_version"] != "unknown" || records[0]["metadata_truncated"] != true {
		t.Fatalf("bounded metadata record = %#v", records)
	}
	if len(contents) > maxLogEventBytes {
		t.Fatalf("encoded event size %d exceeds maximum", len(contents))
	}
	for key, value := range records[0] {
		if _, ok := value.(map[string]any); ok {
			t.Fatalf("unexpected nested field %q", key)
		}
		if _, ok := value.([]any); ok {
			t.Fatalf("unexpected nested array field %q", key)
		}
	}
}

func TestLogRotationBoundsRecoveryAndTornTail(t *testing.T) {
	dir := t.TempDir()
	sink, err := newJSONLLogSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink.maxFile = 512
	for index := 0; index < 40; index++ {
		line, _ := json.Marshal(map[string]any{"format_version": 1, "index": index, "text": strings.Repeat("x", 90)})
		line = append(line, '\n')
		if err := sink.appendBatch([][]byte{line}); err != nil {
			t.Fatal(err)
		}
	}
	assertBoundedLogFiles(t, sink)
	if err := os.WriteFile(sink.archivePath(4), []byte("older-configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sink.appendBatch([][]byte{[]byte("{\"format_version\":1,\"legacy\":true}\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sink.archivePath(4)); !os.IsNotExist(err) {
		t.Fatalf("legacy numeric archive was not pruned: %v", err)
	}
	if contents, err := os.ReadFile(filepath.Join(dir, "unrelated.txt")); err != nil || string(contents) != "preserve" {
		t.Fatalf("unrelated file changed during recovery: %q err=%v", contents, err)
	}
	oversized := bytes.Repeat([]byte("{\"format_version\":1,\"legacy\":true}\n"), 40)
	if err := os.WriteFile(sink.active, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sink.appendBatch([][]byte{[]byte("{\"format_version\":1,\"after_oversize\":true}\n")}); err != nil {
		t.Fatal(err)
	}
	assertBoundedLogFiles(t, sink)

	if err := os.Remove(sink.active); err != nil {
		t.Fatal(err)
	}
	if err := sink.appendBatch([][]byte{[]byte("{\"format_version\":1,\"recovered\":true}\n")}); err != nil {
		t.Fatalf("recover active file after interrupted rotation: %v", err)
	}
	assertBoundedLogFiles(t, sink)

	if err := os.WriteFile(sink.active, []byte("{\"format_version\":1}\n{\"partial\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sink.appendBatch([][]byte{[]byte("{\"format_version\":1,\"after_torn\":true}\n")}); err != nil {
		t.Fatalf("recover torn final line: %v", err)
	}
	active, err := os.ReadFile(sink.active)
	if err != nil {
		t.Fatal(err)
	}
	decodeLogLines(t, active)
}

func TestRotationFailuresPreserveRecordsAndRecoverGaps(t *testing.T) {
	tests := []struct {
		name            string
		fail            func(*testing.T, *jsonlLogSink)
		wantAfter       map[string]int
		wantAfterAppend map[string]int
	}{
		{
			name: "oldest archive removal failure",
			fail: func(t *testing.T, sink *jsonlLogSink) {
				sink.removeFile = func(path string) error {
					if path == sink.archivePath(3) {
						return errors.New("injected archive removal failure")
					}
					return removeLogFile(path)
				}
			},
			wantAfter:       map[string]int{"active": 1, "one": 1, "two": 1, "three": 1},
			wantAfterAppend: map[string]int{"active": 1, "new": 1, "one": 1, "two": 1, "three": 1},
		},
		{
			name: "middle archive rename failure",
			fail: func(t *testing.T, sink *jsonlLogSink) {
				sink.renameFile = func(source, target string) error {
					if source == sink.archivePath(1) && target == sink.archivePath(2) {
						return errors.New("injected middle archive rename failure")
					}
					return renameLogFile(source, target)
				}
			},
			wantAfter:       map[string]int{"active": 1, "one": 1, "two": 1},
			wantAfterAppend: map[string]int{"active": 1, "new": 1, "one": 1, "two": 1},
		},
		{
			name: "active archive rename failure",
			fail: func(t *testing.T, sink *jsonlLogSink) {
				sink.renameFile = func(source, target string) error {
					if source == sink.active && target == sink.archivePath(1) {
						return errors.New("injected active rename failure")
					}
					return renameLogFile(source, target)
				}
			},
			wantAfter:       map[string]int{"active": 1, "one": 1, "two": 1},
			wantAfterAppend: map[string]int{"active": 1, "new": 1, "one": 1, "two": 1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink, unrelated := seedRotationFiles(t)
			test.fail(t, sink)
			if err := sink.rotate(); err == nil {
				t.Fatal("injected rotation failure succeeded")
			}
			assertRotationMarkers(t, sink, test.wantAfter, unrelated)
			sink.renameFile = nil
			sink.removeFile = nil
			line := []byte("{\"format_version\":1,\"marker\":\"new\"}\n")
			if err := sink.appendBatch([][]byte{line}); err != nil {
				t.Fatalf("append after interrupted rotation: %v", err)
			}
			assertRotationMarkers(t, sink, test.wantAfterAppend, unrelated)
			assertActiveRotationMarkers(t, sink, map[string]int{"active": 1, "new": 1})
			for range 3 {
				if err := sink.appendBatch([][]byte{line}); err != nil {
					t.Fatalf("repeat append after interrupted rotation: %v", err)
				}
			}
			assertActiveRotationMarkers(t, sink, map[string]int{"active": 1, "new": 4})
			assertBoundedLogFiles(t, sink)
		})
	}
}

func seedRotationFiles(t *testing.T) (*jsonlLogSink, string) {
	t.Helper()
	dir := t.TempDir()
	sink, err := newJSONLLogSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink.maxFile = 4096
	markers := map[string]string{"active": "active", "archive1": "one", "archive2": "two", "archive3": "three"}
	paths := map[string]string{
		"active": sink.active, "archive1": sink.archivePath(1),
		"archive2": sink.archivePath(2), "archive3": sink.archivePath(3),
	}
	for name, marker := range markers {
		line := []byte(fmt.Sprintf("{\"format_version\":1,\"marker\":%q}\n", marker))
		if err := os.WriteFile(paths[name], line, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	unrelated := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("preserve this file"), 0o600); err != nil {
		t.Fatal(err)
	}
	return sink, unrelated
}

func assertRotationMarkers(t *testing.T, sink *jsonlLogSink, want map[string]int, unrelated string) {
	t.Helper()
	paths := map[string]string{
		"active": sink.active, "archive1": sink.archivePath(1),
		"archive2": sink.archivePath(2), "archive3": sink.archivePath(3),
	}
	got := make(map[string]int)
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range decodeLogLines(t, contents) {
			marker := record["marker"].(string)
			got[marker]++
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rotation markers = %v, want %v", got, want)
	}
	if contents, err := os.ReadFile(unrelated); err != nil || string(contents) != "preserve this file" {
		t.Errorf("unrelated file changed: %q err=%v", contents, err)
	}
}

func assertActiveRotationMarkers(t *testing.T, sink *jsonlLogSink, want map[string]int) {
	t.Helper()
	contents, err := os.ReadFile(sink.active)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]int)
	for _, record := range decodeLogLines(t, contents) {
		got[record["marker"].(string)]++
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("active log markers after interrupted rotation = %v, want %v", got, want)
	}
}

func TestLogWriteFailureIsBoundedAndNextAppendRepairsTornTail(t *testing.T) {
	sink, err := newJSONLLogSink(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink.writeFile = func(file *os.File, line []byte) (int, error) {
		n, err := file.Write(line[:len(line)/2])
		if err != nil {
			return n, err
		}
		return n, fmt.Errorf("synthetic disk full: %w", os.ErrInvalid)
	}
	if err := sink.appendBatch([][]byte{[]byte("{\"format_version\":1,\"partial\":true}\n")}); err == nil {
		t.Fatal("injected partial write succeeded")
	}
	sink.writeFile = nil
	if err := sink.appendBatch([][]byte{[]byte("{\"format_version\":1,\"recovered\":true}\n")}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(sink.active)
	if err != nil {
		t.Fatal(err)
	}
	records := decodeLogLines(t, contents)
	if len(records) != 1 || records[0]["recovered"] != true {
		t.Fatalf("partial-write recovery retained invalid data: %#v", records)
	}
}

func TestLogLockWaitAndShutdownStayWithinBudgets(t *testing.T) {
	dir := t.TempDir()
	logger, err := newEventLogger(dir, "1.2.3", "0.9.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- withLocalFileLockTimeout(logger.sink.lockPath, time.Second, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("could not acquire synthetic log lock")
	}
	lockStart := time.Now()
	lockErr := logger.sink.appendBatch([][]byte{[]byte("{\"format_version\":1}\n")})
	lockElapsed := time.Since(lockStart)
	if lockErr == nil {
		t.Fatal("append unexpectedly succeeded while another process held the log lock")
	}
	if lockElapsed > logLockBudget+25*time.Millisecond {
		t.Fatalf("log lock wait %s exceeded the configured %s budget", lockElapsed, logLockBudget)
	}
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	start := time.Now()
	logger.Close()
	if elapsed := time.Since(start); elapsed > logCloseBudget+100*time.Millisecond {
		t.Fatalf("shutdown exceeded flush budget: %s", elapsed)
	}
	if !logger.Unavailable() {
		t.Fatal("lock contention did not expose the sink failure state")
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
}

func TestProductionEventLoggerPressureRecoveryAndClose(t *testing.T) {
	logger, err := newEventLogger(t.TempDir(), "1.2.3", "0.9.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	logger.sink.writeFile = func(file *os.File, line []byte) (int, error) {
		once.Do(func() {
			close(entered)
			<-release
		})
		return file.Write(line)
	}
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("production logger did not enter the controlled blocked write")
	}
	errorKey := logKey{event: appLogCollectorFailed, code: appLogCodeCollectorPoll, command: appCommandWatch}
	logger.Event(errorKey.event, errorKey.code, errorKey.command, 2)
	const repeatCount = 900
	for range repeatCount {
		logger.Event(errorKey.event, errorKey.code, errorKey.command, 2)
	}
	for event := appLogProcessStarted; event <= appLogCollectorFailed && len(logger.queue) < cap(logger.queue); event++ {
		for code := appLogCodeNone; code <= appLogCodeLogWrite && len(logger.queue) < cap(logger.queue); code++ {
			for command := appCommandInteractive; command <= appCommandOther && len(logger.queue) < cap(logger.queue); command++ {
				key := logKey{event: event, code: code, command: command}
				if key == errorKey {
					continue
				}
				logger.mu.Lock()
				if _, exists := logger.last[key]; !exists && len(logger.last) >= maxLogKeyState-1 {
					logger.last[overflowLogKey] = time.Time{}
				} else {
					logger.last[key] = time.Time{}
				}
				logger.mu.Unlock()
				logger.Event(event, code, command, 1)
			}
		}
	}
	if got := len(logger.queue); got != cap(logger.queue) {
		t.Fatalf("production queue filled to %d, want bound %d", got, cap(logger.queue))
	}
	if len(logger.last) > maxLogKeyState || len(logger.suppressed) > maxLogKeyState {
		t.Fatalf("production logger state exceeded bounds: last=%d suppressed=%d", len(logger.last), len(logger.suppressed))
	}
	const droppedAttempts = 20
	publicationDone := make(chan struct{})
	go func() {
		for range droppedAttempts {
			logger.mu.Lock()
			logger.last[overflowLogKey] = time.Time{}
			logger.mu.Unlock()
			logger.Event(appLogEventsDropped, appLogCodeNone, appCommandOther, 0)
		}
		close(publicationDone)
	}()
	select {
	case <-publicationDone:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("publisher waited for queue capacity while the file writer was blocked")
	}
	if got := logger.dropped.Load(); got != droppedAttempts {
		close(release)
		t.Fatalf("drop accounting before recovery=%d, want %d", got, droppedAttempts)
	}
	close(release)
	closeStart := time.Now()
	logger.Close()
	if elapsed := time.Since(closeStart); elapsed > logCloseBudget+100*time.Millisecond {
		t.Fatalf("production logger close took %s, budget %s", elapsed, logCloseBudget)
	}
	logger.Close()
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	select {
	case <-logger.done:
	default:
		t.Fatal("worker did not exit after pressure was released and Close returned")
	}
	if len(logger.queue) != 0 {
		t.Fatalf("queue retained %d events after successful drain", len(logger.queue))
	}
	contents, err := os.ReadFile(logger.sink.active)
	if err != nil {
		t.Fatal(err)
	}
	records := decodeLogLines(t, contents)
	var written, suppressed, dropped uint64
	for _, record := range records {
		switch record["msg"] {
		case appLogEventsSuppressed.String():
			suppressed += uint64(record["count"].(float64))
		case appLogEventsDropped.String():
			dropped += uint64(record["count"].(float64))
		default:
			written++
		}
	}
	if written != uint64(cap(logger.queue)+1) || suppressed < repeatCount || dropped != droppedAttempts {
		t.Fatalf("pressure evidence: written=%d suppressed=%d dropped=%d", written, suppressed, dropped)
	}
	beforeDrop := logger.dropped.Load()
	beforeQueue := len(logger.queue)
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	if logger.dropped.Load() != beforeDrop || len(logger.queue) != beforeQueue {
		t.Fatal("publication after Close changed logger state")
	}
}

func TestProductionEventLoggerCloseBudgetDoesNotCancelBlockedFilesystemWork(t *testing.T) {
	logger, err := newEventLogger(t.TempDir(), "1.2.3", "0.9.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	logger.sink.writeFile = func(file *os.File, line []byte) (int, error) {
		close(entered)
		<-release
		return file.Write(line)
	}
	logger.Event(appLogProcessStarted, appLogCodeNone, appCommandOther, 0)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not enter the controlled blocked filesystem operation")
	}
	started := time.Now()
	logger.Close()
	if elapsed := time.Since(started); elapsed > logCloseBudget+100*time.Millisecond {
		close(release)
		t.Fatalf("Close exceeded its wait budget: %s", elapsed)
	}
	select {
	case <-logger.done:
		close(release)
		t.Fatal("logger claimed its worker was cancelled while the filesystem hook remained blocked")
	default:
	}
	logger.Close()
	close(release)
	select {
	case <-logger.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after blocked filesystem operation was released")
	}
}

func TestProductionEventLoggerPublishersRaceNormalClose(t *testing.T) {
	logger, err := newEventLogger(t.TempDir(), "1.2.3", "0.9.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	const publishers = 8
	start := make(chan struct{})
	var ready, workers sync.WaitGroup
	ready.Add(publishers)
	workers.Add(publishers)
	for index := range publishers {
		go func(index int) {
			defer workers.Done()
			ready.Done()
			<-start
			for attempt := 0; attempt < 500; attempt++ {
				event := appLogEvent(1 + (attempt+index)%int(appLogEventsDropped))
				code := appLogCode((attempt + index) % int(appLogCodeLogWrite+1))
				command := appCommand(1 + (attempt+index)%int(appCommandOther))
				logger.Event(event, code, command, attempt%3)
			}
		}(index)
	}
	ready.Wait()
	closeDone := make(chan struct{})
	go func() {
		<-start
		logger.Close()
		close(closeDone)
	}()
	close(start)
	workers.Wait()
	select {
	case <-closeDone:
	case <-time.After(2 * logCloseBudget):
		t.Fatal("normal Close did not finish after racing publishers")
	}
	logger.Close()
	select {
	case <-logger.done:
	case <-time.After(time.Second):
		t.Fatal("logger worker leaked after publisher/Close race")
	}
	if len(logger.queue) != 0 {
		t.Fatalf("publisher/Close race left %d queued events", len(logger.queue))
	}
	if len(logger.last) > maxLogKeyState || len(logger.suppressed) > maxLogKeyState {
		t.Fatalf("publisher/Close race exceeded bounded state: last=%d suppressed=%d", len(logger.last), len(logger.suppressed))
	}
}

func TestLogRejectsSymlinkAndSpecialTargets(t *testing.T) {
	for _, targetName := range []string{"klg.log", "klg.log.lock", "klg.log.1"} {
		t.Run(targetName, func(t *testing.T) {
			dir := t.TempDir()
			sink, err := newJSONLLogSink(dir)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("do not touch"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, targetName)); err != nil {
				t.Skipf("symlink creation is unavailable: %v", err)
			}
			if err := sink.appendBatch([][]byte{[]byte("{\"format_version\":1}\n")}); err == nil {
				t.Fatal("symlink log target accepted")
			}
			contents, err := os.ReadFile(outside)
			if err != nil || string(contents) != "do not touch" {
				t.Fatalf("outside symlink target changed: %q err=%v", contents, err)
			}
		})
	}
}

func TestConcurrentProcessesRotateWithoutInterleaving(t *testing.T) {
	dir := t.TempDir()
	const processes, records = 4, 60
	type child struct {
		command *exec.Cmd
		output  *bytes.Buffer
	}
	commands := make([]child, 0, processes)
	for id := 0; id < processes; id++ {
		command := logChildCommand("rotate", dir, id, 4096, records)
		output := new(bytes.Buffer)
		command.Stdout, command.Stderr = output, output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, child{command: command, output: output})
	}
	for _, child := range commands {
		if err := child.command.Wait(); err != nil {
			t.Fatalf("rotation writer failed: %v: %s", err, child.output.String())
		}
	}
	sink, err := newJSONLLogSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink.maxFile = 4096
	assertBoundedLogFiles(t, sink)
	seenWriters := make(map[int]bool, processes)
	seenRecords := make(map[[2]int]bool)
	for _, path := range append([]string{sink.active}, sink.archivePath(1), sink.archivePath(2), sink.archivePath(3)) {
		contents, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range decodeLogLines(t, contents) {
			writer, sequence := int(record["writer"].(float64)), int(record["sequence"].(float64))
			if writer < 0 || writer >= processes || sequence < 0 || sequence >= records {
				t.Fatalf("unexpected synthetic log record: %#v", record)
			}
			key := [2]int{writer, sequence}
			if seenRecords[key] {
				t.Fatalf("duplicate synthetic log record: %#v", record)
			}
			seenRecords[key] = true
			seenWriters[writer] = true
		}
	}
	if len(seenWriters) != processes {
		t.Fatalf("concurrent rotation omitted writer records: got %v; want all %d writers", seenWriters, processes)
	}
}

func TestLogLockOwnerCrashReleasesLock(t *testing.T) {
	dir := t.TempDir()
	command := logChildCommand("lock-owner", dir, 0, 0, 0)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		ready <- strings.TrimSpace(line)
	}()
	select {
	case line := <-ready:
		if line != "LOCKED" {
			t.Fatalf("lock helper readiness = %q", line)
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("lock owner did not acquire the lock")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	sink, err := newJSONLLogSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.appendBatch([][]byte{[]byte("{\"format_version\":1,\"after_crash\":true}\n")}); err != nil {
		t.Fatalf("log lock remained held after owner crash: %v", err)
	}
}

func TestShortCommandProcessWritesLogWithoutSQLite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	command := logChildCommand("process", dir, 0, 0, 0)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("short command child failed: %v: %s", err, output)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(decodeLogLines(t, contents)); got != 2 {
		t.Fatalf("short-command log lines=%d, want 2", got)
	}
}

func TestDetachedCollectorPersistsSafeLoggingFailureDiagnostic(t *testing.T) {
	dataDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	args := []string{"--data-dir", dataDir, "--interval", "1s", "--instance", "logging-unavailable-test"}
	if code := runWatchServeWithLogger(args, io.Discard, ctx, unavailableApplicationLog{}); code != 0 {
		t.Fatalf("detached collector exit=%d", code)
	}
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pending, err := store.PendingDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range pending {
		if diagnostic.Message == "reason=local_log_sink_unavailable" && diagnostic.Category == tokenlogger.DiagnosticCategoryCLI {
			found = true
		}
	}
	if !found {
		t.Fatalf("detached logging failure is not inspectable in pending local diagnostics: %+v", pending)
	}
}

func BenchmarkRunVersionLogging(b *testing.B) {
	for _, scenario := range []struct {
		name string
		args []string
		exit int
	}{
		{name: "normal-version", args: []string{"version"}, exit: 0},
		{name: "error-unknown-command", args: []string{"invalid-command"}, exit: 1},
	} {
		for _, enabled := range []bool{false, true} {
			name := "logging-off"
			dir := ""
			if enabled {
				name = "logging-on"
				dir = filepath.Join(b.TempDir(), scenario.name)
			}
			b.Run(scenario.name+"/"+name, func(b *testing.B) {
				if enabled {
					b.Setenv("TOKEN_LOGGER_LOG_DIR", dir)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					var code int
					if enabled {
						code = runProcess(scenario.args, strings.NewReader(""), io.Discard, io.Discard)
					} else {
						code = runWithInput(scenario.args, strings.NewReader(""), io.Discard, io.Discard)
					}
					if code != scenario.exit {
						b.Fatalf("exit code=%d want=%d", code, scenario.exit)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(logDirectoryBytes(dir)), "log-bytes")
			})
		}
	}
}

func logChildCommand(mode, dir string, id int, capBytes, count int) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestIssue56LogChildHelper$")
	env := []string{
		issue56ChildModeEnv + "=" + mode,
		issue56ChildDirEnv + "=" + dir,
		issue56ChildIDEnv + "=" + strconv.Itoa(id),
		issue56ChildCapEnv + "=" + strconv.Itoa(capBytes),
		issue56ChildCount + "=" + strconv.Itoa(count),
	}
	command.Env = environmentWith(env...)
	return command
}

func TestIssue56ApplicationLogProcessHelper(t *testing.T) {
	mode := os.Getenv(issue56AppModeEnv)
	if mode == "" {
		return
	}
	ready := os.Getenv(issue56AppReadyEnv)
	gate := os.Getenv(issue56AppGateEnv)
	if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatal("timed out waiting for application writer gate")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.Setenv("TOKEN_LOGGER_LOG_DIR", os.Getenv(issue56AppLogEnv)); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("TOKEN_LOGGER_DATA_DIR", os.Getenv(issue56AppDataEnv)); err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "short":
		if code := runProcess([]string{"version"}, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
			t.Fatalf("short command exit=%d", code)
		}
	case "repl":
		if code := runProcess(nil, strings.NewReader("/status\n/quit\n"), io.Discard, io.Discard); code != 0 {
			t.Fatalf("REPL exit=%d", code)
		}
	case "background":
		logger := newProcessLogger(io.Discard)
		command := appCommandWatch
		logger.Event(appLogProcessStarted, appLogCodeNone, command, 0)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		code := runWatchWithContextAndLogger([]string{"serve", "--data-dir", os.Getenv(issue56AppDataEnv), "--interval", "1s", "--instance", "issue56-log-test"}, io.Discard, io.Discard, ctx, logger)
		logger.Event(appLogProcessFinished, appLogCodeNone, command, code)
		logger.Close()
		if code != 0 {
			t.Fatalf("background worker exit=%d", code)
		}
	default:
		t.Fatalf("unknown application logger helper mode %q", mode)
	}
}

func TestSimultaneousREPLBackgroundAndShortCommandLogging(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	settings := defaultSettings(dataDir)
	settings.OnboardingComplete = true
	if err := saveSettings(filepath.Join(dataDir, "settings.json"), settings); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(t.TempDir(), "shared-log")
	gate := filepath.Join(t.TempDir(), "release")
	readyDir := t.TempDir()
	type childProcess struct {
		cmd    *exec.Cmd
		stdout *bytes.Buffer
		stderr *bytes.Buffer
		ready  string
	}
	children := make([]childProcess, 0, 3)
	for index, mode := range []string{"short", "repl", "background"} {
		ready := filepath.Join(readyDir, strconv.Itoa(index))
		cmd := exec.Command(os.Args[0], "-test.run=^TestIssue56ApplicationLogProcessHelper$")
		cmd.Env = environmentWith(
			issue56AppModeEnv+"="+mode,
			issue56AppReadyEnv+"="+ready,
			issue56AppGateEnv+"="+gate,
			issue56AppDataEnv+"="+dataDir,
			issue56AppLogEnv+"="+logDir,
		)
		stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, childProcess{cmd: cmd, stdout: stdout, stderr: stderr, ready: ready})
	}
	for _, child := range children {
		waitForLogTestFile(t, child.ready)
	}
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		if err := child.cmd.Wait(); err != nil {
			t.Fatalf("application writer failed: %v stdout=%q stderr=%q", err, child.stdout.String(), child.stderr.String())
		}
	}
	contents, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	records := decodeLogLines(t, contents)
	seen := map[string]bool{}
	for _, record := range records {
		seen[record["msg"].(string)] = true
	}
	for _, event := range []string{"process.started", "process.finished", "collector.started", "collector.stopped"} {
		if !seen[event] {
			t.Fatalf("shared log is missing %s event: %#v", event, records)
		}
	}
}

func waitForLogTestFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func environmentWith(values ...string) []string {
	overrides := make(map[string]struct{}, len(values))
	for _, value := range values {
		key, _, _ := strings.Cut(value, "=")
		overrides[key] = struct{}{}
	}
	environment := make([]string, 0, len(os.Environ())+len(values))
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, overridden := overrides[key]; !overridden {
			environment = append(environment, value)
		}
	}
	return append(environment, values...)
}

func decodeLogLines(t *testing.T, contents []byte) []map[string]any {
	t.Helper()
	if len(contents) == 0 || contents[len(contents)-1] != '\n' {
		t.Fatalf("JSONL file has no complete final line: %q", contents)
	}
	lines := bytes.Split(bytes.TrimSuffix(contents, []byte{'\n'}), []byte{'\n'})
	records := make([]map[string]any, 0, len(lines))
	for index, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("line %d is not JSON: %v: %q", index, err, line)
		}
		if record["format_version"] != float64(logFormatVersion) {
			t.Fatalf("line %d format_version=%v", index, record["format_version"])
		}
		records = append(records, record)
	}
	return records
}

func assertBoundedLogFiles(t *testing.T, sink *jsonlLogSink) {
	t.Helper()
	entries, err := os.ReadDir(sink.dir)
	if err != nil {
		t.Fatal(err)
	}
	var retained int64
	for _, entry := range entries {
		if entry.Name() == filepath.Base(sink.lockPath) {
			continue
		}
		if entry.Name() != filepath.Base(sink.active) {
			prefix := filepath.Base(sink.active) + "."
			if !strings.HasPrefix(entry.Name(), prefix) {
				continue
			}
			index, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), prefix))
			if err != nil {
				continue
			}
			if index > sink.archiveN {
				t.Fatalf("old managed archive was retained: %s", entry.Name())
			}
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Size() > sink.maxFile {
			t.Fatalf("unbounded/unsafe log file %s: mode=%s size=%d", entry.Name(), info.Mode(), info.Size())
		}
		retained += info.Size()
		if info.Size() > 0 {
			contents, err := os.ReadFile(filepath.Join(sink.dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			decodeLogLines(t, contents)
		}
	}
	if retained > int64(sink.archiveN+1)*sink.maxFile {
		t.Fatalf("retained log content %d exceeds configured total", retained)
	}
}

func logDirectoryBytes(dir string) int64 {
	if dir == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}
