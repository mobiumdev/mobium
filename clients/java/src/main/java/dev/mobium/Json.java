package dev.mobium;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * The smallest JSON reader and writer that can carry Mobium's wire format.
 *
 * <p>The JDK has no JSON, and the obvious answer — Jackson or Gson — is the
 * wrong one for a testing library. A test harness that drags a JSON parser
 * onto the classpath will sooner or later collide with the version the
 * application under test already uses, and the person who has to untangle
 * that is the user. Every other Mobium client is dependency-free; this one
 * stays that way too.
 *
 * <p>It parses what {@code mobium pipe} emits, which is ordinary JSON: it is
 * not trying to be a general-purpose library, and it rejects malformed input
 * rather than guessing at it.
 */
final class Json {

    private Json() {}

    // -- reading -----------------------------------------------------------

    static Object parse(String text) {
        Parser p = new Parser(text);
        p.skipWhitespace();
        Object value = p.value();
        p.skipWhitespace();
        if (!p.done()) {
            throw new MobiumException("trailing characters after JSON value at offset " + p.pos);
        }
        return value;
    }

    /**
     * Deeper than any hierarchy a device reports -- a React Native screen is a
     * few dozen levels -- and far short of the stack. The parser recurses, and
     * past this a StackOverflowError would escape every catch written for a
     * MobiumException.
     */
    static final int MAX_DEPTH = 1000;

    private static final class Parser {
        private final String s;
        private int pos;
        private int depth;

        Parser(String s) { this.s = s; }

        boolean done() { return pos >= s.length(); }

        void skipWhitespace() {
            while (pos < s.length()) {
                char c = s.charAt(pos);
                if (c == ' ' || c == '\t' || c == '\n' || c == '\r') pos++;
                else break;
            }
        }

        Object value() {
            if (done()) throw new MobiumException("unexpected end of JSON");
            char c = s.charAt(pos);
            switch (c) {
                case '{': return nested(true);
                case '[': return nested(false);
                case '"': return string();
                case 't': return literal("true", Boolean.TRUE);
                case 'f': return literal("false", Boolean.FALSE);
                case 'n': return literal("null", null);
                default:  return number();
            }
        }

        Object nested(boolean object) {
            if (++depth > MAX_DEPTH) throw new MobiumException("JSON nested deeper than " + MAX_DEPTH + " levels");
            try {
                return object ? object() : array();
            } finally {
                depth--;
            }
        }

        Map<String, Object> object() {
            Map<String, Object> out = new LinkedHashMap<>();
            expect('{');
            skipWhitespace();
            if (peek() == '}') { pos++; return out; }
            while (true) {
                skipWhitespace();
                String key = string();
                skipWhitespace();
                expect(':');
                skipWhitespace();
                out.put(key, value());
                skipWhitespace();
                char c = next();
                if (c == '}') return out;
                if (c != ',') throw new MobiumException("expected ',' or '}' at offset " + (pos - 1));
            }
        }

        List<Object> array() {
            List<Object> out = new ArrayList<>();
            expect('[');
            skipWhitespace();
            if (peek() == ']') { pos++; return out; }
            while (true) {
                skipWhitespace();
                out.add(value());
                skipWhitespace();
                char c = next();
                if (c == ']') return out;
                if (c != ',') throw new MobiumException("expected ',' or ']' at offset " + (pos - 1));
            }
        }

