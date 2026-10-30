// Pure data and search helpers shared by the native mod and its tests.
export const MODES = ["launcher", "panes", "spaces"];
export const STATUSES = ["blocked", "working", "idle", "done"];

export function parseConfig(text) {
  const cfg = text === null ? { commands: [] } : JSON.parse(text);
  if (!cfg || typeof cfg !== "object" || Array.isArray(cfg) ||
      Object.keys(cfg).some(k => k !== "commands") || !Array.isArray(cfg.commands)) {
    throw new Error("config.json must contain only a commands array");
  }
  const names = new Set();
  for (const c of cfg.commands) {
    const allowed = ["name", "description", "argv", "action", "args", "interactive", "close"];
    if (!c || typeof c !== "object" || Object.keys(c).some(k => !allowed.includes(k)) ||
        typeof c.name !== "string" || !c.name.trim() || names.has(c.name)) {
      throw new Error("Commands need unique, nonempty names and supported fields");
    }
    names.add(c.name);
    if ((c.argv !== undefined) === (c.action !== undefined)) {
      throw new Error(`${c.name}: set exactly one of argv or action`);
    }
    if (c.argv !== undefined && (!Array.isArray(c.argv) || !c.argv.length ||
        c.argv.some(a => typeof a !== "string") || !c.argv[0])) {
      throw new Error(`${c.name}: argv must be a nonempty array of strings`);
    }
    if (c.action !== undefined && (typeof c.action !== "string" || !c.action.trim())) {
      throw new Error(`${c.name}: action must be an app command name`);
    }
    if (c.args !== undefined && (!c.args || typeof c.args !== "object" || Array.isArray(c.args))) {
      throw new Error(`${c.name}: args must be an object`);
    }
    for (const key of ["interactive", "close"]) {
      if (c[key] !== undefined && typeof c[key] !== "boolean") throw new Error(`${c.name}: ${key} must be boolean`);
    }
    if (c.description !== undefined && typeof c.description !== "string") throw new Error(`${c.name}: description must be a string`);
    if (c.action !== undefined && (c.interactive !== undefined || c.close !== undefined) || c.argv !== undefined && c.args !== undefined) {
      throw new Error(`${c.name}: args belongs to actions; interactive and close belong to argv commands`);
    }
  }
  return cfg;
}

export function collectItems(spaceList, paneList, config, mode, context) {
  const out = [];
  if (mode === "launcher") {
    for (const c of config.commands) out.push({
      id: `command:${c.name}`, kind: "command", title: c.name,
      subtitle: c.description ?? (c.argv ? c.argv.join(" ") : c.action),
      extra: "", command: c, status: null,
    });
  }
  for (const s of spaceList) {
    const active = s.tabs.find(t => t.active) ?? s.tabs[0];
    const inTab = paneList.filter(p => p.tab === active?.id);
    const preview = inTab.find(p => p.pane === context?.pane) ?? inTab.find(p => p.focused) ?? inTab[0];
    if (mode === "spaces") {
      out.push({
        id: `space:${s.id}`, kind: "space", space: s.id, title: s.name,
        subtitle: [s.branch, s.machine, preview?.program?.name, preview?.title].filter(Boolean).join(" · "),
        extra: [s.folder, ...inTab.map(p => p.cwd)].filter(Boolean).join(" "),
        status: s.status, current: s.id === context?.space,
        terminal: preview?.terminal, cwd: preview?.cwd ?? s.folder,
      });
      continue;
    }
    for (const t of s.tabs) {
      for (const p of paneList.filter(p => p.tab === t.id)) {
        out.push({
          id: `pane:${p.pane}`, kind: "pane", pane: p.pane, space: s.id,
          title: p.title || p.program?.name || p.mod || p.pane,
          subtitle: [s.name, t.name, p.program?.name ?? p.mod, p.branch, s.machine].filter(Boolean).join(" · "),
          extra: p.cwd ?? "", status: p.status,
          current: p.pane === context?.pane, terminal: p.terminal, cwd: p.cwd,
        });
      }
    }
  }
  return out;
}

// A small smart-case subsequence matcher, not the TUI's fzf engine.
// Each space-separated term is required. ! excludes, ' is exact, ^/$ anchor.
export function matchScore(text, query) {
  let total = 0;
  for (let term of query.trim().split(/\s+/).filter(Boolean)) {
    const exclude = term.startsWith("!");
    if (exclude) term = term.slice(1);
    let hay = text;
    if (term === term.toLowerCase()) hay = hay.toLowerCase();
    let score = -1;
    if (term.startsWith("'")) score = hay.includes(term.slice(1)) ? 100 : -1;
    else if (term.startsWith("^")) score = hay.startsWith(term.slice(1)) ? 100 : -1;
    else if (term.endsWith("$")) score = hay.endsWith(term.slice(0, -1)) ? 100 : -1;
    else {
      let at = -1, first = -1, gaps = 0;
      for (const char of term) {
        const next = hay.indexOf(char, at + 1);
        if (next < 0) { first = -1; break; }
        if (first < 0) first = next;
        else gaps += next - at - 1;
        at = next;
      }
      if (first >= 0) score = Math.max(1, 100 - gaps - first * 0.1);
    }
    if (exclude ? score >= 0 : score < 0) return null;
    if (!exclude) total += score;
  }
  return total;
}

export function historyScore(record, now) {
  if (!record || !Number.isFinite(record.rank) || !Number.isFinite(record.last)) return 0;
  const age = Math.max(0, now - record.last);
  return record.rank * (age < 3600000 ? 4 : age < 86400000 ? 2 : age < 604800000 ? 0.5 : 0.25);
}

export function filterItems(items, query, filter, history = {}, frecency = false, now = Date.now()) {
  return items.flatMap((item, index) => {
    if (filter === "commands" && item.kind !== "command" || STATUSES.includes(filter) && item.status !== filter) return [];
    const score = matchScore([item.title, item.subtitle, item.extra].filter(Boolean).join(" "), query);
    return score === null ? [] : [{ item, index, score }];
  }).sort((a, b) => (query.trim() ? b.score - a.score : frecency ? historyScore(history[b.item.id], now) - historyScore(history[a.item.id], now) : 0) || a.index - b.index)
    .map(row => row.item);
}

export function recordPick(history, id, now = Date.now()) {
  const next = Object.fromEntries(Object.entries(history).filter(([, r]) =>
    r && Number.isFinite(r.rank) && r.rank > 0 && Number.isFinite(r.last) && now - r.last < 90 * 86400000)
    .map(([key, r]) => [key, { ...r }]));
  next[id] = { rank: (next[id]?.rank ?? 0) + 1, last: now };
  const total = Object.values(next).reduce((sum, r) => sum + r.rank, 0);
  if (total > 10000) for (const r of Object.values(next)) r.rank *= 9000 / total;
  return next;
}

export function commandTarget(context, spaceList) {
  const s = spaceList.find(s => s.id === context?.space);
  return { space: s?.id, cwd: context?.cwd || s?.folder || undefined, on: s?.machine ?? null };
}
