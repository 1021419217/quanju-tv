package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSiteAnalyzerGeneratedRuleRunsThroughEngine 补上游测试没覆盖的一环：
// 生成的规则不仅要能 parseXBPQRule，还必须真的走引擎的目录 / 详情 / 分集
// 消费路径产出内容。上游只断言「数组锚点在样本 HTML 上能截出 2 条」，
// 这正是「UI 显示识别成功、保存后却播不了」的盲区。
func TestSiteAnalyzerGeneratedRuleRunsThroughEngine(t *testing.T) {
	server := httptest.NewServer(newSiteAnalyzerFixture())
	defer server.Close()
	d := &Downloader{
		cfg:           Config{Retries: 1},
		client:        server.Client(),
		limiter:       newRequestLimiter(20, 0),
		providerHosts: map[string]string{},
	}
	result, err := d.analyzeCustomSourceSite(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("自动识别失败: %v", err)
	}
	if result.Mode != "规则" || result.Rule == "" {
		t.Fatalf("未生成规则（mode=%s）：%v", result.Mode, result.Notes)
	}
	rule, ok := parseXBPQRule(result.Rule)
	if !ok {
		t.Fatalf("规则无法被引擎解析: %s", result.Rule)
	}

	// 引擎实际请求分类页时用的 URL（不是分析器自己拼的那个）。
	engineURL := xbpqCategoryURL(rule, server.URL, "1", 1)
	t.Logf("引擎分类页 URL = %s", engineURL)
	t.Logf("规则分类url = %s", rule.field("分类url"))
	t.Logf("规则数组 = %q 标题 = %q 链接 = %q",
		rule.field("数组"), rule.field("标题"), rule.field("链接"))

	dramas, _, catalogErr := d.xbpqCatalog(context.Background(), "custom:e2e", server.URL, 1, "1", rule)
	t.Logf("引擎目录结果: %d 部, err=%v", len(dramas), catalogErr)
	if catalogErr != nil || len(dramas) == 0 {
		t.Errorf("生成的规则在引擎目录路径下取不到剧目")
		return
	}

	detail, chapters, detailErr := d.xbpqDetail(
		context.Background(), "custom:e2e", server.URL, dramas[0].SourceID, rule)
	t.Logf("引擎详情结果: title=%q 分集=%d err=%v (sourceID=%q)",
		detail.Title, len(chapters), detailErr, dramas[0].SourceID)
	if detailErr != nil || len(chapters) == 0 {
		t.Errorf("生成的规则在引擎详情路径下取不到分集")
	}
}

// newFalsePositiveFixture 首页有正常分类导航，但分类页只返回一句「暂无内容」。
// 分析器仍能拼出能 parseXBPQRule 的规则（主页url/分类url/分类齐全），
// 列表字段却全被省略；引擎拿着这条规则去取分类页必然 0 条。
func newFalsePositiveFixture() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if request.URL.Path == "/" {
			fmt.Fprint(writer, `<!doctype html><html><body><nav>`+
				`<a href="/vodtype/1.html">电影</a>`+
				`<a href="/vodtype/2.html">电视剧</a>`+
				`<a href="/vodtype/3.html">综艺</a>`+
				`</nav>`+strings.Repeat(`<p>首页推荐占位内容</p>`, 20)+`</body></html>`)
			return
		}
		fmt.Fprint(writer, `<html><body>暂无内容</body></html>`)
	})
}

// TestSiteAnalyzerFalsePositiveRule 复现「UI 显示识别成功、保存后播不了」。
func TestSiteAnalyzerFalsePositiveRule(t *testing.T) {
	server := httptest.NewServer(newFalsePositiveFixture())
	defer server.Close()
	d := &Downloader{
		cfg:           Config{Retries: 1},
		client:        server.Client(),
		limiter:       newRequestLimiter(20, 0),
		providerHosts: map[string]string{},
	}
	result, err := d.analyzeCustomSourceSite(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("自动识别失败: %v", err)
	}
	t.Logf("mode=%s rule=%q notes=%v", result.Mode, result.Rule, result.Notes)
	// 修复前这里会返回 mode=规则 + 一条只有 主页url/分类url/分类 的空壳规则，
	// 引擎实测报「规则未抽取到列表条目」；现在必须回落普通网页源并说明原因。
	if result.Mode != "网址" || result.Rule != "" {
		t.Fatalf("取不到列表条目时不应给出假阳性规则：mode=%s rule=%q", result.Mode, result.Rule)
	}
	if !strings.Contains(strings.Join(result.Notes, ""), "引擎实测") {
		t.Fatalf("回落时诊断里没有说明实测结果: %v", result.Notes)
	}
}

