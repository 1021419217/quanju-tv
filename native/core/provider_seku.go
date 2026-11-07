package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"duanjuapp/native/core/seku"
)

// sekuSlotPublicIDs 与 Dart 侧 models.dart 的 v_ 站点 id 逐字对齐。
var sekuSlotPublicIDs = map[string]string{
	"hongguo":        "v_missav",
	"ikanbot":        "v_xvideos",
	"a123":           "v_hsck",
	"hanxiaoquan":    "v_javmap",
	"guipian":        "v_dayday",
	"huangdou":       "v_whos",
	"huangju":        "v_91sanji",
	"yeguo":          "v_hstv",
	"dsd":            "v_dsd",
	"cloudfront":     "v_qishe",
	"yaguo":          "v_aipornx",
	"maoguo":         "v_51xz",
	"fanguo":         "v_haijiao_fanguo",
	"guanguo":        "v_javher",
	"heguo":          "v_one",
	"xingguo":        "v_javhd",
	"huaguo":         "v_yuuhc",
	"niuguo":         "v_setang",
	"wangguo":        "v_javxu",
	"faguo":          "v_javrate",
	"wuguo":          "v_99jj",
	"javmenu":        "v_javmenu",
	"pektino":        "v_pektino",
	"xszav":          "v_xszav",
	"av123":          "v_123av",
	"av520":          "v_520av",
	"pigav":          "v_pigav",
	"aoxx69":         "v_aoxx69",
	"javdb":          "v_javdb",
"z2_17rip":       "v_z2_17rip",
	"z2_321av":       "v_z2_321av",
	"z2_av6k":        "v_z2_av6k",
	"z2_7mm":         "v_z2_7mm",
	"z2_xanimu":      "v_z2_xanimu",
	"z2_cn":          "v_z2_cn",
	"z2_thdh":        "v_z2_thdh",
	"xhy11":          "v_xhy11",
	"jack20":         "v_jack20",
	"javguru":        "v_javguru",
	"sihu":           "v_sihu",
	"tg":             "v_tg",
	"kuaihuo":        "v_kuaihuo",
	"avbigplat":      "v_avbigplat",
	"hxc":            "v_hxc",
	"goodav":         "v_goodav",
	"ikun":           "v_ikun",
	"hjw":            "v_hjw",
	"yh_senlin":      "v_senlin",
	"yh_huangdou":    "huangdou_yh",
	"yh_km":          "v_km",
	"yh_lxs":         "v_lxs",
	"yh_km2":         "v_km2",
	"yh_maomi":       "v_maomi",
	"yh_fulao2":      "v_fulao2",
	"yh_xj":          "v_xj",
	"yh_cucumber":    "v_cucumber",
	"yh_avbobo":      "v_avbobo",
	"yh_bilibili":    "v_bilibili",
	"yh_fruitpie":    "v_fruitpie",
	"yh_melon":       "v_melon",
	"yh_hl365":       "v_hl365",
	"yh_pron91":      "v_pron91",
	"yh_short91":     "v_short91",
	"yh_melon51":     "v_melon51",
	"yh_utube":       "v_utube",
	"yh_av91":        "v_av91",
	"yh_md":          "v_md",
	"yh_haijiao":     "v_haijiao",
	"yh_sex8":        "v_sex8",
	"yh_gdian":       "v_gdian",
	"yh_ins":         "v_ins",
	"yh_huangguo":    "v_huangguo",
}

var sekuPublicSlotIDs = func() map[string]string {
	out := make(map[string]string, len(sekuSlotPublicIDs))
	for slot, id := range sekuSlotPublicIDs {
		out[id] = slot
	}
	return out
}()

func sekuSourceID(slot string) string {
	if id, ok := sekuSlotPublicIDs[slot]; ok {
		return id
	}
	return slot
}

