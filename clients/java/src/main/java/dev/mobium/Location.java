package dev.mobium;

/**
 * Where a device believes it is.
 *
 * <p>{@code mock} and {@code mocking} answer different questions and disagree
 * after a clear: the first says this fix was injected, the second says a test
 * provider is installed now. Android keeps its last known position after the
 * provider supplying it is removed, so a read straight after clearing reports
 * an injected fix from a provider that no longer exists.
 *
 * <p>{@code known} is false when the platform cannot report a position at all,
 * as on iOS, which is different from the device having none.
 */
public final class Location {
    /** Degrees north; negative is south. */
    public final double latitude;
    /** Degrees east; negative is west. */
    public final double longitude;
    /** This fix was injected by a test provider. */
    public final boolean mock;
    /** A test provider is installed now. */
    public final boolean mocking;
    /** The platform could report a position at all. */
    public final boolean known;

    Location(double latitude, double longitude, boolean mock, boolean mocking, boolean known) {
        this.latitude = latitude;
        this.longitude = longitude;
        this.mock = mock;
        this.mocking = mocking;
        this.known = known;
    }

    @Override
    public String toString() {
        return "Location[" + latitude + "," + longitude + " mock=" + mock
                + " mocking=" + mocking + " known=" + known + "]";
    }
}
