#!/usr/bin/env node

/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const MODEL = "grok-imagine-video-1.5";
const RESOLUTION = "720p";
const ASPECT_RATIOS = new Set(["16:9", "9:16"]);
const FIRST_FRAME_DURATIONS = new Set([6, 12, 18, 24, 30]);
const MAX_REFERENCE_IMAGES = 6;
const MAX_REFERENCE_AUDIOS = 3;
const MAX_SERIALIZED_MEDIA_BYTES = 100 * 1024 * 1024;

const IMAGE_MIME_TYPES = new Map([
  [".avif", "image/avif"],
  [".bmp", "image/bmp"],
  [".gif", "image/gif"],
  [".jpeg", "image/jpeg"],
  [".jpg", "image/jpeg"],
  [".png", "image/png"],
  [".webp", "image/webp"],
]);

const AUDIO_MIME_TYPES = new Map([
  [".aac", "audio/aac"],
  [".flac", "audio/flac"],
  [".m4a", "audio/mp4"],
  [".mp3", "audio/mpeg"],
  [".mpeg", "audio/mpeg"],
  [".oga", "audio/ogg"],
  [".ogg", "audio/ogg"],
  [".wav", "audio/wav"],
  [".webm", "audio/webm"],
]);

class HttpError extends Error {
  constructor(message, status, body) {
    super(message);
    this.name = "HttpError";
    this.status = status;
    this.body = body;
  }
}

function printHelp() {
  console.log(`
Grok video generation example for New API

Requirements:
  Node.js 18+

Environment:
  NEW_API_BASE_URL  New API origin, default: https://geekapis.com
  NEW_API_KEY       New API token, required unless --dry-run is used

Usage:
  node docs/Grok/grok-video-example.mjs <mode> [options]

Modes:
  first-frame       Text-to-video or one optional first-frame image
  reference         Text-to-video, reference images/audio, or audio-only

Common options:
  --prompt <text>             Required video prompt
  --aspect-ratio <ratio>      16:9 or 9:16, default: 16:9
  --duration <seconds>        First-frame: 6/12/18/24/30; reference: 6
  --base-url <url>            Overrides NEW_API_BASE_URL
  --api-key <token>           Overrides NEW_API_KEY
  --poll-interval <ms>        Default: 10000
  --timeout <ms>              Default: 1200000 (20 minutes)
  --output <path>             Download destination
  --no-download               Stop after the task completes
  --dry-run                   Build and validate only; no HTTP requests
  --help                      Show this help

Media options:
  --image <path>              first-frame: at most one; reference: repeat up to 6
  --audio <path>              reference only; repeat up to 3

Examples:
  NEW_API_KEY='sk-...' node docs/Grok/grok-video-example.mjs first-frame \\
    --prompt 'Bring the frame to life' \\
    --image './first-frame.png' \\
    --duration 12

  NEW_API_KEY='sk-...' node docs/Grok/grok-video-example.mjs reference \\
    --prompt 'Use the same subject and voice' \\
    --image './subject.png' \\
    --image './scene.jpg' \\
    --audio './voice.mp3' \\
    --aspect-ratio '9:16'

  node docs/Grok/grok-video-example.mjs first-frame \\
    --prompt 'Text-only request preview' \\
    --duration 30 \\
    --dry-run
`);
}

function takeOptionValue(args, index, option) {
  const value = args[index + 1];
  if (!value || value.startsWith("--")) {
    throw new Error(`${option} requires a value`);
  }
  return value;
}

function parseInteger(value, option) {
  const number = Number(value);
  if (!Number.isInteger(number) || number < 1) {
    throw new Error(`${option} must be a positive integer`);
  }
  return number;
}

