using System;
using System.Collections.Generic;

namespace Mobium
{
    /// <summary>
    /// What to wait for, and how long.
    /// </summary>
    /// <remarks>
    /// Named <c>Until</c> rather than <c>WaitFor</c> because a type and a
    /// method of the same name in the same scope cannot both be reached in
    /// C#. <c>Until.Hidden()</c> reads better at the call site anyway.
    /// </remarks>
    /// <example>
    /// <code>
    /// device.WaitFor("text=Welcome");                                   // visible, 10s
    /// device.WaitFor("role=progressbar", Until.Hidden());               // until it goes
    /// device.WaitFor("@e4", Until.Text("Sent").Timeout(TimeSpan.FromSeconds(30)));
    /// </code>
    /// </example>
    public sealed class Until
    {
        private readonly string _condition;
        private readonly string? _text;
        private readonly TimeSpan? _timeout;

        private Until(string condition, string? text, TimeSpan? timeout)
        {
            _condition = condition; _text = text; _timeout = timeout;
        }

        /// <summary>Wait for the element to be on screen. The default.</summary>
        public static Until Visible() => new Until("visible", null, null);

        /// <summary>Wait for it to go away — a spinner, say.</summary>
        public static Until Hidden() => new Until("hidden", null, null);

        /// <summary>Wait until it contains this text.</summary>
        public static Until Text(string expected) => new Until("text", expected, null);

        /// <summary>Wait until a field holds exactly this value; <c>""</c> waits for it to be empty. A password field is refused.</summary>
        public static Until Value(string expected) => new Until("value", expected, null);

        /// <summary>Wait for a control to be enabled — a Submit the app enables once a form is valid.</summary>
        public static Until Enabled() => new Until("enabled", null, null);

        /// <summary>Wait for a control to be disabled.</summary>
        public static Until Disabled() => new Until("disabled", null, null);

        /// <summary>Wait for a checkbox, radio or switch to be checked.</summary>
        public static Until Checked() => new Until("checked", null, null);

        /// <summary>Wait for a checkbox, radio or switch to be unchecked.</summary>
        public static Until Unchecked() => new Until("unchecked", null, null);

        /// <summary>Wait for a field to have keyboard focus.</summary>
        public static Until Focused() => new Until("focused", null, null);

        /// <summary>How long before giving up. Ten seconds by default, two minutes at most.</summary>
        public Until Timeout(TimeSpan d) => new Until(_condition, _text, d);

        internal IDictionary<string, object?> Args(string target)
        {
            var m = new Dictionary<string, object?>(StringComparer.Ordinal)
            {
                ["target"] = target,
                ["condition"] = _condition,
            };
            if (_text != null) m["text"] = _text;
            if (_timeout.HasValue) m["timeout_ms"] = (long)_timeout.Value.TotalMilliseconds;
            return m;
        }
    }
}
