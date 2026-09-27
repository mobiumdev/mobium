using System;
using System.Collections.Generic;
using System.Globalization;
using System.Text;

namespace Mobium
{
    /// <summary>
    /// The smallest JSON reader and writer that speaks what mobium speaks.
    /// </summary>
    /// <remarks>
    /// <para>Hand-written for the same reason the Java client's is: this
    /// package targets netstandard2.0, where <c>System.Text.Json</c> is a
    /// NuGet package rather than part of the framework. Taking it would mean
    /// a consumer of a client library inherits a dependency to drive a phone,
    /// and Mobium's whole shape is an argument against that.</para>
    /// <para>Whole numbers stay <see cref="long"/> and never become
    /// <see cref="double"/>. A coordinate that arrives as 540.0 prints wrongly
    /// and compares wrongly, and every bound on the wire is a device pixel.</para>
    /// </remarks>
    internal static class Json
    {
        // -- writing ------------------------------------------------------

        internal static string Write(object? value)
        {
            var b = new StringBuilder();
            WriteValue(b, value);
            return b.ToString();
        }

        private static void WriteValue(StringBuilder b, object? v)
        {
            switch (v)
            {
                case null:
                    b.Append("null");
                    return;
                case string s:
                    WriteString(b, s);
                    return;
                case bool t:
                    b.Append(t ? "true" : "false");
                    return;
                case IDictionary<string, object?> map:
                    b.Append('{');
                    var first = true;
                    foreach (var kv in map)
                    {
                        if (!first) b.Append(',');
                        first = false;
                        WriteString(b, kv.Key);
                        b.Append(':');
                        WriteValue(b, kv.Value);
                    }
                    b.Append('}');
                    return;
                case System.Collections.IEnumerable list when !(v is string):
                    b.Append('[');
                    var firstItem = true;
                    foreach (var item in list)
                    {
                        if (!firstItem) b.Append(',');
                        firstItem = false;
                        WriteValue(b, item);
                    }
                    b.Append(']');
                    return;
                // JSON has no NaN or Infinity; written bare they make the whole
                // request unreadable, and the daemon would blame the pipe.
                case double d when double.IsNaN(d) || double.IsInfinity(d):
                    throw new InvalidArgumentException(d.ToString(CultureInfo.InvariantCulture) + " is not a number JSON can carry",
                        "", "", false, null);
                case float f when float.IsNaN(f) || float.IsInfinity(f):
                    throw new InvalidArgumentException(f.ToString(CultureInfo.InvariantCulture) + " is not a number JSON can carry",
                        "", "", false, null);
                case double d:
                    b.Append(d.ToString("R", CultureInfo.InvariantCulture));
                    return;
                case float f:
                    b.Append(f.ToString("R", CultureInfo.InvariantCulture));
                    return;
                default:
                    // Every integral type, written without a culture's idea of
                    // what a digit separator is.
                    b.Append(Convert.ToString(v, CultureInfo.InvariantCulture));
                    return;
            }
        }

        private static void WriteString(StringBuilder b, string s)
        {
            b.Append('"');
            foreach (var c in s)
            {
                switch (c)
                {
                    case '"': b.Append("\\\""); break;
                    case '\\': b.Append("\\\\"); break;
                    case '\n': b.Append("\\n"); break;
                    case '\r': b.Append("\\r"); break;
                    case '\t': b.Append("\\t"); break;
                    case '\b': b.Append("\\b"); break;
                    case '\f': b.Append("\\f"); break;
                    default:
                        // A raw control character in a JSON string is invalid,
                        // and mobium's own error text can carry one. Anything
                        // above is emitted as itself: the stream is UTF-8, so
                        // a non-breaking hyphen in "Wi‑Fi" must survive whole
                        // or the locator built from it matches nothing.
                        if (c < 0x20) b.Append("\\u").Append(((int)c).ToString("x4", CultureInfo.InvariantCulture));
                        else b.Append(c);
                        break;
                }
            }
            b.Append('"');
        }

        // -- reading ------------------------------------------------------

        internal static object? Parse(string text)
        {
            var p = new Parser(text);
            p.SkipWhitespace();
            var v = p.ReadValue();
            p.SkipWhitespace();
            if (!p.AtEnd) throw new MobiumException("trailing text after JSON value");
            return v;
        }

