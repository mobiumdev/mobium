package dev.mobium;

/** An on-screen rectangle in device pixels, on both platforms. */
public record Bounds(int x1, int y1, int x2, int y2) {

    /** The x coordinate a tap targets. */
    public int centerX() { return (x1 + x2) / 2; }

    /** The y coordinate a tap targets. */
    public int centerY() { return (y1 + y2) / 2; }

    public int width()  { return x2 - x1; }
    public int height() { return y2 - y1; }

    static Bounds from(java.util.Map<String, Object> m) {
        return new Bounds(Json.integer(m, "x1"), Json.integer(m, "y1"),
                          Json.integer(m, "x2"), Json.integer(m, "y2"));
    }

    @Override public String toString() {
        return "[" + x1 + "," + y1 + "][" + x2 + "," + y2 + "]";
    }
}
