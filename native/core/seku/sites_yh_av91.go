package seku

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---- av91 (yh_av91) — PWA 加密 API, 分发式单端点 ----
//
// 配方出欲火 APK jadx 实证 (ui/av91 + n0 ApiService/Av91Utils/拦截器
// + h6.b 默认基址):
//   base = https://api3.bwkhawkdv.cc (h6.b.C0, 写死)
//   单端点 POST {base}/api.php, mod/code 分发 (n0.a 全方法同路径)
//   信封 (n0.c.a/b + n0.b 拦截器):
//     外层 form: client=pwa, timestamp=<秒>, data, sign
//     data = Base64( AES-128-CBC/PKCS7( JSON(biz), key, iv ) )
//     key = f1b97d6206b8ccd91b75f90d97cb11ea, iv = bbb5e8de80737ee0
//     sign = MD5hex( SHA256hex( "client=pwa&data="+data+
//            "&timestamp="+ts+"f479a05f2c1b6688aed5c4a5213fcdfe" ) )
//     响应 {"data":"<Base64>"} 同 key/iv 解密 (n0.b 拦截器)
//   biz 公共 (n0.c.b): oauth_id=8X5CLgmXrHhDEsxG_1784029458860
//   (单值), bundleId=com.pwa.jyav, version=4.5.1,
//   oauth_type=pwa, language=zh, via=web, _hash=<毫秒>.
//   端点 (n0.a, 均 /api.php):
//     每日播单 a.o {mod:mv, code:playlist, id:1, page}
//     构造 a.p {mod:element, code:getConstructById, id, page}
//     主题 a.n {mod:mv, code:mvTopicList}
//     推荐 a.r {mod:mv, code:recommendAll}
//     女优 a.q {mod:Mvactor, code:actorList, sort, cup_size, page,
//       limit:32}; 女优详情 a.r {mod:mvactor, code:actorDetail, id,
//       sort:praise}
//     筛选 a.p {mod:mv, code:search, sort, cate, toll_type, page,
//       limit:16}
//     热门 a.m {sort:popular, page, limit:16} (无 mod/code)
//     黑料 a.h {sort:new, id:1, page, limit:16}
//     吃瓜详情 a.f {id}
//     短视频 a.c {sort:rec, page} (仿 utube 同族)
//     直播 a.g {id:1, page, limit:16}; 直播详情 a.j {id}
//     详情 a.d {mod:mv, code:detail, id}
//     搜索 a.p {mod:seek, code:seekInAll, keywords, type:0,
//       last_ix:24, page}
//   翻页 (ElementVideo.fitData): 响应中 type=4 元素的
//   more_api_params {mod, code, construct_id, id, page, date}
//   拼下一页; 本实现简化为同业务 page 递增, 与 App 差异如实注记.
//   ElementVideo 展平 (bean getAllVideo): 优先 data.data 列表,
//   其次 mv_recomment_official, 其次 mvs.data, 最后遍历
//   elements 中 type=4 者取其 value 列表.
// 分类与 App 导航 1:1 (Home/Theme/Actor/Screen 四 tab 展开):
//   每日播单 / 构造 3 最新创作 / 4 热门影片 / 7 暗网猎奇 /
//   2 华语国产 / 5 VIP专区 / 12 禁漫天堂 / 9 日本AV / 主题 /
//   推荐 / 女优·人气 / 女优·片量 / 筛选 (cate/sort/toll 组合,
//   组合以 category 编码, 见 yhAv91Categories).
//   (热门/黑料/短视频/直播四 tab 因服务端裸业务路由现状
//   未挂分类, 见文件头验真段.)
//   女优 cup_size 组合以 "actor:<sort>:<cup>" 编码, cup 全部="",
//   A–M 单字母; 筛选以 "screen:<sort>:<cate>:<toll>" 编码.
//   ACG (album/cartoon) 为图集非视频, 本单不发视频卡.
// 播放 (PlayInfo.getPlayUrl): mv.m3u8 -> detail.m3u8 ->
//   detail.preview_url (hf- 改写) -> source_720/480/240;
//   直播取 hls[0].url.
// 付费: 列表过滤 toll_type 3(VIP)/4(钻石) 与 is_pay=1,
//   及 (coins>0 且 isfree=2) 的金币片; 详情同规则拒解析.
// 封面: 本族封面字节 AES-128-CBC 加密, 族共享 Glide 密钥
//   (yhAv91CoverFetch; 密钥已在 melon51/utube 同族图床实测解密出
//   明文魔数, av91 自身图床待冒烟时以 CoverFetch 验).
// 验真 (2026-10-24 本机, 含 jadx 推断被实测纠正处):
//   element getConstructById id=3 实拉 200, type=4 元素 20 条视频
//   经查全为付费 (15 VIP toll=3 + 5 钻石 toll=4), 故按付费过滤后
//   该分类首屏 0 免费卡——是站方现状, 非展平故障 (同查 id=4、
//   search、recommend 首屏亦全付费); 免费内容经筛选 toll=1 腿
//   实拉 16 条、主题腿 13 个文件夹、每日播单 3 组、女优腿 32 条,
//   均已验; 每日播单 3 组各 6 条经查亦全付费, 故首屏 0 免费卡.
//   详情响应实测为 {data:{mv}} 无 detail 键, 标题取 mv (见 Detail).
//   封面 cover_full/cover_thumb 为 JSON 串
//   {"ori","360","720"}, 本件解出 ori 直链 (见 yhAv91UnwrapCover).
//   裸 sort/id 业务腿 (热门 sort=popular、短视频 sort=rec、
//   黑料 sort=new、直播 id=1) 经实测: 服务端当前对一切无
//   mod/code 业务统一回站点配置体 (pwa_site/im_*/bury_point),
//   非视频列表, 故本件不挂这四分类 (jadx 片段链本身无
//   mod/code, 属服务端现行路由与旧版 jadx 不一致, 非配方错);
//   待服务端恢复裸业务路由或新版配方出现后再补.
//   搜索 type 经实测纠正为 "mv" (见 Search 注释).

