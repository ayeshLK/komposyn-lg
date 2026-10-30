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
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const defaultDiagnosticPageSize = 20
const maxHumanDiagnosticListRows = 40

func runDiagnosticDetails(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	return runDiagnosticDetailsForStore(args, stdout, stderr, logger, nil)
}

func runDiagnosticDetailsForStore(args []string, stdout, stderr io.Writer, logger applicationLog, existing *tokenlogger.Store) int {
	action := args[0]
	if action != "list" && action != "show" && action != "review" {
		fmt.Fprintln(stderr, "usage: komposyn-lg diagnostics {list|show|review} [options]")
		return 1
	}
	options, err := parseDiagnosticDetailArgs(action, args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "diagnostics: %s\n", err)
		return 1
	}
	if existing != nil && options.dataDir != "" {
		fmt.Fprintln(stderr, "diagnostics: --data-dir cannot be used from the REPL; commands use the open data directory")
		return 1
	}
	store := existing
	closeStore := func() {}
	if store == nil {
		var ok bool
		store, closeStore, ok = openResourceStoreWithLogger(options.dataDir, stderr, logger, appCommandOther)
		if !ok {
			return 2
		}
	}
	defer closeStore()

	switch action {
	case "list":
		page, err := store.ListDiagnosticDetails(options.query)
		if err != nil {
			logDiagnosticQueryFailure(logger, existing != nil)
			return writeDiagnosticCoreError(stderr, "list diagnostics", err)
		}
		if !supportedDiagnosticPage(page) {
			fmt.Fprintln(stderr, "list diagnostics: unsupported diagnostic contract version")
			return 2
		}
		if options.format == "json" {
			if err := writeResourceJSON(stdout, page); err != nil {
				logDiagnosticRenderFailure(logger, existing != nil)
				fmt.Fprintln(stderr, "write diagnostic list: output error")
				return 2
			}
			return 0
		}
		if err := writeDiagnosticPageText(stdout, page); err != nil {
			logDiagnosticRenderFailure(logger, existing != nil)
			fmt.Fprintln(stderr, "write diagnostic list: output error")
			return 2
		}
		return 0
	case "show":
		detail, err := store.InspectDiagnosticDetail(options.problemID)
		if err != nil {
			logDiagnosticQueryFailure(logger, existing != nil)
			return writeDiagnosticCoreError(stderr, "inspect diagnostic", err)
		}
		if detail.ContractVersion != tokenlogger.DiagnosticContractVersion {
			fmt.Fprintln(stderr, "inspect diagnostic: unsupported diagnostic contract version")
			return 2
		}
		if options.format == "json" {
			if err := writeResourceJSON(stdout, detail); err != nil {
				logDiagnosticRenderFailure(logger, existing != nil)
				fmt.Fprintln(stderr, "write diagnostic detail: output error")
				return 2
			}
			return 0
		}
		if err := writeDiagnosticDetailText(stdout, detail); err != nil {
			logDiagnosticRenderFailure(logger, existing != nil)
			fmt.Fprintln(stderr, "write diagnostic detail: output error")
			return 2
		}
		return 0
	case "review":
		err := store.ReviewDiagnosticDetail(tokenlogger.DiagnosticReviewRequest{
			ProblemID: options.problemID, ExpectedEvidenceRevision: options.expectedRevision,
			State: options.reviewState,
		})
		if err != nil {
			logDiagnosticQueryFailure(logger, existing != nil)
			return writeDiagnosticCoreError(stderr, "review diagnostic", err)
		}
		fmt.Fprintf(stdout, "diagnostic %s marked %s at evidence revision %s; this records review only and does not change its operational condition.\n", boundedTerminalText(options.problemID, 96), options.reviewState, boundedTerminalText(options.expectedRevision, 96))
		return 0
	}
	return 1
}

type diagnosticDetailOptions struct {
	dataDir          string
	format           string
	problemID        string
	expectedRevision string
	reviewState      tokenlogger.DiagnosticReviewState
	query            tokenlogger.DiagnosticQuery
}

func supportedDiagnosticPage(page tokenlogger.DiagnosticPage) bool {
	if page.ContractVersion != tokenlogger.DiagnosticContractVersion {
		return false
	}
	for _, item := range page.Items {
		if item.ContractVersion != tokenlogger.DiagnosticContractVersion {
			return false
		}
	}
	return true
}

