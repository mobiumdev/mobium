package device

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// CurrentUser is the Android user in the foreground. It is 0 on a phone, and
// 10 on Android Automotive, where user 0 runs only the system and the driver
// is user 10. `pm` and `dumpsys package` act for or list user 0 first unless
// told otherwise (CHALLENGES 397). A device that cannot say is taken to be
// user 0, as every command here assumed before.
func (a *ADB) CurrentUser(ctx context.Context) int {
	out, err := a.Shell(ctx, "am", "get-current-user")
	if err != nil {
		return 0
	}
	return parseCurrentUser(string(out))
}

func parseCurrentUser(out string) int {
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// userArgs is `--user N` for a `pm` subcommand when the foreground user is not
// user 0, and nothing otherwise, so a phone runs the same command as before.
func (a *ADB) userArgs(ctx context.Context) (args []string, user int) {
	user = a.CurrentUser(ctx)
	if user == 0 {
		return nil, 0
	}
	return []string{"--user", strconv.Itoa(user)}, user
}

// pmArgs is a `pm` command for the foreground user: the subcommand, then
// userArgs, then the rest.
func (a *ADB) pmArgs(ctx context.Context, sub string, rest ...string) []string {
	user, _ := a.userArgs(ctx)
	return append(append([]string{"pm", sub}, user...), rest...)
}

// packageDump is `dumpsys package <pkg>` cut to start at the foreground
// user's section, so a parser that takes the first runtime-permissions block
// reads that user's. User 0's section comes first, so the dump is unchanged
// for it. A dump with no section for the user is refused rather than read as
// user 0's.
func (a *ADB) packageDump(ctx context.Context, pkg string) (string, error) {
	out, err := a.Shell(ctx, "dumpsys", "package", shellQuote(pkg))
	if err != nil {
		return "", err
	}
	dump := string(out)
	if strings.Contains(dump, "Unable to find package") {
		return dump, nil
	}
	user := a.CurrentUser(ctx)
	section, ok := userSection(dump, user)
	if !ok {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed for user %d, the one in front", pkg, user).
			WithRemedy("app_install installs it for every user")
	}
	return section, nil
}

var userHeaderRe = regexp.MustCompile(`(?m)^\s*User (\d+):`)

// userSection is dump from the first line that opens user's section, and
// whether there is one. User 0 is the whole dump.
func userSection(dump string, user int) (string, bool) {
	if user == 0 {
		return dump, true
	}
	for _, m := range userHeaderRe.FindAllStringSubmatchIndex(dump, -1) {
		if dump[m[2]:m[3]] == strconv.Itoa(user) {
			return dump[m[0]:], true
		}
	}
	return "", false
}
