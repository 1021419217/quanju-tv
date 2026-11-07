package seku

// 海角社区 Haijiao (fanguo, v5) — ported from the ZakaTV jar class
// ZakaHaijiaoAmns (+ its HjProxy helper), with every step verified
// live from this machine on 2026-10-23:
//
//   - API: GET <host>/api/... answers an envelope {isEncrypted,data};
//     when encrypted, data is a string that is base64-decoded
//     repeatedly (<=6 layers) until it parses as JSON (jar decodePayload).
//   - Hosts: https://haijiao.com, backup https://664fc46b35ab85.top
//     (ZakaSec s(82)/s(354) in the jar's HOST_POOL).
//   - Categories: 最新发布(new) + 热门榜单(hot) + the /api/topic/nodes
//     tree flattened exactly like the jar: display=="0" and
//     external_url nodes dropped, grouped by parentId, sortNo order,
//     roots first with children as "根-子", leftovers appended.
//   - Catalog: /topic/node/news (new), /topic/hot/topics (hot),
//     /topic/node/topics?nodeId&type&page&limit=20 for nodes, with the
//     jar's type probing (7 first; if <20 rows or no video, compare
//     type 1 and 0 by video count). Video topics only (the app is a
//     video player; image posts are the jar's separate 图集 mode).
//   - Detail: /topic/<id>; each video attachment resolves via
//     remoteUrl, else a preview playlist derived from coverUrl
//     (jar coverToPreview over CDN hosts ts10/pic), else the
//     /topic/att/<id> attachment endpoint.
//   - Playback (the subtle part, all proven live): the preview
//     playlist is truncated (~31s). The store also hosts the FULL
//     playlist at <segmentPrefix>_i.m3u8 in the same directory
//     (segment prefix read from the preview's first segment). The
//     served AES key file is XOR-masked with the base64-decoded
//     bytes of a sibling ".jpg" file (jar HjProxy.deriveKey); the
//     derived key + playlist IV decrypt segments to valid TS
//     (0x47 sync verified with openssl on segments 0 and 139).
//     Because mpv can only use the DERIVED key, resolve writes the
//     full playlist (segments absolutized, key URI pointed at a
//     local key file) into the core store dir and returns a file://
//     URL — the same on-disk hand-off the cover action already uses.
//   - Covers: attachment images are served as TEXT in a custom
//     base64 alphabet wrapping a data: URL (jar HjProxy.image);
//     hjCoverFetch decodes them back to real image bytes.
//   - Search: /topic/search_topic_by_tag then /topic/search.

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const hjUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
const hjMobileUA = "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

var hjHosts = []string{"https://haijiao.com", "https://664fc46b35ab85.top"}
var hjCDNs = []string{"ts10", "pic"}

var (
	hjMu       sync.Mutex
	hjHost     string
	hjNodesRaw []interface{}
)

var hjCoverVideoRe = regexp.MustCompile(`(?i)/hjstore/video/(\d{8})/([0-9a-f]{32})/(\d+)\.(?:jpe?g|png|gif)(?:\.txt)?$`)
var hjKeyLineRe = regexp.MustCompile(`#EXT-X-KEY:([^\r\n]+)`)
var hjKeyURIRe = regexp.MustCompile(`URI="([^"]+)"`)
var hjTagRe = regexp.MustCompile(`<[^>]+>`)

func hjHeadersFor(host string) map[string]string {
	return map[string]string{
		"User-Agent": hjUA,
		"Accept":     "application/json, text/plain, */*",
		"Referer":    host + "/",
	}
}

func hjSite() string {
	hjMu.Lock()
	defer hjMu.Unlock()
	if hjHost != "" {
		return hjHost
	}
	for _, h := range hjHosts {
		b, code, _, err := httpGetTimeout(h+"/api/topic/nodes", hjHeadersFor(h), 12*time.Second)
		if err == nil && code == 200 && strings.Contains(string(b), "isEncrypted") {
			hjHost = h
			return h
		}
	}
	hjHost = hjHosts[0]
	return hjHost
}

