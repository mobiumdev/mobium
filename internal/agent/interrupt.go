package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// call is app_call: a simulated incoming call.
//
// Interruption is where mobile apps fail and desktop software largely does
// not — a call arriving mid-form, state lost on resume, a callback that never
// fires. None of it can be provoked by tapping, which is why it needs a tool
// at all.
func (h *Handlers) call(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.callOn(ctx, s, args)
}

func (h *Handlers) callOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsInterruptions(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapInterruptions, "simulate calls")
	}

	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action")))
	if action == "" {
		action = mobiumdriver.CallRing
	}
	valid := false
	for _, a := range mobiumdriver.CallActions() {
		if a == action {
			valid = true
			break
		}
	}
	if !valid {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown call action %q (want \"ring\", \"accept\" or \"hang\")", action)
	}

	number := stringArg(args, "number")
	if err := ctrl.Call(ctx, action, number); err != nil {
		return nil, err
	}
	// A call takes over the screen, so nothing from the last map survives it.
	delete(h.refs, s.dev.Serial)

	if number == "" {
		number = "5551234"
	}
	return Result(fmt.Sprintf("%s a call from %s", callVerb(action), number),
		CallView{Action: action, Number: number, Device: s.dev.Serial}), nil
}

func callVerb(action string) string {
	switch action {
	case mobiumdriver.CallAccept:
		return "answered"
	case mobiumdriver.CallHang:
		return "ended"
	}
	return "ringing"
}

// CallView is the result of app_call.
type CallView struct {
	Action string `json:"action"`
	Number string `json:"number"`
	Device string `json:"device"`
}

// sms is app_sms: a simulated incoming text message.
func (h *Handlers) sms(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.smsOn(ctx, s, args)
}

func (h *Handlers) smsOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsInterruptions(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapInterruptions, "simulate messages")
	}
	text := stringArg(args, "text")
	if text == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_sms needs the message text")
	}
	from := stringArg(args, "from")
	if err := ctrl.SendSMS(ctx, from, text); err != nil {
		return nil, err
	}
	if from == "" {
		from = "5551234"
	}
	// Delivery is confirmed by the emulator console accepting it; whether the
	// messaging app showed anything is that app's business and is visible in
	// the next map, which is why the refs go.
	delete(h.refs, s.dev.Serial)
	return Result(fmt.Sprintf("delivered a message from %s", from),
		SMSView{From: from, Text: text, Device: s.dev.Serial}), nil
}

// SMSView is the result of app_sms.
type SMSView struct {
	From   string `json:"from"`
	Text   string `json:"text"`
	Device string `json:"device"`
}

// timezone is app_timezone: read or change what time the device thinks it is.
//
// Unlike calls this works on real hardware, and the checklist calls time-based
// interruptions particularly revealing — a clock that jumps a zone mid-session
// breaks scheduling, caching and anything that stored a local timestamp.
func (h *Handlers) timezone(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.timezoneOn(ctx, s, args)
}

func (h *Handlers) timezoneOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsClock(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapClock, "read or change the timezone")
	}

	before, err := ctrl.Timezone(ctx)
	if err != nil {
		return nil, err
	}
	want := strings.TrimSpace(stringArg(args, "timezone"))
	if want == "" {
		return Result(before, TimezoneView{Timezone: before, Device: s.dev.Serial}), nil
	}
	if want == before {
		return Result(fmt.Sprintf("already %s", want),
			TimezoneView{Timezone: want, Device: s.dev.Serial}), nil
	}
	if err := ctrl.SetTimezone(ctx, want); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)
	msg := fmt.Sprintf("%s (was %s)", want, before)
	if s.backend == BackendWDA {
		// Said because it is not Android's device-wide change: iOS has none
		// that can be made from outside, so it is the launch environment.
		msg += " — on iOS for the apps Mobium launches in this session, as TZ in their environment; " +
			"the app in front was launched again in it, and setting the device's own zone ends it"
	}
	return Result(msg, TimezoneView{Timezone: want, Previous: before, Device: s.dev.Serial}), nil
}

// TimezoneView is the result of app_timezone.
type TimezoneView struct {
	Timezone string `json:"timezone"`
	Previous string `json:"previous,omitempty"`
	Device   string `json:"device"`
}

// notifications is app_notifications: read the shade, post to it, or open it.
//
// One tool with three jobs because they are the same subject and an agent
// reaches for them together: assert what an app posted, interrupt it with
// something else, then open the shade to tap what arrived.
func (h *Handlers) notifications(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.notificationsOn(ctx, s, args)
}

func (h *Handlers) notificationsOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsNotifications(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapNotifications, "read or post notifications")
	}

	shade := strings.ToLower(strings.TrimSpace(stringArg(args, "shade")))
	switch shade {
	case "", "open", "close":
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown shade %q (want \"open\" or \"close\")", shade)
	}

	title := stringArg(args, "title")
	text := stringArg(args, "text")
	if text != "" || title != "" {
		if title == "" {
			title = "Mobium"
		}
		tag := stringArg(args, "tag")
		// Confirmed by reading the shade back: `cmd notification post` prints
		// the notification it thinks it built and says nothing about whether
		// the system accepted it.
		if err := ctrl.PostNotification(ctx, tag, title, text); err != nil {
			return nil, err
		}
	}

	if shade != "" {
		if err := ctrl.SetShade(ctx, shade == "open"); err != nil {
			return nil, err
		}
		// The shade covers the app, so nothing from the last map survives it.
		delete(h.refs, s.dev.Serial)
	}

	list, err := ctrl.Notifications(ctx)
	if err != nil {
		return nil, err
	}

	view := NotificationsView{Notifications: list, Device: s.dev.Serial}
	if len(list) == 0 {
		return Result("no notifications", view), nil
	}
	lines := make([]string, 0, len(list))
	for _, n := range list {
		line := n.Package
		if n.Title != "" {
			line += "  " + n.Title
		}
		if n.Text != "" {
			line += " — " + n.Text
		}
		lines = append(lines, line)
	}
	return Result(strings.Join(lines, "\n"), view), nil
}

// NotificationsView is the result of app_notifications.
type NotificationsView struct {
	Notifications []device.Notification `json:"notifications"`
	Device        string                `json:"device"`
}
