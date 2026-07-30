/// <reference lib="dom" />

export const NATIVE_SIGNAL_VERSION: 2;
export const DEFAULT_NOSTR_SIGNALING_URLS: readonly string[];
export const DEFAULT_TORRENT_SIGNALING_URLS: readonly string[];

export type NativeSignalType = "offer" | "answer" | "candidate" | "bye";
export type NativeSignalMessage = Readonly<{
  version: 2;
  room: string;
  type: NativeSignalType;
  sessionId: string;
  from: string;
  to?: string;
  createdAt: number;
  sdp?: string;
  candidate?: RTCIceCandidateInit;
}>;
export type NativeSignalStatus = Readonly<{
  adapter: string;
  url: string;
  state: string;
  detail?: string;
}>;
export type NativeSignalingSession = {
  readonly name: string;
  readonly peerId: string;
  readonly topic: string;
  readonly trickleIce: boolean;
  readonly ready: Promise<void>;
  subscribe(listener: (message: NativeSignalMessage) => void): () => void;
  publish(message: {
    type: NativeSignalType;
    sessionId: string;
    to?: string;
    sdp?: string;
    candidate?: RTCIceCandidateInit;
  }): Promise<void>;
  status(): Readonly<{ name: string; open: number; connecting: number; total: number }>;
  close(): Promise<void>;
};
export type NativeSignalingOptions = {
  roomId: string;
  peerId?: string;
  urls?: readonly string[];
  WebSocket?: typeof globalThis.WebSocket;
  onStatus?: (status: NativeSignalStatus) => void;
  pairingToken?: string | import("./pairing.js").PairingToken | import("./pairing.js").PairingTokenInput;
};

export function createSignalingPeerId(): string;
export function createSignalingSessionId(): string;
export function nativeSignalingRoomTopic(
  roomId: string,
  options?: { pairingToken?: NativeSignalingOptions["pairingToken"] },
): Promise<string>;
export function createNostrSignalingSession(options: NativeSignalingOptions): Promise<NativeSignalingSession>;
export function createTorrentSignalingSession(options: NativeSignalingOptions): Promise<NativeSignalingSession>;
