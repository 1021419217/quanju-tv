package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
const p91JiuseFallbackEnabled = true

// p91JiuseFirst：91 直链先不试，直接走九色。
//
// 91 的直链失败率高到「十部九不播」，先试它等于每次先给用户一段转圈；
// 九色失败只要 0.5~1 秒（1002 立刻判死），而且成功率高得多。等哪天 91 的
// CDN 恢复，把这个常量改回 false 即可回到「直链优先」。
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
	pageURL := jiuseBaseURL + "/video/view/" + viewkey
	budget, cancel := context.WithTimeout(ctx, p91JiuseBudget)
	defer cancel()

	var reasons []string
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
	p91NoteJiuseFailure()
	if len(reasons) == 0 {
		reasons = append(reasons, "没有可用的播放线路")
	}
	return providerMedia{}, fmt.Errorf("九色取流失败：%s", strings.Join(reasons, "；"))
}
