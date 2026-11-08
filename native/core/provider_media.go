package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"duanjuapp/native/core/seku"
)

type providerMedia struct {
	credentials *providerMediaCredentials
	URL         string
	Referer     string
	Duration    time.Duration
	Playlist    string
	HLSKey      []byte
	CENCKey     []byte
	Quality     int
	NeedsParse  bool
	Variants    []providerMedia
	Hook        seku.StreamHook
	// Reload 让站源在播放中途失效时重新解析一次播放地址。
	// 91porn 的直链带时间戳签名（st 参数），播到一半签名过期就只能重新取；
	// 只有实现了这个回调的站源会启用，其它源为 nil，传输路径完全不变。
	Reload func(context.Context) (string, error)
	// QualityOptions 是站源直接给出的可选清晰度档位（如 1080/720/540/480/360）。
	// 番茄这类「按档位分别缓存」的源走这个字段，避免为了列档位而伪造多条线路。
	QualityOptions []int
}

func (d *Downloader) providerBaseURL(source string) string {
	source = canonicalProviderSource(source)
	var configured, fallback string
	switch source {
	case sourceHuangguoAI:
		configured, fallback = d.cfg.HuangguoAIURL, huangguoAIBaseURL
	case sourceHuangguoVideo:
		configured, fallback = d.cfg.HuangguoVideoURL, huangguoVideoBaseURL
	case sourceHuangdou:
		configured, fallback = d.cfg.HuangdouURL, huangdouBaseURL
	case sourceHongguo:
		configured, fallback = d.cfg.HongguoURL, hongguoBaseURL
	case sourceHuangju:
		configured, fallback = d.cfg.HuangjuURL, huangjuBaseURL
	case sourceYeguo:
		configured, fallback = d.cfg.YeguoURL, yeguoBaseURL
	case sourceDSD:
		configured, fallback = d.cfg.DSDURL, dsdBaseURL
	case sourceSorani:
		configured, fallback = d.cfg.SoraniURL, soraniSiteBaseURL
	case sourceGuipian:
		configured, fallback = d.cfg.GuipianURL, guipianSiteBaseURL
	case sourceHanxiaoquan:
		configured, fallback = d.cfg.HanxiaoquanURL, hanxiaoquanSiteBaseURL
	case sourceIkanbot:
		fallback = ikanbotSiteBaseURL
	case sourceA123:
		fallback = a123SiteBaseURL
	case sourceFanqie:
		configured, fallback = d.cfg.FanqieURL, fanqieDefaultBaseURL
	case sourceXChina:
		configured, fallback = d.cfg.XChinaURL, xchinaBaseURL
	case sourceXChinaLive:
		// 与 xchina 同一个站，域名配置共用。
		configured, fallback = d.cfg.XChinaURL, xchinaBaseURL
	case sourceP91:
		fallback = p91SiteURL
	case sourceTheporny:
		configured, fallback = d.cfg.ThepornyURL, thepornyHosts[0]
	case sourceJinguoDuanju:
		configured, fallback = d.cfg.JinguoDuanjuURL, jinguoDuanjuBaseURL
	case sourceJinguo:
		configured, fallback = d.cfg.JinguoURL, jinguoAPIHosts[0]
	case sourceHeitao:
		configured, fallback = d.cfg.HeitaoURL, heitaoBaseURL
	default:
		fallback = "https://d2pypzndaqisk.cloudfront.net"
	}
	return strings.TrimRight(firstNonEmpty(configured, fallback), "/")
}

func providerSourceForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case isHuangguoAIHost(host):
		return sourceHuangguoAI
	case host == "huangguo.video":
		return sourceHuangguoVideo
	case host == "tideember.cc" || host == "xqjurgek.top" || isHuangdouHost(host):
		return sourceHuangdou
	case host == "hongguoduanju.com" || host == "www.hongguoduanju.com":
		return sourceHongguo
	case host == "huangju.net" || host == "www.huangju.net" || host == "api.huangju.net":
		return sourceHuangju
	case host == "ygdj7.com" || host == "www.ygdj7.com" || host == "ygdj15.com" || host == "www.ygdj15.com" ||
		host == "analyze.buxefaex.cc" || strings.HasSuffix(host, ".buxefaex.cc") ||
		strings.HasSuffix(host, ".fzchosdi.cc") || strings.HasSuffix(host, ".lwblvxvy.cc") ||
		strings.HasSuffix(host, ".vgbyziuz.cc") ||
		host == "delta.ygrwdsgt.cc" || host == "yeguodj.com" || host == "www.yeguodj.com":
		return sourceYeguo
	case host == "dsd.com.se" || host == "www.dsd.com.se":
		return sourceDSD
	case host == "sorani.net" || host == "www.sorani.net" || host == "api.sorani.cc" || host == "sorani.cc":
		return sourceSorani
	case host == "guipianwu.com" || host == "www.guipianwu.com":
		return sourceGuipian
	case host == "jennyhow.com" || host == "www.jennyhow.com":
		return sourceHanxiaoquan
	case host == "ikanbot.com" || host == "www.ikanbot.com" || host == "www1.ikanbot.com":
		return sourceIkanbot
	case host == "a123tv.com" || host == "www.a123tv.com":
		return sourceA123
	case host == "xchina.tube" || host == "www.xchina.tube":
		return sourceXChina
	case host == "theporny.com" || host == "www.theporny.com":
		return sourceTheporny
	default:
		return ""
	}
}

func (d *Downloader) providerURLCandidates(raw string) []string {
	source := providerSourceForURL(raw)
	if source == "" || source == sourceHuangju || source == sourceYeguo {
		return []string{raw}
	}
	parsed, _ := url.Parse(raw)
	d.providerMu.Lock()
	preferred := d.providerHosts[source]
	d.providerMu.Unlock()
	var candidates []string
	seen := map[string]bool{}
	add := func(candidate string) {
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}
	add(rehostProviderURL(parsed, preferred))
	configured := d.providerBaseURL(source)
	add(rehostProviderURL(parsed, configured))
	if providerSourceForURL(configured) == "" {
		return candidates
	}
	for _, candidate := range providerURLCandidates(raw) {
		add(candidate)
	}
	return candidates
}

