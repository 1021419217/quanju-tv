package seku

// ---- 麻豆 2 md2 (yh_md2, 欲火 type 48) — 与 md 同族, 差异逐行实证 ----
//
// 基址 https://gcmw84.cc (e4.d.f7517c 静态初值虽为 md 基址, 但 ad/r.java
// case 48 发起前重赋 h6.b.f8269r0 = gcmw84.cc, 运行时基址以此为准)。
// 加密/预签/拦截与 md 逐字节同配方 (e4.c/e4.b): 同 AES key、IV 前缀、
// sign 尾串、post-data/suffix/user-agent 行为、".json" 直通。
// 差异 (e4.a Retrofit + y/a.java 合成变换 + VideoAdapter):
//   端点带 /api 前缀: POST /api/video/list -> SearchBean; POST
//     /api/video/channel -> ChanelList; POST /api/video/listcache ->
//     MdApiBean{code,msg,data:{file_name}}; 另有 @GET @Url 取 VideoList。
//   列表二跳: listcache 回 file_name, 再以该 URL 原样 GET (走 ".json"
//     直通, 不经拦截加密), 直接得 VideoList; 其 getData() 即条目数组
//     (md 为 getData().getData() 两层嵌套, md2 为一层; 本文件解析时
//     数组/对象嵌套两种形态都兼容)。
//   首页 tab 与 md 顺序相反: 最新("")在前, 91TV("21")在后。
//   播放签名 (VideoList.DataEntity.generateVideo_url, md2 独有):
//     video_url 为空或非 http 开头 -> 死值 https://www.baidu.com (本文件
//     视为空播放, Catalog 直接丢弃该条);
//     否则 wsTime = 当前秒+7200, path = URL path 并把连续斜杠压成单斜杠,
//     wsSecret = MD5hex("E9MpC7D5AqWvjmXL3hGkQ2XjZNohAQ" + path + wsTime),
//     结果 = origin + path + "?wsSecret=" + wsSecret + "&wsTime=" + wsTime。
//     签名在 Resolve 时现算 (2 小时有效期, 不在 Catalog 时固化)。
//     rawID 形为 "<id>$<原始 video_url>"。
//   封面变换不同 (f4.d, md2 独有): 体先 Base64 解码, 再 DES/CBC/
//     PKCS7Padding (key "pnhXgN0U", IV "GY4gEvBD") 解密; 解出串去首 22 字
//     后再 Base64 解码得图; 解密失败时回源字节。
//
// 判定: 例外点名① (服务端域名已死, 非本机出口问题), 多路实证 (2026-10-24):
//   Google 公共 DNS (DoH) 对 gcmw84.cc 回 NXDOMAIN (Status 3, 无 Answer);
//   HTTP-DNS 119.28.28.28/d?dn=gcmw84.cc 回 "0" (无记录; 同法对 md 基址
//     正常回 Cloudflare IP);
//   本机出口 TLS 后服务器空回 (Empty reply), POST /api/video/channel
//     四次重试皆空体;
//   jina 阅读代理回 422 "Domain 'gcmw84.cc' could not be resolved";
//   远程配置 log3.log 当日实取解密后 config 仅含 XG_APP, 无 URL_MD2 覆盖。
//   故以下代码未经活体校准: 加密层与 md 逐字节同配方且 md 侧已活体全通
//   (频道/列表/搜索/真 m3u8/封面 JPEG 魔数), 差异点均按 jadx 逐行复刻;
//   域名恢复或远程配置给出新基址后, 仅改 yhMd2Base 即可重验。
// 付费: 与 md 同口径, is_vip!=0 的条目 Catalog/Search 过滤。

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
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

const yhMd2Base = "https://gcmw84.cc"
const yhMd2UA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
const yhMd2AESKey = "Vq234zBeSdGgYXzVTEfnnjjdmaTkk7A4"
const yhMd2IVPrefix = "-p9B[~PnPs"
const yhMd2SignTail = "&m}q%ea6:LDcmS?aK)CeF287bPvd99@E,9Up^"
const yhMd2WsKey = "E9MpC7D5AqWvjmXL3hGkQ2XjZNohAQ"
const yhMd2DESKey = "pnhXgN0U"
const yhMd2DESIV = "GY4gEvBD"

