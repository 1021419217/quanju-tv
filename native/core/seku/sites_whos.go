package seku

// ---- whos.tv (识图搜片) — huangdou slot, v4 replacement for SupJav ----
//
// Ported from the user's ZakaTV jar classes ZakaWhosAmns / ZakaWhosCats
// (config key ZakaWhos "🔍whos识图┃原生"). SupJav was removed from
// huangdou in v4 by user order; the supjav* functions stay in
// sites_g2.go, unregistered.
//
// Site model: a frame-recognition / image-search catalogue. The stream
// the site itself exposes per video is its preview HLS
// (data-preview-source on the watch page) — that is also what the jar
// plays, and it is ported as-is.
//
// Live-verified deviations from the jar (2026-10-23):
//   - The jar's frame category table (ZakaWhosCats, ~398 label pairs)
//     no longer matches the site: /frames?type_id=, ?label_id=, ?tags=
//     ALL return the same unfiltered pool now. Filtering moved
//     client-side (frame-explore.js filters by each item's data-tags).
//     Categories therefore are: the pool itself plus the first 12 live
//     label chips, implemented by bounded pool scanning (5 pool pages
//     per UI page) with data-tags matching. The remaining ~670 chips
//     are not categories: the server cannot page them and a bounded
//     scan cannot fill a page for sparse labels — fake categories
//     would render empty.
//   - The jar leaves frame items unplayable (its id encoding drops the
//     source slug). Here a frame card carries its source video slug
//     (parsed from the frame title's leading code token, the jar's own
//     slugOf rule) and detail/resolve delegate to that video.

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const whosSite = "https://whos.tv"
const whosImgCDN = "https://f.hersav.me"

const whosUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func whosHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      whosUA,
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.6",
		"Referer":         whosSite + "/",
	}
}

// small page cache (jar http() caches pages; 120s is enough here)
var whosPageCache = &struct {
	mu sync.Mutex
	m  map[string]whosPageEntry
}{m: map[string]whosPageEntry{}}

type whosPageEntry struct {
	at   time.Time
	text string
}

// whosAnchorsOK validates that a fetched page is really the list page
// it claims to be. v5 device hardening: the jar reaches whos.tv via a
// shield-aware fetcher (ZakaShield.fetch) because the site serves
// interstitial/challenge variants to some egresses with HTTP 200. The
// old check only recognised a "<12000B + 'just a moment'" page, so an
// undetected interstitial was parsed as a list page, yielded zero
// cards, and the UI showed a fake empty state ("no data" on device
// while this egress got the real page). A real list page always
// carries its item anchors; anything else is a failed fetch.
func whosAnchorsOK(path, text string) bool {
	switch {
	case path == "/videos" || strings.HasPrefix(path, "/videos/page-"):
		return whosVideoAnchorRe.MatchString(text)
	case path == "/actresses" || strings.HasPrefix(path, "/actresses/page-"):
		return strings.Contains(text, "/actresses/")
	case path == "/frames" || strings.HasPrefix(path, "/frames/page-"):
		return whosFrameAnchorRe.MatchString(text)
	case path == "/ranking/video":
		return whosVideoAnchorRe.MatchString(text)
	}
	return true // detail & search pages: no list-anchor contract
}

func whosFetch(path string) (string, error) {
	whosPageCache.mu.Lock()
	if e, ok := whosPageCache.m[path]; ok && time.Since(e.at) < 120*time.Second {
		t := e.text
		whosPageCache.mu.Unlock()
		return t, nil
	}
	whosPageCache.mu.Unlock()
	u := whosSite + path
	body, code, _, err := httpGet(u, whosHeaders())
	if err != nil || code != 200 || isChallengePage(string(body)) || !whosAnchorsOK(path, string(body)) {
		b2, c2, _, e2 := tlsGet(u, whosHeaders())
		if e2 == nil && c2 == 200 && !isChallengePage(string(b2)) && whosAnchorsOK(path, string(b2)) {
			body, code, err = b2, c2, nil
		}
	}
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("whos fetch %s -> %d", path, code)
	}
	text := string(body)
	if !whosAnchorsOK(path, text) {
		// Never cache or serve an anchor-less list page: it is an
		// interstitial variant, and serving it would surface as a
		// fake empty category in the UI.
		return "", fmt.Errorf("whos fetch %s: page failed anchor validation (interstitial?)", path)
	}
	whosPageCache.mu.Lock()
	whosPageCache.m[path] = whosPageEntry{at: time.Now(), text: text}
	whosPageCache.mu.Unlock()
	return text, nil
}

