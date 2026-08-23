// Package qiwei implements a magnet-site adapter for the Qiwei engine.
//
// Logic adapted from fish2018/pansou (https://github.com/fish2018/pansou),
// plugin/qiwei, MIT License, Copyright (c) 2025 fish2018.
package qiwei

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"searchterm/internal/model"
)

// DefaultBaseURLs are the known Qiwei mirrors. The latest addresses come
// first; the two permanent addresses may require a proxy.
var DefaultBaseURLs = []string{
	"https://www.qwmp4.com",
	"https://www.qwmkv.com",
	"https://www.qwfilm.com",
	"https://www.qwfun.com",
	"https://www.qn63.com",
	"https://www.qmp4.com",
	"https://www.gmp4.com",
}

const (
	maxSuggestItems      = 20
	maxDetailConcurrency = 12
	requestTimeout       = 15 * time.Second
	maxBodyBytes         = 64 << 20
	userAgent            = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
)

var (
	whitespaceRe = regexp.MustCompile(`\s+`)
	yearSuffixRe = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)
	titleRe      = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	htmlTagRe    = regexp.MustCompile(`(?is)<[^>]+>`)
	magnetRe     = regexp.MustCompile(`magnet:\?[^\s"'<>]+`)
	hashRe       = regexp.MustCompile(`(?i)btih:([a-f0-9]{40})`)
	magnetLinkRe = regexp.MustCompile(`(?is)<a\s[^>]*href\s*=\s*["'](magnet:[^"']+)["'][^>]*>(.*?)</a>`)
	magnetSizeRe = regexp.MustCompile(`(?i)\[([\d.]+\s*(?:k|m|g|t)(?:i?b)?)\]|([\d.]+\s*(?:k|m|g|t)(?:i?b)?)\b`)
)

type suggestResponse struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	List []suggestItem `json:"list"`
}

type suggestItem struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Pic  string `json:"pic"`
	En   string `json:"en"`
}

// Client is a stateless Qiwei adapter with mirror fallback.
type Client struct {
	baseURLs []string
	client   *http.Client

	mu     sync.Mutex
	active string
}

func New(baseURLs []string) *Client {
	if len(baseURLs) == 0 {
		baseURLs = DefaultBaseURLs
	}
	return &Client{
		baseURLs: baseURLs,
		client: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          20,
				MaxIdleConnsPerHost:   8,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
	}
}

func (c *Client) Source() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != "" {
		return c.active
	}
	if len(c.baseURLs) == 0 {
		return ""
	}
	return strings.TrimRight(c.baseURLs[0], "/")
}

func (c *Client) Search(ctx context.Context, keyword string) ([]model.ProviderResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	var lastErr error
	for _, base := range c.candidates() {
		items, err := c.searchSuggest(ctx, base, keyword)
		if err != nil {
			lastErr = err
			continue
		}
		results := c.enrichResults(ctx, base, items)
		if len(results) > 0 {
			c.setActive(base)
			return results, nil
		}
	}
	if lastErr == nil {
		return nil, nil
	}
	return nil, lastErr
}

func (c *Client) searchSuggest(ctx context.Context, base, keyword string) ([]suggestItem, error) {
	u := fmt.Sprintf("%s/index.php/ajax/suggest?mid=1&limit=%d&wd=%s", base, maxSuggestItems, url.QueryEscape(keyword))
	body, err := c.fetch(ctx, base, u)
	if err != nil {
		return nil, err
	}
	var resp suggestResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qiwei: parse suggest: %w", err)
	}
	if resp.Code != 1 && len(resp.List) == 0 {
		return nil, fmt.Errorf("qiwei: suggest abnormal code=%d msg=%s", resp.Code, resp.Msg)
	}
	seen := make(map[int]struct{}, len(resp.List))
	items := make([]suggestItem, 0, len(resp.List))
	for _, it := range resp.List {
		if it.ID == 0 || cleanText(it.Name) == "" {
			continue
		}
		if _, ok := seen[it.ID]; ok {
			continue
		}
		seen[it.ID] = struct{}{}
		items = append(items, it)
		if len(items) >= maxSuggestItems {
			break
		}
	}
	return items, nil
}

