package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"searchterm/internal/gying"
	"searchterm/internal/model"
	"searchterm/internal/nyaa"
	"searchterm/internal/p115"
	"searchterm/internal/qiwei"
	"searchterm/internal/tgbot"
)

type siteRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	Cookie   string `json:"cookie"`
	Enabled  *bool  `json:"enabled"`
	Type     string `json:"type"`
}

func normalizeSiteType(t string) string {
	switch t {
	case "gying", "nyaa", "sukebei", "qiwei":
		return t
	case "":
		return "gying"
	default:
		return ""
	}
}

func defaultSiteBaseURLs(t string) []string {
	switch t {
	case "nyaa":
		return nyaa.DefaultBaseURLs
	case "sukebei":
		return nyaa.DefaultSukebeiBaseURLs
	case "qiwei":
		return qiwei.DefaultBaseURLs
	default:
		return gying.DefaultBaseURLs
	}
}

func defaultSiteEnabled(t string) bool {
	return t == "qiwei" || t == "nyaa"
}

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := s.st.ListSites()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	filtered := make([]model.Site, 0, len(sites))
	for _, site := range sites {
		if site.Type == "bt1207" {
			continue
		}
		if site.Type == "gying" {
			if password, err := s.st.Decrypt(site.PasswordEnc); err == nil {
				site.Password = password
			}
		}
		site.PasswordEnc = ""
		site.CookieEnc = ""
		filtered = append(filtered, site)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": filtered})
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var req siteRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	siteType := normalizeSiteType(req.Type)
	if siteType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "不支持的站点类型"})
		return
	}
	if siteType == "gying" && (req.Username == "" || req.Password == "") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "账号和密码不能为空"})
		return
	}
	enabled := defaultSiteEnabled(siteType)
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := time.Now()
	name := "观影"
	if siteType == "nyaa" {
		name = "Nyaa"
	}
	if siteType == "sukebei" {
		name = "Sukebei"
	}
	if siteType == "qiwei" {
		name = "七味"
	}
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	site := model.Site{
		ID:        newID("site_"),
		Type:      siteType,
		Name:      name,
		BaseURLs:  defaultSiteBaseURLs(siteType),
		Username:  req.Username,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if siteType == "gying" {
		enc, err := s.st.Encrypt(req.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		site.PasswordEnc = enc
	}
	if err := s.st.SaveSite(site); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.registerSiteProvider(site)
	site.Password = ""
	site.PasswordEnc = ""
	site.CookieEnc = ""
	writeJSON(w, http.StatusOK, map[string]any{"site": site})
}

func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok, err := s.st.GetSite(id)
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "站点不存在"})
		return
	}
	var req siteRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.Type != "" {
		siteType := normalizeSiteType(req.Type)
		if siteType == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "不支持的站点类型"})
			return
		}
		existing.Type = siteType
	}
	if existing.Type == "" {
		existing.Type = "gying"
	}
	existing.BaseURLs = defaultSiteBaseURLs(existing.Type)
	if strings.TrimSpace(req.Name) != "" {
		existing.Name = strings.TrimSpace(req.Name)
	}
	if req.Username != "" {
		existing.Username = req.Username
	}
	if req.Password != "" {
		enc, err := s.st.Encrypt(req.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		existing.PasswordEnc = enc
	}
	existing.CookieEnc = ""
	if existing.Type == "nyaa" || existing.Type == "sukebei" {
		existing.PasswordEnc = ""
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.UpdatedAt = time.Now()
	if err := s.st.SaveSite(existing); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.registerSiteProvider(existing)
	existing.Password = ""
	existing.PasswordEnc = ""
	existing.CookieEnc = ""
	writeJSON(w, http.StatusOK, map[string]any{"site": existing})
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.st.DeleteSite(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.search.Remove(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) registerSiteProvider(site model.Site) {
	if !site.Enabled {
		s.search.Remove(site.ID)
		return
	}
	switch normalizeSiteType(site.Type) {
	case "nyaa":
		name := site.Name
		if name == "" {
			name = "Nyaa"
		}
		s.search.Register(site.ID, name, nyaa.New(site.BaseURLs))
	case "sukebei":
		name := site.Name
		if name == "" {
			name = "Sukebei"
		}
		s.search.Register(site.ID, name, nyaa.NewAdult(site.BaseURLs))
	case "gying":
		password, err := s.st.Decrypt(site.PasswordEnc)
		if err != nil || password == "" {
			s.search.Remove(site.ID)
			return
		}
		client, err := gying.New(site.Username, password, site.BaseURLs)
		if err != nil {
			s.search.Remove(site.ID)
			return
		}
		s.search.Register(site.ID, site.Name, client)
	case "qiwei":
		name := site.Name
		if name == "" {
			name = "七味"
		}
		s.search.Register(site.ID, name, qiwei.New(site.BaseURLs))
	default:
		s.search.Remove(site.ID)
	}
}

// RegisterSite wires a stored site into the search service at startup.
func (s *Server) RegisterSite(site model.Site) {
	s.registerSiteProvider(site)
}

// EnsureDefaultSites applies the v2 site defaults once: it renames the legacy
// 观影站 record to 观影, enables Nyaa, and creates 七味 when missing. User
// toggles made later are never overwritten.
func (s *Server) EnsureDefaultSites() error {
	settings, _ := s.st.GetSettings()
	if settings.DefaultsV2Applied {
		return nil
	}
	sites, err := s.st.ListSites()
	if err != nil {
		return err
	}
	byType := make(map[string]model.Site, len(sites))
	for _, site := range sites {
		byType[site.Type] = site
	}
	now := time.Now()
	upsert := func(site model.Site) (model.Site, error) {
		if err := s.st.SaveSite(site); err != nil {
			return model.Site{}, err
		}
		s.registerSiteProvider(site)
		return site, nil
	}
	if site, ok := byType["gying"]; ok && site.Name == "观影站" {
		site.Name = "观影"
		site.UpdatedAt = now
		if _, err := upsert(site); err != nil {
			return err
		}
	}
	if site, ok := byType["nyaa"]; ok {
		if !site.Enabled {
			site.Enabled = true
			site.UpdatedAt = now
			if _, err := upsert(site); err != nil {
				return err
			}
		}
	} else {
		if _, err := upsert(model.Site{
			ID:        newID("site_"),
			Type:      "nyaa",
			Name:      "Nyaa",
			BaseURLs:  defaultSiteBaseURLs("nyaa"),
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return err
		}
	}
	if _, ok := byType["qiwei"]; !ok {
		if _, err := upsert(model.Site{
			ID:        newID("site_"),
			Type:      "qiwei",
			Name:      "七味",
			BaseURLs:  defaultSiteBaseURLs("qiwei"),
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return err
		}
	}
	settings.DefaultsV2Applied = true
	return s.st.SaveSettings(settings)
}

// -------- settings --------

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, _ := s.st.GetSettings()
	settings.P115Cookie = maskSecret(settings.P115Cookie)
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackgroundImageURL *string `json:"background_image_url"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	settings, _ := s.st.GetSettings()
	if req.BackgroundImageURL != nil {
		v := strings.TrimSpace(*req.BackgroundImageURL)
		if v != "" && !validBackgroundURL(v) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "背景图 URL 无效"})
			return
		}
		settings.BackgroundImageURL = v
	}
	if err := s.st.SaveSettings(settings); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.search.ClearCache()
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func validBackgroundURL(v string) bool {
	if strings.HasPrefix(v, "data:image/") {
		return true
	}
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// -------- account --------

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	settings, _ := s.st.GetSettings()
	username := settings.AdminUser
	if username == "" {
		username = s.cfg.AdminUser
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username": username,
		"avatar":   settings.Avatar,
	})
}

func (s *Server) handlePutAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
		Avatar      string `json:"avatar"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "用户名不能为空"})
		return
	}
	if len(req.Avatar) > 2*1024*1024 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "头像图片过大"})
		return
	}
	if req.Avatar != "" && !strings.HasPrefix(req.Avatar, "data:image/") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "头像格式无效"})
		return
	}
	settings, _ := s.st.GetSettings()
	currentUser := settings.AdminUser
	if currentUser == "" {
		currentUser = s.cfg.AdminUser
	}
	if req.NewPassword != "" {
		if req.OldPassword == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "修改密码需要输入当前密码"})
			return
		}
		if !s.checkPassword(currentUser, req.OldPassword) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "当前密码错误"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		settings.AdminPasswordHash = string(hash)
	}
	settings.AdminUser = username
	settings.Avatar = req.Avatar
	if err := s.st.SaveSettings(settings); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// -------- tg users / bots --------

