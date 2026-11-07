package seku

// ---- 海角社区 haijiao (yh_haijiao, 欲火 type 72) — 签名 API, 逐行复刻 ----
//
// 基址 https://api1.ogokfmrx.com (f2.d NetHelper 用 h6.b.B0; 未见远程
// 配置覆盖该值, 2026-10-24 实取 log3.log 亦无覆盖)。
// 任务所问 "模块内 1 个 hex key": 5589d41f92a597d016b037ac37db243d,
// 即签名盐, 拼在 sign 明文末尾 (见下), 非加解密密钥。
// 请求链 (f2.c HaiJiaoUtils + f2.b 拦截器, 经 ac/i.java 与 io/realm/
// p2.java 的混淆 HashMap 链逐项解码核实):
//   外层 c.a(): {client:"pwa", timestamp:当前秒串}。
//   内层 c.b(): 随机取 0-4 号三元组 (oauth_id/sid/token, 见 yhHaijiaoTriples,
//     APK 内嵌值) + bundleId:"com.pwa.hjsq" + version:"4.1.5" +
//     oauth_type:"web" + language:"zh" + via:"pwa" + _hash:当前秒串,
//     再并各端点附加字段; 整体 Gson JSON 后 AES/CBC/PKCS7
//     (key "2acf7e91e9864673", IV "1c29882d3ddfcfd6") 加密, Base64 得
//     外层 data 字段。
//   签名: signInput = "client=pwa&data=" + data + "&timestamp=" +
//     timestamp + "5589d41f92a597d016b037ac37db243d";
//     sign = MD5hex(SHA256hex(signInput) 的 UTF-8 字节)。
//     即先 SHA-256 取小写 hex 串, 再对该串做 MD5 (c.d 定义)。
//   外层表单 {client, timestamp, data, sign}; 拦截器补头 user-agent
//     (h6.j.b() 生成的 iPhone UA, 本文件取其一固定实例) 与 origin
//     https://p3.soqjpaqj.cc; 响应体 data 串以同 AES 解密还原。
//   成功标志为解密后 status==1; 服务端偶发空数据/限流 ("您操作太快了")
//     时本文件跨三元组重试 (实测 5 组皆通, 空列表为偶发波动)。
// 端点 (f2.a, 全 POST form):
//   /api.php/api/api/mv/list_construct {sort:"see",id,page,limit:"30"}
//   /api.php/api/api/original/topic   {sort:"recommend",id,page,limit:"30"}
//   /api.php/api/community/construct  {sort:"recommend",id,page,limit:"30"}
//   /api.php/api/vlog/list_sort       {sort:"rec",limit:"30",page}
//   /api.php/api/mv/getDetail         {id}
//   /api.php/api/search/mv            {page,limit,kwy,style:"0"}
//   /api.php/api/tabnew/hot_list      {id:"0",p_type:"0",page,limit:"30"}
// 分类与 App 导航 1:1 (HJMainActivity 四 tab 及其子 tab):
//   视频 list_construct: 1 推荐, 73 伦理之爱, 238 官方原创, 220 重口猎奇,
//     3 国产精选, 2 华语原创, 257 性感网黄, 4 日韩欧美, 5 禁漫天堂,
//     203 男男之爱, 75 少女制服, 185 异域风情。
//   帖子 original/topic: 54 海角原创, 49 海角伦理, 63 海角认证。
//   社区 community/construct: 1 热门推荐, 6 分享, 4 娱乐, 3 禁区。
//   短视频 list_sort 单列。
//   hot_list 未纳入: 模块内无任何宿主创建 VideoHotFragment, 且实测
//     该端点回 {"status":0,"msg":"系统错误"}。
// 内容形态 (只做视频; 论坛帖按视频卡片收编):
//   VideoList.getAllList() = list + bot_style_one 各组 list +
//     bot_style_two, 再删 aff 为空者 (aff 为字符串, "0" 保留, null 删)。
//   视频卡 rawID 为数字 id; Detail 走 getDetail; Resolve 取
//     PlayInfo.getPlayUrl2(): source_240 非空即用, 否则取 preview_url
//     并把首个 host 标签换成 "m3u8" (正则 (https?://)[^.] + 尾段换名)。
//   短视频卡 rawID "short:<id>:<is_pay>:<url>", url 为 ShortVideoAdapter
//     所播 getPlayUrl2() (source_240 或 preview 改写); Resolve 直出。
//   帖子卡只收 medias.type==2 的视频帖, rawID
//     "post:<id>:<unlock_coins>:<media_url>", 封面取该 media.cover。
// 付费口径: 原版 PlayActivity 无条件播 getPlayUrl2(), Resolve 不查
//   is_pay/unlock_coins, 直接播视频 getPlayUrl2() / 帖子 media_url /
//   短视频带出 URL; 列表卡片上的解锁金币备注仅作展示, 不拦解析。
// 封面: cover_horizontal/vertical 为 JSON 串 {"ori","360","720"},
//   h6.e.R 取值顺序 720 -> ori -> 360, 本文件同序。
//
// 验况 (2026-10-24 本机出口实测):
//   列表: list_construct nag=1 sort=see status 1, 16-17 条/页, page2 亦有
//     数据 (推荐流式排序, page2 与 page1 有重叠, 非严格分页, 如实记);
//     nag=73 亦通; topic id=49 30 帖; community id=1 30 帖其中 16 帖含
//     type-2 视频 media; list_sort 21 条, 首条 source_240 齐全。
//   详情: getDetail id=2554910, detail 全字段 (title/source_240/
//     preview_url/tags/duration/count_play 等) 齐全。
//   播放: source_240 清单 27479 字节真 HLS (AES-128 key + 分片地址),
//     首分片 Range 206 已验 (分片本身 AES 加密, 魔数不适用)。
//   帖子 media: media_url 清单 77004 字节真 HLS 已验。
//   搜索腿未验: /api.php/api/search/mv 对 kwy/kw/keyword/q 五种键名及
//     麻豆/学生两词一律失败 (请求签密已过, 属服务端逻辑报错; 报错文案
//     随三元组在 "系统错误" 与 "token无效" 间变化); hotSearch 同错。
//     代码按 jadx 原样保留, 不谎报。
//   封面: new.drleao.cn 封面字节为定 IV 分组加密, 密钥来自 jadx
//     g6/a.java 的 Glide 取流器 (经 dec.py 解码: key
//     f5d965df75336270, IV 97b60394abc2fbe1, AES/CBC/NoPadding,
//     与 lxs/hl365 同款), yhHaijiaoCoverFetch 已注册进 init, 明文图
//     直通、解密失败回退原字节。

