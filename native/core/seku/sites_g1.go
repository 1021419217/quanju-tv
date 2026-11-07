package seku

// ---- G1 batch: py-source ports (javmenu / xvideos / 91sanji / javmit) ----
//
// Slot assignment (batch/槽位映射表.md):
//   javmenu  -> hanxiaoquan
//   xvideos  -> ikanbot
//   91sanji  -> huangju
//   javmit   -> huangguo-video
//
// Ported from the delivered py spiders (final authority where a recipe
// and the py source disagree):
//   ~/workspace/javmenu_work/javmenu.py
//   ~/workspace/xvideos_work/xvideos.py
//   ~/workspace/91sanji_work/91sanji.py
//   ~/workspace/javmit_work/javmit.py
//
// Folder adaptation (this core's established method): index categories
// (actresses / series / channels / tags / category & actor indexes)
// return folder cards whose rawID is "idx:" + site path; Detail on an
// "idx:" rawID fetches that folder's page and returns its works as the
// drama's chapters (episodeId = work rawID), so Resolve on a chapter
// lands on the normal work path.
//
// Discrepancies vs recipes (py wins):
//   - 91sanji covers: the recipe text says "Go 回 og 固定图即可，不必实现
//     AES 图解密", but the assigned task's CoverFetch contract exists
//     exactly for transformed cover bytes, and the py source itself
//     documents the AES-128-CBC key/IV published in the site's inline
//     script (verified live 2026-10-23: decrypting a pic.drleao.cn
//     cover yields a real 1280x720 PNG). So catalog/detail keep the
//     REAL per-video cover URL and sjCoverFetch decrypts it — real
//     covers, not the site's one fixed og image repeated everywhere.
//   - javmenu covers: py appends "@Referer=/@User-Agent=" loader
//     suffixes; the Go core returns bare URLs (recipe porting note).

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	// v4: hanxiaoquan was reassigned to Javmap (sites_javmap.go) by user
	// order 2026-10-23 17:48 — JavMenu's CDN stitches a 3 s foreign ad
	// segment into the media playlist ahead of the AES-encrypted feature
	// and the device player looped on the ad. The jm* provider below
	// stays compiled but unregistered (the ad diagnosis is archived in
	// the v4 batch notes).
	registerSite("ikanbot", &siteImpl{
		Key: "xvideos", Name: "XVideos", DefaultCat: "new", HomeURL: xvHost + "/",
		Categories: func(slot string) []interface{} { return g1Categories(xvCats, slot) },
		Catalog:    xvCatalog,
		Detail:     xvDetail,
		Resolve:    xvResolve,
		Search:     xvSearch,
		Headers:    xvHeaders,
	}, "xvideos")
	registerSite("huangju", &siteImpl{
		Key: "91sanji", Name: "91三级", DefaultCat: "category", HomeURL: sjHost + "/",
		Categories: func(slot string) []interface{} { return g1Categories(sjCats, slot) },
		Catalog:    sjCatalog,
		Detail:     sjDetail,
		Resolve:    sjResolve,
		Search:     sjSearch,
		Headers:    sjHeaders,
		CoverFetch: sjCoverFetch,
	}, "91sanji", "91三级")
	registerSite("v_javmit", &siteImpl{
		Key: "javmit", Name: "JAVMIT", DefaultCat: "home", HomeURL: jmtHost + "/",
		Categories: func(slot string) []interface{} { return g1Categories(jmtCats, slot) },
		Catalog:    jmtCatalog,
		Detail:     jmtDetail,
		Resolve:    jmtResolve,
		Search:     jmtSearch,
		Headers:    jmtHeaders,
	}, "javmit")
}

// ---- shared G1 helpers ----

type g1Cat struct{ id, name string }

func g1Categories(cats []g1Cat, slot string) []interface{} {
	out := []interface{}{}
	for _, c := range cats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func g1CatName(cats []g1Cat, id string) string {
	for _, c := range cats {
		if c.id == id {
			return c.name
		}
	}
	return id
}

// g1Text mirrors the py txt_of(): strip tags, unescape entities,
// collapse whitespace.
func g1Text(s string) string {
	var b strings.Builder
	inTag := false
	for _, ch := range s {
		switch {
		case ch == '<':
			inTag = true
		case ch == '>':
			inTag = false
		case !inTag:
			b.WriteRune(ch)
		}
	}
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}

func g1Attr(s string) string {
	return strings.TrimSpace(html.UnescapeString(s))
}

func g1Get(u string, headers map[string]string) (string, error) {
	body, code, _, err := httpGet(u, headers)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("http %d for %s", code, u)
	}
	return string(body), nil
}

func g1MaxInt(nums []string, def int) int {
	m := def
	for _, s := range nums {
		if n, err := strconv.Atoi(s); err == nil && n > m {
			m = n
		}
	}
	return m
}

// g1WorksDrama builds the folder-detail drama: the folder itself is the
// drama, its works are the chapters (episodeId = work rawID).
func g1WorksDrama(slot, rawID, title, cover, catName string, works []interface{}) map[string]interface{} {
	if title == "" {
		title = catName
	}
	d := makeDrama(slot, rawID, title, cover, fmt.Sprintf("%d 部作品", len(works)), catName, "")
	if len(works) > 0 {
		chapters := []interface{}{}
		for i, w := range works {
			wm, _ := w.(map[string]interface{})
			wraw, _ := wm["vod_id"].(string)
			wtitle, _ := wm["title"].(string)
			chapters = append(chapters, map[string]interface{}{
				"episodeId": wraw,
				"id":        dramaID(slot, wraw),
				"dramaId":   dramaID(slot, rawID),
				"sourceId":  slot,
				"source":    slot,
				"title":     wtitle,
				"name":      wtitle,
				"index":     i + 1,
				"episode":   i + 1,
				"url":       "",
				"videoUrl":  "",
				"mediaSize": 0,
			})
		}
		g1SetChapters(d, chapters)
	}
	return d
}

// g1FolderDrama builds a v6 folder info drama: no embedded episodes;
// the works live in the catalog drill category drillCat.
//
// v7 WARNING (device-proven dead end, do NOT use for tappable
// cards): the Dart app dispatches card taps as detail -> episodes
// only; zero episodes aborts the tap and Drama.category is never
// used as a navigation target. All v6 callers were converted to
// g1WorksDrama embeds in v7. Kept for reference only.
func g1FolderDrama(slot, rawID, title, cover, catName, drillCat string) map[string]interface{} {
	if title == "" {
		title = catName
	}
	d := makeDrama(slot, rawID, title, cover, "作品文件夹", catName, "作品请通过分类入口打开（文件夹卡片列表）")
	d["category"] = drillCat
	g2ZeroEpisodes(d)
	return d
}

// g1RouteChapters replaces a drama's single chapter with one chapter
// per play route (alternative lines/qualities of the SAME video);
// episodeId carries the "#<n>" suffix Resolve uses to pick the route.
func g1RouteChapters(d map[string]interface{}, slot, rawID string, names []string) {
	if len(names) <= 1 {
		return
	}
	chapters := []interface{}{}
	for i, name := range names {
		epID := fmt.Sprintf("%s#%d", rawID, i+1)
		chapters = append(chapters, map[string]interface{}{
			"episodeId": epID,
			"id":        dramaID(slot, epID),
			"dramaId":   dramaID(slot, rawID),
			"sourceId":  slot,
			"source":    slot,
			"title":     name,
			"name":      name,
			"index":     i + 1,
			"episode":   i + 1,
			"url":       "",
			"videoUrl":  "",
			"mediaSize": 0,
		})
	}
	g1SetChapters(d, chapters)
}

func g1SetChapters(d map[string]interface{}, chapters []interface{}) {
	d["chapters"] = chapters
	d["episodeList"] = chapters
	d["episodes"] = len(chapters)
	d["episodeCount"] = len(chapters)
	d["chapterCount"] = len(chapters)
	d["totalEpisode"] = len(chapters)
	d["totalEpisodes"] = len(chapters)
}

// g1StoredMeta recovers the title/cover a folder card was listed with
// (the store is merged on every catalog fetch by the core).
func g1StoredMeta(slot, rawID string) (string, string) {
	if st := getStore(slot).findDrama(rawID); st != nil {
		t, _ := st["title"].(string)
		c, _ := st["cover"].(string)
		return t, c
	}
	return "", ""
}

func g1PlayResult(u, slot, rawID, ua, referer string) map[string]interface{} {
	format, proto := "mp4", "file"
	if strings.Contains(u, ".m3u8") {
		format, proto = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, ua, referer, format, proto)
}

// =====================================================================
// JavMenu (javmenu.com) — port of javmenu.py
// =====================================================================

const jmHost = "https://javmenu.com"
const jmUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

func jmHeaders() map[string]string {
	return map[string]string{
		"User-Agent": jmUA,
		"Referer":    jmHost + "/",
		"Accept":     "text/html,application/xhtml+xml,*/*",
	}
}

// Navigation 1:1 (29 categories; /games is an external H-game ad slot
// with no on-site videos — excluded in the py source, kept excluded).
var jmCats = []g1Cat{
	{"censored/online", "在線看·有碼"},
	{"uncensored/online", "在線看·無碼"},
	{"western/online", "在線看·歐美"},
	{"fc2/online", "在線看·FC2"},
	{"hanime/online", "在線看·成人動畫"},
	{"chinese/online", "在線看·國產"},
	{"censored", "類型·有碼"},
	{"uncensored", "類型·無碼"},
	{"western", "類型·歐美"},
	{"fc2", "類型·FC2"},
	{"hanime", "類型·成人動畫"},
	{"chinese", "類型·國產"},
	{"censored/filter", "篩選器·有碼"},
	{"uncensored/filter", "篩選器·無碼"},
	{"western/filter", "篩選器·歐美"},
	{"fc2/filter", "篩選器·FC2"},
	{"rank/censored/day", "排行榜·日榜"},
	{"rank/censored/week", "排行榜·週榜"},
	{"rank/censored/month", "排行榜·月榜"},
	{"idx:rank/censored/actress", "排行榜·女優榜"},
	{"idx:censored/actor", "女優·有碼"},
	{"idx:uncensored/actor", "女優·無碼"},
	{"idx:western/actor", "女優·歐美"},
	{"idx:series/censored", "系列·有碼"},
	{"idx:series/uncensored", "系列·無碼"},
	{"idx:series/western", "系列·歐美"},
	{"code/LUXU", "LUXU"},
	{"code/ARA", "ARA"},
	{"code/SIRO", "SIRO"},
}

var jmReserved = map[string]bool{
	"censored": true, "uncensored": true, "western": true, "fc2": true,
	"hanime": true, "chinese": true, "actor": true, "series": true,
	"rank": true, "search": true, "games": true,
}

