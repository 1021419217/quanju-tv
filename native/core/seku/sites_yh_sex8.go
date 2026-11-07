package seku

// ---- sex8 / 夜杏 / 杏吧视频 (yh_sex8) — Discuz 论坛壳 + 本地服务，
// 本单只做视频 + 有声子集；楼凤维度明确剔除（见下） ----
//
// 口径来源：本轮用户令「70 模块全量进 Go，例外只许三类
// （服务端已死/共享账号/工具重复空壳）」；sex8 有真实可播的
// 视频与有声内容，不在例外内，故单摘媒体子集。
// 剔除范围（不实现、不入分类、不出卡片）：
//   · 楼凤列表/详情（city/hostBean/rankhost_list 的服务维度、
//     LouFengList/LouFengDetail/LouFengList2/LouFengDetail2、
//     /v1/info/second_menu/data、/v1/post/data、/v1/post/version、
//     LouFengActivity 的城市分区：北京/上海/广州/深圳/川渝/东北/
//     山东/津·冀/陕·豫/苏·皖/杭·宁/晋·云/华东/华南/华中/其他地区）
//   · 楼凤详情里的价格/电话/QQ/微信/服务项目/地址等线下服务信息
//     （LouFengDetail 字段 price/phone/qq/wechat/item/area_name）。
//   · rankhost_list 的主播榜只取其「主播→有声作品」下钻
//     （radio_by_hostid），不取任何服务信息。
//   · 视频/有声里的付费内容：只进免费（见付费过滤实证）。
//
// 配方（jadx 逐行复刻，f5 包）：
//   base https://kongsjieie.bar/（h6.b f8256l），Retrofit f5.a，
//   拦截器 f5.c：请求头 APPIMAGE=100、VERSIONNAME=1.0.0；
//   响应若不以 "{" 开头则 Base64 解码后 AES/CBC/NoPadding 解密，
//   key = native getSex82，IV = native getSex81（f5.d 持有，
//   d.a = getSex81（IV）、d.b = getSex82（key）——注意 f5.d 的
//   a/b 序与 getSex81/getSex82 的对应，加密时 key=d.b、IV=d.a）。
//   请求加密（YXParams.toJson，z.b Gson 序列化后）：
//   JSON 补 0x00 至 16 倍数（已整倍不补），AES/CBC/NoPadding
//   加密，Base64（NO_WRAP）编码，作表单 field "request"。
//   响应解密同上，解密后 trim（含 \x00）。
//   视频：GET /v1/video/index/free/data（无参数，App 忽略翻页，
//   Sex8VideoListActivity.o() 恒调 a()），
//   响应 {status, data{models[], version}}；models 字段：
//   tid/subject/views/author/avatar/images[]/video_url/is_free/
//   type_id/fid；点击直播 PlayActivity.n(video_url, subject)。
//   有声：POST http://radioluntan.space/apiv286.php?0=<毫秒>
//   表单 request=<加密 YXParams>，YXParams 字段：
//   album_id/category/child_category/device_type/hostid/is_best/
//   ispay/op/page/page_size/radio_by_hostid/radio_id/type/v。
//   各腿 v 值（YXActivity 五 tab + 榜单，jadx 解码实证）：
//     radio_album_list + type=new + category=
//       erotic_long（长篇）/ erotic_short（短篇）/
//       passion_mic（骚麦）/ other（其他），page_size=10；
//     rank_list + type=recommand（推荐）/ hot（热门）/ new（最新）；
//     rankhost_list（主播榜，→ YXHostBean）；
//     radio_by_hostid + hostid（主播作品）；
//     radio_album_info + album_id + device_type=1（专辑详情，
//       data.radio_list[] 为选集，字段 radio_id/name/price/
//       single_price/is_free/is_share/duration/url/img）；
//     album_radio + album_id + radio_id + op=0 + device_type=1
//       （单集播放，data.radio_list[i].url 优先，缺省 data.url；
//       data.url 为 radioluntan.space/apiv286.m3u8?request=… 代理链）。
//   有声分片形态（f5.f YXPlayerInterceptor 实证）：
//   播放清单为 m3u8，段名 .png（如 audio000.png），实为 TS 流；
//   .ts 请求在 App 内被改写为 .png 请求——即段 URL 按清单字面
//   取即可，播放器不需改写。
//
// 会话依赖结论（无会话实测通过，不带任何 cookie）：
//   sex8 的 f5 客户端（f5.b）拦截器只有 f5.c（两头）+ w4.b
//   （缓存）+ e0.a（日志），全程不附加任何 Cookie；
//   h6.b Q（Discuz 会话 cookie，脱敏项，原值未写入本文件）
//   在 jadx 全量检索中只被 xhl/xiuxiu 的 InfoActivity 页面抓取
//   使用，sex8 模块内零引用。故本单无会话依赖，
//   以下实测均在无 Cookie 条件下完成。
//
// 实测证据（2026-10-24 本机，curl + Go 冒烟前置探活；
//   kongsjieie.bar 与 radioluntan.space 均经 HTTP-DNS 实 IP
//   与沙箱代理双路确认，非本机 DNS fake-IP 假象）：
//   · 视频 GET free/data：200，明文 JSON（以 "{" 开头，走
//     f5.c 明文分支），260 条 models，全 is_free=true，
//     首条 tid=21112660，video_url 为 media-server.t7ggv.com
//     的 master.m3u8；master→variant（hROz0.m3u8，480x854）→
//     KEY（16B，/Resource/File/…）→ 首段 AES-128-CBC 解密后
//     首字节 0x47（TS 同步字节），384,656B 段——真播验字节通过。
//   · 视频封面：media-server.t7ggv.com/Resource/Image/…origin.new
//     ?type=webp 在沙箱出口与浏览器出口双路均回 406
//     （换 UA/Accept/Referer/直连 IP 均 406；同主机 m3u8/KEY
//     正常 200，属该图片路径自身拒访，非加密混淆）。
//     App 侧 Glide（h6.e.N → g5.b）即原样加载该 URL，不加头、
//     不解密，故本文件不设 CoverFetch、原样透传 URL，
//     封面魔数腿标记「未验（406）」，不冒充已验。
//   · 有声 radio_album_list 长篇 p1/p2：status=1 各 10 条；
//     短篇/骚麦/其他：各 10 条；rank_list 推荐：10 条；
//     rankhost_list：10 主播；radio_by_hostid（host 9）：10 专辑；
//     radio_album_info（专辑 1059《岁月的爱痕》）：5 集；
//     album_radio（免费集 15836）：status=1，is_free=1，price=0，
//     直链 m3u8 拉取成功（312 行，153 段 audio*.png），
//     首段 73,696B、首字节 0x47——真播验字节通过。
//   · 付费过滤实证（专辑 1059）：第 1 集 price=0/is_free=1；
//     第 2–5 集 price=2.00000/is_free=0；album_radio 对付费集
//     仍返回 url（故不能只看有无 url），本文件以
//     is_free=="1" 且 price==0 为准：详情选集只列免费集，
//     Resolve 对付费集直接拒解析（见 yhSex8Paid）。
//   · 有声封面：photos.q4rtz.com 的 jpg 实测魔数 FF D8 FF E0
//     （JPEG）；个别专辑内页 img 有 404（如 1059 的集图），
//     故卡片统一用列表层 cover_img/album_img，不用集图。
//   · 搜索：f5.a 无 search 方法、YXParams 无关键词字段，
//     App 内 sex8/YX 无搜索入口，故 Search 返回明示错误，
//     不编造搜索结果。
//   · 分类与 App 导航 1:1：视频在 App 内是无分类单列
//     （Sex8VideoListActivity 无分类参数），故视频只一类；
//     有声按 YXActivity 五 tab + 排行三档 + 主播榜展开。
//   · 未验：视频封面魔数（上 406）、radioluntan 代理 m3u8
//     链（data.url，本机回 526；App 实际走 radio_list 直链，
//     故 Resolve 优先直链，代理链仅作回退且不验字节）。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto/aes"
	"crypto/cipher"
)

