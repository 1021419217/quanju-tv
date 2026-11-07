package seku

// ---- Javmap (javmap.com, 日本AV大全) — hanxiaoquan slot, v4 ----
//
// Ported from the user's ZakaTV jar class ZakaJavmapAmns (config key
// ZakaJavmap, site name "JAVMAP┃日本AV大全"). Hanxiaoquan's previous
// occupant JavMenu was removed by user order (2026-10-23 17:48): its
// CDN stitches a 3-second foreign ad segment into the media playlist
// ahead of the AES-encrypted feature, and the player looped on it.
// JavMenu code stays in sites_g1.go, unregistered.
//
// Javmap is an Empire CMS site. Live-verified 2026-10-23 from this
// egress (no Cloudflare, no login):
//   - List pages: / (home), /popular/ + /popular/index_N.html,
//     /e/action/ListInfo/?classid=N&page=P (20 cards/page),
//     /e/tags/index.php?page=P&tagname=T&line=20&tempid=2.
//   - Detail pages carry `var vid=, cid=` plus data-pathid source tabs
//     (SP/SW/VH servers). Play: POST /e/DownSys/Player/ (form
//     classid&id&pathid, XHR) -> JSON {player: iframe html} -> iframe
//     page carries a Dean-Edwards packed script or a `links = {...}`
//     object; stream priority hls3>hls4>hls2>hls1>hls.
//   - Ad check on the verified title (T38-073, VH server): media
//     playlist 825 segments / 137.5 min == site-stated 8249 s, zero
//     EXT-X-DISCONTINUITY, EXT-X-ENDLIST present, segment 0 starts
//     with the TS sync byte. No ad segments, no interstitials.
//   - Resolve probes candidate streams (requested path first, then
//     2,1,0) and returns the first working stream as main plus the
//     remaining working paths as routes — some path CDNs refuse this
//     egress but answer the user's network; the probe keeps dead paths
//     out of the answer.
//   - The jar's JmProxy local-URL wrapping needs no javmap-specific
//     hook: the generic relay already rewrites playlists and KEY/MAP
//     URIs and fetches every segment with the iframe-origin Referer,
//     and the dex shows JmProxy adds no signing, token refresh, or
//     expiry handling on top of that.
//   - Search: POST /e/search/index.php (keyboard/show=title/tempid=1)
//     -> 302 Location -> result list page, parsed like a list page.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const jmapSite = "https://javmap.com"
const jmapUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func jmapHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      jmapUA,
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.6",
		"Referer":         jmapSite + "/",
	}
}

var (
	jmapCardRe   = regexp.MustCompile(`(?s)<div class="th">(.*?)<a class="th-description"[^>]*>(.*?)</a>`)
	jmapHrefRe   = regexp.MustCompile(`href="(/[a-z0-9-]+/(\d+)\.html)"`)
	jmapImgRe    = regexp.MustCompile(`<img[^>]*src="([^"]+)"`)
	jmapHDRe     = regexp.MustCompile(`<span class="th-hd">([^<]*)</span>`)
	jmapDateRe   = regexp.MustCompile(`class="th-duration"[^>]*>\s*<i[^>]*></i>([^<]*)<`)
	jmapTitleRe  = regexp.MustCompile(`(?s)<span class="inner-title">(.*?)</span>`)
	jmapBGRe     = regexp.MustCompile(`background:\s*url\(([^)]+)\)`)
	jmapPathsRe  = regexp.MustCompile(`(?s)data-pathid="(\d+)"[^>]*>(?:.*?<span>([^<]*)</span>)`)
	jmapInfoRe   = regexp.MustCompile(`(?s)<div id="av-data-info"[^>]*>(.*?)</div>`)
	jmapVidRe    = regexp.MustCompile(`var vid=(\d+),\s*cid=(\d+)`)
	jmapPackerRe = regexp.MustCompile(`(?s)eval\(function\(p,a,c,k,e,d\)\{.*?\}\('(.*?)',(\d+),(\d+),'(.*?)'\.split\('\|'\)`)
	jmapLinksRe  = regexp.MustCompile(`(?s)links\s*=\s*(\{.*?\})\s*;`)
	jmapLinkKVRe = regexp.MustCompile(`"?(hls\d*|hls)"?\s*:\s*"([^"]+)"`)
	jmapIframeRe = regexp.MustCompile(`src="([^"]+)"`)
	jmapTagRe    = regexp.MustCompile(`<[^>]+>`)
)

var jmapCats = []struct{ id, name string }{
	{"h:home", "★最新"},
	{"p:popular", "🔥热门"},
	{"c:1", "有码 Censored"},
	{"c:2", "无码 Uncensored"},
	{"c:3", "素人 Amateur"},
	{"c:4", "欧美 Europe"},
	{"c:5", "中文字幕"},
}

