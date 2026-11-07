package seku

import (
	"crypto/aes"
	"crypto/md5"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- AVbobo （type 28）: signed + AES-ECB envelope API ----
// Recipe: jadx q0/{a,b,c}.java + avbobo fragments/beans, verified
// line by line 2026-10-24. Corrections to the original task brief,
// all jadx-proven (do not reintroduce the brief's versions):
//
//  1. SIGNATURE IS MD5, NOT SHA-256. q0/c.java (AvboboInterceptor)
//     uses MessageDigest.getInstance("md5"); the hex digest is
//     left-padded with "0" to 32 chars (the MD5 length). Plaintext,
//     concatenated exactly in this order:
//       app_unique + "63f28879a63066080c766a08b2687f10@"
//       + "ef5680ece71acfb32b3689ecdf561ae0@" + millis + "@"
//       + encodedPath + "@" + bodyString
//       + "58691d0bac1349411f1679f3330d230b"
//     where app_unique = "fen.dajiba-8cm.com-" + deviceID, deviceID =
//     32 chars from [0-9A-F] regenerated per AvboboApi construction
//     (h6.l.t()). Headers set: app_now, authorization ("Bearer "+
//     token or ""), app_sign, app_unique.
//  2. THE "61 x 32-HEX KEY POOL" IS NOT A DECRYPTION-KEY POOL.
//     Location work (2026-10-24): a jadx-wide search finds NO code
//     that selects among the 32-hex strings by day, by class, or by
//     any index for decryption. The strings are group_id FILTER
//     VALUES for Mongo-style list queries (s = [{"group_id": sort}],
//     q = {"$and": [fixed prefix groups..., one group_id per visible
//     filter tab]}); selection is purely "array index = selected tab
//     position" inside each fragment. The ONLY AES key in the module
//     is the single native value below, used for every response.
//     Conclusion: there is no key-pool subscript rule to locate —
//     the pool is a filter table, not a key table. The tables are
//     copied verbatim into yhAvboboFilters below.
//  3. "/src2/v2", "/single/images", "/single/ju" are NOT API
//     endpoints. They are substrings tested in cover-path logic
//     (MovieDetail.getCoverImage path prefix rule, see
//     yhAvboboCoverURL). No AES/XOR cover transform exists in code
//     (plain Glide load); covers therefore need NO CoverFetch.
//  4. AVboboConfig{MainHosts, BackupHosts, LastestRefHosts} is a bean
//     with NO caller in this build (jadx-wide search: definition
//     only). Live hosts are the three hard-coded hosts in q0/b.java,
//     selected by SharedPreferences "avbobo_host" index 0-2, default 0.
//     Do NOT invent a config-discovery endpoint for AVboboConfig.
//  5. Interceptor path encoding (q0/c.java, replicate literally):
//     replace = full URL minus (host + "/s2") — i.e. "/gw/..." plus
//     query. If replace has no "?", OR contains
//     "/gw/res-bobo/bo/v1/api/app_resources", OR
//     "/gw/res-bobo/bo/v2/api/rank_boards/week", OR
//     "/gw/res-bobo/bo/v2/api/rank_boards/month", then
//     encoded = Base64(replace) (standard, padded). Else split on
//     "?" and encoded = Base64(part0) + "_" + Base64(part1) with ALL
//     "=" stripped. Final URL = host + "/s2/" + encoded.
//     Response: outer JSON field "body"; header "enc-type" ==
//     "base64" -> Base64-decode; otherwise AES/ECB/PKCS7-decrypt the
//     Base64-decoded body with key yhAvboboAESKey.
//  6. Login is REQUIRED, not optional. AvboboLaunchActivity logs in
//     on launch with the APK's built-in shared-account body for the
//     current host index (Base64 wire format, yhAvboboLoginBodies);
//     the token (LoginInfo.tokens.token.token) becomes the Bearer
//     token for every later call. Anonymous calls are NOT part of
//     the app's flow, and the server 401s them (see verification
//     status below). If product policy forbids reusing the built-in
//     shared accounts, delete yhAvboboLoginBodies: login will then
//     fail closed and every call returns the login error — there is
//     no anonymous fallback because the server does not offer one.
//  7. Play (MoviePlayActivity.o + callback a.f): line_id default
//     "11" (radio_line1 sets "1"); sharpness defaults to the LAST
//     entry of MovieDetail.types; call
//     play/{line_version}/{id}/{sharpness}/{line_id} (line_version
//     defaults to "res-v1" when empty); serialise the returned map
//     as k=v&...; final URL, assembled literally:
//       <host> + "/gw/res-bobo/bo/v2/hls/" + line_version + "/"
//       + id + "/" + sharpness + "/" + line_id + ".m3u8?"
//       + <serialised map>
//     (host WITHOUT "/s2"; this URL is built directly, NOT passed
//     through the interceptor). Empty types -> the app shows
//     "暂无播放源"; Resolve returns an error in that case.
//  8. Home rows (HomeTypeFragment): query_groups with page,
//     parent_group_id, needLoadChildren=1, limit=12. Returned
//     ChannelList rows named "轮播" render as a carousel, rows named
//     "女优推荐" are SKIPPED as a section, other rows render title +
//     movie grid from loadDataItems. Catalog for home categories
//     flattens loadDataItems of all rows except "女优推荐" rows.
//  9. Rank lists: rank_boards/{day|week|month}?limit=10&page=N;
//     RankVideoList.data[].detail is a MovieDetail (playCount copied
//     from the rank entry's count). Note the encoding asymmetry:
//     week/month use the single-Base64 branch, day uses the split
//     branch (it contains "?" and matches no special path).
// 10. Search (SearchResultActivity): same filter arrays as NoCode,
//     plus a "keyword" query param; q fixed prefix differs from
//     NoCode's (see yhAvboboFilters["search"]).
//
// Categories vs App navigation (flat host contract; the fragments'
// four-tab filter matrix cannot be exposed as tabs, so each category
// uses the App's DEFAULT tab values — first entry of every filter
// row, i.e. 最新上架 / 所有类型 / 所有片长 / 所有地区 / 所有片种):
//   home:jingxuan/home:av/home:xiaoshipin = HomeFragment tabs 1-3
//     (query_groups groups, see §8); hot = HotFragment (bottom tab
//     2); nocode/gif/cartoon/sj = HomeFragment tabs 4-7 (NoCode /
//     Gif / Cartoon / SJ fragments); rank:day/week/month =
//     SearchFragment rank tabs. SearchFragment's 热搜 tab is a
//     KEYWORD list (hot_search endpoint), not a video category, so
//     it has no Catalog entry; Search(kw) covers keyword lookup.
//
// VERIFICATION STATUS (2026-10-24, sandbox egress; honest per leg):
//   - Categories: 1:1 against jadx tab lists (code evidence below).
//   - Catalog/Detail/Search/Resolve/cover-magic: 出口未验 FROM HERE.
//     Evidence, all three hosts tried: host 0 (s.bolimian2017.com)
//     is alive (root path returns nginx 404) but EVERY non-root
//     path — including the login path with a valid preset-account
//     body and a correctly computed MD5 signature, the signed list
//     path, hot_search, rank, and even /anything and /favicon.ico —
//     returns HTTP 401 with body = the hostname itself, over HTTP
//     and HTTPS, both Cloudflare IPs, and with curl/OkHttp/browser
//     User-Agents. Host 1 (s.abbbcgt1.com) has NO DNS record
//     (HTTP-DNS and DoH both empty). Host 2 (s.lookbtv.com)
//     resolves but every connection from here returns 000. The
//     401 gate is therefore an egress/IP-level gate (or a
//     repurposed host), NOT a protocol error: the signature,
//     encoding, login body and AES key above are jadx-literal, and
//     the gate rejects even paths that never existed. This is NOT
//     "site dead": the origin answers, it refuses this egress.
//     Re-verify from a residential/phone egress before calling any
//     content leg verified. Cover magic bytes likewise await that
//     re-verification (cover URLs are plain https, no transform).

const yhAvboboSlot = "yh_avbobo"
const yhAvboboUA = "okhttp/4.9.0"

var yhAvboboHosts = []string{
	"https://s.bolimian2017.com",
	"https://s.abbbcgt1.com",
	"https://s.lookbtv.com",
}

// yhAvboboAESKey is the single native AES key for ALL responses:
// SignHelper.getAvbobo() (逆向报告 §2.4).
var yhAvboboAESKey = []byte("R87ARK9xdWGpIWfW")

const yhAvboboSignMid = "63f28879a63066080c766a08b2687f10@ef5680ece71acfb32b3689ecdf561ae0@"
const yhAvboboSignTail = "58691d0bac1349411f1679f3330d230b"

// yhAvboboLoginBodies are the APK's built-in shared-account request
// bodies, host-indexed, in the exact Base64 wire format
// AvboboLaunchActivity sends (Base64 of the account JSON). They are
// kept in wire format on purpose; see header §6 for the policy note.
var yhAvboboLoginBodies = []string{
	"eyJjb3VudHJ5Q29kZSI6Iis4NiIsIm1vYmlsZSI6IjEzMjcwMDk1MDQ5IiwicGFzc3dvcmQiOiIxMjM0NTYifQ==",
	"eyJjb3VudHJ5Q29kZSI6Iis4NiIsIm1vYmlsZSI6IjEzMzQzNjcyNDg2IiwicGFzc3dvcmQiOiIwMDAwMDAifQ==",
	"eyJjb3VudHJ5Q29kZSI6Iis4NiIsIm1vYmlsZSI6IjEzMzEzMDc0MDcyIiwicGFzc3dvcmQiOiIwMDAwMDAifQ==",
}

// yhAvboboFilter is one fragment's filter matrix. sort/type/duration/
// region/kind are the tab arrays IN TAB ORDER; fixed is the q fixed
// prefix; vars lists which filter rows enter q, in order.
type yhAvboboFilter struct {
	sort     []string
	typ      []string
	duration []string
	region   []string
	kind     []string
	fixed    []string
	vars     []string // subset of {"type","duration","region","kind"}
}

var yhAvboboFilters = map[string]yhAvboboFilter{
	"hot": {
		sort:     []string{"fc66e7064bdb66d13a5e01f339b261c8", "824f22d929ee216a8146f58aa41fbcb6"},
		typ:      []string{"0dd49fc26c9b6bda66e227746829b223", "cf822dc0757a316ee146ad51c931599a", "b493d1a7dadc016c610aa08003aeeb3f", "7df27d4c484f7fd5ae066ddcf47c2c7b"},
		duration: []string{"a12c3e7a987f96c84d6273eb91b7cd06", "ef7ba5675fb09a6fd6779c66afaf161b", "657aaed8d6f277843041db245f09d3ad", "ea5bb6a931f0af425fc30e1d6ac95d01", "90af9bcb0cdd27c9e02be5d6a09306bf", "a2df0f499764c61ef0a43d8ec876546d"},
		region:   []string{"424ea47ff0027a5a98446d7e3540d20d", "9da9af1c800db9bc8a04f7349ca86415"},
		fixed:    []string{"021e05a7b4e932e26511dee2391b94b7"},
		vars:     []string{"type", "duration", "region"},
	},
	"nocode": {
		sort:     []string{"25717f0c74135ff4ec56ebc6028e38a1", "71eddad4671c044855d2295a0a62be50"},
		typ:      []string{"e6b3b6ceae531a5920368cacf172a078", "189de3b02d366a911d7f7c2423c221b6", "38972cc0f84302019804955f50a4bb1a"},
		duration: []string{"c57c1944c0160234cd6abc448b38d9d1", "0a7e672bfa1fdd2e3622c77d06f44394", "c9efabb47dae74a586e4233da9ba10d8", "de18af0e0a00372f60edf1e7d7ba6cc7", "4bad913afc885a08273433f63afb8796", "f0557e08a10b1063847efdfe5d2f73d9"},
		kind:     []string{"d2899748a9ce2a1e3b6fe8e810f64133", "0d03520abb115f21f61a71ac7102a2f2", "3ebe7086df4665fbffa34a9f1524d1ac", "e5b97821717836d69fb4765e18dc2b53"},
		fixed:    []string{"983cdc5cd383fadee22baad9e5a1261d", "f0fb24fc21eb33a36b78a2d7fe83f42f"},
		vars:     []string{"type", "duration", "kind"},
	},
	"search": {
		sort:     []string{"25717f0c74135ff4ec56ebc6028e38a1", "71eddad4671c044855d2295a0a62be50"},
		typ:      []string{"e6b3b6ceae531a5920368cacf172a078", "189de3b02d366a911d7f7c2423c221b6", "38972cc0f84302019804955f50a4bb1a"},
		duration: []string{"c57c1944c0160234cd6abc448b38d9d1", "0a7e672bfa1fdd2e3622c77d06f44394", "c9efabb47dae74a586e4233da9ba10d8", "de18af0e0a00372f60edf1e7d7ba6cc7", "4bad913afc885a08273433f63afb8796", "f0557e08a10b1063847efdfe5d2f73d9"},
		kind:     []string{"d2899748a9ce2a1e3b6fe8e810f64133", "0d03520abb115f21f61a71ac7102a2f2", "3ebe7086df4665fbffa34a9f1524d1ac", "e5b97821717836d69fb4765e18dc2b53"},
		fixed:    []string{"d5fefee3d47872736717eb1ae927cfef", "f0fb24fc21eb33a36b78a2d7fe83f42f"},
		vars:     []string{"type", "duration", "kind"},
	},
	"cartoon": {
		sort:     []string{"3f3c91e33234ef8f4253d89f0fcea55e", "17062ed64034521a0910e695f03c10e9"},
		duration: []string{"d9b34ffa64c2d5077afb30d6e7b93e69", "50055082200298b9a3586f6e6e66e369", "91493d0e1e9a6ec5c1ffbfbbcb25271f", "403aa15650c508a3c2e67bb9857b2593", "f04f9c6ef88fb82f727bfc0f5b8e7464", "1e6a247a66f6a0aaabf1ff668ca32503"},
		fixed:    []string{"b2434abd513be5b7fa4951c463bbe95b", "91f4866bf40c06e6cf69989c336f63ba"},
		vars:     []string{"duration"},
	},
	"gif": {
		sort:     []string{"b88d90a30b0a77420bb6c17577f7940d", "0b8c451d91cbc5791ed4665ccf5fba0e", "0667e874d62990b508777afc068c9fa4"},
		duration: []string{"606fb98ce6e73201365e109edf7f3a57", "adeb504343cb7c42466977f5ada9fa26", "f741cbe05d980288d6448abebf84d066", "40cebc7590d900233962c1a48888be14", "0c07547db3a8c3e251ba2a46ad894852", "3de2361e5469f24a198b651333b93fe2"},
		fixed:    []string{"3b08b603cb1d1ee61d55b9a3297447e5", "a791e130c3b15137783e877d8ddc85a5"},
		vars:     []string{"duration"},
	},
	"sj": {
		sort:     []string{"7a61455a5cc3aabf5dcd3c4b47b565b2", "4359c14df0c6551007722b9857693281"},
		duration: []string{"5b5d921b8d574ef5f8b397ed2c51b25b", "b0bc8674a63aea86a1047cdb4863ac00", "9cc0657aa4358e0863b58fd708b492ca", "019acdfdf2691e52fbecd43c3cc7bf76", "03c2e4d267f1985981939c7e6c9dfc1e", "876d990b2f4619ee701d1a6d4103a3e2"},
		region:   []string{"71df259968cda98599fd5229d9deb145", "35e882afe0022011e8f403b61e49571e", "20da816cd36adcd0186236ec1b03976b", "d5d6976202ca34897c8fc863cb2535ad"},
		fixed:    []string{"f6da8301abac378a1b777183e544fede", "b8c1ae15c49eb44c25c36f2a30208568"},
		vars:     []string{"duration", "region"},
	},
}

// yhAvboboHomeGroups are the HomeTypeFragment parent groups for the
// first three HomeFragment tabs, in tab order.
var yhAvboboHomeGroups = map[string]string{
	"home:jingxuan":   "55de7d3996a7aaed271a56799a59c467",
	"home:av":         "6d828a281f92c79d75a6064458c8ed8d",
	"home:xiaoshipin": "73e3768c89b8e2d4f56b35e3d49a80d1",
}

var (
	yhAvboboMu          sync.Mutex
	yhAvboboHostIdx     = 0
	yhAvboboToken       string
	yhAvboboDeviceID    string
	yhAvboboDetailCache sync.Map // id -> map[string]interface{} (raw MovieDetail)
)

func yhAvboboDevice() string {
	yhAvboboMu.Lock()
	defer yhAvboboMu.Unlock()
	if yhAvboboDeviceID == "" {
		const hexd = "0123456789ABCDEF"
		var sb strings.Builder
		var rnd [32]byte
		if _, err := crand.Read(rnd[:]); err != nil {
			for i := range rnd {
				rnd[i] = byte(time.Now().UnixNano() >> uint(i%8))
			}
		}
		for _, b := range rnd {
			sb.WriteByte(hexd[int(b)%16])
		}
		yhAvboboDeviceID = sb.String()
	}
	return yhAvboboDeviceID
}

func yhAvboboUniq() string {
	return "fen.dajiba-8cm.com-" + yhAvboboDevice()
}

// yhAvboboEncodePath replicates q0/c.java's path transform exactly.
func yhAvboboEncodePath(replace string) string {
	if !strings.Contains(replace, "?") ||
		strings.Contains(replace, "/gw/res-bobo/bo/v1/api/app_resources") ||
		strings.Contains(replace, "/gw/res-bobo/bo/v2/api/rank_boards/week") ||
		strings.Contains(replace, "/gw/res-bobo/bo/v2/api/rank_boards/month") {
		return base64.StdEncoding.EncodeToString([]byte(replace))
	}
	parts := strings.SplitN(replace, "?", 2)
	return strings.ReplaceAll(
		base64.StdEncoding.EncodeToString([]byte(parts[0]))+"_"+
			base64.StdEncoding.EncodeToString([]byte(parts[1])), "=", "")
}

func yhAvboboSign(uniq, ts, enc, body string) string {
	plain := uniq + yhAvboboSignMid + ts + "@" + enc + "@" + body + yhAvboboSignTail
	sum := md5.Sum([]byte(plain))
	hexs := fmt.Sprintf("%x", sum)
	for len(hexs) < 32 {
		hexs = "0" + hexs
	}
	return hexs
}

func yhAvboboECBDecrypt(b64body string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64body)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(yhAvboboAESKey)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("yh_avbobo: bad ciphertext length %d", len(raw))
	}
	out := make([]byte, len(raw))
	for i := 0; i < len(raw); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], raw[i:i+aes.BlockSize])
	}
	if len(out) == 0 {
		return out, nil
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return nil, fmt.Errorf("yh_avbobo: bad pkcs7 pad %d", pad)
	}
	return out[:len(out)-pad], nil
}

