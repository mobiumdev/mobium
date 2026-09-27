using System;
using System.Collections.Generic;

namespace Mobium
{
    /// <summary>
    /// A tool reported that it could not do what was asked, or the connection
    /// to mobium failed.
    /// </summary>
    /// <remarks>
    /// Every tool failure carries a stable <see cref="Code"/>, the same in every
    /// client and on the wire (docs/decisions/0005 in the mobium repository),
    /// and each code has a subclass — <see cref="NoSuchElementException"/>,
    /// <see cref="UnsupportedException"/> and so on — so a test catches the kind
    /// it can handle and lets the rest through. <see cref="Remedy"/> says what
    /// to do about it and <see cref="Retryable"/> whether the same call can
    /// succeed if made again. A code this client does not know, a daemon too
    /// old to send one, and a failure of the connection itself are plain
    /// <c>MobiumException</c>s. <see cref="Tool"/> names the tool that failed,
    /// or is empty for a transport-level failure.
    /// </remarks>
    public class MobiumException : Exception
    {
        /// <summary>The tool that failed, or an empty string.</summary>
        public string Tool { get; }

        /// <summary>The error code — <c>no_such_element</c>, <c>timeout</c>, ... — or <c>error</c> when unclassified.</summary>
        public string Code { get; }

        /// <summary>What to do about it, or an empty string.</summary>
        public string Remedy { get; }

        /// <summary>Whether the same call, made again unchanged, can reasonably succeed.</summary>
        public bool Retryable { get; }

        /// <summary>Machine-readable facts: the locator, the W3C code a device server sent.</summary>
        public IDictionary<string, object?> Details { get; }

        /// <summary>A transport-level failure, with no tool to blame.</summary>
        public MobiumException(string message) : this(message, "") { }

        /// <summary>A named tool reported that it could not do what was asked.</summary>
        public MobiumException(string message, string tool)
            : this(message, tool, "error", "", false, null) { }

        /// <summary>A failure caused by something further down, kept as the inner exception.</summary>
        public MobiumException(string message, Exception cause) : base(message, cause)
        {
            Tool = "";
            Code = "error";
            Remedy = "";
            Details = new Dictionary<string, object?>();
        }

        internal MobiumException(string message, string tool, string code, string remedy,
            bool retryable, IDictionary<string, object?>? details)
            : base(string.IsNullOrEmpty(tool) ? message : tool + ": " + message)
        {
            Tool = tool ?? "";
            Code = code;
            Remedy = remedy ?? "";
            Retryable = retryable;
            Details = details ?? new Dictionary<string, object?>();
        }

        /// <summary>
        /// The exception for a failed tool call: the text as always, and the
        /// code, remedy and details when the daemon sent them.
        /// </summary>
        internal static MobiumException From(string text, string tool, object? structuredContent)
        {
            var s = structuredContent as IDictionary<string, object?>;
            if (s == null) return new MobiumException(text, tool);
            var code = Json.Str(s, "code");
            if (code.Length == 0) return new MobiumException(text, tool);
            var remedy = Json.Str(s, "remedy");
            var retryable = Json.Bool(s, "retryable");
            s.TryGetValue("details", out var d);
            var details = Json.AsObject(d);
            switch (code)
            {
                case NoDeviceException.ErrorCode: return new NoDeviceException(text, tool, remedy, retryable, details);
                case DeviceNotReadyException.ErrorCode: return new DeviceNotReadyException(text, tool, remedy, retryable, details);
                case ToolchainMissingException.ErrorCode: return new ToolchainMissingException(text, tool, remedy, retryable, details);
                case NoSuchElementException.ErrorCode: return new NoSuchElementException(text, tool, remedy, retryable, details);
                case AmbiguousLocatorException.ErrorCode: return new AmbiguousLocatorException(text, tool, remedy, retryable, details);
                case ElementNotReachableException.ErrorCode: return new ElementNotReachableException(text, tool, remedy, retryable, details);
                case NoSuchContextException.ErrorCode: return new NoSuchContextException(text, tool, remedy, retryable, details);
                case NoSuchAlertException.ErrorCode: return new NoSuchAlertException(text, tool, remedy, retryable, details);
                case UnsupportedException.ErrorCode: return new UnsupportedException(text, tool, remedy, retryable, details);
                case NotConfirmedException.ErrorCode: return new NotConfirmedException(text, tool, remedy, retryable, details);
                case TimedOutException.ErrorCode: return new TimedOutException(text, tool, remedy, retryable, details);
                case InvalidArgumentException.ErrorCode: return new InvalidArgumentException(text, tool, remedy, retryable, details);
                case DeviceServerException.ErrorCode: return new DeviceServerException(text, tool, remedy, retryable, details);
                case InternalException.ErrorCode: return new InternalException(text, tool, remedy, retryable, details);
                default: return new MobiumException(text, tool, code, remedy, retryable, details);
            }
        }
    }

