import test from 'node:test'
import assert from 'node:assert/strict'
import { encode, rfc8949EncodeOptions } from 'cborg'
import { generateKeyPair, privateKeyFromProtobuf } from '@libp2p/crypto/keys'
import { peerIdFromPrivateKey } from '@libp2p/peer-id'
import {
  DEFAULT_STUN_URLS,
  NATIVE_WEBRTC_FRAME_CONTROL,
  NATIVE_WEBRTC_FRAME_DATA,
  PTY_FRAME_DATA,
  PTY_FRAME_RESIZE,
  PUBSUB_DISCOVERY_INTERVAL_MS,
  PUBSUB_DISCOVERY_TOPIC,
  PtyFrameDecoder,
  ROUTE_CAPABILITIES,
  SESSION_AUTH_FRAME_BYTES,
  WebRtcStream,
  authenticateClientStream,
  authenticateServerStream,
  assertPairingTokenUsable,
  base64UrlEncode,
  createPairingToken,
  browserDialableAddress,
  createRelayDialPlan,
  createSessionAuthAck,
  createSessionAuthHello,
  createWebRtcClientChallenge,
  createWebRtcActionHub,
  createNostrSignalingSession,
  createTorrentSignalingSession,
  decodePairingToken,
  decodeNativeWebRtcControl,
  decodeNativeWebRtcFrame,
  decodeWebRtcAuthResponse,
  decodeWebRtcAuthResponseV2,
  defaultRtcConfiguration,
  decodePtyResize,
  derivePairingKey,
  deriveRendezvousId,
  encodePtyData,
  encodePtyResize,
  encodeNativeWebRtcFrame,
  encodeWebRtcAuthResponse,
  encodeWebRtcAuthResponseV2,
  normalizeRelayAddress,
  openPairingPayload,
  pairingProviderCids,
  pairingRendezvousWindows,
  preferDialAddresses,
  protocolForService,
  rendezvousProviderCid,
  routeCapabilityMask,
  routeCapabilitiesFromMask,
  sealPairingPayload,
  signRouteRecord,
  signWebRtcAuthResponse,
  signWebRtcAuthResponseV2,
  verifyWebRtcAuthResponse,
  verifyWebRtcAuthResponseV2,
  verifyRouteRecord,
  verifySessionAuthAck,
  verifySessionAuthHello,
  webRtcAuthPayload,
  webRtcAuthPayloadV2,
  webRtcClientIdFromChallenge,
  webRtcRoomId,
  validateService
} from '../src/index.js'

const TEST_SERVER_PEER_ID = '12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9'

function hex (value) {
  return Array.from(value, byte => byte.toString(16).padStart(2, '0')).join('')
}

function unhex (value) {
  if (value.length % 2 !== 0) throw new Error('hex value must contain complete bytes')
  return Uint8Array.from(value.match(/../g) ?? [], byte => Number.parseInt(byte, 16))
}

class FakeSignalingWebSocket {
  static sockets = new Set()
  static sent = []

  constructor (url) {
    this.url = url
    this.readyState = 0
    this.subscriptionId = null
    this.filter = null
    this.infoHash = null
    this.peerId = null
    FakeSignalingWebSocket.sockets.add(this)
    queueMicrotask(() => {
      if (this.readyState !== 0) return
      this.readyState = 1
      this.onopen?.()
    })
  }

  send (raw) {
    FakeSignalingWebSocket.sent.push(String(raw))
    const message = JSON.parse(raw)
    if (Array.isArray(message)) {
      if (message[0] === 'REQ') {
        this.subscriptionId = message[1]
        this.filter = message[2]
      } else if (message[0] === 'EVENT') {
        for (const socket of FakeSignalingWebSocket.sockets) {
          if (socket.url !== this.url || socket.readyState !== 1 || socket.subscriptionId == null) continue
          const event = message[1]
          const topic = event.tags.find(tag => tag[0] === 't')?.[1]
          if (!socket.filter?.['#t']?.includes(topic)) continue
          queueMicrotask(() => socket.onmessage?.({
            data: JSON.stringify(['EVENT', socket.subscriptionId, event])
          }))
        }
      }
      return
    }

    if (message.action !== 'announce') return
    this.infoHash = message.info_hash
    this.peerId = message.peer_id
    for (const offer of message.offers ?? []) {
      for (const socket of FakeSignalingWebSocket.sockets) {
        if (
          socket === this ||
          socket.url !== this.url ||
          socket.readyState !== 1 ||
          socket.infoHash !== message.info_hash
        ) continue
        queueMicrotask(() => socket.onmessage?.({
          data: JSON.stringify({
            action: 'announce',
            info_hash: message.info_hash,
            peer_id: message.peer_id,
            offer_id: offer.offer_id,
            offer: offer.offer
          })
        }))
      }
    }
    if (message.answer != null) {
      for (const socket of FakeSignalingWebSocket.sockets) {
        if (
          socket.url !== this.url ||
          socket.readyState !== 1 ||
          socket.infoHash !== message.info_hash ||
          socket.peerId !== message.to_peer_id
        ) continue
        queueMicrotask(() => socket.onmessage?.({
          data: JSON.stringify({
            action: 'announce',
            info_hash: message.info_hash,
            peer_id: message.peer_id,
            offer_id: message.offer_id,
            answer: message.answer
          })
        }))
      }
    }
  }

