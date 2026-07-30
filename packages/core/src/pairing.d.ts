import type { CID } from "multiformats/cid";

export const PAIRING_TOKEN_PREFIX: "pnc1_";
export const PAIRING_TOKEN_VERSION: 1;
export const PAIRING_SECRET_BYTES: 32;
export const DEFAULT_RENDEZVOUS_INTERVAL_SECONDS: 300;

export type PairingToken = Readonly<{
  version: 1;
  peerId: string;
  service: number;
  secret: Uint8Array;
  relayHints: readonly string[];
  expiresAt?: number;
}>;

export type PairingTokenInput = {
  peerId: unknown;
  service?: unknown;
  secret?: ArrayBuffer | ArrayBufferView;
  relayHints?: readonly unknown[];
  expiresAt?: unknown;
};

export function createPairingToken(input: PairingTokenInput): string;
export function encodePairingToken(value: PairingToken | PairingTokenInput): string;
export function decodePairingToken(value: unknown): PairingToken;
export function normalizePairingToken(value: string | PairingToken | PairingTokenInput): PairingToken;
export function assertPairingTokenUsable(
  value: string | PairingToken | PairingTokenInput,
  options?: { peerId?: unknown; service?: unknown; nowSeconds?: number },
): PairingToken;
export function derivePairingKey(
  value: string | PairingToken | PairingTokenInput,
  purpose: "rendezvous" | "signaling" | "admission" | "route-record",
): Promise<Uint8Array>;
export function deriveRendezvousId(
  value: string | PairingToken | PairingTokenInput,
  options: { purpose?: "dht" | "pubsub" | "signaling"; epoch: number },
): Promise<string>;
export function pairingRendezvousWindows(
  value: string | PairingToken | PairingTokenInput,
  options?: {
    purpose?: "dht" | "pubsub" | "signaling";
    nowMs?: number;
    intervalSeconds?: number;
    offsets?: readonly number[];
  },
): Promise<readonly Readonly<{ epoch: number; id: string }>[]>;
export function rendezvousProviderCid(rendezvousId: unknown): Promise<CID>;
export function pairingProviderCids(
  value: string | PairingToken | PairingTokenInput,
  options?: {
    nowMs?: number;
    intervalSeconds?: number;
    offsets?: readonly number[];
  },
): Promise<CID[]>;
export function sealPairingPayload(
  value: string | PairingToken | PairingTokenInput,
  purpose: "signaling" | "admission" | "route-record",
  plaintext: ArrayBuffer | ArrayBufferView,
  options?: { additionalData?: ArrayBuffer | ArrayBufferView; nonce?: ArrayBuffer | ArrayBufferView },
): Promise<Uint8Array>;
export function openPairingPayload(
  value: string | PairingToken | PairingTokenInput,
  purpose: "signaling" | "admission" | "route-record",
  envelope: ArrayBuffer | ArrayBufferView,
  options?: { additionalData?: ArrayBuffer | ArrayBufferView },
): Promise<Uint8Array>;
export function base64UrlEncode(value: ArrayBuffer | ArrayBufferView): string;
export function base64UrlDecode(value: unknown): Uint8Array;
