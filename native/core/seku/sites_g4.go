package seku

import (
	"crypto/md5"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ---- G4: 17c (cwmgxpu.com:2087, channel 6054) -> slot maoguo ----
// ---- G4: 91n (vqbsegi.com:2087, channel 1063) -> slot fanguo  ----
//
// Both are t1-template channel sites sharing the RSA+AES envelope
// engine in sites_g4_crypto.go, with INDEPENDENT RSA parameters
// (copied verbatim from each site's own py source — never mixed).
// Play URLs are short-lived signed addresses: detail only carries the
// site play page (as in the py sources) and Resolve re-reads the
// detail endpoint at play time to fetch a fresh URL.

const (
	g4Slot17c = "fanguo"
	g4Slot91n = "guanguo"
)

// ============================== 17c ==============================

var g4C17Cfg = &g4T1Config{
	Key:          "17c",
	Host:         "https://www.cwmgxpu.com:2087",
	Channel:      "6054",
	UA:           "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
	AvatarPrefix: "https://ji17df6a.xn--7jw54o.net/static/images/avatar/",
	RSAN:         "10910068403673309929544930905444769160105630640081089553456959579998136492842508801573955217594777606989635733031275338676446178451281832266964514305546667",
	RSAD:         "90893653179206054923654306336464042623520672741875198684521719497062461282815746879289714052011326571854041193302925894974550332608737978971373489583681",
	ImgReferer:   "https://www.cwmgxpu.com:2087/",
	PlayReferer:  "https://www.cwmgxpu.com:2087/",
	BlankOnMiss:  false,
}

// 17c navigation 1:1 (menu order, playable sections only — same set as
// the py CLASSES). Five parents carry sub-categories (py SUB_CATES,
// exposed there as filters); here each sub is its own category placed
// right after its parent, named "{parent}·{sub}".
var g4C17Cats = []struct{ id, name string }{
	{"home", "首页"},
	{"cg", "吃瓜"},
	{"cate:1", "国产"},
	{"cate:1:13", "国产·偷拍自拍"},
	{"cate:1:12", "国产·网红明星"},
	{"cate:1:11", "国产·重磅泄密"},
	{"cate:1:14", "国产·极品反差"},
	{"cate:1:15", "国产·热门校园"},
	{"cate:1:16", "国产·家庭乱伦"},
	{"cate:3", "日韩"},
	{"cate:3:51", "日韩·中文字幕"},
	{"cate:3:35", "日韩·有码"},
	{"cate:3:34", "日韩·无码"},
	{"cate:40", "原创"},
	{"actress", "女优"},
	{"cate:2", "传媒"},
	{"cate:2:18", "传媒·国产"},
	{"cate:2:46", "传媒·天美"},
	{"cate:2:47", "传媒·Love6"},
	{"cate:2:43", "传媒·爱豆"},
	{"cate:2:19", "传媒·麻豆"},
	{"cate:2:49", "传媒·蜜桃"},
	{"cate:2:45", "传媒·七度空间"},
	{"cate:2:50", "传媒·星空传媒"},
	{"cate:2:52", "传媒·大象传媒"},
	{"cate:4", "欧美"},
	{"cate:5", "动漫"},
	{"cate:6", "AI换脸"},
	{"cate:7", "主播"},
	{"cate:7:36", "主播·韩国主播"},
	{"cate:7:37", "主播·国产主播"},
	{"cate:8", "AV解说"},
	{"cate:10", "同性"},
	{"cate:10:38", "同性·男同"},
	{"cate:10:39", "同性·女同"},
}

func g4C17CatName(id string) string {
	for _, c := range g4C17Cats {
		if c.id == id {
			return c.name
		}
	}
	return "17c"
}

func g4C17VodCard(slot string, v map[string]interface{}, catName string) map[string]interface{} {
	return makeDrama(slot, "vod:"+g4Str(v["id"]), cleanText(g4Str(v["name"])),
		strings.TrimSpace(g4Str(v["enc_img"])), g4Str(v["time"]), catName, "")
}

func g4C17VodList(slot string, params map[string]string, page int, catName string) ([]interface{}, int, bool, error) {
	p := map[string]string{"sort": "new", "page": strconv.Itoa(page), "limit": "20"}
	for k, v := range params {
		p[k] = v
	}
	data, err := g4C17Cfg.g4API("/v1/vod", p)
	if err != nil {
		return nil, 0, false, err
	}
	items := []interface{}{}
	for _, raw := range g4List(data["videos"]) {
		vm := g4Map(raw)
		if g4IsAd(vm) {
			continue
		}
		items = append(items, g4C17VodCard(slot, vm, catName))
	}
	total, hasMore := g4ListPage(data, page, 20)
	if total == 0 {
		total = len(items)
	}
	return items, total, hasMore, nil
}

func g4C17Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "home"
	}
	if page < 1 {
		page = 1
	}
	switch {
	case category == "home":
		return g4C17VodList(slot, map[string]string{}, page, "首页")
	case category == "cg":
		data, err := g4C17Cfg.g4API("/v1/cg", map[string]string{"page": strconv.Itoa(page), "limit": "20"})
		if err != nil {
			return nil, 0, false, err
		}
		items := []interface{}{}
		for _, raw := range g4List(data["chiguas"]) {
			gm := g4Map(raw)
			if gm == nil || g4Str(gm["id"]) == "" {
				continue
			}
			sub := g4Str(gm["subtitle"])
			date := sub
			if i := strings.LastIndex(sub, " "); i >= 0 {
				date = sub[i+1:]
			}
			items = append(items, makeDrama(slot, "cg:"+g4Str(gm["id"]), cleanText(g4Str(gm["title"])),
				strings.TrimSpace(g4Str(gm["enc_img"])), date, "吃瓜", ""))
		}
		total, hasMore := g4ListPage(data, page, 20)
		if total == 0 {
			total = len(items)
		}
		return items, total, hasMore, nil
	case category == "actress":
		if page > 1 {
			return []interface{}{}, len(g4C17Actress), false, nil
		}
		items := []interface{}{}
		for _, a := range g4C17Actress {
			remark := strings.TrimSpace(strings.ReplaceAll(a.Height, "T", "") + " " + a.Cup)
			items = append(items, makeDrama(slot, "idx:"+a.Name, a.Name,
				g4C17Cfg.AvatarPrefix+a.PyKey+".jpg", remark, "女优", ""))
		}
		return items, len(g4C17Actress), false, nil
	case strings.HasPrefix(category, "idx:"):
		name := strings.TrimPrefix(category, "idx:")
		return g4C17VodList(slot, map[string]string{"name": name}, page, "女优")
	case strings.HasPrefix(category, "cate:"):
		parts := strings.Split(strings.TrimPrefix(category, "cate:"), ":")
		if len(parts) == 2 {
			return g4C17VodList(slot, map[string]string{"cate_id": parts[1], "cate_pid": parts[0]}, page, g4C17CatName(category))
		}
		return g4C17VodList(slot, map[string]string{"cate_id": parts[0]}, page, g4C17CatName(category))
	default:
		return []interface{}{}, 0, false, nil
	}
}

