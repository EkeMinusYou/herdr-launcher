import { div } from "gpui-kit";
import { v_flex } from "gpui-base";

const CELL_WIDTH = 6.6; // JetBrains Mono at 11px: 0.6em per terminal column.
const CELL_HEIGHT = 16;

function color(value, t, fallback) {
  if (typeof value === "string") return value;
  if (!Number.isInteger(value)) return fallback;
  if (value < 16) return t[`ansi${value}`] ?? fallback;
  if (value >= 232) {
    const level = (8 + (value - 232) * 10).toString(16).padStart(2, "0");
    return `#${level}${level}${level}`;
  }
  const levels = [0, 95, 135, 175, 215, 255];
  const n = value - 16;
  return "#" + [Math.floor(n / 36), Math.floor(n / 6) % 6, n % 6]
    .map(index => levels[index].toString(16).padStart(2, "0")).join("");
}

export function screenView(rows, t) {
  const columns = rows.reduce((width, row) => row.reduce((width, run) =>
    Math.max(width, run.col + run.width), width), 1);
  return v_flex().w(columns * CELL_WIDTH).flex_none().gap(0).items_start()
    .font_family("JetBrains Mono").text_size(11).line_height(`${CELL_HEIGHT}px`)
    .children(rows.map(row =>
      div().relative().w_full().h(CELL_HEIGHT).flex_none()
        .children(row.map(run => {
          const s = run.style;
          const foreground = color(s.fg, t, t.foreground);
          const background = color(s.bg, t, t.background);
          return div().absolute().left(run.col * CELL_WIDTH).top(0)
            .w(run.width * CELL_WIDTH).h(CELL_HEIGHT).overflow_hidden().whitespace_nowrap()
            .text_color(s.reverse ? background : foreground)
            .when(s.bg !== undefined || s.reverse, el => el.bg(s.reverse ? foreground : background))
            .when(s.bold, el => el.font_bold())
            .when(s.dim, el => el.opacity(0.65))
            .when(s.italic, el => el.italic())
            .when(s.underline, el => el.underline())
            .child(run.text);
        }))));
}
