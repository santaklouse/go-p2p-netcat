import { publicKeyFromProtobuf, publicKeyToProtobuf } from '@libp2p/crypto/keys'
import { peerIdFromPrivateKey, peerIdFromPublicKey, peerIdFromString } from '@libp2p/peer-id'
import { multiaddr } from '@multiformats/multiaddr'

export * from './native-webrtc.js'
export * from './signaling.js'
export * from './native-endpoint.js'
export * from './pairing.js'
export * from './route-record.js'
export * from './session-auth.js'
export * from './authenticated-stream.js'

export const APP_NAME = 'p2p-netcat'
export const PROTOCOL_PREFIX = '/p2p-netcat/1.0.0'
export const DEFAULT_SERVICE = 31337
export const WEBRTC_APP_ID = 'io.github.santaklouse.p2p-netcat.v1'
export const WEBRTC_AUTH_VERSION = 1
export const WEBRTC_CLIENT_ID_BYTES = 20
export const PUBSUB_DISCOVERY_TOPIC = 'io.github.santaklouse.p2p-netcat.peer-discovery.v1'
export const PUBSUB_DISCOVERY_INTERVAL_MS = 10_000
export const WEBRTC_RECONNECT_GRACE_MS = 120_000
export const WEBRTC_DATA_ACTION = 'pnc-data-v1'
export const WEBRTC_CONTROL_ACTION = 'pnc-ctl-v1'
export const PTY_FRAME_DATA = 0
export const PTY_FRAME_RESIZE = 1
export const PTY_FRAME_HEADER_LENGTH = 5
export const PTY_MAX_FRAME_LENGTH = 1024 * 1024
export const DEFAULT_STUN_URLS = Object.freeze([
  'stun:stun.l.google.com:19302',
  'stun:stun1.l.google.com:19302',
  'stun:stun2.l.google.com:19302',
  'stun:stun3.l.google.com:19302',
  'stun:stun4.l.google.com:19302',
  'stun:stun.counterpath.com:3478',
  'stun:stun.sipgate.net:3478',
  'stun:stun.voipbuster.com:3478',
  'stun:stun.internetcalls.com:3478'
])

export function defaultRtcConfiguration () {
  return {
    iceServers: [{ urls: [...DEFAULT_STUN_URLS] }]
  }
}

export function validateService (value = DEFAULT_SERVICE) {
  const service = Number(value)

  if (!Number.isInteger(service) || service < 1 || service > 65535) {
    throw new Error(`Логический порт должен быть целым числом от 1 до 65535, получено: ${value}`)
  }

  return service
}

export function protocolForService (service) {
  return `${PROTOCOL_PREFIX}/${validateService(service)}`
}

export function encodePtyData (value) {
  return encodePtyFrame(PTY_FRAME_DATA, value)
}

export function encodePtyResize (columns, rows) {
  const payload = new Uint8Array(4)
  const view = new DataView(payload.buffer)
  view.setUint16(0, normalizePtyDimension(columns, 80))
  view.setUint16(2, normalizePtyDimension(rows, 24))
  return encodePtyFrame(PTY_FRAME_RESIZE, payload)
}

export function decodePtyResize (value) {
  const payload = asBytes(value)
  if (payload.byteLength !== 4) throw new Error(`PTY resize payload must contain 4 bytes, received: ${payload.byteLength}`)
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
  return Object.freeze({ columns: Math.max(1, view.getUint16(0)), rows: Math.max(1, view.getUint16(2)) })
}

export class PtyFrameDecoder {
  #buffer = new Uint8Array(0)