  close () {
    if (this.readyState === 3) return
    this.readyState = 3
    FakeSignalingWebSocket.sockets.delete(this)
    queueMicrotask(() => this.onclose?.())
  }
}

test('PTY codec is browser-safe and preserves fragmented frames', () => {
  const data = encodePtyData(new TextEncoder().encode('hello'))
  const resize = encodePtyResize(132, 43)
  const combined = new Uint8Array(data.byteLength + resize.byteLength)
  combined.set(data)
  combined.set(resize, data.byteLength)

  const decoder = new PtyFrameDecoder()
  assert.deepEqual(decoder.push(combined.slice(0, 3)), [])
  const frames = decoder.push(combined.slice(3))
  decoder.finish()

  assert.equal(frames[0].type, PTY_FRAME_DATA)
  assert.equal(new TextDecoder().decode(frames[0].data), 'hello')
  assert.equal(frames[1].type, PTY_FRAME_RESIZE)
  assert.deepEqual(decodePtyResize(frames[1].data), { columns: 132, rows: 43 })
  assert.throws(() => new PtyFrameDecoder().push(Uint8Array.from([0, 0, 16, 0, 1])), /exceeds/)
})

test('native WebRTC wire codec preserves binary data and control messages', () => {
  const data = decodeNativeWebRtcFrame(encodeNativeWebRtcFrame(
    NATIVE_WEBRTC_FRAME_DATA,
    Uint8Array.from([0, 255, 42])
  ))
  const control = decodeNativeWebRtcFrame(encodeNativeWebRtcFrame(
    NATIVE_WEBRTC_FRAME_CONTROL,
    'ack:42'
  ))

  assert.equal(data.type, NATIVE_WEBRTC_FRAME_DATA)
  assert.deepEqual([...data.payload], [0, 255, 42])
  assert.equal(control.type, NATIVE_WEBRTC_FRAME_CONTROL)
  assert.equal(decodeNativeWebRtcControl(control.payload), 'ack:42')
  assert.throws(() => decodeNativeWebRtcFrame(Uint8Array.from([99, 0])), /Unsupported/)
})

test('native Nostr signaling exchanges signed room-scoped messages', async () => {
  const roomId = 'nostr-test-room'
  const url = 'wss://nostr.test/'
  const statuses = []
  const server = await createNostrSignalingSession({
    roomId,
    peerId: 'SERVER12345678901234',
    urls: [url],
    WebSocket: FakeSignalingWebSocket,
    onStatus: status => statuses.push(status)
  })
  const client = await createNostrSignalingSession({
    roomId,
    peerId: 'CLIENT12345678901234',
    urls: [url],
    WebSocket: FakeSignalingWebSocket
  })
  await Promise.all([server.ready, client.ready])
  assert.equal(server.trickleIce, true)
  assert.equal(client.trickleIce, true)

  const offer = nextSignal(server)
  await client.publish({ type: 'offer', sessionId: 'session-1234', sdp: 'offer-sdp' })
  const signal = await offer.catch(error => {
    throw new Error(`${error.message}: ${JSON.stringify(statuses)}`, { cause: error })
  })
  assert.deepEqual(signal, {
    version: 2,
    room: server.topic,
    type: 'offer',
    sessionId: 'session-1234',
    from: client.peerId,
    createdAt: signal.createdAt,
    sdp: 'offer-sdp'
  })

  await Promise.all([server.close(), client.close()])
})

function nextSignal (session, timeoutMs = 2_000) {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      unsubscribe()
      reject(new Error(`${session.name} signal timeout`))
    }, timeoutMs)
    const unsubscribe = session.subscribe(message => {
      clearTimeout(timeout)
      unsubscribe()
      resolve(message)
    })
  })
}

