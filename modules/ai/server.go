package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"github.com/tmc/langchaingo/llms"
)

// server.go lets tyde run a local model server for the user. When Local AI is
// selected and the kronk binary is installed, tyde starts "kronk server" itself
// if nothing is already listening, and stops it when the assistant is
// deactivated - so a beginner with kronk installed gets a working local AI with
// no server to set up or babysit. Without kronk the user runs their own server
// (e.g. ollama) and the settings panel helps them point at it.

// kronkBinaryName is the model-server binary tyde can manage on the user's behalf.
const kronkBinaryName = "kronk"

// kronkAvailable reports whether the kronk binary is on PATH.
func kronkAvailable() bool {
	_, err := exec.LookPath(kronkBinaryName)
	return err == nil
}

// kronkManaged reports whether tyde should run the local server itself: kronk
// is installed and the user has left "Automatically manage with Kronk" on.
func kronkManaged() bool {
	if !kronkAvailable() {
		return false
	}
	return fyne.CurrentApp().Preferences().BoolWithFallback(prefLocalManaged, true)
}

// endpointHost extracts host:port from an endpoint URL
// ("http://localhost:11435/v1" -> "localhost:11435"), or "" if it can't parse.
func endpointHost(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	return u.Host
}

// endpointReachable reports whether a server is already listening at the
// endpoint's host:port. A quick TCP dial avoids a full HTTP round-trip.
func endpointReachable(endpoint string) bool {
	host := endpointHost(endpoint)
	if host == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", host, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// serverManager owns the single kronk server process tyde may start. All access
// is serialised; the process is put in its own group so it (and any children)
// can be torn down as a unit.
type serverManager struct {
	mu  sync.Mutex
	cmd *exec.Cmd // the server we started, or nil
}

var serverMgr serverManager

// ensure starts the managed kronk server if it makes sense to: kronk is
// installed and nothing already answers on its (fixed, known) port. There is no
// URL to configure - tyde owns the server, so it owns the address. Returns
// immediately; the probe and spawn run on a goroutine so callers (module load,
// switching to Local) never block.
func (m *serverManager) ensure() {
	go m.ensureSync()
}

func (m *serverManager) ensureSync() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil {
		return // we already run one
	}
	if !kronkManaged() {
		return // nothing to launch; the user manages their own server
	}
	if endpointReachable(kronkEndpoint) {
		return // a kronk server is already running - reuse it
	}

	host := endpointHost(kronkEndpoint)

	// Left in tyde's process group (no Setpgid) so that if the whole session is
	// torn down the server goes too. kronk runs inference in-process, so there
	// are no child processes to sweep up separately.
	cmd := exec.Command(kronkBinaryName, "server", "start", "--api-host", host)

	// Capture the server's output so a startup or inference failure - which can
	// take the whole process down (e.g. an incompatible llama.cpp library, or a
	// model it can't load) - is diagnosable rather than silent.
	logFile, _ := os.Create(kronkLogPath())
	if logFile != nil {
		cmd.Stdout, cmd.Stderr = logFile, logFile
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return
	}
	m.cmd = cmd

	// Reap the process when it exits so it never lingers as a zombie, and drop
	// our handle if it dies on its own.
	go func() {
		_ = cmd.Wait()
		if logFile != nil {
			_ = logFile.Close()
		}
		m.mu.Lock()
		if m.cmd == cmd {
			m.cmd = nil
		}
		m.mu.Unlock()
	}()
}

// kronkLogPath is where the managed server's stdout/stderr is written, so a
// crash can be inspected (it is otherwise a background process logging nowhere).
func kronkLogPath() string {
	return cachePath("kronk.log")
}

// aiPanicLogPath is where recovered panics from the AI module are appended.
func aiPanicLogPath() string {
	return cachePath("ai-panic.log")
}

// cachePath returns a path under tyde's own cache directory
// (<user cache>/fyne/com.fyshos.tyde/ - the same place tyde writes its logs),
// creating the directory if needed.
func cachePath(name string) string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "fyne", "com.fyshos.tyde")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, name)
}

