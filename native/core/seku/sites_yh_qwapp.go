package seku

// ---- 青蛙/奇葩 qwapp (yh_qwapp) — 新闻+视频 POST JSON API ----
//
// 配方出欲火 APK jadx 实证（a5/a ApiService + a5/b NetHelper +
// qwapp 各 Fragment/Adapter）：此前仅字符串级盘点，本单深读后
// 确认：有完整 Retrofit 接口与基址，并非空壳。
//   基址：https://qw.lisseworks.cc（h6.b f8279w0，URL_QW_APP；
//     远程配置可覆盖）。全部为 POST + JSON 体（ud.o=POST，
//     ud.a=Body HashMap，即 Gson 序列化后的 JSON 对象体）：
//     POST /v1/article/category {post_type:"1"}
//         -> CategoryBean{data[{id,name,post_type}]}（首页新闻分类，
//         HomeFragment，c()）
//     POST /v1/article/category {post_type:"3"}
//         -> CategoryBean（发现视频分类，DiscoverFragment，c()）
//     POST /v1/article/list {page, keyword:"", filter1, [cid,
//         filter2], userId:"0"}
//         -> ArticleBean{data{list[]}}（ArticleFragment，d()；
//         无 cid 时 filter1="1"；有 cid 时 ArticleFragment 实证
//         链式覆盖：filter1 被后写为 "2"，filter2="2"）
//     POST /v1/video/list {page, keyword:"", filter1:"1",
//         [cid, filter2:"2"], userId:"0"}
//         -> ArticleBean{data{list[]}}（VideoFragment，a()；
//         与 article/list 的区别是 video 口 + filter1 不被覆盖）
//     POST /v1/article/getArticle {id}
//         -> ArticleDetail{data{post_content,...}}（ArticleReadActivity，b()）
//     POST /v1/searchPage {}
//         -> SearchPage{data{news{list[]}}}（SearchFragment 热门，e()；
//         真搜索走 SearchResultActivity：新闻 + 视频两页签，
//         即 ArticleFragment("", kw) 与 VideoFragment("", kw)）
//   列表项（ArticleBean ListEntity）：id、post_title、thumbnail /
//     thumbnail_list[{thumbnail}]、post_excerpt、views、price、
//     tag、more（JSON 串，MoreImage{video[{id,name,url}], thumbnails}）。
//     视频播放：VideoListAdapter 实证 click 后取 more.video 直链
//     播（commom PlayActivity），故视频卡片的 Resolve 直接取
//     more.video[0].url。新闻卡片无视频，Detail 返回 markdown
//     正文（post_content），Resolve 对新闻返回错误（非视频内容）。
//   App 导航 1:1：底部三页签（首页新闻 / 发现视频 / 搜索），其下
//     分类全部来自服务端 category 接口（动态）。Go 侧 Categories
//     = 新闻分类（news:<id>）+ 视频分类（video:<id>），分类名取
//     服务端 name 原文；搜索合并新闻 + 视频两腿。
//   付费：列表项 price>0 的内容，Resolve 直接拒解析（不读付费
//     内容）；目录卡片保留并标注「付费」。
//
// 验证门（2026-10-24 本机实测）：
//   直连 https://qw.lisseworks.cc 000（本机沙箱对该域返回假 IP
//   198.18.79.173 段、连接超时）；独立公共 DNS 双路核查：Google
//   DNS 与 Cloudflare DNS 均回 NXDOMAIN（qw.lisseworks.cc），
//   HTTP-DNS（119.28.28.28）回 "0"（无记录）。即：域名在公共
//   解析层面已死亡，非仅本机出口问题——代码按 jadx 逐行实证
//   实现，活体冒烟须待域名恢复或新基址下发后补验。各腿状态：
//   Categories（代码级，服务端动态分类）/ Catalog / Detail /
//   Resolve / Search 均为代码实证、活体未验（域名 NXDOMAIN）。
//   封面为明文 thumbnail URL，无加密形态，无需 CoverFetch。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const yhQWBase = "https://qw.lisseworks.cc"
const yhQWUA = "okhttp/4.9.0"

var yhQWCache sync.Map // article id -> raw list item (more/price lookup)

func yhQWHeaders() map[string]string {
	return map[string]string{
		"User-Agent":   yhQWUA,
		"Content-Type": "application/json; charset=utf-8",
		"Accept":       "application/json, text/plain, */*",
	}
}

