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
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const (
	logFormatVersion = 1
	maxLogFileBytes  = int64(5 << 20)
	maxLogArchives   = 3
	maxLogEventBytes = 8 << 10
	logQueueCapacity = 256
	logBatchSize     = 64
	maxLogKeyState   = 64
	logLockBudget    = 50 * time.Millisecond
	logCloseBudget   = 500 * time.Millisecond
	logRepeatWindow  = time.Second
)

var logVersionPattern = regexp.MustCompile(`^v?[0-9]{1,6}(\.[0-9]{1,6}){2}([+-][A-Za-z0-9.-]{1,32})?$`)

type appLogEvent uint8

const (
	appLogProcessStarted appLogEvent = iota + 1
	appLogProcessFinished
	appLogCommandFailed
	appLogCollectorStarted
	appLogCollectorStopped
	appLogCollectorFailed
	appLogSinkUnavailable
	appLogEventsSuppressed
	appLogEventsDropped
)

func (event appLogEvent) String() string {
	switch event {
	case appLogProcessStarted:
		return "process.started"
	case appLogProcessFinished:
		return "process.finished"
	case appLogCommandFailed:
		return "command.failed"
	case appLogCollectorStarted:
		return "collector.started"
	case appLogCollectorStopped:
		return "collector.stopped"
	case appLogCollectorFailed:
		return "collector.failed"
	case appLogSinkUnavailable:
		return "logging.unavailable"
	case appLogEventsSuppressed:
		return "logging.events_suppressed"
	case appLogEventsDropped:
		return "logging.events_dropped"
	default:
		return ""
	}
}

type appLogCode uint8

const (
	appLogCodeNone appLogCode = iota
	appLogCodeDatabaseOpen
	appLogCodeSettingsLoad
	appLogCodeCollectorAcquire
	appLogCodeCollectorHeartbeat
	appLogCodeCollectorPoll
	appLogCodeCollectorStore
	appLogCodeLogWrite
)

func (code appLogCode) String() string {
	switch code {
	case appLogCodeDatabaseOpen:
		return "database_open"
	case appLogCodeSettingsLoad:
		return "settings_load"
	case appLogCodeCollectorAcquire:
		return "collector_acquire"
	case appLogCodeCollectorHeartbeat:
		return "collector_heartbeat"
	case appLogCodeCollectorPoll:
		return "collector_poll"
	case appLogCodeCollectorStore:
		return "collector_store"
	case appLogCodeLogWrite:
		return "log_write"
	default:
		return ""
	}
}

type appCommand uint8

const (
	appCommandInteractive appCommand = iota + 1
	appCommandVersion
	appCommandHarness
	appCommandCompletion
	appCommandImport
	appCommandProjects
	appCommandSessions
	appCommandUsage
	appCommandCost
	appCommandWatch
	appCommandSettings
	appCommandReportIssue
	appCommandOther
)

func commandForLog(args []string) appCommand {
	if len(args) == 0 {
		return appCommandInteractive
	}
	switch args[0] {
	case "version", "--version":
		return appCommandVersion
	case "harness":
		return appCommandHarness
	case "completion":
		return appCommandCompletion
	case "import":
		return appCommandImport
	case "projects":
		return appCommandProjects
	case "sessions":
		return appCommandSessions
	case "usage":
		return appCommandUsage
	case "cost":
		return appCommandCost
	case "watch", "_watch-lifecycle":
		return appCommandWatch
	case "settings":
		return appCommandSettings
	case "report-issue":
		return appCommandReportIssue
	default:
		return appCommandOther
	}
}

func (command appCommand) String() string {
	switch command {
	case appCommandInteractive:
		return "interactive"
	case appCommandVersion:
		return "version"
	case appCommandHarness:
		return "harness"
	case appCommandCompletion:
		return "completion"
	case appCommandImport:
		return "import"
	case appCommandProjects:
		return "projects"
	case appCommandSessions:
		return "sessions"
	case appCommandUsage:
		return "usage"
	case appCommandCost:
		return "cost"
	case appCommandWatch:
		return "watch"
	case appCommandSettings:
		return "settings"
	case appCommandReportIssue:
		return "report-issue"
	case appCommandOther:
		return "other"
	default:
		return ""
	}
}

