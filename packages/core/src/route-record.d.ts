import type { PrivateKey } from "@libp2p/interface";

export const ROUTE_RECORD_VERSION: 1;
export const DEFAULT_ROUTE_RECORD_TTL_SECONDS: 180;
export const ROUTE_CAPABILITIES: Readonly<{
  tcp: 1;
  quic: 2;
  ws: 4;
  wss: 8;
  webtransport: 16;
  webrtc: 32;
  relay: 64;
}>;

export type RouteRecord = Readonly<{
  version: 1;
  peerId: string;
  sequence: number;
  issuedAt: number;
  expiresAt: number;
  services: readonly number[];
  addresses: readonly string[];
  relayReservations: readonly string[];
  capabilities: number;
}>;

export type RouteRecordInput = {
  version?: unknown;
  peerId?: unknown;
  sequence?: unknown;
  issuedAt?: unknown;
  expiresAt?: unknown;
  services: readonly unknown[];
  addresses?: readonly unknown[];
  relayReservations?: readonly unknown[];
  capabilities?: number | Partial<Record<keyof typeof ROUTE_CAPABILITIES, boolean>>;
};

export function signRouteRecord(privateKey: PrivateKey, value: RouteRecordInput): Promise<Uint8Array>;
export function verifyRouteRecord(
  value: ArrayBuffer | ArrayBufferView,
  options?: {
    expectedPeerId?: unknown;
    expectedService?: unknown;
    nowSeconds?: number;
    clockSkewSeconds?: number;
  },
): Promise<RouteRecord>;
export function encodeRouteRecordPayload(value: RouteRecord | RouteRecordInput): Uint8Array;
export function decodeRouteRecordPayload(value: ArrayBuffer | ArrayBufferView): RouteRecord;
export function normalizeRouteRecord(value: RouteRecord | RouteRecordInput): RouteRecord;
export function routeCapabilityMask(
  value?: number | Partial<Record<keyof typeof ROUTE_CAPABILITIES, boolean>>,
): number;
export function routeCapabilitiesFromMask(value: unknown): Readonly<Record<keyof typeof ROUTE_CAPABILITIES, boolean>>;
