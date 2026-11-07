package seku

// ---- 虾米 xiami (yh_xiami, type 15) — 视频+小说+图集三栖 ----
//
// 基址域名 tu9im1a569.com 已死亡: 2026-10-24 经
// Google 公共 DNS (dns.google/resolve) 实查返回 Status 3 (NXDOMAIN,
// 非本机出口问题, 为全局解析不存在); 浏览器出口直取同样不可解析;
// 本机 shell 出口当日全灭 (含对照 baidu.com 亦 000) 只作旁证。
// 全路换路已穷: APK 全量字符串/decoded 串/远程配置快照内无任何备用
// API 域名 (仅媒体 CDN xiamivideo.b-cdn.net), h6.b.H 无远程覆盖项
// (config_remote.json 仅 XG_APP)。按 PORTING_GUIDE 例外口径①
// (服务端已死亡/域名停放) 本站以完整代码 + 未验回执交付: 域名一旦
// 复活或经远程配置换址, 改 yhXiamiBase 一行即可冒烟。
//
// 逆向存疑串回 native 层 capstone 逐指令复核
// (libnative-lib.so, 2026-10-24): getXiaMiKey @0xecac 的
// NewStringUTF 实参指针精确落 0x27271 = "DES/EBC/PKCS5PAD" (16B),
// getXiaMiIv @0xecf0 落 0x27282 = "RequestContext00" (16B), 无任何
// 二次变换/偏移错误 — 两串虽形似变换名/上下文片段, 确为 y5.a 实际
// 使用的 AES-128-CBC/PKCS5Padding key/IV 原文 (y5.a 直接 getBytes
// 作 SecretKeySpec/IvParameterSpec)。因域名 NXDOMAIN 无真实响应
// 可拉, "以一条真实响应验 key" 一步无从执行, 此为唯一未闭合点;
// 代码按 native 实证原值实现, 待域名复活后一条 /index/home 响应
// 即可终验 (解密成功判据: data 段 JSON 可解析且 code=200)。
//
// 配方 (jadx y5.* 逐行复刻):
// 基址 http://tu9im1a569.com:81 (h6.b.H), Retrofit y5.b:
// GET /index/home 首页推荐
// GET /index/hot?pageNo&channel&type=1|2 最新/最热
// GET /index/featured?pageNo&channel 精选专题列表
// GET /index/featured/info?featuredId&pageNo&channel 专题作品
// GET /index/categoryVideoInfo?pageNo&channel&childId&categoryId
// GET /index/galleryInfo?pageNo&channel&childId 图集列表
// GET /index/galleryGroupInfo?galleryId&channel 图集详情
// GET /fiction/list?pageNo&channel&childId 小说列表
// GET /fiction/info?fictionId&channel 小说详情(正文)
// POST /index/search (form pageNo&channel&keyWord) 搜索(仅视频)
// POST /index/videoInfo (form channel&videoId&device) 视频详情
// channel 恒 "web_official"; device = h6.b.I =
// 45932A8C0B3F08C6F597FBCCF00E6479 (XM_DEVICE, jadx 实读一致)。
// 拦截器 y5.f: 逐请求加头 timestamp=<毫秒>, sign=MD5hex(timestamp +
// "cca5baf7ab20f74500YIUHIUHlmlkjjl203a93d72601836611"); 响应 JSON
// 的 data 字符串段以 y5.a 解密后回填 (Base64 NO_WRAP, AES/CBC/PKCS5,
// key/IV 见上)。分类树 = APK assets/xiami.txt 快照 (39 类, 与 App
// 导航 1:1: 首页推荐/最新/最热/精选专题 + 精选系列 10 + 激情视频 7
// + 美女写真 9 + 情色小说 9, 子分类含"全部"childId=0)。
// 付费口径: 列表条目 videoVip 为真者过滤, 详情 isPlay=false 或
// videoVip 为真者不解析播放 (账本口径, 代码级落实)。
// 验证状态: 全腿出口未验 (域名 NXDOMAIN, 见顶部证据), 代码逐行实证。

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

const yhXiamiBase = "http://tu9im1a569.com:81"
const yhXiamiUA = "okhttp/4.9.0"
const yhXiamiDevice = "45932A8C0B3F08C6F597FBCCF00E6479"
const yhXiamiChannel = "web_official"
const yhXiamiSignSuffix = "cca5baf7ab20f74500YIUHIUHlmlkjjl203a93d72601836611"

