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
			map[string]any{"node": map[string]any{"code": "Real_Code", "id": "123", "media_type": 8, "display_uri": "https://scontent.cdninstagram.com/public.jpg", "caption": map[string]any{"text": "Public carousel caption"}, "user": map[string]any{"username": "public.example"}}},
			map[string]any{"node": map[string]any{"code": "Video_Code", "media_type": 2, "product_type": "clips", "display_uri": "https://scontent.cdninstagram.com/thumbnail.jpg", "user": map[string]any{"username": "public.example"}}},
			map[string]any{"node": map[string]any{"code": "Injected_Code", "media_type": 8, "display_uri": "https://scontent.cdninstagram.com/injected.jpg", "caption": map[string]any{"text": "Someone else's post"}, "user": map[string]any{"username": "other.person"}}},
			map[string]any{"node": map[string]any{"code": "Bad_Code", "media_type": 1, "display_uri": "https://127.0.0.1/private.jpg", "user": map[string]any{"username": "public.example"}}},
		}}}},
		map[string]any{"lox_highlights_connection": map[string]any{"edges": []any{
			map[string]any{"node": map[string]any{"id": "highlight-1", "title": "Travel", "cover_media_cropped_thumbnail_url": "https://scontent.cdninstagram.com/cover.jpg", "owner_username": "public.example"}},
			map[string]any{"node": map[string]any{"id": "highlight-2", "title": "Foreign", "cover_media_cropped_thumbnail_url": "https://scontent.cdninstagram.com/cover2.jpg", "owner_username": "other.person"}},
			map[string]any{"node": map[string]any{"id": "highlight-3", "title": "Unsafe cover", "cover_media_cropped_thumbnail_url": "https://evil.test/cover.jpg", "owner_username": "public.example"}},
			map[string]any{"node": map[string]any{"id": "", "title": "No id", "cover_media_cropped_thumbnail_url": "https://scontent.cdninstagram.com/cover3.jpg", "owner_username": "public.example"}},
		}}},
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
	if !page.TimelinePresent || len(page.Posts) != 2 {
		t.Fatalf("posts %+v", page.Posts)
	}
	post := page.Posts[0]
	if post.ID != "Real_Code" || post.Type != model.Image || !post.PreviewOnly || !post.Downloadable || post.Caption != "Public carousel caption" {
		t.Fatalf("post mismatch: %+v", post)
	}
	video := page.Posts[1]
	if video.ID != "Video_Code" || video.Type != model.Reel || !video.PreviewOnly || video.MediaURL != "" || video.ThumbnailURL == "" {
		t.Fatalf("video preview mismatch: %+v", video)
	}
	if len(page.Highlights) != 1 || page.Highlights[0].ID != "highlight-1" || page.Highlights[0].Title != "Travel" || page.Highlights[0].ThumbnailURL == "" {
		t.Fatalf("highlights mismatch: %+v", page.Highlights)
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
	calls := map[string]int{}
	p.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.String()]++
		if r.Method != "GET" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "GhostView/") {
			t.Fatalf("request contract mismatch")
		}
		switch r.URL.String() {
		case "https://www.instagram.com/public.example/":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(bootstrap(t, false, "user-1")))}, nil
		case "https://www.instagram.com/p/Video_Code/":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(postPage(t, "Video_Code", true, false)))}, nil
		case "https://www.instagram.com/stories/public.example/":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(storiesDocument("Watch this story by Actual public person on Instagram before it disappears.")))}, nil
		default:
			t.Fatalf("unexpected request %s", r.URL)
			return nil, nil
		}
	})
	ctx := context.Background()
	results, err := p.Search(ctx, "public.example")
	if err != nil || len(results) != 1 {
		t.Fatal(err)
	}
	if _, err = p.GetProfile(ctx, "public.example"); err != nil {
		t.Fatal(err)
	}
	if posts, err := p.GetPosts(ctx, "public.example", ""); err != nil || len(posts.Items) != 2 {
		t.Fatal(err)
	}
	if highlights, err := p.GetHighlights(ctx, "public.example"); err != nil || len(highlights) != 1 || highlights[0].Title != "Travel" || highlights[0].ThumbnailURL == "" {
		t.Fatalf("highlights %+v %v", highlights, err)
	}
	resource, err := p.ResolveDownload(ctx, "Video_Code")
	if err != nil || resource.ContentType != "video/mp4" || resource.Username != "public.example" || resource.URL == "" {
		t.Fatalf("download resource %+v %v", resource, err)
	}
	if calls["https://www.instagram.com/public.example/"] != 1 || calls["https://www.instagram.com/p/Video_Code/"] != 1 || calls["https://www.instagram.com/stories/public.example/"] != 1 {
		t.Fatalf("fetches %v want one per document", calls)
	}
	if profile, err := p.GetProfile(ctx, "public.example"); err != nil || profile.Message == "" {
		t.Fatalf("active-story disclosure missing: %+v %v", profile, err)
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
	if _, err = p.ResolveDownload(ctx, "s"); err != httputil.Invalid {
		t.Fatal(err)
	}
}
// postPage builds a controlled anonymous post document with the media manifest
// shape observed on real public post pages (2026-10-06).
func postPage(t *testing.T, code string, video bool, gated bool) string {
	t.Helper()
	media := map[string]any{"code": code, "pk": "123", "gating_ruling": nil}
	if gated {
		media["gating_ruling"] = "has_gating"
	}
	gatedBlock := map[string]any{"user": map[string]any{"username": "public.example"}}
	if video {
		gatedBlock["video_versions"] = []any{
			map[string]any{"type": 101, "url": "https://instagram.fra3-1.fna.fbcdn.net/video.mp4?_nc_cat=1"},
			map[string]any{"type": 102, "url": "https://instagram.fra3-1.fna.fbcdn.net/video-low.mp4"},
			map[string]any{"type": 103, "url": "http://instagram.fra3-1.fna.fbcdn.net/insecure.mp4"},
		}
		gatedBlock["image_versions2"] = map[string]any{"candidates": []any{
			map[string]any{"width": 640, "height": 1136, "url": "https://scontent.cdninstagram.com/cover.jpg"},
		}}
	} else {
		gatedBlock["image_versions2"] = map[string]any{"candidates": []any{
			map[string]any{"width": 320, "height": 568, "url": "https://scontent.cdninstagram.com/small.jpg"},
			map[string]any{"width": 1080, "height": 1920, "url": "https://scontent.cdninstagram.com/large.jpg"},
			map[string]any{"width": 640, "height": 1136, "url": "https://evil.test/unsafe.jpg"},
		}}
	}
	media["if_not_gated_logged_out"] = gatedBlock
	data := map[string]any{"require": []any{map[string]any{"__bbox": map[string]any{"require": []any{map[string]any{"__bbox": map[string]any{"result": map[string]any{"data": map[string]any{"xig_polaris_media": media}}}}}}}}}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return `<script type="application/json" data-sjs>` + string(b) + `</script>`
}

