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
	"crypto/sha256"
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
	appLogDataDirectoryResolved
	appLogCollectorHealthChanged
	appLogCollectorSummary
	appLogSettingsUpdated
	appLogSettingsLoadFailed
	appLogCoreDiagnostic
	appLogCoreDiagnosticLoss
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
	case appLogDataDirectoryResolved:
		return "data_directory.resolved"
	case appLogCollectorHealthChanged:
		return "collector.health_changed"
	case appLogCollectorSummary:
		return "collector.summary"
	case appLogSettingsUpdated:
		return "settings.updated"
	case appLogSettingsLoadFailed:
		return "settings.load_failed"
	case appLogCoreDiagnostic:
		return "core.diagnostic"
	case appLogCoreDiagnosticLoss:
		return "core.diagnostic_loss"
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
	appLogCodeDataDirectory
	appLogCodeSettingsUpdate
	appLogCodeOnboardingSave
	appLogCodeReportQuery
	appLogCodeReportRender
	appLogCodeProcessLaunch
	appLogCodeCollectorReadiness
	appLogCodeCollectorRelease
	appLogCodeCollectorStop
	appLogCodeCollectorHealthRunning
	appLogCodeCollectorHealthDegraded
	appLogCodeCollectorHealthStale
	appLogCodeDataDirectoryExplicit
	appLogCodeDataDirectoryEnvironment
	appLogCodeDataDirectoryCurrent
	appLogCodeDataDirectoryLegacy
	appLogCodeImportDiscovery
	appLogCodeImportSource
	appLogCodeProtocolRecord
	appLogCodeCoreOperationFailed
	appLogCodeCoreConditionSummary
	appLogCodeCorePollSummary
	appLogCodeCoreOperationRecovered
	appLogCodeCoreObserverLoss
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
	case appLogCodeDataDirectory:
		return "data_directory"
	case appLogCodeSettingsUpdate:
		return "settings_update"
	case appLogCodeOnboardingSave:
		return "onboarding_save"
	case appLogCodeReportQuery:
		return "report_query"
	case appLogCodeReportRender:
		return "report_render"
	case appLogCodeProcessLaunch:
		return "process_launch"
	case appLogCodeCollectorReadiness:
		return "collector_readiness"
	case appLogCodeCollectorRelease:
		return "collector_release"
	case appLogCodeCollectorStop:
		return "collector_stop"
	case appLogCodeCollectorHealthRunning:
		return "collector_health_running"
	case appLogCodeCollectorHealthDegraded:
		return "collector_health_degraded"
	case appLogCodeCollectorHealthStale:
		return "collector_health_stale"
	case appLogCodeDataDirectoryExplicit:
		return "explicit"
	case appLogCodeDataDirectoryEnvironment:
		return "environment"
	case appLogCodeDataDirectoryCurrent:
		return "current_default"
	case appLogCodeDataDirectoryLegacy:
		return "legacy_default"
	case appLogCodeImportDiscovery:
		return "import_discovery"
	case appLogCodeImportSource:
		return "import_source"
	case appLogCodeProtocolRecord:
		return "protocol_record"
	case appLogCodeCoreOperationFailed:
		return "core_operation_failed"
	case appLogCodeCoreConditionSummary:
		return "core_condition_summary"
	case appLogCodeCorePollSummary:
		return "core_poll_summary"
	case appLogCodeCoreOperationRecovered:
		return "core_operation_recovered"
	case appLogCodeCoreObserverLoss:
		return "core_observer_loss"
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
	CollectorSummary(appCommand, appLogCode, int64, appLogOutcomeCounts, appLogOutcomeCounts)
	CoreObserver(appCommand) *tokenlogger.DiagnosticObserver
	CoreFailure(error, appLogEvent, appLogCode, appCommand, int)
	Unavailable() bool
	Close()
}

type discardApplicationLog struct{}

