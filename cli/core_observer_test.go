// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

func validCoreDiagnosticEvent() tokenlogger.DiagnosticEvent {
	classification := tokenlogger.DiagnosticClassification{
		Component: tokenlogger.DiagnosticComponentSchema, Operation: tokenlogger.DiagnosticOperationOpen,
		Reason: tokenlogger.DiagnosticReasonSchemaUnsupported, Severity: tokenlogger.DiagnosticSeverityError,
		Category: tokenlogger.DiagnosticCategoryMigration, Adapter: tokenlogger.DiagnosticAdapterUnavailable,
		Retryability: tokenlogger.DiagnosticNotRetryable, Impact: tokenlogger.DiagnosticImpactInspectionUnavailable,
		ExplanationCode: tokenlogger.DiagnosticExplanationSchemaUnsupported,
		RemediationCode: tokenlogger.DiagnosticRemediationUpgradeCore,
	}
	missing := tokenlogger.DiagnosticOptionalUint64{Availability: tokenlogger.DiagnosticAvailabilityNotCollected}
	return tokenlogger.DiagnosticEvent{
		ContractVersion: tokenlogger.DiagnosticContractVersion, CoreVersion: "0.10.0", ObservedAt: time.Now().UTC(),
		Kind: tokenlogger.DiagnosticEventOperationFailed, Classification: classification, HasClassification: true,
		OperationID:         "00112233445566778899aabbccddeeff",
		Subject:             tokenlogger.DiagnosticSubject{Availability: tokenlogger.DiagnosticAvailabilityNotCollected},
		SourceSchemaVersion: missing, TargetSchemaVersion: missing, DurationMilliseconds: missing,
		RetryAttempt: missing, RetryCount: missing,
		Position: tokenlogger.DiagnosticPosition{ByteOffset: missing, LineNumber: missing},
		Evidence: tokenlogger.DiagnosticEvidence{Scope: tokenlogger.DiagnosticEvidenceOperation,
			Availability: tokenlogger.DiagnosticAvailabilityNotCollected, UnavailableReason: tokenlogger.DiagnosticUnavailableNotCollected},
		Counts: tokenlogger.DiagnosticOutcomeCounts{Scope: tokenlogger.DiagnosticCountOperation,
			Availability: tokenlogger.DiagnosticAvailabilityNotCollected},
	}
}

func TestCoreDiagnosticProjectionValidatesAndOmitsIdentity(t *testing.T) {
	event := validCoreDiagnosticEvent()
	projection, ok := projectCoreDiagnostic(event)
	if !ok || projection.kind != tokenlogger.DiagnosticEventOperationFailed {
		t.Fatalf("valid event rejected: %+v, %v", projection, ok)
	}
	if event.Validate() != nil {
		t.Fatalf("test event is invalid: %v", event.Validate())
	}
	event.OperationID = "private-operation-id"
	if _, ok := projectCoreDiagnostic(event); ok {
		t.Fatal("unvalidated operation identifier was accepted")
	}
	event = validCoreDiagnosticEvent()
	event.ContractVersion = 2
	if _, ok := projectCoreDiagnostic(event); ok {
		t.Fatal("unknown contract version was accepted")
	}
}

