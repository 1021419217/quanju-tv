package seku

// ---- G6 group: youtube (slot xingguo), javxu (slot wangguo),
// xszav (slot faguo) ----
//
// Ports of the workspace py sources:
//   youtube -> ~/workspace/youtube_work/youtube.py (InnerTube protocol)
//   javxu   -> ~/workspace/javxu_work/javxu.py + 摸站笔记.md
//   xszav   -> ~/workspace/xszav_work/xszav.py + 摸站笔记.md
//
// Folder adaptation ("idx:" scheme): index categories (actresses,
// genres, tags, playlists) return folder dramas whose rawID is
// "idx:<target>"; Detail on a folder fetches the target's first page
// and exposes its videos as chapters (episodeId = the video's own
// rawID), so Resolve on a chapter plays the real video. Resolve on a
// folder rawID itself delegates to its first chapter.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------- shared helpers ----------

var g6TagRe = regexp.MustCompile(`(?s)<!--.*?-->|<[^>]+>`)

// g6StripTags removes HTML tags/comments and collapses whitespace
// (the py sources' txt_of).
func g6StripTags(s string) string {
	s = g6TagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return cleanText(strings.Join(strings.Fields(s), " "))
}

func g6Map(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func g6List(v interface{}) []interface{} {
	l, _ := v.([]interface{})
	return l
}

func g6Str(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return ""
}

// g6Walk is a DFS over decoded JSON, visiting every object node
// before its children (mirrors the py _walk).
func g6Walk(node interface{}, fn func(map[string]interface{})) {
	switch t := node.(type) {
	case map[string]interface{}:
		fn(t)
		for _, v := range t {
			g6Walk(v, fn)
		}
	case []interface{}:
		for _, v := range t {
			g6Walk(v, fn)
		}
	}
}

// g6Chapter builds one chapter object (maccms_api.go shape).
func g6Chapter(slot, parentRaw, epRaw, title string, idx int) map[string]interface{} {
	return map[string]interface{}{
		"episodeId": epRaw,
		"id":        dramaID(slot, epRaw),
		"dramaId":   dramaID(slot, parentRaw),
		"sourceId":  slot,
		"source":    slot,
		"title":     title,
		"name":      title,
		"index":     idx,
		"episode":   idx,
		"url":       "",
		"videoUrl":  "",
		"mediaSize": 0,
	}
}

type g6ChapSpec struct{ raw, title string }

// g6FolderDrama turns a makeDrama result into a folder drama whose
// chapters are the folder's videos.
func g6FolderDrama(slot, rawID, title, cover, remark, catName string, chaps []g6ChapSpec) map[string]interface{} {
	d := makeDrama(slot, rawID, title, cover, remark, catName, "")
	if len(chaps) > 0 {
		chapters := []interface{}{}
		for i, c := range chaps {
			chapters = append(chapters, g6Chapter(slot, rawID, c.raw, c.title, i+1))
		}
		d["chapters"] = chapters
		d["episodeList"] = chapters
		d["episodes"] = len(chaps)
		d["episodeCount"] = len(chaps)
		d["chapterCount"] = len(chaps)
		d["totalEpisode"] = len(chaps)
		d["totalEpisodes"] = len(chaps)
	}
	return d
}

// g6NoReferer drops the Referer entry playResult always sets (py
// play headers for youtube carry only a User-Agent).
func g6NoReferer(res map[string]interface{}) {
	if h, ok := res["headers"].(map[string]interface{}); ok {
		delete(h, "Referer")
	}
	res["referer"] = ""
}

// ================= YouTube (slot xingguo) =================
//
// InnerTube protocol per youtube.py: WEB client for browse/search/
// detail, VISIONOS player for the HLS line, ANDROID_VR player for
// the direct-format line. hl/gl defaults confirmed in py _lazy():
// hl="zh-CN", gl="US".

const ytAPI = "https://www.youtube.com/youtubei/v1/"
const ytKeyWeb = "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8"
const ytKeyAnd = "AIzaSyA8eiZmM1FaDVjRy-df2KTyQ_vz_yYM39w"

const ytUAWeb = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
const ytUAVis = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
const ytUAVR = "com.google.android.apps.youtube.vr.oculus/1.65.10 (Linux; U; Android 12L; eureka-user Build/SQ3A.220605.009.A1) gzip"

const ytShortsSP = "EgIYAQ%3D%3D"

var ytClients = map[string]map[string]interface{}{
	"WEB":        {"clientName": "WEB", "clientVersion": "2.20241127.01.00", "userAgent": ytUAWeb},
	"VISIONOS":   {"clientName": "VISIONOS", "clientVersion": "1.02", "deviceMake": "Apple", "deviceModel": "Apple Vision Pro", "userAgent": ytUAVis, "osName": "visionOS", "osVersion": "2.0"},
	"ANDROID_VR": {"clientName": "ANDROID_VR", "clientVersion": "1.65.10", "deviceMake": "Oculus", "deviceModel": "Quest 3", "androidSdkVersion": 32, "userAgent": ytUAVR, "osName": "Android", "osVersion": "12L"},
}
var ytClientNum = map[string]string{"WEB": "1", "VISIONOS": "101", "ANDROID_VR": "28"}
var ytClientKey = map[string]string{"WEB": ytKeyWeb, "VISIONOS": ytKeyWeb, "ANDROID_VR": ytKeyAnd}

// Categories = py homeContent CLASSES (site guide 1:1; login-only
// sections are not in the py source either).
var ytCats = []struct{ id, name string }{
	{"home", "首页"},
	{"shorts", "Shorts"},
	{"channel:UC-9-kyTW8ZkZNDHQJ6FgpwQ", "音乐"},
	{"browse:FEstorefront", "影视"},
	{"channel:UC4R8DWoMoI7CAwX8_LjQHig", "直播"},
	{"channel:UCOpNcN46UbXVtpKMrmU4Abg", "游戏"},
	{"browse:FEpodcasts_destination|qgcCCAE%3D", "播客"},
	{"preset:新闻", "新闻"},
	{"preset:体育", "体育"},
	{"preset:时尚", "时尚"},
	{"preset:学习", "学习"},
}

func ytCatName(id string) string {
	for _, c := range ytCats {
		if c.id == id {
			return c.name
		}
	}
	return "YouTube"
}

var ytVidRe = regexp.MustCompile(`^[\w-]{11}$`)
var ytDurRe = regexp.MustCompile(`^\d{1,2}:\d{2}(:\d{2})?$`)
var ytShortsSuffixRe = regexp.MustCompile(`\s*[-–]\s*(play Short|播放 Short).*$`)

var ytVideoKinds = []string{"videoRenderer", "gridVideoRenderer", "compactVideoRenderer", "videoCardRenderer", "reelItemRenderer", "tileRenderer", "gridMovieRenderer", "lockupViewModel"}
var ytShortsKinds = []string{"shortsLockupViewModel", "reelItemRenderer"}

func ytCall(path, clientName string, extra map[string]interface{}) (map[string]interface{}, error) {
	client := map[string]interface{}{}
	for k, v := range ytClients[clientName] {
		client[k] = v
	}
	client["hl"] = "zh-CN"
	client["gl"] = "US"
	body := map[string]interface{}{"context": map[string]interface{}{"client": client}}
	for k, v := range extra {
		body[k] = v
	}
	data, _ := json.Marshal(body)
	u := ytAPI + path + "?key=" + ytClientKey[clientName] + "&prettyPrint=false"
	headers := map[string]string{
		"Content-Type":             "application/json",
		"User-Agent":               ytClients[clientName]["userAgent"].(string),
		"X-YouTube-Client-Name":    ytClientNum[clientName],
		"X-YouTube-Client-Version": ytClients[clientName]["clientVersion"].(string),
	}
	var lastErr error
	for i := 0; i < 2; i++ {
		raw, code, _, err := httpDo("POST", u, bytes.NewReader(data), headers)
		if err == nil && code == 200 {
			var out map[string]interface{}
			if jerr := json.Unmarshal(raw, &out); jerr == nil {
				return out, nil
			} else {
				lastErr = jerr
			}
		} else if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("innertube http %d", code)
		}
		time.Sleep(600 * time.Millisecond)
	}
	return nil, lastErr
}

func ytPlayer(vid, clientName string) (map[string]interface{}, error) {
	return ytCall("player", clientName, map[string]interface{}{
		"videoId":         vid,
		"playbackContext": map[string]interface{}{"contentPlaybackContext": map[string]interface{}{"signatureTimestamp": 20073}},
	})
}

