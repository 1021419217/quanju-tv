package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// 一条播放连接最多续传几次、最多续多久。
//
// 站方 CDN 掐得很勤（有的十几秒一次），次数太少等于没做；但也不能无限续，
// 否则一个已经死掉的地址会一直挂着，播放器看到的是永远转圈。
const (
	nativeRelayMaxResumes  = 240
	nativeRelayMaxDuration = 30 * time.Minute
	// 连续这么多次续不上就认为这条链废了，换备用线路 / 重新解析。
	nativeRelayFailBeforeSwitch = 2
	// 服务器不支持 Range 时只能从头丢字节，丢得太多还不如换线路。
	nativeRelayMaxSkip = 8 << 20
	// 一次播放最多重新解析几次地址：签名过期要重新取，但不能把详情页打爆。
	nativeRelayMaxReloads = 6
)

// nativeRelayIdleTimeout 是「多久没有新字节就认为这条连接已经僵死」。
// 站方 CDN 有时既不断开也不发数据，不主动断开就只能一直转圈。15 秒是折中：
// 短于播放器 20 秒的 network-timeout，长于跨境慢节点的正常首包间隔。
const nativeRelayIdleTimeout = 15 * time.Second

// nativeIdleReader 给上游响应体加一个空闲看门狗：连续 timeout 没有读到字节就
// 关掉连接，让阻塞中的 Read 立刻返回错误，交给续传循环重连。
//
// 注意计时只在「已经读到过字节」之后才开始：首字节本来就要等 CDN 建连 + 排队，
// 3.20 从创建时就起 10 秒倒计时，慢节点上一个字节都没来就被判死，表现成
// 「刚才能播的这会儿一直转圈」。首包超时交给传输层的 ResponseHeaderTimeout。
type nativeIdleReader struct {
	body    io.ReadCloser
	timeout time.Duration
	timer   *time.Timer
}

func newNativeIdleReader(body io.ReadCloser) *nativeIdleReader {
	return newNativeIdleReaderWithTimeout(body, nativeRelayIdleTimeout)
}

func newNativeIdleReaderWithTimeout(body io.ReadCloser, timeout time.Duration) *nativeIdleReader {
	return &nativeIdleReader{body: body, timeout: timeout}
}

func (reader *nativeIdleReader) Read(buffer []byte) (int, error) {
	if reader.timer == nil {
		reader.timer = time.AfterFunc(reader.timeout, func() { _ = reader.body.Close() })
	} else {
		reader.timer.Reset(reader.timeout)
	}
	n, err := reader.body.Read(buffer)
	if n > 0 && reader.timer != nil {
		reader.timer.Reset(reader.timeout)
	}
	return n, err
}

func (reader *nativeIdleReader) Close() error {
	if reader.timer != nil {
		reader.timer.Stop()
	}
	return reader.body.Close()
}

func (reader *nativeIdleReader) stop() {
	if reader.timer != nil {
		reader.timer.Stop()
	}
}

// nativeRelayBody 把上游媒体流转发给播放器，并在上游掐断时从断点续拉。
//
// 91porn 这类站的 CDN 会在几秒到几十秒后主动断开长连接，播放器看到的就是
// 「传输中断」。这里记录已经写出的字节数，断了就用 Range 从断点再拉一段接上，
// 播放器全程只看到一条连续的流；只有真的续不上时才让连接结束。
//
// 续不上时依次尝试：同一地址重试 → 换备用直链（providerMedia.Variants）→
// 让站源重新解析一条新地址（签名过期的情况只有这条能救）。
//
// 只对显式标记了 resilient 的媒体凭据启用（目前只有 91porn），
// 其它站源的传输路径完全不变。
func (stream *nativeStreamServer) nativeRelayBody(ctx context.Context, writer http.ResponseWriter, method string, upstream *http.Request, session *nativeStreamSession, assetKey, address string, response *http.Response, reader *bufio.Reader, info nativeStreamDiagnostic) {
	start, total := int64(0), int64(-1)
	if a, _, t, valid := nativeContentRange(response.Header.Get("Content-Range")); valid {
		start, total = a, t
	} else if response.StatusCode == http.StatusOK && response.ContentLength >= 0 {
		total = response.ContentLength
	}
	written := int64(0)
	// 已经写给播放器的 Content-Length 是承诺：续传再多也不能超过它，
	// 否则 Go 的 http 服务会以「wrote more than the declared Content-Length」截断。
	limit := int64(-1)
	if response.ContentLength >= 0 {
		limit = response.ContentLength
	}
	resumes := 0
	failures := 0
	deadline := time.Now().Add(nativeRelayMaxDuration)
	current := address
	// 上一次续传打开的连接，续传前先显式关掉：放在 defer 里会随循环累积，
	// 反复断流时同时挂着好几条上游连接。
	closeCurrent := func() {}
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 && limit >= 0 && written+int64(n) > limit {
			n = max(0, int(limit-written))
		}
		if n > 0 {
			if _, writeErr := writer.Write(buffer[:n]); writeErr != nil {
				closeCurrent()
				return
			}
			written += int64(n)
			failures = 0
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		// 已经写满声明的 Content-Length：上游还可能有数据，但再读下去只会
		// 被截成 0 字节空转，必须在这里收尾。
		if limit >= 0 && written >= limit {
			closeCurrent()
			return
		}
		if readErr == io.EOF {
			closeCurrent()
			return
		}
		if readErr == nil {
			continue
		}
		if ctx.Err() != nil || method != http.MethodGet {
			closeCurrent()
			return
		}
		if total >= 0 && start+written >= total {
			closeCurrent()
			return
		}
		if resumes >= nativeRelayMaxResumes || time.Now().After(deadline) {
			stream.nativeLogError(info, "relay_give_up", current, readErr, "")
			closeCurrent()
			return
		}
		resumes++
		failures++
		stream.nativeLogError(info, "relay_resume", current, readErr, "")
		select {
		case <-time.After(nativeRelayBackoff(resumes)):
		case <-ctx.Done():
			closeCurrent()
			return
		}
		offset := start + written
		// 连续几次都续不上，说明这条链多半已经废了（签名过期 / 节点拉黑）：
		// 先换同片的备用直链，不行再让站源重新解析一条新地址。
		if failures >= nativeRelayFailBeforeSwitch {
			if switched, next, closer, err := stream.nativeRelaySwitch(ctx, upstream, session, assetKey, current, offset); err == nil {
				stream.nativeLog(info, "relay_switch", next, "from="+nativeStreamHost(current), 0, "")
				closeCurrent()
				closeCurrent, reader = closer, switched
				current = next
				failures = 0
				continue
			}
			failures = 0
		}
		next, closer, err := stream.nativeResumeFrom(ctx, upstream, current, offset)
		if err != nil || next == nil {
			stream.nativeLogError(info, "relay_resume_failed", current, err, "")
			closeCurrent()
			return
		}
		closeCurrent()
		closeCurrent, reader = closer, next
	}
}

