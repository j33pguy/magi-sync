package syncagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	token      string
	maxRetries int
	client     *http.Client
}

type enrollRequest struct {
	User        string   `json:"user"`
	MachineID   string   `json:"machine_id"`
	AgentName   string   `json:"agent_name"`
	AgentType   string   `json:"agent_type"`
	Groups      []string `json:"groups,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Description string   `json:"description,omitempty"`
}

type EnrollResponse struct {
	OK     bool   `json:"ok"`
	Token  string `json:"token"`
	Record struct {
		ID        string   `json:"id"`
		User      string   `json:"user"`
		MachineID string   `json:"machine_id"`
		Groups    []string `json:"groups"`
	} `json:"record"`
}

type rememberRequest struct {
	Content    string   `json:"content"`
	Summary    string   `json:"summary"`
	Project    string   `json:"project"`
	Type       string   `json:"type"`
	Visibility string   `json:"visibility"`
	Tags       []string `json:"tags"`
	Source     string   `json:"source"`
	Speaker    string   `json:"speaker"`
}

func NewClient(cfg ServerConfig) *Client {
	return &Client{
		baseURL:    strings.TrimRight(cfg.URL, "/"),
		token:      cfg.Token,
		maxRetries: 3,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) SetToken(token string) {
	c.token = strings.TrimSpace(token)
}

func (c *Client) Remember(ctx context.Context, p Payload) error {
	body, err := json.Marshal(rememberRequest{
		Content:    p.Content,
		Summary:    p.Summary,
		Project:    p.Project,
		Type:       p.Type,
		Visibility: p.Visibility,
		Tags:       p.Tags,
		Source:     p.Source,
		Speaker:    p.Speaker,
	})
	if err != nil {
		return fmt.Errorf("marshal remember request: %w", err)
	}

	if err := c.postJSON(ctx, "/sync/memories", body, true); err == nil {
		return nil
	} else if !isNotFoundError(err) {
		return err
	}
	return c.postJSON(ctx, "/remember", body, true)
}

func (c *Client) Enroll(ctx context.Context, cfg *Config) (*EnrollResponse, error) {
	adminToken := strings.TrimSpace(cfg.Server.EnrollToken)
	if adminToken == "" {
		return nil, fmt.Errorf("server.enroll_token or server.enroll_token_env is required")
	}

	body, err := json.Marshal(enrollRequest{
		User:        cfg.Machine.User,
		MachineID:   cfg.Machine.ID,
		AgentName:   "magi-sync",
		AgentType:   "syncagent",
		Groups:      dedupeMachineGroups(cfg.Machine.Groups),
		DisplayName: cfg.Machine.ID,
		Description: "magi-sync enrolled machine credential",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal enroll request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/auth/machines/enroll", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("enroll returned status %d", resp.StatusCode)
	}

	var result EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode enroll response: %w", err)
	}
	return &result, nil
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health returned status %d", resp.StatusCode)
	}
	return nil
}

// RememberBatch uploads multiple payloads in a single batch request.
// Falls back to individual uploads if the batch endpoint is not available.
func (c *Client) RememberBatch(ctx context.Context, payloads []Payload) (int, error) {
	if len(payloads) == 0 {
		return 0, nil
	}

	// Try batch endpoint first
	items := make([]rememberRequest, len(payloads))
	for i, p := range payloads {
		items[i] = rememberRequest{
			Content:    p.Content,
			Summary:    p.Summary,
			Project:    p.Project,
			Type:       p.Type,
			Visibility: p.Visibility,
			Tags:       p.Tags,
			Source:     p.Source,
			Speaker:    p.Speaker,
		}
	}

	body, err := json.Marshal(map[string]any{"memories": items})
	if err != nil {
		return 0, fmt.Errorf("marshal batch: %w", err)
	}

	if err := c.postJSONWithRetry(ctx, "/sync/memories/batch", body, true); err == nil {
		return len(payloads), nil
	}

	// Fall back to individual uploads
	uploaded := 0
	for _, p := range payloads {
		if err := c.Remember(ctx, p); err != nil {
			continue
		}
		uploaded++
	}
	return uploaded, nil
}

func (c *Client) postJSON(ctx context.Context, path string, body []byte, auth bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("request to %s returned status %d", path, resp.StatusCode)
	}
	return nil
}

// postJSONWithRetry wraps postJSON with exponential backoff retry.
func (c *Client) postJSONWithRetry(ctx context.Context, path string, body []byte, auth bool) error {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		lastErr = c.postJSON(ctx, path, body, auth)
		if lastErr == nil {
			return nil
		}
		// Don't retry 4xx errors (except 429)
		if isClientError(lastErr) && !isRateLimited(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func isClientError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for status := 400; status < 500; status++ {
		if strings.HasSuffix(msg, fmt.Sprintf("returned status %d", status)) {
			return true
		}
	}
	return false
}

func isRateLimited(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), "returned status 429")
}

func isNotFoundError(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), "returned status 404")
}

func dedupeMachineGroups(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
