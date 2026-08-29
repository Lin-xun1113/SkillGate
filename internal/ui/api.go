package ui

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The API types deliberately expose identities, lifecycle state and evidence
// metadata, while omitting lease tokens and raw prompt/model data.
type apiExperiment struct {
	ExperimentID      string       `json:"experiment_id"`
	ManifestHash      string       `json:"manifest_hash"`
	PlanHash          string       `json:"plan_hash"`
	Status            string       `json:"status"`
	TotalTrials       int          `json:"total_trials"`
	TerminalCount     int          `json:"terminal_count"`
	SucceededCount    int          `json:"succeeded_count"`
	FailedCount       int          `json:"failed_count"`
	TimedOutCount     int          `json:"timed_out_count"`
	CancelledCount    int          `json:"cancelled_count"`
	BudgetDeadlineAt  time.Time    `json:"budget_deadline_at"`
	CancelRequestedAt *time.Time   `json:"cancel_requested_at,omitempty"`
	CancelReason      *string      `json:"cancel_reason,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
	Report            *apiReport   `json:"report,omitempty"`
	Decision          *apiDecision `json:"decision,omitempty"`
}

type apiReport struct {
	ReportID                 string    `json:"report_id"`
	ReportType               string    `json:"report_type"`
	FileHash                 string    `json:"file_hash"`
	ValidPairs               int       `json:"valid_pairs"`
	InvalidPairs             int       `json:"invalid_pairs"`
	MeanLift                 *float64  `json:"mean_lift,omitempty"`
	CILower                  *float64  `json:"ci_lower,omitempty"`
	CIUpper                  *float64  `json:"ci_upper,omitempty"`
	StatisticallySignificant *bool     `json:"statistically_significant,omitempty"`
	CreatedAt                time.Time `json:"created_at"`
}

type apiDecision struct {
	DecisionID    string          `json:"decision_id"`
	Result        string          `json:"result"`
	PolicyID      string          `json:"policy_id"`
	PolicyVersion string          `json:"policy_version"`
	PolicyHash    string          `json:"policy_hash"`
	SnapshotHash  string          `json:"snapshot_hash"`
	Explanation   string          `json:"explanation"`
	Actor         string          `json:"actor"`
	Trace         json.RawMessage `json:"trace"`
	EvidenceLinks json.RawMessage `json:"evidence_links"`
	CreatedAt     time.Time       `json:"created_at"`
}

type apiTrialSummary struct {
	LogicalTrialID string    `json:"logical_trial_id"`
	ExperimentID   string    `json:"experiment_id"`
	PairID         string    `json:"pair_id"`
	CaseID         string    `json:"case_id,omitempty"`
	Arm            string    `json:"arm"`
	Status         string    `json:"status"`
	CurrentAttempt int       `json:"current_attempt"`
	Repetition     int       `json:"repetition_index"`
	FinalResultID  *string   `json:"final_result_id,omitempty"`
	Outcome        *string   `json:"outcome,omitempty"`
	ResultHash     *string   `json:"result_manifest_hash,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type apiCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func writeAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	requestID := r.Header.Get("X-Request-ID")
	writeAPIJSON(w, status, map[string]any{
		"error": map[string]any{
			"code": code, "message": message, "request_id": requestID,
		},
	})
}

func apiGETOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	writeAPIError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "该 API 端点只读")
	return false
}

