// Contract: https://www.zhi168.it.com/console/api-docs, checked 2026-09-30.
// Model spelling and limits come from GET /api/v1/models. Prices remain admin-owned.
const WIDE_RATIOS = ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16"];
const BASIC_RATIOS = ["16:9", "9:16", "1:1"];
const IMAGE_RATIOS = ["auto", "1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"];
const VIDEO_MODELS = {
  "Sd-2.0满血933": { min: 4, max: 15, seconds: 4, resolution: "720p", resolutions: ["720p", "1080p", "4k"], ratios: WIDE_RATIOS, prompt: 5000, images: 9, audio: 3, videos: 0 },
  "minmax-h3": { min: 4, max: 15, seconds: 15, resolution: "2k", resolutions: ["768p", "2k"], ratios: BASIC_RATIOS, prompt: 4000, images: 9, audio: 3, videos: 3, requiresImage: true },
  "Sd-2.5": { min: 4, max: 30, seconds: 5, resolution: "720p", resolutions: ["480p", "720p", "1080p"], ratios: WIDE_RATIOS, prompt: 15000, images: 30, audio: 10, videos: 0 },
  "Sd-2.0fast": { min: 4, max: 15, seconds: 4, resolution: "720p", resolutions: ["720p", "1080p"], ratios: WIDE_RATIOS, prompt: 5000, images: 9, audio: 3, videos: 0 },
  "Sd-2.0mini": { min: 4, max: 15, seconds: 5, resolution: "480p", resolutions: ["480p", "720p"], ratios: BASIC_RATIOS, prompt: 2000, images: 9, audio: 3, videos: 3, perTask: true },
  "wan-3.0": { min: 5, max: 30, seconds: 5, resolution: "1080p", resolutions: ["1080p"], ratios: BASIC_RATIOS, prompt: 15000, images: 10, audio: 5, videos: 5 },
};
const IMAGE_MODELS = ["Nano_Banana_2img", "gpt-image-2 z", "gpt-image-2.5z", "gpt-image-2.5-flare", "gpt-image-2.5-sunburst"];
const VIDEO_USAGE = {
  seconds: { type: "number", unit: "second", description: { en: "Video generation unit price", zh: "视频生成单价" } },
  resolution: { enum: ["480p", "720p", "768p", "1080p", "2k", "4k"], description: { en: "Output video resolution", zh: "输出视频分辨率" } },
  with_audio: { type: "boolean", description: { en: "Whether audio is generated", zh: "是否生成音频" } },
};
const IMAGE_USAGE = {
  image_count: { type: "number", unit: "count", unitLabel: { en: "image", zh: "张" }, description: { en: "Image generation unit price", zh: "图片生成单价" } },
};
const VIDEO_TASK_USAGE = {
  video_count: { type: "number", unit: "count", unitLabel: { en: "video", zh: "条" }, description: { en: "Video generation unit price", zh: "视频生成单价" } },
  resolution: { enum: ["480p", "720p"], description: VIDEO_USAGE.resolution.description },
  with_audio: VIDEO_USAGE.with_audio,
};

export const meta = {
  apiVersion: 1,
  key: "mengwuxian",
  name: "梦无限",
  icon: "text:梦",
  version: "1.0.2",
  author: { name: "Niu Dali" },
  description: { en: "Video and image generation via Mengwuxian", zh: "通过梦无限生成视频和图片" },
  website: "https://www.zhi168.it.com",
  baseUrl: "https://www.zhi168.it.com",
  auth: "api_key",
  models: Object.keys(VIDEO_MODELS).concat(IMAGE_MODELS),
  fetchMode: "per_task",
  protocols: [{ name: "openai_video", models: Object.keys(VIDEO_MODELS) }, { name: "openai_image", models: IMAGE_MODELS }],
  routes: [
    { method: "POST", path: "/mengwuxian/api/v1/video-tasks", type: "submit", decode: "createVideo", render: "taskStatus" },
    { method: "POST", path: "/mengwuxian/api/v1/video-tasks/multipart", type: "submit", decode: "createVideo", render: "taskStatus" },
    { method: "GET", path: "/mengwuxian/api/v1/video-tasks/:task_id", type: "query", render: "taskStatus" },
    { method: "POST", path: "/mengwuxian/api/v1/image-tasks", type: "submit", decode: "createImage", render: "taskStatus" },
    { method: "POST", path: "/mengwuxian/api/v1/image-tasks/multipart", type: "submit", decode: "createImage", render: "taskStatus" },
    { method: "GET", path: "/mengwuxian/api/v1/image-tasks/:task_id", type: "query", render: "taskStatus" },
  ],
  usageSchema: VIDEO_USAGE,
  usageProfiles: [{ models: ["Sd-2.0mini"], schema: VIDEO_TASK_USAGE }, { models: IMAGE_MODELS, schema: IMAGE_USAGE }],
};

