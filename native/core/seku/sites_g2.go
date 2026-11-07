package seku

// ---- G2 batch: py-source ports (missav / supjav / aipornx / javhd) ----
//
// Ported from the user's TVBox py spiders (recipes in batch/recipes_py.json;
// the py files are the final authority). Where recipe and py disagreed, the
// py source wins — no material conflicts were found in this group; the
// closest calls, resolved for the py:
//   - missav covers: py appends shell pic suffix headers (@Referer/@UA);
//     this core returns the bare URL (fourhoi serves it directly), per the
//     batch contract for cover keys.
//   - javhd categories: py tries the live /cn/categories/ page first and
//     only falls back to the 99-entry snapshot when live yields <50 —
//     kept as-is (fixed 15 + live-or-snapshot).
//   - supjav: the py never solves Cloudflare; from a datacenter egress
//     supjav.com is CF hard-blocked (Attention Required). The port keeps
//     the py behaviour (www-subdomain retry on a CF block page) and
//     reports failures honestly instead of faking success.
//   - py "parse=1" web-sniff fallbacks have no equivalent in this core;
//     Resolve returns an error in those spots (noted per site).
//
// Folder/index drill contract (batch): catalog returns folder cards whose
// rawID is "idx:"+path; Detail on an "idx:" rawID fetches the folder page
// and returns its works as chapters (episodeId = work rawID).

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- shared helpers (g2-prefixed; other batch files use their own) ----

var (
	g2ScriptRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	g2StyleRe  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	g2TagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	g2WsRe     = regexp.MustCompile(`\s+`)
)

// g2TxtOf mirrors the py txt_of(): strip script/style/tags, unescape,
// collapse whitespace.
func g2TxtOf(s string) string {
	s = g2ScriptRe.ReplaceAllString(s, " ")
	s = g2StyleRe.ReplaceAllString(s, " ")
	s = g2TagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(g2WsRe.ReplaceAllString(s, " "))
}

func g2Trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func g2PlayFormat(u string) (string, string) {
	if strings.Contains(u, ".m3u8") {
		return "m3u8", "hls"
	}
	return "mp4", "file"
}

type g2Work struct {
	RawID, Title, Cover, Remark string
}

func g2Chapter(slot, parentRaw, epRaw, title string, idx int) map[string]interface{} {
	return map[string]interface{}{
		"episodeId": epRaw,
		"id":        dramaID(slot, epRaw),
		"dramaId":   dramaID(slot, parentRaw),
		"sourceId":  slot,
		"source":    slot,
		"title":     title,
		"name":      title,
		"index":     idx,
		"episode":   idx,
		"url":       "",
		"videoUrl":  "",
		"mediaSize": 0,
	}
}

func g2SetChapters(d map[string]interface{}, chs []interface{}) {
	d["chapters"] = chs
	d["episodeList"] = chs
	n := len(chs)
	d["episodes"] = n
	d["episodeCount"] = n
	d["chapterCount"] = n
	d["totalEpisode"] = n
	d["totalEpisodes"] = n
}

// g2WorksDetail builds a folder drama whose chapters are the folder's works.
func g2WorksDetail(slot, rawID, title, cover, remark, catName string, works []g2Work, desc string) map[string]interface{} {
	d := makeDrama(slot, rawID, title, cover, remark, catName, desc)
	if len(works) > 0 {
		chs := []interface{}{}
		for i, w := range works {
			chs = append(chs, g2Chapter(slot, rawID, w.RawID, w.Title, i+1))
		}
		g2SetChapters(d, chs)
	}
	return d
}

// g2ZeroEpisodes clears a drama's episode list (folder dramas carry
// no episodes; their content lives in a catalog drill category).
func g2ZeroEpisodes(d map[string]interface{}) {
	d["chapters"] = []interface{}{}
	d["episodeList"] = []interface{}{}
	d["episodes"] = 0
	d["episodeCount"] = 0
	d["chapterCount"] = 0
	d["totalEpisode"] = 0
	d["totalEpisodes"] = 0
}

// g2FolderDetail builds a FOLDER info drama (v6, user request):
// actress/index entries must open as a card list, not as a detail
// page whose episodes are the works. The works are served by the
// catalog drill category drillCat; the same id is written into the
// drama's "category" field so the app can reach the card page from
// here. No episodes are embedded.
//
// v7 WARNING (device-proven dead end, do NOT use for tappable
// cards): the Dart app has no folder/category navigation — a card
// tap dispatches detail -> episodes, and a zero-episode detail
// aborts with "暂时没有可播放的集数"; Drama.category is display
// metadata only. All v6 callers were converted to g2WorksDetail
// embeds in v7. Kept for reference only.
func g2FolderDetail(slot, rawID, drillCat, title, cover, remark, catName, desc string) map[string]interface{} {
	d := makeDrama(slot, rawID, title, cover, remark, catName, desc)
	d["category"] = drillCat
	g2ZeroEpisodes(d)
	return d
}

func g2WorksToItems(slot string, works []g2Work, catName string) []interface{} {
	items := []interface{}{}
	for _, w := range works {
		items = append(items, makeDrama(slot, w.RawID, w.Title, w.Cover, w.Remark, catName, ""))
	}
	return items
}

// reverseString reverses by rune (supjav tokens are ASCII hex, but the py
// reverses by character; runes are the faithful equivalent here).
func g2Reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// ============================================================
// missav — missav.ws (pure HTML; play URL assembled from reversed
// token string on the detail page). Slot: hongguo.
// ============================================================

const missavUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
const missavBase = "https://missav.ws"

var missavDomains = []string{"https://missav.ws", "https://missav123.com", "https://missav.ai"}

func missavHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      missavUA,
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.6",
		"Referer":         "https://missav.ws/",
	}
}

// 36 categories, site navigation snapshot (py CATS, order preserved).
var missavCats = []struct{ id, name string }{
	{"new", "最近更新"},
	{"release", "新作上市"},
	{"chinese-subtitle", "中文字幕"},
	{"today-hot", "今日热门"},
	{"weekly-hot", "本周热门"},
	{"monthly-hot", "本月热门"},
	{"uncensored-leak", "无码流出"},
	{"actresses", "女优一览"},
	{"actresses/ranking", "女优排行"},
	{"genres", "类型"},
	{"makers", "发行商"},
	{"genres/VR", "VR"},
	{"siro", "SIRO"},
	{"luxu", "LUXU"},
	{"gana", "GANA"},
	{"maan", "PRESTIGE PREMIUM"},
	{"scute", "S-CUTE"},
	{"ara", "ARA"},
	{"fc2", "FC2"},
	{"heyzo", "HEYZO"},
	{"tokyohot", "东京热"},
	{"1pondo", "一本道"},
	{"caribbeancom", "Caribbeancom"},
	{"caribbeancompr", "Caribbeancompr"},
	{"10musume", "10musume"},
	{"pacopacomama", "pacopacomama"},
	{"gachinco", "Gachinco"},
	{"xxxav", "XXX-AV"},
	{"marriedslash", "人妻斩"},
	{"naughty4610", "顽皮4610"},
	{"naughty0930", "顽皮0930"},
	{"madou", "麻豆传媒"},
	{"twav", "TWAV"},
	{"furuke", "Furuke"},
	{"klive", "韩国直播"},
	{"clive", "中国直播"},
}

var missavIndexTIDs = map[string]bool{
	"actresses": true, "actresses/ranking": true, "genres": true, "makers": true,
}

var missavIndexPrefix = map[string]string{
	"actresses": "actresses", "actresses/ranking": "actresses",
	"genres": "genres", "makers": "makers",
}

var missavReserved = map[string]bool{
	"release": true, "new": true, "actresses": true, "genres": true,
	"makers": true, "search": true, "chinese-subtitle": true,
	"uncensored-leak": true, "login": true, "register": true,
	"history": true, "playlists": true, "tags": true, "dm": true,
	"vip": true, "saved": true, "today-hot": true, "weekly-hot": true,
	"monthly-hot": true,
}

var missavPlaceholderHints = []string{
	"data:image", "placeholder", "lazy", "loading", "blank", "spinner",
	"1x1", "pixel", ".svg", "noimage", "no-image", "default.jpg",
	"grey", "gray",
}

var (
	missavHostRe    = regexp.MustCompile(`^(https?://[^/]+)(/.*)$`)
	missavImgTagRe  = regexp.MustCompile(`(?i)<img[^>]*>`)
	missavDurRe     = regexp.MustCompile(`\b(\d{1,2}:\d{2}(?::\d{2})?)\b`)
	missavListRe    = regexp.MustCompile(`(?is)<a[^>]+href=["']?(https?://[^"'\s>]+/(?:dm\d+/)?cn/([a-z0-9][a-z0-9\-]*))["']?[^>]*>(.*?)</a>`)
	missavGenericRe = regexp.MustCompile(`(?is)<a[^>]+href=["']?((?:https?://[^"'\s>]+)?/[^"'\s>]+)["']?[^>]*>(.*?)</a>`)
	missavCodeRe    = regexp.MustCompile(`-\d`)
	missavDmRe      = regexp.MustCompile(`//[^/]+/(dm\d+/)(cn/)`)
)

func missavImgAttr(tag, attr string) string {
	re := regexp.MustCompile(`(?i)` + attr + `\s*=\s*["']([^"']+)["']`)
	m := re.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return m[1]
}

func missavIsPlaceholder(u string) bool {
	low := strings.ToLower(u)
	for _, h := range missavPlaceholderHints {
		if strings.Contains(low, h) {
			return true
		}
	}
	return false
}

func missavNormPic(u string) string {
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "/") {
		return "https://missav.ws" + u
	}
	return u
}

// missavPickImage ports py pick_image(): lazy attrs beat src, placeholders
// skipped, fourhoi preferred.
func missavPickImage(body string) string {
	type cand struct {
		fourhoi, prio int
		url           string
	}
	good := []cand{}
	attrs := []string{"data-src", "data-original", "data-lazy-src", "data-url", "src"}
	for _, tag := range missavImgTagRe.FindAllString(body, -1) {
		for _, attr := range attrs {
			u := strings.TrimSpace(missavImgAttr(tag, attr))
			if u == "" {
				continue
			}
			prio := 1
			if strings.HasPrefix(attr, "data-") {
				prio = 0
			}
			if missavIsPlaceholder(u) {
				break
			}
			u = missavNormPic(u)
			if !strings.HasPrefix(u, "http") {
				break
			}
			f := 1
			if strings.Contains(u, "fourhoi.com") {
				f = 0
			}
			good = append(good, cand{f, prio, u})
			break
		}
	}
	if len(good) == 0 {
		return ""
	}
	sort.SliceStable(good, func(i, j int) bool {
		if good[i].fourhoi != good[j].fourhoi {
			return good[i].fourhoi < good[j].fourhoi
		}
		if good[i].prio != good[j].prio {
			return good[i].prio < good[j].prio
		}
		return good[i].url < good[j].url
	})
	return good[0].url
}

// missavPickWindowImages ports py pick_window_images().
func missavPickWindowImages(page string, winStart, winEnd, anchorStart int, prefer string) []string {
	if winStart < 0 {
		winStart = 0
	}
	if winEnd > len(page) {
		winEnd = len(page)
	}
	if winStart > winEnd {
		return nil
	}
	seg := page[winStart:winEnd]
	anchorOff := anchorStart - winStart
	if anchorOff < 0 {
		anchorOff = 0
	}
	type cand struct {
		pref, dist, seq int
		url             string
	}
	best := map[string]*cand{}
	order := []string{}
	attrs := []string{"data-src", "data-original", "data-lazy-src", "data-url", "src"}
	locs := missavImgTagRe.FindAllStringIndex(seg, -1)
	for _, loc := range locs {
		tag := seg[loc[0]:loc[1]]
		for _, attr := range attrs {
			u := strings.TrimSpace(missavImgAttr(tag, attr))
			if u == "" {
				continue
			}
			if missavIsPlaceholder(u) {
				break
			}
			u = missavNormPic(u)
			if !strings.HasPrefix(u, "http") {
				break
			}
			pref := 1
			if prefer != "" && strings.Contains(u, prefer) {
				pref = 0
			}
			dist := loc[0] - anchorOff
			if dist < 0 {
				dist = -dist
			}
			if old, ok := best[u]; ok {
				if pref < old.pref || (pref == old.pref && dist < old.dist) {
					old.pref, old.dist = pref, dist
				}
			} else {
				best[u] = &cand{pref, dist, len(order), u}
				order = append(order, u)
			}
			break
		}
	}
	cands := []*cand{}
	for _, u := range order {
		cands = append(cands, best[u])
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].pref != cands[j].pref {
			return cands[i].pref < cands[j].pref
		}
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].seq < cands[j].seq
	})
	out := []string{}
	for _, c := range cands {
		out = append(out, c.url)
	}
	return out
}

