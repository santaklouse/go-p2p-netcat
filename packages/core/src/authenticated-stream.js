import {
  SESSION_AUTH_FRAME_BYTES,
  createSessionAuthAck,
  createSessionAuthHello,
  verifySessionAuthAck,
  verifySessionAuthHello
} from './session-auth.js'

const DEFAULT_AUTH_TIMEOUT_MS = 10_000

export async function authenticateClientStream (stream, pairingToken, {
  timeoutMs = DEFAULT_AUTH_TIMEOUT_MS,
  nowSeconds
} = {}) {
  assertStream(stream)
  const hello = await createSessionAuthHello(pairingToken, { nowSeconds })
  try {
    await sendAndDrain(stream, hello.frame)
    const consumed = await consumePrefix(stream, SESSION_AUTH_FRAME_BYTES, timeoutMs)
    await verifySessionAuthAck(pairingToken, hello, consumed.prefix)
    return wrapConsumedStream(stream, consumed.iterator, consumed.remainder)
  } catch (error) {
    abortStream(stream, error)
    throw error
  }
}

export async function authenticateServerStream (stream, pairingToken, {
  timeoutMs = DEFAULT_AUTH_TIMEOUT_MS,
  nowSeconds
} = {}) {
  assertStream(stream)
  try {
    const consumed = await consumePrefix(stream, SESSION_AUTH_FRAME_BYTES, timeoutMs)
    const hello = await verifySessionAuthHello(pairingToken, consumed.prefix, { nowSeconds })
    await sendAndDrain(stream, await createSessionAuthAck(pairingToken, hello))
    return wrapConsumedStream(stream, consumed.iterator, consumed.remainder)
  } catch (error) {
    abortStream(stream, error)
    throw error
  }
}

async function consumePrefix (stream, length, timeoutMs) {
  const timeout = normalizeTimeout(timeoutMs)
  const deadline = Date.now() + timeout
  const iterator = stream[Symbol.asyncIterator]()
  const chunks = []
  let received = 0
  while (received < length) {
    const remaining = deadline - Date.now()
    if (remaining < 1) throw new Error(`Pairing token authentication timed out after ${timeout} ms`)
    const result = await withTimeout(iterator.next(), remaining, timeout)
    if (result.done) throw new Error('P2P stream ended during pairing token authentication')
    const chunk = binaryChunk(result.value)
    if (chunk.byteLength === 0) continue
    chunks.push(chunk)
    received += chunk.byteLength
  }
  const combined = new Uint8Array(received)
  let offset = 0
  for (const chunk of chunks) {
    combined.set(chunk, offset)
    offset += chunk.byteLength
  }
  return {
    iterator,
    prefix: combined.slice(0, length),
    remainder: combined.slice(length)
  }
}

function wrapConsumedStream (stream, iterator, remainder) {
  let iterated = false
  const wrapped = {
    get status () {
      return stream.status
    },
    get writeStatus () {
      return stream.writeStatus
    },
    get connectionStatus () {
      return stream.connectionStatus
    },
    get signalingStrategy () {
      return stream.signalingStrategy
    },
    send (chunk) {
      return stream.send(chunk)
    },
    onDrain () {
      return stream.onDrain()
    },
    close (...args) {
      return stream.close(...args)
    },
    abort (...args) {
      return stream.abort(...args)
    },
    [Symbol.asyncIterator] () {
      if (iterated) throw new Error('Authenticated P2P stream can only be consumed once')
      iterated = true
      let pending = remainder
      return {
        async next () {
          if (pending.byteLength > 0) {
            const value = pending
            pending = new Uint8Array()
            return { value, done: false }
          }
          return iterator.next()
        },
        async return () {
          if (typeof iterator.return === 'function') return iterator.return()
          return { value: undefined, done: true }
        },
        async throw (error) {
          if (typeof iterator.throw === 'function') return iterator.throw(error)
          throw error
        }
      }
    }
  }
  return wrapped
}

async function sendAndDrain (stream, bytes) {
  const writable = stream.send(bytes)
  if (writable === false || typeof stream.onDrain === 'function') await stream.onDrain()
}

function abortStream (stream, error) {
  try {
    stream.abort(error instanceof Error ? error : new Error(String(error)))
  } catch {}
}

function assertStream (stream) {
  if (
    stream == null ||
    typeof stream.send !== 'function' ||
    typeof stream.onDrain !== 'function' ||
    typeof stream[Symbol.asyncIterator] !== 'function'
  ) {
    throw new TypeError('A writable async-iterable P2P stream is required')
  }
}

function binaryChunk (value) {
  if (value instanceof Uint8Array) return value
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  if (value?.subarray != null) return value.subarray()
  throw new TypeError('P2P stream returned a non-binary chunk')
}

function normalizeTimeout (value) {
  const timeout = Number(value)
  if (!Number.isSafeInteger(timeout) || timeout < 1) throw new Error('Authentication timeout must be a positive integer')
  return timeout
}

function withTimeout (promise, remainingMs, timeoutMs) {
  let timer
  return Promise.race([
    promise,
    new Promise((resolve, reject) => {
      timer = setTimeout(
        () => reject(new Error(`Pairing token authentication timed out after ${timeoutMs} ms`)),
        remainingMs
      )
      timer.unref?.()
    })
  ]).finally(() => clearTimeout(timer))
}