func (d *Downloader) resolveProviderMedia(ctx context.Context, task Task) (providerMedia, error) {
	chapter := task.Chapter
	chapter.Source = canonicalProviderSource(chapter.Source)
	if chapter.Source == "" {
		chapter.Source = sourceFromDramaID(task.DramaID)
	}
	if isSekuSource(chapter.Source) {
		return d.resolveSekuMedia(ctx, task)
	}
	if chapter.Source == sourceCloudFront {
		return d.resolveLegacyMedia(ctx, task)
	}
	if chapter.Source == sourceHuangju {
		return d.resolveHuangjuMedia(ctx, task)
	}
	if chapter.Source == sourceYeguo {
		return d.resolveYeguoMedia(ctx, task)
	}
	if chapter.Source == sourceDSD {
		return d.resolveDSDMedia(ctx, task)
	}
	if chapter.Source == sourceSorani {
		return d.resolveSoraniMedia(ctx, task)
	}
	if chapter.Source == sourceGuipian {
		return d.resolveGuipianMedia(ctx, task)
	}
	if chapter.Source == sourceHanxiaoquan {
		return d.resolveHanxiaoquanMedia(ctx, task)
	}
	// 爱看机器人 / A123 / 短剧家族各有专用取流逻辑（抓当集播放页、算令牌、取 pp.la 等），
	// 不能落到下面通用兜底：兜底按 chapter.VideoURL 直连解析，对 a123-play:// 前缀、
	// 爱看一次性令牌这类地址会解不出有效播放地址，表现为「网页源视频均加载失败」。
	if chapter.Source == sourceIkanbot {
		return d.resolveIkanbotMedia(ctx, task)
	}
	if chapter.Source == sourceA123 {
		return d.resolveA123Media(ctx, task)
	}
	if chapter.Source == sourceFanqie {
		return d.resolveFanqieMedia(ctx, chapter, task.Quality)
	}
	if chapter.Source == sourceXChina {
		return d.resolveXChinaMedia(ctx, task)
	}
	if chapter.Source == sourceXChinaLive {
		return d.resolveXChinaMedia(ctx, task)
	}
	if chapter.Source == sourceP91 {
		return d.resolveP91Media(ctx, task)
	}
	if chapter.Source == sourceTheporny {
		return d.resolveThepornyMedia(ctx, task)
	}
	if chapter.Source == sourceJinguoDuanju {
		return d.resolveJinguoDuanjuMedia(ctx, task)
	}
	if chapter.Source == sourceJinguo {
		return d.resolveJinguoMedia(ctx, task)
	}
	if chapter.Source == sourceHeitao {
		return d.resolveHeitaoMedia(ctx, task)
	}
	if isDuanjuProviderSource(chapter.Source) {
		return d.resolveDuanjuMedia(ctx, task)
	}
	if strings.HasPrefix(chapter.VideoURL, "hongguo-cenc://") {
		return d.resolveHongguoMedia(ctx, task)
	}
	if chapter.PageURL == "" && (chapter.Source == sourceHuangguoAI || chapter.Source == sourceHuangguoVideo) {
		if source, sourceID, valid := splitProviderDramaID(task.DramaID); valid {
			_, chapters, err := d.GetHuangguoChapters(ctx, source, sourceID)
			if err != nil {
				return providerMedia{}, fmt.Errorf("刷新旧任务播放地址失败: %w", err)
			}
			matched := false
			for _, fresh := range chapters {
				if fresh.ID == chapter.ID {
					chapter = fresh
					matched = true
					break
				}
			}
			if !matched {
				return providerMedia{}, fmt.Errorf("原章节已变化，请更新该合集后重新下载")
			}
		}
	}
	media := providerMedia{URL: chapter.VideoURL, Referer: firstNonEmpty(chapter.Referer, d.providerBaseURL(chapter.Source)+"/")}
	// 黄果改版后播放地址不再内嵌在详情页里，必须单独调
	// /api/videos/{id}/play?ep=N 取 m3u8。章节的 VideoURL 是空的，落到下面
	// 通用兜底只会报「未返回有效播放地址」。
	if chapter.Source == sourceHuangguoAI {
		_, sourceID, valid := splitProviderDramaID(task.DramaID)
		if !valid || sourceID == "" {
			return providerMedia{}, fmt.Errorf("黄果剧集信息无效")
		}
		address, err := d.fetchHuangguoAIPlayURL(ctx, sourceID, chapterEpisodeNumber(chapter, task.Index))
		if err != nil {
			return providerMedia{}, err
		}
		media.URL = address
		media.Referer = d.providerBaseURL(sourceHuangguoAI) + "/"
	}
	if chapter.Source == sourceHuangdou {
		_, sourceID, valid := splitProviderDramaID(task.DramaID)
		if valid {
			sequence, err := strconv.Atoi(chapter.EpisodeString(task.Index))
			if err != nil || sequence < 1 {
				return providerMedia{}, fmt.Errorf("黄豆集数无效")
			}
			client := newHuangdouAPIClient(d)
			media, err = d.resolveHuangdouPlayback(ctx, client, sourceID, sequence)
			if err != nil {
				return providerMedia{}, err
			}
			media.Referer = client.host + "/home"
		}
	}
	// 页面兜底只对「黄果视频」这种地址内嵌在页面里的源生效。黄果 AI 的地址
	// 由上面的 /api/videos/{id}/play 直接给出，上游 106 也是这么分工的：AI 绝不
	// 走详情页解析（新站详情页是前端渲染的空壳，解析结果为空，会把刚取到的
	// m3u8 清掉，表现就是封面/目录正常、一点播放就报「未返回有效播放地址」）。
	if chapter.PageURL != "" && chapter.Source == sourceHuangguoVideo {
		responses := &playbackResponseURLs{}
		pageContext := context.WithValue(ctx, playbackResponseURLsKey{}, responses)
		body, err := d.fetchProviderText(pageContext, chapter.PageURL, media.Referer)
		if err != nil {
			return providerMedia{}, err
		}
		pageURL := chapter.PageURL
		if actual, ok := responses.values.Load(chapter.PageURL); ok {
			pageURL = actual.(string)
		}
		media.URL = parseDataHLS(body, pageURL)
		media.Referer = pageURL
	}
	if !isProviderHTTPMediaURL(media.URL) {
		return providerMedia{}, fmt.Errorf("%s 未返回有效播放地址，请刷新章节或确认站点访问权限", chapter.Source)
	}
	parsed, _ := url.Parse(media.URL)
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		playlist, finalURL, err := d.fetchMediaPlaylist(ctx, media.URL, media.Referer)
		if err != nil {
			return providerMedia{}, fmt.Errorf("获取播放列表失败: %w", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), "#EXTM3U") {
			return providerMedia{}, fmt.Errorf("站点未返回有效 M3U8，可能需要登录或链接已失效")
		}
		if duration := m3u8Duration(playlist); duration > 0 {
			media.Duration = duration
		}
		media.Playlist = playlist
		media.URL = finalURL
	}
	return media, nil
}
