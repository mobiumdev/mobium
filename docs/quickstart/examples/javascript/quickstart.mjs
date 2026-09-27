// Mobium quick start: start a session, drive Settings, quit.
//
//   MOBIUM_PLATFORM=android node quickstart.mjs    # or ios
import { start } from 'mobium'

// Settings is on every emulator, simulator and phone, with nothing to install.
const PLATFORMS = {
  android: { app: 'com.android.settings', row: 'Network & internet', next: 'text=Internet' },
  ios: { app: 'com.apple.Preferences', row: 'General', next: 'label=About,role=button' },
}
const platform = process.env.MOBIUM_PLATFORM || 'android'
const p = PLATFORMS[platform]

// 1. Start the session: the driver is started on the device and Settings is
//    launched.
const device = await start({ platform, app: p.app, device: process.env.MOBIUM_DEVICE })
try {
  const s = device.session
  console.log(`session on ${s.device} (${s.platform}, ${s.driver})`)

  // 2. Map the screen: every element you can act on, each with a @ref.
  const elements = await device.map()
  for (const e of elements.slice(0, 5)) console.log(' ', `${e.ref} ${e.label}${e.role ? ` (${e.role})` : ''}`)

  // 3. Tap a row by its ref, then wait for the screen it opens. A row's label
  //    can carry its summary too ("Network & internet Mobile, Wi-Fi, ..."),
  //    so match its start.
  const row = elements.find((e) => e.label.startsWith(p.row))
  await device.tap(row.ref)
  await device.waitFor(p.next)
  console.log(`opened ${p.row}`)

  // 4. Take a screenshot.
  await device.screenshot(`quickstart-${platform}.png`)
  console.log(`saved quickstart-${platform}.png`)
} finally {
  // 5. Quit: the device's session is closed, even after an error.
  await device.quit()
}
console.log('session ended')
