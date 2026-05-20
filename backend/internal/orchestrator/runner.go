package orchestrator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"mini-paperclip/backend/internal/security"
	"mini-paperclip/backend/internal/store"
)

type Runner interface {
	Kind() string
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

type RunRequest struct {
	Prompt       string
	SystemPrompt string
	WorktreePath string
	Profile      string
	SessionID    string
	Model        string
	Timeout      time.Duration
	MaxCostUSD   float64
	OnEvent      func(level, msg string)
}

type RunResult struct {
	Stdout    string
	Parsed    store.AgentResponse
	TokensIn  int
	TokensOut int
	CostUSD   float64
	ExitCode  int
	SessionID *string
}

type ClaudeRunner struct{}

func (ClaudeRunner) Kind() string { return "claude" }

func (ClaudeRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	args := []string{"--print", "--dangerously-skip-permissions", "--output-format", "stream-json", "--append-system-prompt", req.SystemPrompt}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	args = append(args, req.Prompt)
	return runCommand(ctx, "claude", args, req)
}

type MockRunner struct{}

func (MockRunner) Kind() string { return "mock" }

func (MockRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if req.OnEvent != nil {
		req.OnEvent("info", "mock runner generated deterministic acceptance response")
	}
	if strings.Contains(req.Prompt, "INVALID_JSON_SMOKE") {
		return RunResult{Stdout: "{invalid-json", ExitCode: 0}, errors.New("invalid agent JSON response")
	}
	parsed := store.AgentResponse{
		TaskUpdates: store.TaskUpdates{
			Status:  "done",
			Comment: "Mock plan: acceptance criteria captured and implementation subtasks created.",
			CreateSubtasks: []store.Subtask{
				{Title: "Implement backend slice", Description: "Implement the backend portion from the parent task.", AssigneeAgentID: stringPtr("backend"), InitialComment: "Created by mock PM flow."},
				{Title: "Verify implementation", Description: "Verify the delivered behavior and record evidence.", AssigneeAgentID: stringPtr("qa"), InitialComment: "Created by mock PM flow."},
			},
		},
	}
	body, _ := json.Marshal(parsed)
	return RunResult{Stdout: string(body), Parsed: parsed, ExitCode: 0}, nil
}

type CodexRunner struct{}

func (CodexRunner) Kind() string { return "codex" }

func (CodexRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if req.SessionID != "" {
		args := []string{"exec", "resume", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"}
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		args = append(args, req.SessionID, "System instructions:\n"+req.SystemPrompt+"\n\nTask prompt:\n"+req.Prompt)
		return runCommand(ctx, "codex", args, req)
	}
	args := []string{"exec", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.WorktreePath != "" {
		args = append(args, "--cd", req.WorktreePath)
	}
	if req.Profile != "" {
		args = append(args, "--profile", req.Profile)
	}
	args = append(args, "System instructions:\n"+req.SystemPrompt+"\n\nTask prompt:\n"+req.Prompt)
	return runCommand(ctx, "codex", args, req)
}

func runCommand(ctx context.Context, binary string, args []string, req RunRequest) (RunResult, error) {
	runCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	runCtx, kill := context.WithCancel(runCtx)
	defer cancel()
	defer kill()
	cmd := exec.CommandContext(runCtx, binary, args...)
	if req.WorktreePath != "" {
		cmd.Dir = req.WorktreePath
	}
	if req.OnEvent != nil {
		req.OnEvent("info", "runner command: "+runnerCommandMetadata(binary, args))
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return RunResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return RunResult{ExitCode: 127}, err
	}
	var out bytes.Buffer
	metrics := &runMetrics{}
	blocked := make(chan string, 1)
	done := make(chan struct{}, 2)
	go stream(stdout, &out, "stdout", req, metrics, kill, blocked, done)
	go stream(stderr, nil, "stderr", req, metrics, kill, blocked, done)
	<-done
	<-done
	err = cmd.Wait()
	exit := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
		} else {
			exit = 1
		}
	}
	result := RunResult{Stdout: out.String(), ExitCode: exit, TokensIn: metrics.tokensIn, TokensOut: metrics.tokensOut, CostUSD: metrics.costUSD, SessionID: metrics.sessionID}
	select {
	case reason := <-blocked:
		return result, errors.New(reason)
	default:
	}
	if parsed, parseErr := ParseAgentResponse(result.Stdout); parseErr == nil {
		result.Parsed = parsed
	} else if err == nil {
		err = parseErr
	}
	return result, err
}

func stream(r io.Reader, out *bytes.Buffer, level string, req RunRequest, metrics *runMetrics, kill context.CancelFunc, blocked chan<- string, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if out != nil {
			out.WriteString(line)
			out.WriteByte('\n')
		}
		if req.OnEvent != nil && strings.TrimSpace(line) != "" {
			req.OnEvent(level, security.RedactSensitive(line))
		}
		if reason := blockedOutputReason(line); reason != "" {
			select {
			case blocked <- reason:
			default:
			}
			kill()
			return
		}
		metrics.observe(line)
		if req.MaxCostUSD > 0 && metrics.cost() > req.MaxCostUSD {
			select {
			case blocked <- fmt.Sprintf("run cost ceiling exceeded: %.6f > %.6f", metrics.cost(), req.MaxCostUSD):
			default:
			}
			kill()
			return
		}
	}
}

