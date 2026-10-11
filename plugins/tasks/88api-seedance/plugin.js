// Wire contract: docs/88api-seedance-video-guide.md, verified 2026-10-11.
// Prices are administrator-owned; duration is a requested usage quantity.
const MODELS = {
  "seedance-2.5官方版": { maxSeconds: 30, seconds: 4, maxImages: 30, resolutions: ["720p"], resolution: "720p" },
  "seedance-2.0官方版": { maxSeconds: 15, seconds: 4, maxImages: 9, resolutions: ["720p"], resolution: "720p" },
  "seedance-2.0-fast官方版": { maxSeconds: 15, seconds: 4, maxImages: 9, resolutions: ["720p"], resolution: "720p" },
  "seedance-2.0-mini官方版": { maxSeconds: 15, seconds: 5, maxImages: 9, resolutions: ["480p", "720p"], resolution: "480p", noFrames: true },
};
const RATIOS = ["16:9", "9:16", "1:1", "4:3", "3:4", "21:9"];
const SECONDS_USAGE = { type: "number", unit: "second", description: { en: "Video generation unit price", zh: "视频生成单价" } };
const RESOLUTION_DESCRIPTION = { en: "Output video resolution", zh: "输出视频分辨率" };

export const meta = {
  apiVersion: 1,
  key: "88api-seedance",
  name: "88API Seedance",
  icon: "text:88",
  version: "1.0.0",
  author: { name: "Niu Dali" },
  description: { en: "88API official Seedance text and image video generation", zh: "88API Seedance 官方版文字及图片视频生成" },
  baseUrl: "https://api.88api.ai",
  auth: "api_key",
  upstreams: ["vendor", "new_api"],
  models: Object.keys(MODELS),
  fetchMode: "per_task",
  protocols: ["openai_video"],
  usageSchema: {
    seconds: SECONDS_USAGE,
    resolution: { enum: ["720p"], description: RESOLUTION_DESCRIPTION },
  },
  usageProfiles: [{
    models: ["seedance-2.0-mini官方版"],
    schema: { seconds: SECONDS_USAGE, resolution: { enum: ["480p", "720p"], description: RESOLUTION_DESCRIPTION } },
  }],
};

function modelConfig(model) {
  if (!Object.prototype.hasOwnProperty.call(MODELS, model)) throw new Error("Unsupported 88API Seedance model");
  return MODELS[model];
}