test('native BitTorrent signaling routes offer and addressed answer', async () => {
  const roomId = 'torrent-test-room'
  const url = 'wss://tracker.test/'
  const server = await createTorrentSignalingSession({
    roomId,
    peerId: 'SERVER12345678901234',
    urls: [url],
    WebSocket: FakeSignalingWebSocket
  })
  const client = await createTorrentSignalingSession({
    roomId,
    peerId: 'CLIENT12345678901234',
    urls: [url],
    WebSocket: FakeSignalingWebSocket
  })
  await Promise.all([server.ready, client.ready])
  assert.equal(server.trickleIce, false)
  assert.equal(client.trickleIce, false)

  const receivedOffer = nextSignal(server)
  await client.publish({ type: 'offer', sessionId: 'session-5678', sdp: 'offer-sdp' })
  const offer = await receivedOffer
  assert.equal(offer.from, client.peerId)
  assert.equal(offer.sdp, 'offer-sdp')

  const receivedAnswer = nextSignal(client)
  await server.publish({
    type: 'answer',
    sessionId: offer.sessionId,
    to: offer.from,
    sdp: 'answer-sdp'
  })
  const answer = await receivedAnswer
  assert.equal(answer.from, server.peerId)
  assert.equal(answer.sdp, 'answer-sdp')

  await Promise.all([server.close(), client.close()])
})

test('pairing token encrypts SDP on native signaling relays and trackers', async () => {
  const pairingToken = createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret: new Uint8Array(32).fill(23)
  })

  for (const [createSession, url] of [
    [createNostrSignalingSession, 'wss://private-nostr.test/'],
    [createTorrentSignalingSession, 'wss://private-tracker.test/']
  ]) {
    FakeSignalingWebSocket.sent.length = 0
    const server = await createSession({
      roomId: 'public-peer-id-room-is-ignored',
      peerId: 'SERVER12345678901234',
      urls: [url],
      WebSocket: FakeSignalingWebSocket,
      pairingToken
    })
    const client = await createSession({
      roomId: 'public-peer-id-room-is-ignored',
      peerId: 'CLIENT12345678901234',
      urls: [url],
      WebSocket: FakeSignalingWebSocket,
      pairingToken
    })
    await Promise.all([server.ready, client.ready])

    const received = nextSignal(server)
    await client.publish({
      type: 'offer',
      sessionId: 'private-session',
      sdp: 'private-sdp-with-candidate-203.0.113.9'
    })
    assert.equal((await received).sdp, 'private-sdp-with-candidate-203.0.113.9')
    assert.equal(FakeSignalingWebSocket.sent.some(raw => raw.includes('private-sdp')), false)

    await Promise.all([server.close(), client.close()])
  }
})

test('shared networking uses one PubSub topic and the supplied STUN pool', () => {
  assert.equal(PUBSUB_DISCOVERY_TOPIC, 'io.github.santaklouse.p2p-netcat.peer-discovery.v1')
  assert.equal(PUBSUB_DISCOVERY_INTERVAL_MS, 10_000)
  assert.deepEqual(DEFAULT_STUN_URLS, [
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

  const first = defaultRtcConfiguration()
  const second = defaultRtcConfiguration()
  assert.deepEqual(first.iceServers[0].urls, DEFAULT_STUN_URLS)
  assert.notEqual(first.iceServers[0].urls, second.iceServers[0].urls)
})

test('pairing token uses deterministic CBOR and validates its scope', () => {
  const secret = Uint8Array.from({ length: 32 }, (_, index) => index)
  const encoded = createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret,
    relayHints: [
      '/dns4/relay-b.example/tcp/443/wss/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW',
      '/dns4/relay-a.example/tcp/443/wss/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW'
    ],
    expiresAt: 2_000_000_000
  })
  const token = decodePairingToken(encoded)

  assert.ok(encoded.startsWith('pnc1_'))
  assert.equal(token.peerId, TEST_SERVER_PEER_ID)
  assert.equal(token.service, 31337)
  assert.deepEqual([...token.secret], [...secret])
  assert.equal(token.relayHints[0].includes('relay-a.example'), true)
  assert.equal(assertPairingTokenUsable(encoded, {
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    nowSeconds: 1_900_000_000
  }).peerId, TEST_SERVER_PEER_ID)
  assert.throws(() => assertPairingTokenUsable(encoded, {
    service: 31338,
    nowSeconds: 1_900_000_000
  }), /logical port 31337/)

  const nonCanonical = encode(new Map([
    [0, 1],
    [1, TEST_SERVER_PEER_ID],
    [2, 31337],
    [3, secret],
    [4, []]
  ]), {
    ...rfc8949EncodeOptions,
    mapSorter: (left, right) => -rfc8949EncodeOptions.mapSorter(left, right)
  })
  assert.throws(
    () => decodePairingToken(`pnc1_${base64UrlEncode(nonCanonical)}`),
    /not in RFC 8949 deterministic form/
  )
  assert.throws(() => createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret,
    relayHints: ['/dns4/relay.example/tcp/443/wss']
  }), /relay PeerId is missing/)
})

