// Package nyaa implements a magnet-site adapter for Nyaa (nyaa.si).
package nyaa

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"searchterm/internal/model"
)

// DefaultBaseURLs are the known mirrors. nyaa.si is the primary mirror;
// nyaa.land and nyaa.one are kept as fallbacks when it is unreachable.
var DefaultBaseURLs = []string{
	"https://nyaa.si",
	"https://nyaa.land",
	"https://nyaa.one",
}

// DefaultSukebeiBaseURLs is the adult-only mirror of Nyaa.
var DefaultSukebeiBaseURLs = []string{
	"https://sukebei.nyaa.si",
}

const maxResults = 50

var magnetHashRe = regexp.MustCompile(`^[a-f0-9]{40}$`)

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title    string `xml:"title"`
	Link     string `xml:"link"`
	GUID     string `xml:"guid"`
	PubDate  string `xml:"pubDate"`
	Seeders  string `xml:"seeders"`
	Leechers string `xml:"leechers"`
	InfoHash string `xml:"infoHash"`
	Size     string `xml:"size"`
	Category string `xml:"category"`
}

// Client is a stateless Nyaa adapter. Nyaa does not require login.
type Client struct {
	baseURLs []string
	client   *http.Client
	adult    bool
}

// New creates a client for the regular Nyaa mirrors.
func New(baseURLs []string) *Client {
	return newClient(baseURLs, false)
}

// NewAdult creates a client for an adult-only mirror such as Sukebei.
func NewAdult(baseURLs []string) *Client {
	return newClient(baseURLs, true)
}

func newClient(baseURLs []string, adult bool) *Client {
	if len(baseURLs) == 0 {
		baseURLs = DefaultBaseURLs
	}
	return &Client{
		baseURLs: baseURLs,
		adult:    adult,
		client: &http.Client{
			Timeout: 30 * time.Second,
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
	if len(c.baseURLs) == 0 {
		return ""
	}
	return strings.TrimRight(c.baseURLs[0], "/")
}

func (c *Client) Search(ctx context.Context, keyword string) ([]model.ProviderResult, error) {
	var lastErr error
	for _, base := range c.baseURLs {
		items, err := c.searchBase(ctx, strings.TrimRight(base, "/"), keyword)
		if err == nil {
			return items, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("nyaa: no base urls configured")
	}
	return nil, lastErr
}

func (c *Client) searchBase(ctx context.Context, base, keyword string) ([]model.ProviderResult, error) {
	searchURL := fmt.Sprintf("%s/?page=rss&c=0_0&f=0&q=%s", base, url.QueryEscape(keyword))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nyaa rss http %d", resp.StatusCode)
	}
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("nyaa rss parse: %w", err)
	}
	out := make([]model.ProviderResult, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		if len(out) >= maxResults {
			break
		}
		hash := strings.ToLower(strings.TrimSpace(it.InfoHash))
		if !magnetHashRe.MatchString(hash) {
			continue
		}
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = strings.TrimSpace(it.GUID)
		}
		if title == "" {
			continue
		}
		out = append(out, model.ProviderResult{
			Title:     title,
			Name:      title,
			Magnet:    "magnet:?xt=urn:btih:" + hash,
			InfoHash:  hash,
			Size:      normalizeSize(it.Size),
			Source:    base,
			UpdatedAt: strings.TrimSpace(it.PubDate),
			Adult:     c.adult || strings.Contains(strings.ToLower(it.Category), "adult"),
		})
	}
	return out, nil
}

func normalizeSize(s string) string {
	return strings.NewReplacer(
		"TiB", "TB", "GiB", "GB", "MiB", "MB", "KiB", "KB",
		"tib", "TB", "gib", "GB", "mib", "MB", "kib", "KB",
	).Replace(strings.TrimSpace(s))
}
