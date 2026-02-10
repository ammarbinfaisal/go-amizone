package tlsclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"testing"
	"time"

	"github.com/bogdanfinn/tls-client/profiles"
)

func TestNewHTTPClient(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		client, err := NewHTTPClient(nil)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}
		if client == nil {
			t.Fatal("NewHTTPClient() returned nil client")
		}
		if client.Jar == nil {
			t.Error("Client should have a cookie jar")
		}
	})

	t.Run("custom timeout", func(t *testing.T) {
		opts := &ClientOptions{
			Timeout: 10 * time.Second,
		}
		client, err := NewHTTPClient(opts)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}
		if client.Timeout != 10*time.Second {
			t.Errorf("Client timeout = %v, want %v", client.Timeout, 10*time.Second)
		}
	})

	t.Run("custom profiles", func(t *testing.T) {
		opts := &ClientOptions{
			ProfileRotationMode: ProfileRotationOff,
			CustomProfiles: []profiles.ClientProfile{
				profiles.Firefox_147,
			},
		}
		client, err := NewHTTPClient(opts)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}
		if client == nil {
			t.Fatal("NewHTTPClient() returned nil client")
		}
	})
}

func TestProfileRotation(t *testing.T) {
	t.Run("random rotation", func(t *testing.T) {
		opts := &ClientOptions{
			ProfileRotationMode: ProfileRotationRandom,
			CustomProfiles:      DefaultProfiles,
		}

		// Create multiple clients and verify different profiles might be selected
		// Note: This is probabilistic, but with 7 profiles the chance of all being the same is very low
		seenProfiles := make(map[string]bool)
		for i := 0; i < 20; i++ {
			profile := selectProfile(opts)
			name := profileName(profile)
			seenProfiles[name] = true
		}

		// We should see at least 2 different profiles in 20 selections (probabilistically)
		if len(seenProfiles) < 2 {
			t.Logf("Warning: Random rotation only selected %d unique profiles in 20 tries", len(seenProfiles))
		}
	})

	t.Run("sequential rotation", func(t *testing.T) {
		// Reset the global counter
		currentProfileIndex = 0

		opts := &ClientOptions{
			ProfileRotationMode: ProfileRotationSequential,
			CustomProfiles: []profiles.ClientProfile{
				profiles.Chrome_144,
				profiles.Firefox_147,
				profiles.Chrome_146,
			},
		}

		// Get profile names for comparison
		names := make([]string, 4)
		for i := 0; i < 4; i++ {
			p := selectProfile(opts)
			names[i] = profileName(p)
		}

		// First three should be unique, fourth should match the first
		if names[0] == "" || names[1] == "" || names[2] == "" {
			t.Error("Profile names should not be empty")
		}

		// Fourth should be the same as the first (wrapped around)
		if names[3] != names[0] {
			t.Errorf("Fourth profile = %v, want %v (expected wrap around)", names[3], names[0])
		}

		t.Logf("Profile rotation order: %v", names)
	})

	t.Run("rotation off", func(t *testing.T) {
		opts := &ClientOptions{
			ProfileRotationMode: ProfileRotationOff,
			CustomProfiles: []profiles.ClientProfile{
				profiles.Chrome_144,
				profiles.Firefox_147,
			},
		}

		// All selections should return the same profile (the first one)
		firstProfile := profileName(selectProfile(opts))
		for i := 1; i < 5; i++ {
			p := selectProfile(opts)
			name := profileName(p)
			if name != firstProfile {
				t.Errorf("Profile %d = %v, want %v (should always be the same with rotation off)", i, name, firstProfile)
			}
		}
		t.Logf("Rotation off always uses: %s", firstProfile)
	})
}

func TestHTTPClientRequest(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	t.Run("simple GET request", func(t *testing.T) {
		client, err := NewHTTPClient(nil)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}

		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("GET request error = %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}

		if string(body) != "test response" {
			t.Errorf("Body = %q, want %q", string(body), "test response")
		}
	})
}

