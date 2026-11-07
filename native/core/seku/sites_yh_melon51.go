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

// ---- melon51 51 西瓜 (yh_melon51) — 吃瓜文章站 PWA 加密 API ----
//
// 配方出欲火 APK jadx 实证 (ui/melon51 + k4 ApiService/Cg51Utils +
// h6.b 默认基址):
//   base = https://bpi4.ofwyemne.cc (h6.b.F0, 写死)
//   信封 (k4.c.a/b + Melon51 各调用点):
//     外层 form: client=pwa, _ver=v4, timestamp=<秒>, data, sign
//     data = Base64( AES-128-CBC/PKCS7( JSON(biz), key, iv ) )
//     key = 63a596644d5f430d, iv = 01886c8acd691d99
//     sign = MD5hex( SHA256hex( "client=pwa&data="+data+
//            "&timestamp="+ts+"68e347ceebacf415bd8e00883451d046" ) )
//     响应 {"data":"<Base64>"} 同 key/iv 解密得业务 JSON (k4.c 拦截器)
//   biz 公共 (k4.c.b): oauth_id=<MD5hex(SHA256hex(<毫秒>))>
//   (启动时 c.c(毫秒) 生成, 见 Melon51LaunchActivity),
//   bundleId=com.pwa.cgqz, version=3.8.9, oauth_type=android,
//   language=zh, net_type=wifi, token="".
//   端点 (k4.a): /api.php/api/contents/list_contents
//   (biz mid/page/limit=30), /api.php/api/contents/list_contents_tag
//   (biz mid/page), /api.php/api/contents/list_tags,
//   /api.php/api/contents/search (biz kwy/page/limit=18),
//   /api.php/api/contents/{path} (biz cid; path=detail_content,
//   或类型 big_went 时 bigEvent、skits 时 skits).
// 分类与 App 导航 1:1:
//   首页 (HomeFragment): 0 首页 / -1 秘闻 / 1 今日吃瓜 /
//     10554 吃瓜榜单 / 4 热门大瓜 / 3141 必看大瓜;
//   分类 (TypeFragment 章节): 1 实时热点 / 23 原创专区 /
//     22 大神专区 / 17 成人精选 / 13 成人AI / 3 明星网红黑料 /
//     21 发片求种 (mid 与首页同一编号空间, 故 1 去重);
//   标签 (TagFragment): list_tags 的 popular[] 动态标签.
// 文章卡: ArticleBean{cid, title, created, fields[0].str_value=封面,
//   author.screenName, type}; 详情 data.cur.content 为 HTML,
//   ArticleContent 解析: p=文本, img=图片, video(src=播放地址,
//   pic=封面), a=链接; 含 "<h2>TOP 1" 时为榜单体 (h2/span/a/img
//   四列, a href 的 /archives/<cid>/ 为子文章).
// 播放: 详情视频段即播 (video src 直链); 纯图文帖无视频段,
//   Resolve 如实报错, 不编造地址.
// 付费: 无 coins/price 字段, 不设账本墙.
// 封面: pic.sugfew.cn 封面字节为 AES-128-CBC 加密 (族共享 Glide 密钥,
//   见 yhMelon51CoverFetch); 2026-10-24 实测解密后首字节 FF D8 FF E0
//   (JPEG/JFIF), 明文魔数已验; 未解密前验魔数必为 UNKNOWN, 属正常.
// 验真 (2026-10-24 本机): list_contents mid=0 实拉 200, 首条
//   cid 279713「贵州遵义敖溪…」当日新帖, fields[0] 封面为真图
//   URL; 其余腿按同一信封实现, 待 Go 冒烟逐腿补验.

const yhMelon51Slot = "yh_melon51"
const yhMelon51Base = "https://bpi4.ofwyemne.cc"
const yhMelon51UA = "Mozilla/5.0 (Linux; Android 13; SM-A205GN) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"

var yhMelon51Key = []byte("63a596644d5f430d")
var yhMelon51IV = []byte("01886c8acd691d99")

const yhMelon51Secret = "68e347ceebacf415bd8e00883451d046"

func yhMelon51Headers() map[string]string {
	return map[string]string{
		"User-Agent":   yhMelon51UA,
		"Content-Type": "application/x-www-form-urlencoded",
		"Referer":      yhMelon51Base + "/",
	}
}