test('rotating rendezvous and provider CIDs are deterministic across time windows', async () => {
  const token = createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret: new Uint8Array(32).fill(7)
  })
  const id = await deriveRendezvousId(token, { purpose: 'dht', epoch: 12345 })
  const repeated = await deriveRendezvousId(token, { purpose: 'dht', epoch: 12345 })
  const windows = await pairingRendezvousWindows(token, {
    purpose: 'dht',
    nowMs: 12345 * 300 * 1000,
    intervalSeconds: 300
  })
  const cids = await pairingProviderCids(token, {
    nowMs: 12345 * 300 * 1000,
    intervalSeconds: 300
  })

  assert.equal(id, repeated)
  assert.match(id, /^[A-Za-z0-9_-]{43}$/)
  assert.deepEqual(windows.map(window => window.epoch), [12344, 12345, 12346])
  assert.equal(windows[1].id, id)
  assert.equal(cids.length, 3)
  assert.ok(cids.every(cid => cid.version === 1))
})

test('pairing wire format matches the language-neutral interoperability vector', async () => {
  const token = createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret: Uint8Array.from({ length: 32 }, (_, index) => index),
    relayHints: [],
    expiresAt: 2_000_000_000
  })
  assert.equal(
    token,
    'pnc1_pgABAXg0MTJEM0tvb1dRM3V4cEhnakRLRTZ2R212ektTOFJQYnhVREx3SjdYQ0xhRDZZWGRVZmJSOQIZemkDWCAAAQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHwSABRp3NZQA'
  )
  assert.equal(
    hex(await derivePairingKey(token, 'rendezvous')),
    'e8d5fc0873810ff06039af654896909c86521e878d5970c3f8b3fed58df0385f'
  )
  assert.equal(
    hex(await derivePairingKey(token, 'signaling')),
    'f4c7b6f69d0024bdfec6c7c017843977f3adb728bb1f398b09e222031d19abeb'
  )
  assert.equal(
    hex(await derivePairingKey(token, 'admission')),
    '976b46fe450808bae0694e793fc9db6de10107ffa58b7b0cbfa8e86cb94a3b57'
  )
  assert.equal(
    hex(await derivePairingKey(token, 'route-record')),
    '7fe47fb8573ec1e37980a3621d29cf2a7f8f600ab41663821a87280b15070fd4'
  )

  const rendezvous = await deriveRendezvousId(token, { purpose: 'dht', epoch: 12345 })
  assert.equal(rendezvous, '9mtMRyxbxPkVQlj7WJW9oCXuVBlgtkxzj9z0F0H8gW0')
  assert.equal(
    (await rendezvousProviderCid(rendezvous)).toString(),
    'bafkreihzwnotx7weylzypbxqzsogwwec44rq6ujft6ewn4lh3jjdscylgi'
  )

  const envelope = await sealPairingPayload(
    token,
    'signaling',
    new TextEncoder().encode('hello'),
    {
      additionalData: new TextEncoder().encode('vector-aad'),
      nonce: Uint8Array.from({ length: 12 }, (_, index) => index)
    }
  )
  assert.equal(
    hex(envelope),
    'a30001014c000102030405060708090a0b02556e5682c115e1b4ce0fe7930b863d097a7734a2b530'
  )

  const hello = await createSessionAuthHello(token, {
    nowSeconds: 1_700_000_000,
    nonce: Uint8Array.from({ length: 16 }, (_, index) => index)
  })
  assert.equal(
    hex(hello.frame),
    '504e43410101000000006553f100000102030405060708090a0b0c0d0e0ffa6363937e457a4bad2b60a5d0ab571b842cd30db93d77d613aca8a0208b5e23'
  )
  assert.equal(
    hex(await createSessionAuthAck(token, hello, {
      nonce: Uint8Array.from({ length: 16 }, (_, index) => 0xf0 + index)
    })),
    '504e43410102000000006553f100f0f1f2f3f4f5f6f7f8f9fafbfcfdfeffc0c3fb8250fd6fae4e1e58520ed5048f7da7dec31f0bf355c1d0c4e8c3910b61'
  )
})

test('pairing AEAD hides payloads and rejects altered context', async () => {
  const token = createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret: new Uint8Array(32).fill(11)
  })
  const plaintext = new TextEncoder().encode('private SDP candidate 203.0.113.9')
  const additionalData = new TextEncoder().encode('offer/session-1')
  const envelope = await sealPairingPayload(token, 'signaling', plaintext, {
    additionalData,
    nonce: new Uint8Array(12).fill(3)
  })
  const opened = await openPairingPayload(token, 'signaling', envelope, { additionalData })

  assert.deepEqual(opened, plaintext)
  assert.equal(new TextDecoder().decode(envelope).includes('203.0.113.9'), false)
  await assert.rejects(
    openPairingPayload(token, 'signaling', envelope, {
      additionalData: new TextEncoder().encode('answer/session-1')
    }),
    /authentication failed/
  )
})

