// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxLogReadBytes     = int64(20 << 20)
	defaultLogReadBytes = maxLogReadBytes
	defaultLogReadLimit = 50
	maxLogReadLimit     = 200
	maxLogReadLineBytes = 8 << 10
	maxLogReadTextBytes = 256
	maxLogReadEntries   = 25_000
	maxLogReadDepth     = 0
	logReadTimeBudget   = 2 * time.Second
)

var logReadVersion = regexp.MustCompile(`^(unknown|v?[0-9]{1,6}(\.[0-9]{1,6}){2}([+-][A-Za-z0-9.-]{1,32})?)$`)

var managedLogNames = []string{"klg.log", "klg.log.1", "klg.log.2", "klg.log.3"}

type logReadOptions struct {
	Since    *time.Time
	MinLevel string
	Limit    int
	Deadline time.Time
}

type logReadRecord struct {
	Time              string            `json:"time"`
	Level             string            `json:"level"`
	Event             string            `json:"event"`
	CLI               string            `json:"cli_version"`
	Core              string            `json:"core_version"`
	PID               int               `json:"pid"`
	RunID             string            `json:"run_id"`
	Command           string            `json:"command"`
	Code              string            `json:"code,omitempty"`
	ExitCode          *int              `json:"exit_code,omitempty"`
	Count             *uint64           `json:"count,omitempty"`
	PollDurationMS    *uint64           `json:"poll_duration_ms,omitempty"`
	Summary           map[string]uint64 `json:"summary,omitempty"`
	File              string            `json:"file"`
	MetadataTruncated bool              `json:"metadata_truncated"`
	parsedTime        time.Time
	fileRank          int
	lineOrder         uint64
}

type logReadResult struct {
	FormatVersion   int             `json:"format_version"`
	Available       bool            `json:"available"`
	Records         []logReadRecord `json:"records"`
	BytesScanned    int64           `json:"bytes_scanned"`
	EntriesScanned  int             `json:"entries_scanned"`
	ScanLimit       int64           `json:"scan_limit"`
	InvalidEntries  int             `json:"invalid_entries"`
	Unsupported     int             `json:"unsupported_entries"`
	UnreadableFiles []string        `json:"unreadable_files"`
	MissingFiles    []string        `json:"missing_files"`
	RetentionGap    bool            `json:"retention_gap"`
	Truncated       bool            `json:"truncated"`
	TimedOut        bool            `json:"timed_out"`
	RuntimeLimitMS  int64           `json:"runtime_limit_ms"`
	Ordering        string          `json:"ordering"`
	ReadFailure     bool            `json:"-"`
}

type rawLogEnvelope struct {
	Time              string  `json:"time"`
	Level             string  `json:"level"`
	Message           string  `json:"msg"`
	FormatVersion     int     `json:"format_version"`
	CLI               string  `json:"cli_version"`
	Core              string  `json:"core_version"`
	PID               int     `json:"pid"`
	RunID             string  `json:"run_id"`
	Command           string  `json:"command"`
	Code              string  `json:"code"`
	ExitCode          *int    `json:"exit_code"`
	Count             *uint64 `json:"count"`
	MetadataTruncated bool    `json:"metadata_truncated"`
}

type logWindowResult struct {
	Records        []logReadRecord
	BytesScanned   int64
	EntriesScanned int
	Partial        bool
	EntryLimit     bool
	TimedOut       bool
	Trimmed        bool
	ReadFailed     bool
	Invalid        int
	Unsupported    int
}

var knownLogFields = map[string]struct{}{
	"time": {}, "level": {}, "msg": {}, "format_version": {}, "cli_version": {},
	"core_version": {}, "pid": {}, "run_id": {}, "command": {}, "code": {},
	"exit_code": {}, "count": {}, "metadata_truncated": {}, "poll_duration_ms": {},
}

var logSummaryCounters = []string{
	"imported", "duplicates", "ignored_non_usage", "unchanged_snapshot",
	"deferred_incomplete_tail", "unsupported_usage", "malformed_record", "failed_source",
}

