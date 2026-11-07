package seku

// ---- 撸Tube lutube (yh_lutube, type 9) — HostInfo2 发现 + 响应解密 ----
//
// 配方来源（jadx 逐行实证，u3 包 + LutubeLaunchActivity + t3 回调）：
//   发现候选（u3/b ApiService 绝对地址，三路竞速，首个成功者胜）：
//     https://d1676ulzgwoe0d.cloudfront.net/web1.txt
//     https://d1gccac30ij1az.cloudfront.net/web1.txt
//     https://d1p2iubpaqa4pj.cloudfront.net/web1.txt
//     → HostInfo2{api[], img[], stream[]}（各 {mode, name, weights}；
//     api 项 mode=="manual" 者剔除，余者 name 即候选域）。
//   测速（LutubeLaunchActivity.p + t3/d/e/f）：对每个候选域 GET
//     {name}/speed.html（头 Connection: close），首个 ≤300 者胜出，
//     胜出域去掉 "/speed.html" 后写回：Api→h6.b.f8264p（默认
//     https://lutube.xyz）、Img→f8270s（默认 https://imz.lyxingdao.net）、
//     Stream→f8272t（默认 https://abc.iplay01.com）。
//   拦截器（u3/c）：请求删 User-Agent/token/Accept-Encoding 头，置
//     origin: https://lutube.xyz、authorization: <h6.b LUTUBE_TOKEN>、
//     User-Agent: iPhone Safari 16.6 串；若此前响应带 Set-Cookie，则
//     后续请求置 cookie: <该值>。响应：若 X-VTag 头非空，体按 u3/a.a
//     解密：AES/CBC/PKCS5，key = native getLutube
//     "322b63a3be0567ae7cae7a2f368ee38a"（32 字节原文作 key），
//     IV = MD5hex(X-VTag)[8:24]。
//     ※ 任务简述称「u3.a 解密前有 SHA-256 摘要参与」，jadx 实读 u3/a.java
//     为 MessageDigest.getInstance("md5")（WgZe→md5，见 h6/l 同串），
//     故按实读 MD5 复刻，特此注明差异。
//   端点（u3/b）：
//     GET /api/v4/menu/{menuId}/layout?page=（首页菜单；长片 13 最新/
//       14 排行/17 中文/16 无码；短片 19 最新/20 排行/21 探花/23 美女/
//       22 素人）
//     GET /api/v2/category/{type}/{menu}/{order} query {page, token}
//     GET /api/v1/category/{long|short}?token= → VideoType[{id,name,url}]
//     GET /api/v2/category/actor query {page, order(views|counts),
//       cup, token}
//     GET /api/v2/category/actor/{id}/time query {page, token}
//     GET /api/v2/long/info/{videoId}?host=<stream>
//     GET /api/v2/short/info/{videoId}?token=&host=<stream>
//     GET /api/v2/{long|short}/search/keyword query {page, query, token}
//     GET /api/v2/{videoType}/search/recommend
//   播放（VideoInfo2.getPlayUrl/PlayName）：Api 基址 + video_urls.480 /
//     .240 / intro，名 480P/240P/预览；封面 = 图基址 + cover（bean 原样
//     前缀；本核对已含 http 的封面直用，属防御性偏离，已注明）。
//   /api/v2/menu/ 响应体 JSON 修补（u3/c 后段）：data[] 项 content 为
//     数组者保留、非数组者删除 content 键——本核按响应原样解析，
//     content 数组与否均不影响卡片提取。
//
// ⚠ 发现链现状（2026-10-24 多路实证，非本机出口问题）：
//   ① 对账报告新证据：lutube.xyz/speed.html 已变广告落地页（JS 跳
//      /lander），发现链断；
//   ② 本机 HTTP-DNS（119.28.28.28/d）：lutube.xyz 返回空，三条
//      cloudfront 发现域均返回 0（无记录）；
//   ③ 浏览器出口拉 https://lutube.xyz/speed.html 与
//      https://d1676ulzgwoe0d.cloudfront.net/web1.txt 均网络错误
//      （DNS/超时/连接拒绝）；
//   ④ 沙箱直连 lutube.xyz 同为 000（代理假 IP 拦截）。
//   结论：发现链现行已死亡（例外口径①）。本文件仍按 jadx 原路完整
//   实现发现 + 全部端点（运行时发现成功即可用；发现失败时退 h6.b
//   默认三域并返回可读错误），不编造任何新基址。Catalog/Detail/
//   Resolve/Search/封面魔数均标「发现链死亡，未验」。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