// g4C17ActressWorks fetches an actress's works (name search on the
// shared list endpoint) for the folder drill-down, capped at 10 pages.
func g4C17ActressWorks(slot, name string) ([]interface{}, error) {
	works := []interface{}{}
	for page := 1; page <= 10; page++ {
		data, err := g4C17Cfg.g4API("/v1/vod", map[string]string{
			"name": name, "sort": "new", "page": strconv.Itoa(page), "limit": "20",
		})
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		n := 0
		for _, raw := range g4List(data["videos"]) {
			vm := g4Map(raw)
			if g4IsAd(vm) {
				continue
			}
			works = append(works, g4C17VodCard(slot, vm, "女优"))
			n++
		}
		_, hasMore := g4ListPage(data, page, 20)
		if n == 0 || !hasMore {
			break
		}
	}
	return works, nil
}

func g4C17Detail(slot, rawID string) (map[string]interface{}, error) {
	switch {
	case strings.HasPrefix(rawID, "idx:"):
		name := strings.TrimPrefix(rawID, "idx:")
		cover := ""
		for _, a := range g4C17Actress {
			if a.Name == name {
				cover = g4C17Cfg.AvatarPrefix + a.PyKey + ".jpg"
				break
			}
		}
		works, err := g4C17ActressWorks(slot, name)
		if err != nil {
			return nil, err
		}
		d := makeDrama(slot, rawID, name, cover, fmt.Sprintf("%d 部作品", len(works)), "女优", name)
		chapters := []interface{}{}
		for i, w := range works {
			wm, _ := w.(map[string]interface{})
			epRaw, _ := wm["vod_id"].(string)
			title, _ := wm["title"].(string)
			chapters = append(chapters, g4Chapter(slot, rawID, epRaw, title, i+1))
		}
		if len(chapters) == 0 {
			chapters = append(chapters, g4Chapter(slot, rawID, rawID, "无作品", 1))
		}
		g4SetChapters(d, chapters)
		return d, nil
	case strings.HasPrefix(rawID, "cg:"):
		cid := strings.TrimPrefix(rawID, "cg:")
		data, err := g4C17Cfg.g4API("/v1/cg/"+cid, nil)
		if err != nil {
			return nil, err
		}
		g := g4Map(data["chigua"])
		if g == nil {
			return nil, fmt.Errorf("17c cg detail empty")
		}
		return makeDrama(slot, rawID, cleanText(g4Str(g["title"])), strings.TrimSpace(g4Str(g["enc_img"])),
			g4Str(g["subtitle"]), "吃瓜", cleanText(g4Str(g["content"]))), nil
	default:
		oid := strings.TrimPrefix(rawID, "vod:")
		data, err := g4C17Cfg.g4API("/v1/vod/"+oid, nil)
		if err != nil {
			return nil, err
		}
		v := g4Map(data["video"])
		if v == nil {
			return nil, fmt.Errorf("17c vod detail empty")
		}
		cateName := ""
		if cm := g4Map(v["cate"]); cm != nil {
			cateName = g4Str(cm["name"])
		}
		vid := g4Str(v["id"])
		if vid == "" {
			vid = oid
		}
		return makeDrama(slot, "vod:"+vid, cleanText(g4Str(v["name"])), strings.TrimSpace(g4Str(v["enc_img"])),
			cateName, cateName, ""), nil
	}
}