func sekuSlotForSource(source string) (string, bool) {
	s := canonicalProviderSource(source)
	if isSekuPlainSharedSource(s) {
		return "", false
	}
	if slot, ok := sekuPublicSlotIDs[s]; ok && seku.HasSlot(slot) {
		return slot, true
	}
	if seku.HasSlot(s) {
		return s, true
	}
	return "", false
}

// isSekuPlainSharedSource 列出「有原生实现、且与 seku 槽位同名的源」。
// 这些 id 必须优先走原生实现，否则会被 seku 的同名槽位抢走：
//   - huangguoai  / huangguo-video / cloudfront 的原生实现分别对接黄果
//     AI（*.gitiecgad.cc）、黄果视频、旧版剧库，而 seku 里恰好也有同名
//     槽位（vjav / javmit / qishe）。没有这道白名单时，前端选「黄果 AI」
//     会被送进 vjav 实现，站点一挂就表现为「无封面 + 不能播」。
//   - UI 里访问这些 seku 站点用的是 v_ 前缀 id（v_vjav / v_javmit /
//     v_qishe），仍能正常命中对应槽位。
func isSekuPlainSharedSource(source string) bool {
	switch source {
	case sourceHongguo, sourceIkanbot, sourceA123, sourceHanxiaoquan, sourceGuipian,
		sourceHuangdou, sourceHuangju, sourceYeguo,
		sourceHuangguoAI, sourceHuangguoVideo, sourceCloudFront:
		return true
	}
	return false
}

func isSekuSource(source string) bool {
	_, ok := sekuSlotForSource(source)
	return ok
}

func sekuPublicSources() []string {
	out := []string{}
	for _, slot := range seku.Slots() {
		out = append(out, sekuSourceID(slot))
	}
	return out
}

func sekuMapText(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		switch v := m[key].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case json.Number:
			return v.String()
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case int:
			return strconv.Itoa(v)
		}
	}
	return ""
}

func sekuMapInt(m map[string]interface{}, keys ...string) int {
	text := sekuMapText(m, keys...)
	if text == "" {
		return 0
	}
	if n, err := strconv.Atoi(text); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return int(f)
	}
	return 0
}

func sekuMapStrings(value interface{}) []string {
	out := []string{}
	switch v := value.(type) {
	case []interface{}:
		for _, entry := range v {
			if s, ok := entry.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		out = append(out, v...)
	case string:
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

type sekuTagRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func sekuTagRefs(m map[string]interface{}) []sekuTagRef {
	out := []sekuTagRef{}
	switch v := m["tags"].(type) {
	case []interface{}:
		for _, entry := range v {
			switch t := entry.(type) {
			case string:
				if name := strings.TrimSpace(t); name != "" {
					out = append(out, sekuTagRef{ID: "tag:" + name, Name: name})
				}
			case map[string]interface{}:
				name := sekuMapText(t, "name", "title")
				id := sekuMapText(t, "id", "folderId")
				if name == "" {
					continue
				}
				if id == "" {
					id = "tag:" + name
				}
				out = append(out, sekuTagRef{ID: id, Name: name})
			}
		}
	case []string:
		for _, name := range v {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, sekuTagRef{ID: "tag:" + name, Name: name})
			}
		}
	case string:
		for _, name := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '/' }) {
			if name != "" {
				out = append(out, sekuTagRef{ID: "tag:" + name, Name: name})
			}
		}
	}
	return out
}

func sekuRawID(m map[string]interface{}, fallback string) string {
	if raw := sekuMapText(m, "vod_id"); raw != "" {
		return raw
	}
	if id := sekuMapText(m, "id"); id != "" {
		if _, raw, found := strings.Cut(id, ":"); found && raw != "" {
			return raw
		}
	}
	return fallback
}

