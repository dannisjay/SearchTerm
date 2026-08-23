// Package tgbot runs Telegram bots configured in the admin backend.
package tgbot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"searchterm/internal/config"
	"searchterm/internal/model"
	"searchterm/internal/p115"
	"searchterm/internal/search"
	"searchterm/internal/store"
)

const (
	maxResultsPerSearch = 30
	resultsPerPage      = 6
	pollTimeoutSec      = 30
)

var downloadStartRe = regexp.MustCompile(`(?i)magnet:\?|ed2k://`)

type Manager struct {
	cfg config.Config
	st  *store.Store
	svc *search.Service

	mu       sync.Mutex
	bots     map[string]*botRunner
	pending  sync.Map // detailID -> detailPayload
	searches sync.Map // searchID -> *searchResult
}

type botRunner struct {
	cancel context.CancelFunc
}

type detailPayload struct {
	Name   string
	Magnet string
	Links  []string
}

type searchResult struct {
	Query string
	Items []model.SearchItem
}

type update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *message       `json:"message"`
	CallbackQuery *callbackQuery `json:"callback_query"`
}

type message struct {
	MessageID int64  `json:"message_id"`
	Chat      *chat  `json:"chat"`
	From      *user  `json:"from"`
	Text      string `json:"text"`
}

type chat struct {
	ID int64 `json:"id"`
}

type user struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type callbackQuery struct {
	ID      string   `json:"id"`
	From    user     `json:"from"`
	Message *message `json:"message"`
	Data    string   `json:"data"`
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

type inlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

func New(cfg config.Config, st *store.Store, svc *search.Service) *Manager {
	return &Manager{cfg: cfg, st: st, svc: svc, bots: make(map[string]*botRunner)}
}

// Start begins bot polling and periodic reconciliation with stored config.
func (m *Manager) Start(ctx context.Context) {
	m.Reload()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.Reload()
			}
		}
	}()
}

// Reload starts new bots and stops removed/disabled ones.
func (m *Manager) Reload() {
	bots, err := m.st.ListTGBots()
	if err != nil {
		log.Printf("[tgbot] list bots: %v", err)
		return
	}
	current := make(map[string]bool, len(bots))
	for _, b := range bots {
		if !b.Enabled {
			continue
		}
		token, err := m.st.Decrypt(b.TokenEnc)
		if err != nil || token == "" {
			continue
		}
		current[b.ID] = true
		m.mu.Lock()
		if _, ok := m.bots[b.ID]; !ok {
			ctx, cancel := context.WithCancel(context.Background())
			m.bots[b.ID] = &botRunner{cancel: cancel}
			m.mu.Unlock()
			go m.runBot(ctx, b.Name, token)
		} else {
			m.mu.Unlock()
		}
	}
	m.mu.Lock()
	for id, runner := range m.bots {
		if !current[id] {
			runner.cancel()
			delete(m.bots, id)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) runBot(ctx context.Context, name, token string) {
	log.Printf("[tgbot] bot %s started", name)
	var offset int64
	go m.ensureCommands(ctx, token)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[tgbot] bot %s stopped", name)
			return
		default:
		}
		updates, err := m.getUpdates(ctx, token, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[tgbot] bot %s getUpdates: %v", name, err)
			time.Sleep(5 * time.Second)
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			m.handleUpdate(ctx, token, u)
		}
	}
}

func (m *Manager) getUpdates(ctx context.Context, token string, offset int64) ([]update, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=%d", token, offset, pollTimeoutSec)
	var resp struct {
		apiResponse
		Result []update `json:"result"`
	}
	if err := m.call(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("telegram api: %s", resp.Description)
	}
	return resp.Result, nil
}

