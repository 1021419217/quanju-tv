package seku

// iKun 资源（ikunzyapi.com）—— 标准 MacCMS v10 JSON API，走通用引擎
// mcAPIImpl。2026-10-24 本机全链实测：分类 53 个（ac=list 原样，含伦理片/
// 里番动漫等成人分类；电影/连续剧/综艺/动漫/体育/明星 7 个父类在站方
// 本身为空、内容挂在子类下）、目录 20 条/页可翻页、详情单线 ikm3u8、
// 搜索 code=1 有果、播放 master→variant→首分片 206 + TS 魔数已验。
// 本机出口到该站呈阵发性超时（同一小时内时通时断），引擎请求失败如实
// 上抛、由壳层提示重试，不在源内做静默兜底。
const ikunUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func init() {
	registerSite("ikun", mcAPIImpl("ikun", "iKun资源", &mcAPIConfig{
		Base: "https://ikunzyapi.com",
		UA:   ikunUA,
	}), "ikun")
}
