package seku

// v2 additions (2026-10-23): the 99jj (久久热) provider, ported from
// the Zaka jar class Zaka99jjAmns (decompiled source archived in
// batch/v2work/jar/Zaka99jjAmns.java; ZakaSec-free — host and the 11
// category hashes are plaintext in the class). It replaces YouTube
// in the xingguo slot at the user's request; the YouTube provider
// code stays in this module (impl registry), just unregistered from
// slots.
//
// Site facts (live-verified 2026-10-23 from this egress):
//   host https://www.99jj25.com — KVS template, plain HTML, no
//   shield; Go stdlib reaches it fine (200, 24 cards/page).
//   Lists: "" / "new" -> /latest-updates/ (+ /{page}/). DEVIATION
//   from the jar, deliberate: the jar maps "" to
//   /videos_list_vip.php?page=N, but that feed is 100% member-locked
//   (0/8 sampled detail pages carry a player anonymously), so as
//   the slot's main stream it would look broken; /latest-updates/
//   is the site's own public latest stream (24/page, paginated).
//   Categories -> /categories/{hash}/ and /categories/{hash}/{page}/.
//   Navigation
//   on the live homepage carries exactly the jar's 11 category
//   hashes with matching names (1:1).
//   Cards: jar regex P_CARD on <div class="item..."> anchors, cover
//   and duration read from the following 1600 chars.
//   Detail: flashvars video_url (a /get_file/... mp4 URL) plus
//   video_alt_url (HD line only when it contains /get_file/, per
//   the jar); og:image cover; h1/og:title title.
//   Play: /get_file/ and /hd.php URLs are resolved through one
//   no-redirect hop (Location -> signed CDN mp4), like the jar's
//   ZakaHttp.loc; headers UA Chrome/131 + Referer host root.
//   Search: the jar does NOT use a site search endpoint (the KVS
//   /search/{kw}/ page returns no cards) — it fetches the home
//   stream, the vip list (2 pages) and every category's first page
//   and filters titles locally. Ported as-is.

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

const jjBase = "https://www.99jj25.com"
const jjUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func jjHeaders(referer string) map[string]string {
	if referer == "" {
		referer = jjBase + "/"
	}
	return map[string]string{
		"User-Agent": jjUA,
		"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		// NO Accept-Language on purpose (v5 root-cause fix): with a
		// zh-CN Accept-Language the site serves a region-gated detail
		// variant WITHOUT the player block (36,681B, no video_url) —
		// every item then looked "member-locked" and detail returned
		// zero chapters ("暂时没有可播放的集数" on device). Without
		// the header the same page is 40,585B with video_url present.
		// Bisect-verified 2026-10-23 (jj_debug tests): lang-only ->
		// stripped, accept-only -> full.
		"Referer": referer,
	}
}

var jjCats = []struct{ id, name string }{
	{"new", "最新"},
	{"cb186ba8ba160b86eb97025b94353cac", "高清"},
	{"ffc7a71b8aae8ddd42031cea42f5bb7c", "欧美"},
	{"2f3d48fda20378cbdaaa099069a4af1e", "日本有码"},
	{"5a4e793520c46cbaee2b67760e5aea28", "东京热"},
	{"0c3d3255db00e2d3b27db08661096b23", "肛交"},
	{"281b22d3c2f5255ed57e7a09bee253f1", "韩国女主播系列"},
	{"f8789a0bf37c07c754c8e6fe27daf5f6", "成人动漫"},
	{"5d915fafb4889f0b225f2b80cc975332", "小格式综合"},
	{"251e96ab3240ae3fb3dc8bc854061957", "久久热会员认证作品"},
	{"b8e26987b0bae1d56a00de631e4d1a68", "李宗瑞全集"},
	{"sm", "SM性虐"},
}

func jjCatName(id string) string {
	for _, c := range jjCats {
		if c.id == id {
			return c.name
		}
	}
	return "久久热"
}

func jjCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range jjCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

// jjFetch mirrors the jar's html(): GET host+path, body must be at
// least 800 bytes and (when a marker is given) contain it.
func jjFetch(path, marker string) (string, error) {
	body, code, _, err := httpGet(jjBase+path, jjHeaders(""))
	if err != nil || code != 200 {
		return "", fmt.Errorf("99jj http %d err %v", code, err)
	}
	text := string(body)
	if len(text) < 800 {
		return "", fmt.Errorf("99jj short body (%d) for %s", len(text), path)
	}
	if marker != "" && !strings.Contains(text, marker) {
		return "", fmt.Errorf("99jj marker %q missing for %s", marker, path)
	}
	return text, nil
}

