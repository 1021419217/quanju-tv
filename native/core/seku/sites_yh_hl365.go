package seku

// ---- hl365 黑料不打烊 (yh_hl365, type 75) — Typecho HTML 抓取 ----
//
// Recipe: yuhuo hl365 module is a pure WebView shell (Hl365Activity
// loads one of absence.rhemepsy.cc / boat.rhemepsy.cc /
// d30ff67nu2qnbd.cloudfront.net); there is no native API, so this port
// scrapes the site itself — the same pages the WebView shows.
// Domain drift, live-probed 2026-10-24: absence.rhemepsy.cc 301s to
// https://absence.egiaajsd.cc/ (CloudFront, 200, 236KB homepage);
// the page canonical host is https://hl365.com/ (all internal links
// point there). Base rotation tries egiaajsd first, then hl365.com,
// then the two legacy rhemepsy hosts.
//
// Site shape (Typecho + Mirages theme, "黑料不打烊"):
//   Home:     {base}/  (page n: /page/{n}/)
//   Category: {base}/category/{slug}/  (page n: /category/{slug}/page/{n}/)
//   Post:     {base}/archives/{id}.html
//   Search:   {base}/search/{kw}/
// List cards (<article> blocks): meta url /archives/{id}.html, cover
//   img z-image-loader-url (pic.hdhwqx.cn), date + categories in
//   .post-card-info, title in .post-card-bottom-text. Ad cards (img on
//   hc237 + title "广告") are dropped.
// Post player: <div class="dplayer defplay" data-video_id="..." ...>
//   data-config='{...,"video":{"url":"<m3u8|mp4>","pic":"...",
//   "type":"hls"}}'. The video.url is the real stream (same
//   op.udhhzr.cn CDN family as melon); pre_ads/post_ads entries in the
//   same config are ads and are ignored. Multiple dplayer divs on one
//   post become multiple chapters.
//
// Categories vs site nav (homepage nav, order verbatim, 23):
//   今日看料 24hcg / 优选投放 youxuan / 每日大赛 mrds / AI短剧 swdj /
//   每日热瓜 mrrg / 黑料大事 hlda / 热门吃瓜 rgtj / 反差女神 fcns /
//   学院热瓜 xyrg / 网红吃瓜 whhl / 撸友看片 lydt / 海角乱伦 llsq /
//   黑料杂谈 hlzt / 探花大全 thdq / 网黄专辑 whzj / 明星吃瓜 mxbg /
//   av解说 avjs / 原创投稿 qgzq / 性爱技巧 wyxs / 偷拍盗摄 chjlb /
//   官场秘闻 gcmw / 禁播动漫 mrst / PMV混剪 pmv, plus 首页 (all).
//
// Live verification 2026-10-24 (this sandbox, egress proxy):
//   homepage 200 (236KB), category /category/lydt/ 200 (33 article
//     cards incl. 2026-10-24 posts), post /archives/224185.html 200;
//     its player config video.url is an op.udhhzr.cn m3u8 with
//     auth_key (same CDN as melon) — Resolve leg byte-shape proven
//     (playlist fetch + first-segment check runs in the Go smoke).
//   Covers: pic.hdhwqx.cn images are AES-128-CBC/NoPadding encrypted
//     (same transform as melon g6/a: key \"f5d965df75336270\", iv
//     \"97b60394abc2fbe1\") — verified 2026-10-24: the first home
//     cover's raw bytes are high-entropy and decrypt to a JFIF
//     JPEG. CoverFetch below applies the same decrypt; plain images
//     pass through untouched.

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var yhHL365Bases = []string{
	"https://absence.egiaajsd.cc",
	"https://hl365.com",
	"https://boat.rhemepsy.cc",
	"https://rhemepsy.cc",
}