// Tag cloud exactly as the site's list pages render it (2026-10-23).
var jmapTags = []string{
	"Beautifulgirl", "BigTits", "Breasts", "Censored", "CensoredJAV",
	"Cheka", "Cowgirl", "Creampie", "Cuckold", "Cunnilingus",
	"DMMExclusive", "DigitalMosaic", "Doggystyle", "Drama",
	"FeaturedActress", "Handjob", "Hi-Def", "JavPlayerDecensored",
	"MarriedWoman", "MatureWoman", "POV", "ReducingMosaic", "Slender",
	"Slut", "Solowork", "Squirting", "Tittyfuck", "Uncensored",
	"amateur", "blowjob",
}

func jmapCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range jmapCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	for _, t := range jmapTags {
		out = append(out, categoryObj("t:"+t, "标签·"+t))
	}
	return out
}

func jmapFetch(u string) (string, error) {
	b, code, _, err := httpGet(u, jmapHeaders())
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("javmap: HTTP %d for %s", code, u)
	}
	return string(b), nil
}

func jmapStrip(s string) string {
	return cleanText(jmapTagRe.ReplaceAllString(s, ""))
}

type jmapCard struct {
	rawID, title, cover, remark string
}

func jmapParseList(text string) []jmapCard {
	out := []jmapCard{}
	for _, m := range jmapCardRe.FindAllStringSubmatch(text, -1) {
		full := m[0]
		hm := jmapHrefRe.FindStringSubmatch(full)
		if hm == nil {
			continue
		}
		rawID := strings.TrimSuffix(strings.TrimPrefix(hm[1], "/"), ".html")
		title := jmapStrip(m[2])
		if title == "" {
			continue
		}
		cover := ""
		if im := jmapImgRe.FindStringSubmatch(m[1]); im != nil {
			cover = strings.TrimSpace(im[1])
		}
		parts := []string{}
		if hd := jmapHDRe.FindStringSubmatch(full); hd != nil && hd[1] != "" {
			parts = append(parts, hd[1])
		}
		if dt := jmapDateRe.FindStringSubmatch(full); dt != nil && dt[1] != "" {
			parts = append(parts, dt[1])
		}
		out = append(out, jmapCard{rawID, title, cover, strings.Join(parts, " · ")})
	}
	return out
}

func jmapCardsToItems(slot string, cards []jmapCard, catName string) []interface{} {
	items := []interface{}{}
	for _, c := range cards {
		items = append(items, makeDrama(slot, c.rawID, c.title, c.cover, c.remark, catName, ""))
	}
	return items
}

func jmapListURL(category string, page int) string {
	if page < 1 {
		page = 1
	}
	switch {
	case strings.HasPrefix(category, "p:"):
		if page == 1 {
			return jmapSite + "/popular/"
		}
		return fmt.Sprintf("%s/popular/index_%d.html", jmapSite, page)
	case strings.HasPrefix(category, "t:"):
		return fmt.Sprintf("%s/e/tags/index.php?page=%d&tagname=%s&line=20&tempid=2",
			jmapSite, page, url.QueryEscape(category[2:]))
	case strings.HasPrefix(category, "c:"):
		return fmt.Sprintf("%s/e/action/ListInfo/?classid=%s&page=%d", jmapSite, category[2:], page)
	default: // h:home and anything else -> homepage flow
		if page == 1 {
			return jmapSite + "/"
		}
		return fmt.Sprintf("%s/e/action/ListInfo/?classid=1&page=%d", jmapSite, page)
	}
}

func jmapCatName(category string) string {
	for _, c := range jmapCats {
		if c.id == category {
			return c.name
		}
	}
	if strings.HasPrefix(category, "t:") {
		return "标签·" + category[2:]
	}
	return "★最新"
}

func jmapCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	text, err := jmapFetch(jmapListURL(category, page))
	if err != nil {
		return nil, 0, false, err
	}
	cards := jmapParseList(text)
	return jmapCardsToItems(slot, cards, jmapCatName(category)), len(cards), len(cards) >= 15, nil
}

// jmapDetailPage fetches a detail page and returns (vid, cid, html).
func jmapDetailPage(rawID string) (string, string, string, error) {
	text, err := jmapFetch(jmapSite + "/" + rawID + ".html")
	if err != nil {
		return "", "", "", err
	}
	vm := jmapVidRe.FindStringSubmatch(text)
	if vm == nil {
		return "", "", "", fmt.Errorf("javmap: no vid/cid on detail page")
	}
	return vm[1], vm[2], text, nil
}

