package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	AppExportVersion = 1
	FullKind         = "full_db"
	AppKind          = "ace3_app"
)

var ErrValidation = errors.New("validation failed")

type Service struct {
	db       *sqlx.DB
	dsn      string
	dir      string
	lookPath func(string) (string, error)
	runCmd   func(context.Context, string, ...string) error
	now      func() time.Time
}

type Artifact struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Filename    string    `json:"filename"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
	DownloadURL string    `json:"download_url"`
}

type FullValidation struct {
	OK       bool     `json:"ok"`
	Artifact Artifact `json:"artifact"`
	Warnings []string `json:"warnings"`
}

type RestorePlan struct {
	Artifact Artifact `json:"artifact"`
	Command  string   `json:"command"`
	Runbook  string   `json:"runbook"`
}

type AppExport struct {
	Version   int                                     `json:"version"`
	CreatedAt time.Time                               `json:"created_at"`
	Source    string                                  `json:"source"`
	Bundles   map[string]map[string][]json.RawMessage `json:"bundles"`
}

type BundleSelection struct {
	Bundles []string `json:"bundles"`
}

type AppValidation struct {
	OK               bool     `json:"ok"`
	Version          int      `json:"version"`
	AvailableBundles []string `json:"available_bundles"`
	SelectedBundles  []string `json:"selected_bundles"`
	EffectiveBundles []string `json:"effective_bundles"`
	Warnings         []string `json:"warnings"`
	Errors           []string `json:"errors"`
}

type TableDiff struct {
	Table   string `json:"table"`
	Insert  int    `json:"insert"`
	Update  int    `json:"update"`
	Skipped int    `json:"skipped"`
}

type AppDryRun struct {
	Validation AppValidation `json:"validation"`
	Tables     []TableDiff   `json:"tables"`
	Warnings   []string      `json:"warnings"`
}

type AppImportResult struct {
	PreRestoreBackup Artifact  `json:"pre_restore_backup"`
	DryRun           AppDryRun `json:"dry_run"`
	ImportedAt       time.Time `json:"imported_at"`
}

type Table struct {
	Name         string
	Bundle       string
	Columns      []string
	PK           []string
	OrderBy      string
	NullOnImport []string
}

var tables = []Table{
	{Name: "skill_sources", Bundle: "configuration", PK: []string{"id"}, Columns: []string{"id", "name", "upstream_url", "pinned_sha", "last_synced_at", "kind", "has_update", "created_at", "updated_at", "path_filter"}, OrderBy: "name"},
	{Name: "skills", Bundle: "configuration", PK: []string{"id"}, Columns: []string{"id", "source_id", "name", "path_in_source", "version", "archived", "created_at", "updated_at", "ignored"}, OrderBy: "source_id, path_in_source"},
	{Name: "agents", Bundle: "configuration", PK: []string{"id"}, Columns: []string{"id", "name", "role", "role_prompt", "cli_kind", "cli_profile", "enabled", "created_at", "updated_at"}, OrderBy: "role, name"},
	{Name: "agent_skills", Bundle: "configuration", PK: []string{"agent_id", "skill_id"}, Columns: []string{"agent_id", "skill_id"}, OrderBy: "agent_id, skill_id"},
	{Name: "lifecycles", Bundle: "configuration", PK: []string{"id"}, Columns: []string{"id", "description", "is_default", "created_at", "updated_at"}, OrderBy: "is_default DESC, id"},
	{Name: "lifecycle_steps", Bundle: "configuration", PK: []string{"id"}, Columns: []string{"id", "lifecycle_id", "position", "agent_id", "cli_kind", "skip_when", "include_when", "model_id", "created_at", "updated_at"}, OrderBy: "lifecycle_id, position"},
	{Name: "app_settings", Bundle: "configuration", PK: []string{"key"}, Columns: []string{"key", "value", "updated_at"}, OrderBy: "key"},
	{Name: "projects", Bundle: "projects", PK: []string{"id"}, Columns: []string{"id", "name", "description", "default_cli_kind", "default_branch_strategy", "created_at", "updated_at"}, OrderBy: "updated_at DESC"},
	{Name: "repos", Bundle: "projects", PK: []string{"id"}, Columns: []string{"id", "project_id", "local_path", "default_branch", "status", "created_at", "updated_at"}, OrderBy: "project_id, local_path"},
	{Name: "tasks", Bundle: "tasks", PK: []string{"id"}, Columns: []string{"id", "project_id", "repo_id", "title", "description", "status", "assignee_agent_id", "parent_id", "priority", "retry_count", "created_at", "updated_at", "tags", "lifecycle_id", "checkout_run_id", "execution_run_id", "execution_state"}, OrderBy: "parent_id NULLS FIRST, created_at"},
	{Name: "comments", Bundle: "tasks", PK: []string{"id"}, Columns: []string{"id", "task_id", "author", "body", "created_at"}, OrderBy: "task_id, created_at"},
	{Name: "task_artifacts", Bundle: "tasks", PK: []string{"id"}, Columns: []string{"id", "task_id", "kind", "title", "body", "format", "metadata", "created_by", "run_id", "created_at", "updated_at"}, OrderBy: "task_id, created_at"},
	{Name: "task_interactions", Bundle: "tasks", PK: []string{"id"}, Columns: []string{"id", "task_id", "kind", "status", "title", "summary", "payload", "continuation_policy", "idempotency_key", "source_comment_id", "source_run_id", "created_by", "resolved_by", "resolved_at", "created_at", "updated_at"}, OrderBy: "task_id, created_at", NullOnImport: []string{"source_run_id"}},
	{Name: "agent_wakeups", Bundle: "execution_history", PK: []string{"id"}, Columns: []string{"id", "agent_id", "task_id", "source", "reason", "payload_json", "context_snapshot", "idempotency_key", "requester_type", "requester_id", "status", "coalesced_count", "run_id", "claimed_at", "finished_at", "error", "created_at", "updated_at"}, OrderBy: "task_id, created_at", NullOnImport: []string{"run_id"}},
	{Name: "runs", Bundle: "execution_history", PK: []string{"id"}, Columns: []string{"id", "agent_id", "task_id", "wakeup_id", "status", "cli_kind", "started_at", "finished_at", "exit_code", "tokens_in", "tokens_out", "cost_usd", "prompt_hash", "worktree_path", "log_path", "created_at"}, OrderBy: "task_id, created_at"},
	{Name: "run_events", Bundle: "execution_history", PK: []string{"id"}, Columns: []string{"id", "run_id", "ts", "level", "message"}, OrderBy: "run_id, id"},
	{Name: "agent_runtime_state", Bundle: "execution_history", PK: []string{"agent_id", "task_id", "adapter_type"}, Columns: []string{"agent_id", "task_id", "adapter_type", "session_id", "state_json", "last_run_id", "last_run_status", "updated_at"}, OrderBy: "agent_id, task_id, adapter_type"},
}

var bundleOrder = []string{"configuration", "projects", "tasks", "execution_history"}

func New(db *sqlx.DB, dsn, dir string) *Service {
	return &Service{
		db:       db,
		dsn:      dsn,
		dir:      dir,
		lookPath: exec.LookPath,
		runCmd: func(ctx context.Context, name string, args ...string) error {
			var stderr bytes.Buffer
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				if msg := strings.TrimSpace(stderr.String()); msg != "" {
					return fmt.Errorf("%w: %s", err, msg)
				}
				return err
			}
			return nil
		},
		now: time.Now,
	}
}

func (s *Service) List() ([]Artifact, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		if !isBackupFilename(entry.Name()) {
			continue
		}
		artifact, err := s.artifactFor(entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Service) CreateFull(ctx context.Context) (Artifact, error) {
	if _, err := s.lookPath("pg_dump"); err != nil {
		return Artifact{}, fmt.Errorf("pg_dump is not available on PATH")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Artifact{}, err
	}
	name := fmt.Sprintf("full-db-%s.dump", s.now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(s.dir, name)
	tmp := path + ".tmp"
	if err := s.runCmd(ctx, "pg_dump", "--format=custom", "--no-owner", "--no-privileges", "--file", tmp, s.dsn); err != nil {
		_ = os.Remove(tmp)
		return Artifact{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return Artifact{}, err
	}
	return s.artifactFor(name)
}

func (s *Service) StoreUploaded(kind, original string, reader io.Reader) (Artifact, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Artifact{}, err
	}
	ext := filepath.Ext(original)
	if kind == FullKind {
		ext = ".dump"
	} else if ext != ".json" {
		ext = ".json"
	}
	prefix := "uploaded-app"
	if kind == FullKind {
		prefix = "uploaded-full-db"
	}
	name := fmt.Sprintf("%s-%s%s", prefix, s.now().UTC().Format("20060102T150405Z"), ext)
	path := filepath.Join(s.dir, name)
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Artifact{}, err
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if copyErr != nil {
			return Artifact{}, copyErr
		}
		return Artifact{}, closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return Artifact{}, err
	}
	return s.artifactFor(name)
}

func (s *Service) ValidateFull(id string) (FullValidation, error) {
	artifact, err := s.artifactForID(id)
	if err != nil {
		return FullValidation{}, err
	}
	warnings := []string{}
	if artifact.Kind != FullKind {
		warnings = append(warnings, "Selected artifact is not a full database dump.")
	}
	if artifact.SizeBytes == 0 {
		warnings = append(warnings, "Backup file is empty.")
	}
	return FullValidation{OK: len(warnings) == 0, Artifact: artifact, Warnings: warnings}, nil
}

func (s *Service) RestorePlan(id string) (RestorePlan, error) {
	validation, err := s.ValidateFull(id)
	if err != nil {
		return RestorePlan{}, err
	}
	if !validation.OK {
		return RestorePlan{}, fmt.Errorf("%w: full database backup is not valid", ErrValidation)
	}
	path := filepath.Join(s.dir, validation.Artifact.Filename)
	command := fmt.Sprintf("pg_restore --clean --if-exists --no-owner --no-privileges --dbname \"$MP_DB_DSN\" %q", path)
	runbook := strings.Join([]string{
		"1. Stop the backend and workers so no writes are running.",
		"2. Create an out-of-band volume or database backup.",
		"3. On the server, export MP_DB_DSN for the target database.",
		"4. Run the generated pg_restore command.",
		"5. Start the app and verify /healthz plus the Backup & Restore page.",
	}, "\n")
	return RestorePlan{Artifact: validation.Artifact, Command: command, Runbook: runbook}, nil
}

func (s *Service) ExportApp(ctx context.Context, selection []string) (Artifact, error) {
	doc, err := s.ExportDocument(ctx, selection)
	if err != nil {
		return Artifact{}, err
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return Artifact{}, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Artifact{}, err
	}
	name := fmt.Sprintf("ace3-app-%s.json", s.now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(s.dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return Artifact{}, err
	}
	return s.artifactFor(name)
}

func (s *Service) ExportDocument(ctx context.Context, selection []string) (AppExport, error) {
	selected, err := normalizeSelection(selection)
	if err != nil {
		return AppExport{}, err
	}
	bundles := make(map[string]map[string][]json.RawMessage, len(selected))
	for _, bundle := range selected {
		bundles[bundle] = map[string][]json.RawMessage{}
	}
	for _, table := range tables {
		if !contains(selected, table.Bundle) {
			continue
		}
		query := fmt.Sprintf("SELECT row_to_json(t) FROM (SELECT %s FROM %s ORDER BY %s) t", strings.Join(table.Columns, ","), table.Name, table.OrderBy)
		var rows []json.RawMessage
		if err := s.db.SelectContext(ctx, &rows, query); err != nil {
			return AppExport{}, err
		}
		bundles[table.Bundle][table.Name] = rows
	}
	return AppExport{Version: AppExportVersion, CreatedAt: s.now().UTC(), Source: "ace3-orchestrator", Bundles: bundles}, nil
}

func (s *Service) ReadAppArtifact(id string) (AppExport, error) {
	artifact, err := s.artifactForID(id)
	if err != nil {
		return AppExport{}, err
	}
	if artifact.Kind != AppKind {
		return AppExport{}, fmt.Errorf("%w: selected artifact is not an ACE3 app export", ErrValidation)
	}
	body, err := os.ReadFile(filepath.Join(s.dir, artifact.Filename))
	if err != nil {
		return AppExport{}, err
	}
	var doc AppExport
	if err := json.Unmarshal(body, &doc); err != nil {
		return AppExport{}, fmt.Errorf("%w: invalid ACE3 export JSON", ErrValidation)
	}
	return doc, nil
}

func (s *Service) ValidateApp(doc AppExport, selection []string) AppValidation {
	available := availableBundles(doc)
	selected, selErr := normalizeSelection(selection)
	if len(selection) == 0 {
		selected = available
	}
	effective := append([]string(nil), selected...)
	warnings := []string{}
	errorsOut := []string{}
	if selErr != nil {
		errorsOut = append(errorsOut, selErr.Error())
	}
	if doc.Version != AppExportVersion {
		errorsOut = append(errorsOut, fmt.Sprintf("unsupported ACE3 export version %d", doc.Version))
	}
	if len(available) == 0 {
		errorsOut = append(errorsOut, "export contains no known bundles")
	}
	for _, bundle := range selected {
		if !contains(available, bundle) {
			errorsOut = append(errorsOut, fmt.Sprintf("selected bundle %q is not present in export", bundle))
		}
	}
	effective, warnings, errorsOut = expandBundleDependencies(selected, available, warnings, errorsOut)
	return AppValidation{
		OK:               len(errorsOut) == 0,
		Version:          doc.Version,
		AvailableBundles: available,
		SelectedBundles:  selected,
		EffectiveBundles: effective,
		Warnings:         warnings,
		Errors:           errorsOut,
	}
}

func (s *Service) DryRunApp(ctx context.Context, doc AppExport, selection []string) (AppDryRun, error) {
	validation := s.ValidateApp(doc, selection)
	if !validation.OK {
		return AppDryRun{Validation: validation, Warnings: validation.Warnings}, nil
	}
	tablesOut := []TableDiff{}
	normalized := normalizeDocumentForImport(doc, validation.EffectiveBundles)
	for _, table := range tables {
		if !contains(validation.EffectiveBundles, table.Bundle) {
			continue
		}
		rows := normalized.Bundles[table.Bundle][table.Name]
		insert, update, err := s.diffTable(ctx, table, rows)
		if err != nil {
			return AppDryRun{}, err
		}
		tablesOut = append(tablesOut, TableDiff{Table: table.Name, Insert: insert, Update: update})
	}
	return AppDryRun{Validation: validation, Tables: tablesOut, Warnings: validation.Warnings}, nil
}

func (s *Service) ImportApp(ctx context.Context, doc AppExport, selection []string, confirmation string) (AppImportResult, error) {
	if confirmation != "RESTORE" {
		return AppImportResult{}, fmt.Errorf("%w: confirmation must be RESTORE", ErrValidation)
	}
	active, err := s.ActiveExecutionCount(ctx)
	if err != nil {
		return AppImportResult{}, err
	}
	if active > 0 {
		return AppImportResult{}, fmt.Errorf("%w: app restore is blocked while runs or wakeups are active", ErrValidation)
	}
	dryRun, err := s.DryRunApp(ctx, doc, selection)
	if err != nil {
		return AppImportResult{}, err
	}
	if !dryRun.Validation.OK {
		return AppImportResult{}, fmt.Errorf("%w: app export failed validation", ErrValidation)
	}
	preBackup, err := s.ExportApp(ctx, nil)
	if err != nil {
		return AppImportResult{}, err
	}
	normalized := normalizeDocumentForImport(doc, dryRun.Validation.EffectiveBundles)
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return AppImportResult{}, err
	}
	for _, table := range tables {
		if !contains(dryRun.Validation.EffectiveBundles, table.Bundle) {
			continue
		}
		rows := normalized.Bundles[table.Bundle][table.Name]
		if len(rows) == 0 {
			continue
		}
		if err := upsertRows(ctx, tx, table, rows); err != nil {
			_ = tx.Rollback()
			return AppImportResult{}, err
		}
	}
	if err := restoreWakeupRunLinks(ctx, tx, normalized); err != nil {
		_ = tx.Rollback()
		return AppImportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AppImportResult{}, err
	}
	return AppImportResult{PreRestoreBackup: preBackup, DryRun: dryRun, ImportedAt: s.now().UTC()}, nil
}

func (s *Service) ActiveExecutionCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.GetContext(ctx, &count, `SELECT
		(SELECT count(*) FROM runs WHERE status IN ('queued','running')) +
		(SELECT count(*) FROM agent_wakeups WHERE status IN ('queued','claimed','running'))`)
	return count, err
}

func (s *Service) ArtifactPath(id string) (string, Artifact, error) {
	artifact, err := s.artifactForID(id)
	if err != nil {
		return "", Artifact{}, err
	}
	return filepath.Join(s.dir, artifact.Filename), artifact, nil
}

func (s *Service) artifactForID(id string) (Artifact, error) {
	name := filepath.Base(strings.TrimSpace(id))
	if name == "." || name == "" || name != strings.TrimSpace(id) || !isBackupFilename(name) {
		return Artifact{}, fmt.Errorf("invalid backup id")
	}
	return s.artifactFor(name)
}

func (s *Service) artifactFor(name string) (Artifact, error) {
	info, err := os.Stat(filepath.Join(s.dir, name))
	if err != nil {
		return Artifact{}, err
	}
	hash, err := fileSHA256(filepath.Join(s.dir, name))
	if err != nil {
		return Artifact{}, err
	}
	kind := AppKind
	if strings.HasSuffix(name, ".dump") {
		kind = FullKind
	}
	return Artifact{
		ID:          name,
		Kind:        kind,
		Filename:    name,
		SizeBytes:   info.Size(),
		SHA256:      hash,
		CreatedAt:   info.ModTime().UTC(),
		DownloadURL: "/api/backups/" + name + "/download",
	}, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func isBackupFilename(name string) bool {
	return strings.HasSuffix(name, ".dump") || strings.HasSuffix(name, ".json")
}

func normalizeSelection(selection []string) ([]string, error) {
	if len(selection) == 0 {
		return append([]string(nil), bundleOrder...), nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range selection {
		bundle := strings.TrimSpace(raw)
		if bundle == "" {
			continue
		}
		if !contains(bundleOrder, bundle) {
			return nil, fmt.Errorf("unknown bundle %q", bundle)
		}
		if !seen[bundle] {
			seen[bundle] = true
			out = append(out, bundle)
		}
	}
	sort.Slice(out, func(i, j int) bool { return bundleIndex(out[i]) < bundleIndex(out[j]) })
	return out, nil
}

func availableBundles(doc AppExport) []string {
	out := []string{}
	for _, bundle := range bundleOrder {
		if len(doc.Bundles[bundle]) > 0 {
			out = append(out, bundle)
		}
	}
	return out
}

func expandBundleDependencies(selected, available []string, warnings, errorsOut []string) ([]string, []string, []string) {
	effective := append([]string(nil), selected...)
	requires := map[string][]string{
		"tasks":             {"configuration", "projects"},
		"execution_history": {"configuration", "projects", "tasks"},
	}
	for _, bundle := range selected {
		for _, dep := range requires[bundle] {
			if contains(effective, dep) {
				continue
			}
			if contains(available, dep) {
				effective = append(effective, dep)
				warnings = append(warnings, fmt.Sprintf("Selected bundle %q requires %q; it will be included.", bundle, dep))
			} else {
				errorsOut = append(errorsOut, fmt.Sprintf("selected bundle %q requires missing bundle %q", bundle, dep))
			}
		}
	}
	sort.Slice(effective, func(i, j int) bool { return bundleIndex(effective[i]) < bundleIndex(effective[j]) })
	return effective, warnings, errorsOut
}

func bundleIndex(bundle string) int {
	for i, candidate := range bundleOrder {
		if candidate == bundle {
			return i
		}
	}
	return len(bundleOrder)
}

func contains(items []string, item string) bool {
	for _, candidate := range items {
		if candidate == item {
			return true
		}
	}
	return false
}

func normalizeDocumentForImport(doc AppExport, effective []string) AppExport {
	out := doc
	out.Bundles = map[string]map[string][]json.RawMessage{}
	for _, bundle := range effective {
		out.Bundles[bundle] = map[string][]json.RawMessage{}
		for table, rows := range doc.Bundles[bundle] {
			copied := make([]json.RawMessage, 0, len(rows))
			for _, row := range rows {
				copied = append(copied, normalizeRow(table, row))
			}
			out.Bundles[bundle][table] = copied
		}
	}
	return out
}

func normalizeRow(table string, row json.RawMessage) json.RawMessage {
	var obj map[string]any
	if err := json.Unmarshal(row, &obj); err != nil {
		return row
	}
	switch table {
	case "agent_wakeups":
		if obj["status"] == "queued" || obj["status"] == "claimed" || obj["status"] == "running" {
			obj["status"] = "cancelled"
			obj["finished_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			obj["error"] = "normalized during restore"
		}
	case "runs":
		if obj["status"] == "queued" || obj["status"] == "running" {
			obj["status"] = "error"
			obj["finished_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			if obj["exit_code"] == nil {
				obj["exit_code"] = 1
			}
		}
	case "tasks":
		obj["checkout_run_id"] = nil
		obj["execution_run_id"] = nil
		obj["execution_state"] = nil
	case "task_interactions":
		obj["source_run_id"] = nil
	}
	body, err := json.Marshal(obj)
	if err != nil {
		return row
	}
	return body
}

func (s *Service) diffTable(ctx context.Context, table Table, rows []json.RawMessage) (int, int, error) {
	if len(rows) == 0 {
		return 0, 0, nil
	}
	var update int
	filter := pkExistsFilter(table)
	query := fmt.Sprintf("SELECT count(*) FROM jsonb_populate_recordset(NULL::%s, $1::jsonb) incoming WHERE EXISTS (SELECT 1 FROM %s existing WHERE %s)", table.Name, table.Name, filter)
	body, err := json.Marshal(rows)
	if err != nil {
		return 0, 0, err
	}
	if err := s.db.GetContext(ctx, &update, query, string(body)); err != nil {
		return 0, 0, err
	}
	return len(rows) - update, update, nil
}

func upsertRows(ctx context.Context, tx *sqlx.Tx, table Table, rows []json.RawMessage) error {
	body, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	cols := strings.Join(table.Columns, ",")
	updates := []string{}
	for _, col := range table.Columns {
		if contains(table.PK, col) {
			continue
		}
		updates = append(updates, fmt.Sprintf("%s=EXCLUDED.%s", col, col))
	}
	conflict := strings.Join(table.PK, ",")
	action := "DO NOTHING"
	if len(updates) > 0 {
		action = "DO UPDATE SET " + strings.Join(updates, ",")
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM jsonb_populate_recordset(NULL::%s, $1::jsonb) ON CONFLICT (%s) %s", table.Name, cols, cols, table.Name, conflict, action)
	_, err = tx.ExecContext(ctx, query, string(body))
	return err
}

func restoreWakeupRunLinks(ctx context.Context, tx *sqlx.Tx, doc AppExport) error {
	rows := doc.Bundles["execution_history"]["agent_wakeups"]
	for _, row := range rows {
		var obj map[string]any
		if err := json.Unmarshal(row, &obj); err != nil {
			return err
		}
		id, _ := obj["id"].(string)
		runID, _ := obj["run_id"].(string)
		if id == "" || runID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agent_wakeups SET run_id=$2 WHERE id=$1 AND EXISTS (SELECT 1 FROM runs WHERE id=$2)`, id, runID); err != nil {
			return err
		}
	}
	return nil
}

func pkExistsFilter(table Table) string {
	parts := make([]string, 0, len(table.PK))
	for _, col := range table.PK {
		parts = append(parts, fmt.Sprintf("existing.%s = incoming.%s", col, col))
	}
	return strings.Join(parts, " AND ")
}
