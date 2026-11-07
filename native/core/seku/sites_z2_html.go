package seku

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---- Batch2 (ZakaSec-restored) HTML-profile sites ----
// Recipes restored from jar classes that hid their hosts/paths behind
// ZakaSec.s() indices; hosts verified live 2026-10-24 (see batch report).

const z2UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// pagedCatalog overrides an htmlProfileImpl catalog with a custom
// page-URL builder (sites whose page-1 URL differs from page-N).
func z2PagedCatalog(p *htmlProfile, key string, pageURL func(cat string, page int) string) func(slot, category string, page int) ([]interface{}, int, bool, error) {
	return func(slot, category string, page int) ([]interface{}, int, bool, error) {
		if category == "" && len(p.Cats) > 0 {
			category = p.Cats[0].ID
		}
		u := pageURL(category, page)
		body, code, _, err := httpGet(u, p.headers())
		if err != nil || code != 200 {
			return nil, 0, false, fmt.Errorf("%s catalog http %d err %v", key, code, err)
		}
		items := p.parseItems(slot, string(body), p.catName(category), nil)
		return items, page * 24, len(items) > 0, nil
	}
}

// ---- AV6K (ZakaAv6kAmns) https://av6k.com ----
var z2Av6kProfile = &htmlProfile{
	Base:      "https://av6k.com",
	UA:        z2UA,
	ListURL:   "/{cat}/",
	DetailURL: "/{id}",
	Cats: []htmlCat{
		{ID: "rihanyouma", Name: "日韓有碼"},
		{ID: "rihanwuma", Name: "日韓無碼"},
		{ID: "oumeiwuma", Name: "歐美無碼"},
		{ID: "jxny", Name: "中文字幕"},
		{ID: "fc2", Name: "FC2無碼"},
		{ID: "chengrendongman", Name: "成人動漫"},
		{ID: "surenzipai", Name: "自拍偷拍"},
	},
	ItemRe:  regexp.MustCompile(`href="(?P<id>/[a-z]+/\d+\.html)"\s+title="(?P<title>[^"]*)"[^>]*>[\s\S]{0,120}?<img src="(?P<cover>[^"]+)"`),
	TitleRe: regexp.MustCompile(`<title>([^<]{1,200})</title>`),
	CoverRe: regexp.MustCompile(`<meta[^>]*property="og:image"[^>]*content="([^"]+)"`),
	PlayRes: []*regexp.Regexp{
		regexp.MustCompile(`data-hls-src="([^"]+)"`),
		regexp.MustCompile(`(https?://[^"'\s<>]+\.m3u8[^"'\s<>]*)`),
		regexp.MustCompile(`(https?://[^"'\s<>]+\.mp4[^"'\s<>]*)`),
	},
	SearchURL: "/plus/search.php?q={kw}",
}

// av6k category page ids (empire CMS): only two confirmed from live
// homepage pagination links; other categories serve page 1 only.
var z2Av6kCatIDs = map[string]string{"chengrendongman": "5", "jxny": "11"}

// ---- 7MMTV (Zaka7mmAmns) https://7mmtv.sx ----
var z2MmProfile = &htmlProfile{
	Base:      "https://7mmtv.sx",
	UA:        z2UA,
	ListURL:   "/en/{cat}_list/all/{page}.html",
	DetailURL: "/en/{id}",
	Cats: []htmlCat{
		{ID: "censored", Name: "有碼"},
		{ID: "uncensored", Name: "無碼"},
		{ID: "amateurjav", Name: "素人"},
	},
	ItemRe:  regexp.MustCompile(`href="https://7mmtv\.sx/en/(?P<id>(?:censored|uncensored|amateurjav)_content/\d+/[^"]+\.html)"[^>]*><img[^>]*?data-src='(?P<cover>[^']+)'[^>]*?alt='(?P<title>[^']*)'`),
	TitleRe: regexp.MustCompile(`"name":\s*"([^"]{2,200})"`),
	CoverRe: regexp.MustCompile(`"image":\s*"(https?://[^"]+)"`),
	PlayRes: []*regexp.Regexp{
		regexp.MustCompile(`"hls[234]":\s*"([^"]+)"`),
		regexp.MustCompile(`"(?:contentUrl|embedUrl)":\s*"([^"]+)"`),
	},
	SearchURL: "/en/searchall_search/all/{kw}/{page}.html",
}