var yhHL365Cats = []struct{ slug, name string }{
	{"24hcg", "今日看料"}, {"youxuan", "优选投放"}, {"mrds", "每日大赛"},
	{"swdj", "AI短剧"}, {"mrrg", "每日热瓜"}, {"hlda", "黑料大事"},
	{"rgtj", "热门吃瓜"}, {"fcns", "反差女神"}, {"xyrg", "学院热瓜"},
	{"whhl", "网红吃瓜"}, {"lydt", "撸友看片"}, {"llsq", "海角乱伦"},
	{"hlzt", "黑料杂谈"}, {"thdq", "探花大全"}, {"whzj", "网黄专辑"},
	{"mxbg", "明星吃瓜"}, {"avjs", "av解说"}, {"qgzq", "原创投稿"},
	{"wyxs", "性爱技巧"}, {"chjlb", "偷拍盗摄"}, {"gcmw", "官场秘闻"},
	{"mrst", "禁播动漫"}, {"pmv", "PMV混剪"},
}

var yhHL365BaseMu sync.Mutex
var yhHL365BaseIdx int

var yhHL365ArticleRe = regexp.MustCompile(`(?s)<article.*?</article>`)
var yhHL365URLRe = regexp.MustCompile(`content="[^"]*?/archives/(\d+)\.html"`)
var yhHL365ImgRe = regexp.MustCompile(`z-image-loader-url="([^"]+)"`)
var yhHL365TitleRe = regexp.MustCompile(`post-card-bottom-text">\s*([^<]+)`)
var yhHL365DateRe = regexp.MustCompile(`datePublished"[^>]*>([^<]+)`)
var yhHL365TitleTagRe = regexp.MustCompile(`<title>([^<]+)</title>`)

