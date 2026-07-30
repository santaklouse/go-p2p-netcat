import {
  NATIVE_WEBRTC_FRAME_AUTH_CHALLENGE,
  NATIVE_WEBRTC_FRAME_AUTH_READY,
  NATIVE_WEBRTC_FRAME_AUTH_RESPONSE,
  NATIVE_WEBRTC_FRAME_CONTROL,
  NATIVE_WEBRTC_FRAME_DATA,
  NativeWebRtcPeer,
  decodeNativeWebRtcControl
} from './native-webrtc.js'
import { createSignalingSessionId } from './signaling.js'
import { WEBRTC_RECONNECT_GRACE_MS, WebRtcStream } from './index.js'

const INITIAL_ATTEMPT_TIMEOUT_MS = 20_000
const RETRY_DELAY_MS = 750
const MAX_LISTENER_ATTEMPTS = 64
const MAX_LISTENER_ATTEMPTS_PER_PEER = 4
const MAX_PENDING_CANDIDATE_SESSIONS = 128
const MAX_PENDING_CANDIDATES_PER_ATTEMPT = 32

export function startNativeWebRtcListener ({
  signalingSessions,
  RTCPeerConnection,
  rtcConfig,
  createAuthResponse,
  onStream = () => {},
  onStreamClosed = () => {},
  onPeerDisconnected = () => {},
  onPeerReconnected = () => {},
  onState = () => {},
  onLog = () => {},
  reconnectGraceMs = WEBRTC_RECONNECT_GRACE_MS
}) {
  validateSessions(signalingSessions)
  if (typeof createAuthResponse !== 'function') throw new TypeError('createAuthResponse callback is required')

  const streams = new Map()
  const attempts = new Map()
  const pendingCandidates = new Map()
  const unsubscribe = []
  let closed = false

  const forgetPendingCandidates = key => {
    const pending = pendingCandidates.get(key)
    if (pending == null) return []
    clearTimeout(pending.timer)
    pendingCandidates.delete(key)
    return pending.messages
  }

  const queuePendingCandidate = (key, message) => {
    let pending = pendingCandidates.get(key)
    if (pending == null) {
      if (pendingCandidates.size >= MAX_PENDING_CANDIDATE_SESSIONS) {
        const oldestKey = pendingCandidates.keys().next().value
        if (oldestKey != null) forgetPendingCandidates(oldestKey)
      }
      const timer = setTimeout(() => forgetPendingCandidates(key), INITIAL_ATTEMPT_TIMEOUT_MS)
      timer.unref?.()
      pending = { messages: [], timer }
      pendingCandidates.set(key, pending)
    }
    if (pending.messages.length < MAX_PENDING_CANDIDATES_PER_ATTEMPT) {
      pending.messages.push(message)
    }
  }

  const receiveSignal = (session, message) => {
    if (closed) return
    if (message.type === 'offer') {
      void answerOffer(session, message).catch(error => {
        log(onLog, `${session.name}: offer ${message.sessionId} rejected: ${errorMessage(error)}`)
      })
      return
    }
    if (message.type === 'candidate') {
      const key = attemptKey(session, message.from, message.sessionId)
      const attempt = attempts.get(key)
      if (attempt != null) {
        void attempt.peer.signal(message).catch(error => attempt.peer.close(asError(error)))
      } else if (session.trickleIce === true) {
        queuePendingCandidate(key, message)
      }
      return
    }
    if (message.type === 'bye') {
      const key = attemptKey(session, message.from, message.sessionId)
      forgetPendingCandidates(key)
      const attempt = attempts.get(key)
      attempt?.peer.close(new Error('Remote native WebRTC attempt was cancelled'))
    }
  }

  const answerOffer = async (session, offer) => {
    const key = attemptKey(session, offer.from, offer.sessionId)
    if (attempts.has(key)) return
    if (attempts.size >= MAX_LISTENER_ATTEMPTS) {
      throw new Error('Native WebRTC listener is at its pending connection limit')
    }
    const attemptsForPeer = [...attempts.values()]
      .filter(attempt => attempt.remoteId === offer.from)
      .length
    if (attemptsForPeer >= MAX_LISTENER_ATTEMPTS_PER_PEER) {
      throw new Error('Native WebRTC peer has too many pending connections')
    }

    let challengeAnswered = false
    let authenticated = false
    let attemptTimer
    let peer
    const cleanupAttempt = () => {
      clearTimeout(attemptTimer)
      forgetPendingCandidates(key)
      const current = attempts.get(key)
      if (current?.peer === peer) attempts.delete(key)
    }
    const handleLost = error => {
      cleanupAttempt()
      const entry = streams.get(offer.from)
      if (entry == null || entry.link.peer !== peer || entry.stream.status === 'closed') return
      entry.link.peer = null
      entry.stream.peerDisconnected(reconnectGraceMs)
      onPeerDisconnected(offer.from, entry.stream, error)
    }

    peer = new NativeWebRtcPeer({
      RTCPeerConnection,
      initiator: false,
      rtcConfig,
      trickleIce: session.trickleIce === true,
      onSignal: signal => session.publish({
        ...signal,
        sessionId: offer.sessionId,
        to: offer.from
      }),
      onFrame: async frame => {
        if (frame.type === NATIVE_WEBRTC_FRAME_AUTH_CHALLENGE) {
          const response = await createAuthResponse(frame.payload)
          await peer.sendFrame(NATIVE_WEBRTC_FRAME_AUTH_RESPONSE, response)
          challengeAnswered = true
          return
        }
        if (frame.type === NATIVE_WEBRTC_FRAME_AUTH_READY) {
          if (!challengeAnswered) throw new Error('Client confirmed authentication before receiving a response')
          if (!authenticated) {
            authenticated = true
            clearTimeout(attemptTimer)
            activateStream(offer.from, session, peer)
          }
          return
        }
        if (!authenticated) throw new Error('Received user data before PeerId authentication')
        const entry = streams.get(offer.from)
        if (entry == null || entry.link.peer !== peer) return
        if (frame.type === NATIVE_WEBRTC_FRAME_DATA) entry.stream.receiveData(frame.payload)
        else if (frame.type === NATIVE_WEBRTC_FRAME_CONTROL) {
          entry.stream.receiveControl(decodeNativeWebRtcControl(frame.payload))
        }
      },
      onClose: handleLost,
      onError: error => log(onLog, `${session.name}: ${error.message}`),
      onState: state => onState(session.name, offer.from, state)
    })

    attempts.set(key, { peer, remoteId: offer.from })
    attemptTimer = setTimeout(() => {
      peer.close(new Error('Native WebRTC listener attempt timed out'))
    }, INITIAL_ATTEMPT_TIMEOUT_MS)
    attemptTimer.unref?.()
    try {
      await peer.signal(offer)
      for (const candidate of forgetPendingCandidates(key)) {
        await peer.signal(candidate)
      }
    } catch (error) {
      peer.close(asError(error))
      throw error
    }
  }

  const activateStream = (remoteId, session, peer) => {
    const existing = streams.get(remoteId)
    if (existing != null) {
      if (existing.stream.status === 'closed') {
        streams.delete(remoteId)
      } else if (existing.link.peer != null && existing.link.peer !== peer) {
        peer.close(new Error('A native WebRTC connection is already active for this client'))
        return
      } else {
        existing.link.peer = peer
        existing.session = session
        if (existing.stream.peerReconnected()) {
          setSignalingStrategy(existing.stream, session.name)
          onPeerReconnected(remoteId, existing.stream, session.name)
        }
        return
      }
    }

    const link = { peer }
    let stream
    stream = new WebRtcStream({
      sendData: bytes => sendThroughLink(link, NATIVE_WEBRTC_FRAME_DATA, bytes),
      sendControl: control => sendThroughLink(link, NATIVE_WEBRTC_FRAME_CONTROL, control),
      onFinalize: () => {
        const current = streams.get(remoteId)
        if (current?.stream === stream) streams.delete(remoteId)
        const finalizedPeer = link.peer
        link.peer = null
        deferPeerClose(finalizedPeer, new Error('Native WebRTC stream finalized'))
        onStreamClosed(remoteId, stream)
      }
    })
    setSignalingStrategy(stream, session.name)
    streams.set(remoteId, { stream, link, session })
    onStream(stream, remoteId, session.name)
  }

  for (const session of signalingSessions) {
    unsubscribe.push(session.subscribe(message => receiveSignal(session, message)))
  }

  return {
    async close () {
      if (closed) return
      closed = true
      for (const stop of unsubscribe.splice(0)) stop()
      const closingFrames = [...streams.values()].map(entry => (
        entry.stream.status === 'open' && entry.link.peer != null
          ? entry.link.peer.sendFrame(NATIVE_WEBRTC_FRAME_CONTROL, 'abort')
          : Promise.resolve()
      ))
      await Promise.allSettled(closingFrames)
      if (closingFrames.length > 0) await controlDeliveryDelay()
      for (const attempt of attempts.values()) {
        attempt.peer.close(new Error('Native WebRTC listener stopped'))
      }
      attempts.clear()
      for (const key of pendingCandidates.keys()) forgetPendingCandidates(key)
      for (const entry of streams.values()) {
        entry.stream.peerLeft()
        entry.link.peer?.close(new Error('Native WebRTC listener stopped'))
      }
      streams.clear()
      await Promise.allSettled(signalingSessions.map(session => session.close()))
    }
  }
}