// yhAvboboRaw performs one interceptor-equivalent request against the
// given host index and returns the decrypted inner JSON object.
func yhAvboboRaw(hostIdx int, method, pre, body string) (map[string]interface{}, http.Header, error) {
	if hostIdx < 0 || hostIdx >= len(yhAvboboHosts) {
		return nil, nil, fmt.Errorf("yh_avbobo: bad host index %d", hostIdx)
	}
	host := yhAvboboHosts[hostIdx]
	enc := yhAvboboEncodePath(pre)
	uniq := yhAvboboUniq()
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	yhAvboboMu.Lock()
	token := yhAvboboToken
	yhAvboboMu.Unlock()
	headers := map[string]string{
		"app_now":    ts,
		"app_sign":   yhAvboboSign(uniq, ts, enc, body),
		"app_unique": uniq,
	}
	if token != "" {
		headers["authorization"] = "Bearer " + token
	} else {
		headers["authorization"] = ""
	}
	var rdr io.Reader
	if body != "" {
		headers["Content-Type"] = "application/json"
		rdr = strings.NewReader(body)
	}
	raw, code, hdr, err := httpDo(method, host+"/s2/"+enc, rdr, headers)
	if err != nil {
		return nil, nil, err
	}
	if code != 200 {
		return nil, hdr, fmt.Errorf("yh_avbobo http %d: %s", code, strings.TrimSpace(string(raw)))
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, hdr, fmt.Errorf("yh_avbobo parse outer: %v", err)
	}
	bodyField := g5StrVal(outer["body"])
	if bodyField == "" {
		// No encrypted envelope (e.g. an error object) — surface it.
		return outer, hdr, nil
	}
	var plain []byte
	if strings.EqualFold(hdr.Get("enc-type"), "base64") {
		plain, err = base64.StdEncoding.DecodeString(bodyField)
		if err != nil {
			return nil, hdr, fmt.Errorf("yh_avbobo base64 body: %v", err)
		}
	} else {
		plain, err = yhAvboboECBDecrypt(bodyField)
		if err != nil {
			return nil, hdr, fmt.Errorf("yh_avbobo aes body: %v", err)
		}
	}
	var inner map[string]interface{}
	if err := json.Unmarshal(plain, &inner); err != nil {
		return nil, hdr, fmt.Errorf("yh_avbobo parse inner: %v", err)
	}
	return inner, hdr, nil
}

