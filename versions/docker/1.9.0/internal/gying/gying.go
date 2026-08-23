// Package gying implements a magnet-site adapter for gying-engine sites.
//
// Logic adapted from fish2018/pansou (https://github.com/fish2018/pansou),
// plugin/gying, MIT License, Copyright (c) 2025 fish2018.
package gying

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"searchterm/internal/model"
)

var (
	challengeJSONRe = regexp.MustCompile(`(?s)const\s+json\s*=\s*(\{.*?\})\s*;\s*const\s+jss\s*=`)
	searchDataRe    = regexp.MustCompile(`(?s)_obj\s*\.\s*search\s*=\s*(\{.*?\})\s*;`)
	magnetHashRe    = regexp.MustCompile(`(?i)^[a-f0-9]{40}$`)
)

// DefaultBaseURLs are the known mirror domains of the gying site. They are
// baked into the app so the admin only needs to configure an account.
var DefaultBaseURLs = []string{
	"https://www.hgeme.com",
	"https://www.xn--wcv59z.com",
	"https://www.xn--kivn76b41nnhi.com",
	"https://www.xn--10vr61a3xc5x3b.com",
	"https://www.xn--vcsx1ip8b8w4i.com",
	"https://www.xn--74qz10cqsltibh40akss.com",
}

const (
	defaultDetailConcurrency = 4
	minPowSolveTime          = 3 * time.Second
)

type challengeData struct {
	ID        string   `json:"id"`
	Challenge []string `json:"challenge"`
	Diff      int      `json:"diff"`
	Salt      string   `json:"salt"`
	N         string   `json:"N"`
	X         string   `json:"x"`
	T         int      `json:"t"`
}

type verifyResponse struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
}

type searchData struct {
	Q string `json:"q"`
	N string `json:"n"`
	L struct {
		Title  []string `json:"title"`
		Year   []int    `json:"year"`
		D      []string `json:"d"`
		I      []string `json:"i"`
		Info   []string `json:"info"`
		Daoyan []string `json:"daoyan"`
		Zhuyan []string `json:"zhuyan"`
	} `json:"l"`
}

type detailData struct {
	Code     int `json:"code"`
	Downlist struct {
		List struct {
			M []string `json:"m"`
			T []string `json:"t"`
			S []string `json:"s"`
			N []string `json:"n"`
		} `json:"list"`
	} `json:"downlist"`
}

// Client is a stateful adapter for one gying account.
type Client struct {
	baseURLs []string
	username string
	password string

	client *http.Client

	mu      sync.Mutex
	baseURL string
	authed  bool
}

func New(username, password string, baseURLs []string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURLs: baseURLs,
		username: username,
		password: password,
		client: &http.Client{
			Timeout: 60 * time.Second,
			Jar:     jar,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          20,
				MaxIdleConnsPerHost:   8,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
	}, nil
}

func (c *Client) Source() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseURL
}

func (c *Client) Search(ctx context.Context, keyword string) ([]model.ProviderResult, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	base := c.Source()
	searchURL := fmt.Sprintf("%s/search?q=%s&type=0&mode=2", base, url.QueryEscape(keyword))
	body, status, err := c.do(ctx, http.MethodGet, searchURL, nil, base)
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden || isLoginShell(body) {
		c.resetLogin()
		return nil, fmt.Errorf("gying search requires re-login")
	}
	matches := searchDataRe.FindSubmatch(body)
	if len(matches) < 2 {
		return nil, fmt.Errorf("gying search data not found")
	}
	var sd searchData
	if err := json.Unmarshal(matches[1], &sd); err != nil {
		return nil, fmt.Errorf("parse gying search data: %w", err)
	}

	// Warm up the anti-bot cookie chain before detail requests.
	_, _, _ = c.do(ctx, http.MethodGet, base+"/mv/wkMn", nil, searchURL)

	return c.fetchDetails(ctx, &sd, keyword, base)
}

func (c *Client) ensureLogin(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authed && c.baseURL != "" {
		return nil
	}
	if len(c.baseURLs) == 0 {
		return fmt.Errorf("gying: no base urls configured")
	}
	for _, base := range c.baseURLs {
		base = strings.TrimRight(base, "/")
		if err := c.loginTo(ctx, base); err != nil {
			continue
		}
		c.baseURL = base
		c.authed = true
		return nil
	}
	return fmt.Errorf("gying: login failed on all mirrors")
}

func (c *Client) resetLogin() {
	c.mu.Lock()
	c.authed = false
	c.baseURL = ""
	c.mu.Unlock()
}

func (c *Client) loginTo(ctx context.Context, base string) error {
	if _, _, err := c.do(ctx, http.MethodGet, base, nil, ""); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("code", "")
	form.Set("siteid", "1")
	form.Set("dosubmit", "1")
	form.Set("cookietime", "10506240")
	form.Set("username", c.username)
	form.Set("password", c.password)
	body, status, err := c.do(ctx, http.MethodPost, base+"/user/login", form, base)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("gying login http %d", status)
	}
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("gying login response: %w", err)
	}
	if resp.Code != 200 {
		return fmt.Errorf("gying login code=%d", resp.Code)
	}
	_, _, _ = c.do(ctx, http.MethodGet, base+"/mv/wkMn", nil, base+"/user/login")
	return nil
}

