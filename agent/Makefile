CC      ?= cc
CSTD    := -std=c11 -D_POSIX_C_SOURCE=200809L
WARN    := -Wall -Wextra -Wpedantic -Wshadow -Wconversion -Wno-sign-conversion -Werror
CFLAGS  ?= -O2 -g
LDLIBS  := -lm

SRC_LIB := src/base64.c src/config.c src/http.c src/linkmodel.c src/log.c src/net.c \
           src/netdev.c src/probe.c src/queue.c src/report.c src/sha1.c src/ws.c
BUILD   := build

.PHONY: all test asan static clean format-check

all: $(BUILD)/satfleet-agent

$(BUILD)/satfleet-agent: $(SRC_LIB) src/main.c src/*.h | $(BUILD)
	$(CC) $(CSTD) $(WARN) $(CFLAGS) -o $@ $(SRC_LIB) src/main.c $(LDLIBS)

$(BUILD)/test_agent: $(SRC_LIB) tests/test_agent.c src/*.h | $(BUILD)
	$(CC) $(CSTD) $(WARN) $(CFLAGS) -o $@ $(SRC_LIB) tests/test_agent.c $(LDLIBS)

test: $(BUILD)/test_agent
	./$(BUILD)/test_agent

# AddressSanitizer + UndefinedBehaviorSanitizer build of the test suite (run in CI).
asan: | $(BUILD)
	$(CC) $(CSTD) $(WARN) -O1 -g -fsanitize=address,undefined -fno-omit-frame-pointer \
		-o $(BUILD)/test_agent_asan $(SRC_LIB) tests/test_agent.c $(LDLIBS)
	ASAN_OPTIONS=detect_leaks=1 UBSAN_OPTIONS=halt_on_error=1 ./$(BUILD)/test_agent_asan

# Fully static, stripped binary for the container image / terminal firmware.
static: | $(BUILD)
	$(CC) $(CSTD) $(WARN) -Os -static -s -o $(BUILD)/satfleet-agent $(SRC_LIB) src/main.c $(LDLIBS)

$(BUILD):
	mkdir -p $(BUILD)

clean:
	rm -rf $(BUILD)