var (
	jmHrefRe      = regexp.MustCompile(`href="https://javmenu\.com/([A-Za-z0-9][A-Za-z0-9\-]{1,40})"`)
	jmTitleRe     = regexp.MustCompile(`class="card-text text-primary" title="([^"]+)"`)
	jmAltTitleRe  = regexp.MustCompile(`alt="([^"]{3,}?)\s*-\s*JAV`)
	jmDataSrcRe   = regexp.MustCompile(`data-src="([^"]+)"`)
	jmDateRe      = regexp.MustCompile(`class="text-muted">(\d{4}-\d{2}-\d{2})<`)
	jmPageRe      = regexp.MustCompile(`[?&]page=(\d+)`)
	jmActorHrefRe = regexp.MustCompile(`href="https://javmenu\.com/(actor/[A-Za-z0-9]+)"`)
	jmActorPicRe  = regexp.MustCompile(`data-src="([^"]+)"[^>]*alt="([^"]+)"`)
	jmActorAltRe  = regexp.MustCompile(`alt="([^"]+?)\s*-\s*JAV`)
	jmNameTailRe  = regexp.MustCompile(`\s*-\s*JAV.*$`)
	jmSeriesRe    = regexp.MustCompile(`(?s)href="https://javmenu\.com/((?:censored|uncensored|western)/series/[A-Za-z0-9]+)"[^>]*>\s*<div class="card-body series-card-text[^"]*">([^<]+)</div>`)
	jmOgTitleRe   = regexp.MustCompile(`property="og:title" content="([^"]+)"`)
	jmPosterRe    = regexp.MustCompile(`(?s)<video[^>]*poster="([^"]+)"`)
	jmOgImageRe   = regexp.MustCompile(`property="og:image" content="([^"]+)"`)
	jmOgDescRe    = regexp.MustCompile(`property="og:description" content="([^"]+)"`)
	jmLineRe      = regexp.MustCompile(`(?s)<a[^>]*data-m3u8="([^"]+)"[^>]*>(.*?)</a>`)
	jmVideoM3u8Re = regexp.MustCompile(`(?s)<video[^>]*data-m3u8="([^"]+)"`)
	jmReleaseRe   = regexp.MustCompile(`發佈日期為(\d{4}-\d{2}-\d{2})`)
)

func jmPageURL(path string, page int) string {
	u := jmHost + "/" + strings.TrimPrefix(path, "/")
	if page > 1 {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "page=" + strconv.Itoa(page)
	}
	return u
}

func jmPic(u string) string {
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}

func jmParseVideos(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, block := range strings.Split(htmlStr, "video-list-item ")[1:] {
		m := jmHrefRe.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		code := m[1]
		if seen[code] || jmReserved[code] {
			continue
		}
		tm := jmTitleRe.FindStringSubmatch(block)
		if tm == nil {
			continue
		}
		title := strings.TrimSpace(tm[1])
		if len([]rune(title)) < 3 {
			if am := jmAltTitleRe.FindStringSubmatch(block); am != nil {
				title = strings.TrimSpace(am[1])
			}
		}
		seen[code] = true
		pic := ""
		if im := jmDataSrcRe.FindStringSubmatch(block); im != nil {
			pic = im[1]
		}
		if strings.Contains(pic, "loading") || strings.Contains(pic, "website_building") {
			pic = ""
		}
		remark := code
		if dm := jmDateRe.FindStringSubmatch(block); dm != nil {
			remark = dm[1]
		}
		items = append(items, makeDrama(slot, code, g1Attr(title), jmPic(pic), remark, catName, ""))
	}
	return items
}

func jmVideoList(slot, path string, page int, catName string) ([]interface{}, int, error) {
	htmlStr, err := g1Get(jmPageURL(path, page), jmHeaders())
	if err != nil {
		return nil, 0, err
	}
	return jmParseVideos(slot, htmlStr, catName), jmPagecount(htmlStr, page), nil
}

func jmPagecount(htmlStr string, cur int) int {
	nums := []string{}
	for _, m := range jmPageRe.FindAllStringSubmatch(htmlStr, -1) {
		nums = append(nums, m[1])
	}
	return g1MaxInt(nums, cur)
}

func jmParseActors(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, block := range strings.Split(htmlStr, "video-list-item ")[1:] {
		m := jmActorHrefRe.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		aid := m[1]
		if seen[aid] {
			continue
		}
		seen[aid] = true
		name, pic := "", ""
		if im := jmActorPicRe.FindStringSubmatch(block); im != nil {
			pic = im[1]
			name = strings.TrimSpace(jmNameTailRe.ReplaceAllString(im[2], ""))
		}
		if name == "" {
			if nm := jmActorAltRe.FindStringSubmatch(block); nm != nil {
				name = strings.TrimSpace(nm[1])
			} else {
				name = aid
			}
		}
		if strings.Contains(pic, "loading") {
			pic = ""
		}
		d := makeDrama(slot, "idx:"+aid, g1Attr(name), jmPic(pic), "女優", catName, "")
		d["category"] = "idx:" + aid
		items = append(items, d)
	}
	return items
}

func jmParseSeries(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range jmSeriesRe.FindAllStringSubmatch(htmlStr, -1) {
		sid, name := m[1], strings.TrimSpace(m[2])
		if seen[sid] {
			continue
		}
		seen[sid] = true
		items = append(items, makeDrama(slot, "idx:"+sid, g1Attr(name), "", "系列", catName, ""))
	}
	return items
}

func jmCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "censored/online"
	}
	if page < 1 {
		page = 1
	}
	catName := g1CatName(jmCats, category)
	if strings.HasPrefix(category, "idx:") {
		path := category[4:]
		htmlStr, err := g1Get(jmPageURL(path, page), jmHeaders())
		if err != nil {
			return nil, 0, false, err
		}
		var items []interface{}
		if strings.Contains(path, "/actor") || strings.Contains(path, "actress") {
			items = jmParseActors(slot, htmlStr, catName)
		} else {
			items = jmParseSeries(slot, htmlStr, catName)
		}
		pc := jmPagecount(htmlStr, page)
		return items, pc * max(len(items), 1), page < pc, nil
	}
	items, pc, err := jmVideoList(slot, category, page, catName)
	if err != nil {
		return nil, 0, false, err
	}
	return items, pc * max(len(items), 1), page < pc, nil
}

// jmFetchDetail returns title, cover, desc, release date and the play
// lines (name, m3u8) of a video page.
func jmFetchDetail(rawID string) (string, string, string, string, [][2]string, error) {
	htmlStr, err := g1Get(jmHost+"/"+strings.TrimPrefix(rawID, "/"), jmHeaders())
	if err != nil {
		return "", "", "", "", nil, err
	}
	title := rawID
	if m := jmOgTitleRe.FindStringSubmatch(htmlStr); m != nil {
		title = strings.TrimSpace(m[1])
	}
	pic := ""
	if m := jmPosterRe.FindStringSubmatch(htmlStr); m != nil {
		pic = m[1]
	} else if m := jmOgImageRe.FindStringSubmatch(htmlStr); m != nil {
		pic = m[1]
	}
	desc := ""
	if m := jmOgDescRe.FindStringSubmatch(htmlStr); m != nil {
		desc = strings.TrimSpace(m[1])
	}
	rel := ""
	if m := jmReleaseRe.FindStringSubmatch(desc); m != nil {
		rel = m[1]
	}
	lines := [][2]string{}
	seen := map[string]bool{}
	for _, m := range jmLineRe.FindAllStringSubmatch(htmlStr, -1) {
		u := strings.ReplaceAll(m[1], "&amp;", "&")
		if seen[u] {
			continue
		}
		seen[u] = true
		name := g1Text(m[2])
		if name == "" {
			name = fmt.Sprintf("線路 %d", len(lines)+1)
		}
		lines = append(lines, [2]string{name, u})
	}
	if len(lines) == 0 {
		if m := jmVideoM3u8Re.FindStringSubmatch(htmlStr); m != nil {
			lines = append(lines, [2]string{"線路 1", strings.ReplaceAll(m[1], "&amp;", "&")})
		}
	}
	return g1Attr(title), jmPic(pic), g1Attr(desc), rel, lines, nil
}

func jmDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		path := rawID[4:]
		title, cover := g1StoredMeta(slot, rawID)
		if strings.Contains(path, "/actor") {
			// v7 (hotfix, user device report on v6): embed the
			// actress's works as titled episodes (pages 1..3). The
			// v6 zero-episode folder drama was untappable in the
			// app: card taps dispatch detail -> episodes only.
			var aworks []interface{}
			for pg := 1; pg <= 3; pg++ {
				pw, _, perr := jmVideoList(slot, path, pg, "索引")
				if perr != nil {
					if pg == 1 {
						return nil, perr
					}
					break
				}
				aworks = append(aworks, pw...)
				if len(pw) == 0 {
					break
				}
			}
			if len(aworks) == 0 {
				return nil, fmt.Errorf("javmap actor folder empty: %s", path)
			}
			return g1WorksDrama(slot, rawID, title, cover, "索引", aworks), nil
		}
		works, _, err := jmVideoList(slot, path, 1, "索引")
		if err != nil {
			return nil, err
		}
		return g1WorksDrama(slot, rawID, title, cover, "索引", works), nil
	}
	base, _ := splitEpisodeSuffix(rawID)
	title, cover, desc, rel, lines, err := jmFetchDetail(base)
	if err != nil {
		return nil, err
	}
	remark := rel
	if remark == "" {
		remark = base
	}
	d := makeDrama(slot, base, title, cover, remark, "JavMenu", desc)
	names := []string{}
	for _, l := range lines {
		names = append(names, l[0])
	}
	g1RouteChapters(d, slot, base, names)
	return d, nil
}

func jmResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return nil, fmt.Errorf("javmenu: folder has no play url")
	}
	base, idx := splitEpisodeSuffix(rawID)
	_, _, _, _, lines, err := jmFetchDetail(base)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("javmenu: no play url")
	}
	if idx < 1 || idx > len(lines) {
		idx = 1
	}
	return playResult(lines[idx-1][1], slot, rawID, jmUA, jmHost+"/", "m3u8", "hls"), nil
}

func jmSearch(slot, kw string) ([]interface{}, error) {
	path := "search?wd=" + url.QueryEscape(kw)
	items, _, err := jmVideoList(slot, path, 1, "搜索")
	return items, err
}

// =====================================================================
// XVideos (xvideos.com) — port of xvideos.py
// =====================================================================

const xvHost = "https://www.xvideos.com"
const xvUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

func xvHeaders() map[string]string {
	return map[string]string{
		"User-Agent": xvUA,
		"Referer":    xvHost + "/",
		"Accept":     "text/html,application/xhtml+xml,*/*",
	}
}

