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

    private static final class Parser {
        private final String s;
        private int pos;

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
                case '{': return object();
                case '[': return array();
                case '"': return string();
                case 't': return literal("true", Boolean.TRUE);
                case 'f': return literal("false", Boolean.FALSE);
                case 'n': return literal("null", null);
                default:  return number();
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
                        b.append((char) Integer.parseInt(s.substring(pos, pos + 4), 16));
                        pos += 4;
                        break;
                    default: throw new MobiumException("unknown escape \\" + esc);
                }
            }
        }

        Object number() {
            int start = pos;
            if (peek() == '-') pos++;
            boolean fractional = false;
            while (!done()) {
                char c = s.charAt(pos);
                if (c >= '0' && c <= '9') { pos++; }
                else if (c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-') { fractional = true; pos++; }
                else break;
            }
            String raw = s.substring(start, pos);
            if (raw.isEmpty() || raw.equals("-")) {
                throw new MobiumException("expected a value at offset " + start);
            }
            // Integers come back as Long so a coordinate does not arrive as
            // "540.0" when it is printed.
            if (!fractional) {
                try { return Long.parseLong(raw); } catch (NumberFormatException ignored) { }
            }
            return Double.parseDouble(raw);
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
        return v instanceof Number ? ((Number) v).intValue() : 0;
    }

    static boolean bool(Map<String, Object> m, String key) {
        return Boolean.TRUE.equals(m.get(key));
    }

    static double dbl(Map<String, Object> m, String key) {
        Object v = m.get(key);
        return v instanceof Number ? ((Number) v).doubleValue() : 0d;
    }
}