// missavFetch ports py http_get(): candidate-domain fallback, CF
// challenge page ("just a moment", short body) counts as failure.
func missavFetch(rawurl string) string {
	urls := []string{rawurl}
	if m := missavHostRe.FindStringSubmatch(rawurl); m != nil {
		for _, d := range missavDomains {
			cand := d + m[2]
			if cand != rawurl {
				urls = append(urls, cand)
			}
		}
	}
	for _, u := range urls {
		for attempt := 0; attempt < 2; attempt++ {
			body, code, _, err := tlsGet(u, missavHeaders())
			if err != nil || code != 200 {
				continue
			}
			text := string(body)
			if strings.Contains(strings.ToLower(text), "just a moment") && len(text) < 12000 {
				continue
			}
			return text
		}
	}
	return ""
}

// missav play cache: detail-stage m3u8 keyed by rawID, so Resolve
// can skip the second CF-fronted fetch (py detailContent hands the
// play url back the same way). Process-lifetime only.
var missavPlayMu sync.Mutex
var missavPlayCache = map[string]string{}

func missavCachePlay(rawID, m3u8 string) {
	if rawID == "" || m3u8 == "" {
		return
	}
	missavPlayMu.Lock()
	missavPlayCache[rawID] = m3u8
	missavPlayMu.Unlock()
}

func missavCachedPlay(rawID string) string {
	missavPlayMu.Lock()
	defer missavPlayMu.Unlock()
	return missavPlayCache[rawID]
}

// missavCoverFetch fetches cover bytes with the site headers. py
// appends "@Referer=…@User-Agent=…" suffixes to vod_pic for the
// shell's image loader; the Dart side loads covers through the core
// instead, so the headers ride here (fourhoi rejects requests the
// same way the py notes describe — the bytes path needs the site
// header set, and a non-200 must not be cached as an image).
func missavCoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpGet(u, missavHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("missav cover: HTTP %d", code)
	}
	return body, nil
}

// missavParseList ports py parse_list() (+ parse_generic fallback).
func missavParseList(page string) []g2Work {
	type card struct {
		work g2Work
	}
	cards := map[string]*card{}
	order := []string{}
	for _, m := range missavListRe.FindAllStringSubmatch(page, -1) {
		code := strings.ToLower(m[2])
		body := m[3]
		if missavReserved[code] || !missavCodeRe.MatchString(code) {
			continue
		}
		c, ok := cards[code]
		if !ok {
			c = &card{work: g2Work{RawID: "https://missav.ws/cn/" + code}}
			cards[code] = c
			order = append(order, code)
		}
		if pic := missavPickImage(body); pic != "" && c.work.Cover == "" {
			c.work.Cover = pic
		}
		if dm := missavDurRe.FindStringSubmatch(body); dm != nil && c.work.Remark == "" {
			c.work.Remark = dm[1]
		}
		if title := g2TxtOf(body); len([]rune(title)) > len([]rune(c.work.Title)) {
			c.work.Title = title
		}
	}
	out := []g2Work{}
	for _, code := range order {
		w := cards[code].work
		if w.Cover == "" {
			w.Cover = "https://fourhoi.com/" + code + "/cover-t.jpg"
		}
		if w.Title == "" {
			w.Title = strings.ToUpper(code)
		}
		w.Title = g2Trunc(w.Title, 120)
		out = append(out, w)
	}
	if len(out) == 0 {
		return missavParseGeneric(page)
	}
	return out
}

func missavParseGeneric(page string) []g2Work {
	out := []g2Work{}
	seen := map[string]bool{}
	for _, m := range missavGenericRe.FindAllStringSubmatch(page, -1) {
		href, body := m[1], m[2]
		pic := missavPickImage(body)
		title := g2TxtOf(body)
		if pic == "" || len([]rune(title)) < 2 {
			continue
		}
		if strings.HasPrefix(href, "/") {
			href = "https://missav.ws" + href
		}
		href = missavDmRe.ReplaceAllString(href, "//missav.ws/${2}")
		if seen[href] || !strings.Contains(href, "missav") {
			continue
		}
		seen[href] = true
		out = append(out, g2Work{RawID: href, Title: g2Trunc(title, 120), Cover: pic})
		if len(out) >= 24 {
			break
		}
	}
	return out
}

var (
	missavCountRe = regexp.MustCompile(`([\d,]+)\s*条影片`)
	missavDispRe  = regexp.MustCompile(`^(.+?)\s*\d[\d,]*\s*条影片`)
	missavYearRe  = regexp.MustCompile(`\b((?:19|20)\d{2})\b`)
)

// missavParseIndex ports py parse_index(): folder entries for
// actresses/genres/makers lists, RawID = site path (caller adds "idx:").
func missavParseIndex(page, prefix string) []g2Work {
	pat := regexp.MustCompile(`(?is)<a[^>]+href=["']?(https?://[^"'\s>]+/(?:dm\d+/)?cn/` + prefix + `/([^"'\s>/]+))["']?[^>]*>(.*?)</a>`)
	out := []g2Work{}
	seen := map[string]bool{}
	used := map[string]bool{}
	for _, m := range pat.FindAllStringSubmatchIndex(page, -1) {
		slug := page[m[4]:m[5]]
		body := page[m[6]:m[7]]
		name, _ := url.PathUnescape(slug)
		if name == "" || name == "ranking" || seen[slug] {
			continue
		}
		seen[slug] = true
		pic := missavPickImage(body)
		prefer := ""
		if prefix == "actresses" {
			prefer = "/actress/"
		}
		if (prefer != "" && !strings.Contains(pic, prefer)) || pic == "" {
			for _, cand := range missavPickWindowImages(page, m[0]-1600, m[1]+1600, m[0], prefer) {
				if !used[cand] {
					pic = cand
					break
				}
			}
		}
		if pic != "" {
			used[pic] = true
		}
		text := g2TxtOf(body)
		disp := name
		if dm := missavDispRe.FindStringSubmatch(text); dm != nil && len([]rune(dm[1])) <= len([]rune(name))+4 {
			if t := strings.TrimSpace(dm[1]); t != "" {
				disp = t
			}
		}
		remarks := ""
		if cm := missavCountRe.FindStringSubmatch(text); cm != nil {
			remarks = cm[1] + " 部"
		}
		if ym := missavYearRe.FindStringSubmatch(text); ym != nil && prefix == "actresses" {
			if remarks != "" {
				remarks += " · "
			}
			remarks += ym[1] + " 出道"
		}
		out = append(out, g2Work{
			RawID: "/cn/" + prefix + "/" + slug,
			Title: disp, Cover: pic, Remark: remarks,
		})
	}
	return out
}

// missavExtractM3u8 ports py extract_m3u8() verbatim (three tiers).
func missavExtractM3u8(page string) string {
	re1 := regexp.MustCompile(`(?s)'m3u8(.*?)video`)
	if m := re1.FindStringSubmatch(page); m != nil {
		parts := strings.Split(m[1], "|")
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		if len(parts) >= 9 && parts[2] != "" && parts[3] != "" {
			return fmt.Sprintf("%s://%s.%s/%s-%s-%s-%s-%s/playlist.m3u8",
				parts[1], parts[2], parts[3], parts[4], parts[5], parts[6], parts[7], parts[8])
		}
	}
	if m := regexp.MustCompile(`https?://[^\s"']+/playlist\.m3u8`).FindString(page); m != "" {
		return m
	}
	re3 := regexp.MustCompile(`https?:[\\/]+surrit\.com[\\/]+([a-f0-9\-]+)[\\/]+`)
	if m := re3.FindStringSubmatch(page); m != nil {
		return "https://surrit.com/" + m[1] + "/playlist.m3u8"
	}
	if m := regexp.MustCompile(`https?://[^\s"']+\.m3u8[^\s"']*`).FindString(page); m != "" {
		return m
	}
	return ""
}

func missavCatName(id string) string {
	for _, c := range missavCats {
		if c.id == id {
			return c.name
		}
	}
	return "MissAV"
}

func missavCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range missavCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

var missavDrillRe = regexp.MustCompile(`^/cn/(actresses|genres|makers)/[^/]+$`)

func missavCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "new"
	}
	if page < 1 {
		page = 1
	}
	// Folder drill passed as a category (py categoryContent drill forms).
	drill := ""
	switch {
	case strings.HasPrefix(category, "idx:"):
		drill = strings.TrimPrefix(category, "idx:")
	case strings.HasPrefix(category, "/cn/"):
		drill = category
	case regexp.MustCompile(`^(actresses|genres|makers)/[^/]+$`).MatchString(category):
		drill = "/cn/" + category
	}
	if drill != "" && missavDrillRe.MatchString(drill) {
		pageHTML := missavFetch(fmt.Sprintf("%s%s?page=%d", missavBase, drill, page))
		if pageHTML == "" {
			// v3 (diff D10): same failure grading as normal
			// categories — a dead fetch is an error, not a fake
			// empty folder.
			return nil, 0, false, fmt.Errorf("missav: fetch failed (drill %s page %d)", drill, page)
		}
		works := missavParseList(pageHTML)
		return g2WorksToItems(slot, works, "MissAV"), 999999, len(works) > 0, nil
	}
	if missavIndexTIDs[category] {
		prefix := missavIndexPrefix[category]
		pageHTML := missavFetch(fmt.Sprintf("%s/cn/%s?page=%d", missavBase, category, page))
		if pageHTML == "" {
			return nil, 0, false, fmt.Errorf("missav: fetch failed (index %s page %d)", category, page)
		}
		entries := missavParseIndex(pageHTML, prefix)
		items := []interface{}{}
		for _, w := range entries {
			items = append(items, makeDrama(slot, "idx:"+w.RawID, w.Title, w.Cover, w.Remark, missavCatName(category), ""))
		}
		return items, 999999, len(items) > 0, nil
	}
	pageHTML := missavFetch(fmt.Sprintf("%s/cn/%s?page=%d", missavBase, category, page))
	if pageHTML == "" {
		// v2: a failed fetch (CF still blocking, network down) must
		// surface as an error so Dart shows its retryable error
		// state; v1 silently returned an empty list, which the UI
		// renders as "没有找到匹配的短剧" and looks like a data bug.
		return nil, 0, false, fmt.Errorf("missav: fetch failed (category %s page %d)", category, page)
	}
	works := missavParseList(pageHTML)
	return g2WorksToItems(slot, works, missavCatName(category)), 999999, len(works) > 0, nil
}

var (
	missavOgTitleRe = regexp.MustCompile(`property="og:title"\s+content="([^"]+)"`)
	missavOgImageRe = regexp.MustCompile(`property="og:image"\s+content="([^"]+)"`)
	missavKwRe      = regexp.MustCompile(`name="keywords"\s+content="([^"]+)"`)
	missavRelDateRe = regexp.MustCompile(`property="og:video:release_date"\s+content="([^"]+)"`)
	missavH1Re      = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	missavIndexIDRe = regexp.MustCompile(`/cn/(actresses|genres|makers)/([^/]+)$`)
)

func missavDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		path := strings.TrimPrefix(rawID, "idx:")
		works := missavParseList(missavFetch(missavBase + path))
		if len(works) == 0 {
			return nil, fmt.Errorf("missav index folder empty: %s", path)
		}
		name := path
		if mm := missavIndexIDRe.FindStringSubmatch(path); mm != nil {
			if un, err := url.PathUnescape(mm[2]); err == nil {
				name = un
			}
		}
		return g2WorksDetail(slot, rawID, name, works[0].Cover,
			fmt.Sprintf("%d 部作品（第一页）", len(works)), "索引", works,
			fmt.Sprintf("共 %d 部作品（第一页）", len(works))), nil
	}
	vid := rawID
	if strings.HasPrefix(vid, "/") {
		vid = "https://missav.ws" + vid
	}
	page := missavFetch(vid)
	title := ""
	if m := missavOgTitleRe.FindStringSubmatch(page); m != nil {
		title = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if title == "" {
		if m := missavH1Re.FindStringSubmatch(page); m != nil {
			title = g2TxtOf(m[1])
		}
	}
	pic := ""
	if m := missavOgImageRe.FindStringSubmatch(page); m != nil {
		pic = m[1]
	}
	if pic == "" {
		code := strings.TrimSuffix(vid, "/")
		if i := strings.LastIndex(code, "/"); i >= 0 {
			code = code[i+1:]
		}
		pic = "https://fourhoi.com/" + code + "/cover-n.jpg"
	}
	tags := ""
	if m := missavKwRe.FindStringSubmatch(page); m != nil {
		tags = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	year := ""
	if m := missavRelDateRe.FindStringSubmatch(page); m != nil && len(m[1]) >= 4 {
		year = m[1][:4]
	}
	if title == "" {
		title = strings.ToUpper(strings.TrimSuffix(vid, "/"))
		if i := strings.LastIndex(title, "/"); i >= 0 {
			title = title[i+1:]
		}
	}
	remark := "网页播放"
	if found := missavExtractM3u8(page); found != "" {
		remark = "正片"
		// v3 (diff D6): py hands the detail-stage m3u8 back so
		// playback needs no second fetch; cache it for Resolve so
		// the CF-fronted page is hit once, not twice.
		missavCachePlay(rawID, found)
	}
	desc := tags
	if year != "" {
		desc = year + " · " + desc
	}
	// NOTE: py appends the shell pic-suffix headers to vod_pic; this core
	// returns the bare cover URL (see file header).
	return makeDrama(slot, rawID, title, pic, remark, "MissAV", desc), nil
}

func missavResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return nil, fmt.Errorf("missav: folder has no direct play url")
	}
	u := rawID
	if strings.HasPrefix(u, "/") {
		u = "https://missav.ws" + u
	}
	if strings.Contains(u, ".m3u8") {
		return playResult(u, slot, rawID, missavUA, "https://missav.ws/", "m3u8", "hls"), nil
	}
	if cached := missavCachedPlay(rawID); cached != "" {
		return playResult(cached, slot, rawID, missavUA, "https://missav.ws/", "m3u8", "hls"), nil
	}
	found := missavExtractM3u8(missavFetch(u))
	if found == "" {
		// py falls back to parse=1 (web sniffing); this core cannot.
		return nil, fmt.Errorf("missav: no m3u8 on detail page")
	}
	return playResult(found, slot, rawID, missavUA, "https://missav.ws/", "m3u8", "hls"), nil
}