// Navigation 1:1 (46: newest/best + 38 /c/ categories + gay/trans +
// tags/channels/pornstars/profiles indexes).
var xvCats = []g1Cat{
	{"new", "最新"},
	{"best", "最佳"},
	{"/c/AI-239", "AI"},
	{"/c/Amateur-65", "Amateur"},
	{"/c/Anal-12", "Anal"},
	{"/c/Arab-159", "Arab"},
	{"/c/Asian_Woman-32", "Asian"},
	{"/c/ASMR-229", "ASMR"},
	{"/c/Ass-14", "Ass"},
	{"/c/bbw-51", "BBW"},
	{"/c/Bi_Sexual-62", "Bi"},
	{"/c/Big_Ass-24", "Big Ass"},
	{"/c/Big_Cock-34", "Big Cock"},
	{"/c/Big_Tits-23", "Big Tits"},
	{"/c/Black_Woman-30", "Black"},
	{"/c/Blonde-20", "Blonde"},
	{"/c/Blowjob-15", "Blowjob"},
	{"/c/Brunette-25", "Brunette"},
	{"/c/Cam_Porn-58", "Cam Porn"},
	{"/c/Creampie-40", "Creampie"},
	{"/c/Cuckold-237", "Cuckold/Hotwife"},
	{"/c/Cumshot-18", "Cumshot"},
	{"/c/Femdom-235", "Femdom"},
	{"/c/Fisting-165", "Fisting"},
	{"/c/Fucked_Up_Family-81", "Fucked Up Family"},
	{"/c/Gangbang-69", "Gangbang"},
	{"/c/Gapes-167", "Gapes"},
	{"/c/Indian-89", "Indian"},
	{"/c/Interracial-27", "Interracial"},
	{"/c/Latina-16", "Latina"},
	{"/c/Lesbian-26", "Lesbian"},
	{"/c/Lingerie-83", "Lingerie"},
	{"/c/Mature-38", "Mature"},
	{"/c/Milf-19", "Milf"},
	{"/c/Oiled-22", "Oiled"},
	{"/c/Redhead-31", "Redhead"},
	{"/c/Solo_and_Masturbation-33", "Solo"},
	{"/c/Squirting-56", "Squirting"},
	{"/c/Stockings-28", "Stockings"},
	{"/c/Teen-13", "Teen"},
	{"gay", "Gay"},
	{"trans", "Transsexual"},
	{"tags", "标签"},
	{"channels", "频道"},
	{"pornstars", "女优"},
	{"profiles", "播主"},
}

var (
	xvBlockTagRe = regexp.MustCompile(`<div[^>]+class="[^"]*thumb-block[^"]*"[^>]*>`)
	xvHrefRe     = regexp.MustCompile(`href="(/video\.[^"]+)"`)
	xvCoverRes   = []*regexp.Regexp{
		regexp.MustCompile(`data-src="(https://[^"]+?\.jpg)"`),
		regexp.MustCompile(`data-srcset="(https://[^"]+?\.jpg)"`),
		regexp.MustCompile(`data-mzl="(https://[^"]+?\.jpg)"`),
		regexp.MustCompile(`data-sfwthumb="(https://[^"]+?\.jpg)"`),
		regexp.MustCompile(`(https://(?:thumb|thumbs)[^"]+?\.jpg)`),
	}
	xvTitleRes = []*regexp.Regexp{
		regexp.MustCompile(`<p class="title"><a[^>]*title="([^"]+)"`),
		regexp.MustCompile(`<img[^>]+alt="([^"]+)"`),
		regexp.MustCompile(`<div class="title" title="([^"]+)"`),
		regexp.MustCompile(`(?s)class="title"[^>]*>\s*<a[^>]*>([^<]+)</a>`),
	}
	xvDurRe        = regexp.MustCompile(`<span class="duration">([^<]+)</span>`)
	xvHDRe         = regexp.MustCompile(`video-hd-mark">([^<]+)<`)
	xvFolderHrefRe = regexp.MustCompile(`href="(/(?:pornstars/|profiles/|channels/)?[A-Za-z0-9_-]+)"`)
	xvFolderPicRe  = regexp.MustCompile(`(https://[^"']+?\.jpg(?:\?[^"']*)?)`)
	xvProfName1Re  = regexp.MustCompile(`<span class="profile-name">\s*([^<]+)`)
	xvProfName2Re  = regexp.MustCompile(`(?s)<p class="profile-name">.*?<a[^>]*>([^<]+)</a>`)
	xvProfName3Re  = regexp.MustCompile(`(?s)<p class="profile-name">(.*?)</p>`)
	xvProfHashRe   = regexp.MustCompile(`#\d+`)
	xvVideosCntRe  = regexp.MustCompile(`([\d.,]+\s*[kM]?)\s*videos`)
	xvTagsScopeRe  = regexp.MustCompile(`(?s)<ul class="tags-list" id="tags">(.*?)</ul>`)
	xvTagRe        = regexp.MustCompile(`(?s)<a[^>]+href="(/tags/[^"/]+)"[^>]*>(.*?)</a>`)
	xvTagBoldRe    = regexp.MustCompile(`<b>([^<]+)</b>`)
	xvTagCountRe   = regexp.MustCompile(`navbadge[^>]*>([\d,]+)<`)
	xvOgTitleRe    = regexp.MustCompile(`property="og:title" content="([^"]+)"`)
	xvOgImageRe    = regexp.MustCompile(`property="og:image" content="([^"]+)"`)
	xvHLSRe        = regexp.MustCompile(`setVideoHLS\('([^']+)'\)`)
	xvHighRe       = regexp.MustCompile(`setVideoUrlHigh\('([^']+)'\)`)
	xvLowRe        = regexp.MustCompile(`setVideoUrlLow\('([^']+)'\)`)
	xvCfgURLRe     = regexp.MustCompile(`"url"\s*:\s*"((?:\\/|/)[^"]+)"`)
	xvBestMonthRe  = regexp.MustCompile(`(/best/\d{4}-\d{2})`)
)

func xvShortPath(href string) string {
	parts := strings.Split(href, "/")
	if len(parts) > 3 && strings.HasPrefix(parts[1], "video.") {
		return "/" + parts[1] + "/" + parts[len(parts)-1]
	}
	return href
}

// xvBlocks splits a list page into (opening tag, body<=7000 chars)
// thumb-block segments, mirroring the py _blocks().
func xvBlocks(htmlStr string) [][2]string {
	locs := xvBlockTagRe.FindAllStringIndex(htmlStr, -1)
	out := [][2]string{}
	for i, loc := range locs {
		bodyEnd := len(htmlStr)
		if i+1 < len(locs) && locs[i+1][0] < bodyEnd {
			bodyEnd = locs[i+1][0]
		}
		if bodyEnd > loc[1]+7000 {
			bodyEnd = loc[1] + 7000
		}
		out = append(out, [2]string{htmlStr[loc[0]:loc[1]], htmlStr[loc[1]:bodyEnd]})
	}
	return out
}

func xvParseVideos(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, blk := range xvBlocks(htmlStr) {
		tag, body := blk[0], blk[1]
		if strings.Contains(tag, "thumb-block-profile") {
			continue
		}
		m := xvHrefRe.FindStringSubmatch(body)
		if m == nil {
			continue
		}
		href := xvShortPath(m[1])
		if seen[href] {
			continue
		}
		seen[href] = true
		pic := ""
		for _, re := range xvCoverRes {
			if pm := re.FindStringSubmatch(body); pm != nil && !strings.Contains(pm[1], "blank") {
				pic = pm[1]
				break
			}
		}
		if pic == "" {
			// coverless blocks are promos mixed into the grid (py drops them)
			continue
		}
		title := ""
		for _, re := range xvTitleRes {
			if tm := re.FindStringSubmatch(body); tm != nil {
				title = g1Text(tm[1])
				break
			}
		}
		if title == "" {
			title = g1Text(m[0])
		}
		if len([]rune(title)) > 120 {
			title = string([]rune(title)[:120])
		}
		remarks := ""
		if dm := xvDurRe.FindStringSubmatch(body); dm != nil {
			remarks = strings.TrimSpace(dm[1])
		}
		if hm := xvHDRe.FindStringSubmatch(body); hm != nil {
			remarks = strings.TrimSpace(hm[1] + " " + remarks)
		}
		items = append(items, makeDrama(slot, href, title, pic, remarks, catName, ""))
	}
	return items
}

func xvParseFolders(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, blk := range xvBlocks(htmlStr) {
		tag, body := blk[0], blk[1]
		if !strings.Contains(tag, "thumb-block-profile") {
			continue
		}
		m := xvFolderHrefRe.FindStringSubmatch(body)
		if m == nil {
			continue
		}
		href := m[1]
		if seen[href] || href == "/" || href == "/pornstars" || href == "/profiles" || href == "/channels" {
			continue
		}
		seen[href] = true
		pic := ""
		if pm := xvFolderPicRe.FindStringSubmatch(body); pm != nil {
			pic = strings.ReplaceAll(pm[1], `\/`, "/")
		}
		name := ""
		if nm := xvProfName1Re.FindStringSubmatch(body); nm != nil {
			name = g1Text(nm[1])
		}
		if name == "" {
			if nm := xvProfName2Re.FindStringSubmatch(body); nm != nil {
				name = g1Text(nm[1])
			}
		}
		if name == "" {
			if nm := xvProfName3Re.FindStringSubmatch(body); nm != nil {
				name = strings.TrimSpace(xvProfHashRe.ReplaceAllString(g1Text(nm[1]), ""))
			}
		}
		if len([]rune(name)) > 60 {
			name = string([]rune(name)[:60])
		}
		remarks := ""
		if cm := xvVideosCntRe.FindStringSubmatch(body); cm != nil {
			remarks = strings.TrimSpace(cm[1]) + " videos"
		}
		items = append(items, makeDrama(slot, "idx:"+href, name, pic, remarks, catName, ""))
	}
	return items
}

func xvParseTags(slot, htmlStr string) []interface{} {
	scope := htmlStr
	if m := xvTagsScopeRe.FindStringSubmatch(htmlStr); m != nil {
		scope = m[1]
	}
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range xvTagRe.FindAllStringSubmatch(scope, -1) {
		href, inner := m[1], m[2]
		if seen[href] {
			continue
		}
		name := ""
		if bm := xvTagBoldRe.FindStringSubmatch(inner); bm != nil {
			name = g1Text(bm[1])
		} else {
			name = g1Text(inner)
		}
		if name == "" {
			continue
		}
		seen[href] = true
		if len([]rune(name)) > 40 {
			name = string([]rune(name)[:40])
		}
		remarks := ""
		if cm := xvTagCountRe.FindStringSubmatch(inner); cm != nil {
			remarks = cm[1] + " videos"
		}
		items = append(items, makeDrama(slot, "idx:"+href, name, "", remarks, "标签", ""))
	}
	return items
}

var xvBestCache string

func xvBestPath() string {
	if xvBestCache != "" {
		return xvBestCache
	}
	xvBestCache = "/best"
	// /best 301s to the current monthly path; read the Location hop.
	req, err := http.NewRequest("GET", xvHost+"/best", nil)
	if err == nil {
		for k, v := range xvHeaders() {
			req.Header.Set(k, v)
		}
		resp, err := noRedirectClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if m := xvBestMonthRe.FindStringSubmatch(resp.Header.Get("Location")); m != nil {
				xvBestCache = m[1]
				return xvBestCache
			}
		}
	}
	if htmlStr, err := g1Get(xvHost+"/best", xvHeaders()); err == nil {
		if m := xvBestMonthRe.FindStringSubmatch(htmlStr); m != nil {
			xvBestCache = m[1]
		}
	}
	return xvBestCache
}