function parseArguments(argv) {
  if (argv.length === 0 || argv.includes("--help") || argv.includes("-h")) {
    return { help: true };
  }

  const rawMode = argv[0];
  let mode;
  if (rawMode === "first-frame") mode = "first-frame";
  if (rawMode === "reference" || rawMode === "reference-images") {
    mode = "reference-images";
  }
  if (!mode) {
    throw new Error("mode must be first-frame or reference");
  }

  const options = {
    mode,
    prompt: "",
    aspectRatio: "16:9",
    duration: undefined,
    baseUrl: process.env.NEW_API_BASE_URL || "https://geekapis.com",
    apiKey: process.env.NEW_API_KEY || "",
    images: [],
    audios: [],
    pollIntervalMs: 10_000,
    timeoutMs: 20 * 60 * 1000,
    output: "",
    noDownload: false,
    dryRun: false,
  };

  for (let index = 1; index < argv.length; index += 1) {
    const option = argv[index];
    switch (option) {
      case "--prompt":
        options.prompt = takeOptionValue(argv, index, option);
        index += 1;
        break;
      case "--aspect-ratio":
        options.aspectRatio = takeOptionValue(argv, index, option);
        index += 1;
        break;
      case "--duration":
        options.duration = parseInteger(
          takeOptionValue(argv, index, option),
          option,
        );
        index += 1;
        break;
      case "--base-url":
        options.baseUrl = takeOptionValue(argv, index, option);
        index += 1;
        break;
      case "--api-key":
        options.apiKey = takeOptionValue(argv, index, option);
        index += 1;
        break;
      case "--image":
        options.images.push(takeOptionValue(argv, index, option));
        index += 1;
        break;
      case "--audio":
        options.audios.push(takeOptionValue(argv, index, option));
        index += 1;
        break;
      case "--poll-interval":
        options.pollIntervalMs = parseInteger(
          takeOptionValue(argv, index, option),
          option,
        );
        index += 1;
        break;
      case "--timeout":
        options.timeoutMs = parseInteger(
          takeOptionValue(argv, index, option),
          option,
        );
        index += 1;
        break;
      case "--output":
        options.output = takeOptionValue(argv, index, option);
        index += 1;
        break;
      case "--no-download":
        options.noDownload = true;
        break;
      case "--dry-run":
        options.dryRun = true;
        break;
      default:
        throw new Error(`unknown option: ${option}`);
    }
  }

  return options;
}

function normalizeBaseUrl(value) {
  const baseUrl = String(value || "")
    .trim()
    .replace(/\/+$/, "");
  if (!baseUrl) throw new Error("New API base URL is required");
  if (/\/v1$/i.test(baseUrl)) {
    throw new Error("base URL must be the site origin without a trailing /v1");
  }
  return baseUrl;
}

function normalizeApiKey(value) {
  const apiKey = String(value || "").trim();
  if (!apiKey) throw new Error("NEW_API_KEY or --api-key is required");
  return apiKey.startsWith("sk-") ? apiKey : `sk-${apiKey}`;
}

function maskApiKey(value) {
  if (!value) return "<not set>";
  if (value.length <= 12) return "sk-***";
  return `${value.slice(0, 7)}...${value.slice(-4)}`;
}

function validateOptions(options) {
  if (!options.prompt.trim()) throw new Error("--prompt is required");
  if (!ASPECT_RATIOS.has(options.aspectRatio)) {
    throw new Error("--aspect-ratio must be 16:9 or 9:16");
  }

  if (options.mode === "first-frame") {
    options.duration ??= 6;
    if (!FIRST_FRAME_DURATIONS.has(options.duration)) {
      throw new Error("first-frame duration must be 6, 12, 18, 24, or 30");
    }
    if (options.images.length > 1) {
      throw new Error("first-frame mode supports at most one --image");
    }
    if (options.audios.length > 0) {
      throw new Error("first-frame mode does not support --audio");
    }
    return;
  }

  options.duration ??= 6;
  if (options.duration !== 6) {
    throw new Error("reference mode duration is fixed at 6 seconds");
  }
  if (options.images.length > MAX_REFERENCE_IMAGES) {
    throw new Error(
      `reference mode supports at most ${MAX_REFERENCE_IMAGES} images`,
    );
  }
  if (options.audios.length > MAX_REFERENCE_AUDIOS) {
    throw new Error(
      `reference mode supports at most ${MAX_REFERENCE_AUDIOS} audio files`,
    );
  }
}

function mimeTypeFor(filePath, kind) {
  const extension = path.extname(filePath).toLowerCase();
  const mapping = kind === "image" ? IMAGE_MIME_TYPES : AUDIO_MIME_TYPES;
  const mimeType = mapping.get(extension);
  if (!mimeType) {
    throw new Error(
      `unsupported ${kind} file extension: ${extension || "<none>"}`,
    );
  }
  return mimeType;
}

async function fileToDataUrl(filePath, kind) {
  const absolutePath = path.resolve(filePath);
  const mimeType = mimeTypeFor(absolutePath, kind);
  const buffer = await readFile(absolutePath);
  if (buffer.length === 0)
    throw new Error(`media file is empty: ${absolutePath}`);
  const b64 = `data:${mimeType};base64,${buffer.toString("base64")}`;
  return {
    b64,
    summary: {
      file: absolutePath,
      kind,
      mimeType,
      bytes: buffer.length,
      serializedBytes: Buffer.byteLength(b64, "utf8"),
      base64Characters: b64.length - b64.indexOf(",") - 1,
    },
  };
}

