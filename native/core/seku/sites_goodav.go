package seku

import (
	"encoding/base64"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// GoodAV（goodav17.com）— 配方出 ZakaTV jar ZakaGoodavAmns 反编译：
// 静态 HTML 站，首页导航动态给类型（52）与女优（121）索引；列表页
// 以 <div class='movie'> 分块，卡片 id 按 html/local_html/vr_html
// 前缀分流；详情页聚合 ggjav / youjizz / porn87 三家 embed 为多线
// 路，VR 片另有 vr.goodav17.com 直链媒体。ggjav 的 u 参数是直链
// mp4 的 base64，播放优先试 {mp4}/index.m3u8（HLS 派生），不行
// 再挖 embed 页。
//
// 2026-10-24 本机全链实拉：首页 23 卡、类型/女优导航齐、详情
// 104KB 含 gg embed、embed 解出真 m3u8（video-8.ggjav.com）、
// VR 列表 23 卡、搜索 23 卡。

const gaHost = "https://goodav17.com"

const gaUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var (
	gaBlockID  = regexp.MustCompile(`goodav17\.com/(vr_html/|local_html/|html/)(\d+)/`)
	gaTitleRe  = regexp.MustCompile(`<a href="https://goodav17\.com/(?:vr_html/|local_html/|html/)\d+/">([^<]+)</a>`)
	gaLargeRe  = regexp.MustCompile(`large_image='([^']+)'`)
	gaSrcRe    = regexp.MustCompile(`<img\s+src='([^']+)'`)
	gaNavRe    = regexp.MustCompile(`goodav17\.com/(type|actor)/([^/<>"\s][^/<>" ]*?)/1/["<>]*>([^<]{1,40})<`)
	gaGGRe     = regexp.MustCompile(`https://ggjav\.com/main/embed\?u=([A-Za-z0-9_\-=+/%]+)`)
	gaIframeRe = regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`)
	gaVRMedia  = regexp.MustCompile(`https://vr\.goodav17\.com/media/[A-Za-z0-9_\-]+\.(?:mp4|m3u8)`)
	gaM3u8Re   = regexp.MustCompile(`https?://[^\s"'<>\\\)]+\.m3u8[^\s"'<>\\\)]*`)
	gaMp4Re    = regexp.MustCompile(`https?://[^\s"'<>\\\)]+\.mp4[^\s"'<>\\\)]*`)
	gaFileRe   = regexp.MustCompile(`"(?:file|src|source|url)"\s*:\s*"(https?://[^"]+)"`)
	gaTitleTag = regexp.MustCompile(`(?s)<title>([^<]+)</title>`)
	gaActorLnk = regexp.MustCompile(`goodav17\.com/actor/([^/"]+)/1/["<>]*>([^<]{1,40})<`)
	gaTypeLnk  = regexp.MustCompile(`goodav17\.com/type/([^/"]+)/1/["<>]*>([^<]{1,40})<`)
)

var gaState = struct {
	sync.Mutex
	types  []string
	actors []string
}{}

func gaHeaders() map[string]string {
	return map[string]string{"User-Agent": gaUA}
}

func gaFetch(u string) (string, error) {
	b, _, _, err := httpGet(u, gaHeaders())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// gaNav 解析首页导航的类型与女优名单（会话内缓存）。
func gaNav() (types, actors []string) {
	gaState.Lock()
	defer gaState.Unlock()
	if gaState.types != nil {
		return gaState.types, gaState.actors
	}
	types = []string{"無碼", "人妻", "巨乳", "OL", "潮吹", "中出", "絲襪", "強迫"}
	page, err := gaFetch(gaHost + "/")
	if err == nil {
		seenT, seenA := map[string]bool{}, map[string]bool{}
		var lt, la []string
		for _, m := range gaNavRe.FindAllStringSubmatch(page, -1) {
			name := html.UnescapeString(strings.TrimSpace(m[3]))
			if name == "" {
				continue
			}
			if m[1] == "type" {
				if !seenT[name] {
					seenT[name] = true
					lt = append(lt, name)
				}
			} else if !seenA[name] {
				seenA[name] = true
				la = append(la, name)
			}
		}
		if len(lt) >= 8 {
			types = lt
		}
		actors = la
	}
	gaState.types, gaState.actors = types, actors
	return types, actors
}

func gaCategories(slot string) []interface{} {
	types, _ := gaNav()
	out := []interface{}{
		categoryObj("home", "🆕 最近更新"),
		categoryObj("vr", "🥽 VR专区"),
		categoryObj("local", "🏠 本土自拍"),
		categoryObj("actor", "👩 女優作品"),
	}
	for _, t := range types {
		out = append(out, categoryObj("t:"+t, "類型·"+t))
	}
	return out
}

// gaCardID 与 jar 同规：html 片裸 id、local 片 lc 前缀、vr 片 vr 前缀。
func gaCardID(kind, num string) string {
	switch kind {
	case "local_html/":
		return "lc" + num
	case "vr_html/":
		return "vr" + num
	}
	return num
}

func gaParseList(page string) []map[string]interface{} {
	var out []map[string]interface{}
	seen := map[string]bool{}
	for _, block := range strings.Split(page, "<div class='movie'>")[1:] {
		if len(block) > 2600 {
			block = block[:2600]
		}
		idm := gaBlockID.FindStringSubmatch(block)
		if idm == nil {
			continue
		}
		rawID := gaCardID(idm[1], idm[2])
		if seen[rawID] {
			continue
		}
		seen[rawID] = true
		title := ""
		if tm := gaTitleRe.FindAllStringSubmatch(block, -1); len(tm) > 0 {
			title = html.UnescapeString(strings.TrimSpace(tm[len(tm)-1][1]))
		}
		if title == "" {
			continue
		}
		cover := ""
		if lm := gaLargeRe.FindStringSubmatch(block); lm != nil {
			cover = lm[1]
		} else if sm := gaSrcRe.FindStringSubmatch(block); sm != nil {
			cover = sm[1]
		}
		out = append(out, map[string]interface{}{
			"rawID": rawID, "title": title, "cover": cover,
		})
	}
	return out
}

func gaListPath(category string, page int) string {
	switch {
	case category == "vr":
		return fmt.Sprintf("%s/vr/%d/", gaHost, page)
	case category == "local":
		return fmt.Sprintf("%s/local/%d/", gaHost, page)
	case strings.HasPrefix(category, "t:"):
		return fmt.Sprintf("%s/type/%s/%d/", gaHost, url.PathEscape(strings.TrimPrefix(category, "t:")), page)
	case strings.HasPrefix(category, "a:"):
		return fmt.Sprintf("%s/actor/%s/%d/", gaHost, url.PathEscape(strings.TrimPrefix(category, "a:")), page)
	default: // home 最近更新即首页（分页同 /{n}/ 由站方根路径承担，jar 用根页）
		if page <= 1 {
			return gaHost + "/"
		}
		return fmt.Sprintf("%s/%d/", gaHost, page)
	}
}

func gaCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "actor" {
		// 女優作品：索引页给女优文件夹卡（点入即该女优作品列表）
		_, actors := gaNav()
		var out []interface{}
		for _, name := range actors {
			out = append(out, makeDrama(slot, "idx:a:"+name, name, "", "女優", "女優作品", ""))
		}
		return out, len(out), false, nil
	}
	cat := category
	if strings.HasPrefix(category, "idx:a:") {
		cat = strings.TrimPrefix(category, "idx:")
	}
	body, err := gaFetch(gaListPath(cat, page))
	if err != nil {
		return nil, 0, false, err
	}
	cards := gaParseList(body)
	out := make([]interface{}, 0, len(cards))
	for _, c := range cards {
		out = append(out, makeDrama(slot, c["rawID"].(string), c["title"].(string), c["cover"].(string), "", "", ""))
	}
	return out, len(out), len(cards) > 0, nil
}

func gaDetailPath(rawID string) string {
	switch {
	case strings.HasPrefix(rawID, "lc"):
		return gaHost + "/local_html/" + strings.TrimPrefix(rawID, "lc") + "/"
	case strings.HasPrefix(rawID, "vr"):
		return gaHost + "/vr_html/" + strings.TrimPrefix(rawID, "vr") + "/"
	default:
		return gaHost + "/html/" + rawID + "/"
	}
}

// gaLineID 与 jar lineId 同规：embed URL → 线路令牌。
func gaLineID(embed string) string {
	if m := gaGGRe.FindStringSubmatch(embed); m != nil {
		u := strings.ReplaceAll(m[1], "%3D", "=")
		u = strings.ReplaceAll(u, "%3d", "=")
		if dec, err := url.QueryUnescape(u); err == nil {
			u = dec
		}
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(u, " ", "+"))
		if err == nil {
			s := string(raw)
			if strings.HasPrefix(s, "http") && strings.Contains(s, ".mp4") {
				if i := strings.Index(s, "?"); i > 0 {
					s = s[:i]
				}
				return "gv|" + m[1] + "|" + s
			}
		}
		return ""
	}
	if strings.Contains(embed, "porn87.com/main/embed") {
		return "pr|" + embed
	}
	if strings.Contains(embed, "youjizz.com") {
		return "yj|" + embed
	}
	return ""
}

