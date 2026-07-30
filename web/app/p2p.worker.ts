/// <reference lib="webworker" />

import { createLibp2p } from "libp2p";
import { webSockets } from "@libp2p/websockets";
import { webTransport } from "@libp2p/webtransport";
import { circuitRelayTransport } from "@libp2p/circuit-relay-v2";
import { noise } from "@chainsafe/libp2p-noise";
import { yamux } from "@chainsafe/libp2p-yamux";
import { identify } from "@libp2p/identify";
import { ping } from "@libp2p/ping";
import { gossipsub } from "@libp2p/gossipsub";
import { pubsubPeerDiscovery } from "@libp2p/pubsub-peer-discovery";
import { bootstrap } from "@libp2p/bootstrap";
import { kadDHT, removePrivateAddressesMapper } from "@libp2p/kad-dht";
import { peerIdFromString } from "@libp2p/peer-id";
import { multiaddr, type Multiaddr } from "@multiformats/multiaddr";
import {
  PUBSUB_DISCOVERY_INTERVAL_MS,
  PUBSUB_DISCOVERY_TOPIC,
  assertPairingTokenUsable,
  authenticateClientStream,
  browserDialableAddress,
  createRelayDialPlan,
  isWebSocketAddress,
  normalizePeerId,
  pairingProviderCids,
  preferDialAddresses,
  protocolForService,
  validateService,
} from "p2p-netcat-core";
import type { PairingToken } from "p2p-netcat-core";

const workerScope: DedicatedWorkerGlobalScope = self as unknown as DedicatedWorkerGlobalScope;
type Node = Awaited<ReturnType<typeof createLibp2p>>;
type Stream = Awaited<ReturnType<Node["dialProtocol"]>>;

type WorkerRequest = {
  id: number;
  action: "start" | "connect" | "send" | "ackData" | "closeWrite" | "stop";
  payload?: Record<string, unknown>;
};

let node: Node | null = null;
let stream: Stream | null = null;
let receiveTask: Promise<void> | null = null;
let unacknowledgedOutputBytes = 0;
let outputCreditWaiters: Array<() => void> = [];

const OUTPUT_HIGH_WATER_MARK = 512 * 1024;
const OUTPUT_LOW_WATER_MARK = 128 * 1024;

const IPFS_BOOTSTRAP_PEERS = [
  "/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
  "/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
  "/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
  "/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
  "/dnsaddr/va1.bootstrap.libp2p.io/p2p/12D3KooWKnDdG3iXw9eTFijk3EWSunZcFi54Zka4wmtqtt6rPxc8",
] as const;

type NetworkConfig = {
  delegatedRouting: string[];
  relays: string[];
};

type RoutingRecord = {
  ID?: string;
  Addrs?: string[];
};

const EMBEDDED_NETWORK_CONFIG: NetworkConfig = {
  delegatedRouting: ["https://delegated-ipfs.dev/routing/v1"],
  relays: [],
};

const ROUTE_CACHE_DB = "p2p-netcat-network";
const ROUTE_CACHE_STORE = "routes";
const ROUTE_CACHE_MAX_AGE = 24 * 60 * 60 * 1000;

function postLog(message: string, kind: "info" | "success" | "error" = "info") {
  workerScope.postMessage({ type: "log", message, kind });
}

function secureContext() {
  return workerScope.location.protocol === "https:";
}

function assertWebCrypto() {
  if (!workerScope.isSecureContext || workerScope.crypto?.subtle == null) {
    throw new Error("Web Crypto API недоступен. Откройте приложение по HTTPS, а не по HTTP.");
  }
}

function unique(values: string[]) {
  return [...new Set(values)];
}

function browserAddresses(values: Iterable<string>) {
  return unique([...values].filter((address) => (
    isWebSocketAddress(address) || address.includes("/webtransport")
  ) && browserDialableAddress(address, { secureContext: secureContext() })));
}

function targetAddress(address: string, peerId: string) {
  const normalized = multiaddr(address).toString().replace(/\/$/, "");
  if (normalized.endsWith(`/p2p/${peerId}`)) return normalized;
  return `${normalized}/p2p/${peerId}`;
}

