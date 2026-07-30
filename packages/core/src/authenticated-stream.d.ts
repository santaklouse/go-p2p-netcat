import type { PairingToken, PairingTokenInput } from "./pairing.js";

export type PairingStream = AsyncIterable<unknown> & {
  readonly status?: string;
  readonly writeStatus?: string;
  readonly connectionStatus?: string;
  readonly signalingStrategy?: string;
  send(value: ArrayBuffer | ArrayBufferView): boolean;
  onDrain(): Promise<void>;
  close(...args: unknown[]): Promise<void>;
  abort(error?: Error): void;
};

export function authenticateClientStream<T>(
  stream: T,
  pairingToken: string | PairingToken | PairingTokenInput,
  options?: { timeoutMs?: number; nowSeconds?: number },
): Promise<T>;
export function authenticateServerStream<T>(
  stream: T,
  pairingToken: string | PairingToken | PairingTokenInput,
  options?: { timeoutMs?: number; nowSeconds?: number },
): Promise<T>;
