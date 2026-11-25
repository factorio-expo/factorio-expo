package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"factorio-expo/internal/runnerapi"
)

// Client is the API's typed client for the runner HTTP API; request and
// response bodies are the shared runnerapi contract.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// artifactPathPrefix bounds which runner paths the API is willing to fetch.
const artifactPathPrefix = runnerapi.JobsPath + "/"

type Error struct {
	Status int
	Code   string
	Title  string
	Detail string
}

func (e *Error) Error() string {
	return fmt.Sprintf("runner returned %d: %s", e.Status, e.Title)
}

// NewClient builds a runner client for baseURL, applying timeout to every
// request.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Health is the runner's liveness answer.
type Health struct {
	Service string `json:"service" doc:"The runner service answering."`
	Status  string `json:"status" doc:"ok while the runner is up."`
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var response Health
	if err := c.getJSON(ctx, "/healthz", &response); err != nil {
		return Health{}, err
	}
	return response, nil
}

// Readiness reads the runner's readiness probe. A not-ready runner answers
// 503 with the same body, so both statuses are a result; only an unreachable
// runner or an unreadable body is an error.
func (c *Client) Readiness(ctx context.Context) (runnerapi.Readiness, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+runnerapi.ReadyPath, nil)
	if err != nil {
		return runnerapi.Readiness{}, fmt.Errorf("build runner request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return runnerapi.Readiness{}, fmt.Errorf("call runner: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusServiceUnavailable {
		return runnerapi.Readiness{}, decodeError(response)
	}
	var readiness runnerapi.Readiness
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&readiness); err != nil {
		return runnerapi.Readiness{}, fmt.Errorf("decode runner readiness: %w", err)
	}
	return readiness, nil
}

// CreatePreviewJob enqueues one leaf blueprint for rendering and returns the
// accepted job, whose id is the handle for later polling. A non-empty
// idempotencyKey is sent as the Idempotency-Key header so that a repeated
// request for the same dispatch attempt can be answered with the same job.
func (c *Client) CreatePreviewJob(
	ctx context.Context, blueprint string, phases []runnerapi.Phase, idempotencyKey string,
) (runnerapi.Job, error) {
	body, err := json.Marshal(runnerapi.CreateJobRequest{Blueprint: blueprint, Phases: phases})
	if err != nil {
		return runnerapi.Job{}, fmt.Errorf("encode runner request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+runnerapi.JobsPath, bytes.NewReader(body),
	)
	if err != nil {
		return runnerapi.Job{}, fmt.Errorf("build runner request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		request.Header.Set(runnerapi.IdempotencyKeyHeader, idempotencyKey)
	}
	var response runnerapi.Job
	if err := c.doJSON(request, &response); err != nil {
		return runnerapi.Job{}, err
	}
	return response, nil
}

// DownloadAnalysis streams the JSON analysis artifact for a completed job. The
// caller must close the returned body.
func (c *Client) DownloadAnalysis(ctx context.Context, id string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, c.baseURL+runnerapi.AnalysisPath(id), nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build analysis request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download runner analysis: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		return nil, decodeError(response)
	}
	return response, nil
}

func (c *Client) GetPreviewJob(ctx context.Context, id string) (runnerapi.Job, error) {
	var response runnerapi.Job
	if err := c.getJSON(ctx, runnerapi.JobPath(id), &response); err != nil {
		return runnerapi.Job{}, err
	}
	return response, nil
}

// FetchArtifact streams a stored artifact href (the import downloads the
// preview PNG with it). Only runner artifact paths are
// accepted, so a stored value can never be used to make the API fetch an
// arbitrary internal URL. The caller must close the returned body.
func (c *Client) FetchArtifact(ctx context.Context, path string) (*http.Response, error) {
	if !strings.HasPrefix(path, artifactPathPrefix) || strings.Contains(path, "..") {
		return nil, fmt.Errorf("refusing to fetch non-artifact path %q", path)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build artifact request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch runner artifact: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		return nil, decodeError(response)
	}
	return response, nil
}

// DeleteAnalysisArtifact releases the runner's temporary analysis JSON after
// the API has stored its durable production snapshot.
func (c *Client) DeleteAnalysisArtifact(ctx context.Context, id string) error {
	return c.delete(ctx, runnerapi.AnalysisPath(id), "analysis")
}

// DeletePreviewArtifact releases the runner's temporary preview PNG after the
// API has stored its own files.
func (c *Client) DeletePreviewArtifact(ctx context.Context, id string) error {
	return c.delete(ctx, runnerapi.ArtifactPath(id), "preview")
}

func (c *Client) delete(ctx context.Context, path, what string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build %s deletion request: %w", what, err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("delete runner %s: %w", what, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return decodeError(response)
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build runner request: %w", err)
	}
	return c.doJSON(request, target)
}

// doJSON performs a request and decodes a JSON response, turning any non-2xx
// status into a typed *Error so callers can branch on the runner's status.
func (c *Client) doJSON(request *http.Request, target any) error {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call runner: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeError(response)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode runner response: %w", err)
	}
	return nil
}

// decodeError converts a runner problem response into a typed error, falling
// back to the HTTP status text when the body is not the expected shape.
func decodeError(response *http.Response) error {
	var payload runnerapi.Problem
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		payload.Title = http.StatusText(response.StatusCode)
	}
	return &Error{
		Status: response.StatusCode, Code: payload.Code,
		Title: payload.Title, Detail: payload.Detail,
	}
}