// hjDecodePayload unwraps the envelope: repeated base64 until JSON.
func hjDecodePayload(b []byte) (interface{}, error) {
	var env struct {
		IsEncrypted bool            `json:"isEncrypted"`
		Data        json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}
	if !env.IsEncrypted {
		var v interface{}
		if err := json.Unmarshal(env.Data, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	var s string
	if err := json.Unmarshal(env.Data, &s); err != nil {
		return nil, fmt.Errorf("haijiao: encrypted data is not a string")
	}
	cur := strings.TrimSpace(s)
	for i := 0; i < 6 && !strings.HasPrefix(cur, "{") && !strings.HasPrefix(cur, "["); i++ {
		dec, err := base64.StdEncoding.DecodeString(cur)
		if err != nil {
			dec, err = base64.URLEncoding.DecodeString(cur)
			if err != nil {
				return nil, fmt.Errorf("haijiao: base64 layer %d: %v", i, err)
			}
		}
		cur = strings.TrimSpace(string(dec))
	}
	var v interface{}
	if err := json.Unmarshal([]byte(cur), &v); err != nil {
		return nil, fmt.Errorf("haijiao: payload json: %v", err)
	}
	return v, nil
}

func hjAPI(path string) (interface{}, error) {
	host := hjSite()
	b, code, _, err := httpGetTimeout(host+"/api"+path, hjHeadersFor(host), 20*time.Second)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("haijiao: HTTP %d for %s", code, path)
	}
	return hjDecodePayload(b)
}

func hjNumStr(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case string:
		return n
	case json.Number:
		return n.String()
	}
	return ""
}

func hjStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func hjClean(s string) string {
	s = hjTagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func hjResults(v interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	m, ok := v.(map[string]interface{})
	if !ok {
		return out
	}
	lst, ok := m["results"].([]interface{})
	if !ok {
		return out
	}
	for _, e := range lst {
		if em, ok := e.(map[string]interface{}); ok {
			out = append(out, em)
		}
	}
	return out
}

func hjLooksVideo(t map[string]interface{}) bool {
	if b, ok := t["hasVideo"].(bool); ok && b {
		return true
	}
	if atts, ok := t["attachments"].([]interface{}); ok {
		for _, a := range atts {
			if am, ok := a.(map[string]interface{}); ok && hjStr(am, "category") == "video" {
				return true
			}
		}
	}
	return false
}

func hjNodes() ([]interface{}, error) {
	hjMu.Lock()
	cached := hjNodesRaw
	hjMu.Unlock()
	if cached != nil {
		return cached, nil
	}
	v, err := hjAPI("/topic/nodes")
	if err != nil {
		return nil, err
	}
	lst, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("haijiao: nodes payload is not a list")
	}
	hjMu.Lock()
	hjNodesRaw = lst
	hjMu.Unlock()
	return lst, nil
}

type hjCat struct{ id, name string }

func hjCategoryList() []hjCat {
	out := []hjCat{{"new", "最新发布"}, {"hot", "热门榜单"}}
	nodes, err := hjNodes()
	if err != nil {
		return out
	}
	type node struct {
		id, name, parent string
		sortNo           float64
	}
	byParent := map[string][]node{}
	parentOrder := []string{}
	names := map[string]string{}
	for _, e := range nodes {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		if hjStr(m, "display") == "0" {
			continue
		}
		if disp, ok := m["display"].(float64); ok && disp == 0 {
			continue
		}
		if strings.TrimSpace(hjStr(m, "external_url")) != "" {
			continue
		}
		name := hjClean(hjStr(m, "name"))
		id := hjNumStr(m["nodeId"])
		if name == "" || id == "" {
			continue
		}
		parent := hjNumStr(m["parentId"])
		sn, _ := m["sortNo"].(float64)
		if _, seen := byParent[parent]; !seen {
			parentOrder = append(parentOrder, parent)
		}
		byParent[parent] = append(byParent[parent], node{id, name, parent, sn})
		names[id] = name
	}
	for _, g := range byParent {
		sort.SliceStable(g, func(i, j int) bool { return g[i].sortNo < g[j].sortNo })
	}
	emitted := map[string]bool{}
	emit := func(n node, display string) {
		if emitted[n.id] {
			return
		}
		emitted[n.id] = true
		out = append(out, hjCat{n.id, display})
	}
	for _, r := range byParent["0"] {
		emit(r, r.name)
		for _, c := range byParent[r.id] {
			emit(c, r.name+"-"+c.name)
		}
	}
	for _, p := range parentOrder {
		if p == "0" {
			continue
		}
		for _, n := range byParent[p] {
			display := n.name
			if pn, ok := names[p]; ok && pn != "" {
				display = pn + "-" + n.name
			}
			emit(n, display)
		}
	}
	return out
}

func hjCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range hjCategoryList() {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

// hjTopicCard maps one API topic object to a browse card.
func hjTopicCard(t map[string]interface{}) map[string]interface{} {
	id := hjNumStr(t["topicId"])
	title := hjClean(hjStr(t, "title"))
	if id == "" || title == "" {
		return nil
	}
	cover := ""
	dur := 0
	if atts, ok := t["attachments"].([]interface{}); ok {
		for _, a := range atts {
			am, _ := a.(map[string]interface{})
			if am == nil {
				continue
			}
			switch hjStr(am, "category") {
			case "images":
				if cover == "" {
					cover = hjStr(am, "remoteUrl")
				}
			case "video":
				if f, ok := am["video_time_length"].(float64); ok && int(f) > dur {
					dur = int(f)
				}
			}
		}
		if cover == "" {
			for _, a := range atts {
				am, _ := a.(map[string]interface{})
				if am != nil && hjStr(am, "category") == "video" {
					cover = hjStr(am, "coverUrl")
					break
				}
			}
		}
	}
	nodeName := ""
	if nm, ok := t["node"].(map[string]interface{}); ok {
		nodeName = hjClean(hjStr(nm, "name"))
	}
	remark := nodeName
	if dur > 0 {
		if remark != "" {
			remark += " · "
		}
		mins := dur / 60
		if mins < 1 {
			mins = 1
		}
		remark += fmt.Sprintf("%d分钟", mins)
	} else if hjLooksVideo(t) {
		if remark != "" {
			remark += " · "
		}
		remark += "视频"
	}
	return makeDrama("fanguo", id, title, cover, remark, nodeName, "")
}

func hjCards(items []map[string]interface{}) []interface{} {
	out := []interface{}{}
	for _, t := range items {
		if c := hjTopicCard(t); c != nil {
			out = append(out, c)
		}
	}
	return out
}

func hjVideoTopics(items []map[string]interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, t := range items {
		if hjLooksVideo(t) {
			out = append(out, t)
		}
	}
	return out
}

func hjCatalog(slot, cat string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	lim := 20
	var v interface{}
	var err error
	switch cat {
	case "", "new":
		v, err = hjAPI(fmt.Sprintf("/topic/node/news?page=%d&limit=%d", page, lim))
		if err == nil && page == 1 && len(hjVideoTopics(hjResults(v))) == 0 {
			if hv, herr := hjAPI(fmt.Sprintf("/topic/hot/topics?page=%d&limit=%d", page, lim)); herr == nil {
				v = hv
			}
		}
	case "hot":
		v, err = hjAPI(fmt.Sprintf("/topic/hot/topics?page=%d&limit=%d", page, lim))
	default:
		fetch := func(typ int) (interface{}, []map[string]interface{}) {
			pv, perr := hjAPI(fmt.Sprintf("/topic/node/topics?nodeId=%s&type=%d&page=%d&limit=%d", cat, typ, page, lim))
			if perr != nil {
				return nil, nil
			}
			return pv, hjResults(pv)
		}
		var best []map[string]interface{}
		v7, r7 := fetch(7)
		_ = v7
		best = r7
		if len(r7) < lim || len(hjVideoTopics(r7)) == 0 {
			for _, typ := range []int{1, 0} {
				_, rt := fetch(typ)
				if len(hjVideoTopics(rt)) > len(hjVideoTopics(best)) {
					best = rt
				}
			}
		}
		items := hjVideoTopics(best)
		if page == 1 && len(items) == 0 {
			return nil, 0, false, fmt.Errorf("haijiao: node %s returned no video topics", cat)
		}
		return hjCards(items), len(best), len(best) >= lim, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	items := hjResults(v)
	vids := hjVideoTopics(items)
	return hjCards(vids), len(items), len(items) >= lim, nil
}

// hjResolveAtt turns one video attachment into a play reference:
// a direct URL, or "hjpl|<previewURL>" for store-hosted previews.
func hjResolveAtt(att map[string]interface{}, topicID string) string {
	if ru := strings.TrimSpace(hjStr(att, "remoteUrl")); ru != "" {
		return ru
	}
	cover := strings.TrimSpace(hjStr(att, "coverUrl"))
	if m := hjCoverVideoRe.FindStringSubmatch(cover); m != nil {
		for _, cdn := range hjCDNs {
			preview := fmt.Sprintf("https://%s.hj260302818.top/hjstore/video/%s/%s/%s_i_preview.m3u8", cdn, m[1], m[2], m[3])
			b, code, _, err := httpGetTimeout(preview, map[string]string{"User-Agent": hjMobileUA, "Referer": hjSite() + "/"}, 10*time.Second)
			if err == nil && code == 200 && strings.Contains(string(b), "#EXTM3U") {
				return "hjpl|" + preview
			}
		}
	}
	if aid := hjNumStr(att["id"]); aid != "" {
		if v, err := hjAPI("/topic/att/" + aid); err == nil {
			if lst, ok := v.([]interface{}); ok {
				for _, e := range lst {
					if em, ok := e.(map[string]interface{}); ok {
						if u := strings.TrimSpace(hjStr(em, "url")); u != "" {
							return u
						}
					}
				}
			}
		}
	}
	return ""
}

func hjDetail(slot, rawID string) (map[string]interface{}, error) {
	rawID = strings.SplitN(rawID, "$", 2)[0]
	v, err := hjAPI("/topic/" + rawID)
	if err != nil {
		return nil, err
	}
	t, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("haijiao: detail payload malformed")
	}
	title := hjClean(hjStr(t, "title"))
	cover := ""
	var refs []string
	if atts, ok := t["attachments"].([]interface{}); ok {
		for _, a := range atts {
			am, _ := a.(map[string]interface{})
			if am == nil {
				continue
			}
			switch hjStr(am, "category") {
			case "images":
				if cover == "" {
					cover = hjStr(am, "remoteUrl")
				}
			case "video":
				if r := hjResolveAtt(am, rawID); r != "" {
					refs = append(refs, r)
				}
			}
		}
		if cover == "" {
			for _, a := range atts {
				am, _ := a.(map[string]interface{})
				if am != nil && hjStr(am, "category") == "video" {
					cover = hjStr(am, "coverUrl")
					break
				}
			}
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("haijiao: topic %s has no playable video attachment", rawID)
	}
	nodeName := ""
	if nm, ok := t["node"].(map[string]interface{}); ok {
		nodeName = hjClean(hjStr(nm, "name"))
	}
	author := ""
	if um, ok := t["user"].(map[string]interface{}); ok {
		author = hjClean(hjStr(um, "nickname"))
	}
	desc := hjClean(hjStr(t, "content"))
	if desc == "" {
		desc = title
	}
	d := makeDrama(slot, rawID, title, cover, nodeName, nodeName, desc)
	if author != "" {
		d["actor"] = author
		d["vod_actor"] = author
	}
	chs := []interface{}{}
	for i, r := range refs {
		name := "正片"
		if len(refs) > 1 {
			name = fmt.Sprintf("第%d集", i+1)
		}
		ep := g2Chapter(slot, rawID, r, name, i)
		ep["sourceName"] = "海角社区"
		ep["source"] = "海角社区"
		chs = append(chs, ep)
	}
	g2SetChapters(d, chs)
	d["playSources"] = []interface{}{map[string]interface{}{
		"name": "海角社区", "sourceName": "海角社区", "source": "海角社区",
		"episodes": chs, "chapterCount": len(chs),
	}}
	return d, nil
}

// hjFetchText fetches a store/CDN text resource with playback headers.
func hjFetchText(u string) (string, int, error) {
	b, code, _, err := httpGetTimeout(u, map[string]string{"User-Agent": hjMobileUA, "Referer": hjSite() + "/"}, 15*time.Second)
	if err != nil {
		return "", 0, err
	}
	return string(b), code, nil
}

// hjDeriveKey implements HjProxy.deriveKey: real key = served key
// bytes XOR the base64-decoded sibling ".jpg" mask (cyclic). Without
// a usable mask the served bytes are used as-is.
func hjDeriveKey(playlistURL, playlistText string) ([]byte, error) {
	m := hjKeyLineRe.FindStringSubmatch(playlistText)
	if m == nil {
		return nil, fmt.Errorf("haijiao: playlist has no EXT-X-KEY")
	}
	um := hjKeyURIRe.FindStringSubmatch(m[1])
	if um == nil {
		return nil, fmt.Errorf("haijiao: key line has no URI")
	}
	keyURL := um[1]
	if !strings.HasPrefix(keyURL, "http") {
		keyURL = playlistURL[:strings.LastIndex(playlistURL, "/")+1] + keyURL
	}
	kb, code, _, err := httpGetTimeout(keyURL, map[string]string{"User-Agent": hjMobileUA, "Referer": hjSite() + "/"}, 15*time.Second)
	if err != nil || code != 200 || len(kb) == 0 {
		return nil, fmt.Errorf("haijiao: key fetch HTTP %d err %v", code, err)
	}
	maskURL := strings.Replace(playlistURL, ".m3u8", ".jpg", 1)
	if maskURL != playlistURL {
		if mb, mcode, _, merr := httpGetTimeout(maskURL, map[string]string{"User-Agent": hjMobileUA, "Referer": hjSite() + "/"}, 15*time.Second); merr == nil && mcode == 200 {
			if dec, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(mb))); derr == nil && len(dec) > 0 {
				out := make([]byte, len(kb))
				for i := range kb {
					out[i] = kb[i] ^ dec[i%len(dec)]
				}
				return out, nil
			}
		}
	}
	return kb, nil
}

