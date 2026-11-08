package core

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// 这两个探针会真的联网，CI 与日常 go test 一律跳过，需要时用
// LIVE_PROBE=1 go test ./core -run TestLiveProbe -v 手动跑。
func liveProbeEnabled(t *testing.T) bool {
	if os.Getenv("LIVE_PROBE") != "1" {
		t.Skip("需要 LIVE_PROBE=1")
		return false
	}
	return true
}

// 真实联网探针：只在手动执行时运行（-run TestLiveProbeViralAndKissav）。
func liveProbeDownloader() *Downloader {
	cfg := defaultConfig()
	d := &Downloader{cfg: cfg, providerHosts: map[string]string{}, limiter: newRequestLimiter(6, 150*time.Millisecond)}
	d.client = &http.Client{Timeout: 40 * time.Second}
	return d
}

func TestLiveProbeViral(t *testing.T) {
	if !liveProbeEnabled(t) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	d := liveProbeDownloader()

	items, more, err := d.fetchViralCatalogPage(ctx, 1, "latest", "")
	if err != nil {
		t.Fatalf("目录失败: %v", err)
	}
	t.Logf("目录 %d 条 hasMore=%v", len(items), more)
	if len(items) == 0 {
		t.Fatal("目录为空")
	}
	for i := 0; i < 2 && i < len(items); i++ {
		t.Logf("  [%d] id=%s title=%q cover=%v dur=%v author=%q/%q", i, items[i].SourceID, items[i].Title, items[i].Cover, items[i].Duration, items[i].Author, items[i].AuthorID)
	}

	drama, chapters, err := d.fetchViralDetail(ctx, items[0].SourceID)
	if err != nil {
		t.Fatalf("详情失败: %v", err)
	}
	t.Logf("详情 title=%q dur=%v chapters=%d url=%s", drama.Title, drama.Duration, len(chapters), chapters[0].VideoURL)
	if chapters[0].VideoURL == "" {
		t.Fatal("详情没有播放地址")
	}

	media, err := d.resolveViralMedia(ctx, Task{DramaID: drama.ID, Chapter: chapters[0]})
	if err != nil {
		t.Fatalf("取流失败: %v", err)
	}
	t.Logf("取流 URL=%s referer=%s", media.URL, media.Referer)

	items2, _, err := d.fetchViralCatalogPage(ctx, 1, "", "sophie")
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	t.Logf("搜索 %d 条", len(items2))

	items3, _, err := d.fetchViralCatalogPage(ctx, 2, "onlyfans", "")
	if err != nil {
		t.Fatalf("分类分页失败: %v", err)
	}
	t.Logf("分类第2页 %d 条 first=%s", len(items3), items3[0].SourceID)
}

func TestLiveProbeKissav(t *testing.T) {
	if !liveProbeEnabled(t) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	d := liveProbeDownloader()

	items, _, err := d.fetchKissavCatalogPage(ctx, 1, "new", "")
	if err != nil {
		t.Fatalf("目录失败: %v", err)
	}
	t.Logf("目录 %d 条", len(items))
	if len(items) == 0 {
		t.Fatal("目录为空")
	}
	t.Logf("  [0] id=%s title=%q cover=%v", items[0].SourceID, items[0].Title, items[0].Cover)

	drama, chapters, err := d.fetchKissavDetail(ctx, items[0].SourceID)
	if err != nil {
		t.Fatalf("详情失败: %v", err)
	}
	t.Logf("详情 title=%q dur=%v url=%s", drama.Title, drama.Duration, chapters[0].VideoURL)

	media, err := d.resolveKissavMedia(ctx, Task{DramaID: drama.ID, Chapter: chapters[0]})
	if err != nil {
		t.Fatalf("取流失败: %v", err)
	}
	head := media.Playlist
	if len(head) > 200 {
		head = head[:200]
	}
	t.Logf("取流 playlist %d 字节: %s", len(media.Playlist), head)
}
