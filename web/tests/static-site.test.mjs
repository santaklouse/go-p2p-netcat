import assert from "node:assert/strict";
import { access, readFile, readdir } from "node:fs/promises";
import test from "node:test";

test("builds as a static PWA without a server bundle", async () => {
  const [html, manifest, files, networkConfig] = await Promise.all([
    readFile(new URL("../dist/index.html", import.meta.url), "utf8"),
    readFile(new URL("../dist/manifest.webmanifest", import.meta.url), "utf8"),
    readdir(new URL("../dist/", import.meta.url)),
    readFile(new URL("../dist/network-config.json", import.meta.url), "utf8"),
  ]);

  assert.match(html, /p2p-netcat web/);
  assert.match(html, /manifest\.webmanifest/);
  assert.match(html, /og-en\.png/);
  assert.doesNotMatch(html, /%BASE_URL%/);
  assert.ok(files.includes("sw.js"));
  assert.ok(files.includes("og-en.png"));
  assert.ok(files.includes("og.png"));
  assert.deepEqual(JSON.parse(networkConfig).delegatedRouting, ["https://delegated-ipfs.dev/routing/v1"]);
  const parsedManifest = JSON.parse(manifest);
  assert.equal(parsedManifest.display, "standalone");
  assert.equal(parsedManifest.lang, "en");
  assert.equal(parsedManifest.start_url, parsedManifest.scope);
  assert.ok(parsedManifest.icons.every((icon) => icon.src.startsWith(parsedManifest.scope)));
  await assert.rejects(access(new URL("../dist/server/", import.meta.url)));
});

