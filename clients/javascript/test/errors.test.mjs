// The error mapping, with no test framework and no device:
// `node clients/javascript/test/errors.test.mjs`. make ci runs it.
import * as m from '../index.js'

const codes = ['no_device', 'device_not_ready', 'toolchain_missing', 'no_such_element',
  'ambiguous_locator', 'element_not_reachable', 'no_such_context', 'no_such_alert',
  'unsupported', 'not_confirmed', 'timeout', 'invalid_argument', 'device_server', 'internal']
const failures = []
const check = (cond, what) => { if (!cond) failures.push(what) }

for (const code of codes) {
  const e = m.errorFrom('x', { code })
  check(e.code === code && e.constructor !== m.MobiumError, `no class for ${code}`)
  check(e instanceof m.MobiumError && e instanceof Error, `${code} is not a MobiumError`)
}

const e = m.errorFrom('no element matches text=Go', {
  code: 'no_such_element', remedy: 'run app_map again', retryable: false,
  details: { locator: 'text=Go' } })
check(e instanceof m.NoSuchElementError, `got ${e.name}`)
check(e.name === 'NoSuchElementError' && e.message === 'no element matches text=Go', 'name/message')
check(e.remedy === 'run app_map again' && e.details.locator === 'text=Go', 'remedy/details lost')
check(!(e instanceof m.UnsupportedError), 'matched another code')

const t = m.errorFrom('timed out', { code: 'timeout', retryable: true })
check(t instanceof m.TimedOutError && t.retryable, 'timeout not TimedOutError/retryable')

const u = m.errorFrom('new kind', { code: 'something_new' })
check(u.constructor === m.MobiumError && u.code === 'something_new', 'unknown code mishandled')
const o = m.errorFrom('plain text', undefined)
check(o.constructor === m.MobiumError && o.code === 'error', 'text-only failure mishandled')

if (failures.length) { console.log(failures.map(f => 'FAIL: ' + f).join('\n')); process.exit(1) }
console.log(`javascript errors: ${codes.length} codes mapped, all checks passed`)