const yhSex8Base = "https://kongsjieie.bar/"
const yhSex8RadioURL = "http://radioluntan.space/apiv286.php"
const yhSex8UA = "okhttp/4.9.0"

var yhSex8Key = []byte("Af234dfdf0io@#$*")
var yhSex8IV = []byte("1234567890123456")

func yhSex8Headers() map[string]string {
	return map[string]string{
		"User-Agent":      yhSex8UA,
		"APPIMAGE":        "100",
		"VERSIONNAME":     "1.0.0",
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9",
	}
}

// yhSex8Enc 复刻 YXParams.toJson 的加密段：0x00 补齐至 16 倍数
// （已整倍不补），AES/CBC/NoPadding，Base64 NO_WRAP 编码。
func yhSex8Enc(plain string) string {
	b := []byte(plain)
	for len(b)%16 != 0 {
		b = append(b, 0)
	}
	block, err := aes.NewCipher(yhSex8Key)
	if err != nil {
		return ""
	}
	out := make([]byte, len(b))
	cipher.NewCBCEncrypter(block, yhSex8IV).CryptBlocks(out, b)
	return base64.StdEncoding.EncodeToString(out)
}

// yhSex8Dec 复刻 f5.c 的响应解密：以 "{" 开头即明文，
// 否则 Base64 解码 + AES/CBC/NoPadding 解密 + trim（含 \x00）。
func yhSex8Dec(body []byte) string {
	s := strings.TrimSpace(string(body))
	if strings.HasPrefix(s, "{") {
		return s
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw)%16 != 0 {
		return s
	}
	block, err := aes.NewCipher(yhSex8Key)
	if err != nil {
		return s
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhSex8IV).CryptBlocks(out, raw)
	return strings.Trim(string(out), " \t\r\n\x00")
}

