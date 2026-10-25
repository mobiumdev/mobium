package codegen

import (
	"fmt"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Each writer below is the same table in one language: a step's tool, the
// client method that calls it, and its arguments in that method's order.
// The spellings are the clients' own, checked against their source by
// TestEveryCallIsAClientMethod, and match the docs site's five translations
// of each tool.

func writePython(b *builder, calls []call) error {
	q := func(s string) string { return quote("python", s) }
	b.line("# " + header)
	b.line("from mobium import start")
	b.line("")
	b.line("with start() as device:")
	b.indent = "    "
	for _, c := range calls {
		a := c.args
		switch c.tool {
		case "app_tap":
			m := "tap"
			if flag(a, "double") {
				m = "double_tap"
			}
			if has(a, "target") {
				b.line("device.%s(%s)", m, q(str(a, "target")))
			} else {
				b.line("device.%s(x=%d, y=%d)", m, num(a, "x"), num(a, "y"))
			}
		case "app_type", "app_fill":
			b.line("device.%s(%s, %s)", strings.TrimPrefix(c.tool, "app_"), q(str(a, "target")), q(str(a, "text")))
		case "app_long_press":
			if has(a, "duration_ms") {
				b.line("device.long_press(%s, duration_ms=%d)", q(str(a, "target")), num(a, "duration_ms"))
			} else {
				b.line("device.long_press(%s)", q(str(a, "target")))
			}
		case "app_set":
			if has(a, "value") && !flag(a, "value") {
				b.line("device.unset(%s)", q(str(a, "target")))
			} else {
				b.line("device.set(%s)", q(str(a, "target")))
			}
		case "app_swipe":
			if has(a, "target") {
				b.line("device.swipe(%s, target=%s)", q(str(a, "direction")), q(str(a, "target")))
			} else {
				b.line("device.swipe(%s)", q(str(a, "direction")))
			}
		case "app_press":
			b.line("device.press(%s)", q(str(a, "button")))
		case "app_scroll_to":
			if has(a, "direction") {
				b.line("device.scroll_to(%s, direction=%s)", q(str(a, "target")), q(str(a, "direction")))
			} else {
				b.line("device.scroll_to(%s)", q(str(a, "target")))
			}
		case "app_wait_for":
			args := []string{q(str(a, "target"))}
			if has(a, "condition") {
				args = append(args, "condition="+q(str(a, "condition")))
			}
			if has(a, "text") {
				args = append(args, "text="+q(str(a, "text")))
			}
			if has(a, "timeout_ms") {
				args = append(args, fmt.Sprintf("timeout_ms=%d", num(a, "timeout_ms")))
			}
			if flag(a, "not") {
				args = append(args, "negate=True")
			}
			if flag(a, "exact") {
				args = append(args, "exact=True")
			}
			b.line("device.wait_for(%s)", strings.Join(args, ", "))
		case "app_launch", "app_terminate":
			b.line("device.%s(%s)", strings.TrimPrefix(c.tool, "app_"), q(str(a, "app")))
		case "app_context":
			b.line("device.context(%s)", q(str(a, "context")))
		case "app_keyboard":
			var args []string
			if has(a, "text") {
				args = append(args, "text="+q(str(a, "text")))
			}
			if has(a, "key") {
				args = append(args, "key="+q(str(a, "key")))
			}
			if flag(a, "hide") {
				args = append(args, "hide=True")
			}
			b.line("device.keyboard(%s)", strings.Join(args, ", "))
		}
	}
	return nil
}

func writeJavaScript(b *builder, calls []call) error {
	q := func(s string) string { return quote("javascript", s) }
	b.line("// " + header)
	b.line("import { start } from 'mobium'")
	b.line("")
	b.line("const device = await start()")
	b.line("try {")
	b.indent = "  "
	for _, c := range calls {
		a := c.args
		switch c.tool {
		case "app_tap":
			m := "tap"
			if flag(a, "double") {
				m = "doubleTap"
			}
			if has(a, "target") {
				b.line("await device.%s(%s)", m, q(str(a, "target")))
			} else {
				b.line("await device.%s({ x: %d, y: %d })", m, num(a, "x"), num(a, "y"))
			}
		case "app_type", "app_fill":
			b.line("await device.%s(%s, %s)", strings.TrimPrefix(c.tool, "app_"), q(str(a, "target")), q(str(a, "text")))
		case "app_long_press":
			if has(a, "duration_ms") {
				b.line("await device.longPress(%s, { durationMs: %d })", q(str(a, "target")), num(a, "duration_ms"))
			} else {
				b.line("await device.longPress(%s)", q(str(a, "target")))
			}
		case "app_set":
			if has(a, "value") && !flag(a, "value") {
				b.line("await device.unset(%s)", q(str(a, "target")))
			} else {
				b.line("await device.set(%s)", q(str(a, "target")))
			}
		case "app_swipe":
			if has(a, "target") {
				b.line("await device.swipe(%s, { target: %s })", q(str(a, "direction")), q(str(a, "target")))
			} else {
				b.line("await device.swipe(%s)", q(str(a, "direction")))
			}
		case "app_press":
			b.line("await device.press(%s)", q(str(a, "button")))
		case "app_scroll_to":
			if has(a, "direction") {
				b.line("await device.scrollTo(%s, { direction: %s })", q(str(a, "target")), q(str(a, "direction")))
			} else {
				b.line("await device.scrollTo(%s)", q(str(a, "target")))
			}
		case "app_wait_for":
			var opts []string
			if has(a, "condition") {
				opts = append(opts, "condition: "+q(str(a, "condition")))
			}
			if has(a, "text") {
				opts = append(opts, "text: "+q(str(a, "text")))
			}
			if has(a, "timeout_ms") {
				opts = append(opts, fmt.Sprintf("timeoutMs: %d", num(a, "timeout_ms")))
			}
			if flag(a, "not") {
				opts = append(opts, "not: true")
			}
			if flag(a, "exact") {
				opts = append(opts, "exact: true")
			}
			if len(opts) == 0 {
				b.line("await device.waitFor(%s)", q(str(a, "target")))
			} else {
				b.line("await device.waitFor(%s, { %s })", q(str(a, "target")), strings.Join(opts, ", "))
			}
		case "app_launch", "app_terminate":
			b.line("await device.%s(%s)", strings.TrimPrefix(c.tool, "app_"), q(str(a, "app")))
		case "app_context":
			b.line("await device.context(%s)", q(str(a, "context")))
		case "app_keyboard":
			var opts []string
			if has(a, "text") {
				opts = append(opts, "text: "+q(str(a, "text")))
			}
			if has(a, "key") {
				opts = append(opts, "key: "+q(str(a, "key")))
			}
			if flag(a, "hide") {
				opts = append(opts, "hide: true")
			}
			b.line("await device.keyboard({ %s })", strings.Join(opts, ", "))
		}
	}
	b.indent = ""
	b.line("} finally {")
	b.line("  await device.quit()")
	b.line("}")
	return nil
}

func writeGo(b *builder, calls []call) error {
	q := func(s string) string { return quote("go", s) }
	var body builder
	body.indent = "\t"
	usesTime := false
	// do writes one call that returns only an error.
	do := func(format string, a ...interface{}) {
		body.line("if err := "+format+"; err != nil {", a...)
		body.line("\treturn err")
		body.line("}")
	}
	// discard writes one call that returns a value and an error.
	discard := func(format string, a ...interface{}) {
		body.line("if _, err := "+format+"; err != nil {", a...)
		body.line("\treturn err")
		body.line("}")
	}
	for _, c := range calls {
		a := c.args
		switch c.tool {
		case "app_tap":
			switch {
			case has(a, "target") && flag(a, "double"):
				do("dev.DoubleTap(ctx, %s)", q(str(a, "target")))
			case has(a, "target"):
				do("dev.Tap(ctx, %s)", q(str(a, "target")))
			case flag(a, "double"):
				do("dev.DoubleTapPoint(ctx, %d, %d)", num(a, "x"), num(a, "y"))
			default:
				do("dev.TapPoint(ctx, %d, %d)", num(a, "x"), num(a, "y"))
			}
		case "app_type":
			do("dev.Type(ctx, %s, %s)", q(str(a, "target")), q(str(a, "text")))
		case "app_fill":
			do("dev.Fill(ctx, %s, %s)", q(str(a, "target")), q(str(a, "text")))
		case "app_long_press":
			if has(a, "duration_ms") {
				usesTime = true
				do("dev.LongPress(ctx, %s, %d*time.Millisecond)", q(str(a, "target")), num(a, "duration_ms"))
			} else {
				do("dev.LongPress(ctx, %s, 0)", q(str(a, "target")))
			}
		case "app_set":
			do("dev.Set(ctx, %s, %t)", q(str(a, "target")), !has(a, "value") || flag(a, "value"))
		case "app_swipe":
			if has(a, "target") {
				do("dev.SwipeOn(ctx, %s, %s)", q(str(a, "target")), q(str(a, "direction")))
			} else {
				do("dev.Swipe(ctx, %s)", q(str(a, "direction")))
			}
		case "app_press":
			do("dev.Press(ctx, %s)", q(str(a, "button")))
		case "app_scroll_to":
			discard("dev.ScrollTo(ctx, %s, %s)", q(str(a, "target")), q(str(a, "direction")))
		case "app_wait_for":
			var opts []string
			if has(a, "condition") {
				opts = append(opts, "Condition: "+q(str(a, "condition")))
			}
			if has(a, "text") {
				opts = append(opts, "Text: "+q(str(a, "text")))
			}
			if has(a, "timeout_ms") {
				usesTime = true
				opts = append(opts, fmt.Sprintf("Timeout: %d * time.Millisecond", num(a, "timeout_ms")))
			}
			if flag(a, "not") {
				opts = append(opts, "Not: true")
			}
			if flag(a, "exact") {
				opts = append(opts, "Exact: true")
			}
			if len(opts) == 0 {
				discard("dev.WaitFor(ctx, %s, nil)", q(str(a, "target")))
			} else {
				discard("dev.WaitFor(ctx, %s, &mobium.WaitOptions{%s})", q(str(a, "target")), strings.Join(opts, ", "))
			}
		case "app_launch":
			do("dev.Launch(ctx, %s)", q(str(a, "app")))
		case "app_terminate":
			do("dev.Terminate(ctx, %s)", q(str(a, "app")))
		case "app_context":
			discard("dev.Context(ctx, %s)", q(str(a, "context")))
		case "app_keyboard":
			var opts []string
			if has(a, "key") {
				opts = append(opts, "Key: "+q(str(a, "key")))
			}
			if flag(a, "hide") {
				opts = append(opts, "Hide: true")
			}
			if !has(a, "text") {
				discard("dev.Keyboard(ctx, mobium.KeyboardOptions{%s})", strings.Join(opts, ", "))
				break
			}
			// Text is a pointer, so "" can be told from no text.
			opts = append([]string{"Text: &text"}, opts...)
			body.line("{")
			body.indent = "\t\t"
			body.line("text := %s", q(str(a, "text")))
			discard("dev.Keyboard(ctx, mobium.KeyboardOptions{%s})", strings.Join(opts, ", "))
			body.indent = "\t"
			body.line("}")
		}
	}

	b.line("// " + header)
	b.line("package main")
	b.line("")
	b.line("import (")
	b.line("\t\"context\"")
	b.line("\t\"log\"")
	if usesTime {
		b.line("\t\"time\"")
	}
	b.line("")
	b.line("\tmobium \"github.com/mobiumdev/mobium/clients/go\"")
	b.line(")")
	b.line("")
	b.line("func main() {")
	b.line("\tif err := run(context.Background()); err != nil {")
	b.line("\t\tlog.Fatal(err)")
	b.line("\t}")
	b.line("}")
	b.line("")
	b.line("func run(ctx context.Context) error {")
	b.line("\tdev, err := mobium.Start(ctx)")
	b.line("\tif err != nil {")
	b.line("\t\treturn err")
	b.line("\t}")
	b.line("\tdefer dev.Quit(ctx)")
	b.WriteString(body.String())
	b.line("\treturn nil")
	b.line("}")
	return nil
}

func writeJava(b *builder, calls []call) error {
	q := func(s string) string { return quote("java", s) }
	var body builder
	body.indent = "            "
	usesDuration, usesWaitFor := false, false
	for _, c := range calls {
		a := c.args
		switch c.tool {
		case "app_tap":
			m := "tap"
			if flag(a, "double") {
				m = "doubleTap"
			}
			if has(a, "target") {
				body.line("device.%s(%s);", m, q(str(a, "target")))
			} else {
				body.line("device.%s(%d, %d);", m, num(a, "x"), num(a, "y"))
			}
		case "app_type", "app_fill":
			body.line("device.%s(%s, %s);", strings.TrimPrefix(c.tool, "app_"), q(str(a, "target")), q(str(a, "text")))
		case "app_long_press":
			if has(a, "duration_ms") {
				usesDuration = true
				body.line("device.longPress(%s, Duration.ofMillis(%d));", q(str(a, "target")), num(a, "duration_ms"))
			} else {
				body.line("device.longPress(%s);", q(str(a, "target")))
			}
		case "app_set":
			body.line("device.set(%s, %t);", q(str(a, "target")), !has(a, "value") || flag(a, "value"))
		case "app_swipe":
			if has(a, "target") {
				body.line("device.swipe(%s, %s);", q(str(a, "target")), q(str(a, "direction")))
			} else {
				body.line("device.swipe(%s);", q(str(a, "direction")))
			}
		case "app_press":
			body.line("device.press(%s);", q(str(a, "button")))
		case "app_scroll_to":
			if has(a, "direction") {
				body.line("device.scrollTo(%s, %s);", q(str(a, "target")), q(str(a, "direction")))
			} else {
				body.line("device.scrollTo(%s);", q(str(a, "target")))
			}
		case "app_wait_for":
			w, err := javaLikeWait(a, q, "WaitFor", "visible", "hidden", "text", "exactText", "value", "timeout",
				"Duration.ofMillis(%d)", "not")
			if err != nil {
				return err
			}
			if w == "" {
				body.line("device.waitFor(%s);", q(str(a, "target")))
			} else {
				usesWaitFor = true
				usesDuration = usesDuration || has(a, "timeout_ms")
				body.line("device.waitFor(%s, %s);", q(str(a, "target")), w)
			}
		case "app_launch", "app_terminate":
			body.line("device.%s(%s);", strings.TrimPrefix(c.tool, "app_"), q(str(a, "app")))
		case "app_context":
			body.line("device.context(%s);", q(str(a, "context")))
		case "app_keyboard":
			text, key := "null", "null"
			if has(a, "text") {
				text = q(str(a, "text"))
			}
			if has(a, "key") {
				key = q(str(a, "key"))
			}
			body.line("device.keyboard(%s, %s, %t);", text, key, flag(a, "hide"))
		}
	}
	b.line("// " + header)
	b.line("import dev.mobium.Mobium;")
	if usesWaitFor {
		b.line("import dev.mobium.WaitFor;")
	}
	if usesDuration {
		b.line("import java.time.Duration;")
	}
	b.line("")
	b.line("public class Recorded {")
	b.line("    public static void main(String[] args) {")
	b.line("        try (Mobium device = Mobium.builder().start()) {")
	b.WriteString(body.String())
	b.line("        }")
	b.line("    }")
	b.line("}")
	return nil
}

func writeDotnet(b *builder, calls []call) error {
	q := func(s string) string { return quote("dotnet", s) }
	var body builder
	usesSystem := false
	for _, c := range calls {
		a := c.args
		switch c.tool {
		case "app_tap":
			m := "Tap"
			if flag(a, "double") {
				m = "DoubleTap"
			}
			if has(a, "target") {
				body.line("device.%s(%s);", m, q(str(a, "target")))
			} else {
				body.line("device.%s(%d, %d);", m, num(a, "x"), num(a, "y"))
			}
		case "app_type":
			body.line("device.Type(%s, %s);", q(str(a, "target")), q(str(a, "text")))
		case "app_fill":
			body.line("device.Fill(%s, %s);", q(str(a, "target")), q(str(a, "text")))
		case "app_long_press":
			if has(a, "duration_ms") {
				usesSystem = true
				body.line("device.LongPress(%s, TimeSpan.FromMilliseconds(%d));", q(str(a, "target")), num(a, "duration_ms"))
			} else {
				body.line("device.LongPress(%s);", q(str(a, "target")))
			}
		case "app_set":
			if has(a, "value") && !flag(a, "value") {
				body.line("device.Unset(%s);", q(str(a, "target")))
			} else {
				body.line("device.Set(%s);", q(str(a, "target")))
			}
		case "app_swipe":
			if has(a, "target") {
				body.line("device.Swipe(%s, %s);", q(str(a, "target")), q(str(a, "direction")))
			} else {
				body.line("device.Swipe(%s);", q(str(a, "direction")))
			}
		case "app_press":
			body.line("device.Press(%s);", q(str(a, "button")))
		case "app_scroll_to":
			if has(a, "direction") {
				body.line("device.ScrollTo(%s, %s);", q(str(a, "target")), q(str(a, "direction")))
			} else {
				body.line("device.ScrollTo(%s);", q(str(a, "target")))
			}
		case "app_wait_for":
			w, err := javaLikeWait(a, q, "Until", "Visible", "Hidden", "Text", "ExactText", "Value", "Timeout",
				"TimeSpan.FromMilliseconds(%d)", "Not")
			if err != nil {
				return err
			}
			if w == "" {
				body.line("device.WaitFor(%s);", q(str(a, "target")))
			} else {
				usesSystem = usesSystem || has(a, "timeout_ms")
				body.line("device.WaitFor(%s, %s);", q(str(a, "target")), w)
			}
		case "app_launch":
			body.line("device.Launch(%s);", q(str(a, "app")))
		case "app_terminate":
			body.line("device.Terminate(%s);", q(str(a, "app")))
		case "app_context":
			body.line("device.Context(%s);", q(str(a, "context")))
		case "app_keyboard":
			var args []string
			if has(a, "text") {
				args = append(args, "text: "+q(str(a, "text")))
			}
			if has(a, "key") {
				args = append(args, "key: "+q(str(a, "key")))
			}
			if flag(a, "hide") {
				args = append(args, "hide: true")
			}
			body.line("device.Keyboard(%s);", strings.Join(args, ", "))
		}
	}
	b.line("// " + header)
	if usesSystem {
		b.line("using System;")
	}
	b.line("using Mobium;")
	b.line("")
	b.line("using var device = Device.Builder().Start();")
	b.WriteString(body.String())
	return nil
}

// javaLikeWait writes a wait's options as Java's WaitFor or .NET's Until:
// a factory named for the condition, then .timeout and .not. "" means the
// default, a visible wait of ten seconds, written as the plain call.
func javaLikeWait(a map[string]interface{}, q func(string) string, class, visible, hidden, text, exactText,
	value, timeout, duration, not string) (string, error) {
	cond := str(a, "condition")
	if cond == "" {
		cond = "visible"
	}
	if has(a, "text") && cond != "text" && cond != "value" {
		return "", mobiumerr.New(mobiumerr.Unsupported, "a wait's text goes with the text or value condition, not %s", cond)
	}
	var w string
	switch cond {
	case "visible":
		w = class + "." + visible + "()"
	case "hidden":
		w = class + "." + hidden + "()"
	case "text":
		if flag(a, "exact") {
			w = fmt.Sprintf("%s.%s(%s)", class, exactText, q(str(a, "text")))
		} else {
			w = fmt.Sprintf("%s.%s(%s)", class, text, q(str(a, "text")))
		}
	case "value":
		w = fmt.Sprintf("%s.%s(%s)", class, value, q(str(a, "text")))
	default:
		// enabled, disabled, checked, unchecked, selected, unselected, focused:
		// each a factory of its own name.
		name := cond
		if class == "Until" {
			name = strings.ToUpper(cond[:1]) + cond[1:]
		}
		w = class + "." + name + "()"
	}
	if has(a, "timeout_ms") {
		w += "." + timeout + "(" + fmt.Sprintf(duration, num(a, "timeout_ms")) + ")"
	}
	if flag(a, "not") {
		w += "." + not + "()"
	}
	if w == class+"."+visible+"()" {
		return "", nil
	}
	return w, nil
}