var yhXiamiKey = []byte("DES/EBC/PKCS5PAD")
var yhXiamiIV = []byte("RequestContext00")

var yhXiamiCache sync.Map // rawID -> map[string]interface{}

func yhXiamiHeaders() map[string]string {
	ts := fmt.Sprint(time.Now().UnixMilli())
	sum := md5.Sum([]byte(ts + yhXiamiSignSuffix))
	return map[string]string{
		"User-Agent": yhXiamiUA,
		"timestamp":  ts,
		"sign":       fmt.Sprintf("%x", sum),
		"Accept":     "*/*",
	}
}

func yhXiamiUnpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > 16 || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}

// yhXiamiDecrypt 复刻 y5.a: Base64(NO_WRAP) -> AES/CBC/PKCS5 解密。
func yhXiamiDecrypt(s string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		// NO_WRAP 解码容忍换行缺失填充
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return "", err
		}
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return "", fmt.Errorf("yh_xiami: bad cipher len %d", len(raw))
	}
	block, err := aes.NewCipher(yhXiamiKey)
	if err != nil {
		return "", err
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhXiamiIV).CryptBlocks(out, raw)
	return string(yhXiamiUnpad(out)), nil
}

// yhXiamiCall 发起请求并按 y5.f 回填解密 data 段, 返回整包对象。
func yhXiamiCall(method, path string, params url.Values) (map[string]interface{}, error) {
	u := yhXiamiBase + path
	var body *bytes.Reader
	if method == "POST" {
		body = bytes.NewReader([]byte(params.Encode()))
	} else {
		if len(params) > 0 {
			u += "?" + params.Encode()
		}
		body = bytes.NewReader(nil)
	}
	h := yhXiamiHeaders()
	if method == "POST" {
		h["Content-Type"] = "application/x-www-form-urlencoded"
	}
	b, code, _, err := httpDo(method, u, body, h)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_xiami http %d %s", code, path)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("yh_xiami parse %s: %v", path, err)
	}
	if ds, ok := obj["data"].(string); ok && ds != "" {
		plain, derr := yhXiamiDecrypt(ds)
		if derr != nil {
			return nil, fmt.Errorf("yh_xiami decrypt %s: %v", path, derr)
		}
		var dv interface{}
		if err := json.Unmarshal([]byte(plain), &dv); err != nil {
			return nil, fmt.Errorf("yh_xiami decrypt-parse %s: %v", path, err)
		}
		obj["data"] = dv
	}
	if c, ok := obj["code"].(float64); ok && c != 200 {
		return nil, fmt.Errorf("yh_xiami code %v: %s", c, g5StrVal(obj["msg"]))
	}
	return obj, nil
}

func yhXiamiParams(page int) url.Values {
	return url.Values{"pageNo": {fmt.Sprint(page)}, "channel": {yhXiamiChannel}}
}

func yhXiamiIsVIP(rec map[string]interface{}) bool {
	v := rec["videoVip"]
	if v == nil {
		return false
	}
	return g5Truthy(v) || g5StrVal(v) == "2"
}

func yhXiamiVideoCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	vid := g5StrVal(rec["videoId"])
	title := g5StrVal(rec["videoTitle"])
	if title == "" {
		title = g5StrVal(rec["title"])
	}
	if vid == "" || title == "" {
		return nil, false
	}
	if yhXiamiIsVIP(rec) {
		return nil, false // 付费过滤 (账本口径)
	}
	rawID := "vid:" + vid
	yhXiamiCache.Store(rawID, rec)
	cover := g5StrVal(rec["videoImage"])
	if cover == "" {
		cover = g5StrVal(rec["image"])
	}
	remark := g5StrVal(rec["videoTime"])
	desc := ""
	if vv := g5StrVal(rec["videoViews"]); vv != "" {
		desc = "播放 " + vv
	}
	return makeDrama(slot, rawID, title, cover, remark, g5StrVal(rec["videoType"]), desc), true
}

func yhXiamiVideoCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if rm := yhMap(e); rm != nil {
			if c, ok := yhXiamiVideoCard(slot, rm); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

type yhXiamiCat struct {
	id     string
	name   string
	parent int
	child  int
	kind   string // v=视频 g=图集 n=小说
}

var yhXiamiSnapshot = []yhXiamiCat{
	// 精选系列 (parent 9, type 2)
	{"v:9:0", "精选系列", 9, 0, "v"}, {"v:9:10", "精选系列·裙底偷拍", 9, 10, "v"},
	{"v:9:11", "精选系列·迷奸记", 9, 11, "v"}, {"v:9:12", "精选系列·邻居偷情", 9, 12, "v"},
	{"v:9:14", "精选系列·演艺圈", 9, 14, "v"}, {"v:9:15", "精选系列·美女主播", 9, 15, "v"},
	{"v:9:16", "精选系列·户外", 9, 16, "v"}, {"v:9:17", "精选系列·酒店偷拍", 9, 17, "v"},
	{"v:9:18", "精选系列·萌白酱", 9, 18, "v"}, {"v:9:19", "精选系列·性感教师", 9, 19, "v"},
	// 激情视频 (parent 20, type 2)
	{"v:20:0", "激情视频", 20, 0, "v"}, {"v:20:30", "激情视频·亚洲虾米", 20, 30, "v"},
	{"v:20:31", "激情视频·欧美虾米", 20, 31, "v"}, {"v:20:32", "激情视频·另类虾米", 20, 32, "v"},
	{"v:20:33", "激情视频·动漫虾米", 20, 33, "v"}, {"v:20:48", "激情视频·国产虾米", 20, 48, "v"},
	{"v:20:49", "激情视频·中文字幕", 20, 49, "v"},
	// 美女写真 (parent 21, type 3)
	{"g:21:0", "美女写真", 21, 0, "g"}, {"g:21:23", "美女写真·自拍偷拍", 21, 23, "g"},
	{"g:21:24", "美女写真·亚洲色图", 21, 24, "g"}, {"g:21:25", "美女写真·欧美色图", 21, 25, "g"},
	{"g:21:26", "美女写真·美腿丝袜", 21, 26, "g"}, {"g:21:27", "美女写真·清纯唯美", 21, 27, "g"},
	{"g:21:29", "美女写真·卡通动漫", 21, 29, "g"}, {"g:21:46", "美女写真·街拍套图", 21, 46, "g"},
	{"g:21:47", "美女写真·GIF动图", 21, 47, "g"},
	// 情色小说 (parent 22, type 4)
	{"n:22:0", "情色小说", 22, 0, "n"}, {"n:22:34", "情色小说·都市激情", 22, 34, "n"},
	{"n:22:35", "情色小说·人妻交换", 22, 35, "n"}, {"n:22:36", "情色小说·校园春色", 22, 36, "n"},
	{"n:22:37", "情色小说·家庭乱伦", 22, 37, "n"}, {"n:22:38", "情色小说·情色笑话", 22, 38, "n"},
	{"n:22:39", "情色小说·性爱技巧", 22, 39, "n"}, {"n:22:40", "情色小说·武侠古典", 22, 40, "n"},
	{"n:22:41", "情色小说·另类小说", 22, 41, "n"},
}

func yhXiamiFindCat(id string) *yhXiamiCat {
	for i := range yhXiamiSnapshot {
		if yhXiamiSnapshot[i].id == id {
			return &yhXiamiSnapshot[i]
		}
	}
	return nil
}

func yhXiamiCategories(slot string) []interface{} {
	out := []interface{}{
		categoryObj("home", "首页推荐"),
		categoryObj("hot:1", "最新"),
		categoryObj("hot:2", "最热"),
		categoryObj("featured", "精选专题"),
	}
	for _, c := range yhXiamiSnapshot {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func yhXiamiCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "home":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhXiamiCall("GET", "/index/home", nil)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		seen := map[string]bool{}
		data := yhMap(obj["data"])
		if data != nil {
			for _, g := range yhArr(data["video"]) {
				if gm := yhMap(g); gm != nil {
					for _, c := range yhXiamiVideoCards(slot, yhArr(gm["info"])) {
						id := g5StrVal(c.(map[string]interface{})["vod_id"])
						if !seen[id] {
							seen[id] = true
							out = append(out, c)
						}
					}
				}
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "hot:"):
		p := yhXiamiParams(page)
		p.Set("type", strings.TrimPrefix(category, "hot:"))
		obj, err := yhXiamiCall("GET", "/index/hot", p)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhXiamiVideoCards(slot, yhArr(obj["data"]))
		return items, len(items), len(items) > 0, nil
	case category == "featured":
		p := yhXiamiParams(page)
		obj, err := yhXiamiCall("GET", "/index/featured", p)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(obj["data"]) {
			rm := yhMap(e)
			if rm == nil {
				continue
			}
			fid := g5StrVal(rm["featuredId"])
			title := g5StrVal(rm["title"])
			if fid == "" || title == "" {
				continue
			}
			cover := g5StrVal(rm["image"])
			if cover == "" {
				cover = g5StrVal(rm["videoImage"])
			}
			out = append(out, makeDrama(slot, "dir:featured:"+fid, title, cover, "专题", "精选专题", ""))
		}
		return out, len(out), len(out) > 0, nil
	case strings.HasPrefix(category, "dir:featured:"):
		p := yhXiamiParams(page)
		p.Set("featuredId", strings.TrimPrefix(category, "dir:featured:"))
		obj, err := yhXiamiCall("GET", "/index/featured/info", p)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhXiamiVideoCards(slot, yhArr(obj["data"]))
		return items, len(items), len(items) > 0, nil
	}
	cat := yhXiamiFindCat(category)
	if cat == nil {
		return []interface{}{}, 0, false, nil
	}
	p := yhXiamiParams(page)
	p.Set("childId", fmt.Sprint(cat.child))
	switch cat.kind {
	case "v":
		p.Set("categoryId", fmt.Sprint(cat.parent))
		obj, err := yhXiamiCall("GET", "/index/categoryVideoInfo", p)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhXiamiVideoCards(slot, yhArr(obj["data"]))
		return items, len(items), len(items) > 0, nil
	case "g":
		obj, err := yhXiamiCall("GET", "/index/galleryInfo", p)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(obj["data"]) {
			rm := yhMap(e)
			if rm == nil {
				continue
			}
			gid := g5StrVal(rm["galleryId"])
			title := g5StrVal(rm["galleryTitle"])
			if gid == "" || title == "" {
				continue
			}
			rawID := "gal:" + gid
			yhXiamiCache.Store(rawID, rm)
			remark := g5StrVal(rm["galleryCounts"]) + "张"
			out = append(out, makeDrama(slot, rawID, title, g5StrVal(rm["galleryImage"]), remark, g5StrVal(rm["categoryTitle"]), ""))
		}
		return out, len(out), len(out) > 0, nil
	case "n":
		obj, err := yhXiamiCall("GET", "/fiction/list", p)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(obj["data"]) {
			rm := yhMap(e)
			if rm == nil {
				continue
			}
			fid := g5StrVal(rm["fictionId"])
			title := g5StrVal(rm["title"])
			if fid == "" || title == "" {
				continue
			}
			rawID := "nov:" + fid
			yhXiamiCache.Store(rawID, rm)
			remark := g5StrVal(rm["author"])
			out = append(out, makeDrama(slot, rawID, title, "", remark, g5StrVal(rm["categoryTitle"]), ""))
		}
		return out, len(out), len(out) > 0, nil
	}
	return []interface{}{}, 0, false, nil
}

func yhXiamiVideoDetail(slot, vid string) (map[string]interface{}, error) {
	p := url.Values{"channel": {yhXiamiChannel}, "videoId": {vid}, "device": {yhXiamiDevice}}
	obj, err := yhXiamiCall("POST", "/index/videoInfo", p)
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_xiami: videoInfo empty for %s", vid)
	}
	yhXiamiCache.Store("detail:vid:"+vid, data)
	return data, nil
}

func yhXiamiDetail(slot, rawID string) (map[string]interface{}, error) {
	switch {
	case strings.HasPrefix(rawID, "dir:featured:"):
		items, _, _, err := yhXiamiCatalog(slot, rawID, 1)
		if err != nil {
			return nil, err
		}
		title := strings.TrimPrefix(rawID, "dir:featured:")
		cover := ""
		if v, ok := yhXiamiCache.Load(rawID); ok {
			_ = v
		}
		d := makeDrama(slot, rawID, title, cover, "专题", "精选专题", "")
		epIDs, epNames := []string{}, []string{}
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
	case strings.HasPrefix(rawID, "vid:"):
		vid := strings.TrimPrefix(rawID, "vid:")
		data, err := yhXiamiVideoDetail(slot, vid)
		if err != nil {
			return nil, err
		}
		d := makeDrama(slot, rawID, g5StrVal(data["videoTitle"]), g5StrVal(data["videoImage"]), g5StrVal(data["videoTime"]), "", g5StrVal(data["videoViews"]))
		if yhXiamiIsVIP(data) || (data["isPlay"] != nil && !g5Truthy(data["isPlay"])) {
			// 付费/不可播: 只回信息, 不给章节 (Resolve 同步拒解析)
			d["desc"] = strings.TrimSpace(g5StrVal(d["desc"]) + " " + g5StrVal(data["vipTitle"]))
			d["chapters"] = []interface{}{}
			d["episodeList"] = []interface{}{}
			return d, nil
		}
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
		related := yhXiamiVideoCards(slot, yhArr(data["info"]))
		if len(related) > 0 {
			d["related"] = related
		}
		return d, nil
	case strings.HasPrefix(rawID, "gal:"):
		gid := strings.TrimPrefix(rawID, "gal:")
		p := url.Values{"galleryId": {gid}, "channel": {yhXiamiChannel}}
		obj, err := yhXiamiCall("GET", "/index/galleryGroupInfo", p)
		if err != nil {
			return nil, err
		}
		data := yhMap(obj["data"])
		if data == nil {
			return nil, fmt.Errorf("yh_xiami: gallery empty for %s", gid)
		}
		title, cover := gid, ""
		if v, ok := yhXiamiCache.Load(rawID); ok {
			rec := v.(map[string]interface{})
			title = g5StrVal(rec["galleryTitle"])
			cover = g5StrVal(rec["galleryImage"])
		}
		d := makeDrama(slot, rawID, title, cover, g5StrVal(data["galleryCounts"]), g5StrVal(data["categoryTitle"]), "")
		epIDs, epNames, imgs := []string{}, []string{}, []string{}
		for i, e := range yhArr(data["group"]) {
			gm := yhMap(e)
			if gm == nil {
				continue
			}
			u := g5StrVal(gm["url"])
			if u == "" {
				u = g5StrVal(gm["high"])
			}
			if u == "" {
				continue
			}
			imgs = append(imgs, u)
			epIDs = append(epIDs, fmt.Sprintf("%s:%d", rawID, i))
			epNames = append(epNames, fmt.Sprintf("第%d张", i+1))
		}
		yhXiamiCache.Store("imgs:"+rawID, imgs)
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, epNames)
		}
		related := []interface{}{}
		for _, e := range yhArr(data["hot"]) {
			if hm := yhMap(e); hm != nil {
				hid := g5StrVal(hm["galleryId"])
				ht := g5StrVal(hm["galleryTitle"])
				if hid != "" && ht != "" {
					related = append(related, makeDrama(slot, "gal:"+hid, ht, g5StrVal(hm["galleryImage"]), "", "图集", ""))
				}
			}
		}
		if len(related) > 0 {
			d["related"] = related
		}
		return d, nil
	case strings.HasPrefix(rawID, "nov:"):
		fid := strings.TrimPrefix(rawID, "nov:")
		p := url.Values{"fictionId": {fid}, "channel": {yhXiamiChannel}}
		obj, err := yhXiamiCall("GET", "/fiction/info", p)
		if err != nil {
			return nil, err
		}
		data := yhMap(obj["data"])
		if data == nil {
			return nil, fmt.Errorf("yh_xiami: fiction empty for %s", fid)
		}
		yhXiamiCache.Store("detail:nov:"+fid, data)
		content := g5StrVal(data["content"])
		desc := content
		if len([]rune(desc)) > 300 {
			desc = string([]rune(desc)[:300])
		}
		d := makeDrama(slot, rawID, g5StrVal(data["title"]), "", g5StrVal(data["author"]), g5StrVal(data["categoryTitle"]), desc)
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正文"})
		related := []interface{}{}
		for _, e := range yhArr(data["info"]) {
			if rm := yhMap(e); rm != nil {
				rid := g5StrVal(rm["fictionId"])
				rt := g5StrVal(rm["title"])
				if rid != "" && rt != "" {
					related = append(related, makeDrama(slot, "nov:"+rid, rt, "", g5StrVal(rm["author"]), "小说", ""))
				}
			}
		}
		if len(related) > 0 {
			d["related"] = related
		}
		return d, nil
	}
	return nil, fmt.Errorf("yh_xiami: unknown id %s", rawID)
}