// ---- Xanimu (ZakaXanimuAmns) https://xanimu.one ----
var z2XanimuProfile = &htmlProfile{
	Base:      "https://xanimu.one",
	UA:        z2UA,
	ListURL:   "/zh-CN/{cat}page/{page}/",
	DetailURL: "{id}",
	Cats: []htmlCat{
		{ID: "", Name: "最新", ListURL: "/zh-CN/page/{page}/"},
		{ID: "4-%E6%AF%9B%E8%8C%B8%E8%8C%B8%E7%9A%84%E6%97%A0%E5%B0%BD/", Name: "毛茸茸的无尽"},
		{ID: "5-3d-%E6%97%A0%E5%B0%BD/", Name: "3D无尽"},
		{ID: "6-%E4%B9%B1%E4%BC%A6%E6%97%A0%E5%B0%BD/", Name: "乱伦无尽"},
		{ID: "7-%E8%A7%A6%E6%89%8B%E6%97%A0%E5%B0%BD/", Name: "触手无尽"},
		{ID: "8-futanari-%E6%97%A0%E5%B0%BD/", Name: "Futanari无尽"},
		{ID: "10-%E5%A5%B3%E5%B7%A8%E4%BA%BA%E6%97%A0%E5%B0%BD/", Name: "女巨人无尽"},
		{ID: "11-%E4%B9%B3%E6%88%BF%E6%89%A9%E5%BC%A0%E6%97%A0%E5%B0%BD/", Name: "乳房扩张无尽"},
		{ID: "13-%E6%80%AA%E7%89%A9%E6%97%A0%E5%B0%BD/", Name: "怪物无尽"},
		{ID: "9476-yaoi-%E6%97%A0%E5%B0%BD/", Name: "Yaoi无尽"},
		{ID: "47441-hmv/", Name: "HMV"},
	},
	ItemRe:  regexp.MustCompile(`href="https://xanimu\.one(?P<id>/zh-CN/\d+-[^"/]+/)"[^>]*>[\s\S]{0,160}?<img[^>]*?alt="(?P<title>[^"]*)"[^>]*?src="(?P<cover>[^"]+)"`),
	TitleRe: regexp.MustCompile(`<h1[^>]*>([^<]{1,200})</h1>`),
	PlayRes: []*regexp.Regexp{
		regexp.MustCompile(`<source[^>]+src="([^"]+\.mp4[^"]*)"`),
		regexp.MustCompile(`"(https?://[^"]+?\.mp4[^"]*)"`),
	},
	SearchURL: "/zh-CN/?s={kw}",
}

// ---- 中文秘密资源 (ZakaCnAmns) https://cnlovehubx18.xyz/962a ----
var z2CnProfile = &htmlProfile{
	Base:      "https://cnlovehubx18.xyz",
	UA:        z2UA,
	ListURL:   "/962a/vodtype/{cat}",
	DetailURL: "{id}",
	Cats: []htmlCat{
		{ID: "guochanchuanmei/", Name: "国产传媒"},
		{ID: "ribenyouma/", Name: "日本有码"},
		{ID: "ribenwuma/", Name: "无码视频"},
		{ID: "zhongwenzimu/", Name: "中文字幕"},
	},
	ItemRe:  regexp.MustCompile(`href="(?P<id>/962a/watch/[^"]+)"[^>]*>[\s\S]{0,400}?<img[^>]*?alt="(?P<title>[^"]*?)(?: [a-z][a-z0-9]{10,})? 在线观看"[^>]*?src="(?P<cover>[^"]+)"`),
	TitleRe: regexp.MustCompile(`<title>([^<]{1,200})</title>`),
	CoverRe: regexp.MustCompile(`<meta[^>]*property="og:image"[^>]*content="([^"]+)"`),
	PlayRes: []*regexp.Regexp{
		regexp.MustCompile(`"url"\s*:\s*"(https?:\\?/\\?/[^"]+?\.m3u8[^"]*)"`),
		regexp.MustCompile(`(https?://[^"'\s<>]+\.m3u8[^"'\s<>]*)`),
	},
	SearchURL: "/962a/vodsearch/wd/{kw}",
}

