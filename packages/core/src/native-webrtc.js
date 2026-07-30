const textEncoder = new TextEncoder()
const textDecoder = new TextDecoder()

export const NATIVE_WEBRTC_PROTOCOL_VERSION = 2
export const NATIVE_WEBRTC_DATA_CHANNEL_LABEL = 'p2p-netcat-v2'
export const NATIVE_WEBRTC_FRAME_DATA = 0
export const NATIVE_WEBRTC_FRAME_CONTROL = 1
export const NATIVE_WEBRTC_FRAME_AUTH_CHALLENGE = 2
export const NATIVE_WEBRTC_FRAME_AUTH_RESPONSE = 3
export const NATIVE_WEBRTC_FRAME_AUTH_READY = 4
export const NATIVE_WEBRTC_DISCONNECT_DELAY_MS = 8_000
export const NATIVE_WEBRTC_BUFFER_HIGH_WATER_MARK = 256 * 1024

export function encodeNativeWebRtcFrame (type, value = new Uint8Array(0)) {
  if (!Number.isInteger(type) || type < 0 || type > 0xff) {
    throw new RangeError(`Native WebRTC frame type must be an unsigned byte, received: ${type}`)
  }
  const payload = typeof value === 'string' ? textEncoder.encode(value) : nativeBytes(value)
  const frame = new Uint8Array(2 + payload.byteLength)
  frame[0] = NATIVE_WEBRTC_PROTOCOL_VERSION
  frame[1] = type
  frame.set(payload, 2)
  return frame
}

export function decodeNativeWebRtcFrame (value) {
  const frame = nativeBytes(value)
  if (frame.byteLength < 2) throw new Error('Native WebRTC frame is shorter than its header')
  if (frame[0] !== NATIVE_WEBRTC_PROTOCOL_VERSION) {
    throw new Error(`Unsupported native WebRTC protocol version: ${frame[0]}`)
  }
  return Object.freeze({
    type: frame[1],
    payload: frame.slice(2)
  })
}

export function decodeNativeWebRtcControl (value) {
  return textDecoder.decode(nativeBytes(value))
}

export class NativeWebRtcPeer {
  connection
  #initiator
  #trickleIce
  #onSignal
  #onFrame
  #onOpen
  #onClose
  #onError
  #onState
  #channel = null
  #openPromise
  #resolveOpen
  #rejectOpen
  #pendingCandidates = []
  #receiveChain = Promise.resolve()
  #disconnectTimer
  #started = false
  #closed = false
  #finalized = false
  #bufferHighWaterMark

