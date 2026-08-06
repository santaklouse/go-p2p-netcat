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

test("exposes project status badges and presentation links in the README and PWA", async () => {
  const [
    readme,
    readmeRu,
    architecture,
    architectureRu,
    page,
    localization,
    styles,
    sourceHtml,
    builtHtml,
    presentationRuPptx,
    presentationEnPptx,
    presentationRuPdf,
    presentationEnPdf,
    presentationRuMobile,
    presentationEnMobile,
  ] = await Promise.all([
    readFile(new URL("../../README.md", import.meta.url), "utf8"),
    readFile(new URL("../../README.RU.md", import.meta.url), "utf8"),
    readFile(new URL("../../docs/ARCHITECTURE.md", import.meta.url), "utf8"),
    readFile(new URL("../../docs/ARCHITECTURE.RU.md", import.meta.url), "utf8"),
    readFile(new URL("../app/page.tsx", import.meta.url), "utf8"),
    readFile(new URL("../app/i18n.ts", import.meta.url), "utf8"),
    readFile(new URL("../app/globals.css", import.meta.url), "utf8"),
    readFile(new URL("../index.html", import.meta.url), "utf8"),
    readFile(new URL("../dist/index.html", import.meta.url), "utf8"),
    readFile(new URL("../../docs/p2p-netcat-product-technical-overview-ru.pptx", import.meta.url)),
    readFile(new URL("../../docs/p2p-netcat-product-technical-overview-en.pptx", import.meta.url)),
    readFile(new URL("../../docs/p2p-netcat-product-technical-overview-ru.pdf", import.meta.url)),
    readFile(new URL("../../docs/p2p-netcat-product-technical-overview-en.pdf", import.meta.url)),
    readFile(new URL("../../docs/p2p-netcat-product-technical-overview-ru-mobile.pdf", import.meta.url)),
    readFile(new URL("../../docs/p2p-netcat-product-technical-overview-en-mobile.pdf", import.meta.url)),
  ]);

  for (const document of [readme, readmeRu]) {
    assert.match(document, /actions\/workflows\/ci\.yml\/badge\.svg\?branch=main/);
    assert.match(document, /actions\/workflows\/pages\.yml\/badge\.svg\?branch=main/);
    assert.match(document, /img\.shields\.io\/github\/v\/release/);
    assert.match(document, /pkg\.go\.dev\/badge/);
    assert.match(document, /goreportcard\.com\/badge/);
    assert.match(document, /GHCR-published/);
    assert.match(document, /img\.shields\.io\/github\/license/);
  }
  assert.match(readme, /docs\/p2p-netcat-product-technical-overview-en-mobile\.pdf/);
  assert.match(readme, /docs\/p2p-netcat-product-technical-overview-en\.pdf/);
  assert.match(readme, /docs\/p2p-netcat-product-technical-overview-en\.pptx/);
  assert.match(readmeRu, /docs\/p2p-netcat-product-technical-overview-ru-mobile\.pdf/);
  assert.match(readmeRu, /docs\/p2p-netcat-product-technical-overview-ru\.pdf/);
  assert.match(readmeRu, /docs\/p2p-netcat-product-technical-overview-ru\.pptx/);
  assert.match(architecture, /\(p2p-netcat-product-technical-overview-en-mobile\.pdf\)/);
  assert.match(architecture, /\(p2p-netcat-product-technical-overview-en\.pdf\)/);
  assert.match(architecture, /\(p2p-netcat-product-technical-overview-en\.pptx\)/);
  assert.match(architectureRu, /\(p2p-netcat-product-technical-overview-ru-mobile\.pdf\)/);
  assert.match(architectureRu, /\(p2p-netcat-product-technical-overview-ru\.pdf\)/);
  assert.match(architectureRu, /\(p2p-netcat-product-technical-overview-ru\.pptx\)/);
  const sourceArtifacts = [presentationRuPptx, presentationEnPptx, presentationRuPdf, presentationEnPdf, presentationRuMobile, presentationEnMobile];
  for (const artifact of sourceArtifacts) assert.ok(artifact.byteLength > 0);
  await assert.rejects(access(new URL("../dist/docs/", import.meta.url)));
  assert.equal(presentationRuPdf.subarray(0, 5).toString(), "%PDF-");
  assert.equal(presentationEnPdf.subarray(0, 5).toString(), "%PDF-");
  assert.equal(presentationRuMobile.subarray(0, 5).toString(), "%PDF-");
  assert.equal(presentationEnMobile.subarray(0, 5).toString(), "%PDF-");
  assert.equal(presentationRuPptx.subarray(0, 2).toString(), "PK");
  assert.equal(presentationEnPptx.subarray(0, 2).toString(), "PK");
  assert.match(page, /const PROJECT_BADGES/);
  assert.match(page, /const PRESENTATION_BASE_URL = "https:\/\/raw\.githubusercontent\.com\/santaklouse\/go-p2p-netcat\/main\/docs\/"/);
  assert.match(page, /const PRESENTATION_RU_PDF_URL/);
  assert.match(page, /const PRESENTATION_EN_PDF_URL/);
  assert.match(page, /const PRESENTATION_RU_MOBILE_URL/);
  assert.match(page, /const PRESENTATION_EN_MOBILE_URL/);
  assert.match(page, /const PRESENTATION_RU_PPTX_URL/);
  assert.match(page, /const PRESENTATION_EN_PPTX_URL/);
  assert.match(page, /p2p-netcat-product-technical-overview-ru\.pdf/);
  assert.match(page, /p2p-netcat-product-technical-overview-en\.pdf/);
  assert.match(page, /p2p-netcat-product-technical-overview-ru\.pptx/);
  assert.match(page, /p2p-netcat-product-technical-overview-en\.pptx/);
  assert.match(page, /language === "ru" \? PRESENTATION_RU_PDF_URL : PRESENTATION_EN_PDF_URL/);
  assert.match(page, /language === "ru" \? PRESENTATION_RU_MOBILE_URL : PRESENTATION_EN_MOBILE_URL/);
  assert.match(page, /language === "ru" \? PRESENTATION_RU_PPTX_URL : PRESENTATION_EN_PPTX_URL/);
  assert.match(page, /copy\.presentationGuide/);
  assert.match(page, /copy\.presentationMobileGuide/);
  assert.match(page, /copy\.presentationSourceGuide/);
  assert.match(page, /className="project-badges"/);
  assert.match(page, /aria-label=\{copy\.projectBadgesAria\}/);
  assert.match(page, /alt=\{badge\.alt\}/);
  assert.match(localization, /projectBadgesAria: "Project status and resources"/);
  assert.match(localization, /projectBadgesAria: "Статус и ресурсы проекта"/);
  assert.match(localization, /presentationGuide: "Product & technical deck \(PDF\) ↗"/);
  assert.match(localization, /presentationMobileGuide: "Mobile PDF ↗"/);
  assert.match(localization, /presentationSourceGuide: "PPTX source ↓"/);
  assert.match(localization, /presentationGuide: "Презентация о продукте и архитектуре \(PDF\) ↗"/);
  assert.match(localization, /presentationMobileGuide: "Мобильный PDF ↗"/);
  assert.match(localization, /presentationSourceGuide: "Исходник PPTX ↓"/);
  assert.match(styles, /\.project-badge:focus-visible/);
  for (const html of [sourceHtml, builtHtml]) {
    assert.match(html, /img-src 'self' data: https:\/\/github\.com https:\/\/img\.shields\.io https:\/\/pkg\.go\.dev https:\/\/goreportcard\.com/);
  }
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
  assert.match(client, /if \(pairingToken\.length === 0\)/);
  assert.match(client, /Native WebRTC отключён без pairing token/);
  assert.match(worker, /indexedDB\.open/);
  assert.match(worker, /workerScope\.crypto\?\.subtle/);
  assert.match(worker, /Откройте приложение по HTTPS/);
  assert.match(client, /new Worker\(new URL/);
  assert.match(client, /Promise\.any/);
  assert.match(nativeWebRtc, /connectNativeWebRtc/);
  assert.match(nativeWebRtc, /createNostrSignalingSession/);
  assert.match(nativeWebRtc, /createTorrentSignalingSession/);
  assert.match(nativeWebRtc, /verifyWebRtcAuthResponseV2/);
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
  assert.match(endpoint, /offerSdp: attempt\.offerSdp/);
  assert.match(endpoint, /answerSdp: attempt\.answerSdp/);
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
