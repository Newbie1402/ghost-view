package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"ghostview/internal/cache"
	"ghostview/internal/downloader"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"ghostview/internal/provider"
)

const maxHTMLBytes = 8 << 20
const userAgent = "GhostView/1.0 (+public-content verification)"

// Provider reads only the anonymous public profile HTML. It never signs private
// API requests, logs in, retains cookies, or retries a platform restriction.
type Provider struct {
	client        *http.Client
	cache         cache.Cache
	mu            sync.Mutex
	cooldownUntil time.Time
}

func New() provider.SocialProvider {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 8 * time.Second
	transport.MaxIdleConnsPerHost = 4
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != "www.tiktok.com" || port != "443" {
			return nil, httputil.Unavailable
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, httputil.Unavailable
		}
		for _, address := range addresses {
			if !downloader.IsPublicIP(address.IP) {
				return nil, httputil.Unavailable
			}
		}
		dialer := net.Dialer{Timeout: 5 * time.Second}
		for _, address := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
			if err == nil {
				return connection, nil
			}
		}
		return nil, httputil.Unavailable
	}
	return &Provider{client: &http.Client{Timeout: 8 * time.Second, Transport: transport, CheckRedirect: publicProfileRedirect}, cache: cache.NewMemory(200)}
}

func (*Provider) Platform() model.Platform { return model.TikTok }
func (*Provider) Capabilities() model.ProviderCapabilities {
	return model.ProviderCapabilities{Search: true, Profile: true}
}
func (*Provider) Status() string { return "EXPERIMENTAL" }

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.]{0,63}$`)
var bootstrapPattern = regexp.MustCompile(`(?is)<script\b[^>]*\bid\s*=\s*["']__UNIVERSAL_DATA_FOR_REHYDRATION__["'][^>]*>(.*?)</script\s*>`)
var titlePattern = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)

func publicProfileRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.Host != "www.tiktok.com" || req.URL.User != nil || req.URL.RawQuery != "" || req.URL.Fragment != "" || !strings.HasPrefix(req.URL.Path, "/@") || !usernamePattern.MatchString(strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/@"), "/")) {
		return httputil.Unavailable
	}
	return nil
}

