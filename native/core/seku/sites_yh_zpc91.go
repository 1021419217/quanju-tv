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

// ---- zpc91 (yh_zpc91) — PWA 加密 API, 与撸先生同平台不同肤 ----
//
// 配方出欲火 APK jadx 实证 (ui/zpc91 + f6 ApiService/ZpcUtils/拦截器 +
// h6.b 默认基址):
//   base = https://bak.kqqagxd.cc (h6.b.f8283y0, 写死; 远程配置
//   config_remote.json 的 config 字典仅含 XG_APP, 无本站覆盖)
//   信封 (f6.f.a/b + f6.e 响应拦截器):
//     form: _ver=v2, timestamp=<秒>, data, sign
//     data = Base64( AES-128-CBC/PKCS7( JSON(biz), key, iv ) )
//     sign = MD5hex( SHA256hex( "_ver=v2&client=pwa&data="+data+
//            "&timestamp="+ts+"be51da32b19f92856cb82d5639b6b00a" ) )
//     响应 {"data":"<Base64>"} 同 key/iv 解密得业务 JSON
//   biz 公共 (f6.f.c): bundleId=com.pwa.soully, version=1.2.2,
//   language=zh, oauth_type=web, oauth_id=<7 串池随机>,
//   via=pwa. oauth 池 (f6.f.f7686c 解码后):
//   9acafe86c64f6559baa7b3458e31d8b7 / e419f1c4486090b8d45e3052ba77c085 /
//   d7239e5c23b428b126d866347bfb7951 / dc5ba43b45da7f78f2d977978c76d7f7 /
//   734747bbc4b7be4e346ac7efd998eb5b / 08537ae370cfd3e02ac84fb42817e700 /
//   44812067a60b810c3695c63966a27626.
//   端点 (f6.a): /api.php/api/mv/list_construct (biz id/page/limit=16),
//   /api.php/api/mv/getDetail (biz id), /api.php/api/mv/search
//   (biz word/type=1/page/limit=18), /api.php/api/mv/list_cates,
//   /api.php/api/mv/list_tags (biz id/limit=60),
//   /api/system/getInfo (启动时取 video_encrypt_m3u8, 当前 404/系统错误).
// 分类与 App 导航 1:1: HomeFragment 8 个 construct 页签
//   1 推荐 / 155 华语原创 / 69 家庭乱伦 / 156 国产精选 / 144 网黄女神 /
//   157 日韩欧美 / 158 动漫天堂 / 75 制服最爱; Type 页签 = list_cates
//   动态分类 + list_tags 标签 (标签走 mv/search word 检索).
// 播放 (PlayActivity + VideoDetail.getPlayUrl2):
//   detail.preview_url 经主机改写 hf-[^.]+. -> hf-long. 即播;
//   .m3u8 响应若非 #EXTM3U 开头, 为 hex 编码体: 前 16 字节 IV,
//   key = EVP_BytesToKey(MD5, video_encrypt_m3u8) (f6.d, 默认
//   e79465cfbbimgkcusimcuekd3b066aae, getInfo 下发可覆盖),
//   AES-CFB 解密 (yhZpc91DecryptM3U8 供播放层复用, Resolve 仍按
//   App 原路返回改写后 preview_url).
// 付费: Video/VideoDetail 含 coins/is_pay/isfree 字段; 目录过滤
//   is_pay=1 与 (coins>0 且 isfree=2) 的金币片, 详情同规则拒解析.
// 封面: 本族封面字节 AES-128-CBC 加密, 族共享 Glide 密钥
//   (yhZpc91CoverFetch; 密钥已在 melon51/utube 同族图床实测解密出
//   明文魔数, zpc91 自身图床待后端恢复后冒烟时验).
// 验真 (2026-10-24, 如实, 非"出口未验"可搪塞的情形):
//   主机存活 (浏览器出口根 404 = API 主机正常响应; shell 经代理可达
//   200); 但业务层对一切输入回同一 canned 错误
//   {"data":[],"status":0,"msg":"错误的请求","crypt":true}——错密钥、
//   明文 data、空 biz {}、全部 7 种信封/签名变体皆同拒, 证明该站
//   后端当前不解析业务请求 (服务侧故障, 非本站客户端配方错:
//   同平台 av91/utube/melon51 用同一信封结构当日即通).
//   本件按 jadx 逐行实现, 待该站后端恢复后冒烟补验.

