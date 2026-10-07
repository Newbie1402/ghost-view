package tiktok

import (
	"context"
	"errors"
	"ghostview/internal/cache"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPublicProfileCapabilities(t *testing.T) {
	p := New()
	want := model.ProviderCapabilities{Search: true, Profile: true, Stories: true, Downloads: true}
	if p.Platform() != model.TikTok || p.Status() != "EXPERIMENTAL" || p.Capabilities() != want {
		t.Fatalf("public-profile adapter metadata: %s %+v", p.Status(), p.Capabilities())
	}
}

// Synthetic fixtures exercise parsing without storing actual platform sessions,
// signing parameters, or live response bodies in the repository.
func fixture(user, stats string) string {
	return `<html><title>Alex | TikTok</title><script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">{"__DEFAULT_SCOPE__":{"webapp.user-detail":{"statusCode":0,"userInfo":{"user":` + user + `,"stats":` + stats + `}}}}</script></html>`
}

const publicUserJSON = `{"id":"6676363774038033409","uniqueId":"public.user","nickname":"Public User","signature":"A <script> caption is plain text","privateAccount":false,"verified":true,"avatarLarger":"https://p16-sign-va.tiktokcdn.com/avatar.jpeg"}`
const statsJSON = `{"followerCount":120,"followingCount":7,"videoCount":4}`

func TestParsePublicProfileAndMissingCounts(t *testing.T) {
	profile, _, err := parseProfile([]byte(fixture(publicUserJSON, statsJSON)), "public.user")
	if err != nil || profile.AccessStatus != model.AccessPublic || profile.DisplayName != "Public User" || profile.AvatarURL == "" || *profile.FollowerCount != 120 || *profile.FollowingCount != 7 || *profile.PostCount != 4 || !profile.Verified {
		t.Fatalf("public profile %+v %v", profile, err)
	}
	profile, _, err = parseProfile([]byte(fixture(publicUserJSON, `{}`)), "public.user")
	if err != nil || profile.FollowerCount != nil || profile.FollowingCount != nil || profile.PostCount != nil {
		t.Fatalf("unknown counts must be absent: %+v %v", profile, err)
	}
}
func TestParsePrivateProfileStripsProtectedData(t *testing.T) {
	html := fixture(strings.Replace(publicUserJSON, `"privateAccount":false`, `"privateAccount":true`, 1), statsJSON)
	profile, _, err := parseProfile([]byte(html), "public.user")
	if err != nil || profile.AccessStatus != model.AccessPrivate || profile.Message != httputil.Private.Message || profile.Bio != "" || profile.AvatarURL != "" || profile.FollowerCount != nil || profile.FollowingCount != nil || profile.PostCount != nil {
		t.Fatalf("private data exposed %+v %v", profile, err)
	}
}
func TestParseProfileFailsClosed(t *testing.T) {
	good := fixture(publicUserJSON, statsJSON)
	cases := map[string]string{
		"missing bootstrap":   "<html>Not accessible</html>",
		"broken json":         strings.Replace(good, `"statusCode":0`, `"statusCode":`, 1),
		"unknown privacy":     strings.Replace(good, `"privateAccount":false,`, "", 1),
		"null privacy":        strings.Replace(good, `"privateAccount":false`, `"privateAccount":null`, 1),
		"nonzero status":      strings.Replace(good, `"statusCode":0`, `"statusCode":10299`, 1),
		"missing status":      strings.Replace(good, `"statusCode":0,`, "", 1),
		"different account":   strings.Replace(good, `"uniqueId":"public.user"`, `"uniqueId":"someone.else"`, 1),
		"duplicate bootstrap": good + good,
		"visible challenge":   strings.Replace(good, "Alex | TikTok", "Verify you are human", 1),
		"negative counts":     strings.Replace(good, `"followerCount":120`, `"followerCount":-1`, 1),
	}
	for name, html := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseProfile([]byte(html), "public.user")
			if !errors.Is(err, httputil.Unavailable) {
				t.Fatalf("failed-open: %v", err)
			}
		})
	}
	if _, _, err := parseProfile([]byte(good+`<script>var captcha = "ordinary bundled identifier";</script>`), "public.user"); err != nil {
		t.Fatal("normal bundle identifier rejected", err)
	}
}
func TestAvatarValidation(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "http://p16.tiktokcdn.com/a.jpg", "https://user:password@p16.tiktokcdn.com/a.jpg", "https://p16.tiktokcdn.com:443/a.jpg", "https://p16.tiktokcdn.com.evil.test/a.jpg", "https://localhost/a.jpg", "https://127.0.0.1/a.jpg", "https://169.254.169.254/a.jpg", "https://p16.tiktokcdn.com/a.jpg?access_token=secret"} {
		if validMediaURL(raw) {
			t.Errorf("unsafe avatar accepted %s", raw)
		}
	}
	for _, raw := range []string{"https://p16-sign-va.tiktokcdn.com/a.jpg", "https://p16.tiktokcdn-us.com/a.jpg", "https://p16.tiktokcdn-eu.com/a.jpg", "https://v16-webapp-prime.tiktok.com/video/tos/x.mp4?a=b", "https://www.tiktok.com/x"} {
		if !validMediaURL(raw) {
			t.Errorf("valid avatar rejected %s", raw)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func testProvider(fn roundTripFunc) *Provider                               { return &Provider{client: &http.Client{Transport: fn}} }
func htmlResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}
func TestPublicRequestContractAndSearch(t *testing.T) {
	calls := 0
	p := testProvider(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != "https://www.tiktok.com/@public.user" || req.Header.Get("User-Agent") != userAgent || req.Header.Get("Accept") != "text/html,application/json" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Fatalf("unsafe request %+v", req)
		}
		return htmlResponse(200, fixture(publicUserJSON, statsJSON)), nil
	})
	results, err := p.Search(context.Background(), "public.user")
	if err != nil || len(results) != 1 || results[0].Username != "public.user" {
		t.Fatalf("search %+v %v", results, err)
	}
	_, err = p.Search(context.Background(), "Public User")
	if !errors.Is(err, httputil.Unsupported) || calls != 1 {
		t.Fatalf("display search incorrectly supported %v calls%d", err, calls)
	}
	_, err = p.GetProfile(context.Background(), "https://localhost/")
	if !errors.Is(err, httputil.Invalid) || calls != 1 {
		t.Fatal("arbitrary profile request reached transport")
	}
}
func TestHTTPFailuresAndBounds(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   error
	}{"notfound": {404, "missing", httputil.NotFound}, "restricted": {403, "restricted", httputil.Unavailable}, "challenge": {200, "<html>captcha</html>", httputil.Unavailable}, "oversized": {200, strings.Repeat("x", maxHTMLBytes+1), httputil.Unavailable}} {
		t.Run(name, func(t *testing.T) {
			p := testProvider(func(*http.Request) (*http.Response, error) { return htmlResponse(tc.status, tc.body), nil })
			_, err := p.GetProfile(context.Background(), "public.user")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	p := testProvider(func(*http.Request) (*http.Response, error) {
		r := htmlResponse(200, strings.Repeat("x", maxHTMLBytes+1))
		r.ContentLength = -1
		return r, nil
	})
	if _, err := p.GetProfile(context.Background(), "public.user"); !errors.Is(err, httputil.Unavailable) {
		t.Fatal("unknown length HTML not bounded")
	}
	p = testProvider(func(*http.Request) (*http.Response, error) {
		r := htmlResponse(200, fixture(publicUserJSON, statsJSON))
		r.Header.Set("Content-Type", "application/octet-stream")
		return r, nil
	})
	if _, err := p.GetProfile(context.Background(), "public.user"); !errors.Is(err, httputil.Unavailable) {
		t.Fatal("unexpected content type accepted")
	}
	p = testProvider(func(*http.Request) (*http.Response, error) { return nil, errors.New("private infrastructure secret") })
	if _, err := p.GetProfile(context.Background(), "public.user"); !errors.Is(err, httputil.Unavailable) {
		t.Fatal("internal provider error leaked", err)
	}
}
func TestRateLimitHonorsCooldownWithoutRetry(t *testing.T) {
	calls := 0
	p := testProvider(func(*http.Request) (*http.Response, error) {
		calls++
		r := htmlResponse(429, "limited")
		r.Header.Set("Retry-After", "120")
		return r, nil
	})
	for i := 0; i < 2; i++ {
		if _, err := p.GetProfile(context.Background(), "public.user"); !errors.Is(err, httputil.RateLimited) {
			t.Fatalf("rate limit %v", err)
		}
	}
	if calls != 1 || time.Until(p.cooldownUntil) < 119*time.Second {
		t.Fatalf("429 retried or cooldown discarded: calls%d until%s", calls, time.Until(p.cooldownUntil))
	}
	now := time.Now()
	if httputil.RetryAfterTime("0", now).Sub(now) != time.Minute {
		t.Fatal("minimum cooldown missing")
	}
	date := now.Add(10 * time.Minute).UTC().Format(http.TimeFormat)
	if httputil.RetryAfterTime(date, now).Sub(now) < 9*time.Minute {
		t.Fatal("HTTP date Retry-After ignored")
	}
}
func TestContextCancellationAndTimeout(t *testing.T) {
	p := testProvider(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := p.GetProfile(ctx, "public.user"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := p.GetProfile(ctx, "public.user"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation %v", err)
	}
}
func TestRedirectRestrictions(t *testing.T) {
	for _, raw := range []string{"https://localhost/@public.user", "http://www.tiktok.com/@public.user", "https://www.tiktok.com/login", "https://www.tiktok.com/@public.user?msToken=secret", "https://user:pass@www.tiktok.com/@public.user", "https://www.tiktok.com:444/@public.user"} {
		req, _ := http.NewRequest("GET", raw, nil)
		if publicProfileRedirect(req, []*http.Request{{}}) == nil {
			t.Errorf("unsafe redirect accepted %s", raw)
		}
	}
	req, _ := http.NewRequest("GET", "https://www.tiktok.com/@public.user/", nil)
	if err := publicProfileRedirect(req, []*http.Request{{}}); err != nil {
		t.Fatal("canonical profile redirect rejected", err)
	}
	if publicProfileRedirect(req, []*http.Request{{}, {}, {}}) == nil {
		t.Fatal("redirect limit not enforced")
	}
}

func TestLivePublicTikTokProfile(t *testing.T) {
	if os.Getenv("GHOSTVIEW_LIVE_TEST") != "1" {
		t.Skip("live platform test is opt-in; skipped execution is not verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	profile, err := New().GetProfile(ctx, "marcmarquez93")
	if err != nil {
		t.Fatal("public anonymous profile lookup failed:", err)
	}
	if profile.Username != "marcmarquez93" || profile.DisplayName != "Marc Márquez" || profile.AccessStatus != model.AccessPublic || profile.FollowerCount == nil || *profile.FollowerCount <= 0 || profile.PostCount == nil || *profile.PostCount <= 0 || profile.FollowingCount == nil || *profile.FollowingCount < 0 || profile.AvatarURL == "" {
		t.Fatalf("public profile verification mismatch: username%q displayName%q status%s", profile.Username, profile.DisplayName, profile.AccessStatus)
	}
	avatar, _ := url.Parse(profile.AvatarURL)
	t.Logf("actual public profile: username=%s displayName=%q accessStatus=%s followers=%d following=%d posts=%d avatarHost=%s", profile.Username, profile.DisplayName, profile.AccessStatus, *profile.FollowerCount, *profile.FollowingCount, *profile.PostCount, avatar.Hostname())
}

func TestShortPublicProfileCacheAvoidsDuplicateLookup(t *testing.T) {
	calls := 0
	p := testProvider(func(*http.Request) (*http.Response, error) {
		calls++
		return htmlResponse(200, fixture(publicUserJSON, statsJSON)), nil
	})
	p.cache = cache.NewMemory(2)
	if _, err := p.Search(context.Background(), "public.user"); err != nil {
		t.Fatal(err)
	}
	profile, err := p.GetProfile(context.Background(), "public.user")
	if err != nil || profile.Username != "public.user" || calls != 1 {
		t.Fatalf("duplicate anonymous lookup: calls%d error%v", calls, err)
	}
}

func TestPrivateProfilesAreNotCached(t *testing.T) {
	calls := 0
	p := testProvider(func(*http.Request) (*http.Response, error) {
		calls++
		return htmlResponse(200, fixture(strings.Replace(publicUserJSON, `"privateAccount":false`, `"privateAccount":true`, 1), statsJSON)), nil
	})
	p.cache = cache.NewMemory(2)
	for i := 0; i < 2; i++ {
		profile, err := p.GetProfile(context.Background(), "public.user")
		if err != nil || profile.AccessStatus != model.AccessPrivate {
			t.Fatal("private fixture failed", err)
		}
	}
	if calls != 2 {
		t.Fatal("private platform data cached")
	}
}

const storyFixtureJSON = `{"statusCode":0,"TotalCount":"2","itemList":[` +
	`{"id":"7693420078263487765","createTime":1791263956,"desc":"a day out","privateItem":false,"isAd":false,` +
	`"author":{"uniqueId":"public.user"},` +
	`"video":{"playAddr":"https://v16-webapp-prime.tiktok.com/video/tos/story.mp4?sig=1","cover":"https://p19-common-sign.tiktokcdn.com/tos/story.jpg","duration":44,"width":576,"height":1024},"story":{"ExpiredAt":1791350356000}},` +
	`{"id":"7693420078263487766","createTime":1791263957,"desc":"","privateItem":true,` +
	`"author":{"uniqueId":"public.user"},"video":{"playAddr":"https://v16-webapp-prime.tiktok.com/video/tos/private.mp4"}},` +
	`{"id":"7693420078263487767","createTime":1791263958,"desc":"","privateItem":false,` +
	`"author":{"uniqueId":"other.user"},"video":{"playAddr":"https://v16-webapp-prime.tiktok.com/video/tos/other.mp4"}}]}`

func TestGetStoriesContract(t *testing.T) {
	calls := map[string]int{}
	p := testProvider(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatalf("request contract mismatch")
		}
		switch r.URL.Path {
		case "/@public.user":
			return htmlResponse(200, fixture(publicUserJSON, statsJSON)), nil
		case "/api/story/item_list/":
			_ = r.URL.Query().Get("authorId")
			if r.URL.Query().Get("authorId") != "6676363774038033409" || r.URL.Query().Get("aid") != "1988" {
				t.Fatalf("story query mismatch: %s", r.URL.RawQuery)
			}
			return jsonResponse(200, storyFixtureJSON), nil
		default:
			t.Fatalf("unexpected request %s", r.URL)
			return nil, nil
		}
	})
	p.cache = cache.NewMemory(4)
	ctx := context.Background()
	if _, err := p.GetProfile(ctx, "public.user"); err != nil {
		t.Fatal(err)
	}
	stories, err := p.GetStories(ctx, "public.user")
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 {
		t.Fatalf("stories %+v", stories)
	}
	item := stories[0]
	if item.ID != "7693420078263487765" || item.Type != model.Story || item.MediaURL == "" || item.ThumbnailURL == "" || !item.Downloadable || item.Duration != 44 || item.Width != 576 || item.Height != 1024 {
		t.Fatalf("story item mismatch: %+v", item)
	}
	if item.CreatedAt.IsZero() || item.Caption != "a day out" {
		t.Fatalf("story metadata mismatch: %+v", item)
	}
	// The profile fetch is cached, so only the story API is called again.
	if calls["/@public.user"] != 1 || calls["/api/story/item_list/"] != 1 {
		t.Fatalf("call counts %v", calls)
	}
}

func TestGetStoriesFailClosed(t *testing.T) {
	p := testProvider(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/@public.user":
			return htmlResponse(200, fixture(publicUserJSON, statsJSON)), nil
		case "/api/story/item_list/":
			return jsonResponse(200, `{"statusCode":10221,"status_msg":"stories not available"}`), nil
		default:
			t.Fatalf("unexpected request %s", r.URL)
			return nil, nil
		}
	})
	if _, err := p.GetStories(context.Background(), "public.user"); err != httputil.Unavailable {
		t.Fatalf("non-zero story statusCode must fail closed: %v", err)
	}
	blocked := testProvider(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/@public.user" {
			return htmlResponse(200, fixture(publicUserJSON, statsJSON)), nil
		}
		return jsonResponse(429, `{"statusCode":10204}`), nil
	})
	if _, err := blocked.GetStories(context.Background(), "public.user"); err != httputil.RateLimited {
		t.Fatalf("429 must map to rate limited: %v", err)
	}
}