// ytText renders an InnerTube text node (simpleText / content / runs).
func ytText(node interface{}) string {
	switch t := node.(type) {
	case string:
		return t
	case map[string]interface{}:
		if s, ok := t["simpleText"].(string); ok && s != "" {
			return s
		}
		if s, ok := t["content"].(string); ok && s != "" {
			return s
		}
		var b strings.Builder
		for _, r := range g6List(t["runs"]) {
			if rm, ok := r.(map[string]interface{}); ok {
				if s, ok := rm["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	}
	return ""
}

func ytThumb(sources []interface{}) string {
	best, bw := "", -1.0
	for _, t := range sources {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		u, _ := tm["url"].(string)
		if u == "" {
			continue
		}
		w, _ := tm["width"].(float64)
		if w >= bw {
			best, bw = u, w
		}
	}
	return best
}

func ytSubtreeVid(sub map[string]interface{}) string {
	found := ""
	g6Walk(sub, func(n map[string]interface{}) {
		if found != "" {
			return
		}
		if we, ok := n["watchEndpoint"].(map[string]interface{}); ok {
			if id, _ := we["videoId"].(string); ytVidRe.MatchString(id) {
				found = id
				return
			}
		}
		for _, k := range []string{"videoId", "contentId"} {
			if s, ok := n[k].(string); ok && ytVidRe.MatchString(s) {
				found = s
				return
			}
		}
	})
	return found
}

type ytItem struct{ id, title, pic, remarks string }

func ytItemFrom(kind string, sub map[string]interface{}) (ytItem, bool) {
	var it ytItem
	switch kind {
	case "lockupViewModel":
		if ct, _ := sub["contentType"].(string); ct != "" && ct != "LOCKUP_CONTENT_TYPE_VIDEO" {
			return it, false
		}
		vid, _ := sub["contentId"].(string)
		if !ytVidRe.MatchString(vid) {
			return it, false
		}
		it.id = vid
		meta := g6Map(g6Map(sub["metadata"])["lockupMetadataViewModel"])
		it.title = ytText(meta["title"])
		img := g6Map(sub["contentImage"])
		var pics []interface{}
		if tv := g6Map(img["thumbnailViewModel"]); tv != nil {
			pics = g6List(g6Map(tv["image"])["sources"])
		}
		if len(pics) == 0 {
			pv := g6Map(g6Map(img["collectionThumbnailViewModel"])["primaryThumbnail"])
			pics = g6List(g6Map(g6Map(pv["thumbnailViewModel"])["image"])["sources"])
		}
		it.pic = ytThumb(pics)
		g6Walk(img, func(n map[string]interface{}) {
			if it.remarks != "" {
				return
			}
			if t, ok := n["text"]; ok {
				if s := ytText(t); ytDurRe.MatchString(strings.TrimSpace(s)) {
					it.remarks = strings.TrimSpace(s)
				}
			}
		})
		if it.remarks == "" {
			parts := []string{}
			for _, row := range g6List(meta["metadataRows"]) {
				for _, p := range g6List(g6Map(row)["metadataParts"]) {
					if s := ytText(g6Map(p)["text"]); s != "" {
						parts = append(parts, s)
					}
				}
			}
			it.remarks = strings.Join(parts, " · ")
			if len([]rune(it.remarks)) > 40 {
				it.remarks = string([]rune(it.remarks)[:40])
			}
		}
	case "shortsLockupViewModel":
		vid := ytSubtreeVid(sub)
		if vid == "" {
			return it, false
		}
		it.id = vid
		title, _ := sub["accessibilityText"].(string)
		title = strings.TrimSpace(ytShortsSuffixRe.ReplaceAllString(title, ""))
		if title == "" {
			title = ytText(sub["title"])
		}
		it.title = title
		it.pic = ytThumb(g6List(g6Map(sub["thumbnail"])["sources"]))
		it.remarks = "Shorts"
	default:
		vid, _ := sub["videoId"].(string)
		if !ytVidRe.MatchString(vid) {
			vid = ytSubtreeVid(sub)
		}
		if !ytVidRe.MatchString(vid) {
			return it, false
		}
		it.id = vid
		it.title = ytText(sub["title"])
		if it.title == "" {
			it.title = ytText(sub["headline"])
		}
		it.pic = ytThumb(g6List(g6Map(sub["thumbnail"])["thumbnails"]))
		it.remarks = ytText(sub["lengthText"])
		badges, _ := json.Marshal(sub["badges"])
		owner := ytText(sub["ownerText"])
		vc := ytText(sub["viewCountText"])
		if vc == "" {
			vc = ytText(sub["shortViewCountText"])
		}
		if strings.Contains(string(badges), "LIVE") || strings.Contains(string(badges), "直播") || strings.Contains(vc, "正在观看") {
			it.remarks = "直播"
		} else if it.remarks == "" && (vc != "" || owner != "") {
			parts := []string{}
			if owner != "" {
				parts = append(parts, owner)
			}
			if vc != "" {
				parts = append(parts, vc)
			}
			it.remarks = strings.Join(parts, " · ")
			if len([]rune(it.remarks)) > 40 {
				it.remarks = string([]rune(it.remarks)[:40])
			}
		}
	}
	if it.id == "" || strings.TrimSpace(it.title) == "" {
		return it, false
	}
	it.title = strings.TrimSpace(it.title)
	if it.pic == "" {
		it.pic = "https://i.ytimg.com/vi/" + it.id + "/hqdefault.jpg"
	}
	return it, true
}

func ytHarvest(data map[string]interface{}, kinds []string) []ytItem {
	out := []ytItem{}
	seen := map[string]bool{}
	var rec func(node interface{})
	rec = func(node interface{}) {
		switch t := node.(type) {
		case map[string]interface{}:
			for _, k := range kinds {
				if sub, ok := t[k].(map[string]interface{}); ok {
					if it, ok := ytItemFrom(k, sub); ok && !seen[it.id] {
						seen[it.id] = true
						out = append(out, it)
					}
				}
			}
			for _, v := range t {
				rec(v)
			}
		case []interface{}:
			for _, v := range t {
				rec(v)
			}
		}
	}
	rec(data)
	return out
}

func ytContinuation(data map[string]interface{}) string {
	cirToks := []string{}
	g6Walk(data, func(n map[string]interface{}) {
		if cir, ok := n["continuationItemRenderer"].(map[string]interface{}); ok {
			tok := g6Str(g6Map(g6Map(cir["continuationEndpoint"])["continuationCommand"])["token"])
			if tok != "" {
				cirToks = append(cirToks, tok)
			}
		}
	})
	if len(cirToks) > 0 {
		return cirToks[len(cirToks)-1]
	}
	toks := []string{}
	g6Walk(data, func(n map[string]interface{}) {
		if cc, ok := n["continuationCommand"].(map[string]interface{}); ok {
			if tok, _ := cc["token"].(string); tok != "" {
				toks = append(toks, tok)
			}
		}
	})
	if len(toks) > 0 {
		return toks[len(toks)-1]
	}
	return ""
}

// Continuation stream state (py _streams): per stream key, the mode
// plus page->token map and a seen set for cross-page dedupe.
type ytMode struct{ kind, a, b string }

type ytStream struct {
	mode   ytMode
	tokens map[int]string
	seen   map[string]bool
}

var ytMu sync.Mutex
var ytStreams = map[string]*ytStream{}

func ytModeOf(tid string) ytMode {
	switch {
	case tid == "home":
		return ytMode{kind: "home"}
	case tid == "shorts":
		return ytMode{kind: "shorts"}
	case strings.HasPrefix(tid, "channel:"):
		return ytMode{kind: "browse", a: strings.TrimPrefix(tid, "channel:")}
	case strings.HasPrefix(tid, "browse:"):
		rest := strings.TrimPrefix(tid, "browse:")
		if i := strings.Index(rest, "|"); i >= 0 {
			return ytMode{kind: "browse", a: rest[:i], b: rest[i+1:]}
		}
		return ytMode{kind: "browse", a: rest}
	case strings.HasPrefix(tid, "preset:"):
		return ytMode{kind: "search", a: strings.TrimPrefix(tid, "preset:")}
	default:
		return ytMode{kind: "search", a: tid}
	}
}

func ytFetchPage(key string, mode ytMode, pg int) ([]ytItem, bool) {
	ytMu.Lock()
	defer ytMu.Unlock()
	return ytFetchLocked(key, mode, pg)
}

func ytFetchLocked(key string, mode ytMode, pg int) ([]ytItem, bool) {
	st := ytStreams[key]
	if st == nil {
		st = &ytStream{mode: mode, tokens: map[int]string{}, seen: map[string]bool{}}
		ytStreams[key] = st
	}
	mode = st.mode
	token, has := st.tokens[pg]
	if pg > 1 && !has {
		return nil, false
	}
	var data map[string]interface{}
	var err error
	var items []ytItem
	switch mode.kind {
	case "home":
		extra := map[string]interface{}{"browseId": "FEwhat_to_watch"}
		if token != "" {
			extra = map[string]interface{}{"continuation": token}
		}
		data, err = ytCall("browse", "WEB", extra)
		if err != nil {
			return nil, false
		}
		items = ytHarvest(data, ytVideoKinds)
		if len(items) == 0 && pg == 1 {
			// Anonymous home is often empty: fall back to the 热门
			// search stream (py behaviour).
			st.mode = ytMode{kind: "search", a: "热门"}
			return ytFetchLocked(key, st.mode, 1)
		}
	case "browse":
		extra := map[string]interface{}{"browseId": mode.a}
		if mode.b != "" {
			extra["params"] = mode.b
		}
		if token != "" {
			extra = map[string]interface{}{"continuation": token}
		}
		data, err = ytCall("browse", "WEB", extra)
		if err != nil {
			return nil, false
		}
		items = ytHarvest(data, ytVideoKinds)
	case "search":
		extra := map[string]interface{}{"query": mode.a}
		if mode.b != "" {
			extra["params"] = mode.b
		}
		if token != "" {
			extra = map[string]interface{}{"continuation": token}
		}
		data, err = ytCall("search", "WEB", extra)
		if err != nil {
			return nil, false
		}
		items = ytHarvest(data, ytVideoKinds)
	case "shorts":
		extra := map[string]interface{}{"query": "热门", "params": ytShortsSP}
		if token != "" {
			extra = map[string]interface{}{"continuation": token}
		}
		data, err = ytCall("search", "WEB", extra)
		if err != nil {
			return nil, false
		}
		items = ytHarvest(data, ytShortsKinds)
		if len(items) == 0 {
			for _, it := range ytHarvest(data, ytVideoKinds) {
				if it.remarks == "Shorts" {
					items = append(items, it)
				}
			}
		}
	default:
		return nil, false
	}
	nxt := ytContinuation(data)
	if nxt != "" {
		st.tokens[pg+1] = nxt
	}
	fresh := []ytItem{}
	for _, it := range items {
		if !st.seen[it.id] {
			st.seen[it.id] = true
			fresh = append(fresh, it)
		}
	}
	return fresh, nxt != ""
}

func ytItemsToDramas(slot string, items []ytItem, catName string) []interface{} {
	out := []interface{}{}
	for _, it := range items {
		out = append(out, makeDrama(slot, it.id, it.title, it.pic, it.remarks, catName, ""))
	}
	return out
}

func ytCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range ytCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func ytCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "home"
	}
	if page < 1 {
		page = 1
	}
	items, hasNext := ytFetchPage("cat:"+category, ytModeOf(category), page)
	total := len(items)
	if hasNext {
		total = 999999
	}
	return ytItemsToDramas(slot, items, ytCatName(category)), total, hasNext, nil
}

func ytSearch(slot, kw string) ([]interface{}, error) {
	items, _ := ytFetchPage("search:"+kw, ytMode{kind: "search", a: kw}, 1)
	return ytItemsToDramas(slot, items, "搜索"), nil
}

func ytDetail(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	title := vid
	author, desc, pic := "", "", "https://i.ytimg.com/vi/"+vid+"/hqdefault.jpg"
	remarks := ""
	if r, err := ytPlayer(vid, "WEB"); err == nil {
		vd := g6Map(r["videoDetails"])
		if vd != nil {
			if t, _ := vd["title"].(string); t != "" {
				title = t
			}
			author, _ = vd["author"].(string)
			desc, _ = vd["shortDescription"].(string)
			if pics := g6List(g6Map(vd["thumbnail"])["thumbnails"]); len(pics) > 0 {
				if p := ytThumb(pics); p != "" {
					pic = p
				}
			}
			if ls := g6Str(vd["lengthSeconds"]); ls != "" {
				if n, err := strconv.Atoi(ls); err == nil && n > 0 {
					remarks = fmt.Sprintf("%d:%02d", n/60, n%60)
				}
			}
			if live, _ := vd["isLiveContent"].(bool); live {
				remarks = "直播"
			}
		}
	}
	if title == vid {
		// oembed fallback (py behaviour).
		u := "https://www.youtube.com/oembed?url=https://www.youtube.com/watch?v=" + vid + "&format=json"
		if body, code, _, err := httpGet(u, map[string]string{"User-Agent": ytUAWeb}); err == nil && code == 200 {
			var oe map[string]interface{}
			if json.Unmarshal(body, &oe) == nil {
				if t, _ := oe["title"].(string); t != "" {
					title = t
				}
				if a, _ := oe["author_name"].(string); a != "" {
					author = a
				}
				if p, _ := oe["thumbnail_url"].(string); p != "" {
					pic = p
				}
			}
		}
	}
	if len([]rune(desc)) > 800 {
		desc = string([]rune(desc)[:800])
	}
	if author != "" {
		desc = "作者：" + author + "\n" + desc
	}
	return makeDrama(slot, vid, title, pic, remarks, "YouTube", desc), nil
}

func ytResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	// Line 1 (py flag != 360p直链）: VISIONOS HLS manifest.
	if r, err := ytPlayer(vid, "VISIONOS"); err == nil {
		if hls := g6Str(g6Map(r["streamingData"])["hlsManifestUrl"]); hls != "" {
			res := playResult(hls, slot, vid, ytUAVis, "", "m3u8", "hls")
			g6NoReferer(res)
			return res, nil
		}
	}
	// Line 2: ANDROID_VR direct formats (itag 22 > 18 > others),
	// then its HLS manifest.
	if r, err := ytPlayer(vid, "ANDROID_VR"); err == nil {
		sd := g6Map(r["streamingData"])
		type fmtEntry struct {
			url  string
			itag float64
		}
		fmts := []fmtEntry{}
		for _, f := range g6List(sd["formats"]) {
			fm := g6Map(f)
			if u, _ := fm["url"].(string); u != "" {
				itag, _ := fm["itag"].(float64)
				fmts = append(fmts, fmtEntry{u, itag})
			}
		}
		sort.SliceStable(fmts, func(i, j int) bool {
			rank := func(t float64) int {
				if t == 22 {
					return 0
				}
				if t == 18 {
					return 1
				}
				return 2
			}
			return rank(fmts[i].itag) < rank(fmts[j].itag)
		})
		if len(fmts) > 0 {
			u := fmts[0].url
			format, proto := "mp4", "file"
			if strings.Contains(u, ".m3u8") {
				format, proto = "m3u8", "hls"
			}
			res := playResult(u, slot, vid, ytUAVR, "", format, proto)
			g6NoReferer(res)
			return res, nil
		}
		if hls := g6Str(sd["hlsManifestUrl"]); hls != "" {
			res := playResult(hls, slot, vid, ytUAVR, "", "m3u8", "hls")
			g6NoReferer(res)
			return res, nil
		}
	}
	return nil, fmt.Errorf("youtube: no stream for %s (visitor LOGIN_REQUIRED / region limit)", vid)
}

func ytHeaders() map[string]string {
	return map[string]string{"User-Agent": ytUAWeb, "Referer": "https://www.youtube.com/"}
}

// ================= JavXu (slot wangguo) =================
//
// Per javxu.py + 摸站笔记.md: entry gate is the client-side cookie
// _slide_passed=1; every HTML page is wrapped in a
// document.write(X(unescape(Y("<hex>")))) shell that must be hex
// decoded first; detail pages carry window.videoConfig with the
// signed playergo m3u8 (fetched fresh at resolve time); covers on
// cache-*.sgvafw.com embed the fourhoi original URL as base64 in
// the path and are unwrapped back to the bare original (fourhoi
// rejects any Referer, so covers stay bare URLs).

const jxHost = "https://javxu.com"
const jxUA = "Mozilla/5.0 (Linux; Android 13; SGT-AL10) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

// jxCoverFetch fetches cover bytes the way fourhoi demands: a
// browser UA and NO Referer at all. Verified live 2026-10-23:
// Referer present -> 403, no UA -> 403, UA-only -> 200 image bytes.
// The core's default cover path uses impl.Headers (jxHeaders, which
// carries the site Referer for page fetches), so every JavXu cover
// 403'd in the app ("视频没有封面" device report) until this
// dedicated fetcher was registered.
func jxCoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpGet(u, map[string]string{"User-Agent": jxUA})
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("javxu cover: HTTP %d", code)
	}
	return body, nil
}

func jxHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      jxUA,
		"Referer":         jxHost + "/zh/",
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Cookie":          "_slide_passed=1",
	}
}

var jxCats = []struct{ id, name string }{
	{"home", "首页"},
	{"chinese-subtitle", "中文字幕"},
	{"recent-update", "最近更新"},
	{"new-release", "新发布"},
	{"uncensored", "未审查"},
	{"uncensored-leaked", "泄露未审查"},
	{"genres/vr", "VR"},
	{"actresses_hot", "热门女演员"},
	{"trending", "热门"},
	{"today-hot", "今日热门"},
	{"weekly-hot", "本周热门"},
	{"monthly-hot", "本月热门"},
	{"playlists", "公开片单"},
	{"genres_index", "类型"},
	{"makers_index", "制作商"},
	{"actresses_index", "演员"},
	{"tags/siro", "SIRO"},
	{"tags/259luxu", "LUXU"},
	{"tags/200gana", "200GANA"},
	{"tags/prestige-premium", "PRESTIGE PREMIUM"},
	{"tags/s-cute", "S-CUTE"},
	{"tags/261ara", "ARA"},
	{"tags/fc2", "FC2"},
	{"tags/heyzo", "HEYZO"},
	{"tags/tokyo-hot", "Tokyo-Hot"},
	{"tags/1pondo", "1pondo"},
	{"tags/caribbeancom", "Caribbeancom"},
	{"tags/caribbeancompr", "Caribbeancompr"},
	{"tags/10musume", "10musume"},
	{"tags/pacopacomama", "pacopacomama"},
	{"tags/gachig", "Gachinco"},
	{"tags/xxx-av", "XXX-AV"},
	{"tags/c0930", "C0930"},
	{"tags/h0930", "H0930"},
	{"tags/h4610", "H4610"},
}

