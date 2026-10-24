import http from "node:http";
import { mkdir, readdir, rm, stat } from "node:fs/promises";
import { join } from "node:path";
import { Agent, Cursor } from "@cursor/sdk";

const env = (k, d) => process.env[k] ?? d;
const HOST = env("LLM_HOST", "0.0.0.0");
const PORT = Number(env("LLM_PORT", "8790"));
const TOKEN = env("LLM_TOKEN", "");
const CONCURRENCY = Number(env("LLM_CONCURRENCY", "16"));
const TIMEOUT_MS = Number(env("LLM_TIMEOUT_MS", "600000"));
const WORKSPACE = env("LLM_WORKSPACE", "/tmp/cursor-llm/ws");
const CURSOR_HOME = join(env("HOME", "/tmp"), ".cursor");

if (!process.env.CURSOR_API_KEY) throw new Error("CURSOR_API_KEY is required");
if (!TOKEN) throw new Error("LLM_TOKEN is required");
await mkdir(WORKSPACE, { recursive: true });

const catalog = new Map((await Cursor.models.list()).map((m) => [m.id, m]));

const resolveModel = (name, effort) => {
  const [id, suffixEffort, fast] = String(name ?? "").split(":");
  const m = catalog.get(id);
  if (!m) return null;
  const want = { reasoning_effort: effort ?? suffixEffort, effort: effort ?? suffixEffort, fast: fast === "fast" ? "true" : "false" };
  const params = (m.parameters ?? [])
    .filter((p) => want[p.id] && p.values?.some((v) => v.value === want[p.id]))
    .map((p) => ({ id: p.id, value: want[p.id] }));
  return { id, params };
};

const textOf = (content) =>
  Array.isArray(content) ? content.map((c) => (typeof c === "string" ? c : c?.text ?? "")).join("") : String(content ?? "");

// The account refuses Agent options.systemPrompt ("Custom system prompt is not
// enabled for this account"), so system messages travel as the head of the prompt.
const renderPrompt = (messages) => {
  const system = messages.filter((m) => m.role === "system").map((m) => textOf(m.content));
  const turns = messages.filter((m) => m.role !== "system");
  const body =
    turns.length === 1 && turns[0].role === "user"
      ? textOf(turns[0].content)
      : turns.map((m) => `${m.role.toUpperCase()}:\n${textOf(m.content)}`).join("\n\n");
  return [...system, body].filter(Boolean).join("\n\n");
};

let active = 0;
const waiting = [];
const acquire = () => (active < CONCURRENCY ? (active++, Promise.resolve()) : new Promise((r) => waiting.push(r)));
const release = () => (waiting.length ? waiting.shift()() : active--);

const withTimeout = (p, ms) =>
  Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(Object.assign(new Error("timeout"), { code: "timeout" })), ms))]);

const send = (res, status, body) => {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
};
const fail = (res, status, message) => send(res, status, { error: { message } });

const readBody = (req) =>
  new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });

const complete = async (req, res) => {
  let body;
  try {
    body = JSON.parse(await readBody(req));
  } catch {
    return fail(res, 400, "invalid JSON body");
  }
  const model = resolveModel(body.model, body.reasoning_effort);
  if (!model) return fail(res, 400, `unknown model ${JSON.stringify(body.model)}`);
  if (!Array.isArray(body.messages) || body.messages.length === 0) return fail(res, 400, "messages is required");

  const started = Date.now();
  await acquire();
  try {
    const r = await withTimeout(
      Agent.prompt(renderPrompt(body.messages), { model, tools: [], local: { cwd: WORKSPACE, settingSources: [] } }),
      TIMEOUT_MS,
    );
    const u = r.usage ?? {};
    const promptTokens = (u.inputTokens ?? 0) + (u.cacheReadTokens ?? 0);
    const msg = r.status === "finished" ? undefined : r.error?.message ?? `run ${r.status}`;
    console.log(JSON.stringify({ model: body.model, status: r.status, ms: Date.now() - started, in: promptTokens, out: u.outputTokens, error: msg }));
    if (msg) {
      if (/usage guidelines|request blocked/i.test(msg)) return send(res, 422, { error: { message: msg, type: "content_blocked" } });
      return fail(res, /rate|limit|quota/i.test(msg) ? 429 : 502, msg);
    }
    send(res, 200, {
      id: `chatcmpl-${started}`,
      object: "chat.completion",
      created: Math.floor(started / 1000),
      model: body.model,
      choices: [{ index: 0, message: { role: "assistant", content: r.result ?? "" }, finish_reason: "stop" }],
      usage: { prompt_tokens: promptTokens, completion_tokens: u.outputTokens ?? 0, total_tokens: promptTokens + (u.outputTokens ?? 0) },
    });
  } catch (e) {
    console.log(JSON.stringify({ model: body.model, status: "error", ms: Date.now() - started, error: String(e?.message ?? e) }));
    if (e?.code === "timeout") return fail(res, 504, "upstream run timed out");
    return fail(res, e?.name === "RateLimitError" ? 429 : e?.name === "AuthenticationError" ? 401 : 502, String(e?.message ?? e));
  } finally {
    release();
  }
};

const server = http.createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/healthz") return send(res, 200, { ok: true, active, waiting: waiting.length });
  if (req.headers.authorization !== `Bearer ${TOKEN}`) return fail(res, 401, "bad token");
  if (req.method === "GET" && req.url === "/v1/models") {
    return send(res, 200, { object: "list", data: [...catalog.keys()].map((id) => ({ id, object: "model" })) });
  }
  if (req.method === "POST" && req.url === "/v1/chat/completions") return complete(req, res);
  fail(res, 404, "not found");
});

// Every local run leaves a transcript and an agent store under $HOME/.cursor;
// a backfill of tens of thousands of calls would otherwise fill the disk.
const removeOlderThan = async (dir, cutoff) => {
  for (const e of await readdir(dir).catch(() => [])) {
    const path = join(dir, e);
    const st = await stat(path).catch(() => null);
    if (st && st.mtimeMs < cutoff) await rm(path, { recursive: true, force: true });
  }
};

const sweep = async () => {
  const cutoff = Date.now() - 15 * 60 * 1000;
  const projects = join(CURSOR_HOME, "projects");
  for (const p of await readdir(projects).catch(() => [])) {
    await removeOlderThan(join(projects, p, "agent-transcripts"), cutoff);
    const stores = join(projects, p, "sdk-agent-store");
    for (const s of await readdir(stores).catch(() => [])) {
      await removeOlderThan(join(stores, s, "agents"), cutoff);
    }
  }
};
setInterval(() => sweep().catch((e) => console.error("sweep:", e)), 5 * 60 * 1000).unref();

server.listen(PORT, HOST, () => console.log(`cursor-llm listening on ${HOST}:${PORT} (concurrency ${CONCURRENCY})`));