test("runs the network stack in a dedicated Web Worker", async () => {
  const [worker, client, nativeWebRtc, core, signaling, endpoint, page, localization, terminal, main, styles, goModule, webPackage] = await Promise.all([
    readFile(new URL("../app/p2p.worker.ts", import.meta.url), "utf8"),
    readFile(new URL("../app/p2p-client.ts", import.meta.url), "utf8"),
    readFile(new URL("../app/native-webrtc-client.ts", import.meta.url), "utf8"),
    readFile(new URL("../../packages/core/src/index.js", import.meta.url), "utf8"),
    readFile(new URL("../../packages/core/src/signaling.js", import.meta.url), "utf8"),
    readFile(new URL("../../packages/core/src/native-endpoint.js", import.meta.url), "utf8"),
    readFile(new URL("../app/page.tsx", import.meta.url), "utf8"),
    readFile(new URL("../app/i18n.ts", import.meta.url), "utf8"),
    readFile(new URL("../app/browser-terminal.tsx", import.meta.url), "utf8"),
    readFile(new URL("../src/main.tsx", import.meta.url), "utf8"),
    readFile(new URL("../app/globals.css", import.meta.url), "utf8"),
    readFile(new URL("../../go.mod", import.meta.url), "utf8"),
    readFile(new URL("../package.json", import.meta.url), "utf8"),
  ]);

  assert.match(worker, /p2p-netcat-core/);
  assert.doesNotMatch(worker, /const PROTOCOL_PREFIX/);
  assert.match(core, /\/p2p-netcat\/1\.0\.0/);
  assert.match(core, /class PtyFrameDecoder/);
  assert.match(core, /class WebRtcStream/);
  assert.match(core, /createWebRtcActionHub/);
  assert.match(core, /encodePtyData/);
  assert.match(core, /encodePtyResize/);
  assert.match(worker, /circuitRelayTransport\(\)/);
  assert.match(worker, /webSockets\(\)/);
  assert.match(worker, /webTransport\(\)/);
  assert.match(worker, /delegated-ipfs\.dev\/routing\/v1/);
  assert.match(worker, /kadDHT\(/);
  assert.match(worker, /pubsubPeerDiscovery\(/);
  assert.match(worker, /gossipsub\(/);
  assert.match(worker, /PUBSUB_DISCOVERY_TOPIC/);
  assert.match(worker, /privateDiscovery[\s\S]*?pubsubPeerDiscovery/);
  assert.match(client, /privateDiscovery: pairingToken\.trim\(\)\.length > 0/);
  assert.match(worker, /indexedDB\.open/);
  assert.match(worker, /workerScope\.crypto\?\.subtle/);
  assert.match(worker, /Откройте приложение по HTTPS/);
  assert.match(client, /new Worker\(new URL/);
  assert.match(client, /Promise\.any/);
  assert.match(nativeWebRtc, /connectNativeWebRtc/);
  assert.match(nativeWebRtc, /createNostrSignalingSession/);
  assert.match(nativeWebRtc, /createTorrentSignalingSession/);
  assert.match(nativeWebRtc, /verifyWebRtcAuthResponse/);
  assert.match(nativeWebRtc, /defaultRtcConfiguration/);
  assert.doesNotMatch(client, /trystero/i);
  assert.doesNotMatch(goModule, /trystero/i);
  assert.doesNotMatch(webPackage, /@trystero-p2p/);
  assert.match(signaling, /class NostrSignalingSession/);
  assert.match(signaling, /class TorrentSignalingSession/);
  assert.match(signaling, /trickleIce: true/);
  assert.match(signaling, /trickleIce: false/);
  assert.match(endpoint, /connectNativeWebRtc/);
  assert.match(endpoint, /startNativeWebRtcListener/);
  assert.match(core, /stun:stun\.l\.google\.com:19302/);
  assert.match(core, /stun:stun\.internetcalls\.com:3478/);
  assert.match(client, /transfer/);
  assert.match(client, /ackData/);
  assert.match(worker, /OUTPUT_HIGH_WATER_MARK/);
  assert.match(worker, /unacknowledgedOutputBytes/);
  assert.match(worker, /connectWithTimeout/);
  assert.match(worker, /libp2p не установил соединение за/);
  assert.match(terminal, /terminal\.write\(bytes, resolve\)/);
  assert.match(core, /flowWindowBytes/);
  assert.match(core, /ack:/);
  assert.match(core, /peerDisconnected/);
  assert.match(core, /peerReconnected/);
  assert.match(core, /WEBRTC_RECONNECT_GRACE_MS/);
  assert.match(nativeWebRtc, /WebRTC-канал восстановлен/);
  assert.match(page, /reconnecting/);
  assert.match(localization, /Optional · automatic discovery is enabled/);
  assert.match(localization, /Необязательно · используется автопоиск/);
  assert.match(localization, /languageLink: "Русская версия"/);
  assert.match(localization, /languageLink: "English version"/);
  assert.match(localization, /socialImageAlt: "p2p-netcat web — a terminal between two peers"/);
  assert.match(localization, /socialImageAlt: "p2p-netcat web — терминал между двумя узлами"/);
  assert.match(localization, /get\("lang"\) === "ru"/);
  assert.match(localization, /export function localizeDiagnostic/);
  assert.match(localization, /Starting the network stack in a Web Worker/);
  assert.match(page, /getLanguageUrl\(alternateLanguage\)/);
  assert.match(page, /language === "ru" \? "og\.png" : "og-en\.png"/);
  assert.match(page, /localizeDiagnostic\(text, language\)/);
  assert.doesNotMatch(page, /!targetPeerId \|\| !relayAddress/);
  assert.match(main, /location\.hostname\.endsWith\("\.github\.io"\)/);
  assert.match(main, /window\.location\.replace\(secureUrl\)/);
  assert.match(localization, /Show sent text/);
  assert.match(localization, /Показывать отправленное/);
  assert.match(page, /entry\.direction === "received"/);
  assert.match(page, /p2p-netcat-show-sent/);
  assert.match(page, /go install github\.com\/santaklouse\/go-p2p-netcat\/cmd\/p2p-nc@latest/);
  assert.match(page, /INSTALLATION\.RU\.md/);
  assert.match(page, /navigator\.clipboard\.writeText/);
  assert.match(localization, /Interactive PTY/);
  assert.match(localization, /Интерактивный PTY/);
  assert.match(page, /p2p-netcat-interactive/);
  assert.doesNotMatch(page, /p2p-netcat-native-only/);
  assert.doesNotMatch(page, /get\("native-only"\)/);
  assert.doesNotMatch(page, /localStorage\.(?:setItem|getItem)\([^)]*pairing/i);
  assert.match(page, /lazy\(\(\) => import\("\.\/browser-terminal"\)\)/);
  assert.match(client, /PtyFrameDecoder/);
  assert.match(client, /encodePtyData/);
  assert.match(client, /encodePtyResize/);
  assert.match(terminal, /@xterm\/xterm/);
  assert.match(terminal, /@xterm\/addon-fit/);
  assert.match(terminal, /character === "q"/);
  assert.match(styles, /\.terminal-echo-toggle/);
  assert.match(styles, /\.terminal-sent/);
  assert.match(styles, /\.browser-terminal/);
  assert.match(styles, /\.install-strip/);
});