// applicationLog is deliberately narrower than slog.Logger. Callers can emit
// only enumerated lifecycle events and error classes; raw arguments, paths,
// errors, source records, and arbitrary attributes have no API here.
type applicationLog interface {
	Event(appLogEvent, appLogCode, appCommand, int)
	Unavailable() bool
	Close()
}

type discardApplicationLog struct{}

func (discardApplicationLog) Event(appLogEvent, appLogCode, appCommand, int) {}
func (discardApplicationLog) Close()                                         {}
func (discardApplicationLog) Unavailable() bool                              { return false }

type unavailableApplicationLog struct{}

func (unavailableApplicationLog) Event(appLogEvent, appLogCode, appCommand, int) {}
func (unavailableApplicationLog) Close()                                         {}
func (unavailableApplicationLog) Unavailable() bool                              { return true }

type logKey struct {
	event   appLogEvent
	code    appLogCode
	command appCommand
}

var overflowLogKey = logKey{event: appLogEventsSuppressed, command: appCommandOther}

type eventLogger struct {
	sink              *jsonlLogSink
	cliVersion        string
	coreVersion       string
	metadataTruncated bool
	pid               int
	runID             string
	queue             chan []byte
	stop              chan struct{}
	done              chan struct{}
	mu                sync.Mutex
	encodeMu          sync.Mutex
	encodeBuffer      bytes.Buffer
	encoder           *slog.Logger
	closed            bool
	closeBy           time.Time
	last              map[logKey]time.Time
	suppressed        map[logKey]uint64
	dropped           atomic.Uint64
	sinkFailed        atomic.Bool
}

func newEventLogger(dir, cli, core string, pid int, runID string) (*eventLogger, error) {
	if runID == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, errors.New("generate process run identifier")
		}
		runID = hex.EncodeToString(id[:])
	}
	if len(runID) != 32 || !isHexString(runID) {
		return nil, errors.New("invalid process run identifier")
	}
	sink, err := newJSONLLogSink(dir)
	if err != nil {
		return nil, err
	}
	last := make(map[logKey]time.Time, maxLogKeyState)
	last[overflowLogKey] = time.Time{}
	cliValue, cliTruncated := boundedVersion(cli)
	coreValue, coreTruncated := boundedVersion(core)
	logger := &eventLogger{
		sink: sink, cliVersion: cliValue, coreVersion: coreValue, metadataTruncated: cliTruncated || coreTruncated, pid: pid, runID: runID,
		queue: make(chan []byte, logQueueCapacity), stop: make(chan struct{}), done: make(chan struct{}),
		last: last, suppressed: make(map[logKey]uint64, maxLogKeyState),
	}
	logger.encoder = newLogEncoder(&logger.encodeBuffer)
	go logger.run()
	return logger, nil
}

func newProcessLogger(stderr io.Writer) applicationLog {
	dir, err := resolveLogDirectory(os.Getenv("TOKEN_LOGGER_LOG_DIR"), os.UserHomeDir)
	if err == nil {
		var logger *eventLogger
		logger, err = newEventLogger(dir, cliVersion, tokenlogger.CoreVersion(), os.Getpid(), "")
		if err == nil {
			return logger
		}
	}
	if stderr != nil {
		_, _ = io.WriteString(stderr, "warning: local troubleshooting log unavailable\n")
	}
	return unavailableApplicationLog{}
}

func resolveLogDirectory(override string, home func() (string, error)) (string, error) {
	if override != "" {
		path, err := filepath.Abs(override)
		if err != nil {
			return "", errors.New("resolve configured log directory")
		}
		return filepath.Clean(path), nil
	}
	homeDir, err := home()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return "", errors.New("home directory unavailable")
	}
	return filepath.Join(homeDir, ".komposyn", "token-logger"), nil
}

