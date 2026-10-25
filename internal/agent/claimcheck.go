package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/mobiumdev/mobium/internal/claim"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// newClaimSession is the model a check asks: Anthropic's, unless a test
// gives a stand-in.
var newClaimSession = func(model string) (claim.Session, error) {
	if err := claim.Credentials(); err != nil {
		return nil, err
	}
	return claim.NewAnthropic(model), nil
}

// CheckView is app_check's answer: the verdict, and what it rests on.
type CheckView struct {
	Status   string           `json:"status"`
	Claim    string           `json:"claim"`
	Summary  string           `json:"summary"`
	Evidence []claim.Evidence `json:"evidence"`
	Record   string           `json:"record"`
	Model    string           `json:"model"`
	Steps    int              `json:"steps"`
}

// claimCheck is app_check: a claim about a recorded session, judged by a
// model in a fresh context that reads the recording and nothing else. It
// needs no device: a recording is enough. A check against a live device is
// not built yet, so a record is required.
func (h *Handlers) claimCheck(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	text := strings.TrimSpace(stringArg(args, "claim"))
	record := stringArg(args, "record")
	if text == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_check needs a claim, such as \"the order confirmation was shown\"")
	}
	if record == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_check needs record: a recording, the .zip `mobium trace stop` saves. "+
			"A check against a live device is not built yet").
			WithRemedy("app_trace with action start, run what the claim is about, app_trace with action stop and a path, then app_check with that path as record")
	}
	model := stringArg(args, "model")
	if model == "" {
		model = claim.DefaultModel
	}
	t, err := claim.Open(ctx, record)
	if err != nil {
		return nil, err
	}
	defer t.Close()
	s, err := newClaimSession(model)
	if err != nil {
		return nil, err
	}
	v, err := claim.Check(ctx, t, text, s)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", strings.ToUpper(v.Status), v.Summary)
	for _, e := range v.Evidence {
		fmt.Fprintf(&b, "\n  - %s", e.Summary)
	}
	fmt.Fprintf(&b, "\n(%s read %s in %d steps; a model's judgment, not proof)", model, record, v.Steps)
	return Result(b.String(), CheckView{Status: v.Status, Claim: v.Claim, Summary: v.Summary, Evidence: v.Evidence,
		Record: record, Model: model, Steps: v.Steps}), nil
}
