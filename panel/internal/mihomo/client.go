package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"webclash/internal/util"
)

// ClashError is an upstream mihomo API error (port of ClashError). The panel
// error handler maps every ClashError to HTTP 502 regardless of StatusCode.
type ClashError struct {
	Message    string
	StatusCode int
}

func (e *ClashError) Error() string { return e.Message }

// Client talks to the mihomo external-controller (Clash RESTful API).
type Client struct {
	port   int
	secret string
	http   *http.Client
}

// NewClient builds a controller client for the given port/secret.
func NewClient(port int, secret string) *Client {
	return &Client{port: port, secret: secret, http: &http.Client{}}
}

func (c *Client) baseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", c.port) }

// WSBaseURL is the controller WebSocket origin.
func (c *Client) WSBaseURL() string { return fmt.Sprintf("ws://127.0.0.1:%d", c.port) }

// WSURL appends the secret token query param (port of wsUrl).
func (c *Client) WSURL(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return c.WSBaseURL() + path + sep + "token=" + util.EncodeURIComponent(c.secret)
}

// Request performs a controller API call. A nil body sends no payload; timeoutMs
// bounds the whole request. Returns parsed JSON (any), raw text, or nil for
// 204/empty bodies (port of ClashClient.request).
func (c *Client) Request(method, path string, body any, timeoutMs int) (any, error) {
	raw, err := c.do(method, path, body, timeoutMs)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		return v, nil
	}
	return string(raw), nil
}

// RequestInto performs a controller API call and decodes a successful JSON body
// straight into out, avoiding the any round-trip (and its extra copy) for large
// high-frequency payloads such as /connections. Error semantics match Request.
func (c *Client) RequestInto(method, path string, body any, timeoutMs int, out any) error {
	raw, err := c.do(method, path, body, timeoutMs)
	if err != nil || len(raw) == 0 {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &ClashError{Message: "无法解析内核响应：" + err.Error(), StatusCode: 502}
	}
	return nil
}

// do executes one controller call and returns the raw 2xx body (nil for
// 204/empty), mapping transport and non-2xx responses onto ClashError.
func (c *Client) do(method, path string, body any, timeoutMs int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, &ClashError{Message: "无法连接 mihomo 内核：" + err.Error(), StatusCode: 502}
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL()+path, reqBody)
	if err != nil {
		return nil, &ClashError{Message: "无法连接 mihomo 内核：" + err.Error(), StatusCode: 502}
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, &ClashError{Message: "无法连接 mihomo 内核：" + err.Error(), StatusCode: 502}
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, &ClashError{Message: "无法连接 mihomo 内核：" + err.Error(), StatusCode: 502}
	}
	text := string(raw)

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		message := text
		var parsed struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &parsed) == nil && parsed.Message != "" {
			message = parsed.Message
		}
		if message == "" {
			message = fmt.Sprintf("内核接口返回 HTTP %d", res.StatusCode)
		}
		return nil, &ClashError{Message: message, StatusCode: res.StatusCode}
	}

	if text == "" {
		return nil, nil
	}
	return raw, nil
}
