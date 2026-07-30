import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

export type BrowserTerminalHandle = {
  clear: () => void;
  focus: () => void;
  write: (bytes: Uint8Array) => Promise<void>;
};

type BrowserTerminalProps = {
  ariaLabel: string;
  connected: boolean;
  onExit: () => void;
  onInput: (bytes: Uint8Array) => void;
  onResize: (columns: number, rows: number) => void;
};

export const BrowserTerminal = forwardRef<BrowserTerminalHandle, BrowserTerminalProps>(function BrowserTerminal({
  ariaLabel,
  connected,
  onExit,
  onInput,
  onResize,
}, ref) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const connectedRef = useRef(connected);
  const callbacksRef = useRef({ onExit, onInput, onResize });
  const escapeRef = useRef(false);

  connectedRef.current = connected;
  callbacksRef.current = { onExit, onInput, onResize };

  useImperativeHandle(ref, () => ({
    clear: () => {
      terminalRef.current?.reset();
      terminalRef.current?.clear();
    },
    focus: () => terminalRef.current?.focus(),
    write: (bytes) => {
      const terminal = terminalRef.current;
      if (terminal == null) return Promise.resolve();
      return new Promise<void>((resolve) => terminal.write(bytes, resolve));
    },
  }), []);

  useEffect(() => {
    const container = containerRef.current;
    if (container == null) return;

    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: "block",
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.25,
      scrollback: 5000,
      theme: {
        background: "#11140f",
        foreground: "#d9f2a1",
        cursor: "#cbff4a",
        cursorAccent: "#11140f",
        selectionBackground: "#536524",
      },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(container);
    terminalRef.current = terminal;
    fitRef.current = fit;

    const inputSubscription = terminal.onData((value) => {
      if (!connectedRef.current) return;
      let outbound = "";
      for (const character of value) {
        if (escapeRef.current) {
          escapeRef.current = false;
          if (character === "q") {
            callbacksRef.current.onExit();
            return;
          }
          outbound += `\x05${character}`;
        } else if (character === "\x05") {
          escapeRef.current = true;
        } else {
          outbound += character;
        }
      }
      if (outbound) callbacksRef.current.onInput(new TextEncoder().encode(outbound));
    });
    const resizeSubscription = terminal.onResize(({ cols, rows }) => {
      if (connectedRef.current) callbacksRef.current.onResize(cols, rows);
    });
    const resizeObserver = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {
        // A transient zero-sized element can occur during responsive layout changes.
      }
    });
    resizeObserver.observe(container);
    requestAnimationFrame(() => fit.fit());

    return () => {
      resizeObserver.disconnect();
      inputSubscription.dispose();
      resizeSubscription.dispose();
      terminal.dispose();
      terminalRef.current = null;
      fitRef.current = null;
    };
  }, []);

  useEffect(() => {
    if (!connected) return;
    requestAnimationFrame(() => {
      fitRef.current?.fit();
      terminalRef.current?.focus();
      const terminal = terminalRef.current;
      if (terminal != null) callbacksRef.current.onResize(terminal.cols, terminal.rows);
    });
  }, [connected]);

  return (
    <div
      className="browser-terminal"
      ref={containerRef}
      role="application"
      aria-label={ariaLabel}
    />
  );
});

export default BrowserTerminal;
