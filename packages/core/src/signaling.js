import { schnorr } from '@noble/secp256k1'
import {
  assertPairingTokenUsable,
  base64UrlDecode,
  base64UrlEncode,
  deriveRendezvousId,
  openPairingPayload,
  sealPairingPayload
} from './pairing.js'

const encoder = new TextEncoder()
const SIGNAL_VERSION = 2
const SIGNAL_TTL_SECONDS = 120
const NOSTR_EVENT_KIND = 25050
const TRACKER_ANNOUNCE_INTERVAL_MS = 10_000
const RECONNECT_MIN_MS = 1_000
const RECONNECT_MAX_MS = 30_000
const ENCRYPTED_SIGNAL_PREFIX = 'pnc-signal-v1:'

export const NATIVE_SIGNAL_VERSION = SIGNAL_VERSION
export const DEFAULT_NOSTR_SIGNALING_URLS = Object.freeze([
  'wss://nos.lol',
  'wss://nostr-01.yakihonne.com',
  'wss://relay.primal.net',
  'wss://purplerelay.com',
  'wss://relay.nostr.place'
])
export const DEFAULT_TORRENT_SIGNALING_URLS = Object.freeze([
  'wss://open.ftorrent.com',
  'wss://tracker.webtorrent.dev',
  'wss://tracker.openwebtorrent.com',
  'wss://tracker.btorrent.xyz',
  'wss://tracker.files.fm:7073/announce'
])

export function createSignalingPeerId () {
  const alphabet = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz'
  const random = crypto.getRandomValues(new Uint8Array(20))
  return Array.from(random, byte => alphabet[byte % alphabet.length]).join('')
}

export function createSignalingSessionId () {
  return randomHex(16)
}

export async function nativeSignalingRoomTopic (roomId, { pairingToken } = {}) {
  const room = String(roomId ?? '').trim()
  if (room.length === 0) throw new Error('Native signaling roomId is required')
  if (pairingToken != null) {
    const token = assertPairingTokenUsable(pairingToken)
    return deriveRendezvousId(token, { purpose: 'signaling', epoch: 0 })
  }
  return toHex(await digest('SHA-256', `p2p-netcat:native-webrtc:v${SIGNAL_VERSION}:${room}`))
}

export async function createNostrSignalingSession ({
  roomId,
  peerId = createSignalingPeerId(),
  urls = DEFAULT_NOSTR_SIGNALING_URLS,
  WebSocket: WebSocketImpl = globalThis.WebSocket,
  onStatus = () => {},
  pairingToken
}) {
  const token = pairingToken == null ? null : assertPairingTokenUsable(pairingToken)
  const topic = await nativeSignalingRoomTopic(roomId, { pairingToken: token })
  return new NostrSignalingSession({
    topic,
    peerId: normalizeSignalingPeerId(peerId),
    urls: normalizeWebSocketUrls(urls),
    WebSocketImpl,
    onStatus,
    pairingToken: token
  })
}

export async function createTorrentSignalingSession ({
  roomId,
  peerId = createSignalingPeerId(),
  urls = DEFAULT_TORRENT_SIGNALING_URLS,
  WebSocket: WebSocketImpl = globalThis.WebSocket,
  onStatus = () => {},
  pairingToken
}) {
  const token = pairingToken == null ? null : assertPairingTokenUsable(pairingToken)
  const topic = await nativeSignalingRoomTopic(roomId, { pairingToken: token })
  const infoHash = Array.from(
    await digest('SHA-1', topic),
    byte => byte.toString(36)
  )
    .join('')
    .slice(0, 20)
  return new TorrentSignalingSession({
    topic,
    infoHash,
    peerId: normalizeSignalingPeerId(peerId),
    urls: normalizeWebSocketUrls(urls),
    WebSocketImpl,
    onStatus,
    pairingToken: token
  })
}

class SignalingSession {
  name
  peerId
  topic
  ready
  pairingToken
  trickleIce
  #listeners = new Set()

  constructor ({ name, peerId, topic, ready, pairingToken, trickleIce }) {
    this.name = name
    this.peerId = peerId
    this.topic = topic
    this.ready = ready
    this.pairingToken = pairingToken
    this.trickleIce = Boolean(trickleIce)
  }