func sekuDramaFromMap(source string, m map[string]interface{}) Drama {
	rawID := sekuRawID(m, "")
	if rawID == "" {
		return Drama{}
	}
	drama := Drama{
		ID:           providerDramaID(source, rawID),
		Source:       source,
		SourceID:     rawID,
		Title:        sekuMapText(m, "title", "name"),
		Desc:         sekuMapText(m, "desc", "description", "intro"),
		Intro:        sekuMapText(m, "intro", "description", "desc"),
		Cover:        sekuMapText(m, "cover", "coverUrl", "vod_pic", "pic", "thumb"),
		Category:     sekuMapText(m, "categoryName", "category", "typeName"),
		CategoryName: sekuMapText(m, "categoryName", "category", "typeName"),
		Remark:       sekuMapText(m, "remark", "remarks", "vod_remarks"),
		Tags:         sekuMapStrings(m["tags"]),
		Kind:         sekuMapText(m, "kind"),
		FolderID:     sekuMapText(m, "folderId"),
		Backdrop:     sekuMapText(m, "backdrop"),
		Stills:       sekuMapStrings(m["stills"]),
		ChannelName:  seku.SiteName(sekuSlotOrEmpty(source)),
	}
	if count := sekuMapInt(m, "episodeCount", "episodes", "chapterCount", "totalEpisode"); count > 0 {
		drama.EpisodeCount = json.Number(strconv.Itoa(count))
	}
	if vip, ok := m["vip"].(bool); ok {
		value := vip
		drama.VIP = &value
	}
	if rel, ok := m["related"].([]interface{}); ok {
		for _, entry := range rel {
			if item, ok := entry.(map[string]interface{}); ok {
				if related := sekuDramaFromMap(source, item); related.ID != "" {
					drama.Related = append(drama.Related, related)
				}
			}
		}
	}
	return drama
}

func sekuSlotOrEmpty(source string) string {
	slot, _ := sekuSlotForSource(source)
	return slot
}

func sekuChaptersFromMap(source, dramaRaw string, m map[string]interface{}) []Chapter {
	list, _ := m["chapters"].([]interface{})
	if len(list) == 0 {
		list, _ = m["episodeList"].([]interface{})
	}
	chapters := []Chapter{}
	for index, entry := range list {
		node, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		epRaw := sekuMapText(node, "episodeId")
		if epRaw == "" {
			epRaw = sekuRawID(node, dramaRaw)
		}
		number := sekuMapInt(node, "episode", "index")
		if number <= 0 {
			number = index + 1
		}
		title := sekuMapText(node, "title", "name")
		if title == "" {
			title = "第" + strconv.Itoa(number) + "集"
		}
		chapter := Chapter{
			ID:             providerChapterID(source, dramaRaw, epRaw),
			Source:         source,
			Title:          title,
			CurrentEpisode: rawEpisode(number),
			VideoURL:       sekuMapText(node, "videoUrl"),
			PageURL:        sekuMapText(node, "pageUrl"),
		}
		chapters = append(chapters, chapter)
	}
	return chapters
}

func (d *Downloader) fetchSekuCatalogPage(ctx context.Context, source string, page int, category string) ([]Drama, bool, error) {
	slot, ok := sekuSlotForSource(source)
	if !ok {
		return nil, false, errors.New("站源无效")
	}
	category = strings.TrimSpace(category)
	if category == "" {
		category = seku.DefaultCategory(slot)
	}
	items, _, hasMore, err := seku.Catalog(slot, category, page)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 && page == 1 && category == seku.DefaultCategory(slot) {
		return nil, false, fmt.Errorf("%s暂未返回内容，请稍后刷新", seku.SiteName(slot))
	}
	out := []Drama{}
	for _, item := range items {
		if drama := sekuDramaFromMap(source, item); drama.ID != "" {
			out = append(out, drama)
		}
	}
	return out, hasMore, nil
}

