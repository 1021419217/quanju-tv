package seku

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- short91 91 短视频 (yh_short91) — PWA 加密 API (CFB 族) ----
//
// 配方出欲火 APK jadx 实证 (ui/short91 + j5 ApiService/Short91Utils/
// Short91Interceptor + h6.b 默认基址):
//   base = https://ujvxsl.rsvurzldo.cc (h6.b.E0, 写死)
//   单端点 POST {base}/pwa.php, mod/code 分发 (j5.a 七法全同路径)
//   信封 (j5.d.a/b/c): 外层 form timestamp=<秒>, client=pwa, data, sign
//     data = 大写 hex( AES-256-CFB/NoPadding( JSON(biz), key, iv ) )
//     key = cc88ddc9357ff461e08f047aedee692b (32B)
//     iv  = e89225cfbbimgkcu
//     sign = MD5hex( SHA256hex( "client=pwa&data="+data+
//            "&timestamp="+ts+key ) ) (j5.d.d, 与撸先生同构)
//     响应 {"data":"<hex>"} 同 CFB 解密得业务 JSON (j5.c 拦截器)
//   biz 公共 (j5.d.b): version=7.4.5, oauth_type=pwa,
//   timestamp=<毫秒>, oauth_id=khXRGM8BOPEorpWH1784033586211,
//   token=1cd2810e5278a21d311113ab564eca2d.
//   业务 (d.c 助手: biz += mod, code, id, page, limit=12):
//     首页 a.e: {mod:index, code:<auth|home>, page}
//     发现/原创 a.b/a.g: {mod:element, code:<getConstructById|
//       getElementItembyId>, id, page, limit:12}
//     热点 a.c: {mod:hot, code:index, uuid:
//       e797299777626b984f4fa26fc8706b10}
//     搜索 a.a: {mod:index, code:search, type:1, key:<kw>, page}
//     标签 a.d: {mod:index, code:listByTag, page, tags, type:new}
//     元素详情 a.g: {mod:element, code:getElementItembyId, id, page,
//       type:new}; 热榜单 a.e: {mod:index, code:hotLists, page, type}.
// 分类与 App 导航 1:1:
//   首页 最新 (code auth) / 推荐 (code home);
//   发现 推荐 (construct 1) / 禁区 (element 429) / 最新 (element 33) /
//     乱伦 (construct 13) / 黑料 (element 7) / 自制 (construct 10);
//   原创 热点 (HotFragment) / 91大神 (construct 2).
// 播放: 短视频列表项自带 playUrl (VideoInfo.playUrl), App 内直接播,
//   无独立详情端点; 本实现以 Catalog 填充的进程内缓存供
//   Detail/Resolve 取 playUrl, 缓存未命中时走 getElementItembyId.
// 付费: VideoInfo 无 coins/price 字段 (仅 is_ads 广告项, 列表过滤);
//   无付费过滤项, 详情/播放不设账本墙.
// 封面: 本族封面字节 AES-128-CBC 加密, 族共享 Glide 密钥
//   (yhShort91CoverFetch; 密钥已在 melon51/utube 同族图床实测解密出
//   明文魔数, short91 自身图床待后端恢复后冒烟时验).
// 验真 (2026-10-24 本机): 主机存活; 当日早些时候 home/auth
//   两路实拉曾回 HTTP 500, 同日稍后 Go 冒烟时服务端已恢复:
//   home:auth 首屏 24 条、搜索 24 条、首卡封面经 CoverFetch 解密
//   后 PNG 魔数已验、Resolve 取到带 auth_key 的真 m3u8
//   (yd-long.hzjvlo.cn)。500 属服务端瞬时故障, 非本站配方错.
//   另: 发现·推荐腿实拉 90 条 (多 section 展平)、推荐 11 条;
//   原创·热点腿现行回活动 (大赛) 数据, 非 jadx HotBean 形态,
//   故出活动文件夹卡, 其视频穿透端点未追通、如实标注.

const yhShort91Slot = "yh_short91"
const yhShort91Base = "https://ujvxsl.rsvurzldo.cc"
const yhShort91UA = "Mozilla/5.0 (Linux; Android 13; SM-A205GN) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"

var yhShort91Key = []byte("cc88ddc9357ff461e08f047aedee692b")
var yhShort91IV = []byte("e89225cfbbimgkcu")

var (
	yhShort91CacheMu       sync.Mutex
	yhShort91Cache         = map[string]map[string]interface{}{}
	yhShort91ActivityCache = map[string]map[string]interface{}{}
)