const yhAv91Slot = "yh_av91"
const yhAv91Base = "https://api3.bwkhawkdv.cc"
const yhAv91UA = "Mozilla/5.0 (Linux; Android 13; SM-A205GN) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"

var yhAv91Key = []byte("f1b97d6206b8ccd91b75f90d97cb11ea")
var yhAv91IV = []byte("bbb5e8de80737ee0")

const yhAv91Secret = "f479a05f2c1b6688aed5c4a5213fcdfe"

func yhAv91Headers() map[string]string {
	return map[string]string{
		"User-Agent":   yhAv91UA,
		"Content-Type": "application/x-www-form-urlencoded",
		"Referer":      yhAv91Base + "/",
	}
}

func yhAv91Pad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhAv91Enc(plain []byte) string {
	block, _ := aes.NewCipher(yhAv91Key)
	out := make([]byte, len(yhAv91Pad(plain)))
	cipher.NewCBCEncrypter(block, yhAv91IV).CryptBlocks(out, yhAv91Pad(plain))
	return base64.StdEncoding.EncodeToString(out)
}

func yhAv91Dec(b64s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64s)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw)%16 != 0 {
		return nil, fmt.Errorf("yh_av91 bad cipher len")
	}
	block, _ := aes.NewCipher(yhAv91Key)
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhAv91IV).CryptBlocks(out, raw)
	n := int(out[len(out)-1])
	if n < 1 || n > 16 || n > len(out) {
		return nil, fmt.Errorf("yh_av91 bad pad")
	}
	return out[:len(out)-n], nil
}

func yhAv91Sign(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", md5.Sum([]byte(hex.EncodeToString(sum[:]))))
}

func yhAv91Call(biz map[string]string) (map[string]interface{}, error) {
	b := map[string]string{
		"oauth_id":   "8X5CLgmXrHhDEsxG_1784029458860",
		"bundleId":   "com.pwa.jyav",
		"version":    "4.5.1",
		"oauth_type": "pwa",
		"language":   "zh",
		"via":        "web",
		"_hash":      strconv.FormatInt(time.Now().UnixMilli(), 10),
	}
	for k, v := range biz {
		b[k] = v
	}
	jb, _ := json.Marshal(b)
	data := yhAv91Enc(jb)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := yhAv91Sign("client=pwa&data=" + data + "&timestamp=" + ts + yhAv91Secret)
	form := url.Values{
		"client":    {"pwa"},
		"timestamp": {ts},
		"data":      {data},
		"sign":      {sign},
	}.Encode()
	var body []byte
	var code int
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err = httpDo("POST", yhAv91Base+"/api.php", strings.NewReader(form), yhAv91Headers())
		if err == nil && code == 200 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_av91 http %d", code)
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("yh_av91 outer parse: %v", err)
	}
	enc := g5StrVal(outer["data"])
	if enc == "" {
		return outer, nil
	}
	plain, err := yhAv91Dec(enc)
	if err != nil {
		return nil, fmt.Errorf("yh_av91 decrypt: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("yh_av91 parse: %v", err)
	}
	return out, nil
}

