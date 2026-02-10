package tlsclient

import (
	"crypto/tls"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"k8s.io/klog/v2"
)

// ProfileRotationMode determines how browser profiles are selected
type ProfileRotationMode int

const (
	// ProfileRotationOff uses a single profile (Chrome_144)
	ProfileRotationOff ProfileRotationMode = iota
	// ProfileRotationRandom selects a random profile for each client
	ProfileRotationRandom
	// ProfileRotationSequential rotates through profiles in order
	ProfileRotationSequential
)

var (
	// DefaultProfiles contains the browser profiles to rotate between
	// Focused on modern Chrome and Firefox versions
	DefaultProfiles = []profiles.ClientProfile{
		profiles.Chrome_144,
		profiles.Chrome_146,
		profiles.Chrome_133,
		profiles.Chrome_131,
		profiles.Firefox_147,
		profiles.Firefox_135,
		profiles.Firefox_133,
	}

	// currentProfileIndex tracks the current profile for sequential rotation
	currentProfileIndex int
	profileMutex        sync.Mutex
)

// ClientOptions configures the TLS client behavior
type ClientOptions struct {
	// ProfileRotationMode determines how profiles are selected
	ProfileRotationMode ProfileRotationMode
	// CustomProfiles allows overriding the default profile list
	CustomProfiles []profiles.ClientProfile
	// Timeout sets the HTTP client timeout
	Timeout time.Duration
	// FollowRedirects controls redirect behavior
	FollowRedirects bool
	// CookieJar allows setting a custom cookie jar
	CookieJar http.CookieJar
}

// DefaultClientOptions returns sensible defaults for the TLS client
func DefaultClientOptions() *ClientOptions {
	return &ClientOptions{
		ProfileRotationMode: ProfileRotationRandom,
		CustomProfiles:      DefaultProfiles,
		Timeout:             90 * time.Second, // longer timeout for thermoptic CDP workflow
		FollowRedirects:     true,
		CookieJar:           nil,
	}
}

// selectProfile chooses a browser profile based on the rotation mode
func selectProfile(opts *ClientOptions) profiles.ClientProfile {
	profileList := opts.CustomProfiles
	if len(profileList) == 0 {
		profileList = DefaultProfiles
	}

	switch opts.ProfileRotationMode {
	case ProfileRotationOff:
		// Always use the first profile
		return profileList[0]
	case ProfileRotationRandom:
		// Random selection
		return profileList[rand.Intn(len(profileList))]
	case ProfileRotationSequential:
		// Sequential rotation with mutex protection
		profileMutex.Lock()
		defer profileMutex.Unlock()
		profile := profileList[currentProfileIndex%len(profileList)]
		currentProfileIndex++
		return profile
	default:
		return profileList[0]
	}
}

// NewHTTPClient creates a new HTTP client with TLS fingerprinting support
// It returns an *http.Client that can be used as a drop-in replacement for standard net/http clients
// If HTTP_PROXY or HTTPS_PROXY environment variables are set, the client will use a simple proxy
// client instead of TLS fingerprinting (useful for thermoptic integration)
func NewHTTPClient(opts *ClientOptions) (*http.Client, error) {
	if opts == nil {
		opts = DefaultClientOptions()
	}

	// Check if we should use a proxy (e.g., thermoptic)
	httpProxy := os.Getenv("HTTP_PROXY")
	httpsProxy := os.Getenv("HTTPS_PROXY")

	if httpProxy != "" || httpsProxy != "" {
		klog.V(2).Infof("HTTP_PROXY or HTTPS_PROXY detected, using proxy transport instead of TLS fingerprinting")
		return newProxyClient(opts, httpProxy, httpsProxy)
	}

	// Select browser profile
	profile := selectProfile(opts)
	klog.V(2).Infof("Creating TLS client with profile: %s", profileName(profile))

	// Create TLS client's own cookie jar (fhttp.CookieJar)
	tlsJar := tls_client.NewCookieJar()

	// Build TLS client options
	clientOptions := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(int(opts.Timeout.Seconds())),
		tls_client.WithClientProfile(profile),
		tls_client.WithCookieJar(tlsJar),
		tls_client.WithRandomTLSExtensionOrder(),
	}

	if !opts.FollowRedirects {
		clientOptions = append(clientOptions, tls_client.WithNotFollowRedirects())
	}

	// Create the TLS client
	tlsClient, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), clientOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS client: %w", err)
	}

	// Create transport wrapper
	transport := &tlsClientTransport{
		client:  tlsClient,
		jar:     tlsJar,
		profile: profile,
	}

	// Create standard http.Client with the wrapper
	// Note: We provide the TLS client's jar wrapped in a compatibility layer
	return &http.Client{
		Transport:     transport,
		CheckRedirect: nil,
		Jar:           &cookieJarWrapper{jar: tlsJar},
		Timeout:       opts.Timeout,
	}, nil
}