func yhSex8Radio(params map[string]interface{}) (map[string]interface{}, error) {
	jb, _ := json.Marshal(params)
	form := url.Values{"request": {yhSex8Enc(string(jb))}}.Encode()
	h := yhSex8Headers()
	h["Content-Type"] = "application/x-www-form-urlencoded"
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(300 * time.Millisecond)
		}
		raw, code, _, err := httpDo("POST", fmt.Sprintf("%s?0=%d", yhSex8RadioURL, time.Now().UnixMilli()), strings.NewReader(form), h)
		if err != nil {
			lastErr = err
			continue
		}
		if code != 200 {
			lastErr = fmt.Errorf("yh_sex8 radio http %d", code)
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(yhSex8Dec(raw)), &obj); err != nil {
			lastErr = fmt.Errorf("yh_sex8 radio parse: %v", err)
			continue
		}
		if st := g5StrVal(obj["status"]); st != "" && st != "1" {
			// 业务错误不重试，直接返回。
			return nil, fmt.Errorf("yh_sex8 radio status %s: %s", st, g5StrVal(obj["msg"]))
		}
		return obj, nil
	}
	return nil, lastErr
}

func yhSex8VideoList() ([]map[string]interface{}, error) {
	// 主域偶发 502/短时抖动（API 路径通常正常）。重试 2 次，避免用户
	// 一点进站源就看到「暂时无法加载」。
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(300 * time.Millisecond)
		}
		b, code, _, err := httpGet(yhSex8Base+"v1/video/index/free/data", yhSex8Headers())
		if err != nil {
			lastErr = err
			continue
		}
		if code != 200 {
			lastErr = fmt.Errorf("yh_sex8 video http %d", code)
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(yhSex8Dec(b)), &obj); err != nil {
			lastErr = fmt.Errorf("yh_sex8 video parse: %v", err)
			continue
		}
		data := yhMap(obj["data"])
		if data == nil {
			lastErr = fmt.Errorf("yh_sex8 video empty data")
			continue
		}
		out := []map[string]interface{}{}
		for _, e := range yhArr(data["models"]) {
			if m := yhMap(e); m != nil {
				out = append(out, m)
			}
		}
		return out, nil
	}
	return nil, lastErr
}

var yhSex8VideoCache sync.Map // tid -> model

