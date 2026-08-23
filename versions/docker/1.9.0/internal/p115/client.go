// Package p115 implements the subset of 115 web APIs needed for offline
// magnet transfer. Interface behavior is informed by p115client
// (https://github.com/ChenyangGao/p115client), MIT License.
package p115

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	webAPIBase        = "https://webapi.115.com"
	proAPIBase        = "https://proapi.115.com"
	clouddownloadBase = "https://clouddownload.115.com"
)

// Client talks to the 115 web APIs using a full cookie string.
type Client struct {
	cookie string
	http   *http.Client
}

// Task is one offline download task.
type Task struct {
	InfoHash  string `json:"info_hash"`
	Name      string `json:"name"`
	Size      string `json:"size"`
	Status    string `json:"status"`
	State     string `json:"state"`
	AddTime   string `json:"addtime"`
	FileCount int64  `json:"file_count"`
}

// Dir is one folder in the 115 file tree.
type Dir struct {
	CID       string `json:"cid"`
	PID       string `json:"pid"`
	Name      string `json:"n"`
	Count     int64  `json:"count"`
	FileCount int64  `json:"file_count"`
}

// OfflineDir is the directory pair returned by get_id: cid is the torrent
// upload folder and dest_cid is the offline download target folder.
type OfflineDir struct {
	CID     string `json:"cid"`
	DestCID string `json:"dest_cid"`
}

