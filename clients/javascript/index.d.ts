// Type declarations for the Mobium JavaScript client.
//
// Hand-written, because the client is plain JavaScript with no build step and
// no dependencies. test/types.test.mjs fails if a function or Device method
// exists in index.js and is missing here, or the other way round.

// -- values ----------------------------------------------------------------

/** An on-screen rectangle, in device pixels on both platforms. */
export interface Bounds {
  x1: number
  y1: number
  x2: number
  y2: number
  width: number
  height: number
  /** The point a tap targets. */
  center: { x: number; y: number }
}

/** One actionable thing on screen, from `map()` or `find()`. */
export interface Element {
  /** The `@e1` handle, valid only for the screen it came from. */
  ref: string
  /** What a person would call it. */
  label: string
  /** button, input, checkbox and so on; empty if Mobium could not say. */
  role: string
  bounds: Bounds
  /** How the ref resolves on a later screen, as `kind=value`. */
  locator: string
  /** The WebView it came from; empty for native elements. */
  context: string
  /** A checkbox, radio or switch's state; null for anything with no such state. */
  checked: boolean | null
}

/** One attached device or simulator. */
export interface DeviceInfo {
  /** The serial (Android) or UDID (iOS) to pass as `device`. */
  id: string
  /** `"android"` or `"ios"`. */
  platform: string
  state: string
  model: string
  runtime: string
  /** True for an emulator or simulator, false for real hardware. */
  emulator: boolean
}

/** What `start()` opened. */
export interface Session {
  /** The serial (Android) or UDID (iOS) the session is on. */
  device: string
  /** `"android"`, `"ios"`, or a third-party driver's name. */
  platform: string
  /** The driver driving it, such as `"uiautomator2"` or `"wda"`. */
  driver: string
  /** True when a session was already open on the device, and was kept. */
  reused: boolean
  /** The app start launched, or empty. */
  app: string
}

/** One of a page's cookies, with Playwright's and Vibium's keys. */
export interface Cookie {
  name: string
  value: string
  /** Defaults to the page's host when set. */
  domain?: string
  /** Defaults to "/" when set. */
  path?: string
  /** Seconds since the epoch; absent for a session cookie. */
  expires?: number
  httpOnly?: boolean
  secure?: boolean
  sameSite?: 'Strict' | 'Lax' | 'None'
}

/** A page's cookies and web storage, in the shape Playwright and Vibium save. */
export interface StorageState {
  cookies: Cookie[]
  origins: {
    origin: string
    localStorage: { name: string; value: string }[]
    sessionStorage: { name: string; value: string }[]
  }[]
}

/** A point in device pixels. */
export interface Point {
  x: number
  y: number
}

/** A tool's structured answer, for the calls that return one as is. */
export type Data = Record<string, unknown>

// -- options ---------------------------------------------------------------

export interface ConnectOptions {
  /** A serial (Android) or UDID (iOS). Omit when one device is running. */
  device?: string
  /** `"uiautomator2"` (default on Android), `"uiautomator"`, `"wda"`, or a third-party driver's name. */
  driver?: string
  /** The mobium executable, ahead of MOBIUM_BIN_PATH and PATH. */
  binary?: string
  /**
   * The longest any one call may take, the handshake included. Unset waits as
   * long as it takes. A call that runs out ends the connection.
   */
  callTimeoutMs?: number
  /**
   * A daemon of this connection's own, by name, as MOBIUM_SESSION sets one.
   * One daemon serves one call at a time across every device, so parallel
   * runs on different devices should each name one. Keep it short; it is
   * part of a socket path.
   */
  session?: string
}

export interface StartOptions extends ConnectOptions {
  /** `"android"` or `"ios"`; `"ios"` picks the wda driver. */
  platform?: 'android' | 'ios'
  /** A package name (Android) or bundle id (iOS) to launch fresh once the session is up. */
  app?: string
}

