package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 91porn 的「借九色取流」通道。
//
// 背景：91 主站给自己的直链是 mp4 + 时间戳签名（?st=&e=&k=&f=），CDN（*.91p52.com /
// la.btc620.com 等）按 TLS 指纹放行；同一个 viewkey 在九色（91porny.com）那边是
// HLS + 另一套 CDN（jiuse3.cloud）。两边内容同源、viewkey 一一对应，所以九色
// 可以当 91 的备用取流后端——91 直链拉不动时，用同一个 viewkey 去九色换一条。
//
// 不做「无条件走九色」：九色详情页 + 播放列表至少两个请求，全走它会拖慢本来
// 能秒播的 91 直链。只在三种情况下借道：
//  1. 91 详情页根本没给出地址；
//  2. 给出的地址签名全空（st/e/k/f 都为空的直链，站方不会放行，必然白转圈）；
//  3. 刚刚有过一次 91 播放失败（Reload 取不到新地址），10 分钟内整体降级到九色。
//
// 九色自己也取不到时，原样退回 91 的旧逻辑，行为与 3.25 一致。

// p91JiuseFallbackEnabled 控制 91 是否借九色取流。
//
// 2026-11 复测结论：九色并不是「全部 1002」，而是分池——免费老片池正常，
// VIP 新片池才 1002（详见 provider_jiuse.go 的分类注释）。91 自己的 CDN 现在
// 对 HTML5 播放器基本全线掐断（PROTOCOL_ERROR），而同一个 viewkey 在九色的
// 免费池里能拿到正经 HLS，所以这条通道重新打开。
// 2026-11-09 实测（GitHub Actions 海外机房 + 真实详情页）：
//
//	带 Referer 请求 https://la.btc620.com//mp43/{id}.mp4?st=…&e=…&k=…&f=…
//	  → HTTP 206，65536 字节正常返回
//	去掉 Referer → 连接直接被掐（http=000）
//	去掉签名   → HTTP 302
//
// 2026-11-09 真机复测（3.30/3.31 两版都失败）：海外机房直链 40MB 1.4 秒拉完、
// 连续 Range 全 206，但用户手机上 91 **每一部**都报「诊断：传输中断」
// （nativeStreamError 归到 io_error，即连接被重置）。同一台手机上九色
// 「大部分秒播」——说明不是整网封锁，而是 91 直链的 CDN（la.btc620.com
// 等）在这条链路上不可用。
//
// 海外机房也验证了同源映射：91 的 viewkey 直接请求九色
// /video/view/{viewkey} 返回 200、有 data-src 和 24 张推荐卡片，即 ID 通用。
// 所以 3.32 改为「九色优先、直链兜底」：用同源 HLS 避开不可达的 mp4 CDN。
// 代价是起播多一次详情页 + 一次播放列表（实测合计 1~2 秒），但换来能播。
const p91JiuseFallbackEnabled = true

// p91JiuseFirst：91 直链先不试，直接走九色。
//
// 3.26 曾因「每次白等 8 秒」被用户否定，那时直链本来能播，白等是纯损失；
// 现在直链在这条链路上 100% 连不上，先试它才是纯损失。1002 是片源级限制，
// p91JiuseMedia 会秒判并 break，不会逐条线路白等。
var p91JiuseFirst = true

const (
	// p91JiuseFallbackWindow 是一次 91 播放失败后，降级到九色取流的持续时间。
	p91JiuseFallbackWindow = 10 * time.Minute
	// p91JiuseBudget 是借道九色的总预算：四条线路逐条试，整体不超过这个时间。
	p91JiuseBudget = 8 * time.Second
	// p91JiuseBlockThreshold 是九色连续失败几次后熔断，避免每个视频都白等十几秒。
	p91JiuseBlockThreshold = 3
)

var p91JiuseState struct {
	mu           sync.Mutex
	degradeUntil time.Time // 91 直链失败后的降级窗口
	blockedUntil time.Time // 九色连续失败后的熔断窗口
	jiuseFails   int
}

// p91MediaSignatureMissing 判断直链是否「四个签名参数全空」。
// 只要有一个参数有值就当它是正常签名地址，宁可交给中转去试，也不误判。
func p91MediaSignatureMissing(address string) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Host == "" {
		return true
	}
	query := parsed.Query()
	for _, name := range []string{"st", "e", "k", "f"} {
		if strings.TrimSpace(query.Get(name)) != "" {
			return false
		}
	}
	return true
}