function openRouteCache() {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(ROUTE_CACHE_DB, 1);
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(ROUTE_CACHE_STORE)) {
        request.result.createObjectStore(ROUTE_CACHE_STORE, { keyPath: "peerId" });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("IndexedDB недоступна"));
  });
}

async function cachedAddresses(peerId: string) {
  try {
    const database = await openRouteCache();
    const record = await new Promise<{ peerId: string; addresses: string[]; updatedAt: number } | undefined>((resolve, reject) => {
      const request = database.transaction(ROUTE_CACHE_STORE).objectStore(ROUTE_CACHE_STORE).get(peerId);
      request.onsuccess = () => resolve(request.result as { peerId: string; addresses: string[]; updatedAt: number } | undefined);
      request.onerror = () => reject(request.error ?? new Error("Не удалось прочитать кеш маршрутов"));
    });
    database.close();
    if (record == null || Date.now() - record.updatedAt > ROUTE_CACHE_MAX_AGE) return [];
    return browserAddresses(record.addresses);
  } catch {
    return [];
  }
}

async function cacheAddress(peerId: string, address: string) {
  try {
    const database = await openRouteCache();
    await new Promise<void>((resolve, reject) => {
      const request = database.transaction(ROUTE_CACHE_STORE, "readwrite").objectStore(ROUTE_CACHE_STORE).put({
        peerId,
        addresses: [address],
        updatedAt: Date.now(),
      });
      request.onsuccess = () => resolve();
      request.onerror = () => reject(request.error ?? new Error("Не удалось записать кеш маршрутов"));
    });
    database.close();
  } catch {
    // IndexedDB is only an optimization; private browsing may disable it.
  }
}

async function loadNetworkConfig(): Promise<NetworkConfig> {
  try {
    const url = new URL("../network-config.json", workerScope.location.href);
    const response = await fetch(url, { cache: "no-cache", signal: AbortSignal.timeout(4_000) });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const value = await response.json() as Partial<NetworkConfig>;
    return {
      delegatedRouting: Array.isArray(value.delegatedRouting)
        ? value.delegatedRouting.filter((item): item is string => typeof item === "string")
        : EMBEDDED_NETWORK_CONFIG.delegatedRouting,
      relays: Array.isArray(value.relays)
        ? value.relays.filter((item): item is string => typeof item === "string")
        : EMBEDDED_NETWORK_CONFIG.relays,
    };
  } catch (error) {
    postLog(`Сетевой конфиг недоступен, используем встроенный: ${error instanceof Error ? error.message : String(error)}`);
    return EMBEDDED_NETWORK_CONFIG;
  }
}

async function fetchRoutingRecords(endpoint: string, path: string, signal: AbortSignal) {
  const url = `${endpoint.replace(/\/$/, "")}/${path}`;
  const response = await fetch(url, {
    headers: { Accept: "application/json" },
    signal,
  });
  if (response.status === 404) return [];
  if (!response.ok) throw new Error(`${new URL(endpoint).host}: HTTP ${response.status}`);
  const value = await response.json() as { Peers?: RoutingRecord[]; Providers?: RoutingRecord[] };
  return value.Peers ?? value.Providers ?? [];
}

async function delegatedAddresses(
  peerId: ReturnType<typeof peerIdFromString>,
  endpoints: string[],
  pairingToken: PairingToken | null,
) {
  const providerCids = pairingToken == null
    ? [peerId.toCID()]
    : await pairingProviderCids(pairingToken, { offsets: [-1, 0, 1] });
  const paths = [
    ...(pairingToken == null ? [`peers/${encodeURIComponent(peerId.toString())}`] : []),
    ...providerCids.map((cid) => `providers/${encodeURIComponent(cid.toString())}`),
  ];
  const requests = endpoints.flatMap((endpoint) => paths.map((path) => ({ endpoint, path })));
  const controllers = requests.map(() => new AbortController());
  try {
    return await Promise.any(requests.map(async ({ endpoint, path }, index) => {
      const records = await fetchRoutingRecords(endpoint, path, AbortSignal.any([
        controllers[index].signal,
        AbortSignal.timeout(8_000),
      ]));
      const addresses = browserAddresses(records.flatMap((record) => {
        try {
          if (pairingToken != null && record.ID == null) return [];
          if (record.ID != null && normalizePeerId(record.ID) !== peerId.toString()) return [];
        } catch {
          return [];
        }
        return record.Addrs ?? [];
      }));
      if (addresses.length === 0) throw new Error("Delegated routing returned no browser-dialable address");
      return addresses;
    }));
  } catch {
    return [];
  } finally {
    for (const controller of controllers) {
      controller.abort(new Error("Delegated routing completed through another endpoint"));
    }
  }
}

