import { div } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";

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
  return v_flex().gap(0).items_start().children(rows.map(row =>
    h_flex().h(16).flex_none().gap(0).items_start().whitespace_nowrap()
      .when(row.length === 0, el => el.child(" "))
      .children(row.map(run => {
        const s = run.style;
        const foreground = color(s.fg, t, t.foreground);
        const background = color(s.bg, t, t.background);
        return div().flex_none().whitespace_nowrap()
          .text_color(s.reverse ? background : foreground)
          .when(s.bg !== undefined || s.reverse, el => el.bg(s.reverse ? foreground : background))
          .when(s.bold, el => el.font_bold())
          .when(s.dim, el => el.opacity(0.65))
          .when(s.italic, el => el.italic())
          .when(s.underline, el => el.underline())
          .child(run.text);
      }))));
}
