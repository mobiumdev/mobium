package dev.mobium.reader;

import android.accessibilityservice.AccessibilityServiceInfo;
import android.app.UiAutomation;
import android.graphics.Point;
import android.graphics.Rect;
import android.os.Build;
import android.os.HandlerThread;
import android.os.Looper;
import android.view.Display;
import android.view.accessibility.AccessibilityNodeInfo;
import java.io.PrintStream;
import java.lang.reflect.Constructor;
import java.lang.reflect.Method;

// Mobium's own reader of the screen: what `uiautomator dump` writes, read over
// a UiAutomation connection opened with FLAG_DONT_SUPPRESS_ACCESSIBILITY_SERVICES.
//
// `uiautomator dump` connects with no flags, and a connection with no flags
// suppresses every other accessibility service for as long as it is open: on a
// Fire TV, VoiceView was unbound for each read, and an app that publishes its
// contents only to a screen reader stopped publishing them. Somebody who
// relies on TalkBack or VoiceView loses it while the read runs. This reader
// leaves them running.
//
// Run from the shell, as uiautomator is:
//
//   CLASSPATH=<dir>/reader.dex app_process /system/bin dev.mobium.reader.Main
//
// It prints the hierarchy between BEGIN and END marker lines, so a vendor
// library that writes to stdout first — a Fire TV's does — cannot corrupt it,
// and reports a failure on a line starting ERROR_MARK. The format is
// AccessibilityNodeInfoDumper's: the same elements and attributes, nodes not
// visible to the user left out, bounds clipped to the display.
public final class Main {
    static final String BEGIN = "MOBIUM-READER-BEGIN";
    static final String END = "MOBIUM-READER-END";
    static final String ERROR_MARK = "MOBIUM-READER-ERROR: ";

    // UiAutomation.FLAG_DONT_SUPPRESS_ACCESSIBILITY_SERVICES, API 24.
    static final int DONT_SUPPRESS = 1;

    // main gives the process a main looper and runs the read beside it. Since
    // Android 14 or so, AccessibilityInteractionClient builds a Handler on the
    // main looper when the connection comes up, and a bare app_process has
    // none: on a Pixel 8 Pro on Android 17 the reader died with a
    // NullPointerException in Handler.<init> 150ms in. A Fire TV on Android 9
    // never asked for it.
    public static void main(String[] args) {
        Looper.prepareMainLooper();
        Thread worker = new Thread(new Runnable() {
            @Override
            public void run() {
                read();
            }
        }, "mobium-reader-main");
        worker.start();
        Looper.loop();
    }

    static void read() {
        PrintStream out;
        try {
            out = new PrintStream(System.out, false, "UTF-8");
        } catch (Exception e) {
            out = System.out;
        }
        HandlerThread thread = new HandlerThread("mobium-reader");
        thread.start();
        UiAutomation ua = null;
        try {
            if (Build.VERSION.SDK_INT < 24) {
                throw new IllegalStateException("API " + Build.VERSION.SDK_INT
                    + " cannot open a connection that leaves screen readers running (needs 24)");
            }
            ua = connect(thread.getLooper());
            AccessibilityServiceInfo info = ua.getServiceInfo();
            info.flags |= AccessibilityServiceInfo.FLAG_INCLUDE_NOT_IMPORTANT_VIEWS
                | AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS
                | AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS;
            ua.setServiceInfo(info);
            boolean idle = true;
            try {
                ua.waitForIdle(500, 3000);
            } catch (Exception e) {
                idle = false;
            }
            // A fresh connection has no window state until the system sends it,
            // so the first asks can return null: on a Fire TV a cold read
            // found no active window for five seconds and the next one at
            // once. The window list is asked as well, and whichever answers
            // first is used; "via" says which, and after how many tries.
            AccessibilityNodeInfo root = null;
            String via = "";
            for (int i = 0; i < 50 && root == null; i++) {
                root = ua.getRootInActiveWindow();
                via = "active-window/" + (i + 1);
                if (root == null) {
                    root = activeWindowRoot(ua);
                    via = "window-list/" + (i + 1);
                }
                if (root == null) {
                    Thread.sleep(200);
                }
            }
            if (root == null) {
                throw new IllegalStateException("no active window from UiAutomation in 10s");
            }
            Point size = new Point();
            int rotation = displayInfo(size);
            StringBuilder xml = new StringBuilder(64 * 1024);
            xml.append("<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>");
            xml.append("<hierarchy rotation=\"").append(rotation).append('"');
            if (!idle) {
                xml.append(" idle=\"false\"");
            }
            xml.append(" via=\"").append(via).append('"');
            xml.append('>');
            dump(xml, root, 0, size.x, size.y);
            xml.append("</hierarchy>");
            out.println(BEGIN);
            out.println(xml);
            out.println(END);
        } catch (Throwable t) {
            Throwable cause = t.getCause() != null ? t.getCause() : t;
            out.println(ERROR_MARK + cause.getClass().getSimpleName() + ": " + cause.getMessage());
        } finally {
            if (ua != null) {
                try {
                    Method disconnect = UiAutomation.class.getDeclaredMethod("disconnect");
                    disconnect.setAccessible(true);
                    disconnect.invoke(ua);
                } catch (Throwable ignored) {
                    // The process is about to exit, which closes the connection anyway.
                }
            }
            thread.quitSafely();
            out.flush();
        }
        System.exit(0);
    }