func jmapDetail(slot, rawID string) (map[string]interface{}, error) {
	_, _, text, err := jmapDetailPage(rawID)
	if err != nil {
		return nil, err
	}
	title := ""
	if tm := jmapTitleRe.FindStringSubmatch(text); tm != nil {
		title = jmapStrip(tm[1])
	}
	if title == "" {
		title = rawID
	}
	cover := ""
	if bm := jmapBGRe.FindStringSubmatch(text); bm != nil {
		cover = strings.Trim(strings.TrimSpace(bm[1]), `'"`)
	}
	desc := ""
	if im := jmapInfoRe.FindStringSubmatch(text); im != nil {
		desc = jmapStrip(im[1])
	}
	remark := ""
	if dt := jmapDateRe.FindStringSubmatch(text); dt != nil {
		remark = dt[1]
	}
	vm := jmapVidRe.FindStringSubmatch(text)
	d := makeDrama(slot, rawID, title, cover, remark, "日本AV大全", desc)
	chs := []interface{}{}
	seen := map[string]bool{}
	for i, pm := range jmapPathsRe.FindAllStringSubmatch(text, -1) {
		pid := pm[1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		name := jmapStrip(pm[2])
		if name == "" {
			name = "线路" + pid
		}
		epRaw := vm[2] + ":" + vm[1] + ":" + pid
		chs = append(chs, g2Chapter(slot, rawID, epRaw, name, i+1))
	}
	if len(chs) == 0 {
		if vm == nil {
			return nil, fmt.Errorf("javmap: no play paths on detail page")
		}
		for i, pid := range []string{"0", "1"} {
			name := "SW"
			if pid == "1" {
				name = "VH"
			}
			epRaw := vm[2] + ":" + vm[1] + ":" + pid
			chs = append(chs, g2Chapter(slot, rawID, epRaw, name, i+1))
		}
	}
	g2SetChapters(d, chs)
	return d, nil
}

// jmapUnpack reverses a Dean Edwards packer payload.
func jmapUnpack(payload string, radix, count int, dict []string) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if radix > len(digits) || radix < 2 {
		return payload
	}
	alpha := digits[:radix]
	tokRe := regexp.MustCompile(`[0-9a-zA-Z]+`)
	return tokRe.ReplaceAllStringFunc(payload, func(tok string) string {
		// decode token as a base-radix number over alpha
		n := 0
		for _, r := range tok {
			i := strings.IndexRune(alpha, r)
			if i < 0 {
				return tok
			}
			n = n*radix + i
		}
		if n < count && n < len(dict) && dict[n] != "" {
			return dict[n]
		}
		return tok
	})
}

// jmapLinksFromIframe extracts the stream map from an iframe page.
func jmapLinksFromIframe(text string) map[string]string {
	if pm := jmapPackerRe.FindStringSubmatch(text); pm != nil {
		radix, _ := strconv.Atoi(pm[2])
		count, _ := strconv.Atoi(pm[3])
		dict := strings.Split(pm[4], "|")
		text = jmapUnpack(pm[1], radix, count, dict)
	}
	out := map[string]string{}
	if lm := jmapLinksRe.FindStringSubmatch(text); lm != nil {
		for _, kv := range jmapLinkKVRe.FindAllStringSubmatch(lm[1], -1) {
			u := strings.ReplaceAll(kv[2], `\/`, "/")
			if strings.HasPrefix(u, "http") {
				out[kv[1]] = u
			}
		}
	}
	return out
}

func jmapPickLink(links map[string]string) string {
	for _, k := range []string{"hls3", "hls4", "hls2", "hls1", "hls"} {
		if u := links[k]; u != "" {
			return u
		}
	}
	return ""
}

var jmapMasterTokenRe = regexp.MustCompile(`["'\s=]([^"'\s<>]*?(?:master\.txt|master\.m3u8|\.m3u8)[^"'\s<>]*)`)

func jmapAbsoluteURL(base, raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	bu, err := url.Parse(base)
	if err != nil {
		return raw
	}
	ru, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return bu.ResolveReference(ru).String()
}

func jmapMasterFallback(text, base string) string {
	plain := strings.ReplaceAll(text, `\/`, "/")
	tokens := []string{}
	for _, m := range jmapMasterTokenRe.FindAllStringSubmatch(plain, -1) {
		tokens = append(tokens, m[1])
	}
	for _, want := range []string{"master.txt", "master.m3u8", ".m3u8"} {
		for _, tok := range tokens {
			if strings.Contains(tok, want) {
				return jmapAbsoluteURL(base, tok)
			}
		}
	}
	return ""
}

// jmapProbeStream reports whether a playlist URL answers like a playlist
// under exactly the headers the player will send.
func jmapProbeStream(u, referer string) bool {
	b, code, _, err := httpGet(u, map[string]string{"User-Agent": jmapUA, "Referer": referer})
	if err != nil || code != 200 {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(b)), "#EXTM3U")
}

