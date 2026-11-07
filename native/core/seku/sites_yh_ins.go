package seku

// ---- INS 社区 ins (yh_ins) — 原生 JSON API + AES 响应解密 ----
//
// 配方来源：欲火 APK jadx 实证（模块 ins；旧报告「纯 H5 壳」已由
//   本轮深读推翻：H5 入口只用于嗅探 Authorization 令牌，内容全部
//   走 Retrofit x2.a 接口）
//   基址：x2.d 的 Retrofit base = h6.b.f8271s0 =
//   http://kakzpm.ins164.com:29909；H5 入口 = h6.b.f8273t0 =
//   https://nkwaey.top（Config 接口可下发新 h5Domain）。
//   接口（x2.a；ud.f=GET、ud.u=QueryMap、ud.i=Header）：
//     GET /party/common/generals/public/getGeneral（公开，返回
//       Config{h5Domain…}，响应不解密）
//     GET /party/common/domain/private/domain（公开，不解密）
//     GET /post/app/p/post/private/type/page?sortType=1&page={n}
//         &limit=20&sort=asc&type=1&tagId={tagId}
//         （VideoItemFragment.j 原路；type=1 为视频）
//     GET /post/app/p/post/private/search/page?page={n}&size=20
//         &sort=asc&type=1&content={kw}（SearchFragment 原路）
//     GET /post/app/p/post/private/info/{id}（PlayActivity 原路）
//   全部私域接口带 Header Authorization: <f8281x0>。
//   响应解密（x2.c InsInterceptor 原路，除 domain/getGeneral 两
//   端点外）：取 JSON 的 data 字段（Base64）→ AES/CBC/PKCS7
//   解密，key = "q3t6w9z$C&F)J@NcRfUjXn2r5u8x/A?D"，
//   IV = "G+KbPeShVmYq3t6v"；解密失败时按原文返回（拦截器同款
//   回退，本件在解密失败时报错并注明原文形态）。
//   令牌（关键卡点）：f8281x0 不是写死值——App 在 InsLaunch 的
//   WebView 里嗅探 H5 页面自身发出的、URL 含 user/app/u/user、
//   /getGeneral 或 user/app/u/visit 的请求的 Authorization 请求头，
//   存 CookiePrefs/cookie_ins 3 天内复用（v2.b）。本件以公开端点
//   + visit 探针尝试自举令牌（响应体 token 字段或响应头
//   Authorization）；自举失败时 Catalog/Search/Detail/Resolve
//   如实报错，不伪造令牌、不调用 H5 嗅探之外的凭据。旧报告称 ins
//   依赖 pixiv Bearer（h6.b K）——该描述对应 as2605/isiyu 族，
//   ins 模块内实际使用的是上述 H5 嗅探令牌，特此纠正。
//   播放（VideoAdapter/PlayActivity 原路）：取 files[0]
//   .postVideoFile.postparam.play_url；封面取 files[0].coverUrl。
//   付费（guide 验证门）：列表过滤 isPay/whetherPay/payAmount/
//   subscriptionPrice 任一非空非零的帖；Detail 照常返回信息，
//   Resolve 在付费帖上拒解析。
//   分类（VideoFragment，1:1，视频区）：大V（女神用户页，非视频
//   列表，本件不纳入）+ 热门 121 / 国产 60 / AI明星换脸 138 /
//   综艺 135 / 萝莉 63 / AV 61 / 动漫 65 / 欧美 127 / 韩国 124。
//   图片区（ImageFragment tag 155/146/11-14/145）非视频，本件
//   不纳入。
//
// 验证门（逐腿，2026-10-24）：
//   Categories/接口/解密/付费规则：逐行按 jadx 复刻 ✔（代码实证）。
//   Catalog/Detail/Search/Resolve/封面魔数：本机 shell 出口对
//   kakzpm.ins164.com:29909 全灭（代理 fake-IP、连接超时），
//   浏览器文本抓取 getGeneral 回 403（无 App 级请求头）；Google
//   DoH 证实 API 域名解析正常（54.46.6.160）——故标「出口未验」，
//   且令牌自举路径未活体校准，待可用出口/真机嗅探后补验。

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const yhInsBase = "http://kakzpm.ins164.com:29909"
const yhInsUA = "Mozilla/5.0 (Linux; Android 8.0.0; SM-G955U Build/R16NW) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Mobile Safari/537.36 Edg/106.0.0.0"