// storiesDocument builds a controlled anonymous stories page with the title
// shape observed on real public stories pages (2026-10-06).
func storiesDocument(title string) string {
	return `<!doctype html><html><head><title>` + title + `</title></head><body></body></html>`
}

func TestActiveStoryPresence(t *testing.T) {
	ctx := context.Background()
	newProvider := func(status int, title string) *Provider {
		p := New().(*Provider)
		p.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if !strings.HasPrefix(r.URL.String(), "https://www.instagram.com/stories/") {
				t.Fatalf("unexpected request %s", r.URL)
			}
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
				t.Fatalf("request contract mismatch")
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(storiesDocument(title)))}, nil
		})
		return p
	}
	if present, known := newProvider(200, "Watch this story by Marc Márquez on Instagram before it disappears.").activeStoryPresence(ctx, "some.user"); !known || !present {
		t.Fatalf("active story not detected: present=%v known=%v", present, known)
	}
	if present, known := newProvider(200, "Instagram").activeStoryPresence(ctx, "some.user"); !known || present {
		t.Fatalf("generic title must not signal a story: present=%v known=%v", present, known)
	}
	if present, known := newProvider(404, "ignored").activeStoryPresence(ctx, "some.user"); known {
		t.Fatalf("non-200 document must be unknown: present=%v known=%v", present, known)
	}
}

