package seku

// ---- ONE （一个ONE) App API — heguo, v6 ----
//
// Faithful Go port of the user's uploaded TVBox spider
// __one_____15_j5wk.py (the SECOND upload, user order 2026-10-23
// 20:47 "换这个 py"; it supersedes __one____14_1ddu.py — the two
// differ ONLY in the actress folder navigation, see v6 notes).
// This replaces the v1-v5 simplified ONE (free/preview subset with a
// different signing identity), which stays archived in sites_g5.go.
//
// Recipe (all values from the py source):
//   - ENTRY_HOSTS x5; POST /v2.5/bootstrap (empty form) -> visitor
//     token + domain pools (api/image/video/audio) + server_time +
//     first_day; 600s TTL.
//   - Every API POST: body = base64(AES-128-CBC(urlencode(form)))
//     with API key/iv; headers carry a fresh UUID per request and
//     sign = md5(md5("NA.0.0.0.0.NA.1.<ts>.<user-key>.<uuid>") + SALT)
//     (TreeMap order brand<ip<model<platform<timestamp<user-key<uuid>,
//     brand/model are the literal "NA").
//   - Response: plain JSON if it starts with "{", else base64 +
//     AES-CBC decrypt, first JSON object wins (the server sometimes
//     concatenates two independently encrypted segments).
//   - Categories mirror the py homeContent classes 1:1 (11).
//   - Discovery categories page through a month-jump state machine
//     (the server filters published_at by month; an exhausted month
//     jumps to the previous month down to first_day's month).
//   - Actress cards are FOLDER cards: catalog category "actress/<id>"
//     lists the actress's works as titled cards (py _cat_actor_works),
//     "work/<id>" collapses to the single playable entry card.
//   - Covers/content images are AES-encrypted at rest on the image
//     domains; the py routes them through localProxy for decryption —
//     the core equivalent is oneCoverFetch (decrypt on the cover
//     channel). pics:/wv:/rd: playback schemes (FongMi pics viewer,
//     webview:// article pages, novel:// reader) have NO channel in
//     this app: those episodes are listed in detail (the article text
//     itself rides in the detail description, as in the py) but their
//     Resolve returns an error — see v6 notes for the boundary.
//   - The py's ext options (host/proxy/sort) have no core channel:
//     py defaults are used (sort=published_at). The py's
//     127.0.0.1:9978 shell-proxy fallback exists only to serve image
//     decryption in the shell; in-core it is replaced by CoverFetch,
//     and API traffic is direct with multi-host rotation (the core's
//     jinaFallback is GET-text only and does not apply to these
//     encrypted POSTs).

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- constants (py source values, verbatim) ----

var oneEntryHosts = []string{
	"https://api.mgzsua.com/",
	"https://api.h52ft6.com/",
	"https://api.xeajfv.com/",
	"https://api.phplhr.com/",
	"https://api.5urjzs.com/",
}

const (
	oneSalt       = "m4n2hjPeYWkD6tFpqKF^3HO^h24P@idT"
	oneAppVersion = "2.5.9.2"
	oneUA         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
	oneAppUA      = "okhttp/3.12.1"
)

var (
	oneAPIKey = []byte("l*bv%Ziq000Biaog")
	oneAPIIV  = []byte("8597506002939249")
	oneImgKey = []byte("saIZXc4yMvq0Iz56")
	oneImgIV  = []byte("kbJYtBJUECT0oyjo")
)

// ---- session state (py class-level cache) ----

type oneDetailEntry struct {
	d  map[string]interface{}
	at time.Time
}

type oneCatState struct {
	date   string
	end    string
	page   int
	served int
}

var oneSt = &struct {
	mu         sync.Mutex
	token      string
	domains    map[string][]string
	serverTime string
	firstDay   string
	bootTS     time.Time
	apiHost    string
	detail     map[string]oneDetailEntry
	catState   map[string]*oneCatState
}{domains: map[string][]string{}, firstDay: "2019-11-08", detail: map[string]oneDetailEntry{}, catState: map[string]*oneCatState{}}

// ---- signing / transport ----

func oneSignHeaders(token string) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	u := g5UUID4()
	joined := strings.Join([]string{"NA", "0.0.0.0", "NA", "1", ts, "", u}, ".")
	return map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
		"User-Agent":   oneUA,
		"uuid":         u,
		"timestamp":    ts,
		"ip":           "0.0.0.0",
		"user-key":     "",
		"platform":     "1",
		"sign":         g5MD5Hex(g5MD5Hex(joined) + oneSalt),
		"brand":        "NA",
		"model":        "NA",
		"app-version":  oneAppVersion,
		"token":        token,
	}
}

// oneDecodeResp mirrors the py response handling: "{" -> first JSON
// object; else base64 -> AES-CBC decrypt -> first JSON object.
func oneDecodeResp(raw []byte) map[string]interface{} {
	txt := strings.TrimSpace(string(raw))
	if txt == "" {
		return nil
	}
	decodeFirst := func(b []byte) map[string]interface{} {
		dec := json.NewDecoder(strings.NewReader(string(b)))
		var m map[string]interface{}
		if err := dec.Decode(&m); err != nil {
			return nil
		}
		return m
	}
	if strings.HasPrefix(txt, "{") {
		return decodeFirst([]byte(txt))
	}
	ct, err := base64.StdEncoding.DecodeString(txt)
	if err != nil {
		return nil
	}
	pt, err := g5AESCBCDecrypt(ct, oneAPIKey, oneAPIIV)
	if err != nil {
		return nil
	}
	return decodeFirst(pt)
}

