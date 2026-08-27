package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/store/postgres"
	"github.com/jackc/pgx/v5"
)

// Server is the HTTP server for the Web UI
type Server struct {
	store  *postgres.Store
	server *http.Server
	mux    *http.ServeMux
}

// NewServer creates a new UI server
func NewServer(store *postgres.Store, listen string) *Server {
	s := &Server{
		store: store,
		mux:   http.NewServeMux(),
	}

	// Register routes
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/experiments/{id}", s.handleExperimentDetail)

	s.server = &http.Server{
		Addr:    listen,
		Handler: s.mux,
	}

	return s
}

// ListenAndServe starts the HTTP server
func (s *Server) ListenAndServe() error {
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// handleIndex shows the experiment list page
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.render404(w)
		return
	}

	ctx := r.Context()

	// Query all experiments using the pool's Query method
	rows, err := s.store.Pool().Query(ctx, `
		SELECT experiment_id, status, created_at, total_logical_trials, terminal_count
		FROM experiments
		ORDER BY created_at DESC
	`)
	if err != nil {
		s.render500(w, fmt.Sprintf("Database query failed: %v", err))
		return
	}
	defer rows.Close()

	type ExperimentRow struct {
		ID            string
		Status        string
		CreatedAt     time.Time
		TotalTrials   int
		TerminalCount int
	}

	var experiments []ExperimentRow
	for rows.Next() {
		var exp ExperimentRow
		if err := rows.Scan(&exp.ID, &exp.Status, &exp.CreatedAt, &exp.TotalTrials, &exp.TerminalCount); err != nil {
			s.render500(w, fmt.Sprintf("Failed to scan row: %v", err))
			return
		}
		experiments = append(experiments, exp)
	}

	if err := rows.Err(); err != nil {
		s.render500(w, fmt.Sprintf("Row iteration error: %v", err))
		return
	}

	// Render template
	tmpl := template.Must(template.New("index").Parse(indexTemplate))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, experiments); err != nil {
		s.render500(w, fmt.Sprintf("Template execution failed: %v", err))
	}
}

// handleExperimentDetail shows experiment detail page
func (s *Server) handleExperimentDetail(w http.ResponseWriter, r *http.Request) {
	experimentID := r.PathValue("id")
	if experimentID == "" {
		s.render404(w)
		return
	}

	ctx := r.Context()

	// Get experiment with full stats
	var expData struct {
		ExperimentID   string
		Status         string
		CreatedAt      time.Time
		TotalTrials    int
		TerminalCount  int
		SucceededCount int
		FailedCount    int
		TimedOutCount  int
		CancelledCount int
	}

	err := s.store.Pool().QueryRow(ctx, `
		SELECT experiment_id, status, created_at, total_logical_trials, terminal_count,
		       succeeded_count, failed_count, timed_out_count, cancelled_count
		FROM experiments
		WHERE experiment_id = $1
	`, experimentID).Scan(
		&expData.ExperimentID,
		&expData.Status,
		&expData.CreatedAt,
		&expData.TotalTrials,
		&expData.TerminalCount,
		&expData.SucceededCount,
		&expData.FailedCount,
		&expData.TimedOutCount,
		&expData.CancelledCount,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.render404(w)
		} else {
			s.render500(w, fmt.Sprintf("Failed to query experiment: %v", err))
		}
		return
	}

	// Get experiment report
	var reportData struct {
		ReportID                 string
		FilePath                 string
		ValidPairs               int
		InvalidPairs             int
		MeanLift                 *float64
		CILower                  *float64
		CIUpper                  *float64
		StatisticallySignificant *bool
	}
	var hasReport bool

	err = s.store.Pool().QueryRow(ctx, `
		SELECT report_id, file_path, valid_pairs, invalid_pairs,
		       mean_lift, ci_lower, ci_upper, statistically_significant
		FROM experiment_reports
		WHERE experiment_id = $1
	`, experimentID).Scan(
		&reportData.ReportID,
		&reportData.FilePath,
		&reportData.ValidPairs,
		&reportData.InvalidPairs,
		&reportData.MeanLift,
		&reportData.CILower,
		&reportData.CIUpper,
		&reportData.StatisticallySignificant,
	)
	if err == nil {
		hasReport = true
	} else if err != pgx.ErrNoRows {
		s.render500(w, fmt.Sprintf("Failed to query report: %v", err))
		return
	}

	// Read report.json for case results
	var caseResults []map[string]interface{}
	if hasReport && reportData.FilePath != "" {
		reportBytes, err := os.ReadFile(reportData.FilePath)
		if err == nil {
			var report struct {
				CaseResults []map[string]interface{} `json:"case_results"`
			}
			if json.Unmarshal(reportBytes, &report) == nil {
				caseResults = report.CaseResults
			}
		}
	}

	// Get release decision
	var decisionData struct {
		Result        string
		PolicyID      string
		PolicyVersion string
		Explanation   string
		EvidenceLinks []string
		CreatedAt     time.Time
	}
	var hasDecision bool

	var evidenceLinksJSON []byte
	err = s.store.Pool().QueryRow(ctx, `
		SELECT result, policy_id, policy_version, explanation,
		       COALESCE(evidence_links, '[]'::jsonb),
		       created_at
		FROM release_decisions
		WHERE experiment_id = $1
	`, experimentID).Scan(
		&decisionData.Result,
		&decisionData.PolicyID,
		&decisionData.PolicyVersion,
		&decisionData.Explanation,
		&evidenceLinksJSON,
		&decisionData.CreatedAt,
	)
	if err == nil {
		hasDecision = true
		json.Unmarshal(evidenceLinksJSON, &decisionData.EvidenceLinks)
	} else if err != pgx.ErrNoRows {
		s.render500(w, fmt.Sprintf("Failed to query decision: %v", err))
		return
	}

	// Render template
	data := map[string]interface{}{
		"Experiment":  expData,
		"Report":      reportData,
		"HasReport":   hasReport,
		"CaseResults": caseResults,
		"Decision":    decisionData,
		"HasDecision": hasDecision,
	}

	// Add helper functions to template
	funcMap := template.FuncMap{
		"formatFloat": func(f *float64) string {
			if f == nil {
				return "N/A"
			}
			return fmt.Sprintf("%.2f", *f)
		},
		"formatPercent": func(f *float64) string {
			if f == nil {
				return "N/A"
			}
			return fmt.Sprintf("%.2f%%", *f*100)
		},
	}

	tmpl := template.Must(template.New("detail").Funcs(funcMap).Parse(detailTemplate))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		s.render500(w, fmt.Sprintf("Template execution failed: %v", err))
	}
}

func (s *Server) render404(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, notFoundTemplate)
}

func (s *Server) render500(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl := template.Must(template.New("error").Parse(errorTemplate))
	tmpl.Execute(w, map[string]string{"Message": message})
}
