import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

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