// oneRawPost is the py _raw_post: returns (json, isAuth).
func oneRawPost(host, path string, form url.Values, token string) (map[string]interface{}, bool) {
	plain := ""
	if form != nil {
		plain = form.Encode()
	}
	ct, err := g5AESCBCEncrypt([]byte(plain), oneAPIKey, oneAPIIV)
	if err != nil {
		return nil, false
	}
	body := base64.StdEncoding.EncodeToString(ct)
	raw, code, _, err := httpDo("POST", strings.TrimRight(host, "/")+path,
		strings.NewReader(body), oneSignHeaders(token))
	if err != nil {
		return nil, false
	}
	if code == 401 || code == 403 {
		return nil, true
	}
	return oneDecodeResp(raw), false
}

func oneBootstrapLocked(force bool) bool {
	if oneSt.token != "" && !force && time.Since(oneSt.bootTS) < 600*time.Second {
		return true
	}
	hosts := []string{}
	if oneSt.apiHost != "" {
		hosts = append(hosts, oneSt.apiHost)
	}
	hosts = append(hosts, oneEntryHosts...)
	for _, host := range hosts {
		j, _ := oneRawPost(host, "/v2.5/bootstrap", url.Values{}, "")
		if j == nil {
			continue
		}
		if code, ok := g5MapInt(j, "code"); !ok || code != 200 {
			continue
		}
		d, _ := j["data"].(map[string]interface{})
		if d == nil {
			continue
		}
		if u, _ := d["user"].(map[string]interface{}); u != nil {
			oneSt.token = g5MapStr(u, "token")
		}
		if dom, _ := d["domain"].(map[string]interface{}); dom != nil {
			m := map[string][]string{}
			for k, v := range dom {
				if lst := g5StrList(v); len(lst) > 0 {
					m[k] = lst
				}
			}
			oneSt.domains = m
		}
		oneSt.serverTime = g5MapStr(d, "server_time")
		if fd := g5MapStr(d, "first_day"); fd != "" {
			oneSt.firstDay = fd
		}
		oneSt.bootTS = time.Now()
		oneSt.apiHost = host
		if oneSt.token != "" {
			return true
		}
	}
	return oneSt.token != ""
}

func oneAPIHostsLocked() []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(h string) {
		if h == "" {
			return
		}
		if !strings.HasSuffix(h, "/") {
			h += "/"
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	for _, h := range oneSt.domains["api"] {
		add(h)
	}
	add(oneSt.apiHost)
	for _, h := range oneEntryHosts {
		add(h)
	}
	return out
}

// oneAPI mirrors py _api_post: returns j["data"] of the first
// code==200 response across the host ring; nil when none succeeds.
func oneAPI(path string, form url.Values) interface{} {
	oneSt.mu.Lock()
	defer oneSt.mu.Unlock()
	return oneAPILocked(path, form, true)
}

func oneAPILocked(path string, form url.Values, retry bool) interface{} {
	if !oneBootstrapLocked(false) {
		return nil
	}
	for _, host := range oneAPIHostsLocked() {
		j, auth := oneRawPost(host, path, form, oneSt.token)
		if auth {
			if retry {
				oneBootstrapLocked(true)
				return oneAPILocked(path, form, false)
			}
			continue
		}
		if j == nil {
			continue
		}
		if code, ok := g5MapInt(j, "code"); ok && code == 200 {
			return j["data"]
		}
	}
	return nil
}

// ---- domain helpers ----

func oneTodayLocked() string {
	if len(oneSt.serverTime) >= 10 {
		return oneSt.serverTime[:10]
	}
	return time.Now().Format("2006-01-02")
}

func oneDomLocked(kind string, idx int) string {
	lst := oneSt.domains[kind]
	if len(lst) == 0 {
		return ""
	}
	if idx >= len(lst) {
		idx = len(lst) - 1
	}
	return strings.TrimRight(lst[idx], "/")
}

func oneAbsLocked(kind, path string, idx int) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		return path
	}
	d := oneDomLocked(kind, idx)
	if d == "" {
		return path
	}
	return d + path
}

// onePic: py _pic minus the shell-proxy wrap (CoverFetch decrypts).
func onePicLocked(path string) string {
	return oneAbsLocked("image", path, 0)
}

func oneF(pairs ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return v
}

// ---- detail cache (py _detail_cached, 300s, id-bearing dicts only) ----

func oneDetailCached(id int) map[string]interface{} {
	key := strconv.Itoa(id)
	oneSt.mu.Lock()
	if e, ok := oneSt.detail[key]; ok && time.Since(e.at) < 300*time.Second {
		d := e.d
		oneSt.mu.Unlock()
		return d
	}
	oneSt.mu.Unlock()
	data := oneAPI("/v2.5/article/detail", oneF("id", key))
	d, _ := data.(map[string]interface{})
	if d == nil {
		return nil
	}
	if _, ok := d["id"]; !ok {
		return nil
	}
	oneSt.mu.Lock()
	if len(oneSt.detail) > 200 {
		oneSt.detail = map[string]oneDetailEntry{}
	}
	oneSt.detail[key] = oneDetailEntry{d: d, at: time.Now()}
	oneSt.mu.Unlock()
	return d
}

var oneImgRe = regexp.MustCompile(`(?i)<img[^>]+?(?:src|data-src)=["']([^"']+)["']`)