func whosClean(s string) string {
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func whosTrunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// ---- xor-obfuscated cover URLs ----

func whosXorURL(hexStr string) string {
	if len(hexStr) < 6 {
		return ""
	}
	key, err := strconv.ParseUint(hexStr[len(hexStr)-2:], 16, 8)
	if err != nil {
		return ""
	}
	body := hexStr[:len(hexStr)-2]
	var b strings.Builder
	for i := 0; i+1 < len(body); i += 2 {
		v, err := strconv.ParseUint(body[i:i+2], 16, 8)
		if err != nil {
			return ""
		}
		b.WriteByte(byte(v ^ key))
	}
	return b.String()
}

// ---- video cards ----

var (
	whosVideoAnchorRe = regexp.MustCompile(`<a[^>]+href="/videos/`)
	whosVideoSlugRe   = regexp.MustCompile(`<a[^>]+href="/videos/([A-Za-z0-9\-_]+)"`)
	whosCoverHexRe    = regexp.MustCompile(`data-cover-src="([0-9a-fA-F]{20,})"`)
	whosImgSrcRe      = regexp.MustCompile(`<img[^>]+src="(https?://[^"]+)"`)
	whosH3Re          = regexp.MustCompile(`(?s)<h3[^>]*>(.{0,300}?)</h3>`)
	whosAltRe         = regexp.MustCompile(`alt="([^"]{2,200})"`)
	whosPRe           = regexp.MustCompile(`(?s)<p[^>]*>(.{0,200}?)</p>`)
)

// whosVideos parses a /videos-style list page. rawID "v:<slug>".
func whosVideos(page string) []g2Work {
	works := []g2Work{}
	seen := map[string]bool{}
	idxs := whosVideoAnchorRe.FindAllStringIndex(page, -1)
	for i, ix := range idxs {
		end := len(page)
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		}
		chunk := page[ix[0]:end]
		if len(chunk) > 8000 {
			chunk = chunk[:8000]
		}
		m := whosVideoSlugRe.FindStringSubmatch(chunk)
		if m == nil {
			continue
		}
		slug := m[1]
		if seen[slug] {
			continue
		}
		seen[slug] = true
		cover := ""
		if cm := whosCoverHexRe.FindStringSubmatch(chunk); cm != nil {
			if u := whosXorURL(cm[1]); strings.HasPrefix(u, "http") {
				cover = u
			}
		}
		if cover == "" {
			if im := whosImgSrcRe.FindStringSubmatch(chunk); im != nil {
				cover = html.UnescapeString(im[1])
			}
		}
		title := ""
		if tm := whosH3Re.FindStringSubmatch(chunk); tm != nil {
			title = whosClean(tm[1])
		}
		if title == "" {
			if am := whosAltRe.FindStringSubmatch(chunk); am != nil {
				title = whosClean(am[1])
			}
		}
		if title == "" {
			if pm := whosPRe.FindStringSubmatch(chunk); pm != nil {
				title = whosClean(pm[1])
			}
		}
		if title == "" {
			title = strings.ToUpper(slug)
		}
		title = whosTrunc(title, 120)
		works = append(works, g2Work{RawID: "v:" + slug, Title: title, Cover: cover, Remark: strings.ToUpper(slug)})
	}
	return works
}

// ---- actress cards ----

var (
	whosActAnchorRe = regexp.MustCompile(`<a[^>]+href="/actresses/`)
	whosActSlugRe   = regexp.MustCompile(`<a[^>]+href="/actresses/([^"?#]{1,90})"`)
	whosActAltRe    = regexp.MustCompile(`<img[^>]+alt="([^"]+)"`)
)