test('pairing session authentication is mutual and rejects a different secret', async () => {
  const token = createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret: new Uint8Array(32).fill(17)
  })
  const otherToken = createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret: new Uint8Array(32).fill(18)
  })
  const client = await createSessionAuthHello(token, {
    nowSeconds: 1_900_000_000,
    nonce: new Uint8Array(16).fill(1)
  })
  assert.equal(client.frame.byteLength, SESSION_AUTH_FRAME_BYTES)
  const hello = await verifySessionAuthHello(token, client.frame, {
    nowSeconds: 1_900_000_010
  })
  const ack = await createSessionAuthAck(token, hello, {
    nonce: new Uint8Array(16).fill(2)
  })

  await verifySessionAuthAck(token, client, ack)
  await assert.rejects(
    verifySessionAuthHello(otherToken, client.frame, { nowSeconds: 1_900_000_010 }),
    /authentication failed/
  )
})

test('authenticated stream removes admission frames before application data', async () => {
  const token = createPairingToken({
    peerId: TEST_SERVER_PEER_ID,
    service: 31337,
    secret: new Uint8Array(32).fill(19)
  })
  let clientTransport
  let serverTransport
  clientTransport = new WebRtcStream({
    keepAliveIntervalMs: 0,
    sendData: bytes => serverTransport.receiveData(bytes),
    sendControl: control => serverTransport.receiveControl(control)
  })
  serverTransport = new WebRtcStream({
    keepAliveIntervalMs: 0,
    sendData: bytes => clientTransport.receiveData(bytes),
    sendControl: control => clientTransport.receiveControl(control)
  })
  await new Promise(resolve => setImmediate(resolve))

  const [client, server] = await Promise.all([
    authenticateClientStream(clientTransport, token),
    authenticateServerStream(serverTransport, token)
  ])
  client.send(new TextEncoder().encode('application-data'))
  await client.onDrain()
  const received = await server[Symbol.asyncIterator]().next()

  assert.equal(new TextDecoder().decode(received.value), 'application-data')
  await Promise.all([client.close(), server.close()])
})

test('signed route records bind addresses and services to the exact PeerId', async () => {
  const privateKey = await generateKeyPair('Ed25519')
  const peerId = peerIdFromPrivateKey(privateKey).toString()
  const capabilities = routeCapabilityMask({
    quic: true,
    webrtc: true,
    relay: true
  })
  const signed = await signRouteRecord(privateKey, {
    sequence: 42,
    issuedAt: 1_900_000_000,
    expiresAt: 1_900_000_180,
    services: [31337, 8080],
    addresses: ['/ip4/203.0.113.7/udp/4001/quic-v1'],
    relayReservations: [
      `/dns4/relay.example/tcp/443/wss/p2p/${TEST_SERVER_PEER_ID}/p2p-circuit`
    ],
    capabilities
  })
  const record = await verifyRouteRecord(signed, {
    expectedPeerId: peerId,
    expectedService: 31337,
    nowSeconds: 1_900_000_030
  })

  assert.equal(record.peerId, peerId)
  assert.equal(record.sequence, 42)
  assert.deepEqual(routeCapabilitiesFromMask(record.capabilities), {
    tcp: false,
    quic: true,
    ws: false,
    wss: false,
    webtransport: false,
    webrtc: true,
    relay: true
  })
  await assert.rejects(
    verifyRouteRecord(signed, {
      expectedPeerId: TEST_SERVER_PEER_ID,
      nowSeconds: 1_900_000_030
    }),
    /belongs to PeerId/
  )
  const altered = signed.slice()
  altered[altered.length - 1] ^= 1
  await assert.rejects(verifyRouteRecord(altered, {
    nowSeconds: 1_900_000_030
  }))
  assert.equal((capabilities & ROUTE_CAPABILITIES.webrtc) !== 0, true)
})

test('shared library validates the logical port and protocol id', () => {
  assert.equal(validateService('8080'), 8080)
  assert.equal(protocolForService(8080), '/p2p-netcat/1.0.0/8080')
  assert.throws(() => validateService(0), /between 1 and 65535/)
  assert.throws(() => validateService(65536), /between 1 and 65535/)
})

test('shared library builds an immutable relay dial plan', async () => {
  const relayId = '12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW'
  const targetId = '12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9'
  const relay = `/ip4/127.0.0.1/tcp/9091/ws/p2p/${relayId}`
  const plan = createRelayDialPlan({
    peerId: targetId,
    service: 31337,
    relay,
    requireWebSocket: true
  })

  assert.equal(plan.protocol, '/p2p-netcat/1.0.0/31337')
  assert.equal(plan.destination, `${relay}/p2p-circuit/p2p/${targetId}`)
  assert.ok(Object.isFrozen(plan))
})

