// Copyright 2025-2026 Mirian Aïnar & Christophe Pallier
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).
//
// Baud: 115200  8N1

#include <Arduino.h>

static constexpr uint8_t OUT_PINS[8] = {30,31,32,33,34,35,36,37};
static constexpr uint8_t IN_PINS[8]  = {22,23,24,25,26,27,28,29};

// === Direct port access ===
//
// On the Mega 2560 both banks happen to land on a single AVR port each, so all
// 8 lines can be written or read in ONE instruction instead of 8 sequential
// digitalWrite/digitalRead calls (~4-5 us each). That matters for two reasons:
// a trigger code becomes glitch-free (no interval during which the port shows a
// half-written value an amplifier could latch), and reads sample all 8 buttons
// at the same instant instead of smeared over ~40 us.
//
//   OUT_PINS 30..37 -> PORTC bits 7..0   (REVERSED: pin 30 = PC7, pin 37 = PC0)
//   IN_PINS  22..29 -> PINA  bits 0..7   (in order: pin 22 = PA0, pin 29 = PA7)
//
// The reversal on PORTC is why maskToPortC/portCToMask exist; getting it wrong
// mirrors the trigger code, which is the kind of bug that only shows up in the
// recorded data. The static_asserts below fail the build if anyone renumbers
// the pin arrays without updating this mapping.
constexpr bool outPinsMatchPortC(uint8_t i = 0) {
  return i == 8 ? true : (OUT_PINS[i] == 30 + i && outPinsMatchPortC(i + 1));
}
constexpr bool inPinsMatchPortA(uint8_t i = 0) {
  return i == 8 ? true : (IN_PINS[i] == 22 + i && inPinsMatchPortA(i + 1));
}
static_assert(outPinsMatchPortC(),
              "OUT_PINS must be D30..D37 (PORTC7..PORTC0) for direct port access");
static_assert(inPinsMatchPortA(),
              "IN_PINS must be D22..D29 (PORTA0..PORTA7) for direct port access");

// reverseBits maps logical line order onto PORTC's reversed bit order.
// Branch-free and constant-time, so it adds no jitter to a trigger.
static inline uint8_t reverseBits(uint8_t b) {
  b = (uint8_t)((b & 0xF0) >> 4 | (b & 0x0F) << 4);
  b = (uint8_t)((b & 0xCC) >> 2 | (b & 0x33) << 2);
  b = (uint8_t)((b & 0xAA) >> 1 | (b & 0x55) << 1);
  return b;
}

// Nothing else in this sketch or the Arduino core writes PORTC (the millis()
// ISR touches only timer registers), so the read-modify-write in setPortHigh /
// setPortLow cannot be corrupted by an interrupt and needs no ATOMIC_BLOCK.
static inline void setPortHigh(uint8_t mask) { PORTC |= reverseBits(mask); }
static inline void setPortLow(uint8_t mask)  { PORTC &= (uint8_t)~reverseBits(mask); }

// setPortAll assigns all 8 output lines at once — a single OUT instruction, so
// the lines change simultaneously and no intermediate value is ever visible.
static inline void setPortAll(uint8_t mask)  { PORTC = reverseBits(mask); }

static inline void setLineHigh(uint8_t line) { PORTC |= (uint8_t)(1 << (7 - line)); }
static inline void setLineLow(uint8_t line)  { PORTC &= (uint8_t)~(1 << (7 - line)); }

// Firmware identification, reported by opcode 1 (get_info).
//
// Version 1 is the first build that answers get_info at all. Earlier firmware
// falls through to the `default:` branch below and stays silent, so a host that
// gets no reply must treat the box as pre-version-1 and restrict itself to the
// original opcode set (10-16, 20).
//
// CAPS advertises optional features so a host can feature-detect instead of
// assuming. Set a bit here only when the matching opcode is actually
// implemented below, and keep the values in step with the MEGCap* constants in
// the client libraries.
static const uint8_t PROTOCOL_VERSION = 1;
static const uint8_t CAP_ATOMIC_PORT  = 0x01; // reserved: single-write 8-bit port update
static const uint8_t CAP_TIMESTAMPS   = 0x02; // reserved: micros()-timestamped input events
static const uint8_t CAPS             = CAP_ATOMIC_PORT | CAP_TIMESTAMPS; // opcodes 17, 21-24

static uint16_t g_pulse_ms   = 5;   // pulse duration in ms
static uint8_t  g_active_mask = 0;  // pins currently held HIGH by a pulse
static uint32_t g_pulse_end   = 0;  // millis() value at which the pulse ends

//Helpers binary reading
int readU8Blocking() {
  while (Serial.available() < 1) { /* wait */ }
  return Serial.read() & 0xFF;
}