var (
	g4ReVParam = regexp.MustCompile(`[?&]v=(\d+)`)
	g4ReIParam = regexp.MustCompile(`[?&]i=(\d+)`)
)

func g4C17Resolve(slot, rawID string) (map[string]interface{}, error) {
	pid := strings.TrimSpace(rawID)
	var playURL string
	switch {
	case strings.HasPrefix(pid, "vod:"):
		data, err := g4C17Cfg.g4API("/v1/vod/"+strings.TrimPrefix(pid, "vod:"), nil)
		if err != nil {
			return nil, err
		}
		playURL = g4Str(g4Map(data["video"])["url"])
	case strings.HasPrefix(pid, "cg:"):
		data, err := g4C17Cfg.g4API("/v1/cg/"+strings.TrimPrefix(pid, "cg:"), nil)
		if err != nil {
			return nil, err
		}
		playURL = g4Str(g4Map(data["chigua"])["url"])
	case strings.Contains(pid, "/videoplay/"):
		m := g4ReVParam.FindStringSubmatch(pid)
		if m == nil {
			return nil, fmt.Errorf("17c resolve: no v param")
		}
		data, err := g4C17Cfg.g4API("/v1/vod/"+m[1], nil)
		if err != nil {
			return nil, err
		}
		playURL = g4Str(g4Map(data["video"])["url"])
	case strings.Contains(pid, "/chigua/detail/"):
		m := g4ReIParam.FindStringSubmatch(pid)
		if m == nil {
			return nil, fmt.Errorf("17c resolve: no i param")
		}
		data, err := g4C17Cfg.g4API("/v1/cg/"+m[1], nil)
		if err != nil {
			return nil, err
		}
		playURL = g4Str(g4Map(data["chigua"])["url"])
	case strings.HasPrefix(pid, "http"):
		playURL = pid
	default:
		return nil, fmt.Errorf("17c resolve: unrecognized id %q", pid)
	}
	if playURL == "" {
		return nil, fmt.Errorf("17c resolve: empty play url")
	}
	format, proto := g4PlayFormat(playURL)
	return playResult(playURL, slot, rawID, g4C17Cfg.UA, g4C17Cfg.PlayReferer, format, proto), nil
}

func g4C17Search(slot, kw string) ([]interface{}, error) {
	items, _, _, err := g4C17VodList(slot, map[string]string{"name": kw}, 1, "搜索")
	return items, err
}

// ============================== 91n ==============================

