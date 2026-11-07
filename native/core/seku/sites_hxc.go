package seku

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 含羞草 — 配方出 ZakaTV jar ZakaHxcAmns 反编译：
//   - 请求信封：POST {"endata": AES-256-CBC(参数JSON), "ents": AES(服务器秒)}
//     密钥为 ASCII 串 B77A9FF7F323B5404902102257503C2F（32B 即 AES-256），
//     IV 取同串前 16 字节；响应是明文 JSON，code==0 取 data。
//   - 时间同步：/base/getTimeStamp 给 data.timeStamp，偏移实测约
//     -28800 秒（服务端时钟帧），同步失败按 jar 兜底 -28800。
//   - 封面若为 .aes 地址，字节是 AES/ECB/NoPadding 加密，密钥
//     "46cc793c53dc451b"（jar $RProxy 实测），CoverFetch 解开。
//   - 播放走站方 /videos/getPreUrl 返回的 url 原样播。
//
// 边界（账本类口径，如实记录）：jar 的 widen() 把预告 url 的
// start/end 范围参数改写成全片（start=0&end=999999）——那是篡改
// 站方限时预告范围绕付费，本移植不做；只播站方原样签发的预告/
// 播放地址，免费片即全片、付费片为站方预告。
//
// 2026-10-24 本机活验：getTimeStamp code 0、getType code 0 回
// 5 个真分类（国产/主播/日韩/欧美/动漫）。

var hxcHosts = []string{"https://a48j.jot4v.com", "https://a83k.aqx84.com"}

const hxcUA = "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

var hxcKey = []byte("B77A9FF7F323B5404902102257503C2F")
var hxcPicKey = []byte("46cc793c53dc451b")

var hxcState = struct {
	sync.Mutex
	hostIdx  int
	tsOffset int64
	tsSynced bool
	cats     []tokenAPICat
	catsAt   time.Time
}{}

var hxcClient = &http.Client{Timeout: 25 * time.Second, Transport: sharedTransport}

func hxcEnc(plain string) string {
	block, err := aes.NewCipher(hxcKey)
	if err != nil {
		return ""
	}
	src := []byte(plain)
	pad := aes.BlockSize - len(src)%aes.BlockSize
	for i := 0; i < pad; i++ {
		src = append(src, byte(pad))
	}
	out := make([]byte, len(src))
	cipher.NewCBCEncrypter(block, hxcKey[:16]).CryptBlocks(out, src)
	return base64.StdEncoding.EncodeToString(out)
}

func hxcHost() string {
	hxcState.Lock()
	defer hxcState.Unlock()
	return hxcHosts[hxcState.hostIdx%len(hxcHosts)]
}

