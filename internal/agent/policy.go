package agent

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Policy is what whoever starts Mobium allows an agent to do through it:
// nothing but read, and act only on the apps it names. It is set where
// Mobium is started — `mobium mcp --read-only --apps …`, or MOBIUM_READ_ONLY
// and MOBIUM_APPS in the environment of the daemon and the CLI — never by a
// tool call, so an agent cannot lift it. THREAT-MODEL.md, LLM06.
//
// It is enforced at dispatch, so a batch's steps are held to it one by one,
// and a refusal is not_permitted: no retry, other device or other argument
// helps, only a different policy.
type Policy struct {
	// ReadOnly refuses every call that is not a read (IsReadCall).
	ReadOnly bool
	// Apps, when not empty, are the only apps a call may name, and the only
	// apps a call that acts on the screen may act on.
	Apps []string
}

// PolicyEnvReadOnly and PolicyEnvApps set a Policy from the environment.
const (
	PolicyEnvReadOnly = "MOBIUM_READ_ONLY"
	PolicyEnvApps     = "MOBIUM_APPS"
)

// PolicyFromEnv is the policy MOBIUM_READ_ONLY (1 or true) and MOBIUM_APPS
// (comma-separated app ids) set.
func PolicyFromEnv() Policy {
	var p Policy
	switch strings.ToLower(strings.TrimSpace(os.Getenv(PolicyEnvReadOnly))) {
	case "1", "true", "yes":
		p.ReadOnly = true
	}
	p.Apps = SplitApps(os.Getenv(PolicyEnvApps))
	return p
}

// SplitApps is a comma-separated list of app ids, trimmed, empties dropped.
func SplitApps(s string) []string {
	var apps []string
	for _, a := range strings.Split(s, ",") {
		if a = strings.TrimSpace(a); a != "" {
			apps = append(apps, a)
		}
	}
	return apps
}

// Merge is p with q's restrictions added: either read-only makes it
// read-only, and both app lists must allow an app.
func (p Policy) Merge(q Policy) Policy {
	out := Policy{ReadOnly: p.ReadOnly || q.ReadOnly}
	switch {
	case len(p.Apps) == 0:
		out.Apps = q.Apps
	case len(q.Apps) == 0:
		out.Apps = p.Apps
	default:
		for _, a := range p.Apps {
			if slices.Contains(q.Apps, a) {
				out.Apps = append(out.Apps, a)
			}
		}
		if len(out.Apps) == 0 {
			// Two lists with nothing in common allow nothing, not everything.
			out.Apps = []string{"\x00none"}
		}
	}
	return out
}

// Active says whether p restricts anything.
func (p Policy) Active() bool { return p.ReadOnly || len(p.Apps) > 0 }

func (p Policy) allows(app string) bool { return slices.Contains(p.Apps, app) }

// systemUI are the apps that draw over the app in front without being a
// different app the agent chose to act in: a permission prompt, the
// notification shade, the home screen's dialogs. An action on one of them
// is an action on the allowed app's behalf.
var systemUI = map[string]bool{
	"com.android.systemui":                    true,
	"com.android.permissioncontroller":        true,
	"com.google.android.permissioncontroller": true,
	"android":                             true,
	"com.apple.springboard":               true,
	"com.android.packageinstaller":        true,
	"com.google.android.packageinstaller": true,
}

// screenActions act on whatever is on the screen rather than on an app they
// name: under an app list, the app in front must be one of them.
var screenActions = map[string]bool{
	"app_tap": true, "app_type": true, "app_fill": true, "app_swipe": true, "app_long_press": true,
	"app_drag": true, "app_scroll_to": true, "app_set": true, "app_zoom": true, "app_rotate": true,
	"app_press_tap": true, "app_press_drag": true, "app_eval": true, "app_cookies": true, "app_storage": true,
	"app_hook": true, "app_keyboard": true, "app_alert": true, "app_dialogs": true, "app_clipboard": true,
}

// permit refuses a call the policy does not allow. foreground answers the app
// in front of the call's device, and is asked only when an app list must be
// held against an action on the screen.
func (p Policy) permit(name string, args map[string]interface{}, foreground func() (string, error)) error {
	if !p.Active() || name == "app_batch" {
		return nil
	}
	reads := IsReadCall(name, args)
	if p.ReadOnly && !reads {
		return mobiumerr.New(mobiumerr.NotPermitted, "%s acts, and this Mobium allows only reading (%s, or mobium mcp --read-only)",
			name, PolicyEnvReadOnly).
			WithRemedy("read the screen with app_map, app_text or app_screenshot; acting needs Mobium started without the read-only setting, which only whoever starts it can change")
	}
	if len(p.Apps) == 0 || reads {
		return nil
	}
	if name == "app_install" {
		return mobiumerr.New(mobiumerr.NotPermitted, "app_install is not allowed while Mobium is limited to %s: the package an app file holds is not known until it is installed",
			strings.Join(p.Apps, ", ")).
			WithRemedy("install the app before starting Mobium with an app list, or add it to " + PolicyEnvApps)
	}
	if app, ok := args["app"].(string); ok && app != "" {
		if !p.allows(app) {
			return mobiumerr.New(mobiumerr.NotPermitted, "%s names %s, and this Mobium may act only on %s",
				name, app, strings.Join(p.Apps, ", ")).
				WithRemedy("work in one of the allowed apps; the list is set where Mobium is started (" + PolicyEnvApps + ", or mobium mcp --apps)")
		}
		return nil
	}
	if !screenActions[name] {
		return nil
	}
	front, err := foreground()
	if err != nil {
		return err
	}
	if front == "" || p.allows(front) || systemUI[front] {
		return nil
	}
	return mobiumerr.New(mobiumerr.NotPermitted, "%s would act on %s, the app in front, and this Mobium may act only on %s",
		name, front, strings.Join(p.Apps, ", ")).
		WithRemedy("bring an allowed app to the front with app_launch, then act")
}

// PolicyMeta is p as the _meta value MetaPolicy carries, or nil when p
// restricts nothing.
func PolicyMeta(p Policy) map[string]interface{} {
	if !p.Active() {
		return nil
	}
	apps := make([]interface{}, len(p.Apps))
	for i, a := range p.Apps {
		apps[i] = a
	}
	return map[string]interface{}{"read_only": p.ReadOnly, "apps": apps}
}

// policyOfMeta reads MetaPolicy back.
func policyOfMeta(meta map[string]interface{}) Policy {
	m, _ := meta[MetaPolicy].(map[string]interface{})
	var p Policy
	p.ReadOnly, _ = m["read_only"].(bool)
	if list, ok := m["apps"].([]interface{}); ok {
		for _, a := range list {
			if s, ok := a.(string); ok && s != "" {
				p.Apps = append(p.Apps, s)
			}
		}
	}
	return p
}

// permitCall holds a call to the handlers' policy, narrowed by the call's own
// (MetaPolicy), reading the app in front through the call's session only when
// it has to.
func (h *Handlers) permitCall(ctx context.Context, name string, args map[string]interface{}) error {
	return h.policy.Merge(h.callPolicy).permit(name, args, func() (string, error) {
		s, err := h.sessionFor(ctx, args)
		if err != nil {
			return "", err
		}
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return "", err
		}
		return tree.Package(), nil
	})
}

// SetPolicy holds every later call to p, added to whatever the handlers
// already enforce: a policy only ever narrows.
func (h *Handlers) SetPolicy(p Policy) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.policy = h.policy.Merge(p)
}

// Policy is what the handlers enforce.
func (h *Handlers) Policy() Policy {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.policy
}
