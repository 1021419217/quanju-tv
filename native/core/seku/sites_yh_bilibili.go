package seku

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- 逼哩族 bilibili + bili2tx （type 64/65）, one file, two slots ----
// Recipe: jadx c1/{a,b}.java (bili), z0/{a,d}.java (tx), the two
// Browse activities, fragments and beans, verified line by line
// 2026-10-24. Corrections to the original task brief, all
// jadx-proven (do not reintroduce the brief's versions):
//
//  1. The CloudFront "dc gateway" URLs are WEBVIEW BOOTSTRAP URLs
//     (BiliBrowseActivity.m / TxBrowseActivity.m), NOT the API or
//     play bases:
//       bili: https://d1y4i8e7mim92x.cloudfront.net/?dc=CMZXMGT
//             &UID=<"1"+abs(random long)%10^18>&isLight=true
//       tx:   https://d2llr0wukhemwq.cloudfront.net?dc=yjwxmgs
//             &UID=<same>&isLight=true   (no slash before "?")
//     The string &{"dc":"yjwxmgs","isLight":"true"} is the BILI
//     GUEST-LOGIN SIGN-MESSAGE SUFFIX, not a gateway variant.
//  2. ConfigBean$DomainDTO only overrides the IMAGE base (type
//     "IMAGE"); the API base is hard-coded in h6/b.java and is NOT
//     delivered by config. Config is still fetched (both Browse
//     activities call it unconditionally) because the image base
//     may rotate. Live 2026-10-24 config (see verification status)
//     lists NEWER API bases, but the app never switches to them —
//     they are recorded here as alternates only.
//  3. Response decryption (identical algorithm in both variants,
//     c1/b.f; tx also calls c1/b.f, not its own copy): outer JSON
//     field "data" is Base64; first 12 bytes = nonce, rest =
//     ciphertext. constant (32 bytes)
//     "vEukA&w15z4VAD3kAY#fkL#rBnU!WDhN"; c = constant+nonce
//     (44 bytes); mid = SHA256(c)[8:24]; b11 = SHA256(mid+c[0:22]);
//     b13 = SHA256(c[22:44]+mid); AES-256 key =
//     b11[0:8]+b13[8:24]+b11[24:32]; IV =
//     b13[0:4]+b11[12:20]+b13[28:32]; AES/CBC/PKCS5 decrypt.
//     (The Java hex-encode-then-hex-decode round trips are identity
//     operations and are omitted.)
//  4. Request signing (d(path) in both interceptors): path = group 1
//     of regex https?://[^/]+(/.*) over the full URL; ts =
//     (millis-1111)/1000 seconds; uuid = random UUID; signing
//     string = token + "&" + path + "&" + xUserAgent + "&" + ts +
//     "&" + uuid; sign = lowercase hex HmacSHA1 (bili key
//     "jR6dO6fT1yD9zY7u", tx key "15z4VAD3kAY#fk"); header
//     x-api-key = "timestamp=<ts>;sign=<sign>;nonce=<uuid>".
//     Header x-user-agent: bili (static init overwrites the random
//     construction) "DevType=Apple iPhone mobile;SysType=light;
//     Ver=2.1.2;BuildID=Mobile Safari 18.5"; tx (jadx-literal, embeds
//     the H0 base — live-check note in verification status)
//     "DevType=Apple iPhone mobile " + H0 + "home;SysType=h5_ios;
//     Ver=1.0.0;BuildID=Mobile Safari 16.6". Header authorization =
//     guest token (empty if none). Bili additionally sets
//     user-agent to the iPhone Safari 16.6 string; tx sets no
//     user-agent in its interceptor.
//  5. Guest login (manual OkHttp in the Browse activities, NOT in
//     either ApiService): POST <base>api/app/login/guest,
//     Content-Type application/json, x-api-key = d(path),
//     x-user-agent as above, user-agent = h6/j.b() (a randomised
//     desktop/mobile UA; this port substitutes the interceptor's
//     fixed iPhone Safari UA — see verification status).
//       bili body: devID = base36(abs(random long)) +
//         base36(millis); affCode (JSON STRING value)
//         {"dc":"CMZXMGT","isLight":"true"}; sign = HmacSHA1 hex
//         of devID + "&{\"dc\":\"yjwxmgs\",\"isLight\":\"true\"}"
//         with key "TianwSUDwMXxl12"+"wM5wK1jP3lK7pS2t". NOTE the
//         deliberate asymmetry: affCode uses CMZXMGT, the sign
//         suffix uses yjwxmgs — replicate exactly.
//       tx body: devID = base36((long)(random*9.223372036854776E18))
//         + base36(millis); affCode = {"SysType":"h5_ios"}; sign =
//         HmacSHA1 hex of devID + "&{\"SysType\":\"h5_ios\"}" with key
//         "RcahRMTI7wKAYfkLrMomo"+"ASDhwihcQDHKsFWVwd".
//     Response: outer data decrypted with c1/b.f into
//     LoginInfo{token}; empty/"null" plaintext (with outer code
//     200) means "fall back to the dc WebView" in the app. Token
//     cache: SharedPreferences CookiePrefs, 3 days (259200000 ms);
//     bili keys cookie_bi/cookie_time_bi, tx cookie_tx/
//     cookie_time_tx. This port caches per variant in memory with
//     fetch time and refreshes on expiry or on an auth-empty
//     content response.
//  6. Content endpoints (all POST JSON; id values are integers):
//     video list {id,pageNum,pageSize 10 bili / 30 tx} ->
//     media/home; HIDDEN SENTINEL: id == 27333 calls
//     media/topic/details instead (VideoTypeFragment comparison
//     only; 27333 appears in NO tab list, so it is implemented as
//     a branch, not a category). comic list {id,pageNum,pageSize
//     12} -> bili comics/tabcategory/details, tx comics/home.
//     anime list {id,pageNum,pageSize 10/30} -> media/home. live
//     list {id,pageNum,pageSize 12} -> live/home. novel list
//     {id,pageNum,pageSize 12/30, sort:1} -> bili novel/home, tx
//     novel/topic/details. short video: bili {contentType:1,
//     pageNum,pageSize:10} -> media/recommend; tx {pageNum,
//     pageSize:30} -> media/short/recommend (tx exposes no short
//     tab, so the tx slot does not list a short category).
//     video search bili {content,pageNum,pageSize:10,mediaType
//     1/4/12} -> search/details; tx {searchWord,sort:1,pageNum,
//     pageSize:30,contentType 1/4/3/6} -> video via
//     media/search/detail, comic/cVideo/novel via
//     acg/search/detail. Tx search field mapping (from
//     SearchTypeFragment callbacks a/b/c/d, verified 2026-10-24):
//     video -> searchMediaList, comic -> comicsList, cVideo ->
//     cartoonList, novel -> novelList. Bili search: video/comic
//     -> mediaList, novel -> novelList.
//  7. Play URL — video (VideoListDTO.getVideoUrl_S): videoUrl if it
//     starts with "http", else <base>api/app/media/h5/m3u8/
//     <videoUrl>, then append "?token=<guest token>". Audio novel
//     chapter (NovelChapter.getFieldNameFs_S): same pattern with
//     fieldNameFs. Text novel chapters expose content (HTML) and
//     empty fieldNameFs — Resolve errors for those (no media URL
//     exists; the app renders the HTML instead). Live: room plays
//     its "stream" URL directly.
//  8. Covers are XOR-obfuscated (d1/d.java bili, a1/a.java tx):
//     XOR the first min(100, len) bytes with the repeating 13-byte
//     key "2019ysapp7527". CoverFetch implements exactly that,
//     BUT only for URLs served from the current image base: live
//     snapshots load through plain Glide with no prefix and no XOR,
//     and the app's live adapter never touches the XOR fetcher.
//     The discriminator is the URL host: image-base host -> XOR,
//     any other host -> pass through untouched. Video/comic/novel
//     covers are relative paths prefixed with the image base
//     (d1/a constructor logic); live snapshots are absolute URLs
//     used as-is, so the host test separates the two correctly.
//  9. Paid rule (bean-evident fields price/isBuy/payType): list
//     items with price > 0 AND isBuy == false are FILTERED from
//     Catalog/Search; Detail/Resolve on such an item errors
//     "paid content". The exact paywall UX combination
//     (isEnoughMoney etc.) is not live-verified; this is the
//     conservative rule, not a crack of any paywall.
// 10. Bili comic chapter pictures: bili ComicChapter is an EMPTY
//     bean in jadx and the chapter callback binds no picture list,
//     so the bili chapter-picture field is UNRESOLVED in the app
//     itself. This port attempts the endpoint and parses any
//     chapter/pics/images/picList array generically; if none is
//     present it errors honestly (same limitation as the app).
//     Tx is verified-shape: comicsChapter/pics ->
//     {chapter: [{comicsPic, high, width}]}.
// 11. Category tables are copied VERBATIM from the fragment tab
//     lists (order = tab order). Card rawIDs are kind-prefixed
//     ("video:<id>", "comic:<id>", "anime:<id>", "live:<id>",
//     "novel:<id>") because one flat category list serves three
//     content types plus live; the numeric part and display name
//     are exactly the App's. Detail/Resolve parse the prefix.
//     Tx novel has 12 categories, bili novel 2 (latest + audio).
//
// VERIFICATION STATUS (2026-10-24, sandbox egress; honest per leg):
//   - Crypto + signing: PROVEN LIVE. Guest-login responses decrypt
//     cleanly ("null" plaintext with valid PKCS7 padding) and the
//     config endpoint decrypts to a full 107KB ConfigBean — the KDF,
//     HMAC and AES code below are byte-correct, not just
//     jadx-faithful.
//   - Config leg: VERIFIED LIVE (no token needed). GET
//     /api/app/ping/config on the bili base returns a current
//     config whose domain list is: API [d3a6hmp27efs4i,
//     d1yf4shiabplaw, d2deyaeqecmerw, billa.uf8qbgxy8.com], VID
//     [bcats/bccts.tgxdaes.cn], IMAGE [imbbi.tgxdaes.cn/] (this
//     replaces the jadx default img.txwd009.com at runtime),
//     LAND, TEST, signKey. IMAGE base is applied by this port.
//   - Guest login: SYSTEMATICALLY REFUSED FROM THIS EGRESS.
//     Bili login returns outer code 6004, tip "账户禁止登陆",
//     decrypted data "null", for 5 fresh devIDs across Safari /
//     Android / desktop user-agents and across the exact Java
//     base36 devID shape. The dc WebView fallback cannot help:
//     the bili dc distribution (d1y4i8e7mim92x) returns NO DNS
//     answer (DoH empty) and the tx dc/API distribution
//     (d2llr0wukhemwq) likewise has no A/CNAME record — both
//     distributions appear disabled/deleted, and direct CloudFront
//     -IP connections to them reset. Tx guest login is therefore
//     not even reachable: its API base has no DNS record at all.
//     The newer config API bases hang from here (TLS handshake
//     completes, then no response). Conclusion: the 6004 is an
//     egress/IP-level guest-login ban (or a new-guest suspension),
//     NOT a signing error — the signing was verified against the
//     server's own decryptable responses.
//   - Content legs (Catalog/Detail/Search/Resolve/cover magic):
//     出口未验 FROM HERE because every content endpoint requires a
//     valid guest token and returns the inner envelope
//     {"code":1000,"msg":""} for an empty token (verified for
//     media/home, topic/details, live/home, search/details,
//     comics/tabcategory/details, novel/home, media/recommend).
//     The code paths are jadx-faithful and will run unchanged once
//     a token is obtainable from a non-banned egress (phone). The
//     tx x-user-agent string embedding the H0 base URL is
//     jadx-literal but suspicious; it is likewise untestable until
//     the tx base resolves again. Re-verify from a phone egress
//     before calling any content leg verified.

