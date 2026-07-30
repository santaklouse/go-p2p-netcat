import { decode, encode, rfc8949EncodeOptions } from 'cborg'
import { publicKeyFromProtobuf, publicKeyToProtobuf } from '@libp2p/crypto/keys'
import { peerIdFromPublicKey } from '@libp2p/peer-id'
import { multiaddr } from '@multiformats/multiaddr'

const ROUTE_RECORD_VERSION = 1
const ROUTE_RECORD_ENVELOPE_VERSION = 1
const DEFAULT_ROUTE_RECORD_TTL_SECONDS = 180
const MAX_ROUTE_RECORD_BYTES = 64 * 1024
const MAX_ROUTE_ADDRESSES = 64
const MAX_ROUTE_SERVICES = 64
const CAPABILITIES = Object.freeze({
  tcp: 1,
  quic: 2,
  ws: 4,
  wss: 8,
  webtransport: 16,
  webrtc: 32,
  relay: 64
})

export {
  CAPABILITIES as ROUTE_CAPABILITIES,
  DEFAULT_ROUTE_RECORD_TTL_SECONDS,
  ROUTE_RECORD_VERSION
}

export async function signRouteRecord (privateKey, value = {}) {
  if (privateKey?.publicKey == null || typeof privateKey.sign !== 'function') {
    throw new TypeError('A libp2p private key is required to sign a route record')
  }
  const peerId = peerIdFromPublicKey(privateKey.publicKey).toString()
  const record = normalizeRouteRecord({
    ...value,
    peerId,
    issuedAt: value.issuedAt ?? Math.floor(Date.now() / 1000),
    expiresAt: value.expiresAt ?? Math.floor(Date.now() / 1000) + DEFAULT_ROUTE_RECORD_TTL_SECONDS
  })
  const payload = encodeRouteRecordPayload(record)
  const signature = await privateKey.sign(payload)
  return encode(new Map([
    [0, ROUTE_RECORD_ENVELOPE_VERSION],
    [1, payload],
    [2, publicKeyToProtobuf(privateKey.publicKey)],
    [3, signature]
  ]), rfc8949EncodeOptions)
}

export async function verifyRouteRecord (value, {
  expectedPeerId,
  expectedService,
  nowSeconds = Math.floor(Date.now() / 1000),
  clockSkewSeconds = 30
} = {}) {
  const envelope = decodeMap(binary(value, 'Signed route record'))
  rejectUnknownKeys(envelope, new Set([0, 1, 2, 3]), 'route record envelope')
  if (envelope.get(0) !== ROUTE_RECORD_ENVELOPE_VERSION) {
    throw new Error(`Unsupported route record envelope version: ${envelope.get(0)}`)
  }
  const payload = binary(envelope.get(1), 'Route record payload')
  const publicKeyBytes = binary(envelope.get(2), 'Route record public key')
  const signature = binary(envelope.get(3), 'Route record signature')
  if (payload.byteLength === 0 || payload.byteLength > MAX_ROUTE_RECORD_BYTES) {
    throw new Error(`Route record payload must contain 1..${MAX_ROUTE_RECORD_BYTES} bytes`)
  }
  const publicKey = publicKeyFromProtobuf(publicKeyBytes)
  if (!await publicKey.verify(payload, signature)) throw new Error('Route record signature is invalid')
  const record = decodeRouteRecordPayload(payload)
  const authenticatedPeerId = peerIdFromPublicKey(publicKey).toString()
  if (record.peerId !== authenticatedPeerId) {
    throw new Error(`Route record PeerId ${record.peerId} does not match signing key ${authenticatedPeerId}`)
  }
  if (expectedPeerId != null && record.peerId !== String(expectedPeerId)) {
    throw new Error(`Route record belongs to PeerId ${record.peerId}, not ${expectedPeerId}`)
  }
  if (expectedService != null && !record.services.includes(normalizeService(expectedService))) {
    throw new Error(`Route record does not advertise logical port ${expectedService}`)
  }
  const now = safeInteger(nowSeconds, 'Current Unix time')
  const skew = safeInteger(clockSkewSeconds, 'Route record clock skew')
  if (skew < 0) throw new Error('Route record clock skew must not be negative')
  if (record.issuedAt > now + skew) throw new Error('Route record was issued in the future')
  if (record.expiresAt < now - skew) throw new Error('Route record has expired')
  return record
}

export function encodeRouteRecordPayload (value) {
  const record = normalizeRouteRecord(value)
  return encode(new Map([
    [0, record.version],
    [1, record.peerId],
    [2, record.sequence],
    [3, record.issuedAt],
    [4, record.expiresAt],
    [5, record.services],
    [6, record.addresses],
    [7, record.relayReservations],
    [8, record.capabilities]
  ]), rfc8949EncodeOptions)
}