        internal static IDictionary<string, object?> AsObject(object? v) =>
            v as IDictionary<string, object?> ?? new Dictionary<string, object?>();

        internal static IList<object?> AsArray(object? v) =>
            v as IList<object?> ?? new List<object?>();

        internal static string Str(IDictionary<string, object?>? m, string key) =>
            m != null && m.TryGetValue(key, out var v) && v != null ? v as string ?? Convert.ToString(v, CultureInfo.InvariantCulture) : "";

        internal static bool Bool(IDictionary<string, object?>? m, string key) =>
            m != null && m.TryGetValue(key, out var v) && v is bool b && b;

        internal static double Number(IDictionary<string, object?>? m, string key)
        {
            if (m == null || !m.TryGetValue(key, out var v) || v == null) return 0d;
            switch (v)
            {
                case double d: return d;
                case long l: return l;
                case int i: return i;
                default: return 0d;
            }
        }

        internal static int Integer(IDictionary<string, object?>? m, string key)
        {
            if (m == null || !m.TryGetValue(key, out var v) || v == null) return 0;
            // A value out of int's range is refused rather than wrapped: an
            // unchecked cast turns 2^31 into a negative coordinate, and a tap
            // there lands somewhere nobody asked for.
            switch (v)
            {
                case long l when l >= int.MinValue && l <= int.MaxValue: return (int)l;
                case int i: return i;
                case double d when d >= int.MinValue && d <= int.MaxValue: return (int)d;
                case long _:
                case double _:
                    throw new MobiumException(key + " is " + Convert.ToString(v, CultureInfo.InvariantCulture) + ", out of range for an int");
                default: return 0;
            }
        }

        // Deeper than any hierarchy a device reports -- a React Native screen
        // is a few dozen levels -- and far short of the stack. The parser
        // recurses, and in .NET a stack overflow is not an exception: it ends
        // the process, test host and all, with nothing to catch.
        internal const int MaxDepth = 1000;

        private sealed class Parser
        {
            private readonly string _s;
            private int _i;
            private int _depth;

            internal Parser(string s) { _s = s ?? ""; }

            internal bool AtEnd => _i >= _s.Length;

            internal void SkipWhitespace()
            {
                while (_i < _s.Length && (_s[_i] == ' ' || _s[_i] == '\t' || _s[_i] == '\n' || _s[_i] == '\r')) _i++;
            }

            internal object? ReadValue()
            {
                if (AtEnd) throw new MobiumException("unexpected end of JSON");
                switch (_s[_i])
                {
                    case '{': return Nested(ReadObject);
                    case '[': return Nested(ReadArray);
                    case '"': return ReadString();
                    case 't': return ReadLiteral("true", true);
                    case 'f': return ReadLiteral("false", false);
                    case 'n': return ReadLiteral("null", null);
                    default: return ReadNumber();
                }
            }

            private object Nested<T>(Func<T> read) where T : class
            {
                if (++_depth > MaxDepth) throw new MobiumException("JSON nested deeper than " + MaxDepth + " levels");
                try
                {
                    return read();
                }
                finally
                {
                    _depth--;
                }
            }

            private object? ReadLiteral(string word, object? value)
            {
                if (_i + word.Length > _s.Length || string.CompareOrdinal(_s, _i, word, 0, word.Length) != 0)
                    throw new MobiumException("malformed JSON literal");
                _i += word.Length;
                return value;
            }

            private IDictionary<string, object?> ReadObject()
            {
                var m = new Dictionary<string, object?>(StringComparer.Ordinal);
                _i++; // {
                SkipWhitespace();
                if (!AtEnd && _s[_i] == '}') { _i++; return m; }
                while (true)
                {
                    SkipWhitespace();
                    if (AtEnd || _s[_i] != '"') throw new MobiumException("expected a JSON key");
                    var key = ReadString();
                    SkipWhitespace();
                    if (AtEnd || _s[_i] != ':') throw new MobiumException("expected ':' after a JSON key");
                    _i++;
                    SkipWhitespace();
                    m[key] = ReadValue();
                    SkipWhitespace();
                    if (AtEnd) throw new MobiumException("unterminated JSON object");
                    if (_s[_i] == ',') { _i++; continue; }
                    if (_s[_i] == '}') { _i++; return m; }
                    throw new MobiumException("expected ',' or '}' in a JSON object");
                }
            }