const yhZpc91Slot = "yh_zpc91"
const yhZpc91Base = "https://bak.kqqagxd.cc"
const yhZpc91UA = "Mozilla/5.0 (Linux; Android 13; SM-A205GN) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"

var yhZpc91Key = []byte("cbaede385ffda681")
var yhZpc91IV = []byte("3bb661deb28f50e8")

const yhZpc91Secret = "be51da32b19f92856cb82d5639b6b00a"
const yhZpc91M3U8Key = "e79465cfbbimgkcusimcuekd3b066aae"

var yhZpc91OauthPool = []string{
	"9acafe86c64f6559baa7b3458e31d8b7",
	"e419f1c4486090b8d45e3052ba77c085",
	"d7239e5c23b428b126d866347bfb7951",
	"dc5ba43b45da7f78f2d977978c76d7f7",
	"734747bbc4b7be4e346ac7efd998eb5b",
	"08537ae370cfd3e02ac84fb42817e700",
	"44812067a60b810c3695c63966a27626",
}

var yhZpc91Constructs = []struct{ id, name string }{
	{"1", "推荐"}, {"155", "华语原创"}, {"69", "家庭乱伦"}, {"156", "国产精选"},
	{"144", "网黄女神"}, {"157", "日韩欧美"}, {"158", "动漫天堂"}, {"75", "制服最爱"},
}

func yhZpc91Headers() map[string]string {
	return map[string]string{
		"User-Agent":   yhZpc91UA,
		"Content-Type": "application/x-www-form-urlencoded",
		"Referer":      yhZpc91Base + "/",
	}
}

func yhZpc91Pad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhZpc91Enc(plain []byte) string {
	block, _ := aes.NewCipher(yhZpc91Key)
	out := make([]byte, len(yhZpc91Pad(plain)))
	cipher.NewCBCEncrypter(block, yhZpc91IV).CryptBlocks(out, yhZpc91Pad(plain))
	return base64.StdEncoding.EncodeToString(out)
}

func yhZpc91Dec(b64s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64s)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw)%16 != 0 {
		return nil, fmt.Errorf("yh_zpc91 bad cipher len")
	}
	block, _ := aes.NewCipher(yhZpc91Key)
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhZpc91IV).CryptBlocks(out, raw)
	n := int(out[len(out)-1])
	if n < 1 || n > 16 || n > len(out) {
		return nil, fmt.Errorf("yh_zpc91 bad pad")
	}
	return out[:len(out)-n], nil
}

func yhZpc91Sign(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", md5.Sum([]byte(hex.EncodeToString(sum[:]))))
}

func yhZpc91Call(path string, biz map[string]string) (map[string]interface{}, error) {
	b := map[string]string{
		"bundleId":   "com.pwa.soully",
		"version":    "1.2.2",
		"language":   "zh",
		"oauth_type": "web",
		"oauth_id":   yhZpc91OauthPool[time.Now().UnixNano()%int64(len(yhZpc91OauthPool))],
		"via":        "pwa",
	}
	for k, v := range biz {
		b[k] = v
	}
	jb, _ := json.Marshal(b)
	data := yhZpc91Enc(jb)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sign := yhZpc91Sign("_ver=v2&client=pwa&data=" + data + "&timestamp=" + ts + yhZpc91Secret)
	form := url.Values{
		"_ver":      {"v2"},
		"timestamp": {ts},
		"data":      {data},
		"sign":      {sign},
	}.Encode()
	var body []byte
	var code int
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err = httpDo("POST", yhZpc91Base+path, strings.NewReader(form), yhZpc91Headers())
		if err == nil && code == 200 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_zpc91 http %d", code)
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("yh_zpc91 outer parse: %v", err)
	}
	enc := g5StrVal(outer["data"])
	if enc == "" {
		return outer, nil
	}
	plain, err := yhZpc91Dec(enc)
	if err != nil {
		return nil, fmt.Errorf("yh_zpc91 decrypt: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("yh_zpc91 parse: %v", err)
	}
	return out, nil
}

