// The JavaScript client end to end, against a booted Android device.
//
// Not part of `make ci` — it needs a device. docs/checks/clients.sh runs it,
// with the four other clients' copies of the same flow:
//
//   MOBIUM_E2E_DEVICE=emulator-5554 node clients/javascript/test/e2e.mjs
//
// Reading, acting, waiting, scrolling, lifecycle, permissions, the device log
// and crash reports, and one failure that must arrive as its own error class.
import * as m from '../index.js'

const SETTINGS = 'com.android.settings'

function check(ok, what) {
  if (!ok) {
    console.error(`FAIL: ${what}`)
    process.exit(1)
  }
  console.log(`    ok   ${what}`)
}

const serial = process.env.MOBIUM_E2E_DEVICE
if (!serial) {
  console.log('MOBIUM_E2E_DEVICE is not set; skipping')
  process.exit(0)
}

const d = await m.connect({ device: serial, binary: process.env.MOBIUM_BIN_PATH })
try {
  await d.terminate(SETTINGS)
  await d.launch(SETTINGS)
  check((await d.current()) === SETTINGS, 'launch brings Settings forward')

  const found = await d.waitFor('text=Network & internet')
  check(found && found.ref.startsWith('@e'), 'waitFor returns a ref')
  await d.tap(found.ref)
  // A tap returns when delivered, not when the next screen is up.
  check((await d.waitFor('text=Internet')) !== null, 'tapping the ref opens its screen')

  await d.press('back')
  const row = await d.scrollTo('text=About')
  check(row !== null, 'scrollTo reaches a row below the fold')

  await d.grant('com.android.chrome', 'camera')
  await d.revoke('com.android.chrome', 'camera')
  check(true, 'grant and revoke, each read back by the tool')

  const logs = await d.deviceLogs({ lines: 5 })
  check(Array.isArray(logs.entries) && logs.entries.length > 0, 'deviceLogs reads logcat')
  check(Array.isArray(await d.crashes({ limit: 3 })), 'crashes answers with an array')

  try {
    await d.tap('text=Definitely Not Here')
    check(false, 'a missing element throws')
  } catch (e) {
    check(e instanceof m.NoSuchElementError, 'a missing element throws NoSuchElementError')
  }

  await d.terminate(SETTINGS)
  check((await d.current()) !== SETTINGS, 'terminate takes Settings away')
  console.log('javascript: passed')
} finally {
  d.close()
}