var jxListPath = map[string]string{
	"home":              "/zh/",
	"chinese-subtitle":  "/zh/chinese-subtitle",
	"recent-update":     "/zh/recent-update",
	"new-release":       "/zh/new-release",
	"uncensored":        "/zh/uncensored",
	"uncensored-leaked": "/zh/uncensored-leaked",
	"genres/vr":         "/zh/genres/vr",
	"trending":          "/zh/trending",
	"today-hot":         "/zh/today-hot",
	"weekly-hot":        "/zh/weekly-hot",
	"monthly-hot":       "/zh/monthly-hot",
}

func jxCatName(id string) string {
	for _, c := range jxCats {
		if c.id == id {
			return c.name
		}
	}
	return "JavXu"
}

var (
	jxHexRe       = regexp.MustCompile(`\("([0-9a-f]{200,})"`)
	jxPicRe       = regexp.MustCompile(`/base/([A-Za-z0-9+/=]+)\.cache`)
	jxTotalPageRe = regexp.MustCompile(`data-total-pages="(\d+)"`)
	jxThumbRe     = regexp.MustCompile(`<a href="([^"]+)" class="thumb"`)
	jxCoverRe     = regexp.MustCompile(`<img class="preview-cover"[^>]+src="([^"]+)"`)
	jxTitleRe     = regexp.MustCompile(`(?s)<div class="title">(.*?)</div>`)
	jxCodeRe      = regexp.MustCompile(`class="name">([^<]+)</a>`)
	jxDurRe       = regexp.MustCompile(`fa-clock"></i>\s*([^<]+)`)
	jxIdxHrefRe   = regexp.MustCompile(`<a href="([^"]+)"`)
	jxIdxNameRe   = regexp.MustCompile(`<div class="name">([^<]+)</div>`)
	jxIdxRemarkRe = regexp.MustCompile(`<div class="text-muted">([^<]+)</div>`)
	jxIdxImgRe    = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	jxCfgRe       = regexp.MustCompile(`(?s)window\.videoConfig\s*=\s*\{(.*?)\};`)
	jxH2TitleRe   = regexp.MustCompile(`(?s)<h2 class="title">(.*?)</h2>`)
	jxPosterRe    = regexp.MustCompile(`id="player" data-poster="([^"]+)"`)
	jxFieldRe     = regexp.MustCompile(`(?s)<div>\s*<span>([^<]+):</span>\s*<span[^>]*>(.*?)</span>\s*</div>`)
	jxLinkRe      = regexp.MustCompile(`<a[^>]*>([^<]+)</a>`)
)

// jxUnwrapPage decodes the document.write hex shell when present.
func jxUnwrapPage(text string) string {
	head := text
	if len(head) > 300 {
		head = head[:300]
	}
	if strings.Contains(head, "document.write(") {
		if m := jxHexRe.FindStringSubmatch(text); m != nil {
			if raw, err := hex.DecodeString(m[1]); err == nil {
				return string(raw)
			}
		}
	}
	return text
}

// jxUnwrapPic decodes cache-*.sgvafw.com cover URLs back to the
// fourhoi original (the base64 path segment, possibly nested).
func jxUnwrapPic(u string) string {
	u = strings.TrimSpace(u)
	for i := 0; i < 4; i++ {
		m := jxPicRe.FindStringSubmatch(u)
		if m == nil {
			break
		}
		seg := m[1]
		for len(seg)%4 != 0 {
			seg += "="
		}
		dec, err := base64.StdEncoding.DecodeString(seg)
		if err != nil {
			break
		}
		if s := string(dec); strings.HasPrefix(s, "http") {
			u = s
		} else {
			break
		}
	}
	if strings.Contains(u, "placeholder") {
		return ""
	}
	return u
}

func jxFetch(rawURL string) string {
	for i := 0; i < 2; i++ {
		body, code, _, err := httpGet(rawURL, jxHeaders())
		if err == nil && (code == 200 || code == 404) && len(body) > 0 {
			return jxUnwrapPage(string(body))
		}
		time.Sleep(400 * time.Millisecond)
	}
	return ""
}

func jxFetchJSON(rawURL string) map[string]interface{} {
	text := jxFetch(rawURL)
	if text == "" {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil
	}
	return out
}

