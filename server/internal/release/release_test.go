package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A trimmed copy of what api.github.com answers for a release the vl2 workflow
// published, including an extra asset to prove the filter works.
const sample = `{
  "tag_name": "build-0d5b852",
  "html_url": "https://github.com/henrikhasell/TribesNextBrowser/releases/tag/build-0d5b852",
  "published_at": "2026-08-12T22:41:03Z",
  "body": "Built from ` + "`0d5b852`" + `, pointing at https://tnb.k8s.henrik.si",
  "assets": [
    {"name": "notes.txt", "size": 12, "browser_download_url": "https://example.invalid/notes.txt"},
    {"name": "TNBrowserServer.vl2", "size": 41000, "browser_download_url": "https://example.invalid/TNBrowserServer.vl2"},
    {"name": "TNBrowser.vl2", "size": 184320, "browser_download_url": "https://example.invalid/TNBrowser.vl2"}
  ]
}`

// stub stands in for api.github.com and counts what reached it.
func stub(t *testing.T, status int, body string) (*Cache, *atomic.Int64) {
	t.Helper()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if want := "/repos/henrikhasell/TribesNextBrowser/releases/latest"; r.URL.Path != want {
			t.Errorf("asked for %s, want %s", r.URL.Path, want)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := New(DefaultRepo, DefaultTag, time.Minute)
	c.api = srv.URL
	return c, &hits
}

func TestLatestIsParsedAndFilteredToTheArchives(t *testing.T) {
	c, _ := stub(t, http.StatusOK, sample)

	got := c.Get(context.Background())
	if got.Fallback {
		t.Fatal("a good answer was reported as a fallback")
	}
	if got.Tag != "build-0d5b852" {
		t.Errorf("tag %q", got.Tag)
	}
	if got.Published != "2026-08-12T22:41:03Z" {
		t.Errorf("published %q", got.Published)
	}
	if !strings.Contains(got.Notes, "tnb.k8s.henrik.si") {
		t.Errorf("notes %q", got.Notes)
	}

	// Two assets, and in the order Archives lists them rather than the order
	// GitHub returned -- the page's buttons must not swap places.
	if len(got.Assets) != 2 {
		t.Fatalf("%d assets, want the two archives: %+v", len(got.Assets), got.Assets)
	}
	if got.Assets[0].Name != "TNBrowser.vl2" || got.Assets[1].Name != "TNBrowserServer.vl2" {
		t.Errorf("assets out of order: %+v", got.Assets)
	}
	if got.Assets[0].Size != 184320 {
		t.Errorf("size %d", got.Assets[0].Size)
	}
}

func TestAnswerIsCachedForTheTTL(t *testing.T) {
	c, hits := stub(t, http.StatusOK, sample)

	for i := 0; i < 5; i++ {
		if got := c.Get(context.Background()); got.Tag != "build-0d5b852" {
			t.Fatalf("call %d: tag %q", i, got.Tag)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("%d requests to GitHub, want 1 -- the cache is what keeps us inside the rate limit", n)
	}
}

// With GitHub down, the page still names the configured release and links
// straight at it.
func TestAFailedFetchFallsBackToTheConfiguredRelease(t *testing.T) {
	c, _ := stub(t, http.StatusInternalServerError, "")

	got := c.Get(context.Background())
	if !got.Fallback {
		t.Error("a failed fetch was not reported as a fallback")
	}
	if got.Tag != DefaultTag {
		t.Errorf("tag %q, want the configured %q", got.Tag, DefaultTag)
	}
	if len(got.Assets) != 2 {
		t.Fatalf("%d assets, want both archives even with GitHub down", len(got.Assets))
	}

	// Named and linked must agree: printing a version beside a /latest/ link
	// would claim to offer a build that a later release would silently replace.
	want := "https://github.com/henrikhasell/TribesNextBrowser/releases/download/" +
		DefaultTag + "/TNBrowser.vl2"
	if got.Assets[0].URL != want {
		t.Errorf("download URL %q, want %q", got.Assets[0].URL, want)
	}
	if wantPage := "https://github.com/henrikhasell/TribesNextBrowser/releases/tag/" + DefaultTag; got.URL != wantPage {
		t.Errorf("release page %q, want %q", got.URL, wantPage)
	}

	// Nothing is invented: only GitHub knows these.
	if got.Published != "" || got.Assets[0].Size != 0 {
		t.Errorf("a fallback claimed facts it has no source for: %+v", got)
	}
}

// With no tag configured either, it names nothing and hands out whatever
// GitHub's own "latest" pointer currently resolves to.
func TestAFallbackWithNoTagLinksAtLatest(t *testing.T) {
	c, _ := stub(t, http.StatusInternalServerError, "")
	c.tag = ""

	got := c.Get(context.Background())
	if got.Tag != "" {
		t.Errorf("a fallback named a version it cannot know: %q", got.Tag)
	}
	want := "https://github.com/henrikhasell/TribesNextBrowser/releases/latest/download/TNBrowser.vl2"
	if got.Assets[0].URL != want {
		t.Errorf("download URL %q, want %q", got.Assets[0].URL, want)
	}
}

// A cache that answered once keeps answering after GitHub stops: a version
// number ten minutes out of date is better than a page with no version on it.
func TestALastGoodAnswerSurvivesAFailure(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(sample))
	}))
	defer srv.Close()

	// Zero TTL is not allowed by New (it means "use the default"), so expire it
	// by hand instead, which is what a real ten minutes would do.
	c := New(DefaultRepo, DefaultTag, time.Minute)
	c.api = srv.URL

	if got := c.Get(context.Background()); got.Tag == "" {
		t.Fatal("the first call did not succeed")
	}

	fail.Store(true)
	c.mu.Lock()
	c.fetched = time.Now().Add(-time.Hour)
	c.mu.Unlock()

	got := c.Get(context.Background())
	if got.Tag != "build-0d5b852" {
		t.Errorf("tag %q, want the stale-but-good answer", got.Tag)
	}
	if got.Fallback {
		t.Error("a stale good answer was downgraded to the fallback")
	}
}

// A deployment with no repository configured still serves a download page.
func TestNoRepoIsTheFallbackWithNoRequest(t *testing.T) {
	got := New("", DefaultTag, time.Minute).Get(context.Background())
	if !got.Fallback || len(got.Assets) != 2 {
		t.Errorf("unconfigured cache answered %+v", got)
	}
}
