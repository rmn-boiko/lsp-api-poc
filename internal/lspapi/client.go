package lspapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http status %d: %s", e.StatusCode, e.Body)
}

type NodeClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewNodeClient(baseURL, token string, timeoutSeconds int64) *NodeClient {
	return &NodeClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: timeDurationSeconds(timeoutSeconds)},
	}
}

func (c *NodeClient) DoJSON(ctx context.Context, method, path string, reqBody any, respBody any) error {
	url := c.baseURL + path
	var bodyReader io.Reader
	if reqBody != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
			return err
		}
		bodyReader = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &HTTPError{StatusCode: res.StatusCode, Body: strings.TrimSpace(string(data))}
	}

	if respBody == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, respBody); err != nil {
		return fmt.Errorf("invalid json response from %s: %w", path, err)
	}
	return nil
}

func timeDurationSeconds(seconds int64) time.Duration {
	if seconds <= 0 {
		seconds = 15
	}
	return time.Duration(seconds) * time.Second
}