// oneContentImages mirrors py _content_images (needs the image
// domain pool, hence the lock).
func oneContentImages(d map[string]interface{}) []string {
	oneSt.mu.Lock()
	defer oneSt.mu.Unlock()
	content := g5MapStr(d, "content")
	out := []string{}
	for _, m := range oneImgRe.FindAllStringSubmatch(content, -1) {
		s := strings.TrimSpace(m[1])
		if s == "" || strings.HasPrefix(s, "data:") {
			continue
		}
		s = strings.ReplaceAll(s, "_thumbnail", "")
		if strings.HasPrefix(s, "http") {
			out = append(out, s)
		} else {
			out = append(out, oneAbsLocked("image", s, 0))
		}
	}
	return out
}

// ---- list item mapping (py _from_discovery / _from_series) ----

func oneFromDiscovery(slot string, it map[string]interface{}, catName string) map[string]interface{} {
	oneSt.mu.Lock()
	defer oneSt.mu.Unlock()
	pic := g5MapStr(it, "thumbnail", "thumb")
	parts := []string{}
	if v := g5MapStr(it, "subtitle"); v != "" {
		parts = append(parts, v)
	}
	if v := g5MapStr(it, "number"); v != "" {
		parts = append(parts, v)
	}
	if v := g5MapStr(it, "vol"); v != "" {
		parts = append(parts, "VOL."+v)
	}
	if v := g5MapStr(it, "published_at"); v != "" {
		if len(v) > 10 {
			v = v[:10]
		}
		parts = append(parts, v)
	}
	if v := g5MapStr(it, "like_number"); v != "" {
		parts = append(parts, "♥"+v)
	}
	desc := g5StripHTML(g5MapStr(it, "description"))
	if r := []rune(desc); len(r) > 300 {
		desc = string(r[:300])
	}
	return makeDrama(slot, g5MapStr(it, "id"), g5MapStr(it, "title"),
		onePicLocked(pic), strings.Join(parts, " · "), catName, desc)
}

func oneFromSeries(slot string, it map[string]interface{}, catName string) map[string]interface{} {
	oneSt.mu.Lock()
	defer oneSt.mu.Unlock()
	parts := []string{}
	if v := g5MapStr(it, "author"); v != "" {
		parts = append(parts, v)
	}
	if v := g5MapStr(it, "latest_at"); v != "" {
		if len(v) > 10 {
			v = v[:10]
		}
		parts = append(parts, v)
	}
	if v := g5MapStr(it, "like_total"); v != "" {
		parts = append(parts, "♥"+v)
	}
	return makeDrama(slot, "s:"+g5MapStr(it, "id"), g5MapStr(it, "title"),
		onePicLocked(g5MapStr(it, "thumb")), strings.Join(parts, " · "), catName, "")
}

