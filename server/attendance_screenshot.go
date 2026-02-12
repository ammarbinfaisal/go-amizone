package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

const attendanceScreenshotCooldown = 10 * time.Minute

var (
	errBrowserLoginUnauthorized = errors.New("browser-login unauthorized")

	globalAttendanceScreenshotLimiter = NewAttendanceScreenshotLimiter(attendanceScreenshotCooldown)
)

type attendanceScreenshotLimiter struct {
	mu         sync.Mutex
	cooldown   time.Duration
	lastByUser map[string]time.Time
}

func NewAttendanceScreenshotLimiter(cooldown time.Duration) *attendanceScreenshotLimiter {
	if cooldown <= 0 {
		cooldown = attendanceScreenshotCooldown
	}

	return &attendanceScreenshotLimiter{
		cooldown:   cooldown,
		lastByUser: make(map[string]time.Time),
	}
}

func (l *attendanceScreenshotLimiter) Reserve(user string, now time.Time) (release func(success bool), retryAfter time.Duration, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if last, exists := l.lastByUser[user]; exists {
		nextAllowed := last.Add(l.cooldown)
		if now.Before(nextAllowed) {
			return nil, nextAllowed.Sub(now), false
		}
	}

	l.lastByUser[user] = now
	alreadyReleased := false
	return func(success bool) {
		l.mu.Lock()
		defer l.mu.Unlock()

		if alreadyReleased {
			return
		}
		alreadyReleased = true

		if !success {
			delete(l.lastByUser, user)
		}
	}, 0, true
}

type browserLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type rateLimitedErrorResponse struct {
	Error             string `json:"error"`
	RetryAfterSeconds int64  `json:"retry_after_seconds"`
}

func (s *ApiServer) handleAttendanceScreenshot(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	username, password, ok := request.BasicAuth()
	if !ok || username == "" || password == "" {
		writer.Header().Set("WWW-Authenticate", `Basic realm="go-amizone"`)
		writeJSON(writer, http.StatusUnauthorized, errorResponse{Error: "authentication required"})
		return
	}

	release, retryAfter, allowed := globalAttendanceScreenshotLimiter.Reserve(username, time.Now())
	if !allowed {
		retryAfterSeconds := int64(math.Ceil(retryAfter.Seconds()))
		if retryAfterSeconds < 1 {
			retryAfterSeconds = 1
		}

		writer.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSeconds))
		writeJSON(writer, http.StatusTooManyRequests, rateLimitedErrorResponse{
			Error:             "screenshot is rate limited for this user",
			RetryAfterSeconds: retryAfterSeconds,
		})
		return
	}

	success := false
	defer release(success)

	png, err := s.fetchAttendanceScreenshot(request.Context(), username, password)
	if err != nil {
		switch {
		case errors.Is(err, errBrowserLoginUnauthorized):
			writer.Header().Set("WWW-Authenticate", `Basic realm="go-amizone"`)
			writeJSON(writer, http.StatusUnauthorized, errorResponse{Error: "invalid credentials"})
		default:
			writeJSON(writer, http.StatusBadGateway, errorResponse{Error: "failed to capture attendance screenshot"})
		}
		return
	}

	success = true
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(png)
}

func (s *ApiServer) fetchAttendanceScreenshot(ctx context.Context, username, password string) ([]byte, error) {
	endpoint := strings.TrimRight(s.config.BrowserLoginURL, "/") + "/attendance-screenshot"
	payload, err := json.Marshal(browserLoginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		return nil, err
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 2 * time.Minute}
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized {
		return nil, errBrowserLoginUnauthorized
	}

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return nil, fmt.Errorf("browser-login screenshot failed (%d): %s", response.StatusCode, string(body))
	}

	png, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if len(png) == 0 {
		return nil, errors.New("browser-login returned an empty screenshot")
	}

	return png, nil
}

func writeJSON(writer http.ResponseWriter, statusCode int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(body)
}