func yhShort91Headers() map[string]string {
	return map[string]string{
		"User-Agent":   yhShort91UA,
		"Content-Type": "application/x-www-form-urlencoded",
		"Referer":      yhShort91Base + "/",
	}
}

func yhShort91Enc(plain []byte) string {
	block, _ := aes.NewCipher(yhShort91Key)
	out := make([]byte, len(plain))
	cipher.NewCFBEncrypter(block, yhShort91IV).XORKeyStream(out, plain)
	return strings.ToUpper(hex.EncodeToString(out))
}

func yhShort91Dec(hexs string) ([]byte, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(hexs))
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(yhShort91Key)
	out := make([]byte, len(raw))
	cipher.NewCFBDecrypter(block, yhShort91IV).XORKeyStream(out, raw)
	return out, nil
}

func yhShort91Sign(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", md5.Sum([]byte(hex.EncodeToString(sum[:]))))
}

func yhShort91Call(biz map[string]string) (map[string]interface{}, error) {
	b := map[string]string{
		"version":    "7.4.5",
		"oauth_type": "pwa",
		"timestamp":  strconv.FormatInt(time.Now().UnixMilli(), 10),
		"oauth_id":   "khXRGM8BOPEorpWH1784033586211",
		"token":      "1cd2810e5278a21d311113ab564eca2d",
	}
	for k, v := range biz {
		b[k] = v
	}
	jb, _ := json.Marshal(b)
	data := yhShort91Enc(jb)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := yhShort91Sign("client=pwa&data=" + data + "&timestamp=" + ts + string(yhShort91Key))
	form := url.Values{
		"timestamp": {ts},
		"client":    {"pwa"},
		"data":      {data},
		"sign":      {sign},
	}.Encode()
	var body []byte
	var code int
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err = httpDo("POST", yhShort91Base+"/pwa.php", strings.NewReader(form), yhShort91Headers())
		if err == nil && code == 200 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_short91 http %d", code)
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("yh_short91 outer parse: %v", err)
	}
	enc := g5StrVal(outer["data"])
	if enc == "" {
		return outer, nil
	}
	plain, err := yhShort91Dec(enc)
	if err != nil {
		return nil, fmt.Errorf("yh_short91 decrypt: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("yh_short91 parse: %v", err)
	}
	return out, nil
}

func yhShort91IsAd(rec map[string]interface{}) bool {
	if g5StrVal(rec["is_ads"]) == "1" {
		return true
	}
	if v, ok := rec["is_ads"].(float64); ok && v == 1 {
		return true
	}
	return false
}

func yhShort91Card(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["title"])
	if id == "" || title == "" || yhShort91IsAd(rec) {
		return nil, false
	}
	cover := g5StrVal(rec["thumb"])
	if cover == "" {
		cover = g5StrVal(rec["thumbImg"])
	}
	remark := ""
	if d := g5StrVal(rec["durationStr"]); d != "" {
		remark = d
	} else if d, ok := rec["duration"].(float64); ok && d > 0 {
		remark = fmt.Sprintf("%d:%02d", int(d)/60, int(d)%60)
	}
	desc := ""
	if tags := yhArr(rec["tags"]); len(tags) > 0 {
		parts := []string{}
		for _, t := range tags {
			if m := yhMap(t); m != nil {
				if s := g5StrVal(m["tags_name"]); s != "" {
					parts = append(parts, s)
				}
			} else if s := g5StrVal(t); s != "" {
				parts = append(parts, s)
			}
		}
		desc = strings.Join(parts, " ")
	}
	if desc == "" {
		desc = g5StrVal(rec["nickName"])
	}
	yhShort91CacheMu.Lock()
	yhShort91Cache[id] = rec
	yhShort91CacheMu.Unlock()
	return makeDrama(slot, id, title, cover, remark, "", desc), true
}

