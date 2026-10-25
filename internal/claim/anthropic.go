package claim

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// DefaultModel is the model a check asks unless told another.
const DefaultModel = "claude-opus-5-5"

// Credentials reports whether the machine has an Anthropic credential the
// SDK will find: a key or token in the environment, or a profile `ant auth
// login` stored. It does not read the credential, only where one would be.
func Credentials() error {
	for _, v := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_PROFILE"} {
		if os.Getenv(v) != "" {
			return nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".config", "anthropic")); err == nil {
			return nil
		}
	}
	return mobiumerr.New(mobiumerr.ToolchainMissing, "a check asks a model, and no Anthropic credential is set up on this machine").
		WithRemedy("run `ant auth login`, or export ANTHROPIC_API_KEY in the shell that starts mobium and then `mobium daemon stop` so the daemon starts again with it")
}

// Anthropic is a Session on the Claude API: a fresh conversation, the
// verifier's tools, and every response kept whole in the history, its
// thinking included, as the API requires.
type Anthropic struct {
	Model  string
	client anthropic.Client
	params anthropic.BetaMessageNewParams
}

// NewAnthropic is a session for model, or DefaultModel.
func NewAnthropic(model string) *Anthropic {
	if model == "" {
		model = DefaultModel
	}
	return &Anthropic{Model: model, client: anthropic.NewClient()}
}

// Begin starts the conversation with the claim.
func (a *Anthropic) Begin(ctx context.Context, system, claim string, tools []Tool) (Step, error) {
	var ts []anthropic.BetaToolUnionParam
	for _, t := range tools {
		tool := anthropic.BetaToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.BetaToolInputSchemaParam{
				Properties:  t.Schema["properties"],
				Required:    t.Schema["required"].([]string),
				ExtraFields: map[string]any{"additionalProperties": false},
			},
			Strict: param.NewOpt(true),
		}
		ts = append(ts, anthropic.BetaToolUnionParam{OfTool: &tool})
	}
	a.params = anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(a.Model),
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Tools:     ts,
		// The verdict is asked for in the instruction: forcing a tool is
		// refused by the current models.
		ToolChoice:   anthropic.BetaToolChoiceUnionParam{OfAuto: &anthropic.BetaToolChoiceAutoParam{}},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortHigh},
		// A model that declines a claim on safety grounds is answered by
		// another, by category, rather than the check failing.
		Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(claim))},
	}
	return a.send(ctx)
}

// Next hands the model what its tools answered.
func (a *Anthropic) Next(ctx context.Context, results []Result) (Step, error) {
	var blocks []anthropic.BetaContentBlockParamUnion
	for _, r := range results {
		content := []anthropic.BetaToolResultBlockParamContentUnion{{OfText: &anthropic.BetaTextBlockParam{Text: r.Text}}}
		if r.JPEG != nil {
			img := anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
				Data: base64.StdEncoding.EncodeToString(r.JPEG), MediaType: anthropic.BetaBase64ImageSourceMediaTypeImageJPEG,
			})
			content = append(content, anthropic.BetaToolResultBlockParamContentUnion{OfImage: img.OfImage})
		}
		blocks = append(blocks, anthropic.BetaContentBlockParamUnion{OfToolResult: &anthropic.BetaToolResultBlockParam{
			ToolUseID: r.CallID, Content: content, IsError: param.NewOpt(r.IsError),
		}})
	}
	a.params.Messages = append(a.params.Messages, anthropic.NewBetaUserMessage(blocks...))
	return a.send(ctx)
}

func (a *Anthropic) send(ctx context.Context) (Step, error) {
	resp, err := a.client.Beta.Messages.New(ctx, a.params)
	if err != nil {
		return Step{}, mobiumerr.New(mobiumerr.Unclassified, "the model did not answer: %w", err)
	}
	a.params.Messages = append(a.params.Messages, resp.ToParam())
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		why := resp.StopDetails.Explanation
		if why == "" {
			why = string(resp.StopDetails.Category)
		}
		return Step{Refused: why}, nil
	}
	var step Step
	for _, b := range resp.Content {
		if u, ok := b.AsAny().(anthropic.BetaToolUseBlock); ok {
			step.Calls = append(step.Calls, ToolCall{ID: u.ID, Name: u.Name, Input: []byte(u.JSON.Input.Raw())})
		}
	}
	return step, nil
}
