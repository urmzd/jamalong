import assert from "node:assert/strict";
import { test } from "node:test";
import { ClockSync, formatTime, positionMs } from "./clock.ts";
import type { Now } from "./protocol.ts";
import { plausibleLength } from "./spotify.ts";
import { decide } from "./sync.ts";

function now(over: Partial<Now> = {}): Now {
  return {
    item: {
      id: "item-1",
      track: { provider: "youtube", provider_id: "vid", title: "T", artist: "A", duration_ms: 0 },
      added_by: "m1",
      votes: 0,
      voted: false,
    },
    started_at_ms: 1_000,
    paused_at_ms: null,
    waiting: false,
    seek_seq: 0,
    skip_votes: 0,
    skip_needed: 1,
    skip_voted: false,
    ...over,
  };
}

test("positionMs follows the server clock and freezes when paused", () => {
  assert.equal(positionMs(now(), 11_000), 10_000);
  assert.equal(positionMs(now({ paused_at_ms: 4_000 }), 11_000), 3_000);
  assert.equal(positionMs(now(), 500), 0);
  const known = now();
  known.item.track.duration_ms = 5_000;
  assert.equal(positionMs(known, 60_000), 5_000);
});

test("ClockSync trusts the fastest round trip", () => {
  const c = new ClockSync();
  c.seed(50_000, 0);
  assert.equal(c.serverNow(0), 50_000);
  c.sample(0, 400, 50_300); // slow: offset 50_100
  assert.equal(c.serverNow(1_000), 51_100);
  c.sample(1_000, 1_020, 51_015); // fast: offset 50_005
  assert.equal(c.serverNow(2_000), 52_005);
  c.sample(2_000, 2_900, 99_999); // slower than the best: ignored
  assert.equal(c.serverNow(2_000), 52_005);
});

test("formatTime", () => {
  assert.equal(formatTime(0), "0:00");
  assert.equal(formatTime(65_999), "1:05");
  assert.equal(formatTime(-5), "0:00");
});

const idle = { itemId: null, playing: false, positionMs: 0 };
const follower = { lead: false, seenSeekSeq: 0 };
const lead = { lead: true, seenSeekSeq: 0 };

test("a speaker loads the current item at the clock position", () => {
  assert.deepEqual(decide(now(), 31_000, idle, follower), [
    {
      kind: "load",
      itemId: "item-1",
      provider: "youtube",
      trackId: "vid",
      startMs: 30_000,
      durationMs: 0,
    },
  ]);
  assert.deepEqual(
    decide(now({ paused_at_ms: 2_000 }), 31_000, idle, follower).map((a) => a.kind),
    ["load", "pause"],
  );
  assert.deepEqual(decide(null, 0, { ...idle, itemId: "old" }, follower), [{ kind: "stop" }]);
  assert.deepEqual(decide(null, 0, idle, follower), []);
});

test("a follower chases the clock, the lead does not", () => {
  const behind = { itemId: "item-1", playing: true, positionMs: 27_000 };
  assert.deepEqual(decide(now(), 31_000, behind, follower), [{ kind: "seek", toMs: 30_000 }]);
  assert.deepEqual(decide(now(), 31_000, behind, lead), []);
  const close = { itemId: "item-1", playing: true, positionMs: 29_500 };
  assert.deepEqual(decide(now(), 31_000, close, follower), []);
});

test("an explicit seek moves the lead too", () => {
  const playing = { itemId: "item-1", playing: true, positionMs: 30_000 };
  assert.deepEqual(decide(now({ seek_seq: 1, started_at_ms: -59_000 }), 31_000, playing, lead), [
    { kind: "seek", toMs: 90_000 },
  ]);
});

test("play and pause follow the room", () => {
  const stopped = { itemId: "item-1", playing: false, positionMs: 30_000 };
  assert.deepEqual(decide(now(), 31_000, stopped, lead), [{ kind: "play" }]);
  const playing = { itemId: "item-1", playing: true, positionMs: 3_000 };
  assert.deepEqual(decide(now({ paused_at_ms: 4_000 }), 31_000, playing, lead), [
    { kind: "pause" },
  ]);
});

test("a lead that was on the clock and falls far behind jumps back", () => {
  const n = now();
  const at = 1_000 + 20_000; // the clock reads 20s
  const player = { itemId: "item-1", playing: true, positionMs: 1_800 };
  assert.deepEqual(decide(n, at, player, { lead: true, seenSeekSeq: 0, leadSynced: true }), [
    { kind: "seek", toMs: 20_000 },
  ]);
  // A lead still starting up is not on the clock yet, so the clock waits for it.
  assert.deepEqual(decide(n, at, player, { lead: true, seenSeekSeq: 0, leadSynced: false }), []);
  // A small lag is buffering, not a glitch.
  const close = { ...player, positionMs: 17_000 };
  assert.deepEqual(decide(n, at, close, { lead: true, seenSeekSeq: 0, leadSynced: true }), []);
});

test("a Spotify end counts only at a believable length", () => {
  assert.equal(plausibleLength(166_958, 166_958), true);
  assert.equal(plausibleLength(29_713, 166_958), true); // preview
  assert.equal(plausibleLength(19_645, 166_958), false); // the bogus end seen live
  assert.equal(plausibleLength(19_645, 0), true); // nothing to compare with
});

test("a follower still starting is not sought", () => {
  const n = now();
  const at = 1_000 + 20_000;
  const starting = { itemId: "item-1", playing: false, positionMs: 6_000 };
  assert.deepEqual(decide(n, at, starting, { lead: false, seenSeekSeq: 0 }), [{ kind: "play" }]);
  const playing = { ...starting, playing: true };
  assert.deepEqual(decide(n, at, playing, { lead: false, seenSeekSeq: 0 }), [
    { kind: "seek", toMs: 20_000 },
  ]);
});
