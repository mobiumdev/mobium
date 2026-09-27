// A stand-in for `mobium pipe`, so the connection's failure paths are
// exercised against a real subprocess with no device.
//
// MOBIUM_FAKE picks the behavior:
//   ok      answer every call, first writing lines that must be skipped: a
//           notification, a line that is not JSON, JSON that is not a
//           message (null, a number, an array), and a reply to another id.
//           Tool "slow" answers after 300ms.
//   hang    answer the handshake, never a tool call
//   exit    answer the handshake, exit with status 3 on the first tool call
//   mute    never answer anything, the handshake included
//   refuse  answer the handshake with a protocol error, then wait for stdin
//   noid    answer app_map as mobium answers a line it cannot parse -- an
//           error with no id -- and every other call normally
// MOBIUM_FAKE_PIDFILE, when set, receives this process's id.
import { createInterface } from 'node:readline'
import { writeFileSync } from 'node:fs'

const mode = process.env.MOBIUM_FAKE || 'ok'
if (process.env.MOBIUM_FAKE_PIDFILE) writeFileSync(process.env.MOBIUM_FAKE_PIDFILE, String(process.pid))
const send = (o) => process.stdout.write(JSON.stringify(o) + '\n')

// One at a time, in order, as mobium pipe answers.
let chain = Promise.resolve()
createInterface({ input: process.stdin }).on('line', (line) => {
  chain = chain.then(() => handle(JSON.parse(line)))
})

async function handle(msg) {
  if (msg.id === undefined) return
  const { id, method } = msg
  if (mode === 'mute') return
  if (method === 'initialize') {
    if (mode === 'refuse') send({ jsonrpc: '2.0', id, error: { code: -32602, message: 'unsupported protocol version' } })
    else send({ jsonrpc: '2.0', id, result: {} })
    return
  }
  if (mode === 'hang') return
  if (mode === 'exit') process.exit(3)
  const { name, arguments: args } = msg.params
  if (name === 'slow') await new Promise((r) => setTimeout(r, 300))
  if (mode === 'noid' && name === 'app_map') {
    process.stdout.write('{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error","data":"invalid character"}}\n')
    return
  }
  process.stdout.write('{"jsonrpc":"2.0","method":"notifications/message","params":{}}\n')
  process.stdout.write('progress: this line is not JSON\nnull\n5\n[1,2]\n')
  send({ jsonrpc: '2.0', id: id + 1000, result: { wrong: true } })
  send({ jsonrpc: '2.0', id, result: { content: [{ type: 'text', text: 'ok ' + name }], structuredContent: { tool: name, echo: args } } })
}
