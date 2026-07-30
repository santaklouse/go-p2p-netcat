import {
  assertPairingTokenUsable,
  derivePairingKey,
  normalizePairingToken
} from './pairing.js'

const encoder = new TextEncoder()
const SESSION_AUTH_VERSION = 1
const SESSION_AUTH_CLIENT_HELLO = 1
const SESSION_AUTH_SERVER_ACK = 2
const SESSION_AUTH_NONCE_BYTES = 16
const SESSION_AUTH_MAC_BYTES = 32
const SESSION_AUTH_FRAME_BYTES = 4 + 1 + 1 + 8 + SESSION_AUTH_NONCE_BYTES + SESSION_AUTH_MAC_BYTES
const DEFAULT_SESSION_AUTH_CLOCK_SKEW_SECONDS = 120
const MAGIC = Uint8Array.from([0x50, 0x4e, 0x43, 0x41])

export {
  DEFAULT_SESSION_AUTH_CLOCK_SKEW_SECONDS,
  SESSION_AUTH_CLIENT_HELLO,
  SESSION_AUTH_FRAME_BYTES,
  SESSION_AUTH_SERVER_ACK,
  SESSION_AUTH_VERSION
}

export async function createSessionAuthHello (value, {
  nowSeconds = Math.floor(Date.now() / 1000),
  nonce
} = {}) {
  const token = assertPairingTokenUsable(value, { nowSeconds })
  const timestamp = safeInteger(nowSeconds, 'Session authentication timestamp')
  const clientNonce = normalizeNonce(nonce)
  const mac = await sessionMac(token, sessionMacInput('client', token, timestamp, clientNonce))
  return Object.freeze({
    frame: encodeFrame(SESSION_AUTH_CLIENT_HELLO, timestamp, clientNonce, mac),
    timestamp,
    clientNonce: clientNonce.slice()
  })
}

export async function verifySessionAuthHello (value, frame, {
  nowSeconds = Math.floor(Date.now() / 1000),
  maxClockSkewSeconds = DEFAULT_SESSION_AUTH_CLOCK_SKEW_SECONDS
} = {}) {
  const token = assertPairingTokenUsable(value, { nowSeconds })
  const decoded = decodeFrame(frame, SESSION_AUTH_CLIENT_HELLO)
  assertClock(decoded.timestamp, nowSeconds, maxClockSkewSeconds)
  const expected = await sessionMac(
    token,
    sessionMacInput('client', token, decoded.timestamp, decoded.nonce)
  )
  if (!constantTimeEqual(expected, decoded.mac)) throw new Error('Pairing token authentication failed')
  return Object.freeze({
    timestamp: decoded.timestamp,
    clientNonce: decoded.nonce.slice()
  })
}

export async function createSessionAuthAck (value, hello, {
  nonce
} = {}) {
  const token = normalizePairingToken(value)
  const timestamp = safeInteger(hello?.timestamp, 'Session authentication timestamp')
  const clientNonce = normalizeNonce(hello?.clientNonce)
  const serverNonce = normalizeNonce(nonce)
  const mac = await sessionMac(
    token,
    sessionMacInput('server', token, timestamp, clientNonce, serverNonce)
  )
  return encodeFrame(SESSION_AUTH_SERVER_ACK, timestamp, serverNonce, mac)
}

export async function verifySessionAuthAck (value, hello, frame) {
  const token = normalizePairingToken(value)
  const timestamp = safeInteger(hello?.timestamp, 'Session authentication timestamp')
  const clientNonce = normalizeNonce(hello?.clientNonce)
  const decoded = decodeFrame(frame, SESSION_AUTH_SERVER_ACK)
  if (decoded.timestamp !== timestamp) throw new Error('Session authentication timestamp changed')
  const expected = await sessionMac(
    token,
    sessionMacInput('server', token, timestamp, clientNonce, decoded.nonce)
  )
  if (!constantTimeEqual(expected, decoded.mac)) throw new Error('Pairing token server acknowledgement failed')
  return Object.freeze({ serverNonce: decoded.nonce.slice() })
}

function encodeFrame (type, timestamp, nonce, mac) {
  const frame = new Uint8Array(SESSION_AUTH_FRAME_BYTES)
  frame.set(MAGIC)
  frame[4] = SESSION_AUTH_VERSION
  frame[5] = type
  new DataView(frame.buffer).setBigUint64(6, BigInt(timestamp))
  frame.set(nonce, 14)
  frame.set(mac, 14 + SESSION_AUTH_NONCE_BYTES)
  return frame
}

