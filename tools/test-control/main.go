package main

import (
	"context"
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

type Profile struct {
	Name      string
	Users     int
	SpawnRate int
	Duration  string
	MaxTTL    time.Duration
}

var profiles = map[string]Profile{
	"smoke":    {Name: "smoke", Users: 10, SpawnRate: 2, Duration: "1m", MaxTTL: 10 * time.Minute},
	"baseline": {Name: "baseline", Users: 50, SpawnRate: 5, Duration: "5m", MaxTTL: 15 * time.Minute},
	"stress":   {Name: "stress", Users: 150, SpawnRate: 15, Duration: "8m", MaxTTL: 20 * time.Minute},
}

type RunManifest struct {
	RunID           string     `json:"run_id"`
	Profile         string     `json:"profile"`
	TargetHost      string     `json:"target_host"`
	Users           int        `json:"users"`
	SpawnRate       int        `json:"spawn_rate"`
	Duration        string     `json:"duration"`
	Status          string     `json:"status"`
	DesiredState    string     `json:"desired_state"`
	VMName          string     `json:"vm_name"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	StartedAt       string     `json:"started_at,omitempty"`
	FinishedAt      string     `json:"finished_at,omitempty"`
	StopRequestedAt *time.Time `json:"stop_requested_at,omitempty"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	VMDeleted       bool       `json:"vm_deleted,omitempty"`
}

type Lease struct {
	RunID     string    `json:"run_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type EndpointStat struct {
	Method      string  `json:"method"`
	Name        string  `json:"name"`
	Requests    int64   `json:"requests"`
	Failures    int64   `json:"failures"`
	FailureRate float64 `json:"failure_rate"`
	MedianMs    float64 `json:"median_ms"`
	AvgMs       float64 `json:"avg_ms"`
	MinMs       float64 `json:"min_ms"`
	MaxMs       float64 `json:"max_ms"`
	P95Ms       float64 `json:"p95_ms"`
	P99Ms       float64 `json:"p99_ms"`
	RPS         float64 `json:"rps"`
}

type FailureStat struct {
	Method      string `json:"method"`
	Name        string `json:"name"`
	Error       string `json:"error"`
	Occurrences int64  `json:"occurrences"`
}

type RunReport struct {
	Manifest    RunManifest    `json:"manifest"`
	HasStats    bool           `json:"has_stats"`
	HasFailures bool           `json:"has_failures"`
	HasLog      bool           `json:"has_log"`
	Summary     *EndpointStat  `json:"summary,omitempty"`
	Endpoints   []EndpointStat `json:"endpoints"`
	Failures    []FailureStat  `json:"failures"`
	LogTail     string         `json:"log_tail,omitempty"`
}

type Server struct {
	projectID        string
	zone             string
	resultsBucket    string
	instanceTemplate string
	targetHost       string
	password         string
	gcs              *storage.Client
	instances        *compute.InstancesClient
}

func main() {
	ctx := context.Background()
	gcs, err := storage.NewClient(ctx)
	if err != nil {
		slog.Error("storage client init failed", "error", err)
		os.Exit(1)
	}
	defer gcs.Close()

	instances, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		slog.Error("compute client init failed", "error", err)
		os.Exit(1)
	}
	defer instances.Close()

	srv := &Server{
		projectID:        mustEnv("GCP_PROJECT_ID"),
		zone:             envOr("GCP_ZONE", "europe-north2-a"),
		resultsBucket:    mustEnv("RESULTS_BUCKET"),
		instanceTemplate: mustEnv("LOCUST_INSTANCE_TEMPLATE"),
		targetHost:       envOr("TARGET_HOST", "https://app.socialeventmapper.com"),
		password:         mustEnv("CONTROLLER_PASSWORD"),
		gcs:              gcs,
		instances:        instances,
	}

	go srv.reconcileLoop(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /", srv.withAuth(srv.handleUI))
	mux.HandleFunc("GET /api/runs", srv.withAuth(srv.handleListRuns))
	mux.HandleFunc("POST /api/runs", srv.withAuth(srv.handleStartRun))
	mux.HandleFunc("POST /api/runs/{id}/stop", srv.withAuth(srv.handleStopRun))
	mux.HandleFunc("GET /api/runs/{id}/report", srv.withAuth(srv.handleRunReport))
	mux.HandleFunc("GET /api/runs/{id}/results/{file}", srv.withAuth(srv.handleDownloadResult))

	port := envOr("PORT", "8080")
	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("test controller listening", "addr", httpSrv.Addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "operator" || subtle.ConstantTimeCompare([]byte(pass), []byte(s.password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="SEM Test Controller"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	runs, _ := s.listRuns(r.Context())
	runsJSON, _ := json.Marshal(runs)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = uiTemplate.Execute(w, map[string]any{
		"TargetHost":  s.targetHost,
		"InitialRuns": template.JS(runsJSON),
	})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.listRuns(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []RunManifest{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	profileName := r.FormValue("profile")
	if profileName == "" {
		var body struct {
			Profile string `json:"profile"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		profileName = body.Profile
	}
	prof, ok := profiles[profileName]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid profile")
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()
	runID := fmt.Sprintf("run-%s-%s", now.Format("20060102-150405"), prof.Name)
	vmName := "sem-locust-" + runID

	if err := s.acquireLease(ctx, Lease{RunID: runID, ExpiresAt: now.Add(prof.MaxTTL)}); err != nil {
		writeJSONError(w, http.StatusConflict, "an experiment is already active: "+err.Error())
		return
	}

	manifest := RunManifest{
		RunID:        runID,
		Profile:      prof.Name,
		TargetHost:   s.targetHost,
		Users:        prof.Users,
		SpawnRate:    prof.SpawnRate,
		Duration:     prof.Duration,
		Status:       "STARTING",
		DesiredState: "RUNNING",
		VMName:       vmName,
		CreatedAt:    now,
		ExpiresAt:    now.Add(prof.MaxTTL),
	}
	if err := s.saveManifest(ctx, manifest); err != nil {
		s.releaseLease(ctx)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.createRunnerVM(ctx, manifest); err != nil {
		manifest.Status = "FAILED"
		manifest.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = s.saveManifest(ctx, manifest)
		s.releaseLease(ctx)
		writeJSONError(w, http.StatusInternalServerError, "create runner VM: "+err.Error())
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "text/html") && !strings.Contains(r.Header.Get("Accept"), "application/json") {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusCreated, manifest)
}

func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	manifest, err := s.loadManifest(r.Context(), runID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}
	now := time.Now().UTC()
	manifest.DesiredState = "STOPPING"
	if manifest.Status == "STARTING" || manifest.Status == "RUNNING" {
		manifest.Status = "STOPPING"
	}
	if manifest.StopRequestedAt == nil {
		manifest.StopRequestedAt = &now
	}
	if err := s.saveManifest(r.Context(), manifest); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "text/html") && !strings.Contains(r.Header.Get("Accept"), "application/json") {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (s *Server) handleRunReport(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	ctx := r.Context()

	manifest, err := s.loadManifest(ctx, runID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	report := RunReport{
		Manifest:  manifest,
		Endpoints: []EndpointStat{},
		Failures:  []FailureStat{},
	}

	if statsData, err := s.readObjectBytes(ctx, fmt.Sprintf("runs/%s/locust_stats.csv", runID), 2*1024*1024); err == nil {
		summary, endpoints := parseLocustStatsCSV(string(statsData))
		report.HasStats = true
		report.Summary = summary
		report.Endpoints = endpoints
	}

	if failData, err := s.readObjectBytes(ctx, fmt.Sprintf("runs/%s/locust_failures.csv", runID), 2*1024*1024); err == nil {
		report.HasFailures = true
		report.Failures = parseLocustFailuresCSV(string(failData))
	}

	if logData, err := s.readObjectBytes(ctx, fmt.Sprintf("runs/%s/runner.log", runID), 512*1024); err == nil {
		report.HasLog = true
		report.LogTail = tailLines(string(logData), 150)
	}

	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleDownloadResult(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	file := r.PathValue("file")
	switch file {
	case "locust_stats.csv", "locust_stats_history.csv", "locust_failures.csv", "runner.log", "manifest.json":
	default:
		http.Error(w, "invalid file", http.StatusBadRequest)
		return
	}

	rc, err := s.gcs.Bucket(s.resultsBucket).Object(fmt.Sprintf("runs/%s/%s", runID, file)).NewReader(r.Context())
	if err != nil {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s"`, runID, file))
	_, _ = io.Copy(w, rc)
}

func (s *Server) createRunnerVM(ctx context.Context, m RunManifest) error {
	// Note: Setting InstanceResource.Metadata replaces the instance template's metadata map,
	// so startup-script-url MUST be included explicitly here.
	items := []*computepb.Items{
		{Key: strPtr("startup-script-url"), Value: strPtr(fmt.Sprintf("gs://%s/scripts/runner-entrypoint.sh", s.resultsBucket))},
		{Key: strPtr("run-id"), Value: strPtr(m.RunID)},
		{Key: strPtr("target-host"), Value: strPtr(m.TargetHost)},
		{Key: strPtr("users"), Value: strPtr(fmt.Sprintf("%d", m.Users))},
		{Key: strPtr("spawn-rate"), Value: strPtr(fmt.Sprintf("%d", m.SpawnRate))},
		{Key: strPtr("duration"), Value: strPtr(m.Duration)},
		{Key: strPtr("results-bucket"), Value: strPtr(s.resultsBucket)},
		{Key: strPtr("locustfile-gcs"), Value: strPtr(fmt.Sprintf("gs://%s/scripts/locustfile.py", s.resultsBucket))},
	}
	op, err := s.instances.Insert(ctx, &computepb.InsertInstanceRequest{
		Project:                s.projectID,
		Zone:                   s.zone,
		SourceInstanceTemplate: &s.instanceTemplate,
		InstanceResource: &computepb.Instance{
			Name: strPtr(m.VMName),
			Labels: map[string]string{
				"role":   "locust-runner",
				"run_id": m.RunID,
			},
			Metadata: &computepb.Metadata{
				Items: items,
			},
		},
	})
	if err != nil {
		return err
	}
	return op.Wait(ctx)
}

func (s *Server) deleteRunnerVM(ctx context.Context, vmName string) error {
	op, err := s.instances.Delete(ctx, &computepb.DeleteInstanceRequest{
		Project:  s.projectID,
		Zone:     s.zone,
		Instance: vmName,
	})
	if err != nil {
		if strings.Contains(err.Error(), "notFound") || strings.Contains(err.Error(), "404") {
			return nil
		}
		return err
	}
	return op.Wait(ctx)
}

func (s *Server) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.reconcile(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) reconcile(ctx context.Context) {
	runs, err := s.listRuns(ctx)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, m := range runs {
		if m.VMDeleted {
			continue
		}
		terminal := m.Status == "SUCCEEDED" || m.Status == "FAILED" || m.Status == "STOPPED"
		expired := now.After(m.ExpiresAt)
		stoppingTimedOut := m.Status == "STOPPING" &&
			((m.StopRequestedAt != nil && now.Sub(*m.StopRequestedAt) > 45*time.Second) ||
				(m.StopRequestedAt == nil && now.Sub(m.CreatedAt) > 2*time.Minute))

		if terminal || expired || stoppingTimedOut {
			if err := s.deleteRunnerVM(ctx, m.VMName); err != nil {
				slog.Error("delete runner vm failed", "vm", m.VMName, "error", err)
				continue
			}
			m.VMDeleted = true
			if stoppingTimedOut && !terminal {
				m.Status = "STOPPED"
				if m.FinishedAt == "" {
					m.FinishedAt = now.Format(time.RFC3339)
				}
			} else if expired && !terminal {
				m.Status = "FAILED"
				if m.FinishedAt == "" {
					m.FinishedAt = now.Format(time.RFC3339)
				}
			}
			_ = s.saveManifest(ctx, m)
			s.releaseLease(ctx)
		}
	}
}

func (s *Server) acquireLease(ctx context.Context, lease Lease) error {
	data, _ := json.Marshal(lease)
	obj := s.gcs.Bucket(s.resultsBucket).Object("runs/lease.json")
	w := obj.If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (s *Server) releaseLease(ctx context.Context) {
	_ = s.gcs.Bucket(s.resultsBucket).Object("runs/lease.json").Delete(ctx)
}

func (s *Server) saveManifest(ctx context.Context, m RunManifest) error {
	data, _ := json.MarshalIndent(m, "", "  ")
	w := s.gcs.Bucket(s.resultsBucket).Object(fmt.Sprintf("runs/%s/manifest.json", m.RunID)).NewWriter(ctx)
	w.ContentType = "application/json"
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (s *Server) loadManifest(ctx context.Context, runID string) (RunManifest, error) {
	var m RunManifest
	rc, err := s.gcs.Bucket(s.resultsBucket).Object(fmt.Sprintf("runs/%s/manifest.json", runID)).NewReader(ctx)
	if err != nil {
		return m, err
	}
	defer rc.Close()
	err = json.NewDecoder(rc).Decode(&m)
	return m, err
}

func (s *Server) readObjectBytes(ctx context.Context, objectName string, maxBytes int64) ([]byte, error) {
	rc, err := s.gcs.Bucket(s.resultsBucket).Object(objectName).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxBytes))
}

func (s *Server) listRuns(ctx context.Context) ([]RunManifest, error) {
	it := s.gcs.Bucket(s.resultsBucket).Objects(ctx, &storage.Query{Prefix: "runs/"})
	var runs []RunManifest
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(attrs.Name, "/manifest.json") {
			continue
		}
		rc, err := s.gcs.Bucket(s.resultsBucket).Object(attrs.Name).NewReader(ctx)
		if err != nil {
			continue
		}
		var m RunManifest
		if json.NewDecoder(rc).Decode(&m) == nil {
			runs = append(runs, m)
		}
		_ = rc.Close()
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	return runs, nil
}

func parseLocustStatsCSV(raw string) (*EndpointStat, []EndpointStat) {
	r := csv.NewReader(strings.NewReader(raw))
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, []EndpointStat{}
	}
	col := make(map[string]int, len(records[0]))
	for i, h := range records[0] {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	get := func(row []string, key string) string {
		if idx, ok := col[key]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}
	var summary *EndpointStat
	endpoints := []EndpointStat{}
	for _, row := range records[1:] {
		reqs := parseInt64(get(row, "request count"))
		fails := parseInt64(get(row, "failure count"))
		failRate := 0.0
		if reqs > 0 {
			failRate = (float64(fails) / float64(reqs)) * 100.0
		}
		stat := EndpointStat{
			Method:      get(row, "type"),
			Name:        get(row, "name"),
			Requests:    reqs,
			Failures:    fails,
			FailureRate: failRate,
			MedianMs:    parseFloat64(get(row, "median response time")),
			AvgMs:       parseFloat64(get(row, "average response time")),
			MinMs:       parseFloat64(get(row, "min response time")),
			MaxMs:       parseFloat64(get(row, "max response time")),
			P95Ms:       parseFloat64(get(row, "95%")),
			P99Ms:       parseFloat64(get(row, "99%")),
			RPS:         parseFloat64(get(row, "requests/s")),
		}
		if strings.EqualFold(stat.Name, "Aggregated") {
			sCopy := stat
			summary = &sCopy
		} else {
			endpoints = append(endpoints, stat)
		}
	}
	return summary, endpoints
}

func parseLocustFailuresCSV(raw string) []FailureStat {
	r := csv.NewReader(strings.NewReader(raw))
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return []FailureStat{}
	}
	col := make(map[string]int, len(records[0]))
	for i, h := range records[0] {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	get := func(row []string, key string) string {
		if idx, ok := col[key]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}
	failures := []FailureStat{}
	for _, row := range records[1:] {
		failures = append(failures, FailureStat{
			Method:      get(row, "method"),
			Name:        get(row, "name"),
			Error:       get(row, "error"),
			Occurrences: parseInt64(get(row, "occurrences")),
		})
	}
	return failures
}

func tailLines(s string, maxLines int) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func parseFloat64(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func mustEnv(k string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		slog.Error("missing required env", "key", k)
		os.Exit(1)
	}
	return v
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func strPtr(s string) *string { return &s }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var uiTemplate = template.Must(template.New("ui").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SEM Load Test Controller</title>
  <style>
    :root {
      --bg: #0b1120;
      --surface: #111827;
      --surface-2: #1e293b;
      --border: #263244;
      --text: #f1f5f9;
      --muted: #94a3b8;
      --primary: #3b82f6;
      --primary-hover: #2563eb;
      --green: #10b981;
      --amber: #f59e0b;
      --red: #ef4444;
      --gray: #64748b;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: var(--bg);
      color: var(--text);
      line-height: 1.5;
    }
    .container { max-width: 1180px; margin: 0 auto; padding: 1.5rem 1.25rem 3rem; }
    .header {
      display: flex; justify-content: space-between; align-items: flex-start;
      gap: 1rem; flex-wrap: wrap; margin-bottom: 1.5rem;
    }
    .header h1 { margin: 0 0 0.25rem; font-size: 1.45rem; font-weight: 700; }
    .sub { color: var(--muted); font-size: 0.875rem; display: flex; gap: 0.75rem; align-items: center; flex-wrap: wrap; }
    .sub code { background: var(--surface-2); padding: 0.15rem 0.45rem; border-radius: 5px; color: #cbd5e1; }
    a { color: #60a5fa; text-decoration: none; }
    a:hover { text-decoration: underline; }
    .live-pill {
      display: inline-flex; align-items: center; gap: 0.4rem;
      font-size: 0.78rem; padding: 0.25rem 0.65rem; border-radius: 999px;
      background: rgba(16, 185, 129, 0.12); color: #34d399; border: 1px solid rgba(16, 185, 129, 0.3);
    }
    .dot { width: 7px; height: 7px; border-radius: 50%; background: #34d399; animation: pulse 2s infinite; }
    @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.35} }

    .card {
      background: var(--surface); border: 1px solid var(--border);
      border-radius: 12px; padding: 1.15rem 1.25rem; margin-bottom: 1.25rem;
    }
    .card-title { font-size: 1rem; font-weight: 600; margin: 0 0 0.85rem; display: flex; justify-content: space-between; align-items: center; gap: 0.75rem; flex-wrap: wrap; }
    .row { display: flex; gap: 0.75rem; align-items: center; flex-wrap: wrap; }

    select, button {
      font: inherit; font-size: 0.875rem; border-radius: 8px;
      border: 1px solid var(--border); padding: 0.5rem 0.85rem;
    }
    select { background: var(--surface-2); color: var(--text); }
    button { background: var(--surface-2); color: var(--text); cursor: pointer; font-weight: 500; transition: 0.15s; }
    button:hover:not(:disabled) { background: #2d3b4f; }
    button:disabled { opacity: 0.5; cursor: not-allowed; }
    .btn-primary { background: var(--primary); border-color: var(--primary); color: #fff; }
    .btn-primary:hover:not(:disabled) { background: var(--primary-hover); }
    .btn-danger { background: rgba(239,68,68,0.16); border-color: rgba(239,68,68,0.4); color: #fca5a5; }
    .btn-danger:hover:not(:disabled) { background: rgba(239,68,68,0.28); }
    .btn-sm { padding: 0.3rem 0.6rem; font-size: 0.78rem; }

    .alert {
      margin-top: 0.75rem; padding: 0.65rem 0.9rem; border-radius: 8px;
      font-size: 0.85rem; display: none;
    }
    .alert-error { background: rgba(239,68,68,0.14); border: 1px solid rgba(239,68,68,0.35); color: #fca5a5; }
    .alert-ok { background: rgba(16,185,129,0.14); border: 1px solid rgba(16,185,129,0.35); color: #6ee7b7; }

    .table-wrap { overflow-x: auto; }
    table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
    th, td { padding: 0.62rem 0.7rem; border-bottom: 1px solid var(--border); text-align: left; white-space: nowrap; }
    th { color: var(--muted); font-weight: 600; font-size: 0.78rem; text-transform: uppercase; letter-spacing: 0.03em; }
    tbody tr { cursor: pointer; transition: background 0.12s; }
    tbody tr:hover { background: rgba(255,255,255,0.03); }
    tbody tr.selected { background: rgba(59,130,246,0.12); }

    .badge {
      display: inline-flex; align-items: center; gap: 0.35rem;
      padding: 0.18rem 0.55rem; border-radius: 999px;
      font-size: 0.74rem; font-weight: 600; letter-spacing: 0.02em;
    }
    .badge-STARTING, .badge-RUNNING { background: rgba(59,130,246,0.18); color: #93c5fd; border: 1px solid rgba(59,130,246,0.35); }
    .badge-STOPPING { background: rgba(245,158,11,0.18); color: #fcd34d; border: 1px solid rgba(245,158,11,0.35); }
    .badge-SUCCEEDED { background: rgba(16,185,129,0.18); color: #6ee7b7; border: 1px solid rgba(16,185,129,0.35); }
    .badge-FAILED { background: rgba(239,68,68,0.18); color: #fca5a5; border: 1px solid rgba(239,68,68,0.35); }
    .badge-STOPPED { background: rgba(100,116,139,0.22); color: #cbd5e1; border: 1px solid rgba(100,116,139,0.4); }

    .kpi-grid {
      display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
      gap: 0.75rem; margin-bottom: 1.1rem;
    }
    .kpi {
      background: var(--surface-2); border: 1px solid var(--border);
      border-radius: 10px; padding: 0.8rem 0.9rem;
    }
    .kpi-label { font-size: 0.74rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
    .kpi-val { font-size: 1.35rem; font-weight: 700; margin-top: 0.2rem; }
    .kpi-sub { font-size: 0.75rem; color: var(--muted); margin-top: 0.1rem; }
    .text-green { color: #34d399; }
    .text-red { color: #f87171; }
    .text-amber { color: #fbbf24; }

    .method-pill {
      display: inline-block; padding: 0.1rem 0.4rem; border-radius: 4px;
      font-size: 0.72rem; font-weight: 700; background: rgba(96,165,250,0.15); color: #93c5fd;
    }
    pre.log-box {
      background: #020617; border: 1px solid var(--border); border-radius: 8px;
      padding: 0.85rem; font-size: 0.78rem; color: #cbd5e1;
      max-height: 320px; overflow: auto; white-space: pre-wrap; word-break: break-word; margin: 0;
    }
    .muted { color: var(--muted); font-size: 0.85rem; }
  </style>
</head>
<body>
  <div class="container">
    <div class="header">
      <div>
        <h1>Social Event Mapper — Load Test Controller</h1>
        <div class="sub">
          <span>Target: <code>{{.TargetHost}}</code></span>
          <a href="https://monitoring.socialeventmapper.com" target="_blank" rel="noopener">Open Grafana ↗</a>
        </div>
      </div>
      <div class="row">
        <span class="live-pill"><span class="dot"></span> <span id="liveText">Live · polling 3s</span></span>
        <button id="refreshBtn" type="button">Refresh</button>
      </div>
    </div>

    <div class="card">
      <div class="card-title">Start New Load Test</div>
      <form id="startForm" class="row">
        <label for="profileSelect" class="muted">Profile:</label>
        <select id="profileSelect" name="profile">
          <option value="smoke">smoke (10 users, 2/s, 1m)</option>
          <option value="baseline">baseline (50 users, 5/s, 5m)</option>
          <option value="stress">stress (150 users, 15/s, 8m)</option>
        </select>
        <button id="startBtn" class="btn-primary" type="submit">Start Run</button>
      </form>
      <div id="formAlert" class="alert"></div>
    </div>

    <div class="card">
      <div class="card-title">
        <span>Test Runs</span>
        <span id="runsCount" class="muted"></span>
      </div>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Run ID</th>
              <th>Profile</th>
              <th>Status</th>
              <th>Elapsed / Duration</th>
              <th>VM</th>
              <th>Created</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="runsBody"></tbody>
        </table>
      </div>
    </div>

    <div id="reportCard" class="card" style="display:none;">
      <div class="card-title">
        <span>Test Report — <code id="reportRunId"></code></span>
        <div class="row" id="reportDownloads"></div>
      </div>
      <div id="reportMeta" class="muted" style="margin-bottom:1rem;"></div>
      <div id="reportContent"></div>
    </div>
  </div>

  <script>
    let runs = {{.InitialRuns}} || [];
    let selectedRunId = runs.length > 0 ? runs[0].run_id : null;
    let lastReportRunId = null;
    let lastReportStatus = null;

    function esc(s) {
      return String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    }

    function fmtTime(iso) {
      if (!iso) return '—';
      const d = new Date(iso);
      if (isNaN(d.getTime())) return iso;
      return d.toLocaleString();
    }

    function fmtElapsed(run) {
      if (!run.created_at) return '—';
      const start = new Date(run.started_at || run.created_at).getTime();
      const end = run.finished_at ? new Date(run.finished_at).getTime() : Date.now();
      const sec = Math.max(0, Math.floor((end - start) / 1000));
      const m = Math.floor(sec / 60);
      const s = sec % 60;
      return m > 0 ? m + 'm ' + s + 's' : s + 's';
    }

    function isActive(status) {
      return status === 'STARTING' || status === 'RUNNING' || status === 'STOPPING';
    }

    function showAlert(msg, isError) {
      const el = document.getElementById('formAlert');
      el.textContent = msg;
      el.className = 'alert ' + (isError ? 'alert-error' : 'alert-ok');
      el.style.display = 'block';
      setTimeout(() => { el.style.display = 'none'; }, 6000);
    }

    function renderRuns() {
      const tbody = document.getElementById('runsBody');
      document.getElementById('runsCount').textContent = runs.length + ' run(s)';
      const activeRun = runs.find(r => isActive(r.status));
      document.getElementById('startBtn').disabled = Boolean(activeRun);

      if (runs.length === 0) {
        tbody.innerHTML = '<tr><td colspan="7" class="muted">No test runs yet. Start a smoke test above.</td></tr>';
        return;
      }

      tbody.innerHTML = runs.map(r => {
        const sel = r.run_id === selectedRunId ? 'selected' : '';
        const vmTxt = esc(r.vm_name) + (r.vm_deleted ? ' <span class="muted">(deleted)</span>' : '');
        const stopBtn = (r.status === 'STARTING' || r.status === 'RUNNING')
          ? '<button class="btn-danger btn-sm" onclick="event.stopPropagation(); stopRun(\'' + esc(r.run_id) + '\')">Stop</button> '
          : '';
        return '<tr class="' + sel + '" onclick="selectRun(\'' + esc(r.run_id) + '\')">'
          + '<td><code>' + esc(r.run_id) + '</code></td>'
          + '<td>' + esc(r.profile) + ' <span class="muted">(' + r.users + 'u, ' + esc(r.duration) + ')</span></td>'
          + '<td><span class="badge badge-' + esc(r.status) + '">' + esc(r.status) + '</span></td>'
          + '<td>' + fmtElapsed(r) + '</td>'
          + '<td>' + vmTxt + '</td>'
          + '<td>' + fmtTime(r.created_at) + '</td>'
          + '<td>' + stopBtn + '<button class="btn-sm" onclick="event.stopPropagation(); selectRun(\'' + esc(r.run_id) + '\')">Report</button></td>'
          + '</tr>';
      }).join('');
    }

    async function fetchRuns(forceReportReload) {
      try {
        const res = await fetch('/api/runs', { cache: 'no-store' });
        if (!res.ok) return;
        runs = await res.json();
        if (!selectedRunId && runs.length > 0) {
          selectedRunId = runs[0].run_id;
        }
        renderRuns();
        document.getElementById('liveText').textContent = 'Live · updated ' + new Date().toLocaleTimeString();

        if (selectedRunId) {
          const cur = runs.find(r => r.run_id === selectedRunId);
          const curStatus = cur ? cur.status : null;
          if (forceReportReload || lastReportRunId !== selectedRunId || lastReportStatus !== curStatus || isActive(curStatus)) {
            await fetchReport(selectedRunId);
          }
        }
      } catch (e) {
        document.getElementById('liveText').textContent = 'Reconnecting...';
      }
    }

    async function selectRun(runId) {
      selectedRunId = runId;
      renderRuns();
      await fetchReport(runId);
    }

    async function fetchReport(runId) {
      const card = document.getElementById('reportCard');
      card.style.display = 'block';
      document.getElementById('reportRunId').textContent = runId;

      const res = await fetch('/api/runs/' + encodeURIComponent(runId) + '/report', { cache: 'no-store' });
      if (!res.ok) {
        document.getElementById('reportContent').innerHTML = '<p class="muted">Failed to load report.</p>';
        return;
      }
      const data = await res.json();
      const m = data.manifest;
      lastReportRunId = runId;
      lastReportStatus = m.status;

      const files = [
        ['locust_stats.csv', 'stats.csv'],
        ['locust_stats_history.csv', 'history.csv'],
        ['locust_failures.csv', 'failures.csv'],
        ['runner.log', 'runner.log'],
        ['manifest.json', 'manifest.json'],
      ];
      document.getElementById('reportDownloads').innerHTML = files.map(([f, label]) =>
        '<a class="btn-sm" style="background:var(--surface-2);border:1px solid var(--border);border-radius:6px;padding:0.25rem 0.55rem;" href="/api/runs/' + esc(runId) + '/results/' + f + '">' + label + '</a>'
      ).join('');

      document.getElementById('reportMeta').innerHTML =
        '<span class="badge badge-' + esc(m.status) + '">' + esc(m.status) + '</span> · '
        + 'Profile: <strong>' + esc(m.profile) + '</strong> (' + m.users + ' users, ' + m.spawn_rate + '/s, ' + esc(m.duration) + ') · '
        + 'Started: ' + fmtTime(m.started_at || m.created_at) + ' · '
        + 'Finished: ' + fmtTime(m.finished_at)
        + (m.exit_code !== undefined && m.exit_code !== null ? ' · Exit code: <code>' + m.exit_code + '</code>' : '');

      let html = '';
      if (data.has_stats && data.summary) {
        const s = data.summary;
        const failCls = s.failures > 0 ? 'text-red' : 'text-green';
        html += '<div class="kpi-grid">'
          + kpi('Total Requests', s.requests.toLocaleString(), s.rps.toFixed(1) + ' req/s')
          + kpi('Failure Rate', s.failure_rate.toFixed(2) + '%', s.failures.toLocaleString() + ' failed', failCls)
          + kpi('Median (P50)', Math.round(s.median_ms) + ' ms', 'Avg: ' + Math.round(s.avg_ms) + ' ms')
          + kpi('P95 Latency', Math.round(s.p95_ms) + ' ms', 'P99: ' + Math.round(s.p99_ms) + ' ms')
          + kpi('Min / Max', Math.round(s.min_ms) + ' / ' + Math.round(s.max_ms) + ' ms', 'Response time range')
          + '</div>';

        if (data.endpoints && data.endpoints.length > 0) {
          html += '<h3 style="font-size:0.9rem;margin:0.5rem 0;">Endpoint Breakdown</h3>'
            + '<div class="table-wrap"><table><thead><tr>'
            + '<th>Method</th><th>Endpoint</th><th>Requests</th><th>Failures</th><th>Fail %</th><th>RPS</th><th>Median</th><th>Avg</th><th>P95</th><th>P99</th><th>Max</th>'
            + '</tr></thead><tbody>'
            + data.endpoints.map(ep => {
              const fc = ep.failures > 0 ? 'text-red' : '';
              return '<tr>'
                + '<td><span class="method-pill">' + esc(ep.method || 'REQ') + '</span></td>'
                + '<td><code>' + esc(ep.name) + '</code></td>'
                + '<td>' + ep.requests.toLocaleString() + '</td>'
                + '<td class="' + fc + '">' + ep.failures.toLocaleString() + '</td>'
                + '<td class="' + fc + '">' + ep.failure_rate.toFixed(2) + '%</td>'
                + '<td>' + ep.rps.toFixed(2) + '</td>'
                + '<td>' + Math.round(ep.median_ms) + ' ms</td>'
                + '<td>' + Math.round(ep.avg_ms) + ' ms</td>'
                + '<td>' + Math.round(ep.p95_ms) + ' ms</td>'
                + '<td>' + Math.round(ep.p99_ms) + ' ms</td>'
                + '<td>' + Math.round(ep.max_ms) + ' ms</td>'
                + '</tr>';
            }).join('')
            + '</tbody></table></div>';
        }
      } else if (isActive(m.status)) {
        html += '<p class="muted">Test is currently <strong>' + esc(m.status) + '</strong>. Results and report will appear here automatically once Locust finishes...</p>';
      } else {
        html += '<p class="muted">No Locust stats CSV was produced for this run. Check the runner log below for details.</p>';
      }

      if (data.has_failures && data.failures && data.failures.length > 0) {
        html += '<h3 style="font-size:0.9rem;margin:1.1rem 0 0.5rem;color:#fca5a5;">Failures</h3>'
          + '<div class="table-wrap"><table><thead><tr><th>Method</th><th>Name</th><th>Occurrences</th><th>Error</th></tr></thead><tbody>'
          + data.failures.map(f =>
            '<tr><td><span class="method-pill">' + esc(f.method) + '</span></td><td><code>' + esc(f.name) + '</code></td><td class="text-red">' + f.occurrences + '</td><td><code>' + esc(f.error) + '</code></td></tr>'
          ).join('')
          + '</tbody></table></div>';
      } else if (data.has_stats) {
        html += '<div style="margin-top:0.9rem;padding:0.55rem 0.8rem;border-radius:8px;background:rgba(16,185,129,0.1);border:1px solid rgba(16,185,129,0.28);color:#6ee7b7;font-size:0.84rem;">0 failures recorded during this run.</div>';
      }

      if (data.has_log && data.log_tail) {
        html += '<h3 style="font-size:0.9rem;margin:1.1rem 0 0.5rem;">Runner Log (tail)</h3>'
          + '<pre class="log-box">' + esc(data.log_tail) + '</pre>';
      }

      document.getElementById('reportContent').innerHTML = html;
    }

    function kpi(label, val, sub, valClass) {
      return '<div class="kpi"><div class="kpi-label">' + esc(label) + '</div>'
        + '<div class="kpi-val ' + (valClass || '') + '">' + esc(val) + '</div>'
        + '<div class="kpi-sub">' + esc(sub || '') + '</div></div>';
    }

    document.getElementById('startForm').addEventListener('submit', async (e) => {
      e.preventDefault();
      const profile = document.getElementById('profileSelect').value;
      const btn = document.getElementById('startBtn');
      btn.disabled = true;
      try {
        const res = await fetch('/api/runs', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
          body: JSON.stringify({ profile }),
        });
        const data = await res.json();
        if (!res.ok) {
          showAlert(data.error || 'Failed to start run', true);
          btn.disabled = false;
          return;
        }
        selectedRunId = data.run_id;
        showAlert('Started ' + data.run_id + ' (' + data.profile + ')', false);
        await fetchRuns(true);
      } catch (err) {
        showAlert('Request failed: ' + err.message, true);
        btn.disabled = false;
      }
    });

    async function stopRun(runId) {
      try {
        const res = await fetch('/api/runs/' + encodeURIComponent(runId) + '/stop', {
          method: 'POST',
          headers: { 'Accept': 'application/json' }
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          showAlert(data.error || 'Failed to stop run', true);
          return;
        }
        showAlert('Stop requested for ' + runId, false);
        await fetchRuns(true);
      } catch (err) {
        showAlert('Stop failed: ' + err.message, true);
      }
    }

    document.getElementById('refreshBtn').addEventListener('click', () => fetchRuns(true));

    renderRuns();
    if (selectedRunId) fetchReport(selectedRunId);
    setInterval(() => fetchRuns(false), 3000);
  </script>
</body>
</html>`))
