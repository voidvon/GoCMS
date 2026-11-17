import assert from "node:assert/strict"
import test from "node:test"
import { nextVersion, parseVersion } from "./release-version.mjs"

test("release versions carry after patch 20 and minor 20", () => {
  for (const [current, next] of [
    ["0.1.19", "0.1.20"],
    ["0.1.20", "0.2.0"],
    ["0.19.20", "0.20.0"],
    ["0.20.19", "0.20.20"],
    ["0.20.20", "1.0.0"],
    ["1.0.0", "1.0.1"],
    ["1.20.20", "2.0.0"],
    ["0.1.66", "0.2.0"],
  ]) assert.equal(nextVersion(current), next, current)
})

test("release versions reject invalid or imprecise values", () => {
  for (const value of ["v0.2.0", "0.2", "0.02.0", "-1.0.0", "0.2.0-beta", "9007199254740992.0.0"]) {
    assert.throws(() => parseVersion(value), undefined, value)
  }
})
