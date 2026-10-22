// mobium-sim-audio: what one app in one iOS simulator plays, heard on the
// Mac. Built by Mobium with Xcode's swiftc the first time it is needed and
// kept by the hash of this source (simaudio.go): Mobium is built without
// cgo, and CoreAudio's process taps are reached only from here.
//
//   mobium-sim-audio <bundle-id> <simulator-udid>
//
// An app in the simulator is a process on the Mac, which CoreAudio lists by
// its bundle id once it has opened audio, and a process tap (macOS 14.2+)
// on it hears that app alone, muted on the Mac's speakers while tapped. The
// process's path holds the simulator's UDID, which tells two simulators
// running the same app apart. docs/probes/ios-audio-sim.swift measured it.
//
// stdout is the tapped audio, float32 little-endian, interleaved, as the tap
// delivers it. stderr is one JSON object a line:
//   {"event":"waiting"}                        the app has not opened audio
//   {"event":"tapping","rate":48000,"channels":2,"pid":123}
//   {"event":"no-cycles"}                      tapping, and no audio arrives:
//                                              System Audio Recording is not
//                                              allowed for what runs this
//   {"event":"gone"}                           the app's audio process ended
//   {"event":"error","message":"..."}
// It stops, putting everything back, when stdin closes or on SIGTERM.
import AudioToolbox
import CoreAudio
import Darwin
import Foundation

func say(_ o: [String: Any]) {
    if let d = try? JSONSerialization.data(withJSONObject: o) {
        FileHandle.standardError.write(d + "\n".data(using: .utf8)!)
    }
}
func addr(_ s: AudioObjectPropertySelector) -> AudioObjectPropertyAddress {
    AudioObjectPropertyAddress(mSelector: s, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
}
func processObjects() -> [AudioObjectID] {
    var a = addr(kAudioHardwarePropertyProcessObjectList); var size: UInt32 = 0
    guard AudioObjectGetPropertyDataSize(AudioObjectID(kAudioObjectSystemObject), &a, 0, nil, &size) == noErr else { return [] }
    var out = [AudioObjectID](repeating: 0, count: Int(size) / 4)
    guard AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &a, 0, nil, &size, &out) == noErr else { return [] }
    return out
}
func bundleID(_ o: AudioObjectID) -> String {
    var a = addr(kAudioProcessPropertyBundleID); var s: Unmanaged<CFString>?
    var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
    guard AudioObjectGetPropertyData(o, &a, 0, nil, &size, &s) == noErr, let v = s else { return "" }
    return v.takeRetainedValue() as String
}
func pid(_ o: AudioObjectID) -> pid_t {
    var a = addr(kAudioProcessPropertyPID); var p: pid_t = -1; var size = UInt32(MemoryLayout<pid_t>.size)
    guard AudioObjectGetPropertyData(o, &a, 0, nil, &size, &p) == noErr else { return -1 }
    return p
}
func path(_ p: pid_t) -> String {
    var buf = [CChar](repeating: 0, count: 4096)
    guard proc_pidpath(p, &buf, UInt32(buf.count)) > 0 else { return "" }
    return String(cString: buf)
}

let args = CommandLine.arguments
guard args.count == 3 else {
    say(["event": "error", "message": "usage: mobium-sim-audio <bundle-id> <simulator-udid>"]); exit(2)
}
let bundle = args[1], udid = args[2]
let out = FileHandle.standardOutput
let writes = DispatchQueue(label: "writes")

final class Tap {
    var tap = AudioObjectID(0), device = AudioObjectID(0), proc: AudioDeviceIOProcID?
    var target = AudioObjectID(0)
    var cycles = 0
    let lock = NSLock()

