package seku

// ---- 鉴赏师 (www.javrate.com) — faguo slot, v4 replacement for XSZAV ----
//
// Ported from the user's ZakaTV jar classes ZakaJavrateAmns +
// ZakaJavrateData (config key ZakaJavrate "🛡️鉴赏师┃原生"). XSZAV was
// removed from faguo in v4 by user order; the xs* functions stay in
// sites_g6.go, unregistered.
//
// Verification status: javrate.com sits behind Cloudflare. From this
// datacenter egress every route (stdlib, TLS-impersonated Chrome
// 131/124/133, all URL families) returns the challenge page, and the
// Wayback Machine is egress-blocked too — the port therefore follows
// the jar recipe verbatim and is marked 已转·未活验 (same status MissAV
// and SupJav had in v1): on the user's network the std->tls fetch
// ladder either passes the shield or the slot shows an honest error
// state. See batch/v4work/javrate_recipe.md.
//
// Documented deviations from the jar:
//   - The jar's __tools category (Zaka-shield self-service cards) is
//     not content and is not ported.
//   - The jar returns an EMPTY detail for /actor/detail/ pages; in
//     this app's folder model an actor card must expand to something,
//     so actor detail expands to the actor page's movie cards.
//   - The jar rewrites signed-playlist segment URLs through a local
//     proxy (JrProxy) that re-signs each segment line. The relay
//     covers this with a per-session javrate hook: segment fetches
//     are mkurl-signed with a fresh token at request time.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
)

const jrSite = "https://www.javrate.com"
const jrUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func jrHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      jrUA,
		"Accept-Language": "zh-CN,zh;q=0.9,zh-TW;q=0.7,en;q=0.5",
		"Referer":         jrSite + "/",
	}
}

// ---- fetch: std -> tls ladder, list pages cached 30s ----

var jrPageCache = &struct {
	mu sync.Mutex
	m  map[string]whosPageEntry
}{m: map[string]whosPageEntry{}}

func jrDirectLadder(rawurl string, headers map[string]string, referer string, timeoutMs int) string {
	if b, code, _, err := httpGetTimeout(rawurl, headers, time.Duration(timeoutMs)*time.Millisecond); err == nil && code == 200 && len(b) > 0 {
		return string(b)
	}
	if b, code, _, err := tlsGet(rawurl, headers); err == nil && code == 200 && len(b) > 0 {
		return string(b)
	}
	return ""
}

func jrFetchURL(u string, referer string) (string, error) {
	hdr := jrHeaders()
	if referer != "" {
		hdr["Referer"] = referer
	}
	done := make(chan string, 1)
	go func() { done <- zkShieldRender(u, referer, 25000) }()
	select {
	case body := <-done:
		if body != "" && zkShieldGood(u, body, false) {
			return body, nil
		}
	case <-time.After(15 * time.Second):
	}
	body := zkShieldFetchWithDirect(u, hdr, referer, 25000, false, jrDirectLadder)
	if body == "" {
		return "", fmt.Errorf("javrate shield fetch empty: %s", u)
	}
	return body, nil
}

func jrFetchCached(u string) (string, error) {
	jrPageCache.mu.Lock()
	if e, ok := jrPageCache.m[u]; ok && time.Since(e.at) < 30*time.Second {
		t := e.text
		jrPageCache.mu.Unlock()
		return t, nil
	}
	jrPageCache.mu.Unlock()
	text, err := jrFetchURL(u, "")
	if err != nil {
		return "", err
	}
	jrPageCache.mu.Lock()
	jrPageCache.m[u] = whosPageEntry{at: time.Now(), text: text}
	jrPageCache.mu.Unlock()
	return text, nil
}

