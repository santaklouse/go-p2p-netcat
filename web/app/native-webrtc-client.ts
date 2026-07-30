"use client";

import {
  WEBRTC_RECONNECT_GRACE_MS,
  WebRtcStream,
  authenticateClientStream,
  connectNativeWebRtc,
  createNostrSignalingSession,
  createSignalingPeerId,
  createTorrentSignalingSession,
  defaultRtcConfiguration,
  verifyWebRtcAuthResponse,
  webRtcRoomId,
} from "p2p-netcat-core";
import type { NativeSignalingSession } from "p2p-netcat-core";
import type { ClientEvents } from "./p2p-client";

export class BrowserNativeWebRtcClient {
  private readonly events: ClientEvents;
  private connection: ReturnType<typeof connectNativeWebRtc> | null = null;
  private stream: WebRtcStream | null = null;
  private receiveTask: Promise<void> | null = null;
  private stopped = false;

  constructor(events: ClientEvents) {
    this.events = events;
  }

  async connect(
    targetPeerId: string,
    logicalPort: number,
    timeoutMs = 30_000,
    signalingPeerId = createSignalingPeerId(),
    pairingToken = "",
  ) {
    if (typeof RTCPeerConnection === "undefined") throw new Error("WebRTC не поддерживается этим браузером");
    if (typeof WebSocket === "undefined") throw new Error("WebSocket signaling не поддерживается этим браузером");

    const roomId = webRtcRoomId(targetPeerId, logicalPort);
    const status = ({ adapter, url, state, detail }: {
      adapter: string;
      url: string;
      state: string;
      detail?: string;
    }) => {
      if (!["open", "error", "closed"].includes(state)) return;
      this.events.onLog(`${adapter}: ${url}: ${state}${detail == null ? "" : ` (${detail})`}`);
    };
    const results = await Promise.allSettled([
      createNostrSignalingSession({
        roomId,
        peerId: signalingPeerId,
        WebSocket,
        onStatus: status,
        pairingToken: pairingToken || undefined,
      }),
      createTorrentSignalingSession({
        roomId,
        peerId: signalingPeerId,
        WebSocket,
        onStatus: status,
        pairingToken: pairingToken || undefined,
      }),
    ]);
    const signalingSessions = results
      .filter((result): result is PromiseFulfilledResult<NativeSignalingSession> => result.status === "fulfilled")
      .map((result) => result.value);

    if (this.stopped) {
      await Promise.allSettled(signalingSessions.map((session) => session.close()));
      throw new Error("WebRTC-подключение отменено");
    }
    if (signalingSessions.length === 0) {
      const reasons = results
        .filter((result): result is PromiseRejectedResult => result.status === "rejected")
        .map((result) => result.reason instanceof Error ? result.reason.message : String(result.reason))
        .join("; ");
      throw new Error(`Собственный WebRTC signaling не запущен: ${reasons}`);
    }

    this.events.onLog("WebRTC: собственный поиск через Nostr и BitTorrent signaling");
    const connection = connectNativeWebRtc({
      signalingSessions,
      RTCPeerConnection,
      rtcConfig: defaultRtcConfiguration(),
      timeoutMs,
      reconnectGraceMs: WEBRTC_RECONNECT_GRACE_MS,
      verifyAuthResponse: async (value, challenge) => {
        const valid = await verifyWebRtcAuthResponse(value, targetPeerId, logicalPort, challenge);
        if (!valid) throw new Error("Некорректная подпись WebRTC PeerId");
        this.events.onLog(`WebRTC: PeerId ${targetPeerId} подтверждён`, "success");
        return true;
      },
      onReconnecting: () => {
        this.events.onLog(
          `WebRTC-канал временно потерян; сохраняем PTY и ждём переподключение до ${WEBRTC_RECONNECT_GRACE_MS / 1000} с`,
        );
        this.events.onReconnecting();
      },
      onReconnected: (_stream, strategy) => {
        this.events.onLog(`WebRTC-канал восстановлен через ${strategy}`, "success");
        this.events.onReconnected();
      },
      onState: (strategy, _remoteId, state) => {
        this.events.onLog(
          `${strategy}: connection=${state.connectionState}, ICE=${state.iceConnectionState}`,
        );
      },
      onLog: (message) => this.events.onLog(`WebRTC: ${message}`),
    });
    this.connection = connection;
    const openedStream = await connection.promise;
    if (this.stopped) {
      await connection.close();
      throw new Error("WebRTC-подключение отменено");
    }
    const stream = pairingToken.length === 0
      ? openedStream
      : await authenticateClientStream(openedStream, pairingToken, {
          timeoutMs: Math.min(timeoutMs, 10_000),
        });
    if (pairingToken.length > 0) {
      this.events.onLog("Pairing token подтверждён обеими сторонами", "success");
    }
    this.stream = stream;
    this.receiveTask = this.receiveLoop(stream);
  }

  async send(chunk: Uint8Array) {
    if (this.stream == null) throw new Error("WebRTC-канал не открыт");
    this.stream.send(chunk);
    await this.stream.onDrain();
  }

  async closeWrite() {
    await this.stream?.close();
  }

  async stop() {
    if (this.stopped) return;
    this.stopped = true;
    await this.connection?.close().catch(() => {});
    await this.receiveTask?.catch(() => {});
    this.connection = null;
    this.stream = null;
  }

  private async receiveLoop(stream: WebRtcStream) {
    try {
      for await (const chunk of stream) await this.events.onData(chunk);
    } catch (error) {
      if (!this.stopped) this.events.onLog(error instanceof Error ? error.message : String(error), "error");
    } finally {
      if (!this.stopped && stream.writeStatus === "writable") {
        await stream.close().catch(() => {});
      }
      if (!this.stopped) this.events.onClosed();
    }
  }
}
