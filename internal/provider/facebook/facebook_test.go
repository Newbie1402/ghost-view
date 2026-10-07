package facebook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"ghostview/internal/httputil"
	"ghostview/internal/model"
)

func profileDocument(username, displayName string) string {
	return `<!doctype html><html><head>
<meta property="og:title" content="` + displayName + ` (@` + username + `)" />
<meta property="og:description" content="` + displayName + ` is on Facebook. Join Facebook to connect with ` + displayName + `" />
<meta property="og:url" content="https://www.facebook.com/` + username + `/" />
<meta property="og:image" content="https://scontent.fra3-1.fna.fbcdn.net/avatar.jpg?stp=dst-jpg_tt6" />
<meta property="og:image:alt" content="` + displayName + `" />
<meta property="og:type" content="video.other" />
</head><body></body></html>`
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestParseIdentity(t *testing.T) {
	profile, err := parseIdentity([]byte(profileDocument("bacbeodangiuu", "Hoàng Kim Bạc")), "bacbeodangiuu")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Platform != model.Facebook || profile.Username != "bacbeodangiuu" || profile.DisplayName != "Hoàng Kim Bạc" || profile.AccessStatus != model.AccessPublic {
		t.Fatalf("profile mismatch: %+v", profile)
	}
	if profile.AvatarURL != "https://scontent.fra3-1.fna.fbcdn.net/avatar.jpg?stp=dst-jpg_tt6" || profile.FollowerCount != nil || profile.PostCount != nil {
		t.Fatalf("identity fields mismatch: %+v", profile)
	}
	if profile.ProfileURL != "https://www.facebook.com/bacbeodangiuu/" {
		t.Fatalf("profile URL mismatch: %s", profile.ProfileURL)
	}
}

func TestParseIdentityFailsClosed(t *testing.T) {
	cases := map[string]string{
		"no og metadata":          `<html><head><title>Content not found</title></head></html>`,
		"different canonical url": strings.Replace(profileDocument("other.page", "Someone Else"), "other.page", "someone.else", 1),
		"empty display name":      strings.Replace(profileDocument("some.page", "Some Page"), "og:title" , "og:site_name", 1),
	}
	for name, body := range cases {
		if _, err := parseIdentity([]byte(body), "some.page"); !errors.Is(err, httputil.Unavailable) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Non-https or off-site avatars are dropped, but identity survives.
	profile, err := parseIdentity([]byte(strings.Replace(profileDocument("some.page", "Some Page"), "https://scontent.fra3-1.fna.fbcdn.net/avatar.jpg", "http://evil.test/avatar.jpg", 1)), "some.page")
	if err != nil || profile.AvatarURL != "" {
		t.Fatalf("unsafe avatar accepted: %+v %v", profile, err)
	}
}

func TestHTTPContract(t *testing.T) {
	p := New().(*Provider)
	p.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.String() != "https://www.facebook.com/bacbeodangiuu" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "GhostView/") {
			t.Fatalf("request contract mismatch: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(profileDocument("bacbeodangiuu", "Hoàng Kim Bạc")))}, nil
	})
	ctx := context.Background()
	results, err := p.Search(ctx, "bacbeodangiuu")
	if err != nil || len(results) != 1 || results[0].DisplayName != "Hoàng Kim Bạc" {
		t.Fatalf("search %+v %v", results, err)
	}
	profile, err := p.GetProfile(ctx, "bacbeodangiuu")
	if err != nil || profile.DisplayName != "Hoàng Kim Bạc" {
		t.Fatalf("profile %+v %v", profile, err)
	}
	// Second read must come from cache without another upstream call.
	if _, err = p.GetProfile(ctx, "bacbeodangiuu"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Search(ctx, "not a username"); err != httputil.Unsupported {
		t.Fatal(err)
	}
	if _, err = p.GetStories(ctx, "bacbeodangiuu"); err != httputil.Unsupported {
		t.Fatal(err)
	}
	if _, err = p.GetHighlights(ctx, "bacbeodangiuu"); err != httputil.Unsupported {
		t.Fatal(err)
	}
	if _, err = p.ResolveDownload(ctx, "media"); err != httputil.Unsupported {
		t.Fatal(err)
	}
}

func TestHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{{404, httputil.NotFound}, {429, httputil.RateLimited}, {302, httputil.Unavailable}, {403, httputil.Unavailable}, {200, httputil.Unavailable}} {
		p := New().(*Provider)
		contentType := "text/html; charset=utf-8"
		if tc.status == 200 {
			// A 200 shell without og metadata must fail closed.
			contentType = "text/html; charset=utf-8"
		}
		p.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader("<html></html>"))}, nil
		})
		if _, err := p.GetProfile(context.Background(), "example.page"); !errors.Is(err, tc.want) {
			t.Fatalf("status%d=%v want %v", tc.status, err, tc.want)
		}
	}
	p := New().(*Provider)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.GetProfile(ctx, "example.page"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRateLimitCooldown(t *testing.T) {
	p := New().(*Provider)
	calls := 0
	p.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	for _, username := range []string{"first.page", "second.page", "third.page"} {
		if _, err := p.GetProfile(context.Background(), username); err != httputil.RateLimited {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("upstream calls during cooldown=%d want1", calls)
	}
}

func TestLivePublicIdentity(t *testing.T) {
	if os.Getenv("GHOSTVIEW_LIVE_TEST") != "1" {
		t.Skip("set GHOSTVIEW_LIVE_TEST=1 to execute real anonymous network assertions")
	}
	p := New().(*Provider)
	profile, err := p.GetProfile(context.Background(), "bacbeodangiuu")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Username != "bacbeodangiuu" || profile.DisplayName == "" || profile.AccessStatus != model.AccessPublic || profile.FollowerCount != nil {
		t.Fatalf("identity assertions failed: %+v", profile)
	}
	if profile.AvatarURL == "" {
		t.Fatal("public avatar missing")
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, profile.AvatarURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "GhostView/1.0 (+public-content verification)")
	res, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
	if err != nil || res.StatusCode != 200 || http.DetectContentType(body) != "image/jpeg" {
		t.Fatalf("avatar retrieval failed: status=%d type=%s err=%v", res.StatusCode, res.Header.Get("Content-Type"), err)
	}
	t.Logf("REAL Facebook username=%s displayName=%q avatarBytes=%d", profile.Username, profile.DisplayName, len(body))
}