var yhAv91HostRe = regexp.MustCompile(`hf-[^.]+\.`)

func yhAv91Paid(rec map[string]interface{}) bool {
	if p, ok := rec["is_pay"].(float64); ok && p == 1 {
		return true
	}
	if t := g5StrVal(rec["toll_type"]); t == "3" || t == "4" {
		return true
	}
	if c, ok := rec["coins"].(float64); ok && c > 0 {
		if f, ok := rec["isfree"].(float64); ok && f == 2 {
			return true
		}
	}
	return false
}

// yhAv91UnwrapCover: 本站 cover 字段常为 JSON 串
// {"ori": "...", "360": "...", "720": "..."} (cover_full/cover_thumb),
// 须解出 ori 直链; 明文 URL 原样返回.
func yhAv91UnwrapCover(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return s
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return s
	}
	for _, k := range []string{"ori", "720", "360"} {
		if u := g5StrVal(m[k]); u != "" {
			return u
		}
	}
	return s
}

func yhAv91Card(slot string, rec map[string]interface{}, kind string) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["title"])
	if title == "" {
		title = g5StrVal(rec["name"])
	}
	if id == "" || title == "" {
		return nil, false
	}
	if kind == "video" && yhAv91Paid(rec) {
		return nil, false
	}
	cover := yhAv91UnwrapCover(g5StrVal(rec["cover_horizontal"]))
	if cover == "" {
		cover = yhAv91UnwrapCover(g5StrVal(rec["cover_vertical"]))
	}
	if cover == "" {
		cover = yhAv91UnwrapCover(g5StrVal(rec["cover_full"]))
	}
	if cover == "" {
		cover = yhAv91UnwrapCover(g5StrVal(rec["cover"]))
	}
	remark := ""
	if d, ok := rec["duration"].(float64); ok && d > 0 {
		remark = fmt.Sprintf("%d:%02d", int(d)/60, int(d)%60)
	}
	desc := g5StrVal(rec["tags"])
	if desc == "" {
		desc = strings.Join(g5StrList(rec["tag_list"]), " ")
	}
	return makeDrama(slot, kind+":"+id, title, cover, remark, "", desc), true
}

func yhAv91Cards(slot string, recs []interface{}, kind string) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhAv91Card(slot, m, kind); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// yhAv91Flatten implements ElementVideo.getAllVideo's four routes.
func yhAv91Flatten(slot string, obj map[string]interface{}) []interface{} {
	data := yhMap(obj["data"])
	if data == nil {
		return yhAv91Cards(slot, yhArr(obj["data"]), "video")
	}
	if arr := yhArr(data["data"]); len(arr) > 0 {
		return yhAv91Cards(slot, arr, "video")
	}
	if arr := yhArr(data["mv_recomment_official"]); len(arr) > 0 {
		return yhAv91Cards(slot, arr, "video")
	}
	if mvs := yhMap(data["mvs"]); mvs != nil {
		if arr := yhArr(mvs["data"]); len(arr) > 0 {
			return yhAv91Cards(slot, arr, "video")
		}
	}
	out := []interface{}{}
	for _, e := range yhArr(data["elements"]) {
		if em := yhMap(e); em != nil {
			if t, ok := em["type"].(float64); ok && t == 4 {
				out = append(out, yhAv91Cards(slot, yhArr(em["value"]), "video")...)
			}
		}
	}
	return out
}

