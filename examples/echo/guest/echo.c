// Echo plugin for the memento WASM loader.
//
// Builds to a freestanding core WebAssembly module that:
//   * declares that it provides the context key "echo",
//   * on activation, reads its configuration payload through the memento
//     host ABI and echoes it back to the host log,
//   * registers an effect whose inverse (memento_revert_effect) echoes a
//     farewell line when the fiber is unloaded.

#include <stdint.h>

#define MEMENTO_EXPORT(name) __attribute__((export_name(name)))
#define MEMENTO_IMPORT(name) __attribute__((import_module("memento"), import_name(name)))

MEMENTO_IMPORT("declare_inject") extern uint32_t declare_inject(const char *ptr, uint32_t len);
MEMENTO_IMPORT("declare_provide") extern uint32_t declare_provide(const char *ptr, uint32_t len);
MEMENTO_IMPORT("get_payload_len") extern uint32_t get_payload_len(void);
MEMENTO_IMPORT("get_payload") extern uint32_t get_payload(char *buf, uint32_t max);
MEMENTO_IMPORT("register_effect") extern uint32_t register_effect(uint32_t id);
MEMENTO_IMPORT("log") extern uint32_t log(const char *ptr, uint32_t len);

enum {
	ECHO_EFFECT_ID = 1,
	PAYLOAD_CAP = 1024,
	OUT_CAP = 1024 + 64,
};

static char payload[PAYLOAD_CAP];
static char out[OUT_CAP];

// echo_log writes "echo: " followed by data and a newline, then forwards it
// to the host log. Returns the host status (0 on success).
static uint32_t echo_log(const char *data, uint32_t len) {
	static const char prefix[] = "echo: ";
	uint32_t n = 0;
	for (uint32_t i = 0; i + 1 < (uint32_t)sizeof(prefix) && n < (uint32_t)OUT_CAP - 1; i++)
		out[n++] = prefix[i];
	for (uint32_t i = 0; i < len && n < (uint32_t)OUT_CAP - 1; i++)
		out[n++] = data[i];
	out[n++] = '\n';
	return log(out, n);
}

// memento_declare: the probe entry point. Declares provided keys.
MEMENTO_EXPORT("memento_declare")
uint32_t memento_declare(void) {
	static const char key[] = "echo";
	return declare_provide(key, (uint32_t)sizeof(key) - 1) == 0 ? 0 : 1;
}

// memento_activate: read the payload and echo it, then install the effect
// whose inverse the loader will call on unload.
MEMENTO_EXPORT("memento_activate")
uint32_t memento_activate(void) {
	uint32_t want = get_payload_len();
	if (want > PAYLOAD_CAP)
		want = PAYLOAD_CAP;
	uint32_t got = get_payload(payload, want);
	if (echo_log(payload, got) != 0)
		return 1;
	if (register_effect(ECHO_EFFECT_ID) != 0)
		return 2;
	return 0;
}

// memento_revert_effect: inverse of the effect registered above.
MEMENTO_EXPORT("memento_revert_effect")
uint32_t memento_revert_effect(uint32_t effect_id) {
	static const char msg[] = "echo: reverted effect\n";
	(void)effect_id;
	return log(msg, (uint32_t)sizeof(msg) - 1) == 0 ? 0 : 1;
}
