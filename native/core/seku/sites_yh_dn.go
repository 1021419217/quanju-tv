package seku

// ---- 豆奶/九尾 dn (yh_dn) — 短视频社区 API ----
//
// 配方出欲火 APK jadx 实证（l1/a ApiService + l1/b NetHelper +
// dn 各 Fragment/Adapter）：
//   基址：http://www.jiuwei.tv（h6.b f8254k，URL_DN；远程配置可覆盖）
//   全部为 GET（ud.f=GET，ud.u=QueryMap，即参数拼在 URL 查询串上）：
//   GET /appapi/public/index.php?service=Video.getVideoList
//       &language=zh-cn&uid=-1&p=1            -> 推荐（RecommendFragment，
//       l() 写死 p=1，故推荐只有第 1 页）
//   GET /appapi/public/index.php?service=Video.getVideoList
//       &language=zh-cn&uid=-1&p=<n>          -> 最新（HotFragment，n(n)）
//   GET /appapi/public/index.php?service=Home.videoSearch
//       &language=zh-cn&uid=-1&p=<n>&key=<词> -> 视频搜索（SearchVideoFragment，k()）
//   GET /appapi/public/index.php?service=Home.search
//       &language=zh-cn&uid=-1&p=<n>&key=<词> -> 用户搜索（SearchUserFragment，m()）
//   GET /appapi/public/index.php?service=Video.getHomeVideo
//       &language=zh-cn&uid=-1&touid=<uid>&p=<n> -> 用户作品（UserCenterFragment，g()）
//   GET /appapi/public/index.php?service=User.getLikeVideos
//       &language=zh-cn&uid=-1&touid=<uid>&p=<n> -> 用户喜欢（UserCenterFragment，g()）
//   GET http://www.jiuwei.tv:81/getTxMediaInfo?videoid=<vid>
//       -> PlayUrl{MediaInfoSet[0].TranscodeInfo.TranscodeSet[0].Url}
//       （ShortVideoAdapter.c；直连腾讯云转码地址）
// 响应形：{ret, msg, data{code, msg, info[]}}；VideoList.getData()
//   .getInfo() 实证过滤：标题含「广告」者剔除。卡片 id 用 videoid
//   （播放键），封面 thumb/thumb_s，href（列表自带直链）缓存在
//   本文件 map 中供 Resolve 复用；href 为空时默认占位为百度首页
//   （ShortVideoAdapter:114 实证，Go 侧不把占位当播放地址返回，
//   而是走 getTxMediaInfo 换真地址）。
// App 导航 1:1：推荐 / 最新 两个页签 + 搜索（视频/用户双维）+
// 用户作品/喜欢（文件夹卡下钻）。用户卡片 rawID 形如 "user:<uid>"，
// 其作品/喜欢经 dir:user:<uid>:works / dir:user:<uid>:likes 下钻。
// 成人站：展示名带 🔞 前缀。
//
// 验证门（2026-10-24 本机实测）：
//   直连 http://www.jiuwei.tv 与 :81 均 000（本机沙箱对该域返回
//   假 IP 198.18.79.172 段、连接超时）；独立公共 DNS 双路核查：
//   Google DNS 与 Cloudflare DNS 均回 NXDOMAIN（jiuwei.tv、
//   www.jiuwei.tv），HTTP-DNS（119.28.28.28）回 "0"（无记录）。
//   即：域名在公共解析层面已死亡，非仅本机出口问题——代码按 jadx
//   逐行实证实现，活体冒烟须待域名恢复或新基址下发后补验。
//   各腿状态：Categories（代码级，App 导航 1:1）/ Catalog /
//   Detail / Resolve / Search 均为代码实证、活体未验（域名
//   NXDOMAIN）。封面为明文 thumb URL，无加密形态，无需 CoverFetch。

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

const yhDNBase = "http://www.jiuwei.tv"
const yhDNPlayBase = "http://www.jiuwei.tv:81"
const yhDNUA = "okhttp/4.9.0"