func gaDetail(slot, rawID string) (map[string]interface{}, error) {
	detailURL := gaDetailPath(rawID)
	body, err := gaFetch(detailURL)
	if err != nil {
		return nil, err
	}
	title := ""
	if m := gaTitleTag.FindStringSubmatch(body); m != nil {
		title = html.UnescapeString(strings.TrimSpace(m[1]))
		title = strings.TrimSuffix(title, " - GoodAV")
		title = strings.TrimSuffix(title, " | GoodAV")
	}
	cover := ""
	if m := gaLargeRe.FindStringSubmatch(body); m != nil {
		cover = m[1]
	}
	var actors, types []string
	seenA, seenT := map[string]bool{}, map[string]bool{}
	for _, m := range gaActorLnk.FindAllStringSubmatch(body, -1) {
		if n := html.UnescapeString(strings.TrimSpace(m[2])); n != "" && !seenA[n] {
			seenA[n] = true
			actors = append(actors, n)
		}
	}
	for _, m := range gaTypeLnk.FindAllStringSubmatch(body, -1) {
		if n := html.UnescapeString(strings.TrimSpace(m[2])); n != "" && !seenT[n] {
			seenT[n] = true
			types = append(types, n)
		}
	}
	var descParts []string
	if len(actors) > 0 {
		descParts = append(descParts, "主演："+strings.Join(actors, ", "))
	}
	if len(types) > 0 {
		descParts = append(descParts, "類型："+strings.Join(types, " / "))
	}
	desc := strings.Join(descParts, " · ")
	// 线路：详情页 iframe 中三家 embed + VR 直链媒体
	var epIDs, epNames []string
	seenLine := map[string]bool{}
	for _, m := range gaIframeRe.FindAllStringSubmatch(body, -1) {
		src := strings.ReplaceAll(m[1], "&amp;", "&")
		line := gaLineID(src)
		if line == "" || seenLine[line] {
			continue
		}
		seenLine[line] = true
		epIDs = append(epIDs, line)
		name := "線路" + fmt.Sprint(len(epIDs))
		switch {
		case strings.HasPrefix(line, "gv|"):
			name += "·GG"
		case strings.HasPrefix(line, "pr|"):
			name += "·P87"
		case strings.HasPrefix(line, "yj|"):
			name += "·YJ"
		}
		epNames = append(epNames, name)
	}
	for _, vm := range gaVRMedia.FindAllString(body, -1) {
		line := "vm|" + vm
		if seenLine[line] {
			continue
		}
		seenLine[line] = true
		epIDs = append(epIDs, line)
		epNames = append(epNames, "VR直链")
	}
	if len(epIDs) == 0 {
		return nil, fmt.Errorf("GoodAV 详情未给出播放线路")
	}
	d := makeDrama(slot, rawID, title, cover, "", "", desc)
	g5SetChapters(d, slot, rawID, epIDs, epNames)
	return d, nil
}

