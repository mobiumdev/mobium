// The connection, against fake-mobium.mjs standing in for mobium, with no
// test framework and no device: `node clients/javascript/test/connection.test.mjs`.
// make ci runs it.
import * as m from '../index.js'
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const failures = []
let checks = 0
const check = (cond, what) => { checks++; if (!cond) failures.push(what) }
// An uncaught exception is exactly the failure several of these guard
// against; record it as one rather than letting it end the run silently.
process.on('uncaughtException', (e) => { failures.push(`uncaught ${e.name}: ${e.message}`); report() })
// A regression here tends to be a wait that never ends, and a suite that
// hangs is a CI job that times out saying nothing. Fail it instead.
setTimeout(() => { failures.push('the suite did not finish within 90s: a call is waiting forever'); report() }, 90000).unref()

const here = dirname(fileURLToPath(import.meta.url))
const dir = mkdtempSync(join(tmpdir(), 'mobium-fake-'))
function launcher() {
  if (process.platform === 'win32') {
    const p = join(dir, 'mobium.cmd')
    writeFileSync(p, `@"${process.execPath}" "${join(here, 'fake-mobium.mjs')}" %*\r\n`)
    return p
  }
  const p = join(dir, 'mobium')
  writeFileSync(p, `#!/bin/sh\nexec '${process.execPath}' '${join(here, 'fake-mobium.mjs')}' "$@"\n`)
  chmodSync(p, 0o755)
  return p
}
const FAKE = launcher()
const fake = (mode, opts = {}) => {
  process.env.MOBIUM_FAKE = mode
  process.env.MOBIUM_FAKE_PIDFILE = opts.pidfile || ''
  return m.connect({ binary: FAKE, callTimeoutMs: opts.timeout })
}
const settle = (p) => p.then((v) => ({ ok: true, v }), (e) => ({ ok: false, e }))
const within = (p, ms) => Promise.race([settle(p), new Promise((r) => setTimeout(() => r({ pending: true }), ms))])

// -- what is not the answer is skipped, without throwing -----------------
{
  const d = await fake('ok')
  const r = await d.conn.callTool('app_echo', { k: 'v' })
  check(r.structuredContent.tool === 'app_echo', 'the answer was not found past the lines that are not it')
  check(r.structuredContent.echo.k === 'v', 'arguments did not arrive')
  const all = await Promise.all([...Array(8).keys()].map((n) => d.conn.callTool('slow', { n }).then((x) => x.structuredContent.echo.n)))
  check(JSON.stringify(all) === '[0,1,2,3,4,5,6,7]', `8 concurrent calls did not each get their own answer: ${all}`)
  await d.close()
}

// -- an element carries its checked state ---------------------------------
{
  const d = await fake('ok')
  const [box, sw, button, add] = await d.map()
  check(box.checked === true && sw.checked === false, `checked states lost: ${box.checked} ${sw.checked}`)
  check(button.checked === null, 'a button reported a checked state; null means it has none')
  check(add.disabled === true && button.disabled === false, `disabled states: ${add.disabled} ${button.disabled}`)
  await d.close()
}

// -- NaN is refused, not sent as null -------------------------------------
{
  const d = await fake('ok')
  const r = await settle(d.setLocation(NaN, 0))
  check(!r.ok && r.e instanceof m.InvalidArgumentError, `setLocation(NaN) was sent: ${r.ok ? JSON.stringify(r.v) : r.e}`)
  check((await d.conn.callTool('app_current')).structuredContent.tool === 'app_current', 'the connection broke after refusing NaN')
  await d.close()
}

// -- an answer with no id fails the call in flight ------------------------
{
  const d = await fake('noid', { timeout: 5000 })
  const r = await settle(d.conn.callTool('app_map'))
  check(!r.ok && r.e instanceof m.InvalidArgumentError && /could not read the request/.test(r.e.message),
    `an answer with no id did not fail the call: ${r.ok ? 'it resolved' : r.e}`)
  check((await d.conn.callTool('app_current')).structuredContent.tool === 'app_current', 'the connection fell out of step')
  await d.close()
}

// -- a timed-out call ends the connection --------------------------------
{
  check(!(await settle(m.connect({ binary: FAKE, callTimeoutMs: 0 }))).ok, 'callTimeoutMs: 0 was accepted')
  const d = await fake('hang', { timeout: 500 })
  const start = Date.now()
  const r = await within(d.conn.callTool('app_map'), 5000)
  check(r.ok === false && Date.now() - start < 5000, 'a call with no answer did not time out')
  check(r.e && /callTimeoutMs/.test(r.e.message), `the timeout does not say what to do: ${r.e}`)
  const next = await settle(d.conn.callTool('app_map'))
  check(!next.ok && /no longer usable/.test(next.e.message), `the next call was not refused: ${next.e}`)
  await d.close()
}