var g4N91Cfg = &g4T1Config{
	Key:          "91n",
	Host:         "https://www.vqbsegi.com:2087",
	Channel:      "1063",
	UA:           "Mozilla/5.0 (Linux; Android 13; Mobile) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
	AvatarPrefix: "https://ylkjh9a1.xn--ruqq8fhusk73c.com/static/images/avatar/",
	RSAN:         "11034812746936521251677573694796845190287135084811698639456218467379049151579247190638696356335870349231492335075568852461557851339228649721382342982841093",
	RSAD:         "647908196136712601682336749890569606363197824226855308674909267474473673425334301798322617684275731265203384933954006416901677577458292570728152232337153",
	ImgReferer:   "https://www.vqbsegi.com:2087/1063.html",
	PlayReferer:  "https://www.vqbsegi.com:2087/1063.html",
	BlankOnMiss:  true,
}

// 91n navigation 1:1 (py CLASSES: 精选 + shipin_cates 11-20 + 女优 +
// 吃瓜). The py exposes 日韩AV(15)'s sub-categories 3-10 and a
// new/hot sort as filters; here the subs are categories right after
// 15, named "日韩AV·{sub}". Sort stays at the py default "new" (the
// core contract has no filter channel).
var g4N91Cats = []struct{ id, name string }{
	{"relist", "精选"},
	{"11", "自拍"},
	{"12", "偷拍"},
	{"13", "探花"},
	{"14", "主播"},
	{"15", "日韩AV"},
	{"15:3", "日韩AV·高清无码"},
	{"15:4", "日韩AV·日韩素人"},
	{"15:5", "日韩AV·女优佳作"},
	{"15:6", "日韩AV·中文字幕"},
	{"15:7", "日韩AV·剧情演绎"},
	{"15:8", "日韩AV·衣着身材"},
	{"15:9", "日韩AV·角色扮演"},
	{"15:10", "日韩AV·动作场景"},
	{"16", "动漫"},
	{"17", "AV解说"},
	{"18", "欧美"},
	{"19", "三级"},
	{"20", "传媒"},
	{"actress", "女优"},
	{"cg", "吃瓜"},
}

func g4N91CatName(id string) string {
	for _, c := range g4N91Cats {
		if c.id == id {
			return c.name
		}
	}
	return "91n"
}

// g4N91VodCard mirrors the py _video_card: ad entries (href) and
// nameless entries are dropped; remarks prefer time over create_time.
func g4N91VodCard(slot string, v map[string]interface{}, catName string) map[string]interface{} {
	if g4IsAd(v) {
		return nil
	}
	name := strings.TrimSpace(g4Str(v["name"]))
	if name == "" {
		return nil
	}
	remark := g4Str(v["time"])
	if remark == "" {
		remark = g4Str(v["create_time"])
	}
	return makeDrama(slot, g4Str(v["id"]), cleanText(name),
		strings.TrimSpace(g4Str(v["enc_img"])), remark, catName, "")
}

func g4N91Cards(slot string, videos []interface{}, catName string) []interface{} {
	items := []interface{}{}
	for _, raw := range videos {
		if card := g4N91VodCard(slot, g4Map(raw), catName); card != nil {
			items = append(items, card)
		}
	}
	return items
}

func g4N91VodList(slot string, params map[string]string, page int, catName string) ([]interface{}, int, bool, error) {
	p := map[string]string{"page": strconv.Itoa(page), "limit": "20", "sort": "new"}
	for k, v := range params {
		p[k] = v
	}
	data, err := g4N91Cfg.g4API("/v1/vod", p)
	if err != nil {
		return nil, 0, false, err
	}
	items := g4N91Cards(slot, g4List(data["videos"]), catName)
	total, hasMore := g4ListPage(data, page, 20)
	if total == 0 {
		total = len(items)
	}
	return items, total, hasMore, nil
}

func g4N91Avatar(name string) string {
	sum := md5.Sum([]byte(name))
	return fmt.Sprintf("%s%x.png", g4N91Cfg.AvatarPrefix, sum)
}

