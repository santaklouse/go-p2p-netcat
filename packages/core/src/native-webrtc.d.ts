/// <reference lib="dom" />

export const NATIVE_WEBRTC_PROTOCOL_VERSION: 2;
export const NATIVE_WEBRTC_DATA_CHANNEL_LABEL: "p2p-netcat-v2";
export const NATIVE_WEBRTC_FRAME_DATA: 0;
export const NATIVE_WEBRTC_FRAME_CONTROL: 1;
export const NATIVE_WEBRTC_FRAME_AUTH_CHALLENGE: 2;
export const NATIVE_WEBRTC_FRAME_AUTH_RESPONSE: 3;
export const NATIVE_WEBRTC_FRAME_AUTH_READY: 4;
export const NATIVE_WEBRTC_DISCONNECT_DELAY_MS: 8000;
export const NATIVE_WEBRTC_BUFFER_HIGH_WATER_MARK: 262144;

export type NativeWebRtcSignal =
  | { type: "offer" | "answer"; sdp: string }
  | { type: "candidate"; candidate: RTCIceCandidateInit };

export type NativeWebRtcFrame = Readonly<{
  type: number;
  payload: Uint8Array;
}>;

export function encodeNativeWebRtcFrame(
  type: number,
  value?: string | ArrayBuffer | ArrayBufferView,
): Uint8Array;
export function decodeNativeWebRtcFrame(value: ArrayBuffer | ArrayBufferView): NativeWebRtcFrame;
export function decodeNativeWebRtcControl(value: ArrayBuffer | ArrayBufferView): string;

export type NativeWebRtcPeerOptions = {
  RTCPeerConnection: typeof globalThis.RTCPeerConnection;
  initiator: boolean;
  rtcConfig?: RTCConfiguration;
  trickleIce?: boolean;
  onSignal(signal: NativeWebRtcSignal): void | Promise<void>;
  onFrame(frame: NativeWebRtcFrame): void | Promise<void>;
  onOpen?: () => void;
  onClose?: (error: Error) => void;
  onError?: (error: Error) => void;
  onState?: (state: {
    connectionState: RTCPeerConnectionState;
    iceConnectionState: RTCIceConnectionState;
  }) => void;
  dataChannelLabel?: string;
  bufferHighWaterMark?: number;
};

export class NativeWebRtcPeer {
  readonly connection: RTCPeerConnection;
  readonly channel: RTCDataChannel | null;
  readonly isOpen: boolean;
  readonly isClosed: boolean;
  constructor(options: NativeWebRtcPeerOptions);
  start(): Promise<void>;
  signal(signal: NativeWebRtcSignal): Promise<void>;
  waitUntilOpen(): Promise<void>;
  sendFrame(type: number, value?: string | ArrayBuffer | ArrayBufferView): Promise<void>;
  close(error?: Error): void;
}
