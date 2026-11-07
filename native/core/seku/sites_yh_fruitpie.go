package seku

// ---- 水果派 fruitPie (yh_fruitpie, type 51/62) — RSA+AES 信封 JSON API ----
//
// Recipe: yuhuo fruitPie module, jadx deep-read 2026-10-24 (s1.a ApiService
// + s1.d MyInterceptor + s1.f SgpUtils + FruitLaunchActivity domain check).
// Base: https://api.u7kt9ez.com (h6.b f8277v0; launcher also tries
// api.9mjcuuq.com / api.u6rb3g.com / api.s8nhdy6.com via
// POST /v1_2/domain/check, first <=300 wins — same rotation here).
//
// Request (multipart form, s1.d.b):
//   encrypt_key  = Base64(RSA/ECB/PKCS1v15( {"key":"MTIzNDU2Nzg5MGFiY2RlZg==",
//                  "iv":"YWJjZGVmMDEyMzQ1Njc4OQ=="} , embedded pubkey ))
//   encrypt_data = Base64(AES-128-CBC/PKCS5( paramsJSON,
//                  key "1234567890abcdef", iv "abcdef0123456789" ))
// Header on every v2_0 call: "Hm: 008-api" (carried in body as hm field
// plus the header itself, as the App does).
// Response: {encrypt_key, encrypt_data}; RSA-decrypt encrypt_key with the
// embedded private key -> per-response {key, iv} (base64) -> AES-CBC decrypt.
// (Server also accepts a request without encrypt_key; the response key is
// fresh every call, so the private key is NOT optional on the read side.)
//
// Endpoints (s1.a, all POST):
//   /v2_0/homeExplanationPage  {ltype:1|2, page, length:10, last_page:0,
//     totalPage:77, userToken:"", hm:"008-api", device_id:""} -> VideoList
//     {code, data{list[{library_id,title,subtitle,img_url,show_time,
//     movie_time,date_number}], totalPage, hasNextPage, banner[] (ads)}}
//   /v2_0/find                 {userToken, hm, device_id} -> DiscoverBean
//     {data{recommend[], newHot{data[]}, hotTag[]}}
//   /v2_0/anchorsList          {page, length:10, ...} -> AuthorList
//     (ACCOUNT-GATED: server returns 41001 缺少token with userToken "",
//     42011 用户不存在 with a dummy token; a real user token is needed)
//   /v2_0/columnistList        same shape -> ColumnList
//     {data{list[{columnist_id, columnist_name, columnist_img,
//     description, library_count}], totalPage, hasNextPage}}
//   /v2_0/{anchorsDetail|columnistDetail}  {anchors_id|columnist_id, page,
//     length:10} -> works list + author header
//   /v2_0/articleSearch        {conditions:<kw>, field:"0", target:<type>,
//     sort:"1", ...} -> SearchResult {data[{library_id, title, img_url,
//     love_number, movie_time}]}
//   /v2_0/libraryDetail        {library_id, ltype} -> IdDetail
//     {data{title, subtitle, film_id, content(HTML), detail[]}}
//   /v2_0/filmInfo             {film_id} -> FilmInfo
//     {data[{id, title, actor, introduction, img_url, score, publish_at,
//     movie_time, tags[], url, download_url, encryption_url}]}
// Play: getPlayUrl = url || download_url || encryption_url. In practice
// only encryption_url is filled: an HLS URL whose playlist AND segments
// are AES-128-ECB encrypted (s1.c FruitPlayerInterceptor, key
// "saIZXc4yMvq0Iz56"; a .m3u8 URL decrypts iff body does not start with
// #EXTM3U, .ts segments are decrypted the same way). The Go core has no
// video local-proxy, so Resolve returns the encryption_url verbatim and
// the shell must apply the s1.c ECB transform before playback.
//
// Categories vs App nav (FruitActivity tabs + HomeFragment sub-tabs):
//   视频 (home ltype=1) / 专栏 (home ltype=2) / 发现 (find) /
//   专栏作者 (columnistList, folder drill-down) / 女优 (anchorsList,
//   account-gated, see above).
//
// Live verification 2026-10-24 (this sandbox, egress proxy):
//   homeExplanationPage ltype=1: code=1, 10 items/page, totalPage=189,
//     first item library_id 2173; cover fetched 200, JPEG magic ffd8ffe1.
//   filmInfo(384959) (film_id from libraryDetail(2173)): code=1, full
//     fields; encryption_url fetched 200 and AES-ECB-decrypts to a valid
//     #EXTM3U playlist (TARGETDURATION 5, VOD) — play leg byte-proven.
//   libraryDetail(2173): code=1, film_id 384959, article HTML with iframe.
//   find: code=1, recommend non-empty. columnistList: code=1, 2 pages.
//   columnistDetail(2): code=1, total 3539, currentPage 1/354.
//   articleSearch "闺蜜": code=1, true results.
//   anchorsList: 41001 缺少token (empty) / 42011 用户不存在 (dummy token)
//     — 女优 leg is account-gated, NOT a code bug; it stays wired and
//     surfaces the server error verbatim.
//   SGP side endpoints (bak.sgp1.cc narrate list_post/post_detail/
//   original_detail, s1.f signed form) are NOT wired: the App uses them
//   for the H5 解说 player, not for a direct stream; filmInfo covers
//   the stream leg.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
)

