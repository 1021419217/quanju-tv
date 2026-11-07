package seku

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// AV大平台（9sex）— 配方出 ZakaTV jar Zaka9SexAmns 反编译：
// www.9sex.tv 主域（CF 盾后，抓取走 httpGet 的过盾/免代通道）、
// porndav.com 备用域同构 /tw 树；15 个固定分类即 jar 的 CATS 表
// （站方导航分区）；列表/详情 HTML 正则与 jar 同源。
//
// 边界（按用户账本类口径）：只取详情页对匿名访客直接给出的流
// （L1 前端直挖：JSON 键/source 标签/裸 m3u8·mp4）；jar 的 L3
// 「免费票复用」（把免费区学到的播放路由套到付费片）与 L5 交壳
// 解析不移植。付费片详情页只给预告时，解析结果即预告，如实如此。
//
// 2026-10-24 本机探活：www.9sex.tv 读超时（CF），porndav.com
// /tw/video/all 真回 146KB、21 个详情锚点，结构与 jar 正则吻合。

var nsDomains = []string{"https://www.9sex.tv", "https://9sex.tv", "https://porndav.com"}

const nsUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"

var nsState = struct {
	sync.Mutex
	host string
}{host: nsDomains[0]}

type nsCat struct{ id, name, path string }

var nsCats = []nsCat{
	{"all", "🎬 最新上架", "/tw/video/all"},
	{"exclusive", "🔥 大平台獨家", "/tw/video/exclusive"},
	{"jav", "🇯🇵 中文", "/tw/video/type/jav"},
	{"not-pixelated", "🚫 無碼", "/tw/video/type/not-pixelated"},
	{"europe-america", "🌍 歐美", "/tw/video/type/europe-america"},
	{"china", "🇨🇳 國產", "/tw/video/type/china"},
	{"free", "🎁 限時免費", "/tw/free"},
	{"cumshot", "💦 中出內射", "/tw/video/cumshot"},
	{"big-boobs", "🍈 巨乳", "/tw/video/big-boobs"},
	{"wife", "💍 人妻", "/tw/video/wife"},
	{"ntr", "💔 NTR", "/tw/video/ntr"},
	{"monomer-works", "🎞 單體作品", "/tw/video/monomer-works"},
	{"shorts", "📱 短影片", "/tw/shorts"},
	{"rank", "📊 排行榜", "/tw/rank"},
	{"daily", "📅 每日上新", "/tw/daily"},
}

func nsCatPath(id string) string {
	for _, c := range nsCats {
		if c.id == id {
			return c.path
		}
	}
	return "/tw/video/all"
}

var (
	nsReDetail  = regexp.MustCompile(`(?is)<a[^>]+href="([^"]*?/video/detail/(\d+)[^"]*)"[^>]*>([\s\S]*?)</a>`)
	nsReImg     = regexp.MustCompile(`(?i)(?:data-src|data-original|data-lazy-src|data-echo|src)="(https?://[^"]+?\.(?:jpe?g|png|webp|gif))"`)
	nsReTitleAt = regexp.MustCompile(`(?i)title="([^"]{2,160})"`)
	nsReH1      = regexp.MustCompile(`(?is)<h1[^>]*>([\s\S]*?)</h1>`)
	nsReOG      = regexp.MustCompile(`(?i)<meta[^>]+(?:property|name)="(og:title|og:image|og:description|description|keywords)"[^>]+content="([^"]*)"`)
	nsReDur     = regexp.MustCompile(`(?:duration|時長|时长|片長)[^0-9]{0,20}(\d{1,3}\s*[:：]\s*\d{2}(?:\s*[:：]\s*\d{2})?)`)
	nsReDate    = regexp.MustCompile(`(20\d{2}[-/.]\d{1,2}[-/.]\d{1,2})`)
	nsReActor   = regexp.MustCompile(`(?is)<a[^>]+href="([^"]*?/actors/(\d+))"[^>]*>([\s\S]{0,60}?)</a>`)
	nsReM3U8    = regexp.MustCompile(`https?://[^\s"'<>\\\)]+?\.m3u8[^\s"'<>\\\)]*`)
	nsReMP4     = regexp.MustCompile(`https?://[^\s"'<>\\\)]+?\.(?:mp4|m4v)[^\s"'<>\\\)]*`)
	nsReKeyJSON = regexp.MustCompile(`"(?:file|src|source|url|video|videoUrl|play_url|playUrl|m3u8|hls|hlsUrl|stream|streamUrl|path)"\s*:\s*"([^"]{12,600})"`)
	nsReSrcTag  = regexp.MustCompile(`(?i)<(?:source|video)[^>]+src="([^"]+)"`)
	nsReStrip   = regexp.MustCompile(`(?is)<(header|nav|footer)[^>]*>[\s\S]*?</(header|nav|footer)>`)
	nsReTags    = regexp.MustCompile(`(?is)<[^>]+>`)
)