        String string() {
            expect('"');
            StringBuilder b = new StringBuilder();
            while (true) {
                if (done()) throw new MobiumException("unterminated string in JSON");
                char c = s.charAt(pos++);
                if (c == '"') return b.toString();
                if (c < 0x20) throw new MobiumException("raw control character in a JSON string at offset " + (pos - 1));
                if (c != '\\') { b.append(c); continue; }
                char esc = next();
                switch (esc) {
                    case '"':  b.append('"');  break;
                    case '\\': b.append('\\'); break;
                    case '/':  b.append('/');  break;
                    case 'b':  b.append('\b'); break;
                    case 'f':  b.append('\f'); break;
                    case 'n':  b.append('\n'); break;
                    case 'r':  b.append('\r'); break;
                    case 't':  b.append('\t'); break;
                    case 'u':
                        if (pos + 4 > s.length()) throw new MobiumException("truncated \\u escape");
                        // Surrogate pairs need no special handling: each half
                        // arrives as its own \\u escape and Java strings are
                        // UTF-16, so appending both in order is correct.
                        // Four hex digits exactly. Integer.parseInt(_, 16)
                        // also takes a sign, so "\\u+041" read as 'A', and it
                        // throws NumberFormatException for anything else,
                        // which got past every catch written for a
                        // MobiumException and ended the call it arrived in.
                        int code = 0;
                        for (int k = 0; k < 4; k++) {
                            int h = hexDigit(s.charAt(pos + k));
                            if (h < 0) throw new MobiumException("malformed \\u escape at offset " + pos);
                            code = code * 16 + h;
                        }
                        b.append((char) code);
                        pos += 4;
                        break;
                    default: throw new MobiumException("unknown escape \\" + esc);
                }
            }
        }

        // JSON's number grammar, exactly: -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?
        // Anything looser -- "1-2", ".5", "01", "1." -- is refused as a
        // MobiumException rather than guessed at, or thrown as a
        // NumberFormatException nothing here would catch.
        Object number() {
            int start = pos;
            if (!done() && s.charAt(pos) == '-') pos++;
            if (done() || !digit(s.charAt(pos))) throw new MobiumException("expected a value at offset " + start);
            if (s.charAt(pos) == '0') pos++;
            else while (!done() && digit(s.charAt(pos))) pos++;
            boolean fractional = false;
            if (!done() && s.charAt(pos) == '.') {
                fractional = true;
                pos++;
                if (done() || !digit(s.charAt(pos))) throw new MobiumException("malformed number at offset " + start);
                while (!done() && digit(s.charAt(pos))) pos++;
            }
            if (!done() && (s.charAt(pos) == 'e' || s.charAt(pos) == 'E')) {
                fractional = true;
                pos++;
                if (!done() && (s.charAt(pos) == '+' || s.charAt(pos) == '-')) pos++;
                if (done() || !digit(s.charAt(pos))) throw new MobiumException("malformed number at offset " + start);
                while (!done() && digit(s.charAt(pos))) pos++;
            }
            String raw = s.substring(start, pos);
            // Integers come back as Long so a coordinate does not arrive as
            // "540.0" when it is printed; one too large for a long is kept as
            // a double, as every other JSON reader does.
            if (!fractional) {
                try { return Long.parseLong(raw); } catch (NumberFormatException tooBig) { /* a double, below */ }
            }
            return Double.parseDouble(raw);
        }

        static boolean digit(char c) { return c >= '0' && c <= '9'; }

        // ASCII only: Character.digit also accepts other scripts' digits, such
        // as a fullwidth '１'.
        static int hexDigit(char c) {
            if (c >= '0' && c <= '9') return c - '0';
            if (c >= 'a' && c <= 'f') return c - 'a' + 10;
            if (c >= 'A' && c <= 'F') return c - 'A' + 10;
            return -1;
        }

        Object literal(String word, Object value) {
            if (!s.startsWith(word, pos)) {
                throw new MobiumException("expected " + word + " at offset " + pos);
            }
            pos += word.length();
            return value;
        }

        char peek() {
            if (done()) throw new MobiumException("unexpected end of JSON");
            return s.charAt(pos);
        }

        char next() {
            if (done()) throw new MobiumException("unexpected end of JSON");
            return s.charAt(pos++);
        }

        void expect(char c) {
            if (next() != c) throw new MobiumException("expected '" + c + "' at offset " + (pos - 1));
        }
    }

