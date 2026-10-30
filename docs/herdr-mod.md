# Herdr UI mod

The native Launcher mod brings the command and pane picker to [Herdr UI](https://herdr.dev/mods/). It uses the app's live spaces and panes, with no Go binary or TUI plugin required. Python 3 enables colored screen previews; without it, the mod falls back to plain text. It was checked with Herdr UI 0.1.9. Follow the API files installed by your app if a later version changes the mod interface.

![Launcher in Herdr UI](../preview.png)

## Install

Ask your agent:

```text
Read ~/.config/herdr-app/AGENTS.md, then add the Launcher mod from https://github.com/EkeMinusYou/herdr-launcher. Read RECIPE.md and the mod files first, and merge keys.json into my existing keys without replacing bindings.
```

To install manually from this checkout:

1. Read [RECIPE.md](../RECIPE.md), [keys.json](../keys.json) and `mods/launcher/`.
2. Copy `mods/launcher/` to `${HERDR_APP_HOME:-$HOME/.config/herdr-app}/mods/launcher/`. If that name is taken, keep the existing mod; rename the new folder and update the `mods/launcher/config.json` path in `main.js` and all `launcher` references in the key snippet.
3. Merge the root `keys.json` snippet into the app's `keys.json`. Preserve existing bindings and resolve any conflicts explicitly. Do not replace the whole file. No layout files need to change.
4. Run `herdr-app check ~/.config/herdr-app/mods/launcher` (use your actual app home).
5. Press **Ctrl+Shift+K**, or run `herdr-app do mod.open '{"name":"launcher"}'`.

Check system shortcuts too: macOS can intercept a chord such as Ctrl+Shift+Space even when there is no conflicting Herdr binding. If your chosen chord is reserved, bind a free chord or add `"ctrl-s space": "mod.open:launcher"` to the global and terminal sections when you already use Ctrl+S as a prefix. The prefix shortcut means Ctrl+S, release, then Space.

This mod reads its own `config.json` and runs its Python helper with `hc connect` to observe the selected terminal, including remote terminals. The connection cannot send input or resize the terminal and closes after reading the screen. Fast initial text previews and the fallback use `hc read`. Only commands you add run via `exec` or Herdr app commands. The mod itself makes no HTTP requests.

## Use

The popup starts in select mode. Its top buttons switch between **Commands + panes**, **Panes**, and **Spaces**. The combined list starts in frecency order; the other two preserve Herdr's order and initially select the current pane or space. A space opens its existing active tab, without changing that tab's focused pane.

| Key | Action |
| --- | --- |
| `j` / `k`, arrows, Ctrl+N / Ctrl+P | Select a row |
| Left / Right in select mode | First row of the previous / next space |
| `/` or click the input | Search with native text editing and composition |
| `b` / `w` / `i` / `d` in select mode | Filter blocked / working / idle / done |
| `c` / `a` in select mode | Commands only / all items |
| Tab | Cycle the three sources |
| Ctrl+1 / Ctrl+2 / Ctrl+3 | Commands + panes / panes / spaces |
| Enter | Run the selected command or go to the selected item |
| Escape | Leave search, keeping the query; in select mode, close |
| `q` in select mode or × | Close |
| Click a row | Select; clicking the selected row runs it |

Filters also have clickable buttons. Working agents have a spinner, blocked and done agents have colored dots, idle agents have hollow dots, and non-agent panes have a neutral marker. Status colors follow the current theme.

Search is a small smart-case fuzzy matcher, separate from fzf in the TUI. Space-separated terms must all match. It supports `'exact`, `^prefix`, `suffix$`, and `!exclude`; uppercase terms are case-sensitive. Search covers titles, space/tab names, programs, branches, machines and directories. The search score takes precedence over frecency. It does not support fzf's full query grammar or match highlighting.

The preview starts reading immediately on open or selection, then refreshes the current screen every 500 ms while the popup is open. On a first visit, fast screen text appears before the colored read finishes. An in-memory cache of up to 32 terminals shows previously read screens immediately while refreshing them; it is discarded when the popup closes. Up to two nearby screens are prefetched after the selected screen, with at most one colored read and one fast text read in flight. Changing selection prioritizes the latest target as soon as an existing read finishes, without waiting for the next timer tick. “Loading terminal…” appears only if there is still no screen after 120 ms.

With Python 3 (`python3` or `python`), the mod reads the color-bearing first paint over an observing core connection. It preserves the theme's ANSI palette, 256-color and RGB text/background colors, bold, dim, italic, underline and reverse text, without sending input or resizing the terminal. It displays at most 500 columns, 200 rows and 40,000 cells. Terminal images, cursor animation and exact font shaping are not reproduced.

Without the color helper it requests JSON screen text through `hc read`, so CLI diagnostics are not mixed into the preview. Command output remains plain text, capped at the last 40,000 characters. Mod panes have no terminal preview. For another space, the app's API does not expose every tab's remembered focused pane, so its preview uses the first pane of the active tab when no focused pane is available. Selecting that space still preserves its actual focus.

## Commands

Edit the installed `mods/launcher/config.json`, then reopen the popup. The shipped configuration has no commands. For example:

```json
{
  "commands": [
    {
      "name": "Git status",
      "description": "Show changes in the current repository",
      "argv": ["git", "status", "--short"]
    },
    {
      "name": "lazygit",
      "argv": ["lazygit"],
      "interactive": true
    },
    {
      "name": "Open help",
      "action": "app.help"
    },
    {
      "name": "Split right",
      "action": "pane.split",
      "args": { "direction": "right" }
    }
  ]
}
```

Each command has a unique `name`, an optional `description`, and exactly one of:

- `argv`: executable and arguments, without shell expansion. Noninteractive output and the exit code appear in the preview. `close: true` closes on success and leaves failures visible. `interactive: true` opens a new terminal tab in the original space. Commands run in the directory and on the machine the launcher was opened from. For shell syntax, explicitly use an installed shell, such as `"argv": ["sh", "-lc", "git status && git diff --stat"]`.
- `action` with optional `args`: a Herdr app command, run after closing the popup. App commands use the app's current focus. Run `herdr-app commands` for supported names and arguments.

Configuration errors appear above the list; panes and spaces remain usable. Only one noninteractive command runs at a time. Herdr's `exec` API has a 30-second limit; **closing the launcher does not cancel an already started command**. Use an interactive command for longer tasks. Commands may run programs and access the network according to what you configure.

Picks are stored in the mod's own `localStorage`, independently from the TUI history. Frecency uses the same recency weights as the TUI (×4 within an hour, ×2 within a day, ×0.5 within a week, ×0.25 afterwards), pruning unused entries after 90 days and scaling ranks when their sum exceeds 10,000.

## Development and sharing

```sh
node --test tests/herdr-mod.test.mjs
python3 -m unittest discover -s tests -p 'test_*.py'
herdr-app check mods/launcher
go test ./...
```

Copy the mod into a temporary `HERDR_APP_HOME` for UI checks. The app home isolates layouts, keys and mod state; it still connects to the same herdr-core terminals. Avoid changing or closing existing work during checks.

Verify searching (including Japanese composition), mode switches, status filters, scrolling to a selected row, a pane in another tab, space selection, terminal previews, command success/failure, and Escape/typing outside the popup. A remote command's target is covered by model tests; it also needs a connected machine for an end-to-end check. Capture the window with `herdr-app shot preview.png`.

The repository root contains the mod catalog's `RECIPE.md`, `preview.png`, `keys.json` and `mods/launcher/`, with no `layouts/` folder. To list a published version in the community catalog, push it to the public repository and add the GitHub topic **herdr-mod**. The existing Go release tags and binaries continue to serve the TUI plugin.
