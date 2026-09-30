/**
 * Mobium — native mobile app automation on emulators, simulators and phones.
 *
 *   import { start } from 'mobium'
 *
 *   const device = await start({ platform: 'android', app: 'com.android.settings' })
 *   for (const el of await device.map()) console.log(el.ref, el.label)
 *   await device.tap('@e1')
 *   await device.quit()
 */

import { spawn } from 'node:child_process'
import { accessSync, constants, readFileSync, statSync } from 'node:fs'
import { delimiter, isAbsolute, join, resolve } from 'node:path'
import { createInterface } from 'node:readline'

/**
 * A tool reported that it could not do what was asked. Every failure carries
 * a stable code, the same in every client and on the wire (docs/decisions/0005
 * in the mobium repository), and each code has a subclass below, so a script
 * catches the kind it can handle:
 *
 *   try { await device.tap('text=Continue') }
 *   catch (e) { if (e instanceof NoSuchElementError) await device.scrollTo(...) else throw e }
 *
 * `remedy` says what to do about it, `retryable` whether the same call can
 * succeed if made again, and `details` holds machine-readable facts. A code
 * this client does not know, or a daemon too old to send one, is a plain
 * MobiumError with `code` set ("error" for the latter).
 */
export class MobiumError extends Error {
  static code = 'error'

  constructor(message, { code, remedy = '', retryable = false, details = {} } = {}) {
    super(message)
    this.name = new.target.name
    this.code = code ?? new.target.code
    this.remedy = remedy
    this.retryable = retryable
    this.details = details
  }
}

/** Nothing to drive: no device matches, or none is connected. */
export class NoDeviceError extends MobiumError {
  static code = 'no_device'
}

/** The device is there and cannot be driven yet: locked, not trusted, Developer Mode off. */
export class DeviceNotReadyError extends MobiumError {
  static code = 'device_not_ready'
}

/** Something on this machine is missing: adb, Xcode, a signing certificate. */
export class ToolchainMissingError extends MobiumError {
  static code = 'toolchain_missing'
}

/** A locator or ref matched nothing on screen. Worth scrolling for. */
export class NoSuchElementError extends MobiumError {
  static code = 'no_such_element'
}

/** A locator matched more than one element. Narrow it; Mobium never guesses. */
export class AmbiguousLocatorError extends MobiumError {
  static code = 'ambiguous_locator'
}

/** The element was found and cannot be touched where it is, usually off screen. */
export class ElementNotReachableError extends MobiumError {
  static code = 'element_not_reachable'
}

/** A WebView context that is not there. */
export class NoSuchContextError extends MobiumError {
  static code = 'no_such_context'
}

/** A dialog was expected and none is on screen. */
export class NoSuchAlertError extends MobiumError {
  static code = 'no_such_alert'
}

/** This driver or platform cannot do it, and says why. Retrying cannot help. */
export class UnsupportedError extends MobiumError {
  static code = 'unsupported'
}

/** The command reported success and reading the state back disagreed. */
export class NotConfirmedError extends MobiumError {
  static code = 'not_confirmed'
}

/** A wait ran out. */
export class TimedOutError extends MobiumError {
  static code = 'timeout'
}

/** The request itself is wrong. */
export class InvalidArgumentError extends MobiumError {
  static code = 'invalid_argument'
}

/** The device side failed (a device server, or adb, simctl, devicectl, lockdown); a server's W3C code is in details.w3c. */
export class DeviceServerError extends MobiumError {
  static code = 'device_server'
}

/** A bug in Mobium. */
export class InternalError extends MobiumError {
  static code = 'internal'
}

const ERRORS_BY_CODE = new Map(
  [NoDeviceError, DeviceNotReadyError, ToolchainMissingError, NoSuchElementError, AmbiguousLocatorError, ElementNotReachableError, NoSuchContextError, NoSuchAlertError, UnsupportedError, NotConfirmedError, TimedOutError, InvalidArgumentError, DeviceServerError, InternalError].map(c => [c.code, c]))

/**
 * The exception for a failed tool call: the text as always, and the code,
 * remedy and details when the daemon sent them.
 */
export function errorFrom(text, structured) {
  if (structured && typeof structured === 'object' && structured.code) {
    const Cls = ERRORS_BY_CODE.get(structured.code) ?? MobiumError
    return new Cls(text, {
      code: structured.code,
      remedy: structured.remedy ?? '',
      retryable: Boolean(structured.retryable),
      details: structured.details ?? {},
    })
  }
  return new MobiumError(text)
}

/**
 * Locate the mobium binary: the explicit path, then MOBIUM_BIN_PATH, then
 * PATH — and nothing else. MOBIUM_BIN_PATH wins so a test run can pin a
 * specific build.
 *
 * The current directory is never searched, not in ./bin and not through a
 * relative PATH entry: a library that runs whatever mobium sits where a test
 * was started runs a binary anyone could have planted there. PATH is walked
 * here, to an absolute path, rather than left to spawn, which on Windows
 * looks in the current directory first.
 */
export function findBinary(explicit) {
  for (const candidate of [explicit, process.env.MOBIUM_BIN_PATH]) {
    if (!candidate) continue
    try {
      accessSync(candidate, constants.X_OK)
      return candidate
    } catch {
      throw new MobiumError(`${candidate} is not an executable mobium binary`)
    }
  }
  const name = process.platform === 'win32' ? 'mobium.exe' : 'mobium'
  for (const dir of (process.env.PATH || '').split(delimiter)) {
    if (!isAbsolute(dir)) continue
    const found = join(dir, name)
    try {
      if (statSync(found).isFile()) {
        accessSync(found, constants.X_OK)
        return found
      }
    } catch {
      // Not here, or not executable; keep looking.
    }
  }
  throw new MobiumError('mobium not found — put it on PATH or set MOBIUM_BIN_PATH to the binary')
}

function textOf(result) {
  return (result.content || [])
    .filter((c) => c.type === 'text')
    .map((c) => c.text)
    .join('\n')
    .trim()
}

/** A tool's structured answer, or undefined when it only returned prose. */
function dataOf(result) {
  return result.structuredContent
}

/** Bounds carry a center so callers need not recompute it. */
function toBounds(b = {}) {
  const { x1 = 0, y1 = 0, x2 = 0, y2 = 0 } = b
  return {
    x1,
    y1,
    x2,
    y2,
    width: x2 - x1,
    height: y2 - y1,
    center: { x: Math.floor((x1 + x2) / 2), y: Math.floor((y1 + y2) / 2) },
  }
}

function toElement(e) {
  return {
    ref: e.ref || '',
    label: e.label || '',
    role: e.role || '',
    bounds: toBounds(e.bounds),
    locator: e.locator ? `${e.locator.kind}=${e.locator.value}` : '',
    context: e.context || '',
    // A checkbox, radio or switch's state; null for anything with no such state.
    checked: typeof e.checked === 'boolean' ? e.checked : null,
  }
}