    // -- writing -----------------------------------------------------------

    static String write(Object value) {
        StringBuilder b = new StringBuilder();
        writeTo(b, value);
        return b.toString();
    }

    private static void writeTo(StringBuilder b, Object value) {
        if (value == null) { b.append("null"); return; }
        if (value instanceof String) { writeString(b, (String) value); return; }
        if (value instanceof Double || value instanceof Float) {
            double d = ((Number) value).doubleValue();
            // JSON has no NaN or Infinity; written bare they make the whole
            // request unreadable, and the daemon would blame the pipe.
            if (Double.isNaN(d) || Double.isInfinite(d)) {
                throw new InvalidArgumentException(value + " is not a number JSON can carry", "", "", false, Map.of());
            }
        }
        if (value instanceof Boolean || value instanceof Number) { b.append(value); return; }
        if (value instanceof Map) {
            b.append('{');
            boolean first = true;
            for (Map.Entry<?, ?> e : ((Map<?, ?>) value).entrySet()) {
                if (!first) b.append(',');
                first = false;
                writeString(b, String.valueOf(e.getKey()));
                b.append(':');
                writeTo(b, e.getValue());
            }
            b.append('}');
            return;
        }
        if (value instanceof Iterable) {
            b.append('[');
            boolean first = true;
            for (Object o : (Iterable<?>) value) {
                if (!first) b.append(',');
                first = false;
                writeTo(b, o);
            }
            b.append(']');
            return;
        }
        throw new MobiumException("cannot serialize " + value.getClass().getName() + " as JSON");
    }

    private static void writeString(StringBuilder b, String s) {
        b.append('"');
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"':  b.append("\\\""); break;
                case '\\': b.append("\\\\"); break;
                case '\n': b.append("\\n");  break;
                case '\r': b.append("\\r");  break;
                case '\t': b.append("\\t");  break;
                case '\b': b.append("\\b");  break;
                case '\f': b.append("\\f");  break;
                default:
                    // Control characters must be escaped; everything else,
                    // including non-ASCII, goes through as UTF-8. A locator
                    // like "text=Wi‑Fi" contains a non-breaking hyphen and
                    // must survive the round trip unchanged.
                    if (c < 0x20) b.append(String.format("\\u%04x", (int) c));
                    else b.append(c);
            }
        }
        b.append('"');
    }

    // -- typed access ------------------------------------------------------

    @SuppressWarnings("unchecked")
    static Map<String, Object> asObject(Object o) {
        if (o == null) return Map.of();
        if (o instanceof Map) return (Map<String, Object>) o;
        throw new MobiumException("expected a JSON object, got " + o.getClass().getSimpleName());
    }

    @SuppressWarnings("unchecked")
    static List<Object> asArray(Object o) {
        if (o == null) return List.of();
        if (o instanceof List) return (List<Object>) o;
        throw new MobiumException("expected a JSON array, got " + o.getClass().getSimpleName());
    }

    static String str(Map<String, Object> m, String key) {
        Object v = m.get(key);
        return v == null ? "" : String.valueOf(v);
    }

    static int integer(Map<String, Object> m, String key) {
        Object v = m.get(key);
        if (!(v instanceof Number)) return 0;
        // Refused rather than wrapped: intValue() turns 2^31 into a negative
        // coordinate, and a tap there lands somewhere nobody asked for.
        double d = ((Number) v).doubleValue();
        if (d < Integer.MIN_VALUE || d > Integer.MAX_VALUE) {
            throw new MobiumException(key + " is " + v + ", out of range for an int");
        }
        return ((Number) v).intValue();
    }

    static boolean bool(Map<String, Object> m, String key) {
        return Boolean.TRUE.equals(m.get(key));
    }

    static double dbl(Map<String, Object> m, String key) {
        Object v = m.get(key);
        return v instanceof Number ? ((Number) v).doubleValue() : 0d;
    }
}