func yhSex8VideoModels() ([]map[string]interface{}, error) {
	models, err := yhSex8VideoList()
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		yhSex8VideoCache.Store(g5StrVal(m["tid"]), m)
	}
	return models, nil
}

func yhSex8VideoFind(tid string) (map[string]interface{}, error) {
	if v, ok := yhSex8VideoCache.Load(tid); ok {
		return v.(map[string]interface{}), nil
	}
	if _, err := yhSex8VideoModels(); err != nil {
		return nil, err
	}
	if v, ok := yhSex8VideoCache.Load(tid); ok {
		return v.(map[string]interface{}), nil
	}
	return nil, fmt.Errorf("yh_sex8: video tid %s not in free list", tid)
}

func yhSex8AlbumCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["radio_album_id"])
	title := g5StrVal(rec["name"])
	if id == "" || title == "" {
		return nil, false
	}
	cover := g5StrVal(rec["cover_img"])
	if cover == "" {
		cover = g5StrVal(rec["album_img"])
	}
	host := g5StrVal(rec["host_name"])
	if host == "" {
		host = g5StrVal(rec["host"])
	}
	remark := ""
	if rc := g5StrVal(rec["radio_count"]); rc != "" {
		remark = rc + "集"
	}
	if host != "" {
		if remark != "" {
			remark += " · "
		}
		remark += host
	}
	return makeDrama(slot, "a:"+id, title, cover, remark, "有声", g5StrVal(rec["desc"])), true
}

func yhSex8AlbumCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range recs {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		if c, ok := yhSex8AlbumCard(slot, rec); ok {
			id := g5StrVal(rec["radio_album_id"])
			if !seen[id] {
				seen[id] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// yhSex8AlbumList 拉 radio_album_list / rank_list / radio_by_hostid
// 三类列表（响应 data 均为数组），page_size=10（App 写死）。
func yhSex8AlbumList(slot string, params map[string]interface{}, page int) ([]interface{}, int, bool, error) {
	params["page"] = page
	params["page_size"] = "10"
	obj, err := yhSex8Radio(params)
	if err != nil {
		return nil, 0, false, err
	}
	items := yhSex8AlbumCards(slot, yhArr(obj["data"]))
	return items, 0, len(items) >= 10, nil
}

func yhSex8Categories(slot string) []interface{} {
	return []interface{}{
		categoryObj("video", "视频"),
		categoryObj("audio:rank:recommand", "排行·推荐"),
		categoryObj("audio:rank:hot", "排行·热门"),
		categoryObj("audio:rank:new", "排行·最新"),
		categoryObj("audio:rank:host", "排行·主播"),
		categoryObj("audio:long", "有声·长篇"),
		categoryObj("audio:short", "有声·短篇"),
		categoryObj("audio:mic", "有声·骚麦"),
		categoryObj("audio:other", "有声·其他"),
	}
}

func yhSex8Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "" || category == "video":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		models, err := yhSex8VideoModels()
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		seen := map[string]bool{}
		for _, m := range models {
			tid := g5StrVal(m["tid"])
			title := g5StrVal(m["subject"])
			if tid == "" || title == "" || seen[tid] {
				continue
			}
			seen[tid] = true
			cover := ""
			if imgs := yhArr(m["images"]); len(imgs) > 0 {
				cover = g5StrVal(imgs[0])
			}
			remark := g5StrVal(m["author"])
			if v := g5StrVal(m["views"]); v != "" {
				if remark != "" {
					remark += " · "
				}
				remark += v + "播放"
			}
			out = append(out, makeDrama(slot, "v:"+tid, title, cover, remark, "视频", ""))
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "dir:host:"):
		return yhSex8AlbumList(slot, map[string]interface{}{"v": "radio_by_hostid", "hostid": strings.TrimPrefix(category, "dir:host:")}, page)
	case category == "audio:rank:host":
		obj, err := yhSex8Radio(map[string]interface{}{"v": "rankhost_list", "page": page, "page_size": "10"})
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		seen := map[string]bool{}
		for _, e := range yhArr(obj["data"]) {
			h := yhMap(e)
			if h == nil {
				continue
			}
			hid := g5StrVal(h["host_id"])
			name := g5StrVal(h["name"])
			if hid == "" || name == "" || seen[hid] {
				continue
			}
			seen[hid] = true
			out = append(out, makeDrama(slot, "dir:host:"+hid, name, g5StrVal(h["img"]), g5StrVal(h["hits_count"])+"人气", "主播", g5StrVal(h["desc"])))
		}
		return out, 0, len(out) >= 10, nil
	case strings.HasPrefix(category, "audio:rank:"):
		return yhSex8AlbumList(slot, map[string]interface{}{"v": "rank_list", "type": strings.TrimPrefix(category, "audio:rank:")}, page)
	case category == "audio:long":
		return yhSex8AlbumList(slot, map[string]interface{}{"v": "radio_album_list", "type": "new", "category": "erotic_long", "child_category": ""}, page)
	case category == "audio:short":
		return yhSex8AlbumList(slot, map[string]interface{}{"v": "radio_album_list", "type": "new", "category": "erotic_short", "child_category": ""}, page)
	case category == "audio:mic":
		return yhSex8AlbumList(slot, map[string]interface{}{"v": "radio_album_list", "type": "new", "category": "passion_mic", "child_category": ""}, page)
	case category == "audio:other":
		return yhSex8AlbumList(slot, map[string]interface{}{"v": "radio_album_list", "type": "new", "category": "other", "child_category": ""}, page)
	}
	return yhSex8Catalog(slot, "video", page)
}

func yhSex8AlbumInfo(albumID string) (map[string]interface{}, error) {
	obj, err := yhSex8Radio(map[string]interface{}{"v": "radio_album_info", "album_id": albumID, "device_type": "1"})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_sex8: album %s empty", albumID)
	}
	return data, nil
}

