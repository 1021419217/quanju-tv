package seku

// ---- 懂色帝 DSD (slot dsd) ----
//
// Port of the ZakaTV jar spider ZakaDsdAmns (配方报告:
// batch/v3work/dsd_recipe.md, evidence in v3work/dsd_evidence/).
// MacCMS site, no shield/cookie/account. Play chain: player page
// player_aaaa.url (relative) -> /addons/vplayer/ page -> AAEncode
// script decoded by a Go port of the jar's ZakaAA evaluator ->
// vPath + ?sign=... -> signed AES-128 HLS m3u8.
//
// Deltas vs the jar recipe, from the 2026-10-23 live re-survey:
//   - jar mirrors dsd900.com / dsd900.lol are now parked landing
//     pages (6,244B, would pass the jar's ">500 bytes" liveness
//     check) — liveness here is content-based, and the live mirror
//     www.dsd901.com (not in the jar) is a candidate.
//   - cover anchor changed: list covers are now CSS
//     background-image on .img-bg; the jar's data-src is gone and
//     its <img src> fallback would grab the hits icon, so img src
//     is last-resort with icon exclusions.
//   - the jar's dynamic nav regex matches nothing on the current
//     double-span markup; the fallback table IS the live nav
//     (1 独家精选 / 2 中文字幕 / 4 无码破解, verified 1:1 today), so
//     categories are that static table.
//   - search goes through the MacCMS suggest endpoint like the
//     jar's quick path (the show/wd path caps oddly server-side),
//     with the wd template page as fallback.
//   - the core has no sort-filter channel, so the jar's sort
//     filters (time/score/hits/up) are not surfaced (existing
//     platform-wide downgrade, same as other slots).

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const dsdUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var dsdHosts = []string{
	"https://www.dsd.com.se",
	"https://www.dsd901.com",
	"https://www.dsd900.com",
	"https://www.dsd900.lol",
}

var dsdCats = []struct{ id, name string }{
	{"1", "独家精选"},
	{"2", "中文字幕"},
	{"4", "无码破解"},
}

func dsdCatName(id string) string {
	for _, c := range dsdCats {
		if c.id == id {
			return c.name
		}
	}
	return "懂色帝"
}

func dsdCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range dsdCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func dsdHeaders(referer string) map[string]string {
	h := map[string]string{
		"User-Agent":      dsdUA,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9",
	}
	if referer != "" {
		h["Referer"] = referer
	}
	return h
}

// ---- host walking (content-checked, winner cached) ----

var dsdMu sync.Mutex
var dsdHostCache string

func dsdHostOrder() []string {
	dsdMu.Lock()
	cached := dsdHostCache
	dsdMu.Unlock()
	out := []string{}
	if cached != "" {
		out = append(out, cached)
	}
	for _, h := range dsdHosts {
		if h != cached {
			out = append(out, h)
		}
	}
	return out
}

func dsdSetHost(h string) {
	dsdMu.Lock()
	dsdHostCache = h
	dsdMu.Unlock()
}

func dsdHostSoft() string {
	if o := dsdHostOrder(); len(o) > 0 {
		return o[0]
	}
	return dsdHosts[0]
}

// dsdRealPage rejects the parked-domain landing pages that fooled
// the jar's byte-count check: a real page carries vod paths, the
// player blob, or the AA script.
func dsdRealPage(b string) bool {
	if len(b) <= 500 {
		return false
	}
	if strings.Contains(b, "地址发布页") && !strings.Contains(b, "/index.php/vod/") {
		return false
	}
	return strings.Contains(b, "/index.php/vod/") ||
		strings.Contains(b, "player_aaaa") ||
		strings.Contains(b, "ﾟ") ||
		strings.Contains(b, "vPath")
}

// dsdGet walks the candidate hosts for path (refererPath is resolved
// per host). accept==nil uses dsdRealPage.
func dsdGet(path, refererPath string, accept func(string) bool) (string, string, bool) {
	if accept == nil {
		accept = dsdRealPage
	}
	for _, h := range dsdHostOrder() {
		ref := h + "/"
		if refererPath != "" {
			ref = h + refererPath
		}
		body, code, _, err := httpGet(h+path, dsdHeaders(ref))
		if err == nil && code == 200 && accept(string(body)) {
			dsdSetHost(h)
			return string(body), h, true
		}
	}
	return "", "", false
}