func yhQWPost(path string, body map[string]interface{}) (map[string]interface{}, error) {
	raw, _ := json.Marshal(body)
	b, code, _, err := httpDo("POST", yhQWBase+path, bytes.NewReader(raw), yhQWHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_qwapp http %d", code)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("yh_qwapp parse: %v", err)
	}
	return obj, nil
}

func yhQWListItems(obj map[string]interface{}) []interface{} {
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}
	}
	return yhArr(data["list"])
}

// yhQWMoreVideo 解析列表项 more JSON 串中的视频直链。
func yhQWMoreVideo(rec map[string]interface{}) (string, string) {
	more := g5StrVal(rec["more"])
	if more == "" {
		return "", ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(more), &m); err != nil {
		return "", ""
	}
	for _, v := range yhArr(m["video"]) {
		vm := yhMap(v)
		if vm == nil {
			continue
		}
		if u := g5StrVal(vm["url"]); u != "" {
			return u, g5StrVal(vm["name"])
		}
	}
	return "", ""
}

func yhQWCard(slot string, rec map[string]interface{}, isVideo bool) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["post_title"])
	if id == "" || title == "" {
		return nil, false
	}
	yhQWCache.Store(id, rec)
	cover := g5StrVal(rec["thumbnail"])
	if cover == "" {
		for _, t := range yhArr(rec["thumbnail_list"]) {
			if tm := yhMap(t); tm != nil {
				if u := g5StrVal(tm["thumbnail"]); u != "" {
					cover = u
					break
				}
			}
		}
	}
	if cover == "" {
		if u, _ := yhQWMoreVideo(rec); u != "" {
			// 视频卡封面后备：more.thumbnails[0].url（VideoListAdapter 实证）
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(g5StrVal(rec["more"])), &m); err == nil {
				for _, th := range yhArr(m["thumbnails"]) {
					if thm := yhMap(th); thm != nil {
						if tu := g5StrVal(thm["url"]); tu != "" {
							cover = tu
							break
						}
					}
				}
			}
		}
	}
	remark := ""
	if p := g5StrVal(rec["price"]); p != "" && p != "0" {
		remark = "付费" + p
	} else if v := g5StrVal(rec["views"]); v != "" {
		remark = v + "阅读"
	}
	kind := "新闻"
	prefix := "news:"
	if isVideo {
		kind = "视频"
		prefix = "video:"
	}
	return makeDrama(slot, prefix+id, title, cover, remark, kind, g5StrVal(rec["post_excerpt"])), true
}

func yhQWCards(slot string, obj map[string]interface{}, isVideo bool) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range yhQWListItems(obj) {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		if c, ok := yhQWCard(slot, rec, isVideo); ok {
			id := g5StrVal(rec["id"])
			if !seen[id] {
				seen[id] = true
				out = append(out, c)
			}
		}
	}
	return out
}

func yhQWList(slot, path string, page int, kw, cid string, isVideo bool) ([]interface{}, error) {
	body := map[string]interface{}{
		"page":    page,
		"keyword": kw,
		"filter1": "1",
		"userId":  "0",
	}
	if cid != "" {
		body["cid"] = cid
		if isVideo {
			body["filter2"] = "2"
		} else {
			// ArticleFragment 链式覆盖实证：有 cid 时 filter1 最终为 "2"。
			body["filter1"] = "2"
			body["filter2"] = "2"
		}
	}
	obj, err := yhQWPost(path, body)
	if err != nil {
		return nil, err
	}
	return yhQWCards(slot, obj, isVideo), nil
}

func yhQWCategories(slot string) []interface{} {
	out := []interface{}{}
	if obj, err := yhQWPost("/v1/article/category", map[string]interface{}{"post_type": "1"}); err == nil {
		// CategoryBean.data 本身就是数组（bean 实证：data 为 List<DataEntity>）。
		for _, e := range yhArr(obj["data"]) {
			cm := yhMap(e)
			if cm == nil {
				continue
			}
			if id := g5StrVal(cm["id"]); id != "" {
				out = append(out, categoryObj("news:"+id, g5StrVal(cm["name"])))
			}
		}
	}
	if obj, err := yhQWPost("/v1/article/category", map[string]interface{}{"post_type": "3"}); err == nil {
		for _, e := range yhArr(obj["data"]) {
			cm := yhMap(e)
			if cm == nil {
				continue
			}
			if id := g5StrVal(cm["id"]); id != "" {
				out = append(out, categoryObj("video:"+id, g5StrVal(cm["name"])))
			}
		}
	}
	if len(out) == 0 {
		out = append(out, categoryObj("news:", "新闻"), categoryObj("video:", "视频"))
	}
	return out
}

func yhQWCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "" || category == "news:":
		items, err := yhQWList(slot, "/v1/article/list", page, "", "", false)
		if err != nil {
			return nil, 0, false, err
		}
		return items, 0, len(items) > 0, nil
	case category == "video:":
		items, err := yhQWList(slot, "/v1/video/list", page, "", "", true)
		if err != nil {
			return nil, 0, false, err
		}
		return items, 0, len(items) > 0, nil
	case strings.HasPrefix(category, "news:"):
		items, err := yhQWList(slot, "/v1/article/list", page, "", strings.TrimPrefix(category, "news:"), false)
		if err != nil {
			return nil, 0, false, err
		}
		return items, 0, len(items) > 0, nil
	case strings.HasPrefix(category, "video:"):
		items, err := yhQWList(slot, "/v1/video/list", page, "", strings.TrimPrefix(category, "video:"), true)
		if err != nil {
			return nil, 0, false, err
		}
		return items, 0, len(items) > 0, nil
	}
	return nil, 0, false, fmt.Errorf("yh_qwapp: unknown category %s", category)
}

func yhQWDetail(slot, rawID string) (map[string]interface{}, error) {
	isVideo := strings.HasPrefix(rawID, "video:")
	id := rawID
	if i := strings.Index(id, ":"); i >= 0 {
		id = id[i+1:]
	}
	obj, err := yhQWPost("/v1/article/getArticle", map[string]interface{}{"id": id})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_qwapp: article empty for %s", rawID)
	}
	title := g5StrVal(data["post_title"])
	if title == "" {
		title = "内容" + id
	}
	cover := ""
	if v, ok := yhQWCache.Load(id); ok {
		if rec, ok := v.(map[string]interface{}); ok {
			cover = g5StrVal(rec["thumbnail"])
			if u, _ := yhQWMoreVideo(rec); u != "" && cover == "" {
				cover = u
			}
		}
	}
	kind := "新闻"
	if isVideo {
		kind = "视频"
	}
	d := makeDrama(slot, rawID, title, cover, "", kind, g5StrVal(data["post_content"]))
	if isVideo {
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	}
	return d, nil
}

func yhQWResolve(slot, rawID string) (map[string]interface{}, error) {
	ep := rawID
	if i := strings.Index(ep, "$"); i >= 0 {
		ep = ep[i+1:]
	}
	if !strings.HasPrefix(ep, "video:") {
		return nil, fmt.Errorf("yh_qwapp: %s is not a video item (news has no play url)", rawID)
	}
	id := strings.TrimPrefix(ep, "video:")
	var rec map[string]interface{}
	if v, ok := yhQWCache.Load(id); ok {
		rec, _ = v.(map[string]interface{})
	}
	if rec != nil {
		if p := g5StrVal(rec["price"]); p != "" && p != "0" {
			return nil, fmt.Errorf("yh_qwapp: paid item refused (price=%s)", p)
		}
		if u, _ := yhQWMoreVideo(rec); u != "" {
			format, protocol := "mp4", ""
			if strings.Contains(u, ".m3u8") {
				format, protocol = "m3u8", "hls"
			}
			return playResult(u, slot, rawID, yhQWUA, yhQWBase+"/", format, protocol), nil
		}
	}
	return nil, fmt.Errorf("yh_qwapp: no cached video url for %s (browse its category first)", rawID)
}

func yhQWSearch(slot, kw string) ([]interface{}, error) {
	out := []interface{}{}
	if items, err := yhQWList(slot, "/v1/article/list", 1, kw, "", false); err == nil {
		out = append(out, items...)
	} else {
		return out, err
	}
	if items, err := yhQWList(slot, "/v1/video/list", 1, kw, "", true); err == nil {
		out = append(out, items...)
	}
	return out, nil
}

func init() {
	registerSite("yh_qwapp", &siteImpl{
		Key:        "yh_qwapp",
		Name:       "青蛙",
		DefaultCat: "news:",
		HomeURL:    yhQWBase + "/",
		Categories: yhQWCategories,
		Catalog:    yhQWCatalog,
		Detail:     yhQWDetail,
		Resolve:    yhQWResolve,
		Search:     yhQWSearch,
		Headers:    yhQWHeaders,
	}, "yh_qwapp", "qwapp", "青蛙")
}