func yhMelon51Pad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhMelon51Enc(plain []byte) string {
	block, _ := aes.NewCipher(yhMelon51Key)
	out := make([]byte, len(yhMelon51Pad(plain)))
	cipher.NewCBCEncrypter(block, yhMelon51IV).CryptBlocks(out, yhMelon51Pad(plain))
	return base64.StdEncoding.EncodeToString(out)
}

func yhMelon51Dec(b64s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64s)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw)%16 != 0 {
		return nil, fmt.Errorf("yh_melon51 bad cipher len")
	}
	block, _ := aes.NewCipher(yhMelon51Key)
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhMelon51IV).CryptBlocks(out, raw)
	n := int(out[len(out)-1])
	if n < 1 || n > 16 || n > len(out) {
		return nil, fmt.Errorf("yh_melon51 bad pad")
	}
	return out[:len(out)-n], nil
}

func yhMelon51Sign(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", md5.Sum([]byte(hex.EncodeToString(sum[:]))))
}

func yhMelon51Call(path string, biz map[string]string) (map[string]interface{}, error) {
	millis := strconv.FormatInt(time.Now().UnixMilli(), 10)
	b := map[string]string{
		"oauth_id":   yhMelon51Sign(millis),
		"bundleId":   "com.pwa.cgqz",
		"version":    "3.8.9",
		"oauth_type": "android",
		"language":   "zh",
		"net_type":   "wifi",
		"token":      "",
	}
	for k, v := range biz {
		b[k] = v
	}
	jb, _ := json.Marshal(b)
	data := yhMelon51Enc(jb)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := yhMelon51Sign("client=pwa&data=" + data + "&timestamp=" + ts + yhMelon51Secret)
	form := url.Values{
		"client":    {"pwa"},
		"_ver":      {"v4"},
		"timestamp": {ts},
		"data":      {data},
		"sign":      {sign},
	}.Encode()
	var body []byte
	var code int
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err = httpDo("POST", yhMelon51Base+path, strings.NewReader(form), yhMelon51Headers())
		if err == nil && code == 200 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_melon51 http %d", code)
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("yh_melon51 outer parse: %v", err)
	}
	enc := g5StrVal(outer["data"])
	if enc == "" {
		return outer, nil
	}
	plain, err := yhMelon51Dec(enc)
	if err != nil {
		return nil, fmt.Errorf("yh_melon51 decrypt: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("yh_melon51 parse: %v", err)
	}
	return out, nil
}

var (
	yhMelon51TagRe   = regexp.MustCompile(`<[^>]+>`)
	yhMelon51VideoRe = regexp.MustCompile(`(?s)<video[^>]*>`)
	yhMelon51ImgRe   = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	yhMelon51PRe     = regexp.MustCompile(`(?s)<p[^>]*>(.*?)</p>`)
)

func yhMelon51Clean(s string) string {
	s = yhMelon51TagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(s)
}

func yhMelon51Card(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	cid := g5StrVal(rec["cid"])
	title := yhMelon51Clean(g5StrVal(rec["title"]))
	if cid == "" || title == "" {
		return nil, false
	}
	cover := ""
	if fields := yhArr(rec["fields"]); len(fields) > 0 {
		if fm := yhMap(fields[0]); fm != nil {
			cover = g5StrVal(fm["str_value"])
		}
	}
	author := ""
	if am := yhMap(rec["author"]); am != nil {
		author = g5StrVal(am["screenName"])
	}
	remark := g5StrVal(rec["created"])
	desc := author
	if n := g5StrVal(rec["commentsNum"]); n != "" {
		desc = strings.TrimSpace(desc + " 评论" + n)
	}
	typ := g5StrVal(rec["type"])
	return makeDrama(slot, cid+"|"+typ, title, cover, remark, "", desc), true
}

