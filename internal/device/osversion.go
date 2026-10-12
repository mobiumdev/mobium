package device

import (
	"context"
	"fmt"
	"strings"
)

// OSVersion is the device's Android release and API level, as "Android 15
// (API 35)" — what an iOS device's runtime already says of it. No tool said
// which Android a device ran, and an agent asked for it concluded twice that
// it could not be found (CHALLENGES 304).
func (a *ADB) OSVersion(ctx context.Context) (string, error) {
	out, err := a.Shell(ctx, "getprop ro.build.version.release; getprop ro.build.version.sdk")
	if err != nil {
		return "", err
	}
	return osVersion(string(out)), nil
}

func osVersion(out string) string {
	lines := strings.Fields(out)
	if len(lines) < 2 || lines[0] == "" {
		return ""
	}
	return fmt.Sprintf("Android %s (API %s)", lines[0], lines[1])
}