function decodeFrame (value, expectedType) {
  const frame = binary(value, 'Session authentication frame')
  if (frame.byteLength !== SESSION_AUTH_FRAME_BYTES) {
    throw new Error(`Session authentication frame must contain exactly ${SESSION_AUTH_FRAME_BYTES} bytes`)
  }
  if (!constantTimeEqual(frame.subarray(0, MAGIC.byteLength), MAGIC)) {
    throw new Error('Session authentication frame has invalid magic bytes')
  }
  if (frame[4] !== SESSION_AUTH_VERSION) {
    throw new Error(`Unsupported session authentication version: ${frame[4]}`)
  }
  if (frame[5] !== expectedType) throw new Error(`Unexpected session authentication frame type: ${frame[5]}`)
  const timestampValue = new DataView(frame.buffer, frame.byteOffset, frame.byteLength).getBigUint64(6)
  if (timestampValue > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('Session authentication timestamp exceeds JavaScript safe integer range')
  return {
    timestamp: Number(timestampValue),
    nonce: frame.slice(14, 14 + SESSION_AUTH_NONCE_BYTES),
    mac: frame.slice(14 + SESSION_AUTH_NONCE_BYTES)
  }
}

async function sessionMac (token, message) {
  const keyBytes = await derivePairingKey(token, 'admission')
  const key = await subtle().importKey('raw', keyBytes, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  return new Uint8Array(await subtle().sign('HMAC', key, message))
}

function sessionMacInput (role, token, timestamp, clientNonce, serverNonce) {
  const prefix = encoder.encode(
    `p2p-netcat/session-auth/v1\u0000${role}\u0000${token.peerId}\u0000${token.service}\u0000${timestamp}\u0000`
  )
  const result = new Uint8Array(prefix.byteLength + clientNonce.byteLength + (serverNonce?.byteLength ?? 0))
  result.set(prefix)
  result.set(clientNonce, prefix.byteLength)
  if (serverNonce != null) result.set(serverNonce, prefix.byteLength + clientNonce.byteLength)
  return result
}

function normalizeNonce (value) {
  if (value == null) {
    const nonce = new Uint8Array(SESSION_AUTH_NONCE_BYTES)
    cryptoProvider().getRandomValues(nonce)
    return nonce
  }
  const nonce = binary(value, 'Session authentication nonce')
  if (nonce.byteLength !== SESSION_AUTH_NONCE_BYTES) {
    throw new Error(`Session authentication nonce must contain exactly ${SESSION_AUTH_NONCE_BYTES} bytes`)
  }
  return nonce.slice()
}

function assertClock (timestamp, nowSeconds, maxClockSkewSeconds) {
  const now = safeInteger(nowSeconds, 'Current Unix time')
  const skew = safeInteger(maxClockSkewSeconds, 'Session authentication clock skew')
  if (skew < 0) throw new Error('Session authentication clock skew must not be negative')
  if (Math.abs(timestamp - now) > skew) throw new Error('Session authentication timestamp is outside the accepted clock window')
}

function constantTimeEqual (a, b) {
  const left = binary(a, 'Comparison input')
  const right = binary(b, 'Comparison input')
  let difference = left.byteLength ^ right.byteLength
  const length = Math.max(left.byteLength, right.byteLength)
  for (let index = 0; index < length; index += 1) {
    difference |= (left[index % Math.max(1, left.byteLength)] ?? 0) ^
      (right[index % Math.max(1, right.byteLength)] ?? 0)
  }
  return difference === 0
}

function cryptoProvider () {
  if (globalThis.crypto?.getRandomValues == null) throw new Error('Web Crypto API is unavailable')
  return globalThis.crypto
}

function subtle () {
  if (cryptoProvider().subtle == null) throw new Error('Web Crypto SubtleCrypto API is unavailable')
  return cryptoProvider().subtle
}

function binary (value, label) {
  if (value instanceof Uint8Array) return value
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  throw new TypeError(`${label} must be binary data`)
}

function safeInteger (value, label) {
  const number = Number(value)
  if (!Number.isSafeInteger(number) || number < 0) throw new Error(`${label} must be a non-negative safe integer`)
  return number
}