func jrClean(s string) string {
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// ---- categories ----

var jrCats = []struct{ id, name string }{
	{"/movie/new", "最新更新"},
	{"/menu/uncensored", "无码A片"},
	{"/menu/censored", "日本A片"},
	{"/menu/chinese", "国产AV"},
	{"/movie/subtitle", "中文字幕"},
	{"/best", "最多人看"},
	{"__tags", "标签找片"},
	{"/actor/list/1-0-1.html", "AV女优"},
	{"/moviesets", "专辑系列"},
	{"/issuer", "片商厂牌"},
}

func jrCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range jrCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func jrCatName(id string) string {
	for _, c := range jrCats {
		if c.id == id {
			return c.name
		}
	}
	return "列表"
}

// ---- list URL templates (jar categoryContent, no-filter defaults) ----

func jrListURL(tid string, pg int) string {
	if pg < 1 {
		pg = 1
	}
	switch {
	case tid == "/best":
		if pg == 1 {
			return jrSite + "/best"
		}
		return jrSite + "/best/thisweek?page=" + strconv.Itoa(pg)
	case tid == "/movie/new":
		if pg == 1 {
			return jrSite + tid
		}
		return jrSite + "/movie/new/2-2-" + strconv.Itoa(pg)
	case tid == "/movie/subtitle":
		if pg == 1 {
			return jrSite + tid
		}
		return jrSite + "/movie/subtitle/1-2-" + strconv.Itoa(pg)
	case strings.HasPrefix(tid, "/menu/"):
		if pg == 1 {
			return jrSite + tid
		}
		return jrSite + tid + "/5-2-" + strconv.Itoa(pg)
	case strings.HasPrefix(tid, "/actor/list"):
		if pg == 1 {
			return jrSite + "/actor/list/1-0-1.html"
		}
		return jrSite + "/actor/list/2-0-" + strconv.Itoa(pg) + ".html"
	case tid == "/issuer":
		return jrSite + "/issuer?page=" + strconv.Itoa(pg) + "&sort=3"
	case strings.HasPrefix(tid, "/keywords/"):
		return jrSite + tid + "?page=" + strconv.Itoa(pg)
	default:
		// generic drill-down template (jar): {tid}/{pg}-2-1/
		if pg == 1 {
			return jrSite + tid
		}
		return jrSite + strings.TrimSuffix(tid, "/") + "/" + strconv.Itoa(pg) + "-2-1/"
	}
}

// ---- page info ----

var (
	jrPageInfoRe  = regexp.MustCompile(`(?i)(?:page|p)(?:=|/)(\d+)[^0-9]{0,20}?(页|共)`)
	jrPageInfoRe2 = regexp.MustCompile(`共\s*(\d+)\s*页|/页\s*(\d+)`)
)

func jrTotalPages(page string) int {
	n := 0
	if m := jrPageInfoRe.FindStringSubmatch(page); m != nil {
		n, _ = strconv.Atoi(m[1])
	}
	if n == 0 {
		if m := jrPageInfoRe2.FindStringSubmatch(page); m != nil {
			if m[1] != "" {
				n, _ = strconv.Atoi(m[1])
			} else {
				n, _ = strconv.Atoi(m[2])
			}
		}
	}
	if n > 2000 {
		n = 2000
	}
	return n
}

func jrHasMore(page string, pg, count, threshold int) bool {
	if tp := jrTotalPages(page); tp > 0 {
		return pg < tp
	}
	return count >= threshold
}

// ---- cards ----

var (
	jrCardAnchorRe  = regexp.MustCompile(`<a href="(/movie/detail/([a-f0-9\-]+)\.html)"[^>]*title="([^"]*)"`)
	jrCardAnchor2Re = regexp.MustCompile(`(?s)<a href="(/movie/detail/([a-f0-9\-]+)\.html)"[^>]*>(.*?)</a>`)
	jrImgRe         = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	jrBadgeRe       = regexp.MustCompile(`(?s)mgn-badge[^"]*"[^>]*>([^<]+)`)
	jrCardActressRe = regexp.MustCompile(`<a[^>]+href="/actor/detail/[^"]*"[^>]*>([^<]*)</a>`)
)

// jrCards parses mgn-item movie cards. rawID = the detail uuid.
func jrCards(page string) []g2Work {
	works := []g2Work{}
	seen := map[string]bool{}
	chunks := strings.Split(page, `<div class="mgn-item"`)
	for _, ch := range chunks[1:] {
		if len(ch) > 6000 {
			ch = ch[:6000]
		}
		var path, uuid, title string
		if m := jrCardAnchorRe.FindStringSubmatch(ch); m != nil {
			path, uuid, title = m[1], m[2], jrClean(m[3])
		} else if m := jrCardAnchor2Re.FindStringSubmatch(ch); m != nil {
			path, uuid = m[1], m[2]
			title = jrClean(m[3])
		} else {
			continue
		}
		_ = path
		if seen[uuid] {
			continue
		}
		seen[uuid] = true
		if title == "" {
			title = strings.ToUpper(uuid[:8])
		}
		cover := ""
		if im := jrImgRe.FindStringSubmatch(ch); im != nil {
			cover = html.UnescapeString(im[1])
			if strings.HasPrefix(cover, "//") {
				cover = "https:" + cover
			}
		}
		remarks := []string{}
		for _, bm := range jrBadgeRe.FindAllStringSubmatch(ch, -1) {
			if t := strings.TrimSpace(bm[1]); t != "" {
				remarks = append(remarks, t)
			}
		}
		actresses := []string{}
		for _, am := range jrCardActressRe.FindAllStringSubmatch(ch, -1) {
			if t := jrClean(am[1]); t != "" {
				actresses = append(actresses, t)
			}
			if len(actresses) >= 8 {
				break
			}
		}
		remark := strings.Join(remarks, " · ")
		if remark == "" && len(actresses) > 0 {
			remark = strings.Join(actresses, "、")
		}
		works = append(works, g2Work{RawID: uuid, Title: title, Cover: cover, Remark: remark})
	}
	if len(works) == 0 {
		// v5 fix: list pages (/movie/new and friends) use a different
		// card markup than the homepage — mg-item/mg-card blocks with
		// data-movie-id anchors — so the mgn-item split finds nothing
		// there and the slot's DefaultCat rendered empty (the v4 port
		// could never be verified locally: javrate was CF-blocked
		// until the core smart-route landed). Fall back to the
		// mg-card parse whenever the homepage parse yields zero.
		works = jrMgCards(page)
	}
	return works
}

var (
	jrMgAnchorRe = regexp.MustCompile(`<a href="/movie/detail/([a-f0-9\-]+)\.html"[^>]*title="([^"]*)"`)
	jrMgCodeRe   = regexp.MustCompile(`data-movie-code="([^"]*)"`)
	jrMgDurRe    = regexp.MustCompile(`mg-badge--duration">([^<]+)<`)
	jrMgSubRe    = regexp.MustCompile(`mg-badge--subtitle">([^<]+)<`)
)

// jrMgCards parses the mg-card list markup used by javrate's
// category/list pages (verified live on /movie/new, 2026-10-23).
func jrMgCards(page string) []g2Work {
	works := []g2Work{}
	seen := map[string]bool{}
	chunks := strings.Split(page, `<div class="mg-item mg-card"`)
	for _, ch := range chunks[1:] {
		if len(ch) > 6000 {
			ch = ch[:6000]
		}
		m := jrMgAnchorRe.FindStringSubmatch(ch)
		if m == nil {
			continue
		}
		uuid := m[1]
		if seen[uuid] {
			continue
		}
		seen[uuid] = true
		title := jrClean(m[2])
		if title == "" {
			title = strings.ToUpper(uuid[:8])
		}
		cover := ""
		if im := jrImgRe.FindStringSubmatch(ch); im != nil {
			cover = html.UnescapeString(im[1])
			if strings.HasPrefix(cover, "//") {
				cover = "https:" + cover
			}
		}
		remarks := []string{}
		if cm := jrMgCodeRe.FindStringSubmatch(ch); cm != nil && cm[1] != "" {
			remarks = append(remarks, cm[1])
		}
		if sm := jrMgSubRe.FindStringSubmatch(ch); sm != nil && sm[1] != "" {
			remarks = append(remarks, strings.TrimSpace(sm[1]))
		}
		if dm := jrMgDurRe.FindStringSubmatch(ch); dm != nil && dm[1] != "" {
			remarks = append(remarks, strings.TrimSpace(dm[1]))
		}
		works = append(works, g2Work{RawID: uuid, Title: title, Cover: cover, Remark: strings.Join(remarks, " · ")})
	}
	return works
}

var (
	jrActorAnchorRe  = regexp.MustCompile(`<a href="(/actor/detail/([a-f0-9\-]+)\.html)"[^>]*title="([^"]*)"`)
	jrActorAnchor2Re = regexp.MustCompile(`(?s)<a href="(/actor/detail/([a-f0-9\-]+)\.html)"[^>]*>(.*?)</a>`)
)

// jrActorCards parses actor-card blocks. rawID = the actor detail path.
func jrActorCards(page string) []g2Work {
	works := []g2Work{}
	seen := map[string]bool{}
	chunks := strings.Split(page, `<div class="actor-card`)
	for _, ch := range chunks[1:] {
		if len(ch) > 8000 {
			ch = ch[:8000]
		}
		var path, name string
		if m := jrActorAnchorRe.FindStringSubmatch(ch); m != nil {
			path, name = m[1], jrClean(m[3])
		} else if m := jrActorAnchor2Re.FindStringSubmatch(ch); m != nil {
			path, name = m[1], jrClean(m[3])
		} else {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		if name == "" {
			name = path
		}
		cover := ""
		if im := jrImgRe.FindStringSubmatch(ch); im != nil {
			cover = html.UnescapeString(im[1])
			if strings.HasPrefix(cover, "//") {
				cover = "https:" + cover
			}
		}
		remark := "女优"
		for _, bm := range jrBadgeRe.FindAllStringSubmatch(ch, -1) {
			if t := strings.TrimSpace(bm[1]); t != "" {
				remark = t
				break
			}
		}
		works = append(works, g2Work{RawID: path, Title: name, Cover: cover, Remark: remark})
	}
	return works
}

// jrSimpleCards parses movieset/issuer blocks for the given href prefix.
func jrSimpleCards(page, hrefPrefix string) []g2Work {
	works := []g2Work{}
	seen := map[string]bool{}
	anchorRe := regexp.MustCompile(`(?s)<a href="(` + hrefPrefix + `)"[^>]*>(.*?)</a>`)
	add := func(path, name, chunk string) {
		if seen[path] {
			return
		}
		seen[path] = true
		name = jrClean(name)
		if name == "" {
			name = path
		}
		cover := ""
		if im := jrImgRe.FindStringSubmatch(chunk); im != nil {
			cover = html.UnescapeString(im[1])
			if strings.HasPrefix(cover, "//") {
				cover = "https:" + cover
			}
		}
		works = append(works, g2Work{RawID: path, Title: name, Cover: cover, Remark: "合集"})
	}
	chunks := strings.Split(page, `<div class="movieset`)
	if len(chunks) < 2 {
		chunks = strings.Split(page, `<div class="issuer`)
	}
	if len(chunks) >= 2 {
		for _, ch := range chunks[1:] {
			if len(ch) > 6000 {
				ch = ch[:6000]
			}
			if m := anchorRe.FindStringSubmatch(ch); m != nil {
				add(m[1], m[2], ch)
			}
		}
	}
	if len(works) == 0 {
		for _, m := range anchorRe.FindAllStringSubmatch(page, -1) {
			add(m[1], m[2], "")
		}
	}
	return works
}

// ---- catalog ----

func jrCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "__tags":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		works := []g2Work{}
		for _, t := range javrateTags {
			works = append(works, g2Work{RawID: "/keywords/movie/" + t[1], Title: t[1], Remark: t[0]})
		}
		return g2WorksToItems(slot, works, "标签找片"), len(works), false, nil
	case category == "/moviesets":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		text, err := jrFetchCached(jrSite + "/moviesets")
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := jrSimpleCards(text, "/moviesets/[^\"?]+")
		return g2WorksToItems(slot, works, "专辑系列"), len(works), false, nil
	case category == "/issuer":
		text, err := jrFetchCached(jrListURL("/issuer", page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := jrSimpleCards(text, "/issuer/[^\"?]+")
		return g2WorksToItems(slot, works, "片商厂牌"), len(works), jrHasMore(text, page, len(works), 15), nil
	case strings.HasPrefix(category, "/actor/list"):
		text, err := jrFetchCached(jrListURL("/actor/list/1-0-1.html", page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := jrActorCards(text)
		items := g2WorksToItems(slot, works, "AV女优")
		for _, it := range items {
			if m, ok := it.(map[string]interface{}); ok {
				if vid, _ := m["vod_id"].(string); vid != "" {
					m["category"] = vid
				}
			}
		}
		return items, len(works), jrHasMore(text, page, len(works), 25), nil
	default:
		text, err := jrFetchCached(jrListURL(category, page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := jrCards(text)
		threshold := 15
		if category == "/best" {
			threshold = 40
		}
		return g2WorksToItems(slot, works, jrCatName(category)), len(works), jrHasMore(text, page, len(works), threshold), nil
	}
}

// ---- detail ----

var (
	jrDocTitleRe  = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	jrOgImageRe   = regexp.MustCompile(`<meta property="og:image" content="([^"]+)"`)
	jrMetaDescRe  = regexp.MustCompile(`<meta name="description" content="([^"]*)"`)
	jrActorNameRe = regexp.MustCompile(`(?s)<a[^>]+(?:class="[^"]*(?:actor-name|actress-link)[^"]*"|href="/actor/detail/[^"]*")[^>]*>([^<]{1,40})</a>`)
	jrUUIDRe      = regexp.MustCompile(`^[a-f0-9\-]{8,}$`)
)

func jrDetail(slot, rawID string) (map[string]interface{}, error) {
	switch {
	case strings.HasPrefix(rawID, "/moviesets/") || strings.HasPrefix(rawID, "/issuer/"):
		works := []g2Work{}
		seen := map[string]bool{}
		urls := []string{jrSite + rawID, jrSite + rawID + "?page=2&sort=5", jrSite + rawID + "?page=3&sort=5"}
		for _, u := range urls {
			text, err := jrFetchCached(u)
			if err != nil {
				if len(works) == 0 {
					return nil, err
				}
				break
			}
			for _, w := range jrCards(text) {
				if !seen[w.RawID] {
					seen[w.RawID] = true
					works = append(works, w)
				}
			}
			if len(works) >= 500 {
				break
			}
		}
		return g2WorksDetail(slot, rawID, rawID, "", "合集", "合集", works, ""), nil
	case strings.HasPrefix(rawID, "/actor/detail/"):
		// jar returns an empty detail here; the app folder model needs
		// the actor's works (documented deviation).
		text, err := jrFetchCached(jrSite + rawID)
		if err != nil {
			return nil, err
		}
		name := rawID
		if m := jrDocTitleRe.FindStringSubmatch(text); m != nil {
			name = strings.TrimSpace(strings.Split(jrClean(m[1]), "|")[0])
		}
		cover := ""
		if m := jrOgImageRe.FindStringSubmatch(text); m != nil {
			cover = html.UnescapeString(m[1])
		}
		// v7 (hotfix, user device report on v6): embed the actor's
		// works as titled episodes (pages 1..3 via the same list
		// URL builder the catalog uses). The v6 zero-episode folder
		// detail was untappable in the app (detail -> episodes is
		// the only card dispatch; zero episodes aborts the tap).
		works := []g2Work{}
		seen := map[string]bool{}
		for pg := 1; pg <= 3; pg++ {
			ptext := text
			if pg > 1 {
				var ferr error
				ptext, ferr = jrFetchCached(jrListURL(rawID, pg))
				if ferr != nil {
					break
				}
			}
			for _, w := range jrCards(ptext) {
				if !seen[w.RawID] {
					seen[w.RawID] = true
					works = append(works, w)
				}
			}
		}
		if len(works) == 0 {
			return nil, fmt.Errorf("javrate actor works empty: %s", rawID)
		}
		return g2WorksDetail(slot, rawID, name, cover, "女优作品", "女优", works,
			"选集即作品列表（集名=片名，点开即播）"), nil
	case strings.HasPrefix(rawID, "/keywords/"):
		text, err := jrFetchCached(jrSite + rawID + "?page=1")
		if err != nil {
			return nil, err
		}
		works := jrCards(text)
		tag := strings.TrimPrefix(rawID, "/keywords/movie/")
		return g2WorksDetail(slot, rawID, tag, "", "标签", "标签", works, ""), nil
	case jrUUIDRe.MatchString(rawID):
		detailURL := jrSite + "/movie/detail/" + rawID + ".html"
		text, err := jrFetchURL(detailURL, "")
		if err != nil {
			return nil, err
		}
		title := ""
		if m := jrDocTitleRe.FindStringSubmatch(text); m != nil {
			title = strings.TrimSpace(strings.Split(jrClean(m[1]), "|")[0])
		}
		if title == "" {
			title = strings.ToUpper(rawID[:8])
		}
		cover := ""
		if m := jrOgImageRe.FindStringSubmatch(text); m != nil {
			cover = html.UnescapeString(m[1])
		}
		desc := ""
		if m := jrMetaDescRe.FindStringSubmatch(text); m != nil {
			desc = jrClean(m[1])
		}
		actors := []string{}
		seenA := map[string]bool{}
		for _, m := range jrActorNameRe.FindAllStringSubmatch(text, -1) {
			if t := jrClean(m[1]); t != "" && !seenA[t] {
				seenA[t] = true
				actors = append(actors, t)
			}
			if len(actors) >= 12 {
				break
			}
		}
		if len(actors) > 0 {
			if desc != "" {
				desc += " · "
			}
			desc += "演员：" + strings.Join(actors, "、")
		}
		d := makeDrama(slot, rawID, title, cover, "", "详情", desc)
		chs := []interface{}{g2Chapter(slot, rawID, detailURL, "正片", 1)}
		g2SetChapters(d, chs)
		return d, nil
	}
	return nil, fmt.Errorf("bad javrate id %q", rawID)
}

// ---- resolve: v2-player embed -> HMAC token -> signed playlist ----

var (
	jrEmbedRes = []*regexp.Regexp{
		regexp.MustCompile(`id="v2-player"[^>]*src="([^"]+)"`),
		regexp.MustCompile(`<iframe[^>]+src="(/[Pp]layer[^"]*)"`),
		regexp.MustCompile(`"embedUrl":\s*"([^"]+)"`),
	}
	jrInitialRe = regexp.MustCompile(`initialSignedUrl:\s*'([^']+)'`)
	jrSignedRe  = regexp.MustCompile(`signedUrl:\s*'([^']+)'`)
	jrCdnM3u8Re = regexp.MustCompile(`(https?://videocdn[^\s"\']+m3u8[^\s"\']*)`)
	jrSrcVarRe  = regexp.MustCompile(`var\s+source\s*=\s*["']([^"']+)`)
	jrSrcVar2Re = regexp.MustCompile(`var\s+(?:now|url|videoUrl|src|playUrl)\s*=\s*["']([^"']+)`)
	jrAnyM3u8Re = regexp.MustCompile(`["'](https?://[^"'\s<>]+\.m3u8[^"'\s<>]*)`)
)

func jrGrab(text, name string) string {
	re1 := regexp.MustCompile(regexp.QuoteMeta(name) + `\s*[:=]\s*'([^']+)'`)
	if m := re1.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	re2 := regexp.MustCompile(regexp.QuoteMeta(name) + `\s*[:=]\s*"([^"]+)"`)
	if m := re2.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

func jrExtractM3u8(text string) string {
	if m := jrSrcVarRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	if m := jrSrcVar2Re.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	for _, m := range jrAnyM3u8Re.FindAllStringSubmatch(text, -1) {
		if !strings.Contains(m[1], "/Player?") && !strings.Contains(m[1], "/player/") {
			return m[1]
		}
	}
	return ""
}

var jrTokenPartsCache = &struct {
	mu sync.Mutex
	m  map[string]jrTokenPartsEntry
}{m: map[string]jrTokenPartsEntry{}}

type jrTokenPartsEntry struct {
	token     string
	tokenPath string
	expires   int64
}

func jrParseExpires(value interface{}) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return int64(f)
		}
	}
	return 0
}

func jrFreshTokenParts(movieId, sessionId, guidId, apiSecret string) (string, int64, string, error) {
	key := movieId + "|" + sessionId
	jrTokenPartsCache.mu.Lock()
	if e, ok := jrTokenPartsCache.m[key]; ok && e.expires-time.Now().Unix() > 8 {
		token, tokenPath, expires := e.token, e.tokenPath, e.expires
		jrTokenPartsCache.mu.Unlock()
		return token, expires, tokenPath, nil
	}
	jrTokenPartsCache.mu.Unlock()
	token, expires, tokenPath, err := jrGenerateToken(movieId, sessionId, guidId, apiSecret)
	if err != nil {
		return "", 0, "", err
	}
	jrTokenPartsCache.mu.Lock()
	jrTokenPartsCache.m[key] = jrTokenPartsEntry{token: token, tokenPath: tokenPath, expires: expires}
	jrTokenPartsCache.mu.Unlock()
	return token, expires, tokenPath, nil
}

func jrTokenURL(movieId, sessionId, guidId, apiSecret string) (string, error) {
	token, expires, tokenPath, err := jrFreshTokenParts(movieId, sessionId, guidId, apiSecret)
	if err != nil {
		return "", err
	}
	final := jrSite + "/" + guidId + "/playlist.m3u8?token=" + url.QueryEscape(token) + "&expires=" + fmt.Sprintf("%d", expires)
	if tokenPath != "" {
		final += "&token_path=" + url.QueryEscape(tokenPath)
	}
	return final, nil
}

func jrGenerateToken(movieId, sessionId, guidId, apiSecret string) (string, int64, string, error) {
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(fmt.Sprintf("%s|%d|%s", movieId, ts, sessionId)))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	payload := fmt.Sprintf(`{"sessionId":%q,"movieId":%q,"guidId":%q,"timestamp":%d,"url":%q,"signature":%q}`,
		sessionId, movieId, guidId, ts, jrSite+"/player/v2", sig)
	hdr := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   jrUA,
		"Origin":       jrSite,
		"Referer":      jrSite + "/",
	}
	postURL := jrSite + "/api/token/generate"
	body, code, _, err := httpDo("POST", postURL, strings.NewReader(payload), hdr)
	if err != nil || code != 200 || isChallengePage(string(body)) {
		// TLS-impersonated retry
		follow, _, terr := tlsHTTPClients()
		if terr != nil {
			if err != nil {
				return "", 0, "", err
			}
			return "", 0, "", fmt.Errorf("token generate -> %d", code)
		}
		req, rerr := fhttp.NewRequest("POST", postURL, strings.NewReader(payload))
		if rerr != nil {
			return "", 0, "", rerr
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, derr := follow.Do(req)
		if derr != nil {
			return "", 0, "", derr
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != 200 || isChallengePage(string(b)) {
			return "", 0, "", fmt.Errorf("token generate (tls) -> %d", resp.StatusCode)
		}
		body = b
	}
	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return "", 0, "", err
	}
	data := root
	if d, ok := root["data"].(map[string]interface{}); ok {
		data = d
	}
	token, _ := data["token"].(string)
	if token == "" {
		return "", 0, "", fmt.Errorf("token generate: no token in response")
	}
	expires := jrParseExpires(data["expires"])
	if expires <= 0 {
		expires = time.Now().Unix() + 300
	}
	tokenPath, _ := data["tokenPath"].(string)
	return token, expires, tokenPath, nil
}

type jrSession struct {
	detail        string
	sid           string
	mid           string
	guid          string
	sec           string
	origin        string
	origToken     string
	origExpires   int64
	origTokenPath string
}

func jrSessionOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return jrSite
	}
	return parsed.Scheme + "://" + parsed.Host
}

func jrSignedQuery(raw string) (string, int64, string) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", 0, ""
	}
	q := parsed.Query()
	var expires int64
	if v := q.Get("expires"); v != "" {
		expires, _ = strconv.ParseInt(v, 10, 64)
	}
	return q.Get("token"), expires, q.Get("token_path")
}

