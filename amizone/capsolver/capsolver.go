package capsolver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

const (
	capSolverAPIURL = "https://api.capsolver.com"
	createTaskURL   = capSolverAPIURL + "/createTask"
	getTaskURL      = capSolverAPIURL + "/getTaskResult"
)

// TaskType represents the type of CAPTCHA to solve
type TaskType string

const (
	// TaskTypeTurnstileProxyLess is for Cloudflare Turnstile without proxy
	TaskTypeTurnstileProxyLess TaskType = "AntiTurnstileTaskProxyLess"
	// TaskTypeTurnstile is for Cloudflare Turnstile with proxy
	TaskTypeTurnstile TaskType = "AntiTurnstileTask"
	// TaskTypeRecaptchaV2ProxyLess is for reCAPTCHA v2 without proxy
	TaskTypeRecaptchaV2ProxyLess TaskType = "ReCaptchaV2TaskProxyLess"
	// TaskTypeRecaptchaV2 is for reCAPTCHA v2 with proxy
	TaskTypeRecaptchaV2 TaskType = "ReCaptchaV2Task"
)

// ProxyInfo represents proxy configuration for CapSolver
type ProxyInfo struct {
	ProxyType     string `json:"proxyType"`     // http, https, socks5
	ProxyAddress  string `json:"proxyAddress"`  // host:port
	ProxyLogin    string `json:"proxyLogin,omitempty"`
	ProxyPassword string `json:"proxyPassword,omitempty"`
}

// Client is a CapSolver API client
type Client struct {
	apiKey     string
	httpClient *http.Client
	proxy      *ProxyInfo
}

// NewClient creates a new CapSolver client
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// WithProxy sets proxy configuration for CapSolver tasks
func (c *Client) WithProxy(proxyType, address, login, password string) *Client {
	c.proxy = &ProxyInfo{
		ProxyType:     proxyType,
		ProxyAddress:  address,
		ProxyLogin:    login,
		ProxyPassword: password,
	}
	return c
}

