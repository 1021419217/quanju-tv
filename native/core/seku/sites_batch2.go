package seku

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

var av520Profile = &htmlProfile{
	Base:   "https://www.520avtv.com",
	UA:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	Origin: "https://www.520avtv.com",
	ListURL:   "/video/category/{cat}/{page}",
	DetailURL: "/video/watch/{id}",
	Cats: []htmlCat{
		{ID: "3", Name: "人妻熟女"},
		{ID: "9", Name: "無碼AV"},
		{ID: "1", Name: "中文字幕"},
		{ID: "242", Name: "國產自拍"},
		{ID: "7", Name: "成人卡通"},
	},
	ItemRe:    regexp.MustCompile(`href="https?://[A-Za-z0-9.]*520avtv\.com/video/watch/(?P<id>\d+/[^"/]+)"[^>]*>\s*<img[^>]*?alt="(?P<title>[^"]*)"`),
	TitleRe:   regexp.MustCompile(`(?s)<h1[^>]*class="[^"]*font-size-22[^"]*"[^>]*>\s*([^<]{1,200}?)\s*</h1>`),
	CoverRe:   regexp.MustCompile(`<meta[^>]*property="og:image"[^>]*content="([^"]+)"`),
	DescRe:    regexp.MustCompile(`<meta[^>]*name="description"[^>]*content="([^"]*)"`),
	PlayRes:   []*regexp.Regexp{regexp.MustCompile(`"url"\s*:\s*"(https?://[^"]+\.m3u8[^"]*)"`)},
	SearchURL: "/video/search?search={kw}",
	Fetch:     av520ShieldFetch,
}

func av520ShieldFetch(rawurl string, headers map[string]string, referer string) ([]byte, int, error) {
	body := zkShieldFetch(rawurl, headers, referer, 22000, false)
	if body == "" {
		return nil, 0, fmt.Errorf("av520 shield fetch empty")
	}
	return []byte(body), 200, nil
}

var av520AnchorRe = regexp.MustCompile(`href="https?://[A-Za-z0-9.]*520avtv\.com/video/watch/(\d+)/([^"/]+)"`)
var av520ImgTagRe = regexp.MustCompile(`<img[^>]*>`)
var av520ImgAttrRe = regexp.MustCompile(`(?:src|data-src)="([^"]+)"`)
var av520ImgAltRe = regexp.MustCompile(`alt="([^"]*)"`)
var av520CSRFRes = []*regexp.Regexp{
	regexp.MustCompile(`name="csrf_520"\s+value="([^"]+)"`),
	regexp.MustCompile(`csrf_520[^>]*value="([^"]+)"`),
}

func av520Pic(numID string) string {
	return "https://imgs.520avtv.com/pic/" + numID + ".jpg"
}

func av520NumID(rawID string) string {
	if i := strings.Index(rawID, "/"); i >= 0 {
		return rawID[:i]
	}
	return rawID
}

func av520Cards(slot, page, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, loc := range av520AnchorRe.FindAllStringSubmatchIndex(page, -1) {
		numID, slug := page[loc[2]:loc[3]], page[loc[4]:loc[5]]
		if seen[numID] {
			continue
		}
		seen[numID] = true
		title, cover := slug, av520Pic(numID)
		rest := page[loc[1]:]
		if len(rest) > 2000 {
			rest = rest[:2000]
		}
		if imgLoc := av520ImgTagRe.FindStringIndex(rest); imgLoc != nil {
			imgTag := rest[imgLoc[0]:imgLoc[1]]
			if m := av520ImgAltRe.FindStringSubmatch(imgTag); m != nil && m[1] != "" {
				title = cleanText(m[1])
			}
			if m := av520ImgAttrRe.FindStringSubmatch(imgTag); m != nil && m[1] != "" {
				cover = av520Profile.abs("", html.UnescapeString(m[1]))
			}
		}
		items = append(items, makeDrama(slot, numID+"/"+slug, title, cover, "", catName, ""))
	}
	return items
}

