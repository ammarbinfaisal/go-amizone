package server

import (
	"os"
	"sync"
	"time"

	"github.com/ditsuke/go-amizone/amizone"
	"k8s.io/klog/v2"
)

// SessionCache stores logged-in amizone clients to avoid re-login per request
type SessionCache struct {
	mu       sync.RWMutex
	sessions map[string]*cachedSession
	ttl      time.Duration
}

type cachedSession struct {
	client    *amizone.Client
	createdAt time.Time
	lastUsed  time.Time
}

// DefaultSessionTTL is the default time-to-live for cached sessions
const DefaultSessionTTL = 30 * time.Minute

// NewSessionCache creates a new session cache with the given TTL
func NewSessionCache(ttl time.Duration) *SessionCache {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	sc := &SessionCache{
		sessions: make(map[string]*cachedSession),
		ttl:      ttl,
	}
	// Start cleanup goroutine
	go sc.cleanupLoop()
	return sc
}

// Get retrieves a cached client for the given credentials
// Returns nil if not found or expired
func (sc *SessionCache) Get(username, password string) *amizone.Client {
	key := sc.makeKey(username, password)

	sc.mu.RLock()
	session, exists := sc.sessions[key]
	sc.mu.RUnlock()

	if !exists {
		return nil
	}

	// Check if session is expired
	if time.Since(session.createdAt) > sc.ttl {
		sc.Delete(username, password)
		return nil
	}

	// Update last used time
	sc.mu.Lock()
	session.lastUsed = time.Now()
	sc.mu.Unlock()

	return session.client
}

// Set stores a client in the cache
func (sc *SessionCache) Set(username, password string, client *amizone.Client) {
	key := sc.makeKey(username, password)
	now := time.Now()

	sc.mu.Lock()
	sc.sessions[key] = &cachedSession{
		client:    client,
		createdAt: now,
		lastUsed:  now,
	}
	sc.mu.Unlock()

	klog.V(2).Infof("Session cached for user: %s", username)
}

// Delete removes a session from the cache
func (sc *SessionCache) Delete(username, password string) {
	key := sc.makeKey(username, password)

	sc.mu.Lock()
	delete(sc.sessions, key)
	sc.mu.Unlock()

	klog.V(2).Infof("Session removed for user: %s", username)
}

// GetOrCreate returns a cached client or creates a new one
func (sc *SessionCache) GetOrCreate(username, password string) (*amizone.Client, error) {
	// Try to get from cache first with read lock
	sc.mu.RLock()
	session, exists := sc.sessions[sc.makeKey(username, password)]
	sc.mu.RUnlock()

	if exists && time.Since(session.createdAt) <= sc.ttl {
		klog.V(2).Infof("Using cached session for user: %s", username)
		sc.mu.Lock()
		session.lastUsed = time.Now()
		sc.mu.Unlock()
		return session.client, nil
	}

	// Create new client - we use a lock here to prevent multiple simultaneous creations
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Check again in case someone else created it while we were waiting for the lock
	key := sc.makeKey(username, password)
	if session, exists := sc.sessions[key]; exists && time.Since(session.createdAt) <= sc.ttl {
		return session.client, nil
	}

	klog.V(2).Infof("Creating new session for user: %s", username)
	opts := []amizone.ClientOption{
		amizone.WithTLSClient(nil),
	}
	if apiKey := os.Getenv("CAPSOLVER_API_KEY"); apiKey != "" {
		opts = append(opts, amizone.WithCapSolver(apiKey))
	}
	client, err := amizone.NewClientWithOptions(
		amizone.Credentials{Username: username, Password: password},
		opts...,
	)
	if err != nil {
		return nil, err
	}

	// Cache the new client
	now := time.Now()
	sc.sessions[key] = &cachedSession{
		client:    client,
		createdAt: now,
		lastUsed:  now,
	}
	klog.V(2).Infof("Session cached for user: %s", username)

	return client, nil
}

// makeKey creates a cache key from credentials
func (sc *SessionCache) makeKey(username, password string) string {
	// Simple concatenation - in production you might want to hash this
	return username + ":" + password
}

// cleanupLoop periodically removes expired sessions
func (sc *SessionCache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sc.cleanup()
	}
}

// cleanup removes all expired sessions
func (sc *SessionCache) cleanup() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()
	expired := make([]string, 0)

	for key, session := range sc.sessions {
		if now.Sub(session.createdAt) > sc.ttl {
			expired = append(expired, key)
		}
	}

	for _, key := range expired {
		delete(sc.sessions, key)
	}

	if len(expired) > 0 {
		klog.V(2).Infof("Cleaned up %d expired sessions", len(expired))
	}
}

// Stats returns cache statistics
func (sc *SessionCache) Stats() (total int, active int) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	now := time.Now()
	for _, session := range sc.sessions {
		total++
		if now.Sub(session.createdAt) <= sc.ttl {
			active++
		}
	}
	return
}
