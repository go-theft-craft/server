// Loopback-only client harness for the Go server.
//
// It logs in as an offline player at Java Edition 1.8.8 (protocol 47) using
// the pinned Node minecraft-protocol package, waits until it has joined and
// received chunk data, and reports what it saw as newline-delimited JSON on
// stdout. It exits non-zero on a disconnect, a timeout, or an error.
//
// It never dials anything but the loopback interface and never contacts a
// session server: offline authentication makes no outbound request.

import process from 'node:process'
import mc from 'minecraft-protocol'

const LOOPBACK = '127.0.0.1'
const DEFAULT_TIMEOUT_MS = 30_000

function emit (event) {
  process.stdout.write(`${JSON.stringify(event)}\n`)
}

function fail (message) {
  emit({ event: 'error', message })
  process.exit(1)
}

function parseArgs (argv) {
  const args = { port: 0, username: 'InteropBot', timeout: DEFAULT_TIMEOUT_MS }

  for (let index = 0; index < argv.length; index += 2) {
    const flag = argv[index]
    const value = argv[index + 1]

    if (!flag.startsWith('--') || value === undefined) {
      fail(`bad argument ${flag}`)
    }

    switch (flag.slice(2)) {
      case 'port':
        args.port = Number.parseInt(value, 10)
        break
      case 'username':
        args.username = value
        break
      case 'timeout':
        args.timeout = Number.parseInt(value, 10)
        break
      default:
        fail(`unknown flag ${flag}`)
    }
  }

  if (!Number.isInteger(args.port) || args.port <= 0) {
    fail('a positive --port is required')
  }

  return args
}

const args = parseArgs(process.argv.slice(2))

const client = mc.createClient({
  host: LOOPBACK,
  port: args.port,
  username: args.username,
  version: '1.8.8',
  auth: 'offline',
  hideErrors: false
})

const seen = { login: false, chunk: false, compressed: false }

const timer = setTimeout(() => {
  fail(`timed out after ${args.timeout}ms; saw ${JSON.stringify(seen)}`)
}, args.timeout)

function finishIfDone () {
  if (!seen.login || !seen.chunk) {
    return
  }

  clearTimeout(timer)
  emit({ event: 'joined', compressed: seen.compressed })
  client.end()
  process.exit(0)
}

client.on('login', (packet) => {
  seen.login = true
  emit({
    event: 'login',
    entityId: packet.entityId,
    gameMode: packet.gameMode,
    levelType: packet.levelType
  })
  finishIfDone()
})

// The threshold arrives as its own packet; recording it proves the server
// negotiated compression rather than merely tolerating it.
client.on('compress', (packet) => {
  seen.compressed = true
  emit({ event: 'compress', threshold: packet.threshold })
})

client.on('map_chunk', (packet) => {
  if (seen.chunk) {
    return
  }

  seen.chunk = true
  emit({
    event: 'map_chunk',
    x: packet.x,
    z: packet.z,
    bytes: packet.chunkData ? packet.chunkData.length : 0
  })
  finishIfDone()
})

client.on('kick_disconnect', (packet) => {
  fail(`kicked: ${packet.reason}`)
})

client.on('disconnect', (packet) => {
  fail(`disconnected during login: ${packet.reason}`)
})

client.on('error', (error) => {
  fail(`client error: ${error.message}`)
})

client.on('end', (reason) => {
  if (!seen.login || !seen.chunk) {
    fail(`connection ended early (${reason}); saw ${JSON.stringify(seen)}`)
  }
})
