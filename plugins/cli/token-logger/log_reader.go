// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
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
	Time              string  `json:"time"`
	Level             string  `json:"level"`
	Event             string  `json:"event"`
	CLI               string  `json:"cli_version"`
	Core              string  `json:"core_version"`
	PID               int     `json:"pid"`
	RunID             string  `json:"run_id"`
	Command           string  `json:"command"`
	Code              string  `json:"code,omitempty"`
	ExitCode          *int    `json:"exit_code,omitempty"`
	Count             *uint64 `json:"count,omitempty"`
	File              string  `json:"file"`
	MetadataTruncated bool    `json:"metadata_truncated"`
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

var knownLogFields = map[string]struct{}{
	"time": {}, "level": {}, "msg": {}, "format_version": {}, "cli_version": {},
	"core_version": {}, "pid": {}, "run_id": {}, "command": {}, "code": {},
	"exit_code": {}, "count": {}, "metadata_truncated": {},
}

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
		windowRecords, scanned, entriesScanned, partial, hitEntryLimit, timedOut, ringTrimmed, readFailed, invalid, unsupported := scanLogWindow(file, name, rank, start, readSize, options, entriesRemaining)
		_ = file.Close()
		result.BytesScanned += scanned
		remaining -= scanned
		result.EntriesScanned += entriesScanned
		entriesRemaining -= entriesScanned
		if hitEntryLimit {
			result.Truncated = true
		}
		if timedOut {
			result.TimedOut = true
			result.Truncated = true
		}
		result.InvalidEntries += invalid
		result.Unsupported += unsupported
		if readFailed {
			result.UnreadableFiles = append(result.UnreadableFiles, name)
			result.ReadFailure = true
		}
		if partial || scanned < readSize {
			result.Truncated = true
		}
		files = append(files, windowRecords)
		if ringTrimmed {
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
	for _, records := range files {
		for _, record := range records {
			if len(result.Records) == options.Limit {
				result.Truncated = true
				break
			}
			result.Records = append(result.Records, record)
		}
		if len(result.Records) == options.Limit {
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

func scanLogWindow(file *os.File, name string, rank int, start, size int64, options logReadOptions, entryLimit int) ([]logReadRecord, int64, int, bool, bool, bool, bool, bool, int, int) {
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, 0, 0, true, false, false, false, true, 0, 0
	}
	reader := bufio.NewReaderSize(io.LimitReader(file, size), 4096)
	var scanned int64
	partialStart := start > 0
	if partialStart {
		_, complete, _, consumed, err := readBoundedLogLine(reader)
		scanned += consumed
		if err != nil && err != io.EOF {
			return nil, scanned, 0, true, false, false, false, true, 0, 0
		}
		if !complete {
			return nil, scanned, 0, true, false, false, false, false, 0, 0
		}
	}
	lineOrder := uint64(0)
	ring := newLogRecordRing(maxLogReadLimit)
	invalid, unsupported := 0, 0
	partialEnd := false
	readFailed := false
	entriesScanned := 0
	hitEntryLimit := false
	timedOut := false
	for {
		if time.Now().After(options.Deadline) {
			timedOut = true
			break
		}
		if entriesScanned >= entryLimit {
			hitEntryLimit = true
			break
		}
		line, complete, oversized, consumed, err := readBoundedLogLine(reader)
		scanned += consumed
		if err == io.EOF && len(line) == 0 && consumed == 0 {
			break
		}
		if err != nil && err != io.EOF {
			readFailed = true
			break
		}
		if !complete {
			partialEnd = true
			break
		}
		entriesScanned++
		lineOrder++
		if oversized {
			invalid++
			continue
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		record, status := decodeLogRecord(line)
		if status == "invalid" {
			invalid++
			continue
		}
		if status == "unsupported" {
			unsupported++
			continue
		}
		if !logRecordMatches(record, options) {
			continue
		}
		record.File = name
		record.fileRank = rank
		record.lineOrder = lineOrder
		ring.Push(record)
	}
	return ring.NewestFirst(), scanned, entriesScanned, partialStart || partialEnd, hitEntryLimit, timedOut, ring.overflowed, readFailed, invalid, unsupported
}

func readBoundedLogLine(reader *bufio.Reader) ([]byte, bool, bool, int64, error) {
	line := make([]byte, 0, 256)
	var consumed int64
	oversized := false
	for {
		part, err := reader.ReadSlice('\n')
		consumed += int64(len(part))
		if !oversized {
			if len(line)+len(part) > maxLogReadLineBytes {
				oversized = true
				line = nil
			} else {
				line = append(line, part...)
			}
		}
		if err == nil {
			return line, true, oversized, consumed, nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			if consumed == 0 {
				return nil, false, oversized, 0, io.EOF
			}
			return line, false, oversized, consumed, nil
		}
		return line, false, oversized, consumed, err
	}
}

type logRecordRing struct {
	items      []*logReadRecord
	next       int
	count      int
	overflowed bool
}

func newLogRecordRing(size int) *logRecordRing {
	return &logRecordRing{items: make([]*logReadRecord, size)}
}

func (ring *logRecordRing) Push(record logReadRecord) {
	copy := record
	ring.items[ring.next] = &copy
	if ring.count == len(ring.items) {
		ring.overflowed = true
	}
	ring.next = (ring.next + 1) % len(ring.items)
	if ring.count < len(ring.items) {
		ring.count++
	}
}

func (ring *logRecordRing) NewestFirst() []logReadRecord {
	items := make([]logReadRecord, 0, ring.count)
	index := ring.next - 1
	if index < 0 {
		index = len(ring.items) - 1
	}
	for i := 0; i < ring.count; i++ {
		if record := ring.items[index]; record != nil {
			items = append(items, *record)
		}
		index--
		if index < 0 {
			index = len(ring.items) - 1
		}
	}
	return items
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
	result = logReadRecord{
		Time: parsedTime.UTC().Format(time.RFC3339Nano), Level: envelope.Level, Event: envelope.Message,
		CLI: envelope.CLI, Core: envelope.Core, PID: envelope.PID, RunID: strings.ToLower(envelope.RunID),
		Command: envelope.Command, Code: envelope.Code, ExitCode: envelope.ExitCode, Count: envelope.Count,
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
	case "metadata_truncated":
		_, ok := value.(bool)
		return ok
	default:
		// New fields are unsupported regardless of their scalar type.
		return value != nil
	}
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
}
var allowedLogCodes = []string{"database_open", "settings_load", "collector_acquire", "collector_heartbeat", "collector_poll", "collector_store", "log_write"}
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