// -- an exit mid-call ------------------------------------------------------
{
  const d = await fake('exit')
  const r = await settle(d.conn.callTool('app_map'))
  check(!r.ok && /status 3/.test(r.e.message), `an exit mid-call does not name its status: ${r.e}`)
  const next = await settle(d.conn.callTool('app_map'))
  check(!next.ok && /no longer usable/.test(next.e.message), `the connection did not stay closed: ${next.e}`)
  await d.close()
}

// -- a failed handshake leaves no process --------------------------------
{
  const start = Date.now()
  const silent = await within(fake('mute', { timeout: 500 }), 5000)
  check(silent.ok === false && Date.now() - start < 5000, 'a silent handshake did not fail within callTimeoutMs')
  // Refused rather than silent: a timeout kills the process on its own, so
  // only a handshake that fails some other way shows whether connect cleans
  // up after itself. Measured before the fix: it did not.
  const pidfile = join(dir, 'pid')
  const r = await settle(fake('refuse', { pidfile }))
  check(!r.ok, 'a refused handshake did not fail')
  const pid = Number(readFileSync(pidfile, 'utf8'))
  let gone = false
  for (let i = 0; i < 50 && !gone; i++) {
    try { process.kill(pid, 0); await new Promise((res) => setTimeout(res, 100)) } catch { gone = true }
  }
  check(gone, `a refused handshake left mobium running (pid ${pid})`)
}

// -- a binary that cannot start is a rejection, not a crash --------------
{
  const bad = join(dir, 'cannot-start')
  writeFileSync(bad, '#!/nonexistent/interpreter\n')
  chmodSync(bad, 0o755)
  const r = await within(m.connect({ binary: bad }), 5000)
  check(r.ok === false && r.e instanceof m.MobiumError, `a binary that cannot start did not reject cleanly: ${JSON.stringify(r)}`)
}

// -- close is idempotent, and a call after it rejects rather than crashes -
{
  const d = await fake('hang')
  const waiting = settle(d.conn.callTool('app_map'))
  await new Promise((r) => setTimeout(r, 300))
  await d.close()
  // waiting is already settled into {ok, e}; race it as it is.
  const w = await Promise.race([waiting, new Promise((r) => setTimeout(() => r({ pending: true }), 15000))])
  check(w.ok === false && /closed/.test(w.e.message), `close did not end a waiting call: ${JSON.stringify(w)}`)
  await d.close()
  const after = await within(d.conn.callTool('app_map'), 3000)
  check(after.ok === false && /closed/.test(after.e.message), `a call after close does not say it was closed: ${JSON.stringify(after)}`)
}

// -- start opens the session, quit ends it --------------------------------
{
  process.env.MOBIUM_FAKE = 'ok'
  const d = await m.start({ platform: 'ios', app: 'com.apple.Preferences', binary: FAKE })
  const s = d.session
  check(s && s.device === 'fake-device' && s.platform === 'ios' && s.driver === 'wda' && s.app === 'com.apple.Preferences',
    `start did not report the device, platform, driver and app it was given: ${JSON.stringify(s)}`)
  await d.quit()
  check((await settle(d.quit())).ok, 'a second quit rejected')
  const after = await within(d.map(), 3000)
  check(after.ok === false, 'a call after quit succeeded')
  const c = await m.connect({ binary: FAKE })
  check(c.session === null, 'connect reported a session it did not start')
  await c.close()
}

// -- close() says it is leaving on purpose ------------------------------------
// mobium ends the sessions a client started when the client goes away, and a
// crash closes stdin just as close() does; without the detach, close() quits.
{
  const log = join(dir, 'notify.log')
  process.env.MOBIUM_FAKE = 'ok'
  process.env.MOBIUM_FAKE_NOTIFYLOG = log
  const d = await m.connect({ binary: FAKE })
  await d.close()
  process.env.MOBIUM_FAKE_NOTIFYLOG = ''
  let sent = []
  try { sent = readFileSync(log, 'utf8').split('\n').filter(Boolean) } catch {}
  check(sent.join(',') === 'session=,notifications/initialized,mobium/detach',
    `close() sent ${JSON.stringify(sent)}, not the handshake's notification and then mobium/detach`)
}

