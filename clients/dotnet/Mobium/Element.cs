using System.Collections.Generic;

namespace Mobium
{
    /// <summary>One actionable thing on screen.</summary>
    public sealed class Element
    {
        /// <summary>The <c>@e1</c> handle, valid only for the screen it came from.</summary>
        public string Ref { get; }

        /// <summary>What a person would call it.</summary>
        public string Label { get; }

        /// <summary>button, input, list and so on; empty if Mobium could not say.</summary>
        public string Role { get; }

        /// <summary>How the ref resolves on a later screen, as <c>kind=value</c>.</summary>
        public string Locator { get; }

        /// <summary>Where it is, in device pixels.</summary>
        public Bounds Bounds { get; }

        /// <summary>The WebView it came from; empty for native elements.</summary>
        public string Context { get; }

        /// <summary>Builds an element record.</summary>
        public Element(string @ref, string label, string role, string locator, Bounds bounds, string context)
        {
            Ref = @ref; Label = label; Role = role;
            Locator = locator; Bounds = bounds; Context = context;
        }

        internal static Element From(IDictionary<string, object> m)
        {
            var loc = Json.AsObject(m.TryGetValue("locator", out var l) ? l : null);
            var locator = loc.Count == 0 ? "" : Json.Str(loc, "kind") + "=" + Json.Str(loc, "value");
            return new Element(
                Json.Str(m, "ref"),
                Json.Str(m, "label"),
                Json.Str(m, "role"),
                locator,
                Bounds.From(Json.AsObject(m.TryGetValue("bounds", out var b) ? b : null)),
                Json.Str(m, "context"));
        }

        /// <summary>The ref and label, with the role in brackets when there is one.</summary>
        public override string ToString() =>
            string.IsNullOrEmpty(Role) ? Ref + " " + Label : Ref + " " + Label + " (" + Role + ")";
    }
}