const yhBiliSlot = "yh_bilibili"

const yhBiliConst = "vEukA&w15z4VAD3kAY#fkL#rBnU!WDhN"
const yhBiliCoverKey = "2019ysapp7527"
const yhBiliSafariUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1"

type yhBiliCat struct {
	id   string
	name string
}

type yhBiliVariant struct {
	slot          string
	name          string
	base          string // h6/b.java G0 (bili) / H0 (tx), trailing slash
	hmacKey       string
	xUA           string
	loginKey      string // full HMAC key for guest-login sign
	affCode       string // JSON string value sent as affCode
	signSuffix    string // suffix appended to devID for the sign message
	pageVideo     int
	pageComic     int
	pageLive      int
	pageNovel     int
	comicEndpoint string
	novelEndpoint string
	shortEndpoint string // "" = no short category
	videoCats     []yhBiliCat
	comicCats     []yhBiliCat
	animeCats     []yhBiliCat
	liveCats      []yhBiliCat
	novelCats     []yhBiliCat
}

var yhBiliVariants = map[string]*yhBiliVariant{
	yhBiliSlot: {
		slot:       yhBiliSlot,
		name:       "🔞逼哩",
		base:       "https://d1qc43c8txyadm.cloudfront.net/",
		hmacKey:    "jR6dO6fT1yD9zY7u",
		xUA:        "DevType=Apple iPhone mobile;SysType=light;Ver=2.1.2;BuildID=Mobile Safari 18.5",
		loginKey:   "TianwSUDwMXxl12" + "wM5wK1jP3lK7pS2t",
		affCode:    `{"dc":"CMZXMGT","isLight":"true"}`,
		signSuffix: `&{"dc":"yjwxmgs","isLight":"true"}`,
		pageVideo:  10, pageComic: 12, pageLive: 12, pageNovel: 12,
		comicEndpoint: "comics/tabcategory/details",
		novelEndpoint: "novel/home",
		shortEndpoint: "media/recommend",
		videoCats: []yhBiliCat{
			{"2", "推荐"}, {"568191", "最新"}, {"1", "未成年"}, {"123401", "入站必看"},
			{"3", "乱伦"}, {"13142", "国产"}, {"4", "原创"}, {"13141", "制服"},
			{"13145", "日本"}, {"24243", "传媒"}, {"13143", "P站"},
		},
		comicCats: []yhBiliCat{
			{"52881", "推荐"}, {"33533", "韩漫"}, {"33532", "CG"}, {"33534", "COS"},
			{"33535", "C105"}, {"33537", "同人"}, {"33538", "单本"}, {"33539", "短篇"},
			{"33540", "无修正"},
		},
		animeCats: []yhBiliCat{
			{"568972", "专题"}, {"568971", "推荐"}, {"567331", "排行榜"}, {"568973", "里番"},
			{"568974", "同人"}, {"568976", "原神"}, {"568979", "中字"}, {"568981", "巨乳"},
			{"568982", "泡面番"},
		},
		liveCats: []yhBiliCat{
			{"10041", "推荐"}, {"10101", "新主播"}, {"10105", "变性"}, {"10103", "中国cn"},
			{"10102", "欧美us"}, {"10104", "VR"}, {"10106", "日本"},
		},
		novelCats: []yhBiliCat{
			{"11121", "最新"}, {"12581", "有声小说"},
		},
	},
}

