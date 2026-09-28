package agent

import (
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// deviceParam is accepted by every tool that touches a device: it pins the
// call to one serial when more than one emulator is running.
func deviceParam() map[string]interface{} {
	return map[string]interface{}{
		"type": "string",
		"description": "Device serial to target, e.g. \"emulator-5554\". " +
			"Omit when only one device is running.",
	}
}

func withDevice(props map[string]interface{}) map[string]interface{} {
	props["device"] = deviceParam()
	props["driver"] = map[string]interface{}{
		"type": "string",
		"description": "Driver to use: \"uiautomator2\" (default for Android; fast, installs " +
			"a server APK on first use), \"uiautomator\" (Android, installs nothing, slower, " +
			"cannot type), or \"wda\" (WebDriverAgent: iOS simulators and iPhones).",
		"enum": []string{"uiautomator2", "uiautomator", "wda"},
	}
	return props
}

// GetToolSchemas returns the tools an MCP client can call. The CLI dispatches
// through the same names, so a command and a tool can never disagree.
func GetToolSchemas() []Tool {
	roles := uitree.Roles()
	sort.Strings(roles)

	return []Tool{
		{
			Name: "app_devices",
			Description: "List running Android emulators and devices and iOS simulators, " +
				"with their serial or UDID, state and model.",
			InputSchema: map[string]interface{}{
				"type":                 "object",
				"properties":           map[string]interface{}{},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_map",
			Description: "Map the actionable elements on the current screen and return them as " +
				"refs (@e1, @e2, ...) with a label and role. Call this before interacting, and " +
				"again after anything that changes the screen — refs are only valid for the " +
				"screen they were taken from.",
			InputSchema: map[string]interface{}{
				"type":                 "object",
				"properties":           withDevice(map[string]interface{}{}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_tap",
			Description: "Tap an element. Give a ref from app_map (\"@e5\"), a locator " +
				"(\"text=Sign In\", \"testid=submit\", \"label=Email\", \"role=button\"), or " +
				"x and y in device pixels. The element is re-resolved immediately before the " +
				"tap, so a ref from a screen that has since changed fails rather than tapping " +
				"the wrong place.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "A ref from app_map (\"@e5\") or a locator (\"text=Sign In\").",
					},
					"x": map[string]interface{}{
						"type":        "integer",
						"description": "X coordinate in device pixels. Use with y, instead of target.",
					},
					"y": map[string]interface{}{
						"type":        "integer",
						"description": "Y coordinate in device pixels. Use with x, instead of target.",
					},
					"double": map[string]interface{}{
						"type": "boolean",
						"description": "Tap twice, close enough together that the platform " +
							"reads one gesture rather than two taps. Everything else is " +
							"identical, including how the target is resolved. The " +
							"uiautomator dump driver refuses this: it taps one adb call " +
							"at a time and nothing there controls the 40-300ms interval " +
							"the gesture is made of.",
					},
					"fingers": map[string]interface{}{
						"type": "integer",
						"description": "How many fingers tap at once, 1 to 5. Defaults to 1. " +
							"With more, they land side by side about the same point, a " +
							"tenth of the screen width apart, and lift together — a " +
							"two-finger tap, a three-finger tap. On iOS three fingers can " +
							"reach the system instead of the app: three-finger gestures " +
							"are undo, redo, copy and paste there. Not with double.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_drag",
			Description: "Drag and drop: press something, hold until it is picked up, " +
				"carry it, and release. **Not app_swipe with different names** — a swipe " +
				"has no hold at either end, so it flings the list instead of moving the " +
				"item in it. Give from and to as refs or locators, both resolved from one " +
				"snapshot before anything is touched, or all four of x1, y1, x2, y2. It " +
				"reports that the gesture was delivered and where it went; whether the " +
				"drop was accepted is the app's own state, so call app_map again to see " +
				"it. Discards the refs from the last app_map.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"from": map[string]interface{}{
						"type":        "string",
						"description": "A ref or locator for the thing being dragged.",
					},
					"to": map[string]interface{}{
						"type": "string",
						"description": "A ref or locator for where it is going. Given " +
							"together with from.",
					},
					"x1": map[string]interface{}{
						"type":        "integer",
						"description": "Start X in device pixels. Use all four, instead of from/to.",
					},
					"y1": map[string]interface{}{
						"type":        "integer",
						"description": "Start Y in device pixels.",
					},
					"x2": map[string]interface{}{
						"type":        "integer",
						"description": "End X in device pixels.",
					},
					"y2": map[string]interface{}{
						"type":        "integer",
						"description": "End Y in device pixels.",
					},
					"hold_ms": map[string]interface{}{
						"type": "integer",
						"description": "How long to hold at each end, in milliseconds. " +
							"Defaults to 700, above Android's 500ms long-press timeout " +
							"because that is what a drag-to-reorder list arms on. Raise " +
							"it first when a drag picks nothing up.",
					},
					"duration_ms": map[string]interface{}{
						"type": "integer",
						"description": "How long the travel takes, in milliseconds. " +
							"Defaults to 800.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_text",
			Description: "Read text from the screen. With no target, returns everything readable " +
				"in top-to-bottom order; with a ref or locator, returns that element's text.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Optional ref (\"@e3\") or locator (\"testid=title\").",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_screenshot",
			Description: "Capture the device screen as a PNG. Returns the image to the caller, " +
				"and also writes it to disk when path is given.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Optional file path to save the PNG to.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_type",
			Description: "Type text into a specific element, after what it already holds; " +
				"app_fill replaces it instead. The element is located on the " +
				"device, so the text goes where you aimed it rather than wherever focus " +
				"happens to be, and quotes, spaces and non-ASCII survive intact. Pass an " +
				"empty string to clear the field. Requires the uiautomator2 driver.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "A ref from app_map (\"@e3\") or a locator (\"testid=search\").",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text to enter. An empty string clears the field.",
					},
				}),
				"required":             []string{"target", "text"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_fill",
			Description: "Clear a specific element and type text into it, replacing what it " +
				"held — Vibium's fill; app_type adds to it instead. Located, checked and " +
				"typed as app_type is. Requires the uiautomator2 driver.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "A ref from app_map (\"@e3\") or a locator (\"testid=search\").",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text the field should hold. An empty string clears it.",
					},
				}),
				"required":             []string{"target", "text"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_swipe",
			Description: "Swipe the screen. Give a direction (up, down, left, right) to scroll " +
				"across the middle of the screen, or all four of x1/y1/x2/y2 for an exact drag. " +
				"Swiping up scrolls content down, as a finger does.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"direction": map[string]interface{}{
						"type":        "string",
						"description": "Direction to swipe.",
						"enum":        []string{"up", "down", "left", "right"},
					},
					"x1": map[string]interface{}{"type": "integer", "description": "Start X in device pixels."},
					"y1": map[string]interface{}{"type": "integer", "description": "Start Y in device pixels."},
					"x2": map[string]interface{}{"type": "integer", "description": "End X in device pixels."},
					"y2": map[string]interface{}{"type": "integer", "description": "End Y in device pixels."},
					"duration_ms": map[string]interface{}{
						"type": "integer",
						"description": "How long the drag takes. Shorter flings further; " +
							"default 300.",
						"default": 300,
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_long_press",
			Description: "Press and hold an element or a point — the gesture that opens " +
				"context menus and starts drag-and-drop.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "A ref from app_map (\"@e5\") or a locator.",
					},
					"x":           map[string]interface{}{"type": "integer", "description": "X in device pixels, instead of target."},
					"y":           map[string]interface{}{"type": "integer", "description": "Y in device pixels, instead of target."},
					"duration_ms": map[string]interface{}{"type": "integer", "description": "Hold time; default 800.", "default": 800},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_contexts",
			Description: "List the automatable contexts on the device: the native shell plus " +
				"any debuggable WebViews. A hybrid app's web content is only reachable after " +
				"switching into its context with app_context.",
			InputSchema: map[string]interface{}{
				"type":                 "object",
				"properties":           withDevice(map[string]interface{}{}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_context",
			Description: "Switch between the native shell and a WebView, or report the current " +
				"context when called with no argument. While a WebView context is active, " +
				"app_map and app_tap operate on the page's elements; taps are still delivered " +
				"as real touches on the device.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"context": map[string]interface{}{
						"type": "string",
						"description": "A context id from app_contexts, or \"NATIVE_APP\" to " +
							"return to the native shell. Omit to read the current context.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_launch",
			Description: "Bring an app to the foreground, starting it if it is not running. " +
				"Refs from the previous screen are discarded, so call app_map afterwards.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type": "string",
						"description": "Package name on Android (\"com.example.shop\") or bundle " +
							"id on iOS (\"com.example.Shop\").",
					},
				}),
				"required":             []string{"app"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_terminate",
			Description: "Stop a running app. Useful between flows, to start from a known " +
				"state rather than wherever the last one left off.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Package name on Android or bundle id on iOS.",
					},
				}),
				"required":             []string{"app"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "app_install",
			Description: "Install an app from a local .apk (Android) or .app bundle (iOS).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Local path to the .apk or .app.",
					},
					"content": map[string]interface{}{
						"type": "string",
						"description": "The app itself, base64, instead of a path: a .apk, or a .app " +
							"directory as a .tar.gz. For a daemon on another machine; the CLI " +
							"and pipe send it from a path.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "With content: the file's name, such as \"app.apk\" or \"MobiumApp.app.tar.gz\".",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_open_url",
			Description: "Open a URL or deep link on the device — the quickest way to reach a " +
				"specific screen without tapping through to it.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "A URL or app deep link, e.g. \"myapp://cart\".",
					},
				}),
				"required":             []string{"url"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_current",
			Description: "Report the app currently in the foreground, as a package name or " +
				"bundle id. Use it to confirm that a tap opened what you expected.",
			InputSchema: map[string]interface{}{
				"type":                 "object",
				"properties":           withDevice(map[string]interface{}{}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_wait_for",
			Description: "Wait until an element appears, disappears, or shows particular text, " +
				"then return. Use this instead of sleeping after an action that takes time — a " +
				"login, a network fetch, a screen transition. On success the screen is remapped, " +
				"so the element it waited for already has a ref and can be tapped straight away. " +
				"If the condition never holds the call fails, saying what the screen showed " +
				"instead.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "A ref from app_map (\"@e5\") or a locator (\"text=Welcome\").",
					},
					"condition": map[string]interface{}{
						"type": "string",
						"description": "\"visible\" (default) waits for it to be on screen, " +
							"\"hidden\" waits for it to go away — a spinner, say — " +
							"\"text\" waits until it contains the text given below, and " +
							"\"enabled\" or \"disabled\" waits for a control to be one or " +
							"the other — a Submit the app enables once a form is valid.",
						"enum": []string{condVisible, condHidden, condText, condEnabled, condDisabled},
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text to wait for. Required by, and only used by, condition \"text\".",
					},
					"timeout_ms": map[string]interface{}{
						"type": "integer",
						"description": "How long to wait before failing, in milliseconds. " +
							"Default 10000, maximum 120000.",
					},
				}),
				"required":             []string{"target"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_scroll_to",
			Description: "Scroll until an element is on screen, then return it with a ref. " +
				"Use it for anything below the fold — app_map only sees what is currently " +
				"visible. app_tap, app_type and app_long_press already scroll to a target " +
				"that is not on screen, so call this directly only when you want to see the " +
				"element without acting on it, or want to scroll up. **The direction is " +
				"yours to give and there is no sensible default for the axis**: nothing " +
				"in the hierarchy says which way a container scrolls — Android clips " +
				"child bounds to the parent, so a horizontal pager and a vertical list " +
				"are indistinguishable from outside. Swiping the wrong way is not a " +
				"no-op; if it navigates instead of scrolling, mobium notices after one " +
				"swipe and says so.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "A ref from app_map (\"@e5\") or a locator (\"text=Sign out\").",
					},
					"direction": map[string]interface{}{
						"type": "string",
						"description": "Which way to look: \"down\" (default) or \"up\" for a " +
							"list, \"right\" or \"left\" for a pager or carousel.",
						"enum": []string{"down", "up", "left", "right"},
					},
				}),
				"required":             []string{"target"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_grant",
			Description: "Grant an app's permissions up front, so a first-launch permission " +
				"dialog never appears to block the flow. \"all\" grants everything the app " +
				"declares. Names are cross-platform (" + strings.Join(PermissionNames(), ", ") +
				"); a platform name like \"android.permission.CAMERA\" also works. On Android " +
				"the result is verified by reading the permission state back, because `pm grant` " +
				"reports success for permissions the app never declared.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Package name on Android or bundle id on iOS.",
					},
					"permissions": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Permission names, or [\"all\"].",
					},
				}),
				"required":             []string{"app", "permissions"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_revoke",
			Description: "Deny an app's permissions, to test how it behaves without them. " +
				"Takes the same names as app_grant.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Package name on Android or bundle id on iOS.",
					},
					"permissions": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Permission names, or [\"all\"].",
					},
				}),
				"required":             []string{"app", "permissions"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_reset_permissions",
			Description: "Put permissions back to their defaults, so the app prompts again on " +
				"next use. iOS can reset one app; Android cannot — `pm reset-permissions` is " +
				"device-wide, so on Android omit the app, and naming one is refused rather " +
				"than resetting every app behind your back.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type": "string",
						"description": "Bundle id to reset (iOS only). Omit to reset every app, " +
							"which is the only form Android supports.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_eval",
			Description: "Run a JavaScript expression in the current WebView and return its " +
				"value as a string. Needs a WebView context — switch with app_context first. " +
				"Objects come back as JSON. This is the escape hatch for anything app_map and " +
				"app_text do not cover, and the way to ask a page a question directly rather " +
				"than inferring the answer from what is on screen.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"expression": map[string]interface{}{
						"type":        "string",
						"description": "A JavaScript expression, e.g. \"document.title\".",
					},
				}),
				"required":             []string{"expression"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_cookies",
			Description: "Read, set or clear the current WebView's cookies — the ones its page's URL is sent, " +
				"HttpOnly ones included, which document.cookie cannot see. Vibium's cookies command, on a " +
				"WebView: needs a web context, so switch with app_context first. \"get\" (the default) lists " +
				"them; \"set\" sets each of cookies and reads the store back, so one the browser accepted and " +
				"stored expired is reported, not claimed; \"clear\" deletes them all, or those called name. " +
				"An Android app's WebViews share one cookie store, so a cookie set here is set for all of them.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"get", "set", "clear"},
						"description": "get (default), set or clear.",
					},
					"cookies": map[string]interface{}{
						"type":        "array",
						"description": "With set: the cookies to set.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":     map[string]interface{}{"type": "string"},
								"value":    map[string]interface{}{"type": "string"},
								"domain":   map[string]interface{}{"type": "string", "description": "Defaults to the page's host."},
								"path":     map[string]interface{}{"type": "string", "description": "Defaults to /."},
								"expires":  map[string]interface{}{"type": "number", "description": "Seconds since the epoch; omit for a session cookie."},
								"httpOnly": map[string]interface{}{"type": "boolean"},
								"secure":   map[string]interface{}{"type": "boolean"},
								"sameSite": map[string]interface{}{"type": "string", "enum": []string{"Strict", "Lax", "None"}},
							},
							"required":             []string{"name", "value"},
							"additionalProperties": false,
						},
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "With clear: delete only the cookies with this name.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_storage",
			Description: "Save, restore or clear the current WebView's storage state: its cookies and its " +
				"origin's localStorage and sessionStorage, in the shape Playwright and Vibium save — " +
				"{cookies, origins: [{origin, localStorage, sessionStorage}]} — so a state saved by one " +
				"restores in another. Vibium's storage command, on a WebView: switch with app_context first. " +
				"\"get\" (the default) answers the state; \"restore\" sets its cookies and writes each origin's " +
				"storage only into a page on that origin, saying which it skipped; \"clear\" empties all three. " +
				"Every write is read back. A page with no origin of its own — an app's inline HTML, about:blank " +
				"— cannot hold storage, and is refused as that.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"get", "restore", "clear"},
						"description": "get (default), restore or clear.",
					},
					"state": map[string]interface{}{
						"type":        "object",
						"description": "With restore: a storage state, as get answers it.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_logs",
			Description: "Read a log: the current WebView's console, or the device's own log " +
				"(logcat on Android, the unified log on an iOS simulator, and on a real iPhone " +
				"what the session has captured since it started — a phone keeps no history). " +
				"**With no source it " +
				"follows the context** — the page's console in a WebView, the device log on the " +
				"native shell. **Each read reports what arrived since the last one**, so " +
				"\"nothing was logged during this step\" is assertable. The console includes " +
				"uncaught errors and unhandled promise rejections, and capture starts when the " +
				"context is entered. The device log's first read returns the most recent lines, " +
				"since there is no earlier read to continue from; a read narrowed by app or " +
				"level leaves the rest unread. For why an app died, app_crashes is the record " +
				"that survives — the log is a ring that noise overruns.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"source": map[string]interface{}{
						"type":        "string",
						"description": "Which log. Omit to follow the context.",
						"enum":        []string{"webview", "device"},
					},
					"level": map[string]interface{}{
						"type": "string",
						"description": "Only entries at exactly this level. Omit for all. A console " +
							"uses log/info/warn/error/debug; the device log " +
							"verbose/debug/info/warn/error/fatal.",
						"enum": []string{"log", "verbose", "debug", "info", "warn", "error", "fatal"},
					},
					"app": map[string]interface{}{
						"type": "string",
						"description": "Device log only: keep one app's lines, by package or bundle id. " +
							"Filtered by app rather than process, so a crash and the restart after " +
							"it are both kept. The system's own lines about the app are kept too — " +
							"system_server's on Android (\"ANR in <app>\"), runningboardd's on iOS " +
							"(\"termination reported by launchd\") — since those say it died or hung.",
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Device log only: the most recent lines to return, at most.",
						"default":     100,
						"minimum":     1,
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_record",
			Description: "Record the screen to a video: start, stop with a path to save it, or omit the " +
				"action to ask whether a recording is running. The saved file is checked by what it " +
				"holds — its frame count and duration, read from its own header — and a recorder that " +
				"could not finish is reported, not saved as if it had. Android records a frame only " +
				"when the screen changes, so a still screen is one frame; that is not a failure. One " +
				"recording per device; ending the session finishes and discards it. A real iPhone " +
				"refuses for now.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "start or stop. Omit to ask whether a recording is running.",
						"enum":        []string{"start", "stop"},
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Where to save the video, on stop — an .mp4 on this machine.",
					},
					"return_data": map[string]interface{}{
						"type": "boolean",
						"description": "On stop without a path: return the video, base64, instead of " +
							"saving it — for a daemon on another machine; the CLI and pipe ask for it " +
							"and save it where the caller said.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_keyboard",
			Description: "The soft keyboard: whether it is up and which field has focus, typing at that " +
				"field's cursor, pressing a named key, or hiding it. For the field nothing names — one " +
				"that took focus by itself, the next field after enter — where app_type needs a target. " +
				"Typing is confirmed by reading the field back, and retried on iOS, which drops " +
				"keystrokes; a password field's value is never shown. A key is reported as sent, " +
				"since what it does is the app's to decide. Hiding is confirmed; on an iPhone, whose " +
				"keyboard has no hide key, it is refused when the app offers no way, with the one " +
				"that usually works.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Type this at the cursor of the field that has focus.",
					},
					"key": map[string]interface{}{
						"type":        "string",
						"description": "Press a named key, after any text.",
						"enum":        []string{"enter", "delete", "space"},
					},
					"hide": map[string]interface{}{
						"type":        "boolean",
						"description": "Hide the keyboard. Cannot be combined with text or key.",
						"default":     false,
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_crashes",
			Description: "The crashes the device recorded — Java exceptions, native signals and " +
				"apps that stopped responding — newest first, with the line worth reading first. " +
				"Pass an id from the list to read one report in full; an ANR's leads with its " +
				"reason and the stuck main thread. Android rate-limits crash records per app, so " +
				"an app that crashed many times in a few minutes may crash again and add nothing — " +
				"the next record says how many were dropped. Not drained: a crash is a " +
				"record, kept across reboots until it ages out, so asking twice shows it twice. " +
				"Android reads dropbox; an iOS simulator reads the Mac's crash reports for that " +
				"simulator, and a real iPhone its own, over USB.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Only this app's crashes, by package or bundle id.",
					},
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Read this report in full, e.g. \"data_app_crash@1790357773461\".",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "The newest reports to list, at most.",
						"default":     20,
						"minimum":     1,
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_notifications",
			Description: "Read the notification shade, post a notification to it, or open " +
				"it. Reading is how you assert an app posted what it should. Posting is an " +
				"interruption arriving from elsewhere, and is confirmed by reading the shade " +
				"back — `cmd notification post` prints what it thinks it built and says " +
				"nothing about whether the system accepted it. Opening the shade is what " +
				"makes a notification tappable: until then it is not on screen and app_map " +
				"cannot see it. Opening or closing the shade discards the refs from the last " +
				"app_map, since it covers the app.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Body of a notification to post. Omit to only read.",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Title of the notification to post. Defaults to \"Mobium\".",
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Tag identifying the posted notification. Defaults to \"mobium\".",
					},
					"shade": map[string]interface{}{
						"type":        "string",
						"description": "\"open\" to pull the shade down so notifications can be tapped, \"close\" to dismiss it.",
						"enum":        []string{"open", "close"},
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_call",
			Description: "Simulate an incoming call: \"ring\", then \"accept\" or \"hang\". " +
				"Interruption is where mobile apps fail and desktop software does not — a " +
				"call mid-form, state lost on resume, a callback that never fires — and none " +
				"of it can be provoked by tapping. **Emulator only**: a real phone cannot be " +
				"made to ring from outside, which is a property of phones, and mobium says so " +
				"rather than failing obscurely. Confirmed against the device's telephony " +
				"state, not the console's acknowledgement. Discards the refs from the last " +
				"app_map, since a call takes over the screen.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "\"ring\" (default), \"accept\" or \"hang\".",
						"enum":        []string{"ring", "accept", "hang"},
					},
					"number": map[string]interface{}{
						"type":        "string",
						"description": "Calling number. Defaults to 5551234.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_sms",
			Description: "Deliver a simulated text message, to see what an app does when one " +
				"arrives mid-flow. **Emulator only**, for the same reason as app_call. " +
				"Discards the refs from the last app_map.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The message body.",
					},
					"from": map[string]interface{}{
						"type":        "string",
						"description": "Sender number. Defaults to 5551234.",
					},
				}),
				"required":             []string{"text"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_timezone",
			Description: "Read the device timezone, or change it. Unlike app_call this works " +
				"on real hardware. A clock that jumps a zone mid-session breaks scheduling, " +
				"caching and anything that stored a local timestamp, which is why time-based " +
				"interruptions are worth testing. Takes an IANA name such as \"Asia/Tokyo\". " +
				"Confirmed by reading it back — an unknown zone name is accepted and ignored, " +
				"so the check is the only thing that makes the answer worth anything.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "An IANA zone, e.g. \"Asia/Tokyo\". Omit to read.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_zoom",
			Description: "Pinch two fingers apart or together, about an element or the " +
				"middle of the screen. **It reports that the gesture was delivered and " +
				"nothing more.** Neither platform puts a zoom level in the " +
				"accessibility hierarchy, so unlike app_check there is no state to read " +
				"back — confirming a zoom means asking the thing that was zoomed, and a " +
				"WebView can answer with visualViewport.scale through app_eval. " +
				"Pinching about the wrong point scales the right amount in the wrong " +
				"place, so pass a target when one matters. A direction pinches by a " +
				"default amount; `from` and `to` say exactly how far the fingers " +
				"travel, the way app_swipe takes either a direction or coordinates. Discards the refs from the " +
				"last app_map, since anything on screen may now be a different size.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"direction": map[string]interface{}{
						"type":        "string",
						"description": "\"in\" (default) to zoom in, \"out\" to zoom out.",
						"enum":        []string{"in", "out"},
					},
					"target": map[string]interface{}{
						"type": "string",
						"description": "A ref or locator to pinch about. Defaults to the " +
							"middle of the screen.",
					},
					"from": map[string]interface{}{
						"type": "integer",
						"description": "Half the distance between the fingers at the " +
							"start, in device pixels. Give with `to` instead of a " +
							"direction, for a pinch of a particular size — one sized to " +
							"an element, or deliberately small.",
					},
					"to": map[string]interface{}{
						"type": "integer",
						"description": "Half the distance between the fingers at the end. " +
							"Larger than `from` zooms in, smaller zooms out. **Keep the " +
							"difference above about 100 device pixels**: a shorter travel " +
							"is below the platform's own threshold and is ignored — " +
							"measured, 50px of travel left the scale unchanged and 100px " +
							"moved it to 1.42.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_rotate",
			Description: "Turn two fingers about an element or the middle of the screen. " +
				"Positive degrees are clockwise. **It reports that the gesture was " +
				"delivered and nothing more**, and this is harder to confirm than a " +
				"zoom: nothing in either accessibility hierarchy reports a rotation, " +
				"and there is no WebView property to ask either — a page has to compute " +
				"the angle from raw touch events itself. The fingers are stepped around " +
				"the arc rather than moved straight to the end, because a straight move " +
				"is a chord and the platform reads it as a pinch with some rotation " +
				"attached. Discards the refs from the last app_map.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"degrees": map[string]interface{}{
						"type":        "number",
						"description": "How far to turn, positive clockwise. Defaults to 90.",
					},
					"target": map[string]interface{}{
						"type": "string",
						"description": "A ref or locator to turn about. Defaults to the " +
							"middle of the screen.",
					},
					"radius": map[string]interface{}{
						"type": "integer",
						"description": "How far each finger sits from the center, in " +
							"device pixels. Defaults to 200.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_press_tap",
			Description: "Press and tap: one finger holds an element while a second finger " +
				"taps another, and the first lifts only after the second has. The " +
				"gesture behind \"hold this, tap that\" — adding to a selection, a " +
				"modifier held while choosing. Give hold and tap as refs or locators, " +
				"resolved before anything touches the screen, or x1,y1 (hold) and " +
				"x2,y2 (tap). **It reports that the gesture was delivered**: what it " +
				"means is the app's decision, so call app_map again to see it. " +
				"**Android 15 and earlier only**: on iOS, XCTest adds a zero-length " +
				"touch at the second finger's target at the start of the gesture, " +
				"so WebDriverAgent refuses; on Android 16 and later UiAutomator2 gives " +
				"the second finger its own down time, Android rejects the rest of " +
				"the gesture and then every injected touch, so it refuses too. " +
				"Discards the refs from the last app_map.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"hold": map[string]interface{}{
						"type":        "string",
						"description": "A ref or locator for what the first finger holds.",
					},
					"tap": map[string]interface{}{
						"type":        "string",
						"description": "A ref or locator for what the second finger taps.",
					},
					"x1": map[string]interface{}{"type": "integer", "description": "Held point, X in device pixels. Instead of hold."},
					"y1": map[string]interface{}{"type": "integer", "description": "Held point, Y in device pixels."},
					"x2": map[string]interface{}{"type": "integer", "description": "Tapped point, X in device pixels. Instead of tap."},
					"y2": map[string]interface{}{"type": "integer", "description": "Tapped point, Y in device pixels."},
					"lead_ms": map[string]interface{}{
						"type": "integer",
						"description": "How long the first finger rests before the second " +
							"taps, in milliseconds. Defaults to 300 — under Android's 500ms " +
							"long-press timeout, so the held element does not open its own " +
							"menu first. Raise it for an app that wants a long press.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_press_drag",
			Description: "Press and drag: one finger holds an element while a second finger " +
				"drags from one place to another, and the first lifts only after the " +
				"second has. Not app_drag, which is one finger carrying something; here " +
				"the held finger anchors and the other moves. Give hold, from and to as " +
				"refs or locators, or x1,y1 (hold), x2,y2 (from) and x3,y3 (to). **It " +
				"reports that the gesture was delivered**: call app_map again to see " +
				"what it did. **Android 15 and earlier only**, for app_press_tap's reasons. Discards " +
				"the refs from the last app_map.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"hold": map[string]interface{}{
						"type":        "string",
						"description": "A ref or locator for what the first finger holds.",
					},
					"from": map[string]interface{}{
						"type":        "string",
						"description": "A ref or locator where the second finger lands.",
					},
					"to": map[string]interface{}{
						"type":        "string",
						"description": "A ref or locator where the second finger lifts.",
					},
					"x1": map[string]interface{}{"type": "integer", "description": "Held point, X in device pixels. Instead of hold."},
					"y1": map[string]interface{}{"type": "integer", "description": "Held point, Y in device pixels."},
					"x2": map[string]interface{}{"type": "integer", "description": "Second finger's start, X. Instead of from."},
					"y2": map[string]interface{}{"type": "integer", "description": "Second finger's start, Y."},
					"x3": map[string]interface{}{"type": "integer", "description": "Second finger's end, X. Instead of to."},
					"y3": map[string]interface{}{"type": "integer", "description": "Second finger's end, Y."},
					"lead_ms": map[string]interface{}{
						"type": "integer",
						"description": "How long the first finger rests before the second " +
							"lands, in milliseconds. Defaults to 300, under Android's " +
							"long-press timeout.",
					},
					"duration_ms": map[string]interface{}{
						"type":        "integer",
						"description": "How long the second finger's travel takes, in milliseconds. Defaults to 600.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_check",
			Description: "Put a checkbox or switch into a state, rather than toggling " +
				"whatever it is in. **Idempotent**: asking for a state it is already " +
				"in does nothing and says so, which is what makes it safe to call " +
				"without reading first. That is the difference from app_tap — tapping " +
				"a checkbox flips it, so reaching a known state with tap means read, " +
				"compare, tap, read again, on every checkbox. Refuses anything with no " +
				"checked state rather than tapping it and calling it checked, and " +
				"refuses to uncheck a radio button, which is not a thing a radio does: " +
				"a group is cleared by choosing a different member. The tap is " +
				"confirmed by reading the state back, so a control that did not " +
				"respond is reported rather than assumed.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "A ref from app_map (\"@e2\") or a locator (\"testid=terms\").",
					},
					"checked": map[string]interface{}{
						"type":        "boolean",
						"description": "The state to put it in. Defaults to true.",
					},
				}),
				"required":             []string{"target"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_alert",
			Description: "Read a system dialog, or answer it with accept or dismiss. " +
				"A permission prompt is **not part of the app** — it is another " +
				"process's window, and app_current reports that process while one is " +
				"up. Answered through the W3C alert endpoints rather than by tapping a " +
				"button, which means it works without knowing what the buttons say: a " +
				"flow that taps \"While using the app\" breaks the moment the device " +
				"is in another language. With no action it reports whether a dialog is " +
				"up and what it says; finding none is an answer rather than an error. " +
				"**These answer a dialog; they do not choose an outcome.** On a " +
				"permission prompt they do not mean grant and deny, and on iOS they " +
				"are the other way round — measured on iOS 26.5, accept left the " +
				"permission denied and dismiss left it granted. Which button each " +
				"presses depends on the platform and the dialog: Android presses its " +
				"positive and negative buttons (dismiss pressed the middle of three), " +
				"iOS an alert's last and first but an action sheet's first and last, " +
				"so accept on a three-button iOS alert pressed Cancel. " +
				"To choose an outcome, tap the button: app_map returns them like any " +
				"other element. Answering discards the refs from the last app_map, " +
				"since they named a screen with a dialog over it.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "\"accept\", \"dismiss\", or omitted to read.",
						"enum":        []string{"accept", "dismiss", "read"},
					},
					"text": map[string]interface{}{
						"type": "string",
						"description": "Text to type into a prompt's field, sent before " +
							"any action so one call can fill and confirm. A plain alert " +
							"has no field and the platform says so.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_clipboard",
			Description: "Read the device clipboard, or write it by passing text. " +
				"**The platforms are not symmetric.** iOS reads and writes through " +
				"simctl, so a write is confirmed by reading it back. Android can only " +
				"write: on Android 10 and later reading the clipboard requires the " +
				"requesting app to have focus, and the UiAutomator2 server has no " +
				"activity of its own, so it answers with an empty string however full " +
				"the clipboard is. Reporting that as \"empty\" would be a different " +
				"claim and usually a false one, so the read refuses there instead. To " +
				"check an Android write, paste into a field and read the field.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to put on the clipboard. Omit to read it.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_location",
			Description: "Read where the device believes it is, or place it somewhere. " +
				"Takes latitude and longitude, or clear to stop pretending. " +
				"**The two platforms answer different numbers of questions and this says " +
				"which.** Android sets through a test provider and reads the position back " +
				"tagged as injected, so a set can be confirmed — and it works on real " +
				"hardware, which `adb emu geo fix` does not. iOS sets through simctl and " +
				"cannot be read at all: `simctl location` has set, clear, run and start and " +
				"no get, so there the answer confirms the request was accepted and nothing " +
				"more. Reading a position back is only evidence a set took because the reply " +
				"also says the fix was injected: a device holds its last position across " +
				"boots, so asking for a place it already was would otherwise look identical " +
				"to success. Pass waypoints or gpx to move along a route instead: the " +
				"simulator interpolates one itself, while on Android the daemon steps a " +
				"test provider once a second because the platform has no route command.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"latitude": map[string]interface{}{
						"type":        "number",
						"description": "Degrees, -90 to 90. Give longitude too.",
					},
					"longitude": map[string]interface{}{
						"type":        "number",
						"description": "Degrees, -180 to 180. Give latitude too.",
					},
					"clear": map[string]interface{}{
						"type": "boolean",
						"description": "Remove the injected position so the device reports " +
							"its own again. Also stops a running route.",
					},
					"waypoints": map[string]interface{}{
						"type": "array",
						"description": "Two or more [latitude, longitude] pairs to move " +
							"along over time, interpolated between. \"lat,lon\" strings " +
							"are accepted too.",
						"items": map[string]interface{}{},
					},
					"gpx": map[string]interface{}{
						"type": "string",
						"description": "Path to a GPX file to follow instead of waypoints. " +
							"Track points are used first, then route points, then loose " +
							"waypoints.",
					},
					"gpx_data": map[string]interface{}{
						"type": "string",
						"description": "A GPX document's content, instead of gpx's path — for a daemon " +
							"on another machine; the CLI and pipe send it from a path.",
					},
					"speed": map[string]interface{}{
						"type":        "number",
						"description": "Meters per second along a route. Default 15.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_press",
			Description: "Press a hardware button. On Android: \"back\", \"home\", " +
				"\"recents\", \"volume-up\", \"volume-down\". **back is primary " +
				"navigation on Android** — an app that opened a detail screen expects it, " +
				"and no amount of tapping substitutes. iOS has no back button by design, " +
				"and mobium refuses rather than sending an edge swipe, which is a different " +
				"event that apps can tell apart. Any press can move the screen, so the refs " +
				"from the last app_map are discarded; map again before acting. Only home " +
				"reports a confirmed outcome — what back does is the app's business.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"button": map[string]interface{}{
						"type":        "string",
						"description": "Which button. Refused with the list of what this platform has.",
						"enum":        []string{"back", "home", "recents", "volume-up", "volume-down"},
					},
				}),
				"required":             []string{"button"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_lock",
			Description: "Read whether the screen is locked, or lock and unlock it. A state " +
				"rather than a power-button press: power is a toggle, so asking for it twice " +
				"leaves the device where it started and you cannot tell which way it went. " +
				"Both directions are confirmed by reading the device back. A device with a " +
				"PIN, pattern or password cannot be unlocked from outside and says so rather " +
				"than reporting success. Locking discards the refs from the last app_map.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"state": map[string]interface{}{
						"type":        "string",
						"description": "\"lock\" or \"unlock\". Omit to read.",
						"enum":        []string{"lock", "unlock"},
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_session",
			Description: "Start or end the session on a device, as Appium's new session and quit " +
				"do. Every other tool opens a session on first use, so this is never required; " +
				"it is for putting the slow first start — installing UiAutomator2, building " +
				"WebDriverAgent on an iPhone — where you asked for it, and for ending one " +
				"device's session without stopping the daemon. \"start\" opens it (or keeps " +
				"one already open, reported as reused) and, given an app, launches it fresh — " +
				"stopped first if it was running, as Appium does, so the session begins at the " +
				"app's first screen; its data is kept — and waits for it to be in front. \"end\" closes it with the daemon's own teardown: " +
				"accessibility settings put back, a recording or route stopped, WebViews " +
				"detached, the device-side server stopped, and the device's refs and dialog " +
				"rules forgotten — and the app start launched, if it did, is stopped; apps it " +
				"did not launch are left alone. A browser restores its tabs when it next " +
				"launches, so on Android the tabs app_open_url opened in it are closed " +
				"first; Safari's cannot be closed from outside. Ending a session that is " +
				"not open succeeds and says so. " +
				"\"status\", or no action, lists the sessions open.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "\"start\", \"end\" or \"status\". Omit to read the status.",
						"enum":        []string{"start", "end", "status"},
					},
					"platform": map[string]interface{}{
						"type": "string",
						"description": "\"android\" or \"ios\". With start, \"ios\" picks " +
							"wda, so a driver need not be named.",
						"enum": []string{"android", "ios"},
					},
					"app": map[string]interface{}{
						"type":        "string",
						"description": "With start: a package name or bundle id to launch fresh once the session is up — stopped first if running, data kept.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_locale",
			Description: "Read or set the language one app runs in. Per app, not for the " +
				"device: it is reversible, immediate, and it is the question actually being " +
				"asked. Call with just an app to read; pass a language tag such as \"ja-JP\" " +
				"to pin it, or an empty string to follow the device again. Android 13+. " +
				"What is confirmed is that the device stored the tag — whether the app has " +
				"a translation for it is not something Android reports, so check the screen. " +
				"Changing it invalidates the refs from the previous screen, and the app has " +
				"to be relaunched to re-render.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Package name, e.g. \"com.example.shop\".",
					},
					"locale": map[string]interface{}{
						"type": "string",
						"description": "A BCP-47 tag such as \"ja-JP\", or several separated " +
							"by commas in order of preference. Empty string clears the pin. " +
							"Omit to read.",
					},
				}),
				"required":             []string{"app"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_orientation",
			Description: "Read which way the screen is turned, or turn it. Call with no " +
				"argument to read — the answer says both the orientation and whether it is " +
				"pinned, because a screen that happens to be portrait can rotate under you. " +
				"A rotation re-lays out every screen, so a layout that only exists in " +
				"landscape is one nothing has tested. Setting an orientation pins it; " +
				"\"auto\" hands it back to the sensor. Changing it invalidates the refs " +
				"from the previous screen, because bounds do not survive a rotation. An " +
				"activity that locks its own orientation cannot be turned from outside, and " +
				"that is reported rather than silently ignored.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"orientation": map[string]interface{}{
						"type": "string",
						"description": "\"portrait\", \"landscape\", \"portrait-reverse\", " +
							"\"landscape-reverse\", or \"auto\" to follow the sensor. " +
							"Omit to read. Not \"left\"/\"right\": the two platforms " +
							"disagree about which landscape is which.",
						"enum": []string{"portrait", "landscape", "portrait-reverse",
							"landscape-reverse", "auto"},
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_screen",
			Description: "Read the screen, or make an Android device pretend to be a " +
				"different one, and optionally report what is wrong with the layout at " +
				"that size. A flow that works on the screen you happen to have is a flow " +
				"tested once. Call with no argument to read. Pass a profile to apply it, " +
				"or \"reset\" to put the device back — an override is a state left on the " +
				"device and it outlives this session, so reset when done. Applying a " +
				"profile invalidates the refs from the previous screen, because nothing " +
				"is where it was. On iOS the screen is fixed when the simulator is " +
				"created and cannot be changed while it runs, so this reads only and says " +
				"which simulator to boot instead. With inspect=true it also reports " +
				"overflow, touch targets below the platform minimum, text the platform " +
				"truncated, and tappable elements with nothing to announce — each naming " +
				"the element and the measurement.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"profile": map[string]interface{}{
						"type": "string",
						"description": "A screen profile to apply (Android only), or " +
							"\"reset\" to restore the physical screen. Omit to read. " +
							"The answer always lists the profiles this platform knows.",
					},
					"inspect": map[string]interface{}{
						"type": "boolean",
						"description": "Also report layout findings at this screen. " +
							"Note that a touch-target finding can be wrong about a " +
							"working screen: Android's TouchDelegate enlarges a tap " +
							"area without changing the element's bounds, so treat " +
							"those as worth looking at rather than as defects.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_appearance",
			Description: "Read the device's light/dark setting, or change it. Call with no " +
				"argument to read. Dark mode renders every screen differently and is where " +
				"contrast and hard-coded colors break, so it is worth checking a flow in " +
				"both. Android also accepts \"auto\"; iOS does not and says so. Changing it " +
				"invalidates the refs from the previous screen.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"appearance": map[string]interface{}{
						"type": "string",
						"description": "\"light\", \"dark\", or \"auto\" (Android only). " +
							"Omit to read the current setting.",
						"enum": []string{"light", "dark", "auto"},
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_accessibility",
			Description: "Read the device's accessibility settings, or change one for the rest of the " +
				"session: reduce_motion, bold_text, increase_contrast, reduce_transparency, " +
				"button_shapes, differentiate_without_color, invert_colors, grayscale, and text size — " +
				"text_size, a named category, on iOS, and text_scale, a number, on Android. Call with no " +
				"argument to read them all, with a setting to read one, and with a setting and a value " +
				"to change it; each change is confirmed by reading it back and put back exactly as it " +
				"was when the session ends. Settings a platform lacks are refused with the reason. An " +
				"iOS simulator and Android are supported; on a real iPhone nothing outside can change " +
				"these, and the refusal names the Settings route instead. Changing one invalidates the " +
				"refs from the previous screen.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"setting": map[string]interface{}{
						"type":        "string",
						"description": "Which setting. Omit to read them all.",
						"enum": []string{"reduce_motion", "bold_text", "increase_contrast", "reduce_transparency",
							"button_shapes", "differentiate_without_color", "invert_colors", "grayscale",
							"text_size", "text_scale"},
					},
					"value": map[string]interface{}{
						"type": "string",
						"description": "\"on\" or \"off\" for a switch; a category such as \"accessibility-large\" " +
							"for text_size; a number such as \"1.3\" for text_scale. Omit to read.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_list_apps",
			Description: "List the apps installed on the device, with their id, version and " +
				"whether they came with the platform. By default only apps someone installed, " +
				"which is nearly always the question — a stock Android emulator ships about " +
				"240 system packages.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"system": map[string]interface{}{
						"type":        "boolean",
						"description": "Include the platform's own apps as well.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_uninstall",
			Description: "Remove an app from the device. Verified by listing afterwards, " +
				"because `adb uninstall` reports success when it has only removed the updates " +
				"to a system app.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Package name on Android or bundle id on iOS.",
					},
				}),
				"required":             []string{"app"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_dialogs",
			Description: "Declare how to answer a dialog, so an action that meets it carries on: " +
				"when a dialog whose text contains `when` is in the way of a tap, a type or " +
				"any action with a target, the button captioned `press` is pressed, the " +
				"dialog is confirmed gone, and the action continues — and its result says " +
				"which dialog was answered. For dialogs that arrive on their own schedule, " +
				"such as iOS's Save Password after a login. A rule names the button rather " +
				"than accept or dismiss, because which button those press differs by " +
				"platform and by dialog (accept on a three-button iOS alert pressed Cancel). " +
				"Captions match ignoring case. A rule whose button the dialog does not have " +
				"is refused, listing the buttons it does have. With neither argument it " +
				"lists the rules; clear removes them. Rules last for the daemon, per device. " +
				"The share sheet is not a dialog to the platforms' alert detection, so no " +
				"rule answers it.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"when": map[string]interface{}{
						"type":        "string",
						"description": "Text the dialog contains, ignoring case: its title or message.",
					},
					"press": map[string]interface{}{
						"type":        "string",
						"description": "The caption of the button to press, ignoring case.",
					},
					"clear": map[string]interface{}{
						"type":        "boolean",
						"description": "Remove every rule for this device.",
					},
				}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_source",
			Description: "The raw hierarchy, as the device-side server sent it: UiAutomator2's or " +
				"uiautomator's XML on Android, WebDriverAgent's on iOS, and in a WebView context " +
				"the page's current markup. What Appium calls the page source. app_map is what " +
				"to act on — it hands out refs and leaves out what cannot be acted on; this is " +
				"for when map leaves out the thing you need to see. Password fields have their " +
				"contents hidden, keeping the length. Geometry is in the platform's units: " +
				"pixels on Android, but POINTS on iOS, where map, taps and screenshots are in " +
				"pixels — multiply by the answer's scale. Never tap coordinates read from it.",
			InputSchema: map[string]interface{}{
				"type":                 "object",
				"properties":           withDevice(map[string]interface{}{}),
				"additionalProperties": false,
			},
		},
		{
			Name: "app_clear_data",
			Description: "Delete an app's data and leave it installed — the state of a fresh " +
				"install, without reinstalling. The app is stopped first. Android runs `pm clear`, " +
				"which also revokes the runtime permissions the user granted; iOS simulators delete " +
				"the app's preferences and empty its data container, and leave privacy grants and " +
				"the keychain as they were. The answer says what was read back empty and what was " +
				"kept. A real iPhone refuses: nothing can delete from an app's container there, and " +
				"reinstalling is the reset it has.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Package name on Android or bundle id on iOS.",
					},
				}),
				"required":             []string{"app"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_batch",
			Description: "Run several tools in order, on one device, in one call — a known " +
				"sequence such as tap, type, tap, without a round trip for each. Every step is " +
				"the call it would be on its own: {\"name\": a tool, \"arguments\": what that " +
				"tool takes}, resolving its target against a fresh snapshot and waiting as usual. " +
				"Every step is checked before the first runs — an unknown tool, or an argument " +
				"its tool does not take, refuses the whole batch with nothing done. It **stops " +
				"at the first failure**, which is reported with that step's own error code, its " +
				"number, and what ran before it; nothing after it runs, because the screen the " +
				"next step expected is not the one it would meet. The answer lists each step's " +
				"text and data in order. The device is the batch's: give it here, not per step. " +
				"A screenshot with no path comes back as an image after the text. Use it for " +
				"steps whose outcome you do not " +
				"need to see before choosing the next. A ref means what the last app_map said " +
				"when its step runs, and an app_map step inside the batch replaces them, so " +
				"after one prefer locators (\"text=Sign in\") to refs.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"steps": map[string]interface{}{
						"type":        "array",
						"description": "The calls to make, in order.",
						"minItems":    1,
						"maxItems":    maxBatchSteps,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "A tool, e.g. \"app_tap\".",
								},
								"arguments": map[string]interface{}{
									"type":        "object",
									"description": "That tool's arguments, as it would take them called alone, without device or driver.",
								},
							},
							"required":             []string{"name"},
							"additionalProperties": false,
						},
					},
				}),
				"required":             []string{"steps"},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_doctor",
			Description: "Check that the environment mobium needs is present, and say what is " +
				"wrong with it. Needs no device — its whole job is to be runnable when nothing " +
				"works yet. Run it first when a command fails for a reason that does not make " +
				"sense; several of the toolchain's own errors name the wrong cause.",
			InputSchema: map[string]interface{}{
				"type":                 "object",
				"properties":           map[string]interface{}{},
				"additionalProperties": false,
			},
		},
		{
			Name: "app_find",
			Description: "Find elements matching a locator and return their refs, without " +
				"tapping. Locator kinds: text, label, testid, role, class, path. Roles: " +
				strings.Join(roles, ", ") + ".",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": withDevice(map[string]interface{}{
					"locator": map[string]interface{}{
						"type":        "string",
						"description": "For example \"text=Sign In\", \"role=button\", \"testid=submit\".",
					},
				}),
				"required":             []string{"locator"},
				"additionalProperties": false,
			},
		},
	}
}

// ToolNames lists every dispatchable tool, for error messages.
func ToolNames() []string {
	schemas := GetToolSchemas()
	names := make([]string, 0, len(schemas))
	for _, t := range schemas {
		names = append(names, t.Name)
	}
	return names
}

// PathArguments names each tool's arguments that are paths on the caller's
// machine. A relative one is the caller's to resolve — the daemon's working
// directory is wherever it happened to start — so the CLI and `pipe` make
// them absolute before a call leaves the caller's process. A tool that
// takes a new path argument belongs here.
var PathArguments = map[string][]string{
	"app_install":    {"path"},
	"app_screenshot": {"path"},
	"app_location":   {"gpx"},
	"app_record":     {"path"},
}
