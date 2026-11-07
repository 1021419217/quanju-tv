package seku

func pkCategories(slot string) []interface{} {
	out := []interface{}{categoryObj("hot", "🔥 全站排行")}
	for _, t := range pkTags {
		out = append(out, categoryObj(t.Code, t.ZH))
	}
	for _, t := range pkLangs {
		out = append(out, categoryObj(t.Code, "🌍 "+t.ZH))
	}
	return out
}

func init() {
	// supjav 已按用户 2026-10-24 二轮口径撤注册（fix1 接全壳免代后真机仍打不开）；
	// 实现保留在 sites_g2.go 存档，TestSupjavSlotsUnregistered 锁定不得注册。
	registerSite("javmenu", &siteImpl{
		Key: "javmenu", Name: "JavMenu", DefaultCat: "censored/online", HomeURL: jmHost + "/",
		Categories: func(slot string) []interface{} { return g1Categories(jmCats, slot) },
		Catalog:    jmCatalog,
		Detail:     jmDetail,
		Resolve:    jmResolve,
		Search:     jmSearch,
		Headers:    jmHeaders,
	}, "javmenu")
	registerSite("pektino", &siteImpl{
		Key: "pektino", Name: "Pektino", DefaultCat: "hot", HomeURL: pkHost + "/",
		Categories: pkCategories,
		Catalog:    pkCatalog,
		Detail:     pkDetail,
		Resolve:    pkResolve,
		Search:     pkSearch,
		Headers:    pkHeaders,
	}, "pektino")
	registerSite("xszav", &siteImpl{
		Key: "xszav", Name: "XSZAV", DefaultCat: "japanese", HomeURL: "https://cn.xsz-av.com/",
		Categories: xsCategories,
		Catalog:    xsCatalog,
		Detail:     xsDetail,
		Resolve:    xsResolve,
		Search:     xsSearch,
		Headers:    xsHeaders,
	}, "xszav")
	registerSite("av123", &siteImpl{
		Key: "123av", Name: "123AV", DefaultCat: "new", HomeURL: av123Site + "/",
		Categories: av123Categories,
		Catalog:    av123Catalog,
		Detail:     av123Detail,
		Resolve:    av123Resolve,
		Search:     av123Search,
		Headers:    av123Headers,
	}, "123av")
	// 用户 2026-10-24 定版删除：17c（c17）、91n（n91）、MissAV原站
	// （missav_orig）三站注销，不再进站源列表。三站实现文件保留在树内
	// （g4 加密信封引擎仍被 g4 家族其他站共用；missav 实现在 sites_g2.go
	// 与镜像站的共享提取器同文件），仅撤注册。MissAV镜像（hongguo 槽，
	// sites_mirror.go）不受影响、原样保留。
}
