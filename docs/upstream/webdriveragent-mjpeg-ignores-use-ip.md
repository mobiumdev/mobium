# WebDriverAgent: the MJPEG stream ignores `USE_IP`

Written for [appium/WebDriverAgent](https://github.com/appium/WebDriverAgent).
**Not filed yet.** Found by Mobium's threat model; CHALLENGES 152 and 153.

## Summary

`USE_IP` binds WebDriverAgent's HTTP server to one interface, but its MJPEG
screen stream still listens on every interface. With `USE_IP=127.0.0.1` on a
simulator, or the CoreDevice tunnel's address on a real iPhone, the control
server is off the network while anyone on the same network can still watch
the screen, unauthenticated, for as long as the runner runs.

- **Affected:** v16.12.8 (`3e8aa7de`); nothing in later code was checked.
- **Where:** simulators (the Mac's interfaces) and real devices (the phone's
  Wi-Fi).

## Root cause

`WebDriverAgentLib/Routing/FBWebServer.m`. `-startHTTPServer` reads
`FBConfiguration.sharedInstance.bindingIPAddress` (set from `USE_IP`) and
passes it to the HTTP server with `setInterface:`. `-initScreenshotsBroadcaster`
creates the stream's `FBTCPSocket` with only a port, and never sets its
`interface` property:

```objc
self.screenshotsBroadcaster = [[FBTCPSocket alloc]
                               initWithPort:(uint16_t)FBConfiguration.sharedInstance.mjpegServerPort];
self.mjpegServer.socket = self.screenshotsBroadcaster;
```

`FBTCPSocket` already supports one interface — `-startWithError:` builds a
local endpoint from `self.interface` when it is set, and listens on every
interface only when it is nil — so the stream is unbound only because nothing
tells it otherwise.

## Reproduction

On a simulator, from a Mac:

```sh
# Launch the installed runner with its server bound to loopback. simctl
# passes SIMCTL_CHILD_X to the app as X.
SIMCTL_CHILD_USE_IP=127.0.0.1 SIMCTL_CHILD_USE_PORT=8100 SIMCTL_CHILD_MJPEG_SERVER_PORT=9100 \
  xcrun simctl launch <udid> com.facebook.WebDriverAgentRunner.xctrunner

lsof -nP -iTCP -sTCP:LISTEN | grep WebDriver
#   WebDriver ... TCP 127.0.0.1:8100 (LISTEN)     <- as asked
#   WebDriver ... TCP *:9100 (LISTEN)             <- every interface

# From another device on the same network (not the Mac itself: traffic to
# your own address never reaches a firewall, or anything else):
curl -sD - -o /dev/null http://<mac-lan-address>:9100/     # HTTP/1.0 200 OK, a live JPEG stream
curl -s -o /dev/null -w '%{http_code}' http://<mac-lan-address>:8100/status   # refused
```

On a real iPhone, with the runner started by `xcodebuild test-without-building`
and `TEST_RUNNER_USE_IP=<tunnel address>`: the server answers on the tunnel
and not on the phone's Wi-Fi address, and port 9100 answers on both.

**Measured** 2026-09-28, iPhone 17 Pro simulator (iOS 26.5) and iPhone 15 Plus
(iOS 26.6.2), from the Mac and from a second device on the same Wi-Fi: an
iPhone's Safari loaded a live 1206×2622 frame of the simulator's screen from
the stream while the server refused the same address.

**Expected:** with `USE_IP` set, both listeners bind that address.

## Patch

Against v16.12.8:

```diff
--- a/WebDriverAgentLib/Routing/FBWebServer.m
+++ b/WebDriverAgentLib/Routing/FBWebServer.m
@@ -133,6 +133,7 @@
   self.mjpegServer = [[FBMjpegServer alloc] init];
   self.screenshotsBroadcaster = [[FBTCPSocket alloc]
                                  initWithPort:(uint16_t)FBConfiguration.sharedInstance.mjpegServerPort];
+  self.screenshotsBroadcaster.interface = FBConfiguration.sharedInstance.bindingIPAddress;
   self.mjpegServer.socket = self.screenshotsBroadcaster;
   self.screenshotsBroadcaster.delegate = self.mjpegServer;
   NSError *error;
```

When `USE_IP` is unset, `bindingIPAddress` is nil and the stream listens on
every interface as before, so nothing changes for anyone not asking.

**Tested:** built with this patch for the iPhone 15 Plus; with the runner
started on the tunnel's address, ports 8100 and 9100 both answered over the
tunnel and both refused the phone's Wi-Fi address; Mobium's full device check
(`docs/checks/ios-device.sh`) passed.

## What Mobium does meanwhile

- **Real iPhone:** Mobium builds the runner from the pinned source and applies
  this line before building (`internal/device/wdapatch.go`), refusing to
  build if the line it attaches to is gone.
- **Simulator:** Mobium installs the prebuilt release, which it does not
  patch. The server is bound with `USE_IP=127.0.0.1`; the stream is left to
  the Mac's firewall. A runner built for the simulator from source could not
  be launched the way Mobium launches the prebuilt one — `simctl launch`
  aborted it with `Library not loaded: @rpath/lib_TestingInterop.dylib`,
  which Xcode 26 supplies only when `xcodebuild` runs the test.
