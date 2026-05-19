#!/usr/bin/env node

const command = process.argv[2] || "";
const sourceName = process.env.SOURCE || "";
const sha = process.env.SHA || "";
const token = process.env.API_TOKEN || "dev-token";
const baseURL = (process.env.BASE_URL || "http://127.0.0.1:18081").replace(/\/+$/, "");
const apiBase = baseURL.endsWith("/api") ? baseURL : `${baseURL}/api`;

async function request(path, options = {}) {
  const response = await fetch(`${apiBase}${path}`, {
    ...options,
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      ...(options.headers || {}),
    },
  });
  const text = await response.text();
  const body = text ? JSON.parse(text) : null;
  if (!response.ok) {
    throw new Error(body?.error?.message || `${response.status} ${response.statusText}`);
  }
  return body;
}

async function sourceByName(name) {
  const sources = await request("/skill-sources");
  const source = sources.find((item) => item.name === name || item.id === name);
  if (!source) throw new Error(`skill source not found: ${name}`);
  return source;
}

async function syncAll() {
  const sources = await request("/skill-sources");
  for (const source of sources) {
    process.stdout.write(`sync ${source.name}... `);
    await request(`/skill-sources/${source.id}/sync`, { method: "POST" });
    process.stdout.write("ok\n");
  }
}

async function main() {
  if (command === "sync") {
    await syncAll();
    return;
  }
  if (command === "check") {
    const report = await request("/skill-drift");
    console.log(JSON.stringify(report, null, 2));
    if (!report.ok) process.exitCode = 1;
    return;
  }
  if (command === "update-check") {
    const sources = await request("/skill-sources/check-updates", { method: "POST" });
    console.log(JSON.stringify(sources, null, 2));
    return;
  }
  if (command === "pin") {
    if (!sourceName || !sha) throw new Error("SOURCE and SHA are required");
    const source = await sourceByName(sourceName);
    const updated = await request(`/skill-sources/${source.id}/pin`, {
      method: "POST",
      body: JSON.stringify({ sha }),
    });
    console.log(JSON.stringify(updated, null, 2));
    return;
  }
  throw new Error("usage: skills-api.mjs sync|check|update-check|pin");
}

main().catch((error) => {
  console.error(error.message);
  process.exit(1);
});