// yhZpc91DecryptM3U8 implements the f6.d player interceptor: a .m3u8
// response that does not start with #EXTM3U is hex-encoded, first 16
// bytes IV, key = EVP_BytesToKey(MD5, video_encrypt_m3u8, 32+16).
func yhZpc91DecryptM3U8(hexBody, keyStr string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(hexBody))
	if err != nil || len(raw) <= 16 {
		return "", fmt.Errorf("yh_zpc91 bad m3u8 body")
	}
	iv, ct := raw[:16], raw[16:]
	// EVP_BytesToKey: D_i = MD5(D_{i-1} + password), concat to 48 bytes
	var derived []byte
	prev := []byte{}
	for len(derived) < 48 {
		h := md5.New()
		h.Write(prev)
		h.Write([]byte(keyStr))
		prev = h.Sum(nil)
		derived = append(derived, prev...)
	}
	block, err := aes.NewCipher(derived[:32])
	if err != nil {
		return "", err
	}
	out := make([]byte, len(ct))
	cipher.NewCFBDecrypter(block, iv).XORKeyStream(out, ct)
	return string(out), nil
}

var yhZpc91HostRe = regexp.MustCompile(`hf-[^.]+\.`)

func yhZpc91Paid(rec map[string]interface{}) bool {
	if p, ok := rec["is_pay"].(float64); ok && p == 1 {
		return true
	}
	if c, ok := rec["coins"].(float64); ok && c > 0 {
		if f, ok := rec["isfree"].(float64); ok && f == 2 {
			return true
		}
	}
	return false
}

func yhZpc91Card(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["title"])
	if id == "" || title == "" || yhZpc91Paid(rec) {
		return nil, false
	}
	cover := g5StrVal(rec["cover_horizontal"])
	if cover == "" {
		cover = g5StrVal(rec["cover_vertical"])
	}
	remark := ""
	if d, ok := rec["duration"].(float64); ok && d > 0 {
		remark = fmt.Sprintf("%d:%02d", int(d)/60, int(d)%60)
	}
	desc := g5StrVal(rec["tags"])
	if desc == "" {
		desc = strings.Join(g5StrList(rec["tag_list"]), " ")
	}
	return makeDrama(slot, id, title, cover, remark, "", desc), true
}

func yhZpc91Cards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhZpc91Card(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func yhZpc91Categories(slot string) []interface{} {
	out := []interface{}{}
	// 动态 list_cates 优先, 失败回退写死 8 页签 (与 HomeFragment 一致)
	cates := yhZpc91Constructs
	if obj, err := yhZpc91Call("/api.php/api/mv/list_cates", map[string]string{}); err == nil {
		if d := yhMap(obj["data"]); d != nil {
			if arr := yhArr(d["cates"]); len(arr) > 0 {
				dyn := []struct{ id, name string }{}
				for _, e := range arr {
					if m := yhMap(e); m != nil {
						if id, name := g5StrVal(m["id"]), g5StrVal(m["name"]); id != "" && name != "" {
							dyn = append(dyn, struct{ id, name string }{id, name})
						}
					}
				}
				if len(dyn) > 0 {
					cates = dyn
				}
			}
		}
	}
	for _, c := range cates {
		out = append(out, categoryObj("construct:"+c.id, c.name))
	}
	return out
}

func yhZpc91Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	pg := strconv.Itoa(page)
	if strings.HasPrefix(category, "tag:") {
		kw := strings.TrimPrefix(category, "tag:")
		obj, err := yhZpc91Call("/api.php/api/mv/search", map[string]string{
			"word": kw, "type": "1", "page": pg, "limit": "18",
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhZpc91Cards(slot, yhArr(obj["data"]))
		return items, len(items), len(items) >= 18, nil
	}
	id := strings.TrimPrefix(category, "construct:")
	if id == "" || id == category {
		id = "1"
	}
	obj, err := yhZpc91Call("/api.php/api/mv/list_construct", map[string]string{
		"id": id, "page": pg, "limit": "16",
	})
	if err != nil {
		return nil, 0, false, err
	}
	data := yhMap(obj["data"])
	items := yhZpc91Cards(slot, yhArr(data["list"]))
	return items, len(items), len(items) >= 16, nil
}

func yhZpc91DetailRaw(id string) (map[string]interface{}, error) {
	return yhZpc91Call("/api.php/api/mv/getDetail", map[string]string{"id": id})
}

func yhZpc91Detail(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhZpc91Slot+":")
	obj, err := yhZpc91DetailRaw(vid)
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	det := yhMap(data["detail"])
	if det == nil {
		det = data
	}
	if det != nil && yhZpc91Paid(det) {
		d := makeDrama(slot, vid, g5StrVal(det["title"]), g5StrVal(det["cover_horizontal"]), "金币", "", g5StrVal(det["desc"]))
		return d, nil
	}
	title, cover, desc := vid, "", ""
	if det != nil {
		title = g5StrVal(det["title"])
		cover = g5StrVal(det["cover_horizontal"])
		if cover == "" {
			cover = g5StrVal(det["cover_vertical"])
		}
		desc = g5StrVal(det["desc"])
		if a := g5StrVal(det["actors"]); a != "" {
			desc = strings.TrimSpace(a + " " + desc)
		}
	}
	d := makeDrama(slot, vid, title, cover, "", "", desc)
	if det == nil || !yhZpc91Paid(det) {
		g5SetChapters(d, slot, vid, []string{vid}, []string{"正片"})
	}
	return d, nil
}

func yhZpc91PlayURL(obj map[string]interface{}) string {
	data := yhMap(obj["data"])
	det := yhMap(data["detail"])
	if det == nil {
		det = data
	}
	if det == nil {
		return ""
	}
	u := g5StrVal(det["preview_url"])
	if u == "" {
		u = g5StrVal(det["source_240"])
	}
	if u != "" {
		return yhZpc91HostRe.ReplaceAllString(u, "hf-long.")
	}
	return ""
}

func yhZpc91Resolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhZpc91Slot+":")
	obj, err := yhZpc91DetailRaw(vid)
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	det := yhMap(data["detail"])
	if det != nil && yhZpc91Paid(det) {
		return nil, fmt.Errorf("yh_zpc91: paid content is not resolved")
	}
	u := yhZpc91PlayURL(obj)
	if u == "" {
		return nil, fmt.Errorf("yh_zpc91: no play url for %s", vid)
	}
	format := "m3u8"
	if strings.HasSuffix(strings.Split(u, "?")[0], ".mp4") {
		format = "mp4"
	}
	return playResult(u, slot, rawID, yhZpc91UA, yhZpc91Base+"/", format, "hls"), nil
}

