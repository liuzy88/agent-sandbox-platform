// Command runtime adapts Hermes' loopback OpenAI API to agent-platform-runtime/v1.
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
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	root       = "/app"
	maxBody    = 4 << 20
	version    = "hermes-runtime/1"
	contractV1 = "agent-platform-runtime/v1"
)

type manifest struct {
	ContractVersion string   `json:"contract_version"`
	ImageVersion    string   `json:"image_version"`
	Entrypoint      []string `json:"entrypoint"`
	StateFormat     string   `json:"state_format"`
	Capabilities    []string `json:"capabilities"`
}

type runtimeConfig struct {
	ContractVersion string            `json:"contract_version"`
	RunID           string            `json:"run_id"`
	Stage           int               `json:"stage"`
	Fence           int               `json:"fence"`
	ExecutionID     string            `json:"execution_id"`
	Messages        []json.RawMessage `json:"messages"`
	Model           struct {
		Name string `json:"name"`
	} `json:"model"`
	Runtime struct {
		BaseURL string `json:"base_url"`
		Token   string `json:"token"`
	} `json:"runtime"`
	Steering struct {
		AfterSeq int `json:"after_seq"`
	} `json:"steering"`
}

type service struct {
	manifest manifest
	mu       sync.Mutex
	active   string
	cancel   context.CancelFunc
	client   *http.Client
}

func main() {
	serve := flag.Bool("serve", false, "serve runtime HTTP transport")
	port := flag.Int("port", 8888, "HTTP port")
	flag.Parse()
	if !*serve {
		fmt.Fprintln(os.Stderr, "runtime: --serve is required")
		os.Exit(2)
	}
	m, err := loadManifest()
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtime_protocol_invalid")
		os.Exit(2)
	}
	s := &service{manifest: m, client: &http.Client{Timeout: gatewayTimeout()}}
	httpServer := &http.Server{Addr: fmt.Sprintf("0.0.0.0:%d", *port), Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func gatewayTimeout() time.Duration {
	if raw := os.Getenv("HERMES_RUNTIME_GATEWAY_TIMEOUT_SECONDS"); raw != "" {
		if seconds, err := time.ParseDuration(raw + "s"); err == nil && seconds > 0 {
			return seconds
		}
	}
	return 10 * time.Minute
}

func loadManifest() (manifest, error) {
	var m manifest
	path := os.Getenv("AGENT_RUNTIME_MANIFEST")
	if path == "" {
		path = root + "/manifest.json"
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &m) != nil || m.ContractVersion != contractV1 || len(m.Entrypoint) == 0 || !hasCapabilities(m.Capabilities) {
		return m, errors.New("invalid manifest")
	}
	return m, nil
}

func hasCapabilities(values []string) bool {
	required := map[string]bool{"execution": false, "events": false, "cancel": false, "pause_resume": false, "steering": false, "files": false, "skills": false, "openapi_tools": false, "mcp_tools": false, "artifacts": false}
	for _, value := range values {
		if _, ok := required[value]; ok {
			required[value] = true
		}
	}
	for _, value := range required {
		if !value {
			return false
		}
	}
	return true
}

func (s *service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.health)
	mux.HandleFunc("/manifest", s.getManifest)
	mux.HandleFunc("/execute", s.execute)
	mux.HandleFunc("/cancel", s.cancelExecution)
	mux.HandleFunc("/upload", s.upload)
	mux.HandleFunc("/download/", s.download)
	mux.HandleFunc("/list/", s.list)
	mux.HandleFunc("/exists/", s.exists)
	return mux
}

func (s *service) health(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" || r.Method != http.MethodGet {
		reply(w, http.StatusNotFound, map[string]string{"status": "error", "error_code": "not_found"})
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "ok", "version": version})
}
func (s *service) getManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	reply(w, http.StatusOK, s.manifest)
}

func (s *service) execute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var request struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := decodeRequest(w, r, &request); err != nil {
		replyFailure(w, err)
		return
	}
	cfg, err := readConfig()
	if err != nil {
		replyFailure(w, err)
		return
	}
	if request.ExecutionID != "" && request.ExecutionID != cfg.ExecutionID {
		replyFailure(w, invalid("execution_id does not match run.json"))
		return
	}
	s.mu.Lock()
	if s.active != "" {
		s.mu.Unlock()
		replyFailure(w, conflict("runtime_state_conflict"))
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	s.active, s.cancel = cfg.ExecutionID, cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active, s.cancel = "", nil; s.mu.Unlock(); cancel() }()
	content, err := s.invokeHermes(ctx, cfg)
	if err != nil {
		_ = writeResult(map[string]any{"status": "error", "error_code": codeOf(err), "steering": map[string]int{"incorporated_through_seq": 0}})
		replyFailure(w, err)
		return
	}
	s.emitEvent(cfg, content)
	result := map[string]any{"status": "ok", "summary": content, "delivery": map[string]string{"status": "not_requested"}, "steering": map[string]int{"incorporated_through_seq": cfg.Steering.AfterSeq}}
	if err := writeResult(result); err != nil {
		replyFailure(w, failure("runtime_error", "cannot write result"))
		return
	}
	reply(w, http.StatusOK, map[string]any{"execution_id": cfg.ExecutionID, "stdout": limit(content, 2<<20), "stderr": "", "exit_code": 0})
}

func (s *service) cancelExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var request struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := decodeRequest(w, r, &request); err != nil || request.ExecutionID == "" {
		if err == nil {
			err = invalid("execution_id is required")
		}
		replyFailure(w, err)
		return
	}
	s.mu.Lock()
	active := s.active == request.ExecutionID
	if active && s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	if active {
		reply(w, http.StatusAccepted, map[string]string{"status": "accepted", "execution_id": request.ExecutionID})
	} else {
		reply(w, http.StatusOK, map[string]string{"status": "stopped", "execution_id": request.ExecutionID})
	}
}