func xvListURL(tid string, pg int) string {
	switch {
	case tid == "new":
		if pg <= 1 {
			return "/"
		}
		return fmt.Sprintf("/new/%d", pg-1)
	case tid == "best":
		base := xvBestPath()
		if pg <= 1 {
			return base
		}
		return fmt.Sprintf("%s/%d", base, pg-1)
	case tid == "gay":
		if pg <= 1 {
			return "/gay?fmc=1"
		}
		return fmt.Sprintf("/gay/%d", pg-1)
	case tid == "trans":
		if pg <= 1 {
			return "/trans?fmc=1"
		}
		return fmt.Sprintf("/trans/%d", pg-1)
	case tid == "channels":
		if pg <= 1 {
			return "/channels-index"
		}
		return fmt.Sprintf("/channels-index/%d", pg-1)
	case tid == "pornstars":
		if pg <= 1 {
			return "/pornstars-index"
		}
		return fmt.Sprintf("/pornstars-index/%d", pg-1)
	case tid == "profiles":
		if pg <= 1 {
			return "/profileslist"
		}
		return fmt.Sprintf("/profileslist/%d", pg-1)
	default:
		if pg <= 1 {
			return tid
		}
		return fmt.Sprintf("%s/%d", tid, pg-1)
	}
}

func xvIsProfile(tid string) bool {
	if strings.HasPrefix(tid, "/pornstars/") || strings.HasPrefix(tid, "/profiles/") || strings.HasPrefix(tid, "/channels/") {
		return true
	}
	return strings.HasPrefix(tid, "/") && strings.Count(tid, "/") == 1 && len(tid) > 1
}

var xvBaseCache = map[string]string{}

func xvProfileBase(path string) string {
	if b, ok := xvBaseCache[path]; ok {
		return b
	}
	base := ""
	slug := strings.TrimSuffix(path, "/")
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		slug = slug[i+1:]
	}
	if htmlStr, err := g1Get(xvHost+path, xvHeaders()); err == nil {
		for _, m := range xvCfgURLRe.FindAllStringSubmatch(htmlStr, -1) {
			cand := strings.ReplaceAll(m[1], `\/`, "/")
			c := strings.TrimSuffix(cand, "/")
			last := c
			if i := strings.LastIndex(c, "/"); i >= 0 {
				last = c[i+1:]
			}
			if last == slug && !strings.Contains(cand, "/video") {
				base = cand
				break
			}
		}
	}
	if base == "" {
		if strings.HasPrefix(path, "/pornstars/") || strings.HasPrefix(path, "/profiles/") || strings.HasPrefix(path, "/channels/") {
			base = path
		} else {
			base = "/channels" + path
		}
	}
	xvBaseCache[path] = base
	return base
}

func xvPostJSON(path string) (map[string]interface{}, error) {
	u := path
	if !strings.HasPrefix(u, "http") {
		u = xvHost + u
	}
	body, code, _, err := httpDo("POST", u, strings.NewReader(""), xvHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("xvideos profile http %d", code)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func xvFlexStr(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%d", int64(t))
	default:
		return ""
	}
}

func xvProfileVideos(slot, path string, pg int) ([]interface{}, int, error) {
	base := xvProfileBase(path)
	data, err := xvPostJSON(fmt.Sprintf("%s/videos/best/%d", base, pg-1))
	if (err != nil || data == nil) && base != path {
		data, err = xvPostJSON(fmt.Sprintf("%s/videos/best/%d", path, pg-1))
	}
	if err != nil || data == nil {
		return nil, 0, fmt.Errorf("xvideos profile videos failed")
	}
	rawVids, ok := data["videos"]
	if !ok {
		return nil, 0, fmt.Errorf("xvideos profile: no videos key")
	}
	var vids []interface{}
	switch vv := rawVids.(type) {
	case []interface{}:
		vids = vv
	case map[string]interface{}:
		keys := []int{}
		byIdx := map[int]interface{}{}
		for k, val := range vv {
			if n, err := strconv.Atoi(k); err == nil {
				keys = append(keys, n)
				byIdx[n] = val
			}
		}
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[j] < keys[i] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		for _, n := range keys {
			vids = append(vids, byIdx[n])
		}
	}
	items := []interface{}{}
	for _, vv := range vids {
		vm, ok := vv.(map[string]interface{})
		if !ok {
			continue
		}
		u, _ := vm["u"].(string)
		slug := strings.TrimSuffix(u, "/")
		if i := strings.LastIndex(slug, "/"); i >= 0 {
			slug = slug[i+1:]
		}
		eid := xvFlexStr(vm["eid"])
		if eid == "" || slug == "" {
			continue
		}
		title, _ := vm["tf"].(string)
		if title == "" {
			title, _ = vm["t"].(string)
		}
		if len([]rune(title)) > 120 {
			title = string([]rune(title)[:120])
		}
		pic, _ := vm["il"].(string)
		if pic == "" {
			pic, _ = vm["i"].(string)
		}
		remarks, _ := vm["d"].(string)
		items = append(items, makeDrama(slot, "/video."+eid+"/"+slug, g1Text(title), pic, remarks, "资料页", ""))
	}
	pagecount := pg
	if total, ok := data["nb_videos"].(float64); ok && total > 0 {
		per, ok2 := data["nb_per_page"].(float64)
		if !ok2 || per <= 0 {
			per = 36
		}
		pagecount = int((int64(total) + int64(per) - 1) / int64(per))
		if pagecount < 1 {
			pagecount = 1
		}
	}
	return items, pagecount, nil
}

func xvCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "new"
	}
	if page < 1 {
		page = 1
	}
	catName := g1CatName(xvCats, category)
	if category == "tags" {
		htmlStr, err := g1Get(xvHost+"/tags", xvHeaders())
		if err != nil {
			return nil, 0, false, err
		}
		items := xvParseTags(slot, htmlStr)
		return items, len(items), false, nil
	}
	if xvIsProfile(category) {
		items, pc, err := xvProfileVideos(slot, category, page)
		if err != nil {
			return nil, 0, false, err
		}
		return items, pc * max(len(items), 1), page < pc, nil
	}
	htmlStr, err := g1Get(xvHost+xvListURL(category, page), xvHeaders())
	if err != nil {
		return nil, 0, false, err
	}
	var items []interface{}
	if category == "channels" || category == "pornstars" || category == "profiles" {
		items = xvParseFolders(slot, htmlStr, catName)
	} else {
		items = xvParseVideos(slot, htmlStr, catName)
	}
	return items, 999999, len(items) > 0, nil
}

// xvFetchDetail returns title, cover and play lines (name, url) in the
// py order: HLS first, then MP4 High, else MP4 Low only when neither.
func xvFetchDetail(path string) (string, string, [][2]string, error) {
	htmlStr, err := g1Get(xvHost+path, xvHeaders())
	if err != nil {
		return "", "", nil, err
	}
	title := ""
	if m := xvOgTitleRe.FindStringSubmatch(htmlStr); m != nil {
		title = g1Text(m[1])
	}
	pic := ""
	if m := xvOgImageRe.FindStringSubmatch(htmlStr); m != nil {
		pic = m[1]
	}
	lines := [][2]string{}
	if m := xvHLSRe.FindStringSubmatch(htmlStr); m != nil {
		lines = append(lines, [2]string{"HLS", strings.ReplaceAll(m[1], `\/`, "/")})
	}
	if m := xvHighRe.FindStringSubmatch(htmlStr); m != nil {
		lines = append(lines, [2]string{"MP4", strings.ReplaceAll(m[1], `\/`, "/")})
	} else if len(lines) == 0 {
		if m := xvLowRe.FindStringSubmatch(htmlStr); m != nil {
			lines = append(lines, [2]string{"MP4", strings.ReplaceAll(m[1], `\/`, "/")})
		}
	}
	return title, pic, lines, nil
}

func xvDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		path := rawID[4:]
		var works []interface{}
		var err error
		if strings.HasPrefix(path, "/tags/") {
			var htmlStr string
			htmlStr, err = g1Get(xvHost+path, xvHeaders())
			if err == nil {
				works = xvParseVideos(slot, htmlStr, "标签")
			}
		} else {
			works, _, err = xvProfileVideos(slot, path, 1)
		}
		if err != nil {
			return nil, err
		}
		title, cover := g1StoredMeta(slot, rawID)
		return g1WorksDrama(slot, rawID, title, cover, "索引", works), nil
	}
	base, _ := splitEpisodeSuffix(rawID)
	title, pic, lines, err := xvFetchDetail(base)
	if err != nil {
		return nil, err
	}
	d := makeDrama(slot, base, title, pic, "", "XVideos", "")
	names := []string{}
	for _, l := range lines {
		names = append(names, l[0])
	}
	g1RouteChapters(d, slot, base, names)
	return d, nil
}

func xvResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return nil, fmt.Errorf("xvideos: folder has no play url")
	}
	if strings.HasPrefix(rawID, "http") {
		return g1PlayResult(rawID, slot, rawID, xvUA, xvHost+"/"), nil
	}
	base, idx := splitEpisodeSuffix(rawID)
	_, _, lines, err := xvFetchDetail(base)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("xvideos: no play url")
	}
	if idx < 1 || idx > len(lines) {
		idx = 1
	}
	return g1PlayResult(lines[idx-1][1], slot, rawID, xvUA, xvHost+"/"), nil
}

func xvSearch(slot, kw string) ([]interface{}, error) {
	u := fmt.Sprintf("%s/?k=%s&p=0", xvHost, url.QueryEscape(kw))
	htmlStr, err := g1Get(u, xvHeaders())
	if err != nil {
		return []interface{}{}, err
	}
	return xvParseVideos(slot, htmlStr, "搜索"), nil
}

// =====================================================================
// 91三级 (91sanji.com) — port of 91sanji.py
// =====================================================================

const sjHost = "https://91sanji.com"
const sjUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

func sjHeaders() map[string]string {
	return map[string]string{
		"User-Agent": sjUA,
		"Referer":    sjHost + "/",
		"Accept":     "text/html,application/xhtml+xml,*/*",
	}
}

// Navigation 1:1 (23). Non-video sections （帖子/作品介绍/明星介绍/
// 黑料吃瓜/帖子热门榜 are text posts, /latest/ is an address page,
// ThePornDude is external) are excluded in the py source as well.
var sjCats = []g1Cat{
	{"category", "三级片电影"},
	{"category/chinese-sex-movies", "国产三级片"},
	{"category/hong-kong-sex-movies", "香港三级片"},
	{"category/japanese-sex-movies", "日本三级片"},
	{"category/korean-sex-movies", "韩国三级片"},
	{"category/western-sex-movies", "欧美三级片"},
	{"category/southeast-asian-sex-movies", "东南亚三级片"},
	{"ai", "AI成人电影"},
	{"ai/drama", "AI成人短剧"},
	{"ai/comic", "AI漫剧"},
	{"idx:star", "明星"},
	{"idx:star/chinese-stars", "国产明星"},
	{"idx:star/hong-kong-stars", "香港明星"},
	{"idx:star/japanese-stars", "日本明星"},
	{"idx:star/korean-stars", "韩国明星"},
	{"idx:star/western-stars", "欧美明星"},
	{"idx:star/southeast-asian-stars", "东南亚明星"},
	{"idx:star/ai", "AI明星"},
	{"idx:ranking", "热门榜单"},
	{"ranking/videos", "视频热播榜"},
	{"ranking/ai", "AI热播榜"},
	{"idx:ranking/tags", "标签热看榜"},
	{"idx:ranking/stars", "明星热播榜"},
}