func (m *Manager) handleUpdate(ctx context.Context, token string, u update) {
	if u.CallbackQuery != nil {
		m.handleCallback(token, u.CallbackQuery)
		return
	}
	msg := u.Message
	if msg == nil || msg.Text == "" || msg.From == nil || msg.Chat == nil {
		return
	}
	if !m.allowed(msg.From.ID) {
		m.sendMessage(token, msg.Chat.ID, "未授权：请先在后台管理中添加你的 Telegram User ID")
		return
	}
	text := strings.TrimSpace(msg.Text)
	switch {
	case text == "/start" || text == "开始":
		m.sendMessage(token, msg.Chat.ID, "欢迎使用 SearchTerm 机器人。\n\n/start 开始\n/help 查看帮助\n/status 系统状态\n\n发送关键词即可搜索，发送磁力或 ed2k 链接可添加到 115 离线下载。")
		return
	case text == "/help" || text == "帮助" || text == "查看帮助":
		m.sendMessage(token, msg.Chat.ID, "使用说明：\n\n1. 直接发送关键词，返回磁力搜索结果。\n2. 发送磁力（magnet:?）或 ed2k:// 链接，可以从混合文本中自动识别全部链接，选择离线目录后添加到 115。\n3. 点击搜索结果下方按钮，可复制磁链或添加到 115。\n\n/status 查看系统状态。")
		return
	case text == "/status" || text == "状态" || text == "系统状态":
		m.handleStatus(token, msg.Chat.ID)
		return
	case strings.HasPrefix(text, "/"):
		return
	}
	if links := ExtractDownloadLinks(text); len(links) > 0 {
		m.handleDownloadLinks(token, msg.Chat.ID, links)
		return
	}
	searchCtx, cancel := context.WithTimeout(ctx, m.cfg.SearchTimeout())
	defer cancel()
	items, err := m.svc.Search(searchCtx, text)
	if err != nil {
		m.sendMessage(token, msg.Chat.ID, "搜索失败："+err.Error())
		return
	}
	if len(items) == 0 {
		m.sendMessage(token, msg.Chat.ID, "没有找到结果："+text)
		return
	}
	if len(items) > maxResultsPerSearch {
		items = items[:maxResultsPerSearch]
	}
	searchID := newID()
	res := &searchResult{Query: text, Items: items}
	m.searches.Store(searchID, res)
	pageText, keyboard := m.buildSearchPage(searchID, res, 1)
	m.sendMessageWithButtons(token, msg.Chat.ID, pageText, keyboard)
}

func (m *Manager) handleStatus(token string, chatID int64) {
	sites, _ := m.st.ListSites()
	settings, _ := m.st.GetSettings()
	enabled := make([]string, 0, len(sites))
	for _, s := range sites {
		if s.Enabled {
			enabled = append(enabled, s.Name)
		}
	}
	if len(enabled) == 0 {
		enabled = append(enabled, "无")
	}
	cookie, _ := m.st.Decrypt(settings.P115Cookie)
	p115State := "未配置"
	if cookie != "" {
		p115State = "已配置"
	}
	msg := fmt.Sprintf("系统状态\n\n启用站点：%s\n115：%s\n离线目录：%d 个", strings.Join(enabled, "、"), p115State, len(settings.P115SavePaths))
	m.sendMessage(token, chatID, msg)
}

func (m *Manager) handleDownloadLinks(token string, chatID int64, links []string) {
	settings, _ := m.st.GetSettings()
	cookie, err := m.st.Decrypt(settings.P115Cookie)
	if err != nil || cookie == "" {
		m.sendMessage(token, chatID, "未配置 115 Cookie，无法添加离线任务")
		return
	}
	paths := settings.P115SavePaths
	if len(paths) == 0 && settings.P115SavePathID != "" {
		paths = []model.P115SavePath{{ID: settings.P115SavePathID, Name: settings.P115SavePathName}}
	}
	if len(paths) == 0 {
		m.sendMessage(token, chatID, "未配置离线目录，请先在后台 115 管理中配置")
		return
	}
	detailID := newID()
	m.pending.Store(detailID, detailPayload{Links: links})
	rows := make([][]inlineButton, 0, len(paths)+1)
	for i, p := range paths {
		rows = append(rows, []inlineButton{{Text: p.Name, CallbackData: fmt.Sprintf("p115:%s:%d", detailID, i)}})
	}
	rows = append(rows, []inlineButton{{Text: "取消", CallbackData: "cancel115:" + detailID}})
	m.sendMessageWithButtons(token, chatID, fmt.Sprintf("识别到 %d 个磁力/ed2k 链接，选择保存到哪个 115 文件夹", len(links)), rows)
}

func ExtractDownloadLinks(text string) []string {
	starts := downloadStartRe.FindAllStringIndex(text, -1)
	var out []string
	seen := make(map[string]bool)
	for i, loc := range starts {
		end := len(text)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		link := trimLinkTail(text[loc[0]:end])
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		out = append(out, link)
	}
	return out
}

