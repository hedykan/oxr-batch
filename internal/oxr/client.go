package oxr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrTooLarge = errors.New("document is too large for the OXR endpoint")
var ErrBusy = errors.New("OXR already has a task in progress")

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Retries    int
}

type ParseOptions struct {
	KeepHeaderFooter bool
}

type ParseResult struct {
	Markdown string
	JSON     json.RawMessage
	Pages    int
	TaskID   string
}

type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		TaskID   string `json:"task_id"`
		Status   string `json:"status"`
		Metadata struct {
			PageNum int `json:"page_num"`
		} `json:"metadata"`
		Result struct {
			Outputs struct {
				Markdown string          `json:"markdown"`
				JSON     json.RawMessage `json:"json"`
			} `json:"outputs"`
		} `json:"result"`
	} `json:"data"`
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 2 * time.Minute,
		},
		Retries: 3,
	}
}

// ParseAsync submits a durable OXR task and polls it to completion. It is the
// preferred path for books, batches, and any request that may wait in a queue.
func (c *Client) ParseAsync(ctx context.Context, path string, opts ParseOptions) (ParseResult, error) {
	var taskID string
	for {
		var err error
		taskID, err = c.submitAsync(ctx, path, opts)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrBusy) {
			return ParseResult{}, err
		}
		select {
		case <-ctx.Done():
			return ParseResult{}, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return ParseResult{}, ctx.Err()
		case <-timeout.C:
			return ParseResult{}, fmt.Errorf("OXR task %s timed out after 30 minutes", taskID)
		case <-ticker.C:
			result, status, err := c.pollTask(ctx, taskID)
			if err != nil {
				return ParseResult{}, err
			}
			switch strings.ToLower(status) {
			case "success":
				return result, nil
			case "failed":
				return ParseResult{}, fmt.Errorf("OXR task %s failed", taskID)
			}
		}
	}
}

func (c *Client) submitAsync(ctx context.Context, path string, opts ParseOptions) (string, error) {
	body, contentType, err := multipartBody(path, c.APIKey, opts)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/parse/async", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "oxr-batch/0.1")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("submit OXR task: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return "", ErrTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			var payload apiResponse
			if json.Unmarshal(responseBody, &payload) == nil && payload.Code == 1133 {
				return "", fmt.Errorf("%w: %s", ErrBusy, payload.Message)
			}
		}
		return "", fmt.Errorf("submit OXR task HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var payload apiResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return "", fmt.Errorf("decode OXR submit response: %w", err)
	}
	if payload.Code != 0 || payload.Data.TaskID == "" {
		return "", fmt.Errorf("submit OXR task code %d: %s", payload.Code, payload.Message)
	}
	return payload.Data.TaskID, nil
}

func (c *Client) pollTask(ctx context.Context, taskID string) (ParseResult, string, error) {
	form := url.Values{"api_key": {c.APIKey}, "task_id": {taskID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/parse/async_check", strings.NewReader(form.Encode()))
	if err != nil {
		return ParseResult{}, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "oxr-batch/0.1")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return ParseResult{}, "", fmt.Errorf("poll OXR task %s: %w", taskID, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return ParseResult{}, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ParseResult{}, "", fmt.Errorf("poll OXR task HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var payload apiResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return ParseResult{}, "", fmt.Errorf("decode OXR task response: %w", err)
	}
	if payload.Code != 0 {
		return ParseResult{}, "", fmt.Errorf("poll OXR task code %d: %s", payload.Code, payload.Message)
	}
	result := ParseResult{Markdown: payload.Data.Result.Outputs.Markdown, JSON: payload.Data.Result.Outputs.JSON, Pages: payload.Data.Metadata.PageNum, TaskID: taskID}
	return result, payload.Data.Status, nil
}

func (c *Client) Parse(ctx context.Context, path string, opts ParseOptions) (ParseResult, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		result, retry, err := c.parseOnce(ctx, path, opts)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retry || attempt == c.Retries {
			break
		}
		select {
		case <-ctx.Done():
			return ParseResult{}, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	return ParseResult{}, lastErr
}

func (c *Client) parseOnce(ctx context.Context, path string, opts ParseOptions) (ParseResult, bool, error) {
	body, contentType, err := multipartBody(path, c.APIKey, opts)
	if err != nil {
		return ParseResult{}, false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/parse/sync", body)
	if err != nil {
		return ParseResult{}, false, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "oxr-batch/0.1")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return ParseResult{}, true, fmt.Errorf("request OXR: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return ParseResult{}, true, err
	}
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return ParseResult{}, false, ErrTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return ParseResult{}, retry, fmt.Errorf("OXR HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var payload apiResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return ParseResult{}, false, fmt.Errorf("decode OXR response: %w", err)
	}
	if payload.Code != 0 {
		retry := payload.Code == 1101 || payload.Code == 1124
		return ParseResult{}, retry, fmt.Errorf("OXR code %d: %s", payload.Code, payload.Message)
	}
	return ParseResult{
		Markdown: payload.Data.Result.Outputs.Markdown,
		JSON:     payload.Data.Result.Outputs.JSON,
		Pages:    payload.Data.Metadata.PageNum,
		TaskID:   payload.Data.TaskID,
	}, false, nil
}

func multipartBody(path, apiKey string, opts ParseOptions) (*bytes.Buffer, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", err
	}
	_ = writer.WriteField("api_key", apiKey)
	_ = writer.WriteField("output_formats", "markdown")
	_ = writer.WriteField("output_formats", "json")
	featureConfig, _ := json.Marshal(map[string]bool{"keep_header_footer": opts.KeepHeaderFooter})
	_ = writer.WriteField("feature_config", string(featureConfig))
	_ = writer.WriteField("element_formats", `{"image":"url","formula":"latex","table":"markdown"}`)
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &body, writer.FormDataContentType(), nil
}
