package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"searchterm/internal/config"
	"searchterm/internal/magnetlog"
	"searchterm/internal/model"
	"searchterm/internal/search"
	"searchterm/internal/store"
	"searchterm/internal/tgbot"
	"searchterm/web"
)

const sessionCookie = "searchterm_session"

type Server struct {
	cfg       config.Config
	st        *store.Store
	search    *search.Service
	tg        *tgbot.Manager
	magnetLog *magnetlog.Logger
	sessMu    sync.Mutex
	session   map[string]time.Time
	mux       *http.ServeMux
}

func New(cfg config.Config, st *store.Store, svc *search.Service, tg *tgbot.Manager, magnetLog *magnetlog.Logger) *Server {
	return &Server{
		cfg:       cfg,
		st:        st,
		search:    svc,
		tg:        tg,
		magnetLog: magnetLog,
		session:   make(map[string]time.Time),
		mux:       http.NewServeMux(),
	}
}

func (s *Server) Handler() http.Handler {
	s.routes()
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/session", s.handleSession)
	s.mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/setup/account", s.handleSetupAccount)
	s.mux.HandleFunc("GET /api/public/settings", s.handlePublicSettings)
	s.mux.HandleFunc("GET /api/search", s.withAuth(s.handleSearch))
	s.mux.HandleFunc("GET /api/search/stream", s.withAuth(s.handleSearchStream))
	s.mux.HandleFunc("POST /api/search/save115", s.withAuth(s.handleSaveTo115))
	s.mux.HandleFunc("POST /api/search/save115_batch", s.withAuth(s.handleSaveTo115Batch))
	s.mux.HandleFunc("GET /api/network/test", s.withAuth(s.handleNetworkTest))

	s.mux.HandleFunc("GET /api/admin/settings", s.withAuth(s.handleGetSettings))
	s.mux.HandleFunc("PUT /api/admin/settings", s.withAuth(s.handleUpdateSettings))
	s.mux.HandleFunc("GET /api/admin/account", s.withAuth(s.handleGetAccount))
	s.mux.HandleFunc("PUT /api/admin/account", s.withAuth(s.handlePutAccount))
	s.mux.HandleFunc("GET /api/admin/sites", s.withAuth(s.handleListSites))
	s.mux.HandleFunc("POST /api/admin/sites", s.withAuth(s.handleCreateSite))
	s.mux.HandleFunc("PUT /api/admin/sites/{id}", s.withAuth(s.handleUpdateSite))
	s.mux.HandleFunc("DELETE /api/admin/sites/{id}", s.withAuth(s.handleDeleteSite))
	s.mux.HandleFunc("GET /api/admin/tg/users", s.withAuth(s.handleListTGUsers))
	s.mux.HandleFunc("POST /api/admin/tg/users", s.withAuth(s.handleCreateTGUser))
	s.mux.HandleFunc("PUT /api/admin/tg/users", s.withAuth(s.handleReplaceTGUsers))
	s.mux.HandleFunc("DELETE /api/admin/tg/users/{id}", s.withAuth(s.handleDeleteTGUser))
	s.mux.HandleFunc("GET /api/admin/tg/bots", s.withAuth(s.handleListTGBots))
	s.mux.HandleFunc("POST /api/admin/tg/bots", s.withAuth(s.handleCreateTGBot))
	s.mux.HandleFunc("PUT /api/admin/tg/bots/{id}", s.withAuth(s.handleUpdateTGBot))
	s.mux.HandleFunc("DELETE /api/admin/tg/bots/{id}", s.withAuth(s.handleDeleteTGBot))
	s.mux.HandleFunc("GET /api/admin/p115", s.withAuth(s.handleGetP115))
	s.mux.HandleFunc("PUT /api/admin/p115", s.withAuth(s.handlePutP115))
	s.mux.HandleFunc("POST /api/admin/p115/check", s.withAuth(s.handleCheckP115))
	s.mux.HandleFunc("GET /api/admin/p115/dirs", s.withAuth(s.handleListP115Dirs))
	s.mux.HandleFunc("GET /api/admin/p115/tasks", s.withAuth(s.handleListP115Tasks))
	s.mux.HandleFunc("GET /api/admin/p115/qrcode/token", s.withAuth(s.handleQrcodeToken))
	s.mux.HandleFunc("GET /api/admin/p115/qrcode/status", s.withAuth(s.handleQrcodeStatus))
	s.mux.HandleFunc("GET /api/admin/p115/qrcode/result", s.withAuth(s.handleQrcodeResult))

	sub, err := fs.Sub(web.FS, "static")
	if err != nil {
		log.Fatalf("embed static: %v", err)
	}
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
}