test('shared library applies the browser security policy to relays', async () => {
  const relayId = '12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW'
  const ws = `/dns4/relay.example/tcp/443/ws/p2p/${relayId}`
  const wss = `/dns4/relay.example/tcp/443/wss/p2p/${relayId}`

  assert.throws(() => normalizeRelayAddress(ws, { secureContext: true }), /secure \/wss/)
  assert.equal(normalizeRelayAddress(wss, { requireWebSocket: true, secureContext: true }), wss)
  assert.equal(browserDialableAddress(wss, { secureContext: true }), true)
  assert.equal(browserDialableAddress('/ip4/127.0.0.1/tcp/9090', { secureContext: true }), false)
})

test('shared ordering prefers WebRTC and QUIC and leaves relay last', () => {
  assert.ok(preferDialAddresses('/ip4/127.0.0.1/udp/1/webrtc-direct', '/ip4/127.0.0.1/udp/1/quic-v1') < 0)
  assert.ok(preferDialAddresses('/ip4/127.0.0.1/udp/1/quic-v1', '/ip4/127.0.0.1/tcp/1') < 0)
  assert.ok(preferDialAddresses('/ip4/127.0.0.1/tcp/1', '/ip4/127.0.0.1/tcp/2/ws/p2p/relay/p2p-circuit') < 0)
})

test('WebRTC room and authentication frame are deterministic', async () => {
  const targetId = '12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9'
  const challenge = new Uint8Array(32).fill(7)
  assert.equal(webRtcRoomId(targetId, 31337), `${targetId}:31337`)
  assert.ok(webRtcAuthPayload(targetId, 31337, challenge).byteLength > challenge.byteLength)
  const encoded = encodeWebRtcAuthResponse(new Uint8Array([1, 2, 3]), new Uint8Array([4, 5]))
  const decoded = decodeWebRtcAuthResponse(encoded)
  assert.deepEqual([...decoded.publicKey], [1, 2, 3])
  assert.deepEqual([...decoded.signature], [4, 5])
})

test('core signs and verifies a native WebRTC challenge for the exact PeerId', async () => {
  const privateKey = await generateKeyPair('Ed25519')
  const peerId = peerIdFromPrivateKey(privateKey).toString()
  const challenge = crypto.getRandomValues(new Uint8Array(32))
  const response = await signWebRtcAuthResponse(privateKey, 31337, challenge)

  assert.equal(await verifyWebRtcAuthResponse(response, peerId, 31337, challenge), true)
  assert.equal(await verifyWebRtcAuthResponse(response, peerId, 31338, challenge), false)
})

test('native WebRTC v2 authentication is bound to the exact SDP transcript', async () => {
  const privateKey = await generateKeyPair('Ed25519')
  const peerId = peerIdFromPrivateKey(privateKey).toString()
  const challenge = new Uint8Array(32).fill(0x41)
  const transcript = Object.freeze({
    sessionId: '0123456789abcdef0123456789abcdef',
    offerSdp: 'v=0\r\na=fingerprint:sha-256 OFFER\r\n',
    answerSdp: 'v=0\r\na=fingerprint:sha-256 ANSWER\r\n'
  })
  const payload = await webRtcAuthPayloadV2(peerId, 31337, challenge, transcript)
  assert.ok(payload.byteLength > challenge.byteLength)
  const response = await signWebRtcAuthResponseV2(privateKey, 31337, challenge, transcript)
  assert.equal(decodeWebRtcAuthResponseV2(response).signature.byteLength > 0, true)
  assert.equal(await verifyWebRtcAuthResponseV2(response, peerId, 31337, challenge, transcript), true)

  const mutations = [
    { ...transcript, sessionId: 'fedcba9876543210fedcba9876543210' },
    { ...transcript, offerSdp: `${transcript.offerSdp}a=x:changed\r\n` },
    { ...transcript, answerSdp: `${transcript.answerSdp}a=x:changed\r\n` }
  ]
  for (const changed of mutations) {
    assert.equal(await verifyWebRtcAuthResponseV2(response, peerId, 31337, challenge, changed), false)
  }
  assert.equal(await verifyWebRtcAuthResponseV2(response, peerId, 31338, challenge, transcript), false)

  const legacy = await signWebRtcAuthResponse(privateKey, 31337, challenge)
  await assert.rejects(
    verifyWebRtcAuthResponseV2(legacy, peerId, 31337, challenge, transcript),
    /Unsupported WebRTC authentication response/
  )
})