function modelConfig(model, kind) {
  if (kind === "image" && IMAGE_MODELS.includes(model)) return { prompt: 2000, images: 8, audio: 0, videos: 0, ratios: IMAGE_RATIOS };
  if (kind === "video" && Object.prototype.hasOwnProperty.call(VIDEO_MODELS, model)) return VIDEO_MODELS[model];
  throw new Error("Unsupported Mengwuxian " + kind + " model");
}

function mediaKind(action) {
  if (action === "image") return "image";
  if (["text_to_video", "image_to_video", "reference_to_video"].includes(action)) return "video";
  throw new Error("Unsupported Mengwuxian task action");
}

function requestBody(ctx) {
  const body = ctx.body;
  if (body && body.kind === "json" && body.value && typeof body.value === "object" && !Array.isArray(body.value)) return body.value;
  if (!body || !["form", "multipart"].includes(body.kind)) throw new Error("JSON or multipart body required");
  const value = {};
  for (const key of Object.keys(body.fields || {})) {
    const values = body.fields[key];
    if (values.length !== 1) throw new Error("Each parameter must be provided once: " + key);
    let field = values[0];
    if (["reference_image_urls", "audio_urls", "video_urls"].includes(key)) {
      try { field = JSON.parse(field); } catch (_) { throw new Error(key + " must be a JSON array"); }
    }
    if (key === "with_audio") {
      if (!["true", "false"].includes(field)) throw new Error("with_audio must be a boolean");
      field = field === "true";
    }
    value[key] = field;
  }
  return value;
}

function requestParameter(req, keys, fallback) {
  const present = keys.filter(key => req[key] !== undefined);
  if (present.length > 1) throw new Error("Use only one of " + keys.join(", "));
  return present.length ? req[present[0]] : fallback;
}

function integerParameter(value, min, max, name) {
  if ((typeof value !== "number" && typeof value !== "string") || !/^[0-9]+$/.test(String(value))) throw new Error(name + " must be an integer");
  const number = Number(value);
  if (!Number.isSafeInteger(number) || number < min || number > max) throw new Error(name + " must be between " + min + " and " + max);
  return number;
}