func (s *Server) handleAPIExperiment(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "experiment_id 不能为空")
		return
	}
	var exp apiExperiment
	var cancelAt *time.Time
	var cancelReason *string
	err := s.store.Pool().QueryRow(r.Context(), `
		SELECT experiment_id, manifest_hash, plan_hash, status, total_logical_trials,
		       terminal_count, succeeded_count, failed_count, timed_out_count,
		       cancelled_count, budget_deadline_at, cancel_requested_at,
		       cancel_reason, created_at, updated_at
		FROM experiments WHERE experiment_id=$1`, id).Scan(
		&exp.ExperimentID, &exp.ManifestHash, &exp.PlanHash, &exp.Status,
		&exp.TotalTrials, &exp.TerminalCount, &exp.SucceededCount, &exp.FailedCount,
		&exp.TimedOutCount, &exp.CancelledCount, &exp.BudgetDeadlineAt,
		&cancelAt, &cancelReason, &exp.CreatedAt, &exp.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "experiment 不存在")
		} else {
			writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 experiment 失败")
		}
		return
	}
	exp.CancelRequestedAt, exp.CancelReason = cancelAt, cancelReason
	exp.Report = s.apiReport(r, id)
	exp.Decision = s.apiDecision(r, id)
	writeAPIJSON(w, http.StatusOK, exp)
}

func (s *Server) apiReport(r *http.Request, experimentID string) *apiReport {
	var report apiReport
	err := s.store.Pool().QueryRow(r.Context(), `
		SELECT report_id, report_type, file_hash, valid_pairs, invalid_pairs,
		       mean_lift, ci_lower, ci_upper, statistically_significant, created_at
		FROM experiment_reports WHERE experiment_id=$1`, experimentID).Scan(
		&report.ReportID, &report.ReportType, &report.FileHash, &report.ValidPairs,
		&report.InvalidPairs, &report.MeanLift, &report.CILower, &report.CIUpper,
		&report.StatisticallySignificant, &report.CreatedAt,
	)
	if err != nil {
		return nil
	}
	return &report
}

func (s *Server) apiDecision(r *http.Request, experimentID string) *apiDecision {
	var decision apiDecision
	var trace, links []byte
	err := s.store.Pool().QueryRow(r.Context(), `
		SELECT decision_id, result, policy_id, policy_version, policy_hash,
		       snapshot_hash, explanation, actor, trace,
		       COALESCE(evidence_links, '[]'::jsonb), created_at
		FROM release_decisions WHERE experiment_id=$1`, experimentID).Scan(
		&decision.DecisionID, &decision.Result, &decision.PolicyID,
		&decision.PolicyVersion, &decision.PolicyHash, &decision.SnapshotHash,
		&decision.Explanation, &decision.Actor, &trace, &links, &decision.CreatedAt,
	)
	if err != nil {
		return nil
	}
	decision.Trace = rawJSONOrEmpty(trace, "{}")
	decision.EvidenceLinks = rawJSONOrEmpty(links, "[]")
	return &decision
}

func rawJSONOrEmpty(raw []byte, fallback string) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(raw)
}