test('native WebRTC v2 transcript matches the Go compatibility vector', async () => {
  const challenge = new Uint8Array(32).fill(0x41)
  const transcript = {
    sessionId: '0123456789abcdef0123456789abcdef',
    offerSdp: 'v=0\r\na=fingerprint:sha-256 OFFER\r\n',
    answerSdp: 'v=0\r\na=fingerprint:sha-256 ANSWER\r\n'
  }
  const payload = await webRtcAuthPayloadV2(TEST_SERVER_PEER_ID, 31337, challenge, transcript)
  const expected = '000000207032702d6e65746361742f6e61746976652d7765627274632d617574682f7632' +
    '000000010200000006636c69656e740000000673657276657200000034313244334b6f6f5751337578704867' +
    '6a444b453676476d767a4b53385250627855444c774a3758434c6144365958645566625239000000027a69' +
    '0000002030313233343536373839616263646566303132333435363738396162636465660000002041414141' +
    '41414141414141414141414141414141414141414141414141414141000000200f117532f93ee2cecff98bd8' +
    '2f00fd4aa2e0ba73241e0eadd9f6b5c3dec6d0e200000020105188d110c9603ce7cba3d3668933c4014cbdc5' +
    'ad0d8a8a00295b97b4fc060b'
  assert.equal(hex(payload), expected)
})

test('native WebRTC v2 response matches the Go Ed25519 compatibility vector', async () => {
  const privateKey = privateKeyFromProtobuf(unhex(
    '080112401111111111111111111111111111111111111111111111111111111111111111' +
    'd04ab232742bb4ab3a1368bd4615e4e6d0224ab71a016baf8520a332c9778737'
  ))
  const peerId = '12D3KooWPqT2nMDSiXUSx5D7fasaxhxKigVhcqfkKqrLghCq9jxz'
  const challenge = new Uint8Array(32).fill(0x41)
  const transcript = {
    sessionId: '0123456789abcdef0123456789abcdef',
    offerSdp: 'v=0\r\na=fingerprint:sha-256 OFFER\r\n',
    answerSdp: 'v=0\r\na=fingerprint:sha-256 ANSWER\r\n'
  }
  const response = await signWebRtcAuthResponseV2(privateKey, 31337, challenge, transcript)
  const expected = '020024004008011220d04ab232742bb4ab3a1368bd4615e4e6d0224ab71a016baf8520a332c9778737' +
    'cc75656ed3c03a6dfa36f74183414d2a463301f80f50e8e56382d207ae879cb20ade38f766f5c94a69c007835633a0a475cb7e1d82069a70483523ceb26c7f0f'
  assert.equal(hex(response), expected)
  assert.equal(await verifyWebRtcAuthResponseV2(unhex(expected), peerId, 31337, challenge, transcript), true)
})

test('native WebRTC auth response codecs reject version downgrade', () => {
  const key = new Uint8Array([1, 2, 3])
  const signature = new Uint8Array([4, 5])
  const v2 = encodeWebRtcAuthResponseV2(key, signature)
  assert.equal(v2[0], 2)
  assert.deepEqual([...decodeWebRtcAuthResponseV2(v2).publicKey], [...key])
  assert.throws(() => decodeWebRtcAuthResponse(v2), /Unsupported WebRTC authentication response/)
})

test('native WebRTC attempts share one client-session identity', () => {
  const clientId = 'ClientSession1234567'
  const challenge = createWebRtcClientChallenge(clientId)

  assert.equal(challenge.byteLength, 32)
  assert.equal(webRtcClientIdFromChallenge(challenge), clientId)
  assert.equal(webRtcClientIdFromChallenge(new Uint8Array(31)), null)
})

test('WebRTC action hub owns action mapping and reconnects the same stream', async () => {
  let leaveCount = 0
  const room = {
    makeAction () {
      return {
        async send () {},
        onMessage: null
      }
    },
    async leave () {
      leaveCount += 1
    },
    onPeerJoin: null,
    onPeerLeave: null
  }
  const opened = []
  const reconnected = []
  const hub = createWebRtcActionHub(room, {
    reconnectGraceMs: 1_000,
    onStream: (stream, peerId) => opened.push({ stream, peerId }),
    onPeerReconnected: peerId => reconnected.push(peerId)
  })

  room.onPeerJoin('remote-1')
  const stream = opened[0].stream
  room.onPeerLeave('remote-1')
  room.onPeerJoin('remote-1')

  assert.equal(opened.length, 1)
  assert.equal(stream.connectionStatus, 'connected')
  assert.deepEqual(reconnected, ['remote-1'])

  await hub.close()
  assert.equal(leaveCount, 1)
  assert.equal(stream.status, 'closed')
})

