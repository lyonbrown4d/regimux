import { execFile, spawn } from "node:child_process";
import { promisify } from "node:util";

import {
  aggregateResources,
  augmentReport,
  parseDockerInspect,
  parseDockerStats,
} from "./resources.js";

const execFileAsync = promisify(execFile);
const defaults = {
  intervalMs: 1000,
  timeoutMs: 10000,
  service: "regimux",
};

async function main(args) {
  const options = parseOptions(args);
  const samples = [await collectSample(options)];
  const child = spawn(options.command[0], options.command.slice(1), {
    stdio: "inherit",
    windowsHide: true,
  });
  const commandResult = monitorProcess(child);
  let finished = false;
  const observedResult = commandResult.then((result) => {
    finished = true;
    return result;
  });

  while (!finished) {
    await Promise.race([observedResult, delay(options.intervalMs)]);
    if (!finished) {
      await appendSample(samples, options);
    }
  }
  await appendSample(samples, options);

  const resources = aggregateResources(samples, options.intervalMs, options.service);
  let reportError;
  try {
    await augmentReport(
      options.reportBase + ".json",
      options.reportBase + ".md",
      resources,
    );
  } catch (error) {
    reportError = new Error("augmenting stress report", { cause: error });
  }

  const commandError = await observedResult;
  const failures = [commandError, reportError].filter(Boolean);
  if (failures.length > 0) {
    throw new AggregateError(failures, "stress run failed");
  }
}

function parseOptions(args) {
  const separator = args.indexOf("--");
  const flagArgs = separator === -1 ? args : args.slice(0, separator);
  const command = separator === -1 ? [] : args.slice(separator + 1);
  const options = { ...defaults, command };

  for (let index = 0; index < flagArgs.length; index += 2) {
    const name = flagArgs[index];
    const value = flagArgs[index + 1];
    if (value === undefined) {
      throw new Error("missing value for " + name);
    }
    switch (name) {
      case "--compose-file":
        options.composeFile = value;
        break;
      case "--service":
        options.service = value;
        break;
      case "--report":
        options.reportBase = value;
        break;
      case "--interval":
        options.intervalMs = parseDuration(value);
        break;
      case "--command-timeout":
        options.timeoutMs = parseDuration(value);
        break;
      default:
        throw new Error("unknown option: " + name);
    }
  }

  if (!options.composeFile) {
    throw new Error("--compose-file is required");
  }
  if (!options.reportBase) {
    throw new Error("--report is required");
  }
  if (options.command.length === 0) {
    throw new Error("load command is required after --");
  }
  return options;
}

function parseDuration(value) {
  const match = String(value).match(/^(\d+(?:\.\d+)?)(ms|s|m)?$/);
  if (!match) {
    throw new Error("invalid duration: " + value);
  }
  const factors = { ms: 1, s: 1000, m: 60000 };
  const duration = Number(match[1]) * factors[match[2] || "ms"];
  if (!Number.isFinite(duration) || duration <= 0) {
    throw new Error("duration must be positive: " + value);
  }
  return duration;
}

async function resolveContainer(options) {
  const output = await dockerOutput([
    "compose",
    "-f",
    options.composeFile,
    "ps",
    "-q",
    options.service,
  ], options.timeoutMs);
  const containerIDs = output.trim().split(/\s+/).filter(Boolean);
  if (containerIDs.length !== 1) {
    throw new Error(
      "resolving " + options.service + " container returned " +
      containerIDs.length + " containers",
    );
  }
  return containerIDs[0];
}

async function collectSample(options) {
  const containerID = await resolveContainer(options);
  const [statsData, inspectData] = await Promise.all([
    dockerOutput([
      "stats",
      "--no-stream",
      "--format",
      "{{json .}}",
      containerID,
    ], options.timeoutMs),
    dockerOutput(["inspect", containerID], options.timeoutMs),
  ]);
  const sample = parseDockerStats(statsData);
  return Object.assign(sample, parseDockerInspect(inspectData));
}

async function appendSample(samples, options) {
  try {
    samples.push(await collectSample(options));
  } catch (error) {
    console.warn("skipping resource sample:", error.message);
  }
}

async function dockerOutput(args, timeoutMs) {
  try {
    const result = await execFileAsync("docker", args, {
      encoding: "utf8",
      maxBuffer: 1024 * 1024,
      timeout: timeoutMs,
      windowsHide: true,
    });
    return result.stdout;
  } catch (error) {
    const detail = String(error.stderr || error.message).trim();
    throw new Error("docker " + args[0] + " failed: " + detail, { cause: error });
  }
}

function monitorProcess(child) {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (error) => {
      if (!settled) {
        settled = true;
        resolve(error);
      }
    };
    child.once("error", (error) => {
      finish(new Error("starting load command", { cause: error }));
    });
    child.once("close", (code, signal) => {
      if (code === 0) {
        finish(undefined);
        return;
      }
      const status = signal ? "signal " + signal : "exit code " + code;
      finish(new Error("load command failed with " + status));
    });
  });
}

function delay(milliseconds) {
  return new Promise((resolve) => {
    setTimeout(resolve, milliseconds);
  });
}

main(process.argv.slice(2)).catch((error) => {
  console.error(error);
  process.exitCode = 1;
});