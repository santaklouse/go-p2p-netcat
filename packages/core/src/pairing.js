import { decode, encode, rfc8949EncodeOptions } from 'cborg'
import { peerIdFromString } from '@libp2p/peer-id'
import { multiaddr } from '@multiformats/multiaddr'
import { CID } from 'multiformats/cid'
import * as raw from 'multiformats/codecs/raw'
import { sha256 } from 'multiformats/hashes/sha2'

const encoder = new TextEncoder()
const PAIRING_TOKEN_PREFIX = 'pnc1_'
const PAIRING_TOKEN_VERSION = 1
const PAIRING_SECRET_BYTES = 32
const DEFAULT_RENDEZVOUS_INTERVAL_SECONDS = 300
const MAX_RELAY_HINTS = 16
const MAX_TOKEN_BYTES = 16 * 1024
const TOKEN_KEY_VERSION = 0
const TOKEN_KEY_PEER_ID = 1
const TOKEN_KEY_SERVICE = 2
const TOKEN_KEY_SECRET = 3
const TOKEN_KEY_RELAY_HINTS = 4
const TOKEN_KEY_EXPIRES_AT = 5
const RENDEZVOUS_PURPOSES = new Set(['dht', 'pubsub', 'signaling'])
const PAIRING_KEY_PURPOSES = new Set(['rendezvous', 'signaling', 'admission', 'route-record'])
const HKDF_SALT = encoder.encode('p2p-netcat/pairing/v1')

export {
  DEFAULT_RENDEZVOUS_INTERVAL_SECONDS,
  PAIRING_SECRET_BYTES,
  PAIRING_TOKEN_PREFIX,
  PAIRING_TOKEN_VERSION
}

export function createPairingToken ({
  peerId,
  service = 31337,
  secret,
  relayHints = [],
  expiresAt
}) {
  const normalized = normalizePairingToken({
    version: PAIRING_TOKEN_VERSION,
    peerId,
    service,
    secret: secret == null ? randomBytes(PAIRING_SECRET_BYTES) : secret,
    relayHints,
    expiresAt
  })
  return encodePairingToken(normalized)
}

export function encodePairingToken (value) {
  const token = normalizePairingToken(value)
  const fields = new Map([
    [TOKEN_KEY_VERSION, token.version],
    [TOKEN_KEY_PEER_ID, token.peerId],
    [TOKEN_KEY_SERVICE, token.service],
    [TOKEN_KEY_SECRET, token.secret],
    [TOKEN_KEY_RELAY_HINTS, token.relayHints]
  ])
  if (token.expiresAt != null) fields.set(TOKEN_KEY_EXPIRES_AT, token.expiresAt)
  return `${PAIRING_TOKEN_PREFIX}${base64UrlEncode(encode(fields, rfc8949EncodeOptions))}`
}

export function decodePairingToken (value) {
  const text = String(value ?? '').trim()
  if (!text.startsWith(PAIRING_TOKEN_PREFIX)) {
    throw new Error(`Pairing token must start with ${PAIRING_TOKEN_PREFIX}`)
  }
  const bytes = base64UrlDecode(text.slice(PAIRING_TOKEN_PREFIX.length))
  if (bytes.byteLength === 0 || bytes.byteLength > MAX_TOKEN_BYTES) {
    throw new Error(`Pairing token payload must contain 1..${MAX_TOKEN_BYTES} bytes`)
  }
  const fields = decodeMap(bytes)
  rejectUnknownKeys(fields, new Set([
    TOKEN_KEY_VERSION,
    TOKEN_KEY_PEER_ID,
    TOKEN_KEY_SERVICE,
    TOKEN_KEY_SECRET,
    TOKEN_KEY_RELAY_HINTS,
    TOKEN_KEY_EXPIRES_AT
  ]), 'pairing token')
  return normalizePairingToken({
    version: fields.get(TOKEN_KEY_VERSION),
    peerId: fields.get(TOKEN_KEY_PEER_ID),
    service: fields.get(TOKEN_KEY_SERVICE),
    secret: fields.get(TOKEN_KEY_SECRET),
    relayHints: fields.get(TOKEN_KEY_RELAY_HINTS) ?? [],
    expiresAt: fields.get(TOKEN_KEY_EXPIRES_AT)
  })
}

