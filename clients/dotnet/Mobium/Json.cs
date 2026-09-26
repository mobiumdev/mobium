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

        internal static string Write(object value)
        {
            var b = new StringBuilder();
            WriteValue(b, value);
            return b.ToString();
        }

        private static void WriteValue(StringBuilder b, object v)
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
                case IDictionary<string, object> map:
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

        internal static object Parse(string text)
        {
            var p = new Parser(text);
            p.SkipWhitespace();
            var v = p.ReadValue();
            p.SkipWhitespace();
            if (!p.AtEnd) throw new MobiumException("trailing text after JSON value");
            return v;
        }

        internal static IDictionary<string, object> AsObject(object v) =>
            v as IDictionary<string, object> ?? new Dictionary<string, object>();

        internal static IList<object> AsArray(object v) =>
            v as IList<object> ?? new List<object>();

        internal static string Str(IDictionary<string, object> m, string key) =>
            m != null && m.TryGetValue(key, out var v) && v != null ? v as string ?? Convert.ToString(v, CultureInfo.InvariantCulture) : "";

        internal static bool Bool(IDictionary<string, object> m, string key) =>
            m != null && m.TryGetValue(key, out var v) && v is bool b && b;

        internal static double Number(IDictionary<string, object> m, string key)
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

        internal static int Integer(IDictionary<string, object> m, string key)
        {
            if (m == null || !m.TryGetValue(key, out var v) || v == null) return 0;
            switch (v)
            {
                case long l: return (int)l;
                case int i: return i;
                case double d: return (int)d;
                default: return 0;
            }
        }

        private sealed class Parser
        {
            private readonly string _s;
            private int _i;

            internal Parser(string s) { _s = s ?? ""; }

            internal bool AtEnd => _i >= _s.Length;

            internal void SkipWhitespace()
            {
                while (_i < _s.Length && (_s[_i] == ' ' || _s[_i] == '\t' || _s[_i] == '\n' || _s[_i] == '\r')) _i++;
            }

            internal object ReadValue()
            {
                if (AtEnd) throw new MobiumException("unexpected end of JSON");
                switch (_s[_i])
                {
                    case '{': return ReadObject();
                    case '[': return ReadArray();
                    case '"': return ReadString();
                    case 't': return ReadLiteral("true", true);
                    case 'f': return ReadLiteral("false", false);
                    case 'n': return ReadLiteral("null", null);
                    default: return ReadNumber();
                }
            }

            private object ReadLiteral(string word, object value)
            {
                if (_i + word.Length > _s.Length || string.CompareOrdinal(_s, _i, word, 0, word.Length) != 0)
                    throw new MobiumException("malformed JSON literal");
                _i += word.Length;
                return value;
            }

            private IDictionary<string, object> ReadObject()
            {
                var m = new Dictionary<string, object>(StringComparer.Ordinal);
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

            private IList<object> ReadArray()
            {
                var list = new List<object>();
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
                            var hex = _s.Substring(_i, 4);
                            if (!int.TryParse(hex, NumberStyles.HexNumber, CultureInfo.InvariantCulture, out var code))
                                throw new MobiumException("malformed \\u escape");
                            _i += 4;
                            b.Append((char)code);
                            break;
                        default:
                            throw new MobiumException("unknown JSON escape \\" + e);
                    }
                }
            }

            private object ReadNumber()
            {
                var start = _i;
                if (!AtEnd && (_s[_i] == '-' || _s[_i] == '+')) _i++;
                var isReal = false;
                while (!AtEnd)
                {
                    var c = _s[_i];
                    if (c >= '0' && c <= '9') { _i++; continue; }
                    // A '.', 'e' or 'E' makes it a real number. A '+' or '-'
                    // here is an exponent's sign and changes nothing: what
                    // matters is that 540 stays a long and 1.5 does not.
                    if (c == '.' || c == 'e' || c == 'E') { isReal = true; _i++; continue; }
                    if (c == '+' || c == '-') { _i++; continue; }
                    break;
                }
                var raw = _s.Substring(start, _i - start);
                if (raw.Length == 0) throw new MobiumException("expected a JSON value");
                if (!isReal && long.TryParse(raw, NumberStyles.Integer, CultureInfo.InvariantCulture, out var l)) return l;
                if (double.TryParse(raw, NumberStyles.Float, CultureInfo.InvariantCulture, out var d)) return d;
                throw new MobiumException("malformed JSON number " + raw);
            }
        }
    }
}