func av520FetchDetail(rawID string) (title, cover, desc, play, detailURL string, err error) {
	p := av520Profile
	urls := []string{}
	if strings.Contains(rawID, "/") {
		urls = append(urls, p.Base+"/video/watch/"+rawID)
	}
	urls = append(urls, p.Base+"/video/watch/"+av520NumID(rawID))
	var fallbackTitle, fallbackCover, fallbackDesc, fallbackPlay, fallbackURL string
	for _, u := range urls {
		body, code, _, gerr := httpGet(u, p.headers())
		if gerr != nil || code != 200 {
			err = fmt.Errorf("520av detail http %d err %v", code, gerr)
			continue
		}
		page := string(body)
		t, c, d, pl := "", "", "", ""
		if p.TitleRe != nil {
			if m := p.TitleRe.FindStringSubmatch(page); m != nil {
				t = cleanText(m[1])
			}
		}
		if p.CoverRe != nil {
			if m := p.CoverRe.FindStringSubmatch(page); m != nil {
				c = p.abs(p.CoverBase, m[1])
			}
		}
		if p.DescRe != nil {
			if m := p.DescRe.FindStringSubmatch(page); m != nil {
				d = cleanText(m[1])
			}
		}
		for _, re := range p.PlayRes {
			if m := re.FindStringSubmatch(page); m != nil {
				v := m[0]
				if len(m) > 1 && m[1] != "" {
					v = m[1]
				}
				if v != "" {
					pl = p.abs(p.PlayBase, v)
					break
				}
			}
		}
		if c == "" {
			c = av520Pic(av520NumID(rawID))
		}
		if pl != "" {
			return t, c, d, pl, u, nil
		}
		if fallbackURL == "" {
			fallbackTitle, fallbackCover, fallbackDesc, fallbackPlay, fallbackURL = t, c, d, pl, u
		}
	}
	if fallbackURL != "" {
		return fallbackTitle, fallbackCover, fallbackDesc, fallbackPlay, fallbackURL, nil
	}
	if err == nil {
		err = fmt.Errorf("520av detail failed for %s", rawID)
	}
	return "", "", "", "", "", err
}

func av520Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	p := av520Profile
	if category == "" {
		category = p.Cats[0].ID
	}
	if page < 1 {
		page = 1
	}
	body, code, _, err := httpGet(p.listURL(category, page), p.headers())
	if err != nil || code != 200 {
		return nil, 0, false, fmt.Errorf("520av catalog http %d err %v", code, err)
	}
	items := av520Cards(slot, string(body), p.catName(category))
	return items, page * 24, len(items) > 0, nil
}

func av520Detail(slot, rawID string) (map[string]interface{}, error) {
	title, cover, desc, play, _, err := av520FetchDetail(rawID)
	if err != nil {
		return nil, err
	}
	remark := "无源"
	if play != "" {
		remark = "直连"
	}
	d := makeDrama(slot, rawID, title, cover, remark, "", desc)
	if title == "" {
		if st := getStore(slot).findDrama(rawID); st != nil {
			if t, _ := st["title"].(string); t != "" {
				d["title"] = t
				d["name"] = t
			}
			if c, _ := st["cover"].(string); c != "" && cover == "" {
				d["cover"] = c
				d["coverUrl"] = c
				d["vod_pic"] = c
				d["pic"] = c
				d["thumb"] = c
			}
		}
	}
	return d, nil
}

func av520Resolve(slot, rawID string) (map[string]interface{}, error) {
	_, _, _, play, _, err := av520FetchDetail(rawID)
	if err != nil {
		return nil, err
	}
	if play == "" {
		return nil, fmt.Errorf("520av no play url")
	}
	if strings.Contains(play, ".m3u8") {
		body, code, _, gerr := httpGet(play, av520Profile.headers())
		if gerr != nil || code != 200 {
			return nil, fmt.Errorf("520av playlist http %d err %v", code, gerr)
		}
		first := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
		if !strings.HasPrefix(first, "#EXTM3U") {
			return nil, fmt.Errorf("520av playlist invalid for %s", rawID)
		}
		return playResult(play, slot, rawID, av520Profile.UA, av520Profile.Base+"/", "m3u8", "hls"), nil
	}
	return playResult(play, slot, rawID, av520Profile.UA, av520Profile.Base+"/", "mp4", "file"), nil
}