type yhBiliClient struct {
	mu        sync.Mutex
	token     string
	tokenAt   time.Time
	imageBase string
	configOK  bool
	cache     sync.Map // card rawID -> map[string]interface{} (raw item)
}

var yhBiliClients = map[string]*yhBiliClient{
	yhBiliSlot: {},
}

func yhBiliClientFor(slot string) *yhBiliClient {
	if c, ok := yhBiliClients[slot]; ok {
		return c
	}
	c := &yhBiliClient{}
	yhBiliClients[slot] = c
	return c
}

func yhBiliVariantFor(slot string) *yhBiliVariant {
	if v, ok := yhBiliVariants[slot]; ok {
		return v
	}
	return yhBiliVariants[yhBiliSlot]
}

// yhBiliAPIKey replicates c1/b.d / z0/d.d exactly.
func yhBiliAPIKey(v *yhBiliVariant, token, path string) string {
	ts := (time.Now().UnixMilli() - 1111) / 1000
	var uuidBytes [16]byte
	rand.Read(uuidBytes[:])
	uuidBytes[6] = (uuidBytes[6] & 0x0f) | 0x40
	uuidBytes[8] = (uuidBytes[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", uuidBytes[0:4], uuidBytes[4:6], uuidBytes[6:8], uuidBytes[8:10], uuidBytes[10:16])
	msg := token + "&" + path + "&" + v.xUA + "&" + strconv.FormatInt(ts, 10) + "&" + uuid
	mac := hmac.New(sha1.New, []byte(v.hmacKey))
	mac.Write([]byte(msg))
	return fmt.Sprintf("timestamp=%d;sign=%s;nonce=%s", ts, hex.EncodeToString(mac.Sum(nil)), uuid)
}

// yhBiliDecrypt replicates c1/b.f exactly.
func yhBiliDecrypt(b64data string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64data)
	if err != nil {
		return "", err
	}
	if len(raw) < 12 {
		return "", fmt.Errorf("yh_bili: short envelope %d", len(raw))
	}
	nonce, ct := raw[:12], raw[12:]
	c10 := append([]byte(yhBiliConst), nonce...)
	midFull := sha256.Sum256(c10)
	mid := midFull[8:24]
	half := len(c10) / 2
	c11 := append(append([]byte{}, mid...), c10[:half]...)
	b11 := sha256.Sum256(c11)
	c12 := append(append([]byte{}, c10[half:]...), mid...)
	b13 := sha256.Sum256(c12)
	key := append(append(append([]byte{}, b11[:8]...), b13[8:24]...), b11[24:32]...)
	iv := append(append(append([]byte{}, b13[:4]...), b11[12:20]...), b13[28:32]...)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", fmt.Errorf("yh_bili: bad ciphertext length %d", len(ct))
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ct)
	if len(out) == 0 {
		return "", fmt.Errorf("yh_bili: empty plaintext")
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return "", fmt.Errorf("yh_bili: bad pkcs7 pad %d", pad)
	}
	return string(out[:len(out)-pad]), nil
}

func yhBiliBase36(n int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	out := ""
	for n > 0 {
		out = string(digits[n%36]) + out
		n /= 36
	}
	if neg {
		return "-" + out
	}
	return out
}

