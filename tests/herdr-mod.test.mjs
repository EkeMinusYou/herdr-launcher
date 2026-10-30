import assert from "node:assert/strict";
import { test } from "node:test";
import { collectItems, commandTarget, filterItems, historyScore, matchScore, parseConfig, recordPick } from "../mods/launcher/model.js";
import { PreviewLoader } from "../mods/launcher/preview-loader.js";

const spaces = [
  { id: "local", name: "Launcher", folder: "/repo", active: true, machine: null, status: "blocked", tabs: [
    { id: "a", name: "Code", active: true }, { id: "b", name: "Review", active: false },
  ] },
  { id: "vps/work", name: "Deploy", folder: "/srv/app", machine: "vps", branch: "main", status: "working", tabs: [
    { id: "vps/c", name: "Release", active: true },
  ] },
];
const panes = [
  { pane: "p1", tab: "a", terminal: "t1", title: "Fix navigation", cwd: "/repo", status: "blocked", program: { name: "Codex" } },
  { pane: "p2", tab: "a", terminal: "t2", title: "Shell", cwd: "/repo", focused: true, program: { name: "zsh" } },
  { pane: "p3", tab: "b", terminal: null, mod: "files", title: "Files" },
  { pane: "vps/p4", tab: "vps/c", terminal: "vps/t4", title: "Deploy service", cwd: "/srv/app", status: "working", program: { name: "Claude" } },
];
const config = { commands: [{ name: "git status", argv: ["git", "status", "--short"] }] };
const context = { space: "local", pane: "p1", cwd: "/repo" };

function previewHarness(capacity = 32) {
  const text = [], color = [], updates = [];
  const loader = new PreviewLoader({
    capacity,
    readText: (key, cx, done) => text.push({ key, done: value => done(value, cx) }),
    readColor: (key, cx, done) => color.push({ key, done: value => done(value, cx) }),
    changed: (key, value) => updates.push({ key, value }),
  });
  return { loader, text, color, updates };
}

const colored = text => ({ text: "", rows: [[{ text, style: { fg: 6 } }]] });

test("a first screen appears from the fast text read before color finishes", () => {
  const h = previewHarness();
  h.loader.request("t1", [], {});
  assert.equal(h.text[0].key, "t1");
  assert.equal(h.color[0].key, "t1");
  h.text[0].done({ text: "prompt", rows: null });
  assert.equal(h.updates.at(-1).value.text, "prompt");
  h.color[0].done(colored("prompt"));
  assert.deepEqual(h.updates.at(-1).value.rows, colored("prompt").rows);
});

test("selection changes catch up immediately without a periodic refresh or stale result", () => {
  const h = previewHarness();
  h.loader.request("t1", [], {});
  h.loader.request("t2", [], {});
  h.loader.request("t3", [], {});
  h.text[0].done({ text: "old", rows: null });
  assert.deepEqual(h.text.map(r => r.key), ["t1", "t3"]);
  h.text[1].done({ text: "latest", rows: null });
  h.color[0].done(colored("old"));
  assert.deepEqual(h.color.map(r => r.key), ["t1", "t3"]);
  assert.equal(h.updates.at(-1).key, "t3");
  assert.equal(h.updates.at(-1).value.text, "latest");
  h.color[1].done(colored("latest"));
  assert.equal(h.updates.at(-1).value.rows[0][0].text, "latest");
});

test("revisiting a cached terminal displays its color synchronously while revalidating", () => {
  const h = previewHarness();
  h.loader.request("t1", [], {});
  h.text[0].done({ text: "one", rows: null });
  h.color[0].done(colored("one"));
  h.loader.request("t2", [], {});
  h.text[1].done({ text: "two", rows: null });
  h.color[1].done(colored("two"));
  h.loader.request("t1", [], {});
  assert.deepEqual(h.updates.at(-1), { key: "t1", value: colored("one") });
  assert.equal(h.text.length, 2);
  assert.equal(h.color.at(-1).key, "t1");
});

test("neighbor prefetch stays bounded and foreground reads take priority", () => {
  const h = previewHarness();
  h.loader.request("t1", ["t2", "t3", "t4"], {});
  h.color[0].done(colored("one"));
  assert.equal(h.color[1].key, "t2");
  h.loader.request("t4", ["t3"], {});
  h.color[1].done(colored("two"));
  assert.equal(h.color[2].key, "t4");
  h.color[2].done(colored("four"));
  assert.equal(h.color[3].key, "t3");
  h.color[3].done(colored("three"));
  assert.equal(h.color.length, 4);
});

test("late plain text does not erase color and a failed helper keeps text usable", () => {
  const h = previewHarness();
  h.loader.request("t1", [], {});
  h.color[0].done(colored("colored"));
  h.text[0].done({ text: "plain", rows: null });
  assert.deepEqual(h.updates.at(-1).value.rows, colored("colored").rows);
  h.loader.request("t2", [], {});
  h.text[1].done({ text: "fallback", rows: null });
  h.color[1].done(null);
  assert.equal(h.updates.at(-1).value.text, "fallback");
});

test("preview snapshots stay in a bounded memory cache", () => {
  const h = previewHarness(2);
  for (const key of ["t1", "t2", "t3"]) {
    h.loader.request(key, [], {});
    h.text.at(-1).done({ text: key, rows: null });
    h.color.at(-1).done(colored(key));
  }
  assert.equal(h.loader.cache.size, 2);
  assert.equal(h.loader.cache.has("t1"), false);
  h.loader.request(null, [], {});
  assert.deepEqual(h.updates.at(-1), { key: null, value: undefined });
});

