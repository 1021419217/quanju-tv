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

// nativeRelayBody 把上游媒体流转发给播放器，并在上游掐断时从断点续拉。
//
// 91porn 这类站的 CDN 会在几秒到几十秒后主动断开长连接，播放器看到的就是
// 「传输中断」。这里记录已经写出的字节数，断了就用 Range 从断点再拉一段接上，
// 播放器全程只看到一条连续的流；只有真的续不上时才让连接结束。
//
// 只对显式标记了 resilient 的媒体凭据启用（目前只有 91porn），
// 其它站源的传输路径完全不变。
func (stream *nativeStreamServer) nativeRelayBody(ctx context.Context, writer http.ResponseWriter, method string, upstream *http.Request, finalURL *url.URL, response *http.Response, reader *bufio.Reader, info nativeStreamDiagnostic) {
	start, total := int64(0), int64(-1)
	if a, _, t, valid := nativeContentRange(response.Header.Get("Content-Range")); valid {
		start, total = a, t
	} else if response.StatusCode == http.StatusOK && response.ContentLength >= 0 {
		total = response.ContentLength
	}
	written := int64(0)
	resumes := 0
	// 上一次续传打开的连接，续传前先显式关掉：放在 defer 里会随循环累积，
	// 反复断流时同时挂着好几条上游连接。
	closeCurrent := func() {}
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if _, writeErr := writer.Write(buffer[:n]); writeErr != nil {
				closeCurrent()
				return
			}
			written += int64(n)
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
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
		if resumes >= nativeRelayMaxResumes {
			stream.nativeLogError(info, "relay_give_up", finalURL.String(), readErr, "")
			closeCurrent()
			return
		}
		resumes++
		stream.nativeLogError(info, "relay_resume", finalURL.String(), readErr, "")
		// 断流多半是 CDN 主动掐，不是网络抖，等太久播放器缓冲就见底了。
		// 前三次几乎立刻重连，后面再逐步退避。
		delay := time.Duration(min(resumes, 3)) * 80 * time.Millisecond
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			closeCurrent()
			return
		}
		next, closer, err := stream.nativeResume(ctx, upstream, finalURL, start+written)
		if err != nil || next == nil {
			stream.nativeLogError(info, "relay_resume_failed", finalURL.String(), err, "")
			closeCurrent()
			return
		}
		closeCurrent()
		closeCurrent, reader = closer, next
	}
}

// nativeRelayMaxResumes 是一条播放连接最多续传几次。站方 CDN 掐得很勤
// （有的几十秒一次），次数太少等于没做，太多又会让已经死掉的地址拖住播放器。
const nativeRelayMaxResumes = 16

// nativeResume 从 offset 处重新拉一段。返回的 reader 已经跳到 offset，
// 调用方接着往下写即可，字节不会重复也不会缺失。
func (stream *nativeStreamServer) nativeResume(ctx context.Context, upstream *http.Request, finalURL *url.URL, offset int64) (*bufio.Reader, func(), error) {
	target := finalURL
	if target == nil {
		target = upstream.URL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header = upstream.Header.Clone()
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
			closer()
			return nil, nil, errors.New("续传缺少 Content-Range")
		}
		// 服务器从更早的位置开始给：先把多出来的字节读掉；
		// 从更晚的位置开始给说明中间有缺口，这段数据接不上，直接放弃。
		if actual > offset {
			closer()
			return nil, nil, fmt.Errorf("续传位置超前（%d > %d）", actual, offset)
		}
		skip = offset - actual
	}
	if skip > 0 {
		if _, err := io.CopyN(io.Discard, response.Body, skip); err != nil {
			closer()
			return nil, nil, err
		}
	}
	return bufio.NewReaderSize(response.Body, 64<<10), closer, nil
}
