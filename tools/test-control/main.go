package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
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
	RunID        string    `json:"run_id"`
	Profile      string    `json:"profile"`
	TargetHost   string    `json:"target_host"`
	Users        int       `json:"users"`
	SpawnRate    int       `json:"spawn_rate"`
	Duration     string    `json:"duration"`
	Status       string    `json:"status"`
	DesiredState string    `json:"desired_state"`
	VMName       string    `json:"vm_name"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	StartedAt    string    `json:"started_at,omitempty"`
	FinishedAt   string    `json:"finished_at,omitempty"`
	VMDeleted    bool      `json:"vm_deleted,omitempty"`
}

type Lease struct {
	RunID     string    `json:"run_id"`
	ExpiresAt time.Time `json:"expires_at"`
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = uiTemplate.Execute(w, map[string]any{
		"Runs":       runs,
		"TargetHost": s.targetHost,
		"Profiles":   profiles,
	})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.listRuns(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
		http.Error(w, "invalid profile", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()
	runID := fmt.Sprintf("run-%s-%s", now.Format("20060102-150405"), prof.Name)
	vmName := "sem-locust-" + runID

	if err := s.acquireLease(ctx, Lease{RunID: runID, ExpiresAt: now.Add(prof.MaxTTL)}); err != nil {
		http.Error(w, "an experiment is already active: "+err.Error(), http.StatusConflict)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.createRunnerVM(ctx, manifest); err != nil {
		manifest.Status = "FAILED"
		_ = s.saveManifest(ctx, manifest)
		s.releaseLease(ctx)
		http.Error(w, "create runner VM: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "text/html") || r.FormValue("profile") != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusCreated, manifest)
}

func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	manifest, err := s.loadManifest(r.Context(), runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	manifest.DesiredState = "STOPPING"
	if manifest.Status == "STARTING" || manifest.Status == "RUNNING" {
		manifest.Status = "STOPPING"
	}
	if err := s.saveManifest(r.Context(), manifest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "text/html") || r.Method == http.MethodPost && r.Header.Get("Content-Type") == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, manifest)
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
	items := []*computepb.Items{
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
	ticker := time.NewTicker(20 * time.Second)
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
		terminal := m.Status == "SUCCEEDED" || m.Status == "FAILED"
		expired := now.After(m.ExpiresAt)
		if terminal || expired {
			if err := s.deleteRunnerVM(ctx, m.VMName); err != nil {
				slog.Error("delete runner vm failed", "vm", m.VMName, "error", err)
				continue
			}
			m.VMDeleted = true
			if expired && !terminal {
				m.Status = "FAILED"
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

var uiTemplate = template.Must(template.New("ui").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>SEM Load Test Controller</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 960px; margin: 2rem auto; padding: 0 1rem; color: #1f2937; }
    table { width: 100%; border-collapse: collapse; margin-top: 1rem; }
    th, td { border: 1px solid #e5e7eb; padding: 0.5rem; text-align: left; font-size: 0.9rem; }
    th { background: #f9fafb; }
    button { padding: 0.45rem 0.9rem; cursor: pointer; }
    .row { display: flex; gap: 0.75rem; align-items: center; flex-wrap: wrap; }
  </style>
</head>
<body>
  <h1>Social Event Mapper — Load Test Controller</h1>
  <p>Target: <code>{{.TargetHost}}</code> · <a href="https://monitoring.socialeventmapper.com" target="_blank">Open Grafana</a></p>
  <form method="post" action="/api/runs" class="row">
    <label>Profile:
      <select name="profile">
        <option value="smoke">smoke (10 users, 1m)</option>
        <option value="baseline">baseline (50 users, 5m)</option>
        <option value="stress">stress (150 users, 8m)</option>
      </select>
    </label>
    <button type="submit">Start Run</button>
  </form>
  <table>
    <thead>
      <tr><th>Run ID</th><th>Profile</th><th>Status</th><th>VM</th><th>Created</th><th>Actions / Results</th></tr>
    </thead>
    <tbody>
      {{range .Runs}}
      <tr>
        <td><code>{{.RunID}}</code></td>
        <td>{{.Profile}} ({{.Users}}u, {{.Duration}})</td>
        <td><strong>{{.Status}}</strong></td>
        <td>{{.VMName}} {{if .VMDeleted}}(deleted){{end}}</td>
        <td>{{.CreatedAt.Format "2006-01-02 15:04:05"}}</td>
        <td>
          {{if or (eq .Status "STARTING") (eq .Status "RUNNING")}}
            <form method="post" action="/api/runs/{{.RunID}}/stop" style="display:inline"><button type="submit">Stop</button></form>
          {{end}}
          <a href="/api/runs/{{.RunID}}/results/locust_stats.csv">stats.csv</a> ·
          <a href="/api/runs/{{.RunID}}/results/runner.log">runner.log</a>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>`))