// ---- text helpers ----

var dsdSpaceRe = regexp.MustCompile(`\s+`)

func dsdClean(s string) string {
	s = html.UnescapeString(s)
	s = g6StripTags(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(dsdSpaceRe.ReplaceAllString(s, " "))
}

func dsdAbs(u, host string) string {
	u = strings.TrimSpace(strings.Trim(u, `"'`))
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	if strings.HasPrefix(u, "/") {
		return host + u
	}
	return host + "/" + u
}

func dsdJSONStr(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return ""
}

// ---- list parsing ----

type dsdCard struct {
	vid, title, pic, remark string
}

var (
	dsdCardRe     = regexp.MustCompile(`(?s)<a\s+href="(/index\.php/vod/play/id/(\d+)/sid/(\d+)/nid/(\d+)\.html)"[^>]*class="[^"]*(?:video-surface|video-item)[^"]*"[^>]*>(.*?)</a>`)
	dsdBgRe       = regexp.MustCompile(`background-image:\s*url\(([^)]+)\)`)
	dsdDataSrcRe  = regexp.MustCompile(`data-src="([^"]+)"`)
	dsdImgSrcRe   = regexp.MustCompile(`<img[^>]*\ssrc="([^"]+)"`)
	dsdTitleTxtRe = regexp.MustCompile(`video-title-text[^>]*>([^<]+)`)
	dsdDescRe     = regexp.MustCompile(`(?s)class="video-desc[^"]*"[^>]*>(.*?)</div>`)
	dsdH2Re       = regexp.MustCompile(`(?s)<h2[^>]*>(.*?)</h2>`)
	dsdH1Re       = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	dsdDurRe      = regexp.MustCompile(`video-item-tag-duration"[^>]*>([^<]+)<`)
	dsdHitsRe     = regexp.MustCompile(`(?s)video-item-tag-hits"[^>]*>.*?([\d.]+\s*万?次)`)
	dsdHitsNumRe  = regexp.MustCompile(`video-item-tag-hits"[^>]*>(?:<[^>]+>)*\s*([\d,]+)`)
	dsdPageNumRe  = regexp.MustCompile(`/page/(\d+)\.html`)
)

func dsdPickPic(body, host string) string {
	try := func(re *regexp.Regexp) string {
		m := re.FindStringSubmatch(body)
		if m == nil {
			return ""
		}
		u := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		if u == "" || strings.Contains(u, ".svg") || strings.Contains(u, "loading") || strings.Contains(u, "hits.png") {
			return ""
		}
		return dsdAbs(u, host)
	}
	if p := try(dsdBgRe); p != "" {
		return p
	}
	if p := try(dsdDataSrcRe); p != "" {
		return p
	}
	return try(dsdImgSrcRe)
}

func dsdParseCards(pageHTML, host string) []dsdCard {
	cards := []dsdCard{}
	seen := map[string]bool{}
	for _, m := range dsdCardRe.FindAllStringSubmatch(pageHTML, -1) {
		vid, body := m[2], m[5]
		if seen[vid] {
			continue
		}
		seen[vid] = true
		title := ""
		if tm := dsdTitleTxtRe.FindStringSubmatch(body); tm != nil {
			title = dsdClean(tm[1])
		}
		if title == "" {
			if tm := dsdDescRe.FindStringSubmatch(body); tm != nil {
				title = dsdClean(tm[1])
			}
		}
		if title == "" {
			if tm := dsdH2Re.FindStringSubmatch(body); tm != nil {
				title = dsdClean(tm[1])
			}
		}
		if title == "" {
			title = "未命名"
		}
		remark := ""
		if dm := dsdDurRe.FindStringSubmatch(body); dm != nil {
			remark = dsdClean(dm[1])
		} else if hm := dsdHitsRe.FindStringSubmatch(body); hm != nil {
			remark = hm[1] + "播放"
		} else if hm := dsdHitsNumRe.FindStringSubmatch(body); hm != nil {
			remark = hm[1] + " 播放"
		}
		cards = append(cards, dsdCard{vid: vid, title: title, pic: dsdPickPic(body, host), remark: remark})
	}
	return cards
}

func dsdPageCount(pageHTML string, page int) int {
	pc := page
	for _, m := range dsdPageNumRe.FindAllStringSubmatch(pageHTML, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > pc {
			pc = n
		}
	}
	return pc
}

func dsdCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	tid := category
	ok := false
	for _, c := range dsdCats {
		if c.id == tid {
			ok = true
		}
	}
	if !ok {
		tid = "1"
	}
	if page < 1 {
		page = 1
	}
	path := "/index.php/vod/type/id/" + tid + ".html"
	if page > 1 {
		path = fmt.Sprintf("/index.php/vod/type/id/%s/page/%d.html", tid, page)
	}
	pageHTML, host, got := dsdGet(path, "", nil)
	if !got {
		return nil, 0, false, fmt.Errorf("dsd: fetch failed (type %s page %d)", tid, page)
	}
	cards := dsdParseCards(pageHTML, host)
	items := []interface{}{}
	for _, c := range cards {
		items = append(items, makeDrama(slot, c.vid, c.title, c.pic, c.remark, dsdCatName(tid), ""))
	}
	pc := dsdPageCount(pageHTML, page)
	hasMore := page < pc
	total := pc * 24
	if len(cards) > 0 && pc <= page {
		// jar listPage probe semantics: a non-empty page at the
		// known end still probes one page further.
		hasMore = true
		total = (page + 1) * 24
	}
	return items, total, hasMore, nil
}