const yhFPKeyJSON = `{"key":"MTIzNDU2Nzg5MGFiY2RlZg==","iv":"YWJjZGVmMDEyMzQ1Njc4OQ=="}`
const yhFPAESKey = "1234567890abcdef"
const yhFPAESIV = "abcdef0123456789"
const yhFPPubB64 = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA2WxnqdfalxP45kvAk+pzkabw4T9zk5OvbCgPBpb2KEqMV3ZF9xyPeMaxPZnUXzicai1j/1I3vriSSCfvZ9pNxrAh/P6N3NidzlvWbmvjL3gBzIeQXeWbBMxgUzVYGAE6vxqewmZatxUF0Venh3k89hjdU/pP0Vsqxp3mXOKpR5T9UrOQb9eFhndOJ4mVYxsHQMEAs+H8mn3IA9w++HdE/rZRDdVGsCvUZsgecI0tV4qut1la6oPt6q/27eXUqV/IBdTOaowvU05YtFIRpb66078HGkkmt/qJLNmveIH5vHRpeEqA3twE9L7OgIZJMG2uruoxprH2ysM5YsZIYkCrWwIDAQAB"
const yhFPPrivB64 = "MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDY8WdvLG4ObJyq3Gme9HD9uVSGjWtf5c66C/eiwNJe2O0mratg0t2BuGzI6hIYrHMe/B8ffbkYH6J58Ru8IqIar5eVRSTh5A0rNOzSf/S0PvOlgmPVac8vJijmpTBe1UdJm9wRLfonUZqGjFXcRNhUFr+Fc2vZ1/wjjFz2P4iPe2wim1g5iTNdfJoUbBLg5QAF35JHGq9o3HFKpNk/9neOKo7RXETI2nklfrPy/EOEUU8LD0dyta/idBZz5QXisptQmNZUA1l2VWyt/Q72zdU+KU/yQ2YvAjNmR4nd6NdXPhsKDn6n+JaAzTlWbeYfE0JWh2JcJM8GfD2dZ0HdZXaDAgMBAAECggEAfzIi/BrnuEtUHBW5U7psWUcFWRKnpwVdjNWhbupxLa0wdqISvmVD/F++YJpk29SYds1KDjOvh4Zcw/Rq14bS1qSnUpzEwDDuy6tvQwhE2D2kqyDlGea81B8ikgC+eIvCDVV11RjI966v1D2JxekTkVH5HuyKiqJJMiqFAthDYCPf5XC7eaAf6dpW9sDTisrb4woaN+4nA+NBgww7sFLs0/0OM4fSrJK5ObxUkSxc+fj9xWbHeaVMiyqR/OmOMl3uIf5ffLWDdiqYlxkH18WFDrqr2dkKQxjFvJ0y4kcTf8P8K0xfSXt5SdDiRHpQEe3hfp03zfxoRsRNzRIa3InyuQKBgQDvFv0wPoBxkUupZ8PiHXgPNiqPy5iG77d+Jn9/xJ/gv1vfQoW0J0TYUYL8n1m6c9KwzpUrlJhQuAX+Ad9e/XLtgmG0IcAQ7rLARXsRZSMGRRPYHX9X+vuOfoKLgP/2ZIj0B1rrbZH802rjkqUW8x8kT71SX/OQuBbOcTYs7VAGNQKBgQDoSWuHmkXwtQ0cgVputlaLRzPiqAKkOBQGWL8msqNdYQ9BfFfmPwM+sFsonA4NE4T+5AR/5JysnLpsvZpC2QriIBQlgBQt9Lel6hzSzFVRkPkz3zTZFWRwArL2R0Hjj44897tOYW4oK1PgkLokTtgzBdvGWFS5psSmA7EqX3dA1wKBgAUSKcCSMss117L6Hm93TIqWDYULPmruPqgQTPf6xaFQrbQohwlxI8aqw+pzvsVzm25gk01fU3Oy0321sHUlTwC2mTfm11oElgn570hJKtsBMBCPX5oQGrHFASPQUp15OGZkmYsQwd3o5gV5oMLB2VE917gdjN7u6JzsTcfaKK5ZAoGBAKsxURqDTIB2Ls+PVcK6h1XmiArHoAq/eaKjzV0L0vKDEedFaNK8JfwRpV1shb6FYEMqcVaAeieM6qgxpHsX0rmMwsxQdI7aCG8y90M0bLzMh5z/J8fFyFv04Ug4hAreRgoQ2e6OR807AXpDr6wKn7Qa4Tpw5yBcFBn2Jp//87fJAoGBAOh0SzrlxvzrZauwVjqdveTajwPGQlmYjNPW9XIDFcO1YhR3HCnpl47RYrXT0lvuz0U+OcgeWEoDW+okw1f2uX2xSdwhiNiiOFtOQiGmrEumH2wgHg2rA8u3xOza+OxPpB7bDSFe4DsqlnRVXzDm3DtD0lwzJVe52++V5NhE+/E8"
const yhFPPlayKey = "saIZXc4yMvq0Iz56"
const yhFPUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"

