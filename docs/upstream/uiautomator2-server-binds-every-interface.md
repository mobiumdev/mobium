# UiAutomator2 server: no way to keep it off the device's network

Written for [appium/appium-uiautomator2-server](https://github.com/appium/appium-uiautomator2-server).
**Not filed yet.** Found by Mobium's threat model; CHALLENGES 154.

## Summary

The UiAutomator2 server and its MJPEG stream listen on every interface of the
device, and nothing chooses otherwise. On a real phone on Wi-Fi, anyone on the
same network can read the screen and drive the phone while the server runs —
tap, type, open apps — with no credential, since the protocol has none.
A client that reaches the server through `adb forward` does not need that,
since adb connects to the device's loopback. Mobium does; whether every other
client does was not checked here, which is why the patch leaves the default
alone.

- **Affected:** v10.6.6 (`4a813916`); nothing in later code was checked.
- **Where:** real devices on a network. An emulator's network is behind its
  own NAT, which hides the same binding.

## Root cause

- `app/src/main/java/io/appium/uiautomator2/http/HttpServer.java` binds with
  `bootstrap.bind(port)`, Netty's every-interface form.
- `app/src/main/java/io/appium/uiautomator2/server/mjpeg/MjpegScreenshotServer.java`
  opens `new ServerSocket(port)`, likewise every interface.
- `ServerConfig` reads the ports (`SERVER_PORT`, `MJPEG_SERVER_PORT`) and has
  no address to read.

## Reproduction

With the server installed as the Appium driver installs it, on a phone on the
same Wi-Fi as a second machine:

```sh
adb shell am instrument -w -e disableAnalytics true \
  io.appium.uiautomator2.server.test/androidx.test.runner.AndroidJUnitRunner &

adb shell ss -tln | grep -E ':(6790|7810)'
#   *:6790   *:7810                              <- every interface

# From the second machine, at the phone's Wi-Fi address:
curl -s http://<phone-wifi-address>:6790/status  # {"value":{"ready":true,"message":"UiAutomator2 Server is ready to accept commands",...}}
curl -sD - -o /dev/null http://<phone-wifi-address>:7810/   # HTTP/1.0 200 OK
```

**Measured** 2026-09-28 on a Pixel 8 Pro, Android 17, from a Mac on the same
Wi-Fi: both answered, during a session and not after it. On a Pixel 7 AVD,
Android 15, the same binding answered on the device's own network address.

**Expected:** a way to bind both listeners to one address — in practice
`127.0.0.1`, which loses nothing for a client that comes through `adb forward`.

## Patch

Against v10.6.6. A `serverHost` instrumentation argument (or `SERVER_HOST` in
the environment, next to the existing `SERVER_PORT`); unset, both servers
bind every interface as before.

```diff
diff --git a/app/src/main/java/io/appium/uiautomator2/http/HttpServer.java b/app/src/main/java/io/appium/uiautomator2/http/HttpServer.java
index c6f5dc0..7f93d64 100644
--- a/app/src/main/java/io/appium/uiautomator2/http/HttpServer.java
+++ b/app/src/main/java/io/appium/uiautomator2/http/HttpServer.java
@@ -19,6 +19,7 @@ package io.appium.uiautomator2.http;
 import java.util.ArrayList;
 import java.util.List;
 
+import io.appium.uiautomator2.server.ServerConfig;
 import io.netty.bootstrap.ServerBootstrap;
 import io.netty.channel.Channel;
 import io.netty.channel.ChannelOption;
@@ -58,7 +59,9 @@ public class HttpServer {
                             .option(ChannelOption.TCP_NODELAY, true)
                             .childHandler(new ServerInitializer(handlers));
 
-                    Channel ch = bootstrap.bind(port).sync().channel();
+                    String host = ServerConfig.getServerHost();
+                    Channel ch = (host == null ? bootstrap.bind(port) : bootstrap.bind(host, port))
+                            .sync().channel();
                     ch.closeFuture().sync();
                 } catch (InterruptedException ignored) {
                 } finally {
diff --git a/app/src/main/java/io/appium/uiautomator2/server/ServerConfig.java b/app/src/main/java/io/appium/uiautomator2/server/ServerConfig.java
index 08557d4..84469a1 100644
--- a/app/src/main/java/io/appium/uiautomator2/server/ServerConfig.java
+++ b/app/src/main/java/io/appium/uiautomator2/server/ServerConfig.java
@@ -16,6 +16,9 @@
 
 package io.appium.uiautomator2.server;
 
+import androidx.annotation.Nullable;
+import androidx.test.platform.app.InstrumentationRegistry;
+
 import java.util.HashMap;
 import java.util.Map;
 import java.util.Optional;
@@ -142,4 +145,28 @@ public class ServerConfig {
             MjpegBilinearFiltering.SETTING_NAME,
             mjpegBilinearFiltering);
     }
+
+    /**
+     * The local address the HTTP and MJPEG servers bind to, or null to bind to
+     * every interface, as they always have. Read from the instrumentation
+     * argument "serverHost" ({@code am instrument -e serverHost 127.0.0.1}),
+     * or else the SERVER_HOST environment variable.
+     *
+     * Binding to 127.0.0.1 keeps both servers off the device's network: a
+     * client that reaches the server through {@code adb forward} is still
+     * served, since adb connects to the device's loopback.
+     */
+    @Nullable
+    public static String getServerHost() {
+        String host = null;
+        try {
+            host = InstrumentationRegistry.getArguments().getString("serverHost");
+        } catch (IllegalStateException ignored) {
+            // Not running under instrumentation, as in unit tests.
+        }
+        if (host == null || host.isEmpty()) {
+            host = System.getenv("SERVER_HOST");
+        }
+        return host == null || host.isEmpty() ? null : host;
+    }
 }
diff --git a/app/src/main/java/io/appium/uiautomator2/server/mjpeg/MjpegScreenshotServer.java b/app/src/main/java/io/appium/uiautomator2/server/mjpeg/MjpegScreenshotServer.java
index 6301b2f..884e198 100644
--- a/app/src/main/java/io/appium/uiautomator2/server/mjpeg/MjpegScreenshotServer.java
+++ b/app/src/main/java/io/appium/uiautomator2/server/mjpeg/MjpegScreenshotServer.java
@@ -17,11 +17,13 @@
 package io.appium.uiautomator2.server.mjpeg;
 
 import java.io.IOException;
+import java.net.InetAddress;
 import java.net.ServerSocket;
 import java.util.ArrayList;
 import java.util.List;
 import java.util.Locale;
 
+import io.appium.uiautomator2.server.ServerConfig;
 import io.appium.uiautomator2.utils.Logger;
 
 public class MjpegScreenshotServer extends Thread {
@@ -50,7 +52,10 @@ public class MjpegScreenshotServer extends Thread {
     @Override
     public void run() {
         try {
-            serverSocket = new ServerSocket(port);
+            String host = ServerConfig.getServerHost();
+            serverSocket = host == null
+                    ? new ServerSocket(port)
+                    : new ServerSocket(port, 0, InetAddress.getByName(host));
             Logger.info(String.format(
                 Locale.ROOT,
                 "ServerSocket created on port %d", port));
```

Instrumentation arguments rather than the environment, because
`am instrument` cannot set the server process's environment; `-e` is how a
client already passes `disableAnalytics`.

**Tested** on a Pixel 7 AVD (Android 15), built with `./gradlew
assembleServerDebug assembleServerDebugAndroidTest` and installed in place of
the release:

| Started with | Listens on | From the device's own address | Through `adb forward` |
| --- | --- | --- | --- |
| no argument | `*:6790`, `*:7810` | answers | answers |
| `-e serverHost 127.0.0.1` | `127.0.0.1:6790`, `127.0.0.1:7810` | refused | answers |

Not tested on a real phone, and the project's own test suite was not run.

## What Mobium does meanwhile

Mobium installs the release APK, pinned by checksum, and cannot bind it.
When a session opens on a real Android phone with this server, its answer
says so — the server answers on the phone's network until the session ends —
and names `--driver uiautomator`, which drives the phone through
`uiautomator dump` over adb and listens on nothing: measured on the Pixel 8
Pro, a dump session added no listening socket where this server added two.
When a release has a bind setting, Mobium passes `127.0.0.1` and the note
goes.
