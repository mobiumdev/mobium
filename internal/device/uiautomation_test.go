package device

import "testing"

// ps -A -o PID,USER,ARGS on the Pixel 7 AVD, with another tool's device
// server left running by the shell, beside UiAutomator2's own server, which
// is an app's and holds UiAutomation legitimately for mobium.
func TestUIAutomationHolders(t *testing.T) {
	ps := "  PID USER         ARGS\n" +
		"    1 root         init second_stage\n" +
		" 3767 u0_a213      com.example.app\n" +
		" 5120 u0_a219      io.appium.uiautomator2.server\n" +
		" 6158 shell        app_process / com.example.automation.DeviceServer\n" +
		" 6201 shell        sh -c ps -A -o PID,USER,ARGS\n" +
		" 6202 shell        uiautomator dump /data/local/tmp/x.xml\n"
	got := parseUIAutomationHolders(ps)
	if len(got) != 2 || got[0].PID != 6158 || got[1].PID != 6202 {
		t.Errorf("holders: %+v", got)
	}
	if len(parseUIAutomationHolders("  PID USER ARGS\n 5120 u0_a219 io.appium.uiautomator2.server\n")) != 0 {
		t.Error("UiAutomator2's own server was taken for another tool's")
	}
}