func (d *Downloader) searchSeku(ctx context.Context, source, query string) ([]Drama, error) {
	slot, ok := sekuSlotForSource(source)
	if !ok {
		return nil, errors.New("站源无效")
	}
	items, err := seku.Search(slot, query)
	if err != nil {
		return nil, err
	}
	out := []Drama{}
	for _, item := range items {
		if drama := sekuDramaFromMap(source, item); drama.ID != "" {
			out = append(out, drama)
		}
	}
	return out, nil
}

func (d *Downloader) fetchSekuDetail(ctx context.Context, source, sourceID string) (Drama, []Chapter, error) {
	slot, ok := sekuSlotForSource(source)
	if !ok {
		return Drama{}, nil, errors.New("站源无效")
	}
	m, err := seku.Detail(slot, sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	drama := sekuDramaFromMap(source, m)
	if drama.ID == "" {
		drama = Drama{ID: providerDramaID(source, sourceID), Source: source, SourceID: sourceID}
	}
	return drama, sekuChaptersFromMap(source, sourceID, m), nil
}

func (d *Downloader) resolveSekuMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, dramaRaw, valid := splitProviderDramaID(task.DramaID)
	if !valid {
		return providerMedia{}, errors.New("剧集信息无效")
	}
	slot, ok := sekuSlotForSource(source)
	if !ok {
		return providerMedia{}, errors.New("站源无效")
	}
	epRaw := dramaRaw
	if prefix := source + ":" + dramaRaw + ":"; strings.HasPrefix(task.Chapter.ID, prefix) {
		if rest := strings.TrimPrefix(task.Chapter.ID, prefix); rest != "" {
			epRaw = rest
		}
	}
	result, err := seku.Resolve(slot, epRaw)
	if err != nil {
		return providerMedia{}, err
	}
	address := sekuMapText(result, "url", "videoUrl", "playUrl")
	if !isProviderHTTPMediaURL(address) {
		return providerMedia{}, fmt.Errorf("%s未返回有效播放地址", seku.SiteName(slot))
	}
	referer := sekuMapText(result, "referer")
	if referer == "" {
		if headers, ok := result["headers"].(map[string]interface{}); ok {
			referer = sekuMapText(headers, "Referer", "referer")
		}
	}
	if referer == "" {
		referer = seku.HomeURL(slot)
	}
	media := providerMedia{URL: address, Referer: referer, NeedsParse: parseFlagFromMap(result)}
	if playlist := sekuMapText(result, "playlist"); playlist != "" {
		media.Playlist = playlist
	}
	if hook := seku.HookForPlaylist(address); hook != nil {
		media.Hook = hook
	}
	if routes, ok := result["routes"].([]interface{}); ok {
		for _, entry := range routes {
			if node, ok := entry.(map[string]interface{}); ok {
				variant := sekuMapText(node, "url")
				if variant != "" && variant != address && isProviderHTTPMediaURL(variant) {
					variantReferer := sekuMapText(node, "referer")
					if variantReferer == "" {
						variantReferer = referer
					}
					variantMedia := providerMedia{URL: variant, Referer: variantReferer, NeedsParse: media.NeedsParse}
					if hook := seku.HookForPlaylist(variant); hook != nil {
						variantMedia.Hook = hook
					}
					media.Variants = append(media.Variants, variantMedia)
				}
			}
		}
	}
	return media, nil
}

func sekuRelatedCoverBase(engine *nativeEngine, source string) string {
	if engine == nil || engine.downloader == nil {
		return ""
	}
	base := engine.downloader.providerBaseURL(source)
	if base == "" || base == legacyFrontendURL {
		return ""
	}
	return base
}

func resolveSekuDramaCover(related Drama, base string) Drama {
	if base == "" {
		return related
	}
	for _, value := range []any{related.Cover, related.CoverURL, related.CoverURLSnake, related.Image, related.ImageURL, related.ImageURLSnake, related.Img, related.Pic, related.Picture, related.Poster, related.Thumb, related.Thumbnail} {
		candidate := nativeText(value)
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "//") {
			candidate = "https:" + candidate
		}
		if isProviderHTTPMediaURL(candidate) {
			return related
		}
		if resolved := resolveProviderURL(base, candidate); isProviderHTTPMediaURL(resolved) {
			related.Cover = resolved
			return related
		}
	}
	return related
}