export function decodeRouteRecordPayload (value) {
  const fields = decodeMap(binary(value, 'Route record payload'))
  rejectUnknownKeys(fields, new Set([0, 1, 2, 3, 4, 5, 6, 7, 8]), 'route record')
  return normalizeRouteRecord({
    version: fields.get(0),
    peerId: fields.get(1),
    sequence: fields.get(2),
    issuedAt: fields.get(3),
    expiresAt: fields.get(4),
    services: fields.get(5),
    addresses: fields.get(6),
    relayReservations: fields.get(7),
    capabilities: fields.get(8)
  })
}

export function normalizeRouteRecord (value) {
  if (value == null || typeof value !== 'object') throw new TypeError('Route record must be an object')
  const version = safeInteger(value.version ?? ROUTE_RECORD_VERSION, 'Route record version')
  if (version !== ROUTE_RECORD_VERSION) throw new Error(`Unsupported route record version: ${version}`)
  const peerId = String(value.peerId ?? '').trim()
  if (peerId.length === 0 || peerId.length > 256) throw new Error('Route record PeerId must contain 1..256 characters')
  const sequence = safeInteger(value.sequence ?? Date.now(), 'Route record sequence')
  if (sequence < 0) throw new Error('Route record sequence must not be negative')
  const issuedAt = safeInteger(value.issuedAt, 'Route record issuedAt')
  const expiresAt = safeInteger(value.expiresAt, 'Route record expiresAt')
  if (issuedAt < 0 || expiresAt < 0 || expiresAt <= issuedAt) {
    throw new Error('Route record expiration must be later than its issue time')
  }
  const services = normalizeServices(value.services)
  const addresses = normalizeAddresses(value.addresses, 'addresses')
  const relayReservations = normalizeAddresses(value.relayReservations, 'relayReservations')
  const capabilities = normalizeCapabilities(value.capabilities)
  return Object.freeze({
    version,
    peerId,
    sequence,
    issuedAt,
    expiresAt,
    services: Object.freeze(services),
    addresses: Object.freeze(addresses),
    relayReservations: Object.freeze(relayReservations),
    capabilities
  })
}

export function routeCapabilityMask (value = {}) {
  if (Number.isSafeInteger(value)) return normalizeCapabilities(value)
  if (value == null || typeof value !== 'object') throw new TypeError('Route capabilities must be an object or bit mask')
  let mask = 0
  for (const [name, bit] of Object.entries(CAPABILITIES)) {
    if (value[name] === true) mask |= bit
  }
  return mask
}

export function routeCapabilitiesFromMask (value) {
  const mask = normalizeCapabilities(value)
  return Object.freeze(Object.fromEntries(
    Object.entries(CAPABILITIES).map(([name, bit]) => [name, (mask & bit) !== 0])
  ))
}

function normalizeServices (value) {
  if (!Array.isArray(value) || value.length === 0 || value.length > MAX_ROUTE_SERVICES) {
    throw new Error(`Route record services must contain 1..${MAX_ROUTE_SERVICES} logical ports`)
  }
  return [...new Set(value.map(normalizeService))].sort((a, b) => a - b)
}

function normalizeAddresses (value, label) {
  if (value == null) return []
  if (!Array.isArray(value) || value.length > MAX_ROUTE_ADDRESSES) {
    throw new Error(`Route record ${label} must contain at most ${MAX_ROUTE_ADDRESSES} multiaddrs`)
  }
  return [...new Set(value.map(address => {
    try {
      return multiaddr(String(address ?? '').trim()).toString()
    } catch (error) {
      throw new Error(`Invalid route record multiaddr: ${address}`, { cause: error })
    }
  }))].sort()
}

function normalizeCapabilities (value) {
  const mask = Number(value ?? 0)
  const all = Object.values(CAPABILITIES).reduce((result, bit) => result | bit, 0)
  if (!Number.isSafeInteger(mask) || mask < 0 || (mask & ~all) !== 0) {
    throw new Error(`Invalid route capability mask: ${value}`)
  }
  return mask
}

function normalizeService (value) {
  const service = Number(value)
  if (!Number.isSafeInteger(service) || service < 1 || service > 65535) {
    throw new Error(`Logical port must be an integer from 1 to 65535, received: ${value}`)
  }
  return service
}

function binary (value, label) {
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
  const input = binary(value, 'Route record CBOR payload')
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
    throw new Error('Invalid deterministic CBOR route record', { cause: error })
  }
  if (!(decoded instanceof Map)) throw new Error('Route record CBOR payload must be a map')
  if (!equalBytes(input, encode(decoded, rfc8949EncodeOptions))) {
    throw new Error('Route record CBOR payload is not in RFC 8949 deterministic form')
  }
  return decoded
}

function rejectUnknownKeys (fields, allowed, label) {
  for (const key of fields.keys()) {
    if (!allowed.has(key)) throw new Error(`Unknown ${label} field: ${key}`)
  }
}

function equalBytes (left, right) {
  if (left.byteLength !== right.byteLength) return false
  for (let index = 0; index < left.byteLength; index += 1) {
    if (left[index] !== right[index]) return false
  }
  return true
}