// cnResolve: the watch page's player_aaaa points at
// /962a/vodplay/id/{slug}/sid/1/nid/1/, which inlines the m3u8.
func z2CnResolve(slot, rawID string) (map[string]interface{}, error) {
	p := z2CnProfile
	slug := strings.TrimPrefix(rawID, "/962a/watch/")
	playURL := p.Base + "/962a/vodplay/id/" + slug + "/sid/1/nid/1/"
	body, code, _, err := httpGet(playURL, p.headers())
	if err != nil || code != 200 {
		return nil, fmt.Errorf("z2cn play http %d err %v", code, err)
	}
	for _, re := range p.PlayRes {
		if m := re.FindStringSubmatch(string(body)); m != nil {
			u := strings.ReplaceAll(m[1], `\/`, "/")
			return playResult(u, slot, rawID, p.UA, playURL, "m3u8", "hls"), nil
		}
	}
	return nil, fmt.Errorf("z2cn no stream on play page")
}

var z2MmM3u8Re = regexp.MustCompile(`(https?:\\?/\\?/[^"'\s<>]+?\.m3u8[^"'\s<>]*)`)

var (
	z2MmHlsRe   = regexp.MustCompile(`"hls([234])"\s*:\s*"([^"]+)"`)
	z2MmEmbedRe = regexp.MustCompile(`"(?:embedUrl|contentUrl)"\s*:\s*"([^"]+)"`)
	z2MmVRe     = regexp.MustCompile(`https?://[a-z0-9\-]+\.[a-z0-9\-]+/v/[A-Za-z0-9]+`)
)

func mmUnpackPage(text string) string {
	if pm := jmapPackerRe.FindStringSubmatch(text); pm != nil {
		radix, _ := strconv.Atoi(pm[2])
		count, _ := strconv.Atoi(pm[3])
		dict := strings.Split(pm[4], "|")
		return jmapUnpack(pm[1], radix, count, dict)
	}
	return text
}

func mmStreamsFromPage(text string) (string, string) {
	unpacked := mmUnpackPage(text)
	fields := map[string]string{}
	for _, m := range z2MmHlsRe.FindAllStringSubmatch(unpacked, -1) {
		if _, ok := fields["hls"+m[1]]; !ok {
			fields["hls"+m[1]] = strings.ReplaceAll(m[2], `\/`, "/")
		}
	}
	main := fields["hls2"]
	backup := fields["hls3"]
	if hls4 := fields["hls4"]; len(hls4) > 10 {
		main = hls4
	}
	if main == "" {
		if m := z2MmM3u8Re.FindStringSubmatch(unpacked); m != nil {
			main = strings.ReplaceAll(m[1], `\/`, "/")
		}
	}
	return main, backup
}

func mmOrigin(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return z2MmProfile.Base + "/"
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}

func mmExitsFromDetail(detailHTML string) []string {
	seen := map[string]bool{}
	exits := []string{}
	push := func(u string) {
		u = strings.ReplaceAll(u, `\/`, "/")
		if u == "" || seen[u] {
			return
		}
		if !strings.HasPrefix(u, "http") {
			if strings.HasPrefix(u, "//") {
				u = "https:" + u
			} else {
				return
			}
		}
		seen[u] = true
		exits = append(exits, u)
	}
	for _, m := range z2MmEmbedRe.FindAllStringSubmatch(detailHTML, -1) {
		push(m[1])
	}
	for _, m := range z2MmVRe.FindAllString(detailHTML, -1) {
		push(m)
	}
	if len(exits) > 4 {
		exits = exits[:4]
	}
	return exits
}

func mmFetchEmbed(embedURL, referer string) (string, error) {
	body, code, _, err := httpGetTimeout(embedURL, map[string]string{
		"User-Agent": z2UA,
		"Referer":    referer,
	}, 15*time.Second)
	if err != nil || code != 200 {
		return "", fmt.Errorf("z2_7mm embed http %d err %v", code, err)
	}
	return string(body), nil
}

