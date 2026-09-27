/**
 * Mobium — native mobile app automation on virtual devices.
 *
 *   import { connect } from 'mobium'
 *
 *   const device = await connect()
 *   for (const el of await device.map()) console.log(el.ref, el.label)
 *   await device.tap('@e1')
 *   await device.close()
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

/** This backend or platform cannot do it, and says why. Retrying cannot help. */
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
  }
}

function toElements(data) {
  if (!data || !data.elements) return []
  return data.elements.map(toElement)
}

/**
 * Start a mobium session.
 *
 * The transport is `mobium pipe`, which forwards to the shared daemon rather
 * than starting a session of its own: a device-side server holds one session
 * at a time, so a client with its own would invalidate the CLI's.
 */
export async function connect({ device, backend, binary } = {}) {
  const args = ['pipe']
  if (device) args.push('--device', device)
  if (backend) args.push('--backend', backend)

  const child = spawn(findBinary(binary), args, {
    // Progress notes about downloading a device-side server go to stderr;
    // inheriting keeps a slow first run explicable rather than silent.
    stdio: ['pipe', 'pipe', 'inherit'],
  })

  const conn = new Connection(child)
  await conn.request('initialize', {
    protocolVersion: '2024-11-05',
    capabilities: {},
    clientInfo: { name: 'mobium-js', version: '0.1.0' },
  })
  conn.notify('notifications/initialized')
  return new Device(conn)
}

class Connection {
  constructor(child) {
    this.child = child
    this.nextId = 0
    this.pending = new Map()
    this.closed = false

    this.reader = createInterface({ input: child.stdout })
    this.reader.on('line', (line) => this.#onLine(line))

    child.on('exit', (code) => {
      this.closed = true
      // Reject anything still waiting, or a caller hangs forever on a
      // process that has already gone.
      for (const { reject } of this.pending.values()) {
        reject(new MobiumError(`mobium exited with status ${code}`))
      }
      this.pending.clear()
    })
  }

  #onLine(line) {
    if (!line.trim()) return
    let message
    try {
      message = JSON.parse(line)
    } catch {
      return
    }
    const entry = this.pending.get(message.id)
    if (!entry) return // a notification, or a stale reply
    this.pending.delete(message.id)
    if (message.error) {
      const { message: m, data } = message.error
      // A protocol error: the request itself was refused.
      entry.reject(new InvalidArgumentError(data ? `${m}: ${data}` : m))
    } else {
      entry.resolve(message.result)
    }
  }

  notify(method) {
    if (this.closed) return
    this.child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method }) + '\n')
  }

  request(method, params) {
    if (this.closed) return Promise.reject(new MobiumError('mobium is not running'))
    const id = ++this.nextId
    const payload = { jsonrpc: '2.0', id, method }
    if (params !== undefined) payload.params = params

    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
      this.child.stdin.write(JSON.stringify(payload) + '\n')
    })
  }

  async callTool(name, args) {
    const result = await this.request('tools/call', { name, arguments: args || {} })
    // A failing tool answers with isError rather than a protocol error, so
    // the reason has to be lifted out deliberately.
    if (result.isError) throw errorFrom(textOf(result), result.structuredContent)
    return result
  }

  close() {
    if (!this.closed) this.child.stdin.end()
  }
}

/** A connected device or simulator. */
export class Device {
  constructor(conn) {
    this.conn = conn
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

  /** All readable text, or the text of one element. */
  text(target) {
    return this.#text('app_text', target ? { target } : {})
  }

  /**
   * The raw hierarchy — what Appium calls the page source — for when map
   * leaves out the thing you need to see; map is what to act on. `source` is
   * the platform's XML, or in a WebView the page's markup; `format` is "xml"
   * or "html"; `units` is "px" on Android and "pt" on iOS, where map, taps and
   * screenshots are in pixels, `scale` times as many. `redacted` counts the
   * password fields hidden.
   */
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
   * condition is 'visible' (default), 'hidden', or 'text' — which needs the
   * text to wait for. Throws MobiumError if it never happens, saying what was
   * on screen instead.
   *
   * On success the screen is remapped, so the element returned already has a
   * ref that can be tapped without calling map() first. Nothing is returned
   * when waiting for something to go away.
   */
  async waitFor(target, { condition = 'visible', text, timeoutMs = 10000 } = {}) {
    const args = { target, condition, timeout_ms: timeoutMs }
    if (text !== undefined) args.text = text
    const data = await this.#data('app_wait_for', args)
    return data && data.element ? toElement(data.element) : null
  }

  /**
   * Scroll until an element is on screen, and return it with a ref.
   *
   * map() only sees what is currently visible, and tap(), type() and
   * longPress() already scroll to a target that is not — so call this to look
   * without acting, or to scroll back up. Vertical only; use swipe() for a
   * horizontal pager. Throws MobiumError if it is not found.
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
  async tap(target, point, { fingers } = {}) {
    const extra = fingers === undefined ? {} : { fingers }
    if (typeof target === 'string') {
      if (point && point.fingers !== undefined) extra.fingers = point.fingers
      return void (await this.#text('app_tap', { target, ...extra }))
    }
    if (point || (target && typeof target === 'object')) {
      const { x, y } = point || target
      return void (await this.#text('app_tap', { x, y, ...extra }))
    }
    throw new MobiumError('tap needs a target, or {x, y}')
  }

  /**
   * Tap twice, as one gesture rather than as two taps.
   *
   * The same tool as tap with one argument set, so the target is resolved the
   * same way and refused the same way when the screen has moved. The
   * uiautomator dump backend refuses it: the window is 40-300ms and nothing
   * there controls the interval between two adb calls.
   */
  async doubleTap(target, point) {
    if (typeof target === 'string') {
      return void (await this.#text('app_tap', { target, double: true }))
    }
    if (point || (target && typeof target === 'object')) {
      const { x, y } = point || target
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

  /** Type into an element. An empty string clears it. */
  async type(target, text, { clear = false } = {}) {
    await this.#text('app_type', { target, text, clear })
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
   * iOS can reset one app. Android cannot — `pm reset-permissions` is
   * device-wide — so omit app there; naming one throws rather than resetting
   * every app on the device.
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
   * Pinches apart or together, about an element or the screen.  /**
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
   */
  async answerAlert(accept = true) {
    return (await this.#data('app_alert', { action: accept ? 'accept' : 'dismiss' })) || {}
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

  close() {
    this.conn.close()
  }
}
