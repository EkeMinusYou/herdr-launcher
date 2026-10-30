import { View, div } from "gpui-kit";
import { h_flex, v_flex, Input, InputState, v_virtual_list, VirtualListScrollHandle, Scrollbar } from "gpui-base";
import { appHome, closeOverlay, exec, focused, hc, nextCommand, panes, props, readTextFile, run, spaces, theme } from "herdr";
import { MODES, STATUSES, collectItems, commandTarget, filterItems, parseConfig, recordPick } from "./model.js";
import { screenView } from "./preview.js";
import { PreviewLoader } from "./preview-loader.js";

const HISTORY_KEY = "launcher.history.v1";
const STATUS_KEYS = { b: "blocked", w: "working", i: "idle", d: "done", c: "commands", a: "all" };
const SPIN = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
const plainText = text => text.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "").slice(-40000);

export default class Launcher extends View {
  init(_props, cx) {
    this.context = focused();
    this.mode = MODES.includes(props()?.mode) ? props().mode : "launcher";
    this.filter = "all";
    this.query = "";
    this.searching = false;
    this.selected = null;
    this.config = { commands: [] };
    this.error = "";
    this.output = {};
    this.running = null;
    this.preview = "";
    this.previewRows = null;
    this.previewSignature = "";
    this.python = null;
    this.colorRetryAt = 0;
    this.previewKey = null;
    this.previews = new PreviewLoader({
      readText: (key, cx, done) => cx.spawn(async (cx) => {
        let text;
        try {
          const result = await hc(["read", key, "--lines", "0", "--json"]);
          text = result.code === 0 ? plainText(JSON.parse(result.stdout).text) : `Cannot read terminal: ${result.stderr}`;
        } catch (error) { text = `Cannot read terminal: ${error.message ?? error}`; }
        done({ text, rows: null }, cx);
      }),
      readColor: (key, cx, done) => cx.spawn(async (cx) => {
        const rows = await this.readColor(key);
        done(rows === null ? null : { text: "", rows }, cx);
      }),
      changed: (key, snapshot, cx) => {
        const signature = JSON.stringify([key, snapshot]);
        if (signature === this.previewSignature) return;
        this.previewKey = key;
        this.previewSignature = signature;
        this.preview = snapshot?.text ?? "";
        this.previewRows = snapshot?.rows ?? null;
        cx.notify();
        if (key && !snapshot) cx.timer.after(120, (cx) => {
          if (this.previewKey === key && !this.previews.cache.has(key)) {
            this.preview = "Loading terminal…";
            cx.notify();
          }
        });
      },
    });
    this.handle = cx.focus_handle();
    this.scroll = VirtualListScrollHandle.new();
    this.input = InputState.new({ placeholder: "Search names, branches and folders…" });
    this.input.on("change", (_e, cx) => {
      this.query = this.input.value();
      this.resetSelection(cx);
    });
    this.input.on("submit", (_e, cx) => this.activate(cx));
    this.handle.focus();
    try {
      const stored = JSON.parse(localStorage.getItem(HISTORY_KEY) ?? "{}");
      this.history = stored && typeof stored === "object" && !Array.isArray(stored) ? stored : {};
    } catch { this.history = {}; }
    cx.spawn(async (cx) => {
      try {
        this.config = parseConfig(await readTextFile(`${appHome()}/mods/launcher/config.json`));
      } catch (error) { this.error = `Configuration: ${error.message ?? error}`; }
      this.refreshPreview(cx);
      cx.notify();
    });
    cx.spawn(async (cx) => {
      for (;;) {
        const command = await nextCommand();
        this.command(command, cx);
      }
    });
    // Herdr's model is live. Only terminal screen text needs polling.
    cx.timer.every(500, cx => this.refreshPreview(cx));
    this.resetSelection(cx);
  }

  dismiss() { closeOverlay(props().overlay); }

  rows() {
    const rows = filterItems(collectItems(spaces(), panes(), this.config, this.mode, this.context),
      this.query, this.filter, this.history, this.mode === "launcher");
    if (!rows.some(r => r.id === this.selected)) {
      this.selected = (this.selected === null && this.mode !== "launcher" ? rows.find(r => r.current) : null)?.id ?? rows[0]?.id ?? null;
    }
    return rows;
  }

  select(id, cx) {
    this.selected = id;
    this.refreshPreview(cx);
    cx.notify();
  }

  resetSelection(cx) {
    this.selected = null;
    const rows = this.rows();
    this.scroll.scroll_to_item(Math.max(0, rows.findIndex(r => r.id === this.selected)), "center");
    this.refreshPreview(cx);
    cx.notify();
  }

  setMode(mode, cx) {
    this.mode = mode;
    this.filter = "all";
    this.resetSelection(cx);
  }