func parseDiagnosticDetailArgs(action string, args []string) (diagnosticDetailOptions, error) {
	options := diagnosticDetailOptions{format: "text", query: tokenlogger.DiagnosticQuery{PageSize: defaultDiagnosticPageSize}}
	if action == "show" || action == "review" {
		if len(args) == 0 || args[0] == "" || strings.HasPrefix(args[0], "--") {
			return options, fmt.Errorf("%s requires a problem ID", action)
		}
		options.problemID, args = args[0], args[1:]
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--help" || arg == "-h" {
			return options, errors.New("help")
		}
		if index+1 >= len(args) {
			return options, fmt.Errorf("missing value for %s", arg)
		}
		value := args[index+1]
		index++
		switch arg {
		case "--data-dir":
			options.dataDir = value
		case "--format":
			if action == "review" {
				return options, errors.New("--format is not available for diagnostics review")
			}
			if value != "text" && value != "json" {
				return options, fmt.Errorf("unsupported format %q (use text or json)", boundedTerminalText(value, 32))
			}
			options.format = value
		case "--review-state":
			if action != "list" {
				return options, errors.New("--review-state is available only for diagnostics list")
			}
			options.query.ReviewState = tokenlogger.DiagnosticReviewState(value)
		case "--condition":
			if action != "list" {
				return options, errors.New("--condition is available only for diagnostics list")
			}
			options.query.Condition = tokenlogger.DiagnosticCondition(value)
		case "--category":
			if action != "list" {
				return options, errors.New("--category is available only for diagnostics list")
			}
			options.query.Category = tokenlogger.DiagnosticCategory(value)
		case "--reason":
			if action != "list" {
				return options, errors.New("--reason is available only for diagnostics list")
			}
			options.query.Reason = tokenlogger.DiagnosticReason(value)
		case "--subject-reference":
			if action != "list" {
				return options, errors.New("--subject-reference is available only for diagnostics list")
			}
			options.query.SubjectReference = value
		case "--page-size":
			if action != "list" {
				return options, errors.New("--page-size is available only for diagnostics list")
			}
			size, err := strconv.Atoi(value)
			if err != nil || size < 1 || size > tokenlogger.MaxDiagnosticPageSize {
				return options, fmt.Errorf("page size must be between 1 and %d", tokenlogger.MaxDiagnosticPageSize)
			}
			options.query.PageSize = size
		case "--continuation":
			if action != "list" {
				return options, errors.New("--continuation is available only for diagnostics list")
			}
			if len(value) > tokenlogger.MaxDiagnosticContinuationBytes {
				return options, fmt.Errorf("continuation token exceeds %d bytes", tokenlogger.MaxDiagnosticContinuationBytes)
			}
			options.query.ContinuationToken = value
		case "--state":
			if action != "review" {
				return options, errors.New("--state is available only for diagnostics review")
			}
			options.reviewState = tokenlogger.DiagnosticReviewState(value)
		case "--expected-revision":
			if action != "review" {
				return options, errors.New("--expected-revision is available only for diagnostics review")
			}
			options.expectedRevision = value
		default:
			return options, fmt.Errorf("unknown option %q", boundedTerminalText(arg, 48))
		}
	}
	if action == "review" && options.reviewState != tokenlogger.DiagnosticReviewReviewed && options.reviewState != tokenlogger.DiagnosticReviewSkipped {
		return options, errors.New("review state must be reviewed or skipped")
	}
	if action == "review" && options.expectedRevision == "" {
		return options, errors.New("diagnostics review requires --expected-revision from the detail you inspected")
	}
	return options, nil
}

func writeDiagnosticCoreError(stderr io.Writer, operation string, err error) int {
	switch {
	case errors.Is(err, tokenlogger.ErrInvalidDiagnosticQuery):
		fmt.Fprintf(stderr, "%s: invalid filter or continuation token\n", operation)
	case errors.Is(err, tokenlogger.ErrDiagnosticDetailNotFound):
		fmt.Fprintf(stderr, "%s: diagnostic detail not found\n", operation)
	case errors.Is(err, tokenlogger.ErrDiagnosticInspectionUnavailable):
		fmt.Fprintf(stderr, "%s: diagnostic evidence is unavailable\n", operation)
	case errors.Is(err, tokenlogger.ErrDiagnosticReviewConflict):
		fmt.Fprintf(stderr, "%s: evidence changed during review; inspect it again and retry\n", operation)
	default:
		fmt.Fprintf(stderr, "%s: operation failed\n", operation)
	}
	return 2
}