func hxcPost(host, path string, body []byte) (string, error) {
	req, err := http.NewRequest("POST", host+path, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", hxcUA)
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8;")
	req.Header.Set("Did", "1")
	req.Header.Set("source", "1")
	req.Header.Set("Origin", host)
	req.Header.Set("Referer", host+"/")
	resp, err := hxcClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func hxcServerSec() int64 {
	hxcState.Lock()
	if !hxcState.tsSynced {
		hxcState.tsSynced = true
		hxcState.tsOffset = -28800 // jar 兜底值；同步成功后覆盖
		local := time.Now().Unix()
		body, _ := json.Marshal(map[string]string{
			"endata": hxcEnc("{}"),
			"ents":   hxcEnc(strconv.FormatInt(local, 10)),
		})
		for _, h := range hxcHosts {
			txt, err := hxcPost(h, "/base/getTimeStamp", body)
			if err != nil || txt == "" {
				continue
			}
			var resp map[string]interface{}
			if json.Unmarshal([]byte(txt), &resp) == nil {
				if data := taMap(resp["data"]); data != nil {
					if ts := int64(taInt(data["timeStamp"])); ts > 0 {
						hxcState.tsOffset = ts - local
					}
				}
			}
			break
		}
	}
	off := hxcState.tsOffset
	hxcState.Unlock()
	return time.Now().Unix() + off
}

// hxcAPI performs one enveloped API call; returns the data node.
func hxcAPI(path string, params map[string]interface{}) (interface{}, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	pj, _ := json.Marshal(params)
	do := func() (interface{}, int, string, error) {
		body, _ := json.Marshal(map[string]string{
			"endata": hxcEnc(string(pj)),
			"ents":   hxcEnc(strconv.FormatInt(hxcServerSec(), 10)),
		})
		var lastErr error
		for i := 0; i < len(hxcHosts); i++ {
			host := hxcHost()
			txt, err := hxcPost(host, path, body)
			if err == nil && txt != "" {
				var resp map[string]interface{}
				if json.Unmarshal([]byte(txt), &resp) == nil {
					code := taInt(resp["code"])
					if _, has := resp["code"]; has {
						if code == 0 {
							return resp["data"], 0, "", nil
						}
						return nil, code, g5StrVal(resp["msg"]), nil
					}
				}
			}
			lastErr = err
			hxcState.Lock()
			hxcState.hostIdx++
			hxcState.Unlock()
		}
		return nil, -1, "", lastErr
	}
	data, code, msg, err := do()
	if err == nil && (code == 2001 || code == 2003 || code == 5001 || code == 9001 || code == 2021) {
		// 时间类错误码：强制重同步后重试一次（jar 同款集合）
		hxcState.Lock()
		hxcState.tsSynced = false
		hxcState.Unlock()
		data, code, msg, err = do()
	}
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("hxc %s code %d %s", path, code, msg)
	}
	return data, nil
}

// hxcDur formats a length in seconds (API 实测：全片 2214、短片 37).
func hxcDur(sec int) string {
	if sec >= 3600 {
		return fmt.Sprintf("%d小时%d分", sec/3600, (sec%3600)/60)
	}
	if sec >= 60 {
		return fmt.Sprintf("%d分%d秒", sec/60, sec%60)
	}
	return fmt.Sprintf("%d秒", sec)
}

func hxcCard(v map[string]interface{}, prefix string) (map[string]interface{}, bool) {
	id := taInt(v["id"])
	name := g5StrVal(v["name"])
	if id <= 0 || name == "" {
		return nil, false
	}
	remark := ""
	if ln := taInt(v["length"]); ln > 0 {
		remark = hxcDur(ln)
	}
	return makeDrama("hxc", prefix+strconv.Itoa(id), name, g5StrVal(v["coverImgUrl"]), remark, "", ""), true
}

func hxcCategories() []tokenAPICat {
	hxcState.Lock()
	cached, at := hxcState.cats, hxcState.catsAt
	hxcState.Unlock()
	if cached != nil && time.Since(at) < 10*time.Minute {
		return cached
	}
	cats := []tokenAPICat{}
	if data, err := hxcAPI("/videos/getType", nil); err == nil {
		if dm := taMap(data); dm != nil {
			for _, e := range taArr(dm["list"]) {
				m := taMap(e)
				if m == nil {
					continue
				}
				if id := taInt(m["id"]); id > 0 && g5StrVal(m["type_name"]) != "" {
					cats = append(cats, tokenAPICat{strconv.Itoa(id), g5StrVal(m["type_name"])})
				}
			}
		}
	}
	cats = append(cats, tokenAPICat{"s:1", "专题"}, tokenAPICat{"p:1", "量贩"})
	hxcState.Lock()
	hxcState.cats, hxcState.catsAt = cats, time.Now()
	hxcState.Unlock()
	return cats
}

func hxcCatalog(catID string, page int) ([]map[string]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	var data interface{}
	var err error
	prefix := "m:"
	if strings.HasPrefix(catID, "s:") || strings.HasPrefix(catID, "p:") {
		prefix = catID[:2]
		data, err = hxcAPI("/videoPackage/getList", map[string]interface{}{"page": page, "length": 24})
	} else {
		tid, _ := strconv.Atoi(catID)
		data, err = hxcAPI("/videos/getList", map[string]interface{}{"page": page, "length": 24, "typeId": tid})
	}
	if err != nil {
		return nil, 0, false, err
	}
	dm := taMap(data)
	if dm == nil {
		return nil, 0, false, fmt.Errorf("hxc: bad list data")
	}
	items := []map[string]interface{}{}
	for _, e := range taArr(dm["list"]) {
		if m := taMap(e); m != nil {
			if item, ok := hxcCard(m, prefix); ok {
				items = append(items, item)
			}
		}
	}
	total := taInt(dm["total"])
	if total == 0 {
		total = taInt(dm["count"])
	}
	return items, total, len(items) >= 24, nil
}

func hxcDetail(rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "s:") || strings.HasPrefix(rawID, "p:") {
		id, _ := strconv.Atoi(rawID[2:])
		data, err := hxcAPI("/videoPackage/getDetail", map[string]interface{}{"videoPackageId": id})
		if err != nil {
			return nil, err
		}
		dm := taMap(data)
		if dm == nil {
			return nil, fmt.Errorf("hxc: package detail empty")
		}
		name := g5StrVal(dm["name"])
		if name == "" {
			name = g5StrVal(dm["title"])
		}
		d := map[string]interface{}{"name": name}
		if pic := g5StrVal(dm["coverImgUrl"]); pic != "" {
			d["pic"] = pic
		}
		d["desc"] = g5StrVal(dm["intro"])
		// 合集内视频作选集，逐集走 getPreUrl
		var vids []interface{}
		for _, key := range []string{"list", "videos", "videoList"} {
			if arr := taArr(dm[key]); len(arr) > 0 {
				vids = arr
				break
			}
		}
		epIDs, epNames := []string{}, []string{}
		related := []interface{}{}
		for _, e := range vids {
			m := taMap(e)
			if m == nil {
				continue
			}
			if item, ok := hxcCard(m, "m:"); ok {
				epIDs = append(epIDs, g5StrVal(item["id"]))
				epNames = append(epNames, g5StrVal(m["name"]))
				related = append(related, item)
			}
		}
		if len(epIDs) > 0 {
			// 章节 id 用裸 m:{vid}（去掉 slot 前缀）
			raw := make([]string, len(epIDs))
			for i, full := range epIDs {
				raw[i] = strings.TrimPrefix(full, "hxc:")
			}
			g5SetChapters(d, "hxc", rawID, raw, epNames)
		}
		return d, nil
	}
	id, _ := strconv.Atoi(strings.TrimPrefix(rawID, "m:"))
	data, err := hxcAPI("/videos/getInfo", map[string]interface{}{"videoSort": 1, "videoId": id})
	if err != nil {
		return nil, err
	}
	dm := taMap(data)
	if dm == nil {
		return nil, fmt.Errorf("hxc: getInfo empty for %s", rawID)
	}
	// 活体实证：视频字段在 data.info 子对象里，外层是权限/价格元信息
	if info := taMap(dm["info"]); info != nil {
		dm = info
	}
	name := g5StrVal(dm["name"])
	d := map[string]interface{}{"name": name}
	if pic := g5StrVal(dm["coverImgUrl"]); pic != "" {
		d["pic"] = pic
	}
	var sb strings.Builder
	sb.WriteString(name)
	if ln := taInt(dm["length"]); ln > 0 {
		sb.WriteString("\n时长：" + hxcDur(ln))
	}
	if at := g5StrVal(dm["addTime"]); at != "" {
		sb.WriteString("\n时间：" + at)
		if len(at) >= 4 {
			d["year"] = at[:4]
		}
	}
	d["desc"] = sb.String()
	g5SetChapters(d, "hxc", rawID, []string{"m:" + strconv.Itoa(id)}, []string{"正片"})
	return d, nil
}