// whosActresses parses /actresses list pages. rawID "a:<slug>".
func whosActresses(page string) []g2Work {
	works := []g2Work{}
	seen := map[string]bool{}
	idxs := whosActAnchorRe.FindAllStringIndex(page, -1)
	for i, ix := range idxs {
		end := len(page)
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		}
		chunk := page[ix[0]:end]
		if len(chunk) > 6000 {
			chunk = chunk[:6000]
		}
		m := whosActSlugRe.FindStringSubmatch(chunk)
		if m == nil {
			continue
		}
		slug := strings.TrimSuffix(m[1], "/")
		if slug == "" || strings.HasPrefix(slug, "page-") || seen[slug] {
			continue
		}
		seen[slug] = true
		name := ""
		if am := whosActAltRe.FindStringSubmatch(chunk); am != nil {
			name = whosClean(am[1])
		}
		if name == "" {
			name = slug
		}
		cover := ""
		if im := whosImgSrcRe.FindStringSubmatch(chunk); im != nil {
			cover = html.UnescapeString(im[1])
		}
		works = append(works, g2Work{RawID: "a:" + slug, Title: name, Cover: cover, Remark: "演员"})
	}
	return works
}

// ---- frames ----

var (
	// The jar matches frames with one page-wide regex using
	// [\s\S]{0,1600}? spans; Go's RE2 caps repeat counts at 1000, so
	// the same semantics are done as anchor + forward window scans.
	whosFrameAnchorRe = regexp.MustCompile(`<a href="/frames/(\d+)"`)
	whosFrameImgRe    = regexp.MustCompile(`<img\s+src="([^"]+)"`)
	whosFrameTitleRe  = regexp.MustCompile(`data-frame-title="([^"]*)"`)
	whosFrameTimeRe   = regexp.MustCompile(`<span class="font-medium">([^<]*)</span>`)
	whosTagsRe        = regexp.MustCompile(`data-tags="([^"]*)"`)
	whosCodeRe        = regexp.MustCompile(`([A-Za-z0-9][A-Za-z0-9\-_]{2,40})`)
	whosTimeRe        = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2})`)
)

type whosFrame struct {
	work g2Work
	tags string
}

// whosFrames parses one /frames pool page, keeping each item's data-tags.
func whosFrames(page string) []whosFrame {
	out := []whosFrame{}
	locs := whosFrameAnchorRe.FindAllStringIndex(page, -1)
	for li, loc := range locs {
		id := page[loc[0]+len(`<a href="/frames/`):]
		id = id[:strings.IndexByte(id, '"')]
		winEnd := len(page)
		if li+1 < len(locs) {
			winEnd = locs[li+1][0]
		}
		if winEnd > loc[1]+7000 {
			winEnd = loc[1] + 7000
		}
		window := page[loc[1]:winEnd]
		imgM := whosFrameImgRe.FindStringSubmatch(window)
		titleM := whosFrameTitleRe.FindStringSubmatch(window)
		if imgM == nil || titleM == nil {
			continue
		}
		img := imgM[1]
		rawTitle := titleM[1]
		timeText := ""
		if tm := whosFrameTimeRe.FindStringSubmatch(window); tm != nil {
			timeText = strings.TrimSpace(tm[1])
		}
		// nearest preceding data-tags within ~2500 chars
		tags := ""
		start := loc[0]
		lo := start - 2500
		if lo < 0 {
			lo = 0
		}
		if tm := whosTagsRe.FindAllStringSubmatch(page[lo:start], -1); len(tm) > 0 {
			tags = tm[len(tm)-1][1]
		}
		title := whosClean(rawTitle)
		code := ""
		if cm := whosCodeRe.FindStringSubmatch(title); cm != nil {
			code = cm[1]
		}
		tm := ""
		if mm := whosTimeRe.FindStringSubmatch(title); mm != nil {
			tm = mm[1]
		}
		if tm == "" {
			tm = timeText
		}
		name := title
		if code != "" {
			name = code
			if tm != "" {
				name += " " + tm
			}
			rest := strings.TrimSpace(strings.TrimPrefix(title, code))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, tm))
			if rest != "" {
				name += " " + rest
			}
		}
		name = whosTrunc(name, 60)
		cover := html.UnescapeString(img)
		if !strings.HasPrefix(cover, "http") {
			cover = whosImgCDN + "/" + strings.TrimPrefix(cover, "/")
		}
		remark := tm
		if remark == "" {
			remark = "帧"
		}
		rawID := "f:" + id
		if code != "" {
			rawID += ":" + strings.ToLower(code)
		}
		out = append(out, whosFrame{
			work: g2Work{RawID: rawID, Title: name, Cover: cover, Remark: remark},
			tags: tags,
		})
	}
	return out
}