func TestCoreDiagnosticDuplicateOperationIsSuppressedWithEvidence(t *testing.T) {
	dir := t.TempDir()
	logger, err := newEventLogger(dir, "0.11.0-dev", "0.10.0", os.Getpid(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	event := validCoreDiagnosticEvent()
	logger.CoreDiagnostic(event)
	logger.CoreDiagnostic(event)
	logger.Close()
	result, err := readManagedLogs(dir, logReadOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	coreEvents, suppressed := 0, uint64(0)
	for _, record := range result.Records {
		if record.Event == "core.diagnostic" {
			coreEvents++
		}
		if record.Event == "logging.events_suppressed" && record.Code == "core_operation_failed" && record.Count != nil {
			suppressed += *record.Count
		}
	}
	if coreEvents != 1 || suppressed != 1 {
		t.Fatalf("duplicate was not bounded and reported: core=%d suppressed=%d records=%+v", coreEvents, suppressed, result.Records)
	}
}

func TestCoreDiagnosticCountersPreserveFullUint64AndScope(t *testing.T) {
	dir := t.TempDir()
	logger, err := newEventLogger(dir, "0.11.0-dev", "0.10.0", os.Getpid(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	event := validCoreDiagnosticEvent()
	event.Kind = tokenlogger.DiagnosticEventConditionSummary
	event.Evidence.Scope = tokenlogger.DiagnosticEvidenceSourceGeneration
	event.Evidence.Availability = tokenlogger.DiagnosticAvailabilityAvailable
	event.Evidence.UnavailableReason = ""
	event.Counts.Availability = tokenlogger.DiagnosticAvailabilityAvailable
	event.Counts.Scope = tokenlogger.DiagnosticCountLifetime
	event.Counts.MalformedRecord = ^uint64(0)
	event.Counts.Saturated = true
	if err := event.Validate(); err != nil {
		t.Fatalf("full-range counter event invalid: %v", err)
	}
	logger.CoreDiagnostic(event)
	logger.Close()
	result, err := readManagedLogs(dir, logReadOptions{Limit: 10})
	if err != nil || len(result.Records) == 0 {
		t.Fatalf("read full-range counter event: result=%+v err=%v", result, err)
	}
	for _, record := range result.Records {
		if record.Event == "core.diagnostic" {
			if record.CoreDetails["core_counts_scope"] != "lifetime" || record.CoreDetails["core_count_malformed_record"] != "18446744073709551615" || record.CoreDetails["core_counts_saturated"] != "true" {
				t.Fatalf("counter precision or scope changed: %+v", record.CoreDetails)
			}
			return
		}
	}
	t.Fatalf("core event missing: %+v", result.Records)
}

func TestCoreOpenFailureIsObservedAndReadAsApprovedProjection(t *testing.T) {
	dir := t.TempDir()
	logger, err := newEventLogger(dir, "0.11.0-dev", tokenlogger.CoreVersion(), os.Getpid(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	observer := logger.CoreObserver(appCommandImport)
	if observer == nil {
		t.Fatal("production logger did not provide a core observer")
	}
	_, openErr := openStoreWithLog("", logger, appCommandImport)
	if openErr == nil {
		t.Fatal("empty database path unexpectedly opened")
	}
	failure, ok := tokenlogger.FailureDetails(openErr)
	if !ok || failure.EmissionDisposition != tokenlogger.DiagnosticEmissionQueued {
		t.Fatalf("pre-Open classified failure missing: %+v, %v", failure, ok)
	}
	logger.Close()
	result, err := readManagedLogs(dir, logReadOptions{Limit: 20})
	if err != nil || len(result.Records) == 0 {
		t.Fatalf("observed log unavailable: result=%+v err=%v", result, err)
	}
	var found bool
	var duplicateBaseline bool
	for _, record := range result.Records {
		if record.Event == "core.diagnostic" {
			found = true
			if record.Code != "core_operation_failed" || record.CoreDetails["core_reason"] != "failure_unclassified" {
				t.Fatalf("unexpected safe projection: %+v", record)
			}
		}
		if record.Event == "command.failed" && record.Code == "database_open" {
			duplicateBaseline = true
		}
	}
	if !found {
		t.Fatalf("core failure event not drained: %+v", result.Records)
	}
	if duplicateBaseline {
		t.Fatalf("typed core Open failure also emitted generic duplicate: %+v", result.Records)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"00112233445566778899aabbccddeeff", "operation_id", "byte_offset", "private-operation-id"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("projection exposed %q: %s", private, encoded)
		}
	}
}

func TestCoreOpenFailureFallsBackWhenLocalSinkRejectsRichEvent(t *testing.T) {
	dir := t.TempDir()
	logger, err := newEventLogger(dir, "0.11.0-dev", tokenlogger.CoreVersion(), os.Getpid(), "cccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatal(err)
	}
	var attempted strings.Builder
	logger.sink.writeFile = func(_ *os.File, line []byte) (int, error) {
		_, _ = attempted.Write(line)
		return 0, errors.New("synthetic sink rejection")
	}
	_, openErr := openStoreWithLog("", logger, appCommandImport)
	if openErr == nil {
		t.Fatal("empty database path unexpectedly opened")
	}
	logger.Close()
	if !logger.Unavailable() {
		t.Fatal("synthetic sink rejection was not reflected as unavailable")
	}
	text := attempted.String()
	if !strings.Contains(text, `"msg":"core.diagnostic"`) || !strings.Contains(text, `"msg":"command.failed"`) || !strings.Contains(text, `"code":"database_open"`) {
		t.Fatalf("rich rejection did not attempt generic fallback: %s", text)
	}
}

func TestCoreOpenFailureFallsBackWhenObserverDropsEvent(t *testing.T) {
	dir := t.TempDir()
	logger, err := newEventLogger(dir, "0.11.0-dev", tokenlogger.CoreVersion(), os.Getpid(), "dddddddddddddddddddddddddddddddd")
	if err != nil {
		t.Fatal(err)
	}
	observer, err := tokenlogger.NewDiagnosticObserver(1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tokenlogger.OpenWithObservation("", tokenlogger.OpenOptions{}, observer)
	_, droppedErr := tokenlogger.OpenWithObservation("", tokenlogger.OpenOptions{}, observer)
	failure, ok := tokenlogger.FailureDetails(droppedErr)
	if !ok || failure.EmissionDisposition != tokenlogger.DiagnosticEmissionDropped {
		t.Fatalf("test did not produce observer loss: %+v ok=%v", failure, ok)
	}
	logger.CoreFailure(droppedErr, appLogCommandFailed, appLogCodeDatabaseOpen, appCommandImport, 2)
	logger.Close()
	result, err := readManagedLogs(dir, logReadOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range result.Records {
		if record.Event == "command.failed" && record.Code == "database_open" {
			return
		}
	}
	t.Fatalf("observer drop had no generic fallback: %+v", result.Records)
}

func TestCoreObserverUnreadQueueReportsBoundedLossOnClose(t *testing.T) {
	dir := t.TempDir()
	observer, err := tokenlogger.NewDiagnosticObserver(1)
	if err != nil {
		t.Fatal(err)
	}
	runIDRaw := make([]byte, 16)
	if _, err := rand.Read(runIDRaw); err != nil {
		t.Fatal(err)
	}
	sink, err := newJSONLLogSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger := &eventLogger{
		sink: sink, cliVersion: "0.11.0-dev", coreVersion: tokenlogger.CoreVersion(), pid: os.Getpid(), runID: hex.EncodeToString(runIDRaw),
		queue: make(chan []byte, logQueueCapacity), stop: make(chan struct{}), done: make(chan struct{}),
		coreObserver: observer, coreObserverStop: make(chan struct{}), coreObserverDone: make(chan struct{}), coreCommand: appCommandWatch,
		last: map[logKey]time.Time{overflowLogKey: {}}, suppressed: make(map[logKey]uint64), coreSeen: make(map[string]struct{}), coreSeenOrder: make([]string, 0, maxLogKeyState),
		coreSeenOperations: make(map[string]struct{}), coreSeenOperationOrder: make([]string, 0, maxLogKeyState),
		coreFallbacks: make(map[string]coreFailureFallback), coreOperationLines: make(map[string][32]byte), coreQueuedLines: make(map[[32]byte]string),
	}
	logger.encoder = newLogEncoder(&logger.encodeBuffer)
	for i := 0; i < tokenlogger.MaxDiagnosticObserverCapacity+20; i++ {
		_, _ = tokenlogger.OpenWithObservation("", tokenlogger.OpenOptions{}, observer)
	}
	stats := observer.Stats()
	if stats.Dropped == 0 || stats.Capacity != 1 {
		t.Fatalf("expected bounded mailbox loss, got %+v", stats)
	}
	go logger.run()
	go logger.drainCoreObserver()
	started := time.Now()
	logger.Close()
	if time.Since(started) > logCloseBudget+100*time.Millisecond {
		t.Fatalf("observer and writer teardown exceeded the shared shutdown budget: %s", time.Since(started))
	}
	result, err := readManagedLogs(dir, logReadOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var loss *logReadRecord
	for i := range result.Records {
		if result.Records[i].Event == "core.diagnostic_loss" {
			loss = &result.Records[i]
		}
	}
	if loss == nil || loss.Count == nil || *loss.Count < stats.Dropped {
		t.Fatalf("observer loss was not honestly reported: stats=%+v records=%+v", stats, result.Records)
	}
}

func TestCoreDiagnosticReaderRejectsMalformedProjection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "klg.log")
	line := syntheticLogEvent("2026-10-30T00:00:00Z", "ERROR", "core.diagnostic", "core_operation_failed", map[string]any{
		"core_contract_version": "99", "core_kind": "operation_failed", "core_evidence_scope": "operation",
	})
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := readManagedLogs(dir, logReadOptions{Limit: 5})
	if err != nil || len(result.Records) != 0 || result.InvalidEntries != 1 {
		t.Fatalf("malformed core data accepted: result=%+v err=%v", result, err)
	}
}