func g4N91Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "relist"
	}
	if page < 1 {
		page = 1
	}
	switch {
	case category == "relist":
		data, err := g4N91Cfg.g4API("/v1/relist", nil)
		if err != nil {
			return nil, 0, false, err
		}
		items := g4N91Cards(slot, g4List(data["videos"]), "精选")
		return items, len(items), false, nil
	case category == "cg":
		data, err := g4N91Cfg.g4API("/v1/cg", map[string]string{"page": strconv.Itoa(page), "limit": "20"})
		if err != nil {
			return nil, 0, false, err
		}
		items := []interface{}{}
		for _, raw := range g4List(data["chiguas"]) {
			gm := g4Map(raw)
			if gm == nil || g4Str(gm["id"]) == "" {
				continue
			}
			items = append(items, makeDrama(slot, "cg:"+g4Str(gm["id"]), cleanText(strings.TrimSpace(g4Str(gm["title"]))),
				strings.TrimSpace(g4Str(gm["enc_img"])), g4Str(gm["date"]), "吃瓜", ""))
		}
		total, hasMore := g4ListPage(data, page, 20)
		if total == 0 {
			total = len(items)
		}
		return items, total, hasMore, nil
	case category == "actress":
		const per = 20
		total := len(g4N91ActressNames)
		pagecount := (total + per - 1) / per
		if page > pagecount {
			return []interface{}{}, total, false, nil
		}
		part := g4N91ActressNames[(page-1)*per : min(page*per, total)]
		items := []interface{}{}
		for _, name := range part {
			items = append(items, makeDrama(slot, "idx:"+name, name, g4N91Avatar(name), "", "女优", ""))
		}
		return items, total, page < pagecount, nil
	case strings.HasPrefix(category, "idx:"):
		name := strings.TrimPrefix(category, "idx:")
		return g4N91VodList(slot, map[string]string{"name": name}, page, "女优")
	case strings.Contains(category, ":"):
		parts := strings.SplitN(category, ":", 2)
		return g4N91VodList(slot, map[string]string{"cate_id": parts[1]}, page, g4N91CatName(category))
	default:
		// Bare numeric shipin cate id (11-20).
		return g4N91VodList(slot, map[string]string{"cate_id": category}, page, g4N91CatName(category))
	}
}

func g4N91ActressWorks(slot, name string) ([]interface{}, error) {
	works := []interface{}{}
	for page := 1; page <= 10; page++ {
		data, err := g4N91Cfg.g4API("/v1/vod", map[string]string{
			"name": name, "page": strconv.Itoa(page), "limit": "20", "sort": "new",
		})
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		cards := g4N91Cards(slot, g4List(data["videos"]), "女优")
		works = append(works, cards...)
		_, hasMore := g4ListPage(data, page, 20)
		if len(cards) == 0 || !hasMore {
			break
		}
	}
	return works, nil
}

func g4N91Detail(slot, rawID string) (map[string]interface{}, error) {
	switch {
	case strings.HasPrefix(rawID, "idx:"):
		name := strings.TrimPrefix(rawID, "idx:")
		works, err := g4N91ActressWorks(slot, name)
		if err != nil {
			return nil, err
		}
		d := makeDrama(slot, rawID, name, g4N91Avatar(name), fmt.Sprintf("%d 部作品", len(works)), "女优", name)
		chapters := []interface{}{}
		for i, w := range works {
			wm, _ := w.(map[string]interface{})
			epRaw, _ := wm["vod_id"].(string)
			title, _ := wm["title"].(string)
			chapters = append(chapters, g4Chapter(slot, rawID, epRaw, title, i+1))
		}
		if len(chapters) == 0 {
			chapters = append(chapters, g4Chapter(slot, rawID, rawID, "无作品", 1))
		}
		g4SetChapters(d, chapters)
		return d, nil
	case strings.HasPrefix(rawID, "cg:"):
		cid := strings.TrimPrefix(rawID, "cg:")
		data, err := g4N91Cfg.g4API("/v1/cg/"+cid, nil)
		if err != nil {
			return nil, err
		}
		g := g4Map(data["chigua"])
		if g == nil {
			return nil, fmt.Errorf("91n cg detail empty")
		}
		title := strings.TrimSpace(g4Str(g["title"]))
		if title == "" {
			title = cid
		}
		content := strings.TrimSpace(g4Str(g["content"]))
		if content == "" {
			content = title
		}
		return makeDrama(slot, rawID, cleanText(title), strings.TrimSpace(g4Str(g["enc_img"])),
			g4Str(g["subtitle"]), "吃瓜", cleanText(content)), nil
	default:
		vid := strings.TrimPrefix(rawID, "vod:")
		data, err := g4N91Cfg.g4API("/v1/vod/"+vid, nil)
		if err != nil {
			return nil, err
		}
		v := g4Map(data["video"])
		if v == nil {
			return nil, fmt.Errorf("91n vod detail empty")
		}
		name := strings.TrimSpace(g4Str(v["name"]))
		if name == "" {
			name = vid
		}
		cateNames := []string{}
		for _, raw := range g4List(v["cates"]) {
			if cm := g4Map(raw); cm != nil && g4Str(cm["name"]) != "" {
				cateNames = append(cateNames, g4Str(cm["name"]))
			}
		}
		desc := strings.TrimSpace(g4Str(v["description"]))
		if desc == "" {
			desc = name
		}
		return makeDrama(slot, vid, cleanText(name), strings.TrimSpace(g4Str(v["enc_img"])),
			g4Str(v["fan_no"]), strings.Join(cateNames, ","), cleanText(desc)), nil
	}
}

