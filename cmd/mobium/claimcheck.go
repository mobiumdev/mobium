package main

import "github.com/spf13/cobra"

func newClaimCheckCmd() *cobra.Command {
	var record, model string
	cmd := &cobra.Command{
		Use:   "check <claim> -i <recording.zip>",
		Short: "Check a claim about a recorded session, with an independent model",
		Long: "A model with a fresh context reads the recording — each call, its result or\n" +
			"error, and the screen and screenshot after it — through read-only tools, and\n" +
			"answers PASSED, FAILED or INCONCLUSIVE with the observations it rests on.\n\n" +
			"Inconclusive means the recording cannot settle the claim. A model's judgment\n" +
			"is not proof: keep `wait` and test assertions for what can be asserted\n" +
			"directly. Needs no device. The default judge, Claude, needs an Anthropic\n" +
			"credential (`ant auth login`, or ANTHROPIC_API_KEY where the daemon starts),\n" +
			"and each check is a paid call; it is built but not yet measured against\n" +
			"claims with known answers. openai:<name> judges are measured, and\n" +
			"`recording` with `verdict` lets an agent you run be the judge.\n" +
			"See docs/guides/verifier.md. A check against a live device is not built yet.",
		Example: `  mobium trace start && mobium set testid=termsCheck && mobium trace stop -o form.zip
  mobium check "the terms box was ticked after the tap" -i form.zip`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			call := map[string]interface{}{"claim": args[0], "record": record}
			if model != "" {
				call["model"] = model
			}
			return runTool("app_check", call)
		},
	}
	cmd.Flags().StringVarP(&record, "input", "i", "", "The recording: the .zip `mobium trace stop` saved")
	cmd.Flags().StringVar(&model, "model", "", "The model that judges (default claude-opus-5-5, not yet measured); openai:<name> for one served through the OpenAI-compatible API, Ollama on this machine by default, such as openai:qwen2.5:14b")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}
