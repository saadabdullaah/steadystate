package platformctl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const portalAPIVersion = "portal.steadystate.dev/v1alpha1"
const portalVersion = "v1.0.0"

//go:embed portalassets/*
var portalAssets embed.FS

type portalPlan struct {
	Request ChangeRequest
	Digest  string
	Diff    string
	Files   []FileChange
	Expires time.Time
	Used    bool
}

type portalBreakPlan struct {
	Namespace       string
	Application     string
	Action          string
	UID             string
	ResourceVersion string
	Before          map[string]any
	Expires         time.Time
	Used            bool
}

type portalEvent struct {
	ID   uint64    `json:"id"`
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Data any       `json:"data,omitempty"`
}

type portalServer struct {
	options      *Options
	selected     Context
	contextName  string
	launchToken  string
	launchUntil  time.Time
	session      string
	sessionUntil time.Time
	csrf         string
	assetDigest  string

	mu          sync.Mutex
	plans       map[string]*portalPlan
	breakPlans  map[string]*portalBreakPlan
	events      []portalEvent
	nextEventID uint64
	listeners   map[chan portalEvent]struct{}
	writeTimes  []time.Time
}

type portalReady struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Version    string `json:"version" yaml:"version"`
	Assets     string `json:"assetsDigest" yaml:"assetsDigest"`
	Context    string `json:"context" yaml:"context"`
	Address    string `json:"address" yaml:"address"`
	URL        string `json:"url" yaml:"url"`
	State      string `json:"state" yaml:"state"`
}

func newPortalCommand(options *Options) *cobra.Command {
	var port int
	var noOpen bool
	command := &cobra.Command{
		Use: "portal", Short: "Run the loopback-only SteadyState developer portal", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if port < 0 || port > 65535 {
				return exitError(ExitUsage, "portal port must be between 0 and 65535")
			}
			config, selected, err := options.loadContext()
			if err != nil {
				return err
			}
			contextName := options.ContextName
			if contextName == "" {
				contextName = config.CurrentContext
			}
			listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				return exitError(ExitConflict, "portal loopback port is unavailable: %v", err)
			}
			defer listener.Close()
			server, err := newPortalServer(options, selected, contextName)
			if err != nil {
				return err
			}
			address := listener.Addr().String()
			launchURL := "http://" + address + "/launch/" + server.launchToken
			cleanURL := "http://" + address + "/"
			ready := portalReady{APIVersion: portalAPIVersion, Version: portalVersion, Assets: server.assetDigest, Context: contextName, Address: address, URL: cleanURL, State: "Ready"}
			if err := options.printer().Table([]string{"PORTAL", "CONTEXT", "STATE"}, [][]string{{cleanURL, contextName, "Ready"}}, ready); err != nil {
				return err
			}
			if !noOpen {
				if err := openBrowser(launchURL); err != nil {
					_, _ = fmt.Fprintf(options.Stderr, "Browser launch failed; open %s manually: %s\n", launchURL, ErrorMessage(err))
				}
			} else if !options.Quiet {
				_, _ = fmt.Fprintf(options.Stderr, "Open %s\n", launchURL)
			}
			httpServer := &http.Server{Handler: server.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
			errCh := make(chan error, 1)
			go func() { errCh <- httpServer.Serve(listener) }()
			select {
			case <-cmd.Context().Done():
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = httpServer.Shutdown(ctx)
				return nil
			case err := <-errCh:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return exitError(ExitRemote, "portal server stopped: %v", err)
			}
		},
	}
	command.Flags().IntVar(&port, "port", 0, "fixed loopback port (zero selects an available port)")
	command.Flags().BoolVar(&noOpen, "no-open", false, "do not open the system browser")
	return command
}

func newPortalServer(options *Options, selected Context, contextName string) (*portalServer, error) {
	launch, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	session, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return &portalServer{options: options, selected: selected, contextName: contextName, launchToken: launch, launchUntil: now.Add(time.Minute), session: session, sessionUntil: now.Add(8 * time.Hour), csrf: csrf, assetDigest: portalAssetsDigest(), plans: map[string]*portalPlan{}, breakPlans: map[string]*portalBreakPlan{}, listeners: map[chan portalEvent]struct{}{}}, nil
}

func randomToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate secure portal token: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func portalAssetsDigest() string {
	hash := sha256.New()
	entries, _ := fs.Glob(portalAssets, "portalassets/*")
	for _, name := range entries {
		data, _ := portalAssets.ReadFile(name)
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write(data)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func (s *portalServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/launch/", s.handleLaunch)
	mux.HandleFunc("/api/v1/events", s.auth(s.handleEvents))
	mux.HandleFunc("/api/v1/", s.auth(s.handleAPI))
	assets, _ := fs.Sub(portalAssets, "portalassets")
	fileServer := http.FileServer(http.FS(assets))
	mux.Handle("/", s.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", "Use the documented portal route.")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(assets, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/index.html"
		fileServer.ServeHTTP(w, r)
	}))
	return s.securityHeaders(mux)
}

func (s *portalServer) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" {
			s.writeError(w, r, http.StatusForbidden, "invalid_host", "portal accepts loopback hosts only", "Open the exact URL printed by platformctl portal.")
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *portalServer) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "launch accepts GET only", "Open the launch URL printed by platformctl.")
		return
	}
	s.mu.Lock()
	valid := time.Now().Before(s.launchUntil) && strings.TrimPrefix(r.URL.Path, "/launch/") == s.launchToken && s.launchToken != ""
	if valid {
		s.launchToken = ""
	}
	s.mu.Unlock()
	if !valid {
		s.writeError(w, r, http.StatusUnauthorized, "launch_expired", "launch link is invalid or expired", "Restart platformctl portal to create a new one-time link.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "steadystate_portal_session", Value: s.session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int((8 * time.Hour).Seconds())})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *portalServer) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("steadystate_portal_session")
		if err != nil || cookie.Value != s.session || time.Now().After(s.sessionUntil) {
			s.writeError(w, r, http.StatusUnauthorized, "session_required", "portal session is missing or expired", "Restart platformctl portal and use its one-time launch URL.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			expectedOrigin := "http://" + r.Host
			if r.Header.Get("Origin") != expectedOrigin || r.Header.Get("X-SteadyState-CSRF") != s.csrf {
				s.writeError(w, r, http.StatusForbidden, "csrf_rejected", "request origin or CSRF token is invalid", "Refresh the portal and retry.")
				return
			}
			if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
				s.writeError(w, r, http.StatusUnsupportedMediaType, "json_required", "state changes require application/json", "Submit the action through the portal.")
				return
			}
			if !s.allowWrite() {
				s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many state-changing requests", "Wait one minute and retry.")
				return
			}
		}
		next(w, r)
	}
}

func (s *portalServer) allowWrite() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	kept := s.writeTimes[:0]
	for _, item := range s.writeTimes {
		if item.After(cutoff) {
			kept = append(kept, item)
		}
	}
	s.writeTimes = kept
	if len(s.writeTimes) >= 10 {
		return false
	}
	s.writeTimes = append(s.writeTimes, time.Now())
	return true
}

func (s *portalServer) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *portalServer) writeError(w http.ResponseWriter, r *http.Request, status int, code, message, remediation string) {
	requestID, _ := randomToken(8)
	s.writeJSON(w, status, map[string]any{"apiVersion": portalAPIVersion, "error": map[string]string{"code": code, "message": Redact(message), "remediation": remediation, "requestID": requestID}, "path": r.URL.Path})
}

func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
}