var logSummaryFields = func() []string {
	fields := make([]string, 0, len(logSummaryCounters)*2)
	for _, prefix := range []string{"latest_poll", "run_cumulative"} {
		for _, counter := range logSummaryCounters {
			field := prefix + "_" + counter
			knownLogFields[field] = struct{}{}
			fields = append(fields, field)
		}
	}
	return fields
}()

const maxExactJSONInteger = uint64(1<<53 - 1)

func readManagedLogs(dir string, options logReadOptions) (logReadResult, error) {
	result := logReadResult{
		FormatVersion: 1, ScanLimit: defaultLogReadBytes, RuntimeLimitMS: logReadTimeBudget.Milliseconds(), Records: make([]logReadRecord, 0, options.Limit),
		UnreadableFiles: []string{}, MissingFiles: []string{}, Ordering: "file_priority_then_reverse_line; klg.log, .1, .2, .3; not global chronological order",
	}
	if options.Limit < 1 || options.Limit > maxLogReadLimit {
		return result, errors.New("invalid log record limit")
	}
	if options.Deadline.IsZero() {
		options.Deadline = time.Now().Add(logReadTimeBudget)
	}
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		result.MissingFiles = append(result.MissingFiles, managedLogNames...)
		return result, nil
	}
	_, err := inspectLogDirectory(dir)
	if err != nil {
		return result, errors.New("log directory unavailable or unsafe")
	}

	files := make([][]logReadRecord, 0, len(managedLogNames))
	remaining := defaultLogReadBytes
	entriesRemaining := maxLogReadEntries
	for rank, name := range managedLogNames {
		if time.Now().After(options.Deadline) {
			result.TimedOut = true
			result.Truncated = true
			break
		}
		path := filepath.Join(dir, name)
		exists, _, statErr := inspectLogTarget(path)
		if statErr != nil {
			result.UnreadableFiles = append(result.UnreadableFiles, name)
			result.ReadFailure = true
			continue
		}
		if !exists {
			result.MissingFiles = append(result.MissingFiles, name)
			continue
		}
		file, openErr := openReadOnlyLogFile(path)
		if openErr != nil {
			// A concurrent rotation can remove a name between inspection and open.
			if os.IsNotExist(openErr) {
				result.MissingFiles = append(result.MissingFiles, name)
				continue
			}
			result.UnreadableFiles = append(result.UnreadableFiles, name)
			result.ReadFailure = true
			continue
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			result.UnreadableFiles = append(result.UnreadableFiles, name)
			result.ReadFailure = true
			continue
		}
		result.Available = true
		if remaining <= 0 {
			result.Truncated = true
			_ = file.Close()
			continue
		}
		if entriesRemaining <= 0 {
			result.Truncated = true
			_ = file.Close()
			continue
		}
		readSize := info.Size()
		if readSize > maxLogFileBytes {
			readSize = maxLogFileBytes
			result.Truncated = true
		}
		if readSize > remaining {
			readSize = remaining
			result.Truncated = true
		}
		start := info.Size() - readSize
		if start < 0 {
			start = 0
		}
		if start > 0 {
			result.Truncated = true
		}
		window := scanLogWindow(file, name, rank, start, readSize, options, entriesRemaining)
		_ = file.Close()
		result.BytesScanned += window.BytesScanned
		remaining -= window.BytesScanned
		result.EntriesScanned += window.EntriesScanned
		entriesRemaining -= window.EntriesScanned
		if window.EntryLimit {
			result.Truncated = true
		}
		if window.TimedOut {
			result.TimedOut = true
			result.Truncated = true
		}
		result.InvalidEntries += window.Invalid
		result.Unsupported += window.Unsupported
		if window.ReadFailed {
			result.UnreadableFiles = append(result.UnreadableFiles, name)
			result.ReadFailure = true
		}
		if window.Partial || window.BytesScanned < readSize {
			result.Truncated = true
		}
		files = append(files, window.Records)
		if window.Trimmed {
			result.Truncated = true
		}
	}
	if !result.Available {
		// All files missing means unavailable; any unsafe/unopenable file is
		// separately reflected in unreadable_files and the exit status.
		result.RetentionGap = len(result.MissingFiles) != len(managedLogNames) && hasArchiveGap(result.MissingFiles)
	} else {
		result.RetentionGap = hasArchiveGap(result.MissingFiles)
	}
	// The file names encode archive age; physical line order is the only
	// deterministic order available within one process-shared multiprocess file.
	limitReached := false
	for _, records := range files {
		for _, record := range records {
			if len(result.Records) == options.Limit {
				result.Truncated = true
				limitReached = true
				break
			}
			result.Records = append(result.Records, record)
		}
		if limitReached {
			break
		}
	}
	return result, nil
}