// ---- player_aaaa ----

// dsdPlayerAAAA extracts the player_aaaa JSON object by brace
// balancing from its assignment (the jar does the same scan; a
// regex cut would break on nested braces inside strings).
func dsdPlayerAAAA(htmlBody string) map[string]interface{} {
	i := strings.Index(htmlBody, "player_aaaa")
	if i < 0 {
		return nil
	}
	j := strings.Index(htmlBody[i:], "{")
	if j < 0 {
		return nil
	}
	start := i + j
	depth := 0
	inStr, esc := false, false
	for k := start; k < len(htmlBody); k++ {
		c := htmlBody[k]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				var m map[string]interface{}
				if err := json.Unmarshal([]byte(htmlBody[start:k+1]), &m); err == nil {
					return m
				}
				return nil
			}
		}
	}
	return nil
}

var dsdPlayRefRe = regexp.MustCompile(`/vod/play/id/(\d+)/sid/(\d+)/nid/(\d+)`)
var dsdFirstNumRe = regexp.MustCompile(`(\d+)`)

func dsdParsePlayRef(rawID string) (string, string, string) {
	if strings.HasPrefix(rawID, "play:") {
		parts := strings.Split(rawID, ":")
		if len(parts) == 4 {
			return parts[1], parts[2], parts[3]
		}
	}
	if m := dsdPlayRefRe.FindStringSubmatch(rawID); m != nil {
		return m[1], m[2], m[3]
	}
	if m := dsdFirstNumRe.FindStringSubmatch(rawID); m != nil {
		return m[1], "1", "1"
	}
	return rawID, "1", "1"
}

func dsdDetail(slot, rawID string) (map[string]interface{}, error) {
	id, sid, nid := dsdParsePlayRef(rawID)
	playerPath := fmt.Sprintf("/index.php/vod/player/id/%s/sid/%s/nid/%s.html", id, sid, nid)
	playPath := fmt.Sprintf("/index.php/vod/play/id/%s/sid/%s/nid/%s.html", id, sid, nid)
	body, host, got := dsdGet(playerPath, playPath, nil)
	stored := getStore(slot).findDrama(id)
	if !got {
		return nil, fmt.Errorf("dsd: player page fetch failed for %s", id)
	}
	title, pic, desc := "", "", ""
	psid, pnid := sid, nid
	if a := dsdPlayerAAAA(body); a != nil {
		vd, _ := a["vod_data"].(map[string]interface{})
		title = dsdClean(dsdJSONStr(vd["vod_name"]))
		pic = dsdAbs(dsdJSONStr(a["poster"]), host)
		parts := []string{}
		if v := dsdClean(dsdJSONStr(vd["vod_class"])); v != "" {
			parts = append(parts, "类型: "+v)
		}
		if v := dsdClean(dsdJSONStr(vd["vod_actor"])); v != "" {
			parts = append(parts, "女优: "+v)
		}
		if v := dsdClean(dsdJSONStr(vd["vod_director"])); v != "" {
			parts = append(parts, "导演: "+v)
		}
		desc = strings.Join(parts, " · ")
		if s := dsdJSONStr(a["sid"]); s != "" {
			psid = s
		}
		if s := dsdJSONStr(a["nid"]); s != "" {
			pnid = s
		}
	}
	if title == "" {
		if m := dsdH1Re.FindStringSubmatch(body); m != nil {
			title = dsdClean(m[1])
		}
	}
	if stored != nil {
		if title == "" {
			title, _ = stored["title"].(string)
		}
		if pic == "" {
			pic, _ = stored["cover"].(string)
		}
	}
	if title == "" {
		title = "影片 " + id
	}
	d := makeDrama(slot, id, title, pic, "", "懂色帝", desc)
	chapRaw := fmt.Sprintf("play:%s:%s:%s", id, psid, pnid)
	d["chapters"] = []interface{}{
		map[string]interface{}{
			"dramaId": slot + ":" + chapRaw, "episode": 1, "episodeId": chapRaw,
			"id": slot + ":" + chapRaw, "index": 1, "mediaSize": 0,
			"name": "正片", "source": slot, "sourceId": slot, "title": "正片",
			"url": "", "videoUrl": "",
		},
	}
	return d, nil
}

