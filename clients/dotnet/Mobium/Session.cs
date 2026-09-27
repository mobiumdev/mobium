using System.Collections.Generic;

namespace Mobium
{
    /// <summary>What <see cref="DeviceBuilder.Start"/> opened: the device and how it is driven.</summary>
    public sealed class Session
    {
        /// <summary>The serial (Android) or UDID (iOS) the session is on.</summary>
        public string Device { get; }

        /// <summary><c>android</c>, <c>ios</c>, or a third-party driver's name.</summary>
        public string Platform { get; }

        /// <summary>The driver driving it, such as <c>uiautomator2</c> or <c>wda</c>.</summary>
        public string Driver { get; }

        /// <summary>True when a session was already open on the device, and was kept.</summary>
        public bool Reused { get; }

        /// <summary>The app Start launched, or an empty string.</summary>
        public string App { get; }

        /// <summary>A session as the daemon described it.</summary>
        public Session(string device, string platform, string driver, bool reused, string app)
        {
            Device = device; Platform = platform; Driver = driver; Reused = reused; App = app;
        }

        internal static Session From(IDictionary<string, object?> m) =>
            new Session(Json.Str(m, "device"), Json.Str(m, "platform"), Json.Str(m, "driver"),
                Json.Bool(m, "reused"), Json.Str(m, "app"));

        /// <inheritdoc/>
        public override string ToString() => Device + " (" + Platform + ", " + Driver + ")";
    }
}
