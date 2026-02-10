// Package tlsclient provides HTTP client creation with TLS fingerprinting and browser impersonation.
//
// This package wraps github.com/bogdanfinn/tls-client to create HTTP clients that can impersonate
// real browsers, helping avoid detection and blocking by websites that use TLS fingerprinting.
//
// Features:
//   - Browser Profile Rotation: Randomly or sequentially rotate between multiple browser profiles
//   - TLS Fingerprinting: Accurate TLS fingerprints matching Chrome, Firefox, Safari
//   - HTTP/2 and HTTP/3 Support: Full protocol support with automatic negotiation
//   - Drop-in Replacement: Returns standard *http.Client compatible with existing code
//
// Example Usage:
//
//	// Create client with default options (random profile rotation)
//	client, err := tlsclient.NewHTTPClient(nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create client with custom options
//	opts := &tlsclient.ClientOptions{
//	    ProfileRotationMode: tlsclient.ProfileRotationRandom,
//	    Timeout:            30 * time.Second,
//	    FollowRedirects:    true,
//	}
//	client, err := tlsclient.NewHTTPClient(opts)
//
// Profile Rotation Modes:
//   - ProfileRotationOff: Always use the same profile (Chrome 144)
//   - ProfileRotationRandom: Randomly select a profile for each client
//   - ProfileRotationSequential: Rotate through profiles in order
package tlsclient