func writeDiagnosticPageText(output io.Writer, page tokenlogger.DiagnosticPage) error {
	if _, err := fmt.Fprintf(output, "Diagnostic details (contract %d; evidence=%s; truncated=%t; overflow=%t; overflow_count=%s/%s/saturated=%t/truncated=%t)\n", page.ContractVersion, diagnosticAvailabilityText(page.EvidenceAvailability), page.Truncated, page.Overflow, boundedTerminalText(page.OverflowCount.Value, 32), diagnosticAvailabilityText(page.OverflowCount.Availability), page.OverflowCount.Saturated, page.OverflowCount.Truncated); err != nil {
		return err
	}
	if len(page.Items) == 0 {
		_, err := fmt.Fprintln(output, "No diagnostic details on this page.")
		return err
	}
	limit := min(len(page.Items), maxHumanDiagnosticListRows)
	for _, item := range page.Items[:limit] {
		if err := writeDiagnosticListItem(output, item); err != nil {
			return err
		}
	}
	if limit < len(page.Items) {
		if _, err := fmt.Fprintf(output, "Human output is limited to %d items; use --format json or a smaller --page-size.\n", maxHumanDiagnosticListRows); err != nil {
			return err
		}
	}
	if page.NextToken != "" {
		_, err := fmt.Fprintf(output, "More results are available. Continue with --continuation %q (page-size remains %d).\n", boundedTerminalText(page.NextToken, tokenlogger.MaxDiagnosticContinuationBytes), len(page.Items))
		return err
	}
	return nil
}

func writeDiagnosticListItem(output io.Writer, item tokenlogger.DiagnosticDetail) error {
	_, err := fmt.Fprintf(output, "%s  %s/%s  condition=%s review=%s evidence_revision=%s lifetime=%s (%s)\n",
		boundedTerminalText(string(item.ProblemID), 96), boundedTerminalText(string(item.Classification.Category), 48),
		boundedTerminalText(string(item.Classification.Reason), 64), boundedTerminalText(string(item.Condition.State), 32),
		boundedTerminalText(string(item.Review.State), 16), boundedTerminalText(item.Review.EvidenceRevision, 96),
		boundedTerminalText(item.Lifetime.Value, 32), diagnosticAvailabilityText(item.Lifetime.Availability))
	return err
}

func writeDiagnosticDetailText(output io.Writer, detail tokenlogger.DiagnosticDetail) error {
	write := func(format string, args ...any) error { _, err := fmt.Fprintf(output, format, args...); return err }
	if err := write("Diagnostic detail (contract %d)\n", detail.ContractVersion); err != nil {
		return err
	}
	if err := write("  problem ID: %s\n  component/operation/reason: %s/%s/%s\n  category/severity: %s/%s\n", boundedTerminalText(string(detail.ProblemID), 96), boundedTerminalText(string(detail.Classification.Component), 32), boundedTerminalText(string(detail.Classification.Operation), 32), boundedTerminalText(string(detail.Classification.Reason), 64), boundedTerminalText(string(detail.Classification.Category), 48), boundedTerminalText(string(detail.Classification.Severity), 16)); err != nil {
		return err
	}
	if err := write("  adapter: %s; retryability: %s; impact: %s; explanation: %s; remediation: %s\n", boundedTerminalText(string(detail.Classification.Adapter), 32), boundedTerminalText(string(detail.Classification.Retryability), 32), boundedTerminalText(string(detail.Classification.Impact), 32), boundedTerminalText(string(detail.Classification.ExplanationCode), 48), boundedTerminalText(string(detail.Classification.RemediationCode), 48)); err != nil {
		return err
	}
	if err := write("  subject: kind=%s reference=%s scope=%s availability=%s\n", boundedTerminalText(string(detail.Subject.Kind), 32), boundedTerminalText(string(detail.Subject.Reference), 96), boundedTerminalText(string(detail.Subject.IdentityScope), 32), diagnosticAvailabilityText(detail.Subject.Availability)); err != nil {
		return err
	}
	if err := write("  condition: %s reason=%s scope=%s coverage=%s availability=%s generation=%s coverage_offset=%s as_of=%s\n", boundedTerminalText(string(detail.Condition.State), 32), boundedTerminalText(string(detail.Condition.Reason), 48), boundedTerminalText(string(detail.Condition.Scope), 32), boundedTerminalText(string(detail.Condition.Coverage), 32), diagnosticAvailabilityText(detail.Condition.Availability), diagnosticOptional(detail.Condition.Generation), diagnosticOptional(detail.Condition.CoverageOffset), diagnosticTime(detail.Condition.AsOf)); err != nil {
		return err
	}
	if err := write("  review: %s revision=%s reviewed_at=%s predates_evidence=%t\n", boundedTerminalText(string(detail.Review.State), 16), boundedTerminalText(detail.Review.EvidenceRevision, 96), diagnosticTime(detail.Review.ReviewedAt), detail.Review.PredatesEvidence); err != nil {
		return err
	}
	if err := write("  counts: latest_poll=%s (%s, scope=%s/%s, saturated=%t, truncated=%t) latest_observed_run=%s (%s, scope=%s/%s, saturated=%t, truncated=%t) lifetime=%s (%s, saturated=%t, truncated=%t)\n", boundedTerminalText(detail.LatestPoll.Count.Value, 32), diagnosticAvailabilityText(detail.LatestPoll.Count.Availability), boundedTerminalText(string(detail.LatestPoll.Scope), 32), boundedTerminalText(string(detail.LatestPoll.ScopeID), 96), detail.LatestPoll.Count.Saturated, detail.LatestPoll.Count.Truncated, boundedTerminalText(detail.LatestObservedRun.Count.Value, 32), diagnosticAvailabilityText(detail.LatestObservedRun.Count.Availability), boundedTerminalText(string(detail.LatestObservedRun.Scope), 32), boundedTerminalText(string(detail.LatestObservedRun.ScopeID), 96), detail.LatestObservedRun.Count.Saturated, detail.LatestObservedRun.Count.Truncated, boundedTerminalText(detail.Lifetime.Value, 32), diagnosticAvailabilityText(detail.Lifetime.Availability), detail.Lifetime.Saturated, detail.Lifetime.Truncated); err != nil {
		return err
	}
	if err := write("  last positive: %s; legacy detail: %s; evidence: %s; truncated: %t; sample truncation: %t; capacity overflow: %t\n", diagnosticTime(detail.LastPositiveAt), diagnosticAvailabilityText(detail.LegacyDetailAvailability), diagnosticAvailabilityText(detail.EvidenceAvailability), detail.Truncated, detail.SamplesTruncated, detail.CapacityOverflow); err != nil {
		return err
	}
	for i, sample := range detail.Samples {
		if i >= 4 {
			break
		}
		if err := write("  sample %d: byte_offset=%s line_number=%s generation=%s coverage=%s coverage_availability=%s\n", i+1, diagnosticOptional(sample.Position.ByteOffset), diagnosticOptional(sample.Position.LineNumber), diagnosticOptional(sample.Generation), boundedTerminalText(string(sample.Coverage), 32), diagnosticAvailabilityText(sample.CoverageAvailability)); err != nil {
			return err
		}
	}
	return write("Condition and review are separate: review does not resolve an active/unknown condition. Unknown legacy provenance and unavailable counters remain unavailable. Diagnostic details are local and potentially identifying; review them before sharing.\n")
}