// yhAvboboLogin logs in with the host-indexed built-in account body,
// exactly as AvboboLaunchActivity does on launch.
func yhAvboboLogin(hostIdx int) error {
	if hostIdx < 0 || hostIdx >= len(yhAvboboLoginBodies) {
		return fmt.Errorf("yh_avbobo: no login body for host %d", hostIdx)
	}
	inner, _, err := yhAvboboRaw(hostIdx, "POST", "/gw/user-server/web/login", yhAvboboLoginBodies[hostIdx])
	if err != nil {
		return err
	}
	tokens := yhMap(inner["tokens"])
	// LoginInfo shape: {tokens: {token: {token: "<jwt>"},
	// loginToken: {token: "<jwt>"}}}; prefer token, fall back to
	// loginToken, then a flat {token: "..."} variant.
	tok := ""
	if t := yhMap(tokens["token"]); t != nil {
		tok = g5StrVal(t["token"])
	}
	if tok == "" {
		if t := yhMap(tokens["loginToken"]); t != nil {
			tok = g5StrVal(t["token"])
		}
	}
	if tok == "" {
		tok = g5StrVal(inner["token"])
	}
	if tok == "" {
		return fmt.Errorf("yh_avbobo: login response has no token")
	}
	yhAvboboMu.Lock()
	yhAvboboToken = tok
	yhAvboboHostIdx = hostIdx
	yhAvboboMu.Unlock()
	return nil
}

