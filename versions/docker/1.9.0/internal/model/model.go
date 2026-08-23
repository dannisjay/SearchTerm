package model

import "time"

// Site is a magnet site account configured in the admin backend.
type Site struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	BaseURLs      []string  `json:"base_urls"`
	Username      string    `json:"username,omitempty"`
	Password      string    `json:"password,omitempty"` // plaintext only in API request bodies
	PasswordEnc   string    `json:"password_enc,omitempty"`
	CookieEnc     string    `json:"cookie_enc,omitempty"`
	Cookie        string    `json:"cookie,omitempty"` // plaintext only in API request/response
	Enabled       bool      `json:"enabled"`
	LastError     string    `json:"last_error,omitempty"`
	LastSuccessAt time.Time `json:"last_success_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Settings holds global toggles editable from the admin backend.
type Settings struct {
	BackgroundImageURL string         `json:"background_image_url,omitempty"`
	P115Cookie         string         `json:"p115_cookie,omitempty"`
	P115SavePathID     string         `json:"p115_save_path_id,omitempty"`
	P115SavePathName   string         `json:"p115_save_path_name,omitempty"`
	P115SavePaths      []P115SavePath `json:"p115_save_paths,omitempty"`
	P115QRCodeTokenEnc string         `json:"p115_qrcode_token_enc,omitempty"`
	P115QRCodeSource   string         `json:"p115_qrcode_source,omitempty"`
	AdminUser          string         `json:"admin_user,omitempty"`
	AdminPasswordHash  string         `json:"admin_password_hash,omitempty"`
	Avatar             string         `json:"avatar,omitempty"`
	DefaultsV2Applied  bool           `json:"defaults_v2_applied,omitempty"`
}

// P115SavePath is one folder selectable as the offline download target.
type P115SavePath struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TGUser is a Telegram user allowed to use the bot.
type TGUser struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	TGID      int64     `json:"tg_id"`
	Note      string    `json:"note,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// TGBot is a Telegram bot token managed from the admin backend.
type TGBot struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenEnc  string    `json:"token_enc,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// SearchItem is one deduplicated magnet result.
type SearchItem struct {
	Title     string   `json:"title"`
	Year      int      `json:"year,omitempty"`
	Name      string   `json:"name,omitempty"`
	Magnet    string   `json:"magnet"`
	InfoHash  string   `json:"info_hash"`
	Size      string   `json:"size"`
	Site      string   `json:"site"`
	Source    string   `json:"source"`
	Sources   []string `json:"sources,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
	Adult     bool     `json:"adult,omitempty"`
}

// ProviderResult is the normalized output of one site adapter.
type ProviderResult struct {
	Title     string
	Year      int
	Name      string
	Magnet    string
	InfoHash  string
	Size      string
	Source    string
	UpdatedAt string
	Adult     bool
}