// TurnstileTask represents a Cloudflare Turnstile challenge
type TurnstileTask struct {
	Type       TaskType          `json:"type"`
	WebsiteURL string            `json:"websiteURL"`
	WebsiteKey string            `json:"websiteKey"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Proxy      *ProxyInfo        `json:"proxy,omitempty"`
}

// RecaptchaV2Task represents a reCAPTCHA v2 challenge
type RecaptchaV2Task struct {
	Type       TaskType   `json:"type"`
	WebsiteURL string     `json:"websiteURL"`
	WebsiteKey string     `json:"websiteKey"`
	Proxy      *ProxyInfo `json:"proxy,omitempty"`
}

// CreateTaskRequest is the request structure for creating a task
type CreateTaskRequest struct {
	ClientKey string      `json:"clientKey"`
	Task      interface{} `json:"task"`
}

// CreateTaskResponse is the response from creating a task
type CreateTaskResponse struct {
	ErrorID          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ErrorDescription string `json:"errorDescription,omitempty"`
	TaskID           string `json:"taskId"`
}

// GetTaskResultRequest is the request structure for getting task result
type GetTaskResultRequest struct {
	ClientKey string `json:"clientKey"`
	TaskID    string `json:"taskId"`
}

// TaskSolution represents the solution to a CAPTCHA challenge
type TaskSolution struct {
	Token string `json:"token"`
}

// GetTaskResultResponse is the response from getting task result
type GetTaskResultResponse struct {
	ErrorID          int          `json:"errorId"`
	ErrorCode        string       `json:"errorCode,omitempty"`
	ErrorDescription string       `json:"errorDescription,omitempty"`
	Status           string       `json:"status"`
	Solution         TaskSolution `json:"solution,omitempty"`
}

// SolveTurnstile solves a Cloudflare Turnstile challenge
// Always uses AntiTurnstileTaskProxyLess as Turnstile doesn't require proxy
func (c *Client) SolveTurnstile(websiteURL, websiteKey string) (string, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			klog.Infof("CapSolver: retrying Turnstile solve (attempt %d/3)", i+1)
			time.Sleep(time.Second * 2)
		}

		klog.Infof("CapSolver: creating Turnstile task for URL=%s, siteKey=%s", websiteURL, websiteKey)
		task := TurnstileTask{
			Type:       TaskTypeTurnstileProxyLess,
			WebsiteURL: websiteURL,
			WebsiteKey: websiteKey,
		}

		taskID, err := c.createTask(task)
		if err != nil {
			klog.Errorf("CapSolver: failed to create task: %v", err)
			lastErr = fmt.Errorf("failed to create turnstile task: %w", err)
			continue
		}

		klog.Infof("Created CapSolver task for Turnstile: %s", taskID)

		token, err := c.waitForTaskResult(taskID)
		if err != nil {
			klog.Errorf("CapSolver: failed to get solution: %v", err)
			lastErr = fmt.Errorf("failed to get turnstile solution: %w", err)
			continue
		}

		klog.Infof("CapSolver: got Turnstile token (len=%d)", len(token))
		return token, nil
	}
	return "", lastErr
}

// SolveRecaptchaV2 solves a reCAPTCHA v2 challenge
func (c *Client) SolveRecaptchaV2(websiteURL, websiteKey string) (string, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			klog.Infof("CapSolver: retrying reCAPTCHA v2 solve (attempt %d/3)", i+1)
			time.Sleep(time.Second * 2)
		}

		taskType := TaskTypeRecaptchaV2ProxyLess
		if c.proxy != nil {
			taskType = TaskTypeRecaptchaV2
			klog.V(2).Infof("Using proxy for reCAPTCHA: %s", c.proxy.ProxyAddress)
		}

		task := RecaptchaV2Task{
			Type:       taskType,
			WebsiteURL: websiteURL,
			WebsiteKey: websiteKey,
			Proxy:      c.proxy,
		}

		taskID, err := c.createTask(task)
		if err != nil {
			lastErr = fmt.Errorf("failed to create recaptcha task: %w", err)
			continue
		}

		klog.V(2).Infof("Created CapSolver task for reCAPTCHA v2: %s", taskID)

		token, err := c.waitForTaskResult(taskID)
		if err != nil {
			lastErr = fmt.Errorf("failed to get recaptcha solution: %w", err)
			continue
		}

		return token, nil
	}
	return "", lastErr
}

// createTask creates a new task on CapSolver
func (c *Client) createTask(task interface{}) (string, error) {
	reqBody := CreateTaskRequest{
		ClientKey: c.apiKey,
		Task:      task,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	klog.Infof("CapSolver: sending createTask request to %s", createTaskURL)
	resp, err := c.httpClient.Post(createTaskURL, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	klog.Infof("CapSolver: createTask response: %s", string(body))

	var result CreateTaskResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.ErrorID != 0 {
		return "", fmt.Errorf("capsolver error %s: %s", result.ErrorCode, result.ErrorDescription)
	}

	if result.TaskID == "" {
		return "", errors.New("no task ID returned")
	}

	return result.TaskID, nil
}

// waitForTaskResult polls CapSolver until the task is complete
func (c *Client) waitForTaskResult(taskID string) (string, error) {
	reqBody := GetTaskResultRequest{
		ClientKey: c.apiKey,
		TaskID:    taskID,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Poll for up to 120 seconds
	timeout := time.After(120 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return "", errors.New("timeout waiting for captcha solution")
		case <-ticker.C:
			resp, err := c.httpClient.Post(getTaskURL, "application/json", bytes.NewReader(jsonData))
			if err != nil {
				klog.V(2).Infof("Error polling task result: %v", err)
				continue
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				klog.V(2).Infof("Error reading response: %v", err)
				continue
			}

			var result GetTaskResultResponse
			if err := json.Unmarshal(body, &result); err != nil {
				klog.V(2).Infof("Error unmarshaling response: %v", err)
				continue
			}

			if result.ErrorID != 0 {
				return "", fmt.Errorf("capsolver error %s: %s", result.ErrorCode, result.ErrorDescription)
			}

			if result.Status == "ready" {
				if result.Solution.Token == "" {
					return "", errors.New("no token in solution")
				}
				return result.Solution.Token, nil
			}

			// Status is "processing", continue waiting
			klog.V(3).Infof("Task %s status: %s", taskID, result.Status)
		}
	}
}