export interface WaitOptions {
  condition?: 'visible' | 'hidden' | 'text'
  /** The text the element must contain, with `condition: 'text'`. */
  text?: string
  /** Ten seconds by default, two minutes at most. */
  timeoutMs?: number
}

// -- connecting ------------------------------------------------------------

/**
 * Connect and open the session on the device, as Appium's new session does.
 * End it with `quit()`.
 */
export function start(options?: StartOptions): Promise<Device>

/** Connect to mobium without touching the device. `close()` leaves the session open. */
export function connect(options?: ConnectOptions): Promise<Device>

/** Locate the mobium binary: the explicit path, then MOBIUM_BIN_PATH, then PATH. */
export function findBinary(explicit?: string): string

/** The exception for a failed tool call, from its text and structured content. */
export function errorFrom(text: string, structured?: unknown): MobiumError

// -- the device ------------------------------------------------------------

export class Device {
  /** What `start()` opened, or null after `connect()`. */
  session: Session | null

  // reading
  devices(): Promise<DeviceInfo[]>
  map(): Promise<Element[]>
  find(locator: string): Promise<Element[]>
  text(target?: string): Promise<string>
  source(): Promise<Data>
  current(): Promise<string>
  /** The PNG's bytes (a Node Buffer, which is a Uint8Array). With a path, also written there. */
  screenshot(path?: string): Promise<Uint8Array>
  addDialogRule(when: string, press: string): Promise<void>
  dialogRules(): Promise<Data[]>
  clearDialogRules(): Promise<void>

  // waiting and scrolling
  /** Resolves with the element, or null when waiting for it to go away. */
  waitFor(target: string, options?: WaitOptions): Promise<Element | null>
  scrollTo(target: string, options?: { direction?: 'down' | 'up' | 'left' | 'right' }): Promise<Element | null>

  // acting
  tap(target: string, options?: { fingers?: number }): Promise<void>
  tap(point: Point, options?: { fingers?: number }): Promise<void>
  doubleTap(target: string | Point): Promise<void>
  drag(from: string, to: string, options?: { holdMs?: number }): Promise<Data>
  pressTap(hold: string, tap: string, options?: { leadMs?: number }): Promise<Data>
  pressDrag(hold: string, from: string, to: string, options?: { leadMs?: number }): Promise<Data>
  type(target: string, text: string): Promise<void>
  fill(target: string, text: string): Promise<void>
  swipe(direction?: 'up' | 'down' | 'left' | 'right', options?: { durationMs?: number; from?: Point; to?: Point }): Promise<void>
  longPress(target: string | Point, options?: { durationMs?: number }): Promise<void>
  check(target: string, checked?: boolean): Promise<Data>
  rotate(degrees?: number, target?: string): Promise<Data>
  zoom(direction?: 'in' | 'out', target?: string): Promise<Data>

  // apps
  launch(app: string): Promise<void>
  terminate(app: string): Promise<void>
  install(path: string): Promise<string>
  uninstall(app: string): Promise<void>
  clearData(app: string): Promise<Data>
  openUrl(url: string): Promise<void>
  apps(options?: { system?: boolean }): Promise<Data[]>

  // device state
  grant(app: string, ...permissions: string[]): Promise<void>
  revoke(app: string, ...permissions: string[]): Promise<void>
  resetPermissions(app?: string): Promise<void>
  appearance(mode?: 'light' | 'dark' | 'auto'): Promise<string>
  /** Every setting by name, or one setting's value; with a value, sets it for the session. */
  accessibility(): Promise<Record<string, string>>
  accessibility(setting: string, value?: string | number | boolean): Promise<string>
  orientation(): Promise<{ orientation: string; locked: boolean }>
  setOrientation(mode: 'portrait' | 'landscape' | 'portrait-reverse' | 'landscape-reverse' | 'auto'): Promise<string>
  screen(profile?: string, options?: { inspect?: boolean }): Promise<Data>
  appLocale(app: string): Promise<string[]>
  setAppLocale(app: string, ...tags: string[]): Promise<string[]>
  alert(): Promise<string>
  answerAlert(accept?: boolean): Promise<Data>
  clipboard(): Promise<string>
  setClipboard(text: string): Promise<Data>
  location(): Promise<Data>
  setLocation(latitude: number, longitude: number): Promise<Data>
  clearLocation(): Promise<Data>
  followRoute(waypoints: [number, number][], speed?: number): Promise<Data>
  followGpx(path: string, speed?: number): Promise<Data>
  press(button: 'back' | 'home' | 'recents' | 'volume-up' | 'volume-down'): Promise<void>
  screenLocked(): Promise<boolean>
  setScreenLocked(locked: boolean): Promise<boolean>
  incomingCall(action?: 'ring' | 'accept' | 'hang', number?: string): Promise<void>
  sms(text: string, from?: string): Promise<void>
  timezone(tz?: string): Promise<string>
  notifications(): Promise<Data[]>
  postNotification(text: string, title?: string): Promise<Data[]>
  shade(open: boolean): Promise<Data[]>