// yhBiliGuestLogin replicates the Browse activities' manual login.
func yhBiliGuestLogin(v *yhBiliVariant, c *yhBiliClient) error {
	var rb [8]byte
	rand.Read(rb[:])
	rand63 := int64(0)
	for _, b := range rb {
		rand63 = rand63<<8 | int64(b)
	}
	if rand63 < 0 {
		rand63 = -rand63
	}
	devID := yhBiliBase36(rand63) + yhBiliBase36(time.Now().UnixMilli())
	mac := hmac.New(sha1.New, []byte(v.loginKey))
	mac.Write([]byte(devID + v.signSuffix))
	sign := hex.EncodeToString(mac.Sum(nil))
	payload := map[string]interface{}{"devID": devID, "affCode": v.affCode, "sign": sign}
	jb, _ := json.Marshal(payload)
	headers := map[string]string{
		"x-api-key":    yhBiliAPIKey(v, "", "/api/app/login/guest"),
		"x-user-agent": v.xUA,
		"user-agent":   yhBiliSafariUA,
		"Content-Type": "application/json",
	}
	raw, code, _, err := httpDo("POST", v.base+"api/app/login/guest", strings.NewReader(string(jb)), headers)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("yh_bili login http %d", code)
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return fmt.Errorf("yh_bili login parse: %v", err)
	}
	dataStr := g5StrVal(outer["data"])
	if dataStr == "" {
		return fmt.Errorf("yh_bili login: empty data (outer code %v)", outer["code"])
	}
	plain, err := yhBiliDecrypt(dataStr)
	if err != nil {
		return fmt.Errorf("yh_bili login decrypt: %v", err)
	}
	if plain == "" || plain == "null" {
		return fmt.Errorf("yh_bili login refused: decrypted data is %q (outer code %v) — app falls back to the dc WebView, whose distribution is dead; retry from a non-banned egress", plain, outer["code"])
	}
	var login map[string]interface{}
	if err := json.Unmarshal([]byte(plain), &login); err != nil {
		return fmt.Errorf("yh_bili login parse inner: %v", err)
	}
	tok := g5StrVal(login["token"])
	if tok == "" {
		return fmt.Errorf("yh_bili login: no token in %s", plain)
	}
	c.mu.Lock()
	c.token = tok
	c.tokenAt = time.Now()
	c.mu.Unlock()
	return nil
}

// yhBiliImageBase returns the current image base, refreshing it via
// ping/config on first use (ConfigBean.initHost: IMAGE type only).
func yhBiliImageBase(v *yhBiliVariant, c *yhBiliClient) string {
	c.mu.Lock()
	if c.configOK && c.imageBase != "" {
		base := c.imageBase
		c.mu.Unlock()
		return base
	}
	c.mu.Unlock()
	inner, err := yhBiliCallRaw(v, c, "GET", "/api/app/ping/config", nil, false)
	if err == nil {
		for _, e := range yhArr(inner["domain"]) {
			d := yhMap(e)
			if d == nil {
				continue
			}
			if g5StrVal(d["type"]) == "IMAGE" {
				if urls := yhArr(d["urls"]); len(urls) > 0 {
					if u := g5StrVal(urls[0]); u != "" {
						c.mu.Lock()
						c.imageBase = u
						c.configOK = true
						c.mu.Unlock()
						return u
					}
				}
			}
		}
	}
	c.mu.Lock()
	c.configOK = true
	if c.imageBase == "" {
		c.imageBase = "https://img.txwd009.com/"
	}
	base := c.imageBase
	c.mu.Unlock()
	return base
}

func yhBiliJoinImageBase(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

// yhBiliCallRaw performs one interceptor-equivalent call and returns
// the decrypted inner JSON. If needAuth and the inner payload is the
// auth-empty envelope {"code":1000,"msg":""}, it returns errAuthEmpty.
var yhBiliErrAuthEmpty = fmt.Errorf("yh_bili: auth-empty response (token invalid or missing)")

func yhBiliCallRaw(v *yhBiliVariant, c *yhBiliClient, method, path string, payload map[string]interface{}, needAuth bool) (map[string]interface{}, error) {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	var body io.Reader
	if payload != nil {
		jb, _ := json.Marshal(payload)
		body = strings.NewReader(string(jb))
	}
	headers := map[string]string{
		"x-api-key":     yhBiliAPIKey(v, token, path),
		"x-user-agent":  v.xUA,
		"authorization": token,
		"Content-Type":  "application/json; charset=utf-8",
	}
	if v.slot == yhBiliSlot {
		headers["user-agent"] = yhBiliSafariUA
	}
	raw, code, _, err := httpDo(method, v.base+strings.TrimPrefix(path, "/"), body, headers)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_bili http %d: %s", code, strings.TrimSpace(string(raw)))
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, fmt.Errorf("yh_bili parse outer: %v", err)
	}
	dataStr, isStr := outer["data"].(string)
	if !isStr {
		// Some endpoints return plain JSON (config on some builds).
		return outer, nil
	}
	plain, err := yhBiliDecrypt(dataStr)
	if err != nil {
		return nil, fmt.Errorf("yh_bili decrypt: %v", err)
	}
	var inner map[string]interface{}
	if err := json.Unmarshal([]byte(plain), &inner); err != nil {
		return nil, fmt.Errorf("yh_bili parse inner: %v", err)
	}
	if needAuth {
		if _, hasCode := inner["code"]; hasCode {
			hasContent := false
			for _, k := range []string{"mediaList", "topList", "topicInfos", "topicList", "gatherList", "list", "comicsList", "topicList", "novelList", "cartoonList", "searchMediaList", "novelData", "chapterList", "chapters", "chapter"} {
				if inner[k] != nil {
					hasContent = true
					break
				}
			}
			if !hasContent {
				return nil, yhBiliErrAuthEmpty
			}
		}
	}
	return inner, nil
}

// yhBiliCall ensures a fresh token, then calls once, retrying after a
// forced re-login when the server reports auth-empty.
func yhBiliCall(v *yhBiliVariant, c *yhBiliClient, method, path string, payload map[string]interface{}) (map[string]interface{}, error) {
	c.mu.Lock()
	token, tokenAt := c.token, c.tokenAt
	c.mu.Unlock()
	if token == "" || time.Since(tokenAt) > 72*time.Hour {
		if err := yhBiliGuestLogin(v, c); err != nil {
			return nil, err
		}
	}
	inner, err := yhBiliCallRaw(v, c, method, path, payload, true)
	if err == yhBiliErrAuthEmpty {
		if err2 := yhBiliGuestLogin(v, c); err2 != nil {
			return nil, err2
		}
		return yhBiliCallRaw(v, c, method, path, payload, true)
	}
	return inner, err
}

