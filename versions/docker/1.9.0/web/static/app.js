"use strict";

const $ = (id) => document.getElementById(id);
const WEB_PAGE_SIZE = 10;
const MAX_SAVE_PATHS = 6;
const DEFAULT_AVATAR = "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'><rect width='64' height='64' rx='32' fill='%23e9eef2'/><circle cx='32' cy='25' r='9' fill='%2394a3b8'/><path d='M14 54c2.6-9 10-13 18-13s15.4 4 18 13z' fill='%2394a3b8'/></svg>";
const DEFAULT_BACKGROUND_URL = "";
const SEARCH_HISTORY_KEY = "searchterm.searchHistory";
const MAX_SEARCH_HISTORY = 5;

const SITE_TYPES = [
  { type: "qiwei", name: "七味", desc: "", defaultEnabled: true },
  { type: "gying", name: "观影", desc: "需配置账号", defaultEnabled: false },
  { type: "nyaa", name: "Nyaa", desc: "", defaultEnabled: true },
  { type: "sukebei", name: "Sukebei", desc: "", defaultEnabled: false },
];

let currentSettings = null;
let webResults = [];
let webPage = 1;
let streamItems = new Map();
let streamSiteStatus = new Map();
let save115Target = null;
let currentBotId = "";
let currentTokenMask = "";
let currentCookieMask = "";
let currentSiteCookie = "";
let currentQRCodeTokenMask = "";
let accountAvatar = "";
let qrcodeTimer = null;
let qrcodeState = null;
let siteRecords = [];

let p115State = {
  configured: false,
  savePaths: [],
  path: [{ cid: "0", name: "根目录" }],
  cid: "0",
};

function toast(msg) {
  const el = $("toast");
  el.textContent = msg;
  el.classList.add("show");
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.remove("show"), 2200);
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...opts,
  });
  let data = {};
  try { data = await res.json(); } catch (_) {}
  if (!res.ok) {
    throw new Error(data.error || ("请求失败: " + res.status));
  }
  return data;
}

function getSearchHistory() {
  try {
    const raw = JSON.parse(localStorage.getItem(SEARCH_HISTORY_KEY) || "[]");
    if (!Array.isArray(raw)) return [];
    return raw.filter((x) => typeof x === "string" && x.trim()).slice(0, MAX_SEARCH_HISTORY);
  } catch (_) {
    return [];
  }
}

function setSearchHistory(list) {
  try { localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(list)); } catch (_) {}
}

function addSearchHistory(q) {
  const clean = (q || "").trim();
  if (!clean) return;
  const list = getSearchHistory().filter((x) => x.toLowerCase() !== clean.toLowerCase());
  list.unshift(clean);
  setSearchHistory(list.slice(0, MAX_SEARCH_HISTORY));
}

function renderSearchHistory() {
  const panel = $("search-history");
  const box = $("history-list");
  box.innerHTML = "";
  const list = getSearchHistory();
  if (!list.length) {
    panel.classList.add("hidden");
    return;
  }
  for (const q of list) {
    const chip = document.createElement("span");
    chip.className = "search-history-item";
    const label = document.createElement("button");
    label.type = "button";
    label.className = "search-history-label";
    label.textContent = q;
    label.title = q;
    label.addEventListener("click", () => {
      hideSearchHistory();
      runSearch(q);
    });
    const del = document.createElement("button");
    del.type = "button";
    del.className = "search-history-del";
    del.title = "删除该记录";
    del.textContent = "×";
    del.addEventListener("click", (e) => {
      e.stopPropagation();
      removeSearchHistory(q);
    });
    chip.appendChild(label);
    chip.appendChild(del);
    box.appendChild(chip);
  }
  panel.classList.remove("hidden");
}

function removeSearchHistory(q) {
  setSearchHistory(getSearchHistory().filter((x) => x.toLowerCase() !== q.toLowerCase()));
  renderSearchHistory();
}

function hideSearchHistory() {
  $("search-history").classList.add("hidden");
}

async function init() {
  try {
    const setup = await api("/api/setup/status");
    if (!setup.configured) {
      showSetup();
      return;
    }
    const s = await api("/api/session");
    if (s.authed) {
      showApp();
    } else {
      showLogin();
    }
  } catch (_) {
    showLogin();
  } finally {
    loadAppearance();
  }
}

function showSetup() {
  $("setup-screen").classList.remove("hidden");
  $("login-screen").classList.add("hidden");
  $("app").style.display = "none";
}

function showLogin() {
  $("setup-screen").classList.add("hidden");
  $("login-screen").classList.remove("hidden");
  $("app").classList.remove("hidden");
  $("app").style.display = "none";
}

function showApp() {
  $("setup-screen").classList.add("hidden");
  $("login-screen").style.display = "none";
  $("app").style.display = "block";
  showHome();
  loadSettings();
  loadAccount();
  loadSites();
  loadTGUsers();
  loadTGBots();
  loadP115();
}

function showHome() {
  $("app").classList.remove("admin-mode");
  $("search-form").classList.remove("hidden");
  if (webResults.length) {
    $("filter-bar").classList.remove("hidden");
  } else {
    $("filter-bar").classList.add("hidden");
  }
  $("status-line").classList.remove("hidden");
  $("results").classList.remove("hidden");
  $("pagination").classList.remove("hidden");
  $("admin-panel").classList.add("hidden");
  $("admin-toggle").textContent = "设置中心";
  window.scrollTo({ top: 0, behavior: "smooth" });
}

function showAdmin() {
  $("app").classList.add("admin-mode");
  $("search-form").classList.add("hidden");
  $("filter-bar").classList.add("hidden");
  $("status-line").classList.add("hidden");
  $("results").classList.add("hidden");
  $("pagination").classList.add("hidden");
  $("admin-panel").classList.remove("hidden");
  $("admin-toggle").textContent = "返回首页";
  window.scrollTo({ top: 0, behavior: "smooth" });
}

