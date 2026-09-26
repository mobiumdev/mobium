using System.Collections.Generic;

namespace Mobium
{
    /// <summary>One attached device or simulator.</summary>
    public sealed class DeviceInfo
    {
        /// <summary>The adb serial or the simulator UDID.</summary>
        public string Id { get; }

        /// <summary><c>android</c> or <c>ios</c>.</summary>
        public string Platform { get; }

        /// <summary>Whether it is booted and usable, in the platform's own words.</summary>
        public string State { get; }

        /// <summary>The hardware or simulator model, when the platform says.</summary>
        public string Model { get; }

        /// <summary>The OS version or simulator runtime.</summary>
        public string Runtime { get; }

        /// <summary>Whether it is virtual. Calls and messages can only be simulated on one that is.</summary>
        public bool Emulator { get; }

        /// <summary>Builds a device record.</summary>
        public DeviceInfo(string id, string platform, string state, string model, string runtime, bool emulator)
        {
            Id = id; Platform = platform; State = state;
            Model = model; Runtime = runtime; Emulator = emulator;
        }

        internal static DeviceInfo From(IDictionary<string, object> m) =>
            new DeviceInfo(Json.Str(m, "id"), Json.Str(m, "platform"), Json.Str(m, "state"),
                           Json.Str(m, "model"), Json.Str(m, "runtime"), Json.Bool(m, "emulator"));

        /// <summary>The id, with its platform and state.</summary>
        public override string ToString() => Id + " (" + Platform + ", " + State + ")";
    }
}
