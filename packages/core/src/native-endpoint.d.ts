/// <reference lib="dom" />

import type { WebRtcStream } from "./index.js";
import type { NativeSignalingSession } from "./signaling.js";

export type NativeEndpointState = {
  connectionState: RTCPeerConnectionState;
  iceConnectionState: RTCIceConnectionState;
};

export type NativeWebRtcListenerOptions = {
  signalingSessions: NativeSignalingSession[];
  RTCPeerConnection: typeof globalThis.RTCPeerConnection;
  rtcConfig?: RTCConfiguration;
  createAuthResponse(challenge: Uint8Array): ArrayBuffer | ArrayBufferView | Promise<ArrayBuffer | ArrayBufferView>;
  onStream?: (stream: WebRtcStream, remoteId: string, strategy: string) => void;
  onStreamClosed?: (remoteId: string, stream: WebRtcStream) => void;
  onPeerDisconnected?: (remoteId: string, stream: WebRtcStream, error: Error) => void;
  onPeerReconnected?: (remoteId: string, stream: WebRtcStream, strategy: string) => void;
  onState?: (strategy: string, remoteId: string, state: NativeEndpointState) => void;
  onLog?: (message: string) => void;
  reconnectGraceMs?: number;
};

export type NativeWebRtcConnectionOptions = {
  signalingSessions: NativeSignalingSession[];
  RTCPeerConnection: typeof globalThis.RTCPeerConnection;
  rtcConfig?: RTCConfiguration;
  verifyAuthResponse(response: Uint8Array, challenge: Uint8Array): boolean | Promise<boolean>;
  timeoutMs?: number;
  reconnectGraceMs?: number;
  onReconnecting?: (stream: WebRtcStream, error: Error) => void;
  onReconnected?: (stream: WebRtcStream, strategy: string) => void;
  onState?: (strategy: string, remoteId: string, state: NativeEndpointState) => void;
  onLog?: (message: string) => void;
};

export function startNativeWebRtcListener(options: NativeWebRtcListenerOptions): {
  close(): Promise<void>;
};
export function connectNativeWebRtc(options: NativeWebRtcConnectionOptions): {
  promise: Promise<WebRtcStream>;
  close(): Promise<void>;
};