import (
	"bytes"
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

const yhHaijiaoBase = "https://api1.ogokfmrx.com"
const yhHaijiaoUA = "Mozilla/5.0 (iPhone 12; CPU iPhone OS 14_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1"
const yhHaijiaoOrigin = "https://p3.soqjpaqj.cc"
const yhHaijiaoAESKey = "2acf7e91e9864673"
const yhHaijiaoAESIV = "1c29882d3ddfcfd6"
const yhHaijiaoSalt = "5589d41f92a597d016b037ac37db243d"

var yhHaijiaoTriples = []struct{ oauth, sid, token string }{
	{"dbd843be20cde9b12222bc5e2d484db2", "7073399b9ec74c0e80ff40ccaa8add5d", "9150C3A62C06D814442A6DA9CDA19AE5B03AEB95DBECF9D6747C09398671006BF1898D618ED1EE6DCFECE450A362275D6192641B5583AFFD487DEAEDF0799BFB911663BC69367CFCACCA6E3B6AB49656B3E62557C17AB0EBDCA7FD038620157FC00B768D3910F88F564A4069FD3E5C7D"},
	{"286afbc74a810ee711a2607d8794471d", "5e284b9452dc4360983e4a231f78dd6c", "C1598BA62CD8B2B85F38179D3E9AE948C54A7BD2B5F14F50A383CC8F32D2CF6C7CD60C88171A169A3BBF42567C9FF574281BF7F1BBAE19205F3395F799010B611C92AB30BBB703B1BB9B4FEA3DBE159A797DC49BB97D6C28929786619D29E1EACEA32022B4150ED1B8C47444B155619D"},
	{"de5a851f101c20842e26b3d35f6a048b", "11dfbcce9b89495e9476211fe49f0f37", "5FD285734E0B3F287078CA48C68279BD8348F1CA4099FC189016EA24CE2365AD5E4253C646D29369DD325353D9F4584D6141607D15072188A450763E1DA917F2FBC7197483D66C07A5BC1322EAE849E293FF1E48CDDF25AB92DB087E355FE98EE5188A53A8E54CF8AAD2A962381927F2"},
	{"c70169845085faa0ad39852357de8bb8", "f9976b9ce0e94692819f4f18e5684dbd", "2932C2BAA0A35BEE1318CF1F88B5F4F127E09740F5646FF37807F5998A764CAFA38D86941C49C388B694DD56752FB2F5A0441FE62768C603DDE3B7D55995B38D524CBD874FC48B64BB3DD2F0A38D70A03E9BD842DA1E4FE2D6A86F788CF80BE7B5E888847EC750D78B643AA41F7A97FA"},
	{"33d771b56b6522bcbde85eb5fcd34037", "2a43c42ef3be409595a49963a3123325", "D05760D6D22622E0FDDB8E4ABB3581DA23238081F715709FF5A9B9553C6E956AB14966064B2E2446D130F075933489FD9E97BCF0E2BA8FB7C691AC208859DD39C883D10171086352CE3BA98E2A5CA5EBF2F265CC8D3C6B3ACB422EAADD0511AE78A2EA2E0F14BEAE77DCC0E936B07D50"},
}

func yhHaijiaoPad(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

func yhHaijiaoUnpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("yh_haijiao empty block")
	}
	n := int(b[len(b)-1])
	if n < 1 || n > aes.BlockSize || n > len(b) {
		return nil, fmt.Errorf("yh_haijiao bad padding")
	}
	return b[:len(b)-n], nil
}