export function normalizePairingToken (value) {
  if (typeof value === 'string') return decodePairingToken(value)
  if (value == null || typeof value !== 'object') throw new TypeError('Pairing token must be a string or object')
  const version = safeInteger(value.version, 'Pairing token version')
  if (version !== PAIRING_TOKEN_VERSION) throw new Error(`Unsupported pairing token version: ${version}`)
  const peerId = normalizePeerId(value.peerId)
  const service = normalizeService(value.service)
  const secret = bytes(value.secret, 'Pairing token secret')
  if (secret.byteLength !== PAIRING_SECRET_BYTES) {
    throw new Error(`Pairing token secret must contain exactly ${PAIRING_SECRET_BYTES} bytes`)
  }
  const relayHints = normalizeRelayHints(value.relayHints)
  const expiresAt = value.expiresAt == null
    ? undefined
    : safeInteger(value.expiresAt, 'Pairing token expiration')
  if (expiresAt != null && expiresAt < 1) throw new Error('Pairing token expiration must be a positive Unix timestamp')
  return Object.freeze({
    version,
    peerId,
    service,
    secret: secret.slice(),
    relayHints: Object.freeze(relayHints),
    expiresAt
  })
}

export function assertPairingTokenUsable (value, {
  peerId,
  service,
  nowSeconds = Math.floor(Date.now() / 1000)
} = {}) {
  const token = normalizePairingToken(value)
  if (peerId != null && token.peerId !== normalizePeerId(peerId)) {
    throw new Error(`Pairing token belongs to PeerId ${token.peerId}, not ${normalizePeerId(peerId)}`)
  }
  if (service != null && token.service !== normalizeService(service)) {
    throw new Error(`Pairing token belongs to logical port ${token.service}, not ${normalizeService(service)}`)
  }
  if (token.expiresAt != null && safeInteger(nowSeconds, 'Current Unix time') > token.expiresAt) {
    throw new Error(`Pairing token expired at Unix time ${token.expiresAt}`)
  }
  return token
}

export async function derivePairingKey (value, purpose) {
  const token = normalizePairingToken(value)
  const normalizedPurpose = String(purpose ?? '').trim()
  if (!PAIRING_KEY_PURPOSES.has(normalizedPurpose)) {
    throw new Error(`Unsupported pairing key purpose: ${normalizedPurpose}`)
  }
  const key = await subtle().importKey('raw', token.secret, 'HKDF', false, ['deriveBits'])
  const bits = await subtle().deriveBits({
    name: 'HKDF',
    hash: 'SHA-256',
    salt: HKDF_SALT,
    info: encoder.encode(`p2p-netcat/${normalizedPurpose}/v1`)
  }, key, 256)
  return new Uint8Array(bits)
}

