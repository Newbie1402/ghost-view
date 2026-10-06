package service

import (
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"net/url"
	"regexp"
	"strings"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)
var queryPattern = regexp.MustCompile(`^[\p{L}\p{N}_. @-]{1,100}$`)

func Normalize(platform model.Platform, input string) (model.Platform, string, error) {
	input = strings.TrimSpace(input)
	if input == "" || len(input) > 512 {
		return "", "", httputil.Invalid
	}
	if strings.Contains(input, "://") || strings.ContainsAny(input, "/\\:") {
		u, err := url.Parse(input)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
			return "", "", httputil.Invalid
		}
		host := strings.ToLower(u.Hostname())
		var inferred model.Platform
		switch host {
		case "tiktok.com", "www.tiktok.com":
			inferred = model.TikTok
		case "instagram.com", "www.instagram.com":
			inferred = model.Instagram
		case "facebook.com", "www.facebook.com", "m.facebook.com":
			inferred = model.Facebook
		default:
			return "", "", httputil.Invalid
		}
		path := strings.Trim(u.Path, "/")
		if strings.Contains(path, "/") {
			return "", "", httputil.Invalid
		}
		if inferred == model.Facebook && path == "profile.php" {
			if len(u.Query()) != 1 || len(u.Query()["id"]) != 1 {
				return "", "", httputil.Invalid
			}
			path = u.Query().Get("id")
			if !regexp.MustCompile(`^[0-9]{1,30}$`).MatchString(path) {
				return "", "", httputil.Invalid
			}
		} else {
			if u.RawQuery != "" {
				query, err := url.ParseQuery(u.RawQuery)
				if err != nil || inferred != model.TikTok {
					return "", "", httputil.Invalid
				}
				for key, values := range query {
					if (key != "_r" && key != "_t") || len(values) != 1 || len(values[0]) > 200 {
						return "", "", httputil.Invalid
					}
				}
			}
			if inferred == model.TikTok {
				if !strings.HasPrefix(path, "@") {
					return "", "", httputil.Invalid
				}
				path = strings.TrimPrefix(path, "@")
			}
		}
		if !usernamePattern.MatchString(path) {
			return "", "", httputil.Invalid
		}
		if inferred == model.Instagram {
			switch strings.ToLower(path) {
			case "p", "reel", "reels", "stories", "explore", "accounts":
				return "", "", httputil.Invalid
			}
		}
		if inferred == model.Facebook {
			switch strings.ToLower(path) {
			case "watch", "reel", "reels", "stories", "login", "groups", "pages":
				return "", "", httputil.Invalid
			}
		}
		return inferred, strings.ToLower(path), nil
	}
	if !platform.Valid() {
		return "", "", httputil.Invalid
	}
	input = strings.TrimPrefix(input, "@")
	if !queryPattern.MatchString(input) || strings.Contains(input, "@") {
		return "", "", httputil.Invalid
	}
	return platform, strings.ToLower(input), nil
}
func ValidUsername(s string) bool { return usernamePattern.MatchString(s) }