uint16_t readU16LEBlocking() {
  int lo = readU8Blocking();
  int hi = readU8Blocking();
  return (uint16_t)(lo | (hi << 8));
}

void writeU32LE(uint32_t v) {
  Serial.write((uint8_t)(v & 0xFF));
  Serial.write((uint8_t)((v >> 8) & 0xFF));
  Serial.write((uint8_t)((v >> 16) & 0xFF));
  Serial.write((uint8_t)((v >> 24) & 0xFF));
}

// === Output ===
void applyMaskHigh(uint8_t mask) {
  setPortHigh(mask);
}

void applyMaskLow(uint8_t mask) {
  setPortLow(mask);
}

// releasePulseOwnership removes lines from the set an in-flight pulse will drop
// when it expires.
//
// Without it, a manual level change made while a pulse is running is silently
// undone: the teardown in loop() clears every line in g_active_mask regardless
// of what has happened since. A paradigm that pulses a trigger and then sets a
// persistent line within the pulse window (easy to hit — default width is 5 ms,
// USB latency is 1-2 ms) would lose that line with no error anywhere.
static inline void releasePulseOwnership(uint8_t mask) {
  g_active_mask &= (uint8_t)~mask;
}

void pulseMask(uint8_t mask, uint16_t width_ms) {
  // End any in-progress pulse before starting a new one
  if (g_active_mask) {
    applyMaskLow(g_active_mask);
  }
  applyMaskHigh(mask);
  g_active_mask = mask;
  g_pulse_end   = millis() + width_ms;
}

// === Input ===
// 'invert' value to adapt depending on button polarity
// All 8 inputs are sampled in one PINA read, so they share a single instant
// rather than being smeared across ~40 us of sequential digitalRead calls.
uint8_t readButtons(bool invert=false) {
  uint8_t m = PINA;
  if (invert) m = (uint8_t)~m;
  return m;
}

// === Timestamped input events ===
//
// Polling the buttons from the host costs a USB round trip, so the host can
// only ever say "the press had happened by the time I asked" — its resolution
// is the poll interval (5 ms), which swamps everything else in the chain.
//
// Instead the sketch samples PINA every loop iteration (a few microseconds) and
// records micros() the moment the mask changes. The host drains those events
// later, at its leisure, and the timestamp is unaffected by when it got round to
// asking. That moves reaction-time resolution from milliseconds to the loop
// period.
//
// micros() itself ticks in 4 us steps on a 16 MHz AVR, so that is the floor.
// It wraps every ~71.6 minutes; the host handles that with wrap-safe unsigned
// subtraction against a periodic clock sync (opcode 22), so nothing here needs
// to care.
#define EVENT_QUEUE_LEN 32   // 5 bytes each; must be a power of two

struct InputEvent {
  uint32_t t_us;   // micros() when the change was detected
  uint8_t  mask;   // button mask AFTER the change
};

static InputEvent g_events[EVENT_QUEUE_LEN];
static uint8_t  g_evHead      = 0;
static uint8_t  g_evTail      = 0;
static bool     g_evOverflow  = false;  // sticky: events were dropped
static uint8_t  g_lastButtons = 0;
static uint32_t g_lastChange  = 0;
static uint16_t g_debounce_us = 0;      // 0 = disabled

// pushEvent appends to the ring, or sets the overflow flag if it is full.
// Dropping the newest keeps the oldest events, which are the ones a trial
// usually cares about; either way the host is told data was lost rather than
// being handed a silently incomplete record.
static void pushEvent(uint8_t mask, uint32_t t) {
  uint8_t next = (uint8_t)((g_evHead + 1) & (EVENT_QUEUE_LEN - 1));
  if (next == g_evTail) { g_evOverflow = true; return; }
  g_events[g_evHead].t_us = t;
  g_events[g_evHead].mask = mask;
  g_evHead = next;
}

// sampleButtons is called every loop iteration. The timestamp is taken right
// after the change is seen, so its error is bounded by the loop period rather
// than by anything on the USB side.
static void sampleButtons() {
  uint8_t now = readButtons(/*invert=*/true);
  if (now == g_lastButtons) return;
  uint32_t t = micros();
  // Debounce is off by default: fibre-optic pads do not bounce, and silently
  // swallowing real transitions would be worse than reporting extra ones. Set
  // it with opcode 24 for mechanical buttons.
  if (g_debounce_us != 0 && (uint32_t)(t - g_lastChange) < (uint32_t)g_debounce_us) return;
  g_lastButtons = now;
  g_lastChange  = t;
  pushEvent(now, t);
}

