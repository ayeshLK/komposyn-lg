// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func syntheticLogLine(at, level, event, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"time":%q,"level":%q,"msg":%q,"format_version":1,"cli_version":"v1.2.3","core_version":"v0.9.0","pid":123,"run_id":"0123456789abcdef0123456789abcdef","command":"watch","metadata_truncated":false%s}`+"\n", at, level, event, extra)
}

func syntheticLogEvent(at, level, event, code string, fields map[string]any) string {
	entry := map[string]any{
		"time": at, "level": level, "msg": event, "format_version": 1,
		"cli_version": "v1.2.3", "core_version": "v0.9.0", "pid": 123,
		"run_id": "0123456789abcdef0123456789abcdef", "command": "watch",
		"metadata_truncated": false,
	}
	if code != "" {
		entry["code"] = code
	}
	for key, value := range fields {
		entry[key] = value
	}
	data, err := json.Marshal(entry)
	if err != nil {
		panic(err)
	}
	return string(data) + "\n"
}

func writeSyntheticLog(t *testing.T, dir, name string, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadManagedLogsFiltersAndUsesDeterministicFileOrder(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticLog(t, dir, "klg.log", syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", "")+
		syntheticLogLine("2026-10-20T09:00:00Z", "WARN", "command.failed", `"code":"database_open","exit_code":2`))
	writeSyntheticLog(t, dir, "klg.log.1", syntheticLogLine("2026-10-20T11:00:00Z", "ERROR", "collector.failed", `"code":"collector_poll"`))
	since, err := parseLogSince("2026-10-20T08:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	result, err := readManagedLogs(dir, logReadOptions{Since: &since, MinLevel: "warn", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available || len(result.Records) != 2 || result.Truncated {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Records[0].File != "klg.log" || result.Records[0].Event != "command.failed" || result.Records[1].File != "klg.log.1" {
		t.Fatalf("ordering/filter mismatch: %+v", result.Records)
	}
	if result.Records[0].ExitCode == nil || *result.Records[0].ExitCode != 2 {
		t.Fatalf("approved optional field missing: %+v", result.Records[0])
	}
}

func TestReadManagedLogsReportsCorruptUnsupportedOversizedAndPartialEntries(t *testing.T) {
	dir := t.TempDir()
	content := syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", "")
	content += `{ "format_version": 1, "msg": "privacy-marker-unknown" }` + "\n"
	content += `{"time":"2026-10-20T10:00:00Z","format_version":1,"msg":"x","nested":{"secret":"do-not-show"}}` + "\n"
	content += strings.Repeat("x", maxLogReadLineBytes+1) + "\n"
	content += syntheticLogLine("2026-10-20T10:01:00Z", "INFO", "process.finished", `"unknown_future_field":true`)
	content += `{"time":"partial-final"}`
	writeSyntheticLog(t, dir, "klg.log", content)
	result, err := readManagedLogs(dir, logReadOptions{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.InvalidEntries < 3 || result.Unsupported < 1 || !result.Truncated {
		t.Fatalf("untrusted entries were not represented: %+v", result)
	}
	var rendered bytes.Buffer
	if err := writeLogReadResult(&rendered, result, "text"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.String(), "privacy-marker-unknown") || strings.Contains(rendered.String(), "do-not-show") || strings.Contains(rendered.String(), "unknown_future_field") {
		t.Fatalf("reader rendered unapproved data: %q", rendered.String())
	}
}

func TestReadManagedLogsBoundsScanAndResponse(t *testing.T) {
	dir := t.TempDir()
	var content strings.Builder
	for i := 0; i < maxLogReadLimit+4; i++ {
		content.WriteString(syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", ""))
	}
	writeSyntheticLog(t, dir, "klg.log", content.String())
	result, err := readManagedLogs(dir, logReadOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 3 || !result.Truncated || result.BytesScanned > maxLogReadBytes || result.EntriesScanned > maxLogReadEntries {
		t.Fatalf("reader exceeded bounds or hid truncation: %+v", result)
	}
}

func TestReadManagedLogsLimitAtArchiveBoundary(t *testing.T) {
	t.Run("additional archived match is reported", func(t *testing.T) {
		dir := t.TempDir()
		writeSyntheticLog(t, dir, "klg.log", syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", ""))
		writeSyntheticLog(t, dir, "klg.log.1", syntheticLogLine("2026-10-20T09:00:00Z", "INFO", "process.finished", ""))
		result, err := readManagedLogs(dir, logReadOptions{Limit: 1})
		if err != nil || len(result.Records) != 1 || !result.Truncated {
			t.Fatalf("archive boundary match was hidden: result=%+v err=%v", result, err)
		}
	})
	t.Run("exact result count is not reported as truncated", func(t *testing.T) {
		dir := t.TempDir()
		writeSyntheticLog(t, dir, "klg.log", syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", ""))
		result, err := readManagedLogs(dir, logReadOptions{Limit: 1})
		if err != nil || len(result.Records) != 1 || result.Truncated {
			t.Fatalf("exact result count was marked truncated: result=%+v err=%v", result, err)
		}
	})
}

func TestReadManagedLogsEntryBudgetFindsNewestArchivedWarning(t *testing.T) {
	dir := t.TempDir()
	writeMany := func(name string, count int, tail string) {
		t.Helper()
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		writer := bufio.NewWriter(file)
		line := syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", "")
		for i := 0; i < count; i++ {
			if _, err := writer.WriteString(line); err != nil {
				t.Fatal(err)
			}
		}
		if tail != "" {
			if _, err := writer.WriteString(tail); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Flush(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	writeMany("klg.log", 15000, "")
	writeMany("klg.log.1", 15000, syntheticLogLine("2026-10-20T10:01:00Z", "WARN", "collector.failed", `"code":"collector_poll"`))
	result, err := readManagedLogs(dir, logReadOptions{MinLevel: "warn", Limit: 10})
	if err != nil || len(result.Records) != 1 || result.Records[0].Event != "collector.failed" || result.Records[0].File != "klg.log.1" {
		t.Fatalf("newest warning was missed under entry budget: records=%+v entries=%d err=%v", result.Records, result.EntriesScanned, err)
	}
	if result.EntriesScanned > maxLogReadEntries {
		t.Fatalf("entry bounded scan exceeded its cap: entries=%d", result.EntriesScanned)
	}
}

func TestLogReaderAcceptsIssue57CatalogueAndSummaryShape(t *testing.T) {
	for _, fixture := range []struct{ name, line string }{
		{"directory", syntheticLogEvent("2026-10-20T10:00:00Z", "INFO", "data_directory.resolved", "environment", nil)},
		{"settings update", syntheticLogEvent("2026-10-20T10:00:00Z", "INFO", "settings.updated", "settings_update", nil)},
		{"settings load failure", syntheticLogEvent("2026-10-20T10:00:00Z", "WARN", "settings.load_failed", "settings_load", nil)},
		{"health", syntheticLogEvent("2026-10-20T10:00:00Z", "INFO", "collector.health_changed", "collector_health_running", nil)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, status := decodeLogRecord([]byte(strings.TrimSuffix(fixture.line, "\n"))); status != "ok" {
				t.Fatalf("coordinated catalogue entry status=%q line=%s", status, fixture.line)
			}
		})
	}
	fields := make(map[string]any, len(logSummaryFields)+1)
	for _, field := range logSummaryFields {
		fields[field] = uint64(0)
	}
	fields["poll_duration_ms"] = uint64(1234)
	record, status := decodeLogRecord([]byte(strings.TrimSuffix(syntheticLogEvent("2026-10-20T10:00:00Z", "INFO", "collector.summary", "collector_health_running", fields), "\n")))
	if status != "ok" || len(record.Summary) != len(logSummaryFields) || record.PollDurationMS == nil || *record.PollDurationMS != 1234 {
		t.Fatalf("coordinated summary record status=%q record=%+v", status, record)
	}
	fields["latest_poll_imported"] = "0"
	if _, status := decodeLogRecord([]byte(strings.TrimSuffix(syntheticLogEvent("2026-10-20T10:00:00Z", "INFO", "collector.summary", "collector_health_running", fields), "\n"))); status != "invalid" {
		t.Fatalf("string summary count status=%q, want invalid", status)
	}
}

func TestReadManagedLogsStopsAtByteBudget(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", int(maxLogFileBytes))
	for _, name := range managedLogNames {
		writeSyntheticLog(t, dir, name, content)
	}
	result, err := readManagedLogs(dir, logReadOptions{Limit: 10, Deadline: time.Now().Add(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if result.BytesScanned != maxLogReadBytes || result.BytesScanned > maxLogReadBytes || !result.Truncated {
		t.Fatalf("byte budget not enforced/reported: bytes=%d truncated=%t", result.BytesScanned, result.Truncated)
	}
}

func TestLogReadExpiredDeadlineIsVisible(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticLog(t, dir, "klg.log", syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", ""))
	result, err := readManagedLogs(dir, logReadOptions{Limit: 5, Deadline: time.Now().Add(-time.Second)})
	if err != nil || !result.TimedOut || !result.Truncated {
		t.Fatalf("expired deadline was hidden: result=%+v err=%v", result, err)
	}
	var output bytes.Buffer
	if err := writeLogReadResult(&output, result, "text"); err != nil || !strings.Contains(output.String(), "deadline") {
		t.Fatalf("unavailable timeout rendering=(%q,%v)", output.String(), err)
	}
}

func TestLogJSONEnvelopeHasIndependentVersion(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticLog(t, dir, "klg.log", syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", ""))
	result, err := readManagedLogs(dir, logReadOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := writeLogReadResult(&output, result, "json"); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["format_version"] != float64(1) || envelope["available"] != true {
		t.Fatalf("unexpected log inspection envelope: %s", output.String())
	}
}

func TestReadManagedLogsRetentionGapsAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	result, err := readManagedLogs(dir, logReadOptions{Limit: 10})
	if err != nil || result.Available || len(result.MissingFiles) != 4 {
		t.Fatalf("missing-log response: result=%+v err=%v", result, err)
	}
	writeSyntheticLog(t, dir, "klg.log.2", syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", ""))
	result, err = readManagedLogs(dir, logReadOptions{Limit: 10})
	if err != nil || !result.Available || !result.RetentionGap {
		t.Fatalf("retention gap not reported: result=%+v err=%v", result, err)
	}
}

func TestLogsShowDoesNotInitializeWriterOrOpenDatabase(t *testing.T) {
	dir := t.TempDir()
	logPath := writeSyntheticLog(t, dir, "klg.log", syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", ""))
	before, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOKEN_LOGGER_LOG_DIR", dir)
	t.Setenv("TOKEN_LOGGER_DATA_DIR", filepath.Join(t.TempDir(), "unusable-db"))
	var stdout, stderr bytes.Buffer
	if code := runProcess([]string{"logs", "show", "--format", "json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("logs show exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	after, err := os.ReadFile(logPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("inspection changed log contents: err=%v before=%q after=%q", err, before, after)
	}
	if _, err := os.Stat(os.Getenv("TOKEN_LOGGER_DATA_DIR")); !os.IsNotExist(err) {
		t.Fatalf("inspection created/accessed a database directory: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "klg.log" {
		t.Fatalf("inspection created or rotated files: entries=%v err=%v", entries, err)
	}
}

func TestLogsShowHelpDoesNotCreateLogDirectory(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "missing-logs")
	t.Setenv("TOKEN_LOGGER_LOG_DIR", logDir)
	var stdout, stderr bytes.Buffer
	if code := runProcess([]string{"logs", "show", "--help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("logs help exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Fatalf("logs help created log directory: %v", err)
	}
}

func TestLogReaderRejectsSymlinkTargetsWithoutFollowingThem(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outside, []byte(syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", "")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "klg.log")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	result, err := readManagedLogs(dir, logReadOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Available || !result.ReadFailure || len(result.UnreadableFiles) != 1 || result.UnreadableFiles[0] != "klg.log" || len(result.Records) != 0 {
		t.Fatalf("symlink target was not rejected: %+v", result)
	}
}

func TestLogReaderRejectsInvalidArgumentsAndEscapesTerminalControls(t *testing.T) {
	for _, value := range []string{"2026-10-20", "yesterday", "2026-10-20T10:00:00"} {
		if _, err := parseLogSince(value); err == nil {
			t.Errorf("accepted non-RFC3339 timestamp %q", value)
		}
	}
	if _, err := parseLogSince("2026-10-20T10:00:00+05:30"); err != nil {
		t.Fatalf("rejected explicit offset: %v", err)
	}
	if got := escapeLogText("safe\x1b[31m\x7f"); strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\x7f') || !strings.Contains(got, `\u{1B}`) {
		t.Fatalf("terminal controls were not escaped: %q", got)
	}
	var stdout, stderr bytes.Buffer
	if code := runLogs([]string{"show", "--limit", "201"}, &stdout, &stderr, "", os.UserHomeDir); code != 1 || stdout.Len() != 0 {
		t.Fatalf("invalid argument behavior=(%d,%q,%q)", code, stdout.String(), stderr.String())
	}
}

func TestLogReaderResistsConcurrentAppends(t *testing.T) {
	dir := t.TempDir()
	path := writeSyntheticLog(t, dir, "klg.log", syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", ""))
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return
		}
		defer file.Close()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = file.WriteString(syntheticLogLine(time.Now().UTC().Format(time.RFC3339Nano), "INFO", "process.started", ""))
			}
		}
	}()
	for i := 0; i < 10; i++ {
		result, err := readManagedLogs(dir, logReadOptions{Limit: 5})
		if err != nil || result.BytesScanned > maxLogReadBytes || len(result.Records) > 5 {
			t.Fatalf("concurrent read was unsafe: result=%+v err=%v", result, err)
		}
	}
	close(stop)
	<-done
}

func TestLogReaderResistsConcurrentRotation(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "klg.log")
	archive := filepath.Join(dir, "klg.log.1")
	if err := os.WriteFile(active, []byte(syntheticLogLine("2026-10-20T10:00:00Z", "INFO", "process.started", "")), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Remove(archive)
			_ = os.Rename(active, archive)
			_ = os.WriteFile(active, []byte(syntheticLogLine(time.Now().UTC().Format(time.RFC3339Nano), "INFO", "process.started", "")), 0o600)
			if i >= 25 {
				return
			}
		}
	}()
	for i := 0; i < 10; i++ {
		result, err := readManagedLogs(dir, logReadOptions{Limit: 5})
		if err != nil || result.BytesScanned > maxLogReadBytes || len(result.Records) > 5 {
			t.Fatalf("concurrent rotation read was unsafe: result=%+v err=%v", result, err)
		}
	}
	close(stop)
	<-done
}