func jxPageURL(path string, pg int) string {
	if pg <= 1 {
		return jxHost + path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%s%spage=%d", jxHost, path, sep, pg)
}

func jxTotalPages(htmlText string) int {
	if m := jxTotalPageRe.FindStringSubmatch(htmlText); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

type jxCard struct{ id, title, pic, remarks string }

// jxParseCards parses div.movie-item list cards (py parse_cards).
// Card id is the detail path ("/zh/v/<code>").
func jxParseCards(pageHTML string) []jxCard {
	out := []jxCard{}
	seen := map[string]bool{}
	if pageHTML == "" {
		return out
	}
	for _, chunk := range strings.Split(pageHTML, `<div class="movie-item">`)[1:] {
		m := jxThumbRe.FindStringSubmatch(chunk)
		if m == nil {
			continue
		}
		href := html.UnescapeString(m[1])
		if strings.HasPrefix(href, "/v/") {
			href = "/zh" + href
		}
		path := href
		if u, err := url.Parse(href); err == nil {
			path = u.EscapedPath()
		}
		if !strings.HasPrefix(path, "/zh/v/") || seen[path] {
			continue
		}
		seen[path] = true
		pic := ""
		if pm := jxCoverRe.FindStringSubmatch(chunk); pm != nil {
			pic = jxUnwrapPic(html.UnescapeString(pm[1]))
		}
		title := ""
		if tm := jxTitleRe.FindStringSubmatch(chunk); tm != nil {
			title = g6StripTags(tm[1])
		}
		code := path[strings.LastIndex(path, "/")+1:]
		if nm := jxCodeRe.FindStringSubmatch(chunk); nm != nil {
			code = strings.TrimSpace(nm[1])
		}
		dur := ""
		if dm := jxDurRe.FindStringSubmatch(chunk); dm != nil {
			dur = strings.TrimSpace(dm[1])
		}
		if title == "" {
			title = code
		}
		out = append(out, jxCard{id: path, title: title, pic: pic, remarks: dur})
	}
	return out
}

type jxIndexEntry struct{ path, name, pic, remarks string }

// jxParseIndex parses bl-item index entries (genres/makers/actresses).
func jxParseIndex(pageHTML, kind string) []jxIndexEntry {
	out := []jxIndexEntry{}
	seen := map[string]bool{}
	if pageHTML == "" {
		return out
	}
	pat := regexp.MustCompile(`^/zh/` + kind + `/[^"?#]+$`)
	for _, chunk := range strings.Split(pageHTML, "bl-item")[1:] {
		m := jxIdxHrefRe.FindStringSubmatch(chunk)
		if m == nil {
			continue
		}
		href := html.UnescapeString(m[1])
		path := href
		if u, err := url.Parse(href); err == nil {
			path = u.EscapedPath()
		}
		if !pat.MatchString(path) || seen[path] {
			continue
		}
		nm := jxIdxNameRe.FindStringSubmatch(chunk)
		if nm == nil {
			continue
		}
		seen[path] = true
		remarks := ""
		if cm := jxIdxRemarkRe.FindStringSubmatch(chunk); cm != nil {
			remarks = strings.TrimSpace(cm[1])
		}
		pic := ""
		if pm := jxIdxImgRe.FindStringSubmatch(chunk); pm != nil {
			pic = jxUnwrapPic(html.UnescapeString(pm[1]))
			if pic != "" && !strings.HasPrefix(pic, "http") {
				pic = ""
			}
		}
		out = append(out, jxIndexEntry{path: path, name: html.UnescapeString(strings.TrimSpace(nm[1])), pic: pic, remarks: remarks})
	}
	return out
}

// Full genres/makers index lists are single huge pages; cache them
// for 10 minutes like the py _index_cache.
var jxIndexMu sync.Mutex
var jxIndexCache = map[string]struct {
	at    time.Time
	items []jxIndexEntry
}{}

func jxIndexAll(kind string) []jxIndexEntry {
	jxIndexMu.Lock()
	defer jxIndexMu.Unlock()
	if ent, ok := jxIndexCache[kind]; ok && time.Since(ent.at) < 600*time.Second {
		return ent.items
	}
	path := map[string]string{"genres": "/zh/genres", "makers": "/zh/makers"}[kind]
	items := jxParseIndex(jxFetch(jxHost+path), kind)
	jxIndexCache[kind] = struct {
		at    time.Time
		items []jxIndexEntry
	}{time.Now(), items}
	return items
}

func jxCardsToDramas(slot string, cards []jxCard, catName string) []interface{} {
	out := []interface{}{}
	for _, c := range cards {
		out = append(out, makeDrama(slot, c.id, c.title, c.pic, c.remarks, catName, ""))
	}
	return out
}

func jxCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range jxCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func jxCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "home"
	}
	if page < 1 {
		page = 1
	}
	catName := jxCatName(category)

	// Single-page full indexes, locally paged at 100/page (py).
	if category == "genres_index" || category == "makers_index" {
		kind := "genres"
		if category == "makers_index" {
			kind = "makers"
		}
		all := jxIndexAll(kind)
		const per = 100
		pc := (len(all) + per - 1) / per
		if pc < 1 {
			pc = 1
		}
		start := (page - 1) * per
		items := []interface{}{}
		if start < len(all) {
			end := start + per
			if end > len(all) {
				end = len(all)
			}
			for _, e := range all[start:end] {
				items = append(items, makeDrama(slot, "idx:"+e.path, e.name, e.pic, e.remarks, catName, ""))
			}
		}
		return items, len(all), page < pc, nil
	}

	// Paginated actress index (folder cards).
	if category == "actresses_index" || category == "actresses_hot" {
		path := "/zh/actresses"
		if category == "actresses_hot" {
			path = "/zh/actresses?sort=most_viewed_today"
		}
		pageHTML := jxFetch(jxPageURL(path, page))
		entries := jxParseIndex(pageHTML, "actresses")
		items := []interface{}{}
		for _, e := range entries {
			d := makeDrama(slot, "idx:"+e.path, e.name, e.pic, e.remarks, catName, "")
			d["category"] = "idx:" + e.path
			items = append(items, d)
		}
		pc := jxTotalPages(pageHTML)
		hasMore := false
		if pc > 0 {
			hasMore = page < pc
		} else {
			hasMore = len(entries) >= 24
		}
		total := len(items)
		if pc > 0 {
			total = pc * 24
		}
		return items, total, hasMore, nil
	}

	// Public playlists via the member-api (folder cards).
	if category == "playlists" {
		u := fmt.Sprintf("%s/member-api/playlists/public?page=%d&limit=20&sort=latest", jxHost, page)
		data := jxFetchJSON(u)
		d := g6Map(g6Map(data)["data"])
		items := []interface{}{}
		for _, it := range g6List(d["items"]) {
			im := g6Map(it)
			pid := g6Str(im["id"])
			if pid == "" {
				continue
			}
			name := strings.TrimSpace(g6Str(im["name"]))
			if name == "" {
				name = "片单 " + pid
			}
			pic := ""
			if previews := g6List(im["preview_items"]); len(previews) > 0 {
				pic = jxUnwrapPic(g6Str(g6Map(previews[0])["thumbnail_loc"]))
			}
			remarks := g6Str(im["video_count"]) + " 部影片"
			if un := g6Str(im["username"]); un != "" {
				remarks += " · " + un
			}
			items = append(items, makeDrama(slot, "idx:playlist:"+pid, name, pic, remarks, catName, ""))
		}
		tp := int(0)
		if f, ok := d["totalPages"].(float64); ok {
			tp = int(f)
		}
		total := len(items)
		if f, ok := d["total"].(float64); ok && f > 0 {
			total = int(f)
		}
		return items, total, tp > 0 && page < tp, nil
	}

	// A folder target passed as category: serve its video list.
	if strings.HasPrefix(category, "idx:") {
		cards := jxFolderCards(category[4:])
		return jxCardsToDramas(slot, cards, catName), len(cards), false, nil
	}

	// Direct video-list categories.
	path, ok := jxListPath[category]
	if !ok {
		if strings.Contains(category, "/") {
			path = "/zh/" + category
		} else {
			path = "/zh/"
		}
	}
	pageHTML := jxFetch(jxPageURL(path, page))
	cards := jxParseCards(pageHTML)
	if category == "home" {
		return jxCardsToDramas(slot, cards, catName), len(cards), false, nil
	}
	per := 20
	if strings.Contains(path, "/actresses/") {
		per = 12
	}
	pc := jxTotalPages(pageHTML)
	hasMore := false
	total := 999999
	if pc > 0 {
		hasMore = page < pc
		total = pc * per
	} else {
		hasMore = len(cards) >= per
	}
	return jxCardsToDramas(slot, cards, catName), total, hasMore, nil
}

// jxFolderCards fetches page 1 of a folder target (an index-entry
// path like /zh/genres/<name>, or "playlist:<id>") as video cards.
func jxFolderCards(target string) []jxCard {
	if strings.HasPrefix(target, "playlist:") {
		pid := strings.TrimPrefix(target, "playlist:")
		u := fmt.Sprintf("%s/member-api/playlists/videos/list?id=%s&page=1&limit=20", jxHost, url.QueryEscape(pid))
		data := jxFetchJSON(u)
		d := g6Map(g6Map(data)["data"])
		out := []jxCard{}
		for _, it := range g6List(d["items"]) {
			im := g6Map(it)
			href := g6Str(im["href"])
			if strings.HasPrefix(href, "/v/") {
				href = "/zh" + href
			}
			path := href
			if pu, err := url.Parse(href); err == nil {
				path = pu.EscapedPath()
			}
			if !strings.HasPrefix(path, "/zh/v/") {
				rid := g6Str(im["resource_id"])
				if rid == "" {
					continue
				}
				path = "/zh/v/" + rid
			}
			title := g6Str(im["title"])
			if title == "" {
				title = path
			}
			out = append(out, jxCard{id: path, title: title, pic: jxUnwrapPic(g6Str(im["thumbnail_loc"])), remarks: g6Str(im["duration"])})
		}
		return out
	}
	pageHTML := jxFetch(jxPageURL(target, 1))
	return jxParseCards(pageHTML)
}