func hxcResolve(playID string) (map[string]interface{}, error) {
	id, _ := strconv.Atoi(strings.TrimPrefix(playID, "m:"))
	if id <= 0 {
		return nil, fmt.Errorf("hxc: bad play id %s", playID)
	}
	data, err := hxcAPI("/videos/getPreUrl", map[string]interface{}{"videoId": id})
	if err != nil {
		return nil, err
	}
	dm := taMap(data)
	u := ""
	if dm != nil {
		u = g5StrVal(dm["url"])
	}
	if len(u) < 12 {
		return nil, fmt.Errorf("hxc: getPreUrl empty for %d", id)
	}
	// 站方原样签发地址，不改 start/end 范围参数（见文件头边界注）
	format := "mp4"
	if strings.Contains(u, ".m3u8") {
		format = "hls"
	}
	return playResult(u, "hxc", playID, hxcUA, hxcHost()+"/", "", format), nil
}

func hxcSearch(keyword string) ([]map[string]interface{}, error) {
	items := []map[string]interface{}{}
	for _, typ := range []int{1, 3} {
		data, err := hxcAPI("/base/globalSearch", map[string]interface{}{
			"key": keyword, "type": typ, "page": 1, "length": 24,
		})
		if err != nil {
			continue
		}
		dm := taMap(data)
		if dm == nil {
			continue
		}
		prefix := "m:"
		if typ == 3 {
			prefix = "s:"
		}
		arr := taArr(dm["infos"])
		if len(arr) == 0 {
			arr = taArr(dm["list"])
		}
		for _, e := range arr {
			if m := taMap(e); m != nil {
				if item, ok := hxcCard(m, prefix); ok {
					items = append(items, item)
				}
			}
		}
	}
	return items, nil
}