func hasArchiveGap(missing []string) bool {
	missingSet := make(map[string]bool, len(missing))
	for _, name := range missing {
		missingSet[name] = true
	}
	seenMissing := false
	for _, name := range managedLogNames[1:] { // oldest to newest archive
		if missingSet[name] {
			seenMissing = true
		} else if seenMissing {
			return true
		}
	}
	return false
}

func scanLogWindow(file *os.File, name string, rank int, start, size int64, options logReadOptions, entryLimit int) logWindowResult {
	result := logWindowResult{Records: make([]logReadRecord, 0, maxLogReadLimit)}
	if time.Now().After(options.Deadline) {
		result.TimedOut = true
		result.Partial = true
		return result
	}
	if size <= 0 {
		return result
	}
	data := make([]byte, int(size)) // size is already capped to one managed file.
	n, err := file.ReadAt(data, start)
	result.BytesScanned = int64(n)
	if err != nil && err != io.EOF {
		result.ReadFailed = true
	}
	if int64(n) < size {
		result.Partial = true
	}
	data = data[:n]
	if start > 0 {
		result.Partial = true
	}
	completeEnd := len(data)
	if completeEnd > 0 && data[completeEnd-1] != '\n' {
		result.Partial = true // Do not present an append/rotation torn tail as a record.
		lastNewline := bytes.LastIndexByte(data, '\n')
		if lastNewline < 0 {
			return result
		}
		completeEnd = lastNewline + 1
	}
	lineEnd := completeEnd
	for lineEnd > 0 {
		if time.Now().After(options.Deadline) {
			result.TimedOut = true
			result.Partial = true
			break
		}
		if result.EntriesScanned >= entryLimit {
			result.EntryLimit = true
			result.Partial = true
			break
		}
		previousNewline := bytes.LastIndexByte(data[:lineEnd-1], '\n')
		lineStart := previousNewline + 1
		line := data[lineStart:lineEnd] // Includes its trailing LF.
		lineEnd = lineStart
		if start > 0 && lineStart == 0 {
			// The scan window began in the middle of this older line.
			result.Partial = true
			break
		}
		result.EntriesScanned++
		if len(line) > maxLogReadLineBytes {
			result.Invalid++
			continue
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		record, status := decodeLogRecord(line)
		if status == "invalid" {
			result.Invalid++
			continue
		}
		if status == "unsupported" {
			result.Unsupported++
			continue
		}
		if !logRecordMatches(record, options) {
			continue
		}
		record.File = name
		record.fileRank = rank
		record.lineOrder = uint64(result.EntriesScanned)
		if len(result.Records) < maxLogReadLimit {
			result.Records = append(result.Records, record)
		} else {
			result.Trimmed = true
		}
	}
	return result
}

func decodeLogRecord(line []byte) (logReadRecord, string) {
	var result logReadRecord
	if len(line) == 0 || len(line) > maxLogReadLineBytes {
		return result, "invalid"
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return result, "invalid"
	}
	seen := make(map[string]bool, 16)
	summaryRaw := make(map[string]json.RawMessage, len(logSummaryFields))
	var durationRaw json.RawMessage
	unknown := false
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return result, "invalid"
		}
		seen[key] = true
		if _, ok := knownLogFields[key]; !ok {
			unknown = true
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return result, "invalid"
		}
		if isLogSummaryField(key) {
			summaryRaw[key] = append(json.RawMessage(nil), value...)
		}
		if key == "poll_duration_ms" {
			durationRaw = append(json.RawMessage(nil), value...)
		}
		var scalar any
		valueDecoder := json.NewDecoder(bytes.NewReader(value))
		valueDecoder.UseNumber()
		if err := valueDecoder.Decode(&scalar); err != nil {
			return result, "invalid"
		}
		if _, composite := scalar.(map[string]any); composite {
			if maxLogReadDepth < 1 {
				return result, "invalid"
			}
		}
		if _, composite := scalar.([]any); composite {
			if maxLogReadDepth < 1 {
				return result, "invalid"
			}
		}
		if !logScalarHasExpectedType(key, scalar) {
			return result, "invalid"
		}
	}
	if _, err = decoder.Token(); err != nil {
		return result, "invalid"
	}
	if token, err = decoder.Token(); err != io.EOF || token != nil {
		return result, "invalid"
	}
	if unknown {
		return result, "unsupported"
	}
	var envelope rawLogEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		return result, "invalid"
	}
	if envelope.FormatVersion != logFormatVersion {
		return result, "unsupported"
	}
	if !requiredLogFields(seen) || len(envelope.Time) > maxLogReadTextBytes || len(envelope.CLI) > maxLogReadTextBytes || len(envelope.Core) > maxLogReadTextBytes || len(envelope.Command) > maxLogReadTextBytes || len(envelope.Message) > maxLogReadTextBytes || len(envelope.RunID) > maxLogReadTextBytes {
		return result, "invalid"
	}
	parsedTime, err := time.Parse(time.RFC3339Nano, envelope.Time)
	if err != nil || envelope.Level != "DEBUG" && envelope.Level != "INFO" && envelope.Level != "WARN" && envelope.Level != "ERROR" {
		return result, "invalid"
	}
	if !logReadVersion.MatchString(envelope.CLI) || !logReadVersion.MatchString(envelope.Core) || envelope.PID <= 0 || len(envelope.RunID) != 32 || !isHexString(envelope.RunID) || !containsString(allowedLogCommands, envelope.Command) {
		return result, "invalid"
	}
	if !containsString(allowedLogEvents, envelope.Message) || envelope.Code != "" && !containsString(allowedLogCodes, envelope.Code) {
		return result, "unsupported"
	}
	if envelope.ExitCode != nil && (*envelope.ExitCode < 0 || *envelope.ExitCode > 255) || envelope.Count != nil && *envelope.Count == 0 {
		return result, "invalid"
	}
	var summary map[string]uint64
	var duration *uint64
	if envelope.Message == "collector.summary" {
		if !isCollectorHealthCodeValue(envelope.Code) || len(summaryRaw) != len(logSummaryFields) {
			return result, "invalid"
		}
		if envelope.Code == "collector_health_running" && envelope.Level != "INFO" || envelope.Code != "collector_health_running" && envelope.Level != "WARN" {
			return result, "invalid"
		}
		summary = make(map[string]uint64, len(logSummaryFields))
		for _, key := range logSummaryFields {
			raw, ok := summaryRaw[key]
			if !ok {
				return result, "invalid"
			}
			value, ok := parseExactLogInteger(raw)
			if !ok {
				return result, "invalid"
			}
			summary[key] = value
		}
		if len(durationRaw) > 0 {
			value, ok := parseExactLogInteger(durationRaw)
			if !ok || value > 24*60*60*1000 {
				return result, "invalid"
			}
			duration = &value
		}
	} else if len(summaryRaw) > 0 || len(durationRaw) > 0 {
		return result, "invalid"
	}
	result = logReadRecord{
		Time: parsedTime.UTC().Format(time.RFC3339Nano), Level: envelope.Level, Event: envelope.Message,
		CLI: envelope.CLI, Core: envelope.Core, PID: envelope.PID, RunID: strings.ToLower(envelope.RunID),
		Command: envelope.Command, Code: envelope.Code, ExitCode: envelope.ExitCode, Count: envelope.Count,
		PollDurationMS: duration, Summary: summary,
		MetadataTruncated: envelope.MetadataTruncated, parsedTime: parsedTime,
	}
	return result, "ok"
}