func jrResolveSigned(detailURL string) (string, *jrSession, error) {
	page, err := jrFetchURL(detailURL, "")
	if err != nil {
		return "", nil, err
	}
	embed := ""
	for _, re := range jrEmbedRes {
		if m := re.FindStringSubmatch(page); m != nil {
			embed = html.UnescapeString(m[1])
			break
		}
	}
	if embed == "" {
		return "", nil, fmt.Errorf("no player embed on detail page")
	}
	if strings.HasPrefix(embed, "/") {
		embed = jrSite + embed
	} else if !strings.HasPrefix(embed, "http") {
		embed = jrSite + "/" + embed
	}
	epage, err := jrFetchURL(embed, detailURL)
	if err != nil {
		return "", nil, err
	}
	movieId := jrGrab(epage, "movieId")
	sessionId := jrGrab(epage, "sessionId")
	guidId := jrGrab(epage, "guidId")
	apiSecret := jrGrab(epage, "apiSecret")
	initial := ""
	initialSigned := false
	if m := jrInitialRe.FindStringSubmatch(epage); m != nil {
		initial = m[1]
		initialSigned = true
	} else if m := jrSignedRe.FindStringSubmatch(epage); m != nil {
		initial = m[1]
		initialSigned = true
	} else if m := jrCdnM3u8Re.FindStringSubmatch(epage); m != nil {
		initial = m[1]
	}
	if movieId != "" && apiSecret != "" && sessionId != "" && guidId != "" && initial != "" {
		origin := jrSessionOrigin(initial)
		sess := &jrSession{detail: detailURL, sid: sessionId, mid: movieId, guid: guidId, sec: apiSecret, origin: origin}
		if initialSigned {
			sess.origToken, sess.origExpires, sess.origTokenPath = jrSignedQuery(initial)
		}
		if u, err := jrTokenURL(movieId, sessionId, guidId, apiSecret); err == nil {
			return u, sess, nil
		}
		if initialSigned {
			return initial, sess, nil
		}
	}
	if initial != "" {
		return initial, nil, nil
	}
	if u := jrExtractM3u8(epage); u != "" {
		return u, nil, nil
	}
	return "", nil, fmt.Errorf("no stream derivable from embed page")
}