    // connect opens the connection `uiautomator` itself opens, with the flag it
    // does not pass. Both the constructor and connect(int) are hidden API,
    // reachable from a shell process.
    static UiAutomation connect(Looper looper) throws Exception {
        Class<?> connCls = Class.forName("android.app.UiAutomationConnection");
        Object conn = connCls.getDeclaredConstructor().newInstance();
        Class<?> iconn = Class.forName("android.app.IUiAutomationConnection");
        Constructor<UiAutomation> c = UiAutomation.class.getDeclaredConstructor(Looper.class, iconn);
        c.setAccessible(true);
        UiAutomation ua = c.newInstance(looper, conn);
        Method connect = UiAutomation.class.getDeclaredMethod("connect", int.class);
        connect.setAccessible(true);
        connect.invoke(ua, DONT_SUPPRESS);
        return ua;
    }

    // activeWindowRoot is the root of the window the system calls active, from
    // the window list rather than the active-window shortcut.
    static AccessibilityNodeInfo activeWindowRoot(UiAutomation ua) {
        for (android.view.accessibility.AccessibilityWindowInfo w : ua.getWindows()) {
            if (w.isActive()) {
                return w.getRoot();
            }
        }
        return null;
    }

    // displayInfo fills in the default display's real size and returns its
    // rotation, through DisplayManagerGlobal: a shell process has no Context.
    static int displayInfo(Point size) throws Exception {
        Class<?> dmg = Class.forName("android.hardware.display.DisplayManagerGlobal");
        Object global = dmg.getMethod("getInstance").invoke(null);
        Display display = (Display) dmg.getMethod("getRealDisplay", int.class).invoke(global, Display.DEFAULT_DISPLAY);
        display.getRealSize(size);
        return display.getRotation();
    }

    static void dump(StringBuilder xml, AccessibilityNodeInfo node, int index, int width, int height) {
        Rect bounds = new Rect();
        node.getBoundsInScreen(bounds);
        if (!bounds.intersect(0, 0, width, height)) {
            bounds.setEmpty();
        }
        xml.append("<node index=\"").append(index).append('"');
        attr(xml, "text", node.getText());
        attr(xml, "resource-id", node.getViewIdResourceName());
        attr(xml, "class", node.getClassName());
        attr(xml, "package", node.getPackageName());
        attr(xml, "content-desc", node.getContentDescription());
        bool(xml, "checkable", node.isCheckable());
        bool(xml, "checked", node.isChecked());
        bool(xml, "clickable", node.isClickable());
        bool(xml, "enabled", node.isEnabled());
        bool(xml, "focusable", node.isFocusable());
        bool(xml, "focused", node.isFocused());
        bool(xml, "scrollable", node.isScrollable());
        bool(xml, "long-clickable", node.isLongClickable());
        bool(xml, "password", node.isPassword());
        bool(xml, "selected", node.isSelected());
        if (Build.VERSION.SDK_INT >= 26) {
            attr(xml, "hint", node.getHintText());
            bool(xml, "showing-hint", node.isShowingHintText());
        }
        if (Build.VERSION.SDK_INT >= 24) {
            xml.append(" drawing-order=\"").append(node.getDrawingOrder()).append('"');
        }
        xml.append(" bounds=\"[").append(bounds.left).append(',').append(bounds.top).append("][")
            .append(bounds.right).append(',').append(bounds.bottom).append("]\">");
        int n = node.getChildCount();
        for (int i = 0; i < n; i++) {
            AccessibilityNodeInfo child = node.getChild(i);
            if (child == null) {
                continue;
            }
            if (child.isVisibleToUser()) {
                dump(xml, child, i, width, height);
            }
            child.recycle();
        }
        xml.append("</node>");
    }

    static void bool(StringBuilder xml, String name, boolean v) {
        xml.append(' ').append(name).append("=\"").append(v).append('"');
    }

    // attr writes an attribute, escaped, with characters XML cannot carry
    // dropped, as the platform dumper does.
    static void attr(StringBuilder xml, String name, CharSequence v) {
        xml.append(' ').append(name).append("=\"");
        if (v != null) {
            for (int i = 0; i < v.length(); i++) {
                char ch = v.charAt(i);
                switch (ch) {
                    case '&': xml.append("&amp;"); break;
                    case '<': xml.append("&lt;"); break;
                    case '>': xml.append("&gt;"); break;
                    case '"': xml.append("&quot;"); break;
                    case '\n': xml.append("&#10;"); break;
                    case '\r': xml.append("&#13;"); break;
                    case '\t': xml.append("&#9;"); break;
                    default:
                        if (ch >= 0x20 && ch != 0xFFFE && ch != 0xFFFF) {
                            xml.append(ch);
                        }
                }
            }
        }
        xml.append('"');
    }
}
