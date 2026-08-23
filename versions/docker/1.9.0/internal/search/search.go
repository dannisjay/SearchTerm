package search

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"searchterm/internal/config"
	"searchterm/internal/model"
)

// Provider is one site adapter registered by the server.
type Provider interface {
	Search(ctx context.Context, keyword string) ([]model.ProviderResult, error)
	Source() string
}

type entry struct {
	id       string
	provider Provider
	name     string
}

type cacheEntry struct {
	results  []model.SearchItem
	expireAt time.Time
}

type outcome struct {
	name    string
	results []model.ProviderResult
	err     error
}

// StreamEvent is one incremental result pushed to the streaming search API.
type StreamEvent struct {
	Type      string             `json:"type"`
	Site      string             `json:"site,omitempty"`
	Items     []model.SearchItem `json:"items,omitempty"`
	Error     string             `json:"error,omitempty"`
	Total     int                `json:"total,omitempty"`
	ElapsedMS int64              `json:"elapsed_ms,omitempty"`
}

type Service struct {
	cfg config.Config

	mu        sync.RWMutex
	providers []entry
	cache     map[string]cacheEntry
}

func New(cfg config.Config) *Service {
	return &Service{cfg: cfg, cache: make(map[string]cacheEntry)}
}

func (s *Service) Register(id, name string, p Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeLocked(id)
	s.providers = append(s.providers, entry{id: id, provider: p, name: name})
}

func (s *Service) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeLocked(id)
}

func (s *Service) removeLocked(id string) {
	for i := range s.providers {
		if s.providers[i].id == id {
			s.providers = append(s.providers[:i], s.providers[i+1:]...)
			return
		}
	}
}

func (s *Service) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.providers)
}

func (s *Service) Search(ctx context.Context, keyword string) ([]model.SearchItem, error) {
	var out []model.SearchItem
	err := s.SearchStream(ctx, keyword, func(ev StreamEvent) error {
		if ev.Type == "done" || ev.Type == "cached" {
			out = ev.Items
		}
		return nil
	})
	return out, err
}

func (s *Service) SearchStream(ctx context.Context, keyword string, emit func(StreamEvent) error) error {
	key := strings.TrimSpace(keyword)
	if key == "" {
		return nil
	}
	if hit, ok := s.cached(key); ok {
		return emit(StreamEvent{Type: "cached", Items: hit, Total: len(hit)})
	}

	s.mu.RLock()
	providers := append([]entry(nil), s.providers...)
	s.mu.RUnlock()

	resultsCh := make(chan outcome, len(providers))
	var wg sync.WaitGroup
	for _, p := range providers {
		wg.Add(1)
		go func(p entry) {
			defer wg.Done()
			results, err := p.provider.Search(ctx, key)
			resultsCh <- outcome{name: p.name, results: results, err: err}
		}(p)
	}
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var outcomes []outcome
	for o := range resultsCh {
		outcomes = append(outcomes, o)
		if o.err != nil {
			log.Printf("[search] provider %s: %v", o.name, o.err)
			if err := emit(StreamEvent{Type: "site_error", Site: o.name, Error: o.err.Error()}); err != nil {
				return err
			}
			continue
		}
		items := make([]model.SearchItem, 0, len(o.results))
		for _, r := range o.results {
			r.Adult = isAdult(r)
			if r.InfoHash == "" {
				continue
			}
			item := model.SearchItem{
				Title:     r.Title,
				Year:      r.Year,
				Name:      r.Name,
				Magnet:    r.Magnet,
				InfoHash:  r.InfoHash,
				Size:      r.Size,
				Site:      o.name,
				Source:    r.Source,
				UpdatedAt: r.UpdatedAt,
				Adult:     r.Adult,
			}
			if item.Title == "" {
				item.Title = r.Name
			}
			items = append(items, item)
		}
		if err := emit(StreamEvent{Type: "site", Site: o.name, Items: items}); err != nil {
			return err
		}
	}

	items := s.mergeResults(outcomes)
	s.setCache(key, items)
	return emit(StreamEvent{Type: "done", Items: items, Total: len(items)})
}

func (s *Service) mergeResults(outcomes []outcome) []model.SearchItem {
	merged := make(map[string]*model.SearchItem)
	var order []string
	for _, o := range outcomes {
		if o.err != nil {
			continue
		}
		for _, r := range o.results {
			r.Adult = isAdult(r)
			if r.InfoHash == "" {
				continue
			}
			item, ok := merged[r.InfoHash]
			if !ok {
				item = &model.SearchItem{
					Title:     r.Title,
					Year:      r.Year,
					Name:      r.Name,
					Magnet:    r.Magnet,
					InfoHash:  r.InfoHash,
					Size:      r.Size,
					Site:      o.name,
					Source:    r.Source,
					UpdatedAt: r.UpdatedAt,
					Adult:     r.Adult,
				}
				if item.Title == "" {
					item.Title = r.Name
				}
				merged[r.InfoHash] = item
				order = append(order, r.InfoHash)
			} else {
				item.Sources = append(item.Sources, r.Source)
				if item.Size == "" && r.Size != "" {
					item.Size = r.Size
				}
				if item.Magnet == "" {
					item.Magnet = r.Magnet
				}
			}
		}
	}

	items := make([]model.SearchItem, 0, len(order))
	for _, h := range order {
		it := merged[h]
		if len(it.Sources) > 0 {
			it.Sources = append(it.Sources, it.Source)
			it.Source = strings.Join(unique(it.Sources), ", ")
		}
		items = append(items, *it)
	}

	sort.SliceStable(items, func(i, j int) bool {
		iSize, iOK := parseSize(items[i].Size)
		jSize, jOK := parseSize(items[j].Size)
		if iOK && jOK {
			return iSize > jSize
		}
		return iOK && !jOK
	})

	return items
}

func (s *Service) cached(key string) ([]model.SearchItem, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.cache[key]
	if !ok || time.Now().After(e.expireAt) {
		return nil, false
	}
	return e.results, true
}

func (s *Service) setCache(key string, items []model.SearchItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) > 256 {
		s.cache = make(map[string]cacheEntry)
	}
	s.cache[key] = cacheEntry{results: items, expireAt: time.Now().Add(s.cfg.SearchCacheTTL())}
}

// ClearCache drops all cached results after site or settings changes.
func (s *Service) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]cacheEntry)
}

// defaultAdultKeywords are built-in title markers used to label 18+ results.
var defaultAdultKeywords = []string{
	"jav", "japanese adult", "av-", "hd av", "无码", "有码", "色戒",
	"成人", "18禁", "porn", "hentai", "oppai", "fc2", "一本道", "sod",
}

func isAdult(r model.ProviderResult) bool {
	if r.Adult {
		return true
	}
	haystack := strings.ToLower(r.Title + " " + r.Name)
	for _, kw := range defaultAdultKeywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(haystack, kw) {
			return true
		}
	}
	return false
}

func unique(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func parseSize(s string) (int64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	var num float64
	var unit string
	if _, err := fmt.Sscanf(s, "%f%s", &num, &unit); err != nil {
		return 0, false
	}
	if num <= 0 {
		return 0, false
	}
	switch unit {
	case "b":
		return int64(num), true
	case "k", "kb":
		return int64(num * 1024), true
	case "m", "mb":
		return int64(num * 1024 * 1024), true
	case "g", "gb":
		return int64(num * 1024 * 1024 * 1024), true
	case "t", "tb":
		return int64(num * 1024 * 1024 * 1024 * 1024), true
	default:
		return 0, false
	}
}