test("failed revalidation replaces stale cached color with fresh text or a readable error", () => {
  const h = previewHarness();
  h.loader.request("t1", [], {});
  h.text[0].done({ text: "before", rows: null });
  h.color[0].done(colored("before"));
  h.loader.request("t1", [], {});
  h.color[1].done(null);
  assert.equal(h.text[1].key, "t1");
  h.text[1].done({ text: "Cannot read terminal: disconnected", rows: null });
  assert.equal(h.updates.at(-1).value.rows, null);
  assert.match(h.updates.at(-1).value.text, /disconnected/);
});

test("pane collection preserves Herdr order, includes mod panes and captures original focus", () => {
  const rows = collectItems(spaces, panes, config, "panes", context);
  assert.deepEqual(rows.map(r => r.id), ["pane:p1", "pane:p2", "pane:p3", "pane:vps/p4"]);
  assert.equal(rows[0].current, true);
  assert.equal(rows[1].current, false);
  assert.equal(rows[2].terminal, null);
  assert.match(rows[3].subtitle, /vps/);
});

test("spaces have one row each and preview the original pane or an active-tab pane", () => {
  const rows = collectItems(spaces, panes, config, "spaces", context);
  assert.deepEqual(rows.map(r => r.id), ["space:local", "space:vps/work"]);
  assert.equal(rows[0].terminal, "t1");
  assert.equal(rows[0].current, true);
  assert.equal(rows[1].terminal, "vps/t4");
  assert.equal(rows[1].status, "working");
  const empty = collectItems([{ ...spaces[0], tabs: [] }], [], config, "spaces", context);
  assert.equal(empty[0].terminal, undefined);
});

test("launcher combines configured commands and panes without adding default commands", () => {
  assert.equal(collectItems(spaces, panes, config, "launcher", context)[0].id, "command:git status");
  assert.equal(collectItems(spaces, panes, parseConfig(null), "launcher", context).length, panes.length);
});

test("smart-case fuzzy search supports required terms, exact, exclusion and anchors", () => {
  assert.notEqual(matchScore("Launcher Code Fix navigation /repo", "lch fx"), null);
  assert.notEqual(matchScore("Launcher Code Fix navigation /repo", "'Code !deploy ^Launcher /repo$"), null);
  assert.equal(matchScore("launcher", "Launcher"), null);
  assert.equal(matchScore("Deploy service", "!service"), null);
  assert.notEqual(matchScore(`a${"x".repeat(200)}b`, "ab"), null);
  assert.equal(matchScore("shell", "xyz"), null);
});

test("status and command filters apply before ranking; ties keep model order", () => {
  const rows = collectItems(spaces, panes, config, "launcher", context);
  assert.deepEqual(filterItems(rows, "", "blocked").map(r => r.id), ["pane:p1"]);
  assert.deepEqual(filterItems(rows, "", "commands").map(r => r.id), ["command:git status"]);
  assert.deepEqual(filterItems(rows, "vps", "working").map(r => r.id), ["pane:vps/p4"]);
  assert.deepEqual(filterItems(rows, "", "all"), rows);
});

test("frecency only ranks an empty query when enabled", () => {
  const now = 1_000_000_000;
  const rows = collectItems(spaces, panes, config, "launcher", context);
  const history = { "pane:p2": { rank: 20, last: now } };
  assert.equal(filterItems(rows, "", "all", history, true, now)[0].id, "pane:p2");
  assert.equal(filterItems(rows, "", "all", history, false, now)[0].id, "command:git status");
  assert.equal(historyScore({ rank: 2, last: now - 2 * 86400000 }, now), 1);
  assert.equal(historyScore({ rank: "broken", last: now }, now), 0);
});

test("history prunes old or malformed entries and caps aggregate rank", () => {
  const now = 100 * 86400000;
  const history = { old: { rank: 1, last: 0 }, bad: null, huge: { rank: 10000, last: now } };
  const next = recordPick(history, "pane:p1", now);
  assert.equal(next.old, undefined);
  assert.equal(next.bad, undefined);
  assert.equal(next["pane:p1"].last, now);
  assert.ok(Object.values(next).reduce((sum, r) => sum + r.rank, 0) <= 9001);
  assert.equal(history.huge.rank, 10000);
});

test("command execution stays on the original space and machine", () => {
  assert.deepEqual(commandTarget(context, spaces), { space: "local", cwd: "/repo", on: null });
  assert.deepEqual(commandTarget({ space: "vps/work", cwd: "/srv/app/subdir" }, spaces), {
    space: "vps/work", cwd: "/srv/app/subdir", on: "vps",
  });
  assert.equal(commandTarget({ space: "closed" }, spaces).space, undefined);
});

test("configuration rejects ambiguous commands, typos, duplicates and invalid types", () => {
  for (const bad of [
    {}, [], { commands: {} }, { commands: [], typo: true },
    { commands: [{ name: "a" }] },
    { commands: [{ name: "a", argv: [] }] },
    { commands: [{ name: "a", argv: [1] }] },
    { commands: [{ name: "a", argv: ["git"], action: "app.help" }] },
    { commands: [{ name: "a", action: "app.help", args: [] }] },
    { commands: [{ name: "a", argv: ["git"], close: "yes" }] },
    { commands: [{ name: "a", argv: ["git"], args: {} }] },
    { commands: [{ name: "a", action: "app.help", interactive: true }] },
    { commands: [{ name: "a", action: "app.help" }, { name: "a", action: "tab.new" }] },
  ]) assert.throws(() => parseConfig(JSON.stringify(bad)), JSON.stringify(bad));
  assert.throws(() => parseConfig("invalid JSON"));
  assert.deepEqual(parseConfig(JSON.stringify(config)), config);
  assert.deepEqual(parseConfig('{"commands":[{"name":"Help","action":"app.help","args":{}}]}').commands[0].args, {});
});