// yhShort91Flatten collects VideoInfo items from every bean shape this
// family returns: bare list, sections[].list, or object.items.
func yhShort91Flatten(slot string, data map[string]interface{}) []interface{} {
	out := []interface{}{}
	var recs []interface{}
	if data == nil {
		return out
	}
	if arr := yhArr(data); len(arr) > 0 {
		recs = arr
	} else {
		for _, e := range yhArr(data["list"]) {
			if m := yhMap(e); m != nil {
				if inner := yhArr(m["list"]); len(inner) > 0 {
					recs = append(recs, inner...)
					continue
				}
			}
			recs = append(recs, e)
		}
		recs = append(recs, yhArr(data["items"])...)
	}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhShort91Card(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func yhShort91Categories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range []struct{ id, name string }{
		{"home:auth", "最新"}, {"home:home", "推荐"},
		{"elem:1:getConstructById", "发现·推荐"},
		{"elem:429:getElementItembyId", "发现·禁区"},
		{"elem:33:getElementItembyId", "发现·最新"},
		{"elem:13:getConstructById", "发现·乱伦"},
		{"elem:7:getConstructById", "发现·黑料"},
		{"elem:10:getConstructById", "发现·自制"},
		{"hot", "原创·热点"},
		{"elem:2:getConstructById", "原创·91大神"},
	} {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func yhShort91Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	pg := strconv.Itoa(page)
	switch {
	case category == "hot":
		obj, err := yhShort91Call(map[string]string{
			"mod": "hot", "code": "index",
			"uuid": "e797299777626b984f4fa26fc8706b10",
		})
		if err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		out := []interface{}{}
		if data != nil {
			// 现行服务端 hot 端点回活动 (大赛) 数据 {activity:[{id,
			// title, thumb, mv_count}]}, 非 jadx HotBean 的
			// find/hot 列表; 故出活动文件夹卡, 视频穿透端点未
			// 追通 (见文件头), Detail/Resolve 对活动卡如实报错.
			for _, e := range yhArr(data["activity"]) {
				if m := yhMap(e); m != nil {
					id := g5StrVal(m["id"])
					title := g5StrVal(m["title"])
					if id == "" || title == "" {
						continue
					}
					thumb := g5StrVal(m["thumb"])
					remark := g5StrVal(m["mv_count"])
					yhShort91CacheMu.Lock()
					yhShort91ActivityCache[id] = map[string]interface{}{"title": title, "cover": thumb, "remark": remark}
					yhShort91CacheMu.Unlock()
					out = append(out, makeDrama(slot, "activity:"+id, title, thumb, remark, "", title))
				}
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "home:"):
		code := strings.TrimPrefix(category, "home:")
		obj, err := yhShort91Call(map[string]string{
			"mod": "index", "code": code, "page": pg,
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhShort91Flatten(slot, yhMap(obj["data"]))
		// VideoList 的 data 本身即列表
		if len(items) == 0 {
			items = yhShort91Flatten(slot, map[string]interface{}{"list": yhArr(obj["data"])})
		}
		return items, len(items), len(items) > 0, nil
	case strings.HasPrefix(category, "elem:"):
		parts := strings.SplitN(strings.TrimPrefix(category, "elem:"), ":", 2)
		if len(parts) != 2 {
			return nil, 0, false, fmt.Errorf("yh_short91 bad category %s", category)
		}
		obj, err := yhShort91Call(map[string]string{
			"mod": "element", "code": parts[1], "id": parts[0],
			"page": pg, "limit": "12",
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhShort91Flatten(slot, yhMap(obj["data"]))
		if len(items) == 0 {
			items = yhShort91Flatten(slot, map[string]interface{}{"list": yhArr(obj["data"])})
		}
		return items, len(items), len(items) >= 12, nil
	default:
		obj, err := yhShort91Call(map[string]string{
			"mod": "index", "code": "home", "page": pg,
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhShort91Flatten(slot, yhMap(obj["data"]))
		if len(items) == 0 {
			items = yhShort91Flatten(slot, map[string]interface{}{"list": yhArr(obj["data"])})
		}
		return items, len(items), len(items) > 0, nil
	}
}

func yhShort91Detail(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhShort91Slot+":")
	if strings.HasPrefix(vid, "activity:") {
		yhShort91CacheMu.Lock()
		act, ok := yhShort91ActivityCache[strings.TrimPrefix(vid, "activity:")]
		yhShort91CacheMu.Unlock()
		title, cover, remark := vid, "", ""
		if ok {
			title = g5StrVal(act["title"])
			cover = g5StrVal(act["cover"])
			remark = g5StrVal(act["remark"])
		}
		d := makeDrama(slot, vid, title, cover, remark, "", "活动文件夹, 视频穿透端点待追通")
		return d, nil
	}
	yhShort91CacheMu.Lock()
	rec, ok := yhShort91Cache[vid]
	yhShort91CacheMu.Unlock()
	if !ok {
		// 缓存未命中: 走 getElementItembyId 取元素详情
		if obj, err := yhShort91Call(map[string]string{
			"mod": "element", "code": "getElementItembyId", "id": vid,
			"page": "1", "type": "new",
		}); err == nil {
			items := yhShort91Flatten(slot, yhMap(obj["data"]))
			if len(items) > 0 {
				if m, ok := items[0].(map[string]interface{}); ok {
					return m, nil
				}
			}
		}
		d := makeDrama(slot, vid, vid, "", "", "", "")
		g5SetChapters(d, slot, vid, []string{vid}, []string{"正片"})
		return d, nil
	}
	title := g5StrVal(rec["title"])
	cover := g5StrVal(rec["thumb"])
	if cover == "" {
		cover = g5StrVal(rec["thumbImg"])
	}
	desc := g5StrVal(rec["descriptions"])
	d := makeDrama(slot, vid, title, cover, "", "", desc)
	g5SetChapters(d, slot, vid, []string{vid}, []string{"正片"})
	return d, nil
}

func yhShort91Resolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhShort91Slot+":")
	if strings.HasPrefix(vid, "activity:") {
		return nil, fmt.Errorf("yh_short91: activity folder is not playable")
	}
	yhShort91CacheMu.Lock()
	rec, ok := yhShort91Cache[vid]
	yhShort91CacheMu.Unlock()
	u := ""
	if ok {
		u = g5StrVal(rec["playUrl"])
	}
	if u == "" {
		if _, err := yhShort91Detail(slot, vid); err == nil {
			yhShort91CacheMu.Lock()
			rec, ok = yhShort91Cache[vid]
			yhShort91CacheMu.Unlock()
			if ok {
				u = g5StrVal(rec["playUrl"])
			}
		}
	}
	if u == "" {
		return nil, fmt.Errorf("yh_short91: no play url for %s", vid)
	}
	// 站点偶尔把广告位或占位文案塞进 playUrl（见过直接是 "fuck"），
	// 不校验就会把这个字符串当播放地址交给播放器，界面上表现为
	// 「解析成功但放不出来」。不是 http(s) 一律按取流失败处理。
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		short := []rune(u)
		if len(short) > 40 {
			short = short[:40]
		}
		return nil, fmt.Errorf("yh_short91: invalid play url %q", string(short))
	}
	format := "mp4"
	if strings.Contains(u, ".m3u8") {
		format = "m3u8"
	}
	return playResult(u, slot, rawID, yhShort91UA, yhShort91Base+"/", format, "mp4"), nil
}

func yhShort91Search(slot, kw string) ([]interface{}, error) {
	obj, err := yhShort91Call(map[string]string{
		"mod": "index", "code": "search", "type": "1", "key": kw, "page": "1",
	})
	if err != nil {
		return nil, err
	}
	items := yhShort91Flatten(slot, yhMap(obj["data"]))
	if len(items) == 0 {
		items = yhShort91Flatten(slot, map[string]interface{}{"list": yhArr(obj["data"])})
	}
	return items, nil
}

var yhShort91ImgKey = []byte("f5d965df75336270")
var yhShort91ImgIV = []byte("97b60394abc2fbe1")

func yhShort91ImgMagic(b []byte) bool {
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

// yhShort91CoverFetch: family-shared cover decryption. Cover bytes on
// this family's pic hosts are AES-128-CBC encrypted at rest with the
// same key/IV the app's shared Glide fetcher uses (verified 2026-10-24:
// melon51 pic.sugfew.cn -> JPEG FF D8 FF E0, utube picx.drleao.cn ->
// PNG 89 50 after decrypt; plaintext covers pass through untouched,
// undecodable bodies are returned raw, as the app does).
func yhShort91CoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpDo("GET", u, nil, yhShort91Headers())
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("yh_short91 cover http %d", code)
	}
	if yhShort91ImgMagic(body) || len(body)%aes.BlockSize != 0 {
		return body, nil
	}
	block, err := aes.NewCipher(yhShort91ImgKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, yhShort91ImgIV).CryptBlocks(out, body)
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
	if yhShort91ImgMagic(out) {
		return out, nil
	}
	return body, nil
}

func init() {
	registerSite(yhShort91Slot, &siteImpl{
		Key:        yhShort91Slot,
		Name:       "91短视频",
		DefaultCat: "home:auth",
		HomeURL:    yhShort91Base + "/",
		Headers:    yhShort91Headers,
		CoverFetch: yhShort91CoverFetch,
		Categories: yhShort91Categories,
		Catalog:    yhShort91Catalog,
		Detail:     yhShort91Detail,
		Resolve:    yhShort91Resolve,
		Search:     yhShort91Search,
	})
}