// newProxyClient creates a simple HTTP client that uses the specified proxy
// This is used when HTTP_PROXY/HTTPS_PROXY environment variables are set
func newProxyClient(opts *ClientOptions, httpProxy, httpsProxy string) (*http.Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // thermoptic uses self-signed certs
		},
	}

	// Set proxy function
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		proxyURL := httpProxy
		if req.URL.Scheme == "https" && httpsProxy != "" {
			proxyURL = httpsProxy
		}
		if proxyURL == "" {
			return nil, nil
		}
		return url.Parse(proxyURL)
	}

	// Create cookie jar - use provided one or create a default
	jar := opts.CookieJar
	if jar == nil {
		var err error
		jar, err = cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create cookie jar: %w", err)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		Jar:       jar,
	}

	if !opts.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	return client, nil
}

// tlsClientTransport wraps the TLS client to implement http.RoundTripper
type tlsClientTransport struct {
	client  tls_client.HttpClient
	jar     fhttp.CookieJar
	profile profiles.ClientProfile
}

var profileUserAgents = map[string]string{
	"Chrome_144":  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
	"Chrome_146":  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Chrome_133":  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
	"Chrome_131":  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Firefox_147": "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:147.0) Gecko/20100101 Firefox/147.0",
	"Firefox_135": "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:135.0) Gecko/20100101 Firefox/135.0",
	"Firefox_133": "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
}

// RoundTrip implements http.RoundTripper
func (t *tlsClientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Convert net/http.Request to fhttp.Request
	fReq, err := t.ConvertToFHTTPRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert request: %w", err)
	}

	// Execute request with TLS client
	fResp, err := t.client.Do(fReq)
	if err != nil {
		return nil, err
	}

	// Convert fhttp.Response back to net/http.Response
	return convertToNetHTTPResponse(fResp)
}

// ConvertToFHTTPRequest converts a net/http.Request to fhttp.Request
func (t *tlsClientTransport) ConvertToFHTTPRequest(req *http.Request) (*fhttp.Request, error) {
	fReq, err := fhttp.NewRequest(req.Method, req.URL.String(), req.Body)
	if err != nil {
		return nil, err
	}

	// Copy headers
	fReq.Header = make(fhttp.Header)
	for k, v := range req.Header {
		fReq.Header[k] = v
	}

	// Set User-Agent based on profile if not already set or if it's the default Go UA
	ua := fReq.Header.Get("User-Agent")
	if ua == "" || ua == "Go-http-client/1.1" {
		pName := profileName(t.profile)
		for key, mappedUA := range profileUserAgents {
			if strings.Contains(pName, key) {
				fReq.Header.Set("User-Agent", mappedUA)
				break
			}
		}
	}

	// Copy other important fields
	fReq.Host = req.Host
	fReq.ContentLength = req.ContentLength
	fReq.TransferEncoding = req.TransferEncoding
	fReq.Close = req.Close

	return fReq, nil
}