// p91MediaExpired 判断直链签名是否已过期。
//
// 91 的 mp4 地址带 e=<unix 秒>，是本条地址的失效时间。详情一旦进缓存
// （列表里点开的卡片、上次看过的剧集），下次起播拿到的就是过期地址，
// CDN 直接 302 —— 表现就是「同一部片刚才能播，现在一直播不出来」。
// 留 30 秒余量，到期前就当作没有地址，回源重新解析。
func p91MediaExpired(address string) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	raw := strings.TrimSpace(parsed.Query().Get("e"))
	if raw == "" {
		return false
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds <= 0 {
		return false
	}
	return time.Now().Unix()+30 >= seconds
}

// p91JiuseDegraded 表示当前处于「91 直链刚失败过」的降级窗口。
func p91JiuseDegraded() bool {
	p91JiuseState.mu.Lock()
	defer p91JiuseState.mu.Unlock()
	return time.Now().Before(p91JiuseState.degradeUntil)
}

// p91MarkP91Failure 记录一次 91 取流失败，开启降级窗口。
func p91MarkP91Failure() {
	p91JiuseState.mu.Lock()
	defer p91JiuseState.mu.Unlock()
	p91JiuseState.degradeUntil = time.Now().Add(p91JiuseFallbackWindow)
}

// p91ClearP91Failure 在 91 自己重新解析成功时调用：说明直链又通了，
// 后面的起播可以回到零额外请求的路径。
func p91ClearP91Failure() {
	p91JiuseState.mu.Lock()
	defer p91JiuseState.mu.Unlock()
	p91JiuseState.degradeUntil = time.Time{}
}

// p91JiuseBlocked 是九色侧的熔断：连续多次借道失败说明九色整体不可用
// （被墙 / CDN 全线路占位），短时间内别再让每个视频都白等。
func p91JiuseBlocked() bool {
	p91JiuseState.mu.Lock()
	defer p91JiuseState.mu.Unlock()
	return time.Now().Before(p91JiuseState.blockedUntil)
}

func p91NoteJiuseFailure() {
	p91JiuseState.mu.Lock()
	defer p91JiuseState.mu.Unlock()
	p91JiuseState.jiuseFails++
	if p91JiuseState.jiuseFails >= p91JiuseBlockThreshold {
		p91JiuseState.blockedUntil = time.Now().Add(p91JiuseFallbackWindow)
	}
}

func p91NoteJiuseSuccess() {
	p91JiuseState.mu.Lock()
	defer p91JiuseState.mu.Unlock()
	p91JiuseState.jiuseFails = 0
	p91JiuseState.blockedUntil = time.Time{}
}

// p91JiuseMedia 用 91 的 viewkey 到九色换一条可播的 HLS 地址。
// 线路顺序与九色源自带的一致：详情页默认线 → ?server=line1/2/3。
// 每条线都校验播放列表，占位（1002 / 单分片）直接换下一条。
func (d *Downloader) p91JiuseMedia(ctx context.Context, viewkey string) (providerMedia, error) {
	viewkey = strings.TrimSpace(viewkey)
	if !validJiuseID(viewkey) {
		return providerMedia{}, errors.New("viewkey 无效")
	}
	d.ensureJiuseBase(ctx)
	pageURL := jiuseBase() + "/video/view/" + viewkey
	budget, cancel := context.WithTimeout(ctx, p91JiuseBudget)
	defer cancel()

	var reasons []string
	// 1002 是「这一部片是 VIP 新片」，不是九色整体挂了。若把它计入熔断，
	// 连点三部 VIP 片就会把九色封 10 分钟，后面本来能播的片全被赶回
	// 连不上的 91 直链——3.31 的「统一传输中断」有一半是这个放大出来的。
	vipOnly := false
	for _, line := range jiusePlayLines {
		if budget.Err() != nil {
			break
		}
		target := pageURL
		if line != "" {
			target += "?server=" + line
		}
		pageText, err := d.fetchProviderText(budget, target, pageURL)
		if err != nil {
			reasons = append(reasons, jiuseLineLabel(line)+": "+publicError(err).Error())
			continue
		}
		address, _ := jiusePlayerAddress(pageText)
		if strings.TrimSpace(address) == "" {
			reasons = append(reasons, jiuseLineLabel(line)+": 详情页未给出地址")
			continue
		}
		playlist, finalURL, err := d.jiusePlaylist(budget, address, pageURL)
		if err != nil {
			reason := publicError(err).Error()
			reasons = append(reasons, jiuseLineLabel(line)+": "+reason)
			// 1002 是片源级限制，四条线路一样，换线只会让用户白等。
			if strings.Contains(reason, "1002") {
				vipOnly = true
				break
			}
			continue
		}
		if finalURL == "" {
			finalURL = address
		}
		p91NoteJiuseSuccess()
		return providerMedia{
			URL:      finalURL,
			Playlist: playlist,
			Referer:  pageURL,
			credentials: &providerMediaCredentials{
				referer:   pageURL,
				userAgent: userAgent,
			},
		}, nil
	}
	// 只有「九色整体不可用」（连不上 / 全线取不到地址）才累积熔断；
	// 单片 1002 属于站方片源限制，熔断它等于把能播的片一起判死。
	if !vipOnly {
		p91NoteJiuseFailure()
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "没有可用的播放线路")
	}
	return providerMedia{}, fmt.Errorf("九色取流失败：%s", strings.Join(reasons, "；"))
}