export function connectNativeWebRtc ({
  signalingSessions,
  RTCPeerConnection,
  rtcConfig,
  verifyAuthResponse,
  timeoutMs = 30_000,
  reconnectGraceMs = WEBRTC_RECONNECT_GRACE_MS,
  onReconnecting = () => {},
  onReconnected = () => {},
  onState = () => {},
  onLog = () => {}
}) {
  validateSessions(signalingSessions)
  if (typeof verifyAuthResponse !== 'function') throw new TypeError('verifyAuthResponse callback is required')
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1) throw new RangeError('timeoutMs must be a positive integer')

  const link = { peer: null }
  const attempts = new Set()
  let stream = null
  let settled = false
  let closed = false
  let retryTimer
  let overallTimer
  let resolveConnection
  let rejectConnection

  const promise = new Promise((resolve, reject) => {
    resolveConnection = resolve
    rejectConnection = reject
  })
  promise.catch(() => {})

  const scheduleRound = (delayMs = RETRY_DELAY_MS) => {
    if (closed || stream?.status === 'closed' || retryTimer != null) return
    retryTimer = setTimeout(() => {
      retryTimer = undefined
      void startRound()
    }, delayMs)
    retryTimer.unref?.()
  }

  const handleActivePeerLost = (peer, error) => {
    if (closed || link.peer !== peer || stream == null || stream.status === 'closed') return
    link.peer = null
    stream.peerDisconnected(reconnectGraceMs)
    onReconnecting(stream, error)
    scheduleRound(0)
  }

  const claimPeer = async (attempt, response) => {
    const valid = await verifyAuthResponse(response, attempt.challenge)
    if (valid === false) throw new Error('Native WebRTC PeerId authentication failed')
    if (closed) throw new Error('Native WebRTC connection is closed')
    if (link.peer != null && link.peer !== attempt.peer) {
      throw new Error('Another native WebRTC signaling adapter already connected')
    }

    link.peer = attempt.peer
    await attempt.peer.sendFrame(NATIVE_WEBRTC_FRAME_AUTH_READY)

    if (stream == null) {
      stream = new WebRtcStream({
        sendData: bytes => sendThroughLink(link, NATIVE_WEBRTC_FRAME_DATA, bytes),
        sendControl: control => sendThroughLink(link, NATIVE_WEBRTC_FRAME_CONTROL, control),
        onFinalize: () => {
          if (!closed) {
            const timer = setTimeout(() => void close(), 50)
            timer.unref?.()
          }
        }
      })
      setSignalingStrategy(stream, attempt.session.name)
      settled = true
      clearTimeout(overallTimer)
      resolveConnection(stream)
    } else {
      setSignalingStrategy(stream, attempt.session.name)
      stream.peerReconnected()
      onReconnected(stream, attempt.session.name)
    }

    for (const other of [...attempts]) {
      if (other === attempt) continue
      other.close(new Error('Another native WebRTC signaling adapter won'))
    }
  }

  const startAttempt = async session => {
    if (closed || link.peer != null || stream?.status === 'closed') return
    await session.ready
    if (closed || link.peer != null || stream?.status === 'closed') return
    if ([...attempts].some(attempt => attempt.session === session)) return

    const sessionId = createSignalingSessionId()
    const challenge = crypto.getRandomValues(new Uint8Array(32))
    let authenticated = false
    let attemptTimer
    let unsubscribe = () => {}
    let peer

    const attempt = {
      session,
      sessionId,
      challenge,
      get peer () {
        return peer
      },
      close (error) {
        clearTimeout(attemptTimer)
        unsubscribe()
        attempts.delete(attempt)
        void session.publish({ type: 'bye', sessionId }).catch(() => {})
        peer?.close(error)
      }
    }

    peer = new NativeWebRtcPeer({
      RTCPeerConnection,
      initiator: true,
      rtcConfig,
      trickleIce: session.trickleIce === true,
      onSignal: signal => session.publish({
        ...signal,
        sessionId
      }),
      onOpen: () => {
        void peer.sendFrame(NATIVE_WEBRTC_FRAME_AUTH_CHALLENGE, challenge)
          .catch(error => peer.close(asError(error)))
      },
      onFrame: async frame => {
        if (frame.type === NATIVE_WEBRTC_FRAME_AUTH_RESPONSE) {
          if (authenticated) return
          await claimPeer(attempt, frame.payload)
          authenticated = true
          clearTimeout(attemptTimer)
          return
        }
        if (!authenticated || link.peer !== peer || stream == null) {
          throw new Error('Received native WebRTC data before PeerId authentication')
        }
        if (frame.type === NATIVE_WEBRTC_FRAME_DATA) stream.receiveData(frame.payload)
        else if (frame.type === NATIVE_WEBRTC_FRAME_CONTROL) {
          stream.receiveControl(decodeNativeWebRtcControl(frame.payload))
        }
      },
      onClose: error => {
        clearTimeout(attemptTimer)
        unsubscribe()
        attempts.delete(attempt)
        if (authenticated) handleActivePeerLost(peer, error)
        else if (!closed && link.peer == null && attempts.size === 0) scheduleRound()
      },
      onError: error => log(onLog, `${session.name}: ${error.message}`),
      onState: state => onState(session.name, session.peerId, state)
    })

    unsubscribe = session.subscribe(message => {
      if (
        message.sessionId !== sessionId ||
        message.to !== session.peerId ||
        (message.type !== 'answer' && message.type !== 'candidate')
      ) return
      void peer.signal(message).catch(error => peer.close(asError(error)))
    })
    attempts.add(attempt)
    attemptTimer = setTimeout(() => {
      attempt.close(new Error(`${session.name} native WebRTC attempt timed out`))
      if (!closed && link.peer == null && attempts.size === 0) scheduleRound()
    }, INITIAL_ATTEMPT_TIMEOUT_MS)
    attemptTimer.unref?.()
    await peer.start()
  }

  const startRound = () => {
    if (closed || link.peer != null || stream?.status === 'closed') return
    for (const session of signalingSessions) {
      void startAttempt(session).catch(error => {
        log(onLog, `${session.name}: ${errorMessage(error)}`)
        if (!closed && link.peer == null && attempts.size === 0) scheduleRound()
      })
    }
  }

  const close = async () => {
    if (closed) return
    closed = true
    clearTimeout(retryTimer)
    clearTimeout(overallTimer)
    if (!settled) rejectConnection(new Error('Native WebRTC connection cancelled'))
    if (stream?.status === 'open' && link.peer != null && !link.peer.isClosed) {
      await link.peer.sendFrame(NATIVE_WEBRTC_FRAME_CONTROL, 'abort').catch(() => {})
      await controlDeliveryDelay()
    }
    for (const attempt of [...attempts]) attempt.close(new Error('Native WebRTC connection closed'))
    link.peer = null
    if (stream?.status !== 'closed') stream?.peerLeft()
    await Promise.allSettled(signalingSessions.map(session => session.close()))
  }

  overallTimer = setTimeout(() => {
    if (settled || closed) return
    const statuses = signalingSessions.map(session => {
      const status = session.status()
      return `${status.name} ${status.open}/${status.total}`
    }).join('; ')
    rejectConnection(new Error(`Native WebRTC did not connect in ${Math.ceil(timeoutMs / 1_000)} seconds: ${statuses}`))
    void close()
  }, timeoutMs)
  overallTimer.unref?.()
  startRound()

  return { promise, close }
}