func oneDataList(data interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	switch d := data.(type) {
	case []interface{}:
		for _, e := range d {
			if m, ok := e.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
	case map[string]interface{}:
		if lst, ok := d["list"].([]interface{}); ok {
			for _, e := range lst {
				if m, ok := e.(map[string]interface{}); ok {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// ---- categories: py homeContent classes, 1:1 and in py order ----

var oneCats = []struct{ id, name string }{
	{"video", "影视"}, {"graphic", "图文"}, {"article", "阅读"},
	{"audio", "电台"}, {"photo_art", "写真"}, {"comic_art", "漫画"},
	{"photo", "写真图集"}, {"comic", "漫画合集"}, {"serial", "合集大全"},
	{"day", "每日ONE"}, {"actor", "女优名录"},
}

var oneDiscovery = map[string][2]int{
	"graphic": {1, 0}, "article": {2, 0}, "video": {3, 0},
	"audio": {5, 0}, "photo_art": {4, 1}, "comic_art": {4, 2},
}

var oneSeriesCats = map[string]string{
	"photo": "/v2.5/series/album/list", "comic": "/v2.5/series/manga/list",
	"serial": "/v2.5/series/list",
}

func oneCatName(id string) string {
	for _, c := range oneCats {
		if c.id == id {
			return c.name
		}
	}
	if strings.HasPrefix(id, "actress/") {
		return "女优作品"
	}
	if strings.HasPrefix(id, "work/") {
		return "作品"
	}
	return "一个ONE"
}

func oneCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range oneCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

// ---- catalog ----

func onePrevMonth(d string) string {
	if len(d) < 7 {
		return d
	}
	y, _ := strconv.Atoi(d[0:4])
	m, _ := strconv.Atoi(d[5:7])
	m--
	if m < 1 {
		m = 12
		y--
	}
	return fmt.Sprintf("%04d-%02d-01", y, m)
}

// oneCatDiscovery ports py _cat_discovery (month-jump pager).
func oneCatDiscovery(slot, tid string, pg int) ([]interface{}, int, bool, error) {
	mc := oneDiscovery[tid]
	oneSt.mu.Lock()
	st := oneSt.catState[tid]
	if pg <= 1 || st == nil {
		fd := oneSt.firstDay
		end := fd
		if len(fd) >= 8 {
			end = fd[:8] + "01"
		}
		st = &oneCatState{date: "", end: end, page: 0, served: 0}
		oneSt.catState[tid] = st
	}
	// start date needs a live bootstrap for server_time/first_day
	oneBootstrapLocked(false)
	if st.date == "" {
		st.date = oneTodayLocked()
	}
	firstMonth := oneSt.firstDay
	if len(firstMonth) >= 8 {
		firstMonth = firstMonth[:8] + "01"
	}
	oneSt.mu.Unlock()

	var lastItems []map[string]interface{}
	for i := 0; i < 90; i++ {
		oneSt.mu.Lock()
		if st.served >= pg {
			oneSt.mu.Unlock()
			break
		}
		st.page++
		curDate, curPage := st.date, st.page
		oneSt.mu.Unlock()
		data := oneAPI("/v2.5/article/discovery", oneF(
			"model_id", strconv.Itoa(mc[0]),
			"series_category", strconv.Itoa(mc[1]),
			"published_at", curDate,
			"page", strconv.Itoa(curPage),
			"size", "20"))
		lst := oneDataList(data)
		if len(lst) == 0 {
			oneSt.mu.Lock()
			d := onePrevMonth(st.date)
			if d < firstMonth || d < st.end {
				oneSt.mu.Unlock()
				break
			}
			st.date = d
			st.page = 0
			oneSt.mu.Unlock()
			continue
		}
		lastItems = lst
		oneSt.mu.Lock()
		st.served++
		oneSt.mu.Unlock()
	}
	items := []interface{}{}
	for _, it := range lastItems {
		items = append(items, oneFromDiscovery(slot, it, oneCatName(tid)))
	}
	return items, len(items), len(items) > 0, nil
}

func oneCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	tid := category
	if tid == "" {
		tid = "video"
	}
	if _, ok := oneDiscovery[tid]; ok {
		return oneCatDiscovery(slot, tid, page)
	}
	if path, ok := oneSeriesCats[tid]; ok {
		data := oneAPI(path, oneF("type", "0", "page", strconv.Itoa(page),
			"size", "20", "sort", "published_at"))
		lst := oneDataList(data)
		items := []interface{}{}
		for _, it := range lst {
			items = append(items, oneFromSeries(slot, it, oneCatName(tid)))
		}
		return items, len(items), len(items) > 0, nil
	}
	switch {
	case tid == "day":
		data := oneAPI("/v2.5/article/day", oneF("published_at", oneTodayAfterBoot()))
		var info []map[string]interface{}
		if d, ok := data.(map[string]interface{}); ok {
			info = oneDataList(d["info"])
		}
		items := []interface{}{}
		for _, it := range info {
			items = append(items, oneFromDiscovery(slot, it, "每日ONE"))
		}
		return items, len(items), false, nil
	case tid == "actor":
		data := oneAPI("/v2.5/actor/getHotActors", oneF("page", strconv.Itoa(page), "size", "20"))
		lst := oneDataList(data)
		items := []interface{}{}
		for _, it := range lst {
			items = append(items, oneActorCard(slot, it))
		}
		return items, len(items), len(items) > 0, nil
	case strings.HasPrefix(tid, "actress/"):
		return oneCatActorWorks(slot, strings.TrimPrefix(tid, "actress/"), page)
	case strings.HasPrefix(tid, "work/"):
		return oneCatWork(slot, strings.TrimPrefix(tid, "work/"))
	}
	return []interface{}{}, 0, false, nil
}

func oneTodayAfterBoot() string {
	oneSt.mu.Lock()
	defer oneSt.mu.Unlock()
	oneBootstrapLocked(false)
	return oneTodayLocked()
}

// oneActorCard mirrors py _from_actor (a folder card; in core terms
// the drama's id is the drill category "actress/<id>", and its
// "category" field points at the same drill so the works list is
// reachable as a card page, not only as detail episodes).
func oneActorCard(slot string, it map[string]interface{}) map[string]interface{} {
	oneSt.mu.Lock()
	pic := onePicLocked(g5MapStr(it, "thumb"))
	oneSt.mu.Unlock()
	remark := ""
	if v := g5MapStr(it, "score"); v != "" {
		remark = "♥" + v
	}
	drill := "actress/" + g5MapStr(it, "id")
	d := makeDrama(slot, drill, g5MapStr(it, "main_name", "name"), pic, remark, "女优名录", "")
	d["category"] = drill
	return d
}

// oneCatActorWorks ports py _cat_actor_works (folder card list).
func oneCatActorWorks(slot, aidStr string, pg int) ([]interface{}, int, bool, error) {
	aid, err := strconv.Atoi(aidStr)
	if err != nil {
		return []interface{}{}, 0, false, nil
	}
	items := []interface{}{}
	if pg <= 1 {
		data := oneAPI("/v2.5/actor/detail", oneF("id", strconv.Itoa(aid)))
		if d, ok := data.(map[string]interface{}); ok && d != nil {
			if g5MapStr(d, "main_name", "nickname") != "" {
				oneSt.mu.Lock()
				pic := onePicLocked(g5MapStr(d, "thumb"))
				oneSt.mu.Unlock()
				remark := "演员简介"
				if n := g5MapStr(d, "article_num"); n != "" {
					remark += " · 作品 " + n + " 部"
				}
				items = append(items, makeDrama(slot, "a:"+strconv.Itoa(aid),
					g5MapStr(d, "main_name", "nickname"), pic, remark, "女优作品", ""))
			}
		}
	}
	data := oneAPI("/v2.5/actor/getActorArticles", oneF(
		"id", strconv.Itoa(aid), "page", strconv.Itoa(pg), "size", "20",
		"keyword", "", "sort", ""))
	lst := oneDataList(data)
	for _, it := range lst {
		oneSt.mu.Lock()
		pic := onePicLocked(g5MapStr(it, "thumbnail", "thumb"))
		oneSt.mu.Unlock()
		parts := []string{}
		if v := g5MapStr(it, "published_at"); v != "" {
			if len(v) > 10 {
				v = v[:10]
			}
			parts = append(parts, v)
		}
		if v := strings.TrimSpace(g5MapStr(it, "number")); v != "" && v != "无番号" {
			parts = append(parts, v)
		}
		remark := strings.Join(parts, " · ")
		if remark == "" {
			remark = "点击播放"
		}
		d := makeDrama(slot, "work/"+g5MapStr(it, "id"), g5MapStr(it, "title"),
			pic, remark, "女优作品", "")
		d["category"] = "work/" + g5MapStr(it, "id")
		items = append(items, d)
	}
	return items, len(items), len(lst) >= 20, nil
}

// oneCatWork ports py _cat_work (single playable entry card).
func oneCatWork(slot, widStr string) ([]interface{}, int, bool, error) {
	wid, err := strconv.Atoi(widStr)
	if err != nil {
		return []interface{}{}, 0, false, nil
	}
	d := oneDetailCached(wid)
	if d == nil {
		return []interface{}{}, 0, false, nil
	}
	m, _ := g5MapInt(d, "model_id")
	remark := "点击播放"
	if g5MapStr(d, "video_file") != "" && m != 4 {
		remark = "视频 · 点击播放"
	} else if (g5MapStr(d, "content") != "" && strings.Contains(g5MapStr(d, "content"), "<img")) || m == 4 || m == 1 {
		remark = "图片 · 点击浏览"
	}
	oneSt.mu.Lock()
	pic := onePicLocked(g5MapStr(d, "thumbnail", "thumb"))
	oneSt.mu.Unlock()
	title := g5MapStr(d, "title")
	if title == "" {
		title = "正片"
	}
	item := makeDrama(slot, strconv.Itoa(wid), title, pic, remark, "作品", "")
	return []interface{}{item}, 1, false, nil
}

// ---- detail ----

func oneSeriesChapters(seriesID int) []map[string]interface{} {
	data := oneAPI("/v2.5/series/chapters", oneF("series_id", strconv.Itoa(seriesID)))
	if d, ok := data.(map[string]interface{}); ok && d != nil {
		return oneDataList(d["chapters"])
	}
	return nil
}

func oneSerialVideoEps(seriesID, exclude int) [][2]string {
	chs := oneSeriesChapters(seriesID)
	eps := [][2]string{}
	n := 0
	for _, ch := range chs {
		if n >= 12 {
			break
		}
		cidStr := g5MapStr(ch, "id")
		cid, err := strconv.Atoi(cidStr)
		if cidStr == "" || err != nil || cid == exclude {
			continue
		}
		dd := oneDetailCached(cid)
		if dd == nil || g5MapStr(dd, "video_file") == "" {
			continue
		}
		num, _ := g5MapInt(ch, "chapter")
		eps = append(eps, [2]string{fmt.Sprintf("第%d集", num), "mv:" + cidStr})
		n++
	}
	return eps
}

func oneDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "a:") {
		return oneActorDetail(slot, strings.TrimPrefix(rawID, "a:"))
	}
	if strings.HasPrefix(rawID, "actress/") {
		return oneActorDetail(slot, strings.TrimPrefix(rawID, "actress/"))
	}
	if strings.HasPrefix(rawID, "s:") {
		return oneSeriesDetail(slot, strings.TrimPrefix(rawID, "s:"))
	}
	aid, err := strconv.Atoi(rawID)
	if err != nil {
		return nil, fmt.Errorf("one: bad id %s", rawID)
	}
	d := oneDetailCached(aid)
	if d == nil {
		return nil, fmt.Errorf("one: detail unavailable for %d", aid)
	}
	model, _ := g5MapInt(d, "model_id")
	scat, _ := g5MapInt(d, "series_category")
	seriesID, _ := g5MapInt(d, "series_id")
	title := g5MapStr(d, "title")
	// desc: py joins description + stripped content (<=1200 chars).
	desc := g5StripHTML(g5MapStr(d, "description"))
	if g5MapStr(d, "content") != "" {
		body := g5StripHTML(g5MapStr(d, "content"))
		if body != "" {
			if desc != "" {
				desc += "\n" + body
			} else {
				desc = body
			}
		}
	}
	if r := []rune(desc); len(r) > 1200 {
		desc = string(r[:1200])
	}
	if tags := g5StrList(d["tags"]); len(tags) > 0 {
		if len(tags) > 12 {
			tags = tags[:12]
		}
		desc += "\n标签：" + strings.Join(tags, " · ")
	}
	rparts := []string{}
	if v := g5MapStr(d, "published_at"); v != "" {
		if len(v) > 10 {
			v = v[:10]
		}
		rparts = append(rparts, v)
	}
	if v := g5MapStr(d, "vol"); v != "" {
		rparts = append(rparts, "VOL."+v)
	}
	if v := g5MapStr(d, "like_number"); v != "" {
		rparts = append(rparts, "♥"+v)
	}
	if v := g5MapStr(d, "views"); v != "" {
		rparts = append(rparts, "👁"+v)
	}
	remark := strings.Join(rparts, " · ")

	var epIDs, epNames []string
	switch {
	case model == 5: // audio
		if g5MapStr(d, "audio_file") != "" || g5MapStr(d, "video_file") != "" {
			epIDs = []string{fmt.Sprintf("au:%d", aid)}
			epNames = []string{"正片"}
		} else {
			remark = strings.Trim(remark+" · 暂无音频", " ·")
		}
	case model == 3 || model == 6 || (g5MapStr(d, "video_file") != "" && model != 4 && model != 5):
		eps := [][2]string{}
		if g5MapStr(d, "video_file") != "" {
			eps = append(eps, [2]string{"正片", fmt.Sprintf("mv:%d", aid)})
		}
		if seriesID != 0 {
			eps = append(eps, oneSerialVideoEps(seriesID, aid)...)
		}
		if len(eps) == 0 {
			remark = strings.Trim(remark+" · 暂无直链(仅加密HLS)", " ·")
		}
		for _, e := range eps {
			epNames = append(epNames, e[0])
			epIDs = append(epIDs, e[1])
		}
	case model == 4 || scat == 1 || scat == 2 || model == 1:
		eps := [][2]string{}
		if seriesID != 0 {
			chs := oneSeriesChapters(seriesID)
			for i, ch := range chs {
				if i >= 99 {
					break
				}
				num, ok := g5MapInt(ch, "chapter")
				if !ok || num == 0 {
					num = i + 1
				}
				eps = append(eps, [2]string{fmt.Sprintf("第%d话", num), "pics:" + g5MapStr(ch, "id")})
			}
		}
		if len(eps) == 0 {
			eps = append(eps, [2]string{"全图", fmt.Sprintf("pics:%d", aid)})
		}
		if model == 1 {
			eps = append(eps, [2]string{"图文", fmt.Sprintf("wv:%d", aid)})
			eps = append(eps, [2]string{"文字", fmt.Sprintf("rd:%d", aid)})
		}
		if len(oneContentImages(d)) > 0 || seriesID != 0 || model == 1 {
			for _, e := range eps {
				epNames = append(epNames, e[0])
				epIDs = append(epIDs, e[1])
			}
		}
	case model == 2: // article: wv/pics/rd lines
		epNames = []string{"图文", "全图", "文字"}
		epIDs = []string{fmt.Sprintf("wv:%d", aid), fmt.Sprintf("pics:%d", aid), fmt.Sprintf("rd:%d", aid)}
	}

	oneSt.mu.Lock()
	pic := onePicLocked(g5MapStr(d, "thumbnail", "thumb"))
	oneSt.mu.Unlock()
	drama := makeDrama(slot, rawID, title, pic, remark, oneCatName("video"), desc)
	if len(epIDs) > 0 {
		g5SetChapters(drama, slot, rawID, epIDs, epNames)
	} else {
		drama["chapters"] = []interface{}{}
		drama["episodeList"] = []interface{}{}
		drama["episodes"] = 0
		drama["episodeCount"] = 0
		drama["chapterCount"] = 0
		drama["totalEpisode"] = 0
		drama["totalEpisodes"] = 0
	}
	return drama, nil
}

func oneSeriesDetail(slot, sidStr string) (map[string]interface{}, error) {
	sid, err := strconv.Atoi(sidStr)
	if err != nil {
		return nil, fmt.Errorf("one: bad series id %s", sidStr)
	}
	data := oneAPI("/v2.5/series/chapters", oneF("series_id", strconv.Itoa(sid)))
	d, _ := data.(map[string]interface{})
	if d == nil {
		return nil, fmt.Errorf("one: series unavailable %d", sid)
	}
	chs := oneDataList(d["chapters"])
	if len(chs) == 0 {
		return nil, fmt.Errorf("one: series %d has no chapters", sid)
	}
	kind := "pics"
	label := "图集"
	if first := chs[0]; first != nil {
		if cid, cerr := strconv.Atoi(g5MapStr(first, "id")); cerr == nil {
			if dd := oneDetailCached(cid); dd != nil {
				m, _ := g5MapInt(dd, "model_id")
				sc, _ := g5MapInt(dd, "series_category")
				if g5MapStr(dd, "video_file") != "" && (m == 3 || m == 6) {
					kind = "mv"
					label = "剧集"
				} else if sc == 2 {
					label = "漫画"
				} else if sc == 1 {
					label = "写真"
				}
			}
		}
	}
	epIDs, epNames := []string{}, []string{}
	for i, ch := range chs {
		if i >= 120 {
			break
		}
		num, ok := g5MapInt(ch, "chapter")
		if !ok || num == 0 {
			num = i + 1
		}
		if kind == "pics" {
			epNames = append(epNames, fmt.Sprintf("第%d话", num))
		} else {
			epNames = append(epNames, fmt.Sprintf("第%d集", num))
		}
		epIDs = append(epIDs, kind+":"+g5MapStr(ch, "id"))
	}
	author := g5MapStr(d, "author")
	rparts := []string{}
	if author != "" {
		rparts = append(rparts, author)
	}
	rparts = append(rparts, fmt.Sprintf("共%d章", len(chs)))
	if g5StrVal(d["end"]) == "1" || g5Truthy(d["end"]) {
		rparts = append(rparts, "已完结")
	} else {
		rparts = append(rparts, "连载中")
	}
	desc := fmt.Sprintf("共 %d 个章节，点击集数播放", len(chs))
	if author != "" {
		desc = "作者：" + author + "\n" + desc
	}
	oneSt.mu.Lock()
	pic := onePicLocked(g5MapStr(d, "thumb"))
	oneSt.mu.Unlock()
	drama := makeDrama(slot, "s:"+sidStr, g5MapStr(d, "title"), pic,
		strings.Join(rparts, " · "), label, desc)
	g5SetChapters(drama, slot, "s:"+sidStr, epIDs, epNames)
	return drama, nil
}

func oneActorDetail(slot, aidStr string) (map[string]interface{}, error) {
	aid, err := strconv.Atoi(aidStr)
	if err != nil {
		return nil, fmt.Errorf("one: bad actor id %s", aidStr)
	}
	data := oneAPI("/v2.5/actor/detail", oneF("id", strconv.Itoa(aid)))
	d, _ := data.(map[string]interface{})
	if d == nil {
		return nil, fmt.Errorf("one: actor unavailable %d", aid)
	}
	name := g5MapStr(d, "main_name", "nickname")
	info := []string{}
	for _, kv := range [][2]string{
		{"japanese_name", "日文名"}, {"nickname", "昵称"}, {"height", "身高"},
		{"bust_waist_hip", "三围"}, {"cup_size", "罩杯"}, {"birth_date", "生日"},
	} {
		if v := g5MapStr(d, kv[0]); v != "" {
			info = append(info, kv[1]+": "+v)
		}
	}
	desc := strings.Join(info, "；")
	if v := g5MapStr(d, "desc"); v != "" {
		desc += "\n" + g5StripHTML(v)
	}
	if r := []rune(desc); len(r) > 600 {
		desc = string(r[:600])
	}
	// works (py: first 10 articles probed into playable episodes,
	// episode names are the works' real titles).
	arts := oneAPI("/v2.5/actor/getActorArticles", oneF(
		"id", strconv.Itoa(aid), "page", "1", "size", "10", "keyword", "", "sort", ""))
	lst := oneDataList(arts)
	epIDs, epNames := []string{}, []string{}
	titles := []string{}
	for i, it := range lst {
		titles = append(titles, g5MapStr(it, "title"))
		if i >= 10 {
			continue
		}
		cid, cerr := strconv.Atoi(g5MapStr(it, "id"))
		if cerr != nil {
			continue
		}
		dd := oneDetailCached(cid)
		if dd == nil {
			continue
		}
		m, _ := g5MapInt(dd, "model_id")
		if g5MapStr(dd, "video_file") != "" && m != 4 {
			epNames = append(epNames, g5MapStr(it, "title"))
			epIDs = append(epIDs, "mv:"+g5MapStr(it, "id"))
		} else if (g5MapStr(dd, "content") != "" && strings.Contains(g5MapStr(dd, "content"), "<img")) || m == 4 || m == 1 {
			epNames = append(epNames, g5MapStr(it, "title"))
			epIDs = append(epIDs, "pics:"+g5MapStr(it, "id"))
		}
	}
	if len(titles) > 0 {
		if len(titles) > 20 {
			titles = titles[:20]
		}
		desc += "\n\n作品：" + strings.Join(titles, "、")
	}
	if r := []rune(desc); len(r) > 1500 {
		desc = string(r[:1500])
	}
	oneSt.mu.Lock()
	pic := onePicLocked(g5MapStr(d, "thumb"))
	oneSt.mu.Unlock()
	drama := makeDrama(slot, "a:"+aidStr, name, pic, "演员作品", "女优名录", desc)
	if len(epIDs) > 0 {
		g5SetChapters(drama, slot, "a:"+aidStr, epIDs, epNames)
	}
	return drama, nil
}

// ---- search (py: six model jobs merged, published_at desc) ----

func oneSearch(slot, kw string) ([]interface{}, error) {
	type job struct {
		model, scat int
		hasScat     bool
	}
	jobs := []job{{3, 0, false}, {4, 1, true}, {4, 2, true}, {1, 0, false}, {2, 0, false}, {5, 0, false}}
	results := map[string]map[string]interface{}{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, jb := range jobs {
		wg.Add(1)
		go func(jb job) {
			defer wg.Done()
			form := oneF("keyword", kw, "model_id", strconv.Itoa(jb.model), "page", "1", "size", "20")
			if jb.hasScat {
				form.Set("series_category", strconv.Itoa(jb.scat))
			}
			data := oneAPI("/v2.5/article/search", form)
			for _, it := range oneDataList(data) {
				mu.Lock()
				results[g5MapStr(it, "id")] = it
				mu.Unlock()
			}
		}(jb)
	}
	wg.Wait()
	merged := []map[string]interface{}{}
	for _, it := range results {
		merged = append(merged, it)
	}
	// published_at descending (py sorts by the string value).
	for i := 0; i < len(merged); i++ {
		for j := i + 1; j < len(merged); j++ {
			if g5MapStr(merged[j], "published_at") > g5MapStr(merged[i], "published_at") {
				merged[i], merged[j] = merged[j], merged[i]
			}
		}
	}
	items := []interface{}{}
	for _, it := range merged {
		items = append(items, oneFromDiscovery(slot, it, "搜索"))
	}
	return items, nil
}

// ---- resolve ----

var oneProbeClient = &http.Client{Timeout: 8 * time.Second, Transport: sharedTransport}

// oneProbeMedia ports py _probe_media: HEAD, then Range, over the
// first 3 candidate domains; falls back to the first candidate.
func oneProbeMedia(kind, path string) string {
	oneSt.mu.Lock()
	doms := append([]string{}, oneSt.domains[kind]...)
	oneSt.mu.Unlock()
	if len(doms) == 0 {
		oneSt.mu.Lock()
		d := oneDomLocked(kind, 0)
		oneSt.mu.Unlock()
		if d == "" {
			return path
		}
		return d + path
	}
	cands := []string{}
	for _, dm := range doms {
		cands = append(cands, strings.TrimRight(dm, "/")+path)
	}
	limit := 3
	if len(cands) < limit {
		limit = len(cands)
	}
	for _, u := range cands[:limit] {
		req, err := http.NewRequest("HEAD", u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", oneAppUA)
		resp, err := oneProbeClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 || resp.StatusCode == 206 {
				return u
			}
		}
		req2, err := http.NewRequest("GET", u, nil)
		if err != nil {
			continue
		}
		req2.Header.Set("User-Agent", oneAppUA)
		req2.Header.Set("Range", "bytes=0-0")
		resp2, err := oneProbeClient.Do(req2)
		if err == nil {
			resp2.Body.Close()
			if resp2.StatusCode == 200 || resp2.StatusCode == 206 {
				return u
			}
		}
	}
	return cands[0]
}

func oneResolve(slot, rawID string) (map[string]interface{}, error) {
	switch {
	case strings.HasPrefix(rawID, "mv:"):
		aid, err := strconv.Atoi(strings.TrimPrefix(rawID, "mv:"))
		if err != nil {
			return nil, fmt.Errorf("one: bad mv id")
		}
		d := oneDetailCached(aid)
		vf := ""
		if d != nil {
			vf = g5MapStr(d, "video_file")
		}
		if vf == "" {
			return nil, fmt.Errorf("one: no MP4 direct link (encrypted HLS only)")
		}
		u := oneProbeMedia("video", vf)
		oneSt.mu.Lock()
		ref := oneDomLocked("video", 0)
		oneSt.mu.Unlock()
		if ref == "" {
			ref = "https://46e9nz.mfpt8g.com"
		}
		return playResult(u, slot, rawID, oneAppUA, ref+"/", "mp4", "file"), nil
	case strings.HasPrefix(rawID, "au:"):
		aid, err := strconv.Atoi(strings.TrimPrefix(rawID, "au:"))
		if err != nil {
			return nil, fmt.Errorf("one: bad au id")
		}
		d := oneDetailCached(aid)
		af := ""
		if d != nil {
			af = g5MapStr(d, "audio_file")
			if af == "" {
				af = g5MapStr(d, "video_file")
			}
		}
		if af == "" {
			return nil, fmt.Errorf("one: no audio file")
		}
		u := oneProbeMedia("audio", af)
		oneSt.mu.Lock()
		ref := oneDomLocked("audio", 0)
		oneSt.mu.Unlock()
		return playResult(u, slot, rawID, oneAppUA, ref+"/", "mp4", "file"), nil
	case strings.HasPrefix(rawID, "pics:"), strings.HasPrefix(rawID, "wv:"), strings.HasPrefix(rawID, "rd:"):
		// FongMi-only schemes (pics:// viewer, webview:// article
		// page, novel:// reader) — this app has no such channel.
		// The content itself is browsable via the detail text.
		return nil, fmt.Errorf("one: 图集/图文/文字线路在本应用无播放通道（详情页可读正文）")
	case strings.HasPrefix(rawID, "a:"), strings.HasPrefix(rawID, "s:"),
		strings.HasPrefix(rawID, "actress/"), strings.HasPrefix(rawID, "work/"):
		return nil, fmt.Errorf("one: folder has no play url")
	}
	return nil, fmt.Errorf("one: unknown play id %s", rawID)
}

// ---- cover fetch: image-domain rotation + AES decrypt (py localProxy) ----

var oneImgClient = &http.Client{Timeout: 20 * time.Second, Transport: sharedTransport}

func oneImageMagic(b []byte) bool {
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return true
	}
	if len(b) >= 4 && string(b[:4]) == "\x89PNG" {
		return true
	}
	if len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a") {
		return true
	}
	if len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return true
	}
	return false
}

func oneCoverFetch(rawURL string) ([]byte, error) {
	oneSt.mu.Lock()
	imgDoms := append([]string{}, oneSt.domains["image"]...)
	oneSt.mu.Unlock()
	hosts := []string{rawURL}
	if pu, err := url.Parse(rawURL); err == nil {
		for _, dm := range imgDoms {
			if du, derr := url.Parse(dm); derr == nil && du.Host != "" && du.Host != pu.Host {
				cp := *pu
				cp.Scheme = du.Scheme
				cp.Host = du.Host
				hosts = append(hosts, cp.String())
			}
		}
	}
	if len(hosts) > 4 {
		hosts = hosts[:4]
	}
	var data []byte
	for _, u := range hosts {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", oneAppUA)
		resp, err := oneImgClient.Do(req)
		if err != nil {
			continue
		}
		b, rerr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if rerr == nil && len(b) > 0 {
			data = b
			break
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("one: cover fetch failed")
	}
	if !oneImageMagic(data) {
		if dec, err := g5AESCBCDecrypt(data, oneImgKey, oneImgIV); err == nil && oneImageMagic(dec) {
			data = dec
		}
	}
	return data, nil
}

func oneHeaders() map[string]string {
	return map[string]string{"User-Agent": oneUA}
}

func init() {
	registerSite("heguo", &siteImpl{
		Key: "one", Name: "🔞一个ONE", DefaultCat: "video", HomeURL: oneEntryHosts[0],
		Categories: oneCategories,
		Catalog:    oneCatalog,
		Detail:     oneDetail,
		Resolve:    oneResolve,
		Search:     oneSearch,
		Headers:    oneHeaders,
		CoverFetch: oneCoverFetch,
	}, "one")
}