// ---- resolve: player page -> vplayer -> AA decode -> signed m3u8 ----

var dsdSignedMu sync.Mutex
var dsdSignedCache = map[string]struct {
	url string
	at  time.Time
}{}

func dsdCachedSigned(key string) string {
	dsdSignedMu.Lock()
	defer dsdSignedMu.Unlock()
	if e, ok := dsdSignedCache[key]; ok && time.Since(e.at) < 45*time.Second {
		return e.url
	}
	return ""
}

func dsdCacheSigned(key, u string) {
	dsdSignedMu.Lock()
	dsdSignedCache[key] = struct {
		url string
		at  time.Time
	}{u, time.Now()}
	dsdSignedMu.Unlock()
}

var (
	dsdVPathRe = regexp.MustCompile(`vPath\s*=\s*"([^"]+)"`)
	dsdM3u8Re  = regexp.MustCompile(`https?://[^\s"'<>]+\.m3u8[^\s"'<>]*`)
)

func dsdResolve(slot, rawID string) (map[string]interface{}, error) {
	id, sid, nid := dsdParsePlayRef(rawID)
	key := id + "|" + sid + "|" + nid
	if u := dsdCachedSigned(key); u != "" {
		return playResult(u, slot, rawID, dsdUA, dsdHostSoft()+"/", "m3u8", "hls"), nil
	}
	playerPath := fmt.Sprintf("/index.php/vod/player/id/%s/sid/%s/nid/%s.html", id, sid, nid)
	playPath := fmt.Sprintf("/index.php/vod/play/id/%s/sid/%s/nid/%s.html", id, sid, nid)
	body, host, got := dsdGet(playerPath, playPath, nil)
	if !got {
		return nil, fmt.Errorf("dsd: player fetch failed for %s", id)
	}
	a := dsdPlayerAAAA(body)
	if a == nil {
		return nil, fmt.Errorf("dsd: no player_aaaa for %s", id)
	}
	purl := strings.TrimSpace(dsdJSONStr(a["url"]))
	if purl == "" {
		return nil, fmt.Errorf("dsd: empty player url for %s", id)
	}
	final := ""
	if strings.HasPrefix(purl, "http://") || strings.HasPrefix(purl, "https://") {
		final = purl
	} else {
		vpath := "/addons/vplayer/?url=" + url.QueryEscape(purl) + "&jump="
		vbody, vhost, vok := dsdGet(vpath, playerPath, nil)
		if !vok {
			return nil, fmt.Errorf("dsd: vplayer fetch failed for %s", id)
		}
		decoded := dsdAADecode(vbody)
		vp := ""
		if m := dsdVPathRe.FindStringSubmatch(decoded); m != nil {
			vp = m[1]
		}
		if vp == "" {
			if m := dsdM3u8Re.FindStringSubmatch(decoded); m != nil {
				vp = m[0]
			}
		}
		if vp == "" {
			return nil, fmt.Errorf("dsd: no vPath decoded for %s", id)
		}
		host = vhost
		if strings.HasPrefix(vp, "http://") || strings.HasPrefix(vp, "https://") {
			final = vp
		} else {
			final = vhost + vp
		}
	}
	dsdCacheSigned(key, final)
	return playResult(final, slot, rawID, dsdUA, host+"/", "m3u8", "hls"), nil
}

