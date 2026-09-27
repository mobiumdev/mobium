package dev.mobium;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;

/**
 * Starts {@link FakeMobium} as if it were the mobium binary. A Java class is
 * not an executable, so each mode gets a launcher script that runs it on this
 * JVM and classpath; and since Java cannot change its own environment, which
 * a started process inherits, the script carries the mode itself.
 */
final class FakeProcess {

    private FakeProcess() {}

    static String launcher(String mode, Path pidFile) {
        try {
            String java = Path.of(System.getProperty("java.home"), "bin", "java").toString();
            String cp = System.getProperty("java.class.path");
            String pid = pidFile == null ? "" : pidFile.toString();
            Path dir = Files.createTempDirectory("mobium-fake");
            Path script;
            if (System.getProperty("os.name", "").startsWith("Windows")) {
                script = dir.resolve("mobium.cmd");
                Files.writeString(script, "@set MOBIUM_FAKE=" + mode + "\r\n@set MOBIUM_FAKE_PIDFILE=" + pid + "\r\n"
                        + "@\"" + java + "\" -cp \"" + cp + "\" dev.mobium.FakeMobium %*\r\n");
            } else {
                script = dir.resolve("mobium");
                Files.writeString(script, "#!/bin/sh\nMOBIUM_FAKE='" + mode + "' MOBIUM_FAKE_PIDFILE='" + pid + "' "
                        + "exec '" + java + "' -cp '" + cp + "' dev.mobium.FakeMobium \"$@\"\n");
                if (!script.toFile().setExecutable(true)) throw new AssertionError("could not make " + script + " executable");
            }
            script.toFile().deleteOnExit();
            dir.toFile().deleteOnExit();
            return script.toString();
        } catch (IOException e) {
            throw new AssertionError(e);
        }
    }

    /** A bare connection, for the transport's own behavior. */
    static Connection connect(String mode, Duration timeout, Path pidFile) {
        return new Connection(launcher(mode, pidFile), List.of(), timeout);
    }

    /** A whole client, for what the public API checks before anything is sent. */
    static Mobium mobium(String mode) {
        return Mobium.builder().binary(launcher(mode, null)).connect();
    }
}