func yhMelon51Cards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhMelon51Card(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func yhMelon51Categories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range []struct{ id, name string }{
		{"mid:0", "首页"}, {"mid:-1", "秘闻"}, {"mid:1", "今日吃瓜"},
		{"mid:10554", "吃瓜榜单"}, {"mid:4", "热门大瓜"}, {"mid:3141", "必看大瓜"},
		{"mid:23", "原创专区"}, {"mid:22", "大神专区"}, {"mid:17", "成人精选"},
		{"mid:13", "成人AI"}, {"mid:3", "明星网红黑料"}, {"mid:21", "发片求种"},
	} {
		out = append(out, categoryObj(c.id, c.name))
	}
	// 动态标签 (TagFragment: list_tags -> popular[])
	if obj, err := yhMelon51Call("/api.php/api/contents/list_tags", map[string]string{}); err == nil {
		if d := yhMap(obj["data"]); d != nil {
			for _, e := range yhArr(d["popular"]) {
				if m := yhMap(e); m != nil {
					if mid, name := g5StrVal(m["mid"]), g5StrVal(m["name"]); mid != "" && name != "" {
						out = append(out, categoryObj("tag:"+mid, "标签·"+name))
					}
				}
			}
		}
	}
	return out
}

func yhMelon51Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	pg := strconv.Itoa(page)
	var obj map[string]interface{}
	var err error
	switch {
	case strings.HasPrefix(category, "tag:"):
		obj, err = yhMelon51Call("/api.php/api/contents/list_contents_tag", map[string]string{
			"mid": strings.TrimPrefix(category, "tag:"), "page": pg,
		})
	case strings.HasPrefix(category, "mid:"):
		obj, err = yhMelon51Call("/api.php/api/contents/list_contents", map[string]string{
			"mid": strings.TrimPrefix(category, "mid:"), "page": pg, "limit": "30",
		})
	default:
		obj, err = yhMelon51Call("/api.php/api/contents/list_contents", map[string]string{
			"mid": "0", "page": pg, "limit": "30",
		})
	}
	if err != nil {
		return nil, 0, false, err
	}
	data := yhMap(obj["data"])
	items := yhMelon51Cards(slot, yhArr(data["list"]))
	total := len(items)
	if t, ok := data["count"].(float64); ok {
		total = int(t)
	}
	return items, total, len(items) >= 30, nil
}

type yhMelon51Seg struct {
	PlayURL string
	Pic     string
}

func yhMelon51ParseContent(html string) (videos []yhMelon51Seg, texts []string, pics []string) {
	for _, tag := range yhMelon51VideoRe.FindAllString(html, -1) {
		// video 标签属性顺序不定 (src/pic/data-src), 逐属性兜底取
		src, pic := "", ""
		if sm := regexp.MustCompile(`src="([^"]+)"`).FindStringSubmatch(tag); sm != nil {
			src = sm[1]
		}
		if pm := regexp.MustCompile(`pic="([^"]+)"`).FindStringSubmatch(tag); pm != nil {
			pic = pm[1]
		}
		if src != "" {
			videos = append(videos, yhMelon51Seg{PlayURL: src, Pic: pic})
		}
	}
	for _, m := range yhMelon51ImgRe.FindAllStringSubmatch(html, -1) {
		pics = append(pics, m[1])
	}
	for _, m := range yhMelon51PRe.FindAllStringSubmatch(html, -1) {
		if t := yhMelon51Clean(m[1]); t != "" {
			texts = append(texts, t)
		}
	}
	return videos, texts, pics
}

func yhMelon51DetailRaw(cid, typ string) (map[string]interface{}, []yhMelon51Seg, []string, []string, error) {
	path := "detail_content"
	if strings.EqualFold(typ, "big_went") {
		path = "bigEvent"
	} else if strings.EqualFold(typ, "skits") {
		path = "skits"
	}
	obj, err := yhMelon51Call("/api.php/api/contents/"+path, map[string]string{"cid": cid})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	data := yhMap(obj["data"])
	cur := yhMap(data["cur"])
	content := ""
	if cur != nil {
		content = g5StrVal(cur["content"])
	}
	// bigEvent/skits 形态: 内容在 data.big_event / data.skits 侧
	if content == "" {
		if be := yhMap(data["big_event"]); be != nil {
			content = g5StrVal(be["content"])
		}
		if sk := yhMap(data["skits"]); sk != nil && content == "" {
			content = g5StrVal(sk["content"])
		}
	}
	videos, texts, pics := yhMelon51ParseContent(content)
	// TOP 榜单体 (含 <h2>TOP 1) 的子文章链接形如 /archives/<cid>/,
	// 本实现不展开榜单子帖 (详情按主帖视频段播), 榜单卡由列表直接出.
	return obj, videos, texts, pics, nil
}

