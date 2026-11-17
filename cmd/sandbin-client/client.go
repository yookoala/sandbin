package main

import (
	"bytes"
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

// apiKeyHeader is the one custom header the JSON API still reads; the bin metadata,
// the bin password and the extraction opt-out all live in the body, the URL or Basic
// Auth now.
const apiKeyHeader = "X-API-Key"

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
	http      *http.Client
	base      *url.URL // the server's base URL, with no trailing slash
	apiKey    string
	password  string // empty leaves the bin open
	noExtract bool   // --no-extract: every PUT of the run carries the ?raw=1 opt-out query
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

// binJSON is the bin object every JSON answer about a bin carries. inspectBin's answer
// adds the files array; a create, patch or list answer holds no files.
type binJSON struct {
	Bin         string     `json:"bin"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	HasPassword bool       `json:"has_password"`
	CreatedAt   string     `json:"created_at"`
	Files       []fileInfo `json:"files"`
}

// fileInfo is one file an inspect answer lists.
type fileInfo struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updated_at"`
}

// binInfo is one bin as the API inspects it: its hash, its metadata, when the server
// made it, and the files it holds.
type binInfo struct {
	Hash        string
	Title       string
	Description string
	CreatedAt   string
	HasPassword bool
	Files       []fileInfo
}

func (j binJSON) info() binInfo {
	return binInfo{
		Hash:        j.Bin,
		Title:       j.Title,
		Description: j.Description,
		CreatedAt:   j.CreatedAt,
		HasPassword: j.HasPassword,
		Files:       j.Files,
	}
}

// createBin POSTs /api/bins with a JSON body carrying only the fields worth sending:
// the title and description when they are not empty, and the bin password from
// c.password when one is set. It returns the new bin's hash, read out of the bin object
// the server answers with. No Basic Auth here: the bin password is being set, not
// proven. A client with nothing to say sends the empty object, which the server reads
// as a bin with nothing to say.
func (c *client) createBin(title, description string) (string, error) {
	fields := map[string]string{}
	if title != "" {
		fields["title"] = title
	}
	if description != "" {
		fields["description"] = description
	}
	if c.password != "" {
		fields["password"] = c.password
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.base.String()+"/api/bins", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	_, _, answer, err := c.do(req)
	if err != nil {
		return "", err
	}
	var created binJSON
	if err := json.Unmarshal(answer, &created); err != nil {
		return "", fmt.Errorf("server did not answer a bin object: %q", strings.TrimSpace(string(answer)))
	}
	if created.Bin == "" {
		return "", fmt.Errorf("server did not answer a bin hash: %q", strings.TrimSpace(string(answer)))
	}
	return created.Bin, nil
}

// putFile PUTs one body under a bin and reports whether the server created the file
// (201) or replaced it (200), plus the extracted count an archive answer carries: the
// {"created","replaced"} counts of the files the server unpacked. A plain file answer
// is the stored row and has no counts. --no-extract sends the ?raw=1 opt-out query on
// every PUT.
func (c *client) putFile(bin, name string, body io.Reader) (bool, int, error) {
	req, err := http.NewRequest(http.MethodPut, c.fileURL(bin, name), body)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	if c.password != "" {
		// The server takes any user name; the bin hash is the one the README uses.
		req.SetBasicAuth(bin, c.password)
	}
	if c.noExtract {
		req.URL.RawQuery = "raw=1"
	}
	status, _, answer, err := c.do(req)
	if err != nil {
		return false, 0, err
	}
	// The same decode reads the counts out of an archive answer and finds none in a
	// file answer's {"path","size","updated_at"} row.
	// shortcut: a body this client cannot read keeps its count at 0; the status decides the line.
	var counts struct {
		Created  *int `json:"created"`
		Replaced *int `json:"replaced"`
	}
	extracted := 0
	if err := json.Unmarshal(answer, &counts); err == nil {
		if counts.Created != nil {
			extracted += *counts.Created
		}
		if counts.Replaced != nil {
			extracted += *counts.Replaced
		}
	}
	switch status {
	case http.StatusCreated:
		return true, extracted, nil
	case http.StatusOK:
		return false, extracted, nil
	}
	return false, 0, fmt.Errorf("%s: unexpected status %d", req.URL, status)
}

// deletePaths DELETEs one path under a bin and returns the paths the server says it removed.
// The folder form carries the trailing slash the server reads a folder by, and it is sent on
// exactly one request: a folder's contents are the server's to list, not this client's.
func (c *client) deletePaths(bin, name string, folder bool) ([]string, error) {
	target := c.fileURL(bin, name)
	if folder {
		target += "/"
	}
	req, err := http.NewRequest(http.MethodDelete, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	if c.password != "" {
		// The server takes any user name; the bin hash is the one the README uses.
		req.SetBasicAuth(bin, c.password)
	}
	status, _, body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %d", req.URL, status)
	}
	var answer struct {
		Deleted []string `json:"deleted"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("%s: server answered %q", req.URL, strings.TrimSpace(string(body)))
	}
	if len(answer.Deleted) == 0 {
		return nil, fmt.Errorf("%s: the server removed nothing", req.URL)
	}
	return answer.Deleted, nil
}

// patchBinMeta PATCHes /api/bins/{bin} with a JSON body carrying only the fields the
// run named: a nil value says nothing about that field, an empty one clears it. The
// answer is the bin's row as it now stands, which this client does not read: all it
// needs is that the write took.
func (c *client) patchBinMeta(bin string, title, description *string) error {
	fields := map[string]string{}
	if title != nil {
		fields["title"] = *title
	}
	if description != nil {
		fields["description"] = *description
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPatch, c.base.String()+"/api/bins/"+bin, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	if c.password != "" {
		req.SetBasicAuth(bin, c.password)
	}
	status, _, _, err := c.do(req)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %d", req.URL, status)
	}
	return nil
}

// inspectBin GETs /api/bins/{bin} once and returns the full inspection the server
// answers: the bin's metadata and every file it holds, sorted by path. No API key goes
// out, because reading one bin is what a GET has always been; a protected bin answers
// 401 until the bin password arrives as Basic Auth.
func (c *client) inspectBin(bin string) (binInfo, error) {
	req, err := http.NewRequest(http.MethodGet, c.base.String()+"/api/bins/"+bin, nil)
	if err != nil {
		return binInfo{}, err
	}
	if c.password != "" {
		req.SetBasicAuth(bin, c.password)
	}
	_, _, body, err := c.do(req)
	if err != nil {
		return binInfo{}, err
	}
	var answer binJSON
	if err := json.Unmarshal(body, &answer); err != nil {
		return binInfo{}, fmt.Errorf("%s: server answered %q", req.URL, strings.TrimSpace(string(body)))
	}
	return answer.info(), nil
}

// listBins GETs /api/bins and returns every bin the server holds, newest first: each
// page answers the next cursor when a further bin exists, and the walk ends at the
// page that does not. The API key rides on every page, the same one do sends for a
// write.
func (c *client) listBins() ([]binInfo, error) {
	var all []binInfo
	cursor := ""
	for {
		u := *c.base
		u.Path = c.base.Path + "/api/bins"
		if cursor != "" {
			u.RawQuery = url.Values{"cursor": []string{cursor}}.Encode()
		}
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set(apiKeyHeader, c.apiKey)
		_, _, body, err := c.do(req)
		if err != nil {
			return nil, err
		}
		var page struct {
			Bins       []binJSON `json:"bins"`
			NextCursor string    `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("%s: server answered %q", req.URL, strings.TrimSpace(string(body)))
		}
		for _, b := range page.Bins {
			all = append(all, b.info())
		}
		if page.NextCursor == "" {
			return all, nil
		}
		cursor = page.NextCursor
	}
}

// verifyKey POSTs /api/key with the API key and asks only "is this key ok?". A 204 with
// an empty body is the ok, a 403 is the wrong key, and a 429 is this address already
// locked out by bad keys. One attempt, no retry: a retry would only deepen the lockout.
func (c *client) verifyKey() error {
	req, err := http.NewRequest(http.MethodPost, c.base.String()+"/api/key", nil)
	if err != nil {
		return err
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %v", c.base, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return fmt.Errorf("cannot reach %s: read response: %v", c.base, err)
	}
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusForbidden:
		return errors.New("server rejected this API key")
	case http.StatusTooManyRequests:
		return errors.New("server is blocking this address: too many bad API keys")
	}
	return fmt.Errorf("checking the API key: %v", newAPIError(resp, body))
}

// do runs one request once and returns a 2xx status, headers and body. Anything
// else comes back as an *apiError carrying the server's own words.
func (c *client) do(req *http.Request) (int, http.Header, []byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()

	// The server's error bodies are one short line; 4 KiB is generous and keeps a
	// broken server from spending the client's memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return resp.StatusCode, nil, nil, fmt.Errorf("%s %s: read response: %v", req.Method, req.URL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, nil, nil, newAPIError(resp, body)
	}
	return resp.StatusCode, resp.Header, body, nil
}