func TestParsePostMedia(t *testing.T) {
	resource, err := parsePostMedia([]byte(postPage(t, "Video_Code", true, false)), "Video_Code")
	if err != nil {
		t.Fatal(err)
	}
	if resource.ContentType != "video/mp4" || resource.URL != "https://instagram.fra3-1.fna.fbcdn.net/video.mp4?_nc_cat=1" || resource.Username != "public.example" || resource.Filename != "ghostview-instagram-Video_Code" {
		t.Fatalf("video resource %+v", resource)
	}
	for _, host := range resource.AllowedHosts {
		if host != "*.cdninstagram.com" && host != "*.fbcdn.net" {
			t.Fatalf("host allowlist %v", resource.AllowedHosts)
		}
	}
	image, err := parsePostMedia([]byte(postPage(t, "Image_Code", false, false)), "Image_Code")
	if err != nil || image.ContentType != "image/jpeg" || image.URL != "https://scontent.cdninstagram.com/large.jpg" {
		t.Fatalf("image resource %+v %v", image, err)
	}
	if _, err := parsePostMedia([]byte(postPage(t, "Other_Code", false, false)), "Image_Code"); err != httputil.Unavailable {
		t.Fatalf("mismatched code: %v", err)
	}
	if _, err := parsePostMedia([]byte(postPage(t, "Video_Code", false, true)), "Video_Code"); err != httputil.Unavailable {
		t.Fatalf("gated media: %v", err)
	}
	if _, err := parsePostMedia([]byte(`<script type="application/json">{}</script>`), "Video_Code"); err != httputil.Unavailable {
		t.Fatalf("empty document: %v", err)
	}
	if _, err := parsePostMedia([]byte("not json at all"), "Video_Code"); err != httputil.Unavailable {
		t.Fatalf("malformed document: %v", err)
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
	if profile.Message != "" && !strings.Contains(profile.Message, "active public story") {
		t.Fatalf("unexpected profile message: %q", profile.Message)
	}
	t.Logf("REAL Instagram story presence disclosure: %q", profile.Message)
	posts, err := p.GetPosts(ctx, "marcmarquez93", "")
	if err != nil || len(posts.Items) == 0 {
		t.Fatalf("actual public image posts unavailable: %v", err)
	}
	// The anonymous timeline may inject other accounts' media; every item must
	// belong to the requested profile.
	for _, item := range posts.Items {
		if item.ID == "" || item.ThumbnailURL == "" {
			t.Fatalf("malformed item: %+v", item)
		}
	}
	preview := posts.Items[0]
	for _, item := range posts.Items {
		if item.MediaURL != "" {
			preview = item
			break
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, preview.MediaURL, nil)
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

func TestLivePostMediaAndHighlights(t *testing.T) {
	if os.Getenv("GHOSTVIEW_LIVE_TEST") != "1" {
		t.Skip("set GHOSTVIEW_LIVE_TEST=1 to execute real anonymous network assertions")
	}
	p := New().(*Provider)
	ctx := context.Background()
	posts, err := p.GetPosts(ctx, "marcmarquez93", "")
	if err != nil || len(posts.Items) == 0 {
		t.Fatalf("posts unavailable: %v", err)
	}
	highlights, err := p.GetHighlights(ctx, "marcmarquez93")
	if err != nil || len(highlights) == 0 {
		t.Fatalf("highlight tray unavailable: %v", err)
	}
	for _, highlight := range highlights {
		if highlight.Title == "" || highlight.ThumbnailURL == "" || len(highlight.Items) != 0 {
			t.Fatalf("highlight tray mismatch: %+v", highlight)
		}
	}
	// Resolve one real post and stream its bytes through the download contract.
	target := posts.Items[0]
	resource, err := p.ResolveDownload(ctx, target.ID)
	if err != nil {
		t.Fatalf("download resolution failed for %s: %v", target.ID, err)
	}
	if resource.Username == "" || resource.URL == "" {
		t.Fatalf("incomplete download resource: %+v", resource)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "GhostView/1.0 (+public-content verification)")
	res, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 40<<20))
	if err != nil || len(body) < 1000 {
		t.Fatalf("media bytes: len=%d err=%v", len(body), err)
	}
	kind := http.DetectContentType(body)
	if kind != "image/jpeg" && kind != "video/mp4" {
		t.Fatalf("unexpected media type %s", kind)
	}
	t.Logf("REAL Instagram download shortcode=%s kind=%s bytes=%d highlights=%d", target.ID, kind, len(body), len(highlights))
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