var (
	jjCardRe = regexp.MustCompile(`(?s)<div\s+class="item[^"]*"[^>]*>\s*<a[^>]*href="(https?://[^"]*?/videos/(\d+)/[^"]*)"[^>]*title="([^"]*)"`)
	jjImgRe  = regexp.MustCompile(`<img[^>]*(?:data-original|data-src|src)="([^"]+)"`)
	jjDurRe  = regexp.MustCompile(`<div class="duration">\s*([0-9:]+)`)
	jjPathRe = regexp.MustCompile(`^https?://[^/]+(/.*)$`)
)

func jjAbsolutize(u string) string {
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "/") {
		return jjBase + u
	}
	return u
}

// jjParseCards ports the jar's cards(): dedupe by video id, vod_id
// is the href's path portion.
func jjParseCards(page string) []g2Work {
	works := []g2Work{}
	if len(page) < 500 {
		return works
	}
	seen := map[string]bool{}
	for _, m := range jjCardRe.FindAllStringSubmatchIndex(page, -1) {
		href := page[m[2]:m[3]]
		vid := page[m[4]:m[5]]
		title := html.UnescapeString(page[m[6]:m[7]])
		if seen[vid] {
			continue
		}
		seen[vid] = true
		end := m[1] + 1600
		if end > len(page) {
			end = len(page)
		}
		window := page[m[0]:end]
		cover := ""
		if im := jjImgRe.FindStringSubmatch(window); im != nil {
			cover = jjAbsolutize(strings.TrimSpace(im[1]))
		}
		dur := ""
		if dm := jjDurRe.FindStringSubmatch(window); dm != nil {
			dur = strings.TrimSpace(dm[1])
		}
		rawID := href
		if pm := jjPathRe.FindStringSubmatch(href); pm != nil {
			rawID = pm[1]
		}
		works = append(works, g2Work{RawID: rawID, Title: title, Cover: cover, Remark: dur})
	}
	return works
}

func jjWorksToItems(slot string, works []g2Work, catName string) []interface{} {
	items := []interface{}{}
	for _, w := range works {
		items = append(items, makeDrama(slot, w.RawID, w.Title, w.Cover, w.Remark, catName, ""))
	}
	return items
}

func jjCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	var path string
	if category == "" || category == "new" {
		path = "/latest-updates/"
		if page > 1 {
			path += fmt.Sprintf("%d/", page)
		}
		category = "new"
	} else {
		path = "/categories/" + category + "/"
		if page > 1 {
			path += fmt.Sprintf("%d/", page)
		}
	}
	text, err := jjFetch(path, "/videos/")
	if err != nil {
		return nil, 0, false, err
	}
	works := jjParseCards(text)
	return jjWorksToItems(slot, works, jjCatName(category)), 999999, len(works) >= 24, nil
}

var (
	jjVideoURLRe  = regexp.MustCompile(`video_url:\s*'([^']+)'`)
	jjVideoURLRe2 = regexp.MustCompile(`video_url:\s*"([^"]+)"`)
	jjAltURLRe    = regexp.MustCompile(`video_alt_url:\s*'([^']+)'`)
	jjOgImageRe   = regexp.MustCompile(`og:image"\s+content="([^"]+)"`)
	jjThumbRe     = regexp.MustCompile(`class="thumb[^"]*"[^>]*src="([^"]+)"`)
	jjH1Re        = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	jjOgTitleRe   = regexp.MustCompile(`og:title"\s+content="([^"]+)"`)
)

// jjFetchDetail returns (title, cover, mainURL, altURL).
func jjFetchDetail(rawID string) (string, string, string, string, error) {
	path := rawID
	if !strings.HasPrefix(path, "/") {
		path = "/videos/" + path + "/"
	}
	text, err := jjFetch(path, "")
	if err != nil {
		return "", "", "", "", err
	}
	main := ""
	if m := jjVideoURLRe.FindStringSubmatch(text); m != nil {
		main = strings.TrimSpace(m[1])
	} else if m := jjVideoURLRe2.FindStringSubmatch(text); m != nil {
		main = strings.TrimSpace(m[1])
	}
	alt := ""
	if m := jjAltURLRe.FindStringSubmatch(text); m != nil {
		alt = strings.TrimSpace(m[1])
	}
	cover := ""
	if m := jjOgImageRe.FindStringSubmatch(text); m != nil {
		cover = strings.TrimSpace(m[1])
	} else if m := jjThumbRe.FindStringSubmatch(text); m != nil {
		cover = strings.TrimSpace(m[1])
	}
	cover = jjAbsolutize(cover)
	main = jjAbsolutize(main)
	alt = jjAbsolutize(alt)
	title := ""
	if m := jjH1Re.FindStringSubmatch(text); m != nil {
		title = html.UnescapeString(cleanText(m[1]))
	}
	if title == "" {
		if m := jjOgTitleRe.FindStringSubmatch(text); m != nil {
			title = html.UnescapeString(m[1])
		}
	}
	return title, cover, main, alt, nil
}

