// Package browserserve runs a localhost HTTP listener that receives browser
// (claude.ai) events from the companion Chrome extension and routes them
// through the notification pipeline. It is started as a background LaunchAgent
// on macOS; see cmd install-browser-listener.
package browserserve

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/777genius/claude-notifications/internal/analyzer"
	"github.com/777genius/claude-notifications/internal/config"
	"github.com/777genius/claude-notifications/internal/logging"
	"github.com/777genius/claude-notifications/internal/notifier"
)

// DefaultPort is the fixed loopback port the extension posts to.
const DefaultPort = 52741

// Event is the JSON payload the Chrome extension POSTs to /event.
type Event struct {
	ConversationID string `json:"conversationId"`
	Title          string `json:"title"`
	LastMessage    string `json:"lastMessage"`
	URL            string `json:"url"`
	// Status optionally overrides classification (e.g. "question"); when empty
	// the server classifies from LastMessage.
	Status string `json:"status"`
}

// Server holds runtime state for the listener.
type Server struct {
	cfg      *config.Config
	notifier *notifier.Notifier
	token    string

	mu       sync.Mutex
	lastSeen map[string]string // conversationId → last notified message (dedupe)
}

// TokenPath returns the file storing the shared auth token. Created on install;
// the extension reads the same value (the user pastes it into the extension).
func TokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "browser-listener-token")
}

// LoadToken reads the shared token, or "" if missing.
func LoadToken() string {
	b, err := os.ReadFile(TokenPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// New builds a Server. token must be non-empty; requests without it are rejected.
func New(cfg *config.Config, token string) *Server {
	return &Server{
		cfg:      cfg,
		notifier: notifier.New(cfg),
		token:    token,
		lastSeen: map[string]string{},
	}
}

// ListenAndServe binds 127.0.0.1:port and serves until the process exits.
func (s *Server) ListenAndServe(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/event", s.handleEvent)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot bind %s (already running?): %w", addr, err)
	}
	logging.Info("Browser listener started on %s", addr)
	srv := &http.Server{
		Handler:           s.withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return srv.Serve(ln)
}

// withCORS allows the claude.ai extension origin to call the listener.
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The extension's fetch sends an Origin; allow only chrome-extension://
		// and claude.ai. Loopback binding already restricts to this machine.
		origin := r.Header.Get("Origin")
		if strings.HasPrefix(origin, "chrome-extension://") || strings.HasSuffix(origin, "claude.ai") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Auth-Token")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			// Chrome Private Network Access: extension→loopback preflights ask
			// for this; without it the fetch is silently blocked.
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
		logging.Debug("listener request: %s %s origin=%q auth=%v", r.Method, r.URL.Path, origin, r.Header.Get("X-Auth-Token") != "")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Constant-time token check.
	got := r.Header.Get("X-Auth-Token")
	if len(got) == 0 || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var ev Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if ev.ConversationID == "" {
		http.Error(w, "missing conversationId", http.StatusBadRequest)
		return
	}

	// Dedupe: ignore repeats of the same final message for a conversation
	// (the extension may fire more than once per turn). Empty texts never
	// dedupe — they carry no identity (content script not injected), and two
	// distinct turns would otherwise swallow each other.
	s.mu.Lock()
	if prev, ok := s.lastSeen[ev.ConversationID]; ok && ev.LastMessage != "" && prev == ev.LastMessage {
		s.mu.Unlock()
		logging.Debug("browser event deduped: conversation=%s", ev.ConversationID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"deduped":true}`))
		return
	}
	s.lastSeen[ev.ConversationID] = ev.LastMessage
	s.mu.Unlock()

	status := analyzer.Status(ev.Status)
	if status == "" {
		status = analyzer.ClassifyFinalMessage(ev.LastMessage)
	}
	// A completion with no readable message text (content script not injected
	// in that tab yet) is still a completed turn.
	if status == analyzer.StatusUnknown {
		status = analyzer.StatusTaskComplete
	}

	// The notification itself is rendered by the extension (chrome.notifications)
	// so the click is handled inside the SAME browser/profile that ran the
	// session — a macOS-level "open URL" would hit the default browser, which
	// may be a different browser or Claude account entirely. The Mac side
	// contributes the sound and the classified title/body.
	title, body := s.notifier.BrowserNotificationContent(status, ev.Title, ev.LastMessage)
	s.notifier.PlayStatusSound(status)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]any{"ok": true, "notify": true, "title": title, "message": body}
	_ = json.NewEncoder(w).Encode(resp)
}