// -- session gives the connection a daemon of its own --------------------------
{
  const log = join(dir, 'session.log')
  process.env.MOBIUM_FAKE = 'ok'
  process.env.MOBIUM_FAKE_NOTIFYLOG = log
  process.env.MOBIUM_SESSION = 'outer'
  await (await m.connect({ binary: FAKE, session: 'run7' })).close()
  delete process.env.MOBIUM_SESSION
  process.env.MOBIUM_FAKE_NOTIFYLOG = ''
  let sent = []
  try { sent = readFileSync(log, 'utf8').split('\n').filter(Boolean) } catch {}
  check(sent[0] === 'session=run7', `the pipe saw ${JSON.stringify(sent[0])}, want MOBIUM_SESSION=run7 over the environment's`)
}

// -- tap sends what index.d.ts says it takes ------------------------------
// tap({x, y}, {fingers}) once read the options as the point: x and y went
// out undefined and the fingers were dropped.
{
  const d = await fake('ok')
  const sent = []
  const call = d.conn.callTool.bind(d.conn)
  d.conn.callTool = (name, args) => { sent.push([name, args]); return call(name, args) }
  await d.tap('@e2')
  await d.tap('@e2', { fingers: 2 })
  await d.tap({ x: 540, y: 1200 })
  await d.tap({ x: 540, y: 1200 }, { fingers: 3 })
  await d.doubleTap('@e2')
  await d.doubleTap({ x: 10, y: 20 })
  const want = [
    { target: '@e2' },
    { target: '@e2', fingers: 2 },
    { x: 540, y: 1200 },
    { x: 540, y: 1200, fingers: 3 },
    { target: '@e2', double: true },
    { x: 10, y: 20, double: true },
  ]
  want.forEach((w, i) => {
    const got = sent[i] && sent[i][0] === 'app_tap' ? sent[i][1] : null
    check(JSON.stringify(got) === JSON.stringify(w), `tap form ${i + 1} sent ${JSON.stringify(got)}, want ${JSON.stringify(w)}`)
  })
  const refused = await settle(d.tap())
  check(!refused.ok && refused.e instanceof m.MobiumError, 'tap() with nothing to tap was not refused')
  await d.close()
}

// -- upload, download and downloads send only what is set ------------------
// download with no path answers with the file base64 in `data`; the caller
// gets bytes, not the encoding.
{
  const d = await fake('ok')
  const sent = []
  const call = d.conn.callTool.bind(d.conn)
  const bytes = Buffer.from([0x25, 0x50, 0x44, 0x46, 0x00, 0xff, 0x0a])
  d.conn.callTool = async (name, args) => {
    sent.push([name, args])
    if (name === 'app_download' && args.name && !args.path) {
      return { content: [{ type: 'text', text: 'downloaded' }], structuredContent: { device: 'emulator-5554', name: args.name, where: 'Download', bytes: bytes.length, checked: 'size', data: bytes.toString('base64') } }
    }
    if (name === 'app_download' && !args.name) {
      return { content: [{ type: 'text', text: 'listed' }], structuredContent: { device: 'emulator-5554', folder: 'Download', files: [{ name: 'a.pdf', bytes: 7, modified: '2026-09-29T10:00:00Z' }] } }
    }
    return call(name, args)
  }
  await d.upload('in.pdf')
  await d.upload('in.pdf', { name: 'report.pdf', app: 'com.example' })
  const saved = await d.download('a.pdf', { path: 'out.pdf' })
  await d.download('a.pdf', { path: 'out.pdf', app: 'com.example' })
  const got = await d.download('a.pdf')
  await d.download('a.pdf', { app: 'com.example' })
  const files = await d.downloads()
  await d.downloads({ app: 'com.example' })
  const want = [
    ['app_upload', { path: 'in.pdf' }],
    ['app_upload', { path: 'in.pdf', name: 'report.pdf', app: 'com.example' }],
    ['app_download', { name: 'a.pdf', path: 'out.pdf' }],
    ['app_download', { name: 'a.pdf', path: 'out.pdf', app: 'com.example' }],
    ['app_download', { name: 'a.pdf' }],
    ['app_download', { name: 'a.pdf', app: 'com.example' }],
    ['app_download', {}],
    ['app_download', { app: 'com.example' }],
  ]
  want.forEach((w, i) => {
    check(JSON.stringify(sent[i]) === JSON.stringify(w), `file call ${i + 1} sent ${JSON.stringify(sent[i])}, want ${JSON.stringify(w)}`)
  })
  check(saved.tool === 'app_download', `download with a path resolved to ${JSON.stringify(saved)}, not the transfer`)
  check(got instanceof Uint8Array && Buffer.compare(got, bytes) === 0, `download with no path resolved to ${JSON.stringify(got)}, not the file's bytes`)
  check(files.length === 1 && files[0].name === 'a.pdf', `downloads() resolved to ${JSON.stringify(files)}`)
  const refused = await settle(d.download())
  check(!refused.ok && refused.e instanceof m.MobiumError, 'download() with no name was not refused')
  check(sent.length === want.length, `download() with no name still called mobium: ${JSON.stringify(sent[want.length])}`)
  await d.close()
}