// 3.33：91 详情也借道九色。
//
// 真机反馈：点 91 任意片子「正在加载」一直转圈，最后「剧集信息暂不可用，
// 站源响应超时」——这不是播放失败，是 fetchP91Detail 请求 91porn.com
// 整域不可达，一直挂到 Dart 层全局超时。取流 3.32 已改为九色优先，但详情
// 仍走主站，等于每次点开先卡死在主站上。九色与 91 同源、viewkey 一一对应
// 且在这条链路可达，详情也借它，主站只留作兜底并带死线。
const (
	p91JiuseDetailBudget = 8 * time.Second
	p91DetailMainBudget  = 6 * time.Second
)

// p91DetailFromJiuse 把九色详情解析结果「换身份」回 91 源。
// 纯函数，不联网，便于离线锁住 ID / 章节归属不被串成九色。
func p91DetailFromJiuse(drama Drama, chapters []Chapter, sourceID string) (Drama, []Chapter) {
	drama.ID = providerDramaID(sourceP91, sourceID)
	drama.Source = sourceP91
	drama.SourceID = sourceID
	drama.ChannelName = "91porn"
	if strings.TrimSpace(drama.Title) == "" {
		drama.Title = "91porn " + sourceID
	}
	if strings.TrimSpace(drama.Name) == "" {
		drama.Name = drama.Title
	}
	if len(chapters) == 0 {
		chapters = []Chapter{{}}
	}
	chapter := chapters[0]
	chapter.ID = providerChapterID(sourceP91, sourceID, "main")
	chapter.Source = sourceP91
	chapter.Title = "正片"
	// 不能把九色的 m3u8 塞进 91 章节：resolveP91Media 会把它当成 91 直链
	// 起播。留空，取流阶段继续按「九色优先」用同一个 viewkey 换 HLS。
	chapter.VideoURL = ""
	chapter.PageURL = p91SiteURL + "/view_video.php?viewkey=" + url.QueryEscape(sourceID)
	chapter.Referer = p91SiteURL + "/"
	return drama, []Chapter{chapter}
}

// p91DetailViaJiuse 用同源 viewkey 到九色取详情。
func (d *Downloader) p91DetailViaJiuse(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimSpace(sourceID)
	if !validJiuseID(sourceID) {
		return Drama{}, nil, errors.New("91 viewkey 无法映射到九色")
	}
	d.ensureJiuseBase(ctx)
	pageURL := jiuseBase() + "/video/view/" + sourceID
	detailCtx, cancel := context.WithTimeout(ctx, p91JiuseDetailBudget)
	defer cancel()
	pageText, err := d.fetchProviderText(detailCtx, pageURL, jiuseBase()+"/")
	if err != nil {
		return Drama{}, nil, fmt.Errorf("九色详情请求失败：%v", publicError(err))
	}
	drama, chapters, err := parseJiuseDetail(pageText, sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	drama, chapters = p91DetailFromJiuse(drama, chapters, sourceID)
	return drama, chapters, nil
}