async function knownAddresses(target: ReturnType<typeof peerIdFromString>) {
  try {
    const peer = await node!.peerStore.get(target);
    return browserAddresses(peer.addresses.map((entry) => entry.multiaddr.toString()));
  } catch {
    return [];
  }
}

async function dhtAddresses(
  target: ReturnType<typeof peerIdFromString>,
  pairingToken: PairingToken | null,
  timeoutMs = 20_000,
) {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;

  while (Date.now() < deadline) {
    const known = await knownAddresses(target);
    if (known.length > 0) return known;

    try {
      const providerCids = pairingToken == null
        ? [target.toCID()]
        : await pairingProviderCids(pairingToken, { offsets: [-1, 0, 1] });
      const controllers = providerCids.map(() => new AbortController());
      try {
        const addresses = await Promise.any(providerCids.map(async (cid, index) => {
          const querySignal = AbortSignal.any([
            controllers[index].signal,
            AbortSignal.timeout(Math.min(5_000, Math.max(1, deadline - Date.now()))),
          ]);
          for await (const provider of node!.contentRouting.findProviders(cid, { signal: querySignal })) {
            if (!provider.id.equals(target)) continue;
            const found = browserAddresses(provider.multiaddrs.map((address) => address.toString()));
            if (found.length > 0) return found;
          }
          throw new Error("Provider record not found in this rendezvous window");
        }));
        return addresses;
      } finally {
        for (const controller of controllers) {
          controller.abort(new Error("Provider lookup completed in another rendezvous window"));
        }
      }
    } catch (error) {
      lastError = error;
    }

    if (pairingToken == null) {
      try {
        const querySignal = AbortSignal.timeout(Math.min(5_000, Math.max(1, deadline - Date.now())));
        const info = await node!.peerRouting.findPeer(target, { signal: querySignal });
        const addresses = browserAddresses(info.multiaddrs.map((address) => address.toString()));
        if (addresses.length > 0) return addresses;
      } catch (error) {
        lastError = error;
      }
    }

    await new Promise((resolve) => setTimeout(resolve, 500));
  }

  if (lastError != null) postLog(`DHT не вернула браузерный адрес: ${lastError instanceof Error ? lastError.message : String(lastError)}`);
  return [];
}

async function dialFirst(candidates: Multiaddr[], protocol: string, timeoutMs = 20_000) {
  if (candidates.length === 0) throw new Error("Нет доступных браузеру маршрутов");
  const controllers = candidates.map(() => new AbortController());
  const attempts = candidates.map(async (candidate, index) => ({
    index,
    stream: await node!.dialProtocol(candidate, protocol, {
      signal: AbortSignal.any([controllers[index].signal, AbortSignal.timeout(timeoutMs)]),
      runOnLimitedConnection: true,
    }),
  }));

  try {
    const winner = await Promise.any(attempts);
    controllers.forEach((controller, index) => {
      if (index !== winner.index) controller.abort(new Error("Выбран более быстрый маршрут"));
    });
    return { stream: winner.stream, address: candidates[winner.index].toString() };
  } catch (error) {
    throw new Error("Все найденные браузерные маршруты отклонены", { cause: error });
  }
}