function sendThroughLink (link, type, value) {
  const peer = link.peer
  if (peer == null || peer.isClosed) throw new Error('Native WebRTC peer is reconnecting')
  return peer.sendFrame(type, value)
}

function deferPeerClose (peer, error) {
  if (peer == null) return
  const timer = setTimeout(() => peer.close(error), 50)
  timer.unref?.()
}

function controlDeliveryDelay () {
  return new Promise(resolve => setTimeout(resolve, 50))
}

function setSignalingStrategy (stream, strategy) {
  Object.defineProperty(stream, 'signalingStrategy', {
    configurable: true,
    value: strategy
  })
}

function validateSessions (sessions) {
  if (!Array.isArray(sessions) || sessions.length === 0) {
    throw new Error('At least one native WebRTC signaling session is required')
  }
  const peerIds = new Set(sessions.map(session => session.peerId))
  if (peerIds.size !== 1) {
    throw new Error('Native WebRTC signaling sessions must share one peerId')
  }
}

function attemptKey (session, remoteId, sessionId) {
  return `${session.name}\0${remoteId}\0${sessionId}`
}

function log (callback, message) {
  try {
    callback(message)
  } catch {}
}

function asError (value) {
  return value instanceof Error ? value : new Error(String(value))
}

function errorMessage (value) {
  return asError(value).message
}