// yhAvboboCall runs a request against the working host, logging in
// first if needed, and rotating hosts once on failure.
func yhAvboboCall(method, pre, body string) (map[string]interface{}, error) {
	yhAvboboMu.Lock()
	hostIdx, token := yhAvboboHostIdx, yhAvboboToken
	yhAvboboMu.Unlock()
	if token == "" {
		loggedIn := false
		if err := yhAvboboLogin(hostIdx); err == nil {
			loggedIn = true
		} else {
			for i := range yhAvboboHosts {
				if i == hostIdx {
					continue
				}
				if yhAvboboLogin(i) == nil {
					hostIdx = i
					loggedIn = true
					break
				}
			}
		}
		if !loggedIn {
			return nil, fmt.Errorf("yh_avbobo login failed on all hosts")
		}
	}
	inner, _, err := yhAvboboRaw(hostIdx, method, pre, body)
	if err != nil {
		// One retry: refresh the token, then rotate hosts once.
		if err2 := yhAvboboLogin(hostIdx); err2 == nil {
			if inner2, _, err3 := yhAvboboRaw(hostIdx, method, pre, body); err3 == nil {
				return inner2, nil
			}
		}
		for i := range yhAvboboHosts {
			if i == hostIdx {
				continue
			}
			if yhAvboboLogin(i) == nil {
				if inner2, _, err3 := yhAvboboRaw(i, method, pre, body); err3 == nil {
					return inner2, nil
				}
			}
		}
		return nil, err
	}
	return inner, nil
}

