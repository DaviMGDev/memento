package wasm

import (
	"bytes"
)

func encodeLEB128U(val uint32) []byte {
	var res []byte
	for {
		b := byte(val & 0x7f)
		val >>= 7
		if val != 0 {
			res = append(res, b|0x80)
		} else {
			res = append(res, b)
			break
		}
	}
	return res
}

func encodeLEB128S(val int32) []byte {
	var res []byte
	more := true
	for more {
		b := byte(val & 0x7f)
		val >>= 7
		if (val == 0 && (b&0x40) == 0) || (val == -1 && (b&0x40) != 0) {
			more = false
		} else {
			b |= 0x80
		}
		res = append(res, b)
	}
	return res
}

func encodeVec(items [][]byte) []byte {
	var buf bytes.Buffer
	buf.Write(encodeLEB128U(uint32(len(items))))
	for _, it := range items {
		buf.Write(it)
	}
	return buf.Bytes()
}

func encodeSection(secID byte, content []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(secID)
	buf.Write(encodeLEB128U(uint32(len(content))))
	buf.Write(content)
	return buf.Bytes()
}

func encodeString(s string) []byte {
	b := []byte(s)
	return append(encodeLEB128U(uint32(len(b))), b...)
}

// buildTestWasmModule builds a WASM binary with:
//   - Imports:
//     idx 0: memento.declare_inject(i32, i32) -> i32
//     idx 1: memento.declare_provide(i32, i32) -> i32
//     idx 2: memento.register_effect(i32) -> i32
//     idx 3: memento.get_payload_len() -> i32
//     idx 4: memento.get_payload(i32, i32) -> i32
//   - Exports:
//     "memory" (1 page)
//     "memento_declare" -> calls declare_inject("storage") and declare_provide("cache")
//     "memento_activate" -> calls register_effect(42) and returns 0
//     "memento_revert_effect" -> returns 0
//   - Data:
//     Offset 16: "storage" (7 bytes)
//     Offset 32: "cache" (5 bytes)
func buildTestWasmModule() []byte {
	// Type 0: (i32, i32) -> i32
	type0 := []byte{0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f}
	// Type 1: () -> i32
	type1 := []byte{0x60, 0x00, 0x01, 0x7f}
	// Type 2: (i32) -> i32
	type2 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}

	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1, type2}))

	// Imports
	importInject := append(encodeString("memento"), append(encodeString("declare_inject"), 0x00, 0x00)...)
	importProvide := append(encodeString("memento"), append(encodeString("declare_provide"), 0x00, 0x00)...)
	importEffect := append(encodeString("memento"), append(encodeString("register_effect"), 0x00, 0x02)...)
	importPayloadLen := append(encodeString("memento"), append(encodeString("get_payload_len"), 0x00, 0x01)...)
	importPayload := append(encodeString("memento"), append(encodeString("get_payload"), 0x00, 0x00)...)

	importSec := encodeSection(2, encodeVec([][]byte{
		importInject,
		importProvide,
		importEffect,
		importPayloadLen,
		importPayload,
	}))

	// Func section: types of funcs 5, 6, 7
	funcSec := encodeSection(3, encodeVec([][]byte{
		{0x01}, // func 5 uses type 1
		{0x01}, // func 6 uses type 1
		{0x02}, // func 7 uses type 2
	}))

	// Memory section: 1 page min
	memSec := encodeSection(5, encodeVec([][]byte{
		{0x00, 0x01},
	}))

	// Export section
	expMem := append(encodeString("memory"), 0x02, 0x00)
	expDeclare := append(encodeString("memento_declare"), 0x00, 0x05)
	expActivate := append(encodeString("memento_activate"), 0x00, 0x06)
	expRevert := append(encodeString("memento_revert_effect"), 0x00, 0x07)

	exportSec := encodeSection(7, encodeVec([][]byte{
		expMem,
		expDeclare,
		expActivate,
		expRevert,
	}))

	// Code section
	// Func 5: memento_declare:
	// declare_inject(16, 7) -> "storage"
	// declare_provide(32, 5) -> "cache"
	// return 0
	body5 := []byte{
		0x00,       // 0 locals
		0x41, 0x10, // i32.const 16
		0x41, 0x07, // i32.const 7
		0x10, 0x00, // call 0 (declare_inject)
		0x1a,       // drop
		0x41, 0x20, // i32.const 32
		0x41, 0x05, // i32.const 5
		0x10, 0x01, // call 1 (declare_provide)
		0x1a,       // drop
		0x41, 0x00, // i32.const 0
		0x0f, // return
		0x0b, // end
	}

	// Func 6: memento_activate:
	// register_effect(42)
	// return 0
	body6 := []byte{
		0x00,       // 0 locals
		0x41, 0x2a, // i32.const 42
		0x10, 0x02, // call 2 (register_effect)
		0x1a,       // drop
		0x41, 0x00, // i32.const 0
		0x0f, // return
		0x0b, // end
	}

	// Func 7: memento_revert_effect(effect_id):
	// return 0
	body7 := []byte{
		0x00,       // 0 locals
		0x41, 0x00, // i32.const 0
		0x0f, // return
		0x0b, // end
	}

	code5 := append(encodeLEB128U(uint32(len(body5))), body5...)
	code6 := append(encodeLEB128U(uint32(len(body6))), body6...)
	code7 := append(encodeLEB128U(uint32(len(body7))), body7...)

	codeSec := encodeSection(10, encodeVec([][]byte{code5, code6, code7}))

	// Data section:
	// Offset 16: "storage"
	data1 := append([]byte{0x00, 0x41, 0x10, 0x0b}, append(encodeLEB128U(7), []byte("storage")...)...)
	// Offset 32: "cache"
	data2 := append([]byte{0x00, 0x41, 0x20, 0x0b}, append(encodeLEB128U(5), []byte("cache")...)...)

	dataSec := encodeSection(11, encodeVec([][]byte{data1, data2}))

	var wasm bytes.Buffer
	wasm.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	wasm.Write(typeSec)
	wasm.Write(importSec)
	wasm.Write(funcSec)
	wasm.Write(memSec)
	wasm.Write(exportSec)
	wasm.Write(codeSec)
	wasm.Write(dataSec)

	return wasm.Bytes()
}