func yhHaijiaoAESCBC(data []byte, encrypt bool) ([]byte, error) {
	block, err := aes.NewCipher([]byte(yhHaijiaoAESKey))
	if err != nil {
		return nil, err
	}
	iv := []byte(yhHaijiaoAESIV)
	if encrypt {
		data = yhHaijiaoPad(data)
		out := make([]byte, len(data))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
		return out, nil
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("yh_haijiao cipher len not block aligned")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return yhHaijiaoUnpad(out)
}

func yhHaijiaoSign(data, ts string) string {
	inp := "client=pwa&data=" + data + "&timestamp=" + ts + yhHaijiaoSalt
	sha := sha256.Sum256([]byte(inp))
	shaHex := hex.EncodeToString(sha[:])
	md := md5.Sum([]byte(shaHex))
	return hex.EncodeToString(md[:])
}

// yhHaijiaoCall 发一次带指定三元组的请求, 返回解密后 JSON。
func yhHaijiaoCallIdx(path string, extra map[string]string, idx int) (map[string]interface{}, error) {
	tri := yhHaijiaoTriples[idx%len(yhHaijiaoTriples)]
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	inner := map[string]string{
		"oauth_id": tri.oauth, "sid": tri.sid, "token": tri.token,
		"bundleId": "com.pwa.hjsq", "version": "4.1.5", "oauth_type": "web",
		"language": "zh", "via": "pwa", "_hash": ts,
	}
	for k, v := range extra {
		inner[k] = v
	}
	var ibuf bytes.Buffer
	enc := json.NewEncoder(&ibuf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(inner); err != nil {
		return nil, err
	}
	ct, err := yhHaijiaoAESCBC(bytes.TrimRight(ibuf.Bytes(), "\n"), true)
	if err != nil {
		return nil, err
	}
	data := base64.StdEncoding.EncodeToString(ct)
	form := url.Values{
		"client":    {"pwa"},
		"timestamp": {ts},
		"data":      {data},
		"sign":      {yhHaijiaoSign(data, ts)},
	}.Encode()
	h := map[string]string{
		"User-Agent":   yhHaijiaoUA,
		"Origin":       yhHaijiaoOrigin,
		"Content-Type": "application/x-www-form-urlencoded",
	}
	body, code, _, err := httpDo("POST", yhHaijiaoBase+path, strings.NewReader(form), h)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_haijiao http %d", code)
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("yh_haijiao outer parse: %v", err)
	}
	payload := outer["data"]
	if s, ok := payload.(string); ok {
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("yh_haijiao data b64: %v", err)
		}
		pt, err := yhHaijiaoAESCBC(raw, false)
		if err != nil {
			return nil, fmt.Errorf("yh_haijiao data decrypt: %v", err)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(pt, &obj); err != nil {
			return nil, fmt.Errorf("yh_haijiao payload parse: %v", err)
		}
		return obj, nil
	}
	if m := yhMap(outer["data"]); m != nil {
		return outer, nil
	}
	return outer, nil
}

// yhHaijiaoCall 跨三元组重试: status==1 且 data 非空即算成功。
func yhHaijiaoCall(path string, extra map[string]string) (map[string]interface{}, error) {
	var lastErr error
	start := int(time.Now().UnixNano() % int64(len(yhHaijiaoTriples)))
	for attempt := 0; attempt < len(yhHaijiaoTriples); attempt++ {
		obj, err := yhHaijiaoCallIdx(path, extra, start+attempt)
		if err != nil {
			lastErr = err
			continue
		}
		status, _ := g5MapInt(obj, "status")
		if status != 1 {
			lastErr = fmt.Errorf("yh_haijiao status %d: %s", status, g5StrVal(obj["msg"]))
			continue
		}
		data := yhMap(obj["data"])
		if data == nil {
			if a := yhArr(obj["data"]); a != nil && len(a) > 0 {
				return obj, nil
			}
			if _, ok := obj["data"].([]interface{}); ok {
				return obj, nil
			}
			lastErr = fmt.Errorf("yh_haijiao 空 data")
			continue
		}
		if len(yhArr(data["list"])) == 0 && len(yhArr(data["posts"])) == 0 && yhMap(data["detail"]) == nil && len(data) == 0 {
			lastErr = fmt.Errorf("yh_haijiao 空 data")
			continue
		}
		return obj, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("yh_haijiao 无可用三元组")
}

var yhHaijiaoHostLabelRe = regexp.MustCompile(`(https?://)[^.]+`)

func yhHaijiaoPlayURL2(source240, preview string) string {
	if source240 != "" {
		return source240
	}
	if preview == "" {
		return ""
	}
	loc := yhHaijiaoHostLabelRe.FindStringIndex(preview)
	if loc == nil {
		return preview
	}
	group := preview[loc[0]:loc[1]]
	// Java: group.replaceAll("[^/]+$", "m3u8") —— group 如
	// "https://yd-10play", 尾段换名后为 "https://m3u8"。
	repl := group
	if i := strings.LastIndex(repl, "/"); i >= 0 {
		repl = repl[:i+1] + "m3u8"
	} else {
		repl = "m3u8"
	}
	return preview[:loc[0]] + repl + preview[loc[1]:]
}

func yhHaijiaoCover(jsonStr string) string {
	if !strings.HasPrefix(strings.TrimSpace(jsonStr), "{") {
		return jsonStr
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return jsonStr
	}
	for _, k := range []string{"720", "ori", "360"} {
		if s := g5StrVal(m[k]); s != "" {
			return s
		}
	}
	return ""
}

func yhHaijiaoPlayCt(n int) string {
	if n >= 10000 {
		return fmt.Sprintf("%.1f万次播放", float64(n)/10000)
	}
	return fmt.Sprintf("%d次播放", n)
}

func yhHaijiaoDuration(sec int) string {
	if sec <= 0 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}

func yhHaijiaoVideoCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	if g5StrVal(rec["aff"]) == "" {
		return nil, false
	}
	id := g5StrVal(rec["id"])
	if id == "" {
		return nil, false
	}
	title := g5StrVal(rec["title"])
	cover := yhHaijiaoCover(g5StrVal(rec["cover_horizontal"]))
	if cover == "" {
		cover = yhHaijiaoCover(g5StrVal(rec["cover_vertical"]))
	}
	playCt, _ := g5MapInt(rec, "play_ct")
	dur, _ := g5MapInt(rec, "duration")
	remark := yhHaijiaoDuration(dur)
	if pc := yhHaijiaoPlayCt(playCt); pc != "0次播放" {
		if remark != "" {
			remark += " · "
		}
		remark += pc
	}
	desc := g5StrVal(rec["tags"])
	if ca := g5StrVal(rec["created_at"]); ca != "" {
		if desc != "" {
			desc += "\n"
		}
		desc += ca
	}
	return makeDrama(slot, id, title, cover, remark, "", desc), true
}

func yhHaijiaoVideoList(slot string, obj map[string]interface{}) []interface{} {
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}
	}
	out := []interface{}{}
	add := func(recs []interface{}) {
		for _, it := range recs {
			if c, ok := yhHaijiaoVideoCard(slot, yhMap(it)); ok {
				out = append(out, c)
			}
		}
	}
	add(yhArr(data["list"]))
	for _, g := range yhArr(data["bot_style_one"]) {
		add(yhArr(yhMap(g)["list"]))
	}
	add(yhArr(data["bot_style_two"]))
	return out
}

