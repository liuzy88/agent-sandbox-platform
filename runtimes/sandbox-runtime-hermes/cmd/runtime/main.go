// Command runtime is the HTTP transport for the sandboxed Hermes loop.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const root = "/app"
const maxBody = 4 << 20
const contract = "agent-platform-runtime/v1"

type manifest struct {
	ContractVersion string   `json:"contract_version"`
	Entrypoint      []string `json:"entrypoint"`
	Capabilities    []string `json:"capabilities"`
}
type config struct {
	ContractVersion string            `json:"contract_version"`
	RunID           string            `json:"run_id"`
	Stage           int               `json:"stage"`
	ExecutionID     string            `json:"execution_id"`
	Messages        []json.RawMessage `json:"messages"`
	Model           struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
		Token   string `json:"token"`
	} `json:"model"`
	Runtime struct {
		BaseURL string `json:"base_url"`
		Token   string `json:"token"`
	} `json:"runtime"`
	Limits struct {
		Remaining int `json:"remaining_execution_seconds"`
	} `json:"limits"`
}
type service struct {
	manifest manifest
	mu       sync.Mutex
	active   string
	cancel   context.CancelFunc
}

func main() {
	serve := flag.Bool("serve", false, "serve")
	port := flag.Int("port", 8888, "port")
	flag.Parse()
	if !*serve {
		os.Exit(2)
	}
	m, e := loadManifest()
	if e != nil {
		os.Exit(2)
	}
	s := &service{manifest: m}
	h := http.NewServeMux()
	h.HandleFunc("/", s.health)
	h.HandleFunc("/manifest", s.getManifest)
	h.HandleFunc("/execute", s.execute)
	h.HandleFunc("/cancel", s.cancelRun)
	h.HandleFunc("/upload", s.upload)
	h.HandleFunc("/download/", s.download)
	h.HandleFunc("/list/", s.list)
	h.HandleFunc("/exists/", s.exists)
	if e = http.ListenAndServe(fmt.Sprintf(":%d", *port), h); e != nil {
		fmt.Fprintln(os.Stderr, e)
	}
}
func loadManifest() (manifest, error) {
	var m manifest
	b, e := os.ReadFile(root + "/manifest.json")
	if e != nil || json.Unmarshal(b, &m) != nil || m.ContractVersion != contract || len(m.Entrypoint) == 0 || len(m.Capabilities) != 10 {
		return m, errors.New("invalid manifest")
	}
	return m, nil
}
func (s *service) health(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" || r.Method != "GET" {
		bad(w, 404, "not_found")
		return
	}
	out(w, 200, map[string]string{"status": "ok", "version": "hermes-runtime/2"})
}
func (s *service) getManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		out(w, 405, nil)
		return
	}
	out(w, 200, s.manifest)
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("json required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func readConfig() (config, error) {
	var c config
	b, e := os.ReadFile(root + "/run.json")
	if e != nil || json.Unmarshal(b, &c) != nil || c.ContractVersion != contract || c.RunID == "" || c.Stage < 1 || c.ExecutionID == "" || len(c.Messages) == 0 || c.Model.Name == "" || c.Model.BaseURL == "" || c.Model.Token == "" || c.Runtime.BaseURL == "" || c.Runtime.Token == "" || c.Limits.Remaining <= 0 {
		return c, errors.New("invalid run.json")
	}
	return c, nil
}
func (s *service) execute(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		out(w, 405, nil)
		return
	}
	var q struct {
		ExecutionID string `json:"execution_id"`
	}
	if decode(w, r, &q) != nil {
		bad(w, 400, "invalid_request")
		return
	}
	c, e := readConfig()
	if e != nil || (q.ExecutionID != "" && q.ExecutionID != c.ExecutionID) {
		bad(w, 400, "runtime_protocol_invalid")
		return
	}
	s.mu.Lock()
	if s.active != "" {
		s.mu.Unlock()
		bad(w, 409, "runtime_state_conflict")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	s.active, s.cancel = c.ExecutionID, cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active = ""; s.cancel = nil; s.mu.Unlock(); cancel() }()
	cmd := exec.CommandContext(ctx, "python3", root+"/hermes_loop.py", "--config", root+"/run.json")
	stdout, _ := cmd.CombinedOutput()
	if ctx.Err() != nil {
		writeError("run_cancelled")
		bad(w, 400, "run_cancelled")
		return
	}
	b, e := os.ReadFile(root + "/output/result.json")
	var result map[string]any
	if e != nil || json.Unmarshal(b, &result) != nil {
		writeError("agent_execution_failed")
		bad(w, 400, "agent_execution_failed")
		return
	}
	if result["status"] == "error" {
		bad(w, 400, "agent_execution_failed")
		return
	}
	summary, _ := result["summary"].(string)
	if summary == "" {
		summary = string(stdout)
	}
	out(w, 200, map[string]any{"execution_id": c.ExecutionID, "stdout": clip(summary, 2<<20), "stderr": "", "exit_code": 0})
}
func (s *service) cancelRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		out(w, 405, nil)
		return
	}
	var q struct {
		ExecutionID string `json:"execution_id"`
	}
	if decode(w, r, &q) != nil || q.ExecutionID == "" {
		bad(w, 400, "invalid_request")
		return
	}
	s.mu.Lock()
	yes := s.active == q.ExecutionID
	if yes && s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	if yes {
		out(w, 202, map[string]string{"status": "accepted", "execution_id": q.ExecutionID})
	} else {
		out(w, 200, map[string]string{"status": "stopped", "execution_id": q.ExecutionID})
	}
}
func path(raw string) (string, error) {
	clean := filepath.Clean("/" + raw)
	if raw == "" || strings.HasPrefix(raw, "/") || clean == "/" || strings.Contains(raw, "..") {
		return "", errors.New("unsafe")
	}
	p := filepath.Join(root, clean)
	if !strings.HasPrefix(p, root+"/") {
		return "", errors.New("unsafe")
	}
	return p, nil
}
func (s *service) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		out(w, 405, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if r.ParseMultipartForm(maxBody) != nil {
		bad(w, 400, "invalid_request")
		return
	}
	in, h, e := r.FormFile("file")
	if e != nil {
		bad(w, 400, "invalid_request")
		return
	}
	defer in.Close()
	p, e := path(h.Filename)
	if e != nil {
		bad(w, 400, "invalid_request")
		return
	}
	os.MkdirAll(filepath.Dir(p), 0750)
	f, e := os.Create(p)
	if e != nil {
		bad(w, 400, "runtime_error")
		return
	}
	_, e = io.Copy(f, in)
	f.Close()
	if e != nil {
		bad(w, 400, "runtime_error")
		return
	}
	out(w, 200, map[string]string{"status": "uploaded", "path": "/" + filepath.Base(p)})
}
func (s *service) download(w http.ResponseWriter, r *http.Request) {
	p, e := path(strings.TrimPrefix(r.URL.Path, "/download/"))
	if r.Method != "GET" || e != nil {
		bad(w, 404, "not_found")
		return
	}
	http.ServeFile(w, r, p)
}
func (s *service) list(w http.ResponseWriter, r *http.Request) {
	p, e := path(strings.TrimPrefix(r.URL.Path, "/list/"))
	if r.Method != "GET" || e != nil {
		bad(w, 400, "invalid_request")
		return
	}
	es, e := os.ReadDir(p)
	if e != nil {
		bad(w, 404, "not_found")
		return
	}
	ns := []string{}
	for _, x := range es {
		ns = append(ns, x.Name())
	}
	out(w, 200, map[string]any{"entries": ns})
}
func (s *service) exists(w http.ResponseWriter, r *http.Request) {
	p, e := path(strings.TrimPrefix(r.URL.Path, "/exists/"))
	if r.Method != "GET" || e != nil {
		bad(w, 400, "invalid_request")
		return
	}
	_, e = os.Stat(p)
	out(w, 200, map[string]bool{"exists": e == nil})
}
func writeError(code string) {
	os.MkdirAll(root+"/output", 0750)
	b, _ := json.Marshal(map[string]any{"status": "error", "error_code": code, "steering": map[string]int{"incorporated_through_seq": 0}})
	os.WriteFile(root+"/output/.result.tmp", b, 0600)
	os.Rename(root+"/output/.result.tmp", root+"/output/result.json")
}
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func out(w http.ResponseWriter, n int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(n)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}
func bad(w http.ResponseWriter, n int, c string) {
	out(w, n, map[string]string{"status": "error", "error_code": c})
}

var _ = time.Second