  move(delta, cx) {
    const rows = this.rows();
    if (!rows.length) return;
    const index = (rows.findIndex(r => r.id === this.selected) + delta + rows.length) % rows.length;
    this.select(rows[index].id, cx);
    this.scroll.scroll_to_item(index, "center");
  }

  jumpSpace(delta, cx) {
    const rows = this.rows();
    const groups = [...new Set(rows.map(r => r.space).filter(Boolean))];
    if (!groups.length) return;
    const here = rows.find(r => r.id === this.selected)?.space;
    const index = groups.indexOf(here);
    const target = groups[(index + delta + groups.length) % groups.length];
    const at = rows.findIndex(r => r.space === target);
    this.select(rows[at].id, cx);
    this.scroll.scroll_to_item(at, "center");
  }

  command(command, cx) {
    if (command === "next") this.move(1, cx);
    else if (command === "prev") this.move(-1, cx);
    else if (command === "accept") this.activate(cx);
    else if (command === "escape") {
      if (!this.searching) return this.dismiss();
      this.searching = false;
      this.handle.focus();
    } else if (command === "cycle") this.setMode(MODES[(MODES.indexOf(this.mode) + 1) % MODES.length], cx);
    else if (MODES.includes(command)) this.setMode(command, cx);
    cx.notify();
  }

  key(event, cx) {
    if (this.searching) return;
    const m = event.modifiers;
    if (m.control || m.alt || m.platform) return;
    const k = event.key_char ?? event.key;
    if (k === "/") { this.searching = true; this.input.focus(); }
    else if (k === "q" || event.key === "escape") return this.dismiss();
    else if (k === "j" || event.key === "down") this.move(1, cx);
    else if (k === "k" || event.key === "up") this.move(-1, cx);
    else if (event.key === "left") this.jumpSpace(-1, cx);
    else if (event.key === "right") this.jumpSpace(1, cx);
    else if (event.key === "enter") this.activate(cx);
    else if (STATUS_KEYS[k]) { this.filter = STATUS_KEYS[k]; this.resetSelection(cx); }
    else return;
    cx.stop_propagation();
    cx.notify();
  }

  refreshPreview(cx) {
    const rows = this.rows();
    const at = rows.findIndex(r => r.id === this.selected);
    const item = rows[at];
    const key = item?.terminal ?? null;
    const warmKeys = [rows[at + 1]?.terminal, rows[at - 1]?.terminal,
      rows.find(row => row.current)?.terminal].filter(terminal => terminal !== key);
    this.previews.request(key, warmKeys, cx);
  }

  async readColor(key) {
    if (this.python === false || Date.now() < this.colorRetryAt) return null;
    let available = false;
    for (const python of this.python ? [this.python] : ["python3", "python"]) {
      try {
        const result = await exec(python, [`${appHome()}/mods/launcher/screen.py`, key]);
        available = true;
        if (result.code === 0) {
          const screen = JSON.parse(result.stdout);
          if (!Array.isArray(screen.rows)) throw new Error("Invalid screen response");
          this.python = python;
          return screen.rows;
        }
      } catch { /* Fast text reads remain available without the color helper. */ }
    }
    if (!available) this.python = false;
    this.colorRetryAt = Date.now() + 5000;
    return null;
  }

  remember(id) {
    this.history = recordPick(this.history, id);
    try { localStorage.setItem(HISTORY_KEY, JSON.stringify(this.history)); }
    catch (error) { this.error = `Cannot save history: ${error.message ?? error}`; }
  }

  activate(cx) {
    const item = this.rows().find(r => r.id === this.selected);
    if (!item || this.running) return;
    this.remember(item.id);
    if (item.kind !== "command") {
      this.dismiss();
      run(item.kind === "space" ? "space.select" : "pane.focus", item.kind === "space" ? { id: item.space } : { pane: item.pane });
      return;
    }
    const c = item.command;
    if (c.action) { this.dismiss(); run(c.action, c.args ?? {}); return; }
    const target = commandTarget(this.context, spaces());
    if (!target.space) { this.error = "The space this launcher opened from is no longer available."; cx.notify(); return; }
    if (c.interactive) {
      this.dismiss();
      run("tab.new", { space: target.space, folder: target.cwd, argv: c.argv, name: c.name });
      return;
    }
    this.running = item.id;
    this.output[item.id] = "Running…";
    cx.notify();
    cx.spawn(async (cx) => {
      try {
        const result = await exec(c.argv[0], c.argv.slice(1), { cwd: target.cwd, on: target.on });
        this.output[item.id] = plainText(`${result.stdout}${result.stderr ? `\n${result.stderr}` : ""}\nExit ${result.code}`);
        this.running = null;
        if (result.code === 0 && c.close) { this.dismiss(); return; }
      } catch (error) {
        this.output[item.id] = `Cannot run command: ${error.message ?? error}`;
        this.running = null;
      }
      cx.notify();
    });
  }