func nativeStreamHost(address string) string {
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return ""
}

// nativeRelayBackoff 是断流后的重连间隔。前几次几乎立刻重连（CDN 主动掐断，
// 等久了播放器缓冲就见底），连续失败时逐步退避，避免把站方限流打得更死。
func nativeRelayBackoff(resumes int) time.Duration {
	delay := time.Duration(resumes) * 80 * time.Millisecond
	if delay > 1200*time.Millisecond {
		delay = 1200 * time.Millisecond
	}
	if delay < 80*time.Millisecond {
		delay = 80 * time.Millisecond
	}
	return delay
}

// nativeRelaySwitch 找一条还能用的地址接管当前的断点。
//
// 顺序是「重新解析 → 备用直链」：备用直链和主链来自同一份详情页，签名是
// 一起生成的，过期也是一起过期，断流时先试它们基本是白等；重新解析拿到的是
// 刚签好的新地址，命中率最高。只有重新解析失败（详情页打不开 / 没给地址）
// 才退回同一批备用直链。
func (stream *nativeStreamServer) nativeRelaySwitch(ctx context.Context, upstream *http.Request, session *nativeStreamSession, assetKey, current string, offset int64) (*bufio.Reader, string, func(), error) {
	var lastErr error
	if reissued, err := session.reloadAddress(ctx); err == nil && reissued != "" && reissued != current {
		if reader, closer, resumeErr := stream.nativeResumeFrom(ctx, upstream, reissued, offset); resumeErr == nil {
			session.promoteAddress(assetKey, reissued)
			return reader, reissued, closer, nil
		} else {
			lastErr = resumeErr
		}
	}
	for _, candidate := range session.relayCandidates(current) {
		reader, closer, err := stream.nativeResumeFrom(ctx, upstream, candidate, offset)
		if err == nil {
			session.promoteAddress(assetKey, candidate)
			return reader, candidate, closer, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的备用线路")
	}
	return nil, "", nil, lastErr
}

// nativeResume 从 offset 处重新拉一段。返回的 reader 已经跳到 offset，
// 调用方接着往下写即可，字节不会重复也不会缺失。
func (stream *nativeStreamServer) nativeResume(ctx context.Context, upstream *http.Request, finalURL *url.URL, offset int64) (*bufio.Reader, func(), error) {
	if finalURL == nil {
		return nil, nil, errors.New("缺少续传地址")
	}
	return stream.nativeResumeFrom(ctx, upstream, finalURL.String(), offset)
}

// nativeResumeFrom 用指定地址从 offset 处续拉。
func (stream *nativeStreamServer) nativeResumeFrom(ctx context.Context, upstream *http.Request, address string, offset int64) (*bufio.Reader, func(), error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header = upstream.Header.Clone()
	// If-Range 会让 CDN 在 ETag 不匹配时退回 200 整片，续传就白做了。
	request.Header.Del("If-Range")
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	response, err := stream.nativeRequest(request)
	if err != nil {
		return nil, nil, err
	}
	closer := func() { _ = response.Body.Close() }
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		closer()
		return nil, nil, fmt.Errorf("续传失败 HTTP %d", response.StatusCode)
	}
	skip := offset
	if response.StatusCode == http.StatusPartialContent {
		actual, _, _, valid := nativeContentRange(response.Header.Get("Content-Range"))
		if !valid {
			// 不少 CDN 会回 206 却不带 Content-Range。直接判死等于把能续的连接扔掉，
			// 这里按「从 offset 开始」乐观处理。
			skip = 0
		} else if actual > offset {
			// 从更晚的位置开始给，说明中间有缺口，接不上；换线路去。
			closer()
			return nil, nil, fmt.Errorf("续传位置超前（%d > %d）", actual, offset)
		} else {
			skip = offset - actual
		}
	} else if offset > nativeRelayMaxSkip {
		// 服务器不支持 Range，只能从头丢字节。丢几兆还能接受，丢得多不如换线路。
		closer()
		return nil, nil, fmt.Errorf("续传不受支持（HTTP 200，需丢弃 %d 字节）", offset)
	}
	body := newNativeIdleReader(response.Body)
	if skip > 0 {
		if _, err := io.CopyN(io.Discard, body, skip); err != nil {
			_ = body.Close()
			return nil, nil, err
		}
	}
	return bufio.NewReaderSize(body, 64<<10), func() { _ = body.Close() }, nil
}