func TestLivePublicTikTokStories(t *testing.T) {
	if os.Getenv("GHOSTVIEW_LIVE_TEST") != "1" {
		t.Skip("live platform test is opt-in; skipped execution is not verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	p := New()
	stories, err := p.GetStories(ctx, "thifchann14")
	if err != nil {
		t.Fatal("anonymous story retrieval failed:", err)
	}
	if len(stories) == 0 {
		t.Fatal("test account had an active story during research; empty result fails verification")
	}
	for _, item := range stories {
		if item.Type != model.Story || item.MediaURL == "" || !strings.HasPrefix(item.MediaURL, "https://") {
			t.Fatalf("story item mismatch: %+v", item)
		}
	}
	// Stream the first story's bytes anonymously from the CDN.
	req, _ := http.NewRequestWithContext(ctx, "GET", stories[0].MediaURL, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://www.tiktok.com/")
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 512<<10))
	if err != nil || res.StatusCode != 200 && res.StatusCode != 206 {
		t.Fatalf("story media fetch failed: status=%d err=%v", res.StatusCode, err)
	}
	kind := http.DetectContentType(body)
	if kind != "video/mp4" && kind != "image/jpeg" {
		t.Fatalf("unexpected story media type %s", kind)
	}
	t.Logf("REAL TikTok stories username=thifchann14 items=%d firstID=%s kind=%s bytes=%d", len(stories), stories[0].ID, kind, len(body))
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}

func TestLiveBrowserPostsAndReposts(t *testing.T) {
	if os.Getenv("GHOSTVIEW_LIVE_TEST") != "1" || os.Getenv("GHOSTVIEW_TT_BROWSER") != "1" {
		t.Skip("opt-in: set GHOSTVIEW_LIVE_TEST=1 and GHOSTVIEW_TT_BROWSER=1 to run the headless-browser feed harvest")
	}
	p := NewWithBrowser(true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	posts, err := p.GetPosts(ctx, "thifchann14", "")
	if err != nil {
		t.Fatal("browser posts retrieval failed:", err)
	}
	if len(posts.Items) < 10 {
		t.Fatalf("expected at least 10 real posts, got %d", len(posts.Items))
	}
	for _, item := range posts.Items {
		if item.Type != model.Video || item.ID == "" || item.ThumbnailURL == "" {
			t.Fatalf("post item mismatch: %+v", item)
		}
	}
	reposts, err := p.GetReposts(ctx, "thifchann14", "")
	if err != nil {
		t.Fatal("browser reposts retrieval failed:", err)
	}
	if len(reposts.Items) < 1 {
		t.Fatal("expected real repost items")
	}
	t.Logf("REAL TikTok posts=%d reposts=%d firstPostID=%s firstRepostID=%s", len(posts.Items), len(reposts.Items), posts.Items[0].ID, reposts.Items[0].ID)
}
