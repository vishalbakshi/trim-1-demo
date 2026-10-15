// The wasm build of trim-1 for the static page (GitHub Pages).
//
// It sets two JS functions and then waits:
//
//	trimScore(context, query) → {lines, scores, error}
//	trimModelInfo() → "N trees, ..."
//
// lines is trim.Score's lines joined with "\n" (no line contains one), and
// scores is a Float64Array, so 11,000 lines cross into JS as two values
// instead of 22,000.
//
// Build: GOOS=js GOARCH=wasm go build -o trim.wasm ./wasm
package main

import (
	"strings"
	"syscall/js"
	"unsafe"

	trim "github.com/caveman-ai/trim-1"
)

func main() {
	js.Global().Set("trimScore", js.FuncOf(func(this js.Value, args []js.Value) any {
		lines, scores, err := trim.Score(args[0].String(), args[1].String())
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		out := js.Global().Get("Float64Array").New(len(scores))
		if len(scores) > 0 {
			b := unsafe.Slice((*byte)(unsafe.Pointer(&scores[0])), len(scores)*8)
			js.CopyBytesToJS(js.Global().Get("Uint8Array").New(out.Get("buffer")), b)
		}
		return map[string]any{"lines": strings.Join(lines, "\n"), "scores": out}
	}))
	js.Global().Set("trimModelInfo", js.FuncOf(func(this js.Value, args []js.Value) any {
		info, _ := trim.ModelInfo()
		return info
	}))
	select {}
}
