package seku

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// sekuDialer 优先 IPv4：不少源站域名会被解析出 2a03:2880::/32 的假 IPv6（污染），
// 双栈拨号先试 v6 会一直等到超时，表现成「这个源打不开」。先拨 tcp4，
// 域名只有 AAAA 记录时立刻失败再回退，不额外耗时。
var sekuDialer = &net.Dialer{
	Timeout:   10 * time.Second,
	KeepAlive: 30 * time.Second,
}

func sekuDial(ctx context.Context, network, address string) (net.Conn, error) {
	if conn, err := sekuDialer.DialContext(ctx, "tcp4", address); err == nil {
		return conn, nil
	}
	return sekuDialer.DialContext(ctx, network, address)
}

// sharedTransport 是 seku 引擎所有请求共用的连接池。
// Go 默认 Transport 的 MaxIdleConnsPerHost 只有 2：一个源站首屏要同时取
// 十几张海报/封面，超出的连接用完即关，下一张又要重新 DNS+TLS 握手，
// 表现就是「海报一张一张慢慢冒」「切分类要等一两秒」。这里放宽到 16 条并
// 延长空闲保持时间，让同一站源的突发请求真正复用已建立的连接。
var sharedTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           sekuDial,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          256,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

var httpClient = &http.Client{Timeout: 20 * time.Second, Transport: sharedTransport}

var noRedirectClient = &http.Client{
	Timeout:   20 * time.Second,
	Transport: sharedTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func httpGet(url string, headers map[string]string) ([]byte, int, http.Header, error) {
	return httpDo("GET", url, nil, headers)
}

// httpGetTimeout is httpGet with a per-request timeout, for fetchers
// whose retry loops would otherwise stack the shared client's 20s
// timeout once per candidate host (v5 device fix: a blackholed first
// host must not eat minutes before the next host is tried).
func httpGetTimeout(url string, headers map[string]string, timeout time.Duration) ([]byte, int, http.Header, error) {
	headers = crackMergedHeaders(url, headers)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	cl := &http.Client{Timeout: timeout, Transport: sharedTransport}
	resp, err := cl.Do(req)
	if err != nil {
		if b, code, hdr, jerr, ok := jinaFallback(url, headers, nil, 0, err); ok {
			return b, code, hdr, jerr
		}
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		if jb, jcode, jhdr, jerr, ok := jinaFallback(url, headers, b, resp.StatusCode, err); ok {
			return jb, jcode, jhdr, jerr
		}
		return nil, resp.StatusCode, resp.Header, err
	}
	if jb, jcode, jhdr, jerr, ok := jinaFallback(url, headers, b, resp.StatusCode, nil); ok {
		return jb, jcode, jhdr, jerr
	}
	return b, resp.StatusCode, resp.Header, nil
}

func httpDo(method, url string, body io.Reader, headers map[string]string) ([]byte, int, http.Header, error) {
	headers = crackMergedHeaders(url, headers)
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if method == "GET" {
			if b, code, hdr, jerr, ok := jinaFallback(url, headers, nil, 0, err); ok {
				return b, code, hdr, jerr
			}
		}
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		// A shielded response whose body stream dies mid-read (seen
		// live with javrate: 403 + unexpected EOF) is still a failed
		// direct fetch — give the smart-route fallback its chance.
		if method == "GET" {
			if jb, jcode, jhdr, jerr, ok := jinaFallback(url, headers, b, resp.StatusCode, err); ok {
				return jb, jcode, jhdr, jerr
			}
		}
		return nil, resp.StatusCode, resp.Header, err
	}
	if method == "GET" {
		if jb, jcode, jhdr, jerr, ok := jinaFallback(url, headers, b, resp.StatusCode, nil); ok {
			return jb, jcode, jhdr, jerr
		}
	}
	return b, resp.StatusCode, resp.Header, nil
}

// ---- smart routing: shared "免代" fetch fallback (v5) ----
//
// What the ZakaTV jar's "免代/免翻" labels actually are (evidence in
// batch/v5work/note: ZakaSec table + class strings): per-site
// official mirror-domain pools with /status.html probing — data,
// not a shared transport — PLUS one genuinely shared channel:
// ZakaShield fetches blocked pages through the public reader proxy
// https://r.jina.ai/. The jar's only private relay (MissAV's
// missav.ws286.ystv.top) is dead (410). So the core-layer equivalent
// is: direct first; when a GET demonstrably fails (transport error,
// or a 401/403/503 carrying a Cloudflare challenge signature), retry
// once through r.jina.ai with x-respond-with: html (raw HTML back).
// Binary/media URLs never take the fallback on transport errors
// (playlists, segments, keys, images are not reader material).
// Per-host negative cache (60s) stops dead hosts from doubling
// latency on every request; there is no positive stickiness —
// direct stays the default route and recovers automatically.
var jinaNegMu sync.Mutex
var jinaNegCache = map[string]time.Time{}

