"use strict";

const ROW = 22;
const LANE = 14;
const LEFT = 10;
const DOT = 4;
const COLORS = ["#0969da", "#1a7f37", "#bf3989", "#9a6700", "#8250df", "#cf222e", "#1b7c83", "#bc4c00", "#0550ae", "#4d2d00"];

const $ = (sel) => document.querySelector(sel);
const grid = $("#grid");
const statusEl = $("#status");
const pills = $("#pills");

let repos = [];
let rootName = "";
let intervalMs = 5000;
const panels = new Map(); // repo path -> {el, etag}
let timer = 0;

// layout assigns every commit a lane (column) the way gitk does: a lane
// holds the hash it expects next, a commit takes the lane expecting it, and
// hands that lane to its first parent. Lanes that expected the same commit
// converge into it; extra parents of a merge get their own lanes.
function layout(commits) {
  const lanes = [];
  const rows = [];
  let width = 1;
  for (const c of commits) {
    const incoming = [];
    lanes.forEach((h, j) => { if (h === c.h) incoming.push(j); });
    let col = incoming.length ? incoming[0] : freeLane(lanes);
    lanes[col] = c.h;
    const before = lanes.slice();
    for (const j of incoming) lanes[j] = null;
    lanes[col] = null;

    const out = c.p.map((p) => {
      let k = lanes.indexOf(p);
      if (k < 0) {
        k = lanes[col] === null ? col : freeLane(lanes);
        lanes[k] = p;
      }
      return k;
    });
    while (lanes.length && lanes[lanes.length - 1] == null) lanes.pop();
    // Lanes used by this row, so its text can start right after them.
    const used = Math.max(lanes.length, before.findLastIndex((h) => h != null) + 1);
    width = Math.max(width, used);
    rows.push({ col, before, incoming, out, used });
  }
  return { rows, width, truncated: lanes.some((h) => h != null) };
}

function freeLane(lanes) {
  const i = lanes.findIndex((h) => h == null);
  return i < 0 ? lanes.length : i;
}

const laneX = (j) => LEFT + j * LANE;
const color = (j) => COLORS[j % COLORS.length];

function segment(x1, y1, x2, y2) {
  if (x1 === x2) return `M${x1} ${y1}V${y2}`;
  const my = (y1 + y2) / 2;
  return `M${x1} ${y1}C${x1} ${my} ${x2} ${my} ${x2} ${y2}`;
}

function renderSvg(commits, lay, headHash) {
  const paths = new Map(); // color -> path data
  const add = (j, d) => paths.set(color(j), (paths.get(color(j)) || "") + d);
  let dots = "";

  lay.rows.forEach((row, r) => {
    const y0 = r * ROW;
    const ym = y0 + ROW / 2;
    const y1 = y0 + ROW;
    const xc = laneX(row.col);
    const hash = commits[r].h;

    row.before.forEach((h, j) => {
      if (h != null && h !== hash) add(j, segment(laneX(j), y0, laneX(j), y1));
    });
    for (const j of row.incoming) add(j, segment(laneX(j), y0, xc, ym));
    for (const k of row.out) add(k, segment(xc, ym, laneX(k), y1));

    const fill = color(row.col);
    if (hash === headHash) {
      dots += `<circle cx="${xc}" cy="${ym}" r="${DOT + 2.5}" fill="none" stroke="#1f2328" stroke-width="1.5"/>`;
    }
    dots += commits[r].p.length > 1
      ? `<circle cx="${xc}" cy="${ym}" r="${DOT - 0.5}" fill="#ffffff" stroke="${fill}" stroke-width="2"/>`
      : `<circle cx="${xc}" cy="${ym}" r="${DOT}" fill="${fill}" stroke="#ffffff" stroke-width="1.5"/>`;
  });

  const w = LEFT * 2 + (lay.width - 1) * LANE;
  const h = commits.length * ROW;
  let svg = `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" fill="none" stroke-width="2" stroke-linecap="round">`;
  for (const [c, d] of paths) svg += `<path d="${d}" stroke="${c}"/>`;
  return svg + dots + "</svg>";
}

const FOLDER = `<svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true"><path fill="currentColor" d="M1.75 1A1.75 1.75 0 0 0 0 2.75v10.5C0 14.216.784 15 1.75 15h12.5A1.75 1.75 0 0 0 16 13.25v-8.5A1.75 1.75 0 0 0 14.25 3H7.5a.25.25 0 0 1-.2-.1l-.9-1.2C6.07 1.26 5.55 1 5 1H1.75Z"/></svg>`;

// worktreeIcon marks a branch or commit checked out in a linked worktree.
const worktreeIcon = (wt) => `<span class="wt" title="checked out in worktree ${esc(wt.path)}">${FOLDER}</span>`;