var yhFPBases = []string{
	"https://api.u7kt9ez.com",
	"https://api.9mjcuuq.com",
	"https://api.u6rb3g.com",
	"https://api.s8nhdy6.com",
}

var yhFPBaseMu sync.Mutex
var yhFPBaseIdx int

func yhFPBase() string {
	yhFPBaseMu.Lock()
	defer yhFPBaseMu.Unlock()
	return yhFPBases[yhFPBaseIdx%len(yhFPBases)]
}

func yhFPHeaders() map[string]string {
	return map[string]string{
		"User-Agent": yhFPUA,
		"Hm":         "008-api",
		"Accept":     "application/json, text/plain, */*",
	}
}

func yhFPPad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhFPUnpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n >= 1 && n <= 16 && n <= len(b) {
		return b[:len(b)-n]
	}
	return b
}

func yhFPAESEnc(key, iv, plain []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	ct := make([]byte, len(yhFPPad(plain)))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, yhFPPad(plain))
	return base64.StdEncoding.EncodeToString(ct), nil
}

func yhFPAESDec(key, iv []byte, b64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(raw)%16 != 0 {
		return nil, fmt.Errorf("yh_fruitpie: bad aes length")
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, raw)
	return yhFPUnpad(out), nil
}

func yhFPRSAEncKey() (string, error) {
	der, err := base64.StdEncoding.DecodeString(yhFPPubB64)
	if err != nil {
		return "", err
	}
	pubAny, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return "", err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("yh_fruitpie: pubkey not rsa")
	}
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(yhFPKeyJSON))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

