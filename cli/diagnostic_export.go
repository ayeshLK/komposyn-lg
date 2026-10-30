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
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const (
	diagnosticExportVersion    = 1
	maxExportDiagnostics       = 100
	maxExportLogRecords        = 200
	maxDiagnosticExportBytes   = 1 << 20
	maxExportDiagnosticIDBytes = 128
)

var (
	ErrDiagnosticExportSelection   = errors.New("invalid diagnostic export selection")
	ErrDiagnosticExportConsent     = errors.New("diagnostic export consent required")
	ErrDiagnosticExportNotFound    = errors.New("selected diagnostic was not found")
	ErrDiagnosticExportUnavailable = errors.New("selected diagnostic evidence is unavailable")
	ErrDiagnosticExportCleanup     = errors.New("incomplete diagnostic export could not be safely removed")
)

type diagnosticExportCount struct {
	Value        string `json:"value,omitempty"`
	Availability string `json:"availability"`
	Saturated    bool   `json:"saturated,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
}

type diagnosticExportClassification struct {
	Component       string `json:"component"`
	Operation       string `json:"operation"`
	Reason          string `json:"reason"`
	Severity        string `json:"severity"`
	Category        string `json:"category"`
	Adapter         string `json:"adapter,omitempty"`
	Retryability    string `json:"retryability"`
	Impact          string `json:"impact"`
	ExplanationCode string `json:"explanation_code"`
	RemediationCode string `json:"remediation_code"`
}

type diagnosticExportCondition struct {
	State        string `json:"state"`
	Coverage     string `json:"coverage"`
	Availability string `json:"availability"`
	Reason       string `json:"reason"`
}

type diagnosticExportLogRecord struct {
	Event       string            `json:"event"`
	Level       string            `json:"level"`
	Code        string            `json:"code,omitempty"`
	CLIVersion  string            `json:"cli_version"`
	CoreVersion string            `json:"core_version"`
	Count       *uint64           `json:"count,omitempty"`
	Core        map[string]string `json:"core,omitempty"`
}

type diagnosticExportItem struct {
	Classification           diagnosticExportClassification `json:"classification"`
	Condition                diagnosticExportCondition      `json:"condition"`
	LatestPoll               diagnosticExportCount          `json:"latest_poll"`
	LatestObservedRun        diagnosticExportCount          `json:"latest_observed_run"`
	Lifetime                 diagnosticExportCount          `json:"lifetime"`
	EvidenceAvailability     string                         `json:"evidence_availability"`
	LegacyDetailAvailability string                         `json:"legacy_detail_availability"`
	SamplesTruncated         bool                           `json:"samples_truncated"`
	CapacityOverflow         bool                           `json:"capacity_overflow"`
	Truncated                bool                           `json:"truncated"`
}

type diagnosticExportEnvelope struct {
	FormatVersion             int                         `json:"format_version"`
	DiagnosticContractVersion int                         `json:"diagnostic_contract_version"`
	Diagnostics               []diagnosticExportItem      `json:"diagnostics"`
	Logs                      []diagnosticExportLogRecord `json:"logs,omitempty"`
}

// DiagnosticExportPreview contains the exact bounded envelope bytes shown to
// the user before consent. Keep Bytes private so callers cannot accidentally
// mutate the preview after displaying it.
type DiagnosticExportPreview struct {
	bytes []byte
}

func (p DiagnosticExportPreview) Bytes() []byte { return append([]byte(nil), p.bytes...) }

// prepareDiagnosticExport resolves every explicit full problem ID through
// core. UI-held or stale detail objects are never accepted as export evidence.
// Associated log records are supplied separately and are validated against a
// narrow allowlist before appearing in the same preview envelope.
func prepareDiagnosticExport(store *tokenlogger.Store, problemIDs []string, availableLogs logReadResult, selectedLogRefs []logRecordRef) (DiagnosticExportPreview, error) {
	if store == nil || len(problemIDs) == 0 || len(problemIDs) > maxExportDiagnostics || len(selectedLogRefs) > maxExportLogRecords {
		return DiagnosticExportPreview{}, ErrDiagnosticExportSelection
	}
	seen := make(map[string]struct{}, len(problemIDs))
	items := make([]diagnosticExportItem, 0, len(problemIDs))
	for _, problemID := range problemIDs {
		if len(problemID) == 0 || len(problemID) > maxExportDiagnosticIDBytes {
			return DiagnosticExportPreview{}, ErrDiagnosticExportSelection
		}
		if _, ok := seen[problemID]; ok {
			return DiagnosticExportPreview{}, ErrDiagnosticExportSelection
		}
		seen[problemID] = struct{}{}
		detail, err := store.InspectDiagnosticDetail(problemID)
		if err != nil {
			switch {
			case errors.Is(err, tokenlogger.ErrDiagnosticDetailNotFound):
				return DiagnosticExportPreview{}, ErrDiagnosticExportNotFound
			case errors.Is(err, tokenlogger.ErrDiagnosticInspectionUnavailable):
				return DiagnosticExportPreview{}, ErrDiagnosticExportUnavailable
			case errors.Is(err, tokenlogger.ErrInvalidDiagnosticQuery):
				return DiagnosticExportPreview{}, ErrDiagnosticExportSelection
			default:
				return DiagnosticExportPreview{}, ErrDiagnosticExportUnavailable
			}
		}
		item, err := projectDiagnosticForExport(detail)
		if err != nil {
			return DiagnosticExportPreview{}, err
		}
		items = append(items, item)
	}
	var selectedLogs []logExportProjection
	if len(selectedLogRefs) > 0 {
		var err error
		selectedLogs, err = selectManagedLogExports(availableLogs, selectedLogRefs)
		if err != nil {
			return DiagnosticExportPreview{}, ErrDiagnosticExportSelection
		}
	}
	logs := make([]diagnosticExportLogRecord, len(selectedLogs))
	for i, record := range selectedLogs {
		projected, err := projectSelectedLogForExport(record)
		if err != nil {
			return DiagnosticExportPreview{}, err
		}
		logs[i] = projected
	}
	envelope := diagnosticExportEnvelope{
		FormatVersion:             diagnosticExportVersion,
		DiagnosticContractVersion: tokenlogger.DiagnosticContractVersion,
		Diagnostics:               items,
		Logs:                      logs,
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil || len(data) > maxDiagnosticExportBytes {
		return DiagnosticExportPreview{}, errors.New("diagnostic export exceeds the size limit")
	}
	data = append(data, '\n')
	if len(data) > maxDiagnosticExportBytes {
		return DiagnosticExportPreview{}, errors.New("diagnostic export exceeds the size limit")
	}
	return DiagnosticExportPreview{bytes: data}, nil
}

func projectDiagnosticForExport(detail tokenlogger.DiagnosticDetail) (diagnosticExportItem, error) {
	if detail.ContractVersion != tokenlogger.DiagnosticContractVersion || detail.Review.State != tokenlogger.DiagnosticReviewReviewed || detail.Review.PredatesEvidence {
		return diagnosticExportItem{}, errors.New("only reviewed diagnostic details can be exported")
	}
	if detail.Classification.Category == tokenlogger.DiagnosticCategorySecuritySensitive {
		return diagnosticExportItem{}, errors.New("security-sensitive diagnostics require private disclosure handling")
	}
	if err := detail.Classification.Validate(); err != nil {
		return diagnosticExportItem{}, ErrDiagnosticExportSelection
	}
	if !validExportCondition(detail.Condition.State, detail.Condition.Coverage, detail.Condition.Reason) ||
		!validExportAvailability(detail.Condition.Availability) ||
		!validExportAvailability(detail.EvidenceAvailability) ||
		!validExportAvailability(detail.LegacyDetailAvailability) {
		return diagnosticExportItem{}, ErrDiagnosticExportSelection
	}
	poll, err := projectExportCount(detail.LatestPoll.Count)
	if err != nil || detail.LatestPoll.Scope != tokenlogger.DiagnosticCountPoll {
		return diagnosticExportItem{}, ErrDiagnosticExportSelection
	}
	run, err := projectExportCount(detail.LatestObservedRun.Count)
	if err != nil || detail.LatestObservedRun.Scope != tokenlogger.DiagnosticCountRun {
		return diagnosticExportItem{}, ErrDiagnosticExportSelection
	}
	lifetime, err := projectExportCount(detail.Lifetime)
	if err != nil {
		return diagnosticExportItem{}, ErrDiagnosticExportSelection
	}
	classification := detail.Classification
	return diagnosticExportItem{
		Classification: diagnosticExportClassification{
			Component: string(classification.Component), Operation: string(classification.Operation),
			Reason: string(classification.Reason), Severity: string(classification.Severity),
			Category: string(classification.Category), Adapter: string(classification.Adapter),
			Retryability: string(classification.Retryability), Impact: string(classification.Impact),
			ExplanationCode: string(classification.ExplanationCode), RemediationCode: string(classification.RemediationCode),
		},
		Condition: diagnosticExportCondition{
			State: string(detail.Condition.State), Coverage: string(detail.Condition.Coverage),
			Availability: string(detail.Condition.Availability), Reason: string(detail.Condition.Reason),
		},
		LatestPoll: poll, LatestObservedRun: run, Lifetime: lifetime,
		EvidenceAvailability: string(detail.EvidenceAvailability), LegacyDetailAvailability: string(detail.LegacyDetailAvailability),
		SamplesTruncated: detail.SamplesTruncated, CapacityOverflow: detail.CapacityOverflow, Truncated: detail.Truncated,
	}, nil
}

func projectExportCount(count tokenlogger.DiagnosticExactCount) (diagnosticExportCount, error) {
	if !validExportAvailability(count.Availability) {
		return diagnosticExportCount{}, ErrDiagnosticExportSelection
	}
	if count.Value != "" {
		value, err := strconv.ParseUint(count.Value, 10, 64)
		if err != nil || strconv.FormatUint(value, 10) != count.Value {
			return diagnosticExportCount{}, ErrDiagnosticExportSelection
		}
	} else if count.Availability == tokenlogger.DiagnosticAvailabilityAvailable {
		return diagnosticExportCount{}, ErrDiagnosticExportSelection
	}
	if count.Saturated && count.Value != strconv.FormatUint(math.MaxUint64, 10) {
		return diagnosticExportCount{}, ErrDiagnosticExportSelection
	}
	return diagnosticExportCount{Value: count.Value, Availability: string(count.Availability), Saturated: count.Saturated, Truncated: count.Truncated}, nil
}

func validExportAvailability(value tokenlogger.DiagnosticAvailability) bool {
	switch value {
	case tokenlogger.DiagnosticAvailabilityAvailable, tokenlogger.DiagnosticAvailabilityNotCollected,
		tokenlogger.DiagnosticAvailabilityNotApplicable, tokenlogger.DiagnosticAvailabilityUnsupported,
		tokenlogger.DiagnosticAvailabilityRedacted, tokenlogger.DiagnosticAvailabilityLost,
		tokenlogger.DiagnosticAvailabilityUnknown:
		return true
	default:
		return false
	}
}

func validExportCondition(state tokenlogger.DiagnosticCondition, coverage tokenlogger.DiagnosticCoverageKind, reason tokenlogger.DiagnosticConditionReason) bool {
	validState := state == tokenlogger.DiagnosticConditionActive || state == tokenlogger.DiagnosticConditionResolved || state == tokenlogger.DiagnosticConditionUnknown
	validCoverage := coverage == tokenlogger.DiagnosticCoverageFullSnapshot || coverage == tokenlogger.DiagnosticCoverageUnchangedFull || coverage == tokenlogger.DiagnosticCoverageIncremental || coverage == tokenlogger.DiagnosticCoveragePartial || coverage == tokenlogger.DiagnosticCoverageCancelled || coverage == tokenlogger.DiagnosticCoverageUninspected
	validReason := reason == tokenlogger.DiagnosticConditionObserved || reason == tokenlogger.DiagnosticConditionCoveredAbsence || reason == tokenlogger.DiagnosticConditionCoverageInsufficient || reason == tokenlogger.DiagnosticConditionSourceReplaced || reason == tokenlogger.DiagnosticConditionLegacyUnavailable || reason == tokenlogger.DiagnosticConditionEvidenceLost || reason == tokenlogger.DiagnosticConditionNotEvaluated
	return validState && validCoverage && validReason
}

func projectSelectedLogForExport(record logExportProjection) (diagnosticExportLogRecord, error) {
	if !exportLogEventAllowed(record.Event) || !exportLogLevelAllowed(record.Level) || !exportLogCodeAllowed(record.Code) {
		return diagnosticExportLogRecord{}, ErrDiagnosticExportSelection
	}
	if !logReadVersion.MatchString(record.CLI) || !logReadVersion.MatchString(record.Core) {
		return diagnosticExportLogRecord{}, ErrDiagnosticExportSelection
	}
	core, err := projectExportCoreLogFields(record.CoreFields)
	if err != nil {
		return diagnosticExportLogRecord{}, err
	}
	var count *uint64
	if record.Count != nil {
		value := *record.Count
		count = &value
	}
	return diagnosticExportLogRecord{Event: record.Event, Level: record.Level, Code: record.Code,
		CLIVersion: record.CLI, CoreVersion: record.Core, Count: count, Core: core}, nil
}

var allowedCoreLogFields = map[string]struct{}{
	"core_contract_version": {}, "core_kind": {}, "core_evidence_scope": {}, "core_evidence_availability": {},
	"core_evidence_truncated": {}, "core_counts_scope": {}, "core_counts_availability": {},
	"core_counts_saturated": {}, "core_counts_truncated": {}, "core_component": {}, "core_operation": {},
	"core_reason": {}, "core_severity": {}, "core_category": {}, "core_adapter": {}, "core_retryability": {},
	"core_impact": {}, "core_explanation_code": {}, "core_remediation_code": {},
	"core_count_ignored_non_usage": {}, "core_count_unchanged_snapshot": {}, "core_count_deferred_incomplete_tail": {},
	"core_count_unsupported_usage": {}, "core_count_malformed_record": {}, "core_count_failed_source": {},
}

func projectExportCoreLogFields(fields map[string]string) (map[string]string, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	if len(fields) > len(allowedCoreLogFields) || fields["core_contract_version"] != "1" {
		return nil, ErrDiagnosticExportSelection
	}
	projected := make(map[string]string, len(fields))
	for key, value := range fields {
		if _, ok := allowedCoreLogFields[key]; !ok || len(value) > 128 {
			return nil, ErrDiagnosticExportSelection
		}
		projected[key] = value
	}
	if !oneOf(fields["core_kind"], "operation_failed", "condition_summary", "poll_summary", "operation_recovered") ||
		!oneOf(fields["core_evidence_scope"], "operation", "poll", "run", "source_generation") ||
		!oneOf(fields["core_evidence_availability"], "available", "not_collected", "not_applicable", "unsupported", "redacted", "lost", "unknown") ||
		!oneOf(fields["core_counts_scope"], "operation", "poll", "run", "lifetime") ||
		!oneOf(fields["core_counts_availability"], "available", "not_collected", "not_applicable", "unsupported", "redacted", "lost", "unknown") {
		return nil, ErrDiagnosticExportSelection
	}
	for _, key := range []string{"core_evidence_truncated", "core_counts_saturated", "core_counts_truncated"} {
		if value, ok := fields[key]; ok && value != "true" && value != "false" {
			return nil, ErrDiagnosticExportSelection
		}
	}
	for key, value := range fields {
		if strings.HasPrefix(key, "core_count_") {
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil || strconv.FormatUint(n, 10) != value {
				return nil, ErrDiagnosticExportSelection
			}
		}
	}
	classificationKeys := []string{"core_component", "core_operation", "core_reason", "core_severity", "core_category", "core_adapter", "core_retryability", "core_impact", "core_explanation_code", "core_remediation_code"}
	requiredClassificationKeys := []string{"core_component", "core_operation", "core_reason", "core_severity", "core_category", "core_retryability", "core_impact", "core_explanation_code", "core_remediation_code"}
	classificationPresent := false
	for _, key := range classificationKeys {
		if _, ok := fields[key]; ok {
			classificationPresent = true
		}
	}
	if classificationPresent {
		for _, key := range requiredClassificationKeys {
			if _, ok := fields[key]; !ok {
				return nil, ErrDiagnosticExportSelection
			}
		}
		classification := tokenlogger.DiagnosticClassification{
			Component:       tokenlogger.DiagnosticComponent(fields["core_component"]),
			Operation:       tokenlogger.DiagnosticOperation(fields["core_operation"]),
			Reason:          tokenlogger.DiagnosticReason(fields["core_reason"]),
			Severity:        tokenlogger.DiagnosticSeverity(fields["core_severity"]),
			Category:        tokenlogger.DiagnosticCategory(fields["core_category"]),
			Adapter:         tokenlogger.DiagnosticAdapter(fields["core_adapter"]),
			Retryability:    tokenlogger.DiagnosticRetryability(fields["core_retryability"]),
			Impact:          tokenlogger.DiagnosticImpact(fields["core_impact"]),
			ExplanationCode: tokenlogger.DiagnosticExplanationCode(fields["core_explanation_code"]),
			RemediationCode: tokenlogger.DiagnosticRemediationCode(fields["core_remediation_code"]),
		}
		if err := classification.Validate(); err != nil {
			return nil, ErrDiagnosticExportSelection
		}
		if classification.Category == tokenlogger.DiagnosticCategorySecuritySensitive {
			return nil, errors.New("security-sensitive log evidence requires private disclosure handling")
		}
	}
	return projected, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func exportLogEventAllowed(value string) bool {
	switch value {
	case "process.started", "process.finished", "command.failed", "settings.updated", "settings.load_failed",
		"data_directory.resolved", "collector.started", "collector.stopped", "collector.failed",
		"collector.health_changed", "collector.summary", "logging.unavailable", "logging.events_suppressed", "logging.events_dropped",
		"core.diagnostic", "core.diagnostic_loss":
		return true
	default:
		return false
	}
}

func exportLogLevelAllowed(value string) bool {
	return value == "INFO" || value == "WARN" || value == "ERROR"
}

func exportLogCodeAllowed(value string) bool {
	if value == "" {
		return true
	}
	switch value {
	case "database_open", "settings_load", "collector_acquire", "collector_heartbeat", "collector_poll", "collector_store",
		"log_write", "data_directory", "settings_update", "onboarding_save", "report_query", "report_render", "process_launch",
		"collector_readiness", "collector_release", "collector_stop", "collector_health_running", "collector_health_degraded",
		"collector_health_stale", "explicit", "environment", "current_default", "legacy_default", "import_discovery",
		"import_source", "protocol_record", "core_operation_failed", "core_condition_summary", "core_poll_summary",
		"core_operation_recovered", "core_observer_loss":
		return true
	default:
		return false
	}
}

// writeReviewedDiagnosticExport displays the exact bytes that will be written,
// then requires a separate affirmative consent value. A declined or failed
// preview never creates the destination.
func exportDiagnosticEvidence(store *tokenlogger.Store, problemIDs []string, availableLogs logReadResult, selectedLogRefs []logRecordRef, destination string, previewWriter io.Writer, confirm func() (bool, error)) error {
	preview, err := prepareDiagnosticExport(store, problemIDs, availableLogs, selectedLogRefs)
	if err != nil {
		return err
	}
	return writeReviewedDiagnosticExport(preview, destination, previewWriter, confirm)
}

func writeReviewedDiagnosticExport(preview DiagnosticExportPreview, destination string, previewWriter io.Writer, confirm func() (bool, error)) (returnErr error) {
	if len(preview.bytes) == 0 || len(preview.bytes) > maxDiagnosticExportBytes || previewWriter == nil || confirm == nil || strings.TrimSpace(destination) == "" {
		return ErrDiagnosticExportSelection
	}
	if _, err := io.Copy(previewWriter, bytes.NewReader(preview.bytes)); err != nil {
		return errors.New("could not display diagnostic export preview")
	}
	if flusher, ok := previewWriter.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return errors.New("could not display diagnostic export preview")
		}
	}
	consent, err := confirm()
	if err != nil {
		return errors.New("could not obtain diagnostic export consent")
	}
	if !consent {
		return ErrDiagnosticExportConsent
	}
	destination = filepath.Clean(destination)
	file, err := openPrivateDiagnosticExportFile(destination)
	if err != nil {
		return errors.New("could not create private diagnostic export")
	}
	createdInfo, infoErr := file.Stat()
	if infoErr != nil {
		_ = file.Close()
		if removeErr := removeCreatedDiagnosticExportFile(destination, createdInfo); removeErr != nil {
			return ErrDiagnosticExportCleanup
		}
		return errors.New("inspect new diagnostic export")
	}
	committed := false
	defer func() {
		if !committed {
			if err := removeCreatedDiagnosticExportFile(destination, createdInfo); err != nil {
				returnErr = ErrDiagnosticExportCleanup
			}
		}
	}()
	if err := writeDiagnosticExportPayload(file, preview.bytes); err != nil {
		return errors.New("could not complete diagnostic export")
	}
	committed = true
	return nil
}

type diagnosticExportWritableFile interface {
	io.Writer
	Sync() error
	Close() error
}

func writeDiagnosticExportPayload(file diagnosticExportWritableFile, payload []byte) error {
	_, writeErr := io.Copy(file, bytes.NewReader(payload))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func removeCreatedDiagnosticExportFile(path string, created os.FileInfo) error {
	current, statErr := os.Lstat(path)
	if created == nil || statErr != nil || !os.SameFile(created, current) {
		return errors.New("refusing to remove a replaced export destination")
	}
	if err := os.Remove(path); err != nil {
		return errors.New("remove incomplete diagnostic export")
	}
	return nil
}