func yhHaijiaoPostCards(slot string, obj map[string]interface{}) []interface{} {
	data := yhMap(obj["data"])
	out := []interface{}{}
	for _, p := range yhArr(data["posts"]) {
		pm := yhMap(p)
		medias := yhArr(pm["medias"])
		var vm map[string]interface{}
		for _, md := range medias {
			mm := yhMap(md)
			if t, _ := g5MapInt(mm, "type"); t == 2 && g5StrVal(mm["media_url"]) != "" {
				vm = mm
				break
			}
		}
		if vm == nil {
			continue
		}
		id := g5StrVal(pm["id"])
		unlock, _ := g5MapInt(pm, "unlock_coins")
		cover := g5StrVal(vm["cover"])
		remark := "视频帖"
		if unlock > 0 {
			remark = fmt.Sprintf("视频帖 · 解锁%d金币", unlock)
		}
		rawID := fmt.Sprintf("post:%s:%d:%s", id, unlock, g5StrVal(vm["media_url"]))
		out = append(out, makeDrama(slot, rawID, g5StrVal(pm["title"]), cover, remark, "", g5StrVal(pm["content"])))
	}
	return out
}

func yhHaijiaoShortCards(slot string, obj map[string]interface{}) []interface{} {
	out := []interface{}{}
	for _, it := range yhArr(obj["data"]) {
		rec := yhMap(it)
		if g5StrVal(rec["aff"]) == "" && rec["aff"] != nil {
			continue
		}
		id := g5StrVal(rec["id"])
		if id == "" {
			continue
		}
		u := yhHaijiaoPlayURL2(g5StrVal(rec["source_240"]), g5StrVal(rec["preview_url"]))
		if u == "" {
			continue
		}
		pay, _ := g5MapInt(rec, "is_pay")
		title := g5StrVal(rec["title"])
		cover := yhHaijiaoCover(g5StrVal(rec["cover_vertical"]))
		if cover == "" {
			cover = yhHaijiaoCover(g5StrVal(rec["cover_horizontal"]))
		}
		dur, _ := g5MapInt(rec, "duration")
		playCt, _ := g5MapInt(rec, "play_ct")
		remark := yhHaijiaoDuration(dur)
		if pc := yhHaijiaoPlayCt(playCt); pc != "0次播放" {
			if remark != "" {
				remark += " · "
			}
			remark += pc
		}
		rawID := fmt.Sprintf("short:%s:%d:%s", id, pay, u)
		out = append(out, makeDrama(slot, rawID, title, cover, remark, "", g5StrVal(rec["tags"])))
	}
	return out
}