func (discardApplicationLog) Event(appLogEvent, appLogCode, appCommand, int) {}
func (discardApplicationLog) CollectorSummary(appCommand, appLogCode, int64, appLogOutcomeCounts, appLogOutcomeCounts) {
}
func (discardApplicationLog) CoreObserver(appCommand) *tokenlogger.DiagnosticObserver     { return nil }
func (discardApplicationLog) CoreFailure(error, appLogEvent, appLogCode, appCommand, int) {}
func (discardApplicationLog) Close()                                                      {}
func (discardApplicationLog) Unavailable() bool                                           { return false }

type unavailableApplicationLog struct{}

func (unavailableApplicationLog) Event(appLogEvent, appLogCode, appCommand, int) {}
func (unavailableApplicationLog) CollectorSummary(appCommand, appLogCode, int64, appLogOutcomeCounts, appLogOutcomeCounts) {
}
func (unavailableApplicationLog) CoreObserver(appCommand) *tokenlogger.DiagnosticObserver     { return nil }
func (unavailableApplicationLog) CoreFailure(error, appLogEvent, appLogCode, appCommand, int) {}
func (unavailableApplicationLog) Close()                                                      {}
func (unavailableApplicationLog) Unavailable() bool                                           { return true }

// coreFailureEnqueued reports only the core mailbox disposition. It must not
// be interpreted as evidence that a CLI log write reached disk.
type coreFailureFallback struct {
	event   appLogEvent
	code    appLogCode
	command appCommand
	exit    int
	ready   bool
}

func coreFailureEnqueued(err error) bool {
	failure, ok := tokenlogger.FailureDetails(err)
	return ok && failure.EmissionDisposition == tokenlogger.DiagnosticEmissionQueued
}

type logKey struct {
	event   appLogEvent
	code    appLogCode
	command appCommand
}

// appLogOutcomeCounts is a closed set of counters. It intentionally cannot
// carry provider identifiers, arbitrary core values, paths, or strings.
type appLogOutcomeCounts struct {
	Imported, Duplicates, IgnoredNonUsage, UnchangedSnapshot                int64
	DeferredIncompleteTail, UnsupportedUsage, MalformedRecord, FailedSource int64
}

var overflowLogKey = logKey{event: appLogEventsSuppressed, command: appCommandOther}