func yhBiliNum(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func yhBiliBool(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// yhBiliPaid reports the conservative paid filter: price > 0 and not
// bought. Free/preview items (price 0, or bought) pass.
func yhBiliPaid(rec map[string]interface{}) bool {
	return yhBiliNum(rec["price"]) > 0 && !yhBiliBool(rec["isBuy"])
}

func yhBiliVideoCard(v *yhBiliVariant, c *yhBiliClient, slot string, rec map[string]interface{}, kind string) (map[string]interface{}, bool) {
	if yhBiliPaid(rec) {
		return nil, false
	}
	id := g5StrVal(rec["id"])
	if id == "" {
		id = strconv.Itoa(yhBiliNum(rec["id"]))
	}
	title := g5StrVal(rec["title"])
	if id == "" || id == "0" || title == "" {
		return nil, false
	}
	rawID := kind + ":" + id
	c.cache.Store(rawID, rec)
	cover := yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(rec["coverImg"]))
	remark := ""
	if wt := yhBiliNum(rec["watchTimes"]); wt > 0 {
		remark = strconv.Itoa(wt) + "播"
	}
	desc := g5StrVal(rec["desc"])
	if tags := yhArr(rec["tags"]); len(tags) > 0 {
		ts := []string{}
		for _, t := range tags {
			if s := g5StrVal(t); s != "" {
				ts = append(ts, s)
			}
		}
		if len(ts) > 0 {
			if desc != "" {
				desc += " "
			}
			desc += strings.Join(ts, " ")
		}
	}
	return makeDrama(slot, rawID, title, cover, remark, "", desc), true
}

func yhBiliVideoCards(v *yhBiliVariant, c *yhBiliClient, slot string, recs []interface{}, kind string) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if card, ok := yhBiliVideoCard(v, c, slot, m, kind); ok {
				out = append(out, card)
			}
		}
	}
	return out
}

// yhBiliHomeMedia flattens HomeVideoBean.getAllMedia(): topList +
// topicInfos + topicList + gatherList + mediaList, each list's
// mediaList entries.
func yhBiliHomeMedia(inner map[string]interface{}) []interface{} {
	out := []interface{}{}
	for _, key := range []string{"topList", "topicInfos", "topicList", "gatherList"} {
		for _, e := range yhArr(inner[key]) {
			if g := yhMap(e); g != nil {
				out = append(out, yhArr(g["mediaList"])...)
			}
		}
	}
	out = append(out, yhArr(inner["mediaList"])...)
	return out
}

func yhBiliCategories(slot string) []interface{} {
	v := yhBiliVariantFor(slot)
	out := []interface{}{}
	add := func(kind string, cats []yhBiliCat, label string) {
		for _, cat := range cats {
			out = append(out, categoryObj(kind+":"+cat.id, label+"·"+cat.name))
		}
	}
	add("video", v.videoCats, "视频")
	add("comic", v.comicCats, "漫画")
	add("anime", v.animeCats, "动漫")
	add("live", v.liveCats, "直播")
	add("novel", v.novelCats, "小说")
	if v.shortEndpoint != "" {
		out = append(out, categoryObj("short", "短视频"))
	}
	return out
}

func yhBiliCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	v := yhBiliVariantFor(slot)
	c := yhBiliClientFor(slot)
	if page < 1 {
		page = 1
	}
	kind, numID, _ := strings.Cut(category, ":")
	if category == "short" {
		if v.slot == yhBiliSlot {
			inner, err := yhBiliCall(v, c, "POST", "/api/app/media/recommend", map[string]interface{}{"contentType": 1, "pageNum": page, "pageSize": 10})
			if err != nil {
				return nil, 0, false, err
			}
			items := yhBiliVideoCards(v, c, slot, yhBiliHomeMedia(inner), "video")
			return items, 0, len(items) >= 10, nil
		}
		inner, err := yhBiliCall(v, c, "POST", "/api/app/media/short/recommend", map[string]interface{}{"pageNum": page, "pageSize": 30})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhBiliVideoCards(v, c, slot, yhBiliHomeMedia(inner), "video")
		return items, 0, len(items) >= 30, nil
	}
	idNum, _ := strconv.Atoi(numID)
	switch kind {
	case "video", "anime":
		pageSize := v.pageVideo
		endpoint := "/api/app/media/home"
		if numID == "27333" {
			endpoint = "/api/app/media/topic/details"
		}
		inner, err := yhBiliCall(v, c, "POST", endpoint, map[string]interface{}{"id": idNum, "pageNum": page, "pageSize": pageSize})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhBiliVideoCards(v, c, slot, yhBiliHomeMedia(inner), kind)
		return items, 0, len(items) >= pageSize, nil
	case "comic":
		inner, err := yhBiliCall(v, c, "POST", "/api/app/"+v.comicEndpoint, map[string]interface{}{"id": idNum, "pageNum": page, "pageSize": v.pageComic})
		if err != nil {
			return nil, 0, false, err
		}
		// ComicTypeFragment uses getComicsList() only; the
		// topic-merged getComic() helper is not used by the UI.
		out := []interface{}{}
		for _, e := range yhArr(inner["comicsList"]) {
			m := yhMap(e)
			if m == nil || yhBiliPaid(m) {
				continue
			}
			id := g5StrVal(m["id"])
			title := g5StrVal(m["title"])
			if id == "" || id == "0" || title == "" {
				continue
			}
			rawID := "comic:" + id
			c.cache.Store(rawID, m)
			cover := yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(m["coverImg"]))
			remark := ""
			if cn := yhBiliNum(m["chapterNum"]); cn > 0 {
				remark = strconv.Itoa(cn) + "话"
			}
			out = append(out, makeDrama(slot, rawID, title, cover, remark, "", g5StrVal(m["desc"])))
		}
		return out, 0, len(out) >= v.pageComic, nil
	case "live":
		inner, err := yhBiliCall(v, c, "POST", "/api/app/live/home", map[string]interface{}{"id": idNum, "pageNum": page, "pageSize": v.pageLive})
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(inner["list"]) {
			m := yhMap(e)
			if m == nil {
				continue
			}
			id := g5StrVal(m["id"])
			name := g5StrVal(m["username"])
			if id == "" || id == "0" || name == "" {
				continue
			}
			rawID := "live:" + id
			c.cache.Store(rawID, m)
			// Live snapshots are absolute URLs used as-is (plain
			// Glide, no image-base prefix, no XOR).
			remark := ""
			if !yhBiliBool(m["isOnline"]) {
				remark = "离线"
			} else if vc := yhBiliNum(m["viewersCount"]); vc > 0 {
				remark = strconv.Itoa(vc) + "人"
			}
			out = append(out, makeDrama(slot, rawID, name, g5StrVal(m["snapshot"]), remark, "", ""))
		}
		return out, 0, len(out) >= v.pageLive, nil
	case "novel":
		inner, err := yhBiliCall(v, c, "POST", "/api/app/"+v.novelEndpoint, map[string]interface{}{"id": idNum, "pageNum": page, "pageSize": v.pageNovel, "sort": 1})
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(inner["novelList"]) {
			m := yhMap(e)
			if m == nil || yhBiliPaid(m) {
				continue
			}
			id := g5StrVal(m["id"])
			title := g5StrVal(m["title"])
			if id == "" || id == "0" || title == "" {
				continue
			}
			rawID := "novel:" + id
			c.cache.Store(rawID, m)
			cover := yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(m["coverImg"]))
			if cover == yhBiliJoinImageBase(yhBiliImageBase(v, c), "") {
				cover = yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(m["cover"]))
			}
			out = append(out, makeDrama(slot, rawID, title, cover, "", "", g5StrVal(m["desc"])))
		}
		return out, 0, len(out) >= v.pageNovel, nil
	}
	return nil, 0, false, fmt.Errorf("yh_bili: unknown category %s", category)
}

