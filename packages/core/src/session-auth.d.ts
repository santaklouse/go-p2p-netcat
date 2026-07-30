import type { PairingToken, PairingTokenInput } from "./pairing.js";

export const SESSION_AUTH_VERSION: 1;
export const SESSION_AUTH_CLIENT_HELLO: 1;
export const SESSION_AUTH_SERVER_ACK: 2;
export const SESSION_AUTH_FRAME_BYTES: 62;
export const DEFAULT_SESSION_AUTH_CLOCK_SKEW_SECONDS: 120;

export type SessionAuthHello = Readonly<{
  frame: Uint8Array;
  timestamp: number;
  clientNonce: Uint8Array;
}>;

export function createSessionAuthHello(
  value: string | PairingToken | PairingTokenInput,
  options?: { nowSeconds?: number; nonce?: ArrayBuffer | ArrayBufferView },
): Promise<SessionAuthHello>;
export function verifySessionAuthHello(
  value: string | PairingToken | PairingTokenInput,
  frame: ArrayBuffer | ArrayBufferView,
  options?: { nowSeconds?: number; maxClockSkewSeconds?: number },
): Promise<Readonly<{ timestamp: number; clientNonce: Uint8Array }>>;
export function createSessionAuthAck(
  value: string | PairingToken | PairingTokenInput,
  hello: { timestamp: number; clientNonce: ArrayBuffer | ArrayBufferView },
  options?: { nonce?: ArrayBuffer | ArrayBufferView },
): Promise<Uint8Array>;
export function verifySessionAuthAck(
  value: string | PairingToken | PairingTokenInput,
  hello: { timestamp: number; clientNonce: ArrayBuffer | ArrayBufferView },
  frame: ArrayBuffer | ArrayBufferView,
): Promise<Readonly<{ serverNonce: Uint8Array }>>;