// missavPathQuote ports py quote(s, safe="/") path-segment escaping:
// percent-encode every byte outside [A-Za-z0-9\-_.~/]. v3 (diff D7):
// url.QueryEscape turned spaces into "+" and "/" into %2F, which the
// site search does not accept the way the path-quoted form does.
func missavPathQuote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' || c == '/' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func missavSearch(slot, kw string) ([]interface{}, error) {
	u := fmt.Sprintf("%s/cn/search/%s?page=1", missavBase, missavPathQuote(strings.ToLower(strings.TrimSpace(kw))))
	page := missavFetch(u)
	if page == "" {
		return nil, fmt.Errorf("missav: search fetch failed")
	}
	works := missavParseList(page)
	return g2WorksToItems(slot, works, "搜索"), nil
}

// ============================================================
// supjav — supjav.com (WordPress; play via reversed token ->
// lk1.supremejav.com/supjav.php?c= -> 302 embed -> per-host resolve).
// Slot: huangdou. CF hard-blocks datacenter egress (see header note).
// ============================================================

const supjavUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
const supjavHost = "https://supjav.com"

var supjavJumpHosts = []string{"https://lk1.supremejav.com"}

var supjavCats = []struct{ id, name string }{
	{"home", "首页"},
	{"popular", "热门"},
	{"censored", "有码"},
	{"uncensored", "无码"},
	{"amateur", "素人"},
	{"chnsub", "中文字幕"},
	{"reducing", "薄码"},
	{"engsub", "英文字幕"},
	{"maker", "厂商"},
	{"cast", "演员"},
	{"genre", "类型"},
}

var supjavCatPath = map[string]string{
	"home": "", "popular": "/popular",
	"censored": "/category/censored-jav", "uncensored": "/category/uncensored-jav",
	"amateur": "/category/amateur", "chnsub": "/category/chinese-subtitles",
	"reducing": "/category/reducing-mosaic", "engsub": "/category/english-subtitles",
}

var supjavIndexPath = map[string]string{"maker": "/maker", "cast": "/cast", "genre": "/tag"}

var supjavIndexEntryRe = map[string]*regexp.Regexp{
	"maker": regexp.MustCompile(`^/category/maker/[a-z0-9\-]+$`),
	"cast":  regexp.MustCompile(`^/category/cast/[a-z0-9\-]+$`),
	"genre": regexp.MustCompile(`^/tag/[a-z0-9\-&]+$`),
}

func supjavHeaders(referer string) map[string]string {
	if referer == "" {
		referer = supjavHost + "/"
	}
	return map[string]string{
		"User-Agent":                supjavUA,
		"Referer":                   referer,
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"Accept-Language":           "en-US,en;q=0.9",
		"Upgrade-Insecure-Requests": "1",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "same-origin",
	}
}

func supjavIsCFBlock(text string) bool {
	if text == "" {
		return false
	}
	head := text
	if len(head) > 8000 {
		head = head[:8000]
	}
	return strings.Contains(head, "Attention Required") ||
		strings.Contains(head, "Performing security verification") ||
		strings.Contains(head, "/cdn-cgi/challenge-platform") ||
		strings.Contains(head, "Just a moment")
}

func supjavFetchOnce(rawurl, referer string) string {
	body, _, _, err := tlsGet(rawurl, supjavHeaders(referer))
	if err != nil {
		// 传输失败：走全壳免代兜底（providers.go jinaFallback），与公共
		// 抓取通道同一出口。此前 supjav 自研 fetch 只直连、失败即空，
		// 是真机 CF/传输失败时整站报错的根因。
		if jb, _, _, _, ok := jinaFallback(rawurl, supjavHeaders(referer), nil, 0, err); ok {
			return string(jb)
		}
		return ""
	}
	return string(body)
}

// supjavJinaRetry asks the shared smart-route fallback for a copy of a
// page the direct route could only fetch as a Cloudflare block page.
// The block body is handed over with a synthetic 403 when the real
// status was not one of the fallback's trigger codes — the fallback
// re-validates the challenge signature itself before spending a fetch.
func supjavJinaRetry(rawurl, referer, blocked string) string {
	code := 403
	if jb, _, _, _, ok := jinaFallback(rawurl, supjavHeaders(referer), []byte(blocked), code, nil); ok {
		if t := string(jb); t != "" && !supjavIsCFBlock(t) {
			return t
		}
	}
	return ""
}

// supjavFetch ports py fetch(): on a CF block page for the bare domain,
// retry once on the www subdomain. v3: when every direct route still
// lands on a CF block page, fall back to the shared jina smart route
// (previously the block page propagated and the whole site errored).
func supjavFetch(rawurl, referer string) string {
	text := supjavFetchOnce(rawurl, referer)
	if supjavIsCFBlock(text) {
		blocked := text
		if u, err := url.Parse(rawurl); err == nil && u.Hostname() == "supjav.com" {
			alt := strings.Replace(rawurl, "://supjav.com", "://www.supjav.com", 1)
			if t2 := supjavFetchOnce(alt, referer); t2 != "" && !supjavIsCFBlock(t2) {
				return t2
			} else if supjavIsCFBlock(t2) {
				if t3 := supjavJinaRetry(alt, referer, t2); t3 != "" {
					return t3
				}
			}
		}
		if t := supjavJinaRetry(rawurl, referer, blocked); t != "" {
			return t
		}
	}
	return text
}

// supjavFetchLocation ports py fetch_location(): no redirect following;
// returns (status, Location-or-body). v2: via the impersonated TLS
// transport.
func supjavFetchLocation(rawurl, referer string) (int, string) {
	return tlsFetchLocation(rawurl, supjavHeaders(referer))
}

var (
	supjavCardHrefRe   = regexp.MustCompile(`href="((?:https?://[^"/]+)?/\d+\.html)"`)
	supjavDataOrigRe   = regexp.MustCompile(`data-original="([^"]+)"`)
	supjavImgSrcRe     = regexp.MustCompile(`<img[^>]+src="(https?://img\.[^"]+)"`)
	supjavTitleRe1     = regexp.MustCompile(`<h3[^>]*>\s*<a[^>]*title="([^"]+)"`)
	supjavTitleRe2     = regexp.MustCompile(`<a[^>]+class="img"[^>]*title="([^"]+)"`)
	supjavTitleRe3     = regexp.MustCompile(`(?s)<h3[^>]*>\s*<a[^>]*>(.*?)</a>`)
	supjavMetaRe       = regexp.MustCompile(`(?s)<div class="meta">(.*?)</div>`)
	supjavPageRe       = regexp.MustCompile(`/page/(\d+)`)
	supjavAnchorRe     = regexp.MustCompile(`(?s)<a[^>]+href="((?:https?://[^"/]+)?[^"]+)"[^>]*>(.*?)</a>`)
	supjavH1Re         = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	supjavPlayerPicRe  = regexp.MustCompile(`id="player-wrap"[^>]*background-image:\s*url\(([^)]+)\)`)
	supjavDetailImgRe  = regexp.MustCompile(`<img src="(https?://img\.[^"]+)" class="img"`)
	supjavCatRe        = regexp.MustCompile(`<p class="cat"><a[^>]*>([^<]+)</a>`)
	supjavTagsRe       = regexp.MustCompile(`(?s)<div class="tags">(.*?)</div>`)
	supjavAnchorTxtRe  = regexp.MustCompile(`<a[^>]*>([^<]+)</a>`)
	supjavBtnServerRe  = regexp.MustCompile(`(?s)<a[^>]*btn-server[^>]*>.*?</a>`)
	supjavDataLinkRe   = regexp.MustCompile(`data-link="([0-9a-fA-F]{16,})"`)
	supjavIframeRe     = regexp.MustCompile(`<iframe[^>]+src="([^"]+)"`)
	supjavM3u8Re       = regexp.MustCompile(`https?://[^\s"'<>\\]+\.m3u8[^\s"'<>\\]*`)
	supjavMp4Re        = regexp.MustCompile(`https?://[^\s"'<>\\]+\.mp4[^\s"'<>\\]*`)
	supjavB64Re        = regexp.MustCompile(`[A-Za-z0-9+/=]{120,}`)
	supjavStreamTapeRe = regexp.MustCompile(`get_video\?id=([A-Za-z0-9]+)&(?:amp;)?expires=(\d+)&(?:amp;)?ip=([^&"'<]+)&(?:amp;)?token=([^&"'<]+)`)
)

// supjavParseCards ports py parse_cards().
func supjavParseCards(page string) []g2Work {
	out := []g2Work{}
	seen := map[string]bool{}
	if page == "" {
		return out
	}
	for _, chunk := range strings.Split(page, `<div class="post">`)[1:] {
		m := supjavCardHrefRe.FindStringSubmatch(chunk)
		if m == nil {
			continue
		}
		u, err := url.Parse(m[1])
		if err != nil {
			continue
		}
		path := u.Path
		if seen[path] {
			continue
		}
		seen[path] = true
		pic := ""
		if pm := supjavDataOrigRe.FindStringSubmatch(chunk); pm != nil {
			pic = pm[1]
		} else if pm := supjavImgSrcRe.FindStringSubmatch(chunk); pm != nil {
			pic = pm[1]
		}
		title := ""
		if tm := supjavTitleRe1.FindStringSubmatch(chunk); tm != nil {
			title = g2TxtOf(tm[1])
		} else if tm := supjavTitleRe2.FindStringSubmatch(chunk); tm != nil {
			title = g2TxtOf(tm[1])
		} else if tm := supjavTitleRe3.FindStringSubmatch(chunk); tm != nil {
			title = g2TxtOf(tm[1])
		}
		if title == "" {
			title = path
		}
		remark := ""
		if rm := supjavMetaRe.FindStringSubmatch(chunk); rm != nil {
			remark = g2TxtOf(rm[1])
		}
		out = append(out, g2Work{RawID: path, Title: title, Cover: pic, Remark: remark})
	}
	return out
}

func supjavPageURL(basePath string, pg int) string {
	if pg <= 1 {
		if basePath == "" {
			return supjavHost + "/"
		}
		return supjavHost + basePath
	}
	return fmt.Sprintf("%s%s/page/%d", supjavHost, basePath, pg)
}

func supjavPagecount(page string, items []g2Work, pg int) int {
	mx := pg
	for _, m := range supjavPageRe.FindAllStringSubmatch(page, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > mx {
			mx = n
		}
	}
	if len(items) >= 24 {
		if mx == pg {
			return 9999
		}
		return mx
	}
	return pg
}

// supjavParseIndex ports py parse_index(): folder entries (RawID = path).
func supjavParseIndex(page, kind string) []g2Work {
	out := []g2Work{}
	seen := map[string]bool{}
	pat := supjavIndexEntryRe[kind]
	for _, m := range supjavAnchorRe.FindAllStringSubmatch(page, -1) {
		u, err := url.Parse(m[1])
		if err != nil {
			continue
		}
		path := u.Path
		if !pat.MatchString(path) || seen[path] {
			continue
		}
		name := g2TxtOf(m[2])
		if !strings.Contains(name, "(") {
			continue
		}
		seen[path] = true
		// py leaves folder covers empty on index pages.
		out = append(out, g2Work{RawID: path, Title: name})
	}
	return out
}

func supjavCatName(id string) string {
	for _, c := range supjavCats {
		if c.id == id {
			return c.name
		}
	}
	return "SupJav"
}

func supjavCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range supjavCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func supjavCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	if idxPath, ok := supjavIndexPath[category]; ok {
		htmlText := supjavFetch(supjavPageURL(idxPath, page), "")
		if htmlText == "" || supjavIsCFBlock(htmlText) {
			return nil, 0, false, fmt.Errorf("supjav: fetch failed or CF-blocked (index %s)", category)
		}
		entries := supjavParseIndex(htmlText, category)
		items := []interface{}{}
		for _, w := range entries {
			items = append(items, makeDrama(slot, "idx:"+w.RawID, w.Title, w.Cover, w.Remark, supjavCatName(category), ""))
		}
		hasMore := len(items) >= 100
		return items, len(items), hasMore, nil
	}
	path := ""
	if strings.HasPrefix(category, "idx:") {
		path = strings.TrimPrefix(category, "idx:")
	} else if strings.HasPrefix(category, "/") {
		path = category
	} else {
		path = supjavCatPath[category]
	}
	htmlText := supjavFetch(supjavPageURL(path, page), "")
	if htmlText == "" || supjavIsCFBlock(htmlText) {
		// v2: fetch failure / CF block surfaces as an error
		// (retryable error state in Dart), not a fake empty list.
		return nil, 0, false, fmt.Errorf("supjav: fetch failed or CF-blocked (category %s page %d)", category, page)
	}
	works := supjavParseCards(htmlText)
	pc := supjavPagecount(htmlText, works, page)
	return g2WorksToItems(slot, works, supjavCatName(category)), pc * 24, page < pc, nil
}

type supjavServer struct{ name, token string }

// supjavDetailGroups extracts the CD/server groups from a detail page.
func supjavDetailGroups(page string) [][]supjavServer {
	var blocks []string
	if strings.Contains(page, "cd-server") {
		re := regexp.MustCompile(`class="cd-server[^"]*"`)
		parts := re.Split(page, -1)
		if len(parts) > 1 {
			blocks = parts[1:]
		}
	} else {
		blocks = []string{page}
	}
	groups := [][]supjavServer{}
	for _, b := range blocks {
		servers := []supjavServer{}
		for _, am := range supjavBtnServerRe.FindAllString(b, -1) {
			dm := supjavDataLinkRe.FindStringSubmatch(am)
			if dm == nil {
				continue
			}
			name := g2TxtOf(am)
			if name == "" {
				name = "S"
			}
			servers = append(servers, supjavServer{name, dm[1]})
		}
		if len(servers) > 0 {
			groups = append(groups, servers)
		}
	}
	return groups
}

func supjavDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		path := strings.TrimPrefix(rawID, "idx:")
		works := supjavParseCards(supjavFetch(supjavPageURL(path, 1), ""))
		if len(works) == 0 {
			return nil, fmt.Errorf("supjav index folder empty: %s", path)
		}
		name := works[0].Title
		_ = name
		return g2WorksDetail(slot, rawID, path, works[0].Cover, "", "索引", works, ""), nil
	}
	u := rawID
	if !strings.HasPrefix(u, "http") {
		u = supjavHost + u
	}
	page := supjavFetch(u, "")
	title := ""
	if m := supjavH1Re.FindStringSubmatch(page); m != nil {
		title = g2TxtOf(m[1])
	}
	if title == "" {
		title = rawID
	}
	pic := ""
	if pm := supjavPlayerPicRe.FindStringSubmatch(page); pm != nil {
		pic = pm[1]
	} else if pm := supjavDetailImgRe.FindStringSubmatch(page); pm != nil {
		pic = pm[1]
	}
	info := []string{}
	if cm := supjavCatRe.FindStringSubmatch(page); cm != nil {
		info = append(info, "分类: "+g2TxtOf(cm[1]))
	}
	actor := ""
	for _, label := range []string{"Maker", "Cast"} {
		lm := regexp.MustCompile(`<span>` + label + ` : </span>(.*?)(?:</p>|<br)`).FindStringSubmatch(page)
		if lm == nil {
			continue
		}
		names := []string{}
		for _, am := range supjavAnchorTxtRe.FindAllStringSubmatch(lm[1], -1) {
			names = append(names, g2TxtOf(am[1]))
		}
		if len(names) > 0 {
			joined := strings.Join(names, ", ")
			info = append(info, label+": "+joined)
			if label == "Cast" {
				actor = joined
			}
		}
	}
	_ = actor
	if tm := supjavTagsRe.FindStringSubmatch(page); tm != nil {
		tags := []string{}
		for _, am := range supjavAnchorTxtRe.FindAllStringSubmatch(tm[1], -1) {
			tags = append(tags, g2TxtOf(am[1]))
		}
		if len(tags) > 0 {
			info = append(info, "标签: "+strings.Join(tags, ", "))
		}
	}
	d := makeDrama(slot, rawID, title, pic, "", "SupJav", strings.Join(info, " | "))
	groups := supjavDetailGroups(page)
	if len(groups) > 0 {
		code := title
		if f := strings.Fields(title); len(f) > 0 {
			code = f[0]
		}
		chs := []interface{}{}
		for li, first := range groups[0] {
			for gi, g := range groups {
				tok := ""
				for _, s := range g {
					if s.name == first.name {
						tok = s.token
						break
					}
				}
				if tok == "" && li < len(g) {
					tok = g[li].token
				}
				if tok == "" {
					continue
				}
				ep := code
				if len(groups) > 1 {
					ep = fmt.Sprintf("%s-%d", code, gi+1)
				}
				chs = append(chs, g2Chapter(slot, rawID, tok, first.name+" "+ep, len(chs)+1))
			}
		}
		if len(chs) > 0 {
			g2SetChapters(d, chs)
		}
	}
	return d, nil
}

func supjavSearch(slot, kw string) ([]interface{}, error) {
	u := supjavHost + "/?s=" + url.QueryEscape(kw)
	works := supjavParseCards(supjavFetch(u, ""))
	return g2WorksToItems(slot, works, "搜索"), nil
}

func supjavResolveStreamtape(embed string) string {
	page := supjavFetch(embed, "")
	if page == "" {
		return ""
	}
	m := supjavStreamTapeRe.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	return fmt.Sprintf("https://streamtape.com/get_video?id=%s&expires=%s&ip=%s&token=%s", m[1], m[2], m[3], m[4])
}

func supjavResolveGeneric(embed string) string {
	page := supjavFetch(embed, "")
	if page == "" {
		return ""
	}
	if m := supjavM3u8Re.FindString(page); m != "" {
		return m
	}
	if m := supjavMp4Re.FindString(page); m != "" && !strings.Contains(m, "thumb") && !strings.Contains(m, "poster") {
		return m
	}
	for _, bm := range supjavB64Re.FindAllString(page, -1) {
		dec, err := base64.StdEncoding.DecodeString(bm + "==")
		if err != nil {
			continue
		}
		if mm := supjavM3u8Re.FindString(string(dec)); mm != "" {
			return mm
		}
	}
	return ""
}

// supjavResolveToken ports py playerContent()'s token flow.
func supjavResolveToken(slot, rawID, token string) (map[string]interface{}, error) {
	embed := ""
	cURL := ""
	for _, j := range supjavJumpHosts {
		cURL = j + "/supjav.php?c=" + g2Reverse(token)
		code, loc := supjavFetchLocation(cURL, j+"/supjav.php")
		if code >= 301 && code <= 308 && strings.HasPrefix(loc, "http") {
			embed = loc
			break
		}
		if code == 200 && loc != "" {
			if fm := supjavIframeRe.FindStringSubmatch(loc); fm != nil {
				if base, err := url.Parse(cURL); err == nil {
					if ref, rerr := url.Parse(fm[1]); rerr == nil {
						embed = base.ResolveReference(ref).String()
					}
				}
				if embed == "" {
					embed = fm[1]
				}
				break
			}
			embed = cURL
			break
		}
	}
	if embed == "" {
		// py returns parse=1 with the jump URL; this core cannot sniff.
		return nil, fmt.Errorf("supjav: token jump failed (no embed)")
	}
	host := ""
	if u, err := url.Parse(embed); err == nil {
		host = strings.ToLower(u.Hostname())
	}
	if strings.Contains(host, "streamtape.") || strings.Contains(host, "tapecontent.") {
		if direct := supjavResolveStreamtape(embed); direct != "" {
			return playResult(direct, slot, rawID, supjavUA, "https://streamtape.com/", "mp4", "file"), nil
		}
		return nil, fmt.Errorf("supjav: streamtape resolve failed")
	}
	if direct := supjavResolveGeneric(embed); direct != "" {
		format, proto := g2PlayFormat(direct)
		return playResult(direct, slot, rawID, supjavUA, embed, format, proto), nil
	}
	// py returns parse=1 with the embed URL; this core cannot sniff.
	return nil, fmt.Errorf("supjav: embed has no direct stream")
}

var supjavTokenRe = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)

func supjavResolve(slot, rawID string) (map[string]interface{}, error) {
	token := strings.TrimSpace(rawID)
	if strings.HasPrefix(token, "idx:") {
		return nil, fmt.Errorf("supjav: folder has no direct play url")
	}
	if supjavTokenRe.MatchString(token) {
		return supjavResolveToken(slot, rawID, token)
	}
	// A detail path was passed: fetch the detail page, take the first
	// server token (mirrors opening the first line in the py source).
	u := token
	if !strings.HasPrefix(u, "http") {
		u = supjavHost + u
	}
	groups := supjavDetailGroups(supjavFetch(u, ""))
	if len(groups) == 0 || len(groups[0]) == 0 {
		return nil, fmt.Errorf("supjav: no server token on detail page")
	}
	return supjavResolveToken(slot, rawID, groups[0][0].token)
}

// ---- small net helpers used by the supjav no-redirect hop ----

func newGetRequest(rawurl string, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequest("GET", rawurl, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

func readBodyLimited(resp *http.Response) string {
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return ""
	}
	return string(b)
}

// ============================================================
// aipornx — www.aipornx.cc (vod-card HTML; play via play page
// player_data -> huangguo.php -> base64 token -> m3u8).
// Slot: yaguo. Multi-episode dramas: Detail returns one chapter
// per episode (episodeId = /play/.../ path).
// ============================================================

const aipUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
const aipHost = "https://www.aipornx.cc"
const aipAltHost = "https://aipornx.cc"

func aipHeaders() map[string]string {
	return map[string]string{
		"User-Agent": aipUA,
		"Referer":    "https://www.aipornx.cc/",
	}
}

var aipCats = []struct{ id, name string }{
	{"home", "首页"},
	{"latest", "最新更新"},
	{"/aidj/anthurium/", "AI成人短剧"},
	{"/aidj/camelia/", "AI成人漫剧"},
	{"/aidj/daisy/", "AI换脸"},
	{"/aidj/frangipani/", "AI魔改"},
	{"tags_index", "标签选剧"},
}

var aipTagsSnapshot = []string{"现代", "都市", "奇幻", "大男主", "系统", "后宫"}

// aipFetch ports py http_get(): primary host first, bare-domain
// fallback on failure.
func aipFetch(rawurl string) (string, error) {
	candidates := []string{rawurl}
	if strings.HasPrefix(rawurl, aipHost) {
		candidates = append(candidates, aipAltHost+strings.TrimPrefix(rawurl, aipHost))
	} else if strings.HasPrefix(rawurl, aipAltHost) {
		candidates = append(candidates, aipHost+strings.TrimPrefix(rawurl, aipAltHost))
	}
	var lastErr error
	for _, u := range candidates {
		body, code, _, err := httpGet(u, aipHeaders())
		if err == nil && code == 200 {
			return string(body), nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("aipornx http %d for %s", code, u)
		}
	}
	return "", lastErr
}

var (
	aipCardRe       = regexp.MustCompile(`(?s)<article class="vod-card">(.*?)</article>`)
	aipCardLinkRe   = regexp.MustCompile(`<a class="vod-pic" href="(/detail/\d+/)\s*"`)
	aipCardImgRe    = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	aipCardNameRe   = regexp.MustCompile(`<div class="vod-name"><a[^>]*>([^<]+)</a>`)
	aipCardAltRe    = regexp.MustCompile(`<img[^>]+alt="([^"]+)"`)
	aipCardRemRe    = regexp.MustCompile(`class="vod-remarks">([^<]+)<`)
	aipPageNumRe    = regexp.MustCompile(`/page/(\d+)/`)
	aipCurrentRe    = regexp.MustCompile(`class="[^"]*current[^"]*"[^>]*>\s*(\d+)`)
	aipDashPageRe   = regexp.MustCompile(`-{10}(\d+)-`)
	aipTagIdxRe     = regexp.MustCompile(`href="/search/-+%E[^"]+"[^>]*>#([^<]+)</a>`)
	aipOgTitleRe    = regexp.MustCompile(`property="og:title" content="([^"]+)"`)
	aipOgImageRe    = regexp.MustCompile(`property="og:image" content="([^"]+)"`)
	aipOgDescRe     = regexp.MustCompile(`property="og:description" content="([^"]+)"`)
	aipH1Re         = regexp.MustCompile(`<div class="detail-info"><h1>([^<]+)</h1>`)
	aipDetailTagRe  = regexp.MustCompile(`class="detail-tag"[^>]*>#([^<]+)</a>`)
	aipEpRe         = regexp.MustCompile(`<a class="ep[^"]*"[^>]*href="(/play/[\d\-]+/)"[^>]*>([^<]+)</a>`)
	aipEpRe2        = regexp.MustCompile(`href="(/play/[\d\-]+/)"[^>]*>(第[^<]*集)</a>`)
	aipEpNoRe       = regexp.MustCompile(`-(\d+)/$`)
	aipPlayerDataRe = regexp.MustCompile(`(?s)player_data=(\{.*?\})</script>`)
)

func aipFixPic(pic string) string {
	return strings.Replace(pic, "https://media.hgpicimg.cc//", "https://media.hgpicimg.cc/", 1)
}

// aipParseCards ports py parse_cards().
func aipParseCards(page string) []g2Work {
	out := []g2Work{}
	seen := map[string]bool{}
	for _, m := range aipCardRe.FindAllStringSubmatch(page, -1) {
		blk := m[1]
		lm := aipCardLinkRe.FindStringSubmatch(blk)
		if lm == nil {
			continue
		}
		path := lm[1]
		if seen[path] {
			continue
		}
		seen[path] = true
		pic := ""
		if im := aipCardImgRe.FindStringSubmatch(blk); im != nil {
			pic = aipFixPic(im[1])
		}
		name := ""
		if nm := aipCardNameRe.FindStringSubmatch(blk); nm != nil {
			name = g2TxtOf(nm[1])
		} else if nm := aipCardAltRe.FindStringSubmatch(blk); nm != nil {
			name = g2TxtOf(nm[1])
		}
		if name == "" {
			name = path
		}
		remark := ""
		if rm := aipCardRemRe.FindStringSubmatch(blk); rm != nil {
			remark = g2TxtOf(rm[1])
		}
		out = append(out, g2Work{RawID: path, Title: name, Cover: pic, Remark: remark})
	}
	return out
}

func aipPagecount(page string, pg int) int {
	pc := pg
	scan := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(page, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil && n > pc {
				pc = n
			}
		}
	}
	scan(aipPageNumRe)
	scan(aipCurrentRe)
	scan(aipDashPageRe)
	return pc
}