  subscribe (listener) {
    if (typeof listener !== 'function') throw new TypeError('Signaling listener must be a function')
    this.#listeners.add(listener)
    return () => this.#listeners.delete(listener)
  }

  async _emit (message) {
    const signal = await decodeIncomingSignal(message, this.topic, this.peerId, this.pairingToken)
    if (signal == null) return
    for (const listener of this.#listeners) {
      try {
        listener(signal)
      } catch {}
    }
  }
}

class NostrSignalingSession extends SignalingSession {
  #WebSocket
  #urls
  #onStatus
  #secretKey
  #publicKey
  #sockets = new Map()
  #closed = false
  #resolveReady
  #outbox = []
  #seenEvents = new Map()
  #messageChain = Promise.resolve()

  constructor ({ topic, peerId, urls, WebSocketImpl, onStatus, pairingToken }) {
    let resolveReady
    const ready = new Promise(resolve => {
      resolveReady = resolve
    })
    super({ name: 'Native Nostr', peerId, topic, ready, pairingToken, trickleIce: true })
    if (typeof WebSocketImpl !== 'function') throw new TypeError('WebSocket constructor is required for Nostr signaling')
    this.#WebSocket = WebSocketImpl
    this.#urls = urls
    this.#onStatus = onStatus
    this.#resolveReady = resolveReady
    const keys = schnorr.keygen()
    this.#secretKey = keys.secretKey
    this.#publicKey = toHex(keys.publicKey)
    for (const url of urls) this.#connect(url, RECONNECT_MIN_MS)
  }

