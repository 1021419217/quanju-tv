package seku

// ---- JavHer (javher.com) — guanguo, v6 ----
//
// Ported from the user's ZakaTV jar class ZakaJavherAmns (user order
// 2026-10-23: swap guanguo from 91n to the jar's JavHer; the 91n
// implementation stays in sites_g4.go, unregistered). The jar's
// ZakaGate/ZakaBrand layers are not ported (established practice).
//
// Recipe (jar-verbatim unless noted):
// - HOST https://javher.com, UA Chrome/131 desktop. Categories are
// the jar CATS table 1:1 (53: 2 listing + 34 genre + 17 actress).
// - List pages split on <div class="col-4 col-md-3 col-lg-2">;
// per chunk: watch link, <p class="card-text title mb-0"> name,
// duration badge, date; cover is synthesized from the content id
// as pics.dmm.co.jp/digital/video/<cid>/<cid>ps.jpg (detail:
// pl.jpg) — exactly the jar's listPic/bigPic.
// - Detail goes through GET /api/video/<slug>, which answers ONLY
// with the jar's exact header set (incl. the hardcoded
// Authorization token the site itself ships); failure falls back
// to the watch page's H1/<title> + "trailer":"..." regex, as in
// the jar.
//
// PLAYBACK VERDICT (full investigation in v6work/
// javher_playback_investigation.md): the jar itself has only two
// lines — the DMM freepv official trailer (parse=0) and the watch
// page (parse=1 shell sniffing, which this app does not have).
// A 13-sample API audit proved there is NO usable full-length
// source: openload/streamcherry/jcdn arrays are always empty,
// javLink resolves to a FANZA affiliate link, and avgleSource is
// systematically mismatched (13/13 carry another film's code; two
// different films even share one avgle video). So this provider
// ships the honest trailer-only form: a single episode named
// 官方预告片. Between the two trailer fields, the core prefers
// trailerG (media.javtrailers.com — live-verified playable from
// this egress: master/sub-playlist/first segment 200, TS sync,
// ~97.5s for a 132min film) and falls back to the jar's
// realPlay(trailer) (pv3001 domain swap; DMM domains 403 from this
// egress, user-network dependent). Detail text labels the film's
// own duration as 片长（正片）so it is never confused with the
// preview's length.

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const javherHost = "https://javher.com"
const javherUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
const javherAPIAuth = "HAHA_ADAM_HAVE_TO_RESORT_TO_THIS#@!@#"

var javherCats = []struct{ id, name string }{
	{"n|", "🔥 最新上架"},
	{"u|", "🆕 即将发售"},
	{"c|hi-def", "高清"},
	{"c|4k", "4K"},
	{"c|exclusive-distribution", "独家配信"},
	{"c|featured-actress", "单体作品"},
	{"c|amateur", "素人"},
	{"c|big-tits-lover", "巨乳"},
	{"c|big-asses", "美臀"},
	{"c|beautiful-legs", "美腿"},
	{"c|lesbian", "女同"},
	{"c|bdsm", "BDSM"},
	{"c|creampie", "中出"},
	{"c|blowjob", "口交"},
	{"c|facial", "颜射"},
	{"c|threesome-foursome", "3P/4P"},
	{"c|squirting", "潮吹"},
	{"c|cosplay", "角色扮演"},
	{"c|massage", "按摩"},
	{"c|pov", "POV"},
	{"c|school-uniform", "制服"},
	{"c|sailor-uniform", "水手服"},
	{"c|kimono", "和服"},
	{"c|lingerie", "内衣"},
	{"c|pantyhose", "丝袜"},
	{"c|miniskirt", "迷你裙"},
	{"c|swimsuits", "泳装"},
	{"c|maid", "女仆"},
	{"c|nurse", "护士"},
	{"c|office-lady", "OL"},
	{"c|female-teacher", "女教师"},
	{"c|housewife", "主妇"},
	{"c|mature-woman", "熟女"},
	{"c|cheating-wife", "出轨妻"},
	{"c|married-woman", "人妻"},
	{"c|idol-celebrity", "偶像艺人"},
	{"a|saika-kawakita", "👩 河北彩花"},
	{"a|seto-kanna", "👩 瀬戸環奈"},
	{"a|ichika-matsumoto", "👩 松本いちか"},
	{"a|yui-hatano", "👩 波多野結衣"},
	{"a|kibou-miu", "👩 希望みう"},
	{"a|hanamori-kaho", "👩 花守夏歩"},
	{"a|umpai", "👩 うんぱい"},
	{"a|amamiya-hanan", "👩 天宮花南"},
	{"a|miu-arioka", "👩 有岡みう"},
	{"a|waka-misono", "👩 美園和花"},
	{"a|akari-niimura", "👩 新村あかり"},
	{"a|luna-tsukino", "👩 月乃ルナ"},
	{"a|riho-shishido", "👩 宍戸里帆"},
	{"a|arisu-nanase", "👩 七瀬アリス"},
	{"a|aika-yamagishi", "👩 山岸逢花"},
	{"a|yumi-kazama", "👩 風間ゆみ"},
	{"a|hana-haruna", "👩 春菜はな"},
}

