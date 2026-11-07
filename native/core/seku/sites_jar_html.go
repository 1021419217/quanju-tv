package seku

// HTML-engine sites from the ZakaTV jar, batch 1:
//   後宮電影院 20jack.com (ZakaJack20Amns) — WordPress/xvideos 聚合,
//     verified live 2026-10-24: article cards carry data-post-id,
//     <a title>, img.video-main-thumb; category ids read from the
//     live nav (cat=819/6404/4071/5346/3419/6365/6364/788/5422).
//     Detail pages embed the stream via setVideoHLS /
//     setVideoUrlHigh / setVideoUrlLow (jar regexes).
//   JavGuru jav.guru (ZakaJavguruAmns) — WordPress, verified live
//     2026-10-24: cards are <div class="imgg"><a href="/{id}/{slug}/">
//     <img alt="[CODE] title">; categories from the live nav.
//     Resolve tries the detail page's base64 "iframe_url" embed and
//     direct stream URLs; the jar's packed-JS unpacking of some
//     embed hosts is not ported (see batch report).

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var jackProfile = &htmlProfile{
	Base:    "https://20jack.com",
	UA:      "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
	ListURL: "/?cat={cat}&paged={page}",
	Cats: []htmlCat{
		{ID: "home", Name: "最新影片", ListURL: "/?filter=latest&paged={page}"},
		{ID: "6364", Name: "日本無碼"},
		{ID: "6365", Name: "日本有碼"},
		{ID: "3419", Name: "國語長片"},
		{ID: "5422", Name: "歐美長片"},
		{ID: "788", Name: "歐美短片"},
		{ID: "5346", Name: "卡通長片"},
		{ID: "4071", Name: "卡通短片"},
		{ID: "819", Name: "其他長片"},
		{ID: "6404", Name: "其它短片"},
	},
	ItemRe:    regexp.MustCompile(`(?s)data-post-id="(?P<id>\d+)"[^>]*>\s*<a[^>]*?title="(?P<title>[^"]*)"[^>]*>.*?<img class="video-main-thumb"[^>]*?src="(?P<cover>[^"]+)"`),
	DetailURL: "/?p={id}",
	TitleRe:   regexp.MustCompile(`<meta[^>]*property="og:title"[^>]*content="([^"]+)"`),
	CoverRe:   regexp.MustCompile(`<meta[^>]*property="og:image"[^>]*content="([^"]+)"`),
	PlayRes: []*regexp.Regexp{
		regexp.MustCompile(`setVideoHLS\(\s*["']([^"']+)["']`),
		regexp.MustCompile(`setVideoUrlHigh\(\s*["']([^"']+)["']`),
		regexp.MustCompile(`setVideoUrlLow\(\s*["']([^"']+)["']`),
		regexp.MustCompile(`["'](https?://[^"'\s<>]+?\.m3u8[^"'\s<>]*)["']`),
		regexp.MustCompile(`["'](https?://[^"'\s<>]+?\.mp4[^"'\s<>]*)["']`),
	},
	SearchURL: "/?s={kw}",
}

var guruProfile = &htmlProfile{
	Base:    "https://jav.guru",
	UA:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	ListURL: "/category/{cat}/page/{page}/",
	Cats: []htmlCat{
		{ID: "latest", Name: "最新", ListURL: "/page/{page}/"},
		{ID: "jav", Name: "JAV"},
		{ID: "decensored", Name: "Decensored"},
		{ID: "english-subbed", Name: "英文字幕"},
		{ID: "amateur", Name: "Amateur"},
		{ID: "idol", Name: "Idol"},
		{ID: "FC2", Name: "FC2"},
		{ID: "4k", Name: "4K"},
	},
	ItemRe:    regexp.MustCompile(`(?s)<div class="imgg">\s*<a href="https://jav\.guru/(?P<id>\d+/[^"]+?)/">\s*<img\s+src="(?P<cover>[^"]+)"\s+alt="(?P<title>[^"]*)"`),
	DetailURL: "/{id}/",
	TitleRe:   regexp.MustCompile(`<meta[^>]*property="og:title"[^>]*content="([^"]+)"`),
	CoverRe:   regexp.MustCompile(`<meta[^>]*property="og:image"[^>]*content="([^"]+)"`),
	SearchURL: "/?s={kw}",
}

