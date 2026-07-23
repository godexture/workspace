// Copies @godexture/js's worker-mode runtime assets into public/wasm so
// Vite serves them at a stable, unhashed path. worker.js fetches
// wasm_exec.js and main.wasm by plain relative filename, so all three must
// live side by side at a fixed URL -- Vite's normal (hashed) asset pipeline
// would break that, which is why this bypasses it via public/.
import * as fs from "node:fs";
import * as path from "node:path";

const srcDir = path.join(import.meta.dir, "../node_modules/@godexture/js/dist");
const destDir = path.join(import.meta.dir, "../public/wasm");

const files = ["worker.js", "wasm_exec.js", "main.wasm"];

for (const name of files) {
    if (!fs.existsSync(path.join(srcDir, name))) {
        console.error(
            `Missing ${name} in ${srcDir}.\n` +
                "Build @godexture/js first: (cd ../../../bindings/js && bun run build)",
        );
        process.exit(1);
    }
}

fs.mkdirSync(destDir, { recursive: true });
for (const name of files) {
    fs.copyFileSync(path.join(srcDir, name), path.join(destDir, name));
}
console.log(`Copied wasm assets to ${destDir}`);