    func start(_ object: AudioObjectID) -> Bool {
        target = object
        let desc = CATapDescription(stereoMixdownOfProcesses: [object])
        desc.uuid = UUID()
        desc.muteBehavior = .mutedWhenTapped
        desc.isPrivate = true
        guard AudioHardwareCreateProcessTap(desc, &tap) == noErr else {
            say(["event": "error", "message": "AudioHardwareCreateProcessTap failed"]); return false
        }
        var fa = addr(kAudioTapPropertyFormat)
        var asbd = AudioStreamBasicDescription(); var size = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
        AudioObjectGetPropertyData(tap, &fa, 0, nil, &size, &asbd)
        let agg: [String: Any] = [
            kAudioAggregateDeviceNameKey: "mobium-sim-audio",
            kAudioAggregateDeviceUIDKey: UUID().uuidString,
            kAudioAggregateDeviceIsPrivateKey: true,
            kAudioAggregateDeviceTapAutoStartKey: true,
            kAudioAggregateDeviceTapListKey: [[kAudioSubTapUIDKey: desc.uuid.uuidString, kAudioSubTapDriftCompensationKey: true]],
        ]
        guard AudioHardwareCreateAggregateDevice(agg as CFDictionary, &device) == noErr else {
            AudioHardwareDestroyProcessTap(tap); tap = 0
            say(["event": "error", "message": "AudioHardwareCreateAggregateDevice failed"]); return false
        }
        let st = AudioDeviceCreateIOProcIDWithBlock(&proc, device, nil) { [weak self] _, inData, _, _, _ in
            guard let self else { return }
            self.lock.lock(); self.cycles += 1; self.lock.unlock()
            var chunk = Data()
            for b in UnsafeMutableAudioBufferListPointer(UnsafeMutablePointer(mutating: inData)) {
                if let p = b.mData { chunk.append(Data(bytes: p, count: Int(b.mDataByteSize))) }
            }
            if !chunk.isEmpty { writes.async { out.write(chunk) } }
        }
        guard st == noErr, AudioDeviceStart(device, proc) == noErr else {
            stop(); say(["event": "error", "message": "the tap's device did not start"]); return false
        }
        say(["event": "tapping", "rate": asbd.mSampleRate, "channels": Int(asbd.mChannelsPerFrame), "pid": Int(pid(object))])
        return true
    }

    func stop() {
        if let p = proc { AudioDeviceStop(device, p); AudioDeviceDestroyIOProcID(device, p) }
        proc = nil
        if device != 0 { AudioHardwareDestroyAggregateDevice(device) }
        if tap != 0 { AudioHardwareDestroyProcessTap(tap) }
        device = 0; tap = 0; target = 0
    }
}

let tap = Tap()
let done = DispatchSemaphore(value: 0)
func finish() { tap.stop(); writes.sync {}; exit(0) }
signal(SIGTERM, SIG_IGN)
let term = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
term.setEventHandler { finish() }
term.resume()
// stdin closing is the stop.
Thread.detachNewThread {
    while !FileHandle.standardInput.availableData.isEmpty {}
    DispatchQueue.main.async { finish() }
}

// Find the app — this simulator's, by the UDID in its path — tap it, and
// watch: an app whose audio process ends is waited for again.
DispatchQueue.global().async {
    var saidWaiting = false, saidNoCycles = false
    var tappedAt = Date()
    while true {
        if tap.target == 0 {
            let found = processObjects().first { bundleID($0) == bundle && path(pid($0)).contains(udid) }
            if let o = found {
                DispatchQueue.main.sync { if tap.start(o) { tappedAt = Date(); saidNoCycles = false } }
            } else if !saidWaiting {
                say(["event": "waiting"]); saidWaiting = true
            }
        } else {
            if !processObjects().contains(tap.target) {
                DispatchQueue.main.sync { tap.stop() }
                say(["event": "gone"]); saidWaiting = false
                continue
            }
            tap.lock.lock(); let cycles = tap.cycles; tap.lock.unlock()
            if cycles == 0 && !saidNoCycles && Date().timeIntervalSince(tappedAt) > 2 {
                say(["event": "no-cycles"]); saidNoCycles = true
            }
        }
        Thread.sleep(forTimeInterval: 0.2)
    }
}
dispatchMain()
