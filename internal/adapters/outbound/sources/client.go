// Package sources holds the outbound HTTP clients of the bootstrap rebuild
// (ADR 0003): the paged snapshot reads of offer-availability, pricing and
// product-content. They are used by cmd/rebuild only, never on a user request.
// Each client calls exactly the routes listed in docs/network-ohs-allowlist.yaml
// (enforced by internal/architecture's TestNoInOrgNetworkRoute).
package sources

import (
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

// Attempts is how often a read is tried before the rebuild fails; transport
// errors and 5xx answers are retried with a short growing pause.
const Attempts = 4

// ErrUnexpectedStatus is returned for a non-200 answer that is not retried.
var ErrUnexpectedStatus = errors.New("unexpected HTTP status")

// Client reads JSON from one upstream service.
type Client struct {
	// BaseURL is the service root, without a trailing slash.
	BaseURL string
	// HTTP is the transport; nil means a client with a 30 s timeout.
	HTTP *http.Client
	// Pause is the base retry pause (default 200 ms); tests shrink it.
	Pause time.Duration
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// get fetches path with the query and decodes the JSON body into out.
func (c Client) get(ctx context.Context, path string, query url.Values, out any) error {
	target := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	pause := c.Pause
	if pause <= 0 {
		pause = 200 * time.Millisecond
	}
	var err error
	for attempt := 1; attempt <= Attempts; attempt++ {
		var retry bool
		if retry, err = c.once(ctx, target, out); err == nil || !retry {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(pause * time.Duration(attempt)):
		}
	}
	return err
}

// once performs one request; retry says whether a failure is worth repeating.
func (c Client) once(ctx context.Context, target string, out any) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return true, fmt.Errorf("GET %s: %w", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 500 {
		return true, fmt.Errorf("GET %s: %w: %d", target, ErrUnexpectedStatus, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET %s: %w: %d", target, ErrUnexpectedStatus, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return true, fmt.Errorf("GET %s: read body: %w", target, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("GET %s: decode body: %w", target, err)
	}
	return false, nil
}

func pageQuery(after string, limit int) url.Values {
	q := url.Values{}
	if after != "" {
		q.Set("cursor", after)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}
