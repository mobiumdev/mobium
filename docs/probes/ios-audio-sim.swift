// ios-audio-sim: what an app in the iOS simulator plays, heard from the Mac.
//
// An app in the simulator is a process on the Mac, and CoreAudio lists it
// by its own bundle id once it has opened audio. A process tap (macOS 14.2+)
// on that process hears that app alone, muted on the Mac's speakers while
// tapped. The first run asks for "System Audio Recording" for the app that
// runs this; until it is allowed the tap gets no IO cycles at all.
//
//   swiftc -O docs/probes/ios-audio-sim.swift -o /tmp/ios-audio-sim
//   /tmp/ios-audio-sim dev.mobium.mobiumapp 8 heard.wav &
//   mobium tap testid=audioSequence        # MobiumApp's Audio Demo
//   python3 docs/probes/audio-runs.py heard.wav
//
// Two simulators running the same app share its bundle id; the process's
// path holds the simulator's UDID. ROADMAP, "Audio", has what it measured.
import AudioToolbox
import CoreAudio
import Foundation

func fail(_ s: String) -> Never { FileHandle.standardError.write((s + "\n").data(using: .utf8)!); exit(1) }
func addr(_ s: AudioObjectPropertySelector) -> AudioObjectPropertyAddress {
    AudioObjectPropertyAddress(mSelector: s, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
}
func procs() -> [AudioObjectID] {
    var a = addr(kAudioHardwarePropertyProcessObjectList); var size: UInt32 = 0
    AudioObjectGetPropertyDataSize(AudioObjectID(kAudioObjectSystemObject), &a, 0, nil, &size)
    var out = [AudioObjectID](repeating: 0, count: Int(size) / 4)
    AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &a, 0, nil, &size, &out)
    return out
}
func bundle(_ o: AudioObjectID) -> String {
    var a = addr(kAudioProcessPropertyBundleID); var s: Unmanaged<CFString>?; var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
    guard AudioObjectGetPropertyData(o, &a, 0, nil, &size, &s) == noErr, let v = s else { return "" }
    return v.takeRetainedValue() as String
}
let args = CommandLine.arguments
guard args.count == 4, let secs = Double(args[2]) else { fail("usage: ios-audio-sim <bundle-id> <seconds> <out.wav>") }
let targets = procs().filter { bundle($0) == args[1] }
if targets.isEmpty { fail("no audio process with bundle id \(args[1])") }
print("tapping process objects \(targets)")

let desc = CATapDescription(stereoMixdownOfProcesses: targets)
desc.uuid = UUID()
desc.muteBehavior = .mutedWhenTapped
desc.isPrivate = true
var tap = AudioObjectID(0)
var st = AudioHardwareCreateProcessTap(desc, &tap)
if st != noErr { fail("AudioHardwareCreateProcessTap: \(st)") }

var fa = addr(kAudioTapPropertyFormat)
var asbd = AudioStreamBasicDescription(); var fsize = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
st = AudioObjectGetPropertyData(tap, &fa, 0, nil, &fsize, &asbd)
print("tap format: \(asbd.mSampleRate) Hz, \(asbd.mChannelsPerFrame) ch, \(asbd.mBitsPerChannel) bit, flags \(asbd.mFormatFlags)")

let agg: [String: Any] = [
    kAudioAggregateDeviceNameKey: "ios-audio-sim",
    kAudioAggregateDeviceUIDKey: UUID().uuidString,
    kAudioAggregateDeviceIsPrivateKey: true,
    kAudioAggregateDeviceTapAutoStartKey: true,
    kAudioAggregateDeviceTapListKey: [[kAudioSubTapUIDKey: desc.uuid.uuidString, kAudioSubTapDriftCompensationKey: true]],
]
var dev = AudioObjectID(0)
st = AudioHardwareCreateAggregateDevice(agg as CFDictionary, &dev)
if st != noErr { fail("AudioHardwareCreateAggregateDevice: \(st)") }

var samples = [Float]()
let lock = NSLock()
var cycles = 0
var proc: AudioDeviceIOProcID?
st = AudioDeviceCreateIOProcIDWithBlock(&proc, dev, nil) { _, inData, _, _, _ in
    let abl = UnsafeMutableAudioBufferListPointer(UnsafeMutablePointer(mutating: inData))
    lock.lock(); defer { lock.unlock() }
    cycles += 1
    for b in abl {
        guard let p = b.mData else { continue }
        let n = Int(b.mDataByteSize) / 4
        samples.append(contentsOf: UnsafeBufferPointer(start: p.assumingMemoryBound(to: Float.self), count: n))
    }
}
if st != noErr { fail("AudioDeviceCreateIOProcIDWithBlock: \(st)") }
st = AudioDeviceStart(dev, proc)
if st != noErr { fail("AudioDeviceStart: \(st)") }
let t0 = Date()
Thread.sleep(forTimeInterval: secs)
AudioDeviceStop(dev, proc)
AudioDeviceDestroyIOProcID(dev, proc!)
AudioHardwareDestroyAggregateDevice(dev)
AudioHardwareDestroyProcessTap(tap)

lock.lock()
let ch = Int(asbd.mChannelsPerFrame), rate = Int(asbd.mSampleRate)
var peak: Float = 0; for s in samples { peak = max(peak, abs(s)) }
print("captured \(samples.count / max(ch, 1)) frames in \(cycles) IO cycles over \(String(format: "%.2f", Date().timeIntervalSince(t0))) s, peak \(peak)")
// WAV, IEEE float, interleaved as delivered.
var d = Data()
func u32(_ v: UInt32) { var x = v.littleEndian; d.append(Data(bytes: &x, count: 4)) }
func u16(_ v: UInt16) { var x = v.littleEndian; d.append(Data(bytes: &x, count: 2)) }
let bytes = samples.count * 4
d.append("RIFF".data(using: .ascii)!); u32(UInt32(36 + bytes)); d.append("WAVEfmt ".data(using: .ascii)!)
u32(16); u16(3); u16(UInt16(ch)); u32(UInt32(rate)); u32(UInt32(rate * ch * 4)); u16(UInt16(ch * 4)); u16(32)
d.append("data".data(using: .ascii)!); u32(UInt32(bytes))
samples.withUnsafeBufferPointer { d.append(Data(buffer: $0)) }
lock.unlock()
try! d.write(to: URL(fileURLWithPath: args[3]))
print("wrote \(args[3])")