func runnerCommandMetadata(binary string, args []string) string {
	safe := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--append-system-prompt":
			safe = append(safe, arg, "[system prompt omitted]")
			i++
		default:
			if strings.HasPrefix(arg, "System instructions:\n") {
				safe = append(safe, "[prompt omitted]")
				continue
			}
			safe = append(safe, arg)
		}
	}
	return binary + " " + strings.Join(safe, " ")
}

func blockedOutputReason(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "{") {
		var event any
		if json.Unmarshal([]byte(trimmed), &event) == nil {
			for _, command := range jsonCommandCandidates(event) {
				if reason := blockedTextReason(command); reason != "" {
					return reason
				}
			}
			return ""
		}
	}
	return blockedTextReason(line)
}

func blockedTextReason(text string) string {
	lower := strings.ToLower(text)
	blocked := []string{"curl ", "wget ", "curl|", "wget|", "python3 -c", "python -c", "perl -e", "docker.sock", "sudo "}
	for _, pattern := range blocked {
		if strings.Contains(lower, pattern) {
			return "runner output matched blocked shell pattern: " + pattern
		}
	}
	longRunning := []string{"npm run dev", "vite --host"}
	for _, pattern := range longRunning {
		if strings.Contains(lower, pattern) {
			return "runner output matched long-running command pattern: " + pattern
		}
	}
	return ""
}

func jsonCommandCandidates(value any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch item := v.(type) {
		case []any:
			for _, child := range item {
				walk(child)
			}
		case map[string]any:
			for key, child := range item {
				if key == "command" {
					if command, ok := child.(string); ok {
						out = append(out, command)
					}
					continue
				}
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

func ParseAgentResponse(output string) (store.AgentResponse, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var direct store.AgentResponse
		if json.Unmarshal([]byte(line), &direct) == nil && direct.TaskUpdates.Comment != "" {
			return direct, nil
		}
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) == nil {
			for _, text := range jsonTextCandidates(event) {
				if parsed, err := ParseAgentResponse(text); err == nil {
					return parsed, nil
				}
			}
		}
	}
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start >= 0 && end > start {
		var parsed store.AgentResponse
		if err := json.Unmarshal([]byte(output[start:end+1]), &parsed); err == nil && parsed.TaskUpdates.Comment != "" {
			return parsed, nil
		}
	}
	return store.AgentResponse{}, errors.New("agent response did not contain valid task_updates JSON")
}

func jsonTextCandidates(value any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch item := v.(type) {
		case string:
			if strings.Contains(item, "task_updates") {
				out = append(out, item)
			}
		case []any:
			for _, child := range item {
				walk(child)
			}
		case map[string]any:
			for _, child := range item {
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

func stringPtr(value string) *string {
	return &value
}