const yhLutubeSlot = "yh_lutube"
const yhLutubeUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1"
const yhLutubeOrigin = "https://lutube.xyz"
const yhLutubeDefaultAPI = "https://lutube.xyz"
const yhLutubeDefaultImg = "https://imz.lyxingdao.net"
const yhLutubeDefaultStream = "https://abc.iplay01.com"
const yhLutubeKey = "322b63a3be0567ae7cae7a2f368ee38a"

// yhLutubeToken：h6.b.f8274u（LUTUBE_TOKEN，应用级 Bearer 令牌原值）。
const yhLutubeToken = "Bearer eyJ1c2VyX2lkIjozMTc3ODgzMzgsImxhc3Rsb2dpbiI6MywiZXhwaXJlZCI6MTc1NTA4OTMzNn0.32991f9df4e4e55a788fd5a06e3e0fa3.064bfab6627196eb2e1fdeaae96f3ed0536d33fb792c7d6b1804e62f"

var yhLutubeHostURLs = []string{
	"https://d1676ulzgwoe0d.cloudfront.net/web1.txt",
	"https://d1gccac30ij1az.cloudfront.net/web1.txt",
	"https://d1p2iubpaqa4pj.cloudfront.net/web1.txt",
}

var (
	yhLutubeMu      sync.Mutex
	yhLutubeReady   bool
	yhLutubeAPI     string
	yhLutubeImg     string
	yhLutubeStream  string
	yhLutubeCookie  string
	yhLutubeInitErr error
)

func yhLutubeMD5IV(seed string) []byte {
	sum := md5.Sum([]byte(seed))
	return []byte(hex.EncodeToString(sum[:])[8:24])
}

func yhLutubeUnpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > 16 || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}

func yhLutubeHeaders() map[string]string {
	h := map[string]string{
		"origin":        yhLutubeOrigin,
		"authorization": yhLutubeToken,
		"User-Agent":    yhLutubeUA,
	}
	if yhLutubeCookie != "" {
		h["cookie"] = yhLutubeCookie
	}
	return h
}

// yhLutubeDecrypt：u3/a.a 原路（key 原文 bytes，IV=MD5hex(vtag)[8:24]）。
func yhLutubeDecrypt(body, vtag string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw)%16 != 0 {
		return "", fmt.Errorf("yh_lutube: bad ciphertext len %d", len(raw))
	}
	block, err := aes.NewCipher([]byte(yhLutubeKey))
	if err != nil {
		return "", err
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhLutubeMD5IV(vtag)).CryptBlocks(out, raw)
	return string(yhLutubeUnpad(out)), nil
}

// yhLutubeSpeed：候选域测速，首个 ≤300 者胜出（t3 回调原路）。
func yhLutubeSpeed(names []string) string {
	for _, n := range names {
		u := strings.TrimSuffix(n, "/")
		if u == "" {
			continue
		}
		h := map[string]string{"Connection": "close"}
		_, code, _, err := httpGetTimeout(u+"/speed.html", h, 8*time.Second)
		if err == nil && code <= 300 {
			return u
		}
	}
	return ""
}

func yhLutubeDiscover() (api, img, stream string, err error) {
	type ent struct {
		Mode string `json:"mode"`
		Name string `json:"name"`
	}
	var host struct {
		API    []ent `json:"api"`
		Img    []ent `json:"img"`
		Stream []ent `json:"stream"`
	}
	var lastErr error
	for _, u := range yhLutubeHostURLs {
		b, code, hdr, gerr := httpGet(u, yhLutubeHeaders())
		if gerr != nil || code != 200 {
			lastErr = gerr
			continue
		}
		trimmed := strings.TrimSpace(string(b))
		if strings.HasPrefix(trimmed, "{") {
			if jerr := json.Unmarshal(b, &host); jerr == nil {
				break
			}
		}
		if vtag := hdr.Get("X-VTag"); vtag != "" {
			if plain, derr := yhLutubeDecrypt(trimmed, vtag); derr == nil {
				if jerr := json.Unmarshal([]byte(plain), &host); jerr == nil {
					break
				} else {
					lastErr = jerr
				}
			} else {
				lastErr = derr
			}
		} else {
			lastErr = fmt.Errorf("yh_lutube: host file not json and no X-VTag")
		}
	}
	if len(host.API) == 0 && len(host.Img) == 0 && len(host.Stream) == 0 {
		if lastErr != nil {
			return "", "", "", lastErr
		}
		return "", "", "", fmt.Errorf("yh_lutube: host discovery empty")
	}
	names := func(in []ent, dropManual bool) []string {
		out := []string{}
		for _, e := range in {
			if dropManual && e.Mode == "manual" {
				continue
			}
			out = append(out, e.Name)
		}
		return out
	}
	return yhLutubeSpeed(names(host.API, true)), yhLutubeSpeed(names(host.Img, false)), yhLutubeSpeed(names(host.Stream, false)), nil
}