func trimLinkTail(s string) string {
	if strings.HasPrefix(strings.ToLower(s), "ed2k://") {
		if idx := strings.LastIndex(s, "|/"); idx >= 0 {
			return s[:idx+2]
		}
	}
	return cutAtFirstNonURL(s)
}

func cutAtFirstNonURL(s string) string {
	for i, r := range s {
		if !isURLChar(r) {
			return s[:i]
		}
	}
	return s
}

func isURLChar(r rune) bool {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return true
	}
	return strings.ContainsRune("-._~:/?#[]@!$&'()*+,;=%|", r)
}

func (m *Manager) ensureCommands(ctx context.Context, token string) {
	for {
		if m.setMyCommands(token) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
		}
	}
}

func (m *Manager) setMyCommands(token string) bool {
	payload := map[string]any{"commands": []map[string]string{
		{"command": "start", "description": "开始"},
		{"command": "help", "description": "查看帮助"},
		{"command": "status", "description": "系统状态"},
	}}
	var resp apiResponse
	if err := m.call(context.Background(), http.MethodPost, fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", token), payload, &resp); err != nil {
		log.Printf("[tgbot] setMyCommands: %v", err)
		return false
	}
	if !resp.OK {
		log.Printf("[tgbot] setMyCommands: %s", resp.Description)
		return false
	}
	return true
}