func yhMelon51Detail(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhMelon51Slot+":")
	parts := strings.SplitN(vid, "|", 2)
	cid, typ := parts[0], ""
	if len(parts) == 2 {
		typ = parts[1]
	}
	obj, videos, texts, pics, err := yhMelon51DetailRaw(cid, typ)
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	cur := yhMap(data["cur"])
	title, cover, desc := cid, "", strings.Join(texts, "\n")
	if cur != nil {
		title = yhMelon51Clean(g5StrVal(cur["title"]))
		if fields := yhArr(cur["fields"]); len(fields) > 0 {
			if fm := yhMap(fields[0]); fm != nil {
				cover = g5StrVal(fm["str_value"])
			}
		}
	}
	if cover == "" && len(pics) > 0 {
		cover = pics[0]
	}
	if len(desc) > 800 {
		desc = desc[:800]
	}
	d := makeDrama(slot, vid, title, cover, "", "", desc)
	if len(videos) > 0 {
		epIDs, names := []string{}, []string{}
		for i, v := range videos {
			_ = v
			epIDs = append(epIDs, vid+"#"+strconv.Itoa(i))
			names = append(names, fmt.Sprintf("视频%d", i+1))
		}
		g5SetChapters(d, slot, vid, epIDs, names)
	}
	return d, nil
}

func yhMelon51Resolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhMelon51Slot+":")
	// 章节 id 形如 <cid>|<type>#<segIdx>
	segIdx := 0
	base := vid
	if j := strings.Index(vid, "#"); j >= 0 {
		base = vid[:j]
		segIdx, _ = strconv.Atoi(vid[j+1:])
	}
	parts := strings.SplitN(base, "|", 2)
	cid, typ := parts[0], ""
	if len(parts) == 2 {
		typ = parts[1]
	}
	_, videos, _, _, err := yhMelon51DetailRaw(cid, typ)
	if err != nil {
		return nil, err
	}
	if len(videos) == 0 {
		return nil, fmt.Errorf("yh_melon51: article %s has no video", cid)
	}
	if segIdx < 0 || segIdx >= len(videos) {
		segIdx = 0
	}
	u := videos[segIdx].PlayURL
	format := "mp4"
	if strings.Contains(u, ".m3u8") {
		format = "m3u8"
	}
	return playResult(u, slot, rawID, yhMelon51UA, yhMelon51Base+"/", format, "mp4"), nil
}

func yhMelon51Search(slot, kw string) ([]interface{}, error) {
	obj, err := yhMelon51Call("/api.php/api/contents/search", map[string]string{
		"kwy": kw, "page": "1", "limit": "18",
	})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	return yhMelon51Cards(slot, yhArr(data["list"])), nil
}

var yhMelon51ImgKey = []byte("f5d965df75336270")
var yhMelon51ImgIV = []byte("97b60394abc2fbe1")

func yhMelon51ImgMagic(b []byte) bool {
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

// yhMelon51CoverFetch: family-shared cover decryption. Cover bytes on
// this family's pic hosts are AES-128-CBC encrypted at rest with the
// same key/IV the app's shared Glide fetcher uses (verified 2026-10-24:
// melon51 pic.sugfew.cn -> JPEG FF D8 FF E0, utube picx.drleao.cn ->
// PNG 89 50 after decrypt; plaintext covers pass through untouched,
// undecodable bodies are returned raw, as the app does).
func yhMelon51CoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpDo("GET", u, nil, yhMelon51Headers())
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("yh_melon51 cover http %d", code)
	}
	if yhMelon51ImgMagic(body) || len(body)%aes.BlockSize != 0 {
		return body, nil
	}
	block, err := aes.NewCipher(yhMelon51ImgKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, yhMelon51ImgIV).CryptBlocks(out, body)
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
	if yhMelon51ImgMagic(out) {
		return out, nil
	}
	return body, nil
}

func init() {
	registerSite(yhMelon51Slot, &siteImpl{
		Key:        yhMelon51Slot,
		Name:       "51西瓜",
		DefaultCat: "mid:0",
		HomeURL:    yhMelon51Base + "/",
		Headers:    yhMelon51Headers,
		CoverFetch: yhMelon51CoverFetch,
		Categories: yhMelon51Categories,
		Catalog:    yhMelon51Catalog,
		Detail:     yhMelon51Detail,
		Resolve:    yhMelon51Resolve,
		Search:     yhMelon51Search,
	})
}
