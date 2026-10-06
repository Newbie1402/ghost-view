package service

import (
	"ghostview/internal/model"
	"testing"
)

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		input          string
		selected, want model.Platform
		name           string
	}{
		{"alex.morgan", model.TikTok, model.TikTok, "alex.morgan"},
		{" @alex.morgan ", model.Instagram, model.Instagram, "alex.morgan"},
		{"https://www.tiktok.com/@alex.morgan", model.Facebook, model.TikTok, "alex.morgan"},
		{"https://www.tiktok.com/@marcmarquez93?_r=1&_t=ZS-9AKf9BKqZhS", model.Instagram, model.TikTok, "marcmarquez93"},
		{"https://www.instagram.com/alex.morgan/", model.TikTok, model.Instagram, "alex.morgan"},
		{"https://www.facebook.com/alex.morgan", model.Instagram, model.Facebook, "alex.morgan"},
		{"https://m.facebook.com/profile.php?id=123456789", model.TikTok, model.Facebook, "123456789"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			p, name, err := Normalize(tc.selected, tc.input)
			if err != nil || p != tc.want || name != tc.name {
				t.Fatalf("got %q %q %v", p, name, err)
			}
		})
	}
}

func TestNormalizeRejectsUnsafeInput(t *testing.T) {
	for _, input := range []string{"", "@", "https://", "http://www.tiktok.com/@alex", "ftp://instagram.com/alex", "javascript:alert(1)", "https://localhost/alex", "https://127.0.0.1/alex", "https://[::1]/alex", "https://10.1.2.3/alex", "https://172.16.0.1/alex", "https://192.168.1.1/alex", "https://169.254.169.254/latest/meta-data", "https://evil.example/alex", "https://instagram.com.evil.example/alex", "https://user:password@instagram.com/alex", "https://instagram.com:443/alex", "https://instagram.com/alex/extra", "https://instagram.com/%2e%2e/", "https://instagram.com/alex#fragment", "<script>alert(1)</script>"} {
		t.Run(input, func(t *testing.T) {
			if _, _, err := Normalize(model.TikTok, input); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
	if _, _, err := Normalize(model.Platform("unknown"), "alex"); err == nil {
		t.Fatal("invalid platform accepted")
	}
	for _, input := range []string{"https://facebook.com/profile.php", "https://facebook.com/profile.php?id=123&id=456", "https://facebook.com/profile.php?id=abc", "https://facebook.com/profile.php?id=123&extra=1", "https://instagram.com/reels", "https://facebook.com/groups", "https://tiktok.com/alex"} {
		if _, _, err := Normalize(model.Facebook, input); err == nil {
			t.Errorf("invalid profile URL accepted: %s", input)
		}
	}
	for _, query := range []string{"_r=1&_r=2", "_t=share&_t=other", "_r=%ZZ", "url=https%3A%2F%2F127.0.0.1", "token=secret", "access_token=secret", "query=alex", "_r=1;_t=share", "_r=1&extra=value"} {
		t.Run(query, func(t *testing.T) {
			if _, _, err := Normalize(model.TikTok, "https://www.tiktok.com/@marcmarquez93?"+query); err == nil {
				t.Fatal("unsafe share query accepted")
			}
		})
	}
}
