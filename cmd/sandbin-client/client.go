package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	apiKeyHeader      = "X-API-Key"
	binPasswordHeader = "X-Bin-Password"
)

// apiError is a non-2xx answer: the status, the plain-text body the server wrote,
// and for a 429 the seconds it asked us to wait.
type apiError struct {
	status     int
	message    string
	retryAfter int
}

func (e *apiError) Error() string {
	if e.status == http.StatusTooManyRequests {
		return fmt.Sprintf("%d %s: retry after %ds", e.status, e.message, e.retryAfter)
	}
	return fmt.Sprintf("%d %s", e.status, e.message)
}

// newAPIError keeps what the client reports about a refused request.
func newAPIError(resp *http.Response, body []byte) *apiError {
	// http.Error writes "<message>\n", so the trimmed body is the message itself.
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	retryAfter := 0
	if raw := resp.Header.Get("Retry-After"); raw != "" {
		// A Retry-After that is an HTTP-date instead of seconds reads as 0.
		// shortcut: seconds is all this server sends.
		retryAfter, _ = strconv.Atoi(raw)
	}
	return &apiError{status: resp.StatusCode, message: message, retryAfter: retryAfter}
}

// client talks to one server with one API key. Every call is exactly one attempt:
// a bad API key charges this address a 30 minute block that grows with every
// failure, so a retrying client only digs its own hole deeper.
type client struct {
	http     *http.Client
	base     *url.URL // the server's base URL, with no trailing slash
	apiKey   string
	password string // empty leaves the bin open
}

// newClient checks the base URL once, so a bad --url fails before any request.
func newClient(base, apiKey, password string) (*client, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("url %q: %v", base, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("url %q must be an absolute http or https URL", base)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("url %q must not carry a query or a fragment", base)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return &client{http: &http.Client{Timeout: 30 * time.Second}, base: u, apiKey: apiKey, password: password}, nil
}

// binURL is the bin base path, the one line to hand to a human.
func (c *client) binURL(bin string) string { return c.base.String() + "/" + bin + "/" }

// fileURL is where a name lands under a bin. url.URL escapes the path, so a name
// holding a space, a "#" or a "?" reaches the server as a path and never as a
// query or a fragment.
func (c *client) fileURL(bin, name string) string {
	u := *c.base
	u.Path = c.base.Path + "/" + bin + "/" + name
	return u.String()
}

// createBin POSTs "/" and returns the new bin's hash, read out of the "/abc1234/"
// body the server answers with. No Basic Auth here: the bin password is being set,
// not proven.
func (c *client) createBin() (string, error) {
	req, err := http.NewRequest(http.MethodPost, c.base.String()+"/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	if c.password != "" {
		req.Header.Set(binPasswordHeader, c.password)
	}
	_, body, err := c.do(req)
	if err != nil {
		return "", err
	}
	return binHashFromPath(string(body))
}

// binHashFromPath cuts the "/abc1234/" a created bin answers with down to its hash
// and refuses a body that is not a bin path at all.
// shortcut: the shape is checked as one path segment rather than 8 hex digits, so a
// server that names bins differently still works.
func binHashFromPath(body string) (string, error) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "/") || !strings.HasSuffix(trimmed, "/") {
		return "", fmt.Errorf("server did not return a bin path: %q", trimmed)
	}
	hash := strings.Trim(trimmed, "/")
	if hash == "" || strings.Contains(hash, "/") {
		return "", fmt.Errorf("server did not return a bin path: %q", trimmed)
	}
	return hash, nil
}

// putFile PUTs one body under a bin and reports whether the server created the file
// (201) or replaced it (200).
func (c *client) putFile(bin, name string, body io.Reader) (bool, error) {
	req, err := http.NewRequest(http.MethodPut, c.fileURL(bin, name), body)
	if err != nil {
		return false, err
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	if c.password != "" {
		// The server takes any user name; the bin hash is the one the README uses.
		req.SetBasicAuth(bin, c.password)
	}
	status, _, err := c.do(req)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusCreated:
		return true, nil
	case http.StatusOK:
		return false, nil
	}
	return false, fmt.Errorf("%s: unexpected status %d", req.URL, status)
}

// do runs one request once and returns a 2xx status and body. Anything else comes
// back as an *apiError carrying the server's own words.
func (c *client) do(req *http.Request) (int, []byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()

	// The server's error bodies are one short line; 4 KiB is generous and keeps a
	// broken server from spending the client's memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("%s %s: read response: %v", req.Method, req.URL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, nil, newAPIError(resp, body)
	}
	return resp.StatusCode, body, nil
}
