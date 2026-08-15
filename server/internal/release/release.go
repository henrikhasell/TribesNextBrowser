// Package release answers one question for the download page: what is the
// newest .vl2 build, and where is it?
//
// The archives are published by .github/workflows/vl2.yml, one release per
// commit tagged build-<sha>, with --latest moving GitHub's own pointer at the
// newest. That pointer gives permanent download URLs that need no API call:
//
//	https://github.com/<repo>/releases/latest/download/TNBrowser.vl2
//
// Those alone would be enough to serve two buttons. The API is queried on top
// of them so the page can say WHICH build it is offering -- the tag, the date,
// the size, the notes -- which is the difference between a download link and a
// release page.
//
// Everything here is therefore best-effort. GitHub allows sixty unauthenticated
// requests an hour per address, and that is per address rather than per
// visitor, so an afternoon of traffic would exhaust it in minutes without a
// cache. When the call fails, or the budget is gone, or GitHub is simply down,
// the page still renders and the buttons still work -- they fall back to the
// permanent URLs, which were never conditional on any of this.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// DefaultRepo is the repository the published archives come from. It is the
// slug .do/app.yaml and .github/workflows/vl2.yml both name.
const DefaultRepo = "henrikhasell/TribesNextBrowser"

// DefaultTag is the release this server was built alongside, and the answer the
// download page gives when GitHub cannot be asked for a better one.
//
// It is a constant rather than something discovered, because there is nothing
// to discover it from: the deployed binary is built from a branch, not from a
// tag, so the build knows no version number. Bump it in the same commit that
// gets tagged and the two cannot disagree:
//
//	edit this line -> commit -> git tag -a v1.2.0 -> git push origin v1.2.0
//
// Being out of date costs a stale version number on one panel while GitHub is
// unreachable, and nothing else -- the live lookup overrides it whenever it
// works.
const DefaultTag = "v1.1.0"

// Archives are the two files a release carries, in the order the page shows
// them: the client package first, because most visitors are players.
var Archives = []string{"TNBrowser.vl2", "TNBrowserServer.vl2"}

// DefaultTTL is how long a good answer is reused. Long enough that a busy day
// costs six requests an hour out of sixty; short enough that a new build shows
// up while somebody is still reading about the last one.
const DefaultTTL = 10 * time.Minute

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Size is bytes, or 0 when this is a fallback entry and nothing has told
	// us how big the file is.
	Size int64 `json:"size"`
}

// Latest is what the download page renders.
//
// Every field except Assets may be empty: that is the fallback, and the page is
// expected to show the buttons without a version beside them rather than show
// nothing.
type Latest struct {
	Repo      string  `json:"repo"`
	Tag       string  `json:"tag,omitempty"`
	Published string  `json:"published,omitempty"`
	Notes     string  `json:"notes,omitempty"`
	URL       string  `json:"url,omitempty"` // the release's own page
	Assets    []Asset `json:"assets"`

	// Fallback reports that GitHub could not be asked and these are the
	// permanent URLs rather than a described release. The page says so, quietly
	// -- a visitor who downloads an unlabelled file should know why it is
	// unlabelled.
	Fallback bool `json:"fallback"`
}

// Cache holds the last good answer and refuses to ask GitHub more often than
// its TTL. Safe for concurrent use.
type Cache struct {
	repo string
	// tag is the release to name when GitHub cannot be reached. Empty means
	// answer without naming one, and link at /releases/latest/ instead.
	tag    string
	ttl    time.Duration
	client *http.Client

	// api is the base for the REST call, swapped by tests so the suite never
	// touches the network.
	api string

	mu      sync.Mutex
	last    Latest
	fetched time.Time
	valid   bool
}

// New builds a cache for a repository. An empty repo disables the API call
// entirely and every answer is the fallback -- which is a supported
// configuration, not a broken one: a fork with no releases of its own still
// wants a working download page.
//
// tag is the release the fallback names; see DefaultTag. Empty is allowed and
// means the fallback links at /releases/latest/ without claiming to know which
// version that is.
func New(repo, tag string, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Cache{
		repo: repo,
		tag:  tag,
		ttl:  ttl,
		api:  "https://api.github.com",
		// A timeout, because the download page is rendered behind this call and
		// a visitor should not wait on GitHub's worst day.
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Get answers with the newest release, and never with an error.
//
// A failure is not the caller's problem to handle -- there is nothing useful it
// could do differently -- so it is absorbed here into the best answer
// available: a fresh one, then the last good one however old, then the
// permanent URLs.
func (c *Cache) Get(ctx context.Context) Latest {
	if c == nil || c.repo == "" {
		return c.fallback()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Held across the fetch on purpose. It serialises a burst of first requests
	// into one call to GitHub, which is exactly the traffic the rate limit
	// cares about; the cost is that a few page loads wait behind a five-second
	// timeout on the very first request, and never again.
	if c.valid && time.Since(c.fetched) < c.ttl {
		return c.last
	}

	got, err := c.fetch(ctx)
	if err != nil {
		if c.valid {
			// Stale, and better than nothing: the archives it names are still
			// there, and a slightly old version number beats no page.
			return c.last
		}
		return c.fallback()
	}

	c.last, c.fetched, c.valid = got, time.Now(), true
	return got
}

// githubRelease is the slice of GitHub's release JSON this needs.
type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Assets      []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (c *Cache) fetch(ctx context.Context) (Latest, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.api, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Latest{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.client.Do(req)
	if err != nil {
		return Latest{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Latest{}, fmt.Errorf("github answered %s", resp.Status)
	}

	var gh githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return Latest{}, err
	}

	out := Latest{
		Repo:      c.repo,
		Tag:       gh.TagName,
		Published: gh.PublishedAt,
		Notes:     gh.Body,
		URL:       gh.HTMLURL,
		Assets:    []Asset{},
	}

	// Only the two archives, in the order Archives lists them, so the page's
	// buttons do not reorder themselves because a workflow attached something
	// else. A release missing one of them lists the other.
	for _, want := range Archives {
		for _, a := range gh.Assets {
			if a.Name == want {
				out.Assets = append(out.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
				break
			}
		}
	}
	if len(out.Assets) == 0 {
		return Latest{}, fmt.Errorf("release %s carries no .vl2", gh.TagName)
	}
	return out, nil
}

// fallback is the answer that needs nothing to be reachable.
//
// With a configured tag it names that release and links straight at it. That
// pairing is the point: a page that printed "v1.1.0" beside a /releases/latest/
// link would be claiming to offer a version it might not be offering, the
// moment a newer one is published. Name a version and hand out that version, or
// name none and hand out the newest -- never one and then the other.
//
// Sizes and the publication date are left empty either way. Those are facts
// only GitHub has, and guessing them would be worse than leaving the line off.
func (c *Cache) fallback() Latest {
	repo, tag := DefaultRepo, ""
	if c != nil {
		if c.repo != "" {
			repo = c.repo
		}
		tag = c.tag
	}

	out := Latest{Repo: repo, Tag: tag, Fallback: true, Assets: []Asset{}}

	// "latest" is GitHub's own permanent redirect at the newest release, which
	// .github/workflows/vl2.yml documents as the stable link.
	where := "latest/download"
	out.URL = fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	if tag != "" {
		where = "download/" + tag
		out.URL = fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, tag)
	}

	for _, name := range Archives {
		out.Assets = append(out.Assets, Asset{
			Name: name,
			URL:  fmt.Sprintf("https://github.com/%s/releases/%s/%s", repo, where, name),
		})
	}
	return out
}
