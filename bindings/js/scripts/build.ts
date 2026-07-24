import { execSync } from "child_process";
import * as fs from "node:fs";
import * as path from "node:path";

const distDir = path.join(__dirname, "../dist");
if (!fs.existsSync(distDir)) {
    fs.mkdirSync(distDir, { recursive: true });
}
const generatedDir = path.join(__dirname, "../src/generated");
if (!fs.existsSync(generatedDir)) {
    fs.mkdirSync(generatedDir, { recursive: true });
}

console.log("Generating TS bindings...");
execSync(
    "go run github.com/13rac1/gowasm-bindgen@v1.1.0 main.go --no-build -o ../js/src/generated",
    {
        cwd: path.join(__dirname, "../../wasm"),
        stdio: "inherit",
    },
);

// The generated worker.js always fetches its wasm module as 'main.wasm'
// relative to its own URL, so the build output must use that name.
// TinyGo selects the js/wasm (syscall/js) target via -target, not via the
// GOOS/GOARCH env vars used by the standard Go toolchain.
console.log("Building WASM...");
execSync("tinygo build -target wasm -o ../js/dist/main.wasm .", {
    cwd: path.join(__dirname, "../../wasm"),
    stdio: "inherit",
});

// TinyGo ships its own wasm_exec.js tailored to its runtime; it is not
// interchangeable with the standard Go distribution's copy.
console.log("Copying wasm_exec.js...");
const tinygoroot = execSync("tinygo env TINYGOROOT").toString().trim();
fs.copyFileSync(
    path.join(tinygoroot, "targets/wasm_exec.js"),
    path.join(distDir, "wasm_exec.js"),
);

// worker.js must be served from the same directory as wasm_exec.js and
// main.wasm (it loads both via relative URLs), so mirror it into dist/
// alongside the other runtime assets consumers need to serve/bundle.
console.log("Copying worker.js...");
fs.copyFileSync(
    path.join(generatedDir, "worker.js"),
    path.join(distDir, "worker.js"),
);

console.log("WASM built successfully.");