var sjStarCatSlugs = map[string]bool{
	"star/chinese-stars": true, "star/hong-kong-stars": true,
	"star/japanese-stars": true, "star/korean-stars": true,
	"star/western-stars": true, "star/southeast-asian-stars": true,
	"star/ai": true,
}

var (
	sjVideoHrefRe   = regexp.MustCompile(`href="/video/(\d+)/"`)
	sjVideoTitleRe  = regexp.MustCompile(`class="video-card__title"[^>]*>([^<]+)</a>`)
	sjEncSrcRe      = regexp.MustCompile(`data-enc-src="([^"]+)"`)
	sjImgSrcRe      = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	sjDurationRe    = regexp.MustCompile(`class="duration">([^<]+)<`)
	sjDramaHrefRe   = regexp.MustCompile(`href="/video/(\d+)/"`)
	sjDramaAltRe    = regexp.MustCompile(`<img[^>]+alt="([^"]+)"`)
	sjDramaStateRe  = regexp.MustCompile(`(完结|连载中?|更新至)[^A-Za-z0-9]{0,4}(\d+\s*集)?`)
	sjRankTitleRe   = regexp.MustCompile(`<a class="rank-title" href="/video/(\d+)/">([^<]+)</a>`)
	sjStarHrefRe    = regexp.MustCompile(`href="(/star/[A-Za-z0-9\-]+/)"`)
	sjStarNameRe    = regexp.MustCompile(`<h2[^>]*>([^<]+)</h2>`)
	sjStarCountRe   = regexp.MustCompile(`<span>([^<]*影片[^<]*)</span>`)
	sjRankStarRe    = regexp.MustCompile(`<a class="rank-title" href="/star/([A-Za-z0-9\-]+)/">([^<]+)</a>`)
	sjRankStarCntRe = regexp.MustCompile(`(\d+\s*部影片)`)
	sjTagHrefRe     = regexp.MustCompile(`href="/tag/([A-Za-z0-9\-]+|[^"/]+)/"[^>]*>([^<]{1,30})</a>`)
	sjH1Re          = regexp.MustCompile(`<h1[^>]*>([^<]+)</h1>`)
	sjOgImageRe     = regexp.MustCompile(`property="og:image" content="([^"]+)"`)
	sjInfoPairRe    = regexp.MustCompile(`(?s)<dt>([^<]+)</dt><dd>(.*?)</dd>`)
	sjDescRe        = regexp.MustCompile(`"description":"((?:[^"\\]|\\.)*)"`)
	sjEpGridRe      = regexp.MustCompile(`(?s)class="ep-grid">(.*?)</div>`)
	sjEpLinkRe      = regexp.MustCompile(`href="(/video/\d+/episode/\d+/)"[^>]*>([^<]+)</a>`)
	sjVideoSrcRe    = regexp.MustCompile(`"videoSrc":"(https?://[^"]+?\.m3u8[^"]*)"`)
	sjContentURLRe  = regexp.MustCompile(`"contentUrl":"(https?://[^"]+?\.m3u8[^"]*)"`)
	sjPageNumRe     = regexp.MustCompile(`/page/(\d+)/`)
)

func sjBody(s string) string {
	if i := strings.Index(s, "</header>"); i > 0 {
		s = s[i:]
	}
	if i := strings.Index(s, "<footer"); i > 0 {
		s = s[:i]
	}
	return s
}

// sjPageURL follows the py per-section pagination rules: /page/N/ for
// category/ai/star/tag sections, /{N}/ for ai/drama & ai/comic, and
// single-page (no pagination parameter) for everything else.
func sjPageURL(path string, page int) string {
	base := sjHost + "/" + strings.Trim(path, "/") + "/"
	if page <= 1 {
		return base
	}
	if path == "category" || path == "ai" || path == "star" || strings.HasPrefix(path, "tag/") {
		return fmt.Sprintf("%spage/%d/", base, page)
	}
	if path == "ai/drama" || path == "ai/comic" {
		return fmt.Sprintf("%s%d/", base, page)
	}
	return base
}

func sjPagecount(htmlStr, path string, cur int) int {
	nums := []string{}
	if path == "category" || path == "ai" || path == "star" || strings.HasPrefix(path, "tag/") {
		for _, m := range sjPageNumRe.FindAllStringSubmatch(htmlStr, -1) {
			nums = append(nums, m[1])
		}
	} else if path == "ai/drama" || path == "ai/comic" {
		re := regexp.MustCompile(regexp.QuoteMeta("/"+path+"/") + `(\d+)/`)
		for _, m := range re.FindAllStringSubmatch(htmlStr, -1) {
			nums = append(nums, m[1])
		}
	}
	return g1MaxInt(nums, cur)
}

// sjPic normalizes a cover URL. pic.drleao.cn bytes are AES-128-CBC
// encrypted at rest; the URL is kept AS IS here and sjCoverFetch
// decrypts it on the core's cover path (see the file header note).
func sjPic(u string) string {
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "/") {
		return sjHost + u
	}
	return u
}

// sjStarPic: star avatars are encrypted too and have no decrypted
// counterpart in the folder listing — py leaves them empty.
func sjStarPic(u string) string {
	if u == "" || strings.Contains(u, "pic.drleao.cn") {
		return ""
	}
	return sjPic(u)
}

func sjParseVideoCards(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, block := range strings.Split(htmlStr, `<article class="video-card">`)[1:] {
		m := sjVideoHrefRe.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		vid := "video/" + m[1]
		if seen[vid] {
			continue
		}
		tm := sjVideoTitleRe.FindStringSubmatch(block)
		if tm == nil {
			continue
		}
		seen[vid] = true
		pic := ""
		if im := sjEncSrcRe.FindStringSubmatch(block); im != nil {
			pic = im[1]
		} else if im := sjImgSrcRe.FindStringSubmatch(block); im != nil {
			pic = im[1]
		}
		remarks := ""
		if dm := sjDurationRe.FindStringSubmatch(block); dm != nil {
			remarks = strings.TrimSpace(dm[1])
		}
		items = append(items, makeDrama(slot, vid, g1Attr(tm[1]), sjPic(pic), remarks, catName, ""))
	}
	return items
}

func sjParseDramaCards(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, block := range strings.Split(htmlStr, `<a class="drama-card"`)[1:] {
		m := sjDramaHrefRe.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		vid := "video/" + m[1]
		if seen[vid] {
			continue
		}
		am := sjDramaAltRe.FindStringSubmatch(block)
		if am == nil {
			continue
		}
		seen[vid] = true
		pic := ""
		if im := sjEncSrcRe.FindStringSubmatch(block); im != nil {
			pic = im[1]
		}
		text := block
		if len(text) > 900 {
			text = text[:900]
		}
		remarks := ""
		if rm := sjDramaStateRe.FindStringSubmatch(g1Text(text)); rm != nil {
			remarks = strings.TrimSpace(rm[0])
		}
		items = append(items, makeDrama(slot, vid, g1Attr(am[1]), sjPic(pic), remarks, catName, ""))
	}
	return items
}

func sjParseRankVideos(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range sjRankTitleRe.FindAllStringSubmatchIndex(htmlStr, -1) {
		vid := "video/" + htmlStr[m[2]:m[3]]
		if seen[vid] {
			continue
		}
		seen[vid] = true
		pic := ""
		start := m[0] - 1000
		if start < 0 {
			start = 0
		}
		if ims := sjEncSrcRe.FindAllStringSubmatch(htmlStr[start:m[0]], -1); len(ims) > 0 {
			pic = ims[len(ims)-1][1]
		}
		items = append(items, makeDrama(slot, vid, g1Attr(htmlStr[m[4]:m[5]]), sjPic(pic), "热播榜", catName, ""))
	}
	return items
}

func sjAnyCards(slot, htmlStr, catName string) []interface{} {
	items := sjParseVideoCards(slot, htmlStr, catName)
	seen := map[string]bool{}
	for _, it := range items {
		if m, ok := it.(map[string]interface{}); ok {
			if v, _ := m["vod_id"].(string); v != "" {
				seen[v] = true
			}
		}
	}
	for _, it := range sjParseDramaCards(slot, htmlStr, catName) {
		if m, ok := it.(map[string]interface{}); ok {
			if v, _ := m["vod_id"].(string); v != "" && !seen[v] {
				seen[v] = true
				items = append(items, it)
			}
		}
	}
	return items
}

func sjVideoList(slot, path string, page int, catName string) ([]interface{}, int, error) {
	raw, err := g1Get(sjPageURL(path, page), sjHeaders())
	if err != nil {
		return nil, 0, err
	}
	body := sjBody(raw)
	var items []interface{}
	switch {
	case strings.Contains(body, `class="video-card"`):
		items = sjParseVideoCards(slot, body, catName)
	case strings.Contains(body, `class="drama-card"`):
		items = sjParseDramaCards(slot, body, catName)
	default:
		items = sjParseRankVideos(slot, body, catName)
	}
	return items, sjPagecount(body, path, page), nil
}

func sjParseStarFolders(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, block := range strings.Split(htmlStr, `<a class="star-card"`)[1:] {
		m := sjStarHrefRe.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		sid := strings.Trim(m[1], "/")
		if seen[sid] || sjStarCatSlugs[sid] {
			continue
		}
		nm := sjStarNameRe.FindStringSubmatch(block)
		if nm == nil {
			continue
		}
		seen[sid] = true
		pic := ""
		if im := sjEncSrcRe.FindStringSubmatch(block); im != nil {
			pic = im[1]
		}
		remarks := "明星"
		if sm := sjStarCountRe.FindStringSubmatch(block); sm != nil {
			remarks = strings.TrimSpace(sm[1])
		}
		items = append(items, makeDrama(slot, "idx:"+sid, g1Attr(nm[1]), sjStarPic(pic), remarks, catName, ""))
	}
	if len(items) == 0 {
		// star hot-rank pages use rank-title + rank-avatar markup instead
		for _, idx := range sjRankStarRe.FindAllStringSubmatchIndex(htmlStr, -1) {
			sid := "star/" + htmlStr[idx[2]:idx[3]]
			if seen[sid] {
				continue
			}
			seen[sid] = true
			pic := ""
			start := idx[0] - 700
			if start < 0 {
				start = 0
			}
			if ims := sjEncSrcRe.FindAllStringSubmatch(htmlStr[start:idx[0]], -1); len(ims) > 0 {
				pic = ims[len(ims)-1][1]
			}
			remarks := "明星热播榜"
			tailEnd := idx[1] + 200
			if tailEnd > len(htmlStr) {
				tailEnd = len(htmlStr)
			}
			if sm := sjRankStarCntRe.FindStringSubmatch(htmlStr[idx[1]:tailEnd]); sm != nil {
				remarks = sm[1]
			}
			items = append(items, makeDrama(slot, "idx:"+sid, g1Attr(htmlStr[idx[4]:idx[5]]), sjStarPic(pic), remarks, catName, ""))
		}
	}
	return items
}

func sjStarFolders(slot, path string, page int, catName string) ([]interface{}, int, error) {
	raw, err := g1Get(sjPageURL(path, page), sjHeaders())
	if err != nil {
		return nil, 0, err
	}
	body := sjBody(raw)
	return sjParseStarFolders(slot, body, catName), sjPagecount(body, path, page), nil
}