func (p *Provider) Search(ctx context.Context, query string) ([]model.ProfileSummary, error) {
	if strings.ContainsAny(query, " \t\r\n") {
		return nil, httputil.Unsupported
	}
	profile, err := p.GetProfile(ctx, strings.TrimPrefix(query, "@"))
	if errors.Is(err, httputil.NotFound) {
		return []model.ProfileSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []model.ProfileSummary{{Platform: model.TikTok, Username: profile.Username, DisplayName: profile.DisplayName, AvatarURL: profile.AvatarURL, Verified: profile.Verified}}, nil
}

func (p *Provider) GetProfile(ctx context.Context, username string) (*model.Profile, error) {
	if !usernamePattern.MatchString(username) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := "tiktok:profile:" + strings.ToLower(username)
	if p.cache != nil {
		if data, ok := p.cache.Get(key); ok {
			var profile model.Profile
			if json.Unmarshal(data, &profile) == nil && profile.AccessStatus == model.AccessPublic {
				return &profile, nil
			}
		}
	}
	p.mu.Lock()
	limited := time.Now().Before(p.cooldownUntil)
	p.mu.Unlock()
	if limited {
		return nil, httputil.RateLimited
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.tiktok.com/@"+username, nil)
	if err != nil {
		return nil, httputil.Invalid
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/json")
	response, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, httputil.Unavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusTooManyRequests:
		p.mu.Lock()
		until := httputil.RetryAfterTime(response.Header.Get("Retry-After"), time.Now())
		if until.After(p.cooldownUntil) {
			p.cooldownUntil = until
		}
		p.mu.Unlock()
		return nil, httputil.RateLimited
	case http.StatusNotFound:
		return nil, httputil.NotFound
	case http.StatusOK:
	default:
		return nil, httputil.Unavailable
	}
	if response.ContentLength > maxHTMLBytes {
		return nil, httputil.Unavailable
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != "text/html" {
		return nil, httputil.Unavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHTMLBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, httputil.Unavailable
	}
	if len(body) > maxHTMLBytes {
		return nil, httputil.Unavailable
	}
	profile, err := parseProfile(body, username)
	if err == nil && profile.AccessStatus == model.AccessPublic && p.cache != nil {
		if data, encodeErr := json.Marshal(profile); encodeErr == nil {
			p.cache.Set(key, data, 30*time.Second)
		}
	}
	return profile, err
}

type publicUser struct {
	UniqueID       string `json:"uniqueId"`
	Nickname       string `json:"nickname"`
	Signature      string `json:"signature"`
	AvatarThumb    string `json:"avatarThumb"`
	AvatarMedium   string `json:"avatarMedium"`
	AvatarLarger   string `json:"avatarLarger"`
	Verified       bool   `json:"verified"`
	PrivateAccount *bool  `json:"privateAccount"`
}
type publicStats struct {
	FollowerCount  *int64 `json:"followerCount"`
	FollowingCount *int64 `json:"followingCount"`
	VideoCount     *int64 `json:"videoCount"`
}
type publicDetail struct {
	StatusCode *int `json:"statusCode"`
	UserInfo   struct {
		User  publicUser  `json:"user"`
		Stats publicStats `json:"stats"`
	} `json:"userInfo"`
}

func parseProfile(html []byte, username string) (*model.Profile, error) {
	// A visible challenge page fails closed; a bundled JavaScript identifier named
	// "captcha" is not evidence that the public document is restricted.
	if title := titlePattern.FindSubmatch(html); len(title) > 1 {
		text := strings.ToLower(string(title[1]))
		for _, marker := range []string{"captcha", "verify you", "security check", "access denied", "log in", "login"} {
			if strings.Contains(text, marker) {
				return nil, httputil.Unavailable
			}
		}
	}
	matches := bootstrapPattern.FindAllSubmatch(html, 2)
	if len(matches) != 1 {
		return nil, httputil.Unavailable
	}
	var bootstrap struct {
		Scope map[string]json.RawMessage `json:"__DEFAULT_SCOPE__"`
	}
	if json.Unmarshal(matches[0][1], &bootstrap) != nil {
		return nil, httputil.Unavailable
	}
	var detail publicDetail
	if json.Unmarshal(bootstrap.Scope["webapp.user-detail"], &detail) != nil || detail.StatusCode == nil || *detail.StatusCode != 0 || detail.UserInfo.User.PrivateAccount == nil {
		return nil, httputil.Unavailable
	}
	user := detail.UserInfo.User
	if !strings.EqualFold(user.UniqueID, username) || !usernamePattern.MatchString(user.UniqueID) {
		return nil, httputil.Unavailable
	}
	profile := &model.Profile{Platform: model.TikTok, Username: user.UniqueID, DisplayName: user.Nickname, ProfileURL: "https://www.tiktok.com/@" + user.UniqueID, AccessStatus: model.AccessPublic}
	if *user.PrivateAccount {
		profile.AccessStatus = model.AccessPrivate
		profile.Message = httputil.Private.Message
		return profile, nil
	}
	stats := detail.UserInfo.Stats
	for _, count := range []*int64{stats.FollowerCount, stats.FollowingCount, stats.VideoCount} {
		if count != nil && *count < 0 {
			return nil, httputil.Unavailable
		}
	}
	profile.Bio = user.Signature
	profile.Verified = user.Verified
	profile.FollowerCount = stats.FollowerCount
	profile.FollowingCount = stats.FollowingCount
	profile.PostCount = stats.VideoCount
	for _, candidate := range []string{user.AvatarLarger, user.AvatarMedium, user.AvatarThumb} {
		if validAvatar(candidate) {
			profile.AvatarURL = candidate
			break
		}
	}
	return profile, nil
}

func validAvatar(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	permitted := false
	for _, domain := range []string{"tiktokcdn.com", "tiktokcdn-us.com", "tiktokcdn-eu.com"} {
		if strings.HasSuffix(host, "."+domain) {
			permitted = true
		}
	}
	if !permitted {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		switch strings.ToLower(key) {
		case "access_token", "token", "session", "sessionid", "cookie", "authorization", "auth":
			return false
		}
	}
	return true
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