function badges(c, g) {
  // Worktrees with a detached HEAD have no branch badge to carry the icon.
  let html = g.worktrees.filter((wt) => !wt.branch && wt.head === c.h)
    .map((wt) => `<span class="ref worktree">${worktreeIcon(wt)}detached</span>`).join("");
  const refs = c.r;
  if (!refs) return html;
  const remotes = g.remotes;
  let current = null;
  let detached = false;
  const locals = [];
  const remote = [];
  const tags = [];
  for (const r of refs) {
    if (r.startsWith("HEAD -> ")) {
      current = r.slice(8);
      locals.push(current);
    } else if (r === "HEAD") {
      detached = true;
    } else if (r.startsWith("tag: ")) {
      tags.push(r.slice(5));
    } else if (remotes.some((m) => r.startsWith(m + "/"))) {
      if (!r.endsWith("/HEAD")) remote.push(r);
    } else {
      locals.push(r);
    }
  }

  // A remote branch at the same commit as its local branch is folded into
  // the local badge as a sync mark.
  const synced = new Map();
  const remoteOnly = remote.filter((r) => {
    const name = r.slice(r.indexOf("/") + 1);
    if (!locals.includes(name)) return true;
    synced.set(name, [...(synced.get(name) || []), r]);
    return false;
  });

  if (detached) html += `<span class="ref head">HEAD</span>`;
  for (const l of locals) {
    const cls = l === current ? "ref current" : "ref local";
    const sync = synced.get(l);
    const wt = g.worktrees.find((w) => w.branch === l);
    html += `<span class="${cls}"${sync ? ` title="in sync with ${esc(sync.join(", "))}"` : ""}>`
      + (wt ? worktreeIcon(wt) : "") + esc(l)
      + (sync ? `<span class="sync">⇅</span>` : "") + `</span>`;
  }
  for (const r of remoteOnly) html += `<span class="ref remote">${esc(r)}</span>`;
  for (const t of tags) html += `<span class="ref tag">${esc(t)}</span>`;
  return html;
}

function renderGraph(g) {
  if (!g.commits.length) return `<div class="truncated">No commits yet.</div>`;
  const head = g.commits.find((c) => c.r && c.r.some((r) => r === "HEAD" || r.startsWith("HEAD -> ")));
  const lay = layout(g.commits);
  const svg = renderSvg(g.commits, lay, head && head.h);
  const now = Date.now() / 1000;
  let rows = "";
  g.commits.forEach((c, r) => {
    const indent = LEFT * 2 + (lay.rows[r].used - 1) * LANE;
    rows += `<div class="row${c.p.length > 1 ? " merge" : ""}" style="padding-left:${indent}px">`
      + badges(c, g)
      + `<span class="subject" title="${esc(c.s)}">${esc(c.s)}</span>`
      + `<span class="meta" title="${esc(new Date(c.t * 1000).toLocaleString())}">${esc(c.a)} · ${age(now - c.t)}</span>`
      + `<button type="button" class="copy" data-hash="${c.h}" title="Copy full hash">${c.h.slice(0, 7)}</button></div>`;
  });
  const more = lay.truncated ? `<div class="truncated">Older history not shown.</div>` : "";
  return `<div class="graph">${svg}${rows}</div>${more}`;
}

function age(s) {
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  if (s < 86400 * 14) return `${Math.floor(s / 86400)}d`;
  if (s < 86400 * 60) return `${Math.floor(s / 604800)}w`;
  if (s < 86400 * 365) return `${Math.floor(s / 2592000)}mo`;
  return `${(s / 31536000).toFixed(1)}y`;
}

function esc(s) {
  return String(s).replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch]);
}

const repoName = (path) => (path === "." ? rootName : path.slice(path.lastIndexOf("/") + 1));
const byName = (a, b) => repoName(a).localeCompare(repoName(b)) || a.localeCompare(b);

function enabledRepos() {
  return repos.filter((r) => r.enabled).map((r) => r.path).sort(byName);
}

function layoutGrid() {
  const shown = enabledRepos();
  for (const [path, p] of panels) {
    if (!shown.includes(path)) {
      p.el.remove();
      panels.delete(path);
    }
  }
  grid.querySelector(".empty")?.remove();
  if (!shown.length) {
    grid.style.gridTemplate = "1fr / 1fr";
    grid.innerHTML = `<div class="empty">No repos selected. Pick some on the left.</div>`;
    return;
  }
  const cols = Math.ceil(Math.sqrt(shown.length));
  const rows = Math.ceil(shown.length / cols);
  grid.style.gridTemplate = `repeat(${rows}, minmax(0, 1fr)) / repeat(${cols}, minmax(0, 1fr))`;
  for (const path of shown) {
    let p = panels.get(path);
    if (!p) {
      const el = document.createElement("section");
      el.className = "panel";
      el.innerHTML = `<div class="panel-head">${repoLabel(path)}<span class="head"></span><span class="error"></span></div><div class="panel-body"></div>`;
      p = { el, etag: "" };
      panels.set(path, p);
    }
    grid.appendChild(p.el); // keeps panels in name order
  }
}

