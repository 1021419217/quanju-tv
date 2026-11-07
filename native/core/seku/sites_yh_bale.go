package seku

// ---- 把乐 bale (yh_bale) — JSON API, 配方出欲火 APK jadx 实证 ----
//
// base https://api.blapi004.xyz (h6.b), Retrofit 接口 x0/a:
//   POST /api/v1/video/videoinfo   form videoId -> {result{url}}
//   POST /api/v1/video/videolist   ?tag= -> {result[{name,list[]}]}（首页分组）
//   POST /api/v1/video/list        ?page= + JSON{tagId,sortId} -> {result{data[]}}
//   POST /api/v1/video/rank        ?type=views|zans|downloads|comments&page=
//   POST /api/v1/video/artistvideo ?artistId= -> {result{...,videoList[]}}
//   POST /api/v1/video/tagsearch   ?tags=&page=
//   POST /api/v1/video/searchbywords ?page= + form keyWords
//   GET  /api/v1/video/artist      -> {result{hotArtist[],allArtist[]}}
//   GET  /api/v1/video/alltag      -> {result{tags[]}}
//   GET  /api/v1/video/sort        -> {result[]}（筛选维度,未据此造分类）
// 分类与 App 导航 1:1: 全部 + 26 标签页签(tagId) + 女优 + 四榜 + 标签。
// 女优/标签为 folder 卡（rawID dir:artist:<id> / dir:tag:<名>）下钻出作品卡。
// 注意: 2026-10-24 本机出口对该主机一律空回/断连,响应形态未活体校准
// （代码按 jadx bean 形态实现,待出口可用时冒烟补验）。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const yhBaleBase = "https://api.blapi004.xyz"
const yhBaleUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"

var yhBaleTagTabs = []struct {
	id   int
	name string
}{
	{113, "體液"}, {52, "劇情"}, {54, "痙攣"}, {64, "深喉"}, {67, "潮吹"},
	{68, "輪姦"}, {70, "乳交"}, {75, "顏射"}, {78, "口交"}, {79, "足交"},
	{95, "手淫"}, {103, "推油按摩"}, {43, "女教師"}, {36, "女醫生"},
	{2, "無碼"}, {4, "黑人"}, {6, "第一視角"}, {7, "淩辱"}, {8, "不倫"},
	{14, "中出"}, {15, "短髮"}, {16, "癡女"}, {17, "人妻"},
	{18, "女搜查官"}, {26, "美少女"}, {1, "癡漢"},
}

func yhBaleHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      yhBaleUA,
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Referer":         yhBaleBase + "/",
	}
}

func yhBalePost(path string, body []byte, ctype string) (map[string]interface{}, error) {
	h := yhBaleHeaders()
	if ctype != "" {
		h["Content-Type"] = ctype
	}
	b, code, _, err := httpDo("POST", yhBaleBase+path, bytes.NewReader(body), h)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_bale http %d", code)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("yh_bale parse: %v", err)
	}
	return obj, nil
}

func yhBaleGet(path string) (map[string]interface{}, error) {
	b, code, _, err := httpGet(yhBaleBase+path, yhBaleHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_bale http %d", code)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("yh_bale parse: %v", err)
	}
	return obj, nil
}

func yhMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func yhArr(v interface{}) []interface{} {
	if a, ok := v.([]interface{}); ok {
		return a
	}
	return nil
}

func yhBaleCover(rec map[string]interface{}) string {
	cbURL := ""
	if cb := yhMap(rec["coverbase64"]); cb != nil {
		cbURL = g5StrVal(cb["url"])
	}
	img := g5StrVal(rec["image"])
	for _, c := range []string{cbURL, img} {
		if strings.HasPrefix(c, "http") {
			return c
		}
	}
	if img != "" {
		if strings.HasPrefix(img, "/") {
			return yhBaleBase + img
		}
		return img
	}
	return cbURL
}

func yhBaleViews(v interface{}) string {
	n := g5StrVal(v)
	if n == "" {
		return ""
	}
	var iv int64
	if _, err := fmt.Sscanf(n, "%d", &iv); err == nil && iv >= 10000 {
		return fmt.Sprintf("%d万", iv/10000)
	}
	return n
}

func yhBaleCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["title"])
	if id == "" || title == "" {
		return nil, false
	}
	if g5Truthy(rec["isAd"]) {
		return nil, false
	}
	remark := g5StrVal(rec["code"])
	if remark == "" && rec["views"] != nil {
		remark = yhBaleViews(rec["views"]) + "播放"
	}
	return makeDrama(slot, id, title, yhBaleCover(rec), remark, g5StrVal(rec["tag"]), g5StrVal(rec["desc"])), true
}

func yhBaleCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range recs {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		if c, ok := yhBaleCard(slot, rec); ok {
			id := g5StrVal(rec["id"])
			if !seen[id] {
				seen[id] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// yhBalePaged 解析 RankList 形态 {result{data[],total,last_page}}。
func yhBalePaged(slot string, obj map[string]interface{}, page int) ([]interface{}, int, bool, error) {
	res := yhMap(obj["result"])
	if res == nil {
		return []interface{}{}, 0, false, fmt.Errorf("yh_bale: empty result")
	}
	items := yhBaleCards(slot, yhArr(res["data"]))
	total, _ := g5MapInt(res, "total")
	last, hasLast := g5MapInt(res, "last_page")
	hasMore := false
	if hasLast {
		hasMore = page < last
	} else {
		hasMore = len(items) > 0
	}
	return items, total, hasMore, nil
}

func yhBaleListPage(slot, tagID string, page int) ([]interface{}, int, bool, error) {
	body, _ := json.Marshal(map[string]string{"tagId": tagID, "sortId": ""})
	obj, err := yhBalePost(fmt.Sprintf("/api/v1/video/list?page=%d", page), body, "application/json; charset=utf-8")
	if err != nil {
		return nil, 0, false, err
	}
	return yhBalePaged(slot, obj, page)
}

func yhBaleArtistVideos(slot, artistID string) ([]interface{}, map[string]interface{}, error) {
	obj, err := yhBalePost("/api/v1/video/artistvideo?artistId="+url.QueryEscape(artistID), []byte{}, "")
	if err != nil {
		return nil, nil, err
	}
	res := yhMap(obj["result"])
	if res == nil {
		return nil, nil, fmt.Errorf("yh_bale: artist empty")
	}
	return yhBaleCards(slot, yhArr(res["videoList"])), res, nil
}

func yhBaleCategories(slot string) []interface{} {
	out := []interface{}{categoryObj("all", "全部")}
	for _, t := range yhBaleTagTabs {
		out = append(out, categoryObj(fmt.Sprintf("tag:%d", t.id), t.name))
	}
	out = append(out,
		categoryObj("artist", "女优"),
		categoryObj("rank:views", "排行·播放数"),
		categoryObj("rank:zans", "排行·点赞数"),
		categoryObj("rank:downloads", "排行·下载数"),
		categoryObj("rank:comments", "排行·评论数"),
		categoryObj("tags", "标签"),
	)
	return out
}

func yhBaleCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "" || category == "all":
		return yhBaleListPage(slot, "", page)
	case strings.HasPrefix(category, "dir:artist:"):
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		items, _, err := yhBaleArtistVideos(slot, strings.TrimPrefix(category, "dir:artist:"))
		return items, len(items), false, err
	case strings.HasPrefix(category, "dir:tag:"):
		obj, err := yhBalePost("/api/v1/video/tagsearch?tags="+url.QueryEscape(strings.TrimPrefix(category, "dir:tag:"))+fmt.Sprintf("&page=%d", page), []byte{}, "")
		if err != nil {
			return nil, 0, false, err
		}
		return yhBalePaged(slot, obj, page)
	case category == "artist":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhBaleGet("/api/v1/video/artist")
		if err != nil {
			return nil, 0, false, err
		}
		res := yhMap(obj["result"])
		out := []interface{}{}
		seen := map[string]bool{}
		add := func(beans []interface{}, remark string) {
			for _, e := range beans {
				b := yhMap(e)
				if b == nil {
					continue
				}
				aid := g5StrVal(b["id"])
				name := g5StrVal(b["artist"])
				if aid == "" || name == "" || seen[aid] {
					continue
				}
				seen[aid] = true
				r := remark
				if r == "" {
					r = g5StrVal(b["letter"])
				}
				out = append(out, makeDrama(slot, "dir:artist:"+aid, name, g5StrVal(b["image"]), r, "女优", ""))
			}
		}
		if res != nil {
			add(yhArr(res["hotArtist"]), "人气女优")
			for _, g := range yhArr(res["allArtist"]) {
				if gm := yhMap(g); gm != nil {
					add(yhArr(gm["list"]), "")
				}
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "rank:"):
		obj, err := yhBalePost("/api/v1/video/rank?type="+strings.TrimPrefix(category, "rank:")+fmt.Sprintf("&page=%d", page), []byte{}, "")
		if err != nil {
			return nil, 0, false, err
		}
		return yhBalePaged(slot, obj, page)
	case category == "tags":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhBaleGet("/api/v1/video/alltag")
		if err != nil {
			return nil, 0, false, err
		}
		res := yhMap(obj["result"])
		out := []interface{}{}
		seen := map[string]bool{}
		if res != nil {
			for _, g := range yhArr(res["tags"]) {
				gm := yhMap(g)
				if gm == nil {
					continue
				}
				for _, e := range yhArr(gm["list"]) {
					tm := yhMap(e)
					if tm == nil {
						continue
					}
					name := g5StrVal(tm["title"])
					if name == "" || seen[name] {
						continue
					}
					seen[name] = true
					out = append(out, makeDrama(slot, "dir:tag:"+name, name, "", g5StrVal(gm["name"]), "标签", ""))
				}
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "tag:"):
		return yhBaleListPage(slot, strings.TrimPrefix(category, "tag:"), page)
	}
	return yhBaleListPage(slot, "", page)
}

func yhBalePlayURL(vid string) (string, error) {
	form := url.Values{"videoId": {vid}}.Encode()
	obj, err := yhBalePost("/api/v1/video/videoinfo", []byte(form), "application/x-www-form-urlencoded")
	if err != nil {
		return "", err
	}
	res := yhMap(obj["result"])
	if res == nil {
		return "", fmt.Errorf("yh_bale: videoinfo empty for %s", vid)
	}
	u := g5StrVal(res["url"])
	if u == "" {
		return "", fmt.Errorf("yh_bale: no play url for %s", vid)
	}
	return u, nil
}

func yhBaleDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "dir:artist:") {
		aid := strings.TrimPrefix(rawID, "dir:artist:")
		items, res, err := yhBaleArtistVideos(slot, aid)
		if err != nil {
			return nil, err
		}
		name := g5StrVal(res["artist"])
		if name == "" {
			name = aid
		}
		desc := g5StrVal(res["desc"])
		info := fmt.Sprintf("身高：%s 三围：%s %s杯", g5StrVal(res["height"]), g5StrVal(res["measurement"]), g5StrVal(res["cup"]))
		if desc != "" {
			desc += "\n"
		}
		desc += info
		d := makeDrama(slot, rawID, name, g5StrVal(res["image"]), "女优", "女优", desc)
		epIDs := []string{}
		epNames := []string{}
		for _, it := range items {
			if im, ok := it.(map[string]interface{}); ok {
				epIDs = append(epIDs, g5StrVal(im["vod_id"]))
				epNames = append(epNames, g5StrVal(im["title"]))
			}
		}
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, epNames)
			d["related"] = items
		}
		return d, nil
	}
	title := "视频" + rawID
	cover := ""
	remark := ""
	desc := ""
	if st := getStore(slot).findDrama(rawID); st != nil {
		if s := g5StrVal(st["title"]); s != "" {
			title = s
		}
		cover = g5StrVal(st["cover"])
		remark = g5StrVal(st["remark"])
		desc = g5StrVal(st["desc"])
	}
	d := makeDrama(slot, rawID, title, cover, remark, "", desc)
	g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	return d, nil
}

func yhBaleResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	u, err := yhBalePlayURL(vid)
	if err != nil {
		return nil, err
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhBaleUA, yhBaleBase+"/", format, protocol), nil
}

func yhBaleSearch(slot, kw string) ([]interface{}, error) {
	form := url.Values{"keyWords": {kw}}.Encode()
	obj, err := yhBalePost("/api/v1/video/searchbywords?page=1", []byte(form), "application/x-www-form-urlencoded")
	if err != nil {
		return nil, err
	}
	items, _, _, err := yhBalePaged(slot, obj, 1)
	return items, err
}

func init() {
	registerSite("yh_bale", &siteImpl{
		Key:        "yh_bale",
		Name:       "把乐",
		DefaultCat: "all",
		HomeURL:    yhBaleBase + "/",
		Categories: yhBaleCategories,
		Catalog:    yhBaleCatalog,
		Detail:     yhBaleDetail,
		Resolve:    yhBaleResolve,
		Search:     yhBaleSearch,
		Headers:    yhBaleHeaders,
	}, "yh_bale", "bale", "把乐")
}