var guruIframeRe = regexp.MustCompile(`"iframe_url"\s*:\s*"([A-Za-z0-9+/=_\-]+)"`)
var guruStreamRe = regexp.MustCompile(`https?://[^"'\s<>\\]+\.(?:m3u8|mp4)[^"'\s<>\\]*`)
var guruKeysRe = regexp.MustCompile(`keys:\s*\[([^\]]+)\]`)
var guruPackedRe = regexp.MustCompile(`(?s)\}\('((?:[^'\\]|\\.)*)',(\d+),(\d+),'(.*?)'\.split\('\|'\)`)
var guruWordRe = regexp.MustCompile(`\b[0-9a-zA-Z]+\b`)

// deanEdwardsUnpack reverses the classic p,a,c,k,e,d JS packer used by
// the StreamHG-family player pages JavGuru embeds (the jar's
// ZakaJavguruAmns did the same unpacking).
func deanEdwardsUnpack(payload string, radix, count int, keywords []string) string {
	decode := func(tok string) int {
		n := 0
		for _, ch := range tok {
			var v int
			switch {
			case ch >= '0' && ch <= '9':
				v = int(ch - '0')
			case ch >= 'a' && ch <= 'z':
				v = int(ch-'a') + 10
			case ch >= 'A' && ch <= 'Z':
				v = int(ch-'A') + 36
			default:
				return -1
			}
			n = n*radix + v
		}
		return n
	}
	return guruWordRe.ReplaceAllStringFunc(payload, func(tok string) string {
		idx := decode(tok)
		if idx >= 0 && idx < count && idx < len(keywords) && keywords[idx] != "" {
			return keywords[idx]
		}
		return tok
	})
}

func guruUnescapeJS(s string) string {
	r := strings.NewReplacer(`\'`, "'", `\\`, "\x00", `\/`, "/", "\x00", `\`)
	return r.Replace(s)
}

// guruSearchoURL turns a searcho gateway page into the real player
// page URL: token = concat of the cfg.keys data-* attributes (in cfg
// order), reversed, appended as ?xr= (page JS verified live).
func guruSearchoURL(html string) string {
	km := guruKeysRe.FindStringSubmatch(html)
	if km == nil {
		return ""
	}
	token := ""
	for _, attr := range strings.Split(km[1], ",") {
		attr = strings.Trim(strings.TrimSpace(attr), `'"`)
		if attr == "" {
			continue
		}
		m := regexp.MustCompile(regexp.QuoteMeta(attr) + `="([^"]*)"`).FindStringSubmatch(html)
		if m == nil {
			return ""
		}
		token += m[1]
	}
	if token == "" {
		return ""
	}
	rev := []rune(token)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return "https://jav.guru/searcho/?xr=" + string(rev)
}

func guruPlayFromPlayerPage(slot, rawID, detailURL, html string) (map[string]interface{}, error) {
	p := guruProfile
	if s := guruStreamRe.FindString(html); s != "" && !strings.Contains(s, ".urlset/") {
		format, proto := "mp4", "file"
		if strings.Contains(s, ".m3u8") {
			format, proto = "m3u8", "hls"
		}
		return playResult(s, slot, rawID, p.UA, detailURL, format, proto), nil
	}
	if m := guruPackedRe.FindStringSubmatch(html); m != nil {
		radix := atoiSafe(m[2])
		count := atoiSafe(m[3])
		if radix >= 2 && count > 0 {
			unpacked := deanEdwardsUnpack(guruUnescapeJS(m[1]), radix, count, strings.Split(m[4], "|"))
			if s := guruStreamRe.FindString(unpacked); s != "" {
				format, proto := "mp4", "file"
				if strings.Contains(s, ".m3u8") {
					format, proto = "m3u8", "hls"
				}
				return playResult(s, slot, rawID, p.UA, detailURL, format, proto), nil
			}
		}
	}
	return nil, fmt.Errorf("javguru: player page yielded no stream")
}