func readConfig() (runtimeConfig, error) {
	var cfg runtimeConfig
	b, err := os.ReadFile(root + "/run.json")
	if err != nil || json.Unmarshal(b, &cfg) != nil || cfg.ContractVersion != contractV1 || cfg.RunID == "" || cfg.Stage < 1 || cfg.ExecutionID == "" || cfg.Model.Name == "" || len(cfg.Messages) == 0 || cfg.Runtime.BaseURL == "" || cfg.Runtime.Token == "" {
		return cfg, failure("runtime_protocol_invalid", "invalid run.json")
	}
	return cfg, nil
}

func (s *service) invokeHermes(ctx context.Context, cfg runtimeConfig) (string, error) {
	base := strings.TrimRight(os.Getenv("HERMES_RUNTIME_GATEWAY_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:8642"
	}
	body, _ := json.Marshal(map[string]any{"model": cfg.Model.Name, "messages": cfg.Messages, "stream": false})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	key := os.Getenv("API_SERVER_KEY")
	if key == "" {
		key = "agent-platform-local"
	}
	req.Header.Set("Authorization", "Bearer "+key)
	response, err := s.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", failure("run_cancelled", "execution cancelled")
		}
		return "", failure("agent_execution_failed", "Hermes API request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", failure("agent_execution_failed", "Hermes API request failed")
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, maxBody)).Decode(&payload) != nil || len(payload.Choices) == 0 || payload.Choices[0].Message.Content == "" {
		return "", failure("agent_execution_failed", "Hermes returned no assistant content")
	}
	return payload.Choices[0].Message.Content, nil
}

func (s *service) emitEvent(cfg runtimeConfig, content string) {
	payload, _ := json.Marshal(map[string]any{"execution_id": cfg.ExecutionID, "source_seq": 1, "type": "agent.token", "payload": map[string]string{"delta": limit(content, 64<<10)}})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.Runtime.BaseURL, "/")+"/events", strings.NewReader(string(payload)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Runtime.Token)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err == nil {
		response.Body.Close()
	}
}

func (s *service) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		replyFailure(w, invalid("invalid upload"))
		return
	}
	source, header, err := r.FormFile("file")
	if err != nil {
		replyFailure(w, invalid("file is required"))
		return
	}
	defer source.Close()
	target, err := safePath(header.Filename)
	if err != nil {
		replyFailure(w, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
		replyFailure(w, failure("runtime_error", "cannot create upload path"))
		return
	}
	out, err := os.Create(target)
	if err != nil {
		replyFailure(w, failure("runtime_error", "cannot store upload"))
		return
	}
	_, err = io.Copy(out, source)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		replyFailure(w, failure("runtime_error", "cannot store upload"))
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "uploaded", "path": "/" + filepath.Base(target)})
}
func (s *service) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	target, err := safePath(strings.TrimPrefix(r.URL.Path, "/download/"))
	if err != nil {
		reply(w, 404, map[string]string{"status": "error", "error_code": "not_found"})
		return
	}
	http.ServeFile(w, r, target)
}
func (s *service) exists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	target, err := safePath(strings.TrimPrefix(r.URL.Path, "/exists/"))
	if err != nil {
		replyFailure(w, err)
		return
	}
	_, err = os.Stat(target)
	reply(w, http.StatusOK, map[string]bool{"exists": err == nil})
}
func (s *service) list(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	raw := strings.TrimPrefix(r.URL.Path, "/list/")
	target, err := safePath(raw)
	if err != nil {
		replyFailure(w, err)
		return
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		reply(w, 404, map[string]string{"status": "error", "error_code": "not_found"})
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	reply(w, http.StatusOK, map[string]any{"path": raw, "entries": names})
}

func safePath(raw string) (string, error) {
	clean := filepath.Clean("/" + raw)
	if raw == "" || strings.HasPrefix(raw, "/") || clean == "/" || strings.Contains(raw, "..") {
		return "", invalid("unsafe path")
	}
	target := filepath.Join(root, clean)
	if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", invalid("unsafe path")
	}
	return target, nil
}
func writeResult(value any) error {
	if err := os.MkdirAll(filepath.Dir(root+"/output/result.json"), 0750); err != nil {
		return err
	}
	temporary := root + "/output/.result.json.tmp"
	b, err := json.Marshal(value)
	if err == nil {
		err = os.WriteFile(temporary, b, 0600)
	}
	if err == nil {
		err = os.Rename(temporary, root+"/output/result.json")
	}
	return err
}
func decodeRequest(w http.ResponseWriter, r *http.Request, target any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return invalid("JSON required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalid("invalid request")
	}
	return nil
}

type runtimeFailure struct{ code, message string }

func (e runtimeFailure) Error() string   { return e.message }
func failure(code, message string) error { return runtimeFailure{code, message} }
func invalid(message string) error       { return failure("invalid_request", message) }
func conflict(code string) error         { return failure(code, code) }
func codeOf(err error) string {
	var e runtimeFailure
	if errors.As(err, &e) {
		return e.code
	}
	return "runtime_error"
}
func limit(value string, n int) string {
	if len(value) > n {
		return value[:n]
	}
	return value
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func replyFailure(w http.ResponseWriter, err error) {
	code := codeOf(err)
	status := http.StatusBadRequest
	if code == "runtime_state_conflict" {
		status = http.StatusConflict
	}
	reply(w, status, map[string]string{"status": "error", "error_code": code})
}
