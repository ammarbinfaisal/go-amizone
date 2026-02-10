# TLS Client Package

This package provides HTTP client creation with TLS fingerprinting and browser impersonation capabilities for the go-amizone SDK.

## Overview

The `tlsclient` package wraps [bogdanfinn/tls-client](https://github.com/bogdanfinn/tls-client) to create HTTP clients that mimic real browser TLS fingerprints. This helps avoid detection and blocking by websites that use TLS fingerprinting for bot detection.

## Features

- **Browser Profile Rotation**: Rotate between multiple browser profiles (Chrome, Firefox)
- **TLS Fingerprinting**: Accurate TLS fingerprints matching real browsers
- **HTTP/2 and HTTP/3 Support**: Full protocol support with automatic negotiation
- **Drop-in Replacement**: Returns standard `*http.Client` compatible with existing code
- **Cookie Jar Support**: Automatic cookie management and conversion
- **Configurable Timeouts**: Custom timeout and redirect behavior

## Usage

### Basic Usage

```go
import "github.com/ditsuke/go-amizone/amizone/tlsclient"

// Create client with default options (random profile rotation)
client, err := tlsclient.NewHTTPClient(nil)
if err != nil {
    log.Fatal(err)
}

// Use like a standard http.Client
resp, err := client.Get("https://example.com")
```

### Custom Options

```go
opts := &tlsclient.ClientOptions{
    ProfileRotationMode: tlsclient.ProfileRotationRandom,
    CustomProfiles:      tlsclient.DefaultProfiles,
    Timeout:            30 * time.Second,
    FollowRedirects:    true,
}

client, err := tlsclient.NewHTTPClient(opts)
```

### Profile Rotation Modes

#### Random Rotation (Recommended)
Randomly selects a browser profile for each client instance. Best for avoiding pattern detection.

```go
ProfileRotationMode: tlsclient.ProfileRotationRandom
```

#### Sequential Rotation
Rotates through profiles in order. Useful for consistent testing.

```go
ProfileRotationMode: tlsclient.ProfileRotationSequential
```

#### Fixed Profile
Always uses the same profile (first in the list).

```go
ProfileRotationMode: tlsclient.ProfileRotationOff
```

### Using with go-amizone

```go
import (
    "github.com/ditsuke/go-amizone/amizone"
    "github.com/ditsuke/go-amizone/amizone/tlsclient"
)

// Create Amizone client with TLS fingerprinting
client, err := amizone.NewClientWithOptions(
    amizone.Credentials{Username: "user", Password: "pass"},
    amizone.WithTLSClient(nil), // Use default TLS options
)
```

## Default Browser Profiles

The package includes profiles for modern browser versions:

- **Chrome**: 144, 146, 133, 131
- **Firefox**: 147, 135, 133

These profiles are automatically maintained and updated by the tls-client library.

## Architecture

### Components

- **`NewHTTPClient()`**: Main factory function for creating TLS-enabled HTTP clients
- **`ClientOptions`**: Configuration structure for customizing client behavior
- **`tlsClientTransport`**: Custom RoundTripper that converts between net/http and fhttp
- **`cookieJarWrapper`**: Bridges net/http.CookieJar and fhttp.CookieJar interfaces

### Request Flow

1. Client receives `*http.Request`
2. Request converted to `*fhttp.Request`
3. TLS client executes request with browser fingerprint
4. `*fhttp.Response` converted back to `*http.Response`
5. Response returned to caller

## Performance

- **Initial client creation**: ~10-20ms
- **Per-request overhead**: Negligible (<1ms)
- **Memory usage**: Similar to standard http.Client
- **Connection pooling**: Handled by underlying tls-client library

## When to Use

Use TLS fingerprinting when:

- Experiencing rate limiting or blocking
- Website uses bot detection systems (Cloudflare, DataDome, etc.)
- Need to make many requests without being flagged
- Standard HTTP requests are being rejected

## Testing

The package includes comprehensive tests:

```bash
go test -v github.com/ditsuke/go-amizone/amizone/tlsclient
```

Tests cover:
- Client creation with various options
- Profile rotation modes
- HTTP request/response conversion
- Cookie jar compatibility
- Standard HTTP operations

## Dependencies

- [bogdanfinn/tls-client](https://github.com/bogdanfinn/tls-client) v1.14.0+
- [bogdanfinn/fhttp](https://github.com/bogdanfinn/fhttp) v0.6.8+
- Standard library: `net/http`, `net/url`

## Limitations

- Only supports HTTP/HTTPS protocols
- Some advanced http.Client features may not be fully compatible
- Profile fingerprints depend on upstream tls-client library updates

## Contributing

When adding new features:

1. Ensure backward compatibility with net/http
2. Add tests for new functionality
3. Update documentation
4. Test with real Amizone portal if possible

## See Also

- [TLS Fingerprinting Explained](https://httptoolkit.tech/blog/tls-fingerprinting-node-js/)
- [bogdanfinn/tls-client Documentation](https://bogdanfinn.gitbook.io/open-source-oasis/)
- [Example Usage](../../examples/tls_client/)