function mediaURL(value) {
  // Result downloads additionally pass through the host's media SSRF policy.
  return typeof value === "string" && /^https?:\/\/[^\s/@?#\\]+(?::[0-9]+)?(?:[/?#][^\s\\]*)?$/.test(value);
}

function imageFiles(files) {
  if (!Array.isArray(files) || files.length > 1) throw new Error("Only one input_reference file is supported");
  // Local uploads share the workspace's 10 MiB engineering limit; the guide
  // does not specify a provider limit for remote reference image URLs.
  for (const file of files) {
    if (file.field !== "input_reference" || typeof file.ref !== "string" || !file.ref) throw new Error("Unsupported image file field");
    if (!["image/png", "image/jpeg", "image/webp"].includes(file.mimeType)) throw new Error("Reference images must be PNG, JPEG or WebP");
    if (!Number.isSafeInteger(file.size) || file.size <= 0 || file.size > 10 * 1024 * 1024) throw new Error("Reference image must be at most 10 MiB");
  }
  return files;
}

function videoRequest(req, model, files) {
  const config = modelConfig(model);
  if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("Request must be an object");
  const allowed = ["model", "prompt", "duration", "seconds", "resolution", "ratio", "size", "images", "metadata", "negative_prompt", "seed"];
  for (const key of Object.keys(req)) {
    if (!allowed.includes(key)) throw new Error("Unsupported parameter: " + key);
  }
  if (typeof req.prompt !== "string" || !req.prompt.trim()) throw new Error("prompt is required");
  if (req.duration !== undefined && req.seconds !== undefined) throw new Error("Use duration or seconds, not both");
  let duration = config.seconds;
  if (req.duration !== undefined) {
    if (typeof req.duration !== "number" || !Number.isSafeInteger(req.duration)) throw new Error("duration must be an integer");
    duration = req.duration;
  } else if (req.seconds !== undefined) {
    if (typeof req.seconds !== "string" || !/^[0-9]+$/.test(req.seconds)) throw new Error("seconds must be an integer string");
    duration = Number(req.seconds);
  }
  if (!Number.isSafeInteger(duration) || duration < 4 || duration > config.maxSeconds) throw new Error("duration must be between 4 and " + config.maxSeconds);
  const resolution = req.resolution === undefined ? config.resolution : req.resolution;
  if (!config.resolutions.includes(resolution)) throw new Error("Unsupported resolution for this model");
  // The existing video workspace calls its aspect-ratio field size. Only the
  // documented ratio values are accepted and sent upstream as top-level ratio.
  if (req.size !== undefined && req.ratio !== undefined) throw new Error("Use ratio or size, not both");
  const ratio = req.ratio === undefined ? (req.size === undefined ? "16:9" : req.size) : req.ratio;
  if (!RATIOS.includes(ratio)) throw new Error("Unsupported ratio");
  const uploaded = imageFiles(files || []);
  const body = { model, prompt: req.prompt.trim(), duration, resolution, ratio };
  if (req.images !== undefined) {
    if (!Array.isArray(req.images) || req.images.length > config.maxImages || req.images.some(url => !mediaURL(url))) throw new Error("images must contain at most " + config.maxImages + " HTTP(S) URLs");
    if (uploaded.length) throw new Error("Use images or input_reference, not both");
    body.images = req.images.slice();
  }
  if (req.metadata !== undefined) {
    const metadata = req.metadata;
    if (!metadata || typeof metadata !== "object" || Array.isArray(metadata)) throw new Error("metadata must be an object");
    for (const key of Object.keys(metadata)) {
      if (!["firstFrame", "lastFrame"].includes(key)) throw new Error("Unsupported metadata parameter: " + key);
    }
    const hasFirst = metadata.firstFrame !== undefined;
    const hasLast = metadata.lastFrame !== undefined;
    if (hasLast && !hasFirst) throw new Error("lastFrame requires firstFrame");
    if (hasFirst && !mediaURL(metadata.firstFrame) || hasLast && !mediaURL(metadata.lastFrame)) throw new Error("Frame images must be HTTP(S) URLs");
    if (hasFirst || hasLast) {
      if (config.noFrames) throw new Error("Frame mode is not confirmed for the mini model");
      if (uploaded.length || req.images !== undefined) throw new Error("Frame mode cannot be mixed with reference images");
      body.metadata = { firstFrame: metadata.firstFrame };
      if (hasLast) body.metadata.lastFrame = metadata.lastFrame;
    }
  }
  if (req.negative_prompt !== undefined) {
    if (typeof req.negative_prompt !== "string") throw new Error("negative_prompt must be a string");
    body.negative_prompt = req.negative_prompt;
  }
  if (req.seed !== undefined) {
    if (typeof req.seed !== "number" || !Number.isSafeInteger(req.seed)) throw new Error("seed must be an integer");
    body.seed = req.seed;
  }
  return body;
}

function apiRequest(ctx, path) {
  const base = String(ctx.baseUrl || "").replace(/\/+$/, "");
  if (!/^https?:\/\/[^\s/@?#\\]+(?:\/[^\s?#\\]*)?$/.test(base)) throw new Error("Invalid channel Base URL");
  if (typeof ctx.apiKey !== "string" || !ctx.apiKey.trim() || /[\r\n]/.test(ctx.apiKey)) throw new Error("Channel API key is required");
  return { url: base + "/v1/videos" + path, method: "GET", headers: { Authorization: "Bearer " + ctx.apiKey } };
}

export function buildSubmitRequest(ctx) {
  const files = imageFiles(ctx.files || []);
  const body = videoRequest(ctx.requestBody, ctx.upstreamModel || ctx.model, files);
  const request = apiRequest(ctx, "");
  request.method = "POST";
  if (!files.length) {
    request.headers["Content-Type"] = "application/json";
    request.body = body;
    return request;
  }
  request.bodyType = "multipart";
  request.parts = [];
  for (const key of Object.keys(body)) {
    request.parts.push({ name: key, value: typeof body[key] === "object" ? JSON.stringify(body[key]) : body[key] });
  }
  request.parts.push({ name: "input_reference", fileRef: files[0].ref, filename: files[0].filename });
  return request;
}

function taskID(value) {
  if (typeof value !== "string" || !value.trim() || value !== value.trim() || value === "." || value === ".." || value.length > 256 || /\s/.test(value)) throw new Error("Upstream id is required");
  for (const character of value) {
    if (character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127) throw new Error("Upstream id is required");
  }
  return value;
}

export function parseSubmitResponse(ctx, response) {
  const body = response.body || {};
  const taskId = taskID(body.id);
  const result = { taskId, taskData: body };
  const immediate = parseTaskResult({ taskId }, body);
  if (["SUCCESS", "FAILURE"].includes(immediate.status)) result.immediate = immediate;
  return result;
}

export function extractUsage(ctx) {
  const body = videoRequest(ctx.requestBody, ctx.upstreamModel || ctx.model, ctx.files || []);
  // Legacy per-call billing must not multiply its price by every numeric fact.
  if (ctx.usagePurpose === "billing_ratios") return null;
  return { seconds: body.duration, resolution: body.resolution };
}

export function extractUsageOnComplete() {
  // The guide does not define measured billable seconds. Keep the validated
  // submission duration and resolution frozen; failure refunds are host-owned.
  return {};
}

export function buildQueryRequest(ctx) {
  return apiRequest(ctx, "/" + encodeURIComponent(taskID(ctx.taskId)));
}

function resultURL(body) {
  for (const key of ["url", "video_url", "result_url"]) {
    if (body && mediaURL(body[key])) return body[key];
  }
  return "";
}

export function parseTaskResult(ctx, body) {
  if (!body || typeof body !== "object" || Array.isArray(body)) return { status: "UNKNOWN" };
  if (body.id !== undefined && body.id !== ctx.taskId) return { status: "UNKNOWN", reason: "Upstream task id mismatch" };
  const statuses = { queued: "QUEUED", in_progress: "IN_PROGRESS", completed: "SUCCESS", failed: "FAILURE" };
  const status = Object.prototype.hasOwnProperty.call(statuses, body.status) ? statuses[body.status] : "UNKNOWN";
  const result = { status };
  if (status === "UNKNOWN") result.reason = "Unrecognized upstream status";
  if (status === "FAILURE") result.reason = body.error && typeof body.error.message === "string" ? body.error.message : "Video generation failed";
  if (status === "SUCCESS") {
    const url = resultURL(body);
    if (!url) return { status: "UNKNOWN", reason: "Completed task has no valid result URL" };
    result.url = url;
    result.progress = "100%";
  } else if (typeof body.progress === "number" && Number.isFinite(body.progress) && body.progress >= 0 && body.progress <= 100) {
    result.progress = body.progress + "%";
  }
  return result;
}

export function listArtifacts(task) {
  return task.status === "SUCCESS" && resultURL(task.data) ? [{ key: "video", type: "video" }] : [];
}

export function buildContentRequest(ctx) {
  const url = resultURL(ctx.data);
  if (ctx.artifactKey !== "video" || !url) throw new Error("artifact_not_found");
  // Signed result URLs must remain byte-for-byte intact. Never send channel
  // credentials to the CDN; the host handles media redirects and client HEAD.
  return { url, method: "GET", credentialless: true };
}

export const protocols = {
  openai_video: {
    decodeRequest(ctx) {
      if (!ctx.body || !["json", "multipart"].includes(ctx.body.kind)) throw new Error("JSON or multipart body required");
      let req = ctx.body.value;
      if (ctx.body.kind === "multipart") {
        req = {};
        for (const key of Object.keys(ctx.body.fields || {})) {
          const values = ctx.body.fields[key];
          if (!Array.isArray(values) || values.length !== 1) throw new Error("Each parameter must be provided once: " + key);
          let value = values[0];
          if (["metadata", "images"].includes(key)) {
            try { value = JSON.parse(value); } catch (e) { throw new Error(key + " must contain valid JSON", { cause: e }); }
          } else if (["duration", "seed"].includes(key)) {
            if (!/^-?[0-9]+$/.test(value)) throw new Error(key + " must be an integer");
            value = Number(value);
          }
          req[key] = value;
        }
      }
      const model = ctx.model || (req && req.model);
      const requestBody = videoRequest(req, ctx.upstreamModel || model, ctx.body.files || []);
      const images = (requestBody.images || []).length || requestBody.metadata || (ctx.body.files || []).length;
      return { kind: "submit", model, action: images ? "image_to_video" : "text_to_video", requestBody };
    },
    render(_ctx, task) {
      const output = {};
      if (task.data && typeof task.data === "object" && !Array.isArray(task.data)) {
        // Host presentation owns identity/status; preserve vendor result URLs
        // and error codes, without modifying the persisted provider snapshot.
        Object.assign(output, task.data);
      }
      return output;
    },
  },
};