func jrResolve(slot, rawID string) (map[string]interface{}, error) {
	detailURL := rawID
	if jrUUIDRe.MatchString(rawID) {
		detailURL = jrSite + "/movie/detail/" + rawID + ".html"
	}
	if !strings.HasPrefix(detailURL, "http") {
		return nil, fmt.Errorf("bad javrate resolve id %q", rawID)
	}
	u, sess, err := jrResolveSigned(detailURL)
	if err != nil {
		return nil, err
	}
	if sess != nil && sess.sid != "" && sess.mid != "" && sess.guid != "" && sess.sec != "" {
		StoreStreamHook(u, &jrStreamHook{detail: sess.detail, sid: sess.sid, mid: sess.mid, guid: sess.guid, sec: sess.sec, origin: sess.origin, origToken: sess.origToken, origExpires: sess.origExpires, origTokenPath: sess.origTokenPath})
	} else {
		query := ""
		if parsed, err := url.Parse(u); err == nil {
			query = parsed.RawQuery
		}
		StoreStreamHook(u, &jrFallbackHook{detail: detailURL, query: query})
	}
	return playResult(u, slot, rawID, jrUA, jrSite+"/", "m3u8", "hls"), nil
}

// ---- search ----

func jrSearch(slot, kw string) ([]interface{}, error) {
	text, err := jrFetchURL(jrSite+"/search/"+url.PathEscape(kw), "")
	if err != nil {
		return []interface{}{}, err
	}
	works := jrCards(text)
	if len(works) == 0 {
		text2, err2 := jrFetchURL(jrSite+"/search?keyword="+url.QueryEscape(kw), "")
		if err2 == nil {
			works = jrCards(text2)
		}
	}
	if len(works) > 60 {
		works = works[:60]
	}
	return g2WorksToItems(slot, works, "搜索"), nil
}

func init() {
	registerSite("faguo", &siteImpl{
		Key: "javrate", Name: "鉴赏师", DefaultCat: "/movie/new", HomeURL: jrSite + "/",
		Categories: jrCategories,
		Catalog:    jrCatalog,
		Detail:     jrDetail,
		Resolve:    jrResolve,
		Search:     jrSearch,
		Headers:    jrHeaders,
	}, "javrate")
}
