using System.Collections.Generic;
using System.Globalization;

namespace Mobium
{
    /// <summary>An on-screen rectangle in device pixels, on both platforms.</summary>
    public sealed class Bounds
    {
        /// <summary>Left edge.</summary>
        public int X1 { get; }

        /// <summary>Top edge.</summary>
        public int Y1 { get; }

        /// <summary>Right edge.</summary>
        public int X2 { get; }

        /// <summary>Bottom edge.</summary>
        public int Y2 { get; }

        /// <summary>Builds a rectangle from its two corners.</summary>
        public Bounds(int x1, int y1, int x2, int y2)
        {
            X1 = x1; Y1 = y1; X2 = x2; Y2 = y2;
        }

        /// <summary>The x coordinate a tap targets.</summary>
        public int CenterX => (X1 + X2) / 2;

        /// <summary>The y coordinate a tap targets.</summary>
        public int CenterY => (Y1 + Y2) / 2;

        /// <summary>How wide it is, in device pixels.</summary>
        public int Width => X2 - X1;

        /// <summary>How tall it is, in device pixels.</summary>
        public int Height => Y2 - Y1;

        internal static Bounds From(IDictionary<string, object> m) =>
            new Bounds(Json.Integer(m, "x1"), Json.Integer(m, "y1"),
                       Json.Integer(m, "x2"), Json.Integer(m, "y2"));

        /// <summary>The two corners, as mobium prints them.</summary>
        public override string ToString() =>
            string.Format(CultureInfo.InvariantCulture, "[{0},{1}][{2},{3}]", X1, Y1, X2, Y2);
    }
}
