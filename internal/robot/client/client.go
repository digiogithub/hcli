// Package client is a small, dependency-free client for the Hetzner Robot
// webservice (https://robot.hetzner.com/doc/webservice/en.html).
//
// Protocol notes (from the official reference):
//   - HTTP Basic auth with the *webservice* user (not the Robot login).
//     Three failed logins block the source IP for 10 minutes.
//   - Request bodies are application/x-www-form-urlencoded; arrays are sent
//     as repeated "key[]" fields, nested objects as "a[b][0][c]".
//   - Responses are JSON. Most objects are wrapped by their type name:
//     [{"server":{...}}] or {"reset":{...}}. vSwitch responses are not wrapped.
//   - Errors are {"error":{"status":404,"code":"SERVER_NOT_FOUND","message":"..."}}.
//   - Rate limits are per endpoint (e.g. 200 req/h for /server); hitting one
//     returns 403 (documented) or 429 with code RATE_LIMIT_EXCEEDED.
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

	"github.com/digiogithub/hcli/internal/version"
)

// DefaultEndpoint is the production Robot webservice.
const DefaultEndpoint = "https://robot-ws.your-server.de"

// Client talks to the Robot webservice.
type Client struct {
	endpoint   string
	user       string
	password   string
	httpClient *http.Client
	userAgent  string
	debug      func(format string, args ...any)

	maxRetries int
	retryDelay time.Duration
	maxDelay   time.Duration
	sleep      func(context.Context, time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the default *http.Client (30s timeout).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithTimeout sets the timeout of the default HTTP client.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.httpClient.Timeout = d } }

// WithDebug installs a logger called once per request with method, path,
// status and duration. Credentials and request bodies are never logged.
func WithDebug(f func(format string, args ...any)) Option { return func(c *Client) { c.debug = f } }