func logScalarHasExpectedType(key string, value any) bool {
	switch key {
	case "time", "level", "msg", "cli_version", "core_version", "run_id", "command", "code":
		_, ok := value.(string)
		return ok
	case "format_version", "pid", "exit_code", "count":
		_, ok := value.(json.Number)
		return ok
	case "poll_duration_ms":
		_, ok := value.(json.Number)
		return ok
	case "metadata_truncated":
		_, ok := value.(bool)
		return ok
	default:
		if isLogSummaryField(key) {
			_, ok := value.(json.Number)
			return ok
		}
		// New fields are unsupported regardless of their scalar type.
		return value != nil
	}
}

func isLogSummaryField(key string) bool {
	for _, field := range logSummaryFields {
		if key == field {
			return true
		}
	}
	return false
}

func parseExactLogInteger(raw json.RawMessage) (uint64, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return 0, false
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	integer, err := strconv.ParseUint(number.String(), 10, 64)
	return integer, err == nil && integer <= maxExactJSONInteger
}

func isCollectorHealthCodeValue(code string) bool {
	return code == "collector_health_running" || code == "collector_health_degraded" || code == "collector_health_stale"
}

func requiredLogFields(fields map[string]bool) bool {
	for _, name := range []string{"time", "level", "msg", "format_version", "cli_version", "core_version", "pid", "run_id", "command", "metadata_truncated"} {
		if !fields[name] {
			return false
		}
	}
	return true
}