export async function deriveRendezvousId (value, {
  purpose = 'dht',
  epoch
} = {}) {
  const token = normalizePairingToken(value)
  const normalizedPurpose = String(purpose ?? '').trim()
  if (!RENDEZVOUS_PURPOSES.has(normalizedPurpose)) {
    throw new Error(`Unsupported rendezvous purpose: ${normalizedPurpose}`)
  }
  const normalizedEpoch = safeInteger(epoch, 'Rendezvous epoch')
  if (normalizedEpoch < 0) throw new Error('Rendezvous epoch must not be negative')
  const keyBytes = await derivePairingKey(token, 'rendezvous')
  const key = await subtle().importKey('raw', keyBytes, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const message = encoder.encode(
    `p2p-netcat/rendezvous/v1\u0000${normalizedPurpose}\u0000${token.peerId}\u0000${token.service}\u0000${normalizedEpoch}`
  )
  return base64UrlEncode(new Uint8Array(await subtle().sign('HMAC', key, message)))
}

export async function pairingRendezvousWindows (value, {
  purpose = 'dht',
  nowMs = Date.now(),
  intervalSeconds = DEFAULT_RENDEZVOUS_INTERVAL_SECONDS,
  offsets = [-1, 0, 1]
} = {}) {
  const token = normalizePairingToken(value)
  const interval = safeInteger(intervalSeconds, 'Rendezvous interval')
  if (interval < 30) throw new Error('Rendezvous interval must be at least 30 seconds')
  const timestamp = Number(nowMs)
  if (!Number.isFinite(timestamp) || timestamp < 0) throw new Error('Rendezvous time must be a positive millisecond timestamp')
  if (!Array.isArray(offsets) || offsets.length === 0 || offsets.length > 8) {
    throw new Error('Rendezvous offsets must contain 1..8 entries')
  }
  const baseEpoch = Math.floor(timestamp / 1000 / interval)
  const uniqueOffsets = [...new Set(offsets.map(offset => safeInteger(offset, 'Rendezvous offset')))]
  const windows = await Promise.all(uniqueOffsets.map(async offset => {
    const epoch = baseEpoch + offset
    if (epoch < 0) throw new Error('Rendezvous epoch must not be negative')
    return Object.freeze({
      epoch,
      id: await deriveRendezvousId(token, { purpose, epoch })
    })
  }))
  return Object.freeze(windows)
}

export async function rendezvousProviderCid (rendezvousId) {
  const id = String(rendezvousId ?? '').trim()
  if (!/^[A-Za-z0-9_-]{43}$/.test(id)) throw new Error('Invalid rendezvous identifier')
  const digest = await sha256.digest(encoder.encode(`p2p-netcat:rendezvous:v1:${id}`))
  return CID.createV1(raw.code, digest)
}

export async function pairingProviderCids (value, options = {}) {
  const windows = await pairingRendezvousWindows(value, { ...options, purpose: 'dht' })
  return Promise.all(windows.map(window => rendezvousProviderCid(window.id)))
}

export async function sealPairingPayload (value, purpose, plaintext, {
  additionalData = new Uint8Array(),
  nonce
} = {}) {
  const keyBytes = await derivePairingKey(value, purpose)
  const iv = nonce == null ? randomBytes(12) : bytes(nonce, 'AES-GCM nonce')
  if (iv.byteLength !== 12) throw new Error('AES-GCM nonce must contain exactly 12 bytes')
  const key = await subtle().importKey('raw', keyBytes, 'AES-GCM', false, ['encrypt'])
  const ciphertext = await subtle().encrypt({
    name: 'AES-GCM',
    iv,
    additionalData: bytes(additionalData, 'AES-GCM additional data'),
    tagLength: 128
  }, key, bytes(plaintext, 'AES-GCM plaintext'))
  return encode(new Map([
    [0, 1],
    [1, iv],
    [2, new Uint8Array(ciphertext)]
  ]), rfc8949EncodeOptions)
}

export async function openPairingPayload (value, purpose, envelope, {
  additionalData = new Uint8Array()
} = {}) {
  const fields = decodeMap(bytes(envelope, 'AES-GCM envelope'))
  rejectUnknownKeys(fields, new Set([0, 1, 2]), 'AES-GCM envelope')
  if (fields.get(0) !== 1) throw new Error(`Unsupported AES-GCM envelope version: ${fields.get(0)}`)
  const iv = bytes(fields.get(1), 'AES-GCM nonce')
  const ciphertext = bytes(fields.get(2), 'AES-GCM ciphertext')
  if (iv.byteLength !== 12) throw new Error('AES-GCM nonce must contain exactly 12 bytes')
  if (ciphertext.byteLength < 16) throw new Error('AES-GCM ciphertext is shorter than its authentication tag')
  const keyBytes = await derivePairingKey(value, purpose)
  const key = await subtle().importKey('raw', keyBytes, 'AES-GCM', false, ['decrypt'])
  try {
    return new Uint8Array(await subtle().decrypt({
      name: 'AES-GCM',
      iv,
      additionalData: bytes(additionalData, 'AES-GCM additional data'),
      tagLength: 128
    }, key, ciphertext))
  } catch (error) {
    throw new Error('Pairing payload authentication failed', { cause: error })
  }
}

export function base64UrlEncode (value) {
  const input = bytes(value, 'Base64url input')
  let binary = ''
  for (const byte of input) binary += String.fromCharCode(byte)
  return base64Encode(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/u, '')
}

export function base64UrlDecode (value) {
  const text = String(value ?? '').trim()
  if (!/^[A-Za-z0-9_-]*$/u.test(text)) throw new Error('Invalid base64url data')
  const padded = `${text.replaceAll('-', '+').replaceAll('_', '/')}${'='.repeat((4 - text.length % 4) % 4)}`
  let binary
  try {
    binary = base64Decode(padded)
  } catch (error) {
    throw new Error('Invalid base64url data', { cause: error })
  }
  return Uint8Array.from(binary, character => character.charCodeAt(0))
}

function normalizePeerId (value) {
  try {
    return peerIdFromString(String(value ?? '').trim()).toString()
  } catch (error) {
    throw new Error(`Invalid pairing token PeerId: ${value}`, { cause: error })
  }
}

function normalizeService (value) {
  const service = Number(value)
  if (!Number.isSafeInteger(service) || service < 1 || service > 65535) {
    throw new Error(`Logical port must be an integer from 1 to 65535, received: ${value}`)
  }
  return service
}

function normalizeRelayHints (value) {
  if (value == null) return []
  if (!Array.isArray(value)) throw new TypeError('Pairing token relayHints must be an array')
  if (value.length > MAX_RELAY_HINTS) throw new Error(`Pairing token supports at most ${MAX_RELAY_HINTS} relay hints`)
  return [...new Set(value.map(item => {
    const hint = String(item ?? '').trim()
    if (hint.length === 0 || hint.length > 2048) throw new Error('Pairing token relay hint must contain 1..2048 characters')
    try {
      const address = multiaddr(hint)
      if (!address.getComponents().some(component => component.name === 'p2p')) {
        throw new Error('relay PeerId is missing')
      }
      return address.toString()
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error)
      throw new Error(`Invalid pairing token relay multiaddr: ${hint}: ${reason}`, { cause: error })
    }
  }))].sort()
}