func yhFPRSADecKey(b64 string) (key, iv []byte, err error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, err
	}
	der, err := base64.StdEncoding.DecodeString(yhFPPrivB64)
	if err != nil {
		return nil, nil, err
	}
	priv, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, nil, err
	}
	rk, ok := priv.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("yh_fruitpie: privkey not rsa")
	}
	plain, err := rsa.DecryptPKCS1v15(rand.Reader, rk, raw)
	if err != nil {
		return nil, nil, err
	}
	var kj struct {
		Key string `json:"key"`
		IV  string `json:"iv"`
	}
	if err := json.Unmarshal(plain, &kj); err != nil {
		return nil, nil, err
	}
	key, err = base64.StdEncoding.DecodeString(kj.Key)
	if err != nil {
		return nil, nil, err
	}
	iv, err = base64.StdEncoding.DecodeString(kj.IV)
	if err != nil {
		return nil, nil, err
	}
	return key, iv, nil
}

// yhFPCall posts one v2_0 endpoint on the sticky base, rotating bases on
// transport/HTTP failure (the launcher domain-check rotation, App-side).
func yhFPCall(path string, params map[string]string) (map[string]interface{}, error) {
	var lastErr error
	for attempt := 0; attempt < len(yhFPBases); attempt++ {
		yhFPBaseMu.Lock()
		idx := (yhFPBaseIdx + attempt) % len(yhFPBases)
		yhFPBaseMu.Unlock()
		obj, err := yhFPCallOnce(yhFPBases[idx], path, params)
		if err == nil && obj != nil {
			yhFPBaseMu.Lock()
			yhFPBaseIdx = idx
			yhFPBaseMu.Unlock()
			return obj, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("yh_fruitpie: %s returned no data", path)
}

func yhFPCallOnce(base, path string, params map[string]string) (map[string]interface{}, error) {
	full := map[string]string{
		"userToken": "", "hm": "008-api", "device_id": "",
	}
	for k, v := range params {
		full[k] = v
	}
	jb, _ := json.Marshal(full)
	encData, err := yhFPAESEnc([]byte(yhFPAESKey), []byte(yhFPAESIV), jb)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if ek, err := yhFPRSAEncKey(); err == nil {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="encrypt_key"`)
		fw, _ := w.CreatePart(h)
		_, _ = fw.Write([]byte(ek))
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="encrypt_data"`)
	fw, _ := w.CreatePart(h)
	_, _ = fw.Write([]byte(encData))
	_ = w.Close()
	hdrs := yhFPHeaders()
	hdrs["Content-Type"] = w.FormDataContentType()
	body, code, _, err := httpDo("POST", base+path, &buf, hdrs)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_fruitpie http %d", code)
	}
	var env map[string]interface{}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("yh_fruitpie parse: %v", err)
	}
	// Response envelope: RSA-wrapped per-response key/iv.
	ek := g5StrVal(env["encrypt_key"])
	ed := g5StrVal(env["encrypt_data"])
	if ek != "" && ed != "" {
		key, iv, err := yhFPRSADecKey(ek)
		if err != nil {
			return nil, err
		}
		plain, err := yhFPAESDec(key, iv, ed)
		if err != nil {
			return nil, err
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(plain, &obj); err != nil {
			return nil, fmt.Errorf("yh_fruitpie plain parse: %v", err)
		}
		return obj, nil
	}
	// Fallback branch from s1.d: body carries "timestamp" and the payload
	// is SGP-style AES with the fixed s1.f key/iv.
	if ed != "" {
		plain, err := yhFPAESDec([]byte("tJqkoiJXvD7UCU3i"), []byte("h79lkicjeqwko5nd"), ed)
		if err != nil {
			return nil, err
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(plain, &obj); err != nil {
			return nil, err
		}
		return obj, nil
	}
	return env, nil
}

func yhFPCheckCode(obj map[string]interface{}) error {
	if obj == nil {
		return fmt.Errorf("yh_fruitpie: empty response")
	}
	if c, ok := obj["code"]; ok {
		switch t := c.(type) {
		case float64:
			if t != 1 {
				return fmt.Errorf("yh_fruitpie: server code %v (%v)", t, obj["msg"])
			}
		}
	}
	return nil
}

func yhFPListCards(slot string, list []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range list {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		id := g5StrVal(rec["library_id"])
		title := g5StrVal(rec["title"])
		if id == "" || title == "" || seen[id] {
			continue
		}
		seen[id] = true
		remark := g5StrVal(rec["subtitle"])
		if remark == "" {
			remark = g5StrVal(rec["show_time"])
		}
		if mt := g5StrVal(rec["movie_time"]); mt != "" {
			remark = strings.TrimSpace(remark + " " + mt)
		}
		out = append(out, makeDrama(slot, id, title, g5StrVal(rec["img_url"]), remark, "", ""))
	}
	return out
}

func yhFPCategories(slot string) []interface{} {
	return []interface{}{
		categoryObj("video", "视频"),
		categoryObj("article", "专栏"),
		categoryObj("discover", "发现"),
		categoryObj("columnist", "专栏作者"),
		categoryObj("artist", "女优"),
	}
}

func yhFPCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "" || category == "video" || category == "article":
		ltype := "1"
		if category == "article" {
			ltype = "2"
		}
		obj, err := yhFPCall("/v2_0/homeExplanationPage", map[string]string{
			"ltype": ltype, "page": strconv.Itoa(page), "length": "10",
			"last_page": "0", "totalPage": "77",
		})
		if err != nil {
			return nil, 0, false, err
		}
		if err := yhFPCheckCode(obj); err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		if data == nil {
			return []interface{}{}, 0, false, nil
		}
		total, _ := g5MapInt(data, "totalPage")
		has, _ := g5MapInt(data, "hasNextPage")
		return yhFPListCards(slot, yhArr(data["list"])), total, has == 1, nil
	case category == "discover":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhFPCall("/v2_0/find", map[string]string{})
		if err != nil {
			return nil, 0, false, err
		}
		if err := yhFPCheckCode(obj); err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		out := []interface{}{}
		if data != nil {
			out = append(out, yhFPListCards(slot, yhArr(data["recommend"]))...)
			if nh := yhMap(data["newHot"]); nh != nil {
				out = append(out, yhFPListCards(slot, yhArr(nh["data"]))...)
			}
		}
		return out, len(out), false, nil
	case category == "columnist":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhFPCall("/v2_0/columnistList", map[string]string{
			"page": "1", "length": "10",
		})
		if err != nil {
			return nil, 0, false, err
		}
		if err := yhFPCheckCode(obj); err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		out := []interface{}{}
		seen := map[string]bool{}
		if data != nil {
			for _, e := range yhArr(data["list"]) {
				rec := yhMap(e)
				if rec == nil {
					continue
				}
				id := g5StrVal(rec["columnist_id"])
				name := g5StrVal(rec["columnist_name"])
				if id == "" || name == "" || seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, makeDrama(slot, "dir:col:"+id, name,
					g5StrVal(rec["columnist_img"]),
					g5StrVal(rec["library_count"])+"部作品", "专栏作者",
					g5StrVal(rec["description"])))
			}
		}
		return out, len(out), false, nil
	case category == "artist":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhFPCall("/v2_0/anchorsList", map[string]string{
			"page": "1", "length": "10",
		})
		if err != nil {
			return nil, 0, false, err
		}
		if err := yhFPCheckCode(obj); err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		out := []interface{}{}
		seen := map[string]bool{}
		if data != nil {
			for _, e := range yhArr(data["list"]) {
				rec := yhMap(e)
				if rec == nil {
					continue
				}
				id := g5StrVal(rec["id"])
				name := g5StrVal(rec["name"])
				if name == "" {
					name = g5StrVal(rec["anchor_name"])
				}
				if id == "" || name == "" || seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, makeDrama(slot, "dir:artist:"+id, name,
					g5StrVal(rec["img"]), g5StrVal(rec["desc"]), "女优", ""))
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "dir:col:"):
		id := strings.TrimPrefix(category, "dir:col:")
		obj, err := yhFPCall("/v2_0/columnistDetail", map[string]string{
			"columnist_id": id, "page": strconv.Itoa(page), "length": "10",
		})
		if err != nil {
			return nil, 0, false, err
		}
		if err := yhFPCheckCode(obj); err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		if data == nil {
			return []interface{}{}, 0, false, nil
		}
		total, _ := g5MapInt(data, "totalPage")
		has, _ := g5MapInt(data, "hasNextPage")
		return yhFPListCards(slot, yhArr(data["list"])), total, has == 1, nil
	case strings.HasPrefix(category, "dir:artist:"):
		id := strings.TrimPrefix(category, "dir:artist:")
		obj, err := yhFPCall("/v2_0/anchorsDetail", map[string]string{
			"anchors_id": id, "page": strconv.Itoa(page), "length": "10",
		})
		if err != nil {
			return nil, 0, false, err
		}
		if err := yhFPCheckCode(obj); err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		if data == nil {
			return []interface{}{}, 0, false, nil
		}
		total, _ := g5MapInt(data, "totalPage")
		has, _ := g5MapInt(data, "hasNextPage")
		return yhFPListCards(slot, yhArr(data["list"])), total, has == 1, nil
	}
	return yhFPCatalog(slot, "video", page)
}