var allowedLogEvents = []string{
	"process.started", "process.finished", "command.failed", "collector.started", "collector.stopped",
	"collector.failed", "logging.unavailable", "logging.events_suppressed", "logging.events_dropped",
	"data_directory.resolved", "collector.health_changed", "collector.summary", "settings.updated", "settings.load_failed",
}
var allowedLogCodes = []string{
	"database_open", "settings_load", "collector_acquire", "collector_heartbeat", "collector_poll", "collector_store", "log_write",
	"settings_update", "onboarding_save", "data_directory", "report_query", "report_render", "process_launch",
	"collector_readiness", "collector_release", "collector_stop", "collector_health_running", "collector_health_degraded",
	"collector_health_stale", "explicit", "environment", "current_default", "legacy_default", "import_discovery",
	"import_source", "protocol_record",
}
var allowedLogCommands = []string{"interactive", "version", "harness", "completion", "import", "projects", "sessions", "usage", "cost", "watch", "settings", "report-issue", "other"}

func logRecordMatches(record logReadRecord, options logReadOptions) bool {
	if options.Since != nil && record.parsedTime.Before(*options.Since) {
		return false
	}
	if options.MinLevel != "" && logSeverity(record.Level) < logSeverity(strings.ToUpper(options.MinLevel)) {
		return false
	}
	return true
}

func logSeverity(level string) int {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return 0
	case "INFO":
		return 1
	case "WARN", "WARNING":
		return 2
	case "ERROR":
		return 3
	default:
		return -1
	}
}

func parseLogSince(value string) (time.Time, error) {
	if len(value) > maxLogReadTextBytes {
		return time.Time{}, errors.New("timestamp too long")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("expected RFC3339 timestamp with an explicit UTC offset")
	}
	return parsed.UTC(), nil
}