$("login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  try {
    await api("/api/login", {
      method: "POST",
      body: JSON.stringify({
        username: $("username").value.trim(),
        password: $("password").value,
      }),
    });
    showApp();
  } catch (err) {
    toast(err.message);
  }
});

$("setup-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const username = $("setup-username").value.trim();
  const password = $("setup-password").value;
  const confirmPassword = $("setup-confirm").value;
  if (!username) {
    toast("用户名不能为空");
    return;
  }
  if (password.length < 6) {
    toast("密码至少 6 位");
    return;
  }
  if (password !== confirmPassword) {
    toast("两次输入的密码不一致");
    return;
  }
  try {
    await api("/api/setup/account", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
    await api("/api/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
    showApp();
  } catch (err) {
    toast(err.message);
  }
});

function closeAvatarMenu() {
  $("avatar-menu").classList.add("hidden");
}

$("avatar-img").addEventListener("click", (e) => {
  e.stopPropagation();
  $("avatar-menu").classList.toggle("hidden");
});

document.addEventListener("click", (e) => {
  if (!e.target.closest(".avatar-wrap")) closeAvatarMenu();
});

$("admin-toggle").addEventListener("click", () => {
  if ($("admin-panel").classList.contains("hidden")) {
    showAdmin();
  } else {
    showHome();
  }
});

$("brand-home").addEventListener("click", () => {
  location.reload();
});

$("menu-offline").addEventListener("click", () => {
  closeAvatarMenu();
  openOfflineModal();
});

$("menu-logout").addEventListener("click", async () => {
  closeAvatarMenu();
  await api("/api/logout", { method: "POST" });
  location.reload();
});

async function runSearch(q) {
  q = (q || "").trim();
  if (!q) return;
  $("search-input").value = q;
  addSearchHistory(q);
  webResults = [];
  webPage = 1;
  streamItems = new Map();
  streamSiteStatus = new Map();
  $("results").innerHTML = "";
  $("pagination").innerHTML = "";
  $("filter-bar").classList.remove("hidden");
  $("status-line").textContent = "搜索中，站点返回后依次显示…";
  try {
    await searchStream(q);
  } catch (err) {
    $("status-line").textContent = err.message;
  }
}

$("search-form").addEventListener("submit", (e) => {
  e.preventDefault();
  runSearch($("search-input").value);
});

async function searchStream(q) {
  const resp = await fetch("/api/search/stream?q=" + encodeURIComponent(q), {
    headers: { Accept: "text/event-stream" },
  });
  if (!resp.ok) {
    let data = {};
    try { data = await resp.json(); } catch (_) {}
    throw new Error(data.error || ("请求失败: " + resp.status));
  }
  if (!resp.body) {
    throw new Error("浏览器不支持流式搜索");
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let idx;
    while ((idx = buf.indexOf("\n\n")) >= 0) {
      const frame = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      const dataLine = frame.split("\n").find((l) => l.startsWith("data:"));
      if (!dataLine) continue;
      let evt;
      try { evt = JSON.parse(dataLine.slice(5).trim()); } catch (_) { continue; }
      handleStreamEvent(evt);
    }
  }
}

function handleStreamEvent(evt) {
  if (evt.type === "site") {
    let added = 0;
    for (const item of evt.items || []) {
      const key = item.info_hash;
      if (!key) continue;
      const old = streamItems.get(key);
      if (old) {
        mergeStreamItem(old, item);
      } else {
        streamItems.set(key, item);
        added++;
      }
    }
    streamSiteStatus.set(evt.site, { done: true, count: (evt.items || []).length });
    webResults = Array.from(streamItems.values());
    renderWebPage();
    updateStreamStatus();
  } else if (evt.type === "site_error") {
    streamSiteStatus.set(evt.site, { done: true, count: 0, error: evt.error || "" });
    updateStreamStatus();
  } else if (evt.type === "cached" || evt.type === "done") {
    webResults = evt.items || webResults;
    renderResults(webResults, evt.elapsed_ms || 0);
  }
}

function mergeStreamItem(oldItem, item) {
  if (item.source && !oldItem.source) oldItem.source = item.source;
  if (item.source && !(oldItem.sources || []).includes(item.source)) {
    if (!oldItem.sources) oldItem.sources = [];
    oldItem.sources.push(item.source);
  }
  if (item.site && !oldItem.site) oldItem.site = item.site;
  if (!oldItem.size && item.size) oldItem.size = item.size;
  if (!oldItem.magnet && item.magnet) oldItem.magnet = item.magnet;
  if (!oldItem.title && item.title) oldItem.title = item.title;
  if (!oldItem.updated_at && item.updated_at) oldItem.updated_at = item.updated_at;
  if (!oldItem.year && item.year) oldItem.year = item.year;
  return oldItem;
}

function updateStreamStatus() {
  const parts = [];
  for (const [site, st] of streamSiteStatus) {
    parts.push(st.error ? site + " 搜索失败" : site + " 已返回 " + (st.count || 0) + " 条");
  }
  $("status-line").textContent = (parts.length ? parts.join("，") + "，" : "") + "已显示 " + streamItems.size + " 条，继续搜索中…";
}

$("search-input").addEventListener("focus", renderSearchHistory);
document.addEventListener("click", (e) => {
  if (!e.target.closest(".search-wrap")) hideSearchHistory();
});

function renderResults(items, elapsed) {
  $("status-line").textContent = `共 ${items.length} 条结果，耗时 ${elapsed}ms`;
  renderWebPage();
}

function parseSize(s) {
  if (!s) return 0;
  const m = String(s).match(/([\d.]+)\s*(KB|MB|GB|TB)/i);
  if (!m) return 0;
  const units = { KB: 1024, MB: 1024 ** 2, GB: 1024 ** 3, TB: 1024 ** 4 };
  return parseFloat(m[1]) * units[m[2].toUpperCase()];
}

function parseItemTime(item) {
  const t = item.updated_at || item.time || "";
  const ts = Date.parse(t);
  return Number.isNaN(ts) ? 0 : ts;
}

function renderWebPage() {
  const box = $("results");
  box.innerHTML = "";
  if (!webResults.length) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "没有找到结果";
    box.appendChild(empty);
    $("pagination").innerHTML = "";
    return;
  }
  const siteFilter = $("site-filter").value;
  const sortMode = $("sort-filter").value;
  let filtered = siteFilter ? webResults.filter((it) => it.site === siteFilter) : webResults.slice();
  if (sortMode === "size_desc") filtered.sort((a, b) => parseSize(b.size) - parseSize(a.size));
  if (sortMode === "size_asc") filtered.sort((a, b) => parseSize(a.size) - parseSize(b.size));
  if (sortMode === "time_desc") filtered.sort((a, b) => parseItemTime(b) - parseItemTime(a));
  if (sortMode === "time_asc") filtered.sort((a, b) => parseItemTime(a) - parseItemTime(b));
  if (!filtered.length) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "该站点分类下没有结果";
    box.appendChild(empty);
    $("pagination").innerHTML = "";
    return;
  }
  const total = Math.ceil(filtered.length / WEB_PAGE_SIZE);
  if (webPage > total) webPage = total;
  if (webPage < 1) webPage = 1;
  const start = (webPage - 1) * WEB_PAGE_SIZE;
  const slice = filtered.slice(start, start + WEB_PAGE_SIZE);
  for (const item of slice) {
    box.appendChild(resultCard(item));
  }
  renderPagination(total);
}