func yhXiamiResolve(slot, rawID string) (map[string]interface{}, error) {
	ep := rawID
	if i := strings.Index(ep, "$"); i >= 0 {
		ep = ep[i+1:]
	}
	switch {
	case strings.HasPrefix(ep, "vid:"):
		vid := strings.TrimPrefix(ep, "vid:")
		data, err := yhXiamiVideoDetail(slot, vid)
		if err != nil {
			return nil, err
		}
		if yhXiamiIsVIP(data) || (data["isPlay"] != nil && !g5Truthy(data["isPlay"])) {
			return nil, fmt.Errorf("yh_xiami: %s is VIP/not playable, refused", vid)
		}
		u := g5StrVal(data["videoUrl"])
		if u == "" {
			return nil, fmt.Errorf("yh_xiami: no play url for %s", vid)
		}
		format, protocol := "mp4", ""
		if strings.Contains(u, ".m3u8") {
			format, protocol = "m3u8", "hls"
		}
		return playResult(u, slot, rawID, yhXiamiUA, yhXiamiBase+"/", format, protocol), nil
	case strings.HasPrefix(ep, "gal:"):
		// ep 形如 gal:<id>:<idx>
		rest := strings.TrimPrefix(ep, "gal:")
		idx := strings.LastIndex(rest, ":")
		if idx < 0 {
			return nil, fmt.Errorf("yh_xiami: bad gallery chapter %s", ep)
		}
		galID, num := rest[:idx], rest[idx+1:]
		var n int
		fmt.Sscanf(num, "%d", &n)
		v, ok := yhXiamiCache.Load("imgs:gal:" + galID)
		if !ok {
			if _, err := yhXiamiDetail(slot, "gal:"+galID); err != nil {
				return nil, err
			}
			v, ok = yhXiamiCache.Load("imgs:gal:" + galID)
			if !ok {
				return nil, fmt.Errorf("yh_xiami: gallery %s not loaded", galID)
			}
		}
		imgs := v.([]string)
		if n < 0 || n >= len(imgs) {
			return nil, fmt.Errorf("yh_xiami: gallery page %d out of range", n)
		}
		r := playResult(imgs[n], slot, rawID, yhXiamiUA, yhXiamiBase+"/", "images", "")
		r["images"] = imgs
		return r, nil
	case strings.HasPrefix(ep, "nov:"):
		fid := strings.TrimPrefix(ep, "nov:")
		v, ok := yhXiamiCache.Load("detail:nov:" + fid)
		if !ok {
			if _, err := yhXiamiDetail(slot, ep); err != nil {
				return nil, err
			}
			v, ok = yhXiamiCache.Load("detail:nov:" + fid)
			if !ok {
				return nil, fmt.Errorf("yh_xiami: fiction %s not loaded", fid)
			}
		}
		data := v.(map[string]interface{})
		content := g5StrVal(data["content"])
		u := g5StrVal(data["url"])
		if u == "" {
			u = yhXiamiBase + "/fiction/info?fictionId=" + fid
		}
		r := playResult(u, slot, rawID, yhXiamiUA, yhXiamiBase+"/", "text", "")
		r["content"] = content
		r["contentType"] = "text/plain"
		return r, nil
	}
	return nil, fmt.Errorf("yh_xiami: unknown id %s", rawID)
}

func yhXiamiSearch(slot, kw string) ([]interface{}, error) {
	p := yhXiamiParams(1)
	p.Set("keyWord", kw)
	obj, err := yhXiamiCall("POST", "/index/search", p)
	if err != nil {
		return nil, err
	}
	return yhXiamiVideoCards(slot, yhArr(obj["data"])), nil
}

func init() {
	registerSite("yh_xiami", &siteImpl{
		Key:        "yh_xiami",
		Name:       "虾米",
		DefaultCat: "home",
		HomeURL:    yhXiamiBase + "/",
		Categories: yhXiamiCategories,
		Catalog:    yhXiamiCatalog,
		Detail:     yhXiamiDetail,
		Resolve:    yhXiamiResolve,
		Search:     yhXiamiSearch,
		Headers:    yhXiamiHeaders,
	}, "yh_xiami", "xiami", "虾米")
}