  async publish (message) {
    if (this.#closed) throw new Error('Native Nostr signaling session is closed')
    const signal = await encodeOutgoingSignal(message, this.topic, this.peerId, this.pairingToken)
    const event = await this.#createEvent(signal)
    this.#outbox.push({ event, createdAt: Date.now() })
    this.#pruneOutbox()
    const payload = JSON.stringify(['EVENT', event])
    let sent = 0
    for (const entry of this.#sockets.values()) {
      if (entry.socket.readyState !== 1) continue
      entry.socket.send(payload)
      sent += 1
    }
    this.#status('*', 'published', `${sent}/${this.#sockets.size} relays`)
  }

  status () {
    return socketStatus('Native Nostr', this.#sockets)
  }

  async close () {
    if (this.#closed) return
    this.#closed = true
    for (const entry of this.#sockets.values()) {
      clearTimeout(entry.reconnectTimer)
      try {
        entry.socket.send(JSON.stringify(['CLOSE', entry.subscriptionId]))
      } catch {}
      try {
        entry.socket.close()
      } catch {}
    }
    this.#sockets.clear()
    this.#outbox.length = 0
  }

  #connect (url, retryMs) {
    if (this.#closed) return
    const subscriptionId = `pnc-${randomHex(8)}`
    let socket
    try {
      socket = new this.#WebSocket(url)
    } catch (error) {
      this.#status(url, 'error', errorMessage(error))
      this.#scheduleReconnect(url, retryMs)
      return
    }
    const entry = { socket, subscriptionId, reconnectTimer: undefined, retryMs }
    this.#sockets.set(url, entry)
    this.#status(url, 'connecting')
    socket.onopen = () => {
      if (this.#closed) return
      entry.retryMs = RECONNECT_MIN_MS
      socket.send(JSON.stringify([
        'REQ',
        subscriptionId,
        {
          kinds: [NOSTR_EVENT_KIND],
          '#t': [this.topic],
          since: nowSeconds() - 10
        }
      ]))
      this.#pruneOutbox()
      for (const item of this.#outbox) socket.send(JSON.stringify(['EVENT', item.event]))
      this.#resolveReady()
      this.#status(url, 'open')
    }
    socket.onmessage = event => {
      this.#messageChain = this.#messageChain
        .then(() => this.#receive(url, String(event.data)))
        .catch(error => this.#status(url, 'error', errorMessage(error)))
    }
    socket.onerror = event => this.#status(url, 'error', errorMessage(event?.error ?? 'WebSocket error'))
    socket.onclose = () => {
      this.#status(url, 'closed')
      if (!this.#closed) this.#scheduleReconnect(url, entry.retryMs)
    }
  }

  #scheduleReconnect (url, retryMs) {
    const entry = this.#sockets.get(url)
    clearTimeout(entry?.reconnectTimer)
    const nextRetry = Math.min(RECONNECT_MAX_MS, Math.max(RECONNECT_MIN_MS, retryMs * 2))
    const timer = setTimeout(() => this.#connect(url, nextRetry), retryMs + Math.random() * retryMs)
    timer.unref?.()
    if (entry != null) {
      entry.reconnectTimer = timer
      entry.retryMs = nextRetry
    }
  }

  async #receive (url, raw) {
    const message = JSON.parse(raw)
    if (!Array.isArray(message) || message[0] !== 'EVENT') return
    const event = message[2]
    if (event == null || typeof event !== 'object') return
    if (event.kind !== NOSTR_EVENT_KIND || typeof event.id !== 'string' || typeof event.content !== 'string') return
    if (!event.tags?.some(tag => Array.isArray(tag) && tag[0] === 't' && tag[1] === this.topic)) return
    if (!Number.isInteger(event.created_at) || Math.abs(nowSeconds() - event.created_at) > SIGNAL_TTL_SECONDS) return
    if (this.#seenEvents.has(event.id)) return

    const serialized = JSON.stringify([
      0,
      event.pubkey,
      event.created_at,
      event.kind,
      event.tags,
      event.content
    ])
    const eventId = await digest('SHA-256', serialized)
    if (toHex(eventId) !== event.id) return
    if (!await schnorr.verifyAsync(fromHex(event.sig, 64), eventId, fromHex(event.pubkey, 32))) return

    this.#seenEvents.set(event.id, Date.now())
    this.#pruneSeenEvents()
    await this._emit(JSON.parse(event.content))
    this.#status(url, 'message')
  }

  async #createEvent (signal) {
    const createdAt = nowSeconds()
    const tags = [['t', this.topic]]
    const content = JSON.stringify(signal)
    const serialized = JSON.stringify([0, this.#publicKey, createdAt, NOSTR_EVENT_KIND, tags, content])
    const idBytes = await digest('SHA-256', serialized)
    return {
      id: toHex(idBytes),
      pubkey: this.#publicKey,
      created_at: createdAt,
      kind: NOSTR_EVENT_KIND,
      tags,
      content,
      sig: toHex(await schnorr.signAsync(idBytes, this.#secretKey))
    }
  }

  #pruneOutbox () {
    const oldest = Date.now() - SIGNAL_TTL_SECONDS * 1_000
    this.#outbox = this.#outbox.filter(item => item.createdAt >= oldest).slice(-32)
  }

  #pruneSeenEvents () {
    const oldest = Date.now() - SIGNAL_TTL_SECONDS * 1_000
    for (const [id, createdAt] of this.#seenEvents) {
      if (createdAt < oldest) this.#seenEvents.delete(id)
    }
  }

  #status (url, state, detail) {
    try {
      this.#onStatus({ adapter: this.name, url, state, detail })
    } catch {}
  }
}

class TorrentSignalingSession extends SignalingSession {
  #WebSocket
  #urls
  #infoHash
  #onStatus
  #sockets = new Map()
  #offers = new Map()
  #offerOrigins = new Map()
  #seenSignals = new Map()
  #closed = false
  #resolveReady
  #messageChain = Promise.resolve()

  constructor ({ topic, infoHash, peerId, urls, WebSocketImpl, onStatus, pairingToken }) {
    let resolveReady
    const ready = new Promise(resolve => {
      resolveReady = resolve
    })
    super({ name: 'Native BitTorrent', peerId, topic, ready, pairingToken, trickleIce: false })
    if (typeof WebSocketImpl !== 'function') throw new TypeError('WebSocket constructor is required for BitTorrent signaling')
    this.#WebSocket = WebSocketImpl
    this.#urls = urls
    this.#infoHash = infoHash
    this.#onStatus = onStatus
    this.#resolveReady = resolveReady
    for (const url of urls) this.#connect(url, RECONNECT_MIN_MS)
  }

  async publish (message) {
    if (this.#closed) throw new Error('Native BitTorrent signaling session is closed')
    const signal = await encodeOutgoingSignal(message, this.topic, this.peerId, this.pairingToken)
    if (signal.type === 'candidate') {
      throw new Error('Native BitTorrent signaling requires complete non-trickle SDP')
    }

    if (signal.type === 'bye') {
      this.#offers.delete(signal.sessionId)
      this.#offerOrigins.delete(`${signal.sessionId}:${signal.to ?? ''}`)
      for (const entry of this.#sockets.values()) this.#announce(entry)
      return
    }

    if (signal.type === 'offer') {
      this.#offers.set(signal.sessionId, signal)
      for (const entry of this.#sockets.values()) this.#announce(entry)
      return
    }

    const originKey = `${signal.sessionId}:${signal.to ?? ''}`
    const origin = this.#offerOrigins.get(originKey)
    const entries = origin == null
      ? [...this.#sockets.values()]
      : [this.#sockets.get(origin.url)].filter(Boolean)
    for (const entry of entries) {
      if (entry.socket.readyState !== 1) continue
      entry.socket.send(JSON.stringify({
        action: 'announce',
        info_hash: this.#infoHash,
        peer_id: this.peerId,
        answer: { type: 'answer', sdp: trackerSignal(signal) },
        offer_id: signal.sessionId,
        to_peer_id: signal.to
      }))
    }
  }

  status () {
    return socketStatus('Native BitTorrent', this.#sockets)
  }

  async close () {
    if (this.#closed) return
    this.#closed = true
    for (const entry of this.#sockets.values()) {
      clearInterval(entry.announceTimer)
      clearTimeout(entry.reconnectTimer)
      try {
        entry.socket.close()
      } catch {}
    }
    this.#sockets.clear()
    this.#offers.clear()
    this.#offerOrigins.clear()
  }

  #connect (url, retryMs) {
    if (this.#closed) return
    let socket
    try {
      socket = new this.#WebSocket(url)
    } catch (error) {
      this.#status(url, 'error', errorMessage(error))
      this.#scheduleReconnect(url, retryMs)
      return
    }
    const entry = {
      url,
      socket,
      announceTimer: undefined,
      reconnectTimer: undefined,
      retryMs
    }
    this.#sockets.set(url, entry)
    this.#status(url, 'connecting')
    socket.onopen = () => {
      if (this.#closed) return
      entry.retryMs = RECONNECT_MIN_MS
      this.#announce(entry)
      entry.announceTimer = setInterval(() => this.#announce(entry), TRACKER_ANNOUNCE_INTERVAL_MS)
      entry.announceTimer.unref?.()
      this.#resolveReady()
      this.#status(url, 'open')
    }
    socket.onmessage = event => {
      this.#messageChain = this.#messageChain
        .then(() => this.#receive(entry, JSON.parse(String(event.data))))
        .catch(error => this.#status(url, 'error', errorMessage(error)))
    }
    socket.onerror = event => this.#status(url, 'error', errorMessage(event?.error ?? 'WebSocket error'))
    socket.onclose = () => {
      clearInterval(entry.announceTimer)
      this.#status(url, 'closed')
      if (!this.#closed) this.#scheduleReconnect(url, entry.retryMs)
    }
  }

  #announce (entry) {
    if (entry.socket.readyState !== 1) return
    entry.socket.send(JSON.stringify({
      action: 'announce',
      info_hash: this.#infoHash,
      peer_id: this.peerId,
      numwant: 3,
      offers: [...this.#offers.values()].slice(-3).map(signal => ({
        offer_id: signal.sessionId,
        offer: { type: 'offer', sdp: trackerSignal(signal) }
      }))
    }))
  }

  async #receive (entry, data) {
    if (typeof data?.['failure reason'] === 'string') {
      this.#status(entry.url, 'error', data['failure reason'])
      return
    }
    if (typeof data?.['warning message'] === 'string') {
      this.#status(entry.url, 'warning', data['warning message'])
    }
    if (data?.peer_id === this.peerId || typeof data?.peer_id !== 'string' || typeof data?.offer_id !== 'string') return

    if (typeof data?.offer?.sdp === 'string') {
      const signal = trackerIncomingSignal(data.offer.sdp, {
        topic: this.topic,
        type: 'offer',
        sessionId: data.offer_id,
        from: data.peer_id,
        to: this.peerId
      })
      this.#offerOrigins.set(`${signal.sessionId}:${signal.from}`, {
        url: entry.url,
        offerId: data.offer_id
      })
      await this.#emitOnce(signal)
      return
    }

    if (typeof data?.answer?.sdp === 'string' && this.#offers.has(data.offer_id)) {
      this.#offers.delete(data.offer_id)
      await this.#emitOnce(trackerIncomingSignal(data.answer.sdp, {
        topic: this.topic,
        type: 'answer',
        sessionId: data.offer_id,
        from: data.peer_id,
        to: this.peerId
      }))
    }
  }

  async #emitOnce (signal) {
    const key = `${signal.type}:${signal.sessionId}:${signal.from}`
    const lastSeen = this.#seenSignals.get(key)
    if (lastSeen != null && Date.now() - lastSeen < 5_000) return
    this.#seenSignals.set(key, Date.now())
    for (const [storedKey, seenAt] of this.#seenSignals) {
      if (Date.now() - seenAt > SIGNAL_TTL_SECONDS * 1_000) this.#seenSignals.delete(storedKey)
    }
    await this._emit(signal)
  }

  #scheduleReconnect (url, retryMs) {
    const entry = this.#sockets.get(url)
    clearTimeout(entry?.reconnectTimer)
    const nextRetry = Math.min(RECONNECT_MAX_MS, Math.max(RECONNECT_MIN_MS, retryMs * 2))
    const timer = setTimeout(() => this.#connect(url, nextRetry), retryMs + Math.random() * retryMs)
    timer.unref?.()
    if (entry != null) {
      entry.reconnectTimer = timer
      entry.retryMs = nextRetry
    }
  }

  #status (url, state, detail) {
    try {
      this.#onStatus({ adapter: this.name, url, state, detail })
    } catch {}
  }
}

function normalizeOutgoingSignal (message, topic, peerId) {
  if (message == null || typeof message !== 'object') throw new TypeError('Native signaling message must be an object')
  if (!['offer', 'answer', 'candidate', 'bye'].includes(message.type)) {
    throw new Error(`Unsupported native signaling message type: ${message.type}`)
  }
  const sessionId = String(message.sessionId ?? '').trim()
  if (sessionId.length < 8 || sessionId.length > 128) throw new Error('Native signaling sessionId is invalid')
  const signal = {
    version: SIGNAL_VERSION,
    room: topic,
    type: message.type,
    sessionId,
    from: peerId,
    createdAt: Date.now()
  }
  if (message.to != null) signal.to = normalizeSignalingPeerId(message.to)
  if (message.type === 'offer' || message.type === 'answer') {
    if (typeof message.sdp !== 'string' || message.sdp.length === 0) {
      throw new Error(`Native signaling ${message.type} must contain SDP`)
    }
    signal.sdp = message.sdp
  } else if (message.type === 'candidate') {
    if (message.candidate == null || typeof message.candidate !== 'object') {
      throw new Error('Native signaling candidate is missing')
    }
    signal.candidate = message.candidate
  }
  return signal
}

async function encodeOutgoingSignal (message, topic, peerId, pairingToken) {
  const signal = normalizeOutgoingSignal(message, topic, peerId)
  if (pairingToken == null) return signal
  const metadata = signalMetadata(signal)
  const payload = encoder.encode(JSON.stringify({
    sdp: signal.sdp,
    candidate: signal.candidate
  }))
  const encrypted = await sealPairingPayload(pairingToken, 'signaling', payload, {
    additionalData: signalAdditionalData(metadata)
  })
  return Object.freeze({
    ...metadata,
    encrypted: base64UrlEncode(encrypted)
  })
}

async function decodeIncomingSignal (message, topic, peerId, pairingToken) {
  if (pairingToken == null) return normalizeIncomingSignal(message, topic, peerId)
  if (message == null || typeof message !== 'object' || typeof message.encrypted !== 'string') return null
  const metadata = signalMetadata(message)
  let payload
  try {
    const plaintext = await openPairingPayload(
      pairingToken,
      'signaling',
      base64UrlDecode(message.encrypted),
      { additionalData: signalAdditionalData(metadata) }
    )
    payload = JSON.parse(new TextDecoder().decode(plaintext))
  } catch {
    return null
  }
  return normalizeIncomingSignal({
    ...metadata,
    sdp: payload?.sdp,
    candidate: payload?.candidate
  }, topic, peerId)
}

function signalMetadata (signal) {
  return {
    version: SIGNAL_VERSION,
    room: String(signal.room ?? ''),
    type: String(signal.type ?? ''),
    sessionId: String(signal.sessionId ?? ''),
    from: String(signal.from ?? ''),
    ...(signal.to == null ? {} : { to: String(signal.to) }),
    createdAt: Number(signal.createdAt)
  }
}

function signalAdditionalData (signal) {
  return encoder.encode(JSON.stringify([
    SIGNAL_VERSION,
    signal.room,
    signal.type,
    signal.sessionId,
    signal.from,
    signal.to ?? '',
    signal.createdAt
  ]))
}

function trackerSignal (signal) {
  if (typeof signal.encrypted !== 'string') return signal.sdp
  return `${ENCRYPTED_SIGNAL_PREFIX}${base64UrlEncode(encoder.encode(JSON.stringify(signal)))}`
}

function trackerIncomingSignal (value, { topic, type, sessionId, from, to }) {
  if (!String(value).startsWith(ENCRYPTED_SIGNAL_PREFIX)) {
    return {
      version: SIGNAL_VERSION,
      room: topic,
      type,
      sessionId,
      from,
      to,
      sdp: value,
      createdAt: Date.now()
    }
  }
  const decoded = JSON.parse(new TextDecoder().decode(
    base64UrlDecode(String(value).slice(ENCRYPTED_SIGNAL_PREFIX.length))
  ))
  if (
    decoded?.type !== type ||
    decoded?.sessionId !== sessionId ||
    decoded?.from !== from ||
    decoded?.room !== topic
  ) {
    throw new Error('Encrypted tracker signal metadata does not match its announce envelope')
  }
  if (decoded.to != null && decoded.to !== to) {
    throw new Error('Encrypted tracker signal is addressed to another peer')
  }
  return decoded
}

function normalizeIncomingSignal (message, topic, peerId) {
  if (message == null || typeof message !== 'object') return null
  if (message.version !== SIGNAL_VERSION || message.room !== topic) return null
  if (message.from === peerId || (message.to != null && message.to !== peerId)) return null
  if (!['offer', 'answer', 'candidate', 'bye'].includes(message.type)) return null
  if (typeof message.sessionId !== 'string' || typeof message.from !== 'string') return null
  if (!Number.isFinite(message.createdAt) || Math.abs(Date.now() - message.createdAt) > SIGNAL_TTL_SECONDS * 1_000) return null
  if ((message.type === 'offer' || message.type === 'answer') && typeof message.sdp !== 'string') return null
  if (message.type === 'candidate' && (message.candidate == null || typeof message.candidate !== 'object')) return null
  return Object.freeze({ ...message })
}

function normalizeSignalingPeerId (value) {
  const peerId = String(value ?? '').trim()
  if (!/^[0-9A-Za-z]{20}$/.test(peerId)) {
    throw new Error('Native signaling peerId must contain exactly 20 alphanumeric characters')
  }
  return peerId
}

function normalizeWebSocketUrls (urls) {
  if (!Array.isArray(urls) || urls.length === 0) throw new Error('At least one signaling WebSocket URL is required')
  return [...new Set(urls.map(value => {
    const url = new URL(String(value))
    if (url.protocol !== 'wss:' && url.protocol !== 'ws:') {
      throw new Error(`Signaling URL must use WebSocket: ${value}`)
    }
    return url.toString()
  }))]
}

function socketStatus (name, sockets) {
  const values = [...sockets.values()]
  return Object.freeze({
    name,
    open: values.filter(entry => entry.socket.readyState === 1).length,
    connecting: values.filter(entry => entry.socket.readyState === 0).length,
    total: values.length
  })
}

function nowSeconds () {
  return Math.floor(Date.now() / 1_000)
}

function randomHex (byteLength) {
  return toHex(crypto.getRandomValues(new Uint8Array(byteLength)))
}

function toHex (bytes) {
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
}

function fromHex (value, byteLength) {
  if (typeof value !== 'string' || value.length !== byteLength * 2 || !/^[0-9a-f]+$/.test(value)) {
    throw new Error(`Expected ${byteLength}-byte lowercase hex value`)
  }
  return Uint8Array.from({ length: byteLength }, (_, index) => Number.parseInt(value.slice(index * 2, index * 2 + 2), 16))
}

async function digest (algorithm, value) {
  return new Uint8Array(await crypto.subtle.digest(algorithm, encoder.encode(String(value))))
}

function errorMessage (value) {
  if (value instanceof Error) return value.message
  return String(value)
}