func jxDetail(slot, rawID string) (map[string]interface{}, error) {
	// Folder detail: chapters are the folder's videos.
	if strings.HasPrefix(rawID, "idx:") {
		target := rawID[4:]
		if strings.Contains(target, "/actresses/") {
			// v6: actress folders open as card lists (catalog drill
			// category = this rawID), not episode-embedded details.
			cards := jxFolderCards(target)
			title := target
			if seg := target[strings.LastIndex(target, "/")+1:]; seg != "" {
				if un, err := url.PathUnescape(seg); err == nil {
					title = un
				} else {
					title = seg
				}
			}
			cover := ""
			if len(cards) > 0 {
				cover = cards[0].pic
			}
			if st := getStore(slot).findDrama(rawID); st != nil {
				if t, _ := st["title"].(string); t != "" {
					title = t
				}
				if c, _ := st["cover"].(string); c != "" {
					cover = c
				}
			}
			d := makeDrama(slot, rawID, title, cover, "女优作品", "女优索引", "作品请通过分类入口打开（文件夹卡片列表）")
			d["category"] = rawID
			g2ZeroEpisodes(d)
			return d, nil
		}
		cards := jxFolderCards(target)
		if len(cards) == 0 {
			return nil, fmt.Errorf("javxu folder empty: %s", target)
		}
		chaps := []g6ChapSpec{}
		for _, c := range cards {
			chaps = append(chaps, g6ChapSpec{raw: c.id, title: c.title})
		}
		title := target
		if strings.HasPrefix(target, "playlist:") {
			title = "片单 " + strings.TrimPrefix(target, "playlist:")
		} else if seg := target[strings.LastIndex(target, "/")+1:]; seg != "" {
			if un, err := url.PathUnescape(seg); err == nil {
				title = un
			} else {
				title = seg
			}
		}
		// Prefer the catalog card's own title/cover when stored.
		cover := cards[0].pic
		if st := getStore(slot).findDrama(rawID); st != nil {
			if t, _ := st["title"].(string); t != "" {
				title = t
			}
			if c, _ := st["cover"].(string); c != "" {
				cover = c
			}
		}
		return g6FolderDrama(slot, rawID, title, cover, fmt.Sprintf("%d 部", len(chaps)), "片单/索引", chaps), nil
	}

	vid := rawID
	if strings.HasPrefix(vid, "/v/") {
		vid = "/zh" + vid
	}
	pageHTML := jxFetch(jxHost + vid)
	if pageHTML == "" {
		return nil, fmt.Errorf("javxu detail empty")
	}
	cfg := map[string]string{}
	if cm := jxCfgRe.FindStringSubmatch(pageHTML); cm != nil {
		for _, key := range []string{"fanhao", "url", "img", "title"} {
			km := regexp.MustCompile(key + `:\s*"([^"]*)"`).FindStringSubmatch(cm[1])
			if km != nil {
				cfg[key] = html.UnescapeString(km[1])
			}
		}
	}
	title := cfg["title"]
	if title == "" {
		if hm := jxH2TitleRe.FindStringSubmatch(pageHTML); hm != nil {
			title = g6StripTags(hm[1])
		}
	}
	if title == "" {
		title = vid
	}
	pic := ""
	if cfg["img"] != "" {
		pic = jxUnwrapPic(cfg["img"])
	}
	if pic == "" {
		if pm := jxPosterRe.FindStringSubmatch(pageHTML); pm != nil {
			pic = jxUnwrapPic(html.UnescapeString(pm[1]))
		}
	}
	fields := map[string]string{}
	for _, dm := range jxFieldRe.FindAllStringSubmatch(pageHTML, -1) {
		label := strings.TrimSpace(dm[1])
		links := []string{}
		for _, lm := range jxLinkRe.FindAllStringSubmatch(dm[2], -1) {
			links = append(links, g6StripTags(lm[1]))
		}
		if len(links) > 0 {
			fields[label] = strings.Join(links, ", ")
		} else {
			fields[label] = g6StripTags(dm[2])
		}
	}
	info := []string{}
	for _, label := range []string{"代码", "发布日期", "时长", "女演员", "类型", "制作人"} {
		if fields[label] != "" {
			info = append(info, label+": "+fields[label])
		}
	}
	d := makeDrama(slot, rawID, title, pic, fields["时长"], "JavXu", strings.Join(info, " | "))
	return d, nil
}

func jxResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		cards := jxFolderCards(rawID[4:])
		if len(cards) == 0 {
			return nil, fmt.Errorf("javxu folder empty")
		}
		return jxResolve(slot, cards[0].id)
	}
	vid := rawID
	if strings.HasPrefix(vid, "/v/") {
		vid = "/zh" + vid
	}
	pageHTML := jxFetch(jxHost + vid)
	play := ""
	if cm := jxCfgRe.FindStringSubmatch(pageHTML); cm != nil {
		if um := regexp.MustCompile(`url:\s*"([^"]+)"`).FindStringSubmatch(cm[1]); um != nil {
			play = html.UnescapeString(um[1])
		}
	}
	if play == "" {
		return nil, fmt.Errorf("javxu: no play url for %s", vid)
	}
	return playResult(play, slot, rawID, jxUA, jxHost+"/zh/", "m3u8", "hls"), nil
}

func jxSearch(slot, kw string) ([]interface{}, error) {
	path := "/zh/search?keyword=" + url.QueryEscape(kw)
	pageHTML := jxFetch(jxPageURL(path, 1))
	return jxCardsToDramas(slot, jxParseCards(pageHTML), "搜索"), nil
}

// ================= XSZAV (slot faguo) =================
//
// Per xszav.py + 摸站笔记.md: Qwik SSR plain HTML, no shield. Detail
// pages carry <video src="/media/v_{hash}.m3u8">; segment CDNs
// require BOTH Referer and Origin headers, so Resolve overrides
// the playResult headers with the detail-page Referer plus the
// site Origin. Tag folders have no site cover of their own — the
// py source uses the site logo, kept here.

const xsUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
const xsLogo = "https://cn.xsz-av.com/logo.png"

var xsDomains = []string{"https://cn.xsz-av.com", "https://tw.xsz-av.com", "https://en.xsz-av.com"}

var xsCats = []struct{ id, name string }{
	{"japanese", "日本AV"},
	{"uncensored", "无码影片"},
	{"uncensored-leak", "无码流出"},
	{"amateur", "素人"},
	{"chinese", "中国"},
	{"korean", "韩国"},
	{"western", "欧美"},
	{"hanime", "动漫"},
	{"actresses", "女优"},
	{"tags", "标签"},
}

var xsVideoCats = map[string]bool{
	"japanese": true, "uncensored": true, "uncensored-leak": true,
	"amateur": true, "chinese": true, "korean": true, "western": true,
	"hanime": true,
}

func xsCatName(id string) string {
	for _, c := range xsCats {
		if c.id == id {
			return c.name
		}
	}
	return "XSZAV"
}

var xsBaseMu sync.Mutex
var xsBaseCache string
var xsBaseCacheAt time.Time
var xsNegAt time.Time

// v3 time/cache budgets (diag §5.2 B4/B5): the v2 probe could stall a
// single fetch for ~120s (3 serial probes + fetch + 2 rotations, all
// at the global 20s timeout, repeated on every call), which starved
// the Dart loadCategories chain (cached -> categories) and made even
// the static category tabs disappear on bad networks.
const (
	xsBaseTTL     = 300 * time.Second // positive base cache lifetime
	xsNegTTL      = 15 * time.Second  // all-domains-down negative cache
	xsReqTimeout  = 10 * time.Second  // per-request timeout (py: 15s, retries incl.)
	xsFetchBudget = 25 * time.Second  // total budget of one xsFetch walk
)

func xsHeadersFor(base string) map[string]string {
	return map[string]string{
		"User-Agent": xsUA,
		"Referer":    base + "/",
		"Accept":     "text/html,application/xhtml+xml,*/*",
	}
}

// xsHeaders serves the cover/health-check paths. v3 (diag D2): it
// must NOT trigger a full probe — use the cached base when present,
// else the first candidate domain.
func xsHeaders() map[string]string {
	if b := xsCachedBase(); b != "" {
		return xsHeadersFor(b)
	}
	return xsHeadersFor(xsDomains[0])
}

func xsCachedBase() string {
	xsBaseMu.Lock()
	defer xsBaseMu.Unlock()
	if xsBaseCache != "" && time.Since(xsBaseCacheAt) < xsBaseTTL {
		return xsBaseCache
	}
	return ""
}

func xsSetBase(d string) {
	xsBaseMu.Lock()
	xsBaseCache = d
	xsBaseCacheAt = time.Now()
	xsBaseMu.Unlock()
}

func xsBaseSoft() string {
	if b := xsCachedBase(); b != "" {
		return b
	}
	return xsDomains[0]
}

var xsBlockMarkers = []string{"just a moment", "attention required", "challenges.cloudflare.com"}

func xsBlockedBody(body string) bool {
	low := strings.ToLower(body)
	for _, m := range xsBlockMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// xsGet is a single GET with the XSZAV per-request timeout (the
// shared httpClient's 20s cap still applies as an outer bound).
func xsGet(rawurl string, headers map[string]string) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), xsReqTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", rawurl, nil)
	if err != nil {
		return "", 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(b), resp.StatusCode, nil
}