func sjTagFolders(slot string) ([]interface{}, error) {
	raw, err := g1Get(sjHost+"/ranking/tags/", sjHeaders())
	if err != nil {
		return nil, err
	}
	body := sjBody(raw)
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range sjTagHrefRe.FindAllStringSubmatch(body, -1) {
		tid := "tag/" + m[1]
		name := strings.TrimSpace(m[2])
		if seen[tid] || name == "" {
			continue
		}
		seen[tid] = true
		items = append(items, makeDrama(slot, "idx:"+tid, g1Attr(name), "", "标签", "标签热看榜", ""))
	}
	return items, nil
}

func sjStarWorks(slot, path string) ([]interface{}, error) {
	raw, err := g1Get(sjHost+"/"+strings.Trim(path, "/")+"/", sjHeaders())
	if err != nil {
		return nil, err
	}
	body := sjBody(raw)
	scope := body
	if i := strings.Index(scope, "参演作品"); i > 0 {
		scope = scope[i:]
	}
	for _, cut := range []string{"相关推荐", "热门明星", "<footer"} {
		if j := strings.Index(scope, cut); j > 0 {
			scope = scope[:j]
		}
	}
	return sjParseVideoCards(slot, scope, "明星作品"), nil
}

func sjCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "category"
	}
	if page < 1 {
		page = 1
	}
	catName := g1CatName(sjCats, category)
	switch {
	case category == "idx:ranking":
		entries := []interface{}{}
		for _, e := range []g1Cat{
			{"ranking/videos", "视频热播榜"},
			{"ranking/ai", "AI热播榜"},
			{"idx:ranking/tags", "标签热看榜"},
			{"idx:ranking/stars", "明星热播榜"},
		} {
			rawID := e.id
			if !strings.HasPrefix(rawID, "idx:") {
				rawID = "idx:" + rawID
			}
			entries = append(entries, makeDrama(slot, rawID, e.name, "", "榜单", catName, ""))
		}
		return entries, len(entries), false, nil
	case category == "idx:ranking/tags":
		items, err := sjTagFolders(slot)
		if err != nil {
			return nil, 0, false, err
		}
		return items, len(items), false, nil
	case strings.HasPrefix(category, "idx:"):
		items, pc, err := sjStarFolders(slot, category[4:], page, catName)
		if err != nil {
			return nil, 0, false, err
		}
		return items, pc * max(len(items), 1), page < pc, nil
	case strings.HasPrefix(category, "star/"):
		items, err := sjStarWorks(slot, category)
		if err != nil {
			return nil, 0, false, err
		}
		return items, len(items), false, nil
	default:
		items, pc, err := sjVideoList(slot, category, page, catName)
		if err != nil {
			return nil, 0, false, err
		}
		return items, pc * max(len(items), 1), page < pc, nil
	}
}

// sjDetailIdx resolves a folder rawID ("idx:" + path) to its works.
func sjDetailIdx(slot, rawID string) (map[string]interface{}, error) {
	path := rawID[4:]
	title, cover := g1StoredMeta(slot, rawID)
	var works []interface{}
	var err error
	switch {
	case path == "ranking/tags":
		works, err = sjTagFolders(slot)
	case path == "ranking/stars":
		works, _, err = sjStarFolders(slot, path, 1, "明星热播榜")
	case strings.HasPrefix(path, "star/"):
		// v7 (hotfix, user device report on v6): embed the star's
		// works as titled episodes via the common return below.
		// The v6 zero-episode folder drama was untappable in the
		// app (card taps dispatch detail -> episodes only).
		works, err = sjStarWorks(slot, path)
	case strings.HasPrefix(path, "tag/"):
		works, _, err = sjVideoList(slot, path, 1, "标签")
	case strings.HasPrefix(path, "ranking/"):
		works, _, err = sjVideoList(slot, path, 1, "榜单")
	default:
		// star index pages (star, star/<region>) opened as a folder
		works, _, err = sjStarFolders(slot, path, 1, "明星")
	}
	if err != nil {
		return nil, err
	}
	return g1WorksDrama(slot, rawID, title, cover, "索引", works), nil
}

func sjJSONDesc(htmlStr string) string {
	m := sjDescRe.FindStringSubmatch(htmlStr)
	if m == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(`"`+m[1]+`"`), &s); err == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func sjDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return sjDetailIdx(slot, rawID)
	}
	vid := strings.Trim(rawID, "/")
	raw, err := g1Get(sjHost+"/"+vid+"/", sjHeaders())
	if err != nil {
		return nil, err
	}
	title := vid
	if m := sjH1Re.FindStringSubmatch(raw); m != nil {
		title = strings.TrimSpace(m[1])
	}
	pic := ""
	if m := sjOgImageRe.FindStringSubmatch(raw); m != nil {
		pic = m[1]
	}
	info := map[string]string{}
	for _, m := range sjInfoPairRe.FindAllStringSubmatch(raw, -1) {
		info[strings.TrimSpace(m[1])] = g1Text(m[2])
	}
	desc := sjJSONDesc(raw)
	remarks := info["类型"]
	if remarks == "" {
		remarks = info["片长"]
	}
	fullDesc := desc
	meta := []string{}
	for _, k := range []string{"片长", "导演", "主演", "类型", "地区", "年份"} {
		if v := info[k]; v != "" {
			meta = append(meta, k+"："+v)
		}
	}
	if len(meta) > 0 {
		if fullDesc != "" {
			fullDesc += "\n"
		}
		fullDesc += strings.Join(meta, " / ")
	}
	d := makeDrama(slot, vid, g1Attr(title), sjPic(pic), remarks, "91三级", fullDesc)
	// multi-episode dramas: each episode is its own page with its own
	// signed m3u8, resolved on demand by sjResolve.
	if em := sjEpGridRe.FindStringSubmatch(raw); em != nil {
		chapters := []interface{}{}
		for _, m := range sjEpLinkRe.FindAllStringSubmatch(em[1], -1) {
			epRaw := strings.Trim(m[1], "/")
			name := strings.TrimSpace(m[2])
			chapters = append(chapters, map[string]interface{}{
				"episodeId": epRaw,
				"id":        dramaID(slot, epRaw),
				"dramaId":   dramaID(slot, vid),
				"sourceId":  slot,
				"source":    slot,
				"title":     name,
				"name":      name,
				"index":     len(chapters) + 1,
				"episode":   len(chapters) + 1,
				"url":       "",
				"videoUrl":  "",
				"mediaSize": 0,
			})
		}
		if len(chapters) > 0 {
			g1SetChapters(d, chapters)
		}
	}
	return d, nil
}

func sjResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return nil, fmt.Errorf("91sanji: folder has no play url")
	}
	if strings.Contains(rawID, ".m3u8") && strings.HasPrefix(rawID, "http") {
		return playResult(rawID, slot, rawID, sjUA, sjHost+"/", "m3u8", "hls"), nil
	}
	page := sjHost + "/" + strings.Trim(rawID, "/") + "/"
	raw, err := g1Get(page, sjHeaders())
	if err != nil {
		return nil, err
	}
	m := sjVideoSrcRe.FindStringSubmatch(raw)
	if m == nil {
		m = sjContentURLRe.FindStringSubmatch(raw)
	}
	if m == nil {
		return nil, fmt.Errorf("91sanji: no videoSrc on detail page")
	}
	u := strings.ReplaceAll(strings.ReplaceAll(m[1], `\u0026`, "&"), `\/`, "/")
	return playResult(u, slot, rawID, sjUA, sjHost+"/", "m3u8", "hls"), nil
}

func sjSearch(slot, kw string) ([]interface{}, error) {
	raw, err := g1Get(sjHost+"/search/"+url.PathEscape(kw)+"/", sjHeaders())
	if err != nil {
		return []interface{}{}, err
	}
	body := sjBody(raw)
	scope := body
	start := strings.Index(body, "相关视频")
	end := strings.Index(body, "相关帖子")
	if start > 0 && end > start {
		scope = body[start:end]
	}
	return sjAnyCards(slot, scope, "搜索"), nil
}

// sjCoverFetch fetches a cover and, for pic.drleao.cn URLs, decrypts
// the AES-128-CBC bytes the site stores (key/IV are published in the
// site's own inline player script; verified live 2026-10-23). Other
// hosts are returned as fetched.
func sjCoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpGet(u, sjHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("91sanji cover http %d", code)
	}
	if !strings.Contains(u, "pic.drleao.cn") {
		return body, nil
	}
	return sjAESDecrypt(body)
}

func sjAESDecrypt(data []byte) ([]byte, error) {
	key := []byte("f5d965df75336270")
	iv := []byte("97b60394abc2fbe1")
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("91sanji cover: bad ciphertext size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	if len(out) > 0 {
		pad := int(out[len(out)-1])
		if pad > 0 && pad <= aes.BlockSize && pad <= len(out) {
			ok := true
			for _, b := range out[len(out)-pad:] {
				if int(b) != pad {
					ok = false
					break
				}
			}
			if ok {
				out = out[:len(out)-pad]
			}
		}
	}
	return out, nil
}

// =====================================================================
// JAVMIT (javmit.com) — port of javmit.py
// =====================================================================

const jmtHost = "https://javmit.com"
const jmtLang = "/zh-CN"
const jmtUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"

func jmtHeaders() map[string]string {
	return map[string]string{
		"User-Agent": jmtUA,
		"Referer":    jmtHost + "/",
		"Accept":     "text/html,application/xhtml+xml,*/*",
	}
}

// Navigation 1:1 (6; ThePornDude is an external ad slot, excluded in
// the py source as well). "video" carries site filters (sort/res/date)
// in the py spider; the siteImpl Catalog contract has no filter input,
// so the browse below uses the site's defaults (sort=date, date=all) —
// see the G1 report.
var jmtCats = []g1Cat{
	{"home", "主页"},
	{"/zh-CN/category/subtitles/", "字幕"},
	{"video", "最新"},
	{"uncensored_index", "未经审查"},
	{"categories_index", "分类"},
	{"actors_index", "演员"},
}

// Uncensored sub-source snapshot from the py source (used when the live
// /uncensored/ chips cannot be read).
var jmtUncSnapshot = [][3]string{
	{"/zh-CN/uncensored/uncensored-leak/", "未删减版泄露", "1525"},
	{"/zh-CN/uncensored/fc2/", "FC2", "1218"},
	{"/zh-CN/uncensored/heyzo/", "HEYZO", "245"},
	{"/zh-CN/uncensored/caribbeancom/", "Caribbeancom", "212"},
	{"/zh-CN/uncensored/10musume/", "10musume", "160"},
	{"/zh-CN/uncensored/1pondo/", "1pondo", "154"},
	{"/zh-CN/uncensored/pacopacomama/", "帕科帕科马马", "142"},
	{"/zh-CN/uncensored/tokyohot/", "Tokyo Hot", "109"},
	{"/zh-CN/uncensored/naughty0930/", "H0930", "105"},
	{"/zh-CN/uncensored/marriedslash/", "已婚斜杠", "96"},
	{"/zh-CN/uncensored/naughty4610/", "淘气4610", "90"},
	{"/zh-CN/uncensored/caribbeancompr/", "加勒比公司", "68"},
	{"/zh-CN/uncensored/gachinco/", "加钦科", "0"},
	{"/zh-CN/uncensored/xxx-av/", "XXX-AV", "0"},
}