func (s *Server) handleAPIExperimentTrials(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	id := r.PathValue("id")
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 200 {
			writeAPIError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "limit 必须位于 1..200")
			return
		}
		limit = parsed
	}
	cur, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "cursor 无效")
		return
	}
	query := `
		SELECT l.logical_trial_id, l.experiment_id, l.pair_id, COALESCE(l.case_id,''),
		       l.arm, l.status, l.current_attempt, l.repetition_index,
		       l.final_result_id, r.outcome, r.manifest_hash, l.created_at, l.updated_at
		FROM logical_trials l
		LEFT JOIN trial_results r ON r.logical_trial_id=l.logical_trial_id
		WHERE l.experiment_id=$1`
	args := []any{id}
	if cur != nil {
		query += ` AND (l.created_at, l.logical_trial_id) < ($2, $3)`
		args = append(args, cur.CreatedAt, cur.ID)
	}
	query += fmt.Sprintf(" ORDER BY l.created_at DESC, l.logical_trial_id DESC LIMIT %d", limit+1)
	rows, err := s.store.Pool().Query(r.Context(), query, args...)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 trials 失败")
		return
	}
	defer rows.Close()
	items := make([]apiTrialSummary, 0, limit)
	for rows.Next() {
		var item apiTrialSummary
		if err := rows.Scan(&item.LogicalTrialID, &item.ExperimentID, &item.PairID,
			&item.CaseID, &item.Arm, &item.Status, &item.CurrentAttempt, &item.Repetition,
			&item.FinalResultID, &item.Outcome, &item.ResultHash, &item.CreatedAt, &item.UpdatedAt); err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 trial 行失败")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 trials 失败")
		return
	}
	var next string
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		next = encodeCursor(apiCursor{CreatedAt: last.CreatedAt, ID: last.LogicalTrialID})
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func encodeCursor(cursor apiCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(value string) (*apiCursor, error) {
	if value == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	var cursor apiCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.ID == "" || cursor.CreatedAt.IsZero() {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &cursor, nil
}

func (s *Server) handleAPITrial(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	id := r.PathValue("id")
	var trial struct {
		TrialID       string          `json:"trial_id"`
		LogicalID     string          `json:"logical_trial_id"`
		ExperimentID  string          `json:"experiment_id"`
		PairID        string          `json:"pair_id"`
		CaseID        string          `json:"case_id,omitempty"`
		Arm           string          `json:"arm"`
		Attempt       int             `json:"attempt_no"`
		AttemptStatus string          `json:"attempt_status"`
		TrialStatus   string          `json:"trial_status"`
		Owner         *string         `json:"lease_owner,omitempty"`
		Generation    *int64          `json:"lease_generation,omitempty"`
		LeaseExpires  *time.Time      `json:"lease_expires_at,omitempty"`
		Deadline      *time.Time      `json:"deadline_at,omitempty"`
		RequestHash   *string         `json:"request_hash,omitempty"`
		Outcome       *string         `json:"outcome,omitempty"`
		ResultHash    *string         `json:"result_manifest_hash,omitempty"`
		Manifest      json.RawMessage `json:"outcome_manifest,omitempty"`
		Artifacts     json.RawMessage `json:"artifacts"`
		Usage         json.RawMessage `json:"usage"`
		Grades        json.RawMessage `json:"grades"`
		CreatedAt     time.Time       `json:"created_at"`
		UpdatedAt     time.Time       `json:"updated_at"`
	}
	var manifest, artifacts, usage, grades []byte
	err := s.store.Pool().QueryRow(r.Context(), `
		SELECT a.trial_id, a.logical_trial_id, l.experiment_id, l.pair_id,
		       COALESCE(l.case_id,''), l.arm, a.attempt_no, a.status, l.status,
		       NULLIF(a.lease_owner,''), NULLIF(a.lease_generation,0),
		       a.lease_expires_at, a.deadline_at, NULLIF(a.request_hash,''),
		       r.outcome, r.manifest_hash, a.outcome_manifest,
		       COALESCE(r.artifacts,'[]'::jsonb), COALESCE(r.usage,'{}'::jsonb),
		       COALESCE(r.grades,'{}'::jsonb), a.created_at, a.updated_at
		FROM trial_attempts a
		JOIN logical_trials l ON l.logical_trial_id=a.logical_trial_id
		LEFT JOIN trial_results r ON r.trial_id=a.trial_id
		WHERE a.trial_id=$1`, id).Scan(
		&trial.TrialID, &trial.LogicalID, &trial.ExperimentID, &trial.PairID,
		&trial.CaseID, &trial.Arm, &trial.Attempt, &trial.AttemptStatus,
		&trial.TrialStatus, &trial.Owner, &trial.Generation, &trial.LeaseExpires,
		&trial.Deadline, &trial.RequestHash, &trial.Outcome, &trial.ResultHash,
		&manifest, &artifacts, &usage, &grades, &trial.CreatedAt, &trial.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "trial 不存在")
		} else {
			writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 trial 失败")
		}
		return
	}
	trial.Manifest = rawJSONOrEmpty(manifest, "null")
	trial.Artifacts = sanitizeArtifacts(artifacts)
	trial.Usage = rawJSONOrEmpty(usage, "{}")
	trial.Grades = rawJSONOrEmpty(grades, "{}")
	writeAPIJSON(w, http.StatusOK, trial)
}