function renderPagination(total) {
  const box = $("pagination");
  box.innerHTML = "";
  if (total <= 1) return;
  const bar = document.createElement("div");
  bar.className = "pagination";
  const prev = document.createElement("button");
  prev.textContent = "上一页";
  prev.disabled = webPage <= 1;
  prev.addEventListener("click", () => {
    if (webPage > 1) {
      webPage--;
      renderWebPage();
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
  });
  const info = document.createElement("span");
  info.textContent = `第 ${webPage} / ${total} 页`;
  const next = document.createElement("button");
  next.textContent = "下一页";
  next.disabled = webPage >= total;
  next.addEventListener("click", () => {
    if (webPage < total) {
      webPage++;
      renderWebPage();
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
  });
  bar.appendChild(prev);
  bar.appendChild(info);
  bar.appendChild(next);
  const jump = document.createElement("form");
  jump.className = "page-jump";
  const jumpInput = document.createElement("input");
  jumpInput.type = "number";
  jumpInput.min = "1";
  jumpInput.max = String(total);
  jumpInput.placeholder = "页数";
  const jumpBtn = document.createElement("button");
  jumpBtn.type = "submit";
  jumpBtn.textContent = "跳转";
  jump.appendChild(jumpInput);
  jump.appendChild(jumpBtn);
  jump.addEventListener("submit", (e) => {
    e.preventDefault();
    jumpToPage(jumpInput.value, total);
  });
  bar.appendChild(jump);
  box.appendChild(bar);
}

function jumpToPage(value, total) {
  let page = parseInt(value, 10);
  if (!Number.isInteger(page)) return;
  if (page < 1) page = 1;
  if (page > total) page = total;
  if (page === webPage) return;
  webPage = page;
  renderWebPage();
  window.scrollTo({ top: 0, behavior: "smooth" });
}

function resultCard(item) {
  const card = document.createElement("div");
  card.className = "result";

  const head = document.createElement("div");
  head.className = "result-head";
  const title = document.createElement("div");
  title.className = "result-title";
  title.textContent = item.title || item.name;
  head.appendChild(title);
  if (item.site) {
    head.appendChild(tagEl(item.site, "site"));
  }
  if (item.adult) {
    head.appendChild(tagEl("18+", "adult"));
  }
  card.appendChild(head);

  const meta = document.createElement("div");
  meta.className = "result-meta";
  if (item.size) {
    const size = document.createElement("span");
    size.className = "size";
    size.textContent = item.size;
    meta.appendChild(size);
  }
  if (item.updated_at) {
    meta.appendChild(span(item.updated_at));
  }
  card.appendChild(meta);

  const magnet = document.createElement("div");
  magnet.className = "magnet";
  magnet.textContent = item.magnet;
  card.appendChild(magnet);

  const actions = document.createElement("div");
  actions.className = "row-actions";
  const copyBtn = document.createElement("button");
  copyBtn.className = "primary";
  copyBtn.textContent = "复制磁链";
  copyBtn.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(item.magnet);
      toast("已复制");
    } catch (_) {
      fallbackCopy(item.magnet);
    }
  });
  actions.appendChild(copyBtn);
  const save115 = document.createElement("button");
  save115.textContent = "离线到115";
  if (!p115State.configured || !p115State.savePaths.length) {
    save115.disabled = true;
    save115.title = "请先在后台配置 115 离线目录";
  }
  save115.addEventListener("click", () => {
    if (save115.disabled) return;
    openSave115Modal({ magnet: item.magnet, title: item.title || item.name });
  });
  actions.appendChild(save115);
  card.appendChild(actions);
  return card;
}

function tagEl(text, cls) {
  const tag = document.createElement("span");
  tag.className = "tag" + (cls ? " " + cls : "");
  tag.textContent = text;
  return tag;
}

function span(text) {
  const el = document.createElement("span");
  el.textContent = text;
  return el;
}

function fallbackCopy(text) {
  const ta = document.createElement("textarea");
  ta.value = text;
  document.body.appendChild(ta);
  ta.select();
  try { document.execCommand("copy"); toast("已复制"); } catch (_) { toast("复制失败"); }
  ta.remove();
}