func mmProbeText(u, referer string) (string, bool) {
	body, code, _, err := httpGetTimeout(u, map[string]string{
		"User-Agent": z2UA,
		"Referer":    referer,
	}, 10*time.Second)
	if err != nil || code != 200 {
		return "", false
	}
	text := strings.TrimSpace(string(body))
	if !strings.HasPrefix(text, "#EXTM3U") {
		return "", false
	}
	return text, true
}

func z2MmResolve(slot, rawID string) (map[string]interface{}, error) {
	p := z2MmProfile
	detailURL := p.Base + "/en/" + rawID
	body, code, _, err := httpGet(detailURL, p.headers())
	if err != nil || code != 200 {
		return nil, fmt.Errorf("z2_7mm detail http %d err %v", code, err)
	}
	detailHTML := string(body)
	type mmLine struct {
		stream  string
		referer string
		embed   string
		text    string
	}
	lines := []mmLine{}
	add := func(stream, referer, embed string) {
		if stream == "" || len(lines) >= 3 {
			return
		}
		text, ok := mmProbeText(stream, referer)
		if !ok {
			return
		}
		lines = append(lines, mmLine{stream: stream, referer: referer, embed: embed, text: text})
	}
	if main, backup := mmStreamsFromPage(detailHTML); main != "" {
		referer := mmOrigin(detailURL)
		add(main, referer, "")
		add(backup, referer, "")
	}
	for _, exit := range mmExitsFromDetail(detailHTML) {
		if len(lines) >= 3 {
			break
		}
		page, err := mmFetchEmbed(exit, detailURL)
		if err != nil {
			continue
		}
		main, backup := mmStreamsFromPage(page)
		referer := mmOrigin(exit)
		add(main, referer, exit)
		add(backup, referer, exit)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("z2_7mm no playable stream")
	}
	main := lines[0]
	result := playResult(main.stream, slot, rawID, p.UA, main.referer, "m3u8", "hls")
	if strings.HasSuffix(strings.ToLower(main.stream), ".txt") {
		result["playlist"] = main.text
	}
	if len(lines) > 1 {
		routes := []interface{}{}
		for _, line := range lines[1:] {
			routes = append(routes, map[string]interface{}{"name": "备用", "route": "备用", "url": line.stream, "referer": line.referer})
		}
		result["routes"] = routes
	}
	backups := []string{}
	for _, line := range lines[1:] {
		backups = append(backups, line.stream)
	}
	StoreStreamHook(main.stream, &mmtvStreamHook{embed: main.embed, referer: main.referer, backups: backups, tried: map[string]bool{main.stream: true}})
	return result, nil
}

func init() {
	av6k := htmlProfileImpl("z2_av6k", "AV6K", z2Av6kProfile)
	av6k.Catalog = z2PagedCatalog(z2Av6kProfile, "z2_av6k", func(cat string, page int) string {
		if page <= 1 {
			return z2Av6kProfile.Base + "/" + cat + "/"
		}
		if cid, ok := z2Av6kCatIDs[cat]; ok {
			return fmt.Sprintf("%s/%s/%s_%d.html", z2Av6kProfile.Base, cat, cid, page)
		}
		return z2Av6kProfile.Base + "/" + cat + "/"
	})
	registerSite("z2_av6k", av6k, "av6k")

	mm := htmlProfileImpl("z2_7mm", "7MMTV", z2MmProfile)
	mm.Resolve = z2MmResolve
	registerSite("z2_7mm", mm, "7mmtv")

	registerSite("z2_xanimu", htmlProfileImpl("z2_xanimu", "Xanimu", z2XanimuProfile), "xanimu")

	cn := htmlProfileImpl("z2_cn", "中文秘密资源", z2CnProfile)
	cn.Catalog = z2PagedCatalog(z2CnProfile, "z2_cn", func(cat string, page int) string {
		if page <= 1 {
			return z2CnProfile.Base + "/962a/vodtype/" + cat
		}
		return fmt.Sprintf("%s/962a/vodtype/%spage/%d.html", z2CnProfile.Base, cat, page)
	})
	cn.Resolve = z2CnResolve
	registerSite("z2_cn", cn, "cnlove")
}
