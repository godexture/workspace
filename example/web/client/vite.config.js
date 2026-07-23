import { readFile } from "node:fs/promises";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig(({ isSsrBuild }) => ({
    plugins: [react()],
    resolve: {
        tsconfigPaths: true,
    },
    server: {
        port: 5173,
        proxy: {
            "/api": "http://localhost:8787",
        },
    },
}));