// gaDigEmbed 通用 embed 挖掘：m3u8 → file-json → mp4。
func gaDigEmbed(embedURL, referer string) (string, string) {
	b, _, _, err := httpGet(embedURL, map[string]string{"User-Agent": gaUA, "Referer": referer})
	if err != nil {
		return "", ""
	}
	page := strings.ReplaceAll(string(b), `\/`, "/")
	if m := gaM3u8Re.FindString(page); m != "" {
		return m, "m3u8"
	}
	if m := gaFileRe.FindStringSubmatch(page); m != nil {
		if strings.Contains(m[1], ".m3u8") {
			return m[1], "m3u8"
		}
		return m[1], "mp4"
	}
	if m := gaMp4Re.FindString(page); m != "" {
		return m, "mp4"
	}
	return "", ""
}

func gaResolve(slot, rawID string) (map[string]interface{}, error) {
	line := rawID
	switch {
	case strings.HasPrefix(line, "vm|"):
		u := strings.TrimPrefix(line, "vm|")
		format := "mp4"
		if strings.Contains(u, ".m3u8") {
			format = "m3u8"
		}
		return playResult(u, slot, rawID, gaUA, gaHost+"/", format, ""), nil
	case strings.HasPrefix(line, "gv|"):
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("GoodAV 线路令牌损坏")
		}
		u, mp4Base := parts[1], parts[2]
		// 优先 HLS 派生：{mp4}/index.m3u8（2026-10-24 实拉验证形态）
		hls := mp4Base + "/index.m3u8"
		if b, _, _, err := httpGet(hls, gaHeaders()); err == nil && strings.Contains(string(b), "#EXTM3U") {
			return playResult(hls, slot, rawID, gaUA, gaHost+"/", "m3u8", ""), nil
		}
		embed := "https://ggjav.com/main/embed?u=" + u
		if got, format := gaDigEmbed(embed, gaHost+"/"); got != "" {
			return playResult(got, slot, rawID, gaUA, embed, format, ""), nil
		}
		return playResult(mp4Base, slot, rawID, gaUA, gaHost+"/", "mp4", ""), nil
	case strings.HasPrefix(line, "pr|"), strings.HasPrefix(line, "yj|"):
		embed := line[3:]
		if got, format := gaDigEmbed(embed, gaHost+"/"); got != "" {
			return playResult(got, slot, rawID, gaUA, embed, format, ""), nil
		}
		return nil, fmt.Errorf("GoodAV 该线路暂时挖不到播放地址")
	default:
		// 兜底：当详情 id 传入时重走详情取第一条线路
		d, err := gaDetail(slot, rawID)
		if err != nil {
			return nil, err
		}
		chs, _ := d["chapters"].([]interface{})
		if len(chs) == 0 {
			return nil, fmt.Errorf("GoodAV 无可用线路")
		}
		first, _ := chs[0].(map[string]interface{})
		epID, _ := first["episodeId"].(string)
		if epID == "" || epID == rawID {
			return nil, fmt.Errorf("GoodAV 无可用线路")
		}
		return gaResolve(slot, epID)
	}
}