func yhBiliVideoURL(v *yhBiliVariant, c *yhBiliClient, rec map[string]interface{}) string {
	vu := g5StrVal(rec["videoUrl"])
	if vu == "" {
		return ""
	}
	if strings.HasPrefix(vu, "http://") || strings.HasPrefix(vu, "https://") {
		return vu
	}
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	return v.base + "api/app/media/h5/m3u8/" + vu + "?token=" + token
}

func yhBiliDetail(slot, rawID string) (map[string]interface{}, error) {
	v := yhBiliVariantFor(slot)
	c := yhBiliClientFor(slot)
	kind, rest, _ := strings.Cut(rawID, ":")
	switch kind {
	case "video", "anime":
		val, ok := c.cache.Load(rawID)
		if !ok {
			return nil, fmt.Errorf("yh_bili: video %s not in list cache — open it from a list first", rawID)
		}
		rec := val.(map[string]interface{})
		if yhBiliPaid(rec) {
			return nil, fmt.Errorf("yh_bili: paid content %s", rawID)
		}
		d := makeDrama(slot, rawID, g5StrVal(rec["title"]),
			yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(rec["coverImg"])), "", "", g5StrVal(rec["desc"]))
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
		return d, nil
	case "live":
		val, ok := c.cache.Load(rawID)
		if !ok {
			return nil, fmt.Errorf("yh_bili: live room %s not in list cache", rawID)
		}
		rec := val.(map[string]interface{})
		d := makeDrama(slot, rawID, g5StrVal(rec["username"]), g5StrVal(rec["snapshot"]), "", "", "")
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"直播"})
		return d, nil
	case "comic":
		inner, err := yhBiliCall(v, c, "POST", "/api/app/comics/detail", map[string]interface{}{"id": mustInt(rest)})
		if err != nil {
			return nil, err
		}
		if yhBiliPaid(inner) {
			return nil, fmt.Errorf("yh_bili: paid comic %s", rawID)
		}
		d := makeDrama(slot, rawID, g5StrVal(inner["title"]),
			yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(inner["coverImg"])), "", "", g5StrVal(inner["desc"]))
		chapters := yhArr(inner["chapters"])
		if len(chapters) == 0 {
			chapters = yhArr(inner["chapterList"])
		}
		epIDs := []string{}
		epNames := []string{}
		for _, e := range chapters {
			ch := yhMap(e)
			if ch == nil {
				continue
			}
			cid := g5StrVal(ch["id"])
			if cid == "" || cid == "0" {
				continue
			}
			epIDs = append(epIDs, "comic:"+rest+":"+cid)
			name := g5StrVal(ch["title"])
			if name == "" {
				name = "第" + g5StrVal(ch["chapterNo"]) + "话"
			}
			epNames = append(epNames, name)
		}
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, epNames)
		}
		return d, nil
	case "novel":
		inner, err := yhBiliCall(v, c, "POST", "/api/app/novel/detail", map[string]interface{}{"id": mustInt(rest), "pageNum": 1, "pageSize": 12000})
		if err != nil {
			return nil, err
		}
		nd := yhMap(inner["novelData"])
		if nd == nil {
			nd = inner
		}
		if yhBiliPaid(nd) {
			return nil, fmt.Errorf("yh_bili: paid novel %s", rawID)
		}
		cover := g5StrVal(nd["coverImg"])
		if cover == "" {
			cover = g5StrVal(nd["cover"])
		}
		d := makeDrama(slot, rawID, g5StrVal(nd["title"]),
			yhBiliJoinImageBase(yhBiliImageBase(v, c), cover), "", "", g5StrVal(nd["desc"]))
		epIDs := []string{}
		epNames := []string{}
		for _, e := range yhArr(inner["chapterList"]) {
			ch := yhMap(e)
			if ch == nil {
				continue
			}
			cid := g5StrVal(ch["id"])
			if cid == "" || cid == "0" {
				continue
			}
			epIDs = append(epIDs, "novel:"+rest+":"+cid)
			epNames = append(epNames, g5StrVal(ch["title"]))
		}
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, epNames)
		}
		return d, nil
	}
	return nil, fmt.Errorf("yh_bili: unknown id %s", rawID)
}