void setup() {
  Serial.begin(115200);

  for (uint8_t i=0;i<8;i++) pinMode(OUT_PINS[i], OUTPUT);
  for (uint8_t i=0;i<8;i++) {
    pinMode(IN_PINS[i], INPUT_PULLUP); //most response boxes need pullup
  }

  // Every line to LOW when starting
  applyMaskLow(0xFF);

  // Seed the change detector with the resting state, so a button already held
  // at boot does not look like a press the instant the host connects.
  g_lastButtons = readButtons(/*invert=*/true);
  g_lastChange  = micros();
}

void loop() {
  // End an active pulse when its duration has elapsed (non-blocking)
  if (g_active_mask && millis() >= g_pulse_end) {
    applyMaskLow(g_active_mask);
    g_active_mask = 0;
  }

  sampleButtons();

  if (Serial.available() < 1) return;
  int opcode = readU8Blocking();

  switch (opcode) {
    case 1: { // get_info -> 'M','T','B', version u8, caps u8
      Serial.write('M');
      Serial.write('T');
      Serial.write('B');
      Serial.write(PROTOCOL_VERSION);
      Serial.write(CAPS);
      break;
    }
    case 10: { // set_trigger_duration [u16 ms]
      uint16_t ms = readU16LEBlocking();
      g_pulse_ms = ms;
      break;
    }
    case 11: { // send_trigger_mask [u8 mask]
      uint8_t mask = (uint8_t)readU8Blocking();
      pulseMask(mask, g_pulse_ms);
      break;
    }
    case 12: { // send_trigger_on_line [u8 line 0..7]
      uint8_t line = (uint8_t)readU8Blocking();
      if (line < 8) pulseMask((uint8_t)(1<<line), g_pulse_ms);
      break;
    }
    case 13: { // set_high_mask [u8 mask]
      uint8_t mask = (uint8_t)readU8Blocking();
      releasePulseOwnership(mask);
      applyMaskHigh(mask);
      break;
    }
    case 14: { // set_low_mask [u8 mask]
      uint8_t mask = (uint8_t)readU8Blocking();
      releasePulseOwnership(mask);
      applyMaskLow(mask);
      break;
    }
    case 15: { // set_high_on_line [u8 line]
      uint8_t line = (uint8_t)readU8Blocking();
      if (line < 8) {
        releasePulseOwnership((uint8_t)(1 << line));
        setLineHigh(line);
      }
      break;
    }
    case 16: { // set_low_on_line [u8 line]
      uint8_t line = (uint8_t)readU8Blocking();
      if (line < 8) {
        releasePulseOwnership((uint8_t)(1 << line));
        setLineLow(line);
      }
      break;
    }
    case 17: { // set_port_mask [u8 mask] — assign all 8 lines atomically
      // Unlike 13/14, which only set or only clear, this assigns the whole
      // port in one instruction: no intermediate value is ever on the pins,
      // so a trigger code cannot be latched half-written. Requires
      // CAP_ATOMIC_PORT; hosts must feature-detect via get_info.
      uint8_t mask = (uint8_t)readU8Blocking();
      releasePulseOwnership(0xFF); // the whole port is reassigned
      setPortAll(mask);
      break;
    }
    case 20: { // get_response_button_mask -> write [u8 mask]
      // Become 'true' if input are active when LOW
      uint8_t mask = readButtons(/*invert=*/true);
      Serial.write(mask);
      break;
    }
    case 21: { // get_event -> [flags u8][mask u8][t_us u32 LE], always 6 bytes
      // Fixed-width reply even when the queue is empty, so the host never has
      // to guess how many bytes are coming.
      //   flags bit0: an event follows (mask/t_us meaningful)
      //   flags bit1: events were dropped since the last get_event
      uint8_t flags = 0, mask = 0;
      uint32_t t = 0;
      if (g_evOverflow) { flags |= 0x02; g_evOverflow = false; }
      if (g_evTail != g_evHead) {
        flags |= 0x01;
        t    = g_events[g_evTail].t_us;
        mask = g_events[g_evTail].mask;
        g_evTail = (uint8_t)((g_evTail + 1) & (EVENT_QUEUE_LEN - 1));
      }
      Serial.write(flags);
      Serial.write(mask);
      writeU32LE(t);
      break;
    }
    case 22: { // get_micros -> u32 LE — device clock, for host offset estimation
      writeU32LE(micros());
      break;
    }
    case 23: { // clear_events
      g_evTail     = g_evHead;
      g_evOverflow = false;
      // Re-seed the detector: a button held while draining must not surface as
      // a fresh press on the next sample.
      g_lastButtons = readButtons(/*invert=*/true);
      break;
    }
    case 24: { // set_debounce [u16 us], 0 disables
      g_debounce_us = readU16LEBlocking();
      break;
    }
    default:
      // opcode unknown: ignore
      break;
  }
}