// hjExpandPreview turns a truncated preview playlist into a full
// local playlist file + key file, then serves them from the loopback
// stream server. file:// is deliberately avoided: iOS AVPlayer refuses
// to load a local HLS playlist, which made 海角 look "resolved but
// unplayable" on device.
func hjExpandPreview(previewURL string) (string, error) {
	text, code, err := hjFetchText(previewURL)
	if err != nil || code != 200 || !strings.Contains(text, "#EXTM3U") {
		return "", fmt.Errorf("haijiao: preview fetch HTTP %d err %v", code, err)
	}
	dir := previewURL[:strings.LastIndex(previewURL, "/")+1]
	// First segment name gives the store prefix for the full playlist.
	var firstSeg string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".ts") {
			firstSeg = line
			break
		}
	}
	fullText := ""
	fullURL := ""
	if idx := strings.LastIndex(firstSeg, "_i"); idx > 0 {
		cand := dir + firstSeg[:idx] + "_i.m3u8"
		if ft, fc, ferr := hjFetchText(cand); ferr == nil && fc == 200 && strings.Contains(ft, "#EXTINF:") {
			fullText, fullURL = ft, cand
		}
	}
	if fullText == "" {
		// The preview itself is the whole video (short clips).
		fullText, fullURL = text, previewURL
	}
	key, err := hjDeriveKey(fullURL, fullText)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(previewURL))
	name := hex.EncodeToString(sum[:])[:16]
	outDir := filepath.Join(storeBaseDir(), "hjpl")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	keyName := name + ".key"
	if err := os.WriteFile(filepath.Join(outDir, keyName), key, 0o644); err != nil {
		return "", err
	}
	// Rebuild the playlist: key URI points at the local derived-key
	// file (keeping the original IV), relative segment names are
	// absolutized against the playlist directory.
	keyLine := "#EXT-X-KEY:METHOD=AES-128,URI=\"" + keyName + "\""
	if km := hjKeyLineRe.FindStringSubmatch(fullText); km != nil {
		if ivm := regexp.MustCompile(`IV=0x[0-9a-fA-F]+`).FindString(km[1]); ivm != "" {
			keyLine += "," + ivm
		}
	}
	var b strings.Builder
	for _, line := range strings.Split(fullText, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			b.WriteString(keyLine + "\n")
		case strings.HasPrefix(line, "#"):
			b.WriteString(line + "\n")
		case strings.HasPrefix(line, "http"):
			b.WriteString(line + "\n")
		default:
			b.WriteString(dir + line + "\n")
		}
	}
	pl := b.String()
	plPath := filepath.Join(outDir, name+".m3u8")
	if err := os.WriteFile(plPath, []byte(pl), 0o644); err != nil {
		return "", err
	}
	if u, err := localStreamURL(name + ".m3u8"); err == nil {
		return u, nil
	}
	// 极少数环境绑不上 loopback（例如被系统策略拦），退回原路径，
	// 桌面端仍能播，手机上至少不会毫无线索。
	return "file://" + plPath, nil
}