func (engine *nativeEngine) nativeSekuDetail(ctx context.Context, drama nativeDrama, source, sourceID string) (any, error) {
	slot, ok := sekuSlotForSource(source)
	if !ok {
		return nil, errors.New("站源无效")
	}
	seku.Init(engine.directory)
	m, err := seku.Detail(slot, sourceID)
	if err != nil {
		return nil, err
	}
	raw := sekuDramaFromMap(source, m)
	chapters := sekuChaptersFromMap(source, sourceID, m)
	magnets, _ := m["magnets"].([]interface{})
	if len(chapters) == 0 && len(magnets) == 0 {
		return nil, errors.New("该剧暂时没有可播放的分集")
	}
	if raw.ID == drama.ID {
		drama = mergeNativeDrama(drama, nativeNormalize(raw))
	}
	drama.Source, drama.SourceID, drama.Episodes = source, sourceID, len(chapters)
	if drama.Backdrop == "" {
		drama.Backdrop = sekuMapText(m, "backdrop")
	}
	if len(drama.Stills) == 0 {
		drama.Stills = sekuMapStrings(m["stills"])
	}
	if len(raw.Related) > 0 && (raw.ID == drama.ID || len(drama.Related) == 0) {
		base := sekuRelatedCoverBase(engine, source)
		resolved := make([]nativeDrama, 0, len(raw.Related))
		for _, item := range raw.Related {
			resolved = append(resolved, nativeNormalize(resolveSekuDramaCover(item, base)))
		}
		drama.Related = resolved
	}
	warning := ""
	if err := engine.saveDetailMetadata(drama); err != nil {
		warning = err.Error()
	}
	return map[string]any{
		"drama":    drama,
		"chapters": chapters,
		"warning":  warning,
		"backdrop": drama.Backdrop,
		"stills":   drama.Stills,
		"related":  drama.Related,
		"tags":     sekuTagRefs(m),
		"magnets":  magnets,
	}, nil
}

// ---- 源测速 ----

// sourceSpeedTest 并发探测所有影视站源与自定义源的连通性与响应延迟。
// 每个源最多等 6 秒、8 路并发；只测「能否连上 + 响应头耗时」，不下载正文。
func (engine *nativeEngine) sourceSpeedTest(ctx context.Context) []map[string]any {
	type target struct {
		id   string
		name string
		base string
	}
	var targets []target
	seen := map[string]bool{}
	add := func(id, name, base string) {
		base = strings.TrimRight(strings.TrimSpace(base), "/")
		if base == "" || seen[base] {
			return
		}
		seen[base] = true
		if name == "" {
			name = id
		}
		targets = append(targets, target{id: id, name: name, base: base})
	}
	// seku 影视源（v_ 前缀，73 个）
	for _, slot := range seku.Slots() {
		add(sekuSourceID(slot), seku.SiteName(slot), seku.HomeURL(slot))
	}
	// 自定义源（custom: 前缀）
	for _, record := range engine.customMaccmsSources() {
		add(record.ID, record.Name, record.Base)
	}

	results := make([]map[string]any, len(targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			latency, ok := probeSourceURL(ctx, t.base)
			results[i] = map[string]any{
				"id":        t.id,
				"name":      t.name,
				"base":      t.base,
				"latencyMs": latency,
				"ok":        ok,
			}
		}(i, t)
	}
	wg.Wait()
	return results
}

// probeSourceURL 测单个地址的连通性与响应延迟（毫秒）。
func probeSourceURL(ctx context.Context, rawURL string) (int, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, false
	}
	client := &http.Client{Timeout: 6 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	return int(time.Since(start).Milliseconds()), true
}
