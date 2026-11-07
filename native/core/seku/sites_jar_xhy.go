package seku

// 小黄鸭 xhy11 (ZakaXhyAmns) — MacCMS site behind a whole-page shell:
// every HTML response is
//   <script>...document.write(decodeURIComponent(atob("...")))</script>
// where the atob payload is base64 of the percent-encoded real HTML
// (verified live 2026-10-24 on type/detail pages). Unwrap once, then
// parse the plain MacCMS (dhseo template) HTML:
//   list:  <li><a href="/index.php/vod/detail/id/{id}.html">
//          <img data-original="{cover}"> <p class="txt-ov">{title}</p>
//   detail pages link play pages /index.php/vod/play/id/{id}/sid/{s}/nid/{n}.html
//   play pages carry MacCMS player_aaaa JSON and/or an atob("...")
//   blob whose decoding contains the stream URL.
// Categories 44-67 are the site's own nav (names read from the live
// home page nav, 2026-10-24).

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const xhyBase = "https://www.xhy11.cc"
const xhyUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func xhyHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      xhyUA,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Referer":         xhyBase + "/",
	}
}

var xhyShellRe = regexp.MustCompile(`atob\(\s*"([A-Za-z0-9+/=]{200,})"\s*\)`)

// xhyFetch GETs a page and unwraps the document.write shell when present.
func xhyFetch(u string) (string, error) {
	body, code, _, err := httpGet(u, xhyHeaders())
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("xhy http %d for %s", code, u)
	}
	html := string(body)
	for _, m := range xhyShellRe.FindAllStringSubmatch(html, -1) {
		raw, derr := base64.StdEncoding.DecodeString(m[1])
		if derr == nil {
			if un, uerr := url.QueryUnescape(string(raw)); uerr == nil && strings.Contains(un, "<") {
				return un, nil
			}
		}
	}
	return html, nil
}

var xhyCats = []htmlCat{
	{ID: "44", Name: "三区"},
	{ID: "45", Name: "自拍偷拍"},
	{ID: "46", Name: "伦理禁忌"},
	{ID: "47", Name: "网爆黑料"},
	{ID: "48", Name: "绿帽淫妻"},
	{ID: "49", Name: "制服诱惑"},
	{ID: "50", Name: "国产制作"},
	{ID: "51", Name: "国产直播"},
	{ID: "52", Name: "韩国直播"},
	{ID: "53", Name: "会所技师"},
	{ID: "54", Name: "淫乱学生"},
	{ID: "55", Name: "主播网红"},
	{ID: "56", Name: "国产推荐"},
	{ID: "57", Name: "探花大神"},
	{ID: "58", Name: "中文字幕"},
	{ID: "59", Name: "高清无码"},
	{ID: "60", Name: "高清有码"},
	{ID: "61", Name: "日本素人"},
	{ID: "62", Name: "无码流出"},
	{ID: "63", Name: "FC2"},
	{ID: "64", Name: "东京热"},
	{ID: "65", Name: "一本道"},
	{ID: "66", Name: "欧美精品"},
	{ID: "67", Name: "重口猎奇"},
}

func xhyCatName(id string) string {
	for _, c := range xhyCats {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

var xhyItemRe = regexp.MustCompile(`(?s)<li>\s*<a href="/index\.php/vod/detail/id/(?P<id>\d+)\.html">\s*(?:<span>\s*)?<img[^>]*?data-original="(?P<cover>[^"]+)"[^>]*>\s*(?:</span>\s*)?(?:<h2>\s*)?<p class="txt-ov">(?P<title>.*?)</p>`)
var xhyItemSrcRe = regexp.MustCompile(`(?s)<li>\s*<a href="/index\.php/vod/detail/id/(?P<id>\d+)\.html">\s*(?:<span>\s*)?<img[^>]*?src="(?P<cover>[^"]+)"[^>]*>\s*(?:</span>\s*)?(?:<h2>\s*)?<p class="txt-ov">(?P<title>.*?)</p>`)
var xhyPagesRe = regexp.MustCompile(`/page/(\d+)\.html"[^>]*>\s*尾页`)

func xhyParseItems(slot, html, catName string) []interface{} {
	items := xhyParseItemsWith(slot, html, catName, xhyItemRe)
	if len(items) == 0 {
		items = xhyParseItemsWith(slot, html, catName, xhyItemSrcRe)
	}
	return items
}

func xhyParseItemsWith(slot, html, catName string, re *regexp.Regexp) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		g := namedSubexp(re, m)
		id := g["id"]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		title := cleanText(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(g["title"], ""))
		items = append(items, makeDrama(slot, id, title, strings.TrimSpace(g["cover"]), "", catName, ""))
	}
	return items
}