// -------- auth & sessions --------

func (s *Server) checkPassword(user, pass string) bool {
	settings, _ := s.st.GetSettings()
	if settings.AdminUser != "" && settings.AdminPasswordHash != "" {
		if user != settings.AdminUser {
			return false
		}
		return bcrypt.CompareHashAndPassword([]byte(settings.AdminPasswordHash), []byte(pass)) == nil
	}
	if user != s.cfg.AdminUser {
		return false
	}
	if s.cfg.AdminPasswordHash != "" {
		return bcrypt.CompareHashAndPassword([]byte(s.cfg.AdminPasswordHash), []byte(pass)) == nil
	}
	return pass != "" && s.cfg.AdminPassword != "" && pass == s.cfg.AdminPassword
}

func (s *Server) newSession() (string, time.Time) {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)
	exp := time.Now().Add(s.cfg.SessionTTL())
	s.sessMu.Lock()
	s.session[token] = exp
	s.sessMu.Unlock()
	return token, exp
}

func (s *Server) validSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	exp, ok := s.session[c.Value]
	if !ok || time.Now().After(exp) {
		delete(s.session, c.Value)
		return false
	}
	s.session[c.Value] = time.Now().Add(s.cfg.SessionTTL())
	return true
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.validSession(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	if !s.checkPassword(req.Username, req.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "用户名或密码错误"})
		return
	}
	token, exp := s.newSession()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessMu.Lock()
		delete(s.session, c.Value)
		s.sessMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authed": s.validSession(r)})
}

func (s *Server) setupConfigured() bool {
	settings, _ := s.st.GetSettings()
	return settings.AdminUser != "" && settings.AdminPasswordHash != ""
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"configured": s.setupConfigured()})
}

func (s *Server) handleSetupAccount(w http.ResponseWriter, r *http.Request) {
	if s.setupConfigured() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "系统已初始化"})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "用户名不能为空"})
		return
	}
	if len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "密码至少 6 位"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	settings, _ := s.st.GetSettings()
	settings.AdminUser = username
	settings.AdminPasswordHash = string(hash)
	if err := s.st.SaveSettings(settings); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePublicSettings(w http.ResponseWriter, r *http.Request) {
	settings, _ := s.st.GetSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"background_image_url": settings.BackgroundImageURL,
	})
}

// -------- search --------

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if keyword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少搜索词"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.SearchTimeout())
	defer cancel()
	start := time.Now()
	items, err := s.search.Search(ctx, keyword)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "error": err.Error(), "elapsed_ms": time.Since(start).Milliseconds()})
		return
	}
	s.logSearchResults(keyword, items)
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"elapsed_ms": time.Since(start).Milliseconds(),
	})
}

// -------- helpers --------

func (s *Server) handleSearchStream(w http.ResponseWriter, r *http.Request) {
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if keyword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少搜索词"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.SearchTimeout())
	defer cancel()
	start := time.Now()
	var loggedItems []model.SearchItem
	if err := s.search.SearchStream(ctx, keyword, func(ev search.StreamEvent) error {
		if ev.Type == "done" || ev.Type == "cached" {
			ev.ElapsedMS = time.Since(start).Milliseconds()
			loggedItems = ev.Items
		}
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}); err != nil {
		log.Printf("[search] stream %s: %v", keyword, err)
	}
	if loggedItems != nil {
		s.logSearchResults(keyword, loggedItems)
	}
}

func (s *Server) logSearchResults(keyword string, items []model.SearchItem) {
	if s.magnetLog == nil {
		return
	}
	records := make([]magnetlog.Record, 0, len(items))
	now := time.Now().Format(time.RFC3339Nano)
	for _, it := range items {
		if it.Magnet == "" && it.InfoHash == "" {
			continue
		}
		records = append(records, magnetlog.Record{
			Time:     now,
			Keyword:  keyword,
			Title:    it.Title,
			Site:     it.Site,
			InfoHash: it.InfoHash,
			Magnet:   it.Magnet,
			Size:     it.Size,
		})
	}
	if err := s.magnetLog.Append(records); err != nil {
		log.Printf("[magnetlog] append failed: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	return nil
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return prefix + hex.EncodeToString(buf)
}

var errNotFound = errors.New("not found")
