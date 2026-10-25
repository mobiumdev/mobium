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
	"app_devices": true, "app_doctor": true, "app_current": true, "app_state": true,
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