func (s *Server) handleAPITrialEvents(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	id := r.PathValue("id")
	rows, err := s.store.Pool().Query(r.Context(), `
		SELECT event_id, logical_trial_id, experiment_id, attempt_no, worker_id,
		       sequence, event_type, occurred_at, payload, payload_hash
		FROM trial_events WHERE trial_id=$1 ORDER BY sequence ASC`, id)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 trial events 失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var eventID, logicalID, experimentID, workerID, eventType, payloadHash string
		var attempt int
		var sequence int64
		var occurred time.Time
		var payload []byte
		if err := rows.Scan(&eventID, &logicalID, &experimentID, &attempt, &workerID,
			&sequence, &eventType, &occurred, &payload, &payloadHash); err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 event 行失败")
			return
		}
		items = append(items, map[string]any{
			"event_id": eventID, "logical_trial_id": logicalID, "experiment_id": experimentID,
			"attempt_no": attempt, "worker_id": workerID, "sequence": sequence,
			"event_type": eventType, "occurred_at": occurred, "payload": rawJSONOrEmpty(payload, "{}"),
			"payload_hash": payloadHash,
		})
	}
	if err := rows.Err(); err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 trial events 失败")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAPITrialArtifacts(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	id := r.PathValue("id")
	var raw []byte
	err := s.store.Pool().QueryRow(r.Context(), `
		SELECT COALESCE(r.artifacts,'[]'::jsonb)
		FROM trial_results r WHERE r.trial_id=$1`, id).Scan(&raw)
	if err == pgx.ErrNoRows {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "trial artifact 不存在")
		return
	}
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 artifacts 失败")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": sanitizeArtifacts(raw)})
}

func sanitizeArtifacts(raw []byte) json.RawMessage {
	var values []map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return json.RawMessage("[]")
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item := map[string]any{}
		for _, key := range []string{"artifact_id", "kind", "media_type", "sha256", "size_bytes", "storage_key", "created_at_unix_ms"} {
			if v, ok := value[key]; ok {
				item[key] = v
			}
		}
		items = append(items, item)
	}
	encoded, _ := json.Marshal(items)
	return json.RawMessage(encoded)
}

type apiArtifact struct {
	ArtifactID string
	Kind       string
	MediaType  string
	SHA256     string
	SizeBytes  int64
	StorageKey string
	LocalPath  string
	TrialID    string
	Experiment string
}

func (s *Server) findAPIArtifact(r *http.Request, artifactID string) (apiArtifact, error) {
	var raw []byte
	var artifact apiArtifact
	err := s.store.Pool().QueryRow(r.Context(), `
		SELECT r.trial_id, l.experiment_id, elem
		FROM trial_results r
		JOIN logical_trials l ON l.logical_trial_id=r.logical_trial_id
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(r.artifacts,'[]'::jsonb)) elem
		WHERE COALESCE(elem->>'artifact_id', elem->>'artifactId')=$1
		LIMIT 1`, artifactID).Scan(&artifact.TrialID, &artifact.Experiment, &raw)
	if err != nil {
		return apiArtifact{}, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return apiArtifact{}, fmt.Errorf("invalid artifact metadata")
	}
	artifact.ArtifactID = stringValue(value, "artifact_id", "artifactId")
	artifact.Kind = stringValue(value, "kind")
	artifact.MediaType = stringValue(value, "media_type", "mediaType")
	artifact.SHA256 = stringValue(value, "sha256")
	artifact.StorageKey = stringValue(value, "storage_key", "storageKey")
	artifact.LocalPath = stringValue(value, "local_path", "localPath")
	artifact.SizeBytes = int64Value(value, "size_bytes", "sizeBytes")
	if artifact.ArtifactID == "" || artifact.LocalPath == "" {
		return apiArtifact{}, fmt.Errorf("artifact path missing")
	}
	return artifact, nil
}

func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if result, ok := value[key].(string); ok {
			return result
		}
	}
	return ""
}

func int64Value(value map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch result := value[key].(type) {
		case float64:
			return int64(result)
		case json.Number:
			parsed, _ := result.Int64()
			return parsed
		}
	}
	return 0
}