var jinaBinaryExts = []string{".ts", ".m3u8", ".mp4", ".m4s", ".key", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".zip", ".so", ".bin", ".ico"}

func jinaHostOf(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	return u.Host
}

func jinaFallback(rawurl string, headers map[string]string, body []byte, code int, terr error) ([]byte, int, http.Header, error, bool) {
	if strings.Contains(rawurl, "r.jina.ai") {
		return nil, 0, nil, nil, false
	}
	host := jinaHostOf(rawurl)
	if host == "" {
		return nil, 0, nil, nil, false
	}
	trigger := false
	if terr != nil {
		trigger = true
		lower := strings.ToLower(rawurl)
		if q := strings.Index(lower, "?"); q >= 0 {
			lower = lower[:q]
		}
		for _, ext := range jinaBinaryExts {
			if strings.HasSuffix(lower, ext) {
				return nil, 0, nil, nil, false
			}
		}
	} else if (code == 401 || code == 403 || code == 503) && len(body) > 0 && isChallengePage(string(body)) {
		trigger = true
	}
	if !trigger {
		return nil, 0, nil, nil, false
	}
	jinaNegMu.Lock()
	if t, ok := jinaNegCache[host]; ok && time.Since(t) < 60*time.Second {
		jinaNegMu.Unlock()
		return nil, 0, nil, nil, false
	}
	jinaNegMu.Unlock()
	h := map[string]string{"x-respond-with": "html"}
	if headers != nil {
		if ua := headers["User-Agent"]; ua != "" {
			h["User-Agent"] = ua
		}
	}
	req, err := http.NewRequest("GET", "https://r.jina.ai/"+rawurl, nil)
	if err != nil {
		return nil, 0, nil, nil, false
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	cl := &http.Client{Timeout: 30 * time.Second, Transport: sharedTransport}
	resp, err := cl.Do(req)
	if err != nil {
		jinaNegMu.Lock()
		jinaNegCache[host] = time.Now()
		jinaNegMu.Unlock()
		return nil, 0, nil, nil, false
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil || resp.StatusCode != 200 || len(b) == 0 || isChallengePage(string(b)) {
		jinaNegMu.Lock()
		jinaNegCache[host] = time.Now()
		jinaNegMu.Unlock()
		return nil, 0, nil, nil, false
	}
	return b, 200, resp.Header, nil, true
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(s)
}

// ---- slots ----
//
// v4 slot model: the Dart UI has a static directory of 23 SourceSites
// (models.dart, objs.txt: hongguo, ikanbot, a123, hanxiaoquan, guipian,
// huangdou, huangju, yeguo, dsd, huangguo-video, huangguoai, cloudfront,
// yaguo, maoguo, fanguo, guanguo, heguo, xingguo, huaguo, niuguo, wangguo,
// faguo, wuguo). Content can only be attached to ids in that directory —
// dramas whose "source" is an unknown id are dropped by the UI's
// per-source views (LocalStore.allowsSource -> SourceSite.isAvailable).
// The batch build therefore registers every ported site into one of the
// 23 slots (see registry.go slotSites — the one mapping table).

const (
	slotHsck   = "a123"    // 黄色仓库 hsck123.com (ZakaHsckAmns)
	slotDayday = "guipian" // 天天A片 vjav.tube (ZakaDaydayAmns)
)

// dartSlotNames is what the Dart directory displays for each slot id.
// Payloads keep it only as a harmless extra key; the UI never reads the
// name from the core.
var dartSlotNames = map[string]string{
	"hongguo": "红果", "ikanbot": "爱看机器人", "a123": "A123",
	"hanxiaoquan": "韩小圈", "guipian": "鬼片", "huangdou": "黄豆",
	"huangju": "剧果", "yeguo": "野果", "dsd": "帝果",
	"v_javmit": "JAVMIT", "v_vjav": "VJAV",
	"cloudfront": "黄果旧版", "yaguo": "芽果", "maoguo": "猫果",
	"fanguo": "饭果", "guanguo": "观果", "heguo": "河果",
	"xingguo": "星果", "huaguo": "花果", "niuguo": "牛果",
	"wangguo": "网果", "faguo": "发果", "wuguo": "伍果",
}

// slotForSource maps any incoming source spelling to a registered slot.
// Returns "" for sources this core does not implement.
func slotForSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if _, ok := slotSites[s]; ok {
		return s
	}
	if slot, ok := keyToSlot[s]; ok {
		return slot
	}
	return ""
}

func providerForSlot(slot string) string {
	if impl := implForSlot(slot); impl != nil {
		return impl.Key
	}
	return ""
}

func slotDisplayName(slot string) string {
	if n, ok := dartSlotNames[slot]; ok {
		return n
	}
	return slot
}

func defaultCategory(slot string) string {
	if impl := implForSlot(slot); impl != nil {
		return impl.DefaultCat
	}
	return ""
}

func makeDrama(slot, rawID, title, cover, remark, categoryName, desc string) map[string]interface{} {
	cid := dramaID(slot, rawID)
	// Chapter object aligned with the original core's core.Chapter model.
	ep := map[string]interface{}{
		"episodeId": rawID,
		"id":        cid,
		"dramaId":   cid,
		"sourceId":  slot,
		"source":    slot,
		"title":     "正片",
		"name":      "正片",
		"index":     1,
		"episode":   1,
		"url":       "",
		"videoUrl":  "",
		"mediaSize": 0,
	}
	// Drama object aligned with the Dart Drama.fromJson field table
	// (models.dart @0x431688): id, source (default "hongguo"), sourceId,
	// title, description, cover, episodes (int count!), category, vip,
	// metadataSchema, heat, views, onlineDate, tags, releaseStatus, peers.
	// The canonical cover key is "cover" (not coverUrl); both are given.
	d := map[string]interface{}{
		"metadataSchema": 1,
		"dramaId":        cid,
		"id":             cid,
		"vod_id":         rawID,
		"title":          title,
		"name":           title,
		"coverUrl":       cover,
		"cover":          cover,
		"vod_pic":        cover,
		"pic":            cover,
		"thumb":          cover,
		"sourceId":       slot,
		"source":         slot,
		"sourceName":     slotDisplayName(slot),
		"category":       categoryName,
		"categoryName":   categoryName,
		"typeName":       categoryName,
		"remark":         remark,
		"remarks":        remark,
		"vod_remarks":    remark,
		"desc":           desc,
		"description":    desc,
		"intro":          desc,
		"episodeCount":   1,
		"chapterCount":   1,
		"totalEpisode":   1,
		"totalEpisodes":  1,
		"episodes":       1,
		"chapters":       []interface{}{ep},
		"episodeList":    []interface{}{ep},
		"vip":            false,
		"tags":           []interface{}{},
	}
	return d
}

func categoryObj(id, name string) map[string]interface{} {
	return map[string]interface{}{
		"category":     id,
		"categoryId":   id,
		"id":           id,
		"typeId":       id,
		"type_id":      id,
		"name":         name,
		"categoryName": name,
		"typeName":     name,
		"title":        name,
	}
}

// markerCatID is the v4 slot-identity marker category. The Dart UI's
// 23 slot display names are compiled into libapp.so and cannot be
// renamed safely (variable-length snapshot string rewrite, unverifiable
// without a Flutter runtime — see batch notes v4). Instead every slot's
// category list starts with a marker entry named "全部·<真实站名>"
// that behaves exactly like the slot's default category, so the user
// can see which real site a slot carries right in the tab bar.
const markerCatID = "__all"

func providerCategories(slot string) []interface{} {
	impl := implForSlot(slot)
	if impl == nil || impl.Categories == nil {
		return []interface{}{}
	}
	out := impl.Categories(slot)
	if out == nil {
		return []interface{}{}
	}
	marker := categoryObj(markerCatID, "全部·"+impl.Name)
	return append([]interface{}{marker}, out...)
}

// providerCategoryIDs returns just the ids, in display order.
func providerCategoryIDs(slot string) []string {
	ids := []string{}
	for _, c := range providerCategories(slot) {
		if m, ok := c.(map[string]interface{}); ok {
			if id, _ := m["id"].(string); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func providerCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	impl := implForSlot(slot)
	if impl == nil || impl.Catalog == nil {
		return []interface{}{}, 0, false, fmt.Errorf("unknown provider")
	}
	if category == markerCatID {
		category = impl.DefaultCat
	}
	return impl.Catalog(slot, category, page)
}

func providerDetail(slot, rawID string) (map[string]interface{}, error) {
	impl := implForSlot(slot)
	if impl == nil || impl.Detail == nil {
		return nil, fmt.Errorf("unknown provider")
	}
	return impl.Detail(slot, rawID)
}

func providerResolve(slot, rawID string) (map[string]interface{}, error) {
	impl := implForSlot(slot)
	if impl == nil || impl.Resolve == nil {
		return nil, fmt.Errorf("unknown provider")
	}
	return impl.Resolve(slot, rawID)
}

func providerSearch(slot, kw string) ([]interface{}, error) {
	impl := implForSlot(slot)
	if impl == nil || impl.Search == nil {
		return []interface{}{}, fmt.Errorf("unknown provider")
	}
	return impl.Search(slot, kw)
}

// providerHomeURL / providerHeaders are used by the health check.
func providerHomeURL(slot string) string {
	if impl := implForSlot(slot); impl != nil {
		return impl.HomeURL
	}
	return ""
}

func providerHeaders(slot string) map[string]string {
	if impl := implForSlot(slot); impl != nil && impl.Headers != nil {
		return impl.Headers()
	}
	return map[string]string{}
}