var yhDNCache sync.Map // videoid -> href (list-provided direct url)

func yhDNHeaders() map[string]string {
	return map[string]string{
		"User-Agent": yhDNUA,
		"Accept":     "application/json, text/plain, */*",
	}
}

func yhDNGet(service string, extra map[string]string) (map[string]interface{}, error) {
	q := url.Values{}
	q.Set("service", service)
	q.Set("language", "zh-cn")
	q.Set("uid", "-1")
	for k, v := range extra {
		q.Set(k, v)
	}
	rawurl := yhDNBase + "/appapi/public/index.php?" + q.Encode()
	b, code, _, err := httpGet(rawurl, yhDNHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_dn http %d", code)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("yh_dn parse: %v", err)
	}
	return obj, nil
}

func yhDNVideoCards(slot string, obj map[string]interface{}) []interface{} {
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}
	}
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range yhArr(data["info"]) {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		title := g5StrVal(rec["title"])
		if title == "" || strings.Contains(title, "广告") {
			continue
		}
		if g5StrVal(rec["is_ad"]) == "1" {
			continue
		}
		vid := g5StrVal(rec["videoid"])
		if vid == "" {
			vid = g5StrVal(rec["id"])
		}
		if vid == "" || seen[vid] {
			continue
		}
		seen[vid] = true
		if href := g5StrVal(rec["href"]); href != "" {
			yhDNCache.Store(vid, href)
		}
		cover := g5StrVal(rec["thumb"])
		if cover == "" {
			cover = g5StrVal(rec["thumb_s"])
		}
		remark := g5StrVal(rec["views"])
		if remark != "" {
			remark += "播放"
		}
		author := ""
		if ui := yhMap(rec["userinfo"]); ui != nil {
			author = g5StrVal(ui["user_nicename"])
		}
		out = append(out, makeDrama(slot, vid, title, cover, remark, author, g5StrVal(rec["datetime"])))
	}
	return out
}

func yhDNUserCards(slot string, obj map[string]interface{}) []interface{} {
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}
	}
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range yhArr(data["info"]) {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		uid := g5StrVal(rec["id"])
		name := g5StrVal(rec["user_nicename"])
		if uid == "" || name == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		remark := ""
		if f := g5StrVal(rec["fans"]); f != "" {
			remark = f + "粉丝"
		}
		out = append(out, makeDrama(slot, "user:"+uid, name, g5StrVal(rec["avatar"]), remark, "用户", g5StrVal(rec["signature"])))
	}
	return out
}

func yhDNCategories(slot string) []interface{} {
	return []interface{}{
		categoryObj("recommend", "推荐"),
		categoryObj("hot", "最新"),
		categoryObj("users", "用户搜索"),
	}
}

func yhDNCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "" || category == "recommend":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhDNGet("Video.getVideoList", map[string]string{"p": "1"})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhDNVideoCards(slot, obj)
		return items, 0, false, nil
	case category == "hot":
		obj, err := yhDNGet("Video.getVideoList", map[string]string{"p": fmt.Sprintf("%d", page)})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhDNVideoCards(slot, obj)
		return items, 0, len(items) > 0, nil
	case strings.HasPrefix(category, "dir:user:"):
		rest := strings.TrimPrefix(category, "dir:user:")
		uid, kind, _ := strings.Cut(rest, ":")
		service := "Video.getHomeVideo"
		if kind == "likes" {
			service = "User.getLikeVideos"
		}
		obj, err := yhDNGet(service, map[string]string{"touid": uid, "p": fmt.Sprintf("%d", page)})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhDNVideoCards(slot, obj)
		return items, 0, len(items) > 0, nil
	case category == "users":
		return nil, 0, false, fmt.Errorf("yh_dn: 用户维度请走 Search（用户搜索）或打开用户卡片")
	}
	return nil, 0, false, fmt.Errorf("yh_dn: unknown category %s", category)
}

func yhDNDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "user:") {
		uid := strings.TrimPrefix(rawID, "user:")
		obj, err := yhDNGet("Video.getHomeVideo", map[string]string{"touid": uid, "p": "1"})
		if err != nil {
			return nil, err
		}
		items := yhDNVideoCards(slot, obj)
		d := makeDrama(slot, rawID, "用户"+uid, "", "用户", "用户", "")
		if len(items) > 0 {
			epIDs := []string{}
			epNames := []string{}
			for _, it := range items {
				if im, ok := it.(map[string]interface{}); ok {
					epIDs = append(epIDs, g5StrVal(im["vod_id"]))
					epNames = append(epNames, g5StrVal(im["title"]))
				}
			}
			g5SetChapters(d, slot, rawID, epIDs, epNames)
			d["related"] = items
		}
		return d, nil
	}
	// 单视频：列表已给全量字段；Detail 优先复用缓存的卡片信息，
	// 否则返回以 videoid 为键的占位详情（播放走 Resolve 换真地址）。
	if st := getStore(slot).findDrama(rawID); st != nil {
		title := g5StrVal(st["title"])
		if title == "" {
			title = "视频" + rawID
		}
		d := makeDrama(slot, rawID, title, g5StrVal(st["cover"]), g5StrVal(st["remark"]), "", g5StrVal(st["desc"]))
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
		return d, nil
	}
	d := makeDrama(slot, rawID, "视频"+rawID, "", "", "", "")
	g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	return d, nil
}

func yhDNResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, "user:")
	// 列表自带 href 优先（App 先播 href，再由 getTxMediaInfo 刷新）。
	if v, ok := yhDNCache.Load(vid); ok {
		if href, _ := v.(string); href != "" && strings.HasPrefix(href, "http") {
			format, protocol := "mp4", ""
			if strings.Contains(href, ".m3u8") {
				format, protocol = "m3u8", "hls"
			}
			return playResult(href, slot, rawID, yhDNUA, yhDNBase+"/", format, protocol), nil
		}
	}
	rawurl := yhDNPlayBase + "/getTxMediaInfo?videoid=" + url.QueryEscape(vid)
	b, code, _, err := httpGet(rawurl, yhDNHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_dn getTxMediaInfo http %d", code)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("yh_dn parse: %v", err)
	}
	var u string
	if mis := yhArr(obj["MediaInfoSet"]); len(mis) > 0 {
		if mi := yhMap(mis[0]); mi != nil {
			if ti := yhMap(mi["TranscodeInfo"]); ti != nil {
				if ts := yhArr(ti["TranscodeSet"]); len(ts) > 0 {
					u = g5StrVal(yhMap(ts[0])["Url"])
				}
			}
		}
	}
	if u == "" {
		return nil, fmt.Errorf("yh_dn: no transcode url for %s", vid)
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhDNUA, yhDNBase+"/", format, protocol), nil
}

func yhDNSearch(slot, kw string) ([]interface{}, error) {
	out := []interface{}{}
	obj, err := yhDNGet("Home.videoSearch", map[string]string{"p": "1", "key": kw})
	if err != nil {
		return nil, err
	}
	out = append(out, yhDNVideoCards(slot, obj)...)
	if uobj, err := yhDNGet("Home.search", map[string]string{"p": "1", "key": kw}); err == nil {
		out = append(out, yhDNUserCards(slot, uobj)...)
	}
	return out, nil
}

func init() {
	registerSite("yh_dn", &siteImpl{
		Key:        "yh_dn",
		Name:       "豆奶",
		DefaultCat: "recommend",
		HomeURL:    yhDNBase + "/",
		Categories: yhDNCategories,
		Catalog:    yhDNCatalog,
		Detail:     yhDNDetail,
		Resolve:    yhDNResolve,
		Search:     yhDNSearch,
		Headers:    yhDNHeaders,
	}, "yh_dn", "dn", "豆奶")
}
