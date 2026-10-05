// Mobium quick start: start a session, drive Settings, quit.
//
//   MOBIUM_PLATFORM=android dotnet run    # or ios
using System;
using System.Collections.Generic;
using System.Linq;
using Mobium;

// Settings is on every emulator, simulator and phone, with nothing to install.
var platforms = new Dictionary<string, (string App, string Row, string Next)>
{
    ["android"] = ("com.android.settings", "Network & internet", "text=Airplane mode"),
    ["ios"] = ("com.apple.Preferences", "General", "label=About,role=button"),
};
var platform = Environment.GetEnvironmentVariable("MOBIUM_PLATFORM") ?? "android";
var p = platforms[platform];

var builder = Device.Builder().Platform(platform).App(p.App);
var serial = Environment.GetEnvironmentVariable("MOBIUM_DEVICE");
if (serial != null) builder.OnDevice(serial);

// 1. Start the session: the driver is started on the device and Settings is
//    launched. The using block quits when it ends, even on an exception.
using (var device = builder.Start())
{
    var s = device.Session!;
    Console.WriteLine($"session on {s.Device} ({s.Platform}, {s.Driver})");

    // 2. Map the screen: every element you can act on, each with a @ref.
    var elements = device.Map();
    foreach (var e in elements.Take(5)) Console.WriteLine("  " + e);

    // 3. Tap a row by its ref, then wait for the screen it opens. A row's
    //    label can carry its summary too ("Network & internet Mobile, Wi-Fi,
    //    ..."), so match its start.
    var row = elements.First(e => e.Label.StartsWith(p.Row, StringComparison.Ordinal));
    device.Tap(row.Ref);
    device.WaitFor(p.Next);
    Console.WriteLine($"opened {p.Row}");

    // 4. Take a screenshot.
    device.Screenshot($"quickstart-{platform}.png");
    Console.WriteLine($"saved quickstart-{platform}.png");
}
// 5. The using block has quit: the device's session is closed.
Console.WriteLine("session ended");