func gaSearch(slot, kw string) ([]interface{}, error) {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return nil, nil
	}
	body, err := gaFetch(fmt.Sprintf("%s/search/%s/1/", gaHost, url.PathEscape(kw)))
	if err != nil {
		return nil, err
	}
	cards := gaParseList(body)
	if len(cards) == 0 {
		// jar 同款兜底：去掉空格与连字符再搜一次
		alt := strings.Join(strings.FieldsFunc(kw, func(r rune) bool { return r == ' ' || r == '-' }), "")
		if alt != "" && alt != kw {
			if b2, err2 := gaFetch(fmt.Sprintf("%s/search/%s/1/", gaHost, url.PathEscape(alt))); err2 == nil {
				cards = gaParseList(b2)
			}
		}
	}
	out := make([]interface{}, 0, len(cards))
	for _, c := range cards {
		out = append(out, makeDrama(slot, c["rawID"].(string), c["title"].(string), c["cover"].(string), "", "", ""))
	}
	return out, nil
}

func init() {
	registerSite("goodav", &siteImpl{
		Key: "goodav", Name: "GoodAV", DefaultCat: "home", HomeURL: gaHost + "/",
		Categories: gaCategories,
		Catalog:    gaCatalog,
		Detail:     gaDetail,
		Resolve:    gaResolve,
		Search:     gaSearch,
		Headers:    gaHeaders,
	}, "goodav")
}
