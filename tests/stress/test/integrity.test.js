import assert from "node:assert/strict";
import test from "node:test";

import { bodyByteLength, hasZIPSignature, looksLikeMavenPOM } from "../k6/integrity.js";

test("hasZIPSignature accepts standard ZIP signatures", () => {
  for (const signature of [
    [0x50, 0x4b, 0x03, 0x04],
    [0x50, 0x4b, 0x05, 0x06],
    [0x50, 0x4b, 0x07, 0x08],
  ]) {
    assert.equal(hasZIPSignature(Uint8Array.from(signature).buffer), true);
  }
  assert.equal(hasZIPSignature(Uint8Array.from([0x3c, 0x68, 0x74, 0x6d]).buffer), false);
});

test("looksLikeMavenPOM accepts XML declaration and rejects error pages", () => {
  assert.equal(looksLikeMavenPOM("\uFEFF<?xml version=\"1.0\"?><project></project>"), true);
  assert.equal(looksLikeMavenPOM("<project></project>"), true);
  assert.equal(looksLikeMavenPOM("<html><body>error</body></html>"), false);
  assert.equal(looksLikeMavenPOM(""), false);
});

test("bodyByteLength handles strings and binary bodies", () => {
  assert.equal(bodyByteLength("pom"), 3);
  assert.equal(bodyByteLength(Uint8Array.from([1, 2, 3, 4]).buffer), 4);
  assert.equal(bodyByteLength(null), 0);
});