var (
	jmtArticleRe    = regexp.MustCompile(`(?s)<article class="(?:v-card|list-video)"[^>]*>(.*?)</article>`)
	jmtThumbLinkRe  = regexp.MustCompile(`<a href="(https://javmit\.com/zh-CN/[^"]+/)"\s+class="(?:vc-thumb-wrap|lv-thumb)"`)
	jmtAnyLinkRe    = regexp.MustCompile(`<a href="(https://javmit\.com/zh-CN/[^"]+/)"`)
	jmtImgTagRe     = regexp.MustCompile(`<img[^>]+class="(?:vc-thumb-img|lv-thumb-img)"[^>]*>`)
	jmtSrcsetRe     = regexp.MustCompile(`srcset="([^"]+)"`)
	jmtSrcRe        = regexp.MustCompile(`src="([^"]+)"`)
	jmtSrcsetPartRe = regexp.MustCompile(`^(\S+)\s+(\d+)w$`)
	jmtCardTitleRe  = regexp.MustCompile(`(?s)<h3 class="(?:vc-title|lv-title)">(.*?)</h3>`)
	jmtImgAltRe     = regexp.MustCompile(`<img[^>]+alt="([^"]+)"`)
	jmtCardDurRe    = regexp.MustCompile(`class="(?:vc-dur|lv-dur)">([^<]+)<`)
	jmtTitleTailRe  = regexp.MustCompile(`\[(?:下载和[^\]]*|Download[^\]]*)\]\s*$`)
	jmtDigitsRe     = regexp.MustCompile(`\d+`)
	jmtPageHref1Re  = regexp.MustCompile(`class="[^"]*page-numbers[^"]*"[^>]*href="([^"]+)"`)
	jmtPageHref2Re  = regexp.MustCompile(`href="([^"]+)"[^>]*class="[^"]*page-numbers`)
	jmtPageNumInRe  = regexp.MustCompile(`/page/(\d+)/`)
	jmtActorRe      = regexp.MustCompile(`(?s)<a href="(https://javmit\.com/zh-CN/actor/[^"]+)"\s+class="costar-card actors-hub-card">(.*?)</a>`)
	jmtActorNameRe  = regexp.MustCompile(`actors-hub-name">([^<]+)<`)
	jmtActorCountRe = regexp.MustCompile(`actors-hub-count">([^<]+)<`)
	jmtActorImgRe   = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	jmtCatHeaderRe  = regexp.MustCompile(`<a href="(https://javmit\.com/zh-CN/(?:category|uncensored)/[^"]*)"\s+class="cc-header">`)
	jmtCatHeader2Re = regexp.MustCompile(`(?s)<a href="(https://javmit\.com/zh-CN/(?:category|uncensored)/[^"]+)"\s+class="cc-header">(.*?)</a>`)
	jmtCatNameRe    = regexp.MustCompile(`(?s)cc-name">(.*?)</div>`)
	jmtCatCountRe   = regexp.MustCompile(`cc-count">([^<]+)<`)
	jmtCatPic1Re    = regexp.MustCompile(`class="cc-preview-img"[^>]*src="([^"]+)"`)
	jmtCatPic2Re    = regexp.MustCompile(`<img[^>]+src="([^"]+)"[^>]*class="cc-preview-img"`)
	jmtUncLinkRe    = regexp.MustCompile(`(?s)<a href="https://javmit\.com(/zh-CN/uncensored/[a-z0-9\-]+/)"[^>]*>(.*?)</a>`)
	jmtUncCountRe   = regexp.MustCompile(`<span[^>]*>([\d,]+)</span>`)
	jmtSpanRe       = regexp.MustCompile(`(?s)<span[^>]*>.*?</span>`)
	jmtLDRe         = regexp.MustCompile(`(?s)<script type="application/ld\+json"[^>]*>(.*?)</script>`)
	jmtOgTitleRe    = regexp.MustCompile(`property="og:title" content="([^"]+)"`)
	jmtPosterRe     = regexp.MustCompile(`<video[^>]+poster="([^"]+)"`)
	jmtSiActorRe    = regexp.MustCompile(`(?s)class="si-actor-card">(.*?)</a>`)
	jmtSiActorImgRe = regexp.MustCompile(`<img[^>]+alt="([^"]+)"`)
	jmtChipRe       = regexp.MustCompile(`(?s)<a[^>]+href="https://javmit\.com/zh-CN/actor/[^"]+"[^>]*class="actor-chip"[^>]*>(.*?)</a>`)
	jmtChipNameRe   = regexp.MustCompile(`actor-chip-name">([^<]+)<`)
	jmtKindRe       = regexp.MustCompile(`<a[^>]+href="https://javmit\.com/zh-CN/(?:category|tag)/[^"]+"[^>]*>([^<]+)</a>`)
	jmtQualityRe    = regexp.MustCompile(`data-src="([^"]+)"[^>]*data-label="([^"]+)"`)
	jmtQualityBtnRe = regexp.MustCompile(`class="rawx-quality-btn`)
	jmtDefaultSrcRe = regexp.MustCompile(`data-default-src="([^"]+\.m3u8[^"]*)"`)
	jmtOgVideoRe    = regexp.MustCompile(`property="og:video" content="([^"]+)"`)
	jmtContentURLRe = regexp.MustCompile(`"contentUrl"\s*:\s*"([^"]+)"`)
	jmtISODurRe     = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)
)

func jmtPathOf(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Path == "" {
		return "/"
	}
	return p.Path
}

// jmtDurNorm normalizes the site's machine-translated durations
// ("1 小时 16 米 24 秒") to H:MM:SS like the py _dur_norm().
func jmtDurNorm(t string) string {
	nums := jmtDigitsRe.FindAllString(t, -1)
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	switch {
	case len(nums) >= 3:
		return fmt.Sprintf("%d:%02d:%02d", atoi(nums[0]), atoi(nums[1]), atoi(nums[2]))
	case len(nums) == 2:
		return fmt.Sprintf("%d:%02d", atoi(nums[0]), atoi(nums[1]))
	case len(nums) == 1:
		return nums[0]
	}
	return ""
}

func jmtParseCards(slot, htmlStr, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range jmtArticleRe.FindAllStringSubmatch(htmlStr, -1) {
		blk := m[1]
		lm := jmtThumbLinkRe.FindStringSubmatch(blk)
		if lm == nil {
			lm = jmtAnyLinkRe.FindStringSubmatch(blk)
		}
		if lm == nil {
			continue
		}
		path := jmtPathOf(lm[1])
		if strings.Contains(path, "/category/") || strings.Contains(path, "/actor/") ||
			strings.Contains(path, "/tag/") || strings.Contains(path, "/channel/") {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		pic := ""
		if im := jmtImgTagRe.FindStringSubmatch(blk); im != nil {
			tag := im[0]
			if ss := jmtSrcsetRe.FindStringSubmatch(tag); ss != nil {
				best, bw := "", -1
				for _, part := range strings.Split(ss[1], ",") {
					pm := jmtSrcsetPartRe.FindStringSubmatch(strings.TrimSpace(part))
					if pm != nil {
						if w, err := strconv.Atoi(pm[2]); err == nil && w > bw {
							bw, best = w, pm[1]
						}
					}
				}
				pic = best
			}
			if pic == "" {
				if sm := jmtSrcRe.FindStringSubmatch(tag); sm != nil {
					pic = sm[1]
				}
			}
		}
		title := ""
		if tm := jmtCardTitleRe.FindStringSubmatch(blk); tm != nil {
			title = g1Text(tm[1])
		}
		if title == "" {
			if am := jmtImgAltRe.FindStringSubmatch(blk); am != nil {
				title = g1Attr(am[1])
			} else {
				title = path
			}
		}
		title = strings.TrimSpace(jmtTitleTailRe.ReplaceAllString(title, ""))
		remarks := ""
		if dm := jmtCardDurRe.FindStringSubmatch(blk); dm != nil {
			remarks = jmtDurNorm(dm[1])
		}
		if strings.Contains(blk, "rawx-unc-badge") {
			remarks = strings.TrimSpace("无码 " + remarks)
		}
		items = append(items, makeDrama(slot, path, title, pic, remarks, catName, ""))
	}
	return items
}

func jmtPagecount(htmlStr string, def int) int {
	nums := []string{}
	for _, re := range []*regexp.Regexp{jmtPageHref1Re, jmtPageHref2Re} {
		for _, m := range re.FindAllStringSubmatch(htmlStr, -1) {
			for _, n := range jmtPageNumInRe.FindAllStringSubmatch(m[1], -1) {
				nums = append(nums, n[1])
			}
		}
	}
	return g1MaxInt(nums, def)
}

func jmtUncensoredIndex(slot string) []interface{} {
	entries := []interface{}{
		makeDrama(slot, "idx:"+jmtLang+"/uncensored/", "全部未经审查", "", "4180", "未经审查", ""),
	}
	subs := [][3]string{}
	if htmlStr, err := g1Get(jmtHost+jmtLang+"/uncensored/", jmtHeaders()); err == nil {
		seen := map[string]bool{}
		for _, m := range jmtUncLinkRe.FindAllStringSubmatch(htmlStr, -1) {
			path, inner := m[1], m[2]
			if seen[path] {
				continue
			}
			seen[path] = true
			name := g1Text(jmtSpanRe.ReplaceAllString(inner, ""))
			cnt := ""
			if cm := jmtUncCountRe.FindStringSubmatch(inner); cm != nil {
				cnt = cm[1]
			}
			if name != "" {
				subs = append(subs, [3]string{path, name, cnt})
			}
		}
	}
	if len(subs) == 0 {
		subs = jmtUncSnapshot
	}
	for _, s := range subs {
		entries = append(entries, makeDrama(slot, "idx:"+s[0], s[1], "", s[2], "未经审查", ""))
	}
	return entries
}

func jmtCategoriesIndex(slot string) ([]interface{}, error) {
	entries := []interface{}{}
	htmlStr, err := g1Get(jmtHost+jmtLang+"/categories/", jmtHeaders())
	if err != nil {
		return nil, err
	}
	for _, blk := range strings.Split(htmlStr, `<div class="cat-card">`)[1:] {
		hm := jmtCatHeaderRe.FindStringSubmatch(blk)
		if hm == nil {
			continue
		}
		name := jmtPathOf(hm[1])
		if nm := jmtCatNameRe.FindStringSubmatch(blk); nm != nil {
			name = g1Text(nm[1])
		}
		cnt := ""
		if cm := jmtCatCountRe.FindStringSubmatch(blk); cm != nil {
			cnt = g1Text(cm[1])
		}
		pic := ""
		if pm := jmtCatPic1Re.FindStringSubmatch(blk); pm != nil {
			pic = pm[1]
		} else if pm := jmtCatPic2Re.FindStringSubmatch(blk); pm != nil {
			pic = pm[1]
		}
		entries = append(entries, makeDrama(slot, "idx:"+jmtPathOf(hm[1]), name, pic, cnt, "分类", ""))
	}
	if len(entries) == 0 {
		for _, m := range jmtCatHeader2Re.FindAllStringSubmatch(htmlStr, -1) {
			name := g1Text(m[2])
			if nm := jmtCatNameRe.FindStringSubmatch(m[2]); nm != nil {
				name = g1Text(nm[1])
			}
			entries = append(entries, makeDrama(slot, "idx:"+jmtPathOf(m[1]), name, "", "", "分类", ""))
		}
	}
	return entries, nil
}

func jmtActorsIndex(slot string, page int) ([]interface{}, int, error) {
	u := jmtHost + jmtLang + "/actors/"
	if page > 1 {
		u += fmt.Sprintf("page/%d/", page)
	}
	htmlStr, err := g1Get(u, jmtHeaders())
	if err != nil {
		return nil, 0, err
	}
	entries := []interface{}{}
	for _, m := range jmtActorRe.FindAllStringSubmatch(htmlStr, -1) {
		blk := m[2]
		name := jmtPathOf(m[1])
		if nm := jmtActorNameRe.FindStringSubmatch(blk); nm != nil {
			name = g1Text(nm[1])
		}
		cnt := ""
		if cm := jmtActorCountRe.FindStringSubmatch(blk); cm != nil {
			cnt = g1Text(cm[1])
		}
		pic := ""
		if pm := jmtActorImgRe.FindStringSubmatch(blk); pm != nil {
			pic = pm[1]
		}
		entries = append(entries, makeDrama(slot, "idx:"+jmtPathOf(m[1]), name, pic, cnt, "演员", ""))
	}
	pc := jmtPagecount(htmlStr, page)
	if pc < page {
		pc = page
	}
	return entries, pc, nil
}

// jmtListing fetches a generic in-site list path (category / actor /
// tag / uncensored sub-source / subtitles) with /page/N/ pagination.
func jmtListing(slot, path string, page int, catName string) ([]interface{}, int, error) {
	u := jmtHost + path
	if page > 1 {
		u = strings.TrimSuffix(u, "/") + fmt.Sprintf("/page/%d/", page)
	}
	htmlStr, err := g1Get(u, jmtHeaders())
	if err != nil {
		return nil, 0, err
	}
	return jmtParseCards(slot, htmlStr, catName), jmtPagecount(htmlStr, page), nil
}

func jmtCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "home"
	}
	if page < 1 {
		page = 1
	}
	catName := g1CatName(jmtCats, category)
	switch category {
	case "home":
		htmlStr, err := g1Get(jmtHost+jmtLang+"/", jmtHeaders())
		if err != nil {
			return nil, 0, false, err
		}
		items := jmtParseCards(slot, htmlStr, catName)
		return items, len(items), false, nil
	case "uncensored_index":
		items := jmtUncensoredIndex(slot)
		return items, len(items), false, nil
	case "categories_index":
		items, err := jmtCategoriesIndex(slot)
		if err != nil {
			return nil, 0, false, err
		}
		return items, len(items), false, nil
	case "actors_index":
		items, pc, err := jmtActorsIndex(slot, page)
		if err != nil {
			return nil, 0, false, err
		}
		return items, pc * 100, page < pc, nil
	case "video":
		q := "sort=date&date=all&view=grid&rawx_per_page=48"
		u := fmt.Sprintf("%s%s/video/?%s", jmtHost, jmtLang, q)
		if page > 1 {
			u = fmt.Sprintf("%s%s/video/page/%d/?%s", jmtHost, jmtLang, page, q)
		}
		htmlStr, err := g1Get(u, jmtHeaders())
		if err != nil {
			return nil, 0, false, err
		}
		items := jmtParseCards(slot, htmlStr, catName)
		pc := jmtPagecount(htmlStr, page)
		return items, pc * 48, page < pc, nil
	default:
		path := category
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		items, pc, err := jmtListing(slot, path, page, catName)
		if err != nil {
			return nil, 0, false, err
		}
		return items, pc * 12, page < pc, nil
	}
}