function mediaURL(value) {
  // The host additionally enforces its media SSRF and redirect policy on downloads.
  return typeof value === "string" && /^https?:\/\/[^\s/@?#\\]+(?::[0-9]+)?(?:[/?#][^\s\\]*)?$/.test(value);
}

function referenceURLs(value, max, name) {
  if (value === undefined) return [];
  if (!Array.isArray(value) || value.length > max || value.some(url => !mediaURL(url))) throw new Error(name + " must contain at most " + max + " HTTP(S) URLs");
  return value.slice();
}

function referenceFiles(files, kind, config) {
  const accepted = kind === "video" ? ["input_reference", "reference_images"] : ["image", "image[]", "reference_images"];
  const references = [];
  for (const file of files || []) {
    if (!accepted.includes(file.field)) throw new Error("Unsupported upload field: " + file.field);
    if (!["image/png", "image/jpeg", "image/webp"].includes(file.mimeType)) throw new Error("Reference images must be PNG, JPEG or WebP");
    if (!Number.isInteger(file.size) || file.size <= 0 || file.size > 10 * 1024 * 1024) throw new Error("Reference images must be at most 10 MiB");
    references.push({ name: "reference_images", fileRef: file.ref, filename: file.filename });
  }
  if (references.length > config.images) throw new Error("Too many reference images");
  return references;
}

function mediaRequest(req, model, kind, files) {
  const config = modelConfig(model, kind);
  if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("Request must be an object");
  const allowed = ["model", "model_code", "prompt", "size", "aspect_ratio", "reference_image_urls", "user"];
  if (kind === "video") allowed.push("seconds", "duration", "duration_seconds", "resolution", "with_audio", "input_reference", "audio_urls", "video_urls");
  else allowed.push("n", "image_count", "image", "response_format", "quality", "resolution");
  for (const key of Object.keys(req)) {
    if (!allowed.includes(key)) throw new Error("Unsupported parameter: " + key);
  }
  if (typeof req.prompt !== "string") throw new Error("prompt is required");
  const prompt = req.prompt.trim();
  const promptLength = Array.from(prompt).length;
  if (promptLength < (kind === "image" ? 7 : 1) || promptLength > config.prompt) throw new Error("prompt length is outside this model's supported range");
  let aspect = req.aspect_ratio;
  let resolution = req.resolution;
  if (req.size !== undefined) {
    const dimensions = { "1280x720": ["16:9", "720p"], "720x1280": ["9:16", "720p"], "1920x1080": ["16:9", "1080p"], "1080x1920": ["9:16", "1080p"] };
    if (kind === "video" && Object.prototype.hasOwnProperty.call(dimensions, req.size)) {
      const mapped = dimensions[req.size];
      if (resolution !== undefined && resolution !== mapped[1]) throw new Error("size conflicts with resolution");
      resolution = mapped[1];
      if (aspect !== undefined && aspect !== mapped[0]) throw new Error("size conflicts with aspect_ratio");
      aspect = mapped[0];
    } else {
      const size = kind === "image" && req.size === "1024x1024" ? "1:1" : req.size;
      if (!config.ratios.includes(size)) throw new Error("Unsupported size; use an aspect ratio supported by this model");
      if (aspect !== undefined && aspect !== size) throw new Error("size conflicts with aspect_ratio");
      aspect = size;
    }
  }
  if (aspect === undefined) aspect = kind === "image" ? "1:1" : "16:9";
  if (!config.ratios.includes(aspect)) throw new Error("Unsupported aspect_ratio");
  const singular = kind === "video" ? req.input_reference : req.image;
  if (singular !== undefined && req.reference_image_urls !== undefined) throw new Error("Use one reference image parameter");
  const images = referenceURLs(singular === undefined ? req.reference_image_urls : (Array.isArray(singular) ? singular : [singular]), config.images, "reference_image_urls");
  const uploaded = referenceFiles(files, kind, config);
  if (images.length + uploaded.length > config.images) throw new Error("Too many reference images");
  // The native docs confirm multipart files but do not specify URL-array form
  // encoding. Keep URL inputs on the documented JSON transport.
  if (uploaded.length && (images.length || (req.audio_urls || []).length || (req.video_urls || []).length)) throw new Error("Use uploaded images or media URLs in one request; mixing them is not supported");
  if (config.requiresImage && !images.length && !uploaded.length) throw new Error("This model requires a reference image");
  const normalized = { model_code: model, prompt, aspect_ratio: aspect, reference_image_urls: images };
  if (kind === "image") {
    normalized.image_count = integerParameter(requestParameter(req, ["n", "image_count"], 1), 1, 1, "image_count");
    if (req.response_format !== undefined && !["url", "b64_json"].includes(req.response_format)) throw new Error("Unsupported response_format");
    // Native image documentation has no quality/resolution selector. Refuse an
    // unverified paid tier instead of silently forwarding a field it may ignore.
    if (resolution !== undefined && resolution !== "1k") throw new Error("The native image API currently supports only the default resolution");
    if (req.quality !== undefined && !["auto", "standard", "1k"].includes(req.quality)) throw new Error("The native image API currently supports only default quality");
    return normalized;
  }
  normalized.duration_seconds = integerParameter(requestParameter(req, ["seconds", "duration", "duration_seconds"], config.seconds), config.min, config.max, "duration_seconds");
  normalized.resolution = resolution === undefined ? config.resolution : resolution;
  if (!config.resolutions.includes(normalized.resolution)) throw new Error("Unsupported resolution");
  normalized.with_audio = req.with_audio === undefined ? true : req.with_audio;
  if (typeof normalized.with_audio !== "boolean") throw new Error("with_audio must be a boolean");
  normalized.audio_urls = referenceURLs(req.audio_urls, config.audio, "audio_urls");
  normalized.video_urls = referenceURLs(req.video_urls, config.videos, "video_urls");
  return normalized;
}

function submitIntent(ctx, kind) {
  const req = requestBody(ctx);
  const model = ctx.model || req.model_code;
  const upstreamModel = ctx.upstreamModel || model;
  const files = ctx.body.files || [];
  const normalized = mediaRequest(req, upstreamModel, kind, files);
  if (kind === "image" && ctx.operation === "edit" && !normalized.reference_image_urls.length && !files.length) throw new Error("Image edits require a reference image");
  let action = "image";
  if (kind === "video") {
    action = normalized.audio_urls.length || normalized.video_urls.length ? "reference_to_video"
      : normalized.reference_image_urls.length || files.length ? "image_to_video" : "text_to_video";
  }
  return { kind: "submit", model, action, requestBody: normalized };
}

function apiRequest(ctx, path) {
  const base = String(ctx.baseUrl || "").replace(/\/+$/, "");
  if (!/^https:\/\/[^\s/@?#\\]+(?:\/[^\s?#\\]*)?$/.test(base)) throw new Error("Mengwuxian Base URL must use HTTPS without credentials, query or fragment");
  if (typeof ctx.apiKey !== "string" || !ctx.apiKey.trim() || /[\r\n]/.test(ctx.apiKey)) throw new Error("Mengwuxian API key is required");
  return { url: base + "/api/v1/" + path, method: "GET", headers: { "X-API-Key": ctx.apiKey } };
}

export function buildSubmitRequest(ctx) {
  const kind = mediaKind(ctx.action);
  const model = ctx.upstreamModel || ctx.model;
  const body = mediaRequest(ctx.requestBody, model, kind, ctx.files);
  const files = referenceFiles(ctx.files, kind, modelConfig(model, kind));
  const request = apiRequest(ctx, kind + "-tasks" + (files.length ? "/multipart" : ""));
  request.method = "POST";
  if (typeof ctx.publicTaskId === "string" && ctx.publicTaskId.length > 0 && ctx.publicTaskId.length <= 64) request.headers["Idempotency-Key"] = ctx.publicTaskId;
  if (!files.length) {
    request.headers["Content-Type"] = "application/json";
    request.body = body;
    return request;
  }
  request.bodyType = "multipart";
  request.parts = [];
  for (const key of Object.keys(body)) {
    const value = body[key];
    if (Array.isArray(value) && !value.length) continue;
    request.parts.push({ name: key, value: Array.isArray(value) ? JSON.stringify(value) : String(value) });
  }
  request.parts = request.parts.concat(files);
  return request;
}

function vendorTaskID(value) {
  if (typeof value === "number" && (!Number.isSafeInteger(value) || value <= 0)) throw new Error("Invalid upstream task_id");
  // Live responses use tsk_ plus 32 hex digits; the published examples still
  // use numeric IDs. Both forms are safe as a single query URL path segment.
  if (!["number", "string"].includes(typeof value) || !/^(?:[1-9][0-9]{0,19}|tsk_[a-f0-9]{32})$/.test(String(value))) throw new Error("Invalid upstream task_id");
  return String(value);
}

export function parseSubmitResponse(ctx, response) {
  const body = response.body || {};
  const taskId = vendorTaskID(body.task_id);
  const result = { taskId, taskData: body, state: { usage: extractUsage(ctx) } };
  const immediate = parseTaskResult({ taskId }, body);
  if (["SUCCESS", "FAILURE"].includes(immediate.status)) result.immediate = immediate;
  return result;
}

export function buildQueryRequest(ctx) {
  const kind = mediaKind(ctx.action);
  // Task lookup depends only on its media kind and ID. Historical tasks remain
  // retrievable when model metadata is unavailable or the model is retired.
  return apiRequest(ctx, kind + "-tasks/" + vendorTaskID(ctx.taskId));
}

export function parseTaskResult(ctx, body) {
  const states = { pending: "QUEUED", submitted: "SUBMITTED", processing: "IN_PROGRESS", succeeded: "SUCCESS", failed: "FAILURE", canceled: "FAILURE" };
  if (!body || !Object.prototype.hasOwnProperty.call(states, body.status)) return { status: "UNKNOWN" };
  if (body.task_id !== undefined && ctx.taskId !== undefined && vendorTaskID(body.task_id) !== String(ctx.taskId)) return { status: "UNKNOWN" };
  const status = states[body.status];
  if (status === "FAILURE") return { status, reason: typeof body.error_message === "string" && body.error_message ? body.error_message : "Mengwuxian task failed or was canceled" };
  if (status !== "SUCCESS") return { status };
  if (!mediaURL(body.result_url)) return { status: "UNKNOWN", reason: "Completed task has no valid result URL" };
  return { status, url: body.result_url, progress: "100%" };
}

export function extractUsage(ctx) {
  const model = ctx.upstreamModel || ctx.model;
  const req = mediaRequest(ctx.requestBody, model, mediaKind(ctx.action), ctx.files);
  if (ctx.action === "image") return ctx.usagePurpose === "billing_ratios" ? {} : { image_count: 1 };
  const perTask = modelConfig(model, "video").perTask;
  if (ctx.usagePurpose === "billing_ratios") return perTask ? {} : { seconds: req.duration_seconds };
  const facts = { resolution: req.resolution, with_audio: req.with_audio };
  if (perTask) facts.video_count = 1;
  else facts.seconds = req.duration_seconds;
  return facts;
}

export function extractUsageOnComplete(ctx, result, data) {
  // Omitted facts preserve the frozen reservation. Vendor output duration and
  // charged_points are not our billing quantities; failure refunds are host-owned.
  if (ctx.action === "image" && result.status === "SUCCESS" && data && mediaURL(data.result_url)) return { image_count: 1 };
  return {};
}

export function listArtifacts(task) {
  if (task.status !== "SUCCESS" || !task.data || !mediaURL(task.data.result_url)) return [];
  const kind = mediaKind(task.action);
  return [{ key: kind, type: kind }];
}

export function buildContentRequest(ctx) {
  if (ctx.artifactKey !== mediaKind(ctx.action) || !ctx.data || !mediaURL(ctx.data.result_url)) throw new Error("artifact_not_found");
  // Vendor result URLs are signed for GET. The host suppresses the response
  // body for client HEAD requests while retaining the upstream content headers.
  return { url: ctx.data.result_url, method: "GET", credentialless: true };
}

export const native = {
  createVideo(ctx) { return submitIntent(ctx, "video"); },
  createImage(ctx) { return submitIntent(ctx, "image"); },
  taskStatus(_ctx, task) {
    const states = { NOT_START: "pending", SUBMITTED: "submitted", QUEUED: "pending", IN_PROGRESS: "processing", SUCCESS: "succeeded", FAILURE: "failed" };
    return { task_id: task.task_id, status: states[task.status] || "unknown", result_url: task.status === "SUCCESS" && task.data ? task.data.result_url || "" : "", error_message: task.fail_reason || "" };
  },
};

export const protocols = {
  openai_video: {
    decodeRequest(ctx) { return submitIntent(ctx, "video"); },
    render(_ctx, task) { return { video_url: task.status === "SUCCESS" && task.data ? task.data.result_url || "" : "" }; },
  },
  openai_image: {
    decodeRequest(ctx) { return submitIntent(ctx, "image"); },
    render(_ctx, task) {
      if (task.status !== "SUCCESS" || !task.data || !mediaURL(task.data.result_url)) throw new Error("Image task has no result");
      return { data: [{ url: task.data.result_url }] };
    },
  },
};
