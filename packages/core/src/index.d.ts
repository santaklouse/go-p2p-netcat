import type { Multiaddr } from "@multiformats/multiaddr";
import type { PrivateKey } from "@libp2p/interface";

export * from "./native-webrtc.js";
export * from "./signaling.js";
export * from "./native-endpoint.js";
export * from "./pairing.js";
export * from "./route-record.js";
export * from "./session-auth.js";
export * from "./authenticated-stream.js";

export const APP_NAME: "p2p-netcat";
export const PROTOCOL_PREFIX: "/p2p-netcat/1.0.0";
export const DEFAULT_SERVICE: 31337;
export const WEBRTC_APP_ID: "io.github.santaklouse.p2p-netcat.v1";
export const WEBRTC_AUTH_VERSION: 1;
export const WEBRTC_CLIENT_ID_BYTES: 20;
export const PUBSUB_DISCOVERY_TOPIC: "io.github.santaklouse.p2p-netcat.peer-discovery.v1";
export const PUBSUB_DISCOVERY_INTERVAL_MS: 10000;
export const WEBRTC_RECONNECT_GRACE_MS: 120000;
export const WEBRTC_DATA_ACTION: "pnc-data-v1";
export const WEBRTC_CONTROL_ACTION: "pnc-ctl-v1";
export const PTY_FRAME_DATA: 0;
export const PTY_FRAME_RESIZE: 1;
export const PTY_FRAME_HEADER_LENGTH: 5;
export const PTY_MAX_FRAME_LENGTH: 1048576;
export const DEFAULT_STUN_URLS: readonly string[];

export type P2PNetcatRtcConfiguration = {
  iceServers: Array<{ urls: string[] }>;
};

export function defaultRtcConfiguration(): P2PNetcatRtcConfiguration;

export type RelayValidationOptions = {
  requireWebSocket?: boolean;
  secureContext?: boolean;
};

export type RelayDialPlan = Readonly<{
  peerId: string;
  service: number;
  protocol: string;
  relay: string;
  destination: string;
}>;

export type AddressLike = string | { toString(): string } | { multiaddr: { toString(): string } };

export function validateService(value?: unknown): number;
export function protocolForService(service: unknown): string;
export function encodePtyData(value: ArrayBuffer | ArrayBufferView): Uint8Array;
export function encodePtyResize(columns: unknown, rows: unknown): Uint8Array;
export function decodePtyResize(value: ArrayBuffer | ArrayBufferView): Readonly<{ columns: number; rows: number }>;

export type PtyFrame = Readonly<{ type: number; data: Uint8Array }>;

export class PtyFrameDecoder {
  push(value: ArrayBuffer | ArrayBufferView): PtyFrame[];
  finish(): void;
  reset(): void;
}
export function normalizePeerId(value: unknown): string;
export function normalizeMultiaddr(value: unknown): string;
export function isWebSocketAddress(value: unknown): boolean;
export function isSecureWebSocketAddress(value: unknown): boolean;
export function normalizeRelayAddress(value: unknown, options?: RelayValidationOptions): string;
export function relayedTargetAddress(relay: unknown, peerId: unknown, options?: RelayValidationOptions): Multiaddr;
export function createRelayDialPlan(input: {
  peerId: unknown;
  service?: unknown;
  relay: unknown;
  requireWebSocket?: boolean;
  secureContext?: boolean;
}): RelayDialPlan;
export function addressRank(address: AddressLike): number;
export function preferDialAddresses(a: AddressLike, b: AddressLike): number;
export function browserDialableAddress(address: AddressLike, options?: { secureContext?: boolean }): boolean;
export function webRtcRoomId(peerId: unknown, service?: unknown): string;
export function webRtcAuthPayload(peerId: unknown, service: unknown, challenge: ArrayBuffer | ArrayBufferView): Uint8Array;
export function createWebRtcClientChallenge(clientId: string): Uint8Array;
export function webRtcClientIdFromChallenge(challenge: ArrayBuffer | ArrayBufferView): string | null;
export function signWebRtcAuthResponse(privateKey: PrivateKey, service: unknown, challenge: ArrayBuffer | ArrayBufferView): Promise<Uint8Array>;
export function verifyWebRtcAuthResponse(value: ArrayBuffer | ArrayBufferView, expectedPeerId: unknown, service: unknown, challenge: ArrayBuffer | ArrayBufferView): Promise<boolean>;
export function encodeWebRtcAuthResponse(publicKey: ArrayBuffer | ArrayBufferView, signature: ArrayBuffer | ArrayBufferView): Uint8Array;
export function decodeWebRtcAuthResponse(value: ArrayBuffer | ArrayBufferView): Readonly<{ publicKey: Uint8Array; signature: Uint8Array }>;

export class WebRtcStream implements AsyncIterable<Uint8Array> {
  status: "open" | "closed";
  writeStatus: "writable" | "closing" | "closed";
  connectionStatus: "connected" | "reconnecting" | "disconnected";
  constructor(options: {
    sendData: (bytes: Uint8Array) => void | Promise<void>;
    sendControl: (control: string) => void | Promise<void>;
    onFinalize?: () => void;
    flowWindowBytes?: number;
    keepAliveIntervalMs?: number;
  });
  send(chunk: ArrayBuffer | ArrayBufferView): boolean;
  onDrain(): Promise<void>;
  close(): Promise<void>;
  abort(error?: Error): void;
  receiveData(chunk: ArrayBuffer | ArrayBufferView): void;
  receiveControl(control: string): void;
  peerDisconnected(graceMs?: number): void;
  peerReconnected(): boolean;
  peerLeft(): void;
  [Symbol.asyncIterator](): AsyncIterator<Uint8Array>;
}

export type WebRtcActionContext = { peerId: string };
export type WebRtcAction = {
  send(value: unknown, options?: { target?: string | string[] }): void | Promise<void>;
  onMessage: ((value: unknown, context: WebRtcActionContext) => void) | null;
};
export type WebRtcActionRoom = {
  makeAction(namespace: string): WebRtcAction;
  leave(): void | Promise<void>;
  onPeerJoin: ((peerId: string) => void) | null;
  onPeerLeave: ((peerId: string) => void) | null;
};
export type WebRtcActionHub = Readonly<{
  streamFor(peerId: string): WebRtcStream;
  close(): Promise<void>;
}>;
export type WebRtcActionHubOptions = {
  onStream?: (stream: WebRtcStream, peerId: string) => void;
  onStreamClosed?: (peerId: string, stream: WebRtcStream) => void;
  onPeerDisconnected?: (peerId: string, stream: WebRtcStream) => void;
  onPeerReconnected?: (peerId: string, stream: WebRtcStream) => void;
  leaveAfterStream?: boolean;
  reconnectGraceMs?: number;
  release?: () => void;
};
export function createWebRtcActionHub(room: WebRtcActionRoom, options?: WebRtcActionHubOptions): WebRtcActionHub;