func (c *Client) fetchDetails(ctx context.Context, sd *searchData, keyword, base string) ([]model.ProviderResult, error) {
	ids := sd.L.I
	results := make([]model.ProviderResult, 0, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, defaultDetailConcurrency)
	keywordLower := strings.ToLower(keyword)

	for i := range ids {
		if i >= len(sd.L.Title) {
			continue
		}
		title := sd.L.Title[i]
		if !strings.Contains(strings.ToLower(title), keywordLower) {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			detailURL := fmt.Sprintf("%s/res/downurl/%s/%s", base, sd.L.D[idx], sd.L.I[idx])
			body, status, err := c.do(ctx, http.MethodGet, detailURL, nil, base)
			if err != nil || status != http.StatusOK {
				return
			}
			var detail detailData
			if err := json.Unmarshal(body, &detail); err != nil {
				return
			}
			if detail.Code == 403 {
				c.resetLogin()
				return
			}
			items := c.buildResults(detail, title, sd, idx, base)
			mu.Lock()
			results = append(results, items...)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return results, nil
}

func (c *Client) buildResults(detail detailData, title string, sd *searchData, idx int, base string) []model.ProviderResult {
	list := detail.Downlist.List
	var out []model.ProviderResult
	for i, h := range list.M {
		if !magnetHashRe.MatchString(h) {
			continue
		}
		lowerHash := strings.ToLower(h)
		resName := title
		if i < len(list.T) && list.T[i] != "" {
			resName = list.T[i]
		}
		size := ""
		if i < len(list.S) {
			size = list.S[i]
		}
		updated := ""
		if i < len(list.N) {
			updated = list.N[i]
		}
		year := 0
		if idx < len(sd.L.Year) {
			year = sd.L.Year[idx]
		}
		out = append(out, model.ProviderResult{
			Title:     title,
			Year:      year,
			Name:      resName,
			Magnet:    "magnet:?xt=urn:btih:" + lowerHash,
			InfoHash:  lowerHash,
			Size:      size,
			Source:    base,
			UpdatedAt: updated,
		})
	}
	return out
}

// do performs one request and transparently solves the PoW challenge once.
func (c *Client) do(ctx context.Context, method, requestURL string, form url.Values, referer string) ([]byte, int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		body, status, err := c.raw(ctx, method, requestURL, form, referer)
		if err != nil {
			return nil, status, err
		}
		if !isChallengePage(body) {
			return body, status, nil
		}
		if attempt == 1 {
			return nil, status, fmt.Errorf("gying: challenge retry exhausted")
		}
		if err := c.solveChallenge(ctx, requestURL, body); err != nil {
			return nil, status, err
		}
	}
	return nil, 0, fmt.Errorf("gying: request retries exhausted")
}

func (c *Client) raw(ctx context.Context, method, requestURL string, form url.Values, referer string) ([]byte, int, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

func (c *Client) solveChallenge(ctx context.Context, requestURL string, body []byte) error {
	if m := challengeJSONRe.FindSubmatch(body); len(m) >= 2 {
		var ch challengeData
		if err := json.Unmarshal(m[1], &ch); err != nil {
			return err
		}
		y, err := computePow(ch)
		if err != nil {
			return err
		}
		form := url.Values{}
		form.Set("action", "verify")
		form.Set("id", ch.ID)
		form.Set("y", y)
		return c.submitVerify(ctx, requestURL, form, requestURL)
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return err
	}
	parsed.Path = "/res/pow"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	powURL := parsed.String()

	powBody, status, err := c.raw(ctx, http.MethodGet, powURL, nil, requestURL)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("gying: get pow challenge failed status=%d err=%v", status, err)
	}
	var ch challengeData
	if err := json.Unmarshal(powBody, &ch); err != nil {
		return fmt.Errorf("gying: parse pow challenge: %w", err)
	}
	y, err := computePow(ch)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("y", y)
	return c.submitVerify(ctx, powURL, form, requestURL)
}

func (c *Client) submitVerify(ctx context.Context, verifyURL string, form url.Values, referer string) error {
	body, status, err := c.raw(ctx, http.MethodPost, verifyURL, form, referer)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("gying: verify http %d", status)
	}
	var resp verifyResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("gying: verify response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("gying: challenge verify failed: %s", resp.Msg)
	}
	return nil
}

func computePow(ch challengeData) (string, error) {
	if ch.N == "" || ch.X == "" || ch.T <= 0 {
		return "", fmt.Errorf("gying: invalid pow challenge")
	}
	n, ok := new(big.Int).SetString(ch.N, 16)
	if !ok || n.Sign() <= 0 {
		return "", fmt.Errorf("gying: invalid pow modulus")
	}
	y, ok := new(big.Int).SetString(ch.X, 16)
	if !ok || y.Sign() < 0 {
		return "", fmt.Errorf("gying: invalid pow x")
	}
	start := time.Now()
	for i := 0; i < ch.T; i++ {
		y.Mul(y, y)
		y.Mod(y, n)
	}
	if elapsed := time.Since(start); elapsed < minPowSolveTime {
		time.Sleep(minPowSolveTime - elapsed)
	}
	return y.Text(16), nil
}

func isChallengePage(body []byte) bool {
	text := string(body)
	return strings.Contains(text, "powSolve-") ||
		strings.Contains(text, "pow.worker-") ||
		strings.Contains(text, "/res/pow") ||
		strings.Contains(text, "安全验证")
}

func isLoginShell(body []byte) bool {
	text := string(body)
	return strings.Contains(text, "_BT.PC.HTML('login')") ||
		strings.Contains(text, "_BT.PC.HTML('nologin')")
}