func boundedVersion(value string) (string, bool) {
	if value == "" {
		return "unknown", false
	}
	truncated := len(value) > 64
	if truncated || !logVersionPattern.MatchString(value) {
		return "unknown", truncated
	}
	return value, false
}

func isHexString(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func (logger *eventLogger) Event(event appLogEvent, code appLogCode, command appCommand, exitCode int) {
	if logger == nil || event.String() == "" || code.String() == "" && code != appLogCodeNone || command.String() == "" {
		return
	}
	key := logKey{event: event, code: code, command: command}
	now := time.Now().UTC()
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return
	}
	if _, ok := logger.last[key]; !ok && len(logger.last) >= maxLogKeyState-1 {
		key = overflowLogKey
	}
	if last, ok := logger.last[key]; ok && !last.IsZero() && now.Sub(last) < logRepeatWindow {
		logger.suppressed[key]++
		return
	}
	logger.last[key] = now
	line := logger.encode(event, code, command, exitCode, 0)
	if len(line) == 0 || len(line) > maxLogEventBytes || line[len(line)-1] != '\n' {
		logger.dropped.Add(1)
		return
	}
	select {
	case logger.queue <- line:
	default:
		logger.dropped.Add(1)
	}
}

func (logger *eventLogger) encode(event appLogEvent, code appLogCode, command appCommand, exitCode int, count uint64) []byte {
	logger.encodeMu.Lock()
	defer logger.encodeMu.Unlock()
	logger.encodeBuffer.Reset()
	level := slog.LevelInfo
	if event == appLogCollectorFailed || event == appLogSinkUnavailable || event == appLogCommandFailed || event == appLogProcessFinished && exitCode != 0 {
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		slog.Int("format_version", logFormatVersion),
		slog.String("cli_version", logger.cliVersion),
		slog.String("core_version", logger.coreVersion),
		slog.Int("pid", logger.pid),
		slog.String("run_id", logger.runID),
		slog.String("command", command.String()),
		slog.Bool("metadata_truncated", logger.metadataTruncated),
	}
	if code != appLogCodeNone {
		attrs = append(attrs, slog.String("code", code.String()))
	}
	if event == appLogProcessFinished || event == appLogCommandFailed {
		attrs = append(attrs, slog.Int("exit_code", exitCode))
	}
	if event == appLogEventsSuppressed || event == appLogEventsDropped {
		attrs = append(attrs, slog.Uint64("count", count))
	}
	logger.encoder.LogAttrs(context.Background(), level, event.String(), attrs...)
	if logger.encodeBuffer.Len() > maxLogEventBytes {
		return nil
	}
	return append([]byte(nil), logger.encodeBuffer.Bytes()...)
}

func newLogEncoder(output io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, attr.Value.Time().UTC().Format(time.RFC3339Nano))
			}
			return attr
		},
	})
	return slog.New(handler)
}

func (logger *eventLogger) run() {
	defer close(logger.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case first := <-logger.queue:
			logger.writeBatch(logger.takeBatch(first))
		case <-ticker.C:
			logger.flushSummaries()
		case <-logger.stop:
			logger.mu.Lock()
			deadline := logger.closeBy
			logger.mu.Unlock()
			for time.Now().Before(deadline) {
				batch := logger.takeAvailableBatch()
				if len(batch) == 0 {
					logger.flushSummaries()
					return
				}
				logger.writeBatch(batch)
			}
			logger.dropped.Add(uint64(len(logger.queue)))
			return
		}
	}
}

func (logger *eventLogger) takeBatch(first []byte) [][]byte {
	batch := make([][]byte, 1, logBatchSize)
	batch[0] = first
	for len(batch) < logBatchSize {
		select {
		case line := <-logger.queue:
			batch = append(batch, line)
		default:
			return batch
		}
	}
	return batch
}