// -- mapDiff asks for the diff and answers with it, elements as map() --------
// `first` is the difference between "no earlier map" and "nothing changed":
// both have empty lists.
{
  const d = await fake('ok')
  const sent = []
  const el = (ref, label, x1 = 0) => ({ ref, label, role: 'button', locator: { kind: 'id', value: label }, bounds: { x1, y1: 0, x2: x1 + 100, y2: 50 } })
  const answers = [
    { first: true },
    { since: '2026-09-29T10:00:00Z', added: [el('@e3', 'New')], removed: [el('@e1', 'Gone')], changed: [{ before: el('@e2', 'Old'), after: el('@e2', 'Renamed', 20), what: ['label', 'moved'] }] },
  ]
  d.conn.callTool = async (name, args) => {
    sent.push([name, args])
    const diff = answers.shift()
    return { content: [{ type: 'text', text: 'mapped' }], structuredContent: { elements: [], context: 'NATIVE_APP', device: 'emulator-5554', diff } }
  }
  const first = await d.mapDiff()
  const next = await d.mapDiff()
  check(sent.length === 2, `two mapDiff calls made ${sent.length} tool calls`)
  sent.forEach((s, i) => {
    check(JSON.stringify(s) === JSON.stringify(['app_map', { diff: true }]), `mapDiff call ${i + 1} sent ${JSON.stringify(s)}, want app_map with {diff:true}`)
  })
  check(first.first === true && first.since === '' && first.added.length === 0 && first.removed.length === 0 && first.changed.length === 0,
    `the first mapDiff resolved to ${JSON.stringify(first)}`)
  check(next.first === false && next.since === '2026-09-29T10:00:00Z', `a later mapDiff said first=${next.first}, since=${JSON.stringify(next.since)}`)
  check(next.added.length === 1 && next.added[0].ref === '@e3' && next.added[0].locator === 'id=New' && next.added[0].bounds.center.x === 50 && next.added[0].checked === null,
    `added was not shaped as map() shapes it: ${JSON.stringify(next.added)}`)
  check(next.removed.length === 1 && next.removed[0].label === 'Gone', `removed resolved to ${JSON.stringify(next.removed)}`)
  const c = next.changed[0]
  check(next.changed.length === 1 && c.before.label === 'Old' && c.after.label === 'Renamed' && c.after.bounds.center.x === 70 && JSON.stringify(c.what) === '["label","moved"]',
    `changed resolved to ${JSON.stringify(next.changed)}`)
  await d.close()
}

// -- trace sends only what is set ----------------------------------------------
// screenshots and maps default to true on the device, so false has to go out
// and an unset one must not.
{
  const d = await fake('ok')
  const sent = []
  const call = d.conn.callTool.bind(d.conn)
  d.conn.callTool = (name, args) => { sent.push([name, args]); return call(name, args) }
  await d.traceStart()
  await d.traceStart({ name: 'login', screenshots: false, maps: false })
  await d.traceStart({ screenshots: true })
  await d.trace()
  await d.traceStop('trace.zip')
  const want = [
    ['app_trace', { action: 'start' }],
    ['app_trace', { action: 'start', name: 'login', screenshots: false, maps: false }],
    ['app_trace', { action: 'start', screenshots: true }],
    ['app_trace', {}],
    ['app_trace', { action: 'stop', path: 'trace.zip' }],
  ]
  want.forEach((w, i) => {
    check(JSON.stringify(sent[i]) === JSON.stringify(w), `trace call ${i + 1} sent ${JSON.stringify(sent[i])}, want ${JSON.stringify(w)}`)
  })
  check(sent.length === want.length, `trace made ${sent.length} calls, want ${want.length}`)
  await d.close()
}

report()

function report() {
  rmSync(dir, { recursive: true, force: true })
  if (failures.length) { console.log(failures.map((f) => 'FAIL: ' + f).join('\n')); process.exit(1) }
  console.log(`javascript connection: ${checks} checks passed`)
  process.exit(0)
}
