package seku

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 海角（fanguo 槽位）的播放清单只能在本地重建：站点只给 AES-128 加密的
// 预览清单，key 还要用同目录 .jpg 的内容做一次异或才能还原。播放器自己
// 拿不到这个 key，所以 Go 侧重写清单、把派生出的 key 落到本地。
//
// 但 iOS 的 AVPlayer 不会加载 file:// 的 HLS 清单（部分安卓播放器同理），
// 表现就是「明明解析出了地址，点了却播不了」。这里在 127.0.0.1 上起一个
// 只读小服务，把重建好的清单和 key 用 http 发出去，播放器就把它当普通
// 在线流处理，远程分片照旧直连 CDN。
var (
	localStreamOnce sync.Once
	localStreamBase string
	localStreamErr  error
)

func localStreamURL(name string) (string, error) {
	localStreamOnce.Do(func() {
		dir := filepath.Join(storeBaseDir(), "hjpl")
		mux := http.NewServeMux()
		mux.HandleFunc("/hjpl/", func(w http.ResponseWriter, r *http.Request) {
			f, ok := storeStreamFile(strings.TrimPrefix(r.URL.Path, "/hjpl/"))
			if !ok {
				http.NotFound(w, r)
				return
			}
			p := filepath.Join(dir, f)
			if _, err := os.Stat(p); err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", "*")
			http.ServeFile(w, r, p)
		})
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			localStreamErr = err
			return
		}
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		localStreamBase = "http://" + ln.Addr().String()
	})
	if localStreamErr != nil {
		return "", localStreamErr
	}
	return localStreamBase + "/hjpl/" + name, nil
}

// storeStreamFile 只放行 <16 位十六进制>.m3u8 / .key，
// 其余一律拒绝，避免路径穿越读到沙箱里的其他文件。
func storeStreamFile(name string) (string, bool) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", false
	}
	var base string
	switch {
	case strings.HasSuffix(name, ".m3u8"):
		base = strings.TrimSuffix(name, ".m3u8")
	case strings.HasSuffix(name, ".key"):
		base = strings.TrimSuffix(name, ".key")
	default:
		return "", false
	}
	if len(base) != 16 {
		return "", false
	}
	for _, c := range base {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", false
		}
	}
	return name, true
}
