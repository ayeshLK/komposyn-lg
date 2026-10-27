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
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
	"github.com/mattn/go-runewidth"
)

type resourceOptions struct {
	dataDir   string
	format    string
	projectID string
	filter    tokenlogger.ResourceFilter
}

type sessionDetail struct {
	FormatVersion int                         `json:"format_version"`
	Session       tokenlogger.SessionResource `json:"session"`
	UsageByModel  tokenlogger.UsageReport     `json:"usage_by_model"`
}

func runProjectResources(args []string, stdout, stderr io.Writer) int {
	return runProjectResourcesWithLogger(args, stdout, stderr, discardApplicationLog{})
}

func runProjectResourcesWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	if len(args) == 0 || (args[0] != "list" && args[0] != "show" && args[0] != "inspect") {
		fmt.Fprintln(stderr, "usage: komposyn-lg projects {list [options]|show <project-id> [options]|inspect <project-id> [options]}")
		return 1
	}
	action, id, optionArgs := args[0], "", args[1:]
	if action == "show" || action == "inspect" {
		if len(optionArgs) == 0 || strings.HasPrefix(optionArgs[0], "--") {
			fmt.Fprintf(stderr, "projects %s requires a project ID\n", action)
			return 1
		}
		id, optionArgs = optionArgs[0], optionArgs[1:]
	}
	if action == "inspect" {
		return runProjectInspectionWithLogger(id, optionArgs, stdout, stderr, logger)
	}
	options, err := parseResourceOptions(optionArgs, action == "list")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	store, closeStore, ok := openResourceStoreWithLogger(options.dataDir, stderr, logger, appCommandProjects)
	if !ok {
		return 2
	}
	defer closeStore()
	if action == "show" {
		project, err := store.ProjectResource(id, options.filter)
		if err != nil {
			logger.Event(appLogCommandFailed, appLogCodeReportQuery, appCommandProjects, 2)
			fmt.Fprintf(stderr, "load project %q: %v\n", id, err)
			return 2
		}
		if options.format == "json" {
			err = writeResourceJSON(stdout, struct {
				FormatVersion int                         `json:"format_version"`
				Project       tokenlogger.ProjectResource `json:"project"`
			}{1, project})
		} else {
			err = writeProjectTable(stdout, []tokenlogger.ProjectResource{project})
		}
		if err != nil {
			logger.Event(appLogCommandFailed, appLogCodeReportRender, appCommandProjects, 2)
			fmt.Fprintf(stderr, "write project: %v\n", err)
			return 2
		}
		return 0
	}
	list, err := store.ListProjects(options.filter)
	if err != nil {
		logger.Event(appLogCommandFailed, appLogCodeReportQuery, appCommandProjects, 2)
		fmt.Fprintf(stderr, "list projects: %v\n", err)
		return 2
	}
	if err := writeProjectResources(stdout, list, options.format); err != nil {
		logger.Event(appLogCommandFailed, appLogCodeReportRender, appCommandProjects, 2)
		fmt.Fprintf(stderr, "write projects: %v\n", err)
		return 2
	}
	return 0
}

type projectInspectionOptions struct {
	dataDir string
	format  string
	limit   int
	offset  int
	filter  tokenlogger.ResourceFilter
}

func runProjectInspection(id string, args []string, stdout, stderr io.Writer) int {
	return runProjectInspectionWithLogger(id, args, stdout, stderr, discardApplicationLog{})
}