    /// <summary>Nothing to drive: no device matches, or none is connected. Error code <c>no_device</c>.</summary>
    public sealed class NoDeviceException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "no_device";

        internal NoDeviceException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>The device is there and cannot be driven yet: locked, not trusted, Developer Mode off. Error code <c>device_not_ready</c>.</summary>
    public sealed class DeviceNotReadyException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "device_not_ready";

        internal DeviceNotReadyException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>Something on this machine is missing: adb, Xcode, a signing certificate. Error code <c>toolchain_missing</c>.</summary>
    public sealed class ToolchainMissingException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "toolchain_missing";

        internal ToolchainMissingException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>A locator or ref matched nothing on screen. Worth scrolling for. Error code <c>no_such_element</c>.</summary>
    public sealed class NoSuchElementException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "no_such_element";

        internal NoSuchElementException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>A locator matched more than one element. Narrow it; Mobium never guesses. Error code <c>ambiguous_locator</c>.</summary>
    public sealed class AmbiguousLocatorException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "ambiguous_locator";

        internal AmbiguousLocatorException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>The element was found and cannot be touched where it is, usually off screen. Error code <c>element_not_reachable</c>.</summary>
    public sealed class ElementNotReachableException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "element_not_reachable";

        internal ElementNotReachableException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>A WebView context that is not there. Error code <c>no_such_context</c>.</summary>
    public sealed class NoSuchContextException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "no_such_context";

        internal NoSuchContextException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>A dialog was expected and none is on screen. Error code <c>no_such_alert</c>.</summary>
    public sealed class NoSuchAlertException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "no_such_alert";

        internal NoSuchAlertException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>This backend or platform cannot do it, and says why. Retrying cannot help. Error code <c>unsupported</c>.</summary>
    public sealed class UnsupportedException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "unsupported";

        internal UnsupportedException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>The command reported success and reading the state back disagreed. Error code <c>not_confirmed</c>.</summary>
    public sealed class NotConfirmedException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "not_confirmed";

        internal NotConfirmedException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>A wait ran out. Named TimedOut so it does not collide with System.TimeoutException. Error code <c>timeout</c>.</summary>
    public sealed class TimedOutException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "timeout";

        internal TimedOutException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>The request itself is wrong. Error code <c>invalid_argument</c>.</summary>
    public sealed class InvalidArgumentException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "invalid_argument";

        internal InvalidArgumentException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>The device side failed (a device server, or adb, simctl, devicectl, lockdown); a server's W3C code is Details["w3c"]. Error code <c>device_server</c>.</summary>
    public sealed class DeviceServerException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "device_server";

        internal DeviceServerException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }

    /// <summary>A bug in Mobium. Error code <c>internal</c>.</summary>
    public sealed class InternalException : MobiumException
    {
        /// <summary>The wire code this exception stands for.</summary>
        public const string ErrorCode = "internal";

        internal InternalException(string message, string tool, string remedy, bool retryable,
            IDictionary<string, object?>? details)
            : base(message, tool, ErrorCode, remedy, retryable, details) { }
    }
}
