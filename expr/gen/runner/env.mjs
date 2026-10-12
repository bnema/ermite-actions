// SPDX-License-Identifier: MIT
//
// Shared by the scripts of this directory: finds the runner driver, and checks
// that the clone it was built from is the one the recorded data is about and
// that the .NET runtime is the 9.0 line (number formatting and case folding come
// from the runtime, not from the SDK).
//
//   RUNNER_CLONE=/path/to/actions/runner node expr/gen/runner/<script>.mjs

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

export const here = dirname(fileURLToPath(import.meta.url));

/** The commit of actions/runner the data in testdata/ is about (runner-commit.txt). */
export const runnerCommit = readFileSync(join(here, "runner-commit.txt"), "utf8").trim();

/** The driver built by `dotnet build -c Release expr/gen/runner -p:RunnerSrc=<clone>/src/Sdk`. */
export const driver = join(here, "bin/Release/net9.0/runner.dll");

/**
 * Checks the environment and returns { clone, commit, runtime }. The clone is
 * $RUNNER_CLONE (or the first argument); its HEAD must be runner-commit.txt and
 * its tree clean in src/Sdk, and `dotnet --list-runtimes` must list a
 * Microsoft.NETCore.App 9.0.x.
 */
export function checkEnvironment(arg) {
  const clone = process.env.RUNNER_CLONE || arg;
  if (!clone) throw new Error("set RUNNER_CLONE to a clone of https://github.com/actions/runner");
  const git = (...a) => execFileSync("git", ["-C", clone, ...a]).toString().trim();
  const commit = git("rev-parse", "HEAD");
  if (commit !== runnerCommit) throw new Error(`the clone is at ${commit}, the data is about ${runnerCommit} (runner-commit.txt)`);
  if (git("status", "--porcelain", "--", "src/Sdk") !== "") throw new Error("the clone has local changes in src/Sdk");
  const rt = execFileSync("dotnet", ["--list-runtimes"]).toString().split("\n").filter(l => /^Microsoft\.NETCore\.App 9\.0\.\d+ /.test(l));
  if (rt.length === 0) throw new Error("no Microsoft.NETCore.App 9.0.x runtime installed (dotnet --list-runtimes)");
  return { clone, commit, runtime: rt[rt.length - 1].replace(/ \[.*$/, "") };
}

/** Runs the driver with args; the environment is checked first. */
export function runDriver(args, opts = {}) {
  checkEnvironment();
  return execFileSync("dotnet", [driver, ...args], { maxBuffer: 1 << 30, ...opts }).toString("utf8");
}
