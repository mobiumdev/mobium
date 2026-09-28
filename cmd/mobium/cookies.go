package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/spf13/cobra"
)

func newCookiesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cookies [clear [name] | <name> <value>]",
		Short: "Read, set or clear the current WebView's cookies",
		Long: "Vibium's cookies command, on a WebView — `mobium context WEBVIEW_...` first.\n\n" +
			"With no argument, lists the cookies the page's URL is sent, HttpOnly ones\n" +
			"included. With a name and a value, sets one and reads it back. `clear`\n" +
			"deletes them all, or those with the name given. An Android app's WebViews\n" +
			"share one cookie store, so a cookie set here is set for all of them.",
		Example: `  mobium cookies
  mobium cookies session abc123 --http-only --secure
  mobium cookies clear session
  mobium cookies clear`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0:
				return runTool("app_cookies", map[string]interface{}{"action": "get"})
			case args[0] == "clear":
				call := map[string]interface{}{"action": "clear"}
				if len(args) == 2 {
					call["name"] = args[1]
				}
				return runTool("app_cookies", call)
			case len(args) == 2:
				return runTool("app_cookies", map[string]interface{}{"action": "set",
					"cookies": []interface{}{cookieFromFlags(cmd, args[0], args[1])}})
			default:
				return mobiumerr.New(mobiumerr.InvalidArgument, "give a name and a value to set a cookie, or `clear` to delete them")
			}
		},
	}
	cmd.Flags().String("domain", "", "with a name and value: the cookie's domain (default: the page's host)")
	cmd.Flags().String("path", "", "with a name and value: the cookie's path (default: /)")
	cmd.Flags().Float64("expires", 0, "with a name and value: seconds since the epoch (default: a session cookie)")
	cmd.Flags().Bool("http-only", false, "with a name and value: HttpOnly, so the page's scripts cannot read it")
	cmd.Flags().Bool("secure", false, "with a name and value: sent only over https")
	cmd.Flags().String("same-site", "", "with a name and value: Strict, Lax or None")
	return cmd
}

// cookieFromFlags is the one cookie `mobium cookies <name> <value>` sets. Its
// keys are a cookie's, inside the tool's cookies list, not the tool's own
// arguments.
func cookieFromFlags(cmd *cobra.Command, name, value string) map[string]interface{} {
	c := map[string]interface{}{"name": name, "value": value}
	for flag, key := range map[string]string{"domain": "domain", "path": "path", "same-site": "sameSite"} {
		if v, _ := cmd.Flags().GetString(flag); v != "" {
			c[key] = v
		}
	}
	if v, _ := cmd.Flags().GetFloat64("expires"); v > 0 {
		c["expires"] = v
	}
	if v, _ := cmd.Flags().GetBool("http-only"); v {
		c["httpOnly"] = true
	}
	if v, _ := cmd.Flags().GetBool("secure"); v {
		c["secure"] = true
	}
	return c
}

func newStorageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage [restore <file> | clear]",
		Short: "Save, restore or clear the current WebView's cookies and web storage",
		Long: "Vibium's storage command, on a WebView — `mobium context WEBVIEW_...` first.\n\n" +
			"With no argument, prints the page's storage state — its cookies, and its\n" +
			"origin's localStorage and sessionStorage — in the shape Playwright and\n" +
			"Vibium save, so a state saved by one restores in another; -o writes it to a\n" +
			"file. `restore` sets a saved state's cookies and writes each origin's\n" +
			"storage only into a page on that origin. `clear` empties all three.",
		Example: `  mobium storage -o state.json
  mobium storage restore state.json
  mobium storage clear`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0:
				out, _ := cmd.Flags().GetString("output")
				if out == "" {
					return runTool("app_storage", map[string]interface{}{"action": "get"})
				}
				result, err := daemonCall("app_storage", map[string]interface{}{"action": "get"})
				if err != nil {
					return err
				}
				var view struct {
					State json.RawMessage `json:"state"`
				}
				data, _ := json.Marshal(result.StructuredContent)
				if err := json.Unmarshal(data, &view); err != nil || len(view.State) == 0 {
					return mobiumerr.New(mobiumerr.Internal, "app_storage answered no state")
				}
				var pretty interface{}
				_ = json.Unmarshal(view.State, &pretty)
				body, _ := json.MarshalIndent(pretty, "", "  ")
				if err := os.WriteFile(out, append(body, '\n'), 0o600); err != nil {
					return mobiumerr.New(mobiumerr.InvalidArgument, "write %s: %w", out, err)
				}
				fmt.Printf("saved %s\n", out)
				return nil
			case args[0] == "clear" && len(args) == 1:
				return runTool("app_storage", map[string]interface{}{"action": "clear"})
			case args[0] == "restore" && len(args) == 2:
				data, err := os.ReadFile(args[1])
				if err != nil {
					return mobiumerr.New(mobiumerr.InvalidArgument, "read %s: %w", args[1], err)
				}
				var state map[string]interface{}
				if err := json.Unmarshal(data, &state); err != nil {
					return mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a storage state: %w", args[1], err)
				}
				return runTool("app_storage", map[string]interface{}{"action": "restore", "state": state})
			default:
				return mobiumerr.New(mobiumerr.InvalidArgument, "use `mobium storage`, `mobium storage restore <file>` or `mobium storage clear`")
			}
		},
	}
	// 0600, because a storage state is a login: its cookies are sessions.
	cmd.Flags().StringP("output", "o", "", "write the state to this file (created 0600: its cookies are logins)")
	return cmd
}