func mustInt(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}

func yhBiliResolve(slot, rawID string) (map[string]interface{}, error) {
	v := yhBiliVariantFor(slot)
	c := yhBiliClientFor(slot)
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	kind, rest, _ := strings.Cut(vid, ":")
	switch kind {
	case "video", "anime":
		val, ok := c.cache.Load(kind + ":" + strings.SplitN(rest, ":", 2)[0])
		if !ok {
			val, ok = c.cache.Load(vid)
		}
		if !ok {
			return nil, fmt.Errorf("yh_bili: video %s not in list cache", vid)
		}
		rec := val.(map[string]interface{})
		if yhBiliPaid(rec) {
			return nil, fmt.Errorf("yh_bili: paid content %s", vid)
		}
		u := yhBiliVideoURL(v, c, rec)
		if u == "" {
			return nil, fmt.Errorf("yh_bili: no videoUrl for %s", vid)
		}
		return playResult(u, slot, rawID, yhBiliSafariUA, v.base, "m3u8", "hls"), nil
	case "live":
		val, ok := c.cache.Load(vid)
		if !ok {
			return nil, fmt.Errorf("yh_bili: live room %s not in list cache", vid)
		}
		rec := val.(map[string]interface{})
		stream := g5StrVal(rec["stream"])
		if stream == "" {
			return nil, fmt.Errorf("yh_bili: live room %s has no stream", vid)
		}
		return playResult(stream, slot, rawID, yhBiliSafariUA, v.base, "m3u8", "hls"), nil
	case "comic":
		// Episode ids: "comic:<comicId>:<chapterId>" (whole chapter)
		// or "comic:<comicId>:<chapterId>:<picIndex>" (one picture,
		// kmpic-style). Resolve returns the picture URL via
		// playResult; the whole-chapter form also carries the full
		// picture list under "pics".
		parts := strings.Split(rest, ":")
		if len(parts) < 2 {
			return nil, fmt.Errorf("yh_bili: bad comic episode %s", vid)
		}
		comicID, chapterID := parts[0], parts[1]
		picIdx := 0
		if len(parts) >= 3 {
			picIdx, _ = strconv.Atoi(parts[2])
		}
		pics, err := yhBiliComicPics(v, c, chapterID)
		if err != nil {
			return nil, err
		}
		if picIdx < 0 || picIdx >= len(pics) {
			return nil, fmt.Errorf("yh_bili: comic %s chapter %s has %d pics, index %d out of range", comicID, chapterID, len(pics), picIdx)
		}
		res := playResult(pics[picIdx], slot, rawID, yhBiliSafariUA, v.base, "jpg", "")
		if len(parts) < 3 {
			res["pics"] = pics
			res["picCount"] = len(pics)
		}
		return res, nil
	case "novel":
		parts := strings.Split(rest, ":")
		if len(parts) < 2 {
			return nil, fmt.Errorf("yh_bili: bad novel episode %s", vid)
		}
		inner, err := yhBiliCall(v, c, "POST", "/api/app/novelChapter/info", map[string]interface{}{"id": mustInt(parts[1])})
		if err != nil {
			return nil, err
		}
		fs := g5StrVal(inner["fieldNameFs"])
		if fs == "" {
			return nil, fmt.Errorf("yh_bili: novel chapter %s is text-only (content HTML, no media URL) — render its content field instead", parts[1])
		}
		var u string
		if strings.HasPrefix(fs, "http://") || strings.HasPrefix(fs, "https://") {
			u = fs
		} else {
			c.mu.Lock()
			token := c.token
			c.mu.Unlock()
			u = v.base + "api/app/media/h5/m3u8/" + fs + "?token=" + token
		}
		return playResult(u, slot, rawID, yhBiliSafariUA, v.base, "m3u8", "hls"), nil
	}
	return nil, fmt.Errorf("yh_bili: unknown id %s", vid)
}

// yhBiliComicPics fetches a chapter's picture URLs. Tx shape is
// verified (comicsChapter/pics -> chapter[].comicsPic). Bili shape
// is unresolved in the app itself (empty ComicChapter bean); parse
// any plausible array generically and error honestly if none exists.
func yhBiliComicPics(v *yhBiliVariant, c *yhBiliClient, chapterID string) ([]string, error) {
	var inner map[string]interface{}
	var err error
	inner, err = yhBiliCall(v, c, "POST", "/api/app/comics/chapter/detail", map[string]interface{}{"id": mustInt(chapterID)})
	if err != nil {
		return nil, err
	}
	pics := []string{}
	collect := func(arr []interface{}) {
		for _, e := range arr {
			if s := g5StrVal(e); s != "" {
				pics = append(pics, yhBiliJoinImageBase(yhBiliImageBase(v, c), s))
				continue
			}
			if m := yhMap(e); m != nil {
				if s := g5StrVal(m["comicsPic"]); s != "" {
					pics = append(pics, yhBiliJoinImageBase(yhBiliImageBase(v, c), s))
				}
			}
		}
	}
	collect(yhArr(inner["chapter"]))
	for _, key := range []string{"pics", "images", "picList", "pages"} {
		collect(yhArr(inner[key]))
	}
	if len(pics) == 0 {
		return nil, fmt.Errorf("yh_bili: comic chapter %s returned no picture list (bili chapter-picture field is unresolved in the app itself; tx uses comicsChapter/pics)", chapterID)
	}
	return pics, nil
}

