package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"ghostview/internal/httputil"
	"ghostview/internal/model"
)

func bootstrap(t *testing.T, private any, timelineID string) string {
	t.Helper()
	data := map[string]any{"require": []any{
		map[string]any{"xig_user_by_username": map[string]any{"id": "user-1", "username": "public.example", "full_name": "Actual public person", "is_private": private, "is_verified": true, "follower_count": 42, "following_count": 3, "all_media_count": nil, "profile_pic_url": "https://scontent.cdninstagram.com/avatar.jpg"}},
		map[string]any{"xig_user_by_username": map[string]any{"id": timelineID, "polaris_ordered_timeline_connection": map[string]any{"edges": []any{
			map[string]any{"node": map[string]any{"code": "Real_Code", "id": "123", "media_type": 8, "display_uri": "https://scontent.cdninstagram.com/public.jpg", "caption": map[string]any{"text": "Public carousel caption"}}},
			map[string]any{"node": map[string]any{"code": "Video_Code", "media_type": 2, "display_uri": "https://scontent.cdninstagram.com/thumbnail.jpg"}},
			map[string]any{"node": map[string]any{"code": "Bad_Code", "media_type": 1, "display_uri": "https://127.0.0.1/private.jpg"}},
		}}}},
	}}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return `<script type="application/json" data-sjs>` + string(b) + `</script>`
}
func TestPublicBootstrap(t *testing.T) {
	page, err := parsePublicPage([]byte(bootstrap(t, false, "user-1")), "public.example")
	if err != nil {
		t.Fatal(err)
	}
	if page.Profile.AccessStatus != model.AccessPublic || page.Profile.Username != "public.example" || page.Profile.FollowerCount == nil || *page.Profile.FollowerCount != 42 || page.Profile.PostCount != nil {
		t.Fatalf("profile mismatch: %+v", page.Profile)
	}
	if !page.TimelinePresent || len(page.Posts) != 1 {
		t.Fatalf("posts %+v", page.Posts)
	}
	post := page.Posts[0]
	if post.ID != "Real_Code" || post.Type != model.Image || !post.PreviewOnly || post.Downloadable || post.Caption != "Public carousel caption" {
		t.Fatalf("post mismatch: %+v", post)
	}
}
func TestPrivateBootstrap(t *testing.T) {
	page, err := parsePublicPage([]byte(bootstrap(t, true, "user-1")), "public.example")
	if err != nil || page.Profile.AccessStatus != model.AccessPrivate || len(page.Posts) != 0 || page.Profile.Bio != "" || page.Profile.FollowerCount != nil {
		t.Fatalf("private data exposed: %+v %v", page, err)
	}
}
func TestFailClosed(t *testing.T) {
	cases := []string{`<meta property="og:title" content="Public Person">`, bootstrap(t, nil, "user-1"), bootstrap(t, false, "different-user")}
	for i, body := range cases {
		page, err := parsePublicPage([]byte(body), "public.example")
		if i < 2 {
			if !errors.Is(err, httputil.Unavailable) {
				t.Fatalf("case %d: %v", i, err)
			}
		} else if err != nil || page.TimelinePresent || len(page.Posts) > 0 {
			t.Fatalf("unmatched account timeline exposed")
		}
	}
	if _, err := parsePublicPage([]byte(bootstrap(t, false, "user-1")), "another.person"); err != httputil.Unavailable {
		t.Fatal(err)
	}
}
func TestImageURLValidation(t *testing.T) {
	for _, raw := range []string{"http://scontent.cdninstagram.com/a.jpg", "https://127.0.0.1/a", "https://cdninstagram.com.evil.test/a", "https://user:password@scontent.cdninstagram.com/a", "https://scontent.cdninstagram.com:8443/a", "https://scontent.cdninstagram.com/a?access_token=secret", "https://scontent.cdninstagram.com/a#x"} {
		if safeImageURL(raw) != "" {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"https://scontent.cdninstagram.com/a.jpg?expires=100", "https://scontent-fra.xx.fbcdn.net/a.jpg"} {
		if safeImageURL(raw) == "" {
			t.Fatalf("rejected %q", raw)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHTTPContractAndBundleCache(t *testing.T) {
	p := New().(*Provider)
	calls := 0
	p.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != "https://www.instagram.com/public.example/" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "GhostView/") {
			t.Fatalf("request contract mismatch")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(bootstrap(t, false, "user-1")))}, nil
	})
	ctx := context.Background()
	results, err := p.Search(ctx, "public.example")
	if err != nil || len(results) != 1 {
		t.Fatal(err)
	}
	if _, err = p.GetProfile(ctx, "public.example"); err != nil {
		t.Fatal(err)
	}
	if posts, err := p.GetPosts(ctx, "public.example", ""); err != nil || len(posts.Items) != 1 {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("fetches=%d want1", calls)
	}
	if _, err = p.GetPosts(ctx, "public.example", "next"); err != httputil.Unsupported {
		t.Fatal(err)
	}
	if _, err = p.Search(ctx, "display name"); err != httputil.Unsupported {
		t.Fatal(err)
	}
	if _, err = p.GetProfile(ctx, "../login"); err != httputil.Invalid {
		t.Fatal(err)
	}
	if _, err = p.GetStories(ctx, "public.example"); err != httputil.Unsupported {
		t.Fatal(err)
	}
	if _, err = p.GetHighlights(ctx, "public.example"); err != httputil.Unsupported {
		t.Fatal(err)
	}
	if _, err = p.ResolveDownload(ctx, "Real_Code"); err != httputil.Unsupported {
		t.Fatal(err)
	}
}
func TestHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{{404, httputil.NotFound}, {429, httputil.RateLimited}, {302, httputil.Unavailable}, {403, httputil.Unavailable}} {
		p := New().(*Provider)
		p.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})
		if _, err := p.GetProfile(context.Background(), "example"); err != tc.want {
			t.Fatalf("status%d=%v", tc.status, err)
		}
	}
	p := New().(*Provider)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.GetProfile(ctx, "example"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	p.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxHTMLBytes+1)))}, nil
	})
	if _, err := p.GetProfile(context.Background(), "example"); err != httputil.Unavailable {
		t.Fatal(err)
	}
}
func TestLivePublicProfileAndImage(t *testing.T) {
	if os.Getenv("GHOSTVIEW_LIVE_TEST") != "1" {
		t.Skip("set GHOSTVIEW_LIVE_TEST=1 to execute real anonymous network assertions")
	}
	p := New().(*Provider)
	ctx := context.Background()
	profile, err := p.GetProfile(ctx, "marcmarquez93")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Username != "marcmarquez93" || profile.DisplayName == "" || profile.AccessStatus != model.AccessPublic || profile.FollowerCount == nil || *profile.FollowerCount < 1 {
		t.Fatal("actual public profile assertions failed")
	}
	posts, err := p.GetPosts(ctx, "marcmarquez93", "")
	if err != nil || len(posts.Items) == 0 {
		t.Fatalf("actual public image posts unavailable: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, posts.Items[0].MediaURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "GhostView/1.0 (+public-content verification)")
	res, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
		t.Fatalf("actual CDN image status=%d type=%s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
	if err != nil || len(b) < 100 || http.DetectContentType(b) != "image/jpeg" {
		t.Fatal("real CDN JPEG assertion failed")
	}
	t.Logf("REAL Instagram username=%s access=%s photoPreviews=%d CDNStatus=%d imageBytes=%d", profile.Username, profile.AccessStatus, len(posts.Items), res.StatusCode, len(b))
}

func TestConflictingPrivacyFragmentsFailClosed(t *testing.T) {
	public := bootstrap(t, false, "user-1")
	for _, tc := range []struct {
		name, extra string
		want        error
		private     bool
	}{
		{"private same username", `{"id":"user-1","username":"public.example","is_private":true}`, nil, true},
		{"private same id", `{"id":"user-1","is_private":true}`, nil, true},
		{"unpublished same id", `{"id":"user-1","is_unpublished":true}`, httputil.Unavailable, false},
		{"conflicting identity", `{"id":"other-user","username":"public.example","is_private":false}`, httputil.Unavailable, false},
		{"negative followers", `{"id":"user-1","username":"public.example","follower_count":-1}`, httputil.Unavailable, false},
		{"malformed private fragment", `{"id":"user-1","username":"public.example","is_private":true,"follower_count":"invalid"}`, httputil.Unavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := `<script type="application/json">{"xig_user_by_username":` + tc.extra + `}</script>`
			for _, body := range []string{public + extra, extra + public} {
				page, err := parsePublicPage([]byte(body), "public.example")
				if tc.private {
					if err != nil || page.Profile.AccessStatus != model.AccessPrivate || len(page.Posts) > 0 || page.Profile.FollowerCount != nil || page.Profile.AvatarURL != "" || page.Profile.Bio != "" {
						t.Fatalf("private account leaked: %+v %v", page, err)
					}
				} else if err != tc.want {
					t.Fatalf("got %+v %v want %v", page, err, tc.want)
				}
			}
		})
	}
}
func TestVisibleAccessWallOverridesBootstrap(t *testing.T) {
	public := bootstrap(t, false, "user-1")
	for _, wall := range []string{
		`<title>Log in • Instagram</title>`,
		`<title>Security challenge • Instagram</title>`,
		`<h1>Verify you are human</h1>`,
		`<h2>CAPTCHA required</h2>`,
		`<form action="/challenge/"></form>`,
	} {
		if page, err := parsePublicPage([]byte(wall+public), "public.example"); err != httputil.Unavailable {
			t.Fatalf("wall %q accepted %+v %v", wall, page, err)
		}
	}
	harmless := `<script>const modules=["checkpoint","captcha","accounts/login"];</script><title>Actual public person • Instagram</title>` + public
	if _, err := parsePublicPage([]byte(harmless), "public.example"); err != nil {
		t.Fatal("bundled challenge words must not block actual profile", err)
	}
}
func TestRateLimitCooldown(t *testing.T) {
	p := New().(*Provider)
	calls := 0
	p.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	for _, username := range []string{"first", "second", "third"} {
		if _, err := p.GetProfile(context.Background(), username); err != httputil.RateLimited {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("upstream calls during cooldown=%d want1", calls)
	}
}
