package claim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// LocalPrefix names a model served on this machine, through the
// OpenAI-compatible chat API that Ollama, llama.cpp and LM Studio serve:
// "local:qwen2.5:14b". Vibium's local provider, carried over. It needs no
// credential and costs nothing, and its judgment is the model's — a small
// one is measured against the claim set before it is trusted.
const LocalPrefix = "local:"

// DefaultLocalURL is Ollama's OpenAI-compatible endpoint; MOBIUM_AI_BASE_URL
// names another.
const DefaultLocalURL = "http://127.0.0.1:11434/v1"

// Local is a Session on a local model.
type Local struct {
	Model    string
	base     string
	http     *http.Client
	messages []map[string]interface{}
	tools    []map[string]interface{}
	nudges   int
}

// maxNudges is how many times a model that answers in prose is asked again
// for its verdict: mistral-small read one call and then wrote its answer as
// text on every claim, which the loop can only take as no verdict.
const maxNudges = 2

// NewLocal is a session on model, served at MOBIUM_AI_BASE_URL or Ollama's.
func NewLocal(model string) *Local {
	base := strings.TrimRight(os.Getenv("MOBIUM_AI_BASE_URL"), "/")
	if base == "" {
		base = DefaultLocalURL
	}
	return &Local{Model: model, base: base, http: &http.Client{Timeout: 10 * time.Minute}}
}

// Begin starts the conversation.
func (l *Local) Begin(ctx context.Context, system, claim string, tools []Tool) (Step, error) {
	for _, t := range tools {
		l.tools = append(l.tools, map[string]interface{}{"type": "function", "function": map[string]interface{}{
			"name": t.Name, "description": t.Description, "parameters": t.Schema,
		}})
	}
	l.messages = []map[string]interface{}{
		{"role": "system", "content": system},
		{"role": "user", "content": claim},
	}
	return l.send(ctx)
}

// Next hands the model its tools' answers. A screenshot is described, not
// sent: the chat API puts images in user turns only, and a model that
// cannot see is told so rather than handed bytes it cannot read.
func (l *Local) Next(ctx context.Context, results []Result) (Step, error) {
	for _, r := range results {
		text := r.Text
		if r.JPEG != nil {
			text += " (a screenshot was kept, but this model cannot be shown images: judge from the screen's text, " +
				"or answer inconclusive if only the image could settle it)"
		}
		l.messages = append(l.messages, map[string]interface{}{"role": "tool", "tool_call_id": r.CallID, "content": text})
	}
	return l.send(ctx)
}

func (l *Local) send(ctx context.Context) (Step, error) {
	for {
		step, err := l.ask(ctx)
		if err != nil || len(step.Calls) > 0 || l.nudges >= maxNudges {
			return step, err
		}
		l.nudges++
		l.messages = append(l.messages, map[string]interface{}{"role": "user", "content": "Answer through the tools, not in text: " +
			"read more of the recording if you need to, and finish by calling " + verdictTool + " exactly once."})
	}
}

func (l *Local) ask(ctx context.Context) (Step, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": l.Model, "messages": l.messages, "tools": l.tools, "temperature": 0,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Step{}, mobiumerr.New(mobiumerr.InvalidArgument, "the local model's address %s: %w", l.base, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.http.Do(req)
	if err != nil {
		return Step{}, mobiumerr.New(mobiumerr.ToolchainMissing, "no local model answered at %s: %w", l.base, err).
			WithRemedy("start it (`ollama serve`), or set MOBIUM_AI_BASE_URL to where one is served, then `mobium daemon stop`")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		return Step{}, mobiumerr.New(mobiumerr.ToolchainMissing, "the local model %s refused the request (%d): %s",
			l.Model, resp.StatusCode, clip(string(raw), 300)).
			WithRemedy(fmt.Sprintf("check the model is installed (`ollama list`); %s names one, after %q", l.Model, LocalPrefix))
	}
	var out struct {
		Choices []struct {
			Message map[string]interface{} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return Step{}, mobiumerr.New(mobiumerr.Unclassified, "the local model's answer was not a chat completion: %s", clip(string(raw), 300))
	}
	msg := out.Choices[0].Message
	l.messages = append(l.messages, msg)
	var step Step
	calls, _ := msg["tool_calls"].([]interface{})
	for _, c := range calls {
		cm, _ := c.(map[string]interface{})
		fn, _ := cm["function"].(map[string]interface{})
		args, _ := fn["arguments"].(string)
		id, _ := cm["id"].(string)
		name, _ := fn["name"].(string)
		step.Calls = append(step.Calls, ToolCall{ID: id, Name: name, Input: json.RawMessage(args)})
	}
	return step, nil
}
