package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TrinoExecutor interface {
	Ping(ctx context.Context) bool
	Execute(ctx context.Context, sql string, timeout time.Duration) ([][]any, []string, error)
}

type TrinoClient struct {
	statementURL string
	httpClient   *http.Client
}

type trinoColumn struct {
	Name string `json:"name"`
}

type trinoError struct {
	Message string `json:"message"`
}

type trinoResponse struct {
	Columns []trinoColumn `json:"columns"`
	Data    [][]any        `json:"data"`
	NextURI string         `json:"nextUri"`
	Error   *trinoError    `json:"error"`
}

func NewTrinoClient(baseURL string) *TrinoClient {
	base := strings.TrimRight(baseURL, "/") + "/"
	statementURL, err := url.JoinPath(base, "v1", "statement")
	if err != nil {
		statementURL = base + "v1/statement"
	}
	return &TrinoClient{
		statementURL: statementURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *TrinoClient) Ping(ctx context.Context) bool {
	_, _, err := c.Execute(ctx, "SELECT 1", 5*time.Second)
	return err == nil
}

func (c *TrinoClient) Execute(ctx context.Context, sql string, timeout time.Duration) ([][]any, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.statementURL, strings.NewReader(sql))
	if err != nil {
		return nil, nil, err
	}
	setTrinoHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, nil, fmt.Errorf("trino query failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var body trinoResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, nil, err
	}
	return c.collectPages(ctx, body)
}

func (c *TrinoClient) collectPages(ctx context.Context, body trinoResponse) ([][]any, []string, error) {
	var rows [][]any
	var columns []string

	for {
		if body.Error != nil {
			message := body.Error.Message
			if message == "" {
				message = "unknown error"
			}
			return nil, nil, fmt.Errorf("trino query failed: %s", message)
		}

		rows = append(rows, body.Data...)
		if len(columns) == 0 {
			for _, column := range body.Columns {
				columns = append(columns, column.Name)
			}
		}
		if body.NextURI == "" {
			return rows, columns, nil
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, body.NextURI, nil)
		if err != nil {
			return nil, nil, err
		}
		setTrinoHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			return nil, nil, fmt.Errorf("trino query failed: %s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
		}

		var next trinoResponse
		err = json.NewDecoder(resp.Body).Decode(&next)
		resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		body = next
	}
}

func setTrinoHeaders(req *http.Request) {
	req.Header.Set("X-Trino-User", trinoUser)
	req.Header.Set("X-Trino-Source", "go-trino-log-search")
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if trinoCatalog != "" {
		req.Header.Set("X-Trino-Catalog", trinoCatalog)
	}
	if trinoSchema != "" {
		req.Header.Set("X-Trino-Schema", trinoSchema)
	}
	if trinoPassword != "" {
		req.SetBasicAuth(trinoUser, trinoPassword)
	}
}