// buildStandaloneTestWasmModule builds a WASM binary with no declarations,
// an exported memento_activate that calls register_effect(101),
// and an exported memento_revert_effect.
func buildStandaloneTestWasmModule() []byte {
	// Type 0: () -> i32
	type0 := []byte{0x60, 0x00, 0x01, 0x7f}
	// Type 1: (i32) -> i32
	type1 := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}

	typeSec := encodeSection(1, encodeVec([][]byte{type0, type1}))

	// Import register_effect: func idx 0
	importEffect := append(encodeString("memento"), append(encodeString("register_effect"), 0x00, 0x01)...)
	importSec := encodeSection(2, encodeVec([][]byte{importEffect}))

	// Func section:
	// func 1: type 0 (memento_activate)
	// func 2: type 1 (memento_revert_effect)
	funcSec := encodeSection(3, encodeVec([][]byte{
		{0x00},
		{0x01},
	}))

	// Export section
	expActivate := append(encodeString("memento_activate"), 0x00, 0x01)
	expRevert := append(encodeString("memento_revert_effect"), 0x00, 0x02)
	exportSec := encodeSection(7, encodeVec([][]byte{expActivate, expRevert}))

	// Code section
	// memento_activate: register_effect(101); return 0
	body1 := []byte{
		0x00,
		0x41, 0x65, // i32.const 101
		0x10, 0x00, // call 0
		0x1a,       // drop
		0x41, 0x00, // i32.const 0
		0x0f, // return
		0x0b, // end
	}
	body2 := []byte{
		0x00,
		0x41, 0x00, // i32.const 0
		0x0f,
		0x0b,
	}

	code1 := append(encodeLEB128U(uint32(len(body1))), body1...)
	code2 := append(encodeLEB128U(uint32(len(body2))), body2...)
	codeSec := encodeSection(10, encodeVec([][]byte{code1, code2}))

	var wasm bytes.Buffer
	wasm.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	wasm.Write(typeSec)
	wasm.Write(importSec)
	wasm.Write(funcSec)
	wasm.Write(exportSec)
	wasm.Write(codeSec)

	return wasm.Bytes()
}