  push (value) {
    const chunk = asBytes(value)
    const combined = new Uint8Array(this.#buffer.byteLength + chunk.byteLength)
    combined.set(this.#buffer)
    combined.set(chunk, this.#buffer.byteLength)
    this.#buffer = combined

    const frames = []
    while (this.#buffer.byteLength >= PTY_FRAME_HEADER_LENGTH) {
      const view = new DataView(this.#buffer.buffer, this.#buffer.byteOffset, this.#buffer.byteLength)
      const length = view.getUint32(1)
      if (length > PTY_MAX_FRAME_LENGTH) throw new Error(`PTY frame exceeds ${PTY_MAX_FRAME_LENGTH} bytes`)
      if (this.#buffer.byteLength < PTY_FRAME_HEADER_LENGTH + length) break
      frames.push(Object.freeze({
        type: this.#buffer[0],
        data: this.#buffer.slice(PTY_FRAME_HEADER_LENGTH, PTY_FRAME_HEADER_LENGTH + length)
      }))
      this.#buffer = this.#buffer.slice(PTY_FRAME_HEADER_LENGTH + length)
    }
    return frames
  }

  finish () {
    if (this.#buffer.byteLength !== 0) throw new Error('PTY stream ended inside a frame')
  }

  reset () {
    this.#buffer = new Uint8Array(0)
  }
}

export function normalizePeerId (value) {
  const text = String(value ?? '').trim()
  if (text.length === 0) throw new Error('PeerId не указан')
  return peerIdFromString(text).toString()
}

export function normalizeMultiaddr (value) {
  const text = String(value ?? '').trim().replace(/\/$/, '')
  if (text.length === 0) throw new Error('Multiaddr не указан')
  return multiaddr(text).toString().replace(/\/$/, '')
}

export function isWebSocketAddress (value) {
  const text = String(value)
  return /\/(?:ws|wss)(?:\/|$)/.test(text)
}

export function isSecureWebSocketAddress (value) {
  const text = String(value)
  return /\/wss(?:\/|$)/.test(text) || /\/tls\/ws(?:\/|$)/.test(text)
}

export function normalizeRelayAddress (value, {
  requireWebSocket = false,
  secureContext = false
} = {}) {
  const relay = normalizeMultiaddr(value)

  if (!/\/p2p\/[^/]+(?:\/|$)/.test(relay)) {
    throw new Error(`Адрес relay должен содержать /p2p/PeerId: ${value}`)
  }
  if (requireWebSocket && !isWebSocketAddress(relay)) {
    throw new Error('Браузеру нужен WebSocket relay-адрес с /ws или /wss')
  }
  if (secureContext && !isSecureWebSocketAddress(relay)) {
    throw new Error('HTTPS-страница может подключаться только к защищённому /wss relay')
  }

  return relay
}

export function relayedTargetAddress (relay, peerId, options) {
  const relayAddress = normalizeRelayAddress(relay, options)
  const targetPeerId = normalizePeerId(peerId)
  return multiaddr(`${relayAddress}/p2p-circuit/p2p/${targetPeerId}`)
}

export function createRelayDialPlan ({
  peerId,
  service = DEFAULT_SERVICE,
  relay,
  requireWebSocket = false,
  secureContext = false
}) {
  const targetPeerId = normalizePeerId(peerId)
  const logicalPort = validateService(service)
  const relayAddress = normalizeRelayAddress(relay, { requireWebSocket, secureContext })

  return Object.freeze({
    peerId: targetPeerId,
    service: logicalPort,
    protocol: protocolForService(logicalPort),
    relay: relayAddress,
    destination: `${relayAddress}/p2p-circuit/p2p/${targetPeerId}`
  })
}

export function addressRank (address) {
  const value = addressText(address)
  if (value.includes('/p2p-circuit')) return 50
  if (value.includes('/webrtc-direct')) return 0
  if (value.includes('/quic-v1')) return 10
  if (value.includes('/webtransport')) return 20
  if (isSecureWebSocketAddress(value)) return 30
  if (isWebSocketAddress(value)) return 35
  if (value.includes('/tcp/')) return 40
  return 45
}

export function preferDialAddresses (a, b) {
  return addressRank(addressText(a)) - addressRank(addressText(b))
}

export function browserDialableAddress (address, { secureContext = false } = {}) {
  const value = addressText(address)
  if (value.includes('/webrtc') || value.includes('/webtransport')) return true
  if (!isWebSocketAddress(value)) return false
  return !secureContext || isSecureWebSocketAddress(value)
}

export function webRtcRoomId (peerId, service = DEFAULT_SERVICE) {
  return `${normalizePeerId(peerId)}:${validateService(service)}`
}

export function webRtcAuthPayload (peerId, service, challenge) {
  const nonce = asBytes(challenge)
  if (nonce.byteLength !== 32) throw new Error(`WebRTC challenge must contain 32 bytes, received: ${nonce.byteLength}`)
  // This historical domain is frozen for native peer compatibility; it is not a runtime Trystero dependency.
  const context = new TextEncoder().encode(`p2p-netcat/trystero-auth/v1\0${webRtcRoomId(peerId, service)}\0`)
  const payload = new Uint8Array(context.byteLength + nonce.byteLength)
  payload.set(context)
  payload.set(nonce, context.byteLength)
  return payload
}

export function createWebRtcClientChallenge (clientId) {
  const normalizedClientId = String(clientId ?? '').trim()
  if (!/^[0-9A-Za-z]{20}$/.test(normalizedClientId)) {
    throw new Error('WebRTC client ID must contain exactly 20 alphanumeric characters')
  }
  const challenge = crypto.getRandomValues(new Uint8Array(32))
  challenge.set(new TextEncoder().encode(normalizedClientId))
  return challenge
}

export function webRtcClientIdFromChallenge (challenge) {
  const value = asBytes(challenge)
  if (value.byteLength !== 32) return null
  const clientId = new TextDecoder().decode(value.subarray(0, WEBRTC_CLIENT_ID_BYTES))
  return /^[0-9A-Za-z]{20}$/.test(clientId) ? clientId : null
}

export async function signWebRtcAuthResponse (privateKey, service, challenge) {
  if (privateKey == null || typeof privateKey.sign !== 'function' || privateKey.publicKey == null) {
    throw new TypeError('A libp2p private key is required')
  }
  const peerId = peerIdFromPrivateKey(privateKey).toString()
  return encodeWebRtcAuthResponse(
    publicKeyToProtobuf(privateKey.publicKey),
    await privateKey.sign(webRtcAuthPayload(peerId, service, challenge))
  )
}

export async function verifyWebRtcAuthResponse (value, expectedPeerId, service, challenge) {
  const normalizedPeerId = normalizePeerId(expectedPeerId)
  const response = decodeWebRtcAuthResponse(value)
  const publicKey = publicKeyFromProtobuf(response.publicKey)
  if (peerIdFromPublicKey(publicKey).toString() !== normalizedPeerId) return false
  return publicKey.verify(
    webRtcAuthPayload(normalizedPeerId, service, challenge),
    response.signature
  )
}

export function encodeWebRtcAuthResponse (publicKey, signature) {
  const key = asBytes(publicKey)
  const proof = asBytes(signature)
  if (key.byteLength > 0xffff || proof.byteLength > 0xffff) throw new Error('WebRTC authentication response is too large')
  const response = new Uint8Array(5 + key.byteLength + proof.byteLength)
  const view = new DataView(response.buffer)
  response[0] = WEBRTC_AUTH_VERSION
  view.setUint16(1, key.byteLength)
  view.setUint16(3, proof.byteLength)
  response.set(key, 5)
  response.set(proof, 5 + key.byteLength)
  return response
}

export function decodeWebRtcAuthResponse (value) {
  const response = asBytes(value)
  if (response.byteLength < 5 || response[0] !== WEBRTC_AUTH_VERSION) throw new Error('Unsupported WebRTC authentication response')
  const view = new DataView(response.buffer, response.byteOffset, response.byteLength)
  const publicKeyLength = view.getUint16(1)
  const signatureLength = view.getUint16(3)
  if (5 + publicKeyLength + signatureLength !== response.byteLength) throw new Error('Malformed WebRTC authentication response')
  return Object.freeze({
    publicKey: response.slice(5, 5 + publicKeyLength),
    signature: response.slice(5 + publicKeyLength)
  })
}

export class WebRtcStream {
  status = 'open'
  writeStatus = 'writable'
  connectionStatus = 'connected'
  #sendData
  #sendControl
  #onFinalize
  #flowWindowBytes
  #flowEnabled = false
  #inFlightBytes = 0
  #flowWaiters = []
  #keepAliveTimer
  #peerReconnectTimer
  #peerWaiters = []
  #pending = Promise.resolve()
  #items = []
  #waiters = []
  #readClosed = false
  #finalized = false

  constructor ({
    sendData,
    sendControl,
    onFinalize = () => {},
    flowWindowBytes = 256 * 1024,
    keepAliveIntervalMs = 15_000
  }) {
    if (!Number.isSafeInteger(flowWindowBytes) || flowWindowBytes < 1) {
      throw new RangeError('flowWindowBytes must be a positive integer')
    }
    if (!Number.isSafeInteger(keepAliveIntervalMs) || keepAliveIntervalMs < 0) {
      throw new RangeError('keepAliveIntervalMs must be a non-negative integer')
    }
    this.#sendData = sendData
    this.#sendControl = sendControl
    this.#onFinalize = onFinalize
    this.#flowWindowBytes = flowWindowBytes

    queueMicrotask(() => {
      if (this.status === 'open') void this.#sendControlSafely('flow:1').catch(() => {})
    })
    if (keepAliveIntervalMs > 0) {
      this.#keepAliveTimer = setInterval(() => {
        if (this.status === 'open') void this.#sendControlSafely('ping').catch(() => {})
      }, keepAliveIntervalMs)
      this.#keepAliveTimer.unref?.()
    }
  }

  send (chunk) {
    if (this.writeStatus !== 'writable') throw new Error('WebRTC stream is not writable')
    const bytes = asBytes(chunk).slice()
    this.#pending = this.#pending.then(async () => {
      for (;;) {
        await this.#waitForPeer()
        await this.#waitForFlowWindow(bytes.byteLength)
        await this.#waitForPeer()
        if (this.writeStatus === 'closed') throw new Error('WebRTC stream is not writable')
        if (this.#flowEnabled) this.#inFlightBytes += bytes.byteLength
        try {
          await this.#sendData(bytes)
          return
        } catch (error) {
          if (this.#flowEnabled) this.#inFlightBytes -= bytes.byteLength
          this.#wakeFlowWaiters()
          if (this.status !== 'open' || this.writeStatus === 'closed') throw error
          this.peerDisconnected()
        }
      }
    })
    return false
  }

  onDrain () {
    return this.#pending
  }

  async close () {
    if (this.writeStatus !== 'writable') return
    this.writeStatus = 'closing'
    await this.#pending
    await this.#sendControl('eof')
    this.writeStatus = 'closed'
    this.#maybeFinalize()
  }

  abort (error = new Error('WebRTC stream aborted')) {
    if (this.status === 'closed') return
    this.writeStatus = 'closed'
    this.status = 'closed'
    void this.#sendControlSafely('abort').catch(() => {})
    this.#fail(error)
    this.#finalize()
  }

  receiveData (chunk) {
    if (this.#readClosed || this.status === 'closed') return
    const bytes = asBytes(chunk).slice()
    const waiter = this.#waiters.shift()
    if (waiter != null) waiter.resolve({ value: bytes, done: false })
    else this.#items.push(bytes)
  }

  receiveControl (control) {
    if (control === 'eof') {
      this.#endRead()
      this.#maybeFinalize()
    } else if (control === 'abort') {
      this.status = 'closed'
      this.writeStatus = 'closed'
      const error = new Error('Remote WebRTC peer aborted the stream')
      this.#fail(error)
      this.#wakeFlowWaiters(error)
      this.#finalize()
    } else if (control === 'flow:1') {
      this.#flowEnabled = true
    } else if (control === 'resume') {
      this.#inFlightBytes = 0
      this.#wakeFlowWaiters()
      void this.#sendControlSafely('flow:1').catch(() => {})
    } else if (control.startsWith('ack:')) {
      const acknowledged = Number.parseInt(control.slice(4), 10)
      if (Number.isSafeInteger(acknowledged) && acknowledged > 0) {
        this.#inFlightBytes = Math.max(0, this.#inFlightBytes - acknowledged)
        this.#wakeFlowWaiters()
      }
    } else if (control === 'ping') {
      void this.#sendControlSafely('pong').catch(() => {})
    }
  }

  peerDisconnected (graceMs = WEBRTC_RECONNECT_GRACE_MS) {
    if (this.status === 'closed' || this.connectionStatus === 'reconnecting') return
    if (!Number.isSafeInteger(graceMs) || graceMs < 0) {
      throw new RangeError('graceMs must be a non-negative integer')
    }
    if (graceMs === 0) {
      this.peerLeft()
      return
    }

    this.connectionStatus = 'reconnecting'
    clearTimeout(this.#peerReconnectTimer)
    this.#peerReconnectTimer = setTimeout(() => this.peerLeft(), graceMs)
    this.#peerReconnectTimer.unref?.()
  }

  peerReconnected () {
    if (this.status === 'closed') return false
    if (this.connectionStatus !== 'reconnecting') return true
    clearTimeout(this.#peerReconnectTimer)
    this.#peerReconnectTimer = undefined
    this.connectionStatus = 'connected'
    this.#inFlightBytes = 0
    this.#wakeFlowWaiters()
    this.#wakePeerWaiters()
    void this.#sendControlSafely('resume').catch(() => {})
    void this.#sendControlSafely('flow:1').catch(() => {})
    return true
  }

  peerLeft () {
    if (this.status === 'closed') return
    const error = new Error('Remote WebRTC peer left')
    clearTimeout(this.#peerReconnectTimer)
    this.#peerReconnectTimer = undefined
    this.connectionStatus = 'disconnected'
    this.status = 'closed'
    this.writeStatus = 'closed'
    this.#endRead()
    this.#wakeFlowWaiters(error)
    this.#wakePeerWaiters(error)
    this.#finalize()
  }

  [Symbol.asyncIterator] () {
    let consumedBytes = 0
    return {
      next: async () => {
        if (consumedBytes > 0) {
          const acknowledged = consumedBytes
          consumedBytes = 0
          await this.#sendControlSafely(`ack:${acknowledged}`).catch(() => {})
        }
        const item = this.#items.shift()
        if (item != null) {
          consumedBytes = item.byteLength
          return { value: item, done: false }
        }
        if (this.#readClosed) return { value: undefined, done: true }
        const result = await new Promise((resolve, reject) => this.#waiters.push({ resolve, reject }))
        if (!result.done) consumedBytes = result.value.byteLength
        return result
      },
      return: async () => {
        if (consumedBytes > 0) {
          await this.#sendControlSafely(`ack:${consumedBytes}`).catch(() => {})
          consumedBytes = 0
        }
        return { value: undefined, done: true }
      }
    }
  }

  async #waitForFlowWindow (byteLength) {
    while (
      this.#flowEnabled &&
      this.status === 'open' &&
      this.writeStatus !== 'closed' &&
      this.#inFlightBytes > 0 &&
      this.#inFlightBytes + byteLength > this.#flowWindowBytes
    ) {
      await new Promise((resolve, reject) => this.#flowWaiters.push({ resolve, reject }))
    }
    if (this.status !== 'open' || this.writeStatus === 'closed') {
      throw new Error('WebRTC stream is not writable')
    }
  }

  async #waitForPeer () {
    if (this.connectionStatus === 'connected') return
    if (this.status !== 'open' || this.connectionStatus === 'disconnected') {
      throw new Error('WebRTC stream is not writable')
    }
    await new Promise((resolve, reject) => this.#peerWaiters.push({ resolve, reject }))
    if (this.status !== 'open' || this.connectionStatus !== 'connected') {
      throw new Error('WebRTC stream is not writable')
    }
  }

  #wakePeerWaiters (error) {
    for (const waiter of this.#peerWaiters.splice(0)) {
      if (error == null) waiter.resolve()
      else waiter.reject(error)
    }
  }

  #wakeFlowWaiters (error) {
    for (const waiter of this.#flowWaiters.splice(0)) {
      if (error == null) waiter.resolve()
      else waiter.reject(error)
    }
  }

  #sendControlSafely (control) {
    try {
      return Promise.resolve(this.#sendControl(control))
    } catch (error) {
      return Promise.reject(error)
    }
  }

  #endRead () {
    if (this.#readClosed) return
    this.#readClosed = true
    for (const waiter of this.#waiters.splice(0)) waiter.resolve({ value: undefined, done: true })
  }

  #fail (error) {
    this.#readClosed = true
    for (const waiter of this.#waiters.splice(0)) waiter.reject(error)
  }

  #maybeFinalize () {
    if (this.#readClosed && this.writeStatus === 'closed') {
      this.status = 'closed'
      this.#finalize()
    }
  }

  #finalize () {
    if (this.#finalized) return
    this.#finalized = true
    clearInterval(this.#keepAliveTimer)
    clearTimeout(this.#peerReconnectTimer)
    this.#wakeFlowWaiters(new Error('WebRTC stream is closed'))
    this.#wakePeerWaiters(new Error('WebRTC stream is closed'))
    this.#onFinalize()
  }
}

export function createWebRtcActionHub (room, {
  onStream,
  onStreamClosed,
  onPeerDisconnected,
  onPeerReconnected,
  leaveAfterStream = false,
  reconnectGraceMs = WEBRTC_RECONNECT_GRACE_MS,
  release = () => {}
} = {}) {
  if (room == null || typeof room.makeAction !== 'function' || typeof room.leave !== 'function') {
    throw new TypeError('A WebRTC action room with makeAction() and leave() is required')
  }

  const streams = new Map()
  const data = room.makeAction(WEBRTC_DATA_ACTION)
  const control = room.makeAction(WEBRTC_CONTROL_ACTION)
  let closePromise

  const close = () => {
    closePromise ??= Promise.resolve().then(async () => {
      for (const stream of streams.values()) stream.peerLeft()
      streams.clear()
      try {
        await room.leave()
      } finally {
        release()
      }
    })
    return closePromise
  }

  const streamFor = peerId => {
    let stream = streams.get(peerId)
    if (stream != null) return stream
    stream = new WebRtcStream({
      sendData: chunk => data.send(chunk, { target: peerId }),
      sendControl: value => control.send(value, { target: peerId }),
      onFinalize: () => {
        streams.delete(peerId)
        onStreamClosed?.(peerId, stream)
        if (leaveAfterStream) void close()
      }
    })
    streams.set(peerId, stream)
    return stream
  }

  data.onMessage = (chunk, { peerId }) => streamFor(peerId).receiveData(asBytes(chunk))
  control.onMessage = (value, { peerId }) => streamFor(peerId).receiveControl(String(value))
  room.onPeerJoin = peerId => {
    const existing = streams.get(peerId)
    if (existing != null) {
      if (existing.connectionStatus === 'reconnecting' && existing.peerReconnected()) {
        onPeerReconnected?.(peerId, existing)
      }
      return
    }
    onStream?.(streamFor(peerId), peerId)
  }
  room.onPeerLeave = peerId => {
    const stream = streams.get(peerId)
    if (stream == null) return
    stream.peerDisconnected(reconnectGraceMs)
    onPeerDisconnected?.(peerId, stream)
  }

  return Object.freeze({ streamFor, close })
}

function addressText (address) {
  if (address?.multiaddr != null) return address.multiaddr.toString()
  return address?.toString?.() ?? String(address)
}

function encodePtyFrame (type, value) {
  const payload = asBytes(value)
  if (payload.byteLength > PTY_MAX_FRAME_LENGTH) throw new Error(`PTY frame exceeds ${PTY_MAX_FRAME_LENGTH} bytes`)
  const result = new Uint8Array(PTY_FRAME_HEADER_LENGTH + payload.byteLength)
  const view = new DataView(result.buffer)
  result[0] = type
  view.setUint32(1, payload.byteLength)
  result.set(payload, PTY_FRAME_HEADER_LENGTH)
  return result
}

function normalizePtyDimension (value, fallback) {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return fallback
  return Math.max(1, Math.min(0xffff, Math.trunc(numeric)))
}

function asBytes (value) {
  if (value instanceof Uint8Array) return value
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  throw new TypeError('Expected binary data')
}