async function refreshPanel(path) {
  const p = panels.get(path);
  const res = await fetch(`api/graph?repo=${encodeURIComponent(path)}`, {
    cache: "no-store",
    headers: p.etag ? { "If-None-Match": p.etag } : {},
  });
  const errEl = p.el.querySelector(".error");
  if (res.status === 304) return;
  if (!res.ok) {
    errEl.textContent = (await res.text()).trim() || res.statusText;
    return;
  }
  // Redrawing would drop a selection the user is making in this panel;
  // leave the etag alone so the next refresh tries again.
  const sel = getSelection();
  if (sel && !sel.isCollapsed && p.el.contains(sel.anchorNode)) return;
  const g = await res.json();
  errEl.textContent = "";
  const changed = p.etag !== "";
  p.etag = res.headers.get("ETag") || "";
  p.el.querySelector(".head").textContent = g.head ? `on ${g.head}` : "";
  p.el.querySelector(".panel-body").innerHTML = renderGraph(g);
  if (changed) {
    p.el.classList.remove("changed");
    void p.el.offsetWidth;
    p.el.classList.add("changed");
  }
}

async function tick() {
  clearTimeout(timer);
  if (!document.hidden) {
    try {
      await Promise.all(enabledRepos().map(refreshPanel));
      statusEl.className = "status";
      statusEl.textContent = `updated ${new Date().toLocaleTimeString()}`;
    } catch (e) {
      statusEl.className = "status error";
      statusEl.textContent = `offline: ${e.message}`;
    }
  }
  timer = setTimeout(tick, intervalMs);
}

// repoLabel splits a repo path into its parent folder and name:
// "services/api" shows as a small "services" with "api" under it. Repos right
// in the root get topLevel as their parent.
function repoLabel(path, topLevel = "") {
  const i = path.lastIndexOf("/");
  const parent = i < 0 ? topLevel : path.slice(0, i);
  return (parent ? `<span class="parent">${esc(parent)}</span>` : "") + `<span class="name">${esc(repoName(path))}</span>`;
}

grid.addEventListener("click", async (e) => {
  const btn = e.target.closest(".copy");
  if (!btn) return;
  const ok = await copyText(btn.dataset.hash);
  btn.classList.toggle("copied", ok);
  btn.textContent = ok ? "copied" : "failed";
  setTimeout(() => {
    btn.classList.remove("copied");
    btn.textContent = btn.dataset.hash.slice(0, 7);
  }, 1200);
});

// copyText falls back to execCommand because navigator.clipboard only
// exists on https or localhost, and gitview is usually opened over plain
// http on the tailnet.
async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.cssText = "position:fixed;opacity:0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  }
}

// Active repos first, then by name.
function renderPills() {
  pills.innerHTML = repos
    .toSorted((a, b) => b.enabled - a.enabled || byName(a.path, b.path))
    .map((r) => `<button type="button" class="pill" aria-pressed="${r.enabled}" data-path="${esc(r.path)}">${repoLabel(r.path, "/")}</button>`)
    .join("");
}

pills.addEventListener("click", async (e) => {
  const pill = e.target.closest(".pill");
  if (!pill) return;
  const path = pill.dataset.path;
  const enabled = pill.getAttribute("aria-pressed") !== "true";
  pill.setAttribute("aria-pressed", String(enabled));
  const res = await fetch("api/repos/toggle", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, enabled }),
  });
  if (!res.ok) {
    pill.setAttribute("aria-pressed", String(!enabled));
    statusEl.className = "status error";
    statusEl.textContent = `could not save: ${(await res.text()).trim()}`;
    return;
  }
  repos.find((r) => r.path === path).enabled = enabled;
  renderPills();
  layoutGrid();
  tick();
});

document.addEventListener("visibilitychange", () => {
  if (!document.hidden) tick();
});

async function init() {
  const res = await fetch("api/repos", { cache: "no-store" });
  const data = await res.json();
  repos = data.repos;
  intervalMs = data.intervalMs;
  rootName = data.name;
  document.title = `gitview · ${data.name}`;
  $("#root").textContent = data.root;
  renderPills();
  layoutGrid();
  tick();
}

init();