// newFetchFailureFixture 首页正常，但分类页一律 502：自动识别能拼出规则，
// 引擎实测却因抓取失败无法判定。这种情况不能把规则判死（用户可能是慢站/盾页）。
func newFetchFailureFixture() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(writer, `<!doctype html><html><body><nav>`+
				`<a href="/vodtype/1.html">电影</a>`+
				`<a href="/vodtype/2.html">电视剧</a>`+
				`</nav>`+strings.Repeat(`<p>首页推荐占位内容</p>`, 20)+`</body></html>`)
			return
		}
		writer.WriteHeader(http.StatusBadGateway)
	})
}

func TestSiteAnalyzerFetchFailureDoesNotDiscardRule(t *testing.T) {
	server := httptest.NewServer(newFetchFailureFixture())
	defer server.Close()
	d := &Downloader{
		cfg:           Config{Retries: 1},
		client:        server.Client(),
		limiter:       newRequestLimiter(20, 0),
		providerHosts: map[string]string{},
	}
	result, err := d.analyzeCustomSourceSite(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("自动识别失败: %v", err)
	}
	t.Logf("mode=%s catalogs=%d notes=%v", result.Mode, result.Catalogs, result.Notes)
	if result.Mode != "规则" || result.Rule == "" {
		t.Fatalf("抓取失败不该把规则判死：mode=%s rule=%q", result.Mode, result.Rule)
	}
	if result.Catalogs != 0 {
		t.Fatalf("未完成实测却报了剧目数: %d", result.Catalogs)
	}
	if !strings.Contains(strings.Join(result.Notes, ""), "实测未能完成") {
		t.Fatalf("未标注「未经实测验证」: %v", result.Notes)
	}
}

// TestCustomSourceEmptyNameRejected 实证 3.8「保存按钮点一下闪一下」的根因：
// Go 核心要求源名称必填，而识别不出规则时 UI 不回填名称，保存必然被后端拒绝。
func TestCustomSourceEmptyNameRejected(t *testing.T) {
	registry := newCustomMaccmsRegistry(t.TempDir())
	_, err := registry.add("", "https://example.com", "", "")
	if err == nil || !strings.Contains(err.Error(), "源名称") {
		t.Fatalf("空名称应当被拒，实际: %v", err)
	}
}

// TestCustomSourceAutoNameFromBase 验证 UI 兜底补名后，普通网页源能正常落盘。
func TestCustomSourceAutoNameFromBase(t *testing.T) {
	registry := newCustomMaccmsRegistry(t.TempDir())
	record, err := registry.add("example.com", "https://example.com", "", "web")
	if err != nil {
		t.Fatalf("补齐名称后仍无法保存: %v", err)
	}
	if record.Name != "example.com" || record.Base != "https://example.com" {
		t.Fatalf("落盘记录不符: %+v", record)
	}
}

// TestSiteAnalyzerFallbackKeepsSiteIdentity 任何回落路径都必须带回站点标识，
// 否则 UI 上就是「啥也识别不出来」，且名称为空会连带导致保存失败。
func TestSiteAnalyzerFallbackKeepsSiteIdentity(t *testing.T) {
	for name, fixture := range map[string]http.Handler{
		"抓取失败保留规则": newFetchFailureFixture(),
		"假阳性回落":    newFalsePositiveFixture(),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(fixture)
			defer server.Close()
			d := &Downloader{
				cfg:           Config{Retries: 1},
				client:        server.Client(),
				limiter:       newRequestLimiter(20, 0),
				providerHosts: map[string]string{},
			}
			result, err := d.analyzeCustomSourceSite(context.Background(), server.URL)
			if err != nil {
				t.Fatalf("自动识别失败: %v", err)
			}
			if result.Name == "" || result.Base == "" {
				t.Fatalf("回落路径丢了站点标识: %+v", result)
			}
			if !strings.Contains(strings.Join(result.Notes, ""), "识别目标") {
				t.Fatalf("诊断区缺少站点标识: %v", result.Notes)
			}
		})
	}
}