func aipCatName(id string) string {
	for _, c := range aipCats {
		if c.id == id {
			return c.name
		}
	}
	return "AiPorn"
}

func aipCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range aipCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

// aipTagsIndex ports py _tags_index(): live tag anchors from /home/,
// snapshot fallback. Returns folder works (RawID = "tag:{tag}").
func aipTagsIndex() []g2Work {
	tags := []string{}
	seen := map[string]bool{}
	page, _ := aipFetch(aipHost + "/home/")
	for _, m := range aipTagIdxRe.FindAllStringSubmatch(page, -1) {
		t := g2TxtOf(m[1])
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	if len(tags) == 0 {
		tags = aipTagsSnapshot
	}
	out := []g2Work{}
	for _, t := range tags {
		// py folder cards carry no cover on this index.
		out = append(out, g2Work{RawID: "tag:" + t, Title: t})
	}
	return out
}

func aipListCatalog(slot, page string, pg int, catName string) ([]interface{}, int, bool, error) {
	works := aipParseCards(page)
	pc := aipPagecount(page, pg)
	return g2WorksToItems(slot, works, catName), pc * 30, pg < pc, nil
}

func aipCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "" || category == "home":
		page1, err := aipFetch(aipHost + "/home/")
		if err != nil {
			return nil, 0, false, err
		}
		works := aipParseCards(page1)
		return g2WorksToItems(slot, works, "首页"), len(works), false, nil
	case category == "tags_index":
		entries := aipTagsIndex()
		items := []interface{}{}
		for _, w := range entries {
			items = append(items, makeDrama(slot, "idx:"+w.RawID, w.Title, w.Cover, w.Remark, "标签选剧", ""))
		}
		return items, len(items), false, nil
	case category == "latest":
		u := aipHost + "/search/-------------/"
		if page > 1 {
			u = fmt.Sprintf("%s/search/----------%d---/", aipHost, page)
		}
		pageStr, err := aipFetch(u)
		if err != nil {
			return nil, 0, false, err
		}
		return aipListCatalog(slot, pageStr, page, "最新更新")
	case strings.HasPrefix(category, "idx:"):
		// Folder rawID routed as a category: treat as its target list.
		return aipCatalog(slot, strings.TrimPrefix(category, "idx:"), page)
	case strings.HasPrefix(category, "tag:"):
		tag := strings.TrimPrefix(category, "tag:")
		q := url.QueryEscape(tag)
		u := ""
		if page > 1 {
			u = fmt.Sprintf("%s/search/----------%d--%s-/", aipHost, page, q)
		} else {
			u = fmt.Sprintf("%s/search/------------%s-/", aipHost, q)
		}
		pageStr, err := aipFetch(u)
		if err != nil {
			return nil, 0, false, err
		}
		return aipListCatalog(slot, pageStr, page, tag)
	default:
		path := category
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		u := aipHost + path
		if page > 1 {
			u = aipHost + strings.TrimSuffix(path, "/") + fmt.Sprintf("/page/%d/", page)
		}
		pageStr, err := aipFetch(u)
		if err != nil {
			return nil, 0, false, err
		}
		return aipListCatalog(slot, pageStr, page, aipCatName(category))
	}
}

type aipEpisode struct {
	path, name string
	no         int
}

func aipParseEpisodes(page string) []aipEpisode {
	eps := map[string]string{}
	for _, m := range aipEpRe.FindAllStringSubmatch(page, -1) {
		eps[m[1]] = g2TxtOf(m[2])
	}
	if len(eps) == 0 {
		for _, m := range aipEpRe2.FindAllStringSubmatch(page, -1) {
			eps[m[1]] = g2TxtOf(m[2])
		}
	}
	out := []aipEpisode{}
	for p, n := range eps {
		no := 0
		if mm := aipEpNoRe.FindStringSubmatch(p); mm != nil {
			no, _ = strconv.Atoi(mm[1])
		}
		out = append(out, aipEpisode{p, n, no})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].no < out[j].no })
	return out
}

func aipDetailPath(rawID string) string {
	if strings.HasPrefix(rawID, "/") {
		return rawID
	}
	if strings.HasPrefix(rawID, "http") {
		if u, err := url.Parse(rawID); err == nil {
			return u.Path
		}
	}
	return "/" + strings.Trim(rawID, "/") + "/"
}

func aipDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		rest := strings.TrimPrefix(rawID, "idx:")
		if !strings.HasPrefix(rest, "tag:") {
			return nil, fmt.Errorf("aipornx: unknown folder %s", rawID)
		}
		tag := strings.TrimPrefix(rest, "tag:")
		q := url.QueryEscape(tag)
		page, err := aipFetch(fmt.Sprintf("%s/search/------------%s-/", aipHost, q))
		if err != nil {
			return nil, err
		}
		works := aipParseCards(page)
		if len(works) == 0 {
			return nil, fmt.Errorf("aipornx: tag folder empty: %s", tag)
		}
		return g2WorksDetail(slot, rawID, tag, works[0].Cover, "", "标签选剧", works, ""), nil
	}
	path := aipDetailPath(rawID)
	page, err := aipFetch(aipHost + path)
	if err != nil {
		return nil, err
	}
	if page == "" {
		return nil, fmt.Errorf("aipornx: detail fetch failed")
	}
	title := ""
	if m := aipOgTitleRe.FindStringSubmatch(page); m != nil {
		title = g2TxtOf(m[1])
	}
	if title == "" {
		if m := aipH1Re.FindStringSubmatch(page); m != nil {
			title = g2TxtOf(m[1])
		}
	}
	if title == "" {
		title = path
	}
	pic := ""
	if m := aipOgImageRe.FindStringSubmatch(page); m != nil {
		pic = aipFixPic(m[1])
	}
	desc := ""
	if m := aipOgDescRe.FindStringSubmatch(page); m != nil {
		desc = g2TxtOf(m[1])
	}
	tags := []string{}
	for _, m := range aipDetailTagRe.FindAllStringSubmatch(page, -1) {
		t := g2TxtOf(m[1])
		dup := false
		for _, x := range tags {
			if x == t {
				dup = true
			}
		}
		if t != "" && !dup {
			tags = append(tags, t)
		}
	}
	if len(tags) > 0 {
		if desc != "" {
			desc += "\n"
		}
		desc += "标签：" + strings.Join(tags, " / ")
	}
	eps := aipParseEpisodes(page)
	remark := ""
	if len(eps) > 0 {
		remark = fmt.Sprintf("更新至%d集", len(eps))
	}
	d := makeDrama(slot, path, title, pic, remark, "AiPorn", desc)
	if len(eps) > 0 {
		chs := []interface{}{}
		for i, ep := range eps {
			chs = append(chs, g2Chapter(slot, path, ep.path, ep.name, i+1))
		}
		g2SetChapters(d, chs)
	}
	return d, nil
}

// aipResolvePlayPage ports py playerContent()'s two-hop decode.
func aipResolvePlayPage(slot, rawID, playPath string) (map[string]interface{}, error) {
	pageURL := aipHost + playPath
	page, err := aipFetch(pageURL)
	if err != nil {
		return nil, err
	}
	real := ""
	proxy := ""
	if m := aipPlayerDataRe.FindStringSubmatch(page); m != nil {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &data); err == nil {
			raw, _ := data["url"].(string)
			parts := strings.Split(raw, "|")
			if len(parts) >= 2 {
				api := fmt.Sprintf("%s/huangguo.php?ac=play&id=%s&ep=%s&_=%d",
					aipHost, url.QueryEscape(parts[0]), url.QueryEscape(parts[1]),
					time.Now().UnixMilli())
				jsText, _ := aipFetch(api)
				var d map[string]interface{}
				if jsText != "" && json.Unmarshal([]byte(jsText), &d) == nil {
					u, _ := d["url"].(string)
					code, _ := d["code"].(float64)
					if code == 1 && strings.HasPrefix(u, "/hgmedia.php?e=") {
						proxy = aipHost + u
						token := strings.SplitN(u, "e=", 2)[1]
						for len(token)%4 != 0 {
							token += "="
						}
						if dec, err := base64.StdEncoding.DecodeString(token); err == nil {
							ds := string(dec)
							if strings.HasPrefix(ds, "http") && strings.Contains(ds, ".m3u8") {
								real = ds
							}
						}
					}
				}
			}
		}
	}
	final := real
	if final == "" {
		final = proxy
	}
	if final == "" {
		// py falls back to parse=1 with the play page; this core cannot.
		return nil, fmt.Errorf("aipornx: play resolve failed")
	}
	return playResult(final, slot, rawID, aipUA, aipHost+"/", "m3u8", "hls"), nil
}

func aipResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return nil, fmt.Errorf("aipornx: folder has no direct play url")
	}
	p := rawID
	if strings.HasPrefix(p, "http") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
		}
	}
	if strings.Contains(p, "/play/") {
		return aipResolvePlayPage(slot, rawID, p)
	}
	// Detail path: resolve its first episode (chapter ids normally
	// carry the play path directly).
	path := aipDetailPath(rawID)
	page, err := aipFetch(aipHost + path)
	if err != nil {
		return nil, err
	}
	eps := aipParseEpisodes(page)
	if len(eps) == 0 {
		return nil, fmt.Errorf("aipornx: no episodes on detail page")
	}
	return aipResolvePlayPage(slot, rawID, eps[0].path)
}

func aipSearch(slot, kw string) ([]interface{}, error) {
	u := fmt.Sprintf("%s/search/-------------/?wd=%s", aipHost, url.QueryEscape(kw))
	page, err := aipFetch(u)
	if err != nil {
		return nil, err
	}
	works := aipParseCards(page)
	return g2WorksToItems(slot, works, "搜索"), nil
}

// ============================================================
// javhd — javhd.today (mixed list shapes: sort pages, ?ajax=1 JSON
// for content categories, ?page= for pornstar/channel pages,
// playlist pages; play via embed page data-* base64 server table
// and the FIRST player config). Slot: dsd.
// ============================================================

const javhdUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
const javhdAltHost = "https://javhdz.today"

var (
	javhdMu        sync.Mutex
	javhdHostCur   = "https://javhd.today"
	javhdCatsCache []struct{ id, name string }
	javhdCatIDs    map[string]bool
)

func javhdHost() string {
	javhdMu.Lock()
	defer javhdMu.Unlock()
	return javhdHostCur
}

func javhdHeaders(referer string) map[string]string {
	if referer == "" {
		referer = javhdHost() + "/cn/"
	}
	return map[string]string{
		"User-Agent": javhdUA,
		"Referer":    referer,
		"Accept":     "text/html,application/json,*/*",
	}
}

// javhdFetch ports py _fetch(): 3 tries, alt-host swap on failure
// (the current host flips to the alt host after the 2nd failure).
func javhdFetch(rawurl, referer string) (string, error) {
	u := rawurl
	if !strings.HasPrefix(u, "http") {
		u = javhdHost() + u
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err := httpDo("GET", u, nil, javhdHeaders(referer))
		if err == nil && code == 200 {
			return string(body), nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("javhd http %d", code)
		}
		cur := javhdHost()
		if strings.Contains(u, cur) {
			u = strings.Replace(u, cur, javhdAltHost, 1)
			if attempt == 1 {
				javhdMu.Lock()
				javhdHostCur = javhdAltHost
				javhdMu.Unlock()
			}
		}
	}
	return "", lastErr
}