func yhMd25Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func yhMd2Pad(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

func yhMd2Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("yh_md2 empty block")
	}
	n := int(b[len(b)-1])
	if n < 1 || n > aes.BlockSize || n > len(b) {
		return nil, fmt.Errorf("yh_md2 bad padding")
	}
	return b[:len(b)-n], nil
}

func yhMd2AESCBC(iv, data []byte, encrypt bool) ([]byte, error) {
	block, err := aes.NewCipher([]byte(yhMd2AESKey))
	if err != nil {
		return nil, err
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("yh_md2 bad iv len %d", len(iv))
	}
	if encrypt {
		data = yhMd2Pad(data)
		out := make([]byte, len(data))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
		return out, nil
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("yh_md2 cipher len not block aligned")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return yhMd2Unpad(out)
}

// yhMd2Call 复制 e4.c 预签 + e4.b 拦截 + MdBean 解密, 返回解密后 JSON。
func yhMd2Call(path string, params map[string]string) (map[string]interface{}, error) {
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
	m["encode_sign"] = yhMd25Hex(strings.Join(parts, "&") + yhMd2SignTail)
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
	suffix := yhMd25Hex(strconv.FormatInt(time.Now().UnixMilli(), 10))[:6]
	ct, err := yhMd2AESCBC([]byte(yhMd2IVPrefix+suffix), plain, true)
	if err != nil {
		return nil, err
	}
	form := url.Values{"post-data": {base64.StdEncoding.EncodeToString(ct)}}.Encode()
	h := map[string]string{
		"User-Agent":   yhMd2UA,
		"suffix":       suffix,
		"Content-Type": "application/x-www-form-urlencoded",
	}
	var body []byte
	var code int
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err = httpDo("POST", yhMd2Base+path, strings.NewReader(form), h)
		if err == nil && code == 200 {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_md2 http %d", code)
	}
	var bean map[string]interface{}
	if err := json.Unmarshal(body, &bean); err != nil {
		return nil, fmt.Errorf("yh_md2 bean parse: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(g5StrVal(bean["data"]))
	if err != nil {
		return nil, fmt.Errorf("yh_md2 bean b64: %v", err)
	}
	pt, err := yhMd2AESCBC([]byte(yhMd2IVPrefix+g5StrVal(bean["suffix"])), raw, false)
	if err != nil {
		return nil, fmt.Errorf("yh_md2 bean decrypt: %v", err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(pt, &obj); err != nil {
		return nil, fmt.Errorf("yh_md2 payload parse: %v", err)
	}
	return obj, nil
}

func yhMd2Card(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	if vip, ok := g5MapInt(rec, "is_vip"); ok && vip != 0 {
		return nil, false
	}
	id := g5StrVal(rec["id"])
	u := g5StrVal(rec["video_url"])
	if id == "" || u == "" || !strings.HasPrefix(u, "http") {
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
	return makeDrama(slot, id+"$"+u, title, g5StrVal(rec["thumb"]), remark, "", g5StrVal(rec["description"])), true
}

// yhMd2ListItems 走 listcache -> file_name 二跳, 返回条目数组。
func yhMd2ListItems(category string, page int) ([]interface{}, int, bool, error) {
	params := map[string]string{"list_row": "60", "page": strconv.Itoa(page), "type": "2"}
	if category != "" && category != "latest" {
		params["channel"] = category
	}
	obj, err := yhMd2Call("/api/video/listcache", params)
	if err != nil {
		return nil, 0, false, err
	}
	fileName := g5StrVal(yhMap(obj["data"])["file_name"])
	if fileName == "" {
		return nil, 0, false, fmt.Errorf("yh_md2 listcache 无 file_name")
	}
	fetchURL := fileName
	if !strings.HasPrefix(fetchURL, "http") {
		fetchURL = yhMd2Base + fetchURL
	}
	body, code, _, err := httpGet(fetchURL, map[string]string{"User-Agent": yhMd2UA})
	if err != nil {
		return nil, 0, false, err
	}
	if code != 200 {
		return nil, 0, false, fmt.Errorf("yh_md2 file_name http %d", code)
	}
	var vl map[string]interface{}
	if err := json.Unmarshal(body, &vl); err != nil {
		return nil, 0, false, fmt.Errorf("yh_md2 videolist parse: %v", err)
	}
	var items []interface{}
	if a := yhArr(vl["data"]); a != nil {
		items = a
	} else if a := yhArr(yhMap(vl["data"])["data"]); a != nil {
		items = a
	}
	total, _ := g5MapInt(yhMap(vl["data"]), "total")
	last, _ := g5MapInt(yhMap(vl["data"]), "last_page")
	return items, total, page < last, nil
}

func yhMd2Categories(slot string) []interface{} {
	return []interface{}{
		categoryObj("latest", "最新"),
		categoryObj("21", "91TV"),
	}
}

func yhMd2Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	items, total, hasMore, err := yhMd2ListItems(category, page)
	if err != nil {
		return nil, 0, false, err
	}
	out := []interface{}{}
	for _, it := range items {
		if c, ok := yhMd2Card(slot, yhMap(it)); ok {
			out = append(out, c)
		}
	}
	return out, total, hasMore, nil
}

func yhMd2Detail(slot, rawID string) (map[string]interface{}, error) {
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

// yhMd2SignURL 复制 generateVideo_url 的 wsSecret/wsTime 签名。
func yhMd2SignURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	p := u.Path
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	wsTime := strconv.FormatInt(time.Now().Unix()+7200, 10)
	wsSecret := yhMd25Hex(yhMd2WsKey + p + wsTime)
	origin := u.Scheme + "://" + u.Host
	return origin + p + "?wsSecret=" + wsSecret + "&wsTime=" + wsTime, nil
}

func yhMd2Resolve(slot, rawID string) (map[string]interface{}, error) {
	_, u, found := strings.Cut(rawID, "$")
	if !found || u == "" {
		return nil, fmt.Errorf("yh_md2 resolve: rawID 内无 video_url")
	}
	signed, err := yhMd2SignURL(u)
	if err != nil {
		return nil, err
	}
	format, protocol := "mp4", ""
	if strings.Contains(signed, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(signed, slot, rawID, yhMd2UA, yhMd2Base+"/", format, protocol), nil
}

func yhMd2Search(slot, kw string) ([]interface{}, error) {
	obj, err := yhMd2Call("/api/video/list", map[string]string{"list_row": "60", "page": "1", "keyword": kw})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	var items []interface{}
	if a := yhArr(data["data"]); a != nil {
		items = a
	} else if a := yhArr(data); a != nil {
		items = a
	}
	out := []interface{}{}
	for _, it := range items {
		if c, ok := yhMd2Card(slot, yhMap(it)); ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// yhMd2CoverFetch 复制 f4.d: 体 Base64 解码 -> DES/CBC 解密 -> 去首 22 字
// -> 再 Base64 解码; 解密失败时回源字节 (f4.d 的 T==null 分支)。
func yhMd2CoverFetch(u string) ([]byte, error) {
	b, code, _, err := httpGet(u, map[string]string{"User-Agent": yhMd2UA})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_md2 cover http %d", code)
	}
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xD8 {
		return b, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, fmt.Errorf("yh_md2 cover b64: %v", err)
	}
	block, err := des.NewCipher([]byte(yhMd2DESKey))
	if err != nil {
		return nil, err
	}
	if len(raw)%des.BlockSize != 0 {
		return b, nil
	}
	pt := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, []byte(yhMd2DESIV)).CryptBlocks(pt, raw)
	if len(pt) > 22 {
		if dec, err := base64.StdEncoding.DecodeString(string(pt[22:])); err == nil && len(dec) >= 2 && dec[0] == 0xFF && dec[1] == 0xD8 {
			return dec, nil
		}
	}
	return b, nil
}

func init() {
	registerSite("yh_md2", &siteImpl{
		Key:        "yh_md2",
		Name:       "麻豆2",
		DefaultCat: "latest",
		HomeURL:    yhMd2Base + "/",
		Categories: yhMd2Categories,
		Catalog:    yhMd2Catalog,
		Detail:     yhMd2Detail,
		Resolve:    yhMd2Resolve,
		Search:     yhMd2Search,
		Headers: func() map[string]string {
			return map[string]string{"User-Agent": yhMd2UA}
		},
		CoverFetch: yhMd2CoverFetch,
	}, "yh_md2", "md2", "麻豆2")
}