func escapeLogText(value string) string {
	value = strings.ToValidUTF8(value, "�")
	var builder strings.Builder
	for _, r := range value {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			fmt.Fprintf(&builder, "\\u{%X}", r)
		} else {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func writeLogReadResult(output io.Writer, result logReadResult, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetEscapeHTML(true)
		return encoder.Encode(result)
	}
	if !result.Available {
		if result.TimedOut {
			_, err := fmt.Fprintln(output, "Logs unavailable: inspection work deadline reached before a readable file was confirmed.")
			return err
		}
		message := "Logs unavailable: no managed log files found."
		if len(result.UnreadableFiles) > 0 {
			message = "Logs unavailable: no managed log files could be read."
		}
		_, err := fmt.Fprintln(output, message)
		return err
	}
	if len(result.Records) == 0 {
		if _, err := fmt.Fprintln(output, "No matching log entries."); err != nil {
			return err
		}
	}
	for _, record := range result.Records {
		_, err := fmt.Fprintf(output, "%s %-5s %-28s command=%s", escapeLogText(record.Time), escapeLogText(record.Level), escapeLogText(record.Event), escapeLogText(record.Command))
		if err != nil {
			return err
		}
		if record.Code != "" {
			if _, err := fmt.Fprintf(output, " code=%s", escapeLogText(record.Code)); err != nil {
				return err
			}
		}
		if record.ExitCode != nil {
			if _, err := fmt.Fprintf(output, " exit_code=%d", *record.ExitCode); err != nil {
				return err
			}
		}
		if record.Count != nil {
			if _, err := fmt.Fprintf(output, " count=%d", *record.Count); err != nil {
				return err
			}
		}
		if record.PollDurationMS != nil {
			if _, err := fmt.Fprintf(output, " poll_duration_ms=%d", *record.PollDurationMS); err != nil {
				return err
			}
		}
		for _, key := range logSummaryFields {
			if value, ok := record.Summary[key]; ok {
				if _, err := fmt.Fprintf(output, " %s=%d", key, value); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintf(output, " file=%s\n", record.File); err != nil {
			return err
		}
	}
	if result.Truncated || result.TimedOut || result.RetentionGap || result.InvalidEntries > 0 || result.Unsupported > 0 || len(result.UnreadableFiles) > 0 {
		_, err := fmt.Fprintf(output, "Inspection incomplete: scanned=%d/%d bytes; entries=%d; invalid=%d; unsupported=%d; retention_gap=%t; truncated=%t; timed_out=%t; unreadable_files=%d\n", result.BytesScanned, result.ScanLimit, result.EntriesScanned, result.InvalidEntries, result.Unsupported, result.RetentionGap, result.Truncated, result.TimedOut, len(result.UnreadableFiles))
		return err
	}
	return nil
}

func runLogs(args []string, stdout, stderr io.Writer, logDirOverride string, home func() (string, error)) int {
	if len(args) == 0 || args[0] != "show" {
		fmt.Fprintln(stderr, "usage: komposyn-lg logs show [--since <RFC3339>] [--level <debug|info|warn|error>] [--limit <1-200>] [--format text|json]")
		return 1
	}
	options := logReadOptions{Limit: defaultLogReadLimit}
	format := "text"
	seen := make(map[string]bool)
	for index := 1; index < len(args); index++ {
		name := args[index]
		if name != "--since" && name != "--level" && name != "--limit" && name != "--format" {
			fmt.Fprintf(stderr, "unknown logs show option %q\n", name)
			return 1
		}
		if seen[name] || index+1 >= len(args) {
			fmt.Fprintln(stderr, "invalid or repeated logs show option")
			return 1
		}
		seen[name] = true
		value := args[index+1]
		index++
		switch name {
		case "--since":
			parsed, err := parseLogSince(value)
			if err != nil {
				fmt.Fprintf(stderr, "invalid --since: %v\n", err)
				return 1
			}
			options.Since = &parsed
		case "--level":
			value = strings.ToLower(value)
			if logSeverity(value) < 0 {
				fmt.Fprintln(stderr, "invalid --level: choose debug, info, warn, or error")
				return 1
			}
			options.MinLevel = value
		case "--limit":
			limit, err := strconv.Atoi(value)
			if err != nil || limit < 1 || limit > maxLogReadLimit {
				fmt.Fprintf(stderr, "invalid --limit: choose 1 through %d\n", maxLogReadLimit)
				return 1
			}
			options.Limit = limit
		case "--format":
			if value != "text" && value != "json" {
				fmt.Fprintln(stderr, "invalid --format: choose text or json")
				return 1
			}
			format = value
		}
	}
	dir, err := resolveLogDirectory(logDirOverride, home)
	if err != nil {
		result := logReadResult{FormatVersion: 1, ScanLimit: defaultLogReadBytes, RuntimeLimitMS: logReadTimeBudget.Milliseconds(), Records: []logReadRecord{}, MissingFiles: append([]string(nil), managedLogNames...), Ordering: "file_priority_then_reverse_line; not global chronological order"}
		if err := writeLogReadResult(stdout, result, format); err != nil {
			fmt.Fprintln(stderr, "write log inspection output failed")
			return 2
		}
		return 0
	}
	result, err := readManagedLogs(dir, options)
	if err != nil {
		fmt.Fprintln(stderr, "log inspection unavailable: log directory is unsafe or cannot be read")
		return 2
	}
	if err := writeLogReadResult(stdout, result, format); err != nil {
		fmt.Fprintln(stderr, "write log inspection output failed")
		return 2
	}
	if result.ReadFailure {
		fmt.Fprintln(stderr, "some managed log files could not be read")
		return 2
	}
	return 0
}