// Fixed 15 (py self.fixed, order preserved): name, id.
var javhdFixed = []struct{ name, id string }{
	{"最新更新", "/cn/recent/"},
	{"发布日期", "/cn/releaseday/"},
	{"最受欢迎", "/cn/popular/"},
	{"今日最热", "/cn/popular/today/"},
	{"本周最热", "/cn/popular/week/"},
	{"本月最热", "/cn/popular/month/"},
	{"本年最热", "/cn/popular/year/"},
	{"最高评分", "/cn/rated/"},
	{"评论最多", "/cn/discussed/"},
	{"下载最多", "/cn/downloaded/"},
	{"观看最多", "/cn/watched/"},
	{"最长片时", "/cn/longest/"},
	{"女优索引", "idx:pornstar"},
	{"工作室索引", "idx:channels"},
	{"播放列表", "idx:playlists"},
}

// 99-entry snapshot (py self.cat_snapshot, order preserved): name, id.
var javhdCatSnapshot = []struct{ name, id string }{
	{"7mmtv", "/cn/7mmtv/"},
	{"业余", "/cn/amateur/"},
	{"女主播", "/cn/anchorwoman/"},
	{"亚洲", "/cn/asian/"},
	{"Avgle", "/cn/avgle/"},
	{"BBW", "/cn/bbw/"},
	{"美丽女孩", "/cn/beautiful-girl/"},
	{"Bestjav", "/cn/bestjav/"},
	{"大奶子", "/cn/big-tits/"},
	{"乳房", "/cn/breasts/"},
	{"Bukkake", "/cn/bukkake/"},
	{"巨乳恋物癖", "/cn/busty-fetish/"},
	{"车震", "/cn/car-sex/"},
	{"JAV 中文字幕", "/cn/chinese-subtitle/"},
	{"Cosplay", "/cn/cosplay/"},
	{"女牛仔", "/cn/cowgirl/"},
	{"蛋糕", "/cn/creampie/"},
	{"戴绿帽子", "/cn/cuckold/"},
	{"死醉", "/cn/dead-drunk/"},
	{"首次亮相", "/cn/debut-production/"},
	{"深喉", "/cn/deep-throating/"},
	{"脏话", "/cn/dirty-words/"},
	{"纪录片", "/cn/documentary/"},
	{"剧情片", "/cn/drama/"},
	{"英语字幕 Jav", "/cn/eng-sub-jav/"},
	{"面部护理", "/cn/facials/"},
	{"女医生", "/cn/female-doctor/"},
	{"格斗动作", "/cn/fighting-action/"},
	{"眼镜", "/cn/glasses/"},
	{"手交", "/cn/handjob/"},
	{"硬核", "/cn/hardcore/"},
	{"漢台", "/cn/hentai/"},
	{"巨臀", "/cn/huge-butt/"},
	{"羞辱", "/cn/humiliation/"},
	{"乱伦", "/cn/incest/"},
	{"Jable", "/cn/jable/"},
	{"日本性爱", "/cn/japan-sex/"},
	{"Jav审查", "/cn/jav-censored/"},
	{"Jav Guru", "/cn/jav-guru/"},
	{"Jav Porn", "/cn/jav-porn/"},
	{"Jav Sub", "/cn/jav-sub/"},
	{"Jav Tube", "/cn/jav-tube/"},
	{"JavBangers", "/cn/javbangers/"},
	{"Javct", "/cn/javct/"},
	{"Javfinder", "/cn/javfinder/"},
	{"Javgg", "/cn/javgg/"},
	{"Javhub", "/cn/javhub/"},
	{"Javlibrary", "/cn/javlibrary/"},
	{"Javmost", "/cn/javmost/"},
	{"Javtiful", "/cn/javtiful/"},
	{"Javtrailers", "/cn/javtrailers/"},
	{"和服", "/cn/kimono/"},
	{"亲吻", "/cn/kiss/"},
	{"恋腿癖", "/cn/leg-fetish/"},
	{"女同性恋", "/cn/lesbian/"},
	{"内衣", "/cn/lingerie/"},
	{"已婚妇女", "/cn/married-woman/"},
	{"按摩", "/cn/massage/"},
	{"成熟女性", "/cn/mature-woman/"},
	{"Missav", "/cn/missav/"},
	{"母亲", "/cn/mother/"},
	{"南帕", "/cn/nampa/"},
	{"下流", "/cn/nasty/"},
	{"Njav", "/cn/njav/"},
	{"全集", "/cn/omnibus/"},
	{"户外", "/cn/outdoors/"},
	{"喝尿", "/cn/piss-drinking/"},
	{"规划", "/cn/planning/"},
	{"Pornhub", "/cn/pornhub/"},
	{"POV", "/cn/pov/"},
	{"淫乱", "/cn/promiscuity/"},
	{"妓女", "/cn/prostitutes/"},
	{"强奸", "/cn/rape/"},
	{"减少马赛克", "/cn/reducing-mosaic/"},
	{"要求", "/cn/request/"},
	{"束缚", "/cn/restraints/"},
	{"冒险马赛克", "/cn/risky-mosaic/"},
	{"学校", "/cn/school/"},
	{"Sextb", "/cn/sextb/"},
	{"剃须", "/cn/shaved/"},
	{"姊妹", "/cn/sister/"},
	{"纤细", "/cn/slender/"},
	{"索洛沃克", "/cn/solowork/"},
	{"运动", "/cn/sport/"},
	{"喷水", "/cn/squirting/"},
	{"Supjav", "/cn/supjav/"},
	{"教师", "/cn/teacher/"},
	{"Thisav", "/cn/thisav/"},
	{"乳交", "/cn/titty-fuck/"},
	{"互联网上搜索次数最多的 Jav 电影", "/cn/top-most-searched-jav-movies-on-the-internet/"},
	{"玩具", "/cn/toy/"},
	{"未经审查的 Jav", "/cn/uncensored-jav/"},
	{"各种职业", "/cn/various-professions/"},
	{"处男", "/cn/virgin-man/"},
	{"Vjav", "/cn/vjav/"},
	{"Xhamster", "/cn/xhamster/"},
	{"Xnxx", "/cn/xnxx/"},
	{"Xvideos", "/cn/xvideos/"},
	{"年轻妻子", "/cn/young-wife/"},
}

var javhdLiveCatRe = regexp.MustCompile(`(?s)<li id="category-\d+">\s*<div class="category">\s*<a href="([^"]+)"[^>]*>\s*<img[^>]*>\s*<div class="category-title">([^<]+)</div>`)

// javhdAllCats ports py _live_categories(): fixed 15 + live categories
// (>=50) or the 99-entry snapshot.
func javhdAllCats() []struct{ id, name string } {
	javhdMu.Lock()
	defer javhdMu.Unlock()
	if javhdCatsCache != nil {
		return javhdCatsCache
	}
	cats := []struct{ name, id string }{}
	if page, err := javhdFetchLocked(javhdHostCur + "/cn/categories/"); err == nil {
		for _, m := range javhdLiveCatRe.FindAllStringSubmatch(page, -1) {
			href := m[1]
			if strings.HasPrefix(href, "http") {
				if u, perr := url.Parse(href); perr == nil {
					href = u.Path
				}
			}
			if !strings.HasSuffix(href, "/") {
				href += "/"
			}
			cats = append(cats, struct{ name, id string }{strings.TrimSpace(m[2]), href})
		}
	}
	if len(cats) < 50 {
		cats = nil
		for _, c := range javhdCatSnapshot {
			cats = append(cats, struct{ name, id string }{c.name, c.id})
		}
	}
	out := []struct{ id, name string }{}
	seen := map[string]bool{}
	for _, f := range javhdFixed {
		out = append(out, struct{ id, name string }{f.id, f.name})
		seen[f.id] = true
	}
	catIDs := map[string]bool{}
	for _, c := range cats {
		catIDs[c.id] = true
		if seen[c.id] {
			continue
		}
		seen[c.id] = true
		out = append(out, struct{ id, name string }{c.id, c.name})
	}
	javhdCatIDs = catIDs
	javhdCatsCache = out
	return out
}

// javhdFetchLocked is javhdFetch without host-state locking (caller
// holds javhdMu); used only by the category loader.
func javhdFetchLocked(rawurl string) (string, error) {
	body, code, _, err := httpDo("GET", rawurl, nil, map[string]string{
		"User-Agent": javhdUA,
		"Referer":    javhdHostCur + "/cn/",
		"Accept":     "text/html,application/json,*/*",
	})
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("javhd http %d", code)
	}
	return string(body), nil
}

func javhdIsCatID(id string) bool {
	javhdAllCats()
	javhdMu.Lock()
	defer javhdMu.Unlock()
	return javhdCatIDs[id]
}

func javhdCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range javhdAllCats() {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func javhdCatName(id string) string {
	for _, c := range javhdAllCats() {
		if c.id == id {
			return c.name
		}
	}
	return "JavHD"
}

var (
	javhdCardHrefRe   = regexp.MustCompile(`href="((?:https://javhd\.today)?/cn/\d+/[^"]*?)"`)
	javhdCardTitleRe  = regexp.MustCompile(`title="([^"]{3,})"`)
	javhdCardTitle2Re = regexp.MustCompile(`class="video-title">([^<]+)<`)
	javhdCardImgRe    = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	javhdCardCodeRe   = regexp.MustCompile(`(?s)class="video-overlay1 badge transparent">\s*([^<]+?)\s*<`)
	javhdCardDurRe    = regexp.MustCompile(`(?s)class="video-overlay badge transparent">\s*(?:HD\s*)?([0-9:]{4,8})`)
	javhdPaginationRe = regexp.MustCompile(`(?s)<ul class="pagination[^"]*">(.*?)</ul>`)
	javhdPagePathRe   = regexp.MustCompile(`/(\d+)/["']`)
	javhdPageQryRe    = regexp.MustCompile(`[?&]page=(\d+)`)
)

// javhdParseCards ports py _parse_cards().
func javhdParseCards(page string) []g2Work {
	out := []g2Work{}
	seen := map[string]bool{}
	for _, block := range strings.Split(page, `<li id="video-`)[1:] {
		m := javhdCardHrefRe.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		u, err := url.Parse(m[1])
		if err != nil {
			continue
		}
		path := u.Path
		if seen[path] {
			continue
		}
		seen[path] = true
		name := ""
		if tm := javhdCardTitleRe.FindStringSubmatch(block); tm != nil {
			name = strings.TrimSpace(tm[1])
		}
		if name == "" {
			if vm := javhdCardTitle2Re.FindStringSubmatch(block); vm != nil {
				name = strings.TrimSpace(vm[1])
			}
		}
		if name == "" {
			name = path
		}
		pic := ""
		if im := javhdCardImgRe.FindStringSubmatch(block); im != nil {
			pic = im[1]
			if strings.HasPrefix(pic, "//") {
				pic = "https:" + pic
			}
		}
		code := ""
		if cm := javhdCardCodeRe.FindStringSubmatch(block); cm != nil {
			code = strings.TrimSpace(cm[1])
		}
		dur := ""
		if dm := javhdCardDurRe.FindStringSubmatch(block); dm != nil {
			dur = dm[1]
		}
		remark := strings.TrimSpace(code + " " + dur)
		out = append(out, g2Work{RawID: path, Title: html.UnescapeString(name), Cover: pic, Remark: remark})
	}
	return out
}

func javhdPagecount(page string, cur int) int {
	seg := page
	if m := javhdPaginationRe.FindStringSubmatch(page); m != nil {
		seg = m[1]
	}
	pc := cur
	for _, re := range []*regexp.Regexp{javhdPagePathRe, javhdPageQryRe} {
		for _, m := range re.FindAllStringSubmatch(seg, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil && n > pc {
				pc = n
			}
		}
	}
	return pc
}

func javhdSortList(tid string, pg int) ([]g2Work, int, error) {
	u := javhdHost() + tid
	if pg > 1 {
		u = fmt.Sprintf("%s%s%d/", javhdHost(), tid, pg)
	}
	page, err := javhdFetch(u, "")
	if err != nil {
		return nil, pg, err
	}
	return javhdParseCards(page), javhdPagecount(page, pg), nil
}

type javhdAjax struct {
	HTML       string `json:"html"`
	Total      int    `json:"total"`
	Pagination string `json:"pagination"`
}

func javhdCatList(tid string, pg int) ([]g2Work, int, error) {
	base := tid
	if pg > 1 {
		base = fmt.Sprintf("%srecent/%d/", tid, pg)
	}
	raw, err := javhdFetch(javhdHost()+base+"?ajax=1", "")
	if err == nil {
		var data javhdAjax
		if jerr := json.Unmarshal([]byte(raw), &data); jerr == nil {
			cards := javhdParseCards(data.HTML)
			pc := pg
			if data.Total > 0 {
				pc = (data.Total + 27) / 28
			} else {
				pc = javhdPagecount(data.Pagination, pg)
			}
			if pc < pg {
				pc = pg
			}
			return cards, pc, nil
		}
	}
	page, ferr := javhdFetch(javhdHost()+base, "")
	if ferr != nil {
		return nil, pg, ferr
	}
	return javhdParseCards(page), javhdPagecount(page, pg), nil
}

func javhdQueryList(tid string, pg int) ([]g2Work, int, error) {
	u := javhdHost() + tid
	if pg > 1 {
		u += "?page=" + strconv.Itoa(pg)
	}
	page, err := javhdFetch(u, "")
	if err != nil {
		return nil, pg, err
	}
	return javhdParseCards(page), javhdPagecount(page, pg), nil
}

func javhdSearchList(key string, pg int) ([]g2Work, int, error) {
	q := url.QueryEscape(key)
	u := javhdHost() + "/cn/search/video/?s=" + q + "&ajax=1"
	if pg > 1 {
		u += "&page=" + strconv.Itoa(pg)
	}
	raw, err := javhdFetch(u, javhdHost()+"/cn/search/video/?s="+q)
	if err != nil {
		return nil, pg, err
	}
	var data javhdAjax
	if jerr := json.Unmarshal([]byte(raw), &data); jerr != nil {
		return []g2Work{}, pg, nil
	}
	cards := javhdParseCards(data.HTML)
	pc := pg
	if data.Total > 0 {
		pc = (data.Total + 27) / 28
	} else {
		pc = javhdPagecount(data.Pagination, pg)
	}
	if pc < pg {
		pc = pg
	}
	return cards, pc, nil
}

var (
	javhdPornstarRe = regexp.MustCompile(`(?s)<div class="model">\s*<a href="([^"]+)" title="([^"]*)" class="thumbnail">(.*?)</a>`)
	javhdChannelRe  = regexp.MustCompile(`(?s)<li id="video channellist">.*?<a href="([^"]+)">\s*<div class="imgch">\s*<img src="([^"]*)"[^>]*>\s*<span class="namec">([^<]+)</span>`)
	javhdPlaylistRe = regexp.MustCompile(`(?s)<li id="playlist-\d+">\s*<div class="playlist">\s*<a href="([^"]+)" title="([^"]*)" class="thumbnail">(.*?)</a>`)
	javhdCountRe    = regexp.MustCompile(`fa-video-camera"></i>\s*([\d,]+)`)
)

func javhdPornstarFolders(pg int) ([]g2Work, int, error) {
	u := javhdHost() + "/cn/pornstar/"
	if pg > 1 {
		u = fmt.Sprintf("%s/cn/pornstar/%d/", javhdHost(), pg)
	}
	page, err := javhdFetch(u, "")
	if err != nil {
		return nil, pg, err
	}
	out := []g2Work{}
	seen := map[string]bool{}
	for _, m := range javhdPornstarRe.FindAllStringSubmatch(page, -1) {
		u2, perr := url.Parse(m[1])
		if perr != nil {
			continue
		}
		path := u2.Path
		if seen[path] {
			continue
		}
		seen[path] = true
		name := strings.TrimSpace(m[2])
		if name == "" {
			name = path
		}
		remark := "女优"
		if cm := javhdCountRe.FindStringSubmatch(m[3]); cm != nil {
			remark = cm[1] + " 部"
		}
		// py leaves pornstar folder covers empty.
		out = append(out, g2Work{RawID: path, Title: name, Remark: remark})
	}
	return out, javhdPagecount(page, pg), nil
}

func javhdChannelFolders() ([]g2Work, error) {
	page, err := javhdFetch(javhdHost()+"/cn/channels/", "")
	if err != nil {
		return nil, err
	}
	out := []g2Work{}
	seen := map[string]bool{}
	for _, m := range javhdChannelRe.FindAllStringSubmatch(page, -1) {
		u2, perr := url.Parse(m[1])
		if perr != nil {
			continue
		}
		skey := u2.Query().Get("s")
		if skey == "" || seen[skey] {
			continue
		}
		seen[skey] = true
		name := strings.TrimSpace(m[3])
		if name == "" {
			name = skey
		}
		out = append(out, g2Work{
			RawID: "/cn/search/video/?s=" + url.QueryEscape(skey),
			Title: name, Cover: m[2], Remark: "工作室",
		})
	}
	return out, nil
}

func javhdPlaylistFolders(pg int) ([]g2Work, int, error) {
	u := javhdHost() + "/cn/playlists/"
	if pg > 1 {
		u = fmt.Sprintf("%s/cn/playlists/recent/%d/", javhdHost(), pg)
	}
	page, err := javhdFetch(u, "")
	if err != nil {
		return nil, pg, err
	}
	out := []g2Work{}
	seen := map[string]bool{}
	for _, m := range javhdPlaylistRe.FindAllStringSubmatch(page, -1) {
		u2, perr := url.Parse(m[1])
		if perr != nil {
			continue
		}
		path := u2.Path
		if seen[path] {
			continue
		}
		seen[path] = true
		name := strings.TrimSpace(m[2])
		if name == "" {
			name = path
		}
		pic := ""
		if im := javhdCardImgRe.FindStringSubmatch(m[3]); im != nil {
			pic = im[1]
		}
		remark := "播放列表"
		if cm := javhdCountRe.FindStringSubmatch(m[3]); cm != nil {
			remark = cm[1] + " 部"
		}
		out = append(out, g2Work{RawID: path, Title: html.UnescapeString(name), Cover: pic, Remark: remark})
	}
	return out, javhdPagecount(page, pg), nil
}

func javhdListResult(slot string, works []g2Work, pc, pg int, catName string) ([]interface{}, int, bool, error) {
	n := len(works)
	if n < 1 {
		n = 1
	}
	return g2WorksToItems(slot, works, catName), pc * n, pg < pc, nil
}

func javhdCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	if category == "" {
		category = "/cn/recent/"
	}
	// Folder rawID routed as a category: list its works directly.
	if strings.HasPrefix(category, "idx:") &&
		category != "idx:pornstar" && category != "idx:channels" && category != "idx:playlists" {
		return javhdCatalog(slot, strings.TrimPrefix(category, "idx:"), page)
	}
	switch {
	case category == "idx:pornstar":
		works, pc, err := javhdPornstarFolders(page)
		if err != nil {
			return nil, 0, false, err
		}
		items := []interface{}{}
		for _, w := range works {
			d := makeDrama(slot, "idx:"+w.RawID, w.Title, w.Cover, w.Remark, "女优索引", "")
			d["category"] = "idx:" + w.RawID
			items = append(items, d)
		}
		return items, pc * 160, page < pc, nil
	case category == "idx:channels":
		works, err := javhdChannelFolders()
		if err != nil {
			return nil, 0, false, err
		}
		items := []interface{}{}
		for _, w := range works {
			items = append(items, makeDrama(slot, "idx:"+w.RawID, w.Title, w.Cover, w.Remark, "工作室索引", ""))
		}
		return items, len(items), false, nil
	case category == "idx:playlists":
		works, pc, err := javhdPlaylistFolders(page)
		if err != nil {
			return nil, 0, false, err
		}
		items := []interface{}{}
		for _, w := range works {
			items = append(items, makeDrama(slot, "idx:"+w.RawID, w.Title, w.Cover, w.Remark, "播放列表", ""))
		}
		return items, pc * 24, page < pc, nil
	case strings.HasPrefix(category, "/cn/search/video/"):
		kw := ""
		if u, err := url.Parse(category); err == nil {
			kw = u.Query().Get("s")
		}
		works, pc, err := javhdSearchList(kw, page)
		if err != nil {
			return nil, 0, false, err
		}
		return javhdListResult(slot, works, pc, page, "工作室")
	case strings.HasPrefix(category, "/cn/pornstar/") || strings.HasPrefix(category, "/cn/channel/"):
		works, pc, err := javhdQueryList(category, page)
		if err != nil {
			return nil, 0, false, err
		}
		return javhdListResult(slot, works, pc, page, javhdCatName(category))
	case strings.HasPrefix(category, "/cn/playlist/"):
		pageText, err := javhdFetch(javhdHost()+category, "")
		if err != nil {
			return nil, 0, false, err
		}
		return javhdListResult(slot, javhdParseCards(pageText), 1, page, "播放列表")
	case javhdIsCatID(category):
		works, pc, err := javhdCatList(category, page)
		if err != nil {
			return nil, 0, false, err
		}
		return javhdListResult(slot, works, pc, page, javhdCatName(category))
	default:
		works, pc, err := javhdSortList(category, page)
		if err != nil {
			return nil, 0, false, err
		}
		return javhdListResult(slot, works, pc, page, javhdCatName(category))
	}
}

type javhdServer struct{ name, player, url string }

var (
	javhdServerTagRe  = regexp.MustCompile(`(?s)<[^>]*class="server-option[^"]*"[^>]*>`)
	javhdDataNameRe   = regexp.MustCompile(`data-name="([^"]*)"`)
	javhdDataPlayerRe = regexp.MustCompile(`data-player="([^"]*)"`)
	javhdDataEmbedsRe = regexp.MustCompile(`data-embeds="([^"]+)"`)
	javhdDataEmbedRe  = regexp.MustCompile(`data-embed="([^"]+)"`)
)

// javhdEmbedServers ports py _embed_servers().
func javhdEmbedServers(numid string) []javhdServer {
	page, err := javhdFetch(javhdHost()+"/cn/embed/"+numid+"/", javhdHost()+"/cn/")
	if err != nil {
		return nil
	}
	servers := []javhdServer{}
	for _, tag := range javhdServerTagRe.FindAllString(page, -1) {
		name := ""
		if nm := javhdDataNameRe.FindStringSubmatch(tag); nm != nil {
			name = strings.TrimSpace(nm[1])
		}
		player := "0"
		if pm := javhdDataPlayerRe.FindStringSubmatch(tag); pm != nil {
			player = pm[1]
		}
		surl := ""
		if em := javhdDataEmbedsRe.FindStringSubmatch(tag); em != nil {
			if dec, derr := base64.StdEncoding.DecodeString(em[1]); derr == nil {
				var arr []string
				if jerr := json.Unmarshal(dec, &arr); jerr == nil && len(arr) > 0 {
					surl = arr[0]
				}
			}
		}
		if surl == "" {
			if sm := javhdDataEmbedRe.FindStringSubmatch(tag); sm != nil {
				if dec, derr := base64.StdEncoding.DecodeString(sm[1]); derr == nil {
					surl = string(dec)
				}
			}
		}
		if name != "" && surl != "" {
			servers = append(servers, javhdServer{name, player, surl})
		}
	}
	return servers
}

var (
	javhdH1Re         = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	javhdOgImageRe    = regexp.MustCompile(`property="og:image" content="([^"]+)"`)
	javhdYearRe       = regexp.MustCompile(`发布日:\s*([0-9]{4})-[0-9]{2}-[0-9]{2}`)
	javhdCodeRe       = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9\-]{2,20})\s`)
	javhdTypeSegRe    = regexp.MustCompile(`(?s)类型:(.*?)发布日`)
	javhdTypeLinkRe   = regexp.MustCompile(`href="/cn/[a-z0-9\-]+/"[^>]*>(?:<[^>]+>)*\s*([^<]+)</a>`)
	javhdDirectorRe   = regexp.MustCompile(`导演:\s*<a[^>]*>([^<]+)</a>`)
	javhdStudioRe     = regexp.MustCompile(`Studio:\s*<a[^>]*>([^<]+)</a>`)
	javhdDescRe       = regexp.MustCompile(`(?s)<p class="description">(.*?)</p>`)
	javhdActorRe      = regexp.MustCompile(`type pornstar\s+(.+?)\s+and\s`)
	javhdNumIDRe      = regexp.MustCompile(`/cn/(\d+)/`)
	javhdEmbedIDRe    = regexp.MustCompile(`/cn/embed/(\d+)/`)
	javhdFirstRe      = regexp.MustCompile(`(?s)var FIRST = (\{.*?\});`)
	javhdSrcRe        = regexp.MustCompile(`"src"\s*:\s*"([^"]*)"`)
	javhdPlaylistJSRe = regexp.MustCompile(`"playlist"\s*:\s*"([^"]*)"`)
	javhdTypeJSRe     = regexp.MustCompile(`"type"\s*:\s*"([^"]*)"`)
	javhdM3u8Re       = regexp.MustCompile(`https?://[^\s"'<>\\]+\.m3u8[^\s"'<>\\]*`)
	javhdMp4Re        = regexp.MustCompile(`https?://[^\s"'<>\\]+\.mp4[^\s"'<>\\]*`)
	javhdPParamRe     = regexp.MustCompile(`[?&]p=([^&]+)`)
)

func javhdDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		rest := strings.TrimPrefix(rawID, "idx:")
		var works []g2Work
		var err error
		title := rest
		switch {
		case strings.HasPrefix(rest, "/cn/search/video/"):
			kw := ""
			if u, perr := url.Parse(rest); perr == nil {
				kw = u.Query().Get("s")
			}
			title = kw
			works, _, err = javhdSearchList(kw, 1)
		case strings.HasPrefix(rest, "/cn/playlist/"):
			var pageText string
			pageText, err = javhdFetch(javhdHost()+rest, "")
			if err == nil {
				works = javhdParseCards(pageText)
			}
			title = "播放列表"
		case strings.HasPrefix(rest, "/cn/pornstar/"):
			// v7 (hotfix, user device report on v6): embed the
			// actress's works as titled episodes (pages 1..3). The
			// v6 zero-episode folder detail was a dead end in the
			// app: card taps dispatch detail -> episodes only, and
			// zero episodes aborts with "暂时没有可播放的集数".
			seen := map[string]bool{}
			for pg := 1; pg <= 3; pg++ {
				pw, _, perr := javhdQueryList(rest, pg)
				if perr != nil {
					if pg == 1 {
						return nil, perr
					}
					break
				}
				for _, w := range pw {
					if !seen[w.RawID] {
						seen[w.RawID] = true
						works = append(works, w)
					}
				}
				if len(pw) < 12 {
					break
				}
			}
			if len(works) == 0 {
				return nil, fmt.Errorf("javhd: folder empty: %s", rest)
			}
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			name := parts[len(parts)-1]
			if un, uerr := url.PathUnescape(name); uerr == nil {
				name = un
			}
			cover := works[0].Cover
			if st := getStore(slot).findDrama(rawID); st != nil {
				if c, _ := st["cover"].(string); c != "" {
					cover = c
				}
				if t, _ := st["title"].(string); t != "" {
					name = t
				}
			}
			return g2WorksDetail(slot, rawID, name, cover, "女优作品", "女优索引", works,
				"选集即作品列表（集名=片名，点开即播）"), nil
		case strings.HasPrefix(rest, "/cn/channel/"):
			works, _, err = javhdQueryList(rest, 1)
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) > 0 {
				title = parts[len(parts)-1]
			}
		default:
			return nil, fmt.Errorf("javhd: unknown folder %s", rawID)
		}
		if err != nil {
			return nil, err
		}
		if len(works) == 0 {
			return nil, fmt.Errorf("javhd: folder empty: %s", rest)
		}
		return g2WorksDetail(slot, rawID, title, works[0].Cover, "", "索引", works, ""), nil
	}
	vid := rawID
	u := vid
	if strings.HasPrefix(u, "/") {
		u = javhdHost() + u
	} else if !strings.HasPrefix(u, "http") {
		u = javhdHost() + "/" + u
	}
	page, err := javhdFetch(u, "")
	if err != nil {
		return nil, err
	}
	name := vid
	if hm := javhdH1Re.FindStringSubmatch(page); hm != nil {
		name = strings.TrimSpace(g2TagRe.ReplaceAllString(hm[1], ""))
	}
	pic := ""
	if pm := javhdOgImageRe.FindStringSubmatch(page); pm != nil {
		pic = pm[1]
	}
	year := ""
	if ym := javhdYearRe.FindStringSubmatch(page); ym != nil {
		year = ym[1]
	}
	code := ""
	if cm := javhdCodeRe.FindStringSubmatch(name); cm != nil {
		code = strings.ToUpper(cm[1])
	}
	types := []string{}
	if tseg := javhdTypeSegRe.FindStringSubmatch(page); tseg != nil {
		for _, tm := range javhdTypeLinkRe.FindAllStringSubmatch(tseg[1], -1) {
			t := strings.TrimSpace(tm[1])
			dup := false
			for _, x := range types {
				if x == t {
					dup = true
				}
			}
			if t != "" && !dup {
				types = append(types, t)
			}
		}
	}
	director := ""
	if dm := javhdDirectorRe.FindStringSubmatch(page); dm != nil {
		director = strings.TrimSpace(dm[1])
	}
	studio := ""
	if sm := javhdStudioRe.FindStringSubmatch(page); sm != nil {
		studio = strings.TrimSpace(sm[1])
	}
	desc := ""
	if dpm := javhdDescRe.FindStringSubmatch(page); dpm != nil {
		desc = strings.TrimSpace(html.UnescapeString(g2TagRe.ReplaceAllString(dpm[1], "")))
	}
	actor := ""
	if am := javhdActorRe.FindStringSubmatch(desc); am != nil {
		actor = strings.TrimSpace(am[1])
	}
	desc = g2Trunc(desc, 600)
	meta := []string{}
	if len(types) > 0 {
		meta = append(meta, "类型："+strings.Join(types, " / "))
	}
	if studio != "" {
		meta = append(meta, "Studio："+studio)
	} else if director != "" {
		meta = append(meta, "导演："+director)
	}
	if actor != "" {
		meta = append(meta, "演员："+actor)
	}
	if year != "" {
		meta = append(meta, "年份："+year)
	}
	fullDesc := desc
	if len(meta) > 0 {
		fullDesc = strings.Join(meta, "\n")
		if desc != "" {
			fullDesc += "\n" + desc
		}
	}
	d := makeDrama(slot, rawID, html.UnescapeString(name), pic, code, "JavHD", fullDesc)
	if nm := javhdNumIDRe.FindStringSubmatch(vid); nm != nil {
		servers := javhdEmbedServers(nm[1])
		chs := []interface{}{}
		for _, s := range servers {
			chs = append(chs, g2Chapter(slot, rawID, s.url, s.name, len(chs)+1))
		}
		if len(chs) == 0 {
			// py fallback: the in-house embed page itself as the line.
			embedURL := javhdHost() + "/cn/embed/" + nm[1] + "/"
			chs = append(chs, g2Chapter(slot, rawID, embedURL, "站内", 1))
		}
		g2SetChapters(d, chs)
	}
	return d, nil
}

// javhdFirstSource ports py _first_source().
func javhdFirstSource(page, baseURL string) string {
	var cfg map[string]interface{}
	if m := javhdFirstRe.FindStringSubmatch(page); m != nil {
		_ = json.Unmarshal([]byte(m[1]), &cfg)
	}
	if cfg == nil {
		sm := javhdSrcRe.FindStringSubmatch(page)
		pm := javhdPlaylistJSRe.FindStringSubmatch(page)
		tm := javhdTypeJSRe.FindStringSubmatch(page)
		if sm != nil || pm != nil {
			cfg = map[string]interface{}{}
			if tm != nil {
				cfg["type"] = tm[1]
			}
			if sm != nil {
				cfg["src"] = sm[1]
			}
			if pm != nil {
				cfg["playlist"] = pm[1]
			}
		}
	}
	if cfg == nil {
		return ""
	}
	src, _ := cfg["src"].(string)
	if src == "" {
		src, _ = cfg["playlist"].(string)
	}
	if src == "" {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return src
	}
	ref, err := url.Parse(src)
	if err != nil {
		return src
	}
	return base.ResolveReference(ref).String()
}

// javhdResolveStreamPage ports py _resolve_stream_page().
func javhdResolveStreamPage(rawurl string) string {
	page, err := javhdFetch(rawurl, javhdHost()+"/cn/")
	if err != nil {
		return ""
	}
	if src := javhdFirstSource(page, rawurl); src != "" {
		return src
	}
	if pm := javhdPParamRe.FindStringSubmatch(rawurl); pm != nil {
		if u, perr := url.Parse(rawurl); perr == nil {
			base := u.Scheme + "://" + u.Host
			for k := 1; k <= 4; k++ {
				raw, ferr := javhdFetch(fmt.Sprintf("%s/embed.php?json=1&next=%d&p=%s", base, k, pm[1]), rawurl)
				if ferr != nil {
					continue
				}
				var data map[string]interface{}
				if json.Unmarshal([]byte(raw), &data) != nil {
					continue
				}
				if ok, _ := data["success"].(bool); ok {
					s2, _ := data["src"].(string)
					if s2 == "" {
						s2, _ = data["playlist"].(string)
					}
					if s2 != "" {
						if ref, rerr := url.Parse(s2); rerr == nil {
							return u.ResolveReference(ref).String()
						}
						return s2
					}
				}
			}
		}
	}
	return ""
}

func javhdResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return nil, fmt.Errorf("javhd: folder has no direct play url")
	}
	host := javhdHost()
	// Work path: go through the embed server table (player "1" first).
	if strings.HasPrefix(rawID, "/cn/") && !strings.Contains(rawID, "/embed/") {
		nm := javhdNumIDRe.FindStringSubmatch(rawID)
		if nm == nil {
			return nil, fmt.Errorf("javhd: bad work id %s", rawID)
		}
		servers := javhdEmbedServers(nm[1])
		target := ""
		for _, s := range servers {
			if s.player == "1" {
				target = s.url
				break
			}
		}
		if target == "" && len(servers) > 0 {
			target = servers[0].url
		}
		if target == "" {
			target = host + "/cn/embed/" + nm[1] + "/"
		}
		return javhdResolve(slot, target)
	}
	u := rawID
	if strings.HasPrefix(u, "/") {
		u = host + u
	}
	if strings.Contains(u, "javhdz.today/embed") || strings.Contains(u, "javhd.today/embed") ||
		strings.Contains(u, "/cn/embed/") {
		if strings.Contains(u, "/cn/embed/") {
			nm := javhdEmbedIDRe.FindStringSubmatch(u)
			servers := []javhdServer{}
			if nm != nil {
				servers = javhdEmbedServers(nm[1])
			}
			target := ""
			for _, s := range servers {
				if s.player == "1" {
					target = s.url
					break
				}
			}
			if target == "" && len(servers) > 0 {
				target = servers[0].url
			}
			if target == "" {
				return nil, fmt.Errorf("javhd: no embed server")
			}
			u = target
		}
		real := javhdResolveStreamPage(u)
		if real == "" {
			// py returns parse=1 here; this core cannot sniff.
			return nil, fmt.Errorf("javhd: stream page resolve failed")
		}
		format, proto := g2PlayFormat(real)
		return playResult(real, slot, rawID, javhdUA, u, format, proto), nil
	}
	if strings.Contains(u, ".m3u8") || strings.Contains(u, ".mp4") {
		format, proto := g2PlayFormat(u)
		return playResult(u, slot, rawID, javhdUA, host+"/cn/", format, proto), nil
	}
	// External embed page: scan for a direct stream.
	page, err := javhdFetch(u, host+"/cn/")
	if err != nil {
		return nil, err
	}
	found := javhdM3u8Re.FindAllString(page, -1)
	if len(found) == 0 {
		found = javhdMp4Re.FindAllString(page, -1)
	}
	if len(found) > 0 {
		real := strings.ReplaceAll(found[0], `\/`, "/")
		format, proto := g2PlayFormat(real)
		return playResult(real, slot, rawID, javhdUA, u, format, proto), nil
	}
	// py returns parse=1 here; this core cannot sniff.
	return nil, fmt.Errorf("javhd: embed has no direct stream")
}

func javhdSearch(slot, kw string) ([]interface{}, error) {
	works, _, err := javhdSearchList(kw, 1)
	if err != nil {
		return []interface{}{}, err
	}
	return g2WorksToItems(slot, works, "搜索"), nil
}

// ---- G2 registrations ----

func init() {
	// v4: hongguo no longer carries MissAV (user order 2026-10-23:
	// shield could not be passed — datacenter egress is IP-level
	// CF-blocked on every domain/profile and the jar's MissAV is a thin
	// client of a dead private relay; the slot now carries 123AV,
	// registered in sites_123av.go). The missav* functions stay above,
	// unregistered, for a possible future restore.
	// v4: huangdou no longer carries SupJav either (user order: swap to
	// the jar's whos.tv, registered in sites_whos.go). The supjav*
	// functions stay above, unregistered.
	registerSite("yaguo", &siteImpl{
		Key: "aipornx", Name: "AiPorn", DefaultCat: "home", HomeURL: "https://www.aipornx.cc/",
		Categories: aipCategories,
		Catalog:    aipCatalog,
		Detail:     aipDetail,
		Resolve:    aipResolve,
		Search:     aipSearch,
		Headers:    aipHeaders,
	}, "aipornx")
	// v3: JavHD moved dsd -> wuguo (user order); the dsd slot now
	// carries 懂色帝 DSD (registered in sites_dsd.go).
	// v5: JavHD swapped wuguo -> xingguo (user order 2026-10-23);
	// wuguo now carries 久久热 99jj (registered in sites_v2.go).
	registerSite("xingguo", &siteImpl{
		Key: "javhd", Name: "JavHD", DefaultCat: "/cn/recent/", HomeURL: "https://javhd.today/",
		Categories: javhdCategories,
		Catalog:    javhdCatalog,
		Detail:     javhdDetail,
		Resolve:    javhdResolve,
		Search:     javhdSearch,
		Headers:    func() map[string]string { return javhdHeaders("") },
	}, "javhd")
}