// convertToNetHTTPResponse converts an fhttp.Response to net/http.Response
func convertToNetHTTPResponse(fResp *fhttp.Response) (*http.Response, error) {
	resp := &http.Response{
		Status:        fResp.Status,
		StatusCode:    fResp.StatusCode,
		Proto:         fResp.Proto,
		ProtoMajor:    fResp.ProtoMajor,
		ProtoMinor:    fResp.ProtoMinor,
		Header:        make(http.Header),
		Body:          fResp.Body,
		ContentLength: fResp.ContentLength,
		Close:         fResp.Close,
		Uncompressed:  fResp.Uncompressed,
	}

	// Copy headers
	for k, v := range fResp.Header {
		resp.Header[k] = v
	}

	// Copy request if available
	if fResp.Request != nil {
		resp.Request = &http.Request{
			Method: fResp.Request.Method,
			URL:    fResp.Request.URL,
			Host:   fResp.Request.Host,
			Header: make(http.Header),
		}
		for k, v := range fResp.Request.Header {
			resp.Request.Header[k] = v
		}
	}

	return resp, nil
}

// cookieJarWrapper wraps fhttp.CookieJar to implement http.CookieJar
type cookieJarWrapper struct {
	jar fhttp.CookieJar
}

// SetCookies implements http.CookieJar.SetCookies
func (w *cookieJarWrapper) SetCookies(u *neturl.URL, cookies []*http.Cookie) {
	// Convert net/http cookies to fhttp cookies
	fCookies := make([]*fhttp.Cookie, len(cookies))
	for i, c := range cookies {
		fCookies[i] = &fhttp.Cookie{
			Name:       c.Name,
			Value:      c.Value,
			Path:       c.Path,
			Domain:     c.Domain,
			Expires:    c.Expires,
			RawExpires: c.RawExpires,
			MaxAge:     c.MaxAge,
			Secure:     c.Secure,
			HttpOnly:   c.HttpOnly,
			SameSite:   fhttp.SameSite(c.SameSite),
			Raw:        c.Raw,
			Unparsed:   c.Unparsed,
		}
	}
	w.jar.SetCookies(u, fCookies)
}

// Cookies implements http.CookieJar.Cookies
func (w *cookieJarWrapper) Cookies(u *neturl.URL) []*http.Cookie {
	fCookies := w.jar.Cookies(u)
	cookies := make([]*http.Cookie, len(fCookies))
	for i, fc := range fCookies {
		cookies[i] = &http.Cookie{
			Name:       fc.Name,
			Value:      fc.Value,
			Path:       fc.Path,
			Domain:     fc.Domain,
			Expires:    fc.Expires,
			RawExpires: fc.RawExpires,
			MaxAge:     fc.MaxAge,
			Secure:     fc.Secure,
			HttpOnly:   fc.HttpOnly,
			SameSite:   http.SameSite(fc.SameSite),
			Raw:        fc.Raw,
			Unparsed:   fc.Unparsed,
		}
	}
	return cookies
}

// profileName returns a human-readable name for a profile
func profileName(p profiles.ClientProfile) string {
	switch fmt.Sprintf("%p", &p) { // This won't work reliably, let's use a different approach
	}

	// Profiles in bogdanfinn/tls-client/profiles are structs.
	// We can try to match them by certain fields if needed, but for now
	// let's just use the ones we have in our DefaultProfiles.
	if fmt.Sprintf("%v", p) == fmt.Sprintf("%v", profiles.Chrome_144) {
		return "Chrome_144"
	}
	if fmt.Sprintf("%v", p) == fmt.Sprintf("%v", profiles.Chrome_146) {
		return "Chrome_146"
	}
	if fmt.Sprintf("%v", p) == fmt.Sprintf("%v", profiles.Chrome_133) {
		return "Chrome_133"
	}
	if fmt.Sprintf("%v", p) == fmt.Sprintf("%v", profiles.Chrome_131) {
		return "Chrome_131"
	}
	if fmt.Sprintf("%v", p) == fmt.Sprintf("%v", profiles.Firefox_147) {
		return "Firefox_147"
	}
	if fmt.Sprintf("%v", p) == fmt.Sprintf("%v", profiles.Firefox_135) {
		return "Firefox_135"
	}
	if fmt.Sprintf("%v", p) == fmt.Sprintf("%v", profiles.Firefox_133) {
		return "Firefox_133"
	}

	return fmt.Sprintf("%v", p)
}
