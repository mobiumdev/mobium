// The declarations in index.d.ts against the code in index.js, with no
// TypeScript and no network: `node clients/javascript/test/types.test.mjs`.
// make ci runs it. Fails if a function or Device method exists on one side
// and not the other, which is how hand-written declarations drift.
import * as m from '../index.js'
import { readFileSync } from 'node:fs'

const dts = readFileSync(new URL('../index.d.ts', import.meta.url), 'utf8')
const failures = []

// Every export, and every class and function declared.
const exported = Object.keys(m).sort()
const declared = [...dts.matchAll(/^export (?:class|function|interface|type) (\w+)/gm)].map((x) => x[1])
for (const name of exported) {
  if (!declared.includes(name)) failures.push(`index.js exports ${name}, which index.d.ts does not declare`)
}
for (const name of declared) {
  const isType = new RegExp(`^export (?:interface|type) ${name}\\b`, 'm').test(dts)
  if (!isType && !exported.includes(name)) failures.push(`index.d.ts declares ${name}, which index.js does not export`)
}

// Every public Device method.
const body = dts.slice(dts.indexOf('export class Device {'), dts.indexOf('\n}\n', dts.indexOf('export class Device {')))
const declaredMethods = new Set([...body.matchAll(/^  (\w+)\(/gm)].map((x) => x[1]))
const methods = Object.getOwnPropertyNames(m.Device.prototype).filter((n) => n !== 'constructor')
for (const name of methods) {
  if (!declaredMethods.has(name)) failures.push(`Device.${name} is not declared in index.d.ts`)
}
for (const name of declaredMethods) {
  if (!methods.includes(name)) failures.push(`index.d.ts declares Device.${name}, which index.js does not have`)
}

if (failures.length) { console.log(failures.map((f) => 'FAIL: ' + f).join('\n')); process.exit(1) }
console.log(`javascript types: ${exported.length} exports and ${methods.length} Device methods declared`)