func yhHaijiaoCategories(slot string) []interface{} {
	out := []interface{}{}
	type tab struct{ id, name string }
	videoTabs := []tab{
		{"video:1", "推荐"}, {"video:73", "伦理之爱"}, {"video:238", "官方原创"},
		{"video:220", "重口猎奇"}, {"video:3", "国产精选"}, {"video:2", "华语原创"},
		{"video:257", "性感网黄"}, {"video:4", "日韩欧美"}, {"video:5", "禁漫天堂"},
		{"video:203", "男男之爱"}, {"video:75", "少女制服"}, {"video:185", "异域风情"},
	}
	postTabs := []tab{
		{"post:54", "海角原创"}, {"post:49", "海角伦理"}, {"post:63", "海角认证"},
	}
	commTabs := []tab{
		{"comm:1", "热门推荐"}, {"comm:6", "分享"}, {"comm:4", "娱乐"}, {"comm:3", "禁区"},
	}
	for _, t := range append(append(videoTabs, postTabs...), commTabs...) {
		out = append(out, categoryObj(t.id, t.name))
	}
	out = append(out, categoryObj("short", "短视频"))
	return out
}

func yhHaijiaoCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	pg := strconv.Itoa(page)
	switch {
	case strings.HasPrefix(category, "video:"):
		obj, err := yhHaijiaoCall("/api.php/api/api/mv/list_construct", map[string]string{
			"sort": "see", "id": strings.TrimPrefix(category, "video:"), "page": pg, "limit": "30",
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhHaijiaoVideoList(slot, obj)
		return items, len(items), len(items) == 30, nil
	case strings.HasPrefix(category, "post:"):
		obj, err := yhHaijiaoCall("/api.php/api/api/original/topic", map[string]string{
			"sort": "recommend", "id": strings.TrimPrefix(category, "post:"), "page": pg, "limit": "30",
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhHaijiaoPostCards(slot, obj)
		return items, len(items), len(items) == 30, nil
	case strings.HasPrefix(category, "comm:"):
		obj, err := yhHaijiaoCall("/api.php/api/community/construct", map[string]string{
			"sort": "recommend", "id": strings.TrimPrefix(category, "comm:"), "page": pg, "limit": "30",
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhHaijiaoPostCards(slot, obj)
		return items, len(items), len(items) == 30, nil
	case category == "short":
		obj, err := yhHaijiaoCall("/api.php/api/vlog/list_sort", map[string]string{
			"sort": "rec", "limit": "30", "page": pg,
		})
		if err != nil {
			return nil, 0, false, err
		}
		items := yhHaijiaoShortCards(slot, obj)
		return items, len(items), len(items) == 30, nil
	default:
		return nil, 0, false, fmt.Errorf("yh_haijiao 未知分类 %s", category)
	}
}

func yhHaijiaoDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "post:") || strings.HasPrefix(rawID, "short:") {
		title := rawID
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
	obj, err := yhHaijiaoCall("/api.php/api/mv/getDetail", map[string]string{"id": rawID})
	if err != nil {
		return nil, err
	}
	det := yhMap(yhMap(obj["data"])["detail"])
	if det == nil {
		return nil, fmt.Errorf("yh_haijiao getDetail 无 detail")
	}
	title := g5StrVal(det["title"])
	cover := yhHaijiaoCover(g5StrVal(det["cover_horizontal"]))
	if cover == "" {
		cover = yhHaijiaoCover(g5StrVal(det["cover_vertical"]))
	}
	dur, _ := g5MapInt(det, "duration")
	playCt, _ := g5MapInt(det, "play_ct")
	remark := yhHaijiaoDuration(dur)
	if pc := yhHaijiaoPlayCt(playCt); pc != "0次播放" {
		if remark != "" {
			remark += " · "
		}
		remark += pc
	}
	descParts := []string{}
	if s := g5StrVal(det["desc"]); s != "" {
		descParts = append(descParts, s)
	}
	if tags := yhArr(det["tag_list"]); len(tags) > 0 {
		names := []string{}
		for _, t := range tags {
			names = append(names, g5StrVal(t))
		}
		descParts = append(descParts, "标签: "+strings.Join(names, "、"))
	}
	if s := g5StrVal(det["actors"]); s != "" {
		descParts = append(descParts, "演员: "+s)
	}
	if s := g5StrVal(det["created_at"]); s != "" {
		descParts = append(descParts, s)
	}
	d := makeDrama(slot, rawID, title, cover, remark, "", strings.Join(descParts, "\n"))
	g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	return d, nil
}

func yhHaijiaoResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "post:") {
		parts := strings.SplitN(rawID, ":", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("yh_haijiao 帖子 rawID 非法")
		}
		return playResult(parts[3], slot, rawID, yhHaijiaoUA, yhHaijiaoBase+"/", "m3u8", "hls"), nil
	}
	if strings.HasPrefix(rawID, "short:") {
		parts := strings.SplitN(rawID, ":", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("yh_haijiao 短视频 rawID 非法")
		}
		return playResult(parts[3], slot, rawID, yhHaijiaoUA, yhHaijiaoBase+"/", "m3u8", "hls"), nil
	}
	obj, err := yhHaijiaoCall("/api.php/api/mv/getDetail", map[string]string{"id": rawID})
	if err != nil {
		return nil, err
	}
	det := yhMap(yhMap(obj["data"])["detail"])
	if det == nil {
		return nil, fmt.Errorf("yh_haijiao getDetail 无 detail")
	}
	u := yhHaijiaoPlayURL2(g5StrVal(det["source_240"]), g5StrVal(det["preview_url"]))
	if u == "" {
		return nil, fmt.Errorf("yh_haijiao 详情无播放地址")
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhHaijiaoUA, yhHaijiaoBase+"/", format, protocol), nil
}

func yhHaijiaoSearch(slot, kw string) ([]interface{}, error) {
	obj, err := yhHaijiaoCall("/api.php/api/search/mv", map[string]string{
		"page": "1", "limit": "30", "kwy": kw, "style": "0",
	})
	if err != nil {
		return nil, err
	}
	return yhHaijiaoVideoList(slot, obj), nil
}

var yhHaijiaoImgKey = []byte("f5d965df75336270")
var yhHaijiaoImgIV = []byte("97b60394abc2fbe1")

func yhHaijiaoCoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpDo("GET", u, nil, map[string]string{"User-Agent": yhHaijiaoUA})
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("yh_haijiao cover http %d", code)
	}
	if yhLxsImgMagic(body) || len(body)%aes.BlockSize != 0 {
		return body, nil
	}
	block, err := aes.NewCipher(yhHaijiaoImgKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, yhHaijiaoImgIV).CryptBlocks(out, body)
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
	if yhLxsImgMagic(out) {
		return out, nil
	}
	return body, nil
}

func init() {
	registerSite("yh_haijiao", &siteImpl{
		Key:        "yh_haijiao",
		Name:       "海角社区",
		DefaultCat: "video:1",
		HomeURL:    yhHaijiaoBase + "/",
		Categories: yhHaijiaoCategories,
		Catalog:    yhHaijiaoCatalog,
		Detail:     yhHaijiaoDetail,
		Resolve:    yhHaijiaoResolve,
		Search:     yhHaijiaoSearch,
		CoverFetch: yhHaijiaoCoverFetch,
		Headers: func() map[string]string {
			return map[string]string{"User-Agent": yhHaijiaoUA}
		},
	}, "yh_haijiao", "haijiao", "海角社区")
}