func New(cookie string) *Client {
	return &Client{
		cookie: strings.TrimSpace(cookie),
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Check validates the cookie by resolving the offline download folder ids.
func (c *Client) Check(ctx context.Context) (bool, string, error) {
	if _, err := c.GetOfflineDir(ctx); err != nil {
		return false, "", err
	}
	return true, "Cookie 有效", nil
}

// GetOfflineDir resolves the offline download folder ids.
func (c *Client) GetOfflineDir(ctx context.Context) (OfflineDir, error) {
	params := url.Values{}
	params.Set("ac", "get_id")
	params.Set("torrent", "1")
	var resp struct {
		State   bool   `json:"state"`
		Errno   int    `json:"errno"`
		Error   string `json:"error"`
		CID     string `json:"cid"`
		DestCID string `json:"dest_cid"`
	}
	if err := c.request(ctx, http.MethodGet, clouddownloadBase+"/web/?"+params.Encode(), nil, &resp); err != nil {
		return OfflineDir{}, err
	}
	if !resp.State && (resp.Errno != 0 || resp.Error != "") {
		msg := resp.Error
		if msg == "" {
			msg = fmt.Sprintf("errno=%d", resp.Errno)
		}
		return OfflineDir{}, fmt.Errorf("115 get offline dir: %s", msg)
	}
	if resp.CID == "" && resp.DestCID == "" {
		return OfflineDir{}, fmt.Errorf("115 get offline dir: empty ids")
	}
	return OfflineDir{CID: resp.CID, DestCID: resp.DestCID}, nil
}

// ListDirs lists subfolders of cid (root is "0").
func (c *Client) ListDirs(ctx context.Context, cid string) ([]Dir, error) {
	if cid == "" {
		cid = "0"
	}
	params := url.Values{}
	params.Set("aid", "1")
	params.Set("cid", cid)
	params.Set("limit", "1000")
	params.Set("offset", "0")
	params.Set("record_open_time", "1")
	params.Set("show_dir", "1")
	params.Set("cur", "1")
	params.Set("fc_mix", "1")
	params.Set("asc", "1")
	params.Set("o", "user_ptime")
	params.Set("count_folders", "1")
	var resp struct {
		State bool   `json:"state"`
		Errno int    `json:"errno"`
		Error string `json:"error"`
		Count int64  `json:"count"`
		Data  []Dir  `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, webAPIBase+"/files?"+params.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	if !resp.State && (resp.Errno != 0 || resp.Error != "") {
		msg := resp.Error
		if msg == "" {
			msg = fmt.Sprintf("errno=%d", resp.Errno)
		}
		return nil, fmt.Errorf("115 list dirs: %s", msg)
	}
	return resp.Data, nil
}

// AddURL adds an offline download task for HTTP/HTTPS/FTP/magnet/ed2k links.
// savePath is a relative path inside wpPathID, which is the target folder id.
func (c *Client) AddURL(ctx context.Context, rawURL, savePath, wpPathID string) error {
	form := url.Values{}
	form.Set("ac", "add_task_url")
	form.Set("url", rawURL)
	if savePath != "" {
		form.Set("savepath", savePath)
	}
	if wpPathID != "" {
		form.Set("wp_path_id", wpPathID)
	}
	return c.formPost(ctx, form)
}

// AddBT adds an offline task directly by torrent info hash.
func (c *Client) AddBT(ctx context.Context, infoHash, savePath, wpPathID string) error {
	form := url.Values{}
	form.Set("ac", "add_task_bt")
	form.Set("info_hash", infoHash)
	if savePath != "" {
		form.Set("savepath", savePath)
	}
	if wpPathID != "" {
		form.Set("wp_path_id", wpPathID)
	}
	return c.formPost(ctx, form)
}

// ListTasks returns recent offline download tasks.
func (c *Client) ListTasks(ctx context.Context) ([]Task, error) {
	params := url.Values{}
	params.Set("ac", "task_lists")
	params.Set("page", "1")
	params.Set("page_size", "50")
	var resp struct {
		State bool `json:"state"`
		Data  struct {
			Tasks []map[string]any `json:"tasks"`
		} `json:"data"`
		Errno int    `json:"errno"`
		Error string `json:"error"`
	}
	if err := c.request(ctx, http.MethodGet, clouddownloadBase+"/web/?"+params.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	if !resp.State {
		msg := resp.Error
		if msg == "" {
			msg = fmt.Sprintf("errno=%d", resp.Errno)
		}
		return nil, fmt.Errorf("115 task list: %s", msg)
	}
	tasks := make([]Task, 0, len(resp.Data.Tasks))
	for _, raw := range resp.Data.Tasks {
		tasks = append(tasks, Task{
			InfoHash:  str(raw["info_hash"]),
			Name:      str(raw["name"]),
			Size:      str(raw["size"]),
			Status:    str(raw["status"]),
			State:     str(raw["state"]),
			AddTime:   str(raw["addtime"]),
			FileCount: int64(num(raw["file_count"])),
		})
	}
	return tasks, nil
}

// TaskStatusText maps the numeric task status to a human readable label.
func TaskStatusText(status string) string {
	switch status {
	case "11":
		return "已完成"
	case "12":
		return "进行中"
	case "9":
		return "已失败"
	case "2":
		return "下载中"
	case "1":
		return "等待中"
	case "0":
		return "等待中"
	default:
		if status == "" {
			return "未知"
		}
		return status
	}
}

func (c *Client) formPost(ctx context.Context, form url.Values) error {
	var resp struct {
		State bool   `json:"state"`
		Errno int    `json:"errno"`
		Error string `json:"error"`
		Msg   string `json:"msg"`
	}
	if err := c.request(ctx, http.MethodPost, clouddownloadBase+"/web/", strings.NewReader(form.Encode()), &resp); err != nil {
		return err
	}
	if !resp.State {
		msg := resp.Msg
		if msg == "" {
			msg = resp.Error
		}
		if msg == "" {
			msg = fmt.Sprintf("errno=%d", resp.Errno)
		}
		return fmt.Errorf("115 add task: %s", msg)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, rawURL string, body io.Reader, out any) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return err
		}
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("115 网络错误: %w", ctx.Err())
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		var reader io.Reader
		if bodyBytes != nil {
			reader = bytes.NewReader(bodyBytes)
		}
		lastErr = c.requestOnce(ctx, method, rawURL, reader, bodyBytes != nil, out)
		if lastErr == nil {
			return nil
		}
		if !isRetryableNetError(lastErr) {
			return lastErr
		}
	}
	return fmt.Errorf("115 网络错误: %w", lastErr)
}

func (c *Client) requestOnce(ctx context.Context, method, rawURL string, body io.Reader, hasBody bool, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://115.com/")
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("115 auth failed: http %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("115 http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("115 response: %w", err)
		}
	}
	return nil
}

func isRetryableNetError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := err.Error()
	for _, s := range []string{"TLS handshake timeout", "connection reset", "connection refused", "EOF"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}