var (
	xhyDetailTitleRe = regexp.MustCompile(`(?s)class="content-info[^"]*"[^>]*>(.*?)</div>`)
	xhyH2Re          = regexp.MustCompile(`(?s)<h2[^>]*>(.*?)</h2>`)
	xhyDetailPicRe   = regexp.MustCompile(`(?s)class="detail-pic[^"]*"[^>]*>\s*<img[^>]*?src="([^"]+)"`)
	xhyPlayLinkRe    = regexp.MustCompile(`/index\.php/vod/play/id/(\d+)/sid/(\d+)/nid/(\d+)\.html`)
	xhyStreamRe      = regexp.MustCompile(`https?://[^"'\s<>\\]+\.(?:m3u8|mp4)[^"'\s<>\\]*`)
	xhyPlayerRe      = regexp.MustCompile(`(?s)player_aaaa\s*=\s*(\{.*?\})\s*</script>`)
	xhyPlayerURLRe   = regexp.MustCompile(`"url"\s*:\s*"([^"]+)"`)
	xhyAtobRe        = regexp.MustCompile(`atob\(\s*"([A-Za-z0-9+/=]{40,})"\s*\)`)
)

// xhyPlayURLFromPage extracts a stream URL from a play page's HTML:
// direct stream URL first, then atob blobs, then player_aaaa url.
func xhyStreamFromHTML(html string) string {
	if m := xhyStreamRe.FindString(html); m != "" {
		return strings.ReplaceAll(m, `\/`, "/")
	}
	for _, m := range xhyAtobRe.FindAllStringSubmatch(html, -1) {
		raw, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			continue
		}
		if s := xhyStreamRe.FindString(string(raw)); s != "" {
			return strings.ReplaceAll(s, `\/`, "/")
		}
		if un, uerr := url.QueryUnescape(string(raw)); uerr == nil {
			if s := xhyStreamRe.FindString(un); s != "" {
				return s
			}
		}
	}
	if m := xhyPlayerRe.FindStringSubmatch(html); m != nil {
		if um := xhyPlayerURLRe.FindStringSubmatch(m[1]); um != nil {
			u := strings.ReplaceAll(um[1], `\/`, "/")
			if un, err := url.QueryUnescape(u); err == nil {
				u = un
			}
			if strings.HasPrefix(u, "http") {
				return u
			}
			if raw, err := base64.StdEncoding.DecodeString(u); err == nil {
				if s := xhyStreamRe.FindString(string(raw)); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func xhyDetail(slot, rawID string) (map[string]interface{}, error) {
	id := strings.SplitN(rawID, "#", 2)[0]
	html, err := xhyFetch(xhyBase + "/index.php/vod/detail/id/" + id + ".html")
	if err != nil {
		return nil, err
	}
	title := ""
	if m := xhyDetailTitleRe.FindStringSubmatch(html); m != nil {
		if hm := xhyH2Re.FindStringSubmatch(m[1]); hm != nil {
			title = cleanText(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(hm[1], ""))
		}
	}
	if title == "" {
		if m := regexp.MustCompile(`<title>([^<]{1,200})</title>`).FindStringSubmatch(html); m != nil {
			t := cleanText(m[1])
			t = strings.TrimSuffix(t, " 小黄鸭")
			t = strings.TrimSuffix(t, "-小黄鸭")
			if t != "" && t != "小黄鸭" {
				title = t
			}
		}
	}
	cover := ""
	if m := xhyDetailPicRe.FindStringSubmatch(html); m != nil {
		cover = m[1]
	}
	// Play links -> chapters (rawID carries id#sid#nid per episode).
	type ep struct{ sid, nid string }
	eps := []ep{}
	seenEp := map[string]bool{}
	for _, m := range xhyPlayLinkRe.FindAllStringSubmatch(html, -1) {
		if m[1] != id {
			continue
		}
		k := m[2] + "/" + m[3]
		if seenEp[k] {
			continue
		}
		seenEp[k] = true
		eps = append(eps, ep{m[2], m[3]})
	}
	d := makeDrama(slot, id, title, cover, "", "", "")
	if len(eps) > 0 {
		chs := []interface{}{}
		for i, e := range eps {
			epID := id + "#" + e.sid + "#" + e.nid
			name := "正片"
			if len(eps) > 1 {
				name = "第" + strconv.Itoa(i+1) + "集"
			}
			chs = append(chs, map[string]interface{}{
				"episodeId": epID, "id": dramaID(slot, epID), "dramaId": dramaID(slot, id),
				"sourceId": slot, "source": slot, "title": name, "name": name,
				"index": i + 1, "episode": i + 1, "url": "", "videoUrl": "", "mediaSize": 0,
			})
		}
		d["chapters"] = chs
		d["episodeList"] = chs
		d["episodes"] = len(eps)
		d["episodeCount"] = len(eps)
		d["chapterCount"] = len(eps)
		d["totalEpisode"] = len(eps)
		d["totalEpisodes"] = len(eps)
	}
	if title == "" {
		if st := getStore(slot).findDrama(id); st != nil {
			if t, _ := st["title"].(string); t != "" {
				d["title"] = t
				d["name"] = t
			}
		}
	}
	return d, nil
}

func xhyResolve(slot, rawID string) (map[string]interface{}, error) {
	parts := strings.Split(rawID, "#")
	id := parts[0]
	sid, nid := "1", "1"
	if len(parts) >= 3 {
		sid, nid = parts[1], parts[2]
	}
	playURL := xhyBase + "/index.php/vod/play/id/" + id + "/sid/" + sid + "/nid/" + nid + ".html"
	html, err := xhyFetch(playURL)
	if err != nil {
		return nil, err
	}
	u := xhyStreamFromHTML(html)
	if u == "" {
		return nil, fmt.Errorf("xhy: no stream on play page %s", playURL)
	}
	format, proto := "mp4", "file"
	if strings.Contains(u, ".m3u8") {
		format, proto = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, xhyUA, xhyBase+"/", format, proto), nil
}

func init() {
	impl := &siteImpl{
		Key: "xhy11", Name: "小黄鸭", DefaultCat: "56", HomeURL: xhyBase + "/",
		Headers: xhyHeaders,
	}
	impl.Categories = func(slot string) []interface{} {
		out := []interface{}{}
		for _, c := range xhyCats {
			out = append(out, categoryObj(c.ID, c.Name))
		}
		return out
	}
	impl.Catalog = func(slot, category string, page int) ([]interface{}, int, bool, error) {
		if category == "" {
			category = "56"
		}
		u := xhyBase + "/index.php/vod/type/id/" + category + ".html"
		if page > 1 {
			u = xhyBase + "/index.php/vod/type/id/" + category + "/page/" + strconv.Itoa(page) + ".html"
		}
		html, err := xhyFetch(u)
		if err != nil {
			return nil, 0, false, err
		}
		items := xhyParseItems(slot, html, xhyCatName(category))
		if len(items) == 0 && page == 1 {
			if strings.Contains(html, "/vod/detail/") {
				return nil, 0, false, fmt.Errorf("xhy: list template changed for category %s (detail anchors present but no cards parsed)", category)
			}
			return nil, 0, false, fmt.Errorf("xhy: empty list on page 1 for category %s", category)
		}
		totalPages := page
		hasMore := len(items) > 0
		if m := xhyPagesRe.FindStringSubmatch(html); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				totalPages = n
				hasMore = page < n
			}
		}
		return items, totalPages * 24, hasMore, nil
	}
	impl.Detail = xhyDetail
	impl.Resolve = xhyResolve
	impl.Search = func(slot, kw string) ([]interface{}, error) {
		u := xhyBase + "/index.php/vod/search/wd/" + url.PathEscape(kw) + ".html"
		html, err := xhyFetch(u)
		if err != nil {
			return []interface{}{}, err
		}
		items := xhyParseItems(slot, html, "搜索")
		if len(items) == 0 && strings.Contains(html, "/vod/detail/") {
			return nil, fmt.Errorf("xhy: search template changed (detail anchors present but no cards parsed)")
		}
		return items, nil
	}
	registerSite("xhy11", impl, "xhy11", "小黄鸭")
}