func javherCatKnown(id string) bool {
	for _, c := range javherCats {
		if c.id == id {
			return true
		}
	}
	return false
}

func javherCatName(id string) string {
	for _, c := range javherCats {
		if c.id == id {
			return strings.TrimPrefix(strings.TrimPrefix(c.name, "🔥 "), "🆕 ")
		}
	}
	return "JavHer"
}

func javherCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range javherCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func javherCatURL(tid string, page int) string {
	if page < 1 {
		page = 1
	}
	kind, rest := "n", ""
	if i := strings.Index(tid, "|"); i >= 0 {
		kind, rest = tid[:i], tid[i+1:]
	}
	switch kind {
	case "c":
		return fmt.Sprintf("%s/categories/%s?page=%d", javherHost, rest, page)
	case "a":
		return fmt.Sprintf("%s/casts/%s?page=%d", javherHost, rest, page)
	case "u":
		if page > 1 {
			return fmt.Sprintf("%s/videos/upcoming?page=%d", javherHost, page)
		}
		return javherHost + "/videos/upcoming"
	default:
		return fmt.Sprintf("%s/videos?page=%d", javherHost, page)
	}
}

// ---- fetch layer (jar page(): direct, shield fallback via httpDo's
// jina route; short page cache like the jar's pageCache) ----

var javherState = &struct {
	mu    sync.Mutex
	pages map[string]whosPageEntry
	api   map[string]map[string]interface{}
}{pages: map[string]whosPageEntry{}, api: map[string]map[string]interface{}{}}

func javherHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      javherUA,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9,zh;q=0.8,ja;q=0.7",
		"Referer":         javherHost + "/",
	}
}

func javherFetch(rawURL string) (string, error) {
	javherState.mu.Lock()
	if e, ok := javherState.pages[rawURL]; ok && time.Since(e.at) < 120*time.Second {
		t := e.text
		javherState.mu.Unlock()
		return t, nil
	}
	javherState.mu.Unlock()
	b, code, _, err := httpDo("GET", rawURL, nil, javherHeaders())
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("javher fetch %s -> %d", rawURL, code)
	}
	text := string(b)
	if len(text) <= 800 {
		return "", fmt.Errorf("javher fetch %s: suspiciously short page", rawURL)
	}
	javherState.mu.Lock()
	if len(javherState.pages) > 60 {
		javherState.pages = map[string]whosPageEntry{}
	}
	javherState.pages[rawURL] = whosPageEntry{at: time.Now(), text: text}
	javherState.mu.Unlock()
	return text, nil
}

// javherAPIVideo mirrors the jar apiVideo (exact header set or the
// API answers a Nuxt 404 page).
func javherAPIVideo(slug string) map[string]interface{} {
	if len(slug) < 4 {
		return nil
	}
	javherState.mu.Lock()
	if v, ok := javherState.api[slug]; ok {
		javherState.mu.Unlock()
		return v
	}
	javherState.mu.Unlock()
	hdr := map[string]string{
		"User-Agent":      javherUA,
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "en-US,en;q=0.9",
		"Authorization":   javherAPIAuth,
		"Referer":         javherHost + "/video/" + slug,
	}
	b, code, _, err := httpDo("GET", javherHost+"/api/video/"+slug, nil, hdr)
	if err != nil || code != 200 {
		return nil
	}
	j := oneDecodeResp(b) // plain-JSON first-value decode is all we need
	if j == nil {
		return nil
	}
	if ok, _ := j["success"].(bool); !ok {
		return nil
	}
	video, _ := j["video"].(map[string]interface{})
	if video == nil {
		return nil
	}
	javherState.mu.Lock()
	if len(javherState.api) > 40 {
		javherState.api = map[string]map[string]interface{}{}
	}
	javherState.api[slug] = video
	javherState.mu.Unlock()
	return video
}

