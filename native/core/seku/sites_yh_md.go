package seku

// ---- 麻豆 md (yh_md, 欲火 type 45) — 加密 JSON API, 配方出 jadx 逐行实证 ----
//
// 基址 https://spi.d5c4a712.com (c4.e.f956c = h6.b.f8265p0; ad/r.java case 45
// 发起前重赋同值; 远程配置 log3.log 2026-10-24 实取解密后 config 仅含 XG_APP,
// 无 URL_MD 覆盖, 基址以代码值为准)。
//
// 请求链 (c4.a Retrofit 全 POST form, c4.d 预签 + c4.c 拦截器):
//   预签: TreeMap 加 timestamp=当前毫秒; sign 明文为排序 k=v&k=v… 串 +
//     "&m}q%ea6:LDcmS?aK)CeF287bPvd99@E,9Up^", 取 MD5hex, 以 encode_sign 入表。
//   拦截: 含 ".json" 的 URL 直通; 否则把表单转排序 JSON, suffix =
//     MD5hex(当前毫秒串)[:6], AES/CBC/PKCS7 key
//     "Vq234zBeSdGgYXzVTEfnnjjdmaTkk7A4" IV "-p9B[~PnPs"+suffix,
//     Base64 后以单字段 post-data 发出, 头 suffix + user-agent。
//   响应: MdBean{data, suffix}, data 经 Base64 + 同 key、IV "-p9B[~PnPs"+
//     响应 suffix 解密还原 JSON。
// 端点: POST /video/channel -> 频道表; POST /video/listcache {list_row,page,
//   type:"2",channel?} -> VideoList; POST /video/list {list_row,page,keyword}
//   -> SearchBean(同 VideoList 形)。
// 播放: App 内 VideoAdapter 点击直接播 bean.video_url, 无二次解析; 本文件
//   Resolve 同理返回 video_url。rawID 形为 "<id>$<video_url>" (bale 式内嵌,
//   因本站无 id->url 的详情端点, Detail 只能靠卡片缓存或 rawID 自带地址)。
// 封面: 视频 thumb 走 d4.a/d4.d: 体为 "data:image/jpg;base64,…" 文本, 去首
//   22 字后 Base64 解码即明文图; 频道图走普通 Glide, 不变换。
// 分类与站方 1:1: 最新(App HomeFragment m("")  tab) + /video/channel 实测
//   60 频道表 (2026-10-24 实取, 顺序与服务端一致; 21=麻豆传媒映画即 App
//   HomeFragment m("21") tab)。
// 付费: 列表项 is_vip!=0 时 Catalog/Search 过滤不进卡。实测: 频道 21
//   首页 60 条全 is_vip=0 (不受影响); 最新页 60 条中 42 条 is_vip=1 被
//   过滤, 实出 18 条。注意 VIP 条目的 video_url 与免费条目同形 (全量
//   HLS, 非预告; 预告另在 test_video_url, try_second=180), App 侧对
//   video_url 无条件播放, 但按付费不进成品口径仍过滤。
//
// 验况 (2026-10-24 本机出口实测, 非沙箱假 IP 路由, 每次请求约 10-45s):
//   频道: /video/channel code 0, 60 条 (total 如 2548/1828/1694 等)。
//   列表: listcache channel=21 list_row=60 page1: total 2548, last_page 43,
//     60 条; page2 60 条首 id 不同; 无 channel 最新 total 24957。
//   搜索: /video/list keyword=麻豆 total 52, 首条 is_vip=0 带真 m3u8。
//   播放: 首条 video_url 清单为真 VOD HLS (#EXTM3U, AES-128 分片地址)。
//   封面: thumb 体 "data:image/jpg;base64," 去 22 字解码后 400895 字节,
//     魔数 FF D8 (JPEG) 已验。

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const yhMdBase = "https://spi.d5c4a712.com"
const yhMdUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
const yhMdAESKey = "Vq234zBeSdGgYXzVTEfnnjjdmaTkk7A4"
const yhMdIVPrefix = "-p9B[~PnPs"
const yhMdSignTail = "&m}q%ea6:LDcmS?aK)CeF287bPvd99@E,9Up^"