func yhBiliSearch(slot, kw string) ([]interface{}, error) {
	v := yhBiliVariantFor(slot)
	c := yhBiliClientFor(slot)
	out := []interface{}{}
	seen := map[string]bool{}
	add := func(cards []interface{}) {
		for _, card := range cards {
			m, ok := card.(map[string]interface{})
			if !ok {
				continue
			}
			id := g5StrVal(m["vod_id"])
			if id == "" {
				// makeDrama stores the raw id under "vod_id"; fall
				// back to the title+cover key.
				id = g5StrVal(m["vod_name"]) + "|" + g5StrVal(m["vod_pic"])
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, card)
		}
	}
	// A search whose every sub-call fails (e.g. guest login refused)
	// must surface the error, not a vacuous empty success.
	var firstErr error
	noteErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if v.slot == yhBiliSlot {
		for _, mt := range []struct {
			mediaType int
			kind      string
			field     string
		}{{1, "video", "mediaList"}, {4, "comic", "mediaList"}, {12, "novel", "novelList"}} {
			inner, err := yhBiliCall(v, c, "POST", "/api/app/search/details",
				map[string]interface{}{"content": kw, "pageNum": 1, "pageSize": 10, "mediaType": mt.mediaType})
			if err != nil {
				noteErr(err)
				continue
			}
			if mt.kind == "novel" {
				// Novel search items share the novel list shape.
				items := []interface{}{}
				for _, e := range yhArr(inner[mt.field]) {
					m := yhMap(e)
					if m == nil || yhBiliPaid(m) {
						continue
					}
					id := g5StrVal(m["id"])
					title := g5StrVal(m["title"])
					if id == "" || id == "0" || title == "" {
						continue
					}
					rawID := "novel:" + id
					c.cache.Store(rawID, m)
					items = append(items, makeDrama(slot, rawID, title,
						yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(m["coverImg"])), "", "", g5StrVal(m["desc"])))
				}
				add(items)
				continue
			}
			if mt.kind == "comic" {
				// Comic search returns mediaList items in the app
				// (SearchTypeFragment reads mediaList for the comic
				// tab too); route them as playable video cards.
				add(yhBiliVideoCards(v, c, slot, yhArr(inner[mt.field]), "video"))
				continue
			}
			add(yhBiliVideoCards(v, c, slot, yhArr(inner[mt.field]), "video"))
		}
		if len(out) == 0 && firstErr != nil {
			return nil, firstErr
		}
		return out, nil
	}
	// Tx: video via media/search/detail (contentType 1 ->
	// searchMediaList); comic/cVideo/novel via acg/search/detail
	// (contentType 4/3/6 -> comicsList/cartoonList/novelList).
	inner, err := yhBiliCall(v, c, "POST", "/api/app/media/search/detail",
		map[string]interface{}{"searchWord": kw, "sort": 1, "pageNum": 1, "pageSize": 30, "contentType": 1})
	if err != nil {
		noteErr(err)
	} else {
		add(yhBiliVideoCards(v, c, slot, yhArr(inner["searchMediaList"]), "video"))
	}
	for _, st := range []struct {
		ct    int
		field string
		kind  string
	}{{4, "comicsList", "comic"}, {3, "cartoonList", "anime"}, {6, "novelList", "novel"}} {
		inner, err := yhBiliCall(v, c, "POST", "/api/app/acg/search/detail",
			map[string]interface{}{"searchWord": kw, "sort": 1, "pageNum": 1, "pageSize": 30, "contentType": st.ct})
		if err != nil {
			noteErr(err)
			continue
		}
		if st.kind == "novel" {
			items := []interface{}{}
			for _, e := range yhArr(inner[st.field]) {
				m := yhMap(e)
				if m == nil || yhBiliPaid(m) {
					continue
				}
				id := g5StrVal(m["id"])
				title := g5StrVal(m["title"])
				if id == "" || id == "0" || title == "" {
					continue
				}
				rawID := "novel:" + id
				c.cache.Store(rawID, m)
				items = append(items, makeDrama(slot, rawID, title,
					yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(m["coverImg"])), "", "", g5StrVal(m["desc"])))
			}
			add(items)
			continue
		}
		if st.kind == "comic" {
			items := []interface{}{}
			for _, e := range yhArr(inner[st.field]) {
				m := yhMap(e)
				if m == nil || yhBiliPaid(m) {
					continue
				}
				id := g5StrVal(m["id"])
				title := g5StrVal(m["title"])
				if id == "" || id == "0" || title == "" {
					continue
				}
				rawID := "comic:" + id
				c.cache.Store(rawID, m)
				items = append(items, makeDrama(slot, rawID, title,
					yhBiliJoinImageBase(yhBiliImageBase(v, c), g5StrVal(m["coverImg"])), "", "", g5StrVal(m["desc"])))
			}
			add(items)
			continue
		}
		add(yhBiliVideoCards(v, c, slot, yhArr(inner[st.field]), st.kind))
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// yhBiliCoverFetch implements the XOR cover transform (d1/d.java,
// a1/a.java): XOR the first min(100, len) bytes with the repeating
// key "2019ysapp7527". It applies ONLY to image-base URLs; live
// snapshots and other absolute URLs pass through untouched (the
// app's live adapter never uses the XOR fetcher).
func yhBiliCoverFetch(rawurl string) ([]byte, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	// Determine the image-base host from either variant's cached
	// base; the two variants share the same default and the live
	// config value, so testing both is safe.
	imgHost := false
	for _, slot := range []string{yhBiliSlot} {
		c := yhBiliClientFor(slot)
		c.mu.Lock()
		base := c.imageBase
		c.mu.Unlock()
		if base == "" {
			base = "https://img.txwd009.com/"
		}
		if bu, err := url.Parse(base); err == nil && bu.Host == u.Host {
			imgHost = true
		}
	}
	body, code, _, err := httpGet(rawurl, map[string]string{"User-Agent": yhBiliSafariUA})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_bili cover http %d", code)
	}
	if !imgHost {
		return body, nil
	}
	n := len(body)
	if n > 100 {
		n = 100
	}
	out := append([]byte{}, body...)
	for i := 0; i < n; i++ {
		out[i] ^= yhBiliCoverKey[i%len(yhBiliCoverKey)]
	}
	return out, nil
}

func yhBiliHeaders() map[string]string {
	return map[string]string{"User-Agent": yhBiliSafariUA}
}

func init() {
	for _, v := range yhBiliVariants {
		vv := v
		registerSite(vv.slot, &siteImpl{
			Key:        vv.slot,
			Name:       vv.name,
			DefaultCat: "video:" + vv.videoCats[0].id,
			HomeURL:    vv.base,
			Headers:    yhBiliHeaders,
			Categories: yhBiliCategories,
			Catalog:    yhBiliCatalog,
			Detail:     yhBiliDetail,
			Resolve:    yhBiliResolve,
			Search:     yhBiliSearch,
			CoverFetch: yhBiliCoverFetch,
		})
	}
}
