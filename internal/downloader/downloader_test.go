package downloader

import (
	"context"
	"errors"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeResolver struct {
	hosts   map[string][]net.IPAddr
	failure error
}

func (r fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.hosts[host], r.failure
}
func addresses(ips ...string) []net.IPAddr {
	result := []net.IPAddr{}
	for _, ip := range ips {
		result = append(result, net.IPAddr{IP: net.ParseIP(ip)})
	}
	return result
}

func TestPublicIPPolicy(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "224.0.0.1", "::", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "2001:db8::1", "2002:7f00:1::", "2001::1"} {
		t.Run(ip, func(t *testing.T) {
			if IsPublicIP(net.ParseIP(ip)) {
				t.Fatal("non-public address allowed")
			}
		})
	}
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !IsPublicIP(net.ParseIP(ip)) {
			t.Fatalf("public address blocked: %s", ip)
		}
	}
	if IsPublicIP(nil) {
		t.Fatal("nil IP allowed")
	}
}

func TestDownloadURLPolicy(t *testing.T) {
	d := New(time.Second, 1024)
	d.Resolver = fakeResolver{hosts: map[string][]net.IPAddr{"cdn.example": addresses("8.8.8.8"), "asset.cdn.example": addresses("1.1.1.1"), "private.example": addresses("10.0.0.1"), "mixed.example": addresses("8.8.8.8", "127.0.0.1"), "empty.example": nil}}
	ctx := context.Background()
	for _, raw := range []string{"https://cdn.example/media.jpg", "https://cdn.example:443/media.jpg", "https://asset.cdn.example/media.jpg"} {
		if err := d.ValidateURL(ctx, raw, []string{"cdn.example", "*.cdn.example"}); err != nil {
			t.Fatalf("allowed URL %s: %v", raw, err)
		}
	}
	for _, tc := range []struct{ url, host string }{
		{"http://cdn.example/a", "cdn.example"}, {"file:///etc/passwd", "cdn.example"}, {"https://cdn.example:8443/a", "cdn.example"}, {"https://user:secret@cdn.example/a", "cdn.example"}, {"https://cdn.example/a#fragment", "cdn.example"}, {"https://cdn.example./a", "cdn.example"}, {"https://evil.example/a", "cdn.example"}, {"https://cdn.example.evil.example/a", "*.cdn.example"}, {"https://localhost/a", "localhost"}, {"https://127.0.0.1/a", "127.0.0.1"}, {"https://[::1]/a", "::1"}, {"https://10.0.0.1/a", "10.0.0.1"}, {"https://169.254.169.254/latest/meta-data", "169.254.169.254"}, {"https://private.example/a", "private.example"}, {"https://mixed.example/a", "mixed.example"}, {"https://empty.example/a", "empty.example"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			if err := d.ValidateURL(ctx, tc.url, []string{tc.host}); err == nil {
				t.Fatal("unsafe URL accepted")
			}
		})
	}
	d.Resolver = fakeResolver{failure: errors.New("DNS failure")}
	if err := d.ValidateURL(ctx, "https://cdn.example/a", []string{"cdn.example"}); err == nil {
		t.Fatal("DNS failure ignored")
	}
}

func TestRedirectPolicyBlocksPrivateIP(t *testing.T) {
	d := New(time.Second, 1024)
	d.Resolver = fakeResolver{hosts: map[string][]net.IPAddr{"cdn.example": addresses("8.8.8.8"), "private.cdn.example": addresses("192.168.1.1")}}
	check := d.checkRedirect([]string{"cdn.example", "*.cdn.example", "127.0.0.1", "169.254.169.254"})
	for _, raw := range []string{"https://127.0.0.1/media", "https://169.254.169.254/latest/meta-data", "https://private.cdn.example/media", "http://cdn.example/media", "https://evil.example/media"} {
		u, _ := url.Parse(raw)
		req := &http.Request{URL: u}
		if err := check(req, nil); err == nil {
			t.Fatalf("unsafe redirect allowed: %s", raw)
		}
	}
	u, _ := url.Parse("https://cdn.example/media")
	req := &http.Request{URL: u}
	if err := check(req, nil); err != nil {
		t.Fatalf("allowed redirect: %v", err)
	}
	if err := check(req, make([]*http.Request, 3)); err == nil {
		t.Fatal("redirect loop allowed")
	}
}

func TestFixtureDownloadBoundsAndSafeFilename(t *testing.T) {
	d := New(time.Second, 5)
	resource := &model.DownloadResource{Fixture: []byte("<svg>"), ContentType: "image/svg+xml", Filename: "../../unsafe\r\nname.svg"}
	r, err := d.Fetch(context.Background(), resource)
	if err != nil || string(r.Bytes) != "<svg>" || strings.ContainsAny(r.Filename, "\r\n/") || !strings.HasPrefix(r.Disposition(), "attachment;") {
		t.Fatalf("fixture download: %v %v", r, err)
	}
	resource.Fixture[0] = 'X'
	if string(r.Bytes) != "<svg>" {
		t.Fatal("fixture return aliases source")
	}
	resource.Fixture = []byte("too long")
	if _, err := d.Fetch(context.Background(), resource); !errors.Is(err, httputil.DownloadUnavailable) {
		t.Fatal("oversize accepted")
	}
	resource.Fixture = []byte("small")
	resource.ContentType = "text/html"
	if _, err := d.Fetch(context.Background(), resource); !errors.Is(err, httputil.DownloadUnavailable) {
		t.Fatal("HTML accepted")
	}
	if _, err := d.Fetch(context.Background(), nil); err == nil {
		t.Fatal("nil resource accepted")
	}
	if SafeFilename("../../") != "ghostview-media" || len(SafeFilename(strings.Repeat("a", 200))) > 100 {
		t.Fatal("filename bounds failed")
	}
}