type eventLogger struct {
	sink                   *jsonlLogSink
	cliVersion             string
	coreVersion            string
	metadataTruncated      bool
	pid                    int
	runID                  string
	queue                  chan []byte
	stop                   chan struct{}
	done                   chan struct{}
	mu                     sync.Mutex
	encodeMu               sync.Mutex
	encodeBuffer           bytes.Buffer
	encoder                *slog.Logger
	closed                 bool
	closeBy                time.Time
	last                   map[logKey]time.Time
	suppressed             map[logKey]uint64
	dropped                atomic.Uint64
	sinkFailed             atomic.Bool
	coreObserver           *tokenlogger.DiagnosticObserver
	coreObserverStop       chan struct{}
	coreObserverDone       chan struct{}
	coreCommand            appCommand
	coreObserved           atomic.Uint64
	coreConsumed           atomic.Uint64
	coreInvalid            atomic.Uint64
	coreLostReported       atomic.Uint64
	coreSeen               map[string]struct{}
	coreSeenOrder          []string
	coreSeenOperations     map[string]struct{}
	coreSeenOperationOrder []string
	coreFallbacks          map[string]coreFailureFallback
	coreOperationLines     map[string][32]byte
	coreQueuedLines        map[[32]byte]string
	closeOnce              sync.Once
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
	observer, err := tokenlogger.NewDiagnosticObserver(0)
	if err != nil {
		return nil, errors.New("initialize core diagnostic observer")
	}
	logger := &eventLogger{
		sink: sink, cliVersion: cliValue, coreVersion: coreValue, metadataTruncated: cliTruncated || coreTruncated, pid: pid, runID: runID,
		queue: make(chan []byte, logQueueCapacity), stop: make(chan struct{}), done: make(chan struct{}),
		coreObserver: observer, coreObserverStop: make(chan struct{}), coreObserverDone: make(chan struct{}),
		coreSeen: make(map[string]struct{}, maxLogKeyState), coreSeenOrder: make([]string, 0, maxLogKeyState),
		coreSeenOperations: make(map[string]struct{}, maxLogKeyState), coreSeenOperationOrder: make([]string, 0, maxLogKeyState),
		coreFallbacks:      make(map[string]coreFailureFallback, maxLogKeyState),
		coreOperationLines: make(map[string][32]byte, maxLogKeyState), coreQueuedLines: make(map[[32]byte]string, logQueueCapacity),
		last: last, suppressed: make(map[logKey]uint64, maxLogKeyState),
	}
	logger.encoder = newLogEncoder(&logger.encodeBuffer)
	go logger.run()
	go logger.drainCoreObserver()
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

func (logger *eventLogger) CoreObserver(command appCommand) *tokenlogger.DiagnosticObserver {
	if logger == nil || command.String() == "" {
		return nil
	}
	logger.mu.Lock()
	if !logger.closed {
		logger.coreCommand = command
	}
	observer := logger.coreObserver
	if logger.closed {
		observer = nil
	}
	logger.mu.Unlock()
	return observer
}

func (logger *eventLogger) CoreFailure(err error, event appLogEvent, code appLogCode, command appCommand, exit int) {
	if logger == nil || event.String() == "" || command.String() == "" {
		return
	}
	failure, classified := tokenlogger.FailureDetails(err)
	if !classified || failure.EmissionDisposition != tokenlogger.DiagnosticEmissionQueued || failure.OperationID == "" {
		logger.Event(event, code, command, exit)
		return
	}
	opID := string(failure.OperationID)
	logger.mu.Lock()
	if logger.closed {
		logger.mu.Unlock()
		logger.Event(event, code, command, exit)
		return
	}
	if _, seen := logger.coreSeenOperations[opID]; seen {
		logger.mu.Unlock()
		return
	}
	if len(logger.coreFallbacks) >= maxLogKeyState {
		logger.mu.Unlock()
		logger.Event(event, code, command, exit)
		return
	}
	fallback := coreFailureFallback{event: event, code: code, command: command, exit: exit}
	if _, queued := logger.coreOperationLines[opID]; !queued {
		// The event may be in the caller-owned mailbox but not yet drained.
		// Keep this fallback pending until the matching event arrives or
		// shutdown establishes that the mailbox has been drained.
	}
	logger.coreFallbacks[opID] = fallback
	logger.mu.Unlock()
}

func (logger *eventLogger) resolveCoreFallbackLocked(operationID string) {
	delete(logger.coreFallbacks, operationID)
}

func (logger *eventLogger) markCoreFallbackReady(operationID string) {
	if logger == nil || operationID == "" {
		return
	}
	logger.mu.Lock()
	if fallback, ok := logger.coreFallbacks[operationID]; ok {
		fallback.ready = true
		logger.coreFallbacks[operationID] = fallback
	}
	logger.mu.Unlock()
}

func (logger *eventLogger) flushCoreFallbacks() {
	if logger == nil {
		return
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	for operationID, fallback := range logger.coreFallbacks {
		if !fallback.ready {
			continue
		}
		line := logger.encode(fallback.event, fallback.code, fallback.command, fallback.exit, 0)
		if len(line) == 0 || len(line) > maxLogEventBytes || line[len(line)-1] != '\n' {
			logger.dropped.Add(1)
			delete(logger.coreFallbacks, operationID)
			continue
		}
		select {
		case logger.queue <- line:
			delete(logger.coreFallbacks, operationID)
		default:
			logger.dropped.Add(1)
			return
		}
	}
}

func (logger *eventLogger) rememberCoreOperationLocked(operationID string) {
	if operationID == "" {
		return
	}
	if _, exists := logger.coreSeenOperations[operationID]; exists {
		return
	}
	if len(logger.coreSeenOperationOrder) == maxLogKeyState {
		delete(logger.coreSeenOperations, logger.coreSeenOperationOrder[0])
		copy(logger.coreSeenOperationOrder, logger.coreSeenOperationOrder[1:])
		logger.coreSeenOperationOrder = logger.coreSeenOperationOrder[:len(logger.coreSeenOperationOrder)-1]
	}
	logger.coreSeenOperations[operationID] = struct{}{}
	logger.coreSeenOperationOrder = append(logger.coreSeenOperationOrder, operationID)
}

func (logger *eventLogger) finishCoreWrite(line []byte, operationID string, written bool) {
	if logger == nil || operationID == "" {
		return
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	hash := sha256.Sum256(line)
	delete(logger.coreQueuedLines, hash)
	delete(logger.coreOperationLines, operationID)
	if written {
		logger.rememberCoreOperationLocked(operationID)
		logger.resolveCoreFallbackLocked(operationID)
		return
	}
	if fallback, ok := logger.coreFallbacks[operationID]; ok {
		fallback.ready = true
		logger.coreFallbacks[operationID] = fallback
	}
}

func (logger *eventLogger) coreOperationForLine(line []byte) string {
	if logger == nil {
		return ""
	}
	hash := sha256.Sum256(line)
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return logger.coreQueuedLines[hash]
}

func (logger *eventLogger) finishCoreWriteUncertain(line []byte, operationID string) {
	if logger == nil || operationID == "" {
		return
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	hash := sha256.Sum256(line)
	delete(logger.coreQueuedLines, hash)
	delete(logger.coreOperationLines, operationID)
	// A partial/uncertain append may already contain the full core event. Do not
	// replay it as a generic failure; existing dropped-event accounting records
	// that the sink outcome was uncertain.
	delete(logger.coreFallbacks, operationID)
}

func (logger *eventLogger) prepareCoreFallbacks() {
	logger.mu.Lock()
	for operationID, fallback := range logger.coreFallbacks {
		if _, queued := logger.coreOperationLines[operationID]; !queued {
			fallback.ready = true
			logger.coreFallbacks[operationID] = fallback
		}
	}
	logger.mu.Unlock()
}

func (logger *eventLogger) CoreDiagnostic(event tokenlogger.DiagnosticEvent) {
	if logger == nil {
		return
	}
	logger.coreConsumed.Add(1)
	projection, ok := projectCoreDiagnostic(event)
	if !ok || event.CoreVersion != logger.coreVersion && strings.TrimPrefix(event.CoreVersion, "v") != strings.TrimPrefix(logger.coreVersion, "v") {
		logger.coreInvalid.Add(1)
		logger.markCoreFallbackReady(string(event.OperationID))
		return
	}
	code := coreDiagnosticCode(event.Kind)
	if code == appLogCodeNone {
		logger.coreInvalid.Add(1)
		logger.markCoreFallbackReady(string(event.OperationID))
		return
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return
	}
	command := logger.coreCommand
	if command.String() == "" {
		command = appCommandOther
	}
	key := logKey{event: appLogCoreDiagnostic, code: code, command: command}
	if logger.coreSeen == nil {
		logger.coreSeen = make(map[string]struct{}, maxLogKeyState)
	}
	dedupKey := string(event.Kind) + ":" + string(event.OperationID) + ":" + string(event.Classification.Component) + ":" + string(event.Classification.Operation) + ":" + string(event.Classification.Reason) + ":" + event.Subject.Reference
	if _, duplicate := logger.coreSeen[dedupKey]; duplicate {
		logger.suppressed[key]++
		return
	}
	if len(logger.coreSeenOrder) == maxLogKeyState {
		delete(logger.coreSeen, logger.coreSeenOrder[0])
		copy(logger.coreSeenOrder, logger.coreSeenOrder[1:])
		logger.coreSeenOrder = logger.coreSeenOrder[:len(logger.coreSeenOrder)-1]
	}
	logger.coreSeen[dedupKey] = struct{}{}
	logger.coreSeenOrder = append(logger.coreSeenOrder, dedupKey)
	if _, exists := logger.last[key]; !exists && len(logger.last) >= maxLogKeyState-1 {
		key = overflowLogKey
	}
	now := time.Now().UTC()
	if last, exists := logger.last[key]; exists && !last.IsZero() && now.Sub(last) < logRepeatWindow {
		logger.suppressed[key]++
		return
	}
	logger.last[key] = now
	line := logger.encodeCoreDiagnostic(projection, event, code, command)
	if len(line) == 0 || len(line) > maxLogEventBytes || line[len(line)-1] != '\n' {
		logger.dropped.Add(1)
		if fallback, ok := logger.coreFallbacks[string(event.OperationID)]; ok {
			fallback.ready = true
			logger.coreFallbacks[string(event.OperationID)] = fallback
		}
		return
	}
	select {
	case logger.queue <- line:
		logger.coreObserved.Add(1)
		if event.Kind == tokenlogger.DiagnosticEventOperationFailed {
			opID := string(event.OperationID)
			hash := sha256.Sum256(line)
			logger.coreOperationLines[opID] = hash
			logger.coreQueuedLines[hash] = opID
		}
	default:
		logger.dropped.Add(1)
		if fallback, ok := logger.coreFallbacks[string(event.OperationID)]; ok {
			fallback.ready = true
			logger.coreFallbacks[string(event.OperationID)] = fallback
		}
	}
}

const maxLoggedOutcomeCount int64 = 1<<53 - 1
const maxLoggedDurationMillis int64 = 24 * 60 * 60 * 1000
const appLogOperationExitCodeUnset = -1

func boundedLogCount(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > maxLoggedOutcomeCount {
		return maxLoggedOutcomeCount
	}
	return value
}

func boundedLogDuration(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > maxLoggedDurationMillis {
		return maxLoggedDurationMillis
	}
	return value
}

func (logger *eventLogger) CollectorSummary(command appCommand, status appLogCode, durationMillis int64, latest, cumulative appLogOutcomeCounts) {
	if logger == nil || command.String() == "" || status.String() == "" || !isCollectorHealthCode(status) {
		return
	}
	key := logKey{event: appLogCollectorSummary, code: status, command: command}
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
	line := logger.encodeCollectorSummary(command, status, durationMillis, latest, cumulative)
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

func isCollectorHealthCode(code appLogCode) bool {
	return code == appLogCodeCollectorHealthRunning || code == appLogCodeCollectorHealthDegraded || code == appLogCodeCollectorHealthStale
}

func (logger *eventLogger) encode(event appLogEvent, code appLogCode, command appCommand, exitCode int, count uint64) []byte {
	logger.encodeMu.Lock()
	defer logger.encodeMu.Unlock()
	logger.encodeBuffer.Reset()
	level := slog.LevelInfo
	if event == appLogCollectorFailed || event == appLogSinkUnavailable || event == appLogCommandFailed || event == appLogSettingsLoadFailed || event == appLogCoreDiagnosticLoss || event == appLogProcessFinished && exitCode != 0 || event == appLogCollectorHealthChanged && code != appLogCodeCollectorHealthRunning || event == appLogCollectorSummary && code != appLogCodeCollectorHealthRunning {
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
	if event == appLogProcessFinished || event == appLogCommandFailed && exitCode >= 0 {
		attrs = append(attrs, slog.Int("exit_code", exitCode))
	}
	if event == appLogEventsSuppressed || event == appLogEventsDropped || event == appLogCoreDiagnosticLoss {
		attrs = append(attrs, slog.Uint64("count", count))
	}
	logger.encoder.LogAttrs(context.Background(), level, event.String(), attrs...)
	if logger.encodeBuffer.Len() > maxLogEventBytes {
		return nil
	}
	return append([]byte(nil), logger.encodeBuffer.Bytes()...)
}

func (logger *eventLogger) encodeCollectorSummary(command appCommand, status appLogCode, durationMillis int64, latest, cumulative appLogOutcomeCounts) []byte {
	logger.encodeMu.Lock()
	defer logger.encodeMu.Unlock()
	logger.encodeBuffer.Reset()
	attrs := []slog.Attr{
		slog.Int("format_version", logFormatVersion),
		slog.String("cli_version", logger.cliVersion),
		slog.String("core_version", logger.coreVersion),
		slog.Int("pid", logger.pid),
		slog.String("run_id", logger.runID),
		slog.String("command", command.String()),
		slog.String("code", status.String()),
		slog.Bool("metadata_truncated", logger.metadataTruncated),
	}
	if durationMillis >= 0 {
		attrs = append(attrs, slog.Int("poll_duration_ms", int(boundedLogDuration(durationMillis))))
	}
	appendCounts := func(prefix string, values appLogOutcomeCounts) {
		attrs = append(attrs,
			slog.Int64(prefix+"_imported", boundedLogCount(values.Imported)),
			slog.Int64(prefix+"_duplicates", boundedLogCount(values.Duplicates)),
			slog.Int64(prefix+"_ignored_non_usage", boundedLogCount(values.IgnoredNonUsage)),
			slog.Int64(prefix+"_unchanged_snapshot", boundedLogCount(values.UnchangedSnapshot)),
			slog.Int64(prefix+"_deferred_incomplete_tail", boundedLogCount(values.DeferredIncompleteTail)),
			slog.Int64(prefix+"_unsupported_usage", boundedLogCount(values.UnsupportedUsage)),
			slog.Int64(prefix+"_malformed_record", boundedLogCount(values.MalformedRecord)),
			slog.Int64(prefix+"_failed_source", boundedLogCount(values.FailedSource)),
		)
	}
	appendCounts("latest_poll", latest)
	appendCounts("run_cumulative", cumulative)
	level := slog.LevelInfo
	if status != appLogCodeCollectorHealthRunning {
		level = slog.LevelWarn
	}
	logger.encoder.LogAttrs(context.Background(), level, appLogCollectorSummary.String(), attrs...)
	if logger.encodeBuffer.Len() > maxLogEventBytes {
		return nil
	}
	return append([]byte(nil), logger.encodeBuffer.Bytes()...)
}

func (logger *eventLogger) encodeCoreDiagnostic(projection coreDiagnosticProjection, event tokenlogger.DiagnosticEvent, code appLogCode, command appCommand) []byte {
	logger.encodeMu.Lock()
	defer logger.encodeMu.Unlock()
	logger.encodeBuffer.Reset()
	attrs := []slog.Attr{
		slog.Int("format_version", logFormatVersion),
		slog.String("cli_version", logger.cliVersion),
		slog.String("core_version", logger.coreVersion),
		slog.Int("pid", logger.pid),
		slog.String("run_id", logger.runID),
		slog.String("command", command.String()),
		slog.String("code", code.String()),
		slog.Bool("metadata_truncated", logger.metadataTruncated),
		slog.String("core_contract_version", strconv.Itoa(projection.contractVersion)),
		slog.String("core_kind", string(projection.kind)),
		slog.String("core_evidence_scope", string(projection.evidenceScope)),
		slog.String("core_evidence_availability", string(projection.evidenceAvail)),
		slog.String("core_evidence_truncated", strconv.FormatBool(projection.evidenceLost)),
	}
	if projection.evidenceReason != "" {
		attrs = append(attrs, slog.String("core_evidence_unavailable_reason", string(projection.evidenceReason)))
	}
	classification := projection.classification
	if event.HasClassification {
		attrs = append(attrs,
			slog.String("core_component", string(classification.Component)),
			slog.String("core_operation", string(classification.Operation)),
			slog.String("core_reason", string(classification.Reason)),
			slog.String("core_severity", string(classification.Severity)),
			slog.String("core_category", string(classification.Category)),
			slog.String("core_adapter", string(classification.Adapter)),
			slog.String("core_retryability", string(classification.Retryability)),
			slog.String("core_impact", string(classification.Impact)),
			slog.String("core_explanation_code", string(classification.ExplanationCode)),
			slog.String("core_remediation_code", string(classification.RemediationCode)),
		)
	}
	attrs = append(attrs,
		slog.String("core_counts_scope", string(projection.countsScope)),
		slog.String("core_counts_availability", string(projection.countsAvail)),
		slog.String("core_counts_saturated", strconv.FormatBool(projection.countsSaturated)),
		slog.String("core_counts_truncated", strconv.FormatBool(projection.countsTruncated)),
	)
	if projection.countsAvail == tokenlogger.DiagnosticAvailabilityAvailable {
		counts := projection.counts
		attrs = append(attrs,
			slog.String("core_count_ignored_non_usage", strconv.FormatUint(counts.IgnoredNonUsage, 10)),
			slog.String("core_count_unchanged_snapshot", strconv.FormatUint(counts.UnchangedSnapshot, 10)),
			slog.String("core_count_deferred_incomplete_tail", strconv.FormatUint(counts.DeferredIncompleteTail, 10)),
			slog.String("core_count_unsupported_usage", strconv.FormatUint(counts.UnsupportedUsage, 10)),
			slog.String("core_count_malformed_record", strconv.FormatUint(counts.MalformedRecord, 10)),
			slog.String("core_count_failed_source", strconv.FormatUint(counts.FailedSource, 10)),
		)
	}
	level := slog.LevelInfo
	if classification.Severity == tokenlogger.DiagnosticSeverityWarning {
		level = slog.LevelWarn
	} else if classification.Severity == tokenlogger.DiagnosticSeverityError {
		level = slog.LevelError
	}
	logger.encoder.LogAttrs(context.Background(), level, appLogCoreDiagnostic.String(), attrs...)
	if logger.encodeBuffer.Len() > maxLogEventBytes {
		return nil
	}
	return append([]byte(nil), logger.encodeBuffer.Bytes()...)
}

func (logger *eventLogger) encodeCoreObserverLoss(count uint64) []byte {
	return logger.encode(appLogCoreDiagnosticLoss, appLogCodeCoreObserverLoss, appCommandOther, 0, count)
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

func (logger *eventLogger) drainCoreObserver() {
	defer close(logger.coreObserverDone)
	events := logger.coreObserver.Events()
	for {
		select {
		case event := <-events:
			logger.CoreDiagnostic(event)
		case <-logger.coreObserverStop:
			for {
				select {
				case event := <-events:
					logger.CoreDiagnostic(event)
				default:
					return
				}
			}
		}
	}
}

func (logger *eventLogger) reportCoreObserverLoss() {
	if logger == nil || logger.coreObserver == nil {
		return
	}
	stats := logger.coreObserver.Stats()
	consumed := logger.coreConsumed.Load()
	queuedLost := uint64(0)
	if stats.Admitted > consumed {
		queuedLost = stats.Admitted - consumed
	}
	lost := saturatingAdd(stats.Dropped, queuedLost)
	lost = saturatingAdd(lost, logger.coreInvalid.Load())
	if lost == 0 {
		return
	}
	line := logger.encodeCoreObserverLoss(lost)
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return
	}
	select {
	case logger.queue <- line:
		logger.coreLostReported.Store(lost)
	default:
		logger.dropped.Add(1)
	}
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
			logger.flushCoreFallbacks()
		case <-logger.stop:
			logger.mu.Lock()
			deadline := logger.closeBy
			logger.mu.Unlock()
			for time.Now().Before(deadline) {
				batch := logger.takeAvailableBatch()
				if len(batch) == 0 {
					logger.prepareCoreFallbacks()
					logger.flushCoreFallbacks()
					batch = logger.takeAvailableBatch()
					if len(batch) > 0 {
						logger.writeBatch(batch)
						continue
					}
					logger.flushSummaries()
					return
				}
				logger.writeBatch(batch)
			}
			logger.dropped.Add(uint64(len(logger.queue)))
			logger.prepareCoreFallbacks()
			logger.flushCoreFallbacks()
			if batch := logger.takeAvailableBatch(); len(batch) > 0 {
				logger.writeBatch(batch)
				logger.prepareCoreFallbacks()
				logger.flushCoreFallbacks()
				if fallbackBatch := logger.takeAvailableBatch(); len(fallbackBatch) > 0 {
					logger.writeBatch(fallbackBatch)
				}
			}
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
	err := logger.sink.appendBatch(batch)
	if err == nil {
		for _, line := range batch {
			if operationID := logger.coreOperationForLine(line); operationID != "" {
				logger.finishCoreWrite(line, operationID, true)
			}
		}
		return
	}
	if err != nil {
		logger.sinkFailed.Store(true)
		logger.dropped.Add(uint64(len(batch)))
		progress := batchAppendProgress(err)
		for i, line := range batch {
			operationID := logger.coreOperationForLine(line)
			if operationID == "" {
				continue
			}
			switch {
			case i < progress.completed:
				logger.finishCoreWrite(line, operationID, true)
			case i == progress.completed && progress.uncertain:
				logger.finishCoreWriteUncertain(line, operationID)
			default:
				logger.finishCoreWrite(line, operationID, false)
			}
		}
		logger.flushCoreFallbacks()
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
	logger.closeOnce.Do(func() {
		deadline := time.Now().Add(logCloseBudget)
		close(logger.coreObserverStop)
		observerWait := time.Until(deadline)
		if observerWait > coreObserverDrainBudget {
			observerWait = coreObserverDrainBudget
		}
		if observerWait < 0 {
			observerWait = 0
		}
		observerTimer := time.NewTimer(observerWait)
		select {
		case <-logger.coreObserverDone:
		case <-observerTimer.C:
		}
		if !observerTimer.Stop() {
			select {
			case <-observerTimer.C:
			default:
			}
		}
		logger.reportCoreObserverLoss()
		logger.mu.Lock()
		logger.closed = true
		logger.closeBy = deadline
		close(logger.stop)
		logger.mu.Unlock()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-logger.done:
		case <-timer.C:
		}
	})
}

type jsonlLogSink struct {
	dir          string
	active       string
	lockPath     string
	maxFile      int64
	archiveN     int
	lockWait     time.Duration
	lockSleep    time.Duration
	writeFile    func(*os.File, []byte) (int, error)
	truncateFile func(*os.File, int64) error
	renameFile   func(string, string) error
	removeFile   func(string) error
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
				truncate := sink.truncateFile
				if truncate == nil {
					truncate = (*os.File).Truncate
				}
				truncateErr := truncate(active, size)
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

func (sink *jsonlLogSink) renameLogFile(source, target string) error {
	if sink.renameFile != nil {
		return sink.renameFile(source, target)
	}
	return renameLogFile(source, target)
}

func (sink *jsonlLogSink) removeLogFile(path string) error {
	if sink.removeFile != nil {
		return sink.removeFile(path)
	}
	return removeLogFile(path)
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
		return sink.renameLogFile(first, sink.active)
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
		if err := sink.removeLogFile(path); err != nil {
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
		if err := sink.removeLogFile(oldest); err != nil {
			return err
		}
	}
	for index := sink.archiveN - 1; index >= 1; index-- {
		source, target := sink.archivePath(index), sink.archivePath(index+1)
		if exists, _, err := inspectLogTarget(source); err != nil {
			return err
		} else if exists {
			if err := sink.renameLogFile(source, target); err != nil {
				return err
			}
		}
	}
	if exists, _, err := inspectLogTarget(sink.active); err != nil {
		return err
	} else if exists {
		if err := sink.renameLogFile(sink.active, sink.archivePath(1)); err != nil {
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