// recoverAI turns a panic into a logged, survivable event so the AI module -
// which runs inside the desktop process (the panel under Wayland) - can never
// take the whole desktop down. The trace goes to the app log and to
// aiPanicLogPath so it can be read back after the fact. Use as the first line
// of a goroutine or handler:
//
//	defer recoverAI("chat stream render")
func recoverAI(where string) {
	r := recover()
	if r == nil {
		return
	}

	trace := fmt.Sprintf("%s: %v\n%s", where, r, debug.Stack())
	fyne.LogError("AI module recovered a panic", fmt.Errorf("%s", trace))

	if f, err := os.OpenFile(aiPanicLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		_, _ = f.WriteString(trace + "\n\n")
		_ = f.Close()
	}
}

// stop ends the managed server if tyde started one. A no-op when the user runs
// their own server. Called when the assistant module is deactivated.
func (m *serverManager) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.cmd.Process == nil {
		return
	}
	// The child shares tyde's process group (no Setpgid), so signal it directly
	// by pid - a group signal (negative pid) would target tyde itself too.
	_ = m.cmd.Process.Signal(syscall.SIGTERM)
	m.cmd = nil
}

// injectDoer wraps an HTTP client to add extra top-level fields to the outgoing
// JSON chat request. langchaingo's OpenAI request struct is fixed, so this is
// how we pass server-specific params it doesn't know about - notably
// "enable_thinking": false, so a reasoning model (kronk defaults thinking ON)
// answers directly instead of running a slow hidden think pass before every
// reply. Existing fields are never overwritten. Used only for Local AI.
type injectDoer struct {
	fields map[string]any
	next   *http.Client
}

func (d injectDoer) Do(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return d.next.Do(req)
	}

	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}

	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		for k, v := range d.fields {
			if _, exists := m[k]; !exists {
				m[k] = v
			}
		}
		if nb, err := json.Marshal(m); err == nil {
			body = nb
		}
	}

	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return d.next.Do(req)
}

// localModel wraps the OpenAI-compatible client used for Local AI so a bare
// transport failure (the server isn't up, or crashed mid-request) surfaces as
// an actionable hint instead of a cryptic `Post "...": EOF`.
type localModel struct {
	llms.Model
}

func (m localModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	resp, err := m.Model.GenerateContent(ctx, messages, options...)
	return resp, localError(err)
}

// localError rewrites a connection-level failure from a local server into a
// message that points at the likely cause and next step, and leaves genuine
// model/API errors untouched.
func localError(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	transport := strings.Contains(s, "EOF") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "no such host")
	if !transport {
		return err
	}

	if kronkManaged() {
		// The most common cause we've seen: kronk segfaults loading a
		// multimodal (vision) model's projection, taking the server down.
		return fmt.Errorf("the local AI server isn't responding - it may still be starting, or it crashed "+
			"loading the model. Vision/multimodal models can crash kronk; try a text-only model. See %s for details",
			kronkLogPath())
	}
	return fmt.Errorf("couldn't reach the local AI server - is it running at the Base URL in AI settings? (%w)", err)
}

// probeResult is the outcome of a settings "Test connection" check.
type probeResult struct {
	models []string // model ids the server offers, when reachable
	err    error    // non-nil if the server could not be reached or read
}

// has reports whether the server offers the given model, returning the id the
// server itself uses for it.
func (r probeResult) has(model string) (string, bool) {
	want := normalModel(model)
	if want == "" {
		return "", false
	}
	for _, m := range r.models {
		if m == model || normalModel(m) == want {
			return m, true
		}
	}
	return "", false
}

// normalModel reduces a model id to the part different servers agree on: lower
// case, no "org/" prefix, no ".gguf" suffix, no implicit ":latest" tag.
func normalModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	m = strings.TrimSuffix(m, ".gguf")
	return strings.TrimSuffix(m, ":latest")
}

// probeEndpoint asks a local OpenAI-compatible server for its model list (GET
// <endpoint>/models). It backs the settings "Test connection" button, so it
// both confirms the server is up and lets us check the chosen model is present.
func probeEndpoint(ctx context.Context, endpoint string) probeResult {
	target := strings.TrimSuffix(strings.TrimSpace(endpoint), "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return probeResult{err: err}
	}
	req.Header.Set("Authorization", "Bearer local") // ignored by local servers

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return probeResult{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return probeResult{err: fmt.Errorf("server responded %s", resp.Status)}
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return probeResult{err: fmt.Errorf("unexpected response from server: %w", err)}
	}

	models := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		models = append(models, m.ID)
	}
	return probeResult{models: models}
}
