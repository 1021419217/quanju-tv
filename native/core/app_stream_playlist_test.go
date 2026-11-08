package core

import (
	"context"
	"errors"
	"testing"
)

// 分片签名过期时，刷新播放列表必须把「同一个分片」的地址换成新签名，
// 但资源 ID 保持不变——否则播放器手里的本地地址会全部失效，等于重新起播。
func TestNativeRefreshSegmentsSwapsSignedAddress(t *testing.T) {
	oldSegment := "https://cdn.example.com/hls/a/seg0.ts?auth_key=100-old"
	stream := &nativeStreamServer{sessions: map[string]*nativeStreamSession{}}
	refreshed := false
	session := &nativeStreamSession{
		assets: map[string]nativeStreamAsset{
			"asset0": {kind: "segment", address: oldSegment},
			"asset1": {kind: "entry_playlist", address: "https://kissavs.com/media/m3u8/?url=x"},
		},
		refreshPlaylist: func(context.Context) (string, error) {
			refreshed = true
			return "#EXTM3U\n#EXTINF:10,\nhttps://cdn.example.com/hls/a/seg0.ts?auth_key=200-new\n#EXT-X-ENDLIST\n", nil
		},
	}

	if !stream.nativeRefreshSegments(context.Background(), session) {
		t.Fatal("应替换成功")
	}
	if !refreshed {
		t.Fatal("应调用刷新回调")
	}
	if got := session.assets["asset0"].address; got != "https://cdn.example.com/hls/a/seg0.ts?auth_key=200-new" {
		t.Fatalf("分片地址未更新：%s", got)
	}
	if _, ok := session.assets["asset0"]; !ok {
		t.Fatal("资源 ID 必须保持不变")
	}
}

// 没有新地址可换时不能假装成功，否则上层会反复重试同一个死请求。
func TestNativeRefreshSegmentsNoChange(t *testing.T) {
	stream := &nativeStreamServer{sessions: map[string]*nativeStreamSession{}}
	session := &nativeStreamSession{
		assets: map[string]nativeStreamAsset{
			"asset0": {kind: "segment", address: "https://cdn.example.com/hls/a/seg0.ts?auth_key=100"},
		},
		refreshPlaylist: func(context.Context) (string, error) {
			return "#EXTM3U\n#EXTINF:10,\nhttps://cdn.example.com/hls/other/seg9.ts?auth_key=200\n", nil
		},
	}
	if stream.nativeRefreshSegments(context.Background(), session) {
		t.Fatal("没有对应分片时不应报告成功")
	}
}

func TestNativeRefreshSegmentsBudget(t *testing.T) {
	stream := &nativeStreamServer{sessions: map[string]*nativeStreamSession{}}
	calls := 0
	session := &nativeStreamSession{
		assets: map[string]nativeStreamAsset{},
		refreshPlaylist: func(context.Context) (string, error) {
			calls++
			return "", errors.New("no")
		},
	}
	for i := 0; i < 10; i++ {
		stream.nativeRefreshSegments(context.Background(), session)
	}
	if calls > nativeMaxPlaylistRefreshes {
		t.Fatalf("刷新次数应受上限约束，实际 %d", calls)
	}
}
