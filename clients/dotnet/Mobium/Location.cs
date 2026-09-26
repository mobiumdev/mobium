namespace Mobium
{
    /// <summary>
    /// Where a device believes it is.
    /// </summary>
    /// <remarks>
    /// <c>Mock</c> and <c>Mocking</c> answer different questions and disagree
    /// after a clear: the first says this fix was injected, the second says a
    /// test provider is installed now. Android keeps its last known position
    /// after the provider supplying it is removed, so a read straight after
    /// clearing reports an injected fix from a provider that is gone.
    /// <c>Known</c> is false when the platform cannot report a position at
    /// all, as on iOS, which differs from the device having none.
    /// </remarks>
    public sealed class Location
    {
        /// <summary>Degrees north, -90 to 90.</summary>
        public double Latitude { get; }

        /// <summary>Degrees east, -180 to 180.</summary>
        public double Longitude { get; }

        /// <summary>Whether this fix came from a test provider.</summary>
        public bool Mock { get; }

        /// <summary>
        /// Whether a test provider is installed now. Differs from
        /// <see cref="Mock"/> after a clear.
        /// </summary>
        public bool Mocking { get; }

        /// <summary>
        /// Whether the platform can report a position at all. False on iOS,
        /// which differs from the device having none.
        /// </summary>
        public bool Known { get; }

        internal Location(double latitude, double longitude, bool mock, bool mocking, bool known)
        {
            Latitude = latitude;
            Longitude = longitude;
            Mock = mock;
            Mocking = mocking;
            Known = known;
        }

        /// <summary>The position and both mock flags, for diagnostics.</summary>
        public override string ToString() =>
            string.Format(System.Globalization.CultureInfo.InvariantCulture,
                "Location[{0},{1} mock={2} mocking={3} known={4}]",
                Latitude, Longitude, Mock, Mocking, Known);
    }
}