function openSave115Modal(target) {
  if (!p115State.configured || !p115State.savePaths.length) {
    toast("请先在后台配置 115 离线目录");
    return;
  }
  save115Target = target;
  const box = $("save115-options");
  box.innerHTML = "";
  for (const p of p115State.savePaths) {
    const btn = document.createElement("button");
    btn.className = "primary save-path-option";
    btn.textContent = p.name || p.id;
    btn.addEventListener("click", () => save115To(p));
    box.appendChild(btn);
  }
  $("save115-modal").classList.remove("hidden");
}

function closeSave115Modal() {
  $("save115-modal").classList.add("hidden");
  save115Target = null;
}

async function save115To(path) {
  if (!save115Target) return;
  const btn = document.activeElement;
  if (btn) btn.disabled = true;
  try {
    const data = await api("/api/search/save115", {
      method: "POST",
      body: JSON.stringify({
        magnet: save115Target.magnet,
        title: save115Target.title,
        save_path_id: path.id,
      }),
    });
    toast(data.message || "已添加到 115 网盘");
    closeSave115Modal();
  } catch (err) {
    toast(err.message);
    if (btn) btn.disabled = false;
  }
}

$("save115-cancel").addEventListener("click", closeSave115Modal);
$("save115-modal").addEventListener("click", (e) => {
  if (e.target === $("save115-modal")) closeSave115Modal();
});

let offlinePathId = "";

function openOfflineModal() {
  if (!p115State.configured || !p115State.savePaths.length) {
    toast("请先在后台配置 115 离线目录");
    return;
  }
  offlinePathId = "";
  const box = $("offline-path-options");
  box.innerHTML = "";
  for (const p of p115State.savePaths) {
    const label = document.createElement("label");
    label.className = "path-option";
    const radio = document.createElement("input");
    radio.type = "radio";
    radio.name = "offline-path";
    radio.value = p.id;
    radio.checked = !offlinePathId;
    if (!offlinePathId) offlinePathId = p.id;
    radio.addEventListener("change", () => { offlinePathId = p.id; });
    label.appendChild(radio);
    label.appendChild(document.createTextNode(p.name || p.id));
    box.appendChild(label);
  }
  $("offline-modal").classList.remove("hidden");
  $("offline-links").focus();
}

function closeOfflineModal() {
  $("offline-modal").classList.add("hidden");
  $("offline-links").value = "";
}

let networkTimer = null;
let networkTesting = false;

function openNetworkModal() {
  $("network-modal").classList.remove("hidden");
  clearInterval(networkTimer);
  networkTimer = setInterval(runNetworkTest, 5000);
  runNetworkTest();
}

function closeNetworkModal() {
  clearInterval(networkTimer);
  networkTimer = null;
  $("network-modal").classList.add("hidden");
}

async function runNetworkTest() {
  if (networkTesting) return;
  networkTesting = true;
  $("network-status").textContent = "正在测试…";
  try {
    const data = await api("/api/network/test");
    renderNetworkResults(data.results || []);
    const failed = (data.results || []).filter((r) => !r.ok).length;
    $("network-status").textContent = failed ? `测试完成，${failed} 个不可达，5 秒后自动重测` : "全部可达，5 秒后自动重测";
  } catch (err) {
    $("network-status").textContent = err.message;
  } finally {
    networkTesting = false;
  }
}

function renderNetworkResults(results) {
  const box = $("network-results");
  box.innerHTML = "";
  for (const r of results) {
    const row = document.createElement("div");
    row.className = "net-row";
    const name = document.createElement("span");
    name.className = "net-name";
    name.textContent = r.name;
    const value = document.createElement("span");
    value.className = "net-latency" + (r.ok ? " good" : " bad");
    value.textContent = r.ok ? r.latency_ms + " ms" : (r.error || "不可达");
    row.appendChild(name);
    row.appendChild(value);
    box.appendChild(row);
  }
}

$("menu-net").addEventListener("click", () => {
  closeAvatarMenu();
  openNetworkModal();
});
$("network-close").addEventListener("click", closeNetworkModal);
$("network-refresh").addEventListener("click", runNetworkTest);
$("network-modal").addEventListener("click", (e) => {
  if (e.target === $("network-modal")) closeNetworkModal();
});
$("offline-cancel").addEventListener("click", closeOfflineModal);
$("offline-modal").addEventListener("click", (e) => {
  if (e.target === $("offline-modal")) closeOfflineModal();
});

$("offline-submit").addEventListener("click", async () => {
  const text = $("offline-links").value.trim();
  if (!text) {
    toast("请粘贴磁力或 ed2k 链接");
    return;
  }
  const btn = $("offline-submit");
  btn.disabled = true;
  try {
    const data = await api("/api/search/save115_batch", {
      method: "POST",
      body: JSON.stringify({ text, save_path_id: offlinePathId }),
    });
    let msg = data.message || "已添加";
    if (data.failed) msg += "，失败 " + data.failed + " 个";
    toast(msg);
    closeOfflineModal();
  } catch (err) {
    toast(err.message);
  } finally {
    btn.disabled = false;
  }
});

// ---- admin sidebar ----

document.querySelectorAll(".admin-sidebar button").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll(".admin-sidebar button").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    document.querySelectorAll(".admin-content .admin-section").forEach((s) => s.classList.add("hidden"));
    $("tab-" + btn.dataset.tab).classList.remove("hidden");
    const content = document.querySelector(".admin-content");
    if (content) content.scrollTop = 0;
    if (btn.dataset.tab === "p115") {
      renderSavePaths();
      checkP115Cookie(false);
    }
  });
});

// ---- account ----

async function loadAccount() {
  try {
    const data = await api("/api/admin/account");
    $("account-username").value = data.username || "";
    accountAvatar = data.avatar || "";
    const avatarEl = $("avatar-img");
    avatarEl.src = accountAvatar || DEFAULT_AVATAR;
    $("avatar-preview").src = accountAvatar || "";
  } catch (_) {}
}

$("account-avatar-pick").addEventListener("click", () => $("account-avatar-file").click());