async function startNode(privateDiscovery = false) {
  if (node != null) return node.peerId.toString();
  postLog("Сетевой стек запускается в Web Worker…");
  assertWebCrypto();

  node = await createLibp2p({
    addresses: { listen: [] },
    transports: [webTransport(), webSockets(), circuitRelayTransport()],
    connectionEncrypters: [noise()],
    streamMuxers: [yamux()],
    services: {
      identify: identify(),
      ping: ping(),
      pubsub: gossipsub({
        allowPublishToZeroTopicPeers: true,
      }),
      aminoDHT: kadDHT({
        protocol: "/ipfs/kad/1.0.0",
        clientMode: true,
        peerInfoMapper: removePrivateAddressesMapper,
      }),
    },
    peerDiscovery: [
      bootstrap({ list: [...IPFS_BOOTSTRAP_PEERS], timeout: 10_000 }),
      ...(privateDiscovery
        ? []
        : [pubsubPeerDiscovery({
            interval: PUBSUB_DISCOVERY_INTERVAL_MS,
            topics: [PUBSUB_DISCOVERY_TOPIC],
          })]),
    ],
    connectionGater: {
      denyDialMultiaddr: () => false,
    },
    connectionManager: {
      addressSorter: (a, b) => {
        const order = preferDialAddresses(a, b);
        return order < 0 ? -1 : order > 0 ? 1 : 0;
      },
    },
  });

  const peerId = node.peerId.toString();
  postLog(`Браузерный PeerId: ${peerId}`, "success");
  return peerId;
}

async function connect(payload: Record<string, unknown>) {
  if (stream != null) throw new Error("Соединение уже открыто");
  const targetPeerId = normalizePeerId(payload.targetPeerId);
  const target = peerIdFromString(targetPeerId);
  const service = validateService(payload.logicalPort);
  const protocol = protocolForService(service);
  const relay = String(payload.relayAddress ?? "").trim();
  const pairingTokenValue = String(payload.pairingToken ?? "").trim();
  const pairingToken = pairingTokenValue.length === 0
    ? null
    : assertPairingTokenUsable(pairingTokenValue, { peerId: targetPeerId, service });
  await startNode(pairingToken != null);

  if (relay.length > 0) {
    const plan = createRelayDialPlan({
      peerId: targetPeerId,
      service,
      relay,
      requireWebSocket: true,
      secureContext: secureContext(),
    });
    postLog(`Используем указанный relay для ${plan.peerId}:${plan.service}…`);
    stream = (await dialFirst([multiaddr(plan.destination)], plan.protocol)).stream;
  } else {
    const cached = (await cachedAddresses(targetPeerId)).map((address) => multiaddr(targetAddress(address, targetPeerId)));
    if (cached.length > 0) {
      postLog("Проверяем ранее работавший маршрут…");
      try {
        const winner = await dialFirst(cached, protocol, 6_000);
        stream = winner.stream;
        await cacheAddress(targetPeerId, winner.address);
      } catch {
        postLog("Сохранённый маршрут устарел; запускаем новый поиск");
      }
    }

    if (stream == null) postLog(`Ищем ${targetPeerId}:${service} через подписанный PubSub, delegated routing и IPFS DHT…`);
    if (stream == null) {
      const config = await loadNetworkConfig();
      const delegated = await delegatedAddresses(target, config.delegatedRouting, pairingToken);
      const discovered = delegated.length > 0 ? delegated : await dhtAddresses(target, pairingToken);
      const directCandidates = discovered.map((address) => multiaddr(targetAddress(address, targetPeerId)));
      const relayCandidates = [...(pairingToken?.relayHints ?? []), ...config.relays].flatMap((address) => {
        try {
          return [multiaddr(createRelayDialPlan({
            peerId: targetPeerId,
            service,
            relay: address,
            requireWebSocket: true,
            secureContext: secureContext(),
          }).destination)];
        } catch (error) {
          postLog(`Пропущен некорректный relay из network-config.json: ${error instanceof Error ? error.message : String(error)}`);
          return [];
        }
      });
      const candidates = [...directCandidates, ...relayCandidates];
      if (candidates.length === 0) {
        throw new Error("PeerId найден не был или у него нет WSS/WebTransport/relay-адреса. Откройте дополнительные настройки и укажите relay multiaddr.");
      }
      postLog(`Найдено браузерных маршрутов: ${candidates.length}; выбираем самый быстрый…`);
      const winner = await dialFirst(candidates, protocol);
      stream = winner.stream;
      await cacheAddress(targetPeerId, winner.address);
    }
  }

  if (stream == null) throw new Error("Транспорт не вернул поток");
  if (pairingToken != null) {
    postLog("Проверяем взаимное знание pairing token…");
    stream = await authenticateClientStream(stream, pairingToken, {
      timeoutMs: Math.min(10_000, Number(payload.timeout ?? 30) * 1000),
    });
    postLog("Pairing token подтверждён обеими сторонами", "success");
  }

  postLog(`Канал ${targetPeerId}:${service} открыт`, "success");
  receiveTask = receiveLoop(stream);
}