function toElements(data) {
  if (!data || !data.elements) return []
  return data.elements.map(toElement)
}

/**
 * Connect and open the session on the device, as Appium's new session does:
 * the device-side server is started now and, given an app, it is launched and
 * in front when this resolves. End it with `quit()`.
 *
 * `platform` is "android" or "ios"; "ios" picks the wda driver, so none need
 * be named. `device`, `driver`, `binary` and `callTimeoutMs` are as for
 * `connect()`. Nothing requires it — every call opens a session on first use —
 * but it puts the slow first start (installing UiAutomator2, building
 * WebDriverAgent on an iPhone) where it was asked for.
 *
 *   const device = await start({ platform: 'android', app: 'com.android.settings' })
 *   try { await device.map() } finally { await device.quit() }
 */
export async function start({ platform, device, app, driver, binary, callTimeoutMs, session } = {}) {
  const d = await connect({ device, driver, binary, callTimeoutMs, session })
  const args = { action: 'start' }
  if (platform) args.platform = platform
  if (app) args.app = app
  let got
  try {
    got = (await d.conn.callTool('app_session', args)).structuredContent || {}
  } catch (e) {
    await d.close()
    throw e
  }
  d.session = {
    device: got.device || '', platform: got.platform || '', driver: got.driver || '',
    reused: Boolean(got.reused), app: got.app || '',
  }
  return d
}

/**
 * Connect to mobium. It does not touch the device: the session there opens on
 * the first call that needs it, or with `start()`.
 *
 * The transport is `mobium pipe`, which forwards to the shared daemon rather
 * than starting a session of its own: a device-side server holds one session
 * at a time, so a client with its own would invalidate the CLI's.
 *
 * `callTimeoutMs` is the longest any one call may take before the connection
 * is given up, the handshake included. Unset, the default, waits as long as it
 * takes: the first session on an iPhone builds WebDriverAgent, which takes
 * minutes. A call that runs out ends the connection -- a late answer would be
 * read as the next call's -- and every call after rejects, saying so; connect
 * again. Set it well above the longest `waitFor` timeout you use.
 */
export async function connect({ device, driver, binary, callTimeoutMs, session } = {}) {
  if (callTimeoutMs !== undefined && !(callTimeoutMs > 0)) {
    throw new InvalidArgumentError('callTimeoutMs must be a positive number of milliseconds')
  }
  const args = ['pipe']
  if (device) args.push('--device', device)
  if (driver) args.push('--driver', driver)

  const child = spawn(findBinary(binary), args, {
    // Progress notes about downloading a device-side server go to stderr;
    // inheriting keeps a slow first run explicable rather than silent.
    stdio: ['pipe', 'pipe', 'inherit'],
    // A daemon of this connection's own, as MOBIUM_SESSION names one. One
    // daemon serves one call at a time across every device, so parallel runs
    // on different devices should each have one.
    ...(session ? { env: { ...process.env, MOBIUM_SESSION: session } } : {}),
  })

  const conn = new Connection(child, callTimeoutMs)
  try {
    await conn.request('initialize', {
      protocolVersion: '2024-11-05',
      capabilities: {},
      clientInfo: { name: 'mobium-js', version: '0.1.0' },
    })
  } catch (e) {
    // A handshake that fails leaves a process nobody will ever close -- and a
    // child that keeps Node's event loop alive, so the script cannot exit.
    // Measured: a refused handshake left mobium running.
    conn.kill()
    throw e
  }
  conn.notify('notifications/initialized')
  return new Device(conn)
}

/**
 * JSON for the wire, refusing NaN and Infinity. JSON has neither, and
 * JSON.stringify writes them as null, so setLocation(NaN, 0) reached the
 * daemon as a null latitude and came back as "latitude must be a number".
 */
function wire(payload) {
  return JSON.stringify(payload, (key, value) => {
    if (typeof value === 'number' && !Number.isFinite(value)) {
      throw new InvalidArgumentError(`${key || 'a value'} is ${value}, which JSON cannot carry`)
    }
    return value
  })
}

class Connection {
  constructor(child, callTimeoutMs) {
    this.child = child
    this.timeoutMs = callTimeoutMs
    this.nextId = 0
    // Every request in flight, in the order it was written. The pipe answers
    // one request at a time, in order, so the first entry is the one the
    // next unnumbered answer belongs to.
    this.pending = new Map()
    // Why the connection can no longer be used, once something has made that
    // true: it was closed, mobium exited or could not start, or a call timed
    // out. A call abandoned half-way cannot be resynchronized, so the
    // connection ends and every call after says so.
    this.dead = null
    this.exited = new Promise((resolve) => { this.markExited = resolve })

    this.reader = createInterface({ input: child.stdout })
    this.reader.on('line', (line) => this.#onLine(line))

    // Without these listeners an 'error' event is thrown, and ends the
    // caller's process: measured for a binary that cannot start, and for a
    // call written after close().
    child.on('error', (e) => {
      this.#die(`could not run mobium: ${e.message}`)
      // 'exit' may not follow an 'error', and close() waits for this.
      this.markExited()
    })
    child.stdin.on('error', (e) => this.#die(`mobium closed the connection: ${e.message}`))
    child.on('exit', (code, signal) => {
      this.#die(this.dead ? this.dead : `mobium exited with status ${code ?? signal}`)
      this.markExited()
    })
  }

  #die(why) {
    if (!this.dead) this.dead = why
    for (const { reject, timer } of this.pending.values()) {
      clearTimeout(timer)
      reject(new MobiumError(this.dead))
    }
    this.pending.clear()
    if (this.child.exitCode === null && this.child.signalCode === null) this.child.kill()
  }

  #settle(id, fn) {
    const entry = this.pending.get(id)
    if (!entry) return
    this.pending.delete(id)
    clearTimeout(entry.timer)
    fn(entry)
  }

