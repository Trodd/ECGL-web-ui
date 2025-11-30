import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => ({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target:
          mode === "development"
            ? "http://localhost:8080"
            : "https://ecgleague.com",
        changeOrigin: true,
        secure: false,
        headers: mode === "development" ? {} : { Host: "ecgleague.com" },
      },
    },
  },

  build: {
    rollupOptions: {
      // Optional: better chunking
      output: {
        manualChunks: undefined
      }
    }
  },

  // 👇 SERVE SW.JS WITH CORRECT MIME TYPE
  publicDir: "public",
}));
