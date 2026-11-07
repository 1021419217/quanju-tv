package seku

// ---- site registrations (the slot -> site table) ----
//
// Slot assignment (see batch/槽位映射表.md): py-source sites first (the
// user's daily drivers), then the two pilot sites in their proven slots,
// then jar MacCMS candidates for the remaining slots. Display names are
// Dart-hardcoded and unrelated to the content site in each slot.

func init() {
	// Pilot sites (ported in the v1-v4 rounds, kept in their slots).
	registerSite(slotHsck, &siteImpl{
		Key: "hsck", Name: "黄色仓库", DefaultCat: "new", HomeURL: hsckHost + "/",
		Categories: func(slot string) []interface{} { return hsckCategories(slot) },
		Catalog:    hsckCatalog,
		Detail:     hsckDetail,
		Resolve:    hsckResolve,
		Search: func(slot, kw string) ([]interface{}, error) {
			items, _, _, err := hsckSearch(slot, kw, 1)
			return items, err
		},
		Headers: hsckHeaders,
	}, "黄色仓库")
	registerSite(slotDayday, &siteImpl{
		Key: "dayday", Name: "天天A片", DefaultCat: "new", HomeURL: daydayBase + "/",
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			for _, c := range daydayCats {
				out = append(out, categoryObj(c.id, c.name))
			}
			return out
		},
		Catalog: daydayCatalog,
		Detail:  daydayDetail,
		Resolve: daydayResolve,
		Search:  daydaySearch,
		Headers: daydayHeaders,
	}, "天天a片", "vjav")

	registerBatchSites()
}

// hsckCategories: hsck123.com real navigation (verified against the
// live site nav — see batch/hsck_categories.md). Category id is the
// site's own list path key used by hsckCatalog.
func hsckCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range hsckCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}
