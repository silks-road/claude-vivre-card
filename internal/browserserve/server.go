// Package browserserve runs a localhost HTTP listener that receives browser
// (claude.ai) events from the companion Chrome extension and routes them
// through the notification pipeline. It is started as a background LaunchAgent
// on macOS; see cmd install-browser-listener.
package browserserve

import (
	"bytes"
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

	// focusCh carries conversation ids from notification clicks to the
	// extension's long-poll (/wait-focus), so the browser that ran the
	// session focuses its own tab.
	focusCh chan string
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
		focusCh:  make(chan string, 8),
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
	mux.HandleFunc("/focus", s.handleFocus)
	mux.HandleFunc("/wait-focus", s.handleWaitFocus)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot bind %s (already running?): %w", addr, err)
	}
	logging.Info("Browser listener started on %s", addr)

	// Cover Cowork/Home tasks the hook path can't see by tailing the app log.
	go s.WatchCoworkLog()
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

	// The banner is posted via ClaudeNotifier — the identity the user already
	// allows through macOS Focus — NOT via the browser (whose notifications
	// Focus rightly filters). The click runs request-browser-focus, which
	// feeds /wait-focus: the extension long-polling it focuses the exact tab
	// in ITS OWN browser, so clicks never land in the default browser.
	if err := s.notifier.SendBrowserNotificationWithClick(status, ev.Title, ev.LastMessage, ev.ConversationID); err != nil {
		logging.Warn("browser notification failed: %v", err)
		http.Error(w, "notify failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleFocus receives a conversation id from a notification click (posted by
// the request-browser-focus subcommand) and queues it for the extension.
func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	got := r.Header.Get("X-Auth-Token")
	if len(got) == 0 || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		ConversationID string `json:"conversationId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ConversationID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	select {
	case s.focusCh <- body.ConversationID:
	default: // queue full: drop oldest, keep newest
		select {
		case <-s.focusCh:
		default:
		}
		s.focusCh <- body.ConversationID
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleWaitFocus long-polls until a notification click requests a tab focus
// (or ~25s pass). The extension re-polls immediately after each response.
func (s *Server) handleWaitFocus(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get("X-Auth-Token")
	if len(got) == 0 || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	select {
	case id := <-s.focusCh:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"conversationId": id})
	case <-time.After(25 * time.Second):
		w.WriteHeader(http.StatusNoContent)
	case <-r.Context().Done():
	}
}

// RequestFocus posts a conversation id to the running listener's /focus
// endpoint. Called by the request-browser-focus subcommand from a
// notification click.
func RequestFocus(conversationID string) error {
	token := LoadToken()
	if token == "" {
		return fmt.Errorf("no browser-listener token")
	}
	body, _ := json.Marshal(map[string]string{"conversationId": conversationID})
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/focus", DefaultPort), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", token)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("focus request returned %d", resp.StatusCode)
	}
	return nil
}