var yhInsKey = []byte("q3t6w9z$C&F)J@NcRfUjXn2r5u8x/A?D")
var yhInsIV = []byte("G+KbPeShVmYq3t6v")

var yhInsTokMu sync.Mutex
var yhInsToken string
var yhInsPlayPrefix = "https://ins.m3.66buks.top"
var yhInsPrefixMu sync.Mutex

func yhInsPlayPrefixURL() string {
	yhInsPrefixMu.Lock()
	defer yhInsPrefixMu.Unlock()
	return yhInsPlayPrefix
}

func yhInsAbsPlayURL(u string) string {
	if u == "" || strings.HasPrefix(u, "http") {
		return u
	}
	prefix := yhInsPlayPrefixURL()
	if strings.HasPrefix(u, "/") {
		return prefix + u
	}
	return prefix + "/" + u
}

func yhInsDomainPrefix(obj map[string]interface{}) string {
	get := func(m map[string]interface{}) string {
		if m == nil {
			return ""
		}
		for _, k := range []string{"domain", "url", "host", "value"} {
			if s := g5StrVal(m[k]); s != "" {
				if !strings.HasPrefix(s, "http") {
					s = "https://" + strings.TrimPrefix(s, "/")
				}
				if u, err := url.Parse(s); err == nil && u.Host != "" {
					return u.Scheme + "://" + u.Host
				}
				return s
			}
		}
		return ""
	}
	isType1 := func(m map[string]interface{}) bool {
		if m == nil {
			return false
		}
		if v, ok := g5MapInt(m, "type"); ok && v == 1 {
			return true
		}
		return g5StrVal(m["type"]) == "1"
	}
	if dm := yhMap(obj["data"]); dm != nil {
		if isType1(dm) {
			return get(dm)
		}
	}
	for _, e := range yhArr(obj["data"]) {
		if m := yhMap(e); m != nil && isType1(m) {
			if s := get(m); s != "" {
				return s
			}
		}
	}
	return ""
}

func yhInsHeaders() map[string]string {
	h := map[string]string{
		"User-Agent":      yhInsUA,
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9",
	}
	if ct := CrackVipToken("yh_ins"); ct != "" {
		h["Authorization"] = ct
	} else {
		yhInsTokMu.Lock()
		t := yhInsToken
		yhInsTokMu.Unlock()
		if t != "" {
			h["Authorization"] = t
		}
	}
	return h
}

// yhInsDecrypt：x2.c.b 原路（AES/CBC/PKCS7，失败返回原文错误）。
func yhInsDecrypt(enc string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(enc))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(yhInsKey)
	if err != nil {
		return nil, err
	}
	if len(raw)%aes.BlockSize != 0 || len(raw) == 0 {
		return nil, fmt.Errorf("yh_ins: bad cipher length %d", len(raw))
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhInsIV).CryptBlocks(out, raw)
	if len(out) == 0 {
		return nil, fmt.Errorf("yh_ins: empty decrypt")
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return nil, fmt.Errorf("yh_ins: bad pkcs7 pad")
	}
	return out[:len(out)-pad], nil
}