$("account-avatar-file").addEventListener("change", (e) => {
  const file = e.target.files && e.target.files[0];
  if (!file) return;
  if (!file.type.startsWith("image/")) {
    toast("请选择图片文件");
    return;
  }
  const reader = new FileReader();
  reader.onload = () => {
    const img = new Image();
    img.onload = () => {
      const size = 128;
      const canvas = document.createElement("canvas");
      canvas.width = size;
      canvas.height = size;
      const ctx = canvas.getContext("2d");
      const scale = Math.max(size / img.width, size / img.height);
      const w = img.width * scale;
      const h = img.height * scale;
      ctx.drawImage(img, (size - w) / 2, (size - h) / 2, w, h);
      accountAvatar = canvas.toDataURL("image/png");
      $("avatar-preview").src = accountAvatar;
    };
    img.src = reader.result;
  };
  reader.readAsDataURL(file);
});

$("account-save").addEventListener("click", async () => {
  const username = $("account-username").value.trim();
  const oldPassword = $("account-old-password").value;
  const newPassword = $("account-new-password").value;
  const confirmPassword = $("account-confirm-password").value;
  if (!username) {
    toast("用户名不能为空");
    return;
  }
  if (newPassword && newPassword !== confirmPassword) {
    toast("两次输入的新密码不一致");
    return;
  }
  try {
    await api("/api/admin/account", {
      method: "PUT",
      body: JSON.stringify({
        username,
        old_password: oldPassword,
        new_password: newPassword,
        avatar: accountAvatar,
      }),
    });
    ["account-old-password", "account-new-password", "account-confirm-password"].forEach((id) => ($(id).value = ""));
    await loadAccount();
    toast("账号设置已保存");
  } catch (err) {
    toast(err.message);
  }
});

function applyBackgroundImage(url) {
  const value = (url || "").trim();
  const bg = value ? `url("${value.replace(/["\\]/g, "")}")` : "";
  document.body.classList.toggle("has-bg", !!value);
  document.body.style.backgroundImage = bg;
  const preview = $("bg-preview");
  if (preview) preview.style.backgroundImage = bg;
}

async function loadAppearance() {
  try {
    const data = await api("/api/public/settings");
    const url = data.background_image_url || DEFAULT_BACKGROUND_URL;
    if ($("bg-image-url")) $("bg-image-url").value = url;
    applyBackgroundImage(url);
    if (siteRecords.length > 0) renderSiteBlocks();
  } catch (_) {}
}

async function loadSettings() {
  try {
    const data = await api("/api/admin/settings");
    currentSettings = data.settings;
    const url = data.settings.background_image_url || DEFAULT_BACKGROUND_URL;
    $("bg-image-url").value = url;
    applyBackgroundImage(url);
  } catch (_) {}
}

async function saveBackground(url) {
  const value = (url == null ? $("bg-image-url").value : url).trim();
  try {
    await api("/api/admin/settings", {
      method: "PUT",
      body: JSON.stringify({ background_image_url: value }),
    });
    applyBackgroundImage(value);
    $("bg-image-url").value = value;
    toast(value ? "背景设置已保存" : "背景图已清除");
  } catch (err) {
    toast(err.message);
  }
}

$("bg-image-save").addEventListener("click", () => saveBackground());
$("bg-image-clear").addEventListener("click", () => saveBackground(""));

// ---- sites ----

async function loadSites() {
  const data = await api("/api/admin/sites");
  siteRecords = data.sites || [];
  renderSiteBlocks();
  renderSiteFilter();
}

function renderSiteFilter() {
  const sel = $("site-filter");
  const current = sel.value;
  sel.innerHTML = "";
  const all = document.createElement("option");
  all.value = "";
  all.textContent = "全部";
  sel.appendChild(all);
  const byName = new Map(siteRecords.filter((s) => s.enabled && s.name).map((s) => [s.name, s]));
  const order = ["七味", "观影", "Nyaa", "Sukebei"];
  for (const name of order) {
    if (!byName.has(name)) continue;
    const opt = document.createElement("option");
    opt.value = name;
    opt.textContent = name;
    sel.appendChild(opt);
  }
  sel.value = byName.has(current) ? current : "";
}

["site-filter", "sort-filter"].forEach((id) => {
  $(id).addEventListener("change", () => {
    webPage = 1;
    renderWebPage();
  });
});

function renderSiteBlocks() {
  const box = $("site-blocks");
  box.innerHTML = "";
  for (const meta of SITE_TYPES) {
    box.appendChild(siteBlock(meta));
  }
}