func yhZpc91Search(slot, kw string) ([]interface{}, error) {
	obj, err := yhZpc91Call("/api.php/api/mv/search", map[string]string{
		"word": kw, "type": "1", "page": "1", "limit": "18",
	})
	if err != nil {
		return nil, err
	}
	return yhZpc91Cards(slot, yhArr(obj["data"])), nil
}

var yhZpc91ImgKey = []byte("f5d965df75336270")
var yhZpc91ImgIV = []byte("97b60394abc2fbe1")

func yhZpc91ImgMagic(b []byte) bool {
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

// yhZpc91CoverFetch: family-shared cover decryption. Cover bytes on
// this family's pic hosts are AES-128-CBC encrypted at rest with the
// same key/IV the app's shared Glide fetcher uses (verified 2026-10-24:
// melon51 pic.sugfew.cn -> JPEG FF D8 FF E0, utube picx.drleao.cn ->
// PNG 89 50 after decrypt; plaintext covers pass through untouched,
// undecodable bodies are returned raw, as the app does).
func yhZpc91CoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpDo("GET", u, nil, yhZpc91Headers())
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("yh_zpc91 cover http %d", code)
	}
	if yhZpc91ImgMagic(body) || len(body)%aes.BlockSize != 0 {
		return body, nil
	}
	block, err := aes.NewCipher(yhZpc91ImgKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, yhZpc91ImgIV).CryptBlocks(out, body)
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
	if yhZpc91ImgMagic(out) {
		return out, nil
	}
	return body, nil
}

func init() {
	registerSite(yhZpc91Slot, &siteImpl{
		Key:        yhZpc91Slot,
		Name:       "zpc91",
		DefaultCat: "construct:1",
		HomeURL:    yhZpc91Base + "/",
		Headers:    yhZpc91Headers,
		CoverFetch: yhZpc91CoverFetch,
		Categories: yhZpc91Categories,
		Catalog:    yhZpc91Catalog,
		Detail:     yhZpc91Detail,
		Resolve:    yhZpc91Resolve,
		Search:     yhZpc91Search,
	})
}