// yhInsCall：GET + 解包。plain=true 时不解密（domain/getGeneral）。
func yhInsCall(path string, plain bool) (map[string]interface{}, http.Header, error) {
	b, code, hdr, err := httpGet(yhInsBase+path, yhInsHeaders())
	if err != nil {
		return nil, nil, err
	}
	if code != 200 {
		return nil, hdr, fmt.Errorf("yh_ins http %d for %s", code, path)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, hdr, fmt.Errorf("yh_ins parse: %v", err)
	}
	if plain {
		return obj, hdr, nil
	}
	// 解密 data 字段；解密结果可能是内层 data 对象或完整信封。
	if s, ok := obj["data"].(string); ok && s != "" {
		dec, derr := yhInsDecrypt(s)
		if derr != nil {
			return nil, hdr, derr
		}
		var inner interface{}
		if err := json.Unmarshal(dec, &inner); err != nil {
			return nil, hdr, fmt.Errorf("yh_ins decrypt parse: %v", err)
		}
		if im, ok := inner.(map[string]interface{}); ok {
			if dm := yhMap(im["data"]); dm != nil {
				return dm, hdr, nil
			}
			return im, hdr, nil
		}
		return nil, hdr, fmt.Errorf("yh_ins: decrypted data not object")
	}
	if dm := yhMap(obj["data"]); dm != nil {
		return dm, hdr, nil
	}
	return obj, hdr, nil
}

// yhInsBootstrap：尝试自举 guest 令牌（公开端点 + visit 探针）。
func yhInsBootstrap() string {
	yhInsTokMu.Lock()
	if yhInsToken != "" {
		t := yhInsToken
		yhInsTokMu.Unlock()
		return t
	}
	yhInsTokMu.Unlock()
	try := func(obj map[string]interface{}, hdr http.Header) string {
		for _, k := range []string{"token", "authorization", "Authorization", "accessToken", "authToken"} {
			if s := g5StrVal(obj[k]); s != "" {
				return s
			}
		}
		if d := yhMap(obj["data"]); d != nil {
			for _, k := range []string{"token", "authorization", "Authorization", "accessToken", "authToken"} {
				if s := g5StrVal(d[k]); s != "" {
					return s
				}
			}
		}
		if r := yhMap(obj["response"]); r != nil {
			for _, k := range []string{"token", "authorization", "Authorization", "accessToken", "authToken"} {
				if s := g5StrVal(r[k]); s != "" {
					return s
				}
			}
		}
		if hdr != nil {
			if s := hdr.Get("Authorization"); s != "" {
				return s
			}
		}
		return ""
	}
	for _, p := range []string{"/party/common/generals/public/getGeneral", "/party/common/domain/private/domain", "/user/app/u/visit"} {
		obj, hdr, err := yhInsCall(p, strings.Contains(p, "generals") || strings.Contains(p, "domain"))
		if err != nil || obj == nil {
			continue
		}
		if strings.Contains(p, "domain") {
			if prefix := yhInsDomainPrefix(obj); prefix != "" {
				yhInsPrefixMu.Lock()
				yhInsPlayPrefix = prefix
				yhInsPrefixMu.Unlock()
			}
		}
		if t := try(obj, hdr); t != "" {
			yhInsTokMu.Lock()
			yhInsToken = t
			yhInsTokMu.Unlock()
			return t
		}
	}
	return ""
}

func yhInsAuth() (string, error) {
	if t := yhInsBootstrap(); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("yh_ins: no guest Authorization token (App sniffs it from H5 requests; bootstrap uncalibrated)")
}

func yhInsPlayURL(rec map[string]interface{}) string {
	for _, e := range yhArr(rec["files"]) {
		fm := yhMap(e)
		if fm == nil {
			continue
		}
		if pv := yhMap(fm["postVideoFile"]); pv != nil {
			if pp := yhMap(pv["postparam"]); pp != nil {
				if u := g5StrVal(pp["play_url"]); u != "" {
					return yhInsAbsPlayURL(u)
				}
			}
		}
		if u := g5StrVal(fm["url"]); u != "" {
			return yhInsAbsPlayURL(u)
		}
	}
	return ""
}

func yhInsCover(rec map[string]interface{}) string {
	for _, e := range yhArr(rec["files"]) {
		fm := yhMap(e)
		if fm == nil {
			continue
		}
		if c := g5StrVal(fm["coverUrl"]); c != "" {
			return c
		}
	}
	return ""
}

func yhInsCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range recs {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		id := g5StrVal(rec["postId"])
		title := g5StrVal(rec["title"])
		if title == "" {
			title = g5StrVal(rec["content"])
		}
		if id == "" || title == "" || seen[id] {
			continue
		}
		seen[id] = true
		remark := ""
		if v, ok := g5MapInt(rec, "viewCount"); ok && v > 0 {
			remark = fmt.Sprintf("%d播放", v)
		}
		tags := []string{}
		for _, t := range yhArr(rec["postTags"]) {
			tm := yhMap(t)
			if tm != nil {
				if s := g5StrVal(tm["name"]); s != "" {
					tags = append(tags, s)
				}
			}
		}
		out = append(out, makeDrama(slot, id, title, yhInsCover(rec), remark, strings.Join(tags, " "), g5StrVal(rec["content"])))
	}
	return out
}

var yhInsCats = []struct{ id, name string }{
	{"121", "热门"},
	{"60", "国产"},
	{"138", "AI明星换脸"},
	{"135", "综艺"},
	{"63", "萝莉"},
	{"61", "AV"},
	{"65", "动漫"},
	{"127", "欧美"},
	{"124", "韩国"},
}

func yhInsCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range yhInsCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func yhInsCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	if category == "" {
		category = "121"
	}
	if _, err := yhInsAuth(); err != nil {
		return nil, 0, false, err
	}
	q := fmt.Sprintf("/post/app/p/post/private/type/page?sortType=1&page=%d&limit=20&sort=asc&type=1&tagId=%s", page, url.QueryEscape(category))
	data, _, err := yhInsCall(q, false)
	if err != nil {
		return nil, 0, false, err
	}
	items := yhInsCards(slot, yhArr(data["list"]))
	total, _ := g5MapInt(data, "total")
	hasMore := false
	if v, ok := g5MapInt(data, "totalPageNum"); ok {
		hasMore = page < v
	} else {
		hasMore = len(items) == 20
	}
	return items, total, hasMore, nil
}

func yhInsInfo(id string) (map[string]interface{}, error) {
	if _, err := yhInsAuth(); err != nil {
		return nil, err
	}
	data, _, err := yhInsCall("/post/app/p/post/private/info/"+url.PathEscape(id), false)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func yhInsDetail(slot, rawID string) (map[string]interface{}, error) {
	id, _ := splitEpisodeSuffix(rawID)
	data, err := yhInsInfo(id)
	if err != nil {
		return nil, err
	}
	title := g5StrVal(data["title"])
	if title == "" {
		title = g5StrVal(data["content"])
	}
	if title == "" {
		title = "视频" + id
	}
	d := makeDrama(slot, id, title, yhInsCover(data), "", "", g5StrVal(data["content"]))
	if u := yhInsPlayURL(data); u != "" {
		g5SetChapters(d, slot, id, []string{id}, []string{"正片"})
		_ = u
	}
	return d, nil
}

func yhInsResolve(slot, rawID string) (map[string]interface{}, error) {
	id, _ := splitEpisodeSuffix(rawID)
	data, err := yhInsInfo(id)
	if err != nil {
		return nil, err
	}
	u := yhInsPlayURL(data)
	if u == "" {
		return nil, fmt.Errorf("yh_ins: no play url for %s", id)
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhInsUA, yhInsBase+"/", format, protocol), nil
}

func yhInsSearch(slot, kw string) ([]interface{}, error) {
	if _, err := yhInsAuth(); err != nil {
		return nil, err
	}
	q := fmt.Sprintf("/post/app/p/post/private/search/page?page=1&size=20&sort=asc&type=1&content=%s", url.QueryEscape(kw))
	data, _, err := yhInsCall(q, false)
	if err != nil {
		return nil, err
	}
	return yhInsCards(slot, yhArr(data["list"])), nil
}

func init() {
	registerSite("yh_ins", &siteImpl{
		Key:        "yh_ins",
		Name:       "INS社区",
		DefaultCat: "121",
		HomeURL:    "https://nkwaey.top/",
		Categories: yhInsCategories,
		Catalog:    yhInsCatalog,
		Detail:     yhInsDetail,
		Resolve:    yhInsResolve,
		Search:     yhInsSearch,
		Headers:    yhInsHeaders,
	}, "yh_ins", "ins", "INS社区")
}