func runProjectInspectionWithLogger(id string, args []string, stdout, stderr io.Writer, logger applicationLog) int {
	now := time.Now().UTC()
	options := projectInspectionOptions{
		format: "text", limit: tokenlogger.DefaultInspectionPageSize,
		filter: tokenlogger.ResourceFilter{From: now.AddDate(0, 0, -7), To: now},
	}
	for index := 0; index < len(args); index += 2 {
		if index+1 >= len(args) {
			fmt.Fprintf(stderr, "missing value for %s\n", args[index])
			return 1
		}
		value := args[index+1]
		switch args[index] {
		case "--data-dir":
			options.dataDir = value
		case "--format":
			if value != "text" && value != "json" {
				fmt.Fprintf(stderr, "unsupported format %q (want text or json)\n", value)
				return 1
			}
			options.format = value
		case "--from":
			parsed, err := parseTime(value)
			if err != nil {
				fmt.Fprintf(stderr, "invalid --from: %v\n", err)
				return 1
			}
			options.filter.From = parsed
		case "--to":
			parsed, err := parseTime(value)
			if err != nil {
				fmt.Fprintf(stderr, "invalid --to: %v\n", err)
				return 1
			}
			options.filter.To = parsed
		case "--limit":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				fmt.Fprintf(stderr, "invalid --limit %q\n", value)
				return 1
			}
			options.limit = parsed
		case "--offset":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				fmt.Fprintf(stderr, "invalid --offset %q\n", value)
				return 1
			}
			options.offset = parsed
		default:
			fmt.Fprintf(stderr, "unknown option %q\n", args[index])
			return 1
		}
	}
	if !options.filter.To.After(options.filter.From) {
		fmt.Fprintln(stderr, "inspection range must be increasing")
		return 1
	}
	if options.limit < 1 || options.limit > tokenlogger.MaxInspectionPageSize {
		fmt.Fprintf(stderr, "inspection limit must be between 1 and %d\n", tokenlogger.MaxInspectionPageSize)
		return 1
	}
	if options.offset < 0 || options.offset > tokenlogger.MaxInspectionOffset {
		fmt.Fprintf(stderr, "inspection offset must be between 0 and %d\n", tokenlogger.MaxInspectionOffset)
		return 1
	}
	store, closeStore, ok := openResourceStoreWithLogger(options.dataDir, stderr, logger, appCommandProjects)
	if !ok {
		return 2
	}
	defer closeStore()
	inspection, err := store.InspectProject(tokenlogger.ProjectInspectionRequest{
		ProjectID: id, From: options.filter.From.UTC(), To: options.filter.To.UTC(),
		Limit: options.limit, Offset: options.offset,
	})
	if err != nil {
		logger.Event(appLogCommandFailed, appLogCodeReportQuery, appCommandProjects, 2)
		fmt.Fprintf(stderr, "inspect project: %v\n", err)
		return 2
	}
	if !inspection.Exists {
		fmt.Fprintln(stderr, "project is unavailable in the local database")
		return 2
	}
	if options.format == "json" {
		err = writeResourceJSON(stdout, inspection)
	} else {
		err = writeProjectInspectionText(stdout, inspection)
	}
	if err != nil {
		logger.Event(appLogCommandFailed, appLogCodeReportRender, appCommandProjects, 2)
		fmt.Fprintf(stderr, "write project inspection: %v\n", err)
		return 2
	}
	return 0
}