func yhSex8Episodes(data map[string]interface{}) (epIDs, epNames []string, eps []map[string]interface{}) {
	for _, e := range yhArr(data["radio_list"]) {
		ep := yhMap(e)
		if ep == nil {
			continue
		}
		rid := g5StrVal(ep["radio_id"])
		name := g5StrVal(ep["name"])
		if rid == "" {
			continue
		}
		if name == "" {
			name = "第" + strconv.Itoa(len(epIDs)+1) + "集"
		}
		epIDs = append(epIDs, "ep:"+g5StrVal(data["radio_album_id"])+":"+rid)
		epNames = append(epNames, name)
		eps = append(eps, ep)
	}
	return epIDs, epNames, eps
}

func yhSex8Detail(slot, rawID string) (map[string]interface{}, error) {
	switch {
	case strings.HasPrefix(rawID, "v:"):
		m, err := yhSex8VideoFind(strings.TrimPrefix(rawID, "v:"))
		if err != nil {
			return nil, err
		}
		cover := ""
		if imgs := yhArr(m["images"]); len(imgs) > 0 {
			cover = g5StrVal(imgs[0])
		}
		d := makeDrama(slot, rawID, g5StrVal(m["subject"]), cover, g5StrVal(m["author"]), "视频", "")
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
		return d, nil
	case strings.HasPrefix(rawID, "dir:host:"):
		hid := strings.TrimPrefix(rawID, "dir:host:")
		items, _, _, err := yhSex8AlbumList(slot, map[string]interface{}{"v": "radio_by_hostid", "hostid": hid}, 1)
		if err != nil {
			return nil, err
		}
		d := makeDrama(slot, rawID, "主播作品", "", "有声", "主播", "")
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
	case strings.HasPrefix(rawID, "a:"):
		albumID := strings.TrimPrefix(rawID, "a:")
		data, err := yhSex8AlbumInfo(albumID)
		if err != nil {
			return nil, err
		}
		cover := g5StrVal(data["cover_img"])
		if cover == "" {
			cover = g5StrVal(data["album_img"])
		}
		host := ""
		if hm := yhMap(data["host"]); hm != nil {
			host = g5StrVal(hm["name"])
		}
		if host == "" {
			host = g5StrVal(data["host_name"])
		}
		desc := g5StrVal(data["desc"])
		if host != "" {
			desc = "主播：" + host + "\n" + desc
		}
		d := makeDrama(slot, rawID, g5StrVal(data["name"]), cover, g5StrVal(data["radio_count"])+"集", "有声", desc)
		epIDs, epNames, _ := yhSex8Episodes(data)
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, epNames)
		} else {
			d["desc"] = desc + "\n（本专辑暂无选集）"
			d["description"] = d["desc"]
		}
		if like := yhArr(data["likeAlbumList"]); len(like) > 0 {
			d["related"] = yhSex8AlbumCards(slot, like)
		}
		return d, nil
	}
	return nil, fmt.Errorf("yh_sex8: unknown id %s", rawID)
}

