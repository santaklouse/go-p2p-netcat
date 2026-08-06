import { FormEvent, Suspense, lazy, useEffect, useRef, useState } from "react";
import type { BrowserTerminalHandle } from "./browser-terminal";
import { getLanguageUrl, getPageLanguage, localizeDiagnostic, uiText } from "./i18n";
import { BrowserP2PClient } from "./p2p-client";
import { decodePairingToken } from "p2p-netcat-core";

const BrowserTerminal = lazy(() => import("./browser-terminal"));
const INSTALL_COMMAND = "GOTOOLCHAIN=auto CGO_ENABLED=0 go install github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest";
const INSTALLATION_RU_URL = "https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/INSTALLATION.RU.md";
const INSTALLATION_EN_URL = "https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/INSTALLATION.md";
const PRESENTATION_BASE_URL = "https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/docs/";
const PRESENTATION_RU_PDF_URL = `${PRESENTATION_BASE_URL}p2p-netcat-product-technical-overview-ru.pdf`;
const PRESENTATION_EN_PDF_URL = `${PRESENTATION_BASE_URL}p2p-netcat-product-technical-overview-en.pdf`;
const PRESENTATION_RU_MOBILE_URL = `${PRESENTATION_BASE_URL}p2p-netcat-product-technical-overview-ru-mobile.pdf`;
const PRESENTATION_EN_MOBILE_URL = `${PRESENTATION_BASE_URL}p2p-netcat-product-technical-overview-en-mobile.pdf`;
const PRESENTATION_RU_PPTX_URL = `${PRESENTATION_BASE_URL}p2p-netcat-product-technical-overview-ru.pptx`;
const PRESENTATION_EN_PPTX_URL = `${PRESENTATION_BASE_URL}p2p-netcat-product-technical-overview-en.pptx`;
const PROJECT_BADGES = [
  {
    href: "https://github.com/santaklouse/go-p2p-netcat/actions/workflows/ci.yml",
    src: "https://github.com/santaklouse/go-p2p-netcat/actions/workflows/ci.yml/badge.svg?branch=main",
    alt: "CI workflow status",
  },
  {
    href: "https://santaklouse.github.io/go-p2p-netcat/",
    src: "https://github.com/santaklouse/go-p2p-netcat/actions/workflows/pages.yml/badge.svg?branch=main",
    alt: "PWA deployment status",
  },
  {
    href: "https://github.com/santaklouse/go-p2p-netcat/releases/latest",
    src: "https://img.shields.io/github/v/release/santaklouse/go-p2p-netcat?sort=semver",
    alt: "Latest GitHub release",
  },
  {
    href: "https://pkg.go.dev/github.com/santaklouse/go-p2p-netcat",
    src: "https://pkg.go.dev/badge/github.com/santaklouse/go-p2p-netcat.svg",
    alt: "Go package reference",
  },
  {
    href: "https://goreportcard.com/report/github.com/santaklouse/go-p2p-netcat",
    src: "https://goreportcard.com/badge/github.com/santaklouse/go-p2p-netcat",
    alt: "Go Report Card grade",
  },
  {
    href: "https://github.com/santaklouse/go-p2p-netcat/pkgs/container/go-p2p-netcat",
    src: "https://img.shields.io/badge/GHCR-published-2496ED?logo=docker&logoColor=white",
    alt: "Container published to GitHub Container Registry",
  },
  {
    href: "https://github.com/santaklouse/go-p2p-netcat/blob/main/LICENSE",
    src: "https://img.shields.io/github/license/santaklouse/go-p2p-netcat",
    alt: "Repository license",
  },
] as const;

type ConnectionState = "idle" | "starting" | "connecting" | "connected" | "reconnecting" | "closed" | "error";
type LogEntry = { id: number; time: string; message: string; kind: "info" | "success" | "error" };
type TerminalEntry = { id: number; direction: "sent" | "received"; text: string };

