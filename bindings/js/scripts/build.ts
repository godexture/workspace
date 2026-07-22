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
    "go run github.com/13rac1/gowasm-bindgen@v1.1.0 main.go --no-build --mode sync -o ../js/src/generated",
    {
        cwd: path.join(__dirname, "../../wasm"),
        stdio: "inherit",
    },
);

console.log("Building WASM...");
execSync("go build -o ../js/dist/godec.wasm .", {
    cwd: path.join(__dirname, "../../wasm"),
    env: { ...process.env, GOOS: "js", GOARCH: "wasm" },
    stdio: "inherit",
});

console.log("Copying wasm_exec.js...");
const goroot = execSync("go env GOROOT").toString().trim();
fs.copyFileSync(
    path.join(goroot, "lib/wasm/wasm_exec.js"),
    path.join(distDir, "wasm_exec.js"),
);

console.log("WASM built successfully.");