// yhHL365Configs extracts every dplayer data-config JSON object by
// brace-depth scanning (the config is deeply nested, so a lazy regex
// would stop at the first inner '}').
func yhHL365Configs(html string) []string {
	out := []string{}
	const marker = "data-config='"
	for i := 0; i < len(html); {
		j := strings.Index(html[i:], marker)
		if j < 0 {
			break
		}
		start := i + j + len(marker)
		if start >= len(html) || html[start] != '{' {
			i = start
			continue
		}
		depth := 0
		inStr := false
		esc := false
		end := -1
		for k := start; k < len(html); k++ {
			c := html[k]
			if inStr {
				if esc {
					esc = false
				} else if c == '\\' {
					esc = true
				} else if c == '"' {
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
					end = k + 1
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			break
		}
		out = append(out, html[start:end])
		i = end
	}
	return out
}

func yhHL365Headers() map[string]string {
	return map[string]string{
		"User-Agent": "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36",
	}
}

func yhHL365Base() string {
	yhHL365BaseMu.Lock()
	defer yhHL365BaseMu.Unlock()
	return yhHL365Bases[yhHL365BaseIdx%len(yhHL365Bases)]
}

func yhHL365Get(path string) (string, error) {
	type yhHL365Result struct {
		idx  int
		body []byte
		err  error
	}
	ch := make(chan yhHL365Result, len(yhHL365Bases))
	for idx, base := range yhHL365Bases {
		go func(i int, b string) {
			body, code, _, err := httpGet(b+path, yhHL365Headers())
			if err == nil && code == 200 && len(body) > 0 {
				ch <- yhHL365Result{idx: i, body: body}
				return
			}
			if err != nil {
				ch <- yhHL365Result{idx: i, err: err}
				return
			}
			ch <- yhHL365Result{idx: i, err: fmt.Errorf("yh_hl365 http %d", code)}
		}(idx, base)
	}
	results := make([]yhHL365Result, len(yhHL365Bases))
	var lastErr error
	for range yhHL365Bases {
		r := <-ch
		results[r.idx] = r
		if r.err != nil {
			lastErr = r.err
		}
	}
	for _, r := range results {
		if r.err == nil && !yhHL365Shielded(string(r.body)) {
			yhHL365BaseMu.Lock()
			yhHL365BaseIdx = r.idx
			yhHL365BaseMu.Unlock()
			return string(r.body), nil
		}
	}
	for _, r := range results {
		if r.err == nil {
			yhHL365BaseMu.Lock()
			yhHL365BaseIdx = r.idx
			yhHL365BaseMu.Unlock()
			return string(r.body), nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("yh_hl365: empty response for %s", path)
}

func yhHL365Shielded(html string) bool {
	lower := strings.ToLower(html)
	return strings.Contains(lower, "just a moment") ||
		strings.Contains(lower, "cf_chl") ||
		strings.Contains(lower, "attention required") ||
		strings.Contains(lower, "challenges.cloudflare.com")
}

func yhHL365Cards(slot, html string) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, art := range yhHL365ArticleRe.FindAllString(html, -1) {
		m := yhHL365URLRe.FindStringSubmatch(art)
		if m == nil {
			continue
		}
		id := m[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		title := ""
		if tm := yhHL365TitleRe.FindStringSubmatch(art); tm != nil {
			title = strings.TrimSpace(tm[1])
		}
		if title == "" || title == "广告" {
			continue
		}
		cover := ""
		if im := yhHL365ImgRe.FindStringSubmatch(art); im != nil {
			cover = im[1]
		}
		if strings.Contains(cover, "/hc237/") {
			continue // ad card
		}
		date := ""
		if dm := yhHL365DateRe.FindStringSubmatch(art); dm != nil {
			date = strings.TrimSpace(strings.TrimSuffix(dm[1], "•"))
			date = strings.TrimSpace(date)
		}
		out = append(out, makeDrama(slot, id, title, cover, date, "", ""))
	}
	return out
}

func yhHL365Categories(slot string) []interface{} {
	out := []interface{}{categoryObj("all", "首页")}
	for _, c := range yhHL365Cats {
		out = append(out, categoryObj("cat:"+c.slug, c.name))
	}
	return out
}

func yhHL365Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	var path string
	switch {
	case category == "" || category == "all":
		if page == 1 {
			path = "/"
		} else {
			path = fmt.Sprintf("/page/%d/", page)
		}
	case strings.HasPrefix(category, "cat:"):
		slug := strings.TrimPrefix(category, "cat:")
		if page == 1 {
			path = "/category/" + slug + "/"
		} else {
			path = fmt.Sprintf("/category/%s/page/%d/", slug, page)
		}
	default:
		return nil, 0, false, fmt.Errorf("yh_hl365: unknown category %s", category)
	}
	html, err := yhHL365Get(path)
	if err != nil {
		return nil, 0, false, err
	}
	if yhHL365Shielded(html) {
		return nil, 0, false, fmt.Errorf("yh_hl365: shield page for %s", path)
	}
	items := yhHL365Cards(slot, html)
	if len(items) == 0 {
		return nil, 0, false, fmt.Errorf("yh_hl365: no cards on %s, page structure may have changed", path)
	}
	// Next-page detection: the theme emits relative links on some
	// pages and absolute links on others; match the /page/{n+1}/
	// segment either way (both home and category pagination share it).
	hasMore := strings.Contains(html, "/page/"+strconv.Itoa(page+1)+"/")
	return items, 0, hasMore, nil
}

// yhHL365Players extracts every dplayer config's video URL from a post.
func yhHL365Players(html string) []map[string]string {
	out := []map[string]string{}
	for _, cfgJSON := range yhHL365Configs(html) {
		var cfg struct {
			Video struct {
				URL string `json:"url"`
				Pic string `json:"pic"`
			} `json:"video"`
		}
		if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
			continue
		}
		u := cfg.Video.URL
		if u == "" {
			continue
		}
		// Ads live in pre_ads/post_ads keys, not in video.url; still,
		// skip anything that is not a media URL.
		if !strings.Contains(u, ".m3u8") && !strings.Contains(u, ".mp4") {
			continue
		}
		out = append(out, map[string]string{"url": u, "pic": cfg.Video.Pic})
	}
	return out
}

func yhHL365Detail(slot, rawID string) (map[string]interface{}, error) {
	id := rawID
	if i := strings.Index(rawID, ":"); i >= 0 && strings.HasPrefix(rawID, "post:") {
		rest := strings.TrimPrefix(rawID, "post:")
		if j := strings.LastIndex(rest, ":"); j >= 0 {
			id = rest[:j]
		} else {
			id = rest
		}
	}
	html, err := yhHL365Get("/archives/" + id + ".html")
	if err != nil {
		return nil, err
	}
	title := "文章" + id
	if tm := yhHL365TitleTagRe.FindStringSubmatch(html); tm != nil {
		title = strings.TrimSpace(strings.Split(tm[1], " - ")[0])
	}
	players := yhHL365Players(html)
	cover := ""
	if len(players) > 0 {
		cover = players[0]["pic"]
	}
	d := makeDrama(slot, rawID, title, cover, "", "", "")
	if len(players) > 0 {
		ids := []string{}
		names := []string{}
		for i := range players {
			ids = append(ids, fmt.Sprintf("post:%s:%d", id, i))
			if len(players) == 1 {
				names = append(names, "正片")
			} else {
				names = append(names, fmt.Sprintf("第%d段", i+1))
			}
		}
		g5SetChapters(d, slot, rawID, ids, names)
	}
	return d, nil
}

func yhHL365Resolve(slot, rawID string) (map[string]interface{}, error) {
	id := rawID
	idx := 0
	if strings.HasPrefix(rawID, "post:") {
		rest := strings.TrimPrefix(rawID, "post:")
		parts := strings.Split(rest, ":")
		if len(parts) == 2 {
			id = parts[0]
			if n, err := strconv.Atoi(parts[1]); err == nil {
				idx = n
			}
		} else {
			id = rest
		}
	}
	html, err := yhHL365Get("/archives/" + id + ".html")
	if err != nil {
		return nil, err
	}
	players := yhHL365Players(html)
	if idx >= len(players) {
		return nil, fmt.Errorf("yh_hl365: no video in post %s", id)
	}
	u := players[idx]["url"]
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhHL365Headers()["User-Agent"], yhHL365Base()+"/", format, protocol), nil
}

func yhHL365Search(slot, kw string) ([]interface{}, error) {
	html, err := yhHL365Get("/search/" + url.PathEscape(kw) + "/")
	if err != nil {
		return nil, err
	}
	if yhHL365Shielded(html) {
		return nil, fmt.Errorf("yh_hl365: shield page for search")
	}
	return yhHL365Cards(slot, html), nil
}

func yhHL365CoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpGet(u, yhHL365Headers())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_hl365 cover http %d", code)
	}
	if len(body) >= 4 && (body[0] == 0xFF && body[1] == 0xD8 || body[0] == 0x89 && body[1] == 0x50 || body[0] == 'R' && body[1] == 'I' && body[2] == 'F' && body[3] == 'F' || body[0] == 'G' && body[1] == 'I' && body[2] == 'F') {
		return body, nil
	}
	block, err := aes.NewCipher([]byte("f5d965df75336270"))
	if err != nil {
		return nil, err
	}
	trimmed := body[:len(body)/aes.BlockSize*aes.BlockSize]
	out := make([]byte, len(trimmed))
	cipher.NewCBCDecrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(out, trimmed)
	return out, nil
}

func init() {
	registerSite("yh_hl365", &siteImpl{
		Key:        "yh_hl365",
		Name:       "黑料不打烊",
		DefaultCat: "all",
		HomeURL:    yhHL365Bases[0] + "/",
		Categories: yhHL365Categories,
		Catalog:    yhHL365Catalog,
		Detail:     yhHL365Detail,
		Resolve:    yhHL365Resolve,
		Search:     yhHL365Search,
		Headers:    yhHL365Headers,
		CoverFetch: yhHL365CoverFetch,
	}, "yh_hl365", "hl365", "黑料不打烊")
}