func (s *Server) handleAPIArtifactDownloadURL(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	artifact, err := s.findAPIArtifact(r, r.PathValue("id"))
	if err != nil {
		if err == pgx.ErrNoRows {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "artifact 不存在")
		} else {
			writeAPIError(w, r, http.StatusNotFound, "ARTIFACT_UNAVAILABLE", "artifact 不可下载")
		}
		return
	}
	path, err := s.secureArtifactPath(artifact.LocalPath)
	if err != nil {
		writeAPIError(w, r, http.StatusNotFound, "ARTIFACT_UNAVAILABLE", "artifact 路径不在受信目录内")
		return
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() {
		writeAPIError(w, r, http.StatusNotFound, "ARTIFACT_UNAVAILABLE", "artifact 文件不存在")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{
		"artifact_id": artifact.ArtifactID, "sha256": artifact.SHA256,
		"size_bytes": stat.Size(), "media_type": artifact.MediaType,
		"download_url": "/api/v1/artifacts/" + artifact.ArtifactID + "/content",
	})
}

func (s *Server) handleAPIArtifactContent(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	artifact, err := s.findAPIArtifact(r, r.PathValue("id"))
	if err != nil {
		if err == pgx.ErrNoRows {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "artifact 不存在")
		} else {
			writeAPIError(w, r, http.StatusNotFound, "ARTIFACT_UNAVAILABLE", "artifact 不可下载")
		}
		return
	}
	path, err := s.secureArtifactPath(artifact.LocalPath)
	if err != nil {
		writeAPIError(w, r, http.StatusNotFound, "ARTIFACT_UNAVAILABLE", "artifact 路径不在受信目录内")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeAPIError(w, r, http.StatusNotFound, "ARTIFACT_UNAVAILABLE", "artifact 文件不存在")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		writeAPIError(w, r, http.StatusNotFound, "ARTIFACT_UNAVAILABLE", "artifact 文件不存在")
		return
	}
	if artifact.SHA256 != "" {
		// Verify the content identity before serving a file from a worker volume.
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "ARTIFACT_UNAVAILABLE", "读取 artifact 失败")
			return
		}
		actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(actual, artifact.SHA256) {
			writeAPIError(w, r, http.StatusConflict, "ARTIFACT_HASH_MISMATCH", "artifact 内容 Hash 不匹配")
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			writeAPIError(w, r, http.StatusInternalServerError, "ARTIFACT_UNAVAILABLE", "读取 artifact 失败")
			return
		}
	}
	if artifact.MediaType != "" {
		w.Header().Set("Content-Type", artifact.MediaType)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
}

func (s *Server) secureArtifactPath(raw string) (string, error) {
	if s.artifactsRoot == "" || raw == "" {
		return "", fmt.Errorf("artifact root not configured")
	}
	root, err := filepath.Abs(s.artifactsRoot)
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	target := raw
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolvedTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes artifact root")
	}
	return resolvedTarget, nil
}

func (s *Server) handleAPIDecision(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	decision := s.apiDecision(r, r.PathValue("id"))
	if decision == nil {
		writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "release decision 不存在")
		return
	}
	writeAPIJSON(w, http.StatusOK, decision)
}

func (s *Server) handleAPIMetrics(w http.ResponseWriter, r *http.Request) {
	if !apiGETOnly(w, r) {
		return
	}
	var hash string
	var payload []byte
	err := s.store.Pool().QueryRow(r.Context(), `
		SELECT snapshot_hash, payload FROM metrics_snapshots WHERE experiment_id=$1`, r.PathValue("id")).Scan(&hash, &payload)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "metrics snapshot 不存在")
		} else {
			writeAPIError(w, r, http.StatusInternalServerError, "DATABASE_UNAVAILABLE", "读取 metrics 失败")
		}
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"snapshot_hash": hash, "payload": rawJSONOrEmpty(payload, "{}")})
}