  constructor ({
    RTCPeerConnection,
    initiator,
    rtcConfig,
    trickleIce = false,
    onSignal,
    onFrame,
    onOpen = () => {},
    onClose = () => {},
    onError = () => {},
    onState = () => {},
    dataChannelLabel = NATIVE_WEBRTC_DATA_CHANNEL_LABEL,
    bufferHighWaterMark = NATIVE_WEBRTC_BUFFER_HIGH_WATER_MARK
  }) {
    if (typeof RTCPeerConnection !== 'function') {
      throw new TypeError('RTCPeerConnection constructor is required')
    }
    if (typeof onSignal !== 'function') throw new TypeError('onSignal callback is required')
    if (typeof onFrame !== 'function') throw new TypeError('onFrame callback is required')
    if (!Number.isSafeInteger(bufferHighWaterMark) || bufferHighWaterMark < 1) {
      throw new RangeError('bufferHighWaterMark must be a positive integer')
    }

    this.#initiator = Boolean(initiator)
    this.#trickleIce = Boolean(trickleIce)
    this.#onSignal = onSignal
    this.#onFrame = onFrame
    this.#onOpen = onOpen
    this.#onClose = onClose
    this.#onError = onError
    this.#onState = onState
    this.#bufferHighWaterMark = bufferHighWaterMark
    this.#openPromise = new Promise((resolve, reject) => {
      this.#resolveOpen = resolve
      this.#rejectOpen = reject
    })
    this.#openPromise.catch(() => {})

    this.connection = new RTCPeerConnection(rtcConfig)
    this.connection.onicecandidate = event => {
      if (!this.#trickleIce || event.candidate == null || this.#closed) return
      const candidate = typeof event.candidate.toJSON === 'function'
        ? event.candidate.toJSON()
        : {
            candidate: event.candidate.candidate,
            sdpMid: event.candidate.sdpMid,
            sdpMLineIndex: event.candidate.sdpMLineIndex,
            usernameFragment: event.candidate.usernameFragment
          }
      void this.#emitSignal({ type: 'candidate', candidate }).catch(error => {
        this.#reportError(error)
        this.close(error instanceof Error ? error : new Error(String(error)))
      })
    }
    this.connection.onconnectionstatechange = () => this.#handleConnectionState()
    this.connection.oniceconnectionstatechange = () => this.#handleConnectionState()

    if (this.#initiator) {
      this.#attachChannel(this.connection.createDataChannel(dataChannelLabel, { ordered: true }))
    } else {
      this.connection.ondatachannel = event => {
        if (event.channel.label !== dataChannelLabel) {
          event.channel.close()
          return
        }
        this.#attachChannel(event.channel)
      }
    }
  }

  get channel () {
    return this.#channel
  }

  get isOpen () {
    return this.#channel?.readyState === 'open' && !this.#closed
  }

  get isClosed () {
    return this.#closed
  }

  async start () {
    if (this.#closed) throw new Error('Native WebRTC peer is closed')
    if (!this.#initiator) throw new Error('Only the initiating peer can create an offer')
    if (this.#started) return
    this.#started = true
    await this.connection.setLocalDescription(await this.connection.createOffer())
    await this.#publishLocalDescription()
  }

  async signal (signal) {
    if (this.#closed) return
    if (signal == null || typeof signal !== 'object') throw new TypeError('WebRTC signal must be an object')

    if (signal.type === 'candidate') {
      if (signal.candidate == null) return
      if (this.connection.remoteDescription == null) {
        this.#pendingCandidates.push(signal.candidate)
        return
      }
      await this.connection.addIceCandidate(signal.candidate)
      return
    }

    if (signal.type !== 'offer' && signal.type !== 'answer') {
      throw new Error(`Unsupported WebRTC signal type: ${signal.type}`)
    }
    if (typeof signal.sdp !== 'string' || signal.sdp.length === 0) {
      throw new Error(`WebRTC ${signal.type} does not contain SDP`)
    }

    await this.connection.setRemoteDescription({ type: signal.type, sdp: signal.sdp })
    await this.#flushCandidates()

    if (signal.type === 'offer') {
      await this.connection.setLocalDescription(await this.connection.createAnswer())
      await this.#publishLocalDescription()
    }
  }

  waitUntilOpen () {
    return this.#openPromise
  }

  async sendFrame (type, value) {
    await this.waitUntilOpen()
    const channel = this.#channel
    if (this.#closed || channel == null || channel.readyState !== 'open') {
      throw new Error('Native WebRTC data channel is not open')
    }
    await this.#waitForBuffer(channel)
    try {
      channel.send(encodeNativeWebRtcFrame(type, value))
    } catch (error) {
      this.#reportError(error)
      throw error
    }
  }

  close (error = new Error('Native WebRTC peer closed')) {
    if (this.#closed) return
    this.#closed = true
    clearTimeout(this.#disconnectTimer)
    this.#disconnectTimer = undefined
    this.#rejectOpen(error)
    try {
      this.#channel?.close()
    } catch {}
    try {
      this.connection.close()
    } catch {}
    this.#finalize(error)
  }

  async #publishLocalDescription () {
    if (!this.#trickleIce) await waitForIceGathering(this.connection)
    const description = this.connection.localDescription
    if (description == null || typeof description.sdp !== 'string') {
      throw new Error('RTCPeerConnection did not create a local description')
    }
    await this.#emitSignal({ type: description.type, sdp: description.sdp })
  }

  #emitSignal (signal) {
    try {
      return Promise.resolve(this.#onSignal(signal))
    } catch (error) {
      return Promise.reject(error)
    }
  }

  async #flushCandidates () {
    for (const candidate of this.#pendingCandidates.splice(0)) {
      await this.connection.addIceCandidate(candidate)
    }
  }

  #attachChannel (channel) {
    if (this.#channel != null && this.#channel !== channel) {
      channel.close()
      return
    }
    this.#channel = channel
    channel.binaryType = 'arraybuffer'
    channel.bufferedAmountLowThreshold = Math.max(1, Math.floor(this.#bufferHighWaterMark / 2))
    channel.onopen = () => {
      if (this.#closed) return
      this.#resolveOpen()
      try {
        this.#onOpen()
      } catch (error) {
        this.#reportError(error)
      }
    }
    channel.onmessage = event => {
      this.#receiveChain = this.#receiveChain
        .then(async () => {
          const value = await messageBytes(event.data)
          const frame = decodeNativeWebRtcFrame(value)
          await this.#onFrame(frame)
        })
        .catch(error => {
          this.#reportError(error)
          this.close(error instanceof Error ? error : new Error(String(error)))
        })
    }
    channel.onerror = event => {
      const error = event?.error ?? new Error('Native WebRTC data channel error')
      this.#reportError(error)
      this.close(error instanceof Error ? error : new Error(String(error)))
    }
    channel.onclose = () => {
      const error = new Error('Native WebRTC data channel closed')
      if (!this.#closed) this.close(error)
      else this.#finalize(error)
    }
  }

  async #waitForBuffer (channel) {
    while (!this.#closed && channel.readyState === 'open' && channel.bufferedAmount > this.#bufferHighWaterMark) {
      await new Promise((resolve, reject) => {
        let timeout
        const cleanup = () => {
          clearTimeout(timeout)
          channel.removeEventListener?.('bufferedamountlow', onLow)
          channel.removeEventListener?.('close', onClose)
        }
        const onLow = () => {
          cleanup()
          resolve()
        }
        const onClose = () => {
          cleanup()
          reject(new Error('Native WebRTC data channel closed while waiting for backpressure'))
        }
        channel.addEventListener?.('bufferedamountlow', onLow, { once: true })
        channel.addEventListener?.('close', onClose, { once: true })
        timeout = setTimeout(() => {
          cleanup()
          if (channel.readyState === 'open') resolve()
          else reject(new Error('Native WebRTC data channel closed while waiting for backpressure'))
        }, 1_000)
        timeout.unref?.()
      })
    }
  }

  #handleConnectionState () {
    if (this.#closed) return
    const connectionState = this.connection.connectionState
    const iceConnectionState = this.connection.iceConnectionState
    try {
      this.#onState({ connectionState, iceConnectionState })
    } catch (error) {
      this.#reportError(error)
    }

    const failed = connectionState === 'failed' ||
      connectionState === 'closed' ||
      iceConnectionState === 'failed' ||
      iceConnectionState === 'closed'
    if (failed) {
      this.close(new Error(`Native WebRTC connection failed: connection=${connectionState}, ICE=${iceConnectionState}`))
      return
    }

    const disconnected = connectionState === 'disconnected' || iceConnectionState === 'disconnected'
    if (disconnected) {
      if (this.#disconnectTimer == null) {
        this.#disconnectTimer = setTimeout(() => {
          this.#disconnectTimer = undefined
          if (
            this.connection.connectionState === 'disconnected' ||
            this.connection.iceConnectionState === 'disconnected'
          ) {
            this.close(new Error('Native WebRTC connection stayed disconnected'))
          }
        }, NATIVE_WEBRTC_DISCONNECT_DELAY_MS)
        this.#disconnectTimer.unref?.()
      }
      return
    }

    clearTimeout(this.#disconnectTimer)
    this.#disconnectTimer = undefined
  }

  #reportError (value) {
    const error = value instanceof Error ? value : new Error(String(value))
    try {
      this.#onError(error)
    } catch {}
  }

  #finalize (error) {
    if (this.#finalized) return
    this.#finalized = true
    clearTimeout(this.#disconnectTimer)
    try {
      this.#onClose(error)
    } catch (callbackError) {
      this.#reportError(callbackError)
    }
  }
}

async function waitForIceGathering (connection, timeoutMs = 12_000) {
  if (connection.iceGatheringState === 'complete') return
  await new Promise(resolve => {
    let timeout
    const finish = () => {
      clearTimeout(timeout)
      connection.removeEventListener?.('icegatheringstatechange', check)
      resolve()
    }
    const check = () => {
      if (connection.iceGatheringState === 'complete') finish()
    }
    connection.addEventListener?.('icegatheringstatechange', check)
    timeout = setTimeout(finish, timeoutMs)
    timeout.unref?.()
    check()
  })
}

async function messageBytes (value) {
  if (typeof Blob !== 'undefined' && value instanceof Blob) {
    return new Uint8Array(await value.arrayBuffer())
  }
  return nativeBytes(value)
}

function nativeBytes (value) {
  if (value instanceof Uint8Array) return value
  if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  throw new TypeError('Expected ArrayBuffer or ArrayBufferView')
}