func whosFramesPagePath(pg int) string {
	if pg <= 1 {
		return "/frames"
	}
	return "/frames/page-" + strconv.Itoa(pg)
}

// top-12 live label chips (site order, extracted from the live /frames
// page 2026-10-23; the full 684-chip table is client-side only).
var whosChips = []struct{ id, name string }{
	{"49", "全裸"}, {"255", "裸体"}, {"168", "白色衬衫"}, {"248", "白色上衣"},
	{"116", "白衬衫"}, {"98", "黑色丝袜"}, {"476", "内衣"}, {"152", "黑色短裙"},
	{"568", "半裸"}, {"1110", "白色内裤"}, {"308", "衬衫"}, {"251", "职业装"},
}

// ---- categories / catalog ----

func whosCategories(slot string) []interface{} {
	out := []interface{}{
		categoryObj("vid", "影片库"),
		categoryObj("act", "演员"),
		categoryObj("ranking", "排行榜"),
		categoryObj("frames", "精彩帧图"),
	}
	for _, c := range whosChips {
		out = append(out, categoryObj("fl:"+c.id, "帧·"+c.name))
	}
	return out
}

func whosListPath(base string, pg int) string {
	if pg <= 1 {
		return base
	}
	return base + "/page-" + strconv.Itoa(pg)
}

func whosCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case strings.HasPrefix(category, "a:"):
		// v6 folder drill: an actress's works as video cards.
		slug := strings.TrimPrefix(category, "a:")
		text, err := whosFetch(whosListPath("/actresses/"+slug, page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := whosVideos(text)
		return g2WorksToItems(slot, works, "演员作品"), len(works), len(works) >= 12, nil
	case category == "vid" || category == "":
		text, err := whosFetch(whosListPath("/videos", page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := whosVideos(text)
		if len(works) == 0 && page <= 1 {
			// v5 honesty: the site's main library is never empty on
			// page 1; zero parsed works means the page variant is
			// not the real list — error state, not a fake empty tab.
			return []interface{}{}, 0, false, fmt.Errorf("whos vid: no works parsed on page 1")
		}
		return g2WorksToItems(slot, works, "影片库"), len(works), len(works) >= 12, nil
	case category == "act":
		text, err := whosFetch(whosListPath("/actresses", page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := whosActresses(text)
		if len(works) == 0 && page <= 1 {
			return []interface{}{}, 0, false, fmt.Errorf("whos act: no works parsed on page 1")
		}
		items := g2WorksToItems(slot, works, "演员")
		for _, it := range items {
			if m, ok := it.(map[string]interface{}); ok {
				if vid, _ := m["vod_id"].(string); vid != "" {
					m["category"] = vid
				}
			}
		}
		return items, len(works), len(works) >= 12, nil
	case category == "ranking":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		text, err := whosFetch("/ranking/video")
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := whosVideos(text)
		return g2WorksToItems(slot, works, "排行榜"), len(works), false, nil
	case category == "frames":
		text, err := whosFetch(whosFramesPagePath(page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		frames := whosFrames(text)
		works := []g2Work{}
		for _, f := range frames {
			works = append(works, f.work)
		}
		return g2WorksToItems(slot, works, "精彩帧图"), len(works), len(works) >= 20, nil
	case strings.HasPrefix(category, "fl:"):
		label := strings.TrimPrefix(category, "fl:")
		// bounded scan: UI page pg <- pool pages (pg-1)*5+1 .. pg*5
		works := []g2Work{}
		for p := (page-1)*5 + 1; p <= page*5 && len(works) < 20; p++ {
			text, err := whosFetch(whosFramesPagePath(p))
			if err != nil {
				if len(works) == 0 {
					return []interface{}{}, 0, false, err
				}
				break
			}
			for _, f := range whosFrames(text) {
				if len(works) >= 20 {
					break
				}
				for _, t := range strings.Split(f.tags, ",") {
					if strings.TrimSpace(t) == label {
						works = append(works, f.work)
						break
					}
				}
			}
		}
		return g2WorksToItems(slot, works, "帧图标签"), len(works), len(works) >= 20, nil
	}
	return []interface{}{}, 0, false, fmt.Errorf("unknown whos category %q", category)
}

// ---- video detail ----

var (
	whosH1Re       = regexp.MustCompile(`(?s)<h1[^>]*>(.{0,300}?)</h1>`)
	whosDescRe     = regexp.MustCompile(`<meta name="description" content="([^"]{0,400})"`)
	whosCoverRe    = regexp.MustCompile(`(https://v\.hersav\.me/images/av/[a-f0-9]+\.jpg)`)
	whosOgImageRe  = regexp.MustCompile(`<meta property="og:image" content="([^"]+)"`)
	whosPreviewRe  = regexp.MustCompile(`data-preview-source="(https?://[^"]+\.m3u8[^"]*)"`)
	whosM3u8Re     = regexp.MustCompile(`(https://v\.hersav\.me/[a-f0-9]+/[a-f0-9]+\.m3u8)`)
	whosDurRe      = regexp.MustCompile(`时长[:：]?</span>\s*<span[^>]*>([^<]{1,24})</span>`)
	whosDateRe     = regexp.MustCompile(`发行日期</span>\s*<span[^>]*>([^<]{4,24})</span>`)
	whosAnyDateRe  = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)
	whosActorRefRe = regexp.MustCompile(`href="/actresses/([^"]{1,60})"`)
)

type whosVideoInfo struct {
	Title, Cover, Desc, M3u8, Duration, Date string
	Actors                                   []string
}

func whosVideoInfoFetch(slug string) (*whosVideoInfo, error) {
	text, err := whosFetch("/videos/" + slug)
	if err != nil {
		return nil, err
	}
	info := &whosVideoInfo{}
	if m := whosH1Re.FindStringSubmatch(text); m != nil {
		info.Title = whosClean(m[1])
	}
	if info.Title == "" {
		info.Title = strings.ToUpper(slug)
	}
	if m := whosDescRe.FindStringSubmatch(text); m != nil {
		info.Desc = whosClean(m[1])
	}
	if m := whosCoverRe.FindStringSubmatch(text); m != nil {
		info.Cover = m[1]
	}
	if m := whosPreviewRe.FindStringSubmatch(text); m != nil {
		info.M3u8 = html.UnescapeString(m[1])
	} else if m := whosM3u8Re.FindStringSubmatch(text); m != nil {
		info.M3u8 = m[1]
	}
	if m := whosDurRe.FindStringSubmatch(text); m != nil {
		info.Duration = strings.TrimSpace(m[1])
	}
	if m := whosDateRe.FindStringSubmatch(text); m != nil {
		info.Date = strings.TrimSpace(m[1])
	} else if m := whosAnyDateRe.FindStringSubmatch(text); m != nil {
		info.Date = m[1]
	}
	seen := map[string]bool{}
	for _, m := range whosActorRefRe.FindAllStringSubmatch(text, -1) {
		a := strings.TrimSuffix(m[1], "/")
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		info.Actors = append(info.Actors, a)
		if len(info.Actors) >= 6 {
			break
		}
	}
	return info, nil
}

func whosDetail(slot, rawID string) (map[string]interface{}, error) {
	switch {
	case strings.HasPrefix(rawID, "v:"):
		slug := rawID[2:]
		info, err := whosVideoInfoFetch(slug)
		if err != nil {
			return nil, err
		}
		remark := info.Duration
		desc := info.Desc
		if len(info.Actors) > 0 {
			actorLine := "演员：" + strings.Join(info.Actors, "、")
			if desc != "" {
				desc += " · "
			}
			desc += actorLine
		}
		if info.Date != "" {
			if desc != "" {
				desc += " · "
			}
			desc += "发行：" + info.Date
		}
		d := makeDrama(slot, rawID, info.Title, info.Cover, remark, "详情", desc)
		chs := []interface{}{g2Chapter(slot, rawID, rawID, "正片", 1)}
		g2SetChapters(d, chs)
		return d, nil
	case strings.HasPrefix(rawID, "a:"):
		// v7 (hotfix, user device report on v6): embed the actress's
		// works as titled episodes. v6 returned a zero-episode folder
		// detail pointing at the "a:<slug>" catalog drill via the
		// drama's category field, but the Dart app never navigates
		// from a drama's category — a tap on a zero-episode detail
		// dies with "暂时没有可播放的集数". The "a:" catalog drill
		// stays for the local store; the detail embeds pages 1..5
		// of her works (same fetch as the drill).
		slug := rawID[2:]
		text, err := whosFetch("/actresses/" + slug)
		if err != nil {
			return nil, err
		}
		cover := ""
		if m := whosOgImageRe.FindStringSubmatch(text); m != nil {
			cover = html.UnescapeString(m[1])
		}
		works := []g2Work{}
		seen := map[string]bool{}
		for pg := 1; pg <= 5; pg++ {
			ptext := text
			if pg > 1 {
				ptext, err = whosFetch(whosListPath("/actresses/"+slug, pg))
				if err != nil {
					break
				}
			}
			pageWorks := whosVideos(ptext)
			for _, w := range pageWorks {
				if !seen[w.RawID] {
					seen[w.RawID] = true
					works = append(works, w)
				}
			}
			if len(pageWorks) < 12 {
				break
			}
		}
		if len(works) == 0 {
			return nil, fmt.Errorf("whos actress works empty: %s", slug)
		}
		if cover == "" {
			cover = works[0].Cover
		}
		name := slug
		if st := getStore(slot).findDrama(rawID); st != nil {
			if t, _ := st["title"].(string); t != "" {
				name = t
			}
		}
		return g2WorksDetail(slot, rawID, name, cover, "演员作品", "演员", works,
			"whos.tv 演员："+slug+"（选集即作品列表，集名=片名，点开即播）"), nil
	case strings.HasPrefix(rawID, "f:"):
		// frame -> delegate to its source video (see header note)
		parts := strings.SplitN(rawID, ":", 3)
		if len(parts) != 3 || parts[2] == "" {
			return nil, fmt.Errorf("frame without source slug")
		}
		return whosDetail(slot, "v:"+parts[2])
	}
	return nil, fmt.Errorf("bad whos id %q", rawID)
}

func whosResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "f:") {
		parts := strings.SplitN(rawID, ":", 3)
		if len(parts) != 3 || parts[2] == "" {
			return nil, fmt.Errorf("frame without source slug")
		}
		rawID = "v:" + parts[2]
	}
	if !strings.HasPrefix(rawID, "v:") {
		return nil, fmt.Errorf("bad whos resolve id %q", rawID)
	}
	info, err := whosVideoInfoFetch(rawID[2:])
	if err != nil {
		return nil, err
	}
	if info.M3u8 == "" {
		return nil, fmt.Errorf("no stream for %s", rawID)
	}
	return playResult(info.M3u8, slot, rawID, whosUA, whosSite+"/", "m3u8", "hls"), nil
}

func whosSearch(slot, kw string) ([]interface{}, error) {
	text, err := whosFetch("/result?search=" + url.QueryEscape(kw) + "&page=1")
	if err != nil {
		return []interface{}{}, err
	}
	works := whosVideos(text)
	return g2WorksToItems(slot, works, "搜索"), nil
}

func init() {
	registerSite("huangdou", &siteImpl{
		Key: "whos", Name: "whos识图", DefaultCat: "vid", HomeURL: whosSite + "/",
		Categories: whosCategories,
		Catalog:    whosCatalog,
		Detail:     whosDetail,
		Resolve:    whosResolve,
		Search:     whosSearch,
		Headers:    whosHeaders,
	}, "whos")
}