func (logger *eventLogger) takeAvailableBatch() [][]byte {
	batch := make([][]byte, 0, logBatchSize)
	for len(batch) < logBatchSize {
		select {
		case line := <-logger.queue:
			batch = append(batch, line)
		default:
			return batch
		}
	}
	return batch
}

func (logger *eventLogger) writeBatch(batch [][]byte) {
	if len(batch) == 0 {
		return
	}
	if err := logger.sink.appendBatch(batch); err != nil {
		logger.sinkFailed.Store(true)
		logger.dropped.Add(uint64(len(batch)))
	}
}

func (logger *eventLogger) flushSummaries() {
	logger.mu.Lock()
	suppressed := logger.suppressed
	logger.suppressed = make(map[logKey]uint64, 16)
	logger.mu.Unlock()
	lines := make([][]byte, 0, len(suppressed)+1)
	type summary struct {
		key   logKey
		count uint64
	}
	summaries := make([]summary, 0, len(suppressed))
	for key, count := range suppressed {
		summaries = append(summaries, summary{key: key, count: count})
		lines = append(lines, logger.encode(appLogEventsSuppressed, key.code, key.command, 0, count))
	}
	dropped := logger.dropped.Swap(0)
	if dropped > 0 {
		lines = append(lines, logger.encode(appLogEventsDropped, appLogCodeNone, appCommandOther, 0, dropped))
	}
	if len(lines) > 0 {
		if err := logger.sink.appendBatch(lines); err != nil {
			logger.sinkFailed.Store(true)
			failure := batchAppendProgress(err)
			firstRetry := failure.completed
			if failure.uncertain {
				// The failed line may be present in full despite its write error.
				// Do not replay it; account for its represented observations as
				// dropped evidence, then retry only later, definitely unwritten lines.
				if firstRetry < len(summaries) {
					logger.dropped.Add(summaries[firstRetry].count)
				} else if dropped > 0 && firstRetry == len(summaries) {
					logger.dropped.Add(dropped)
				}
				firstRetry++
			}
			logger.mu.Lock()
			for index := firstRetry; index < len(summaries); index++ {
				key, count := summaries[index].key, summaries[index].count
				logger.suppressed[key] = saturatingAdd(logger.suppressed[key], count)
			}
			logger.mu.Unlock()
			if dropped > 0 && firstRetry <= len(summaries) {
				logger.dropped.Add(dropped)
			}
		}
	}
}

