import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { registerSW } from "virtual:pwa-register";
import App from "../app/page";
import "../app/globals.css";

function githubPagesHttpsUrl(location: Location) {
  if (location.protocol !== "http:" || !location.hostname.endsWith(".github.io")) return null;
  const target = new URL(location.href);
  target.protocol = "https:";
  return target.href;
}

const secureUrl = githubPagesHttpsUrl(window.location);

if (secureUrl != null) {
  window.location.replace(secureUrl);
} else {
  registerSW({
    immediate: true,
    onRegisteredSW(_url, registration) {
      if (registration != null) {
        window.setInterval(() => void registration.update(), 60 * 60 * 1000);
      }
    },
  });

  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