  button(t, id, label, active, callback) {
    return div().id(id).px(10).py(5).rounded(6).cursor_pointer()
      .bg(active ? t.selected : t.surface).text_color(active ? t.foreground : t.muted)
      .hover(s => s.bg(t.hover)).on_click(callback).child(label);
  }

  row(item, t) {
    const selected = item.id === this.selected;
    const status = item.status;
    return h_flex().h(56).px(10).gap(10).items_center().rounded(6)
      .bg(selected ? t.selected : t.popover).text_color(t.foreground)
      .child(div().w(14).flex_none().text_color(t[status] ?? t.dim)
        .when(status === "working", el => el.frames(SPIN, 90))
        .child(status === "working" ? SPIN[0] : status === "idle" ? "○" : status ? "●" : item.kind === "command" ? ">" : "·"))
      .child(v_flex().flex_1().min_w(0).gap(3)
        .child(div().truncate().font_semibold().child(`${item.title}${item.current ? "  ‹ current" : ""}`))
        .child(div().truncate().text_size(11).text_color(t.muted).child(item.subtitle)))
      .child(div().text_size(10).flex_none().text_color(t[status] ?? t.dim).child(status ?? item.kind));
  }

  render() {
    const t = theme();
    const rows = this.rows();
    const item = rows.find(r => r.id === this.selected);
    const body = item?.kind === "command" ? this.output[item.id] ?? item.subtitle : item?.terminal ? this.preview : "No terminal preview for this item.";
    return v_flex().id("launcher").track_focus(this.handle).on_key_down((e, cx) => this.key(e, cx))
      .size_full().min_h(0).p(16).gap(12).bg(t.popover).text_color(t.foreground).text_size(13)
      .child(h_flex().gap(8).items_center()
        .child(div().font_semibold().text_size(16).mr(12).child("Launcher"))
        .children(MODES.map(mode => this.button(t, `mode-${mode}`, mode === "launcher" ? "Commands + panes" : mode === "panes" ? "Panes" : "Spaces", this.mode === mode, (_e, cx) => this.setMode(mode, cx))))
        .child(div().flex_1())
        .child(this.button(t, "dismiss", "×", false, () => this.dismiss())))
      .child(h_flex().id("search-box").h(36).flex_none().px(10).items_center().rounded(7).border(1)
        .border_color(this.searching ? t.accent : t.border).bg(t.background)
        .on_click((_e, cx) => { this.searching = true; this.input.focus(); cx.notify(); })
        .child(Input.new(this.input).flex_1().h_full().text_color(t.foreground))
        .child(div().text_size(11).text_color(t.dim).child(this.searching ? "search" : "/ search")))
      .child(h_flex().gap(6).items_center()
        .children(["all", ...STATUSES, ...(this.mode === "launcher" ? ["commands"] : [])].map(filter =>
          this.button(t, `filter-${filter}`, filter, this.filter === filter, (_e, cx) => { this.filter = filter; this.resetSelection(cx); })))
        .child(div().flex_1())
        .child(div().text_size(11).text_color(t.dim).child(`${rows.length} items`)))
      .when(Boolean(this.error), el => el.child(div().text_size(12).text_color(t.blocked).child(this.error)))
      .child(h_flex().flex_1().min_h(0).items_stretch().gap(12)
        .child(v_flex().flex_1().min_w(0).min_h(0).relative()
          .when(rows.length > 0, el => el
            .child(v_virtual_list("launcher-list", rows.length, 56, index => rows[index].id,
              range => rows.slice(range.start, range.end).map(row => this.row(row, t)))
              .size_full().track_scroll(this.scroll)
              .on_item_click((id, cx) => { if (this.selected === id) this.activate(cx); else this.select(id, cx); }))
            .child(Scrollbar.vertical("launcher-list").absolute().inset_0()))
          .when(rows.length === 0, el => el.child(div().p(16).text_color(t.muted).child("No matches. Try another search or filter."))))
        .child(v_flex().flex_1().min_w(0).min_h(0).gap(8).p(12).rounded(8).bg(t.background).border(1).border_color(t.border)
          .child(div().font_semibold().truncate().child(item?.title ?? "Preview"))
          .child(div().text_size(11).text_color(t.muted).truncate().child(item?.cwd ?? (item?.kind === "command" ? this.context?.cwd ?? "" : "")))
          .child(v_flex().id("preview-text").flex_1().min_h(0).overflow_y_scrollbar().overflow_x_scrollbar().text_size(11).font_family("JetBrains Mono")
            .child(item?.terminal && this.previewRows !== null ? screenView(this.previewRows, t) : div().child(body ?? "")))))
      .child(div().text_size(11).text_color(t.muted).child(this.running ? "Running command… · Esc closes the launcher" :
        this.searching ? "↑ ↓ select · Enter run / go · Esc select mode · Tab change source" :
          "j k select · / search · b w i d filter · a all · ← → space · Tab source · Enter run / go · q close"));
  }
}