func saturatingAdd(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func (logger *eventLogger) Unavailable() bool {
	return logger != nil && logger.sinkFailed.Load()
}

func (logger *eventLogger) Close() {
	if logger == nil {
		return
	}
	logger.mu.Lock()
	if logger.closed {
		logger.mu.Unlock()
		return
	}
	logger.closed = true
	logger.closeBy = time.Now().Add(logCloseBudget)
	close(logger.stop)
	logger.mu.Unlock()
	timer := time.NewTimer(logCloseBudget)
	defer timer.Stop()
	select {
	case <-logger.done:
	case <-timer.C:
	}
}

type jsonlLogSink struct {
	dir       string
	active    string
	lockPath  string
	maxFile   int64
	archiveN  int
	lockWait  time.Duration
	lockSleep time.Duration
	writeFile func(*os.File, []byte) (int, error)
}

type jsonlBatchWriteError struct {
	cause     error
	completed int
	uncertain bool
}

func (failure *jsonlBatchWriteError) Error() string { return failure.cause.Error() }
func (failure *jsonlBatchWriteError) Unwrap() error { return failure.cause }

func batchAppendProgress(err error) jsonlBatchWriteError {
	var failure *jsonlBatchWriteError
	if errors.As(err, &failure) {
		return *failure
	}
	return jsonlBatchWriteError{cause: err, completed: 0}
}

func newJSONLLogSink(dir string) (*jsonlLogSink, error) {
	if dir == "" {
		return nil, errors.New("empty log directory")
	}
	if err := secureLogDirectory(dir); err != nil {
		return nil, err
	}
	return &jsonlLogSink{
		dir: dir, active: filepath.Join(dir, "klg.log"), lockPath: filepath.Join(dir, "klg.log.lock"),
		maxFile: maxLogFileBytes, archiveN: maxLogArchives, lockWait: logLockBudget, lockSleep: 5 * time.Millisecond,
	}, nil
}

func (sink *jsonlLogSink) appendBatch(lines [][]byte) error {
	if len(lines) == 0 {
		return nil
	}
	for _, line := range lines {
		if len(line) == 0 || len(line) > maxLogEventBytes || line[len(line)-1] != '\n' {
			return errors.New("invalid bounded log record")
		}
	}
	completed := 0
	uncertain := false
	err := sink.withLock(func() error {
		if err := secureLogDirectory(sink.dir); err != nil {
			return err
		}
		if err := sink.recoverInterruptedRotation(); err != nil {
			return err
		}
		active, err := openPrivateLogFile(sink.active, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer func() {
			if active != nil {
				_ = active.Close()
			}
		}()
		if err := trimTornLogTail(active); err != nil {
			_ = active.Close()
			return err
		}
		info, err := active.Stat()
		if err != nil {
			_ = active.Close()
			return err
		}
		if info.Size() > sink.maxFile {
			if err := trimLogFileToLimit(active, sink.maxFile); err != nil {
				_ = active.Close()
				return err
			}
			info, err = active.Stat()
			if err != nil {
				_ = active.Close()
				return err
			}
		}
		size := info.Size()
		if _, err := active.Seek(0, io.SeekEnd); err != nil {
			return err
		}
		for _, line := range lines {
			if size+int64(len(line)) > sink.maxFile {
				if err := active.Close(); err != nil {
					active = nil
					return err
				}
				active = nil
				if err := sink.rotate(); err != nil {
					return err
				}
				active, err = openPrivateLogFile(sink.active, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
				if err != nil {
					return err
				}
				size = 0
			}
			write := sink.writeFile
			if write == nil {
				write = (*os.File).Write
			}
			n, writeErr := write(active, line)
			if writeErr != nil || n != len(line) {
				truncateErr := active.Truncate(size)
				_ = active.Close()
				active = nil
				if truncateErr != nil {
					uncertain = true
				}
				if writeErr != nil {
					return errors.Join(writeErr, truncateErr)
				}
				return errors.Join(io.ErrShortWrite, truncateErr)
			}
			size += int64(n)
			completed++
		}
		closeErr := active.Close()
		active = nil
		return closeErr
	})
	if err != nil {
		return &jsonlBatchWriteError{cause: err, completed: completed, uncertain: uncertain}
	}
	return nil
}

func (sink *jsonlLogSink) withLock(fn func() error) error {
	if err := secureLogDirectory(sink.dir); err != nil {
		return err
	}
	lockFile, err := openPrivateLogFile(sink.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(sink.lockWait)
	for {
		locked, lockErr := tryLockSettingsFile(lockFile)
		if lockErr != nil {
			_ = lockFile.Close()
			return errors.New("log lock unavailable")
		}
		if locked {
			break
		}
		if !time.Now().Before(deadline) {
			_ = lockFile.Close()
			return errors.New("log lock wait exceeded")
		}
		time.Sleep(sink.lockSleep)
	}
	defer func() {
		_ = unlockSettingsFile(lockFile)
		_ = lockFile.Close()
	}()
	return fn()
}

func (sink *jsonlLogSink) archivePath(index int) string {
	return fmt.Sprintf("%s.%d", sink.active, index)
}

func (sink *jsonlLogSink) recoverInterruptedRotation() error {
	if err := sink.validateTargets(); err != nil {
		return err
	}
	if err := sink.pruneLegacyArchives(); err != nil {
		return err
	}
	for index := 1; index <= sink.archiveN; index++ {
		archive := sink.archivePath(index)
		exists, size, err := inspectLogTarget(archive)
		if err != nil || !exists {
			if err != nil {
				return err
			}
			continue
		}
		file, err := openPrivateLogFile(archive, os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		if err := trimTornLogTail(file); err != nil {
			_ = file.Close()
			return err
		}
		if size > sink.maxFile {
			if err := trimLogFileToLimit(file, sink.maxFile); err != nil {
				_ = file.Close()
				return err
			}
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	activeExists, _, err := inspectLogTarget(sink.active)
	if err != nil {
		return err
	}
	if activeExists {
		return nil
	}
	first := sink.archivePath(1)
	firstExists, _, err := inspectLogTarget(first)
	if err != nil {
		return err
	}
	if firstExists {
		return renameLogFile(first, sink.active)
	}
	return nil
}

func (sink *jsonlLogSink) pruneLegacyArchives() error {
	entries, err := os.ReadDir(sink.dir)
	if err != nil {
		return errors.New("read log directory")
	}
	prefix := filepath.Base(sink.active) + "."
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		suffix := strings.TrimPrefix(entry.Name(), prefix)
		index, parseErr := strconv.Atoi(suffix)
		if parseErr != nil || index <= sink.archiveN {
			continue
		}
		path := filepath.Join(sink.dir, entry.Name())
		if _, _, err := inspectLogTarget(path); err != nil {
			return err
		}
		if err := removeLogFile(path); err != nil {
			return err
		}
	}
	return nil
}

func (sink *jsonlLogSink) validateTargets() error {
	if _, err := inspectLogDirectory(sink.dir); err != nil {
		return err
	}
	if _, _, err := inspectLogTarget(sink.active); err != nil {
		return err
	}
	if _, _, err := inspectLogTarget(sink.lockPath); err != nil {
		return err
	}
	for index := 1; index <= sink.archiveN; index++ {
		if _, _, err := inspectLogTarget(sink.archivePath(index)); err != nil {
			return err
		}
	}
	return nil
}

func (sink *jsonlLogSink) rotate() error {
	if err := sink.validateTargets(); err != nil {
		return err
	}
	oldest := sink.archivePath(sink.archiveN)
	if exists, _, err := inspectLogTarget(oldest); err != nil {
		return err
	} else if exists {
		if err := removeLogFile(oldest); err != nil {
			return err
		}
	}
	for index := sink.archiveN - 1; index >= 1; index-- {
		source, target := sink.archivePath(index), sink.archivePath(index+1)
		if exists, _, err := inspectLogTarget(source); err != nil {
			return err
		} else if exists {
			if err := renameLogFile(source, target); err != nil {
				return err
			}
		}
	}
	if exists, _, err := inspectLogTarget(sink.active); err != nil {
		return err
	} else if exists {
		if err := renameLogFile(sink.active, sink.archivePath(1)); err != nil {
			return err
		}
	}
	return nil
}

func trimTornLogTail(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil
	}
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, info.Size()-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	buffer := make([]byte, 4096)
	position := info.Size()
	for position > 0 {
		start := position - int64(len(buffer))
		if start < 0 {
			start = 0
		}
		size := position - start
		if _, err := file.ReadAt(buffer[:size], start); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if offset := bytes.LastIndexByte(buffer[:size], '\n'); offset >= 0 {
			return file.Truncate(start + int64(offset) + 1)
		}
		position = start
	}
	return file.Truncate(0)
}

func trimLogFileToLimit(file *os.File, limit int64) error {
	if limit <= 0 {
		return file.Truncate(0)
	}
	buffer := make([]byte, limit)
	n, err := file.ReadAt(buffer, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n == 0 {
		return file.Truncate(0)
	}
	lastLine := bytes.LastIndexByte(buffer[:n], '\n')
	if lastLine < 0 {
		return file.Truncate(0)
	}
	return file.Truncate(int64(lastLine + 1))
}