function log(message, data) {
  const prefix = `[${new Date().toISOString()}]`;
  console.log(`${prefix} ${message}`);
  if (data !== undefined) console.log(JSON.stringify(data, null, 2));
}

function summarizeDataUrl(value) {
  if (typeof value !== "string") return value;
  const match = /^data:([^;,]+);base64,(.*)$/s.exec(value);
  if (!match) return "<invalid data URL>";
  return `<${match[1]} Data URL, ${match[2].length} base64 chars>`;
}

function summarizePayload(payload) {
  const summary = { ...payload };
  if (payload.image) {
    summary.image = { b64: summarizeDataUrl(payload.image.b64) };
  }
  if (payload.reference_images) {
    summary.reference_images = payload.reference_images.map((item) => ({
      b64: summarizeDataUrl(item.b64),
    }));
  }
  if (payload.reference_audios) {
    summary.reference_audios = payload.reference_audios.map((item) => ({
      b64: summarizeDataUrl(item.b64),
    }));
  }
  return summary;
}

async function buildPayload(options) {
  const [images, audios] = await Promise.all([
    Promise.all(options.images.map((file) => fileToDataUrl(file, "image"))),
    Promise.all(options.audios.map((file) => fileToDataUrl(file, "audio"))),
  ]);

  const mediaSummaries = [...images, ...audios].map((entry) => entry.summary);
  const totalSerializedBytes = mediaSummaries.reduce(
    (total, item) => total + item.serializedBytes,
    0,
  );
  if (totalSerializedBytes > MAX_SERIALIZED_MEDIA_BYTES) {
    throw new Error(
      "serialized media exceeds the 100 MB frontend-aligned limit",
    );
  }

  const payload = {
    model: MODEL,
    prompt: options.prompt,
    duration: options.duration,
    resolution: RESOLUTION,
    aspect_ratio: options.aspectRatio,
  };

  if (options.mode === "first-frame" && images[0]) {
    payload.image = { b64: images[0].b64 };
  }
  if (options.mode === "reference-images") {
    if (images.length > 0) {
      payload.reference_images = images.map((entry) => ({ b64: entry.b64 }));
    }
    if (audios.length > 0) {
      payload.reference_audios = audios.map((entry) => ({ b64: entry.b64 }));
    }
  }

  return { payload, mediaSummaries, totalSerializedBytes };
}

async function readResponseBody(response) {
  const contentType = response.headers.get("content-type") || "";
  if (contentType.includes("application/json")) {
    return response.json();
  }
  return response.text();
}

function responseErrorMessage(body, fallback) {
  if (typeof body === "string") return body || fallback;
  if (!body || typeof body !== "object") return fallback;
  if (body.error && typeof body.error === "object") {
    if (typeof body.error.message === "string") return body.error.message;
  }
  if (typeof body.error === "string") return body.error;
  if (typeof body.message === "string" && body.message) return body.message;
  return fallback;
}

async function requestJson(method, url, apiKey, body) {
  const headers = {
    Authorization: `Bearer ${apiKey}`,
    Accept: "application/json",
  };
  const init = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }

  log(`HTTP ${method} ${url}`, {
    headers: {
      ...headers,
      Authorization: `Bearer ${maskApiKey(apiKey)}`,
    },
    body: body === undefined ? undefined : summarizePayload(body),
  });

  const response = await fetch(url, init);
  const responseBody = await readResponseBody(response);
  log(`HTTP ${response.status} ${method} ${url}`, responseBody);

  if (!response.ok) {
    throw new HttpError(
      responseErrorMessage(
        responseBody,
        `request failed with HTTP ${response.status}`,
      ),
      response.status,
      responseBody,
    );
  }
  if (responseBody?.error) {
    throw new HttpError(
      responseErrorMessage(responseBody, "request failed"),
      response.status,
      responseBody,
    );
  }
  return responseBody;
}

function taskIdFrom(response) {
  if (!response || typeof response !== "object") return "";
  return String(response.id || response.task_id || "").trim();
}

function normalizeProgress(value) {
  if (typeof value !== "number" || !Number.isFinite(value)) return undefined;
  const percentage = value <= 1 ? value * 100 : value;
  return Math.max(0, Math.min(100, Math.round(percentage)));
}

function isCompletedStatus(status) {
  return ["completed", "succeeded", "success"].includes(status);
}

function isFailedStatus(status) {
  return ["failed", "canceled", "cancelled"].includes(status);
}

function isRetryablePollError(error) {
  return (
    !(error instanceof HttpError) || error.status === 429 || error.status >= 500
  );
}