// ---- search: suggest endpoint first, wd template fallback ----

func dsdSearch(slot, kw string) ([]interface{}, error) {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return []interface{}{}, nil
	}
	items := []interface{}{}
	sp := "/index.php/ajax/suggest?mid=1&wd=" + url.QueryEscape(kw) + "&limit=50"
	body, host, ok := dsdGet(sp, "", func(b string) bool {
		return len(b) > 2 && strings.Contains(b, "\"list\"")
	})
	suggestFailed := !ok
	if ok {
		var sj struct {
			List []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
				Pic  string `json:"pic"`
			} `json:"list"`
		}
		if err := json.Unmarshal([]byte(body), &sj); err == nil {
			for _, it := range sj.List {
				items = append(items, makeDrama(slot, strconv.Itoa(it.ID), dsdClean(it.Name), dsdAbs(it.Pic, host), "", "搜索", ""))
			}
		}
	}
	if len(items) == 0 {
		wp := "/index.php/vod/search/page/1/wd/" + url.PathEscape(kw) + ".html"
		b2, h2, ok2 := dsdGet(wp, "", nil)
		if !ok2 {
			if suggestFailed {
				return nil, fmt.Errorf("dsd: search fetch failed")
			}
			return items, nil
		}
		for _, c := range dsdParseCards(b2, h2) {
			items = append(items, makeDrama(slot, c.vid, c.title, c.pic, c.remark, "搜索", ""))
		}
	}
	return items, nil
}

func init() {
	registerSite("dsd", &siteImpl{
		Key: "dsd", Name: "懂色帝", DefaultCat: "1", HomeURL: "https://www.dsd.com.se/",
		Categories: dsdCategories,
		Catalog:    dsdCatalog,
		Detail:     dsdDetail,
		Resolve:    dsdResolve,
		Search:     dsdSearch,
		Headers:    func() map[string]string { return dsdHeaders("https://www.dsd.com.se/") },
	}, "dsd")
}

// ---- ZakaAA port (AAEncode evaluator) ----
//
// Faithful port of the jar's ZakaAA + ZakaAA$Eval, cross-checked
// against dsd_evidence/aa_decode_ref.py (which decoded today's live
// vplayer page). The obfuscated script defines its alphabet through
// kaomoji variables; vars_ recovers their numeric values, the inner
// expression is rewritten to plain tokens, and a tiny recursive
// descent evaluator (numbers, strings, +, -, ^, parens) computes the
// hidden string, which is then JS-unescaped.

var dsdScriptRe = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)

func dsdAADecode(page string) string {
	cands := []string{}
	for _, m := range dsdScriptRe.FindAllStringSubmatch(page, -1) {
		s := m[1]
		if strings.Contains(s, "ﾟ") && utf8.RuneCountInString(s) > 500 {
			cands = append(cands, s)
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		return utf8.RuneCountInString(cands[i]) > utf8.RuneCountInString(cands[j])
	})
	for _, s := range cands {
		if out, ok := dsdAATry(s); ok {
			return dsdUnescapeJS(out)
		}
	}
	return ""
}

var (
	dsdAAOvalRe  = regexp.MustCompile(`o=\s*\(ﾟｰﾟ\)\s*=\s*_\s*=\s*(\d+)`)
	dsdAAThetaRe = regexp.MustCompile(`\(ﾟДﾟ\)\s*=\s*\(ﾟΘﾟ\)\s*=`)
	dsdAAPlusRe  = regexp.MustCompile(`\(ﾟｰﾟ\)\s*\+=\s*\(ﾟΘﾟ\)`)
)

type dsdAAVars struct {
	under, o, half, theta, c int
}

func dsdAAVarsOf(p string) dsdAAVars {
	oval := 3
	if m := dsdAAOvalRe.FindStringSubmatch(p); m != nil {
		oval, _ = strconv.Atoi(m[1])
	}
	theta := 0
	if dsdAAThetaRe.MatchString(p) {
		theta = 1
	}
	v := dsdAAVars{under: oval, o: oval, half: oval, theta: theta, c: 0}
	if dsdAAPlusRe.MatchString(p) {
		v.half = oval + theta
	}
	return v
}

