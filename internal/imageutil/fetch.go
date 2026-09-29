package imageutil

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"ai-server/internal/errs"
)

// Fetcher loads image bytes from https URLs and data URIs.
type Fetcher struct {
	lim    Limits
	client *http.Client
}

// NewFetcher creates a fetcher. Private-network destinations are blocked at
// dial time (after DNS resolution) unless lim.AllowPrivate is set, which also
// covers redirects and DNS rebinding.
func NewFetcher(lim Limits) *Fetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !lim.AllowPrivate {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if !IsPublicAddr(ip) {
				return fmt.Errorf("destination %s is not a public address", ip)
			}
			return nil
		}
	}
	transport := &http.Transport{
		Proxy:                 nil, // never route through env proxies
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: lim.FetchTimeout,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		DisableCompression:    true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   lim.FetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > lim.MaxRedirects {
				return fmt.Errorf("too many redirects")
			}
			return checkScheme(req.URL, lim)
		},
	}
	return &Fetcher{lim: lim, client: client}
}

// IsPublicAddr reports whether ip is a globally routable unicast address.
func IsPublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func checkScheme(u *url.URL, lim Limits) error {
	switch u.Scheme {
	case "https":
	case "http":
		if !lim.AllowHTTP {
			return fmt.Errorf("plain http URLs are disabled (use https)")
		}
	default:
		return fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("URLs with credentials are not allowed")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("URL has no host")
	}
	return nil
}

// Fetch returns the raw bytes and declared MIME type of src.
func (f *Fetcher) Fetch(ctx context.Context, src string) ([]byte, string, error) {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(strings.ToLower(src), "data:") {
		return f.decodeDataURI(src)
	}
	u, err := url.Parse(src)
	if err != nil {
		return nil, "", errs.New(errs.InvalidRequest, "invalid image URL")
	}
	if err := checkScheme(u, f.lim); err != nil {
		return nil, "", errs.New(errs.InvalidRequest, "%s", err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", errs.New(errs.InvalidRequest, "invalid image URL")
	}
	req.Header.Set("Accept", "image/jpeg, image/png, image/webp")
	req.Header.Set("User-Agent", "self-ai-server/1")
	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return nil, "", errs.New(errs.ImageFetchFailed, "timed out fetching image")
		}
		return nil, "", errs.Wrap(errs.ImageFetchFailed, err, "could not fetch image")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", errs.New(errs.ImageFetchFailed, "image server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > f.lim.MaxSourceBytes {
		return nil, "", errs.New(errs.ImageTooLarge, "image is %d bytes (limit %d)", resp.ContentLength, f.lim.MaxSourceBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.lim.MaxSourceBytes+1))
	if err != nil {
		return nil, "", errs.Wrap(errs.ImageFetchFailed, err, "error reading image")
	}
	if int64(len(data)) > f.lim.MaxSourceBytes {
		return nil, "", errs.New(errs.ImageTooLarge, "image exceeds %d bytes", f.lim.MaxSourceBytes)
	}
	return data, mediaType(resp.Header.Get("Content-Type")), nil
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func mediaType(ct string) string {
	mt, _, _ := strings.Cut(ct, ";")
	return strings.ToLower(strings.TrimSpace(mt))
}

func (f *Fetcher) decodeDataURI(src string) ([]byte, string, error) {
	meta, payload, ok := strings.Cut(src[len("data:"):], ",")
	if !ok {
		return nil, "", errs.New(errs.InvalidRequest, "malformed data URI")
	}
	parts := strings.Split(meta, ";")
	mt := strings.ToLower(strings.TrimSpace(parts[0]))
	isB64 := false
	for _, p := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(p), "base64") {
			isB64 = true
		}
	}
	if !isB64 {
		return nil, "", errs.New(errs.InvalidRequest, "data URIs must be base64 encoded")
	}
	if int64(base64.StdEncoding.DecodedLen(len(payload))) > f.lim.MaxSourceBytes+3 {
		return nil, "", errs.New(errs.ImageTooLarge, "image exceeds %d bytes", f.lim.MaxSourceBytes)
	}
	payload = strings.TrimRight(payload, "=")
	data, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil {
		// accept URL-safe alphabet as well
		data, err = base64.RawURLEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", errs.New(errs.InvalidRequest, "invalid base64 in data URI")
		}
	}
	if int64(len(data)) > f.lim.MaxSourceBytes {
		return nil, "", errs.New(errs.ImageTooLarge, "image exceeds %d bytes", f.lim.MaxSourceBytes)
	}
	return data, mt, nil
}

// sniff returns the detected format of data (jpeg, png, webp) or "".
func sniff(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}