// yhAvboboCoverURL replicates the cover prefix rule: paths containing
// preview.mp4, /single/images, /src2/v2 or /single/ju take the
// images host, everything else takes the v host. Plain load, no
// transform, so no CoverFetch is needed.
func yhAvboboCoverURL(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if strings.Contains(path, "preview.mp4") || strings.Contains(path, "/single/images") ||
		strings.Contains(path, "/src2/v2") || strings.Contains(path, "/single/ju") {
		return "https://images.bobostrong30.com/enm/bb" + path
	}
	return "https://v.ibobo1.com/enm/tnt" + path
}

func yhAvboboCoverOf(rec map[string]interface{}) string {
	if cf := yhMap(rec["cover_full"]); cf != nil {
		if one := yhMap(cf["01"]); one != nil {
			if local := yhMap(one["local"]); local != nil {
				return yhAvboboCoverURL(g5StrVal(local["path"]))
			}
		}
	}
	return yhAvboboCoverURL(g5StrVal(rec["cover"]))
}

func yhAvboboDurationRemark(rec map[string]interface{}) string {
	sec := 0
	switch v := rec["duration"].(type) {
	case float64:
		sec = int(v)
	case string:
		sec, _ = strconv.Atoi(v)
	}
	if sec > 0 {
		return fmt.Sprintf("%d:%02d", sec/60, sec%60)
	}
	if pc := g5StrVal(rec["playCount"]); pc != "" {
		return pc + "播"
	}
	return ""
}

func yhAvboboCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["title"])
	if id == "" || title == "" {
		return nil, false
	}
	yhAvboboDetailCache.Store(id, rec)
	actors := []string{}
	for _, k := range []string{"actors", "authors", "directors"} {
		for _, a := range yhArr(rec[k]) {
			if s := g5StrVal(a); s != "" {
				actors = append(actors, s)
			}
		}
	}
	desc := strings.Join(actors, " ")
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
	return makeDrama(slot, id, title, yhAvboboCoverOf(rec), yhAvboboDurationRemark(rec), "", desc), true
}

func yhAvboboCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhAvboboCard(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// yhAvboboListQuery builds the app_resources query for a filter
// fragment at its DEFAULT tab values (first entry of every row).
func yhAvboboListQuery(f yhAvboboFilter, page int, keyword string) string {
	groups := []string{}
	for _, g := range f.fixed {
		groups = append(groups, `{"group_id":"`+g+`"}`)
	}
	rowOf := func(name string) []string {
		switch name {
		case "type":
			return f.typ
		case "duration":
			return f.duration
		case "region":
			return f.region
		case "kind":
			return f.kind
		}
		return nil
	}
	for _, v := range f.vars {
		if row := rowOf(v); len(row) > 0 {
			groups = append(groups, `{"group_id":"`+row[0]+`"}`)
		}
	}
	q := `{"$and":[` + strings.Join(groups, ",") + `]}`
	s := `[{"group_id":"` + f.sort[0] + `"}]`
	vals := url.Values{}
	vals.Set("s", s)
	vals.Set("q", q)
	vals.Set("page", strconv.Itoa(page))
	vals.Set("limit", "16")
	if keyword != "" {
		vals.Set("keyword", keyword)
	}
	// Retrofit @QueryMap encodes values; the interceptor then Base64s
	// the whole path+query (app_resources is in the single-Base64
	// branch), so the exact percent-encoding is preserved end to end.
	return "/gw/res-bobo/bo/v1/api/app_resources?" + vals.Encode()
}

func yhAvboboListCat(slot, key string, page int) ([]interface{}, int, bool, error) {
	f, ok := yhAvboboFilters[key]
	if !ok {
		return nil, 0, false, fmt.Errorf("yh_avbobo: unknown filter category %s", key)
	}
	inner, err := yhAvboboCall("GET", yhAvboboListQuery(f, page, ""), "")
	if err != nil {
		return nil, 0, false, err
	}
	items := yhAvboboCards(slot, yhArr(inner["data"]))
	return items, 0, len(items) >= 16, nil
}

func yhAvboboCategories(slot string) []interface{} {
	return []interface{}{
		categoryObj("home:jingxuan", "精选"),
		categoryObj("home:av", "AV"),
		categoryObj("home:xiaoshipin", "小视频"),
		categoryObj("hot", "热门"),
		categoryObj("nocode", "无码"),
		categoryObj("gif", "GIF"),
		categoryObj("cartoon", "动画片"),
		categoryObj("sj", "三级片"),
		categoryObj("rank:day", "日榜"),
		categoryObj("rank:week", "周榜"),
		categoryObj("rank:month", "月榜"),
	}
}

func yhAvboboCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	if group, ok := yhAvboboHomeGroups[category]; ok {
		vals := url.Values{}
		vals.Set("page", strconv.Itoa(page))
		vals.Set("parent_group_id", group)
		vals.Set("needLoadChildren", "1")
		vals.Set("limit", "12")
		inner, err := yhAvboboCall("GET", "/gw/res-bobo/bo/v1/api/query_groups?"+vals.Encode(), "")
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(inner["data"]) {
			row := yhMap(e)
			if row == nil {
				continue
			}
			if strings.Contains(g5StrVal(row["name"]), "女优推荐") {
				continue
			}
			out = append(out, yhAvboboCards(slot, yhArr(row["loadDataItems"]))...)
		}
		return out, 0, len(out) > 0, nil
	}
	if strings.HasPrefix(category, "rank:") {
		typ := strings.TrimPrefix(category, "rank:")
		vals := url.Values{}
		vals.Set("limit", "10")
		vals.Set("page", strconv.Itoa(page))
		inner, err := yhAvboboCall("GET", "/gw/res-bobo/bo/v2/api/rank_boards/"+typ+"?"+vals.Encode(), "")
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(inner["data"]) {
			row := yhMap(e)
			if row == nil {
				continue
			}
			if det := yhMap(row["detail"]); det != nil {
				if c, ok := yhAvboboCard(slot, det); ok {
					out = append(out, c)
				}
			}
		}
		return out, 0, len(out) >= 10, nil
	}
	return yhAvboboListCat(slot, category, page)
}