func dsdAAInner(p string) (string, bool) {
	for _, pat := range []string{"(ﾟДﾟ) ['_'] ( (ﾟДﾟ) ['_'] (", "(ﾟДﾟ)['_']((ﾟДﾟ)['_']("} {
		idx := strings.Index(p, pat)
		if idx < 0 {
			continue
		}
		start := idx + len(pat) - 1 // byte index of the final '('
		depth := 0
		for i := start; i < len(p); i++ {
			switch p[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return p[start+1 : i], true
				}
			}
		}
	}
	return "", false
}

func dsdAAToExpr(p string, v dsdAAVars) string {
	for _, pair := range [][2]string{
		{"(ﾟДﾟ) [ﾟεﾟ]", "'\\\\'"},
		{"(ﾟДﾟ)[ﾟεﾟ]", "'\\\\'"},
		{"(ﾟДﾟ) [ﾟoﾟ]", "'\"'"},
		{"(ﾟДﾟ)[ﾟoﾟ]", "'\"'"},
		{"(ﾟДﾟ) [ﾟΘﾟ]", "'_'"},
		{"(ﾟДﾟ)[ﾟΘﾟ]", "'_'"},
	} {
		p = strings.ReplaceAll(p, pair[0], pair[1])
	}
	p = strings.ReplaceAll(p, "(c^_^o)", fmt.Sprintf("(%d^%d^%d)", v.c, v.under, v.o))
	p = strings.ReplaceAll(p, "(o^_^o)", fmt.Sprintf("(%d^%d^%d)", v.o, v.under, v.o))
	p = strings.ReplaceAll(p, "ﾟεﾟ", "'r'")
	p = strings.ReplaceAll(p, "ﾟoﾟ", "'c'")
	p = strings.ReplaceAll(p, "oﾟｰﾟo", "'u'")
	p = strings.ReplaceAll(p, "(ﾟｰﾟ)", fmt.Sprintf("(%d)", v.half))
	p = strings.ReplaceAll(p, "(ﾟΘﾟ)", fmt.Sprintf("(%d)", v.theta))
	p = strings.ReplaceAll(p, "(ﾟДﾟ)", "(0)")
	return p
}

func dsdAAUnesc(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			break
		}
		n := s[i+1]
		switch n {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(n)
		}
		i++
	}
	return b.String()
}

type dsdAATok struct {
	kind byte // 'n' num, 's' str, 'o' op
	num  int
	str  string
}

func dsdAATokenize(p string) []dsdAATok {
	rs := []rune(p)
	toks := []dsdAATok{}
	i := 0
	for i < len(rs) {
		c := rs[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c >= '0' && c <= '9' {
			j := i
			for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
				j++
			}
			n, _ := strconv.Atoi(string(rs[i:j]))
			toks = append(toks, dsdAATok{kind: 'n', num: n})
			i = j
			continue
		}
		if c == '\'' || c == '"' {
			q := c
			j := i + 1
			var inner []rune
			for j < len(rs) {
				if rs[j] == '\\' && j+1 < len(rs) {
					inner = append(inner, rs[j], rs[j+1])
					j += 2
					continue
				}
				if rs[j] == q {
					break
				}
				inner = append(inner, rs[j])
				j++
			}
			toks = append(toks, dsdAATok{kind: 's', str: dsdAAUnesc(string(inner))})
			i = j + 1
			continue
		}
		if strings.ContainsRune("+-^()", c) {
			toks = append(toks, dsdAATok{kind: 'o', str: string(c)})
			i++
			continue
		}
		i++
	}
	return toks
}

type dsdAAEval struct {
	t []dsdAATok
	i int
}

func (e *dsdAAEval) peek() (dsdAATok, bool) {
	if e.i < len(e.t) {
		return e.t[e.i], true
	}
	return dsdAATok{}, false
}

func (e *dsdAAEval) next() (dsdAATok, bool) {
	t, ok := e.peek()
	e.i++
	return t, ok
}

func dsdAAToInt(v interface{}) (int, error) {
	switch x := v.(type) {
	case int:
		return x, nil
	case string:
		return strconv.Atoi(strings.TrimSpace(x))
	}
	return 0, fmt.Errorf("aa: not a number: %v", v)
}

func (e *dsdAAEval) expr() (interface{}, error) { return e.bitxor() }

