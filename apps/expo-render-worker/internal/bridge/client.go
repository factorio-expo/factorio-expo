package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

type Commander interface {
	Execute(context.Context, string) (string, error)
}

type Client struct {
	rcon Commander
}

type Status struct {
	Status          string     `json:"status"`
	ProtocolVersion int        `json:"protocol_version"`
	Capture         Capture    `json:"capture"`
	ActiveJob       *ActiveJob `json:"active_job"`
	CompletedJob    *Accepted  `json:"completed_job,omitempty"`
}

type Capture struct {
	Connected bool   `json:"connected"`
	Player    string `json:"player,omitempty"`
}

type ActiveJob struct {
	JobID           string       `json:"job_id"`
	Phase           string       `json:"phase"`
	RequestedPhases []jobs.Phase `json:"requested_phases,omitempty"`
	Phases          PhaseStates  `json:"phases"`
	Render          *Render      `json:"render,omitempty"`
}

type PhaseStates struct {
	Preview  string `json:"preview,omitempty"`
	Analysis string `json:"analysis,omitempty"`
}

type Submission struct {
	JobID     string       `json:"job_id"`
	Blueprint string       `json:"blueprint"`
	Phases    []jobs.Phase `json:"phases,omitempty"`
}

type uploadBegin struct {
	JobID      string `json:"job_id"`
	ChunkCount int    `json:"chunk_count"`
}

type uploadChunk struct {
	JobID string `json:"job_id"`
	Index int    `json:"index"`
	Data  string `json:"data"`
}

type jobReference struct {
	JobID  string       `json:"job_id"`
	Phases []jobs.Phase `json:"phases,omitempty"`
}

type actionResponse struct {
	Status          string       `json:"status"`
	JobID           string       `json:"job_id"`
	RequestedPhases []jobs.Phase `json:"requested_phases,omitempty"`
	ErrorCode       string       `json:"error_code,omitempty"`
	Error           string       `json:"error,omitempty"`
}

type Accepted struct {
	Status          string        `json:"status"`
	JobID           string        `json:"job_id"`
	RequestedPhases []jobs.Phase  `json:"requested_phases"`
	Paths           AcceptedPaths `json:"paths"`
	Render          *Render       `json:"render,omitempty"`
	ErrorCode       string        `json:"error_code,omitempty"`
	Error           string        `json:"error,omitempty"`
}

type AcceptedPaths struct {
	PreviewImage   string `json:"preview_image,omitempty"`
	PreviewResult  string `json:"preview_result,omitempty"`
	AnalysisResult string `json:"analysis_result,omitempty"`
}

type Render struct {
	Resolution    Resolution      `json:"resolution"`
	Zoom          float64         `json:"zoom"`
	BoundingBox   json.RawMessage `json:"bounding_box"`
	SpacePlatform bool            `json:"space_platform"`
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("bridge rejected request (%s): %s", e.Code, e.Message)
}

type Resolution struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func NewClient(rcon Commander) *Client {
	return &Client{rcon: rcon}
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	response, err := c.rcon.Execute(ctx, CommandStatus)
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := decodeResponse(response, &status); err != nil {
		return Status{}, fmt.Errorf("decode bridge status: %w", err)
	}
	if status.Status != "ok" {
		return Status{}, fmt.Errorf("bridge status is %q", status.Status)
	}
	if err := checkProtocol(status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func (c *Client) Submit(ctx context.Context, jobID, blueprint string, phases []jobs.Phase) (Accepted, error) {
	chunkCount := (len(blueprint) + chunkSize - 1) / chunkSize
	if chunkCount == 0 {
		return Accepted{}, fmt.Errorf("blueprint must not be empty")
	}
	if chunkCount > maxUploadChunk {
		return Accepted{}, &Error{Code: "blueprint_too_large", Message: "blueprint exceeds bridge upload limit"}
	}
	if err := c.executeAction(ctx, CommandBegin, uploadBegin{
		JobID:      jobID,
		ChunkCount: chunkCount,
	}, jobID); err != nil {
		return Accepted{}, err
	}

	for index, offset := 1, 0; offset < len(blueprint); index, offset = index+1, offset+chunkSize {
		end := min(offset+chunkSize, len(blueprint))
		if err := c.executeAction(ctx, CommandChunk, uploadChunk{
			JobID: jobID, Index: index, Data: blueprint[offset:end],
		}, jobID); err != nil {
			return Accepted{}, err
		}
	}

	payload, err := json.Marshal(jobReference{JobID: jobID, Phases: phases})
	if err != nil {
		return Accepted{}, fmt.Errorf("encode bridge start request: %w", err)
	}
	response, err := c.rcon.Execute(ctx, CommandStart+" "+string(payload))
	if err != nil {
		return Accepted{}, err
	}
	var accepted Accepted
	if err := decodeResponse(response, &accepted); err != nil {
		return Accepted{}, fmt.Errorf("decode bridge response: %w", err)
	}
	if accepted.Status == "error" {
		return accepted, &Error{Code: accepted.ErrorCode, Message: accepted.Error}
	}
	if accepted.Status != "accepted" || accepted.JobID != jobID {
		return Accepted{}, fmt.Errorf("unexpected bridge response")
	}
	if !samePhases(accepted.RequestedPhases, phases) {
		return Accepted{}, fmt.Errorf("bridge returned unexpected requested phases")
	}
	if hasPhase(phases, jobs.PhasePreview) &&
		(accepted.Paths.PreviewImage != RelativePath(jobID, PreviewImageFile) ||
			accepted.Paths.PreviewResult != RelativePath(jobID, PreviewResultFile)) {
		return Accepted{}, fmt.Errorf("bridge returned unexpected artifact paths")
	}
	if hasPhase(phases, jobs.PhaseAnalysis) &&
		accepted.Paths.AnalysisResult != RelativePath(jobID, AnalysisResultFile) {
		return Accepted{}, fmt.Errorf("bridge returned unexpected artifact paths")
	}
	if accepted.Render != nil &&
		(accepted.Render.Resolution.X < 1 || accepted.Render.Resolution.Y < 1 || accepted.Render.Zoom <= 0) {
		return Accepted{}, fmt.Errorf("bridge returned invalid render metadata")
	}
	return accepted, nil
}

func hasPhase(phases []jobs.Phase, wanted jobs.Phase) bool {
	return slices.Contains(phases, wanted)
}

func samePhases(got, want []jobs.Phase) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func (c *Client) executeAction(ctx context.Context, command string, payload any, jobID string) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode bridge upload request: %w", err)
	}
	response, err := c.rcon.Execute(ctx, command+" "+string(encoded))
	if err != nil {
		return err
	}
	var action actionResponse
	if err := decodeResponse(response, &action); err != nil {
		return fmt.Errorf("decode bridge upload response: %w", err)
	}
	if action.Status != "accepted" || action.JobID != jobID {
		if action.Error != "" {
			return &Error{Code: action.ErrorCode, Message: action.Error}
		}
		return fmt.Errorf("unexpected bridge upload response")
	}
	return nil
}

// decodeResponse decodes the bridge's reply, the last line of an RCON
// response. Factorio puts the console messages logged since the previous
// command (such as "[JOIN]" lines) in front of a command's output; the bridge's
// JSON never contains a raw newline.
func decodeResponse(response string, target any) error {
	reply := strings.TrimSpace(response)
	if newline := strings.LastIndexByte(reply, '\n'); newline >= 0 {
		reply = strings.TrimSpace(reply[newline+1:])
	}
	decoder := json.NewDecoder(strings.NewReader(reply))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("response must contain one JSON value")
	}
	return nil
}