function siteBlock(meta) {
  const record = siteRecords.find((s) => s.type === meta.type);
  const el = document.createElement("div");
  el.className = "list-item site-block";
  el.dataset.type = meta.type;

  const head = document.createElement("div");
  head.className = "site-block-head";
  const titles = document.createElement("div");
  const name = document.createElement("div");
  name.className = "site-name";
  name.textContent = meta.name;
  if (meta.type === "sukebei") {
    name.appendChild(tagEl("NSFW", "adult"));
  }
  titles.appendChild(name);
  if (meta.desc) {
    const desc = document.createElement("div");
    desc.className = "muted";
    desc.textContent = meta.desc;
    titles.appendChild(desc);
  }
  head.appendChild(titles);

  const gyingFields = document.createElement("div");
  gyingFields.className = "site-account-fields";
  let gyingDetails = null;
  if (meta.type === "gying") {
    gyingDetails = document.createElement("details");
    gyingDetails.className = "gying-config";
    const summary = document.createElement("summary");
    summary.textContent = "观影账号配置";
    gyingDetails.appendChild(summary);
  }
  const toggle = document.createElement("label");
  toggle.className = "switch-inline";
  const checkbox = document.createElement("input");
  checkbox.type = "checkbox";
  checkbox.className = "checkbox";
  checkbox.checked = record ? record.enabled : !!meta.defaultEnabled;
  const toggleText = document.createElement("span");
  toggleText.textContent = checkbox.checked ? "已开启" : "已关闭";
  checkbox.addEventListener("change", () => {
    toggleText.textContent = checkbox.checked ? "已开启" : "已关闭";
    if (gyingDetails) {
      gyingDetails.hidden = !checkbox.checked;
      if (!checkbox.checked) gyingDetails.open = false;
    }
    if (meta.type === "gying") {
      if (!checkbox.checked) saveSiteToggle(meta.type, false);
    } else {
      saveSiteToggle(meta.type, checkbox.checked);
    }
  });
  toggle.appendChild(checkbox);
  toggle.appendChild(toggleText);
  head.appendChild(toggle);
  el.appendChild(head);

  if (meta.type === "gying") {
    gyingFields.appendChild(fieldEl("账号", "site-user-gying", record ? record.username || "" : "", "text", ""));
    const passWrap = document.createElement("div");
    const passLabel = document.createElement("label");
    passLabel.textContent = "密码";
    passWrap.appendChild(passLabel);
    const passRow = document.createElement("div");
    passRow.className = "token-row";
    const passInput = document.createElement("input");
    passInput.id = "site-pass-gying";
    passInput.type = "password";
    passInput.autocomplete = "off";
    passInput.placeholder = "留空表示不修改";
    passInput.value = record ? record.password || "" : "";
    const passEye = document.createElement("button");
    passEye.type = "button";
    passEye.title = "显示/隐藏";
    passEye.textContent = "👁";
    passEye.addEventListener("click", () => {
      passInput.type = passInput.type === "password" ? "text" : "password";
    });
    passRow.appendChild(passInput);
    passRow.appendChild(passEye);
    passWrap.appendChild(passRow);
    gyingFields.appendChild(passWrap);
    const saveRow = document.createElement("div");
    saveRow.className = "row-actions";
    const saveBtn = document.createElement("button");
    saveBtn.className = "primary";
    saveBtn.type = "button";
    saveBtn.textContent = "保存";
    saveBtn.addEventListener("click", () => saveSite("gying", checkbox.checked));
    saveRow.appendChild(saveBtn);
    gyingFields.appendChild(saveRow);
  }
  if (meta.type === "gying") {
    gyingDetails.appendChild(gyingFields);
    gyingDetails.hidden = !checkbox.checked;
    el.appendChild(gyingDetails);
  }

  return el;
}

function fieldEl(labelText, id, value, type, placeholder) {
  const wrap = document.createElement("div");
  const label = document.createElement("label");
  label.textContent = labelText;
  wrap.appendChild(label);
  const input = document.createElement("input");
  input.id = id;
  input.autocomplete = "off";
  input.type = type || "text";
  input.value = value || "";
  if (placeholder) input.placeholder = placeholder;
  wrap.appendChild(input);
  return wrap;
}

async function saveSite(type, enabled) {
  const record = siteRecords.find((s) => s.type === type);
  const body = { type, enabled };
  if (type === "gying") {
    body.username = $("site-user-gying").value.trim();
    body.password = $("site-pass-gying").value;
    if (!record && (!body.username || !body.password)) {
      toast("请填写观影账号和密码");
      return;
    }
  }
  try {
    if (record) {
      await api("/api/admin/sites/" + record.id, { method: "PUT", body: JSON.stringify(body) });
    } else {
      await api("/api/admin/sites", { method: "POST", body: JSON.stringify(body) });
    }
    await loadSites();
    toast("站点已保存");
  } catch (err) {
    toast(err.message);
  }
}

async function saveSiteToggle(type, enabled) {
  const record = siteRecords.find((s) => s.type === type);
  if (type === "gying" && enabled && !record && (!$("site-user-gying").value.trim() || !$("site-pass-gying").value)) {
    toast("请填写观影账号和密码");
    renderSiteBlocks();
    return;
  }
  try {
    if (record) {
      await api("/api/admin/sites/" + record.id, { method: "PUT", body: JSON.stringify({ enabled }) });
    } else {
      await api("/api/admin/sites", { method: "POST", body: JSON.stringify({ type, enabled }) });
    }
    await loadSites();
    toast(enabled ? "已开启" : "已关闭");
  } catch (err) {
    toast(err.message);
    loadSites();
  }
}

// ---- tg ----

async function loadTGUsers() {
  try {
    const data = await api("/api/admin/tg/users");
    const lines = (data.users || []).map((u) => (u.username ? `${u.tg_id},${u.username}` : String(u.tg_id)));
    $("tg-user-ids").value = lines.join(";");
  } catch (_) {}
}

async function loadTGBots() {
  try {
    const data = await api("/api/admin/tg/bots");
    const first = (data.bots || [])[0];
    currentBotId = first ? first.id : "";
    currentTokenMask = first ? first.token || "" : "";
    $("tg-bot-token").value = currentTokenMask;
  } catch (_) {}
}

$("tg-token-eye").addEventListener("click", () => {
  const input = $("tg-bot-token");
  input.type = input.type === "password" ? "text" : "password";
});

$("tg-save").addEventListener("click", async () => {
  const users = [];
  for (const line of $("tg-user-ids").value.split(/[;\n]+/)) {
    const part = line.trim();
    if (!part) continue;
    const [idPart, username = ""] = part.split(",");
    const tgId = Number(idPart.trim());
    if (!Number.isInteger(tgId) || tgId <= 0) {
      toast("无效的 TG ID: " + part);
      return;
    }
    users.push({ username: username.trim(), tg_id: tgId });
  }
  const typedToken = $("tg-bot-token").value.trim();
  try {
    await api("/api/admin/tg/users", { method: "PUT", body: JSON.stringify({ users }) });
    if (typedToken && typedToken !== currentTokenMask) {
      if (currentBotId) {
        await api("/api/admin/tg/bots/" + currentBotId, {
          method: "PUT",
          body: JSON.stringify({ token: typedToken }),
        });
      } else {
        await api("/api/admin/tg/bots", {
          method: "POST",
          body: JSON.stringify({ token: typedToken }),
        });
      }
    }
    await loadTGUsers();
    await loadTGBots();
    if (typedToken) {
      $("tg-bot-token").value = typedToken;
      currentTokenMask = typedToken;
    }
    toast("已保存");
  } catch (err) {
    toast(err.message);
  }
});