func yhLutubeEnsure() {
	yhLutubeMu.Lock()
	defer yhLutubeMu.Unlock()
	if yhLutubeReady {
		return
	}
	yhLutubeReady = true
	yhLutubeAPI, yhLutubeImg, yhLutubeStream = yhLutubeDefaultAPI, yhLutubeDefaultImg, yhLutubeDefaultStream
	if api, img, stream, err := yhLutubeDiscover(); err == nil {
		if api != "" {
			yhLutubeAPI = api
		}
		if img != "" {
			yhLutubeImg = img
		}
		if stream != "" {
			yhLutubeStream = stream
		}
	} else {
		yhLutubeInitErr = err
	}
}

func yhLutubeCall(path string, q url.Values) (map[string]interface{}, error) {
	yhLutubeEnsure()
	if q == nil {
		q = url.Values{}
	}
	u := strings.TrimSuffix(yhLutubeAPI, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	b, code, hdr, err := httpGet(u, yhLutubeHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_lutube http %d", code)
	}
	if sc := hdr.Get("Set-Cookie"); sc != "" {
		yhLutubeCookie = sc
	}
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, "{") {
		var obj map[string]interface{}
		if err := json.Unmarshal(b, &obj); err == nil {
			return obj, nil
		}
	}
	if vtag := hdr.Get("X-VTag"); vtag != "" {
		plain, err := yhLutubeDecrypt(trimmed, vtag)
		if err != nil {
			return nil, fmt.Errorf("yh_lutube decrypt: %v", err)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(plain), &obj); err != nil {
			return nil, fmt.Errorf("yh_lutube parse: %v", err)
		}
		return obj, nil
	}
	return nil, fmt.Errorf("yh_lutube: non-json response without X-VTag")
}

func yhLutubeCover(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http") {
		return p
	}
	yhLutubeEnsure()
	return strings.TrimSuffix(yhLutubeImg, "/") + "/" + strings.TrimPrefix(p, "/")
}

func yhLutubeCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["title"])
	if id == "" || title == "" {
		return nil, false
	}
	tags := ""
	for _, e := range yhArr(rec["main_tags"]) {
		tags += g5StrVal(e)
	}
	if strings.Contains(tags, "VIP") {
		return nil, false
	}
	return makeDrama(slot, id, title, yhLutubeCover(g5StrVal(rec["cover"])), tags, "", ""), true
}

func yhLutubeCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhLutubeCard(slot, m); ok && !seen[g5StrVal(m["id"])] {
				seen[g5StrVal(m["id"])] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// yhLutubeListData：NVideoList/ActorWork 形态 {data[{content{data[]}}]} 展平，
// 兼容 VideoList {data[]} 与 ActorWork {data[]} 直列形态。
func yhLutubeListData(slot string, obj map[string]interface{}) []interface{} {
	out := []interface{}{}
	for _, sec := range yhArr(obj["data"]) {
		sm := yhMap(sec)
		if sm == nil {
			continue
		}
		if cm := yhMap(sm["content"]); cm != nil {
			out = append(out, yhLutubeCards(slot, yhArr(cm["data"]))...)
		} else if g5StrVal(sm["title"]) != "" || g5StrVal(sm["id"]) != "" {
			if c, ok := yhLutubeCard(slot, sm); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func yhLutubeCategories(slot string) []interface{} {
	return []interface{}{
		categoryObj("menu:13:long", "长片·最新"),
		categoryObj("menu:14:long", "长片·排行"),
		categoryObj("menu:17:long", "长片·中文"),
		categoryObj("menu:16:long", "长片·无码"),
		categoryObj("menu:19:short", "短片·最新"),
		categoryObj("menu:20:short", "短片·排行"),
		categoryObj("menu:21:short", "短片·探花"),
		categoryObj("menu:23:short", "短片·美女"),
		categoryObj("menu:22:short", "短片·素人"),
		categoryObj("types:long", "AV分类·长片"),
		categoryObj("types:short", "视频分类·短片"),
		categoryObj("actors", "女优列表"),
	}
}

func yhLutubeCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case strings.HasPrefix(category, "menu:"):
		p := strings.Split(category, ":")
		if len(p) != 3 {
			return nil, 0, false, fmt.Errorf("yh_lutube: bad menu category %s", category)
		}
		q := url.Values{}
		q.Set("page", fmt.Sprintf("%d", page))
		obj, err := yhLutubeCall("/api/v4/menu/"+p[1]+"/layout", q)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhLutubeListData(slot, obj)
		return items, len(items), len(items) > 0, nil
	case strings.HasPrefix(category, "types:"):
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		typ := strings.TrimPrefix(category, "types:")
		q := url.Values{}
		q.Set("token", yhLutubeToken)
		obj, err := yhLutubeCall("/api/v1/category/"+typ, q)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(obj["data"]) {
			c := yhMap(e)
			if c == nil {
				continue
			}
			cid := g5StrVal(c["id"])
			name := g5StrVal(c["name"])
			if cid == "" || name == "" {
				continue
			}
			// App 内分类点进后是 VideoListActivity 两页签（time/views），
			// 本核展开为两个分类。
			out = append(out, categoryObj("catlist:"+typ+":"+cid+":time", name+"·最新"))
			out = append(out, categoryObj("catlist:"+typ+":"+cid+":views", name+"·最热"))
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "catlist:"):
		// VideoList2Fragment.m(menuId, type, order) 原路：
		// /api/v2/category/{type}/{menu}/{order} query {page, token}。
		p := strings.Split(category, ":")
		if len(p) != 4 {
			return nil, 0, false, fmt.Errorf("yh_lutube: bad catlist category %s", category)
		}
		typ, menu, order := p[1], p[2], p[3]
		q := url.Values{}
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("token", yhLutubeToken)
		obj, err := yhLutubeCall("/api/v2/category/"+typ+"/"+menu+"/"+order, q)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhLutubeListData(slot, obj)
		return items, len(items), len(items) > 0, nil
	case category == "actors" || strings.HasPrefix(category, "actors:"):
		cup, order := "", "views"
		if strings.HasPrefix(category, "actors:") {
			if p := strings.Split(strings.TrimPrefix(category, "actors:"), ":"); len(p) >= 1 {
				cup = p[0]
			}
			if p := strings.Split(strings.TrimPrefix(category, "actors:"), ":"); len(p) >= 2 {
				order = p[1]
			}
		}
		q := url.Values{}
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("order", order)
		q.Set("cup", cup)
		q.Set("token", yhLutubeToken)
		obj, err := yhLutubeCall("/api/v2/category/actor", q)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		seen := map[string]bool{}
		for _, e := range yhArr(obj["data"]) {
			b := yhMap(e)
			if b == nil {
				continue
			}
			aid := g5StrVal(b["id"])
			name := g5StrVal(b["name"])
			if aid == "" || name == "" || seen[aid] {
				continue
			}
			seen[aid] = true
			out = append(out, makeDrama(slot, "dir:actor:"+aid, name, yhLutubeCover(g5StrVal(b["cover"])), g5StrVal(b["cup"]), "女优", ""))
		}
		return out, len(out), len(out) > 0, nil
	case strings.HasPrefix(category, "dir:actor:"):
		q := url.Values{}
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("token", yhLutubeToken)
		obj, err := yhLutubeCall("/api/v2/category/actor/"+strings.TrimPrefix(category, "dir:actor:")+"/time", q)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhLutubeListData(slot, obj)
		return items, len(items), len(items) > 0, nil
	}
	return yhLutubeCatalog(slot, "menu:13:long", page)
}

func yhLutubeDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "dir:actor:") {
		items, _, _, err := yhLutubeCatalog(slot, rawID, 1)
		if err != nil {
			return nil, err
		}
		d := makeDrama(slot, rawID, strings.TrimPrefix(rawID, "dir:actor:"), "", "女优", "女优", "")
		epIDs, names := []string{}, []string{}
		for _, it := range items {
			if im, ok := it.(map[string]interface{}); ok {
				epIDs = append(epIDs, g5StrVal(im["vod_id"]))
				names = append(names, g5StrVal(im["title"]))
			}
		}
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, names)
			d["related"] = items
		}
		return d, nil
	}
	// 长短未知时先长片 info，不成再短片 info（App 按入口分流，本核双试）。
	yhLutubeEnsure()
	var obj map[string]interface{}
	var err error
	q := url.Values{}
	q.Set("host", yhLutubeStream)
	if obj, err = yhLutubeCall("/api/v2/long/info/"+rawID, q); err != nil {
		q2 := url.Values{}
		q2.Set("token", yhLutubeToken)
		q2.Set("host", yhLutubeStream)
		obj, err = yhLutubeCall("/api/v2/short/info/"+rawID, q2)
	}
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_lutube: video info empty for %s", rawID)
	}
	title := g5StrVal(data["title"])
	d := makeDrama(slot, rawID, title, "", "", "", "")
	names, ids := []string{}, []string{}
	for _, q := range []struct {
		key, name string
	}{{"480", "480P"}, {"240", "240P"}, {"intro", "预览"}} {
		if v := yhMap(data["urls"]); v != nil && g5StrVal(v[q.key]) != "" {
			names = append(names, q.name)
			ids = append(ids, rawID+"$q:"+q.key)
		}
	}
	if len(ids) == 0 {
		ids, names = []string{rawID}, []string{"正片"}
	}
	g5SetChapters(d, slot, rawID, ids, names)
	return d, nil
}

