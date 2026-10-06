package downloader

import (
	"bytes"
	"context"
	"fmt"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type DNSResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}
type allowlistKey struct{}

type Downloader struct {
	once      sync.Once
	transport *http.Transport
	Timeout   time.Duration
	MaxBytes  int64
	Resolver  DNSResolver
}
type Result struct {
	Bytes       []byte
	ContentType string
	Filename    string
}

func New(timeout time.Duration, maxBytes int64) *Downloader {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if maxBytes <= 0 {
		maxBytes = 25 << 20
	}
	return &Downloader{Timeout: timeout, MaxBytes: maxBytes, Resolver: net.DefaultResolver}
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/3"), netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func IsPublicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	if a.Is6() && !netip.MustParsePrefix("2000::/3").Contains(a) {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
func allowed(host string, hosts []string) bool {
	for _, h := range hosts {
		h = strings.ToLower(h)
		if h == host {
			return true
		}
		if strings.HasPrefix(h, "*.") && strings.HasSuffix(host, h[1:]) && host != h[2:] {
			return true
		}
	}
	return false
}
func parse(raw string, hosts []string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Fragment != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, httputil.DownloadUnavailable
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasSuffix(host, ".") || !allowed(host, hosts) {
		return nil, httputil.DownloadUnavailable
	}
	if ip := net.ParseIP(host); ip != nil && !IsPublicIP(ip) {
		return nil, httputil.DownloadUnavailable
	}
	return u, nil
}
func (d *Downloader) addresses(ctx context.Context, host string) ([]net.IPAddr, error) {
	resolver := d.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, e := resolver.LookupIPAddr(ctx, host)
	if e != nil || len(ips) == 0 {
		return nil, httputil.DownloadUnavailable
	}
	for _, ip := range ips {
		if !IsPublicIP(ip.IP) {
			return nil, httputil.DownloadUnavailable
		}
	}
	return ips, nil
}
func (d *Downloader) ValidateURL(ctx context.Context, raw string, hosts []string) error {
	u, e := parse(raw, hosts)
	if e != nil {
		return e
	}
	_, e = d.addresses(ctx, u.Hostname())
	return e
}
func ValidateURL(ctx context.Context, raw string, hosts []string) error {
	return New(20*time.Second, 25<<20).ValidateURL(ctx, raw, hosts)
}

var safeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func SafeFilename(filename string) string {
	filename = safeName.ReplaceAllString(filepath.Base(filename), "-")
	filename = strings.Trim(filename, ".-")
	if len(filename) > 100 {
		filename = filename[:100]
	}
	if filename == "" {
		return "ghostview-media"
	}
	return filename
}
func (d *Downloader) Fetch(ctx context.Context, r *model.DownloadResource) (*Result, error) {
	if r == nil {
		return nil, httputil.DownloadUnavailable
	}
	if len(r.Fixture) > 0 {
		if int64(len(r.Fixture)) > d.MaxBytes || r.ContentType != "image/svg+xml" {
			return nil, httputil.DownloadUnavailable
		}
		return &Result{Bytes: bytes.Clone(r.Fixture), ContentType: r.ContentType, Filename: filenameForType(r.Filename, r.ContentType)}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	if e := d.ValidateURL(ctx, r.URL, r.AllowedHosts); e != nil {
		return nil, e
	}
	d.once.Do(func() {
		d.transport = &http.Transport{Proxy: nil, MaxIdleConns: 16, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: d.Timeout, DisableCompression: true}
		d.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, e := net.SplitHostPort(address)
			hosts, _ := ctx.Value(allowlistKey{}).([]string)
			if e != nil || port != "443" || !allowed(strings.ToLower(host), hosts) {
				return nil, httputil.DownloadUnavailable
			}
			ips, e := d.addresses(ctx, host)
			if e != nil {
				return nil, e
			}
			dialer := net.Dialer{Timeout: 5 * time.Second}
			var conn net.Conn
			for _, ip := range ips {
				conn, e = dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if e == nil {
					return conn, nil
				}
			}
			return nil, e
		}
	})
	ctx = context.WithValue(ctx, allowlistKey{}, append([]string(nil), r.AllowedHosts...))
	transport := d.transport
	client := &http.Client{Timeout: d.Timeout, Transport: transport, CheckRedirect: d.checkRedirect(r.AllowedHosts)}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if e != nil {
		return nil, httputil.DownloadUnavailable
	}
	req.Header.Set("Accept", "image/jpeg, image/png, image/webp, video/mp4")
	response, e := client.Do(req)
	if e != nil {
		return nil, httputil.DownloadUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > d.MaxBytes {
		return nil, httputil.DownloadUnavailable
	}
	ct, _, e := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if e != nil {
		return nil, httputil.DownloadUnavailable
	}
	switch ct {
	case "image/jpeg", "image/png", "image/webp", "video/mp4":
	default:
		return nil, httputil.DownloadUnavailable
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, d.MaxBytes+1))
	if e != nil || len(data) == 0 || int64(len(data)) > d.MaxBytes {
		return nil, httputil.DownloadUnavailable
	}
	actual := http.DetectContentType(data)
	if actual != ct {
		return nil, httputil.DownloadUnavailable
	}
	return &Result{Bytes: data, ContentType: ct, Filename: filenameForType(r.Filename, ct)}, nil
}
func (r *Result) Disposition() string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": r.Filename})
}
func (r *Result) ContentLength() string { return fmt.Sprint(len(r.Bytes)) }

func (d *Downloader) checkRedirect(hosts []string) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return httputil.DownloadUnavailable
		}
		return d.ValidateURL(req.Context(), req.URL.String(), hosts)
	}
}

func (d *Downloader) Close() {
	if d.transport != nil {
		d.transport.CloseIdleConnections()
	}
}

func filenameForType(name, ct string) string {
	name = SafeFilename(name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "video/mp4": ".mp4", "image/svg+xml": ".svg"}[ct]
	return name + ext
}