  #onLine(line) {
    // Nothing here may throw: this runs in an event listener, where an
    // exception is uncaught and ends the process. Measured: a line reading
    // `null` did, at `message.id`.
    if (!line.trim()) return
    let message
    try {
      message = JSON.parse(line)
    } catch {
      return // not a message we can read
    }
    if (message === null || typeof message !== 'object' || Array.isArray(message)) return
    const error = message.error && typeof message.error === 'object' ? message.error : null
    if (message.id === undefined || message.id === null) {
      if (!error) return // a notification
      // An error with no id is mobium saying it could not read a request at
      // all, which JSON-RPC answers without an id. Skipped as a notification
      // is, it left the call waiting forever.
      const [first] = this.pending.keys()
      if (first === undefined) return
      this.#settle(first, ({ reject }) => reject(new InvalidArgumentError(
        `mobium could not read the request: ${error.message}${error.data ? `: ${error.data}` : ''}`)))
      return
    }
    this.#settle(message.id, ({ resolve, reject }) => {
      if (error) {
        // A protocol error: the request itself was refused.
        reject(new InvalidArgumentError(error.data ? `${error.message}: ${error.data}` : String(error.message)))
      } else {
        resolve(message.result)
      }
    })
  }

  notify(method) {
    if (this.dead) return
    this.child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method }) + '\n')
  }

  request(method, params) {
    if (this.dead) return Promise.reject(new MobiumError(`this mobium connection is no longer usable: ${this.dead}`))
    const id = ++this.nextId
    const payload = { jsonrpc: '2.0', id, method }
    if (params !== undefined) payload.params = params
    let line
    try {
      line = wire(payload) + '\n'
    } catch (e) {
      return Promise.reject(e)
    }

    return new Promise((resolve, reject) => {
      let timer
      if (this.timeoutMs) {
        timer = setTimeout(() => {
          this.#die(`${method} got no answer within ${this.timeoutMs / 1000}s, so the connection was closed: ` +
            'a reply arriving later would be read as the answer to the next call. ' +
            'Connect again, with a longer callTimeoutMs if the device is slow')
        }, this.timeoutMs)
      }
      this.pending.set(id, { resolve, reject, timer })
      this.child.stdin.write(line)
    })
  }

  async callTool(name, args) {
    const result = await this.request('tools/call', { name, arguments: args || {} })
    if (result === null || typeof result !== 'object') {
      throw new MobiumError(`mobium answered ${name} with ${result}, not a result`)
    }
    // A failing tool answers with isError rather than a protocol error, so
    // the reason has to be lifted out deliberately.
    if (result.isError) throw errorFrom(textOf(result), result.structuredContent)
    return result
  }

  /** Ends mobium at once, for a connection that never became usable. */
  kill() {
    if (!this.dead) this.dead = 'it could not be set up'
    this.child.kill()
  }

  /**
   * Asks mobium to exit and resolves once it has, killing it after ten
   * seconds. Safe to call more than once; a call still waiting is rejected,
   * saying the connection was closed.
   */
  close() {
    if (!this.dead) {
      this.dead = 'it was closed'
      // mobium ends the sessions a client started when the client goes
      // away, and a crash closes stdin just as this does — so say first
      // that this is a deliberate close, or it would quit.
      this.child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method: 'mobium/detach' }) + '\n')
      this.child.stdin.end()
      const force = setTimeout(() => this.child.kill(), 10000)
      force.unref()
      this.exited.then(() => clearTimeout(force))
    }
    return this.exited
  }
}

/** A connected device or simulator. */
export class Device {
  constructor(conn) {
    this.conn = conn
    /** What `start()` opened — device, platform, driver — or null after `connect()`. */
    this.session = null
  }