// jmapResolvePath runs the full player chain for one pathid and returns
// (streamURL, iframeOrigin, ok).
func jmapResolvePath(cid, vid, pathid string) (string, string, bool) {
	form := url.Values{
		"classid": {cid},
		"id":      {vid},
		"pathid":  {pathid},
	}.Encode()
	hdr := jmapHeaders()
	hdr["Content-Type"] = "application/x-www-form-urlencoded"
	hdr["X-Requested-With"] = "XMLHttpRequest"
	b, code, _, err := httpDo("POST", jmapSite+"/e/DownSys/Player/", strings.NewReader(form), hdr)
	if err != nil || code != 200 {
		return "", "", false
	}
	var resp struct {
		Player string `json:"player"`
	}
	if err := json.Unmarshal(b, &resp); err != nil || resp.Player == "" {
		return "", "", false
	}
	im := jmapIframeRe.FindStringSubmatch(resp.Player)
	if im == nil {
		return "", "", false
	}
	iframeURL := strings.ReplaceAll(im[1], `\/`, "/")
	if strings.HasPrefix(iframeURL, "//") {
		iframeURL = "https:" + iframeURL
	}
	pu, perr := url.Parse(iframeURL)
	if perr != nil {
		return "", "", false
	}
	origin := pu.Scheme + "://" + pu.Host + "/"
	text, err := jmapFetch(iframeURL)
	if err != nil {
		return "", "", false
	}
	stream := jmapPickLink(jmapLinksFromIframe(text))
	if stream == "" {
		stream = jmapMasterFallback(text, iframeURL)
	}
	if stream == "" {
		return "", "", false
	}
	if !jmapProbeStream(stream, origin) {
		return "", "", false
	}
	return stream, origin, true
}

func jmapResolve(slot, rawID string) (map[string]interface{}, error) {
	// Episode ids are "cid:vid:pathid"; a bare drama id re-derives its
	// detail and starts from the first listed path.
	cid, vid, want := "", "", ""
	if parts := strings.Split(rawID, ":"); len(parts) == 3 {
		cid, vid, want = parts[0], parts[1], parts[2]
	} else {
		v, c, text, err := jmapDetailPage(rawID)
		if err != nil {
			return nil, err
		}
		vid, cid = v, c
		if pm := jmapPathsRe.FindStringSubmatch(text); pm != nil {
			want = pm[1]
		}
	}
	order := []string{}
	if want != "" {
		order = append(order, want)
	}
	for _, p := range []string{"2", "1", "0"} {
		if p != want {
			order = append(order, p)
		}
	}
	type jmapLine struct {
		stream string
		origin string
	}
	lines := []jmapLine{}
	for _, pid := range order {
		if stream, origin, ok := jmapResolvePath(cid, vid, pid); ok {
			lines = append(lines, jmapLine{stream: stream, origin: origin})
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("javmap: no playable stream on any path")
	}
	main := lines[0]
	result := playResult(main.stream, slot, rawID, jmapUA, main.origin, "m3u8", "hls")
	if len(lines) > 1 {
		routes := []interface{}{}
		for _, line := range lines[1:] {
			routes = append(routes, map[string]interface{}{"name": "备用", "route": "备用", "url": line.stream, "referer": line.origin})
		}
		result["routes"] = routes
	}
	return result, nil
}

func jmapSearch(slot, kw string) ([]interface{}, error) {
	form := url.Values{
		"keyboard": {strings.TrimSpace(kw)},
		"show":     {"title"},
		"tempid":   {"1"},
	}.Encode()
	hdr := jmapHeaders()
	hdr["Content-Type"] = "application/x-www-form-urlencoded"
	b, code, hdrResp, err := httpDo("POST", jmapSite+"/e/search/index.php", strings.NewReader(form), hdr)
	if err != nil {
		return nil, err
	}
	text := string(b)
	if loc := hdrResp.Get("Location"); loc != "" && code >= 300 && code < 400 {
		u := loc
		if strings.HasPrefix(u, "/") {
			u = jmapSite + u
		} else if !strings.HasPrefix(u, "http") {
			u = jmapSite + "/e/search/" + strings.TrimLeft(u, "/")
		}
		t2, err := jmapFetch(u)
		if err != nil {
			return nil, err
		}
		text = t2
	}
	return jmapCardsToItems(slot, jmapParseList(text), "搜索"), nil
}

func init() {
	registerSite("hanxiaoquan", &siteImpl{
		Key:        "javmap",
		Name:       "日本AV大全",
		DefaultCat: "h:home",
		HomeURL:    jmapSite + "/",
		Categories: jmapCategories,
		Catalog:    jmapCatalog,
		Detail:     jmapDetail,
		Resolve:    jmapResolve,
		Search:     jmapSearch,
		Headers:    jmapHeaders,
	})
}