func guruResolve(slot, rawID string) (map[string]interface{}, error) {
	p := guruProfile
	detailURL := p.Base + "/" + rawID + "/"
	body, code, _, err := httpGet(detailURL, p.headers())
	if err != nil || code != 200 {
		return nil, fmt.Errorf("javguru detail http %d err %v", code, err)
	}
	html := string(body)
	if res, err := guruPlayFromPlayerPage(slot, rawID, detailURL, html); err == nil {
		return res, nil
	}
	m := guruIframeRe.FindStringSubmatch(html)
	if m == nil {
		return nil, fmt.Errorf("javguru: no iframe_url on detail page")
	}
	raw, derr := base64.StdEncoding.DecodeString(m[1])
	if derr != nil {
		if raw, derr = base64.URLEncoding.DecodeString(m[1]); derr != nil {
			return nil, fmt.Errorf("javguru: iframe_url b64: %v", derr)
		}
	}
	gateway := string(raw)
	if !strings.HasPrefix(gateway, "http") {
		return nil, fmt.Errorf("javguru: iframe_url is not a URL")
	}
	h := p.headers()
	h["Referer"] = detailURL
	b2, c2, _, e2 := httpGet(gateway, h)
	if e2 != nil || c2 != 200 {
		return nil, fmt.Errorf("javguru gateway http %d err %v", c2, e2)
	}
	playerURL := guruSearchoURL(string(b2))
	if playerURL == "" {
		return nil, fmt.Errorf("javguru: searcho gateway token not found")
	}
	b3, c3, _, e3 := httpGet(playerURL, h)
	if e3 != nil || c3 != 200 {
		return nil, fmt.Errorf("javguru player http %d err %v", c3, e3)
	}
	return guruPlayFromPlayerPage(slot, rawID, detailURL, string(b3))
}

func atoiSafe(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// ---- jack20 two-step resolve ----
//
// Detail pages carry no stream; the clean-tube-player iframe's q
// parameter base64-decodes to
//   post_id=..&type=iframe&tag=<iframe src="{embed}">
// (verified live 2026-10-24). The embed page (usually an xvideos
// embedframe) then carries setVideoHLS / setVideoUrlHigh/Low — the
// jar's exact regexes. Note some embeds are dead on the host side
// (xvideos returned 404 for a sampled post); resolve reports that
// honestly instead of inventing a URL.

var jackQRe = regexp.MustCompile(`player-x\.php\?q=([A-Za-z0-9+/=]+)`)
var jackTagSrcRe = regexp.MustCompile(`src="([^"]+)"`)

func jackResolve(slot, rawID string) (map[string]interface{}, error) {
	p := jackProfile
	detailURL := p.Base + "/?p=" + rawID
	body, code, _, err := httpGet(detailURL, p.headers())
	if err != nil || code != 200 {
		return nil, fmt.Errorf("jack20 detail http %d err %v", code, err)
	}
	html := string(body)
	tryPatterns := func(page string) string {
		for _, re := range p.PlayRes {
			if m := re.FindStringSubmatch(page); m != nil && m[1] != "" {
				return m[1]
			}
		}
		return ""
	}
	if u := tryPatterns(html); u != "" {
		return jackPlayResult(slot, rawID, detailURL, u), nil
	}
	qm := jackQRe.FindStringSubmatch(html)
	if qm == nil {
		return nil, fmt.Errorf("jack20: no player iframe on detail page")
	}
	dec, derr := base64.StdEncoding.DecodeString(qm[1])
	if derr != nil {
		return nil, fmt.Errorf("jack20: q b64: %v", derr)
	}
	tag, _ := url.QueryUnescape(string(dec))
	sm := jackTagSrcRe.FindStringSubmatch(tag)
	if sm == nil {
		return nil, fmt.Errorf("jack20: q payload has no iframe src")
	}
	embed := sm[1]
	h := p.headers()
	h["Referer"] = "https://www.xvideos.com/"
	b2, c2, _, e2 := httpGet(embed, h)
	if e2 != nil || c2 != 200 {
		return nil, fmt.Errorf("jack20: embed http %d err %v (%s)", c2, e2, embed)
	}
	if u := tryPatterns(string(b2)); u != "" {
		return jackPlayResult(slot, rawID, detailURL, u), nil
	}
	return nil, fmt.Errorf("jack20: embed page yielded no stream (%s)", embed)
}

func jackPlayResult(slot, rawID, referer, u string) map[string]interface{} {
	format, proto := "mp4", "file"
	if strings.Contains(u, ".m3u8") {
		format, proto = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, jackProfile.UA, referer, format, proto)
}

func init() {
	jack := htmlProfileImpl("jack20", "後宮電影院", jackProfile)
	jack.Resolve = jackResolve
	registerSite("jack20", jack, "jack20", "20jack")
	guru := htmlProfileImpl("javguru", "JavGuru", guruProfile)
	guru.Resolve = guruResolve
	registerSite("javguru", guru, "javguru", "jav.guru")
}
