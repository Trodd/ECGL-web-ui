import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App.jsx";
import { initLeague } from "./league";
import { initRefreshGuard } from "./refreshGuard";

// Attach the active league (main/rookie) to every API request.
initLeague();

// Force a refresh when backend data changes while the app is open.
initRefreshGuard();

createRoot(document.getElementById("root")).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>
);

if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js', { updateViaCache: 'none' })
      .then((reg) => {
        // Proactively check for updates.
        reg.update().catch(() => { });
      })
      .catch(err => console.log("SW registration failed", err));
  });
}