// yhFPDetailData returns libraryDetail data + filmInfo data (film may be nil).
func yhFPDetailData(libraryID string) (map[string]interface{}, map[string]interface{}, error) {
	ld, err := yhFPCall("/v2_0/libraryDetail", map[string]string{
		"library_id": libraryID, "ltype": "1",
	})
	if err != nil {
		return nil, nil, err
	}
	if err := yhFPCheckCode(ld); err != nil {
		return nil, nil, err
	}
	data := yhMap(ld["data"])
	if data == nil {
		return nil, nil, fmt.Errorf("yh_fruitpie: empty libraryDetail for %s", libraryID)
	}
	var film map[string]interface{}
	if fid := g5StrVal(data["film_id"]); fid != "" {
		if fi, err := yhFPCall("/v2_0/filmInfo", map[string]string{"film_id": fid}); err == nil {
			if err := yhFPCheckCode(fi); err == nil {
				if arr := yhArr(fi["data"]); len(arr) > 0 {
					film = yhMap(arr[0])
				}
			}
		}
	}
	return data, film, nil
}

func yhFPStripHTML(s string) string {
	for {
		a := strings.Index(s, "<")
		if a < 0 {
			break
		}
		b := strings.Index(s[a:], ">")
		if b < 0 {
			break
		}
		s = s[:a] + " " + s[a+b+1:]
	}
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func yhFPDetail(slot, rawID string) (map[string]interface{}, error) {
	libraryID := rawID
	if i := strings.Index(rawID, "@@"); i >= 0 {
		libraryID = rawID[:i]
	}
	data, film, err := yhFPDetailData(libraryID)
	if err != nil {
		return nil, err
	}
	title := g5StrVal(data["title"])
	cover := ""
	desc := yhFPStripHTML(g5StrVal(data["content"]))
	if film != nil {
		if t := g5StrVal(film["title"]); t != "" {
			// Keep the 解说 title as the card title; film title goes to desc.
			desc = strings.TrimSpace(desc + "\n原片：" + t + "\n演员：" +
				g5StrVal(film["actor"]) + "\n" + g5StrVal(film["introduction"]))
		}
		cover = g5StrVal(film["img_url"])
	}
	d := makeDrama(slot, rawID, title, cover, g5StrVal(data["subtitle"]), "", desc)
	ids := []string{}
	names := []string{}
	if film != nil {
		if fid := g5StrVal(film["id"]); fid != "" {
			ids = append(ids, "film:"+fid)
			names = append(names, "原片")
		}
	}
	// Article parts: detail[] entries carry their own URLs.
	if parts := yhArr(data["detail"]); len(parts) > 0 {
		for i, e := range parts {
			pm := yhMap(e)
			if pm == nil {
				continue
			}
			ids = append(ids, fmt.Sprintf("part:%d@%s", i, libraryID))
			names = append(names, fmt.Sprintf("第%d部分", i+1))
		}
	}
	if len(ids) > 0 {
		g5SetChapters(d, slot, rawID, ids, names)
	}
	return d, nil
}

func yhFPPlayURL(libraryID string) (string, error) {
	data, film, err := yhFPDetailData(libraryID)
	if err != nil {
		return "", err
	}
	if film != nil {
		for _, k := range []string{"url", "download_url", "encryption_url"} {
			if u := g5StrVal(film[k]); u != "" {
				return u, nil
			}
		}
	}
	// Fallback: article detail parts.
	if parts := yhArr(data["detail"]); len(parts) > 0 {
		if pm := yhMap(parts[0]); pm != nil {
			for _, k := range []string{"url", "encryption_url"} {
				if u := g5StrVal(pm[k]); u != "" {
					return u, nil
				}
			}
		}
	}
	return "", fmt.Errorf("yh_fruitpie: no play url for %s", libraryID)
}

func yhFPResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "film:") {
		fid := strings.TrimPrefix(rawID, "film:")
		if fi, err := yhFPCall("/v2_0/filmInfo", map[string]string{"film_id": fid}); err == nil {
			if err := yhFPCheckCode(fi); err == nil {
				if arr := yhArr(fi["data"]); len(arr) > 0 {
					if fm := yhMap(arr[0]); fm != nil {
						for _, k := range []string{"url", "download_url", "encryption_url"} {
							if u := g5StrVal(fm[k]); u != "" {
								return yhFPPlayResult(slot, rawID, u), nil
							}
						}
					}
				}
			}
		}
		return nil, fmt.Errorf("yh_fruitpie: no play url for film %s", fid)
	}
	if strings.HasPrefix(rawID, "part:") {
		rest := strings.TrimPrefix(rawID, "part:")
		parts := strings.SplitN(rest, "@", 2)
		if len(parts) == 2 {
			if idx, err := strconv.Atoi(parts[0]); err == nil {
				if data, _, err := yhFPDetailData(parts[1]); err == nil {
					if arr := yhArr(data["detail"]); idx < len(arr) {
						if pm := yhMap(arr[idx]); pm != nil {
							for _, k := range []string{"url", "encryption_url"} {
								if u := g5StrVal(pm[k]); u != "" {
									return yhFPPlayResult(slot, rawID, u), nil
								}
							}
						}
					}
				}
			}
		}
		return nil, fmt.Errorf("yh_fruitpie: no play url for part %s", rawID)
	}
	libraryID := rawID
	if i := strings.Index(rawID, "@@"); i >= 0 {
		libraryID = rawID[:i]
	}
	u, err := yhFPPlayURL(libraryID)
	if err != nil {
		return nil, err
	}
	return yhFPPlayResult(slot, rawID, u), nil
}