  async #text(tool, args) {
    return textOf(await this.conn.callTool(tool, args))
  }

  /**
   * A tool's structured answer. Tools return data alongside their prose, so
   * nothing here parses the human-readable rendering.
   */
  async #data(tool, args) {
    return dataOf(await this.conn.callTool(tool, args))
  }

  /** Every attached device and simulator. */
  async devices() {
    const data = (await this.#data('app_devices')) || {}
    return (data.devices || []).map((d) => ({
      id: d.id || '',
      platform: d.platform || '',
      state: d.state || '',
      model: d.model || '',
      runtime: d.runtime || '',
      emulator: !!d.emulator,
    }))
  }

  /**
   * Actionable elements on the current screen. Refs are only valid for this
   * screen; call map() again after anything that changes it.
   */
  async map() {
    return toElements(await this.#data('app_map'))
  }

  /**
   * What changed since the last map of this device: the elements added,
   * removed and changed, each shaped as map() returns them. It maps the
   * screen as map() does, so it also replaces the refs. Refs in `added` and
   * in each change's `after` are the new map's; those in `removed` and in
   * `before` are stale. `first` is true when there was no earlier map to
   * compare with, and then the whole screen is in `added`. A change's `what` names the fields that differ:
   * "label", "checked" or "moved".
   */
  async mapDiff() {
    const diff = ((await this.#data('app_map', { diff: true })) || {}).diff || {}
    const list = (xs) => (xs || []).map(toElement)
    return {
      first: diff.first === true,
      since: diff.since || '',
      added: list(diff.added),
      removed: list(diff.removed),
      changed: (diff.changed || []).map((c) => ({
        before: toElement(c.before),
        after: toElement(c.after),
        what: c.what || [],
      })),
    }
  }

  /** All readable text, or the text of one element. */
  text(target) {
    return this.#text('app_text', target ? { target } : {})
  }

  /**
   * Declare how to answer a dialog, so an action that meets it carries on:
   * when a dialog whose text contains `when` is in the way, press the button
   * captioned `press`. It names a button, not accept or dismiss, because
   * which button those press differs by platform and by dialog. Captions
   * match ignoring case.
   */
  async addDialogRule(when, press) {
    await this.#text('app_dialogs', { when, press })
  }

  /** The declared rules, each with how often it has answered. */
  async dialogRules() {
    const data = (await this.#data('app_dialogs')) || {}
    return data.rules || []
  }

  /** Remove every rule for this device. */
  async clearDialogRules() {
    await this.#text('app_dialogs', { clear: true })
  }

  /**
   * The raw hierarchy — what Appium calls the page source — for when map
   * leaves out the thing you need to see; map is what to act on. `source` is
   * the platform's XML, or in a WebView the page's markup; `format` is "xml"
   * or "html"; `units` is "px" on Android and "pt" on iOS, where map, taps and
   * screenshots are in pixels, `scale` times as many. `redacted` counts the
   * password fields hidden.
   */
  async source() {
    return (await this.#data('app_source')) || {}
  }

  /** Elements matching a locator, without acting on them. */
  async find(locator) {
    return toElements(await this.#data('app_find', { locator }))
  }

  /**
   * Block until the screen agrees, instead of sleeping.
   *
   * condition is 'visible' (default), 'hidden', 'text' — which needs the
   * text to wait for — 'value', a field's whole content ('' for empty; a
   * password field is refused), 'enabled', 'disabled', 'checked',
   * 'unchecked', 'focused' or 'count' (with count). not waits for the
   * opposite; exact makes 'text' match the whole text. Throws MobiumError if
   * it never happens, saying what was on screen instead.
   *
   * On success the screen is remapped, so the element returned already has a
   * ref that can be tapped without calling map() first. Nothing is returned
   * when waiting for something to go away.
   */
  async waitFor(target, { condition = 'visible', text, timeoutMs = 10000, not, exact, count } = {}) {
    const args = { target, condition, timeout_ms: timeoutMs }
    if (text !== undefined) args.text = text
    if (not) args.not = true
    if (exact) args.exact = true
    if (count !== undefined) args.count = count
    const data = await this.#data('app_wait_for', args)
    return data && data.element ? toElement(data.element) : null
  }

  /**
   * Scroll until an element is on screen, and return it with a ref.
   *
   * map() only sees what is currently visible, and tap(), type() and
   * longPress() already scroll to a target that is not — so call this to look
   * without acting, or to scroll back up. `direction` is 'down', 'up', 'left'
   * or 'right' — nothing on screen says which way a container scrolls, so a
   * horizontal pager needs 'left' or 'right'. Swiping the wrong way is not a
   * no-op, so if a swipe navigates instead of scrolling, it stops after one.
   * Throws MobiumError if it is not found.
   */
  async scrollTo(target, { direction = 'down' } = {}) {
    const data = await this.#data('app_scroll_to', { target, direction })
    return data && data.element ? toElement(data.element) : null
  }

  /**
   * Tap a ref, a locator, or a point in device pixels.
   *
   * `{ fingers }` taps with several at once, side by side: 2 for a two-finger
   * tap, 3 for three (up to 5). On iOS three fingers can reach the system
   * instead of the app — three-finger gestures are undo, redo, copy and paste
   * there.
   */
  async tap(target, { fingers } = {}) {
    const extra = fingers === undefined ? {} : { fingers }
    if (typeof target === 'string') {
      return void (await this.#text('app_tap', { target, ...extra }))
    }
    if (target && typeof target === 'object') {
      const { x, y } = target
      return void (await this.#text('app_tap', { x, y, ...extra }))
    }
    throw new MobiumError('tap needs a target, or {x, y}')
  }

  /**
   * Tap twice, as one gesture rather than as two taps.
   *
   * The same tool as tap with one argument set, so the target is resolved the
   * same way and refused the same way when the screen has moved. The
   * uiautomator dump driver refuses it: the window is 40-300ms and nothing
   * there controls the interval between two adb calls.
   */
  async doubleTap(target) {
    if (typeof target === 'string') {
      return void (await this.#text('app_tap', { target, double: true }))
    }
    if (target && typeof target === 'object') {
      const { x, y } = target
      return void (await this.#text('app_tap', { x, y, double: true }))
    }
    throw new MobiumError('doubleTap needs a target, or {x, y}')
  }

  /**
   * Pick one element up, carry it onto another, and drop it.
   *
   * Not swipe with two targets: a swipe has no hold at either end, so pointed
   * at a reorderable row it scrolls the list instead of moving the row. Both
   * ends are resolved from one snapshot before anything is touched.
   *
   * `holdMs` is how long the finger rests at each end, defaulting to 700 —
   * above Android's 500ms long-press timeout, which is what a drag-to-reorder
   * list arms on. Raise it first when a drag picks nothing up.
   *
   * Reports that the gesture was delivered. Whether the drop was accepted is
   * the app's own state, so call map() again to see it.
   */
  async drag(from, to, { holdMs } = {}) {
    const args = { from, to }
    if (holdMs !== undefined) args.hold_ms = holdMs
    return (await this.#data('app_drag', args)) || {}
  }

  /**
   * Hold one element with a finger while a second finger taps another.
   *
   * The first finger lifts only after the second. Both are resolved before
   * anything is touched. `leadMs` is how long the first rests before the
   * second taps, 300 by default — under Android's 500ms long-press timeout.
   * Reports that the gesture was delivered; call map() again to see what it
   * did. Android 15 and earlier only: on iOS XCTest adds a zero-length touch
   * at the second finger's target when the gesture starts, and on Android 16
   * and later UiAutomator2's down times are rejected, so both refuse with
   * UnsupportedError.
   */
  async pressTap(hold, tap, { leadMs } = {}) {
    const args = { hold, tap }
    if (leadMs !== undefined) args.lead_ms = leadMs
    return (await this.#data('app_press_tap', args)) || {}
  }

  /**
   * Hold one element with a finger while a second finger drags from one
   * element to another. Not drag(), which is one finger carrying something.
   * Android 15 and earlier only, for pressTap's reasons.
   */
  async pressDrag(hold, from, to, { leadMs } = {}) {
    const args = { hold, from, to }
    if (leadMs !== undefined) args.lead_ms = leadMs
    return (await this.#data('app_press_drag', args)) || {}
  }

  /** Type into an element, after what it holds. An empty string clears it. */
  async type(target, text) {
    await this.#text('app_type', { target, text })
  }

  /** Clear an element and type into it, replacing what it held. */
  async fill(target, text) {
    await this.#text('app_fill', { target, text })
  }

  /** Swipe by direction ('up' | 'down' | 'left' | 'right') or exact points. */
  async swipe(direction, { durationMs = 300, from, to } = {}) {
    const args = { duration_ms: durationMs }
    if (from && to) {
      args.x1 = from.x
      args.y1 = from.y
      args.x2 = to.x
      args.y2 = to.y
    } else if (direction) {
      args.direction = direction
    } else {
      throw new MobiumError('swipe needs a direction or from/to points')
    }
    await this.#text('app_swipe', args)
  }

  /** Press and hold an element or a point. */
  async longPress(target, { durationMs = 800 } = {}) {
    const args = { duration_ms: durationMs }
    if (typeof target === 'string') args.target = target
    else if (target) Object.assign(args, { x: target.x, y: target.y })
    else throw new MobiumError('longPress needs a target, or {x, y}')
    await this.#text('app_long_press', args)
  }

  /** Capture the screen as PNG, optionally also writing it to a path. */
  async screenshot(path) {
    if (path) {
      await this.#text('app_screenshot', { path })
      return readFileSync(path)
    }
    const result = await this.conn.callTool('app_screenshot', {})
    for (const block of result.content || []) {
      if (block.type === 'image') return Buffer.from(block.data, 'base64')
    }
    throw new MobiumError('mobium returned no image')
  }

  /**
   * Bring an app to the foreground by package name or bundle id.
   *
   * Every ref from the previous screen is discarded — call map(), or just
   * act, since actions re-resolve their target anyway.
   */
  async launch(app) {
    await this.#text('app_launch', { app })
  }

  /** Stop a running app. */
  async terminate(app) {
    await this.#text('app_terminate', { app })
  }

  /**
   * Install a local .apk (Android) or .app bundle (iOS).
   * Resolves to the absolute path that was installed.
   */
  async install(path) {
    const data = (await this.#data('app_install', { path })) || {}
    return data.path || ''
  }

  /**
   * Installed apps, each with id, name, version and whether it is a system
   * app. By default only apps someone installed — a stock Android emulator
   * ships about 240 system packages. Name is empty on Android.
   */
  async apps({ system = false } = {}) {
    const data = (await this.#data('app_list_apps', { system })) || {}
    return data.apps || []
  }

  /**
   * Remove an app, verified by listing afterwards. `adb uninstall` reports
   * success when it has only removed the updates to a system app, so this
   * throws rather than reporting a lie.
   */
  async uninstall(app) {
    await this.#text('app_uninstall', { app })
  }

  /**
   * Delete an app's data and leave it installed — a fresh install's state,
   * without reinstalling. Resolves to what was read back empty (`emptied`),
   * what was kept (`kept`) and, on Android, the runtime permissions still
   * granted (`still_granted`): `pm clear` revokes what the user granted. An
   * iOS simulator keeps its privacy grants and keychain; a real iPhone
   * refuses.
   */
  async clearData(app) {
    return (await this.#data('app_clear_data', { app })) || {}
  }

  /**
   * Put a file from this machine where the device keeps downloads, so an
   * app's file picker finds it. On Android that is the shared Download
   * folder, one for every app, so `app` is ignored; the file is indexed in
   * MediaStore, which is what the picker reads, and read back there. On an
   * iOS simulator it is an app's own Documents folder — the app in front
   * unless `app` names one — which the Files app shows under On My iPhone.
   * On a real iPhone it is the same folder, confirmed by reading its bytes
   * back. `name` is the name to give it on the
   * device, a name rather than a path, and defaults to the file's own.
   *
   * Resolves to the transfer: `device`, `app` (iOS), `name`, `where`,
   * `bytes`, `checked` (how it was confirmed at both ends) and `path`.
   */
  async upload(path, { name, app } = {}) {
    const args = { path }
    if (name) args.name = name
    if (app) args.app = app
    return (await this.#data('app_upload', args)) || {}
  }

  /**
   * Bring back a file from where the device keeps downloads — Android's
   * shared Download folder, or an iOS simulator app's Documents, the app in
   * front unless `app` names one — to check what an app saved. A real iPhone
   * is not built yet. The copy's size is read back against the device's.
   *
   * With `path`, saves it there and resolves to the transfer (`device`,
   * `app`, `name`, `where`, `bytes`, `checked`, `path`). Without one, resolves
   * to the file's bytes. downloads() lists what there is to fetch.
   */
  async download(name, { path, app } = {}) {
    if (!name) throw new MobiumError('download needs the name of a file — downloads() lists them')
    const args = { name }
    if (path) args.path = path
    if (app) args.app = app
    const data = (await this.#data('app_download', args)) || {}
    if (path) return data
    if (typeof data.data !== 'string') throw new MobiumError(`mobium returned no contents for ${name}`)
    return Buffer.from(data.data, 'base64')
  }

  /**
   * What the downloads folder holds, each file with `name`, `bytes` and
   * `modified`: Android's shared Download folder, or an iOS simulator app's
   * Documents, the app in front unless `app` names one — on a real iPhone
   * too.
   */
  async downloads({ app } = {}) {
    const data = (await this.#data('app_download', app ? { app } : {})) || {}
    return data.files || []
  }

  /**
   * Runs several tools in order, on this device, in one call. Each step is
   * `{ name, arguments }` — a tool and the arguments it takes on its own:
   *
   *   await device.batch([
   *     { name: 'app_tap', arguments: { target: 'text=Sign in' } },
   *     { name: 'app_fill', arguments: { target: 'testid=user', text: 'mobium' } },
   *     { name: 'app_wait_for', arguments: { target: 'text=Welcome' } },
   *   ])
   *
   * Every step is checked before the first runs, and the batch stops at the
   * first failure, throwing that step's own error; its `details` hold `step`
   * and what `completed` before it. Resolves to each step's `name`, `text`
   * and `data`, in order.
   */
  async batch(steps) {
    const data = (await this.#data('app_batch', { steps })) || {}
    return data.steps || []
  }

  /**
   * The network: `airplane`, `online`, and the shaping — `latency_ms`,
   * `download_kbps`, `upload_kbps`, zero for none. Android only.
   */
  async network() {
    return (await this.#data('app_network', {})) || {}
  }

  /**
   * Turns airplane mode on (or off) and waits for the network to go (or come
   * back) — on an emulator or a real Android phone.
   */
  async setOffline(offline = true) {
    return (await this.#data('app_network', { offline })) || {}
  }

  /**
   * Adds latency to each round trip and limits download and upload, in
   * kbit/s, replacing any shaping set before; zero is none. Needs root, so
   * an emulator.
   */
  async shapeNetwork({ latencyMs = 0, downloadKbps = 0, uploadKbps = 0 } = {}) {
    return (await this.#data('app_network', {
      latency_ms: latencyMs, download_kbps: downloadKbps, upload_kbps: uploadKbps,
    })) || {}
  }

  /** Removes the shaping and turns airplane mode off. */
  async resetNetwork() {
    return (await this.#data('app_network', { reset: true })) || {}
  }

  /**
   * The battery: `level` in percent, `state` (charging, discharging,
   * not_charging, full or unknown) and on Android `plugged`. An iOS
   * simulator has none: `present` is false.
   */
  async battery() {
    return (await this.#data('app_battery')) || {}
  }

  /**
   * What time the device thinks it is: `time` (RFC 3339, in its own offset),
   * `zone`, and `clock` — "device", or "mac" on an iOS simulator.
   */
  async deviceTime() {
    return (await this.#data('app_time')) || {}
  }

  /**
   * Shakes an emulator or simulator — what shake-to-undo and shake-to-report
   * listen for. Whether the app reacts is up to its own detector, so check
   * the screen after. A real phone refuses.
   */
  async shake() {
    await this.#text('app_shake', {})
  }

  /**
   * Biometrics on an emulator or simulator. `action` is `status`, `enroll`
   * or `unenroll`, or `match` / `nomatch` to present a matching or a
   * stranger's face or finger to the prompt that is up. Resolves to `kind`
   * (face or fingerprint), `enrolled`, and for a match or non-match the
   * `outcome` read back: accepted, not recognized, failed (the prompt gave
   * up) or locked out. With no prompt up it rejects rather than sending to
   * nothing. A real phone refuses.
   */
  async biometric(action = 'status') {
    return (await this.#data('app_biometric', { action })) || {}
  }

  /**
   * Asks UIKit's own hit test, below accessibility, whether a tap on
   * `target` would reach it. Rejects when the touch would go elsewhere,
   * naming what would take it and whether accessibility can see it. iOS
   * simulators only, and opt-in: it attaches lldb to the app for about two
   * seconds.
   */
  async hitTest(target) {
    return (await this.#data('app_hit_test', { target })) || {}
  }

  /**
   * One app's state: `not_installed`, `not_running`, `background` or
   * `foreground`, for any app. An app under its own permission prompt is
   * still in front, and `covered_by` names the prompt's process. On iOS a
   * background app also has `suspended`.
   */
  async appState(app) {
    return (await this.#data('app_state', { app })) || {}
  }

  /**
   * Sends the app in front away for `seconds` and brings it back, resumed
   * rather than relaunched, confirmed in front again. At most 180 seconds.
   */
  async background(seconds, app) {
    const args = { seconds }
    if (app) args.app = app
    return (await this.#data('app_background', args)) || {}
  }

  /** Open a URL or deep link — the quickest way to a specific screen. */
  async openUrl(url) {
    await this.#text('app_open_url', { url })
  }

  /**
   * The package name or bundle id of the foreground app.
   *
   * Costs no extra device call: it reads the hierarchy a snapshot fetches
   * anyway. Use it to confirm a tap went where you expected.
   */
  async current() {
    const data = (await this.#data('app_current')) || {}
    return data.app || ''
  }

  /**
   * Grant permissions up front, so no dialog blocks the flow.
   *
   * Names are cross-platform ('camera', 'location', 'contacts', ...); 'all'
   * grants everything the app declares, and a platform name like
   * 'android.permission.NFC' also works. On Android the result is verified by
   * reading the state back, because `pm grant` reports success for
   * permissions the app never declared.
   */
  async grant(app, ...permissions) {
    await this.#text('app_grant', { app, permissions })
  }

  /** Deny permissions, to test how the app behaves without them. */
  async revoke(app, ...permissions) {
    await this.#text('app_revoke', { app, permissions })
  }

  /**
   * Put permissions back to their defaults, so the app prompts again.
   *
   * Naming an app resets only that app's, on both platforms; on Android that
   * stops the app if it had a permission granted. Omit app to reset every app
   * on the device.
   */
  async resetPermissions(app) {
    await this.#text('app_reset_permissions', app ? { app } : {})
  }

  /**
   * Read the light/dark setting, or change it and return the new one.
   *
   * mode is 'light', 'dark', or 'auto' (Android only; iOS throws). Dark mode
   * is a different rendering of every screen, so a flow is worth running in
   * both. Changing it discards the refs from the last map.
   */
  async appearance(mode) {
    const data = (await this.#data('app_appearance', mode ? { appearance: mode } : {})) || {}
    return data.appearance || ''
  }

  /**
   * Read the accessibility settings, or change one for the session.
   *
   * With no argument, resolves to an object of every setting the device has
   * — reduce_motion, bold_text, increase_contrast and the rest. With a
   * setting, to its value; with a setting and a value, changes it, confirmed
   * by reading it back, and resolves to the new value. A switch takes 'on' or
   * 'off'; text_size a category such as 'accessibility-large' (iOS);
   * text_scale a number such as '1.3' (Android). Every change is put back
   * when the session ends. A real iPhone rejects. Changing one discards the
   * refs from the last map.
   */
  async accessibility(setting, value) {
    const args = {}
    if (setting) args.setting = setting
    if (value !== undefined && value !== null) args.value = String(value)
    const data = (await this.#data('app_accessibility', args)) || {}
    return setting ? data.value || '' : data.settings || {}
  }

  /**
   * Which way the screen is turned, as `{ orientation, locked }`.
   *
   * A screen that merely happens to be portrait can rotate under you, so the
   * two are separate answers.
   */
  async orientation() {
    const data = (await this.#data('app_orientation')) || {}
    return { orientation: data.orientation || '', locked: !!data.locked }
  }

  /**
   * Turns the screen and pins it there; 'auto' hands it back to the sensor.
   *
   * 'portrait', 'landscape', 'portrait-reverse', 'landscape-reverse' or
   * 'auto' — not 'left'/'right', which the two platforms name differently. A
   * rotation re-lays out every screen, so the refs from the last map are
   * discarded. An activity that locks its own orientation cannot be turned
   * from outside and throws.
   */
  async setOrientation(mode) {
    const data = (await this.#data('app_orientation', { orientation: mode })) || {}
    return data.orientation || ''
  }

  /**
   * Read the screen, or make an Android device pretend to be another.
   *
   * A flow that works on the screen you happen to have is a flow tested once.
   * Pass a profile name to apply it, or 'reset' to put the device back — an
   * override outlives this session, so reset when done. Applying one discards
   * the refs from the last map, because nothing is where it was.
   *
   * On iOS the screen is fixed when the simulator is created, so this reads
   * only and names the simulator to boot instead.
   *
   * With { inspect: true } the answer also carries `findings`: elements past
   * the edge, touch targets below the platform minimum, text the platform
   * truncated, and tappable elements with nothing to announce. Treat a
   * touch-target finding as worth a look rather than a defect — Android can
   * enlarge a tap area without changing an element's bounds.
   */
  async screen(profile = '', { inspect = false } = {}) {
    const args = {}
    if (profile) args.profile = profile
    if (inspect) args.inspect = true
    return (await this.#data('app_screen', args)) || {}
  }

  /** Language tags pinned for an app; empty means it follows the device. */
  async appLocale(app) {
    const data = (await this.#data('app_locale', { app })) || {}
    return data.locales || []
  }

  /**
   * Runs one app in a chosen language; no tags follows the device again.
   * Android 13 and later.
   *
   * What this confirms is that the device stored the tag, not that the app has
   * a translation for it — Android reports no difference — so check the
   * screen. Relaunch the app to re-render it.
   */
  async setAppLocale(app, ...tags) {
    const data = (await this.#data('app_locale', { app, locale: tags.join(',') })) || {}
    return data.locales || []
  }

  /**
   * Turns two fingers about an element or the screen, positive clockwise.
   *
   * Reports that the gesture was delivered and nothing more, and this one is
   * harder still to confirm than a zoom: nothing in either hierarchy reports a
   * rotation, and there is no WebView property to ask either.
   */
  async rotate(degrees = 90, target) {
    const args = { degrees }
    if (target) args.target = target
    return (await this.#data('app_rotate', args)) || {}
  }

  /**
   * Pinches apart or together, about an element or the screen.
   *
   * Reports that the gesture was delivered and nothing more: neither platform
   * exposes a zoom level in the accessibility hierarchy, so confirming a zoom
   * means asking whatever was zoomed — a WebView can answer with
   * visualViewport.scale through eval().
   */
  async zoom(direction = 'in', target) {
    const args = { direction }
    if (target) args.target = target
    return (await this.#data('app_zoom', args)) || {}
  }

  /**
   * Puts a checkbox or switch into a state, rather than toggling it.
   *
   * Idempotent: asking for a state it is already in does nothing, which is
   * what makes it safe to call without reading first. Anything with no
   * checked state is refused rather than tapped, and a radio cannot be
   * unchecked — a group is cleared by choosing a different member.
   */
  async check(target, checked = true) {
    return (await this.#data('app_check', { target, checked })) || {}
  }

  /**
   * What a system dialog says, or '' when none is up.
   *
   * A permission prompt is another process's window, not the app's, and
   * reading it needs no knowledge of what the buttons say.
   *
   * @returns {Promise<string>}
   */
  async alert() {
    const data = (await this.#data('app_alert', {})) || {}
    return data.text || ''
  }

  /**
   * Accepts or dismisses a system dialog.
   *
   * These answer a dialog; they do not choose an outcome. On a permission
   * prompt they do not mean grant and deny, and on iOS they are the other way
   * round — accept leaves it denied and dismiss leaves it granted, because W3C
   * accept presses the affirmative button and Apple puts 'Don't Allow' last.
   * Tap the button by ref for a particular answer.
   *
   * `{ text }` is typed into a prompt's field first, so one call fills and
   * answers it. A plain alert has no field, and the platform refuses it.
   */
  async answerAlert(accept = true, { text } = {}) {
    const args = { action: accept ? 'accept' : 'dismiss' }
    if (text !== undefined) args.text = text
    return (await this.#data('app_alert', args)) || {}
  }

  /**
   * What the device clipboard holds.
   *
   * iOS only. On Android 10 and later only an app with focus may read the
   * clipboard and the UiAutomator2 server has no activity, so it would answer
   * 'empty' for a clipboard that is full — this rejects there instead.
   *
   * @returns {Promise<string>}
   */
  async clipboard() {
    const data = (await this.#data('app_clipboard', {})) || {}
    return data.text || ''
  }

  /**
   * Writes the device clipboard. On iOS the write is confirmed by reading it
   * back; on Android it is reported as sent.
   */
  async setClipboard(text) {
    return (await this.#data('app_clipboard', { text })) || {}
  }

  /**
   * Where the device believes it is.
   *
   * `mock` says this fix was injected; `mocking` says a test provider is
   * installed now. They differ after clearLocation(), because Android keeps
   * the last known position after the provider that supplied it is gone.
   *
   * Android only. `simctl location` has no `get`, so on iOS this rejects
   * rather than returning a position it never read.
   *
   * @returns {Promise<{latitude: number, longitude: number, mock: boolean,
   *   mocking: boolean, known: boolean}>}
   */
  async location() {
    return (await this.#data('app_location', {})) || {}
  }

  /**
   * Places the device at a coordinate.
   *
   * On Android this goes through a test provider, is read back, and works on
   * real hardware. On iOS it goes through simctl and cannot be confirmed —
   * resolving means the request was accepted, not that an app will read it.
   */
  async setLocation(latitude, longitude) {
    return (await this.#data('app_location', { latitude, longitude })) || {}
  }

  /**
   * Removes the injected position. Does not clear the device's last known
   * location, which Android caches.
   */
  async clearLocation() {
    return (await this.#data('app_location', { clear: true })) || {}
  }

  /**
   * Moves along two or more [latitude, longitude] waypoints over time.
   *
   * On iOS the simulator interpolates the route itself; on Android the daemon
   * steps a test provider once a second, the platform having no route
   * command. Either way this resolves as the route starts.
   */
  async followRoute(waypoints, speed) {
    const args = { waypoints }
    if (speed) args.speed = speed
    return (await this.#data('app_location', args)) || {}
  }

  /** Follows a GPX file, read on the machine running the daemon. */
  async followGpx(path, speed) {
    const args = { gpx: path }
    if (speed) args.speed = speed
    return (await this.#data('app_location', args)) || {}
  }

  /**
   * Presses a hardware button: 'back', 'home', 'recents', 'volume-up' or
   * 'volume-down'.
   *
   * On Android back is primary navigation. iOS has no back button by design
   * and throws with what to do instead, rather than sending an edge swipe —
   * a different event an app can tell apart. Any press can move the screen,
   * so the refs from the last map are discarded.
   */
  async press(button) {
    await this.#data('app_press', { button })
  }

  /** Whether the screen is locked. */
  async screenLocked() {
    const data = (await this.#data('app_lock')) || {}
    return !!data.locked
  }

  /**
   * Locks or unlocks the screen, confirmed against the device.
   *
   * A state rather than a power-button press: power is a toggle, so asking
   * twice leaves the device where it started. A device with a PIN, pattern or
   * password cannot be unlocked from outside and throws.
   */
  async setScreenLocked(locked) {
    const data = (await this.#data('app_lock', { state: locked ? 'lock' : 'unlock' })) || {}
    return !!data.locked
  }

  /**
   * Simulates an incoming call: 'ring', 'accept' or 'hang'.
   * Emulator only — a real phone cannot be made to ring from outside.
   */
  async incomingCall(action = 'ring', number) {
    await this.#data('app_call', number ? { action, number } : { action })
  }

  /** Delivers a simulated text message. Emulator only. */
  async sms(text, from) {
    await this.#data('app_sms', from ? { text, from } : { text })
  }

  /**
   * Checks the environment and returns the report.
   *
   * Needs no device: its whole job is to be runnable when nothing works yet,
   * so it is the first thing to call when something fails for a reason that
   * makes no sense.
   */
  async doctor() {
    return (await this.#data('app_doctor', {})) || {}
  }

  /**
   * Console output from the current WebView since the last call, including
   * uncaught errors and unhandled promise rejections.
   *
   * Each read drains what it returns, so it reports what happened since the
   * last call — which is what makes 'nothing was logged during this step'
   * assertable. Capture starts when the context is entered, so a page's
   * initial load is already over by then.
   */
  async logs(level) {
    // Named, because with no source the tool follows the context and would
    // read the device log on the native shell.
    const args = { source: 'webview' }
    if (level) args.level = level
    const data = (await this.#data('app_logs', args)) || {}
    return data.entries || []
  }

  /**
   * The device's own log since the last read: logcat on Android, the unified
   * log on an iOS simulator, what a real iPhone's session has captured. Resolves to { entries, skipped } — each entry has
   * time, level, tag, pid and message, and skipped counts lines newer than
   * the last read that the limit dropped, which will not come back. The first
   * read returns the most recent lines.
   */
  async deviceLogs({ app, level, lines } = {}) {
    const args = { source: 'device' }
    if (app) args.app = app
    if (level) args.level = level
    if (lines) args.lines = lines
    const data = (await this.#data('app_logs', args)) || {}
    return { entries: data.entries || [], skipped: data.skipped || 0 }
  }

  /**
   * Record the screen: action 'start', or 'stop' with a path to save the
   * video; neither asks whether one is running. Stop resolves to the file's
   * frames and duration, read from its own header; a still screen is one
   * frame on Android, which is not a failure.
   */
  async record({ action, path } = {}) {
    const args = {}
    if (action) args.action = action
    if (path) args.path = path
    return (await this.#data('app_record', args)) || {}
  }

  /**
   * Start recording the session as a trace: until traceStop(), every call on
   * this device is a step, with the screen after it and the map's elements
   * drawn over it. `name` titles the trace; `screenshots` and `maps` are true
   * unless set. Text typed into a field is not recorded, only its length. On
   * a real phone the screenshots are its owner's screen; screenshots: false
   * keeps none. One trace per device; ending the session discards it.
   */
  async traceStart({ name, screenshots, maps } = {}) {
    const args = { action: 'start' }
    if (name) args.name = name
    if (screenshots !== undefined) args.screenshots = screenshots
    if (maps !== undefined) args.maps = maps
    return (await this.#data('app_trace', args)) || {}
  }

  /**
   * Stop the trace and save it to `path`: a zip in the Playwright trace
   * format, which trace.playwright.dev and player.vibium.dev open. A relative
   * path is this process's.
   */
  async traceStop(path) {
    return (await this.#data('app_trace', { action: 'stop', path })) || {}
  }

  /** Whether a trace is running: `tracing`, and `calls` and `elapsed` when one is. */
  async trace() {
    return (await this.#data('app_trace', {})) || {}
  }

  /**
   * The soft keyboard: read it, type at the focused field, press a key, or
   * hide it. With no options, resolves to { shown, focused } — a password's
   * value is never shown. `text` is added to the end of the focused field and
   * confirmed; `key` is 'enter', 'delete' or 'space', after any text; `hide`
   * hides the keyboard, confirmed, alone. Throws NoSuchElementError when
   * nothing has focus.
   */
  async keyboard({ text, key, hide } = {}) {
    const args = {}
    if (text !== undefined) args.text = text
    if (key) args.key = key
    if (hide) args.hide = true
    return (await this.#data('app_keyboard', args)) || {}
  }

  /**
   * The crashes the device recorded, newest first: id, time, kind (crash,
   * native_crash or anr), app and summary. Not drained — asking twice shows a
   * crash twice.
   */
  async crashes({ app, limit } = {}) {
    const args = {}
    if (app) args.app = app
    if (limit) args.limit = limit
    const data = (await this.#data('app_crashes', args)) || {}
    return data.crashes || []
  }

  /** One crash report in full, by an id from crashes(); the text is in `text`. */
  async crash(id) {
    const data = (await this.#data('app_crashes', { id })) || {}
    return (data.crashes || [])[0] || {}
  }

  /**
   * Runs a JavaScript expression in the current WebView. Objects come back as
   * JSON.
   */
  async eval(expression) {
    const data = (await this.#data('app_eval', { expression })) || {}
    return data.value || ''
  }

  /**
   * The current WebView's cookies: the ones its page's URL is sent, HttpOnly
   * ones included. Needs a web context — context() first. Each has
   * Playwright's and Vibium's keys: name, value, domain, path, expires
   * (seconds since the epoch, absent for a session cookie), httpOnly, secure,
   * sameSite.
   */
  async cookies() {
    const data = (await this.#data('app_cookies', { action: 'get' })) || {}
    return data.cookies || []
  }

  /**
   * Set each cookie on the current page and read the store back, so one the
   * browser accepted and stored expired rejects with NotConfirmedError.
   */
  async setCookies(cookies) {
    await this.#data('app_cookies', { action: 'set', cookies })
  }

  /** Delete the current page's cookies, or only those called name. */
  async clearCookies(name) {
    await this.#data('app_cookies', name ? { action: 'clear', name } : { action: 'clear' })
  }

  /**
   * The current page's storage state, in the shape Playwright and Vibium
   * save: { cookies, origins: [{ origin, localStorage, sessionStorage }] }.
   */
  async storage() {
    const data = (await this.#data('app_storage', { action: 'get' })) || {}
    return data.state || { cookies: [], origins: [] }
  }

  /**
   * Restore a saved state: its cookies, and each origin's storage into the
   * page only if the page is on that origin.
   */
  async setStorage(state) {
    await this.#data('app_storage', { action: 'restore', state })
  }

  /** Empty the current page's cookies, localStorage and sessionStorage. */
  async clearStorage() {
    await this.#data('app_storage', { action: 'clear' })
  }

  /**
   * What is in the notification shade — how a test asserts an app posted what
   * it should. Each entry has package, title and text.
   */
  async notifications() {
    const data = (await this.#data('app_notifications')) || {}
    return data.notifications || []
  }

  /**
   * Puts a notification in the shade as an interruption, confirmed by reading
   * the shade back.
   */
  async postNotification(text, title) {
    const data = (await this.#data('app_notifications', title ? { text, title } : { text })) || {}
    return data.notifications || []
  }

  /**
   * Opens or closes the notification panel. A notification cannot be tapped
   * until the shade is open: until then it is not on screen and map cannot
   * see it. Discards the refs from the last map.
   */
  async shade(open) {
    const data = (await this.#data('app_notifications', { shade: open ? 'open' : 'close' })) || {}
    return data.notifications || []
  }

  /**
   * Reads the device timezone, or changes it and returns the new one.
   *
   * Takes an IANA name such as 'Asia/Tokyo'. Confirmed by reading it back —
   * an unknown zone is accepted by the device and ignored. Works on real
   * hardware, unlike call and sms.
   */
  async timezone(tz) {
    const data = (await this.#data('app_timezone', tz ? { timezone: tz } : {})) || {}
    return data.timezone || ''
  }

  /** Automatable contexts: the native shell plus any WebViews. */
  async contexts() {
    const data = (await this.#data('app_contexts')) || {}
    return (data.contexts || []).map((c) => c.id)
  }

  /** Switch context, or read the current one. */
  context(name) {
    return this.#text('app_context', name ? { context: name } : {})
  }

  /**
   * Closes the connection and resolves once mobium has exited. The device's
   * session lives in the daemon and stays open, for the next `connect()` or the
   * CLI; `quit()` ends it. Safe to call more than once, and while a call is
   * still waiting: that call is rejected.
   */
  close() {
    return this.conn.close()
  }

  /**
   * Ends the session on the device, as Appium's quit does, and closes the
   * connection. The teardown is the daemon's own: accessibility settings put
   * back, a recording or route stopped, WebViews detached, the device-side
   * server stopped, and the app start() launched, if any, stopped too.
   * Quitting a session that is not open succeeds, and a second quit does
   * nothing. A script that exits without quit() or close() has the sessions
   * it started ended for it: mobium sees the client go.
   */
  async quit() {
    // A second quit does nothing, rather than failing on a closed connection.
    if (this.quitted) return
    this.quitted = true
    const args = { action: 'end' }
    if (this.session) args.device = this.session.device
    try {
      await this.conn.callTool('app_session', args)
    } finally {
      await this.close()
    }
  }

  /** The sessions open on the daemon, each with device, platform and driver. */
  async sessions() {
    return (await this.#data('app_session', { action: 'status' }))?.sessions || []
  }
}
