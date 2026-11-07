package seku

import "strings"

// ---- batch slot registry ----
//
// The Dart UI has a static directory of 23 SourceSites; content can only
// be attached to those slot ids (see providers.go header comment). This
// registry is the single "slot -> site implementation" table for the
// batch build: to move a site to another slot, edit slotSites only.
//
// Every implementation exposes the same six operations the core needs.
// Implementations tag every drama with the SLOT id (makeDrama does it),
// never with their internal key.

type siteImpl struct {
	Key        string // internal site key, e.g. "missav"
	Name       string // content name (shown only in payload extras)
	DefaultCat string // category id used when the request names none
	HomeURL    string

	Categories func(slot string) []interface{}
	Catalog    func(slot, category string, page int) ([]interface{}, int, bool, error)
	Detail     func(slot, rawID string) (map[string]interface{}, error)
	Resolve    func(slot, rawID string) (map[string]interface{}, error)
	Search     func(slot, kw string) ([]interface{}, error)
	Headers    func() map[string]string
	// CoverFetch optionally fetches+transforms a cover (sites whose
	// cover bytes are XOR/AES transformed at rest). Nil -> plain GET.
	CoverFetch func(url string) ([]byte, error)
}

// slotSites maps Dart slot id -> implementation. Filled by registerSites
// (sites.go) and the two pilot files (hsck/dayday register via init-like
// wiring in sites.go too, so this file stays the only table to read).
var slotSites = map[string]*siteImpl{}

// slotOrder is the presentation order used when the core reports all
// slots (status lists, cross search defaults).
var slotOrder = []string{
	"hongguo", "ikanbot", "a123", "hanxiaoquan", "guipian",
	"huangdou", "huangju", "yeguo", "dsd", "huangguo-video",
	"huangguoai", "cloudfront", "yaguo", "maoguo", "fanguo",
	"guanguo", "heguo", "xingguo", "huaguo", "niuguo",
	"wangguo", "faguo", "wuguo",
	"javmenu", "pektino", "xszav", "av123",
	"av520", "pigav", "aoxx69", "javdb",
"z2_17rip", "z2_321av", "z2_av6k", "z2_7mm", "z2_xanimu",
	"z2_cn", "z2_thdh",
	"xhy11", "jack20", "javguru", "sihu", "tg",
	"kuaihuo", "avbigplat", "hxc", "hjw", "goodav", "ikun",
	"yh_senlin", "yh_huangdou",
	// Yuhuo 70-module integration (2026-10-24): registered slots.
	// Deferred files (copied, dormant until slotOrder lists them):
	// yh_kmpic, yh_lutube, yh_xiami, yh_md2, yh_moviecloud, yh_dn,
	// yh_gkj, yh_aimeiju, yh_qwapp. Never registered: yh_cm, yh_live.
	// yh_bale file is present (shared yhMap/yhArr helpers) but its
	// slot is not listed here, so it stays dormant on this host.
	"yh_km", "yh_lxs",
	"yh_km2", "yh_maomi", "yh_fulao2",
	"yh_xj", "yh_cucumber",
	"yh_avbobo", "yh_bilibili",
	"yh_fruitpie", "yh_melon", "yh_hl365",
	"yh_pron91", "yh_short91", "yh_melon51", "yh_utube",
	"yh_av91", "yh_md", "yh_haijiao", "yh_sex8",
	"yh_gdian", "yh_ins", "yh_huangguo",
}

// keyToSlot resolves internal keys and display aliases to slot ids.
var keyToSlot = map[string]string{}

func registerSite(slot string, impl *siteImpl, aliases ...string) {
	slotSites[slot] = impl
	keyToSlot[impl.Key] = slot
	for _, a := range aliases {
		keyToSlot[strings.ToLower(a)] = slot
	}
}

func implForSlot(slot string) *siteImpl {
	if impl, ok := slotSites[slot]; ok {
		return impl
	}
	return nil
}

// allSlots returns the registered slot ids in presentation order.
func allSlots() []string {
	out := []string{}
	for _, s := range slotOrder {
		if _, ok := slotSites[s]; ok {
			out = append(out, s)
		}
	}
	return out
}