func yhAvboboDetail(slot, rawID string) (map[string]interface{}, error) {
	inner, err := yhAvboboCall("GET", "/gw/res-bobo/bo/v1/api/app_resources/"+rawID, "")
	if err != nil {
		return nil, err
	}
	// app_resources/{id} returns the MovieDetail itself (possibly
	// wrapped in a data field — tolerate both).
	rec := inner
	if d := yhMap(inner["data"]); d != nil && g5StrVal(d["title"]) != "" {
		rec = d
	}
	if g5StrVal(rec["title"]) == "" {
		return nil, fmt.Errorf("yh_avbobo: empty detail for %s", rawID)
	}
	yhAvboboDetailCache.Store(rawID, rec)
	title := g5StrVal(rec["title"])
	d := makeDrama(slot, rawID, title, yhAvboboCoverOf(rec), yhAvboboDurationRemark(rec), "", "")
	if v := g5StrVal(rec["playCount"]); v != "" {
		d["playCount"] = v
	}
	actors := []string{}
	for _, k := range []string{"actors", "authors", "directors"} {
		for _, a := range yhArr(rec[k]) {
			if s := g5StrVal(a); s != "" {
				actors = append(actors, s)
			}
		}
	}
	if len(actors) > 0 {
		d["actor"] = strings.Join(actors, " ")
		d["vod_actor"] = strings.Join(actors, " ")
	}
	descParts := []string{}
	for _, k := range []string{"big_types", "tags"} {
		for _, t := range yhArr(rec[k]) {
			if s := g5StrVal(t); s != "" {
				descParts = append(descParts, s)
			}
		}
	}
	if len(descParts) > 0 {
		d["desc"] = strings.Join(descParts, " ")
		d["vod_content"] = strings.Join(descParts, " ")
	}
	types := []string{}
	for _, t := range yhArr(rec["types"]) {
		if s := g5StrVal(t); s != "" {
			types = append(types, s)
		}
	}
	if len(types) == 0 {
		return nil, fmt.Errorf("yh_avbobo: 暂无播放源 for %s", rawID)
	}
	epIDs := []string{}
	epNames := []string{}
	for _, t := range types {
		epIDs = append(epIDs, rawID+"$"+t)
		epNames = append(epNames, t)
	}
	g5SetChapters(d, slot, rawID, epIDs, epNames)
	// Guess-like recommendations (app_resources_recommend/{id}) are
	// fetched lazily by the player UI; expose the endpoint's items
	// under "related" for shells that show them.
	if rec2, err := yhAvboboCall("GET", "/gw/res-bobo/bo/v2/api/app_resources_recommend/"+rawID, ""); err == nil {
		if rel := yhAvboboCards(slot, yhArr(rec2["data"])); len(rel) > 0 {
			d["related"] = rel
		}
	}
	return d, nil
}

func yhAvboboResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	sharpness := ""
	if i := strings.Index(vid, "$"); i >= 0 {
		sharpness = vid[i+1:]
		vid = vid[:i]
	}
	rec := map[string]interface{}(nil)
	if v, ok := yhAvboboDetailCache.Load(vid); ok {
		rec = v.(map[string]interface{})
	}
	if rec == nil {
		if _, err := yhAvboboDetail(slot, vid); err != nil {
			return nil, err
		}
		if v, ok := yhAvboboDetailCache.Load(vid); ok {
			rec = v.(map[string]interface{})
		}
	}
	if rec == nil {
		return nil, fmt.Errorf("yh_avbobo: no detail for %s", vid)
	}
	types := []string{}
	for _, t := range yhArr(rec["types"]) {
		if s := g5StrVal(t); s != "" {
			types = append(types, s)
		}
	}
	if len(types) == 0 {
		return nil, fmt.Errorf("yh_avbobo: 暂无播放源 for %s", vid)
	}
	if sharpness == "" {
		sharpness = types[len(types)-1]
	}
	lineVersion := g5StrVal(rec["line_version"])
	if lineVersion == "" {
		lineVersion = "res-v1"
	}
	lineID := "11"
	playMap, err := yhAvboboCall("GET",
		"/gw/res-bobo/bo/v2/api/play/"+lineVersion+"/"+vid+"/"+sharpness+"/"+lineID, "")
	if err != nil {
		return nil, err
	}
	keys := []string{}
	for k := range playMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, k+"="+g5StrVal(playMap[k]))
	}
	yhAvboboMu.Lock()
	hostIdx := yhAvboboHostIdx
	yhAvboboMu.Unlock()
	u := yhAvboboHosts[hostIdx] + "/gw/res-bobo/bo/v2/hls/" + lineVersion + "/" +
		vid + "/" + sharpness + "/" + lineID + ".m3u8?" + strings.Join(parts, "&")
	return playResult(u, slot, rawID, yhAvboboUA, yhAvboboHosts[hostIdx]+"/", "m3u8", "hls"), nil
}

func yhAvboboSearch(slot, kw string) ([]interface{}, error) {
	f := yhAvboboFilters["search"]
	inner, err := yhAvboboCall("GET", yhAvboboListQuery(f, 1, kw), "")
	if err != nil {
		return nil, err
	}
	return yhAvboboCards(slot, yhArr(inner["data"])), nil
}

func yhAvboboHeaders() map[string]string {
	return map[string]string{"User-Agent": yhAvboboUA}
}

func init() {
	registerSite(yhAvboboSlot, &siteImpl{
		Key:        yhAvboboSlot,
		Name:       "AVbobo",
		DefaultCat: "hot",
		HomeURL:    yhAvboboHosts[0] + "/",
		Headers:    yhAvboboHeaders,
		Categories: yhAvboboCategories,
		Catalog:    yhAvboboCatalog,
		Detail:     yhAvboboDetail,
		Resolve:    yhAvboboResolve,
		Search:     yhAvboboSearch,
	})
}