function sleep(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function pollTask(baseUrl, taskId, apiKey, options) {
  const startedAt = Date.now();
  let attempt = 0;

  while (Date.now() - startedAt <= options.timeoutMs) {
    attempt += 1;
    try {
      const response = await requestJson(
        "GET",
        `${baseUrl}/v1/videos/${encodeURIComponent(taskId)}`,
        apiKey,
      );
      const status = String(response?.status || "").toLowerCase();
      log("Task status", {
        attempt,
        taskId,
        status,
        progress: normalizeProgress(response?.progress),
        elapsedMs: Date.now() - startedAt,
      });

      if (isCompletedStatus(status)) return response;
      if (isFailedStatus(status)) {
        throw new HttpError(
          responseErrorMessage(response, `video task ${status}`),
          422,
          response,
        );
      }
      if (!status) {
        throw new HttpError(
          "task response did not include status",
          422,
          response,
        );
      }
    } catch (error) {
      if (!isRetryablePollError(error)) throw error;
      log("Transient polling error; retrying", {
        message: error instanceof Error ? error.message : String(error),
      });
    }

    await sleep(options.pollIntervalMs);
  }

  throw new Error(`video task timed out after ${options.timeoutMs} ms`);
}

function extensionFromContentType(contentType) {
  const normalized = String(contentType || "").toLowerCase();
  if (normalized.includes("video/webm")) return ".webm";
  if (normalized.includes("video/quicktime")) return ".mov";
  if (normalized.includes("video/x-msvideo")) return ".avi";
  return ".mp4";
}

function safeFilePart(value) {
  return String(value || "video")
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 80);
}

async function downloadVideo(baseUrl, taskId, apiKey, requestedOutput) {
  const url = `${baseUrl}/v1/videos/${encodeURIComponent(taskId)}/content`;
  log(`HTTP GET ${url}`, {
    headers: { Authorization: `Bearer ${maskApiKey(apiKey)}` },
  });

  const response = await fetch(url, {
    headers: { Authorization: `Bearer ${apiKey}` },
  });
  if (!response.ok) {
    const body = await readResponseBody(response);
    throw new HttpError(
      responseErrorMessage(
        body,
        `download failed with HTTP ${response.status}`,
      ),
      response.status,
      body,
    );
  }

  const contentType = response.headers.get("content-type") || "video/mp4";
  const outputPath = requestedOutput
    ? path.resolve(requestedOutput)
    : path.resolve(
        `grok-video-${safeFilePart(taskId)}${extensionFromContentType(contentType)}`,
      );
  const buffer = Buffer.from(await response.arrayBuffer());
  await mkdir(path.dirname(outputPath), { recursive: true });
  await writeFile(outputPath, buffer);
  log("Video downloaded", {
    output: outputPath,
    bytes: buffer.length,
    contentType,
  });
  return outputPath;
}

async function main() {
  if (typeof fetch !== "function") {
    throw new Error(
      "Node.js 18+ is required because global fetch is unavailable",
    );
  }

  const options = parseArguments(process.argv.slice(2));
  if (options.help) {
    printHelp();
    return;
  }

  options.baseUrl = normalizeBaseUrl(options.baseUrl);
  validateOptions(options);
  const built = await buildPayload(options);

  log("Request prepared", {
    mode: options.mode,
    baseUrl: options.baseUrl,
    payload: summarizePayload(built.payload),
    media: built.mediaSummaries,
    totalSerializedMediaBytes: built.totalSerializedBytes,
  });

  if (options.dryRun) {
    log("Dry run complete; no HTTP request was sent");
    return;
  }

  const apiKey = normalizeApiKey(options.apiKey);
  const submitResponse = await requestJson(
    "POST",
    `${options.baseUrl}/v1/videos`,
    apiKey,
    built.payload,
  );
  const taskId = taskIdFrom(submitResponse);
  if (!taskId) throw new Error("submit response did not include id or task_id");

  log("Task submitted", { taskId });
  const completed = await pollTask(options.baseUrl, taskId, apiKey, options);
  log("Task completed", {
    taskId,
    outputUrl:
      completed?.output_url ||
      completed?.metadata?.url ||
      completed?.metadata?.remote_url,
  });

  if (options.noDownload) {
    log("Skipping download because --no-download was provided");
    return;
  }
  await downloadVideo(options.baseUrl, taskId, apiKey, options.output);
}

main().catch((error) => {
  console.error(
    `[${new Date().toISOString()}] ${
      error instanceof Error ? error.stack || error.message : String(error)
    }`,
  );
  process.exitCode = 1;
});