func (c *Client) enrichResults(ctx context.Context, base string, items []suggestItem) []model.ProviderResult {
	results := make([][]model.ProviderResult, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxDetailConcurrency)
	for i, it := range items {
		wg.Add(1)
		go func(idx int, item suggestItem) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			detailURL := fmt.Sprintf("%s/mv/%d.html", base, item.ID)
			body, err := c.fetch(ctx, base, detailURL)
			if err != nil {
				return
			}
			results[idx] = parseDetail(item, body, base)
		}(i, it)
	}
	wg.Wait()

	var out []model.ProviderResult
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}

func parseDetail(item suggestItem, body []byte, base string) []model.ProviderResult {
	var title string
	if m := titleRe.FindSubmatch(body); len(m) >= 2 {
		title = cleanText(string(m[1]))
	}
	title = yearSuffixRe.ReplaceAllString(title, "")
	if title == "" {
		title = cleanText(item.Name)
	}
	html := htmlEntityDecode(string(decodeHTMLBody(body)))
	seen := make(map[string]struct{}, 32)
	urlSeen := make(map[string]struct{}, 32)
	var out []model.ProviderResult
	for _, m := range magnetLinkRe.FindAllStringSubmatch(html, -1) {
		magnet := strings.TrimSpace(m[1])
		if magnet == "" {
			continue
		}
		hash := extractHash(magnet)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		urlSeen[magnet] = struct{}{}
		out = append(out, model.ProviderResult{
			Title:    title,
			Name:     title,
			Magnet:   magnet,
			InfoHash: hash,
			Size:     extractMagnetSize(m[0]),
			Source:   base,
		})
	}
	for _, raw := range magnetRe.FindAllString(html, -1) {
		magnet := strings.TrimSpace(raw)
		if magnet == "" {
			continue
		}
		if _, ok := urlSeen[magnet]; ok {
			continue
		}
		urlSeen[magnet] = struct{}{}
		hash := extractHash(magnet)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		out = append(out, model.ProviderResult{
			Title:    title,
			Name:     title,
			Magnet:   magnet,
			InfoHash: hash,
			Source:   base,
		})
	}
	return out
}

func extractMagnetSize(s string) string {
	if m := magnetSizeRe.FindStringSubmatch(s); len(m) > 1 {
		for _, v := range m[1:] {
			if v != "" {
				return normalizeSizeUnit(v)
			}
		}
	}
	return ""
}

func normalizeSizeUnit(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch {
	case strings.HasSuffix(s, "K"), strings.HasSuffix(s, "M"), strings.HasSuffix(s, "G"), strings.HasSuffix(s, "T"):
		return s + "B"
	}
	return s
}

func decodeHTMLBody(body []byte) []byte {
	s := strings.TrimSpace(string(body))
	if !strings.HasPrefix(s, "\"") {
		return body
	}
	var decoded string
	if err := json.Unmarshal([]byte(s), &decoded); err == nil {
		return []byte(decoded)
	}
	return body
}

func extractHash(magnet string) string {
	m := hashRe.FindStringSubmatch(magnet)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(m[1])
}

func (c *Client) fetch(ctx context.Context, base, requestURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/json;q=0.8,*/*;q=0.7")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", base+"/")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qiwei: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	if isVerifyPage(body) {
		return nil, fmt.Errorf("qiwei: verify page %s", base)
	}
	return body, nil
}

func (c *Client) candidates() []string {
	c.mu.Lock()
	active := c.active
	c.mu.Unlock()
	out := make([]string, 0, len(c.baseURLs)+1)
	seen := make(map[string]struct{}, len(c.baseURLs)+1)
	add := func(host string) {
		host = strings.TrimRight(host, "/")
		if host == "" {
			return
		}
		if _, ok := seen[host]; ok {
			return
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	add(active)
	for _, host := range c.baseURLs {
		add(host)
	}
	return out
}

func (c *Client) setActive(base string) {
	c.mu.Lock()
	c.active = strings.TrimRight(base, "/")
	c.mu.Unlock()
}

func cleanText(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = whitespaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func htmlEntityDecode(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&#38;", "&",
		"&quot;", `"`,
		"&#34;", `"`,
		"&#39;", "'",
		"&apos;", "'",
		"&lt;", "<",
		"&gt;", ">",
	)
	return replacer.Replace(s)
}

func isVerifyPage(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "系统安全验证") ||
		strings.Contains(lower, "verify_check") ||
		strings.Contains(lower, "mac_verify_img") ||
		strings.Contains(lower, "请输入验证码") ||
		strings.Contains(lower, "/_guard/html.js?js=easy_click_html")
}
