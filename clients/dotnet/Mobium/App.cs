using System.Collections.Generic;

namespace Mobium
{
    /// <summary>One installed app.</summary>
    public sealed class App
    {
        /// <summary>The package name or bundle id.</summary>
        public string Id { get; }

        /// <summary>
        /// Empty on Android, where reading a package's label costs a
        /// <c>dumpsys</c> per app and is not worth it for a listing.
        /// </summary>
        public string Name { get; }

        /// <summary>The app's version string, as the platform reports it.</summary>
        public string Version { get; }

        /// <summary>Whether the platform ships it, rather than someone installing it.</summary>
        public bool System { get; }

        /// <summary>Builds an app record.</summary>
        public App(string id, string name, string version, bool system)
        {
            Id = id; Name = name; Version = version; System = system;
        }

        internal static App From(IDictionary<string, object?> m) =>
            new App(Json.Str(m, "id"), Json.Str(m, "name"),
                    Json.Str(m, "version"), Json.Bool(m, "system"));

        /// <summary>The id, with the name in brackets when there is one.</summary>
        public override string ToString() =>
            string.IsNullOrEmpty(Name) ? Id : Id + " (" + Name + ")";
    }
}