func (s *Server) handleListTGUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListTGUsers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleCreateTGUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		TGID     int64  `json:"tg_id"`
		Note     string `json:"note"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.TGID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tg_id 必须大于 0"})
		return
	}
	u := model.TGUser{
		ID:        newID("tg_"),
		Username:  req.Username,
		TGID:      req.TGID,
		Note:      req.Note,
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	if err := s.st.SaveTGUser(u); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) handleDeleteTGUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.st.DeleteTGUser(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleReplaceTGUsers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Users []struct {
			Username string `json:"username"`
			TGID     int64  `json:"tg_id"`
			Note     string `json:"note"`
		} `json:"users"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	for _, u := range req.Users {
		if u.TGID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tg_id 必须大于 0"})
			return
		}
	}
	old, err := s.st.ListTGUsers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	for _, u := range old {
		if err := s.st.DeleteTGUser(u.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}
	now := time.Now()
	for _, u := range req.Users {
		nu := model.TGUser{
			ID:        newID("tg_"),
			Username:  u.Username,
			TGID:      u.TGID,
			Note:      u.Note,
			Enabled:   true,
			CreatedAt: now,
		}
		if err := s.st.SaveTGUser(nu); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListTGBots(w http.ResponseWriter, r *http.Request) {
	bots, err := s.st.ListTGBots()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	type botView struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Token     string    `json:"token,omitempty"`
		Enabled   bool      `json:"enabled"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := make([]botView, 0, len(bots))
	for _, b := range bots {
		view := botView{ID: b.ID, Name: b.Name, Enabled: b.Enabled, CreatedAt: b.CreatedAt}
		if tok, err := s.st.Decrypt(b.TokenEnc); err == nil {
			view.Token = tok
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": out})
}

func (s *Server) handleCreateTGBot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !strings.Contains(req.Token, ":") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Bot Token 格式不正确"})
		return
	}
	enc, err := s.st.Encrypt(req.Token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	b := model.TGBot{ID: newID("bot_"), Name: req.Name, TokenEnc: enc, Enabled: true, CreatedAt: time.Now()}
	if err := s.st.SaveTGBot(b); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.tg.Reload()
	writeJSON(w, http.StatusOK, map[string]any{"bot": map[string]any{"id": b.ID, "name": b.Name, "enabled": b.Enabled}})
}

func (s *Server) handleDeleteTGBot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.st.DeleteTGBot(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.tg.Reload()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUpdateTGBot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok, err := s.st.GetTGBot(id)
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Bot 不存在"})
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.Token != "" {
		if !strings.Contains(req.Token, ":") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Bot Token 格式不正确"})
			return
		}
		enc, err := s.st.Encrypt(req.Token)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		existing.TokenEnc = enc
	}
	existing.Enabled = true
	if err := s.st.SaveTGBot(existing); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.tg.Reload()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// -------- p115 --------

func (s *Server) p115Cookie() (string, bool, error) {
	settings, _ := s.st.GetSettings()
	cookie, err := s.st.Decrypt(settings.P115Cookie)
	if err != nil {
		return "", false, err
	}
	if cookie == "" {
		return "", false, nil
	}
	return cookie, true, nil
}

func (s *Server) p115Client() (*p115.Client, bool, error) {
	cookie, ok, err := s.p115Cookie()
	if err != nil || !ok {
		return nil, ok, err
	}
	return p115.New(cookie), true, nil
}

func (s *Server) handleGetP115(w http.ResponseWriter, r *http.Request) {
	settings, _ := s.st.GetSettings()
	cookie, _ := s.st.Decrypt(settings.P115Cookie)
	qrcodeToken := ""
	if settings.P115QRCodeTokenEnc != "" {
		qrcodeToken, _ = s.st.Decrypt(settings.P115QRCodeTokenEnc)
	}
	source := settings.P115QRCodeSource
	if source == "" {
		source = "alipaymini"
	}
	savePaths := settings.P115SavePaths
	if len(savePaths) == 0 && settings.P115SavePathID != "" {
		savePaths = append(savePaths, model.P115SavePath{ID: settings.P115SavePathID, Name: settings.P115SavePathName})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":    cookie != "",
		"cookie":        cookie,
		"qrcode_token":  maskSecret(qrcodeToken),
		"qrcode_source": source,
		"save_paths":    savePaths,
	})
}

func (s *Server) handlePutP115(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cookie           string               `json:"cookie"`
		SavePaths        []model.P115SavePath `json:"save_paths"`
		QRCodeToken      string               `json:"qrcode_token"`
		QRCodeSource     string               `json:"qrcode_source"`
		ClearQRCodeToken bool                 `json:"clear_qrcode_token"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if len(req.SavePaths) > 6 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "最多选择 6 个离线目录"})
		return
	}
	seen := make(map[string]bool, len(req.SavePaths))
	for _, p := range req.SavePaths {
		id := strings.TrimSpace(p.ID)
		if id == "" || seen[id] {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "离线目录重复或为空"})
			return
		}
		seen[id] = true
	}
	settings, _ := s.st.GetSettings()

	source := strings.TrimSpace(req.QRCodeSource)
	if source == "" {
		source = settings.P115QRCodeSource
	}
	if source == "" {
		source = "alipaymini"
	}
	if !validQRCodeSource(source) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "二维码源设备无效"})
		return
	}
	settings.P115QRCodeSource = source

	token := strings.TrimSpace(req.QRCodeToken)
	currentToken := ""
	if settings.P115QRCodeTokenEnc != "" {
		currentToken, _ = s.st.Decrypt(settings.P115QRCodeTokenEnc)
	}
	if req.ClearQRCodeToken {
		settings.P115QRCodeTokenEnc = ""
		token = ""
	}
	if token == "" && !req.ClearQRCodeToken {
		token = currentToken
	}
	if token != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		cookie, err := p115.GetQrcodeResult(ctx, token, source)
		cancel()
		if err != nil {
			if enc, encErr := s.st.Encrypt(token); encErr == nil {
				settings.P115QRCodeTokenEnc = enc
			}
			_ = s.st.SaveSettings(settings)
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "换取 Cookie 失败：" + err.Error()})
			return
		}
		enc, err := s.st.Encrypt(cleanCookie(cookie))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		settings.P115Cookie = enc
		settings.P115QRCodeTokenEnc = ""
	} else if req.Cookie != "" {
		enc, err := s.st.Encrypt(cleanCookie(req.Cookie))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		settings.P115Cookie = enc
	}
	paths := make([]model.P115SavePath, 0, len(req.SavePaths))
	for _, p := range req.SavePaths {
		paths = append(paths, model.P115SavePath{ID: strings.TrimSpace(p.ID), Name: strings.TrimSpace(p.Name)})
	}
	settings.P115SavePaths = paths
	if len(paths) == 0 {
		settings.P115SavePathID = ""
		settings.P115SavePathName = ""
	} else {
		settings.P115SavePathID = paths[0].ID
		settings.P115SavePathName = paths[0].Name
	}
	if err := s.st.SaveSettings(settings); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCheckP115(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cookie string `json:"cookie"`
	}
	_ = decodeBody(r, &req)
	cookie := cleanCookie(req.Cookie)
	if cookie == "" {
		var ok bool
		var err error
		cookie, ok, err = s.p115Cookie()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未配置 115 Cookie"})
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client := p115.New(cookie)
	ok, msg, err := client.Check(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": msg})
		return
	}
	out := map[string]any{"ok": true, "message": msg}
	if dir, err := client.GetOfflineDir(ctx); err == nil {
		out["offline_dir"] = dir
	} else {
		out["offline_dir_error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListP115Dirs(w http.ResponseWriter, r *http.Request) {
	client, ok, err := s.p115Client()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未配置 115 Cookie"})
		return
	}
	cid := strings.TrimSpace(r.URL.Query().Get("cid"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if cid == "" {
		dir, err := client.GetOfflineDir(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		cid = dir.DestCID
	}
	dirs, err := client.ListDirs(ctx, cid)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	type dirView struct {
		CID  string `json:"cid"`
		PID  string `json:"pid"`
		Name string `json:"name"`
	}
	view := make([]dirView, 0, len(dirs))
	for _, d := range dirs {
		view = append(view, dirView{CID: d.CID, PID: d.PID, Name: d.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cid": cid, "dirs": view})
}

func (s *Server) handleListP115Tasks(w http.ResponseWriter, r *http.Request) {
	client, ok, err := s.p115Client()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未配置 115 Cookie"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	tasks, err := client.ListTasks(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleSaveTo115(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Magnet     string `json:"magnet"`
		Title      string `json:"title"`
		SavePathID string `json:"save_path_id"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	magnet := strings.TrimSpace(req.Magnet)
	if magnet == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少磁力链接"})
		return
	}
	client, ok, err := s.p115Client()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未配置 115 Cookie"})
		return
	}
	settings, _ := s.st.GetSettings()
	savePathID, savePathName, err := resolveSavePath(settings, req.SavePathID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := client.AddURL(ctx, magnet, "", savePathID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	message := "已添加到 115 网盘"
	if savePathName != "" {
		message += "：" + savePathName
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message})
}

func resolveSavePath(settings model.Settings, requestedID string) (string, string, error) {
	id := strings.TrimSpace(requestedID)
	name := ""
	if id != "" {
		found := false
		for _, p := range settings.P115SavePaths {
			if p.ID == id {
				found = true
				name = p.Name
				break
			}
		}
		if !found {
			return "", "", errors.New("离线目录无效")
		}
	} else if settings.P115SavePathID != "" {
		id = settings.P115SavePathID
		name = settings.P115SavePathName
	}
	return id, name, nil
}

func (s *Server) handleSaveTo115Batch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text       string `json:"text"`
		SavePathID string `json:"save_path_id"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	links := tgbot.ExtractDownloadLinks(req.Text)
	if len(links) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未识别到磁力或 ed2k 链接"})
		return
	}
	client, ok, err := s.p115Client()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "未配置 115 Cookie"})
		return
	}
	settings, _ := s.st.GetSettings()
	savePathID, savePathName, err := resolveSavePath(settings, req.SavePathID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	type batchError struct {
		Link  string `json:"link"`
		Error string `json:"error"`
	}
	var batchErrors []batchError
	added := 0
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	for _, link := range links {
		if err := client.AddURL(ctx, link, "", savePathID); err != nil {
			batchErrors = append(batchErrors, batchError{Link: link, Error: err.Error()})
			continue
		}
		added++
	}
	message := fmt.Sprintf("已添加 %d 个链接", added)
	if len(batchErrors) > 0 {
		message += fmt.Sprintf("，失败 %d 个", len(batchErrors))
	}
	if savePathName != "" {
		message += "：" + savePathName
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"added":   added,
		"failed":  len(batchErrors),
		"errors":  batchErrors,
		"message": message,
	})
}

func (s *Server) handleNetworkTest(w http.ResponseWriter, r *http.Request) {
	type target struct {
		Name string
		URL  string
	}
	targets := []target{
		{Name: "TG", URL: "https://t.me"},
		{Name: "谷歌", URL: "https://www.google.com"},
		{Name: "百度", URL: "https://www.baidu.com"},
		{Name: "GitHub", URL: "https://github.com"},
	}
	results := make([]map[string]any, len(targets))
	client := &http.Client{Timeout: 8 * time.Second}
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t target) {
			defer wg.Done()
			start := time.Now()
			resp, err := client.Get(t.URL)
			latency := time.Since(start).Milliseconds()
			item := map[string]any{"name": t.Name, "latency_ms": latency, "ok": err == nil}
			if err == nil {
				resp.Body.Close()
			} else {
				item["error"] = err.Error()
			}
			results[i] = item
		}(i, t)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// -------- p115 qrcode login --------

func (s *Server) handleQrcodeToken(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	token, err := p115.GetQrcodeToken(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token})
}

func (s *Server) handleQrcodeStatus(w http.ResponseWriter, r *http.Request) {
	token := p115.QrcodeToken{
		UID:  r.URL.Query().Get("uid"),
		Time: time.Now().Unix(),
		Sign: r.URL.Query().Get("sign"),
	}
	if token.UID == "" || token.Sign == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 uid 或 sign"})
		return
	}
	if t := r.URL.Query().Get("time"); t != "" {
		token.Time, _ = strconv.ParseInt(t, 10, 64)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 130*time.Second)
	defer cancel()
	status, err := p115.GetQrcodeStatus(ctx, token)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

func (s *Server) handleQrcodeResult(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("uid")
	app := r.URL.Query().Get("app")
	if uid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 uid"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cookie, err := p115.GetQrcodeResult(ctx, uid, app)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cookie": cookie})
}

// -------- helpers --------

var qrcodeSources = map[string]bool{
	"web": true, "android": true, "ios": true, "tv": true,
	"alipaymini": true, "wechatmini": true, "qandroid": true,
}

func validQRCodeSource(s string) bool { return qrcodeSources[s] }

func normalizeURLs(urls []string) []string {
	var out []string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			u = "https://" + u
		}
		u = strings.TrimRight(u, "/")
		out = append(out, u)
	}
	return out
}

func cleanCookie(s string) string {
	s = strings.NewReplacer("\r", "", "\n", "").Replace(s)
	return strings.TrimSpace(s)
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:8] + "****" + s[len(s)-4:]
}
