// Package facebook reads only the public Open Graph identity metadata that the
// anonymous profile document serves. It never logs in, sends cookies, follows
// redirects, or resolves posts, stories, or media, which Facebook serves only
// to authenticated sessions.
package facebook

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"ghostview/internal/cache"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"ghostview/internal/provider"
)

const maxHTMLBytes = 2 << 20

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,49}$`)
var ogPattern = regexp.MustCompile(`(?is)<meta[^>]*property=["']og:([a-z:_]+)["'][^>]*content=["']([^"']*)["']`)
var vanityPattern = regexp.MustCompile(`\s*\(@[^)]*\)\s*$`)
var bulletPattern = regexp.MustCompile(`\s*[•|]\s*`)

// Provider has no cookie jar and follows no redirects, including redirects to
// the login wall.
type Provider struct {
	provider.Unavailable
	client   *http.Client
	identity cache.Cache
	mu       sync.Mutex
	cooldown time.Time
}

func New() provider.SocialProvider {
	return &Provider{Unavailable: provider.Unavailable{Name: model.Facebook}, identity: cache.NewMemory(200), client: &http.Client{
		Timeout:       12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func (*Provider) Status() string { return "EXPERIMENTAL" }
func (*Provider) Capabilities() model.ProviderCapabilities {
	return model.ProviderCapabilities{Search: true, Profile: true}
}
func (p *Provider) Search(ctx context.Context, query string) ([]model.ProfileSummary, error) {
	if !usernamePattern.MatchString(query) {
		return nil, httputil.Unsupported
	}
	profile, err := p.GetProfile(ctx, query)
	if errors.Is(err, httputil.NotFound) {
		return []model.ProfileSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []model.ProfileSummary{{Platform: model.Facebook, Username: profile.Username, DisplayName: profile.DisplayName, AvatarURL: profile.AvatarURL, Verified: profile.Verified}}, nil
}

// GetProfile returns the identity (display name, avatar, canonical URL) that
// the public profile document exposes to anonymous visitors. No follower or
// media counts are invented: Facebook supplies them only inside login-walled
// surfaces, so they stay nil.
func (p *Provider) GetProfile(ctx context.Context, username string) (*model.Profile, error) {
	if !usernamePattern.MatchString(username) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := "facebook:profile:" + strings.ToLower(username)
	if p.identity != nil {
		if b, ok := p.identity.Get(key); ok {
			var profile model.Profile
			if json.Unmarshal(b, &profile) == nil && profile.AccessStatus == model.AccessPublic {
				return &profile, nil
			}
		}
	}
	p.mu.Lock()
	limited := time.Now().Before(p.cooldown)
	p.mu.Unlock()
	if limited {
		return nil, httputil.RateLimited
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.facebook.com/"+url.PathEscape(username), nil)
	if err != nil {
		return nil, httputil.Invalid
	}
	req.Header.Set("User-Agent", "GhostView/1.0 (+public-content verification)")
	req.Header.Set("Accept", "text/html")
	res, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, httputil.Unavailable
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusNotFound:
		return nil, httputil.NotFound
	case http.StatusTooManyRequests:
		until := httputil.RetryAfterTime(res.Header.Get("Retry-After"), time.Now())
		p.mu.Lock()
		if until.After(p.cooldown) {
			p.cooldown = until
		}
		p.mu.Unlock()
		return nil, httputil.RateLimited
	case http.StatusOK:
	default:
		// A redirect to /login/ cannot distinguish a private profile from a
		// login wall, so it fails closed instead of guessing PRIVATE.
		return nil, httputil.Unavailable
	}
	if !strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "text/html") {
		return nil, httputil.Unavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxHTMLBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, httputil.Unavailable
	}
	if len(body) > maxHTMLBytes {
		return nil, httputil.Unavailable
	}
	if _, params, err := mime.ParseMediaType(res.Header.Get("Content-Type")); err == nil && params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		return nil, httputil.Unavailable
	}
	profile, err := parseIdentity(body, username)
	if err == nil && p.identity != nil {
		if b, e := json.Marshal(profile); e == nil {
			p.identity.Set(key, b, time.Minute)
		}
	}
	return profile, err
}

// parseIdentity trusts only Open Graph metadata whose canonical URL matches the
// requested username. "Content not found" shells and pages for other accounts
// fail closed.
func parseIdentity(body []byte, username string) (*model.Profile, error) {
	meta := map[string]string{}
	for _, match := range ogPattern.FindAllSubmatch(body, -1) {
		key := strings.ToLower(string(match[1]))
		if _, exists := meta[key]; !exists {
			meta[key] = html.UnescapeString(string(match[2]))
		}
	}
	title := strings.TrimSpace(meta["title"])
	if title == "" {
		return nil, httputil.Unavailable
	}
	canonical := meta["url"]
	if canonical != "" {
		parsed, err := url.Parse(canonical)
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
			return nil, httputil.Unavailable
		}
		if !strings.EqualFold(strings.Trim(parsed.Path, "/"), username) {
			return nil, httputil.Unavailable
		}
	} else if !strings.Contains(strings.ToLower(title), "(@"+strings.ToLower(username)+")") {
		return nil, httputil.Unavailable
	}
	displayName := title
	if index := bulletPattern.FindStringIndex(displayName); index != nil {
		displayName = displayName[:index[0]]
	}
	displayName = strings.TrimSpace(vanityPattern.ReplaceAllString(displayName, ""))
	if displayName == "" {
		if alt := meta["image:alt"]; alt != "" {
			displayName = alt
		}
	}
	if displayName == "" {
		return nil, httputil.Unavailable
	}
	profile := &model.Profile{
		Platform: model.Facebook, Username: username, DisplayName: displayName,
		ProfileURL: "https://www.facebook.com/" + url.PathEscape(username) + "/",
		AccessStatus: model.AccessPublic,
		Message:      "Public identity only. Facebook serves posts and stories only after login, so they are never retrieved.",
	}
	profile.AvatarURL = safeImageURL(meta["image"])
	return profile, nil
}

func safeImageURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".fbcdn.net") && !strings.HasSuffix(host, ".cdninstagram.com") {
		return ""
	}
	for key := range u.Query() {
		switch strings.ToLower(key) {
		case "access_token", "token", "sessionid", "cookie", "password":
			return ""
		}
	}
	return u.String()
}

func (*Provider) GetPosts(context.Context, string, string) (*model.MediaPage, error) {
	return nil, httputil.Unsupported
}
func (*Provider) GetStories(context.Context, string) ([]model.MediaItem, error) {
	return nil, httputil.Unsupported
}
func (*Provider) GetHighlights(context.Context, string) ([]model.Highlight, error) {
	return nil, httputil.Unsupported
}
func (*Provider) ResolveDownload(context.Context, string) (*model.DownloadResource, error) {
	return nil, httputil.Unsupported
}