// xsBase resolves the serving domain: fresh positive cache first,
// then a concurrent soft probe (py parity: status<500 + non-empty,
// block pages rejected). v3 changes vs v2: the content-marker check
// is no longer a hard gate on the homepage, an all-down probe does
// NOT pin domains[0] into the positive cache (only a 15s negative
// stamp), and probing is concurrent so it costs at most one request
// timeout in total.
func xsBase() string {
	if b := xsCachedBase(); b != "" {
		return b
	}
	xsBaseMu.Lock()
	neg := !xsNegAt.IsZero() && time.Since(xsNegAt) < xsNegTTL
	xsBaseMu.Unlock()
	if neg {
		return xsDomains[0]
	}
	type probeRes struct {
		d  string
		ok bool
	}
	ch := make(chan probeRes, len(xsDomains))
	for _, d := range xsDomains {
		go func(d string) {
			body, code, err := xsGet(d+"/", xsHeadersFor(d))
			ch <- probeRes{d, err == nil && code < 500 && len(body) > 0 && !xsBlockedBody(body)}
		}(d)
	}
	got := map[string]bool{}
	for range xsDomains {
		r := <-ch
		if r.ok {
			got[r.d] = true
		}
	}
	for _, d := range xsDomains {
		if got[d] {
			xsSetBase(d)
			return d
		}
	}
	xsBaseMu.Lock()
	xsNegAt = time.Now()
	xsBaseMu.Unlock()
	return xsDomains[0]
}

func xsCandidates() []string {
	out := []string{}
	if b := xsCachedBase(); b != "" {
		out = append(out, b)
	}
	for _, d := range xsDomains {
		dup := false
		for _, x := range out {
			if x == d {
				dup = true
			}
		}
		if !dup {
			out = append(out, d)
		}
	}
	return out
}

// xsFetch walks the candidate domains the way py http_get does — the
// unit of judgement is each content-page request, not a homepage
// probe. A response counts only when it is a real page: status<400,
// non-empty, no CF block markers, and (when accept is given) matching
// the page-type check, so a 200 soft-block page can no longer parse
// to a fake empty list. Up to 2 tries per candidate (transport/5xx
// only), 25s total budget. ok=false means every candidate failed;
// callers surface that as an error state, while a real page with zero
// cards stays an ok empty result (error grading, diag §5.2 C).
func xsFetch(path string, accept func(string) bool) (string, string, bool) {
	acceptable := func(body string, code int, err error) bool {
		if err != nil || code >= 400 || len(body) == 0 || xsBlockedBody(body) {
			return false
		}
		return accept == nil || accept(body)
	}
	if strings.HasPrefix(path, "http") {
		body, code, err := xsGet(path, xsHeaders())
		if acceptable(body, code, err) {
			return body, xsBaseSoft(), true
		}
		return "", xsBaseSoft(), false
	}
	deadline := time.Now().Add(xsFetchBudget)
	for _, d := range xsCandidates() {
		for attempt := 0; attempt < 2; attempt++ {
			if time.Now().After(deadline) {
				return "", xsBaseSoft(), false
			}
			body, code, err := xsGet(d+path, xsHeadersFor(d))
			if acceptable(body, code, err) {
				xsSetBase(d)
				return body, d, true
			}
			if err == nil && code < 500 && code != 429 {
				break // definitive answer (e.g. block page): next candidate
			}
		}
	}
	xsBaseMu.Lock()
	xsNegAt = time.Now()
	xsBaseMu.Unlock()
	return "", xsBaseSoft(), false
}

// Page-type accept checks for xsFetch (diag B3): a list page counts
// as real when it yields cards or still carries the list scaffolding
// (a genuinely empty category keeps its thumbnail/pagination shell).
func xsAcceptVideos(body string) bool {
	return len(xsParseVideos(body)) > 0 ||
		strings.Contains(body, `class="thumbnail"`) ||
		strings.Contains(body, "?page=") || strings.Contains(body, "&page=")
}

func xsAcceptActresses(body string) bool {
	return xsActressRe.MatchString(body) || strings.Contains(body, "/actresses/")
}

func xsAcceptTags(body string) bool {
	return xsTagRe.MatchString(body) || strings.Contains(body, "text-orange")
}

var (
	xsVideoIDRe    = regexp.MustCompile(`href="/video/(\d+)"`)
	xsVideoPicRe   = regexp.MustCompile(`<img[^>]+data-src="(https://img[^"]+)"`)
	xsVideoAltRe   = regexp.MustCompile(`<img[^>]+alt="([^"]*)"`)
	xsVideoDurRe   = regexp.MustCompile(`>\s*(\d{1,2}:\d{2}(?::\d{2})?)\s*</span>`)
	xsActressRe    = regexp.MustCompile(`(?s)href="(/actresses/[^"]+)"[^>]*>\s*<div class="mx-auto[^"]*"[^>]*>\s*<img[^>]+alt="([^"]*)"[^>]+data-src="(https://img[^"]+)"`)
	xsActCountRe   = regexp.MustCompile(`(\d+)\s*视频`)
	xsActDebutRe   = regexp.MustCompile(`出道:\s*(\d+)`)
	xsTagRe        = regexp.MustCompile(`href="/search/videos/([^"]+)" class="text-orange[^"]*">([^<]+)</a>`)
	xsPageNumRe    = regexp.MustCompile(`[?&]page=(\d+)`)
	xsH1Re         = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	xsDetailPicRe  = regexp.MustCompile(`<img src="(https://img[^"]*w_800[^"]*)"`)
	xsDetailPicRe2 = regexp.MustCompile(`(?s)<video[^>]+>.*?<img src="(https://img[^"]+)"`)
	xsFieldRe      = regexp.MustCompile(`(?s)<span class="mr-1">([^<]+)</span>(.*?)</div>`)
	xsFieldLinkRe  = regexp.MustCompile(`href="[^"]+"[^>]*>([^<]+)</a>`)
	xsPlayRe       = regexp.MustCompile(`src="(/media/v_[a-z0-9]+\.m3u8)"`)
)

func xsPageCount(htmlText string, pg int) int {
	max := pg
	for _, m := range xsPageNumRe.FindAllStringSubmatch(htmlText, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > max {
			max = n
		}
	}
	return max
}

type xsCard struct{ id, title, pic, remarks string }

func xsParseVideos(htmlText string) []xsCard {
	out := []xsCard{}
	seen := map[string]bool{}
	for _, b := range strings.Split(htmlText, `<div class="thumbnail"`)[1:] {
		m := xsVideoIDRe.FindStringSubmatch(b)
		if m == nil {
			continue
		}
		vid := m[1]
		if seen[vid] {
			continue
		}
		seen[vid] = true
		pic := ""
		if mi := xsVideoPicRe.FindStringSubmatch(b); mi != nil {
			pic = mi[1]
		}
		title := ""
		if ma := xsVideoAltRe.FindStringSubmatch(b); ma != nil {
			title = strings.TrimSpace(html.UnescapeString(ma[1]))
		}
		if len([]rune(title)) > 120 {
			title = string([]rune(title)[:120])
		}
		dur := ""
		if md := xsVideoDurRe.FindStringSubmatch(b); md != nil {
			dur = md[1]
		}
		out = append(out, xsCard{id: vid, title: title, pic: pic, remarks: dur})
	}
	return out
}

func xsCardsToDramas(slot string, cards []xsCard, catName string) []interface{} {
	out := []interface{}{}
	for _, c := range cards {
		out = append(out, makeDrama(slot, c.id, c.title, c.pic, c.remarks, catName, ""))
	}
	return out
}

func xsCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range xsCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func xsPageQuery(pg int) string {
	if pg > 1 {
		return fmt.Sprintf("?page=%d", pg)
	}
	return ""
}

func xsCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "japanese"
	}
	if page < 1 {
		page = 1
	}
	catName := xsCatName(category)

	if category == "actresses" {
		pageHTML, _, ok := xsFetch("/actresses"+xsPageQuery(page), xsAcceptActresses)
		if !ok {
			return nil, 0, false, fmt.Errorf("xszav: fetch failed (actresses)")
		}
		items := []interface{}{}
		seen := map[string]bool{}
		for _, m := range xsActressRe.FindAllStringSubmatchIndex(pageHTML, -1) {
			href := pageHTML[m[2]:m[3]]
			name := html.UnescapeString(strings.TrimSpace(pageHTML[m[4]:m[5]]))
			pic := pageHTML[m[6]:m[7]]
			if seen[href] {
				continue
			}
			seen[href] = true
			enc := strings.TrimPrefix(href, "/actresses/")
			tail := ""
			if m[1]+600 <= len(pageHTML) {
				tail = pageHTML[m[1] : m[1]+600]
			} else if m[1] < len(pageHTML) {
				tail = pageHTML[m[1]:]
			}
			remarks := ""
			if cm := xsActCountRe.FindStringSubmatch(tail); cm != nil {
				remarks = cm[1] + " 视频"
			}
			if dm := xsActDebutRe.FindStringSubmatch(tail); dm != nil {
				remarks = strings.Trim((remarks + " · 出道 " + dm[1]), " ·")
			}
			items = append(items, makeDrama(slot, "idx:actress:"+enc, name, pic, remarks, catName, ""))
		}
		pc := xsPageCount(pageHTML, page)
		return items, pc * 24, page < pc, nil
	}

	if category == "tags" {
		pageHTML, _, ok := xsFetch("/tags"+xsPageQuery(page), xsAcceptTags)
		if !ok {
			return nil, 0, false, fmt.Errorf("xszav: fetch failed (tags)")
		}
		items := []interface{}{}
		seen := map[string]bool{}
		for _, m := range xsTagRe.FindAllStringSubmatch(pageHTML, -1) {
			enc, name := m[1], strings.TrimSpace(m[2])
			if seen[enc] || name == "" {
				continue
			}
			seen[enc] = true
			items = append(items, makeDrama(slot, "idx:tag:"+enc, name, xsLogo, "标签", catName, ""))
		}
		pc := xsPageCount(pageHTML, page)
		return items, pc * 36, page < pc, nil
	}

	if strings.HasPrefix(category, "idx:") {
		cards, ok := xsFolderCards(category)
		if !ok {
			return nil, 0, false, fmt.Errorf("xszav: fetch failed (%s)", category)
		}
		return xsCardsToDramas(slot, cards, catName), len(cards), false, nil
	}

	tid := category
	if !xsVideoCats[tid] {
		tid = "japanese"
	}
	pageHTML, _, ok := xsFetch("/videos/"+tid+xsPageQuery(page), xsAcceptVideos)
	if !ok {
		// Fetch failure must surface as an error: Dart shows the
		// error state (retryable) instead of a misleading "no
		// dramas matched" empty state. v1 swallowed this as an
		// empty page; a real page with zero cards stays ok-empty.
		return nil, 0, false, fmt.Errorf("xszav: fetch failed (%s)", tid)
	}
	cards := xsParseVideos(pageHTML)
	pc := xsPageCount(pageHTML, page)
	return xsCardsToDramas(slot, cards, catName), pc * 12, page < pc, nil
}

// xsFolderCards fetches page 1 of a folder target (actress page or
// tag search) as video cards. ok=false means every domain failed
// (error state); ok with zero cards is a real empty page.
func xsFolderCards(rawID string) ([]xsCard, bool) {
	switch {
	case strings.HasPrefix(rawID, "idx:actress:"):
		enc := strings.TrimPrefix(rawID, "idx:actress:")
		pageHTML, _, ok := xsFetch("/actresses/"+enc, xsAcceptVideos)
		if !ok {
			return nil, false
		}
		return xsParseVideos(pageHTML), true
	case strings.HasPrefix(rawID, "idx:tag:"):
		enc := strings.TrimPrefix(rawID, "idx:tag:")
		pageHTML, _, ok := xsFetch("/search/videos/"+enc, xsAcceptVideos)
		if !ok {
			return nil, false
		}
		return xsParseVideos(pageHTML), true
	}
	return nil, true
}

func xsFolderTitle(rawID string) string {
	enc := rawID
	if i := strings.LastIndex(rawID, ":"); i >= 0 {
		enc = rawID[i+1:]
	}
	if un, err := url.PathUnescape(enc); err == nil {
		return un
	}
	return enc
}

func xsDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		cards, ok := xsFolderCards(rawID)
		if !ok {
			return nil, fmt.Errorf("xszav folder fetch failed: %s", rawID)
		}
		if len(cards) == 0 {
			return nil, fmt.Errorf("xszav folder empty: %s", rawID)
		}
		chaps := []g6ChapSpec{}
		for _, c := range cards {
			chaps = append(chaps, g6ChapSpec{raw: c.id, title: c.title})
		}
		title := xsFolderTitle(rawID)
		cover := cards[0].pic
		if st := getStore(slot).findDrama(rawID); st != nil {
			if t, _ := st["title"].(string); t != "" {
				title = t
			}
			if c, _ := st["cover"].(string); c != "" {
				cover = c
			}
		}
		return g6FolderDrama(slot, rawID, title, cover, fmt.Sprintf("%d 部", len(chaps)), "女优/标签", chaps), nil
	}

	vid := rawID
	if strings.Contains(vid, "/") {
		parts := strings.Split(strings.TrimSuffix(vid, "/"), "/")
		vid = parts[len(parts)-1]
	}
	if _, err := strconv.Atoi(vid); err != nil {
		return nil, fmt.Errorf("xszav: bad video id %q", rawID)
	}
	pageHTML, _, ok := xsFetch("/video/"+vid, nil)
	if !ok {
		return nil, fmt.Errorf("xszav detail fetch failed")
	}
	title := ""
	if mh := xsH1Re.FindStringSubmatch(pageHTML); mh != nil {
		title = g6StripTags(mh[1])
	}
	pic := ""
	if mp := xsDetailPicRe.FindStringSubmatch(pageHTML); mp != nil {
		pic = mp[1]
	} else if mp := xsDetailPicRe2.FindStringSubmatch(pageHTML); mp != nil {
		pic = mp[1]
	}
	fields := map[string]string{}
	actors, classes := []string{}, []string{}
	for _, m := range xsFieldRe.FindAllStringSubmatch(pageHTML, -1) {
		lab := strings.TrimSuffix(strings.TrimSpace(m[1]), ":")
		links := []string{}
		for _, lm := range xsFieldLinkRe.FindAllStringSubmatch(m[2], -1) {
			links = append(links, g6StripTags(lm[1]))
		}
		switch lab {
		case "AV女优":
			actors = links
		case "类型":
			classes = links
		default:
			fields[lab] = g6StripTags(m[2])
		}
	}
	parts := []string{}
	if fields["番号"] != "" {
		parts = append(parts, "番号: "+fields["番号"])
	}
	if fields["添加日期"] != "" {
		parts = append(parts, "添加日期: "+fields["添加日期"])
	}
	if fields["标题"] != "" {
		parts = append(parts, "原标题: "+fields["标题"])
	}
	if len(classes) > 0 {
		parts = append(parts, "类型: "+strings.Join(classes, ", "))
	}
	if len(actors) > 0 {
		parts = append(parts, "女优: "+strings.Join(actors, ", "))
	}
	d := makeDrama(slot, rawID, title, pic, fields["番号"], "XSZAV", strings.Join(parts, " · "))
	return d, nil
}

func xsResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		cards, ok := xsFolderCards(rawID)
		if !ok {
			return nil, fmt.Errorf("xszav folder fetch failed")
		}
		if len(cards) == 0 {
			return nil, fmt.Errorf("xszav folder empty")
		}
		return xsResolve(slot, cards[0].id)
	}
	vid := rawID
	if strings.Contains(vid, "/") {
		parts := strings.Split(strings.TrimSuffix(vid, "/"), "/")
		vid = parts[len(parts)-1]
	}
	pageHTML, base, ok := xsFetch("/video/"+vid, nil)
	if !ok {
		return nil, fmt.Errorf("xszav: detail fetch failed for %s", vid)
	}
	m := xsPlayRe.FindStringSubmatch(pageHTML)
	if m == nil {
		return nil, fmt.Errorf("xszav: no play url for %s", vid)
	}
	playURL := base + m[1]
	res := playResult(playURL, slot, rawID, xsUA, base+"/video/"+vid, "m3u8", "hls")
	// Segment CDNs require Referer AND Origin together (py
	// playerContent headers); playResult only sets UA+Referer.
	if hdrs, ok := res["headers"].(map[string]interface{}); ok {
		hdrs["Origin"] = base
	}
	return res, nil
}

func xsSearch(slot, kw string) ([]interface{}, error) {
	pageHTML, _, ok := xsFetch("/search/videos/"+url.PathEscape(strings.TrimSpace(kw)), xsAcceptVideos)
	if !ok {
		// Uniform error grading (diag §5.2 C): all-domains-down is an
		// error, not an empty result set.
		return nil, fmt.Errorf("xszav: search fetch failed")
	}
	return xsCardsToDramas(slot, xsParseVideos(pageHTML), "搜索"), nil
}

// ---------- G6 registrations ----------

func init() {
	// v2: the xingguo slot no longer carries YouTube (user order —
	// replaced by 99jj 久久热, registered in sites_v2.go). The yt*
	// provider functions stay in this file, unregistered.
	registerSite("wangguo", &siteImpl{
		Key: "javxu", Name: "JavXu", DefaultCat: "home", HomeURL: jxHost + "/zh/",
		Categories: jxCategories,
		Catalog:    jxCatalog,
		Detail:     jxDetail,
		Resolve:    jxResolve,
		Search:     jxSearch,
		Headers:    jxHeaders,
		CoverFetch: jxCoverFetch,
	}, "javxu")
	// v4: faguo no longer carries XSZAV (user order 2026-10-23: swap
	// to the jar's 鉴赏师 javrate.com, registered in sites_javrate.go).
	// The xs* functions stay in this file, unregistered.
}