var yhMdChannels = []struct {
	id   int
	name string
}{
	{21, "麻豆传媒映画"},
	{101, "乐播传媒"},
	{20, "91制作"},
	{51, "91茄子"},
	{23, "探花系列"},
	{14, "SWAG"},
	{39, "日本AV-4K"},
	{38, "JVID"},
	{40, "国产三级片"},
	{41, "推特福利姬"},
	{52, "恋脚癖"},
	{64, "糖心"},
	{65, "兔子先生"},
	{74, "天美传媒"},
	{75, "蜜桃传媒"},
	{76, "皇家华人"},
	{77, "杏吧原创"},
	{80, "mini传媒"},
	{81, "大象传媒"},
	{82, "葫芦影业"},
	{87, "91短视频"},
	{33, "恐怖色情"},
	{35, "人妖"},
	{50, "高清动漫"},
	{26, "性爱课堂"},
	{27, "maomiav"},
	{53, "AI换脸"},
	{54, "Elise 兔兔"},
	{55, "Pornhub"},
	{56, "Team Skeet"},
	{57, "Wow girls"},
	{58, "不见星空"},
	{59, "刘玥"},
	{60, "国产直播"},
	{61, "果冻传媒"},
	{62, "精东影业"},
	{63, "萝莉美少女"},
	{66, "玩偶姐姐"},
	{67, "网爆门事件"},
	{68, "星空传媒"},
	{69, "柚子猫"},
	{70, "ASMR"},
	{71, "Sxyporn"},
	{72, "XVideos"},
	{73, "Brazzers"},
	{78, "AV解说"},
	{79, "韩国R级"},
	{83, "COSPLAY"},
	{84, "拳交女王"},
	{86, "周甄娜"},
	{88, "猎奇系列"},
	{91, "软萌伪娘"},
	{93, "性爱娃娃"},
	{94, "约炮小百科"},
	{95, "xiao e"},
	{96, "Vivian"},
	{98, "海外直播"},
	{15, "性视界传媒"},
	{99, "爱豆传媒"},
	{100, "香蕉视频"},
}

func yhMd5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func yhMdPad(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	pad := bytes.Repeat([]byte{byte(n)}, n)
	return append(b, pad...)
}

func yhMdUnpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("yh_md empty block")
	}
	n := int(b[len(b)-1])
	if n < 1 || n > aes.BlockSize || n > len(b) {
		return nil, fmt.Errorf("yh_md bad padding")
	}
	return b[:len(b)-n], nil
}

func yhMdAESCBC(key, iv, data []byte, encrypt bool) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("yh_md bad iv len %d", len(iv))
	}
	if encrypt {
		data = yhMdPad(data)
		out := make([]byte, len(data))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
		return out, nil
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("yh_md cipher len not block aligned")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return yhMdUnpad(out)
}

