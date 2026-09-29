// Package client is a small HTTP client for the Gezor Cloud API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	WorkspaceHeader = "X-GZR-Workspace"
	DefaultEndpoint = "https://app.gezor.cloud"

	expiredRetries = 2
)

type Client struct {
	Endpoint     string
	Token        string
	Workspace    string
	UserAgent    string
	HTTP         *http.Client
	MaxRetries   int
	RetryBase    time.Duration
	RetryMax     time.Duration
	PollInterval time.Duration
	PollTimeout  time.Duration
}

func New(endpoint, token, workspace, userAgent string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		Endpoint:     strings.TrimRight(endpoint, "/"),
		Token:        token,
		Workspace:    workspace,
		UserAgent:    userAgent,
		HTTP:         &http.Client{Timeout: 90 * time.Second},
		MaxRetries:   4,
		RetryBase:    time.Second,
		RetryMax:     15 * time.Second,
		PollInterval: 2 * time.Second,
		PollTimeout:  5 * time.Minute,
	}
}

// APIError is a non-2xx response from the API.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Detail     string
}

func (e *APIError) Error() string {
	detail := e.Detail
	if detail == "" {
		detail = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.StatusCode, detail)
}

func statusIs(err error, code int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == code
}

func IsNotFound(err error) bool { return statusIs(err, http.StatusNotFound) }
func IsConflict(err error) bool { return statusIs(err, http.StatusConflict) }

// PathEscape escapes one path segment.
func PathEscape(s string) string { return url.PathEscape(s) }

func (c *Client) Get(ctx context.Context, path, workspace string, out any) error {
	return c.Do(ctx, http.MethodGet, path, workspace, nil, out)
}

func (c *Client) Post(ctx context.Context, path, workspace string, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, workspace, body, out)
}

func (c *Client) Put(ctx context.Context, path, workspace string, body, out any) error {
	return c.Do(ctx, http.MethodPut, path, workspace, body, out)
}

func (c *Client) Patch(ctx context.Context, path, workspace string, body, out any) error {
	return c.Do(ctx, http.MethodPatch, path, workspace, body, out)
}

func (c *Client) Delete(ctx context.Context, path, workspace string, out any) error {
	return c.Do(ctx, http.MethodDelete, path, workspace, nil, out)
}

// Do sends one request. workspace overrides the client's default workspace when set.
func (c *Client) Do(ctx context.Context, method, path, workspace string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
	}
	if workspace == "" {
		workspace = c.Workspace
	}
	idempotent := method == http.MethodGet || method == http.MethodHead
	for attempt := 0; ; attempt++ {
		status, respBody, header, err := c.send(ctx, method, path, workspace, payload)
		if err != nil {
			if ctx.Err() != nil || !idempotent || attempt >= c.MaxRetries {
				return fmt.Errorf("%s %s: %w", method, path, err)
			}
		} else if status >= 200 && status < 300 {
			if out == nil || len(bytes.TrimSpace(respBody)) == 0 {
				return nil
			}
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("%s %s: decode response: %w", method, path, err)
			}
			return nil
		} else if !retryable(status, idempotent) || attempt >= c.MaxRetries {
			return &APIError{StatusCode: status, Method: method, Path: path, Detail: errorDetail(respBody)}
		}
		if err := sleep(ctx, c.backoff(attempt, header)); err != nil {
			return err
		}
	}
}

func (c *Client) send(ctx context.Context, method, path, workspace string, payload []byte) (int, []byte, http.Header, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, reader)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if workspace != "" {
		req.Header.Set(WorkspaceHeader, workspace)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, data, resp.Header, nil
}

// retryable reports whether a status may be retried. Writes are only retried when the
// server cannot have acted on them.
func retryable(status int, idempotent bool) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	case http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusInternalServerError:
		return idempotent
	}
	return false
}

func (c *Client) backoff(attempt int, header http.Header) time.Duration {
	if header != nil {
		if s, err := strconv.Atoi(header.Get("Retry-After")); err == nil && s >= 0 {
			d := time.Duration(s) * time.Second
			if d > c.RetryMax {
				d = c.RetryMax
			}
			return d
		}
	}
	d := c.RetryBase << attempt
	if d > c.RetryMax || d <= 0 {
		d = c.RetryMax
	}
	return d
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func errorDetail(body []byte) string {
	var parsed struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Detail) == 0 {
		s := strings.TrimSpace(string(body))
		if len(s) > 300 {
			s = s[:300]
		}
		return s
	}
	var text string
	if json.Unmarshal(parsed.Detail, &text) == nil {
		return text
	}
	var items []struct {
		Loc []any  `json:"loc"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(parsed.Detail, &items) == nil && len(items) > 0 {
		parts := make([]string, 0, len(items))
		for _, it := range items {
			loc := make([]string, 0, len(it.Loc))
			for _, l := range it.Loc {
				if s := fmt.Sprint(l); s != "body" {
					loc = append(loc, s)
				}
			}
			if len(loc) > 0 {
				parts = append(parts, strings.Join(loc, ".")+": "+it.Msg)
			} else {
				parts = append(parts, it.Msg)
			}
		}
		return strings.Join(parts, "; ")
	}
	return string(parsed.Detail)
}

// CommandResult is the state of an asynchronous cluster command.
type CommandResult struct {
	RequestID string          `json:"request_id"`
	Status    string          `json:"status"`
	Result    json.RawMessage `json:"result"`
	Error     string          `json:"error"`
	Message   string          `json:"message"`
}

// Enqueued is the response of an endpoint that starts an asynchronous cluster command.
type Enqueued struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

// WaitCommand polls pollPath until the command leaves the pending state.
func (c *Client) WaitCommand(ctx context.Context, pollPath, workspace string) (*CommandResult, error) {
	timeout := c.PollTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	expired := 0
	for {
		var res CommandResult
		if err := c.Get(ctx, pollPath, workspace, &res); err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("timed out waiting for the cluster operator (%s): %w", pollPath, ctx.Err())
			}
			return nil, err
		}
		switch res.Status {
		case "ready":
			return &res, nil
		case "pending", "":
		case "error":
			msg := res.Error
			if msg == "" {
				msg = res.Message
			}
			if msg == "" {
				msg = "the cluster operator reported an error"
			}
			return &res, fmt.Errorf("%s", msg)
		case "expired":
			// The API reports "expired" when a poll lands between the operator
			// finishing the command and its result becoming readable.
			expired++
			if expired <= expiredRetries {
				break
			}
			fallthrough
		default:
			msg := res.Error
			if msg == "" {
				msg = "command " + res.Status
			}
			return &res, fmt.Errorf("%s (is the cluster operator online?)", msg)
		}
		if err := sleep(ctx, c.PollInterval); err != nil {
			return nil, fmt.Errorf("timed out waiting for the cluster operator (%s): %w", pollPath, err)
		}
	}
}

// RunCommand starts a command with a POST (or other method) and waits for its result.
func (c *Client) RunCommand(ctx context.Context, method, path, workspace string, body any, pollPath func(requestID string) string) (*CommandResult, error) {
	var enq Enqueued
	if err := c.Do(ctx, method, path, workspace, body, &enq); err != nil {
		return nil, err
	}
	if enq.RequestID == "" {
		return nil, fmt.Errorf("%s %s: response had no request_id", method, path)
	}
	return c.WaitCommand(ctx, pollPath(enq.RequestID), workspace)
}