// ---- parsing (jar regexes, verbatim) ----

var (
	javherSplitRe   = regexp.MustCompile(`<div class="col-4 col-md-3 col-lg-2">`)
	javherLinkRe    = regexp.MustCompile(`href="(/video/watch-[^"]+)"`)
	javherNameRe    = regexp.MustCompile(`(?s)<p class="card-text title mb-0"[^>]*>(.*?)</p>`)
	javherAttrRe    = regexp.MustCompile(`title="([^"]{2,300})"`)
	javherDurRe     = regexp.MustCompile(`(?s)<span class="badge duration-badge[^"]*"[^>]*>(.*?)</span>`)
	javherDateRe    = regexp.MustCompile(`(?s)<small class="card-text title text-muted"[^>]*>(.*?)</small>`)
	javherH1Re      = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	javherTitleRe   = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	javherTrailerRe = regexp.MustCompile(`"trailer":"([^"]{10,300})"`)
	javherCIDRe     = regexp.MustCompile(`/video/watch-([a-z0-9_]+)-\d+`)
	javherTagRe     = regexp.MustCompile(`<[^>]+>`)
)

func javherClean(s string) string {
	s = javherTagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(html.UnescapeString(s))
}

func javherCIDOf(watchPath string) string {
	if m := javherCIDRe.FindStringSubmatch(watchPath); m != nil {
		return m[1]
	}
	i := strings.LastIndex(watchPath, "watch-")
	if i < 0 {
		return ""
	}
	rest := watchPath[i+len("watch-"):]
	if j := strings.LastIndex(rest, "-"); j > 0 {
		rest = rest[:j]
	}
	return rest
}

func javherListPic(cid string) string {
	if len(cid) < 3 {
		return ""
	}
	return "https://pics.dmm.co.jp/digital/video/" + cid + "/" + cid + "ps.jpg"
}

func javherBigPic(cid string) string {
	if len(cid) < 3 {
		return ""
	}
	return "https://pics.dmm.co.jp/digital/video/" + cid + "/" + cid + "pl.jpg"
}

type javherCard struct {
	url, title, cover, remark string
}

func javherParseList(page string) []javherCard {
	out := []javherCard{}
	if len(page) < 500 {
		return out
	}
	parts := javherSplitRe.Split(page, -1)
	seen := map[string]bool{}
	for _, chunk := range parts[1:] {
		if len(chunk) > 3000 {
			chunk = chunk[:3000]
		}
		m := javherLinkRe.FindStringSubmatch(chunk)
		if m == nil {
			continue
		}
		path := strings.TrimSpace(m[1])
		if len(path) < 12 || seen[path] {
			continue
		}
		seen[path] = true
		title := ""
		if nm := javherNameRe.FindStringSubmatch(chunk); nm != nil {
			title = javherClean(nm[1])
		}
		if title == "" {
			if am := javherAttrRe.FindStringSubmatch(chunk); am != nil {
				title = javherClean(am[1])
			}
		}
		cid := javherCIDOf(path)
		if title == "" {
			title = cid
		}
		dur, date := "", ""
		if dm := javherDurRe.FindStringSubmatch(chunk); dm != nil {
			dur = javherClean(dm[1])
		}
		if dm := javherDateRe.FindStringSubmatch(chunk); dm != nil {
			date = javherClean(dm[1])
		}
		remark := dur
		if date != "" {
			if remark != "" {
				remark += " · "
			}
			remark += date
		}
		out = append(out, javherCard{
			url: javherHost + path, title: title,
			cover: javherListPic(cid), remark: remark,
		})
	}
	return out
}

func javherCardsToItems(slot string, cards []javherCard, catName string) []interface{} {
	items := []interface{}{}
	for _, c := range cards {
		items = append(items, makeDrama(slot, c.url, c.title, c.cover, c.remark, catName, ""))
	}
	return items
}

// ---- catalog / search ----

func javherCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	tid := category
	if !javherCatKnown(tid) {
		tid = "n|"
	}
	if page < 1 {
		page = 1
	}
	text, err := javherFetch(javherCatURL(tid, page))
	if err != nil {
		return []interface{}{}, 0, false, err
	}
	cards := javherParseList(text)
	return javherCardsToItems(slot, cards, javherCatName(tid)), len(cards), len(cards) > 0, nil
}

