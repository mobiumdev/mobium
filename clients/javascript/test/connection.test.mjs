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

report()

function report() {
  rmSync(dir, { recursive: true, force: true })
  if (failures.length) { console.log(failures.map((f) => 'FAIL: ' + f).join('\n')); process.exit(1) }
  console.log(`javascript connection: ${checks} checks passed`)
  process.exit(0)
}