test('WebRTC stream preserves ordering, backpressure, and EOF', async () => {
  const sent = []
  const controls = []
  const stream = new WebRtcStream({
    sendData: async bytes => sent.push([...bytes]),
    sendControl: async control => controls.push(control),
    keepAliveIntervalMs: 0
  })
  assert.equal(stream.send(new Uint8Array([1, 2])), false)
  await stream.onDrain()
  assert.deepEqual(sent, [[1, 2]])
  stream.receiveData(new Uint8Array([3, 4]))
  stream.receiveControl('eof')
  const received = []
  for await (const bytes of stream) received.push([...bytes])
  assert.deepEqual(received, [[3, 4]])
  await stream.close()
  assert.deepEqual(controls, ['flow:1', 'ack:2', 'eof'])
  assert.equal(stream.status, 'closed')
})

test('WebRTC stream limits unacknowledged output until the consumer advances', async () => {
  let sender
  let receiver
  sender = new WebRtcStream({
    flowWindowBytes: 4,
    keepAliveIntervalMs: 0,
    sendData: bytes => receiver.receiveData(bytes),
    sendControl: control => receiver.receiveControl(control)
  })
  receiver = new WebRtcStream({
    flowWindowBytes: 4,
    keepAliveIntervalMs: 0,
    sendData: bytes => sender.receiveData(bytes),
    sendControl: control => sender.receiveControl(control)
  })
  await new Promise(resolve => setImmediate(resolve))

  sender.send(Uint8Array.from([1, 2, 3, 4]))
  await sender.onDrain()
  sender.send(Uint8Array.from([5]))
  const blockedDrain = sender.onDrain()
  let drained = false
  void blockedDrain.then(() => {
    drained = true
  })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(drained, false)

  const iterator = receiver[Symbol.asyncIterator]()
  assert.deepEqual(await iterator.next(), {
    value: Uint8Array.from([1, 2, 3, 4]),
    done: false
  })
  const second = iterator.next()
  assert.deepEqual(await second, {
    value: Uint8Array.from([5]),
    done: false
  })
  await blockedDrain

  const completed = iterator.next()
  await sender.close()
  assert.deepEqual(await completed, { value: undefined, done: true })
  await receiver.close()
})

test('WebRTC stream rejects an oversized receive queue', async () => {
  const controls = []
  const stream = new WebRtcStream({
    maxReadQueueBytes: 4,
    keepAliveIntervalMs: 0,
    sendData: async () => {},
    sendControl: async control => controls.push(control)
  })
  stream.receiveData(Uint8Array.from([1, 2, 3, 4]))
  stream.receiveData(Uint8Array.from([5]))
  await new Promise(resolve => setImmediate(resolve))

  assert.equal(stream.status, 'closed')
  assert.ok(controls.includes('abort'))
  assert.deepEqual(await stream[Symbol.asyncIterator]().next(), {
    value: undefined,
    done: true
  })
})

test('WebRTC stream preserves queued data while the peer reconnects', async () => {
  const sent = []
  const controls = []
  const stream = new WebRtcStream({
    keepAliveIntervalMs: 0,
    sendData: async bytes => sent.push([...bytes]),
    sendControl: async control => controls.push(control)
  })
  await new Promise(resolve => setImmediate(resolve))

  stream.peerDisconnected(1_000)
  assert.equal(stream.connectionStatus, 'reconnecting')
  stream.send(Uint8Array.from([7, 8, 9]))
  const drain = stream.onDrain()
  let drained = false
  void drain.then(() => {
    drained = true
  })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(drained, false)
  assert.deepEqual(sent, [])

  assert.equal(stream.peerReconnected(), true)
  await drain
  assert.equal(stream.connectionStatus, 'connected')
  assert.deepEqual(sent, [[7, 8, 9]])
  assert.ok(controls.includes('resume'))
  await stream.close()
})

test('WebRTC stream closes only after the reconnect grace period expires', async () => {
  const stream = new WebRtcStream({
    keepAliveIntervalMs: 0,
    sendData: async () => {},
    sendControl: async () => {}
  })

  stream.peerDisconnected(20)
  assert.equal(stream.status, 'open')
  assert.equal(stream.connectionStatus, 'reconnecting')
  await new Promise(resolve => setTimeout(resolve, 30))
  assert.equal(stream.status, 'closed')
  assert.equal(stream.connectionStatus, 'disconnected')
})

test('WebRTC stream retries a write that raced with peer disconnection', async () => {
  let attempts = 0
  const sent = []
  const stream = new WebRtcStream({
    keepAliveIntervalMs: 0,
    sendData: async bytes => {
      attempts += 1
      if (attempts === 1) throw new Error('data channel closed during send')
      sent.push([...bytes])
    },
    sendControl: async () => {}
  })

  stream.send(Uint8Array.from([4, 2]))
  const drain = stream.onDrain()
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(stream.connectionStatus, 'reconnecting')
  assert.deepEqual(sent, [])

  stream.peerReconnected()
  await drain
  assert.equal(attempts, 2)
  assert.deepEqual(sent, [[4, 2]])
  await stream.close()
})