func hjResolve(slot, rawID string) (map[string]interface{}, error) {
	ref := rawID
	if strings.HasPrefix(ref, "hjpl|") {
		u, err := hjExpandPreview(strings.TrimPrefix(ref, "hjpl|"))
		if err != nil {
			return nil, err
		}
		return playResult(u, "haijiao", ref, hjMobileUA, hjSite()+"/", "application/x-mpegURL", "hls"), nil
	}
	if strings.HasPrefix(ref, "http") {
		format := ""
		if strings.Contains(ref, ".m3u8") {
			format = "application/x-mpegURL"
		}
		return playResult(ref, "haijiao", ref, hjMobileUA, hjSite()+"/", format, ""), nil
	}
	return nil, fmt.Errorf("haijiao: unresolvable play ref %q", ref)
}

func hjSearch(slot, kw string) ([]interface{}, error) {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return []interface{}{}, nil
	}
	q := "tag=" + strings.ReplaceAll(strings.TrimSpace(kw), " ", "%20") + "&page=1&limit=20"
	v, err := hjAPI("/topic/search_topic_by_tag?" + q)
	if err != nil {
		v, err = hjAPI("/topic/search?keyword=" + strings.ReplaceAll(strings.TrimSpace(kw), " ", "%20") + "&page=1&limit=20")
		if err != nil {
			return nil, err
		}
	}
	items := hjResults(v)
	if _, isObj := v.(map[string]interface{}); !isObj {
		// Some search endpoints answer a bare list.
		if lst, ok := v.([]interface{}); ok {
			for _, e := range lst {
				if em, ok := e.(map[string]interface{}); ok {
					items = append(items, em)
				}
			}
		}
	}
	return hjCards(hjVideoTopics(items)), nil
}

