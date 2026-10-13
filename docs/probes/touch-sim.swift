// Does a Mac event tap see a click on the Simulator window, and where on
// the window? Listen-only, which needs no permission for mouse events;
// prints each left-button down and up with the window under it, and exits
// after `seconds`. Run beside the simulator's own log, which has the same
// touches in the app's points:
//
//   swiftc -O docs/probes/touch-sim.swift -o /tmp/touch-sim && /tmp/touch-sim 300
//   xcrun simctl spawn <udid> log stream --level debug --style compact | grep 'Digitizer token'
//
// ROADMAP, "A recording as code", item 6.
import CoreGraphics
import Foundation

let seconds = Double(CommandLine.arguments.dropFirst().first ?? "60") ?? 60

func windowUnder(_ p: CGPoint) -> String {
    guard let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] else {
        return "no window list"
    }
    for w in list { // front to back
        guard let b = w[kCGWindowBounds as String] as? [String: CGFloat],
              let layer = w[kCGWindowLayer as String] as? Int, layer == 0 else { continue }
        let r = CGRect(x: b["X"]!, y: b["Y"]!, width: b["Width"]!, height: b["Height"]!)
        if r.contains(p) {
            let owner = w[kCGWindowOwnerName as String] as? String ?? "?"
            return String(format: "%@ window [%.0f,%.0f %.0fx%.0f] at %.1f,%.1f in it",
                          owner, r.minX, r.minY, r.width, r.height, p.x - r.minX, p.y - r.minY)
        }
    }
    return "no window"
}

let mask = (1 << CGEventType.leftMouseDown.rawValue) | (1 << CGEventType.leftMouseUp.rawValue)
let cb: CGEventTapCallBack = { _, type, event, _ in
    let p = event.location
    let kind = type == .leftMouseDown ? "down" : type == .leftMouseUp ? "up" : "type \(type.rawValue)"
    print(String(format: "%.3f %@ %.1f,%.1f %@", Date().timeIntervalSince1970, kind, p.x, p.y, windowUnder(p)))
    fflush(stdout)
    return Unmanaged.passUnretained(event)
}
guard let tap = CGEvent.tapCreate(tap: .cgSessionEventTap, place: .headInsertEventTap,
                                  options: .listenOnly, eventsOfInterest: CGEventMask(mask),
                                  callback: cb, userInfo: nil) else {
    print("tapCreate failed: no permission to listen")
    exit(3)
}
let src = CFMachPortCreateRunLoopSource(nil, tap, 0)
CFRunLoopAddSource(CFRunLoopGetCurrent(), src, .commonModes)
CGEvent.tapEnable(tap: tap, enable: true)
print("listening for \(Int(seconds))s"); fflush(stdout)
DispatchQueue.main.asyncAfter(deadline: .now() + seconds) { exit(0) }
CFRunLoopRun()
