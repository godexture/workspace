//go:build js && wasm

package main

// Hello is a simple function exported to JavaScript via gowasm-bindgen.
func Hello() string {
	return "Hello Generated!"
}

func main() {
	select {}
}
