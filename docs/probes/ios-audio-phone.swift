// ios-audio-phone: what an iPhone plays, heard over its USB cable.
//
// A USB iPhone offers the Mac a screen-capture source once CoreMediaIO is
// told to allow such devices, and the source carries the phone's sound as
// well as its screen. This takes the sound alone. While it runs the phone is
// silent and the Mac plays nothing: the capture is an audio route on the
// phone, which asks once what it is (headphones, a car, another device).
//
//   swiftc -O docs/probes/ios-audio-phone.swift -o /tmp/ios-audio-phone
//   /tmp/ios-audio-phone 10 heard.wav &     # the phone plugged in, unlocked
//   sleep 2.5; mobium tap testid=audioSequence
//   python3 docs/probes/audio-runs.py heard.wav
//
// Sound arrives about 1.3s after the capture opens, so start it first.
// ROADMAP, "Audio", has what it measured and what it did not.
import AVFoundation
import CoreMediaIO

var prop = CMIOObjectPropertyAddress(
  mSelector: CMIOObjectPropertySelector(kCMIOHardwarePropertyAllowScreenCaptureDevices),
  mScope: CMIOObjectPropertyScope(kCMIOObjectPropertyScopeGlobal),
  mElement: CMIOObjectPropertyElement(kCMIOObjectPropertyElementMain))
var allow: UInt32 = 1
CMIOObjectSetPropertyData(CMIOObjectID(kCMIOObjectSystemObject), &prop, 0, nil, UInt32(MemoryLayout<UInt32>.size), &allow)

func fail(_ s: String) -> Never { FileHandle.standardError.write((s + "\n").data(using: .utf8)!); exit(1) }
let a = CommandLine.arguments
guard a.count == 3, let secs = Double(a[1]) else { fail("usage: ios-audio-phone <seconds> <out.wav>") }

var dev: AVCaptureDevice?
let deadline = Date().addingTimeInterval(15)
while dev == nil && Date() < deadline {
  RunLoop.main.run(until: Date().addingTimeInterval(0.5))
  dev = AVCaptureDevice.DiscoverySession(deviceTypes: [.external], mediaType: .muxed, position: .unspecified).devices.first
}
guard let dev else { fail("no iPhone screen source") }
print("source: model \(dev.modelID), formats \(dev.formats.count)"); fflush(stdout)

final class Sink: NSObject, AVCaptureAudioDataOutputSampleBufferDelegate {
  var data = Data(); var buffers = 0; var first: CMTime?; var last = CMTime.zero
  var rate = 48000.0, ch = 2
  let lock = NSLock()
  func captureOutput(_ o: AVCaptureOutput, didOutput sb: CMSampleBuffer, from c: AVCaptureConnection) {
    lock.lock(); defer { lock.unlock() }
    if let f = CMSampleBufferGetFormatDescription(sb), let asbd = CMAudioFormatDescriptionGetStreamBasicDescription(f)?.pointee {
      rate = asbd.mSampleRate; ch = Int(asbd.mChannelsPerFrame)
      if buffers == 0 { print("format: \(asbd.mSampleRate) Hz, \(asbd.mChannelsPerFrame) ch, \(asbd.mBitsPerChannel) bit, flags \(asbd.mFormatFlags)"); fflush(stdout) }
    }
    let pts = CMSampleBufferGetPresentationTimeStamp(sb)
    if first == nil { first = pts }; last = pts
    buffers += 1
    guard let bb = CMSampleBufferGetDataBuffer(sb) else { return }
    var len = 0; var p: UnsafeMutablePointer<Int8>?
    CMBlockBufferGetDataPointer(bb, atOffset: 0, lengthAtOffsetOut: nil, totalLengthOut: &len, dataPointerOut: &p)
    if let p { data.append(UnsafeRawPointer(p).assumingMemoryBound(to: UInt8.self), count: len) }
  }
}
let session = AVCaptureSession()
session.addInput(try! AVCaptureDeviceInput(device: dev))
let out = AVCaptureAudioDataOutput()
out.audioSettings = [AVFormatIDKey: kAudioFormatLinearPCM, AVLinearPCMIsFloatKey: true, AVLinearPCMBitDepthKey: 32,
                     AVLinearPCMIsNonInterleaved: false, AVLinearPCMIsBigEndianKey: false]
let sink = Sink()
out.setSampleBufferDelegate(sink, queue: DispatchQueue(label: "audio"))
guard session.canAddOutput(out) else { fail("cannot add an audio output") }
session.addOutput(out)
session.startRunning()
print("capturing"); fflush(stdout)
let t0 = Date()
RunLoop.main.run(until: Date().addingTimeInterval(secs))
session.stopRunning()
sink.lock.lock()
let n = sink.data.count / 4
var peak: Float = 0
sink.data.withUnsafeBytes { r in for v in r.bindMemory(to: Float.self) { peak = max(peak, abs(v)) } }
print("captured \(n / max(sink.ch, 1)) frames in \(sink.buffers) buffers over \(String(format: "%.2f", Date().timeIntervalSince(t0))) s, peak \(peak)")
var d = Data()
func u32(_ v: UInt32) { var x = v.littleEndian; d.append(Data(bytes: &x, count: 4)) }
func u16(_ v: UInt16) { var x = v.littleEndian; d.append(Data(bytes: &x, count: 2)) }
let r = Int(sink.rate), ch = sink.ch
d.append("RIFF".data(using: .ascii)!); u32(UInt32(36 + sink.data.count)); d.append("WAVEfmt ".data(using: .ascii)!)
u32(16); u16(3); u16(UInt16(ch)); u32(UInt32(r)); u32(UInt32(r * ch * 4)); u16(UInt16(ch * 4)); u16(32)
d.append("data".data(using: .ascii)!); u32(UInt32(sink.data.count)); d.append(sink.data)
try! d.write(to: URL(fileURLWithPath: a[2]))
print("wrote \(a[2])")