func yhSex8Resolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	switch {
	case strings.HasPrefix(vid, "v:"):
		m, err := yhSex8VideoFind(strings.TrimPrefix(vid, "v:"))
		if err != nil {
			return nil, err
		}
		u := g5StrVal(m["video_url"])
		if u == "" {
			return nil, fmt.Errorf("yh_sex8: no video_url for %s", vid)
		}
		return playResult(u, slot, rawID, yhSex8UA, yhSex8Base, "m3u8", "hls"), nil
	case strings.HasPrefix(vid, "a:"):
		data, err := yhSex8AlbumInfo(strings.TrimPrefix(vid, "a:"))
		if err != nil {
			return nil, err
		}
		epIDs, _, eps := yhSex8Episodes(data)
		if len(epIDs) == 0 {
			return nil, fmt.Errorf("yh_sex8: album %s has no episode", vid)
		}
		return yhSex8ResolveEp(slot, rawID, g5StrVal(data["radio_album_id"]), g5StrVal(eps[0]["radio_id"]), g5StrVal(eps[0]["url"]))
	case strings.HasPrefix(vid, "ep:"):
		parts := strings.SplitN(strings.TrimPrefix(vid, "ep:"), ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("yh_sex8: bad episode id %s", vid)
		}
		return yhSex8ResolveEp(slot, rawID, parts[0], parts[1], "")
	}
	return nil, fmt.Errorf("yh_sex8: cannot resolve %s", vid)
}

// yhSex8ResolveEp：走 App 的 album_radio 腿；以该腿返回的
// radio_list 匹配集为准；播放地址优先 radio_list 直链，缺省回退 data.url 代理链。
func yhSex8ResolveEp(slot, rawID, albumID, radioID, infoURL string) (map[string]interface{}, error) {
	obj, err := yhSex8Radio(map[string]interface{}{"v": "album_radio", "album_id": albumID, "radio_id": radioID, "op": 0, "device_type": "1"})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_sex8: album_radio empty for %s/%s", albumID, radioID)
	}
	u := ""
	for _, e := range yhArr(data["radio_list"]) {
		ep := yhMap(e)
		if ep == nil {
			continue
		}
		if g5StrVal(ep["radio_id"]) != radioID {
			continue
		}
		u = g5StrVal(ep["url"])
		break
	}
	if u == "" {
		u = g5StrVal(data["url"])
	}
	if u == "" {
		u = infoURL
	}
	if u == "" {
		return nil, fmt.Errorf("yh_sex8: no play url for %s/%s", albumID, radioID)
	}
	format, protocol := "mp3", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhSex8UA, yhSex8Base, format, protocol), nil
}

func yhSex8Search(slot, kw string) ([]interface{}, error) {
	return nil, fmt.Errorf("yh_sex8: no search endpoint in app (f5.a has no search; YXParams has no keyword field)")
}

func init() {
	registerSite("yh_sex8", &siteImpl{
		Key:        "yh_sex8",
		Name:       "杏吧视频·夜杏有声",
		DefaultCat: "video",
		HomeURL:    yhSex8Base,
		Categories: yhSex8Categories,
		Catalog:    yhSex8Catalog,
		Detail:     yhSex8Detail,
		Resolve:    yhSex8Resolve,
		Search:     yhSex8Search,
		Headers:    yhSex8Headers,
	}, "yh_sex8", "sex8", "夜杏", "杏吧视频")
}