func hxcCoverFetch(rawURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", hxcUA)
	req.Header.Set("Accept", "image/*,*/*")
	req.Header.Set("Referer", "https://www.o5ljx.com/")
	resp, err := hxcClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, err
	}
	if !strings.Contains(rawURL, ".aes") || len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return b, nil
	}
	if mime := sniffImageMime(b); mime != "" {
		return b, nil
	}
	block, err := aes.NewCipher(hxcPicKey)
	if err != nil {
		return b, nil
	}
	out := make([]byte, len(b))
	for i := 0; i < len(b); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], b[i:i+aes.BlockSize])
	}
	// NoPadding：尾部零填充截断
	out = bytes.TrimRight(out, "\x00")
	if len(out) == 0 {
		return b, nil
	}
	return out, nil
}

func init() {
	registerSite("hxc", &siteImpl{
		Key: "hxc", Name: "含羞草", DefaultCat: "4", HomeURL: hxcHosts[0],
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			for _, c := range hxcCategories() {
				out = append(out, categoryObj(c.id, c.name))
			}
			return out
		},
		Catalog: func(slot, category string, page int) ([]interface{}, int, bool, error) {
			items, total, hasMore, err := hxcCatalog(category, page)
			if err != nil {
				return nil, 0, false, err
			}
			out := make([]interface{}, 0, len(items))
			for _, it := range items {
				out = append(out, it)
			}
			return out, total, hasMore, nil
		},
		Detail:  func(slot, rawID string) (map[string]interface{}, error) { return hxcDetail(rawID) },
		Resolve: func(slot, rawID string) (map[string]interface{}, error) { return hxcResolve(rawID) },
		Search: func(slot, kw string) ([]interface{}, error) {
			items, err := hxcSearch(kw)
			if err != nil {
				return nil, err
			}
			out := make([]interface{}, 0, len(items))
			for _, it := range items {
				out = append(out, it)
			}
			return out, nil
		},
		CoverFetch: hxcCoverFetch,
		Headers:    func() map[string]string { return map[string]string{"User-Agent": hxcUA} },
	}, "hxc")
}
