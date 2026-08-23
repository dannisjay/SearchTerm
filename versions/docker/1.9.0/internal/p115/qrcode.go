package p115

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const qrcodeAPIBase = "https://qrcodeapi.115.com"

// QrcodeToken is the token response used to render and poll a login QR code.
type QrcodeToken struct {
	UID  string `json:"uid"`
	Time int64  `json:"time"`
	Sign string `json:"sign"`
}

// GetQrcodeToken requests a fresh login QR code token from 115.
func GetQrcodeToken(ctx context.Context) (QrcodeToken, error) {
	var resp struct {
		State int         `json:"state"`
		Data  QrcodeToken `json:"data"`
	}
	if err := qrcodeRequest(ctx, http.MethodGet, qrcodeAPIBase+"/api/1.0/web/1.0/token/", nil, &resp); err != nil {
		return QrcodeToken{}, err
	}
	if resp.State == 0 || resp.Data.UID == "" {
		return QrcodeToken{}, fmt.Errorf("115 qrcode token: invalid response")
	}
	return resp.Data, nil
}

// QrcodeImageURL returns the login QR code image URL for a uid.
func QrcodeImageURL(uid string) string {
	return qrcodeAPIBase + "/api/1.0/web/1.0/qrcode?uid=" + url.QueryEscape(uid)
}

// GetQrcodeStatus polls the scan status of a QR code. Status 2 means signed in.
func GetQrcodeStatus(ctx context.Context, token QrcodeToken) (int, error) {
	params := url.Values{}
	params.Set("uid", token.UID)
	params.Set("time", fmt.Sprintf("%d", token.Time))
	params.Set("sign", token.Sign)
	params.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli()))
	var resp struct {
		State int `json:"state"`
		Data  struct {
			Status int `json:"status"`
		} `json:"data"`
	}
	if err := qrcodeRequest(ctx, http.MethodGet, qrcodeAPIBase+"/get/status/?"+params.Encode(), nil, &resp); err != nil {
		return -99, err
	}
	if resp.State == 0 {
		return -99, fmt.Errorf("115 qrcode status: invalid response")
	}
	return resp.Data.Status, nil
}

// GetQrcodeResult completes the login and returns the cookie string.
// app is the bound device, e.g. "alipaymini" or "web"; alipaymini is the
// default used by the reference implementations.
func GetQrcodeResult(ctx context.Context, uid, app string) (string, error) {
	if app == "" {
		app = "alipaymini"
	}
	form := url.Values{}
	form.Set("account", uid)
	form.Set("app", app)
	var resp struct {
		State int `json:"state"`
		Data  struct {
			Code   int               `json:"code"`
			Msg    string            `json:"msg"`
			Cookie map[string]string `json:"cookie"`
		} `json:"data"`
	}
	if err := qrcodeRequest(ctx, http.MethodPost,
		fmt.Sprintf("%s/app/1.0/%s/1.0/login/qrcode/", qrcodeAPIBase, app),
		strings.NewReader(form.Encode()), &resp); err != nil {
		return "", err
	}
	if resp.State == 0 || len(resp.Data.Cookie) == 0 {
		msg := resp.Data.Msg
		if msg == "" {
			msg = fmt.Sprintf("code=%d", resp.Data.Code)
		}
		return "", fmt.Errorf("115 qrcode login: %s", msg)
	}
	var parts []string
	for k, v := range resp.Data.Cookie {
		parts = append(parts, k+"="+v)
	}
	joined := strings.Join(parts, "; ")
	if !strings.Contains(joined, "uid=") {
		joined += "; uid=" + uid
	}
	return joined, nil
}

func qrcodeRequest(ctx context.Context, method, rawURL string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := &http.Client{Timeout: 130 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("115 qrcode http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("115 qrcode response: %w", err)
		}
	}
	return nil
}