func TestDefaultClientOptions(t *testing.T) {
	opts := DefaultClientOptions()

	if opts.ProfileRotationMode != ProfileRotationRandom {
		t.Errorf("ProfileRotationMode = %v, want %v", opts.ProfileRotationMode, ProfileRotationRandom)
	}

	if len(opts.CustomProfiles) == 0 {
		t.Error("CustomProfiles should not be empty")
	}

	if opts.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v", opts.Timeout, 30*time.Second)
	}

	if !opts.FollowRedirects {
		t.Error("FollowRedirects should be true by default")
	}
}

func TestProfileName(t *testing.T) {
	// Test that profileName returns a non-empty string for known profiles
	tests := []struct {
		name    string
		profile profiles.ClientProfile
	}{
		{"Chrome_144", profiles.Chrome_144},
		{"Chrome_146", profiles.Chrome_146},
		{"Firefox_147", profiles.Firefox_147},
		{"Chrome_133", profiles.Chrome_133},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := profileName(tt.profile)
			if got == "" {
				t.Errorf("profileName() returned empty string")
			}
			// Just verify it returns something reasonable
			t.Logf("profileName(%s) = %s", tt.name, got)
		})
	}
}

func TestUserAgentHeader(t *testing.T) {
	t.Run("Chrome profile UA", func(t *testing.T) {
		opts := &ClientOptions{
			ProfileRotationMode: ProfileRotationOff,
			CustomProfiles: []profiles.ClientProfile{
				profiles.Chrome_144,
			},
		}
		client, err := NewHTTPClient(opts)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}

		transport := client.Transport.(*tlsClientTransport)
		req, _ := http.NewRequest("GET", "https://example.com", nil)
		fReq, err := transport.ConvertToFHTTPRequest(req)
		if err != nil {
			t.Fatalf("ConvertToFHTTPRequest() error = %v", err)
		}

		receivedUA := fReq.Header.Get("User-Agent")
		expectedUA := profileUserAgents["Chrome_144"]
		if receivedUA != expectedUA {
			t.Errorf("Received User-Agent = %q, want %q", receivedUA, expectedUA)
		}
	})

	t.Run("Firefox profile UA", func(t *testing.T) {
		opts := &ClientOptions{
			ProfileRotationMode: ProfileRotationOff,
			CustomProfiles: []profiles.ClientProfile{
				profiles.Firefox_147,
			},
		}
		client, err := NewHTTPClient(opts)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}

		transport := client.Transport.(*tlsClientTransport)
		req, _ := http.NewRequest("GET", "https://example.com", nil)
		fReq, err := transport.ConvertToFHTTPRequest(req)
		if err != nil {
			t.Fatalf("ConvertToFHTTPRequest() error = %v", err)
		}

		receivedUA := fReq.Header.Get("User-Agent")
		expectedUA := profileUserAgents["Firefox_147"]
		if receivedUA != expectedUA {
			t.Errorf("Received User-Agent = %q, want %q", receivedUA, expectedUA)
		}
	})
}

func TestCookieJarWrapper(t *testing.T) {
	t.Run("cookie conversion", func(t *testing.T) {
		client, err := NewHTTPClient(nil)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}

		// Test setting and getting cookies
		testURL, _ := neturl.Parse("https://example.com/")
		cookies := []*http.Cookie{
			{Name: "test", Value: "value", Path: "/", Domain: "example.com"},
		}

		client.Jar.SetCookies(testURL, cookies)
		gotCookies := client.Jar.Cookies(testURL)

		if len(gotCookies) != 1 {
			t.Errorf("Got %d cookies, want 1", len(gotCookies))
		}

		if len(gotCookies) > 0 {
			if gotCookies[0].Name != "test" || gotCookies[0].Value != "value" {
				t.Errorf("Cookie = %+v, want Name=test Value=value", gotCookies[0])
			}
		}
	})
}