func jjDetail(slot, rawID string) (map[string]interface{}, error) {
	title, cover, main, alt, err := jjFetchDetail(rawID)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = "久久热 " + rawID
	}
	remark := "正片"
	desc := "来源：久久热"
	if main == "" {
		// Member-locked entry: the anonymous detail page carries no
		// player at all (verified live). Be honest: no chapters.
		remark = "会员"
		desc = "来源：久久热 · 会员视频，匿名不可播"
	}
	d := makeDrama(slot, rawID, title, cover, remark, "久久热", desc)
	chs := []interface{}{}
	if main != "" {
		chs = append(chs, g2Chapter(slot, rawID, rawID, "正片", 1))
		if len(alt) > 20 && strings.Contains(alt, "/get_file/") {
			chs = append(chs, g2Chapter(slot, rawID, rawID+"#alt", "高清", 2))
		}
	}
	g2SetChapters(d, chs)
	return d, nil
}

func jjResolve(slot, rawID string) (map[string]interface{}, error) {
	wantAlt := strings.HasSuffix(rawID, "#alt")
	vid := strings.TrimSuffix(rawID, "#alt")
	_, _, main, alt, err := jjFetchDetail(vid)
	if err != nil {
		return nil, err
	}
	play := main
	if wantAlt && alt != "" {
		play = alt
	}
	if play == "" {
		return nil, fmt.Errorf("99jj: no play url for %s", vid)
	}
	// /get_file/ and /hd.php go through one no-redirect hop to the
	// signed CDN file (jar ZakaHttp.loc semantics).
	if strings.Contains(play, "/get_file/") || strings.Contains(play, "/hd.php") {
		req, err := newGetRequest(play, jjHeaders(""))
		if err == nil {
			if resp, err2 := noRedirectClient.Do(req); err2 == nil && resp != nil {
				loc := resp.Header.Get("Location")
				resp.Body.Close()
				if len(loc) > 20 {
					play = jjAbsolutize(loc)
				}
			}
		}
	}
	format, kind := g2PlayFormat(play)
	return playResult(play, slot, rawID, jjUA, jjBase+"/", format, kind), nil
}

// jjSearch ports the jar's local-filter search: home stream +
// latest-updates pages 1-2 + every category's first page, title
// match (the jar scans its vip list pages; those are member-locked
// here, so the public latest stream is scanned instead).
func jjSearch(slot, kw string) ([]interface{}, error) {
	kwLow := strings.ToLower(strings.TrimSpace(kw))
	if kwLow == "" {
		return []interface{}{}, nil
	}
	paths := []string{"/", "/latest-updates/", "/latest-updates/2/"}
	for _, c := range jjCats {
		if c.id != "new" {
			paths = append(paths, "/categories/"+c.id+"/")
		}
	}
	seen := map[string]bool{}
	items := []interface{}{}
	for _, p := range paths {
		text, err := jjFetch(p, "/videos/")
		if err != nil {
			continue
		}
		for _, w := range jjParseCards(text) {
			if seen[w.RawID] {
				continue
			}
			seen[w.RawID] = true
			if strings.Contains(strings.ToLower(w.Title), kwLow) {
				items = append(items, makeDrama(slot, w.RawID, w.Title, w.Cover, w.Remark, "搜索", ""))
			}
		}
	}
	return items, nil
}

func init() {
	// v5: 99jj swapped xingguo -> wuguo (user order 2026-10-23);
	// xingguo now carries JavHD (registered in sites_g2.go).
	registerSite("wuguo", &siteImpl{
		Key: "jj99", Name: "久久热", DefaultCat: "new", HomeURL: jjBase + "/",
		Categories: jjCategories,
		Catalog:    jjCatalog,
		Detail:     jjDetail,
		Resolve:    jjResolve,
		Search:     jjSearch,
		Headers:    func() map[string]string { return jjHeaders("") },
	}, "99jj", "久久热")
}