// ---- javmit detail ----

type jmtDetailData struct {
	title, pic, desc, dur string
	actors, kinds         []string
	lines                 [][2]string // (label, m3u8)
}

func jmtFindVideoObject(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		if t["@type"] == "VideoObject" {
			return t
		}
		if g, ok := t["@graph"].([]interface{}); ok {
			for _, o := range g {
				if r := jmtFindVideoObject(o); r != nil {
					return r
				}
			}
		}
	case []interface{}:
		for _, o := range t {
			if r := jmtFindVideoObject(o); r != nil {
				return r
			}
		}
	}
	return nil
}

func jmtFetchDetail(path string) (*jmtDetailData, error) {
	htmlStr, err := g1Get(jmtHost+path, jmtHeaders())
	if err != nil {
		return nil, err
	}
	d := &jmtDetailData{}
	// JSON-LD VideoObject first (py priority), page elements override
	// poster / fallbacks below.
	var ld map[string]interface{}
	for _, m := range jmtLDRe.FindAllStringSubmatch(htmlStr, -1) {
		var v interface{}
		if err := json.Unmarshal([]byte(m[1]), &v); err != nil {
			continue
		}
		if vo := jmtFindVideoObject(v); vo != nil {
			ld = vo
			break
		}
	}
	if ld != nil {
		if s, ok := ld["name"].(string); ok {
			d.title = g1Text(s)
		}
		if s, ok := ld["description"].(string); ok {
			d.desc = g1Text(s)
		}
		switch t := ld["thumbnailUrl"].(type) {
		case string:
			d.pic = t
		case []interface{}:
			if len(t) > 0 {
				if s, ok := t[0].(string); ok {
					d.pic = s
				}
			}
		}
		if s, ok := ld["duration"].(string); ok {
			if dm := jmtISODurRe.FindStringSubmatch(s); dm != nil {
				atoi := func(x string) int { n, _ := strconv.Atoi(x); return n }
				h, mi, sec := atoi(dm[1]), atoi(dm[2]), atoi(dm[3])
				if h > 0 {
					d.dur = fmt.Sprintf("%d:%02d:%02d", h, mi, sec)
				} else {
					d.dur = fmt.Sprintf("%d:%02d", mi, sec)
				}
			}
		}
		if arr, ok := ld["actor"].([]interface{}); ok {
			for _, a := range arr {
				if am, ok := a.(map[string]interface{}); ok {
					if n, ok := am["name"].(string); ok && n != "" {
						d.actors = append(d.actors, n)
					}
				}
			}
		}
	}
	// actors: si-actor-card avatar alt (Chinese names) > actor-chip > LD
	chipNames := []string{}
	for _, m := range jmtSiActorRe.FindAllStringSubmatch(htmlStr, -1) {
		if am := jmtSiActorImgRe.FindStringSubmatch(m[1]); am != nil && g1Text(am[1]) != "" {
			chipNames = append(chipNames, g1Text(am[1]))
		}
	}
	if len(chipNames) == 0 {
		for _, m := range jmtChipRe.FindAllStringSubmatch(htmlStr, -1) {
			if nm := jmtChipNameRe.FindStringSubmatch(m[1]); nm != nil {
				chipNames = append(chipNames, g1Text(nm[1]))
			}
		}
	}
	if len(chipNames) > 0 {
		d.actors = chipNames
	}
	if d.title == "" {
		if om := jmtOgTitleRe.FindStringSubmatch(htmlStr); om != nil {
			d.title = strings.Replace(g1Text(om[1]), " - JAVMIT", "", 1)
		} else {
			d.title = path
		}
	}
	if pm := jmtPosterRe.FindStringSubmatch(htmlStr); pm != nil {
		d.pic = pm[1]
	}
	kinds := []string{}
	for _, m := range jmtKindRe.FindAllStringSubmatch(htmlStr, -1) {
		t := g1Text(m[1])
		dup := false
		for _, k := range kinds {
			if k == t {
				dup = true
			}
		}
		if t != "" && !dup && len(kinds) < 8 {
			kinds = append(kinds, t)
		}
	}
	d.kinds = kinds
	// play lines: quality buttons (data-src + data-label), in page order
	seen := map[string]bool{}
	if jmtQualityBtnRe.MatchString(htmlStr) {
		for _, m := range jmtQualityRe.FindAllStringSubmatch(htmlStr, -1) {
			src, lab := m[1], m[2]
			if src == "" || seen[src] {
				continue
			}
			seen[src] = true
			if lab == "" {
				lab = "默认"
			}
			d.lines = append(d.lines, [2]string{lab, src})
		}
	}
	if len(d.lines) == 0 {
		var mm []string
		if x := jmtDefaultSrcRe.FindStringSubmatch(htmlStr); x != nil {
			mm = x
		} else if x := jmtOgVideoRe.FindStringSubmatch(htmlStr); x != nil {
			mm = x
		} else if x := jmtContentURLRe.FindStringSubmatch(htmlStr); x != nil {
			mm = x
		}
		if mm != nil {
			d.lines = append(d.lines, [2]string{"默认", strings.ReplaceAll(mm[1], `\/`, "/")})
		}
	}
	return d, nil
}

func jmtDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		path := rawID[4:]
		works, _, err := jmtListing(slot, path, 1, "索引")
		if err != nil {
			return nil, err
		}
		title, cover := g1StoredMeta(slot, rawID)
		return g1WorksDrama(slot, rawID, title, cover, "索引", works), nil
	}
	base, _ := splitEpisodeSuffix(rawID)
	path := base
	if !strings.HasPrefix(path, "/") {
		path = jmtPathOf(base)
	}
	dd, err := jmtFetchDetail(path)
	if err != nil {
		return nil, err
	}
	desc := dd.desc
	meta := []string{}
	if len(dd.actors) > 0 {
		meta = append(meta, "主演："+strings.Join(dd.actors, ", "))
	}
	if len(dd.kinds) > 0 {
		meta = append(meta, "分类："+strings.Join(dd.kinds, ", "))
	}
	if len(meta) > 0 {
		if desc != "" {
			desc += "\n"
		}
		desc += strings.Join(meta, " / ")
	}
	catName := strings.Join(dd.kinds, ", ")
	if catName == "" {
		catName = "JAVMIT"
	}
	d := makeDrama(slot, base, dd.title, dd.pic, dd.dur, catName, desc)
	names := []string{}
	for _, l := range dd.lines {
		names = append(names, l[0])
	}
	g1RouteChapters(d, slot, base, names)
	return d, nil
}

func jmtResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return nil, fmt.Errorf("javmit: folder has no play url")
	}
	base, idx := splitEpisodeSuffix(rawID)
	path := base
	if !strings.HasPrefix(path, "/") {
		path = jmtPathOf(base)
	}
	dd, err := jmtFetchDetail(path)
	if err != nil {
		return nil, err
	}
	if len(dd.lines) == 0 {
		return nil, fmt.Errorf("javmit: no play url")
	}
	if idx < 1 || idx > len(dd.lines) {
		idx = 1
	}
	return g1PlayResult(dd.lines[idx-1][1], slot, rawID, jmtUA, jmtHost+"/"), nil
}

func jmtSearch(slot, kw string) ([]interface{}, error) {
	u := fmt.Sprintf("%s%s/?s=%s", jmtHost, jmtLang, url.QueryEscape(kw))
	htmlStr, err := g1Get(u, jmtHeaders())
	if err != nil {
		return []interface{}{}, err
	}
	return jmtParseCards(slot, htmlStr, "搜索"), nil
}