func diagnosticTime(value time.Time) string {
	if value.IsZero() {
		return "unavailable"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func diagnosticOptional(value tokenlogger.DiagnosticOptionalUint64) string {
	if value.Availability != tokenlogger.DiagnosticAvailabilityAvailable {
		return diagnosticAvailabilityText(value.Availability)
	}
	result := strconv.FormatUint(value.Value, 10)
	if value.Saturated || value.Truncated {
		result += " ("
		if value.Saturated {
			result += "saturated"
		}
		if value.Saturated && value.Truncated {
			result += ","
		}
		if value.Truncated {
			result += "truncated"
		}
		result += ")"
	}
	return result
}

func diagnosticAvailabilityText(value tokenlogger.DiagnosticAvailability) string {
	switch value {
	case tokenlogger.DiagnosticAvailabilityAvailable, tokenlogger.DiagnosticAvailabilityNotCollected,
		tokenlogger.DiagnosticAvailabilityNotApplicable, tokenlogger.DiagnosticAvailabilityUnsupported,
		tokenlogger.DiagnosticAvailabilityRedacted, tokenlogger.DiagnosticAvailabilityLost,
		tokenlogger.DiagnosticAvailabilityUnknown:
		return string(value)
	default:
		return "unknown"
	}
}

func boundedTerminalText(value string, maxBytes int) string {
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, value)
	if len(value) > maxBytes {
		end := maxBytes
		for end > 0 && !utf8.ValidString(value[:end]) {
			end--
		}
		value = value[:end]
	}
	return value
}

func logDiagnosticQueryFailure(logger applicationLog, interactive bool) {
	code := appLogOperationExitCodeUnset
	if !interactive {
		code = 2
	}
	logger.Event(appLogCommandFailed, appLogCodeReportQuery, appCommandOther, code)
}

func logDiagnosticRenderFailure(logger applicationLog, interactive bool) {
	code := appLogOperationExitCodeUnset
	if !interactive {
		code = 2
	}
	logger.Event(appLogCommandFailed, appLogCodeReportRender, appCommandOther, code)
}