func yhLutubeResolve(slot, rawID string) (map[string]interface{}, error) {
	vid, quality := rawID, ""
	if i := strings.Index(rawID, "$"); i >= 0 {
		vid, quality = rawID[:i], rawID[i+1:]
	}
	if strings.HasPrefix(quality, "q:") {
		quality = strings.TrimPrefix(quality, "q:")
	}
	// Detail 已取 urls；Resolve 复用同一 info 链（长→短双试）。
	yhLutubeEnsure()
	var obj map[string]interface{}
	var err error
	q := url.Values{}
	q.Set("host", yhLutubeStream)
	if obj, err = yhLutubeCall("/api/v2/long/info/"+vid, q); err != nil {
		q2 := url.Values{}
		q2.Set("token", yhLutubeToken)
		q2.Set("host", yhLutubeStream)
		obj, err = yhLutubeCall("/api/v2/short/info/"+vid, q2)
	}
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_lutube: video info empty for %s", vid)
	}
	urls := yhMap(data["urls"])
	if urls == nil {
		return nil, fmt.Errorf("yh_lutube: no urls for %s", vid)
	}
	pick := func() string {
		if quality != "" {
			if s := g5StrVal(urls[quality]); s != "" {
				return s
			}
		}
		if s := g5StrVal(urls["480"]); s != "" {
			return s
		}
		if s := g5StrVal(urls["240"]); s != "" {
			return s
		}
		return g5StrVal(urls["intro"])
	}
	p := pick()
	if p == "" {
		return nil, fmt.Errorf("yh_lutube: no play url for %s", vid)
	}
	u := p
	if !strings.HasPrefix(p, "http") {
		u = strings.TrimSuffix(yhLutubeAPI, "/") + "/" + strings.TrimPrefix(p, "/")
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhLutubeUA, yhLutubeOrigin+"/", format, protocol), nil
}

func yhLutubeSearch(slot, kw string) ([]interface{}, error) {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, typ := range []string{"long", "short"} {
		q := url.Values{}
		q.Set("page", "1")
		q.Set("query", kw)
		q.Set("token", yhLutubeToken)
		obj, err := yhLutubeCall("/api/v2/"+typ+"/search/keyword", q)
		if err != nil {
			continue
		}
		for _, it := range yhLutubeListData(slot, obj) {
			if im, ok := it.(map[string]interface{}); ok {
				id := g5StrVal(im["vod_id"])
				if !seen[id] {
					seen[id] = true
					out = append(out, it)
				}
			}
		}
	}
	return out, nil
}

func init() {
	registerSite(yhLutubeSlot, &siteImpl{
		Key:        yhLutubeSlot,
		Name:       "撸Tube",
		DefaultCat: "menu:13:long",
		HomeURL:    yhLutubeDefaultAPI + "/",
		Headers:    yhLutubeHeaders,
		Categories: yhLutubeCategories,
		Catalog:    yhLutubeCatalog,
		Detail:     yhLutubeDetail,
		Resolve:    yhLutubeResolve,
		Search:     yhLutubeSearch,
	})
	// Integration: append "yh_lutube" to slotOrder (registry.go tail) and
	// add the builtin_config entry — see port_out integration notes.
	// NOTE: discovery chain currently dead (see header); the impl above
	// is complete and will activate automatically if a candidate host
	// file comes back online.
}