function randomBytes (length) {
  const output = new Uint8Array(length)
  cryptoProvider().getRandomValues(output)
  return output
}

function cryptoProvider () {
  if (globalThis.crypto?.getRandomValues == null) throw new Error('Web Crypto API is unavailable')
  return globalThis.crypto
}

function subtle () {
  if (cryptoProvider().subtle == null) throw new Error('Web Crypto SubtleCrypto API is unavailable')
  return cryptoProvider().subtle
}

function bytes (value, label) {
  if (value instanceof Uint8Array) return value
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  throw new TypeError(`${label} must be binary data`)
}

function safeInteger (value, label) {
  const number = Number(value)
  if (!Number.isSafeInteger(number)) throw new Error(`${label} must be a safe integer`)
  return number
}

function decodeMap (value) {
  const input = bytes(value, 'CBOR payload')
  let decoded
  try {
    decoded = decode(input, {
      useMaps: true,
      strict: true,
      allowIndefinite: false,
      allowNaN: false,
      allowInfinity: false
    })
  } catch (error) {
    throw new Error('Invalid deterministic CBOR payload', { cause: error })
  }
  if (!(decoded instanceof Map)) throw new Error('CBOR payload must be a map')
  if (!equalBytes(input, encode(decoded, rfc8949EncodeOptions))) {
    throw new Error('CBOR payload is not in RFC 8949 deterministic form')
  }
  return decoded
}

function rejectUnknownKeys (fields, allowed, label) {
  for (const key of fields.keys()) {
    if (!allowed.has(key)) throw new Error(`Unknown ${label} field: ${key}`)
  }
}

function base64Encode (value) {
  if (typeof globalThis.btoa !== 'function') throw new Error('Base64 encoder is unavailable')
  return globalThis.btoa(value)
}

function base64Decode (value) {
  if (typeof globalThis.atob !== 'function') throw new Error('Base64 decoder is unavailable')
  return globalThis.atob(value)
}

function equalBytes (left, right) {
  if (left.byteLength !== right.byteLength) return false
  for (let index = 0; index < left.byteLength; index += 1) {
    if (left[index] !== right[index]) return false
  }
  return true
}