// hjCoverFetch decodes 海角's obfuscated cover payloads: the pic CDN
// serves a custom-base64 text wrapping a data: URL instead of image
// bytes. Real image bytes (sniffed by magic) pass through untouched.
var hjImgAlphabet = "ABCD*EFGHIJKLMNOPQRSTUVWX#YZabcdefghijklmnopqrstuvwxyz1234567890"

func hjDecodeCustomB64(s string) []byte {
	idx := map[rune]int{}
	for i, c := range hjImgAlphabet {
		idx[c] = i
	}
	var out []byte
	acc, bits := 0, 0
	for _, c := range s {
		v, ok := idx[c]
		if !ok {
			continue
		}
		acc = (acc << 6) | v
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte((acc>>bits)&0xFF))
		}
	}
	return out
}

func hjCoverFetch(u string) ([]byte, error) {
	b, code, _, err := httpGetTimeout(u, map[string]string{"User-Agent": hjUA, "Referer": hjSite() + "/"}, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if code != 200 || len(b) == 0 {
		return nil, fmt.Errorf("haijiao cover: HTTP %d", code)
	}
	if len(b) >= 3 && ((b[0] == 0xFF && b[1] == 0xD8) || (b[0] == 0x89 && b[1] == 'P') || (b[0] == 'G' && b[1] == 'I')) {
		return b, nil
	}
	if len(b) >= 12 && b[0] == 'R' && b[8] == 'W' {
		return b, nil
	}
	dec := hjDecodeCustomB64(string(b))
	s := string(dec)
	i := strings.Index(s, "base64,")
	if i < 0 {
		return nil, fmt.Errorf("haijiao cover: payload is not an image data URL")
	}
	img, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s[i+len("base64,"):]), ""))
	if err != nil || len(img) == 0 {
		return nil, fmt.Errorf("haijiao cover: inner base64: %v", err)
	}
	return img, nil
}

func init() {
	registerSite("fanguo", &siteImpl{
		Key: "haijiao", Name: "海角社区", DefaultCat: "new", HomeURL: "https://haijiao.com/",
		Categories: hjCategories,
		Catalog:    hjCatalog,
		Detail:     hjDetail,
		Resolve:    hjResolve,
		Search:     hjSearch,
		Headers:    func() map[string]string { return hjHeadersFor(hjSite()) },
		CoverFetch: hjCoverFetch,
	}, "haijiao", "海角")
}
