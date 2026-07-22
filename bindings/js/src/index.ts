import * as fs from "node:fs";
import * as path from "node:path";

// Load the wasm_exec.js file into the global scope
require("../dist/wasm_exec.js");

import { GoMain } from "./generated/go-main";

export class Godexture {
    private goMain?: GoMain;

    async init() {
        if (this.goMain) return;

        let wasmPath = path.join(__dirname, "godec.wasm");
        if (!fs.existsSync(wasmPath)) {
            wasmPath = path.join(__dirname, "../dist/godec.wasm");
        }
        const wasmBuffer = fs.readFileSync(wasmPath);

        this.goMain = await GoMain.init(wasmBuffer);
    }

    hello(): string {
        if (!this.goMain) {
            throw new Error("Godexture not initialized. Call init() first.");
        }
        return this.goMain.hello();
    }
}