            private IList<object?> ReadArray()
            {
                var list = new List<object?>();
                _i++; // [
                SkipWhitespace();
                if (!AtEnd && _s[_i] == ']') { _i++; return list; }
                while (true)
                {
                    SkipWhitespace();
                    list.Add(ReadValue());
                    SkipWhitespace();
                    if (AtEnd) throw new MobiumException("unterminated JSON array");
                    if (_s[_i] == ',') { _i++; continue; }
                    if (_s[_i] == ']') { _i++; return list; }
                    throw new MobiumException("expected ',' or ']' in a JSON array");
                }
            }

            private string ReadString()
            {
                _i++; // opening quote
                var b = new StringBuilder();
                while (true)
                {
                    if (AtEnd) throw new MobiumException("unterminated JSON string");
                    var c = _s[_i++];
                    if (c == '"') return b.ToString();
                    if (c < 0x20) throw new MobiumException("raw control character in a JSON string");
                    if (c != '\\') { b.Append(c); continue; }
                    if (AtEnd) throw new MobiumException("unterminated JSON escape");
                    var e = _s[_i++];
                    switch (e)
                    {
                        case '"': b.Append('"'); break;
                        case '\\': b.Append('\\'); break;
                        case '/': b.Append('/'); break;
                        case 'n': b.Append('\n'); break;
                        case 'r': b.Append('\r'); break;
                        case 't': b.Append('\t'); break;
                        case 'b': b.Append('\b'); break;
                        case 'f': b.Append('\f'); break;
                        case 'u':
                            if (_i + 4 > _s.Length) throw new MobiumException("truncated \\u escape");
                            // Four hex digits exactly. NumberStyles.HexNumber also
                            // allows surrounding whitespace, so "\\u 41a" would
                            // have read as U+041A.
                            var code = 0;
                            for (var k = 0; k < 4; k++)
                            {
                                var h = HexDigit(_s[_i + k]);
                                if (h < 0) throw new MobiumException("malformed \\u escape");
                                code = code * 16 + h;
                            }
                            _i += 4;
                            b.Append((char)code);
                            break;
                        default:
                            throw new MobiumException("unknown JSON escape \\" + e);
                    }
                }
            }

            private static int HexDigit(char c) =>
                c >= '0' && c <= '9' ? c - '0'
                : c >= 'a' && c <= 'f' ? c - 'a' + 10
                : c >= 'A' && c <= 'F' ? c - 'A' + 10
                : -1;

            // JSON's number grammar, exactly: -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?
            // Anything looser -- a leading '+', "01", "1.", "--1" -- is refused
            // rather than guessed at.
            private object ReadNumber()
            {
                var start = _i;
                if (!AtEnd && _s[_i] == '-') _i++;
                if (AtEnd || !IsDigit(_s[_i])) throw new MobiumException("expected a JSON value");
                if (_s[_i] == '0') _i++;
                else while (!AtEnd && IsDigit(_s[_i])) _i++;
                var isReal = false;
                if (!AtEnd && _s[_i] == '.')
                {
                    isReal = true;
                    _i++;
                    if (AtEnd || !IsDigit(_s[_i])) throw new MobiumException("malformed JSON number");
                    while (!AtEnd && IsDigit(_s[_i])) _i++;
                }
                if (!AtEnd && (_s[_i] == 'e' || _s[_i] == 'E'))
                {
                    isReal = true;
                    _i++;
                    if (!AtEnd && (_s[_i] == '+' || _s[_i] == '-')) _i++;
                    if (AtEnd || !IsDigit(_s[_i])) throw new MobiumException("malformed JSON number");
                    while (!AtEnd && IsDigit(_s[_i])) _i++;
                }
                var raw = _s.Substring(start, _i - start);
                // Whole numbers stay long; one too large for a long is kept as
                // a double rather than refused, as every other JSON reader does.
                if (!isReal && long.TryParse(raw, NumberStyles.AllowLeadingSign, CultureInfo.InvariantCulture, out var l)) return l;
                if (double.TryParse(raw, NumberStyles.Float, CultureInfo.InvariantCulture, out var d)) return d;
                throw new MobiumException("malformed JSON number " + raw);
            }

            private static bool IsDigit(char c) => c >= '0' && c <= '9';
        }
    }
}