func yhFPPlayResult(slot, rawID, u string) map[string]interface{} {
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	// NOTE: encryption_url streams are AES-ECB encrypted (s1.c, key
	// yhFPPlayKey); the shell must apply that transform before playback.
	return playResult(u, slot, rawID, yhFPUA, yhFPBase()+"/", format, protocol)
}

func yhFPSearch(slot, kw string) ([]interface{}, error) {
	obj, err := yhFPCall("/v2_0/articleSearch", map[string]string{
		"conditions": kw, "field": "0", "target": "1", "sort": "1",
	})
	if err != nil {
		return nil, err
	}
	if err := yhFPCheckCode(obj); err != nil {
		return nil, err
	}
	return yhFPListCards(slot, yhArr(obj["data"])), nil
}

func init() {
	registerSite("yh_fruitpie", &siteImpl{
		Key:        "yh_fruitpie",
		Name:       "水果派",
		DefaultCat: "video",
		HomeURL:    yhFPBases[0] + "/",
		Categories: yhFPCategories,
		Catalog:    yhFPCatalog,
		Detail:     yhFPDetail,
		Resolve:    yhFPResolve,
		Search:     yhFPSearch,
		Headers:    yhFPHeaders,
	}, "yh_fruitpie", "fruitpie", "水果派")
}