func javherSearch(slot, kw string) ([]interface{}, error) {
	text, err := javherFetch(javherHost + "/search/" + url.PathEscape(kw))
	if err != nil {
		return []interface{}{}, err
	}
	return javherCardsToItems(slot, javherParseList(text), "搜索"), nil
}

// ---- detail / resolve ----

// javherRealPlay ports the jar realPlay domain normalization.
func javherRealPlay(u string) string {
	u = strings.TrimSpace(u)
	u = strings.ReplaceAll(u, `\/`, "/")
	u = strings.ReplaceAll(u, `\u002F`, "/")
	if len(u) < 12 {
		return ""
	}
	if strings.Contains(u, "mgstage") {
		if !strings.HasPrefix(u, "http") {
			return "https://mgstage.javher.com/" + strings.TrimPrefix(u, "/")
		}
		return u
	}
	u = strings.Replace(u, "awscc3001.r18.com", "pv3001.dmm.co.jp", 1)
	u = strings.Replace(u, "cc3001.dmm.co.jp", "pv3001.dmm.co.jp", 1)
	return u
}

// javherPreviewURL picks the playable official-preview URL:
// trailerG (javtrailers mirror, verified playable) first, then the
// jar's realPlay(trailer).
func javherPreviewURL(video map[string]interface{}, pageHTML string) string {
	if video != nil {
		if tg := g5MapStr(video, "trailerG"); tg != "" {
			if u := javherRealPlay(tg); u != "" {
				return u
			}
		}
		if tr := g5MapStr(video, "trailer"); tr != "" {
			if u := javherRealPlay(tr); u != "" {
				return u
			}
		}
	}
	if pageHTML != "" {
		if m := javherTrailerRe.FindStringSubmatch(pageHTML); m != nil {
			return javherRealPlay(m[1])
		}
	}
	return ""
}

func javherDetail(slot, rawID string) (map[string]interface{}, error) {
	watchURL := strings.TrimSpace(rawID)
	if !strings.HasPrefix(watchURL, "http") {
		watchURL = javherHost + "/video/" + strings.TrimPrefix(watchURL, "/")
	}
	slug := watchURL[strings.LastIndex(watchURL, "/")+1:]
	video := javherAPIVideo(slug)

	title, pic, remark, desc := "", "", "", ""
	if video != nil {
		dvdID := g5MapStr(video, "dvdId")
		t := g5MapStr(video, "title")
		if dvdID != "" {
			title = dvdID + " " + t
		} else {
			title = t
		}
		if strings.TrimSpace(title) == "" {
			title = g5MapStr(video, "contentId")
		}
		pic = g5MapStr(video, "image")
		if len(pic) < 12 {
			cid := g5MapStr(video, "contentId")
			if cid == "" {
				cid = javherCIDOf(watchURL)
			}
			pic = javherBigPic(cid)
		}
		rel := g5MapStr(video, "releaseDate")
		if len(rel) > 10 {
			rel = rel[:10]
		}
		remark = rel
		var b strings.Builder
		b.WriteString("")
		idForDesc := dvdID
		if idForDesc == "" {
			idForDesc = g5MapStr(video, "contentId")
		}
		b.WriteString(idForDesc + "\n")
		if rel != "" {
			b.WriteString("發售：" + rel + "\n")
		}
		if st, _ := video["studio"].(map[string]interface{}); st != nil {
			if n := g5MapStr(st, "name"); n != "" {
				b.WriteString("片商：" + n + "\n")
			}
		}
		if dur, ok := g5MapInt(video, "duration"); ok && dur > 0 {
			b.WriteString(fmt.Sprintf("片长（正片）：%d 分\n", dur))
		}
		if v := g5MapStr(video, "jpTitle"); v != "" {
			b.WriteString("日文標題：" + v + "\n")
		}
		if v := g5MapStr(video, "zhTitle"); v != "" {
			b.WriteString("中文標題：" + v + "\n")
		}
		casts := []string{}
		if arr, _ := video["casts"].([]interface{}); arr != nil {
			for i, e := range arr {
				if i >= 8 {
					break
				}
				cm, _ := e.(map[string]interface{})
				if cm == nil {
					continue
				}
				jp := g5MapStr(cm, "jpName")
				nm := g5MapStr(cm, "name")
				label := jp
				if jp != "" && nm != "" {
					label = jp + "(" + nm + ")"
				} else if label == "" {
					label = nm
				}
				if label != "" {
					casts = append(casts, label)
				}
			}
		}
		if len(casts) > 0 {
			b.WriteString("女優：" + strings.Join(casts, " / ") + "\n")
		}
		cats := []string{}
		if arr, _ := video["categories"].([]interface{}); arr != nil {
			for i, e := range arr {
				if i >= 12 {
					break
				}
				cm, _ := e.(map[string]interface{})
				if cm == nil {
					continue
				}
				n := g5MapStr(cm, "zhName")
				if n == "" {
					n = g5MapStr(cm, "name")
				}
				if n != "" {
					cats = append(cats, n)
				}
			}
		}
		if len(cats) > 0 {
			b.WriteString("分類：" + strings.Join(cats, " / ") + "\n")
		}
		b.WriteString("來源：" + watchURL)
		desc = b.String()
	} else {
		page, err := javherFetch(watchURL)
		if err != nil {
			return nil, err
		}
		if m := javherH1Re.FindStringSubmatch(page); m != nil {
			title = javherClean(m[1])
		}
		if title == "" {
			if m := javherTitleRe.FindStringSubmatch(page); m != nil {
				t := javherClean(m[1])
				if i := strings.LastIndex(t, " Jav Online"); i > 0 {
					t = t[:i]
				}
				title = strings.TrimSpace(t)
			}
		}
		cid := javherCIDOf(watchURL)
		pic = javherBigPic(cid)
		desc = "" + cid + "\n來源：" + watchURL
		video = nil
		// trailer comes from the page payload in the fallback path
		preview := javherPreviewURL(nil, page)
		return javherFinishDetail(slot, rawID, watchURL, title, pic, remark, desc, preview), nil
	}
	if title == "" {
		title = slug
	}
	preview := javherPreviewURL(video, "")
	return javherFinishDetail(slot, rawID, watchURL, title, pic, remark, desc, preview), nil
}