async function connectWithTimeout(payload: Record<string, unknown>) {
  const timeoutSeconds = Number(payload.timeout ?? 30);
  if (!Number.isSafeInteger(timeoutSeconds) || timeoutSeconds < 1) {
    throw new Error("Таймаут подключения должен быть положительным целым числом");
  }

  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      connect(payload),
      new Promise<never>((_resolve, reject) => {
        timer = setTimeout(() => {
          reject(new Error(`libp2p не установил соединение за ${timeoutSeconds} с`));
        }, timeoutSeconds * 1000);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

async function send(bytes: ArrayBuffer) {
  if (stream == null) throw new Error("Сначала установите соединение");
  if (stream.writeStatus !== "writable") throw new Error("Запись в канал уже закрыта");
  if (!stream.send(new Uint8Array(bytes))) await stream.onDrain();
}

async function receiveLoop(activeStream: Stream) {
  try {
    for await (const chunk of activeStream) {
      const bytes = chunk instanceof Uint8Array ? chunk : chunk.subarray();
      while (
        unacknowledgedOutputBytes > 0
        && unacknowledgedOutputBytes + bytes.byteLength > OUTPUT_HIGH_WATER_MARK
      ) {
        await new Promise<void>((resolve) => outputCreditWaiters.push(resolve));
      }
      unacknowledgedOutputBytes += bytes.byteLength;
      const transferable = bytes.slice().buffer;
      workerScope.postMessage({ type: "data", bytes: transferable }, [transferable]);
    }
    postLog("Удалённая сторона завершила передачу");
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!message.includes("закрыто пользователем")) postLog(message, "error");
  } finally {
    if (stream === activeStream) stream = null;
    workerScope.postMessage({ type: "closed" });
  }
}

function acknowledgeOutput(bytes: number) {
  if (!Number.isSafeInteger(bytes) || bytes <= 0) return;
  unacknowledgedOutputBytes = Math.max(0, unacknowledgedOutputBytes - bytes);
  if (unacknowledgedOutputBytes <= OUTPUT_LOW_WATER_MARK) {
    for (const resolve of outputCreditWaiters.splice(0)) resolve();
  }
}

function resetOutputCredit() {
  unacknowledgedOutputBytes = 0;
  for (const resolve of outputCreditWaiters.splice(0)) resolve();
}

async function stop() {
  const activeStream = stream;
  stream = null;
  if (activeStream != null && activeStream.status !== "closed") {
    activeStream.abort(new Error("Соединение закрыто пользователем"));
  }
  resetOutputCredit();
  await receiveTask?.catch(() => {});
  receiveTask = null;
  await node?.stop();
  node = null;
}

workerScope.addEventListener("message", (event: MessageEvent<WorkerRequest>) => {
  const { id, action, payload = {} } = event.data;
  if (action === "ackData") {
    acknowledgeOutput(Number(payload.bytes));
    return;
  }
  void (async () => {
    try {
      let value: unknown;
      if (action === "start") value = await startNode(Boolean(payload.privateDiscovery));
      else if (action === "connect") value = await connectWithTimeout(payload);
      else if (action === "send") value = await send(payload.bytes as ArrayBuffer);
      else if (action === "closeWrite") {
        if (stream != null && stream.writeStatus === "writable") await stream.close();
        postLog("EOF отправлен; канал остаётся открытым для приёма");
      } else if (action === "stop") value = await stop();
      workerScope.postMessage({ type: "result", id, value });
    } catch (error) {
      workerScope.postMessage({
        type: "error",
        id,
        message: error instanceof Error ? error.message : String(error),
      });
    }
  })();
});

export {};