func nsCleanText(s string) string {
	s = nsReTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// nsFetch GETs a path on the sticky host; on failure walks the domain
// pool. httpGet already carries the shell shield + jina fallback.
func nsFetch(path string) (string, string, error) {
	nsState.Lock()
	host := nsState.host
	nsState.Unlock()
	try := func(h string) (string, bool) {
		body, code, _, err := httpGet(h+path, map[string]string{
			"User-Agent": nsUA, "Referer": h + "/tw",
		})
		if err != nil || (code != 200 && code != 0) || len(body) == 0 {
			return "", false
		}
		s := string(body)
		if isChallengePage(s) {
			return "", false
		}
		return s, true
	}
	if s, ok := try(host); ok {
		return s, host, nil
	}
	for _, h := range nsDomains {
		if h == host {
			continue
		}
		if s, ok := try(h); ok {
			nsState.Lock()
			nsState.host = h
			nsState.Unlock()
			return s, h, nil
		}
	}
	return "", host, fmt.Errorf("9sex: all domains failed for %s", path)
}

func nsParseList(pageHTML string) []map[string]interface{} {
	body := nsReStrip.ReplaceAllString(pageHTML, " ")
	out := []map[string]interface{}{}
	seen := map[string]bool{}
	matches := nsReDetail.FindAllStringSubmatchIndex(body, -1)
	prevEnd := 0
	for _, m := range matches {
		id := body[m[4]:m[5]]
		if seen[id] {
			prevEnd = m[1]
			continue
		}
		seen[id] = true
		inner := ""
		if m[6] >= 0 {
			inner = body[m[6]:m[7]]
		}
		// 活页 title 属性是完整片名（含番号），内文混排日期/标签；
		// title 属性优先（jar 是内文优先，活页模板下属性更准）。
		title := ""
		if tm := nsReTitleAt.FindStringSubmatch(body[m[0]:m[1]]); tm != nil {
			title = html.UnescapeString(tm[1])
			// 活页 title 形如「片名 - AV大平台 - 中文字幕，…」
			if idx := strings.Index(title, " - AV大平台"); idx > 0 {
				title = title[:idx]
			}
			title = strings.TrimSpace(title)
		}
		if len([]rune(title)) < 2 {
			title = nsCleanText(inner)
		}
		if len([]rune(title)) < 2 {
			title = "影片 " + id
		}
		if r := []rune(title); len(r) > 60 {
			title = string(r[:60])
		}
		// 封面：锚内图优先，其次锚前最近图，再锚后 900 字内首图
		pic := ""
		if im := nsReImg.FindStringSubmatch(inner); im != nil {
			pic = im[1]
		}
		if pic == "" {
			lo := prevEnd
			if seg := body[lo:m[0]]; seg != "" {
				if ims := nsReImg.FindAllStringSubmatch(seg, -1); len(ims) > 0 {
					pic = ims[len(ims)-1][1]
				}
			}
		}
		if pic == "" {
			hi := m[1] + 900
			if hi > len(body) {
				hi = len(body)
			}
			if im := nsReImg.FindStringSubmatch(body[m[1]:hi]); im != nil {
				pic = im[1]
			}
		}
		remark := ""
		if dm := nsReDur.FindStringSubmatch(inner); dm != nil {
			remark = strings.ReplaceAll(dm[1], " ", "")
		} else if dm := nsReDate.FindStringSubmatch(inner); dm != nil {
			remark = dm[1]
		}
		out = append(out, makeDrama("avbigplat", id, title, pic, remark, "", ""))
		prevEnd = m[1]
		if len(out) >= 60 {
			break
		}
	}
	return out
}

func nsCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range nsCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func nsCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	path := nsCatPath(category)
	if page > 1 {
		path += "?page=" + fmt.Sprint(page)
	}
	body, _, err := nsFetch(path)
	if err != nil {
		return nil, 0, false, err
	}
	items := nsParseList(body)
	out := make([]interface{}, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out, 0, len(items) >= 20, nil
}

func nsOG(pageHTML, key string) string {
	for _, m := range nsReOG.FindAllStringSubmatch(pageHTML, -1) {
		if m[1] == key {
			return html.UnescapeString(m[2])
		}
	}
	return ""
}

func nsDetail(slot, rawID string) (map[string]interface{}, error) {
	body, _, err := nsFetch("/tw/video/detail/" + rawID)
	if err != nil {
		return nil, err
	}
	title := ""
	if m := nsReH1.FindStringSubmatch(body); m != nil {
		title = nsCleanText(m[1])
	}
	if title == "" {
		title = nsOG(body, "og:title")
		if idx := strings.Index(title, " - AV大平台"); idx > 0 {
			title = title[:idx]
		}
	}
	if title == "" {
		title = "影片 " + rawID
	}
	d := map[string]interface{}{"name": title}
	pic := nsOG(body, "og:image")
	if pic == "" {
		if im := nsReImg.FindStringSubmatch(body); im != nil {
			pic = im[1]
		}
	}
	if pic != "" {
		d["pic"] = pic
	}
	desc := nsOG(body, "og:description")
	if desc == "" {
		desc = nsOG(body, "description")
	}
	actors := []string{}
	for _, m := range nsReActor.FindAllStringSubmatch(body, -1) {
		if n := nsCleanText(m[3]); n != "" {
			actors = append(actors, n)
		}
	}
	if len(actors) > 0 {
		desc = strings.TrimSpace("演员: " + strings.Join(actors, "、") + "\n" + desc)
	}
	d["desc"] = desc
	if dm := nsReDate.FindStringSubmatch(body); dm != nil {
		d["year"] = dm[1][:4]
	}
	g5SetChapters(d, "avbigplat", rawID, []string{rawID}, []string{"正片"})
	// 相关推荐：同页其他详情卡
	rel := nsParseList(body)
	related := make([]interface{}, 0, len(rel))
	for _, it := range rel {
		if g5StrVal(it["id"]) == dramaID("avbigplat", rawID) {
			continue
		}
		related = append(related, it)
		if len(related) >= 12 {
			break
		}
	}
	if len(related) > 0 {
		d["related"] = related
	}
	return d, nil
}

func nsPlayable(u string) bool {
	u = strings.TrimSpace(u)
	if !strings.HasPrefix(u, "http") {
		return false
	}
	// 活页实证：详情页有「线路测速」JS 组件引用 speed_test/download_test.mp4，
	// 不是影片，必须排除（2026-10-24 详情页存档实测）。
	if strings.Contains(u, "speed_test") || strings.Contains(u, "download_test") {
		return false
	}
	return strings.Contains(u, ".m3u8") || strings.Contains(u, ".mp4") || strings.Contains(u, ".m4v")
}

// nsDigStream = jar 的 L1 前端直挖：JSON 流字段 → source/video 标签 →
// 裸 m3u8 → 裸 mp4。
func nsDigStream(pageHTML string) string {
	body := strings.ReplaceAll(pageHTML, `\/`, "/")
	body = strings.ReplaceAll(body, "&amp;", "&")
	for _, m := range nsReKeyJSON.FindAllStringSubmatch(body, -1) {
		if nsPlayable(m[1]) {
			return strings.TrimSpace(m[1])
		}
	}
	for _, m := range nsReSrcTag.FindAllStringSubmatch(body, -1) {
		if nsPlayable(m[1]) {
			return strings.TrimSpace(m[1])
		}
	}
	for _, m := range nsReM3U8.FindAllString(body, -1) {
		if nsPlayable(m) {
			return m
		}
	}
	for _, m := range nsReMP4.FindAllString(body, -1) {
		if nsPlayable(m) {
			return m
		}
	}
	return ""
}

func nsResolve(slot, rawID string) (map[string]interface{}, error) {
	id := rawID
	if idx := strings.Index(id, "||"); idx >= 0 {
		id = id[:idx]
	}
	id = strings.TrimPrefix(id, "detail@@")
	if nsPlayable(id) {
		return playResult(id, "avbigplat", rawID, nsUA, "", "", ""), nil
	}
	body, host, err := nsFetch("/tw/video/detail/" + id)
	if err != nil {
		return nil, err
	}
	u := nsDigStream(body)
	if u == "" {
		return nil, fmt.Errorf("9sex: no anonymous stream on detail %s", id)
	}
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	} else if strings.HasPrefix(u, "/") {
		u = host + u
	}
	format := "mp4"
	if strings.Contains(u, ".m3u8") {
		format = "hls"
	}
	return playResult(u, "avbigplat", rawID, nsUA, host+"/tw", "", format), nil
}

func nsSearch(slot, kw string) ([]interface{}, error) {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return []interface{}{}, nil
	}
	body, _, err := nsFetch("/tw/search?q=" + url.QueryEscape(kw))
	if err != nil {
		return nil, err
	}
	items := nsParseList(body)
	out := make([]interface{}, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out, nil
}

func nsHeaders() map[string]string {
	return map[string]string{"User-Agent": nsUA}
}

func init() {
	registerSite("avbigplat", &siteImpl{
		Key: "avbigplat", Name: "AV大平台", DefaultCat: "all", HomeURL: nsDomains[0] + "/tw",
		Categories: nsCategories,
		Catalog:    nsCatalog,
		Detail:     nsDetail,
		Resolve:    nsResolve,
		Search:     nsSearch,
		Headers:    nsHeaders,
	}, "avbigplat")
}
