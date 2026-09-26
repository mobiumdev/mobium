using System;
using Mobium;

namespace Mobium.Tests
{
    /// <summary>
    /// The .NET client end to end, against a booted Android device. Run as
    /// <c>dotnet run --project clients/dotnet/Tests -- e2e</c> with
    /// <c>MOBIUM_E2E_DEVICE</c> set; docs/checks/clients.sh does, with the four
    /// other clients' copies of the same flow. Not part of <c>make ci</c> — it
    /// needs a device.
    /// </summary>
    internal static class E2E
    {
        private const string Settings = "com.android.settings";

        private static void Check(bool ok, string what)
        {
            if (!ok)
            {
                Console.Error.WriteLine("FAIL: " + what);
                Environment.Exit(1);
            }
            Console.WriteLine("    ok   " + what);
        }

        internal static int Run()
        {
            var serial = Environment.GetEnvironmentVariable("MOBIUM_E2E_DEVICE");
            if (string.IsNullOrWhiteSpace(serial))
            {
                Console.WriteLine("MOBIUM_E2E_DEVICE is not set; skipping");
                return 0;
            }
            var builder = Device.Builder().OnDevice(serial);
            var bin = Environment.GetEnvironmentVariable("MOBIUM_BIN_PATH");
            if (!string.IsNullOrWhiteSpace(bin)) builder.Binary(bin);

            using (var d = builder.Connect())
            {
                d.Terminate(Settings);
                d.Launch(Settings);
                Check(d.Current() == Settings, "launch brings Settings forward");

                var found = d.WaitFor("text=Network & internet");
                Check(found != null && found.Ref.StartsWith("@e", StringComparison.Ordinal), "WaitFor returns a ref");
                d.Tap(found.Ref);
                // A tap returns when delivered, not when the next screen is up.
                Check(d.WaitFor("text=Internet") != null, "tapping the ref opens its screen");

                d.Press("back");
                Check(d.ScrollTo("text=About") != null, "ScrollTo reaches a row below the fold");

                d.Grant("com.android.chrome", "camera");
                d.Revoke("com.android.chrome", "camera");
                Check(true, "grant and revoke, each read back by the tool");

                var logs = d.DeviceLogs(lines: 5);
                Check(logs.TryGetValue("entries", out var e) && Json.AsArray(e).Count > 0, "DeviceLogs reads logcat");
                Check(d.Crashes(limit: 3) != null, "Crashes answers with a list");

                try
                {
                    d.Tap("text=Definitely Not Here");
                    Check(false, "a missing element throws");
                }
                catch (NoSuchElementException)
                {
                    Check(true, "a missing element throws NoSuchElementException");
                }

                d.Terminate(Settings);
                Check(d.Current() != Settings, "terminate takes Settings away");
            }
            Console.WriteLine("dotnet: passed");
            return 0;
        }
    }
}