func (e *dsdAAEval) bitxor() (interface{}, error) {
	v, err := e.add()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := e.peek()
		if !ok || t.kind != 'o' || t.str != "^" {
			return v, nil
		}
		e.next()
		r, err := e.add()
		if err != nil {
			return nil, err
		}
		vi, err := dsdAAToInt(v)
		if err != nil {
			return nil, err
		}
		ri, err := dsdAAToInt(r)
		if err != nil {
			return nil, err
		}
		v = vi ^ ri
	}
}

func (e *dsdAAEval) add() (interface{}, error) {
	v, err := e.unary()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := e.peek()
		if !ok || t.kind != 'o' || (t.str != "+" && t.str != "-") {
			return v, nil
		}
		op := t.str
		e.next()
		r, err := e.unary()
		if err != nil {
			return nil, err
		}
		if op == "+" {
			_, vsIsStr := v.(string)
			_, rsIsStr := r.(string)
			if vsIsStr || rsIsStr {
				v = fmt.Sprintf("%v%v", v, r)
				continue
			}
			vi, err := dsdAAToInt(v)
			if err != nil {
				return nil, err
			}
			ri, err := dsdAAToInt(r)
			if err != nil {
				return nil, err
			}
			v = vi + ri
			continue
		}
		vi, err := dsdAAToInt(v)
		if err != nil {
			return nil, err
		}
		ri, err := dsdAAToInt(r)
		if err != nil {
			return nil, err
		}
		v = vi - ri
	}
}

func (e *dsdAAEval) unary() (interface{}, error) {
	if t, ok := e.peek(); ok && t.kind == 'o' && t.str == "-" {
		e.next()
		v, err := e.unary()
		if err != nil {
			return nil, err
		}
		vi, err := dsdAAToInt(v)
		if err != nil {
			return nil, err
		}
		return -vi, nil
	}
	return e.primary()
}

func (e *dsdAAEval) primary() (interface{}, error) {
	t, ok := e.next()
	if !ok {
		return nil, fmt.Errorf("aa: unexpected end")
	}
	switch t.kind {
	case 'n':
		return t.num, nil
	case 's':
		return t.str, nil
	case 'o':
		if t.str == "(" {
			v, err := e.expr()
			if err != nil {
				return nil, err
			}
			c, ok := e.next()
			if !ok || c.kind != 'o' || c.str != ")" {
				return nil, fmt.Errorf("aa: expected )")
			}
			return v, nil
		}
	}
	return nil, fmt.Errorf("aa: unexpected token %q", t.str)
}

func dsdAATry(script string) (string, bool) {
	vars := dsdAAVarsOf(script)
	inner, ok := dsdAAInner(script)
	if !ok {
		return "", false
	}
	ev := &dsdAAEval{t: dsdAATokenize(dsdAAToExpr(inner, vars))}
	v, err := ev.expr()
	if err != nil {
		return "", false
	}
	s, isStr := v.(string)
	if !isStr {
		return "", false
	}
	return s, true
}

// dsdUnescapeJS ports the reference unescape_js: \0-\7 octal, \x,
// \u and the letter escapes.
func dsdUnescapeJS(s string) string {
	rs := []rune(s)
	var b strings.Builder
	i := 0
	for i < len(rs) {
		c := rs[i]
		if c != '\\' {
			b.WriteRune(c)
			i++
			continue
		}
		if i+1 >= len(rs) {
			break
		}
		n := rs[i+1]
		if n >= '0' && n <= '7' {
			j := i + 1
			val := 0
			for j < len(rs) && j <= i+3 && rs[j] >= '0' && rs[j] <= '7' {
				val = val*8 + int(rs[j]-'0')
				j++
			}
			b.WriteRune(rune(val))
			i = j
			continue
		}
		if n == 'x' && i+3 < len(rs) {
			if v, err := strconv.ParseInt(string(rs[i+2:i+4]), 16, 32); err == nil {
				b.WriteRune(rune(v))
				i += 4
				continue
			}
		}
		if n == 'u' && i+5 < len(rs) {
			if v, err := strconv.ParseInt(string(rs[i+2:i+6]), 16, 32); err == nil {
				b.WriteRune(rune(v))
				i += 6
				continue
			}
		}
		switch n {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		default:
			b.WriteRune(n)
		}
		i += 2
	}
	return b.String()
}
