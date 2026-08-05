import { copyFile, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import type { Plugin } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

const webRoot = dirname(fileURLToPath(import.meta.url));
const presentationFiles = [
  "p2p-netcat-product-technical-overview-en-mobile.pdf",
  "p2p-netcat-product-technical-overview-en.pdf",
  "p2p-netcat-product-technical-overview-en.pptx",
  "p2p-netcat-product-technical-overview-ru-mobile.pdf",
  "p2p-netcat-product-technical-overview-ru.pdf",
  "p2p-netcat-product-technical-overview-ru.pptx",
];

function copyPresentationArtifacts(): Plugin {
  return {
    name: "copy-presentation-artifacts",
    apply: "build",
    async closeBundle() {
      const targetDirectory = resolve(webRoot, "dist/docs");
      await mkdir(targetDirectory, { recursive: true });
      await Promise.all(
        presentationFiles.map((file) =>
          copyFile(resolve(webRoot, "../docs", file), resolve(targetDirectory, file)),
        ),
      );
    },
  };
}

function normalizeBase(value: string | undefined) {
  if (!value || value === "/") return "/";
  return `/${value.replace(/^\/+|\/+$/g, "")}/`;
}

export default defineConfig(() => {
  const base = normalizeBase(process.env.VITE_BASE_PATH);
  const asset = (name: string) => `${base}${name}`;

  return {
    base,
    server: {
      host: true,
      allowedHosts: true as const
    },
    preview: {
      host: true,
      allowedHosts: true as const
    },
    plugins: [
      react(),
      VitePWA({
        registerType: "autoUpdate",
        injectRegister: null,
        includeAssets: ["icon-192.png", "icon-512.png", "og.png", "og-en.png"],
        manifest: {
          id: base,
          name: "p2p-netcat web",
          short_name: "p2p-nc",
          description: "An encrypted browser P2P terminal addressed by PeerId",
          lang: "en",
          start_url: base,
          scope: base,
          display: "standalone",
          display_override: ["window-controls-overlay", "standalone", "minimal-ui"],
          orientation: "any",
          background_color: "#f1f0e9",
          theme_color: "#11130f",
          categories: ["utilities", "developer tools"],
          icons: [
            { src: asset("icon-192.png"), sizes: "192x192", type: "image/png", purpose: "any maskable" },
            { src: asset("icon-512.png"), sizes: "512x512", type: "image/png", purpose: "any maskable" },
          ],
        },
        workbox: {
          cleanupOutdatedCaches: true,
          clientsClaim: true,
          skipWaiting: true,
          navigateFallback: asset("index.html"),
          globPatterns: ["**/*.{html,js,css,json,png,webmanifest}"],
          runtimeCaching: [
            {
              urlPattern: ({ request }) => request.mode === "navigate",
              handler: "NetworkFirst",
              options: {
                cacheName: "p2p-netcat-pages",
                networkTimeoutSeconds: 3,
              },
            },
          ],
        },
        devOptions: { enabled: true },
      }),
      copyPresentationArtifacts(),
    ],
    build: {
      target: "es2022",
      sourcemap: true,
    },
    worker: {
      format: "es" as const,
    },
  };
});