  // logs, crashes, recording, keyboard
  logs(level?: string): Promise<Data[]>
  deviceLogs(options?: { app?: string; level?: string; lines?: number }): Promise<{ entries: Data[]; skipped: number }>
  crashes(options?: { app?: string; limit?: number }): Promise<Data[]>
  crash(id: string): Promise<Data>
  record(options?: { action?: 'start' | 'stop'; path?: string }): Promise<Data>
  keyboard(options?: { text?: string; key?: 'enter' | 'delete' | 'space'; hide?: boolean }): Promise<Data>

  // contexts
  contexts(): Promise<string[]>
  context(name?: string): Promise<string>
  eval(expression: string): Promise<string>
  /** The current WebView's cookies, HttpOnly ones included. Needs a web context. */
  cookies(): Promise<Cookie[]>
  /** Set each cookie and read the store back. */
  setCookies(cookies: Cookie[]): Promise<void>
  /** Delete the page's cookies, or only those called name. */
  clearCookies(name?: string): Promise<void>
  /** The page's storage state. */
  storage(): Promise<StorageState>
  /** Restore a saved state; each origin's storage goes only into a page on it. */
  setStorage(state: StorageState): Promise<void>
  /** Empty the page's cookies, localStorage and sessionStorage. */
  clearStorage(): Promise<void>

  // diagnostics and lifecycle
  doctor(): Promise<Data>
  /** The sessions open on the daemon. */
  sessions(): Promise<{ device: string; platform: string; driver: string }[]>
  /**
   * End the session on the device, as Appium's quit does, and close the
   * connection. The app start() launched, if any, is stopped too.
   */
  quit(): Promise<void>
  /** Close the connection; the session stays open. Resolves once mobium has exited. */
  close(): Promise<void>
}

// -- errors: the same codes in every Mobium client --------------------------

export class MobiumError extends Error {
  static code: string
  /** `no_such_element`, `timeout`, ... or `error` when unclassified. */
  code: string
  /** What to do about it, or empty. */
  remedy: string
  /** Whether the same call, made again unchanged, can reasonably succeed. */
  retryable: boolean
  /** Machine-readable facts: the locator, a device server's W3C code. */
  details: Record<string, unknown>
  constructor(message: string, options?: { code?: string; remedy?: string; retryable?: boolean; details?: Record<string, unknown> })
}
export class NoDeviceError extends MobiumError {}
export class DeviceNotReadyError extends MobiumError {}
export class ToolchainMissingError extends MobiumError {}
export class NoSuchElementError extends MobiumError {}
export class AmbiguousLocatorError extends MobiumError {}
export class ElementNotReachableError extends MobiumError {}
export class NoSuchContextError extends MobiumError {}
export class NoSuchAlertError extends MobiumError {}
export class UnsupportedError extends MobiumError {}
export class NotConfirmedError extends MobiumError {}
export class TimedOutError extends MobiumError {}
export class InvalidArgumentError extends MobiumError {}
export class DeviceServerError extends MobiumError {}
export class InternalError extends MobiumError {}