// ---- p115 ----

function maskSecret(s) {
  if (!s) return "";
  if (s.length <= 8) return "****";
  return s.slice(0, 8) + "****" + s.slice(-4);
}

async function loadP115() {
  try {
    const data = await api("/api/admin/p115");
    p115State.configured = !!data.configured;
    p115State.savePaths = data.save_paths || [];
    renderSavePaths();
    currentCookieMask = data.configured ? (data.cookie || "") : "";
    $("p115-cookie").value = currentCookieMask;
    currentQRCodeTokenMask = data.qrcode_token || "";
    $("p115-qrcode-token").value = currentQRCodeTokenMask;
    $("p115-qrcode-token").type = "password";
    if (data.qrcode_source) $("p115-qrcode-source").value = data.qrcode_source;
    checkP115Cookie(false);
  } catch (err) {
    console.error("loadP115 failed:", err);
    renderSavePaths();
  }
}

$("p115-cookie-eye").addEventListener("click", () => {
  const input = $("p115-cookie");
  input.type = input.type === "password" ? "text" : "password";
});

$("p115-token-eye").addEventListener("click", () => {
  const input = $("p115-qrcode-token");
  input.type = input.type === "password" ? "text" : "password";
});

$("p115-token-clear").addEventListener("click", async () => {
  try {
    await api("/api/admin/p115", {
      method: "PUT",
      body: JSON.stringify({
        clear_qrcode_token: true,
        qrcode_source: $("p115-qrcode-source").value,
        save_paths: p115State.savePaths,
      }),
    });
    await loadP115();
    toast("二维码令牌已清除");
  } catch (err) {
    toast(err.message);
  }
});

function toggleP115Mode() {
  const qrcode = $("p115-mode").value === "qrcode";
  $("p115-cookie-fields").classList.toggle("hidden", qrcode);
  $("p115-qrcode-fields").classList.toggle("hidden", !qrcode);
}
$("p115-mode").addEventListener("change", toggleP115Mode);
toggleP115Mode();

async function checkP115Cookie(force) {
  const typed = $("p115-cookie").value.trim();
  if (!p115State.configured && !typed && !force) return;
  const status = $("p115-status");
  status.textContent = "正在校验 Cookie…";
  try {
    const data = await api("/api/admin/p115/check", {
      method: "POST",
      body: JSON.stringify({ cookie: typed && typed !== currentCookieMask ? typed : "" }),
    });
    if (data.ok) {
      status.textContent = "✅ Cookie 有效！";
      status.classList.remove("bad");
      status.classList.add("good");
    } else {
      const msg = data.message || "未知错误";
      status.textContent = /网络错误|TLS|timeout|time out/i.test(msg) ? "❌ 网络错误：" + msg : "❌ Cookie 无效：" + msg;
      status.classList.add("bad");
      status.classList.remove("good");
    }
  } catch (err) {
    status.textContent = "❌" + err.message;
    status.classList.add("bad");
    status.classList.remove("good");
  }
}

// ---- qrcode login ----

function closeQrcodeModal() {
  $("qrcode-modal").classList.add("hidden");
  $("qrcode-img").src = "";
  $("qrcode-grab-token").classList.add("hidden");
  qrcodeState = null;
  if (qrcodeTimer) {
    clearTimeout(qrcodeTimer);
    qrcodeTimer = null;
  }
}

async function openQrcodeModal() {
  try {
    const data = await api("/api/admin/p115/qrcode/token");
    qrcodeState = data.token;
    $("qrcode-grab-token").classList.add("hidden");
    $("qrcode-status").textContent = "请使用 115 App 扫码登录";
    $("qrcode-img").src = "https://qrcodeapi.115.com/api/1.0/web/1.0/qrcode?uid=" + encodeURIComponent(qrcodeState.uid);
    $("qrcode-modal").classList.remove("hidden");
    pollQrcodeStatus();
  } catch (err) {
    toast("获取二维码失败: " + err.message);
  }
}

async function pollQrcodeStatus() {
  if (!qrcodeState) return;
  try {
    const data = await api("/api/admin/p115/qrcode/status?uid=" + encodeURIComponent(qrcodeState.uid) +
      "&time=" + encodeURIComponent(qrcodeState.time) + "&sign=" + encodeURIComponent(qrcodeState.sign));
    const st = data.status;
    if (st === 0) {
      $("qrcode-status").textContent = "等待扫码…";
    } else if (st === 1) {
      $("qrcode-status").textContent = "已扫码，请在手机上确认登录";
    } else if (st === 2) {
      $("qrcode-status").textContent = "登录成功，请点击获取 Token";
      $("qrcode-grab-token").classList.remove("hidden");
      return;
    } else if (st === -1) {
      $("qrcode-status").textContent = "二维码已过期，请重新获取";
      closeQrcodeModal();
      return;
    } else if (st === -2) {
      $("qrcode-status").textContent = "已取消扫码";
      closeQrcodeModal();
      return;
    }
    qrcodeTimer = setTimeout(pollQrcodeStatus, 2000);
  } catch (err) {
    qrcodeTimer = setTimeout(pollQrcodeStatus, 2000);
  }
}

async function finishQrcodeLogin() {
  if (!qrcodeState) return;
  const uid = qrcodeState.uid;
  $("p115-qrcode-token").value = uid;
  $("p115-qrcode-token").type = "password";
  currentQRCodeTokenMask = "";
  toast("已获取 Token，请选择设备后点保存配置");
  closeQrcodeModal();
}