function formatBytes(value: number, units: readonly [string, string, string]) {
  if (value < 1024) return `${value} ${units[0]}`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} ${units[1]}`;
  return `${(value / 1024 / 1024).toFixed(1)} ${units[2]}`;
}

export default function Home() {
  const [language] = useState(getPageLanguage);
  const copy = uiText[language];
  const alternateLanguage = language === "en" ? "ru" : "en";
  const [targetPeerId, setTargetPeerId] = useState("");
  const [pairingToken, setPairingToken] = useState("");
  const [relayAddress, setRelayAddress] = useState("");
  const [logicalPort, setLogicalPort] = useState(31337);
  const [timeout, setTimeout] = useState(30);
  const [interactive, setInteractive] = useState(false);
  const [connectionState, setConnectionState] = useState<ConnectionState>("idle");
  const [localPeerId, setLocalPeerId] = useState("");
  const [message, setMessage] = useState("");
  const [terminalEntries, setTerminalEntries] = useState<TerminalEntry[]>([]);
  const [showSentText, setShowSentText] = useState(false);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [receivedBytes, setReceivedBytes] = useState(0);
  const [sentBytes, setSentBytes] = useState(0);
  const [fileProgress, setFileProgress] = useState("");
  const [installCopied, setInstallCopied] = useState(false);
  const clientRef = useRef<BrowserP2PClient | null>(null);
  const receivedChunks = useRef<ArrayBuffer[]>([]);
  const decoder = useRef(new TextDecoder());
  const transcriptRef = useRef<HTMLPreElement | null>(null);
  const browserTerminalRef = useRef<BrowserTerminalHandle | null>(null);
  const terminalSequence = useRef(0);

  const addLog = (text: string, kind: "info" | "success" | "error" = "info") => {
    setLogs((current) => [
      ...current.slice(-99),
      {
        id: Date.now() + Math.random(),
        time: new Date().toLocaleTimeString(copy.locale),
        message: localizeDiagnostic(text, language),
        kind,
      },
    ]);
  };

  useEffect(() => {
    const socialImageName = language === "ru" ? "og.png" : "og-en.png";
    const assetBaseUrl = new URL(import.meta.env.BASE_URL, window.location.origin);
    const socialImageUrl = new URL(socialImageName, assetBaseUrl).href;

    document.documentElement.lang = language;
    document.title = copy.documentTitle;
    document.querySelector('meta[name="description"]')?.setAttribute("content", copy.documentDescription);
    document.querySelector('meta[property="og:description"]')?.setAttribute("content", copy.documentDescription);
    document.querySelector('meta[property="og:image"]')?.setAttribute("content", socialImageUrl);
    document.querySelector('meta[property="og:image:alt"]')?.setAttribute("content", copy.socialImageAlt);
    document.querySelector('meta[name="twitter:image"]')?.setAttribute("content", socialImageUrl);
    document.querySelector('meta[name="twitter:image:alt"]')?.setAttribute("content", copy.socialImageAlt);
  }, [copy.documentDescription, copy.documentTitle, copy.socialImageAlt, language]);

  useEffect(() => {
    const savedRelay = window.localStorage.getItem("p2p-netcat-relay");
    if (savedRelay) setRelayAddress(savedRelay);
    setShowSentText(window.localStorage.getItem("p2p-netcat-show-sent") === "true");
    setInteractive(window.localStorage.getItem("p2p-netcat-interactive") === "true");
    return () => {
      void clientRef.current?.stop();
    };
  }, []);

  useEffect(() => {
    transcriptRef.current?.scrollTo({ top: transcriptRef.current.scrollHeight, behavior: "smooth" });
  }, [terminalEntries, showSentText]);

  const connect = async (event: FormEvent) => {
    event.preventDefault();
    if (connectionState === "connecting" || connectionState === "starting" || connectionState === "reconnecting") return;

    await clientRef.current?.stop();
    clientRef.current = null;
    setConnectionState("starting");
    setTerminalEntries([]);
    setReceivedBytes(0);
    setSentBytes(0);
    receivedChunks.current = [];
    decoder.current = new TextDecoder();
    terminalSequence.current = 0;
    browserTerminalRef.current?.clear();

    const client = new BrowserP2PClient({
      onData: async (bytes) => {
        setReceivedBytes((value) => value + bytes.byteLength);
        if (interactive) {
          await browserTerminalRef.current?.write(bytes);
          return;
        }
        receivedChunks.current.push(bytes.slice().buffer as ArrayBuffer);
        const text = decoder.current.decode(bytes, { stream: true });
        if (text) {
          setTerminalEntries((current) => [
            ...current,
            { id: ++terminalSequence.current, direction: "received", text },
          ]);
        }
      },
      onLog: addLog,
      onReconnecting: () => setConnectionState("reconnecting"),
      onReconnected: () => setConnectionState("connected"),
      onClosed: () => setConnectionState((state) => state === "error" ? state : "closed"),
    });
    clientRef.current = client;

    try {
      setLocalPeerId(await client.start(pairingToken));
      setConnectionState("connecting");
      if (relayAddress.trim()) window.localStorage.setItem("p2p-netcat-relay", relayAddress.trim());
      else window.localStorage.removeItem("p2p-netcat-relay");
      window.localStorage.setItem("p2p-netcat-interactive", String(interactive));
      await client.connect(targetPeerId, logicalPort, relayAddress, interactive, timeout, pairingToken);
      setConnectionState("connected");
      if (interactive) addLog(copy.ptyEnabled, "success");
    } catch (error) {
      const text = error instanceof Error ? error.message : String(error);
      addLog(text, "error");
      setConnectionState("error");
      await client.stop();
      clientRef.current = null;
    }
  };

  const disconnect = async () => {
    await clientRef.current?.stop();
    clientRef.current = null;
    setConnectionState("closed");
    addLog(copy.connectionClosed);
  };

  const exitInteractive = async () => {
    try {
      await clientRef.current?.closeWrite();
      addLog(copy.ptyEofSent);
    } catch (error) {
      addLog(error instanceof Error ? error.message : String(error), "error");
      await disconnect();
    }
  };

  const sendMessage = async () => {
    if (!message || connectionState !== "connected") return;
    const payload = `${message}\n`;
    const entryId = ++terminalSequence.current;
    setTerminalEntries((current) => [...current, { id: entryId, direction: "sent", text: payload }]);
    try {
      await clientRef.current?.sendText(payload);
      setSentBytes((value) => value + new TextEncoder().encode(payload).byteLength);
      setMessage("");
    } catch (error) {
      setTerminalEntries((current) => current.filter((entry) => entry.id !== entryId));
      addLog(error instanceof Error ? error.message : String(error), "error");
    }
  };

  const sendTerminalInput = (bytes: Uint8Array) => {
    void clientRef.current?.send(bytes).then(() => {
      setSentBytes((value) => value + bytes.byteLength);
    }).catch((error) => {
      addLog(error instanceof Error ? error.message : String(error), "error");
    });
  };

  const resizeTerminal = (columns: number, rows: number) => {
    void clientRef.current?.resize(columns, rows).catch((error) => {
      addLog(error instanceof Error ? error.message : String(error), "error");
    });
  };

  const sendFile = async (file: File | undefined) => {
    if (!file || connectionState !== "connected") return;
    setFileProgress(`${copy.sendingFile} ${file.name}: 0 / ${formatBytes(file.size, copy.byteUnits)}`);
    try {
      await clientRef.current?.sendFile(file, (sent, total) => {
        setFileProgress(
          `${copy.sendingFile} ${file.name}: ${formatBytes(sent, copy.byteUnits)} / ${formatBytes(total, copy.byteUnits)}`,
        );
      });
      setSentBytes((value) => value + file.size);
      setFileProgress(`${file.name} ${copy.fileSentSuffix} · ${formatBytes(file.size, copy.byteUnits)}`);
      addLog(`${copy.fileSentPrefix} ${file.name} ${copy.fileSentSuffix}`, "success");
    } catch (error) {
      setFileProgress("");
      addLog(error instanceof Error ? error.message : String(error), "error");
    }
  };

  const downloadReceived = () => {
    const blob = new Blob(receivedChunks.current, { type: "application/octet-stream" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `p2p-netcat-${new Date().toISOString().replaceAll(":", "-")}.bin`;
    link.click();
    URL.revokeObjectURL(url);
  };

  const copyInstallCommand = async () => {
    try {
      await navigator.clipboard.writeText(INSTALL_COMMAND);
      setInstallCopied(true);
      window.setTimeout(() => setInstallCopied(false), 2_000);
    } catch (error) {
      addLog(
        `${copy.copyFailed}: ${error instanceof Error ? error.message : String(error)}`,
        "error",
      );
    }
  };

  const updatePairingToken = (value: string) => {
    setPairingToken(value);
    const normalized = value.trim();
    if (!normalized) return;
    try {
      const decoded = decodePairingToken(normalized);
      setTargetPeerId(decoded.peerId);
      setLogicalPort(decoded.service);
    } catch {
      // Validation is shown when the user submits the form.
    }
  };

  const connected = connectionState === "connected";
  const sessionActive = connected || connectionState === "reconnecting";
  const visibleTerminalEntries = showSentText
    ? terminalEntries
    : terminalEntries.filter((entry) => entry.direction === "received");

  return (
    <main className="app-shell">
      <header className="topbar">
        <a className="brand" href="#top" aria-label={copy.brandAria}>
          <span className="brand-mark" aria-hidden="true">p2p</span>
          <span>netcat<span className="brand-cursor">_</span></span>
        </a>
        <div className="topbar-actions">
          <a
            className="language-link"
            href={getLanguageUrl(alternateLanguage)}
            hrefLang={alternateLanguage}
            lang={alternateLanguage}
            aria-label={copy.languageLinkAria}
          >
            {copy.languageLink}
          </a>
          <div className={`connection-pill state-${connectionState}`}>
            <span className="status-dot" aria-hidden="true" />
            {copy.stateLabels[connectionState]}
          </div>
        </div>
      </header>

      <section className="hero" id="top">
        <div>
          <p className="eyebrow">{copy.eyebrow}</p>
          <h1>{copy.heroLineOne}<br /><span>{copy.heroLineTwo}</span></h1>
        </div>
        <p className="hero-copy">{copy.heroCopy}</p>
      </section>

      <nav className="project-badges" aria-label={copy.projectBadgesAria}>
        {PROJECT_BADGES.map((badge) => (
          <a
            key={badge.href}
            className="project-badge"
            href={badge.href}
            target="_blank"
            rel="noreferrer"
          >
            <img src={badge.src} alt={badge.alt} height="20" />
          </a>
        ))}
      </nav>

      <section className="install-strip" aria-labelledby="install-title">
        <div className="install-heading">
          <span className="step-number">CLI</span>
          <div>
            <p>Node.js 22+</p>
            <h2 id="install-title">{copy.installTitle}</h2>
          </div>
        </div>
        <div className="install-command">
          <code>{INSTALL_COMMAND}</code>
          <button type="button" onClick={() => void copyInstallCommand()}>
            {installCopied ? copy.copied : copy.copy}
          </button>
        </div>
        <nav className="install-links" aria-label={copy.installationNavAria}>
          <a
            href={language === "en" ? INSTALLATION_EN_URL : INSTALLATION_RU_URL}
            hrefLang={language}
            target="_blank"
            rel="noreferrer"
          >
            {copy.installationGuide}
          </a>
          <a
            href={language === "en" ? INSTALLATION_RU_URL : INSTALLATION_EN_URL}
            hrefLang={alternateLanguage}
            target="_blank"
            rel="noreferrer"
          >
            {copy.alternateInstallationGuide}
          </a>
          <a
            href={language === "ru" ? PRESENTATION_RU_PDF_URL : PRESENTATION_EN_PDF_URL}
            hrefLang={language}
            target="_blank"
            rel="noreferrer"
          >
            {copy.presentationGuide}
          </a>
          <a
            href={language === "ru" ? PRESENTATION_RU_MOBILE_URL : PRESENTATION_EN_MOBILE_URL}
            hrefLang={language}
            target="_blank"
            rel="noreferrer"
          >
            {copy.presentationMobileGuide}
          </a>
          <a
            href={language === "ru" ? PRESENTATION_RU_PPTX_URL : PRESENTATION_EN_PPTX_URL}
            hrefLang={language}
            target="_blank"
            rel="noreferrer"
          >
            {copy.presentationSourceGuide}
          </a>
        </nav>
      </section>

      <section className="workspace" aria-label={copy.workspaceAria}>
        <aside className="connection-panel">
          <div className="panel-heading">
            <span className="step-number">01</span>
            <div><p>{copy.route}</p><h2>{copy.newConnection}</h2></div>
          </div>

          <form onSubmit={connect} className="connection-form">
            <label>
              <span>{copy.serverPeerId}</span>
              <input
                value={targetPeerId}
                onChange={(event) => setTargetPeerId(event.target.value)}
                spellCheck={false}
                autoComplete="off"
                required
                disabled={sessionActive}
                aria-describedby="peer-help"
              />
              <small id="peer-help">
                {copy.peerHelp} <code>p2p-nc -l</code>
              </small>
            </label>

            <details className="relay-options">
              <summary>
                <span>{copy.advancedConnection}</span>
                <small>{pairingToken ? copy.privateDiscovery : relayAddress ? copy.manualRoute : copy.automaticDiscovery}</small>
              </summary>
              <label>
                <span>{copy.pairingToken}</span>
                <textarea
                  value={pairingToken}
                  onChange={(event) => updatePairingToken(event.target.value)}
                  spellCheck={false}
                  autoComplete="off"
                  disabled={sessionActive}
                  placeholder="pnc1_…"
                  aria-describedby="pairing-token-help"
                  rows={3}
                />
                <small id="pairing-token-help">{copy.pairingTokenHelp}</small>
              </label>
              <label>
                <span>WebSocket relay multiaddr</span>
                <input
                  value={relayAddress}
                  onChange={(event) => setRelayAddress(event.target.value)}
                  spellCheck={false}
                  autoComplete="off"
                  disabled={sessionActive}
                  placeholder={copy.relayPlaceholder}
                  aria-describedby="relay-help"
                />
                <small id="relay-help">
                  {copy.relayHelp} <code>/dns4/relay.example/tcp/443/wss/p2p/…</code>
                </small>
              </label>
            </details>

            <label className="port-field">
              <span>{copy.logicalPort}</span>
              <input
                type="number"
                min="1"
                max="65535"
                value={logicalPort}
                onChange={(event) => setLogicalPort(Number(event.target.value))}
                required
                disabled={sessionActive}
              />
            </label>

            <label className="timeout-field">
              <span>{copy.timeout}</span>
              <input
                  type="number"
                  min="5"
                  max="65535"
                  value={timeout}
                  onChange={(event) => setTimeout(Number(event.target.value))}
                  required
                  disabled={sessionActive}
              />
            </label>

            <label className="interactive-mode">
              <input
                type="checkbox"
                checked={interactive}
                onChange={(event) => setInteractive(event.target.checked)}
                disabled={connectionState === "starting" || connectionState === "connecting" || sessionActive}
              />
              <span>
                {copy.interactivePty} <code>-i</code>
                <small>
                  {copy.interactiveHelp} <code>p2p-nc -l -i</code>
                </small>
              </span>
            </label>

            {!sessionActive ? (
              <button className="primary-button" type="submit" disabled={!targetPeerId || connectionState === "starting" || connectionState === "connecting"}>
                <span>{copy.connect}</span><span aria-hidden="true">↗</span>
              </button>
            ) : (
              <button className="secondary-button danger" type="button" onClick={disconnect}>{copy.disconnect}</button>
            )}
          </form>

          <div className="identity-card">
            <span>{copy.browserPeerId}</span>
            <code>{localPeerId || copy.peerIdOnConnect}</code>
          </div>

          <div className="security-note">
            <span className="lock-icon" aria-hidden="true">◆</span>
            <p>
              <strong>{copy.endToEndEncryption}</strong>
              {pairingToken ? copy.pairingSecurityNote : copy.securityNote}
            </p>
          </div>
        </aside>

        <div className="terminal-panel">
          <div className="terminal-toolbar">
            <div className="window-dots" aria-hidden="true"><i /><i /><i /></div>
            <span className="terminal-address">p2p://{targetPeerId ? `${targetPeerId}` : "not-connected"}:{logicalPort}</span>
            {interactive ? (
              <span className="pty-mode-label">{copy.ptyExit}</span>
            ) : (
              <label className="terminal-echo-toggle">
                <input
                  type="checkbox"
                  checked={showSentText}
                  onChange={(event) => {
                    const checked = event.target.checked;
                    setShowSentText(checked);
                    window.localStorage.setItem("p2p-netcat-show-sent", String(checked));
                  }}
                />
                <span className="toggle-track" aria-hidden="true"><span /></span>
                <span>{copy.showSentText}</span>
              </label>
            )}
            <div className="traffic-stats">
              <span>↑ {formatBytes(sentBytes, copy.byteUnits)}</span>
              <span>↓ {formatBytes(receivedBytes, copy.byteUnits)}</span>
            </div>
          </div>

          {interactive ? (
            <Suspense fallback={<div className="browser-terminal-loading">{copy.terminalLoading}</div>}>
              <BrowserTerminal
                ref={browserTerminalRef}
                ariaLabel={copy.terminalAria}
                connected={connected}
                onInput={sendTerminalInput}
                onResize={resizeTerminal}
                onExit={() => void exitInteractive()}
              />
            </Suspense>
          ) : (
            <>
              <pre className="terminal-output" ref={transcriptRef} aria-live="polite">
                {visibleTerminalEntries.length > 0
                  ? visibleTerminalEntries.map((entry) => (
                    <span key={entry.id} className={`terminal-${entry.direction}`}>{entry.text}</span>
                  ))
                  : <span className="terminal-empty">{copy.terminalEmpty}</span>}
              </pre>

              <div className="composer">
                <textarea
                  value={message}
                  onChange={(event) => setMessage(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                      event.preventDefault();
                      void sendMessage();
                    }
                  }}
                  disabled={!connected}
                  aria-label={copy.sendDataAria}
                  rows={3}
                />
                <button type="button" className="send-button" disabled={!connected || !message} onClick={() => void sendMessage()}>
                  {copy.send} <kbd>⌘↵</kbd>
                </button>
              </div>
            </>
          )}

          <div className="transfer-bar">
            {!interactive && (
              <label className={`file-button ${!connected ? "disabled" : ""}`}>
                <input type="file" disabled={!connected} onChange={(event) => void sendFile(event.target.files?.[0])} />
                <span aria-hidden="true">＋</span> {copy.sendFile}
              </label>
            )}
            <button type="button" disabled={!connected} onClick={() => void clientRef.current?.closeWrite()}>
              {copy.sendEof}
            </button>
            <button type="button" disabled={receivedBytes === 0} onClick={downloadReceived}>
              {copy.downloadReceived}
            </button>
            {interactive && <span className="pty-help">{copy.ptyHelp}</span>}
            {fileProgress && <span className="file-progress">{fileProgress}</span>}
          </div>
        </div>
      </section>

      <section className="event-log" aria-label={copy.connectionLogAria}>
        <div className="log-header">
          <span className="step-number">02</span>
          <h2>{copy.eventLog}</h2>
          <button type="button" onClick={() => setLogs([])}>{copy.clear}</button>
        </div>
        <div className="log-list">
          {logs.length === 0 ? <p className="empty-log">{copy.noEvents}</p> : logs.map((entry) => (
            <p key={entry.id} className={`log-${entry.kind}`}><time>{entry.time}</time><span>{entry.message}</span></p>
          ))}
        </div>
      </section>

      <footer>
        <p>p2p-netcat web <span>v0.7.0</span></p>
        <p>Delegated Routing · IPFS DHT · WSS · Noise · Yamux</p>
      </footer>
    </main>
  );
}
