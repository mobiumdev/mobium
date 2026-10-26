package agent

// Which calls only read, for two readers: an MCP client, told through the
// readOnlyHint annotation, and the test runner's expect, which retries a call
// until a field matches and so must never be given one that acts — a retrying
// assertion on app_tap would tap until it timed out. Every tool is in exactly
// one of the three sets below; a test fails on one that is in none, so a new
// tool has to be decided rather than defaulted.

// readOnlyTools only ever read. Some leave something off the device behind —
// app_map a ref table, app_screenshot a file on the Mac, app_logs its place
// in the log — but none changes the device or the app.
var readOnlyTools = map[string]bool{
	"app_devices": true, "app_doctor": true, "app_check": true, "app_recording": true, "app_verdict": true, "app_current": true, "app_state": true,
	"app_map": true, "app_find": true, "app_text": true, "app_source": true,
	"app_screenshot": true, "app_wait_for": true, "app_contexts": true, "app_list_apps": true,
	"app_battery": true, "app_time": true, "app_hit_test": true, "app_audit": true, "app_logs": true, "app_crashes": true,
	"app_download": true,
}

// readUnless read when called without any of their listed arguments and act
// with one: app_network reports the conditions, and sets them given offline
// or a proxy. Every other argument they take is in readArgs.
var readUnless = map[string][]string{
	"app_accessibility": {"value"},
	// app_speech reads what the cursor is on, and moves the cursor — which
	// scrolls a list to keep it in view — given a target or a walk.
	"app_speech":        {"target", "walk"},
	"app_alert":         {"action", "button", "text"},
	"app_appearance":    {"appearance"},
	"app_clipboard":     {"text"},
	"app_context":       {"context"},
	"app_dialogs":       {"clear", "press", "when"},
	"app_keyboard":      {"hide", "key", "text"},
	"app_locale":        {"locale"},
	"app_location":      {"clear", "gpx", "gpx_data", "latitude", "longitude", "speed", "waypoints"},
	"app_network":       {"download_kbps", "latency_ms", "offline", "proxy", "reset", "upload_kbps"},
	"app_notifications": {"shade", "tag", "text", "title"},
	"app_orientation":   {"orientation"},
	"app_timezone":      {"timezone"},
}

// readArgs are the arguments a readUnless tool takes that leave it reading:
// which device, and what to read. An argument in neither list is undecided,
// and a test fails on it — a setting argument added without a word here
// would make every call that sets it count as a read.
var readArgs = map[string][]string{
	"*":                 {"device", "driver"},
	"app_accessibility": {"setting"},
	"app_locale":        {"app"},
	"app_speech":        {"limit", "listen"},
}

// actingTools change the device or the app with every call.
var actingTools = map[string]bool{
	"app_boot": true, "app_shutdown": true,
	"app_tap": true, "app_drag": true, "app_type": true, "app_fill": true, "app_swipe": true,
	"app_long_press": true, "app_launch": true, "app_terminate": true, "app_open_url": true,
	"app_scroll_to": true, "app_grant": true, "app_revoke": true, "app_reset_permissions": true,
	"app_eval": true, "app_cookies": true, "app_storage": true, "app_record": true, "app_audio": true,
	"app_call": true, "app_sms": true, "app_zoom": true, "app_rotate": true, "app_press_tap": true,
	"app_press_drag": true, "app_set": true, "app_press": true, "app_lock": true,
	"app_session": true, "app_screen": true, "app_uninstall": true, "app_clear_data": true,
	"app_batch": true, "app_shake": true, "app_biometric": true, "app_background": true, "app_install": true,
	"app_upload": true, "app_trace": true, "app_hook": true, "app_assistant": true,
}

// ReadOnlyTool says whether every call to the tool only reads — MCP's
// readOnlyHint.
func ReadOnlyTool(name string) bool { return readOnlyTools[name] }

// IsReadCall says whether this call, with these arguments, only reads.
func IsReadCall(name string, args map[string]interface{}) bool {
	if readOnlyTools[name] {
		return true
	}
	acts, ok := readUnless[name]
	if !ok {
		return false
	}
	for _, a := range acts {
		if _, given := args[a]; given {
			return false
		}
	}
	return true
}

// destructiveTools may delete what Mobium cannot give back — an app, its
// data, its permissions — or end something running: MCP's destructiveHint,
// true. additiveTools change only what Mobium puts back when the session
// ends, or add: destructiveHint false. An acting tool in neither, such as
// app_tap, says nothing, and MCP's default — it may destroy — stands: a tap
// can press Delete, and claiming otherwise would be a promise Mobium cannot
// keep.
var destructiveTools = map[string]bool{
	"app_uninstall": true, "app_clear_data": true, "app_reset_permissions": true, "app_revoke": true,
	"app_shutdown": true, "app_terminate": true, "app_storage": true, "app_cookies": true,
}

var additiveTools = map[string]bool{
	"app_boot": true, "app_launch": true, "app_install": true, "app_grant": true,
	"app_appearance": true, "app_orientation": true, "app_locale": true, "app_timezone": true,
	"app_network": true, "app_location": true, "app_accessibility": true,
	"app_record": true, "app_audio": true, "app_trace": true, "app_screen": true,
}

// idempotentTools reach a state, so calling one twice with the same arguments
// does what calling it once did: MCP's idempotentHint. app_set is the reason
// the tool exists; the settings are states, not toggles; lock and unlock are
// states rather than a power button.
var idempotentTools = map[string]bool{
	"app_set": true, "app_launch": true, "app_terminate": true, "app_grant": true, "app_revoke": true,
	"app_reset_permissions": true, "app_clear_data": true, "app_uninstall": true, "app_lock": true,
	"app_boot": true, "app_shutdown": true,
	"app_appearance": true, "app_orientation": true, "app_locale": true, "app_timezone": true,
	"app_network": true, "app_location": true, "app_accessibility": true,
}

// Annotations are a tool's MCP hints. readOnlyHint is always given; the
// other two only for a tool that acts, since MCP reads them only then.
func Annotations(name string) map[string]interface{} {
	a := map[string]interface{}{"readOnlyHint": readOnlyTools[name]}
	if readOnlyTools[name] {
		return a
	}
	switch {
	case destructiveTools[name]:
		a["destructiveHint"] = true
	case additiveTools[name]:
		a["destructiveHint"] = false
	}
	if idempotentTools[name] {
		a["idempotentHint"] = true
	}
	return a
}

// ToolsFor is the tools a policy leaves an agent able to call: under
// read-only, those that can read — every read-only tool and every tool that
// reads unless told to act — so an agent is not offered what it would be
// refused.
func ToolsFor(p Policy) []Tool {
	all := GetToolSchemas()
	if !p.ReadOnly {
		return all
	}
	out := make([]Tool, 0, len(all))
	for _, t := range all {
		if _, reads := readUnless[t.Name]; readOnlyTools[t.Name] || reads {
			out = append(out, t)
		}
	}
	return out
}