$("p115-qrcode").addEventListener("click", openQrcodeModal);
$("qrcode-close").addEventListener("click", closeQrcodeModal);
$("qrcode-grab-token").addEventListener("click", finishQrcodeLogin);
$("qrcode-modal").addEventListener("click", (e) => {
  if (e.target === $("qrcode-modal")) closeQrcodeModal();
});

function renderSavePaths() {
  const box = $("p115-save-paths");
  if (!box) return;
  box.innerHTML = "";
  if (!p115State.savePaths.length) {
    const empty = document.createElement("div");
    empty.className = "muted";
    empty.textContent = "未选择，保存时将使用 115 默认离线目录";
    box.appendChild(empty);
    return;
  }
  for (const p of p115State.savePaths) {
    const chip = document.createElement("span");
    chip.className = "path-chip";
    const label = document.createElement("span");
    label.textContent = p.name || p.id;
    const x = document.createElement("button");
    x.className = "chip-x";
    x.type = "button";
    x.textContent = "×";
    x.title = "取消该目录";
    x.addEventListener("click", () => {
      p115State.savePaths = p115State.savePaths.filter((item) => item.id !== p.id);
      renderSavePaths();
    });
    chip.appendChild(label);
    chip.appendChild(x);
    box.appendChild(chip);
  }
}

function addSavePath(dir) {
  if (!dir || !dir.cid) {
    toast("无效目录");
    return;
  }
  if (p115State.savePaths.some((p) => p.id === dir.cid)) {
    toast("该目录已选择");
    return;
  }
  if (p115State.savePaths.length >= MAX_SAVE_PATHS) {
    toast("最多选择 " + MAX_SAVE_PATHS + " 个离线目录");
    return;
  }
  p115State.savePaths.push({ id: dir.cid, name: dir.name || dir.cid });
  renderSavePaths();
}

$("p115-save").addEventListener("click", async () => {
  try {
    const typed = $("p115-cookie").value.trim();
    const token = $("p115-qrcode-token").value.trim();
    await api("/api/admin/p115", {
      method: "PUT",
      body: JSON.stringify({
        cookie: typed && typed !== currentCookieMask ? typed : "",
        qrcode_token: token && token !== currentQRCodeTokenMask ? token : "",
        qrcode_source: $("p115-qrcode-source").value,
        save_paths: p115State.savePaths,
      }),
    });
    await loadP115();
    toast("已保存");
  } catch (err) {
    toast(err.message);
  }
});

$("p115-root-dir").addEventListener("click", () => loadP115Dirs("0"));
$("p115-select-current").addEventListener("click", () => {
  const current = p115State.path[p115State.path.length - 1];
  addSavePath(current);
});

$("p115-up-dir").addEventListener("click", () => {
  if (p115State.path.length <= 1) {
    loadP115Dirs("0");
    return;
  }
  p115State.path.pop();
  const parent = p115State.path[p115State.path.length - 1];
  loadP115Dirs(parent ? parent.cid : "0");
});

function openP115DirModal() {
  if (!p115State.configured) {
    toast("请先配置 115");
    return;
  }
  $("p115-dir-modal").classList.remove("hidden");
  loadP115Dirs("0");
}

function closeP115DirModal() {
  $("p115-dir-modal").classList.add("hidden");
}

$("p115-config-dirs").addEventListener("click", openP115DirModal);
$("p115-dir-close").addEventListener("click", closeP115DirModal);
$("p115-dir-modal").addEventListener("click", (e) => {
  if (e.target === $("p115-dir-modal")) closeP115DirModal();
});

async function loadP115Dirs(cid) {
  try {
    const data = await api("/api/admin/p115/dirs?cid=" + encodeURIComponent(cid || "0"));
    p115State.cid = data.cid || cid || "0";
    if (cid === "0" || cid === "") {
      p115State.path = [{ cid: "0", name: "根目录" }];
    }
    renderP115Dirs(data.dirs || []);
  } catch (err) {
    toast(err.message);
  }
}

function renderP115Dirs(dirs) {
  const box = $("p115-dir-list");
  box.innerHTML = "";
  $("p115-dir-path").textContent = p115State.path.map((d) => d.name).join(" / ");
  if (!dirs.length) {
    box.appendChild(emptyEl("当前目录下没有子文件夹，可以直接选择当前目录"));
    return;
  }
  for (const dir of dirs) {
    const el = document.createElement("div");
    el.className = "list-item dir-item";
    const selected = p115State.savePaths.some((p) => p.id === dir.cid);
    if (selected) el.classList.add("selected");
    const row = document.createElement("div");
    row.className = "row";
    const info = document.createElement("div");
    const name = document.createElement("div");
    name.textContent = dir.name || dir.cid;
    const sub = document.createElement("div");
    sub.className = "muted";
    sub.textContent = "ID: " + dir.cid;
    info.appendChild(name);
    info.appendChild(sub);
    row.appendChild(info);
    const actions = document.createElement("div");
    actions.className = "row-actions";
    const enter = document.createElement("button");
    enter.textContent = "进入";
    enter.addEventListener("click", () => {
      p115State.path.push({ cid: dir.cid, name: dir.name || dir.cid });
      loadP115Dirs(dir.cid);
    });
    const choose = document.createElement("button");
    choose.className = selected ? "" : "primary";
    choose.textContent = selected ? "已选择" : "选择";
    choose.addEventListener("click", () => {
      if (selected) {
        p115State.savePaths = p115State.savePaths.filter((p) => p.id !== dir.cid);
      } else {
        addSavePath(dir);
      }
      renderSavePaths();
      renderP115Dirs(dirs);
    });
    actions.appendChild(enter);
    actions.appendChild(choose);
    row.appendChild(actions);
    el.appendChild(row);
    box.appendChild(el);
  }
}

function emptyEl(text) {
  const el = document.createElement("div");
  el.className = "empty";
  el.textContent = text;
  return el;
}

init();