func writeProjectInspectionText(output io.Writer, inspection tokenlogger.ProjectInspection) error {
	write := func(format string, values ...any) error {
		_, err := fmt.Fprintf(output, format, values...)
		return err
	}
	if err := write("Project inspection (format %d)\n", inspection.FormatVersion); err != nil {
		return err
	}
	if err := write("  project ID digest: %s\n  display name digest: %s\n  display name text omitted: %t\n  recorded name origin: %s\n", inspection.ProjectIDDigest, emptyAsUnavailable(inspection.DisplayNameDigest), inspection.TextOmitted, inspection.NameOrigin); err != nil {
		return err
	}
	if err := write("  name provenance is unavailable for legacy records when it was not persisted; classification evidence is separate and cannot be inferred from a name or adapter.\n"); err != nil {
		return err
	}
	if err := write("  source classification: %s — %s\n", inspection.ClassificationEvidence.Status, inspection.ClassificationEvidence.Reason); err != nil {
		return err
	}
	if err := write("\nSelected range [%s, %s) UTC\n", inspection.SelectedRange.From.UTC().Format(time.RFC3339Nano), inspection.SelectedRange.To.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	selected := inspection.SelectedActivity
	if err := write("  events: %d; usage-bearing sessions: %d; sessionless events: %d; matching events: %t\n", selected.EventCount, selected.UsageBearingSessionCount, selected.SessionlessEventCount, selected.HasMatchingEvents); err != nil {
		return err
	}
	if err := writeInspectionActivityBounds(write, "selected", selected); err != nil {
		return err
	}
	if err := write("  usage: requests=%d; sessions=%d; total tokens=%d\n", inspection.SelectedUsage.RequestCount, inspection.SelectedUsage.SessionCount, inspection.SelectedUsage.TotalTokens); err != nil {
		return err
	}
	historical := inspection.HistoricalActivity
	if err := write("\nPersisted historical activity (coverage: %s)\n", inspection.HistoricalCoverage); err != nil {
		return err
	}
	if err := write("  stored sessions: %d; usage-bearing sessions: %d; events: %d; sessionless events: %d\n", inspection.StoredSessionCount, historical.UsageBearingSessionCount, historical.EventCount, historical.SessionlessEventCount); err != nil {
		return err
	}
	if err := writeInspectionActivityBounds(write, "historical", historical); err != nil {
		return err
	}
	if err := write("  historical activity is stored evidence only; it does not establish complete lifetime history. Name provenance is reported only when persisted and otherwise remains unavailable.\n"); err != nil {
		return err
	}
	if err := write("\nLocation evidence: %s\n", inspection.LocationEvidence.Status); err != nil {
		return err
	}
	if inspection.LocationEvidence.Reason != "" {
		if err := write("  %s\n", inspection.LocationEvidence.Reason); err != nil {
			return err
		}
	}
	for _, location := range inspection.Locations {
		if err := write("  digest=%s preferred=%t provenance=%s\n", location.Digest, location.Preferred, location.Provenance); err != nil {
			return err
		}
	}
	if err := writeInspectionPage(output, "locations", inspection.LocationsPage); err != nil {
		return err
	}
	if err := write("\nAlias evidence: %s\n", inspection.AliasEvidence.Status); err != nil {
		return err
	}
	if inspection.AliasEvidence.Reason != "" {
		if err := write("  %s\n", inspection.AliasEvidence.Reason); err != nil {
			return err
		}
	}
	for _, alias := range inspection.Aliases {
		if err := write("  resource=%s scope=%s scope_digest=%s alias_digest=%s provenance=%s\n", alias.Resource, alias.ScopeKind, alias.ScopeDigest, alias.Digest, alias.Provenance); err != nil {
			return err
		}
	}
	if err := writeInspectionPage(output, "aliases", inspection.AliasesPage); err != nil {
		return err
	}
	if err := write("\nReconciliation warnings (all time; not collection/parser outcomes)\n"); err != nil {
		return err
	}
	for _, warning := range inspection.Warnings {
		if err := write("  kind=%s scope=%s digest=%s occurrences=%d first=%s last=%s: %s\n", warning.Kind, warning.ScopeKind, warning.ScopeDigest, warning.OccurrenceCount, warning.FirstSeenAt.UTC().Format(time.RFC3339Nano), warning.LastSeenAt.UTC().Format(time.RFC3339Nano), warning.Message); err != nil {
			return err
		}
	}
	if err := writeInspectionPage(output, "warnings", inspection.WarningsPage); err != nil {
		return err
	}
	if err := write("\nSource evidence: %s\n", inspection.SourceEvidence.Status); err != nil {
		return err
	}
	if inspection.SourceEvidence.Reason != "" {
		if err := write("  %s\n", inspection.SourceEvidence.Reason); err != nil {
			return err
		}
	}
	for _, source := range inspection.Sources {
		if err := write("  adapter=%s source_id_digest=%s events=%d cursor=%s", source.Adapter, source.SourceIDDigest, source.EventCount, source.CursorStatus); err != nil {
			return err
		}
		if source.CursorByteOffset != nil {
			if err := write(" byte_offset=%d generation=%d pending_tail=%t snapshot_reusable=%t", *source.CursorByteOffset, valueOrZero(source.CursorGeneration), valueOrFalse(source.PendingTail), valueOrFalse(source.SnapshotReusable)); err != nil {
				return err
			}
		}
		if err := write("\n"); err != nil {
			return err
		}
	}
	if err := writeInspectionPage(output, "sources", inspection.SourcesPage); err != nil {
		return err
	}
	if err := write("\nRecovery classification (advisory): %s\n  %s\n", inspection.RecoveryClassification, inspection.RecoveryExplanation); err != nil {
		return err
	}
	return write("\nInspection query reads stored database evidence only; it does not scan provider files, start collection, repair identities, or access the network. Store.Open can still perform its normal migration/catalog synchronization. Digests and exact metrics may identify data; do not treat this output as automatically safe to publish.\n")
}

func writeInspectionActivityBounds(write func(string, ...any) error, label string, activity tokenlogger.ProjectInspectionActivity) error {
	first, last := "unavailable", "unavailable"
	if activity.FirstActive != nil {
		first = activity.FirstActive.UTC().Format(time.RFC3339Nano)
	}
	if activity.LastActive != nil {
		last = activity.LastActive.UTC().Format(time.RFC3339Nano)
	}
	return write("  %s activity bounds: first=%s last=%s\n", label, first, last)
}

func writeInspectionPage(output io.Writer, name string, page tokenlogger.InspectionPage) error {
	returned := max(page.Total-int64(page.Offset), 0)
	returned = min(returned, int64(page.Limit))
	if _, err := fmt.Fprintf(output, "  page: offset=%d limit=%d returned=%d total=%d has_more=%t truncated=%t", page.Offset, page.Limit, returned, page.Total, page.HasMore, page.Truncated); err != nil {
		return err
	}
	if page.NextOffset != nil {
		if _, err := fmt.Fprintf(output, " next_offset=%d (repeat --offset %d)", *page.NextOffset, *page.NextOffset); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output, " [%s]\n", name)
	return err
}

func emptyAsUnavailable(value string) string {
	if value == "" {
		return "unavailable"
	}
	return value
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func valueOrFalse(value *bool) bool { return value != nil && *value }

func runSessionResources(args []string, stdout, stderr io.Writer) int {
	return runSessionResourcesWithLogger(args, stdout, stderr, discardApplicationLog{})
}

func runSessionResourcesWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	action := args[0]
	id, optionArgs := "", args[1:]
	if action == "show" {
		if len(optionArgs) == 0 || strings.HasPrefix(optionArgs[0], "--") {
			fmt.Fprintln(stderr, "sessions show requires a session ID")
			return 1
		}
		id, optionArgs = optionArgs[0], optionArgs[1:]
	}
	options, err := parseResourceOptions(optionArgs, true)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if action == "list" && options.projectID == "" {
		fmt.Fprintln(stderr, "sessions list requires --project <project-id>")
		return 1
	}
	store, closeStore, ok := openResourceStoreWithLogger(options.dataDir, stderr, logger, appCommandSessions)
	if !ok {
		return 2
	}
	defer closeStore()
	if action == "show" {
		session, err := store.SessionResource(id, options.projectID, options.filter)
		if err != nil {
			logger.Event(appLogCommandFailed, appLogCodeReportQuery, appCommandSessions, 2)
			fmt.Fprintf(stderr, "load session %q: %v\n", id, err)
			return 2
		}
		usageFilter := tokenlogger.UsageFilter{From: options.filter.From, To: options.filter.To, ProjectID: session.ProjectID, SessionID: session.ID, Client: options.filter.Client, Provider: options.filter.Provider, Model: options.filter.Model, GroupBy: []string{tokenlogger.GroupModel, tokenlogger.GroupProvider}}
		if session.ProjectID == tokenlogger.UnattributedProjectID {
			usageFilter.ProjectID, usageFilter.ProjectMissing = "", true
		}
		if session.ID == tokenlogger.UnattributedSessionID {
			usageFilter.SessionID, usageFilter.SessionMissing = "", true
		}
		report, err := store.Query(usageFilter)
		if err != nil {
			logger.Event(appLogCommandFailed, appLogCodeReportQuery, appCommandSessions, 2)
			fmt.Fprintf(stderr, "query session usage: %v\n", err)
			return 2
		}
		if options.format == "json" {
			err = writeResourceJSON(stdout, sessionDetail{1, session, report})
		} else if options.format == "csv" {
			err = writeSessionDetailCSV(stdout, session, report)
		} else {
			err = writeSessionTable(stdout, []tokenlogger.SessionResource{session})
			if err == nil {
				err = tokenlogger.WriteTableWidth(stdout, report, terminalWidthOf(stdout))
			}
		}
		if err != nil {
			logger.Event(appLogCommandFailed, appLogCodeReportRender, appCommandSessions, 2)
			fmt.Fprintf(stderr, "write session: %v\n", err)
			return 2
		}
		return 0
	}
	list, err := store.ListSessions(options.projectID, options.filter)
	if err != nil {
		logger.Event(appLogCommandFailed, appLogCodeReportQuery, appCommandSessions, 2)
		fmt.Fprintf(stderr, "list sessions: %v\n", err)
		return 2
	}
	if err := writeSessionResources(stdout, list, options.format); err != nil {
		logger.Event(appLogCommandFailed, appLogCodeReportRender, appCommandSessions, 2)
		fmt.Fprintf(stderr, "write sessions: %v\n", err)
		return 2
	}
	return 0
}

func parseResourceOptions(args []string, csvAllowed bool) (resourceOptions, error) {
	now := time.Now().UTC()
	options := resourceOptions{format: "table", filter: tokenlogger.ResourceFilter{From: now.AddDate(0, 0, -7), To: now}}
	for index := 0; index < len(args); index += 2 {
		if index+1 >= len(args) {
			return options, fmt.Errorf("missing value for %s", args[index])
		}
		value := args[index+1]
		switch args[index] {
		case "--data-dir":
			options.dataDir = value
		case "--project":
			options.projectID = value
		case "--format":
			if value != "table" && value != "json" && (!csvAllowed || value != "csv") {
				return options, fmt.Errorf("unsupported format %q", value)
			}
			options.format = value
		case "--from":
			parsed, err := parseTime(value)
			if err != nil {
				return options, fmt.Errorf("invalid --from: %w", err)
			}
			options.filter.From = parsed
		case "--to":
			parsed, err := parseTime(value)
			if err != nil {
				return options, fmt.Errorf("invalid --to: %w", err)
			}
			options.filter.To = parsed
		case "--client":
			options.filter.Client = value
		case "--provider":
			options.filter.Provider = value
		case "--model":
			options.filter.Model = value
		case "--group-by":
			if value != "model" {
				return options, errors.New("sessions show supports only --group-by model")
			}
		default:
			return options, fmt.Errorf("unknown option %q", args[index])
		}
	}
	if !options.filter.To.After(options.filter.From) {
		return options, errors.New("resource range must be increasing")
	}
	return options, nil
}

func openResourceStore(dataDir string, stderr io.Writer) (*tokenlogger.Store, func(), bool) {
	return openResourceStoreWithLogger(dataDir, stderr, discardApplicationLog{}, appCommandOther)
}

func openResourceStoreWithLogger(dataDir string, stderr io.Writer, logger applicationLog, command appCommand) (*tokenlogger.Store, func(), bool) {
	resolution, err := resolveDataDirWithLog(dataDir, logger, command)
	if err != nil {
		fmt.Fprintf(stderr, "resolve token logger data directory: %v\n", err)
		return nil, nil, false
	}
	reportDataDirResolution(stderr, resolution)
	store, err := openStoreWithLog(filepath.Join(resolution.Path, "token-logger.db"), logger, command)
	if err != nil {
		fmt.Fprintf(stderr, "open token logger database in %s: %v\n", resolution.Path, err)
		return nil, nil, false
	}
	return store, func() { _ = store.Close() }, true
}

func writeProjectResources(output io.Writer, list tokenlogger.ProjectResourceList, format string) error {
	switch format {
	case "json":
		return writeResourceJSON(output, list)
	case "csv":
		writer := csv.NewWriter(output)
		if err := writer.Write(resourceCSVHeader("project")); err != nil {
			return err
		}
		for _, project := range list.Projects {
			if err := writer.Write(resourceCSVRow(project.ID, project.DisplayName, project.Location, project.Clients, project.Models, project.FirstActive, project.LastActive, project.Summary)); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	default:
		if _, err := fmt.Fprintln(output, projectInventoryNote); err != nil {
			return err
		}
		return writeProjectTable(output, list.Projects)
	}
}

const projectInventoryNote = "All known projects; usage summaries, usage-derived session counts, and last-active values use this range.\nWARNINGS count reconciliation only, not all collection/parser errors. Zero usage does not prove lifetime inactivity or token loss."

func writeSessionResources(output io.Writer, list tokenlogger.SessionResourceList, format string) error {
	switch format {
	case "json":
		return writeResourceJSON(output, list)
	case "csv":
		writer := csv.NewWriter(output)
		if err := writer.Write(resourceCSVHeader("session")); err != nil {
			return err
		}
		for _, session := range list.Sessions {
			if err := writer.Write(resourceCSVRow(session.ID, session.Client, session.ProjectID, []string{session.Client}, session.Models, session.FirstActive, session.LastActive, session.Summary)); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	default:
		return writeSessionTable(output, list.Sessions)
	}
}

func writeResourceJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeProjectTable(output io.Writer, projects []tokenlogger.ProjectResource) error {
	rows := [][]string{{"#", "PROJECT", "LOCATION", "ID", "SESSIONS", "REQUESTS", "TOKENS", "WARNINGS", "LAST ACTIVE"}}
	for index, project := range projects {
		rows = append(rows, []string{strconv.Itoa(index + 1), tokenlogger.FormatHumanLabel(project.DisplayName), humanLocation(project.Location), shortResourceID(project.ID), strconv.FormatInt(project.Summary.SessionCount, 10), strconv.FormatInt(project.Summary.RequestCount, 10), tokenlogger.FormatHumanTokens(project.Summary.TotalTokens), strconv.FormatInt(project.Warnings, 10), resourceTime(project.LastActive)})
	}
	return writeResourceRows(output, rows)
}

func writeSessionTable(output io.Writer, sessions []tokenlogger.SessionResource) error {
	rows := [][]string{{"#", "SESSION", "CLIENT", "STARTED", "LAST ACTIVE", "MODELS", "REQUESTS", "TOKENS"}}
	for index, session := range sessions {
		started := session.StartedAt
		if started == nil {
			started = session.FirstActive
		}
		rows = append(rows, []string{strconv.Itoa(index + 1), shortResourceID(session.ID), tokenlogger.FormatHumanLabel(session.Client), resourceTime(started), resourceTime(session.LastActive), strconv.Itoa(len(session.Models)), strconv.FormatInt(session.Summary.RequestCount, 10), tokenlogger.FormatHumanTokens(session.Summary.TotalTokens)})
	}
	return writeResourceRows(output, rows)
}

func writeInteractiveModelTable(output io.Writer, report tokenlogger.UsageReport) error {
	rows := [][]string{{"#", "MODEL", "PROVIDER", "REQUESTS", "INPUT", "CACHED", "CACHE WRITE", "OUTPUT", "REASONING", "TOTAL", "ESTIMATE", "REPORTED", "STATUS"}}
	for index, row := range report.Rows {
		model := row.Dimensions[tokenlogger.GroupModel]
		if model == "" {
			model = "(unattributed)"
		}
		provider := row.Dimensions[tokenlogger.GroupProvider]
		if provider == "" {
			provider = "(unattributed)"
		}
		estimate, reported, status := tokenlogger.FormatHumanCost(row.UsageMeasures)
		rows = append(rows, []string{strconv.Itoa(index + 1), tokenlogger.FormatHumanLabel(model), tokenlogger.FormatHumanLabel(provider), strconv.FormatInt(row.RequestCount, 10), tokenlogger.FormatHumanTokens(row.InputTokens), tokenlogger.FormatHumanTokens(row.CachedInputTokens), tokenlogger.FormatHumanTokens(row.CacheWriteInputTokens), tokenlogger.FormatHumanTokens(row.OutputTokens), tokenlogger.FormatHumanTokens(row.ReasoningOutputTokens), tokenlogger.FormatHumanTokens(row.TotalTokens), tokenlogger.FormatHumanText(estimate), tokenlogger.FormatHumanText(reported), tokenlogger.FormatHumanText(status)})
	}
	if err := writeResourceRows(output, rows); err != nil {
		return err
	}
	return nil
}

func writeResourceRows(output io.Writer, rows [][]string) error {
	if len(rows) == 1 {
		_, err := fmt.Fprintln(output, "NO RESOURCE DATA")
		return err
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for index, cell := range row {
			if width := runewidth.StringWidth(cell); width > widths[index] {
				widths[index] = width
			}
		}
	}
	for _, row := range rows {
		for index, cell := range row {
			if _, err := io.WriteString(output, cell); err != nil {
				return err
			}
			if index+1 < len(row) {
				if _, err := io.WriteString(output, strings.Repeat(" ", widths[index]-runewidth.StringWidth(cell)+2)); err != nil {
					return err
				}
			}
		}
		if _, err := io.WriteString(output, "\n"); err != nil {
			return err
		}
	}
	return nil
}

func humanLocation(location string) string {
	if location == "" {
		return "location unavailable"
	}
	home, err := os.UserHomeDir()
	if err == nil && (location == home || strings.HasPrefix(location, home+string(filepath.Separator))) {
		location = "~" + strings.TrimPrefix(location, home)
	}
	return tokenlogger.FormatHumanLabel(location)
}

func shortResourceID(id string) string {
	return tokenlogger.FormatHumanLabel(id)
}

func writeSessionDetailCSV(output io.Writer, session tokenlogger.SessionResource, report tokenlogger.UsageReport) error {
	writer := csv.NewWriter(output)
	header := []string{"format_version", "session_id", "project_id", "client", "model", "provider", "request_count", "session_count", "input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens", "reasoning_output_tokens", "total_tokens", "calculated_cost_micros", "calculated_currency", "provider_reported_cost_micros", "provider_reported_currency", "unknown_cost_requests", "estimated_cost_requests", "known_zero_cost_requests", "cost_unknown_reason", "calculation_basis", "pricing_provider", "pricing_version", "pricing_source"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range report.Rows {
		m := row.UsageMeasures
		values := []string{"1", session.ID, session.ProjectID, session.Client, row.Dimensions[tokenlogger.GroupModel], row.Dimensions[tokenlogger.GroupProvider], strconv.FormatInt(m.RequestCount, 10), strconv.FormatInt(m.SessionCount, 10), strconv.FormatInt(m.InputTokens, 10), strconv.FormatInt(m.CachedInputTokens, 10), strconv.FormatInt(m.CacheWriteInputTokens, 10), strconv.FormatInt(m.OutputTokens, 10), strconv.FormatInt(m.ReasoningOutputTokens, 10), strconv.FormatInt(m.TotalTokens, 10), strconv.FormatInt(m.CalculatedCostMicros, 10), m.CalculatedCurrency, strconv.FormatInt(m.ProviderReportedCostMicros, 10), m.ProviderReportedCurrency, strconv.FormatInt(m.UnknownCostRequests, 10), strconv.FormatInt(m.EstimatedCostRequests, 10), strconv.FormatInt(m.KnownZeroCostRequests, 10), m.CostUnknownReason, m.CalculationBasis, m.PricingProvider, m.PricingVersion, m.PricingSource}
		if err := writer.Write(values); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func resourceTime(value *time.Time) string {
	if value == nil {
		return "unavailable"
	}
	return value.UTC().Format("2006-01-02 15:04Z")
}

func resourceCSVHeader(kind string) []string {
	return []string{"format_version", kind + "_id", "name", "location_or_project_id", "clients", "models", "first_active", "last_active", "session_count", "request_count", "input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens", "reasoning_output_tokens", "total_tokens", "calculated_cost_micros", "calculated_currency", "unknown_cost_requests", "cost_status"}
}

func resourceCSVRow(id, name, location string, clients, models []string, first, last *time.Time, summary tokenlogger.UsageMeasures) []string {
	return []string{"1", id, name, location, strings.Join(clients, ";"), strings.Join(models, ";"), csvTime(first), csvTime(last), strconv.FormatInt(summary.SessionCount, 10), strconv.FormatInt(summary.RequestCount, 10), strconv.FormatInt(summary.InputTokens, 10), strconv.FormatInt(summary.CachedInputTokens, 10), strconv.FormatInt(summary.CacheWriteInputTokens, 10), strconv.FormatInt(summary.OutputTokens, 10), strconv.FormatInt(summary.ReasoningOutputTokens, 10), strconv.FormatInt(summary.TotalTokens, 10), strconv.FormatInt(summary.CalculatedCostMicros, 10), summary.CalculatedCurrency, strconv.FormatInt(summary.UnknownCostRequests, 10), summary.CostUnknownReason}
}

func csvTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