func av520Search(slot, kw string) ([]interface{}, error) {
	p := av520Profile
	searchURL := p.Base + "/video/search"
	csrf := ""
	if body, code, _, err := httpGet(searchURL, p.headers()); err == nil && code == 200 {
		page := string(body)
		for _, re := range av520CSRFRes {
			if m := re.FindStringSubmatch(page); m != nil && strings.TrimSpace(m[1]) != "" {
				csrf = strings.TrimSpace(m[1])
				break
			}
		}
	}
	if csrf != "" {
		h := p.headers()
		h["Content-Type"] = "application/x-www-form-urlencoded"
		h["Origin"] = p.Base
		h["Referer"] = searchURL
		h["Cookie"] = "csrf_cookie_520=" + csrf
		form := "search=" + url.QueryEscape(kw) + "&csrf_520=" + url.QueryEscape(csrf)
		if b, code, _, err := httpDo("POST", searchURL, strings.NewReader(form), h); err == nil && code == 200 {
			if items := av520Cards(slot, string(b), "搜索"); len(items) > 0 {
				return items, nil
			}
		}
	}
	body, code, _, err := httpGet(searchURL+"?search="+url.QueryEscape(kw), p.headers())
	if err != nil || code != 200 {
		return []interface{}{}, fmt.Errorf("520av search http %d err %v", code, err)
	}
	return av520Cards(slot, string(body), "搜索"), nil
}

var aoxxProfile = &htmlProfile{
	Base:      "https://www.aoxx69.app",
	UA:        "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
	ListURL:   "/hk/{cat}?page={page}",
	DetailURL: "/hk/{id}",
	Cats: []htmlCat{
		{ID: "jav", Name: "JAV"},
		{ID: "clips", Name: "短片"},
		{ID: "anime", Name: "動漫"},
	},
	ItemRe:  regexp.MustCompile(`href="/hk/(?P<id>(?:jav|clips|anime)/[^"]+)"[^>]*>\s*<img[^>]*?data-src="(?P<cover>[^"]+)"[^>]*?alt="(?P<title>[^"]*)"`),
	TitleRe: regexp.MustCompile(`<meta property="og:title" content="([^"]+)"`),
	CoverRe: regexp.MustCompile(`<meta property="og:image" content="([^"]+)"`),
	PlayRes: []*regexp.Regexp{regexp.MustCompile(`<iframe[^>]*src="(/video/video-play[^"]*)"`)},
}

var aoxxVSRe = regexp.MustCompile(`(/vs\.m3u8\?[^"'\s<>]+)`)

func aoxxResolve(slot, rawID string) (map[string]interface{}, error) {
	p := aoxxProfile
	detailURL := p.Base + "/hk/" + rawID
	body, code, _, err := httpGet(detailURL, p.headers())
	if err != nil || code != 200 {
		return nil, fmt.Errorf("aoxx69 detail http %d err %v", code, err)
	}
	m := p.PlayRes[0].FindStringSubmatch(string(body))
	if m == nil {
		return nil, fmt.Errorf("aoxx69 no player iframe")
	}
	playPage := p.abs("", html.UnescapeString(m[1]))
	playHeaders := p.headers()
	playHeaders["Referer"] = detailURL
	body2, code2, _, err2 := httpGet(playPage, playHeaders)
	if err2 != nil || code2 != 200 {
		return nil, fmt.Errorf("aoxx69 play page http %d err %v", code2, err2)
	}
	vm := aoxxVSRe.FindStringSubmatch(string(body2))
	if vm == nil {
		return nil, fmt.Errorf("aoxx69 no stream")
	}
	stream := p.abs("", strings.ReplaceAll(vm[1], "&amp;", "&"))
	return playResult(stream, slot, rawID, p.UA, detailURL, "m3u8", "hls"), nil
}

func init() {
	av520 := htmlProfileImpl("520av", "520AV影城", av520Profile)
	av520.Catalog = av520Catalog
	av520.Detail = av520Detail
	av520.Resolve = av520Resolve
	av520.Search = av520Search
	registerSite("av520", av520, "520av")
	aoxx := htmlProfileImpl("aoxx69", "AOXX69", aoxxProfile)
	aoxx.Resolve = aoxxResolve
	registerSite("aoxx69", aoxx, "aoxx69")
}