func g4N91Resolve(slot, rawID string) (map[string]interface{}, error) {
	pid := strings.TrimSpace(rawID)
	var playURL string
	switch {
	case strings.HasPrefix(pid, "cg:"):
		data, err := g4N91Cfg.g4API("/v1/cg/"+strings.TrimPrefix(pid, "cg:"), nil)
		if err != nil {
			return nil, err
		}
		playURL = g4Str(g4Map(data["chigua"])["url"])
	case strings.Contains(pid, "/cg/"):
		m := g4ReIParam.FindStringSubmatch(pid)
		if m == nil {
			return nil, fmt.Errorf("91n resolve: no i param")
		}
		data, err := g4N91Cfg.g4API("/v1/cg/"+m[1], nil)
		if err != nil {
			return nil, err
		}
		playURL = g4Str(g4Map(data["chigua"])["url"])
	case g4IsDigits(pid):
		data, err := g4N91Cfg.g4API("/v1/vod/"+pid, nil)
		if err != nil {
			return nil, err
		}
		playURL = g4Str(g4Map(data["video"])["url"])
	default:
		if m := g4ReVParam.FindStringSubmatch(pid); m != nil {
			data, err := g4N91Cfg.g4API("/v1/vod/"+m[1], nil)
			if err != nil {
				return nil, err
			}
			playURL = g4Str(g4Map(data["video"])["url"])
		} else if strings.HasPrefix(pid, "http") {
			playURL = pid
		}
	}
	if playURL == "" {
		return nil, fmt.Errorf("91n resolve: empty play url for %q", pid)
	}
	format, proto := g4PlayFormat(playURL)
	return playResult(playURL, slot, rawID, g4N91Cfg.UA, g4N91Cfg.PlayReferer, format, proto), nil
}

func g4IsDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func g4N91Search(slot, kw string) ([]interface{}, error) {
	items, _, _, err := g4N91VodList(slot, map[string]string{"name": kw}, 1, "搜索")
	return items, err
}

// ============================== registration ==============================

func init() {
	// v5: 17c UNREGISTERED from fanguo (user order 2026-10-23: on
	// device its videos "点不动" — tap does nothing). fanguo now
	// carries 海角 Haijiao (sites_hj.go). The 17c implementation is
	// kept intact below for possible later reuse.
	if false {
		registerSite(g4Slot17c, &siteImpl{
			Key: "17c", Name: "17c", DefaultCat: "home", HomeURL: g4C17Cfg.Host + "/",
			Categories: func(slot string) []interface{} {
				out := []interface{}{}
				for _, c := range g4C17Cats {
					out = append(out, categoryObj(c.id, c.name))
				}
				return out
			},
			Catalog:    g4C17Catalog,
			Detail:     g4C17Detail,
			Resolve:    g4C17Resolve,
			Search:     g4C17Search,
			Headers:    g4C17Cfg.headers,
			CoverFetch: g4CoverFetch(g4C17Cfg),
		}, "17c", "cwmgxpu")
	}
	// v6: 91n UNREGISTERED from guanguo (user order 2026-10-23:
	// swap to the jar's JavHer, sites_javher.go). Implementation
	// kept intact for possible later reuse, same as 17c above.
	if false {
		registerSite(g4Slot91n, &siteImpl{
			Key: "91n", Name: "91n", DefaultCat: "relist", HomeURL: g4N91Cfg.Host + "/",
			Categories: func(slot string) []interface{} {
				out := []interface{}{}
				for _, c := range g4N91Cats {
					out = append(out, categoryObj(c.id, c.name))
				}
				return out
			},
			Catalog:    g4N91Catalog,
			Detail:     g4N91Detail,
			Resolve:    g4N91Resolve,
			Search:     g4N91Search,
			Headers:    g4N91Cfg.headers,
			CoverFetch: g4CoverFetch(g4N91Cfg),
		}, "91n", "vqbsegi")
	}
}