func yhAv91Categories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range []struct{ id, name string }{
		{"screen:::1", "筛选·免费"},
		{"daily", "每日播单"},
		{"construct:3", "最新创作"}, {"construct:4", "热门影片"},
		{"construct:7", "暗网猎奇"}, {"construct:2", "华语国产"},
		{"construct:12", "禁漫天堂"},
		{"construct:9", "日本AV"},
		{"theme", "主题"}, {"recommend", "推荐"},
		{"actor:renqi:", "女优·人气"}, {"actor:av_count:", "女优·片量"},
		{"screen:most_play::", "筛选·观看最多"},
	} {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func yhAv91Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	pg := strconv.Itoa(page)
	switch {
	case category == "daily":
		obj, err := yhAv91Call(map[string]string{
			"mod": "mv", "code": "playlist", "id": "1", "page": pg,
		})
		if err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		out := []interface{}{}
		for _, g := range yhArr(data["data"]) {
			if gm := yhMap(g); gm != nil {
				out = append(out, yhAv91Cards(slot, yhArr(gm["mv"]), "video")...)
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "construct:"):
		obj, err := yhAv91Call(map[string]string{
			"mod": "element", "code": "getConstructById",
			"id": strings.TrimPrefix(category, "construct:"), "page": pg,
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhAv91Flatten(slot, obj)
		return items, len(items), len(items) > 0, nil
	case category == "theme":
		obj, err := yhAv91Call(map[string]string{
			"mod": "mv", "code": "mvTopicList",
		})
		if err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		out := []interface{}{}
		for _, e := range yhArr(data["data"]) {
			if m := yhMap(e); m != nil {
				title := g5StrVal(m["title"])
				cid := g5StrVal(m["construct_id"])
				if title == "" || cid == "" {
					continue
				}
				// 主题文件夹卡: rawID 编码 more_api_params 式业务
				out = append(out, makeDrama(slot, "theme:"+cid, title, "",
					"主题 "+g5StrVal(m["mv_count"])+" 部", "", title))
			}
		}
		return out, len(out), false, nil
	case category == "recommend":
		obj, err := yhAv91Call(map[string]string{
			"mod": "mv", "code": "recommendAll",
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhAv91Flatten(slot, obj)
		return items, len(items), false, nil
	case strings.HasPrefix(category, "actor:"):
		parts := strings.SplitN(strings.TrimPrefix(category, "actor:"), ":", 2)
		sort, cup := "renqi", ""
		if len(parts) >= 1 && parts[0] != "" {
			sort = parts[0]
		}
		if len(parts) == 2 {
			cup = parts[1]
		}
		biz := map[string]string{
			"mod": "Mvactor", "code": "actorList", "sort": sort,
			"cup_size": cup, "page": pg, "limit": "32",
		}
		obj, err := yhAv91Call(biz)
		if err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		out := []interface{}{}
		for _, e := range yhArr(data["actors"]) {
			if m := yhMap(e); m != nil {
				id := g5StrVal(m["id"])
				name := g5StrVal(m["name"])
				if id == "" || name == "" {
					continue
				}
				remark := "片量 " + g5StrVal(m["av_count"])
				if cup := g5StrVal(m["cup"]); cup != "" {
					remark += " " + cup + "杯"
				}
				out = append(out, makeDrama(slot, "actor:"+id, name,
					g5StrVal(m["cover"]), remark, "", g5StrVal(m["brief"])))
			}
		}
		return out, len(out), len(out) >= 32, nil
	case strings.HasPrefix(category, "screen:"):
		parts := strings.SplitN(strings.TrimPrefix(category, "screen:"), ":", 3)
		sort, cate, toll := "", "", ""
		if len(parts) >= 1 {
			sort = parts[0]
		}
		if len(parts) >= 2 {
			cate = parts[1]
		}
		if len(parts) == 3 {
			toll = parts[2]
		}
		biz := map[string]string{
			"mod": "mv", "code": "search", "sort": sort, "cate": cate,
			"toll_type": toll, "page": pg, "limit": "16",
		}
		obj, err := yhAv91Call(biz)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhAv91Flatten(slot, obj)
		return items, len(items), len(items) >= 16, nil
	default:
		obj, err := yhAv91Call(map[string]string{
			"mod": "element", "code": "getConstructById", "id": "3", "page": pg,
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhAv91Flatten(slot, obj)
		return items, len(items), len(items) > 0, nil
	}
}

func yhAv91Detail(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhAv91Slot+":")
	kind, id := "video", vid
	if j := strings.Index(vid, ":"); j >= 0 {
		kind, id = vid[:j], vid[j+1:]
	}
	switch kind {
	case "theme":
		// 主题文件夹: 以 construct 业务取视频作选集
		obj, err := yhAv91Call(map[string]string{
			"mod": "element", "code": "getConstructById", "id": id, "page": "1",
		})
		if err != nil {
			return nil, err
		}
		items := yhAv91Flatten(slot, obj)
		d := makeDrama(slot, vid, "主题", "", "", "", "")
		epIDs, names := []string{}, []string{}
		for _, it := range items {
			if m, ok := it.(map[string]interface{}); ok {
				if vodID := g5StrVal(m["vod_id"]); vodID != "" {
					epIDs = append(epIDs, vodID)
					names = append(names, g5StrVal(m["title"]))
					if len(epIDs) == 1 {
						d["title"] = "主题 · " + g5StrVal(m["title"])
					}
				}
			}
		}
		if len(epIDs) == 0 {
			return nil, fmt.Errorf("yh_av91: 主题 %s 无视频", id)
		}
		g5SetChapters(d, slot, vid, epIDs, names)
		return d, nil
	case "actor":
		obj, err := yhAv91Call(map[string]string{
			"mod": "mvactor", "code": "actorDetail", "id": id, "sort": "praise",
		})
		if err != nil {
			return nil, err
		}
		items := yhAv91Flatten(slot, obj)
		d := makeDrama(slot, vid, "女优作品", "", "", "", "")
		epIDs, names := []string{}, []string{}
		for _, it := range items {
			if m, ok := it.(map[string]interface{}); ok {
				if vodID := g5StrVal(m["vod_id"]); vodID != "" {
					epIDs = append(epIDs, vodID)
					names = append(names, g5StrVal(m["title"]))
				}
			}
		}
		if len(epIDs) == 0 {
			return nil, fmt.Errorf("yh_av91: 女优 %s 无作品", id)
		}
		g5SetChapters(d, slot, vid, epIDs, names)
		return d, nil
	}
	obj, err := yhAv91Call(map[string]string{
		"mod": "mv", "code": "detail", "id": id,
	})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	title, cover, desc := vid, "", ""
	var det map[string]interface{}
	switch kind {
	default:
		// 详情响应实测形态为 {data:{mv:{...}, banner:[...]}},
		// 无 detail 键, 标题/封面在 mv 内 (PlayInfo.DataDTO);
		// 另一变体才有 detail 键, 故 mv 优先、detail 次之.
		det = yhMap(data["mv"])
		if det == nil {
			det = yhMap(data["detail"])
		}
		if det == nil {
			det = data
		}
		if det != nil {
			title = g5StrVal(det["title"])
			cover = yhAv91UnwrapCover(g5StrVal(det["cover_horizontal"]))
			if cover == "" {
				cover = yhAv91UnwrapCover(g5StrVal(det["cover_vertical"]))
			}
			if cover == "" {
				cover = yhAv91UnwrapCover(g5StrVal(det["cover_full"]))
			}
			desc = g5StrVal(det["desc"])
			if a := g5StrVal(det["actors"]); a != "" {
				desc = strings.TrimSpace(a + " " + desc)
			}
		}
	}
	if len(desc) > 800 {
		desc = desc[:800]
	}
	d := makeDrama(slot, vid, title, cover, "", "", desc)
	if det == nil || !yhAv91Paid(det) {
		g5SetChapters(d, slot, vid, []string{vid}, []string{"正片"})
	}
	return d, nil
}

func yhAv91PlayURL(obj map[string]interface{}) string {
	data := yhMap(obj["data"])
	det := yhMap(data["detail"])
	if det == nil {
		det = data
	}
	if det == nil {
		return ""
	}
	if u := g5StrVal(det["m3u8"]); u != "" {
		return u
	}
	mv := yhMap(det["mv"])
	if mv != nil {
		if u := g5StrVal(mv["m3u8"]); u != "" {
			return u
		}
	}
	if u := g5StrVal(det["preview_url"]); u != "" {
		return yhAv91HostRe.ReplaceAllString(u, "hf-long.")
	}
	if ps := yhMap(det["preview_sources"]); ps != nil {
		if u := g5StrVal(ps["url_264"]); u != "" {
			return u
		}
		if u := g5StrVal(ps["url_265"]); u != "" {
			return u
		}
	}
	for _, k := range []string{"source_720", "source_480", "source_240"} {
		if u := g5StrVal(det[k]); u != "" {
			return u
		}
	}
	return ""
}

func yhAv91Resolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhAv91Slot+":")
	kind, id := "video", vid
	if j := strings.Index(vid, ":"); j >= 0 {
		kind, id = vid[:j], vid[j+1:]
	}
	var u string
	switch kind {
	case "theme", "actor":
		// 文件夹/女优选集的播放地址由章节 id (即 video:<vid>) 决定,
		// 这里 rawID 仍为文件夹时无法直接播, 返回明确错误
		return nil, fmt.Errorf("yh_av91: folder %s is not playable, pick an episode", vid)
	default:
		obj, err := yhAv91Call(map[string]string{
			"mod": "mv", "code": "detail", "id": id,
		})
		if err != nil {
			return nil, err
		}
		data := yhMap(obj["data"])
		det := yhMap(data["detail"])
		if det != nil && yhAv91Paid(det) {
			return nil, fmt.Errorf("yh_av91: paid content is not resolved")
		}
		u = yhAv91PlayURL(obj)
	}
	if u == "" {
		return nil, fmt.Errorf("yh_av91: no play url for %s", vid)
	}
	format := "m3u8"
	if strings.HasSuffix(strings.Split(u, "?")[0], ".mp4") {
		format = "mp4"
	}
	return playResult(u, slot, rawID, yhAv91UA, yhAv91Base+"/", format, "hls"), nil
}

func yhAv91Search(slot, kw string) ([]interface{}, error) {
	// type 经实测纠正: jadx 解码为 "0" 的字段服务端拒收
	// ("type值只能为:actor,news,topic,mv"), 视频搜索实测用 "mv" 通.
	obj, err := yhAv91Call(map[string]string{
		"mod": "seek", "code": "seekInAll", "keywords": kw,
		"type": "mv", "last_ix": "24", "page": "1",
	})
	if err != nil {
		return nil, err
	}
	return yhAv91Flatten(slot, obj), nil
}

var yhAv91ImgKey = []byte("f5d965df75336270")
var yhAv91ImgIV = []byte("97b60394abc2fbe1")

func yhAv91ImgMagic(b []byte) bool {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return true
	case len(b) >= 4 && b[0] == 0x89 && b[1] == 0x50:
		return true
	case len(b) >= 4 && string(b[:4]) == "GIF8":
		return true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return true
	case len(b) >= 2 && string(b[:2]) == "BM":
		return true
	}
	return false
}

// yhAv91CoverFetch: family-shared cover decryption. Cover bytes on
// this family's pic hosts are AES-128-CBC encrypted at rest with the
// same key/IV the app's shared Glide fetcher uses (verified 2026-10-24:
// melon51 pic.sugfew.cn -> JPEG FF D8 FF E0, utube picx.drleao.cn ->
// PNG 89 50 after decrypt; plaintext covers pass through untouched,
// undecodable bodies are returned raw, as the app does).
func yhAv91CoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpDo("GET", u, nil, yhAv91Headers())
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("yh_av91 cover http %d", code)
	}
	if yhAv91ImgMagic(body) || len(body)%aes.BlockSize != 0 {
		return body, nil
	}
	block, err := aes.NewCipher(yhAv91ImgKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, yhAv91ImgIV).CryptBlocks(out, body)
	if pad := int(out[len(out)-1]); pad > 0 && pad <= aes.BlockSize && pad <= len(out) {
		ok := true
		for _, b := range out[len(out)-pad:] {
			if int(b) != pad {
				ok = false
				break
			}
		}
		if ok {
			out = out[:len(out)-pad]
		}
	}
	if yhAv91ImgMagic(out) {
		return out, nil
	}
	return body, nil
}

func init() {
	registerSite(yhAv91Slot, &siteImpl{
		Key:        yhAv91Slot,
		Name:       "av91",
		DefaultCat: "screen:::1",
		HomeURL:    yhAv91Base + "/",
		Headers:    yhAv91Headers,
		CoverFetch: yhAv91CoverFetch,
		Categories: yhAv91Categories,
		Catalog:    yhAv91Catalog,
		Detail:     yhAv91Detail,
		Resolve:    yhAv91Resolve,
		Search:     yhAv91Search,
	})
}