// WithRetry configures retries on rate limiting: at most n retries, waiting
// base, 2*base, 4*base... (capped at 10s, or the Retry-After header if lower).
func WithRetry(n int, base time.Duration) Option {
	return func(c *Client) { c.maxRetries, c.retryDelay = n, base }
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// New returns a client for endpoint ("" = DefaultEndpoint).
func New(endpoint, user, password string, opts ...Option) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	c := &Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		user:       user,
		password:   password,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  "hcli/" + version.Get(),
		maxRetries: 2,
		retryDelay: 2 * time.Second,
		maxDelay:   10 * time.Second,
		sleep:      sleepCtx,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// APIError is an error answered by the webservice.
type APIError struct {
	Status  int      `json:"status"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Missing []string `json:"missing,omitempty"`
	Invalid []string `json:"invalid,omitempty"`
	// Rate limit details (RATE_LIMIT_EXCEEDED).
	MaxRequest int `json:"max_request,omitempty"`
	Interval   int `json:"interval,omitempty"`
}

func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("robot API: ")
	if e.Code != "" {
		b.WriteString(e.Code)
		b.WriteString(" ")
	}
	fmt.Fprintf(&b, "(HTTP %d)", e.Status)
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if len(e.Missing) > 0 {
		b.WriteString("; missing: " + strings.Join(e.Missing, ", "))
	}
	if len(e.Invalid) > 0 {
		b.WriteString("; invalid: " + strings.Join(e.Invalid, ", "))
	}
	if e.Code == "RATE_LIMIT_EXCEEDED" && e.MaxRequest > 0 {
		fmt.Fprintf(&b, " (limit %d requests per %ds)", e.MaxRequest, e.Interval)
	}
	if e.Status == http.StatusUnauthorized {
		b.WriteString(" (check the webservice user/password; 3 failed logins block your IP for 10 minutes)")
	}
	return b.String()
}

// IsRateLimit reports whether the error is a rate limit answer.
func (e *APIError) IsRateLimit() bool {
	return e.Code == "RATE_LIMIT_EXCEEDED" || e.Status == http.StatusTooManyRequests
}

// IsNotFound reports whether err is an APIError with HTTP 404.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// HasCode reports whether err is an APIError with the given code.
func HasCode(err error, code string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Code == code
}

// Get performs a GET; query is sent as URL query string. key is the envelope
// name to unwrap ("" when the response is not wrapped); out may be nil.
func (c *Client) Get(ctx context.Context, path string, query url.Values, key string, out any) error {
	return c.Do(ctx, http.MethodGet, path, query, key, out)
}

// Post performs a form-encoded POST.
func (c *Client) Post(ctx context.Context, path string, form url.Values, key string, out any) error {
	return c.Do(ctx, http.MethodPost, path, form, key, out)
}

// Put performs a form-encoded PUT.
func (c *Client) Put(ctx context.Context, path string, form url.Values, key string, out any) error {
	return c.Do(ctx, http.MethodPut, path, form, key, out)
}

// Delete performs a DELETE; form (if any) is sent as a form-encoded body,
// as the API expects for e.g. DELETE /vswitch/{id} cancellation_date.
func (c *Client) Delete(ctx context.Context, path string, form url.Values, key string, out any) error {
	return c.Do(ctx, http.MethodDelete, path, form, key, out)
}

// Do sends a request, retries on rate limiting and decodes the response into
// out, unwrapping the type envelope named key (both for objects and arrays).
func (c *Client) Do(ctx context.Context, method, path string, values url.Values, key string, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	u := c.endpoint + path
	var body string
	if len(values) > 0 {
		if method == http.MethodGet {
			u += "?" + values.Encode()
		} else {
			body = values.Encode()
		}
	}
	for attempt := 0; ; attempt++ {
		data, status, header, err := c.once(ctx, method, u, path, body)
		if err != nil {
			return err
		}
		if status >= 200 && status < 300 {
			return decode(data, key, out)
		}
		apiErr := parseError(status, data)
		if apiErr.IsRateLimit() && attempt < c.maxRetries {
			delay := c.retryDelay << attempt
			if ra := retryAfter(header.Get("Retry-After")); ra > 0 && ra < delay {
				delay = ra
			}
			if delay > c.maxDelay {
				delay = c.maxDelay
			}
			c.logf("rate limited on %s %s, retrying in %s (%d/%d)", method, path, delay, attempt+1, c.maxRetries)
			if err := c.sleep(ctx, delay); err != nil {
				return err
			}
			continue
		}
		return apiErr
	}
}

func (c *Client) once(ctx context.Context, method, u, path, body string) ([]byte, int, http.Header, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, 0, nil, err
	}
	req.SetBasicAuth(c.user, c.password)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logf("%s %s -> error after %s", method, path, time.Since(start).Round(time.Millisecond))
		// url.Error contains the URL only (no userinfo: credentials go in a header).
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	c.logf("%s %s -> %d (%s)", method, path, resp.StatusCode, time.Since(start).Round(time.Millisecond))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("read response of %s %s: %w", method, path, err)
	}
	return data, resp.StatusCode, resp.Header, nil
}

func (c *Client) logf(format string, args ...any) {
	if c.debug != nil {
		c.debug(format, args...)
	}
}

func retryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 0
}

func parseError(status int, data []byte) *APIError {
	var env struct {
		Error *APIError `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err == nil && env.Error != nil {
		if env.Error.Status == 0 {
			env.Error.Status = status
		}
		return env.Error
	}
	msg := strings.TrimSpace(string(data))
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	e := &APIError{Status: status, Message: msg}
	if status == http.StatusTooManyRequests {
		e.Code = "RATE_LIMIT_EXCEEDED"
	}
	return e
}

// decode unmarshals data into out, unwrapping the envelope key.
func decode(data []byte, key string, out any) error {
	if out == nil {
		return nil
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if key == "" {
		return json.Unmarshal(data, out)
	}
	inner, err := Unwrap(data, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(inner, out)
}

// Unwrap removes the type envelope from a Robot response: {"key":X} -> X and
// [{"key":X},{"key":Y}] -> [X,Y]. Elements without the key are kept as is.
func Unwrap(data []byte, key string) ([]byte, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return data, nil
	}
	switch data[0] {
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		out := make([]json.RawMessage, len(items))
		for i, item := range items {
			out[i] = unwrapObject(item, key)
		}
		return json.Marshal(out)
	case '{':
		return unwrapObject(data, key), nil
	}
	return data, nil
}

func unwrapObject(data json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return data
	}
	if inner, ok := m[key]; ok && len(m) == 1 {
		return inner
	}
	return data
}

// AddArray appends values as "key[]" form fields (the Robot array syntax).
func AddArray(v url.Values, key string, values ...string) {
	for _, s := range values {
		v.Add(key+"[]", s)
	}
}