func (m *Manager) buildSearchPage(searchID string, res *searchResult, page int) (string, [][]inlineButton) {
	items := res.Items
	total := totalPages(len(items))
	if page < 1 {
		page = 1
	}
	if page > total {
		page = total
	}
	start := (page - 1) * resultsPerPage
	end := start + resultsPerPage
	if end > len(items) {
		end = len(items)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "搜索结果：%s（第 %d/%d 页，共 %d 条）", res.Query, page, total, len(items))
	for i := start; i < end; i++ {
		it := items[i]
		fmt.Fprintf(&b, "\n\n%d. %s\n大小：%s\n磁链：<code>%s</code>", i+1, it.Title, it.Size, it.Magnet)
	}
	rows := make([][]inlineButton, 0, 2)
	var nums []inlineButton
	for i := start; i < end; i++ {
		nums = append(nums, inlineButton{
			Text:         strconv.Itoa(i + 1),
			CallbackData: fmt.Sprintf("item:%s:%d", searchID, i),
		})
	}
	if len(nums) > 0 {
		rows = append(rows, nums)
	}
	var nav []inlineButton
	if page > 1 {
		nav = append(nav, inlineButton{Text: "◀ 上一页", CallbackData: fmt.Sprintf("page:%s:%d", searchID, page-1)})
	}
	if page < total {
		nav = append(nav, inlineButton{Text: "下一页 ▶", CallbackData: fmt.Sprintf("page:%s:%d", searchID, page+1)})
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	return b.String(), rows
}

func (m *Manager) handleCallback(token string, cb *callbackQuery) {
	if cb.Message == nil || cb.Message.Chat == nil {
		return
	}
	data := cb.Data
	switch {
	case strings.HasPrefix(data, "page:"):
		parts := strings.SplitN(data, ":", 3)
		if len(parts) == 3 {
			m.handlePage(token, cb, parts[1], parts[2])
		}
	case strings.HasPrefix(data, "item:"):
		parts := strings.SplitN(data, ":", 3)
		if len(parts) == 3 {
			m.handleItem(token, cb, parts[1], parts[2])
		}
	case strings.HasPrefix(data, "copy:"):
		m.handleCopy(token, cb, strings.TrimPrefix(data, "copy:"))
	case strings.HasPrefix(data, "save115:"):
		m.handleSave115Pick(token, cb, strings.TrimPrefix(data, "save115:"))
	case strings.HasPrefix(data, "p115:"):
		parts := strings.SplitN(data, ":", 3)
		if len(parts) == 3 {
			m.handleSave115Confirm(token, cb, parts[1], parts[2])
		}
	case strings.HasPrefix(data, "cancel115:"):
		m.pending.Delete(strings.TrimPrefix(data, "cancel115:"))
		m.answerCallbackQuery(token, cb.ID, "已取消")
	}
}

func (m *Manager) handlePage(token string, cb *callbackQuery, searchID, pageStr string) {
	val, ok := m.searches.Load(searchID)
	if !ok {
		m.answerCallbackQuery(token, cb.ID, "结果已过期，请重新搜索")
		return
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		return
	}
	res := val.(*searchResult)
	text, keyboard := m.buildSearchPage(searchID, res, page)
	m.editMessageText(token, cb.Message.Chat.ID, cb.Message.MessageID, text, keyboard)
	m.answerCallbackQuery(token, cb.ID, fmt.Sprintf("第 %d/%d 页", page, totalPages(len(res.Items))))
}

func (m *Manager) handleItem(token string, cb *callbackQuery, searchID, idxStr string) {
	val, ok := m.searches.Load(searchID)
	if !ok {
		m.answerCallbackQuery(token, cb.ID, "结果已过期，请重新搜索")
		return
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 {
		return
	}
	res := val.(*searchResult)
	if idx >= len(res.Items) {
		m.answerCallbackQuery(token, cb.ID, "结果已过期")
		return
	}
	it := res.Items[idx]
	detailID := newID()
	m.pending.Store(detailID, detailPayload{Name: it.Title, Magnet: it.Magnet})
	text := fmt.Sprintf("%d. %s\n大小：%s\n来源：%s\n磁链：<code>%s</code>",
		idx+1, it.Title, it.Size, it.Site, it.Magnet)
	rows := [][]inlineButton{{
		{Text: "复制磁链", CallbackData: "copy:" + detailID},
		{Text: "添加到115", CallbackData: "save115:" + detailID},
	}}
	m.answerCallbackQuery(token, cb.ID, fmt.Sprintf("已打开第 %d 项", idx+1))
	m.sendMessageWithButtons(token, cb.Message.Chat.ID, text, rows)
}

func (m *Manager) handleCopy(token string, cb *callbackQuery, detailID string) {
	val, ok := m.pending.Load(detailID)
	if !ok {
		m.answerCallbackQuery(token, cb.ID, "链接已过期，请重新搜索")
		return
	}
	payload := val.(detailPayload)
	m.sendMessage(token, cb.Message.Chat.ID, payload.Magnet)
	m.answerCallbackQuery(token, cb.ID, "已发送，长按消息即可复制")
}

func (m *Manager) handleSave115Pick(token string, cb *callbackQuery, detailID string) {
	val, ok := m.pending.Load(detailID)
	if !ok {
		m.answerCallbackQuery(token, cb.ID, "链接已过期，请重新搜索")
		return
	}
	payload := val.(detailPayload)
	settings, _ := m.st.GetSettings()
	paths := settings.P115SavePaths
	if len(paths) == 0 && settings.P115SavePathID != "" {
		paths = []model.P115SavePath{{ID: settings.P115SavePathID, Name: settings.P115SavePathName}}
	}
	if len(paths) == 0 {
		m.answerCallbackQuery(token, cb.ID, "未配置115离线目录")
		return
	}
	rows := make([][]inlineButton, 0, len(paths)+1)
	for i, p := range paths {
		rows = append(rows, []inlineButton{{Text: p.Name, CallbackData: fmt.Sprintf("p115:%s:%d", detailID, i)}})
	}
	rows = append(rows, []inlineButton{{Text: "取消", CallbackData: "cancel115:" + detailID}})
	m.answerCallbackQuery(token, cb.ID, "选择离线目录")
	label := "选择保存到哪个 115 文件夹"
	if len(payload.Links) > 0 {
		label = fmt.Sprintf("共 %d 个链接，选择保存到哪个 115 文件夹", len(payload.Links))
	}
	m.sendMessageWithButtons(token, cb.Message.Chat.ID, label, rows)
}

func (m *Manager) handleSave115Confirm(token string, cb *callbackQuery, detailID, idxStr string) {
	val, ok := m.pending.Load(detailID)
	if !ok {
		m.answerCallbackQuery(token, cb.ID, "链接已过期，请重新搜索")
		return
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 {
		return
	}
	payload := val.(detailPayload)
	settings, _ := m.st.GetSettings()
	paths := settings.P115SavePaths
	if len(paths) == 0 && settings.P115SavePathID != "" {
		paths = []model.P115SavePath{{ID: settings.P115SavePathID, Name: settings.P115SavePathName}}
	}
	if idx >= len(paths) {
		m.answerCallbackQuery(token, cb.ID, "目录无效")
		return
	}
	cookie, err := m.st.Decrypt(settings.P115Cookie)
	if err != nil || cookie == "" {
		m.answerCallbackQuery(token, cb.ID, "未配置115 Cookie")
		return
	}
	path := paths[idx]
	if len(payload.Links) > 0 {
		m.pending.Delete(detailID)
		m.answerCallbackQuery(token, cb.ID, fmt.Sprintf("正在添加 %d 个任务", len(payload.Links)))
		go m.addLinks(token, cb.Message.Chat.ID, payload.Links, path)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := p115.New(cookie)
	if err := client.AddURL(ctx, payload.Magnet, "", path.ID); err != nil {
		m.answerCallbackQuery(token, cb.ID, "添加失败")
		m.sendMessage(token, cb.Message.Chat.ID, "添加到 115 失败："+err.Error())
		return
	}
	m.pending.Delete(detailID)
	msg := "已添加到 115 网盘：" + path.Name
	m.answerCallbackQuery(token, cb.ID, msg)
	m.sendMessage(token, cb.Message.Chat.ID, msg)
}

func (m *Manager) addLinks(token string, chatID int64, links []string, path model.P115SavePath) {
	settings, _ := m.st.GetSettings()
	cookie, err := m.st.Decrypt(settings.P115Cookie)
	if err != nil || cookie == "" {
		m.sendMessage(token, chatID, "未配置 115 Cookie")
		return
	}
	client := p115.New(cookie)
	okCount, failCount := 0, 0
	var firstErr error
	for _, link := range links {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := client.AddURL(ctx, link, "", path.ID)
		cancel()
		if err != nil {
			failCount++
			if firstErr == nil {
				firstErr = err
			}
		} else {
			okCount++
		}
	}
	msg := fmt.Sprintf("115 离线任务添加完成：成功 %d 个，失败 %d 个（目录：%s）", okCount, failCount, path.Name)
	if failCount > 0 && firstErr != nil {
		msg += "\n示例错误：" + firstErr.Error()
	}
	m.sendMessage(token, chatID, msg)
}

func (m *Manager) allowed(tgID int64) bool {
	users, err := m.st.ListTGUsers()
	if err != nil {
		return false
	}
	for _, u := range users {
		if u.Enabled && u.TGID == tgID {
			return true
		}
	}
	return false
}

func (m *Manager) sendMessage(token string, chatID int64, text string) {
	m.sendMessageWithButtons(token, chatID, text, nil)
}

func (m *Manager) sendMessageWithButtons(token string, chatID int64, text string, buttons [][]inlineButton) {
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if len(buttons) > 0 {
		payload["reply_markup"] = map[string]any{"inline_keyboard": buttons}
	}
	var resp apiResponse
	if err := m.call(context.Background(), http.MethodPost,
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token), payload, &resp); err != nil {
		log.Printf("[tgbot] sendMessage: %v", err)
		return
	}
	if !resp.OK {
		log.Printf("[tgbot] sendMessage: %s", resp.Description)
	}
}

func (m *Manager) editMessageText(token string, chatID, messageID int64, text string, buttons [][]inlineButton) {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if len(buttons) > 0 {
		payload["reply_markup"] = map[string]any{"inline_keyboard": buttons}
	}
	var resp apiResponse
	if err := m.call(context.Background(), http.MethodPost,
		fmt.Sprintf("https://api.telegram.org/bot%s/editMessageText", token), payload, &resp); err != nil {
		log.Printf("[tgbot] editMessageText: %v", err)
		return
	}
	if !resp.OK {
		log.Printf("[tgbot] editMessageText: %s", resp.Description)
	}
}

func (m *Manager) answerCallbackQuery(token, queryID, text string) {
	payload := map[string]any{"callback_query_id": queryID, "text": text}
	var resp apiResponse
	if err := m.call(context.Background(), http.MethodPost,
		fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", token), payload, &resp); err != nil {
		log.Printf("[tgbot] answerCallbackQuery: %v", err)
	}
}

func (m *Manager) call(ctx context.Context, method, url string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 70 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func totalPages(n int) int {
	if n <= 0 {
		return 1
	}
	return (n + resultsPerPage - 1) / resultsPerPage
}

func newID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