func javherFinishDetail(slot, rawID, watchURL, title, pic, remark, desc, preview string) map[string]interface{} {
	if remark == "" {
		remark = "官方预告片"
	} else {
		remark += " · 官方预告片"
	}
	desc += "\n\n播放说明：本站在本应用内仅提供官方预告片（约 1–2 分钟）；全长播放依赖站内嵌入源，其数据经核验与本片不符，故不提供（详见交付说明）。"
	d := makeDrama(slot, rawID, title, pic, remark, "JavHer", desc)
	if preview != "" {
		chs := []interface{}{g2Chapter(slot, rawID, "o|"+preview, "官方预告片", 1)}
		g2SetChapters(d, chs)
	} else {
		d["chapters"] = []interface{}{}
		d["episodeList"] = []interface{}{}
		d["episodes"] = 0
		d["episodeCount"] = 0
		d["chapterCount"] = 0
		d["totalEpisode"] = 0
		d["totalEpisodes"] = 0
	}
	return d
}

func javherResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "o|") {
		u := strings.TrimPrefix(rawID, "o|")
		format, proto := "mp4", "file"
		if strings.Contains(u, ".m3u8") {
			format, proto = "m3u8", "hls"
		}
		return playResult(u, slot, rawID, javherUA, javherHost+"/", format, proto), nil
	}
	if strings.HasPrefix(rawID, "p|") {
		return nil, fmt.Errorf("javher: 原页面线路需壳嗅探，本应用无此通道")
	}
	// A bare watch URL / slug: resolve through detail's preview line.
	d, err := javherDetail(slot, rawID)
	if err != nil {
		return nil, err
	}
	if chs, _ := d["chapters"].([]interface{}); len(chs) > 0 {
		if ch, _ := chs[0].(map[string]interface{}); ch != nil {
			if ep, _ := ch["episodeId"].(string); strings.HasPrefix(ep, "o|") {
				return javherResolve(slot, ep)
			}
		}
	}
	return nil, fmt.Errorf("javher: no preview stream")
}

func init() {
	registerSite("guanguo", &siteImpl{
		Key: "javher", Name: "JavHer", DefaultCat: "n|", HomeURL: javherHost + "/",
		Categories: javherCategories,
		Catalog:    javherCatalog,
		Detail:     javherDetail,
		Resolve:    javherResolve,
		Search:     javherSearch,
		Headers:    javherHeaders,
	}, "javher")
}
