//go:build wasip1

// Command chat is an interactive echo REPL chat guest for the memento WASM
// loader. It is built with GOOS=wasip1 GOARCH=wasm -buildmode=c-shared (see
// build.sh), which produces a WASI reactor module: the host initializes it
// through _rt0_wasm_wasip1_lib and then calls the memento_* exports.
package main

import (
	"bufio"
	"os"
	"strings"
	"unsafe"
)

const (
	chatEffectID = 1 // effect id echoed back to memento_revert_effect
	providedKey  = "chat"
)

//go:wasmimport memento declare_provide
func declareProvide(ptr unsafe.Pointer, n uint32) int32

//go:wasmimport memento get_payload_len
func getPayloadLen() int32

//go:wasmimport memento get_payload
func getPayload(ptr unsafe.Pointer, max uint32) int32

//go:wasmimport memento register_effect
func registerEffect(id uint32) int32

//go:wasmimport memento log
func logMsg(ptr unsafe.Pointer, n uint32) int32

// emit writes s to the host log through the memento ABI.
func emit(s string) {
	b := []byte(s)
	if len(b) == 0 {
		return
	}
	logMsg(unsafe.Pointer(&b[0]), uint32(len(b)))
}

// mementoDeclare declares the key this guest provides.
//
//go:wasmexport memento_declare
func mementoDeclare() uint32 {
	key := []byte(providedKey)
	if declareProvide(unsafe.Pointer(&key[0]), uint32(len(key))) != 0 {
		return 1
	}
	return 0
}

// mementoActivate hosts the chat loop: it reads lines from WASI stdin and
// echoes each one under the nickname taken from the payload. The session ends
// on ":quit" or EOF; unloading then runs the effect inverse.
//
//go:wasmexport memento_activate
func mementoActivate() uint32 {
	if registerEffect(chatEffectID) != 0 {
		return 1
	}

	nick := readPayload()
	if nick == "" {
		nick = "echo"
	}
	emit("chat: " + nick + " joined (type :quit or press Ctrl-D to leave)\n")

	sc := bufio.NewScanner(os.Stdin)
	for {
		emit("you> ")
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		switch line {
		case "":
			continue
		case ":quit":
			return 0
		default:
			emit(nick + ": " + line + "\n")
		}
	}
	if err := sc.Err(); err != nil {
		emit("chat: input error: " + err.Error() + "\n")
		return 2
	}
	return 0
}

// mementoRevertEffect is the inverse of the effect registered on activation.
//
//go:wasmexport memento_revert_effect
func mementoRevertEffect(effectID uint32) uint32 {
	emit("chat: session closed\n")
	return 0
}

// readPayload returns the configuration payload handed to activation.
func readPayload() string {
	n := getPayloadLen()
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	got := getPayload(unsafe.Pointer(&buf[0]), uint32(len(buf)))
	if got <= 0 {
		return ""
	}
	return string(buf[:got])
}

func main() {}