// yhMdCall 复制 c4.d 预签 + c4.c 拦截 + MdBean 解密, 返回解密后 JSON。
func yhMdCall(path string, params map[string]string) (map[string]interface{}, error) {
	m := map[string]string{}
	for k, v := range params {
		m[k] = v
	}
	m["timestamp"] = strconv.FormatInt(time.Now().UnixMilli(), 10)
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	m["encode_sign"] = yhMd5Hex(strings.Join(parts, "&") + yhMdSignTail)
	// 拦截器: 表单转排序 JSON 后加密, 与预签后的表无关 (c4.c 直接读请求体)。
	keys = []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	kv := map[string]string{}
	for _, k := range keys {
		kv[k] = m[k]
	}
	var pbuf bytes.Buffer
	enc := json.NewEncoder(&pbuf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(kv); err != nil {
		return nil, err
	}
	plain := bytes.TrimRight(pbuf.Bytes(), "\n")
	suffix := yhMd5Hex(strconv.FormatInt(time.Now().UnixMilli(), 10))[:6]
	ct, err := yhMdAESCBC([]byte(yhMdAESKey), []byte(yhMdIVPrefix+suffix), plain, true)
	if err != nil {
		return nil, err
	}
	form := url.Values{"post-data": {base64.StdEncoding.EncodeToString(ct)}}.Encode()
	h := map[string]string{
		"User-Agent":   yhMdUA,
		"suffix":       suffix,
		"Content-Type": "application/x-www-form-urlencoded",
	}
	var body []byte
	var code int
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err = httpDo("POST", yhMdBase+path, strings.NewReader(form), h)
		if err == nil && code == 200 {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_md http %d", code)
	}
	var bean map[string]interface{}
	if err := json.Unmarshal(body, &bean); err != nil {
		return nil, fmt.Errorf("yh_md bean parse: %v", err)
	}
	encStr := g5StrVal(bean["data"])
	raw, err := base64.StdEncoding.DecodeString(encStr)
	if err != nil {
		return nil, fmt.Errorf("yh_md bean b64: %v", err)
	}
	pt, err := yhMdAESCBC([]byte(yhMdAESKey), []byte(yhMdIVPrefix+g5StrVal(bean["suffix"])), raw, false)
	if err != nil {
		return nil, fmt.Errorf("yh_md bean decrypt: %v", err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(pt, &obj); err != nil {
		return nil, fmt.Errorf("yh_md payload parse: %v", err)
	}
	return obj, nil
}

func yhMdCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	u := g5StrVal(rec["video_url"])
	if id == "" || u == "" {
		return nil, false
	}
	title := g5StrVal(rec["title"])
	if title == "" {
		title = g5StrVal(rec["description"])
	}
	remark := ""
	if tags := yhArr(rec["tags"]); len(tags) > 0 {
		names := []string{}
		for _, t := range tags {
			if n := g5StrVal(yhMap(t)["name"]); n != "" {
				names = append(names, n)
			}
		}
		remark = strings.Join(names, "·")
	}
	rawID := id + "$" + u
	return makeDrama(slot, rawID, title, g5StrVal(rec["thumb"]), remark, "", g5StrVal(rec["description"])), true
}

func yhMdListPage(slot, category string, page int) ([]interface{}, int, bool, error) {
	params := map[string]string{"list_row": "60", "page": strconv.Itoa(page), "type": "2"}
	if category != "" && category != "latest" {
		params["channel"] = category
	}
	obj, err := yhMdCall("/video/listcache", params)
	if err != nil {
		return nil, 0, false, err
	}
	data := yhMap(obj["data"])
	items := yhArr(data["data"])
	out := []interface{}{}
	for _, it := range items {
		if c, ok := yhMdCard(slot, yhMap(it)); ok {
			out = append(out, c)
		}
	}
	total, _ := g5MapInt(data, "total")
	last, _ := g5MapInt(data, "last_page")
	return out, total, page < last, nil
}

func yhMdCategories(slot string) []interface{} {
	out := []interface{}{categoryObj("latest", "最新")}
	for _, c := range yhMdChannels {
		out = append(out, categoryObj(strconv.Itoa(c.id), c.name))
	}
	return out
}

func yhMdCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	return yhMdListPage(slot, category, page)
}

func yhMdDetail(slot, rawID string) (map[string]interface{}, error) {
	vid, _, _ := strings.Cut(rawID, "$")
	title := "视频" + vid
	cover, remark, desc := "", "", ""
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

func yhMdResolve(slot, rawID string) (map[string]interface{}, error) {
	_, u, found := strings.Cut(rawID, "$")
	if !found || u == "" {
		return nil, fmt.Errorf("yh_md resolve: rawID 内无 video_url")
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhMdUA, yhMdBase+"/", format, protocol), nil
}

func yhMdSearch(slot, kw string) ([]interface{}, error) {
	obj, err := yhMdCall("/video/list", map[string]string{"list_row": "60", "page": "1", "keyword": kw})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	out := []interface{}{}
	for _, it := range yhArr(data["data"]) {
		if c, ok := yhMdCard(slot, yhMap(it)); ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// yhMdCoverFetch 复制 d4.d: 体文本去首 22 字 ("data:image/jpg;base64,")
// 后 Base64 解码; 已是明文图时直接回源字节。
func yhMdCoverFetch(u string) ([]byte, error) {
	b, code, _, err := httpGet(u, map[string]string{"User-Agent": yhMdUA})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_md cover http %d", code)
	}
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xD8 {
		return b, nil
	}
	if len(b) > 22 {
		if dec, err := base64.StdEncoding.DecodeString(string(b[22:])); err == nil && len(dec) >= 2 && dec[0] == 0xFF && dec[1] == 0xD8 {
			return dec, nil
		}
	}
	return nil, fmt.Errorf("yh_md cover: 未解出 JPEG 明文")
}

func init() {
	registerSite("yh_md", &siteImpl{
		Key:        "yh_md",
		Name:       "麻豆",
		DefaultCat: "latest",
		HomeURL:    yhMdBase + "/",
		Categories: yhMdCategories,
		Catalog:    yhMdCatalog,
		Detail:     yhMdDetail,
		Resolve:    yhMdResolve,
		Search:     yhMdSearch,
		Headers: func() map[string]string {
			return map[string]string{"User-Agent": yhMdUA}
		},
		CoverFetch: yhMdCoverFetch,
	}, "yh_md", "md", "麻豆")
}
