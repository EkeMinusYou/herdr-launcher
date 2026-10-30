import importlib.util
import io
import json
import pathlib
import struct
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("launcher_screen", pathlib.Path(__file__).parents[1] / "mods/launcher/screen.py")
screen = importlib.util.module_from_spec(spec)
spec.loader.exec_module(screen)


def text(rows):
    return "\n".join("".join(run["text"] for run in row) for row in rows)


class ScreenTests(unittest.TestCase):
    def test_framed_capture_only_observes_and_reassembles_utf8(self):
        def frame(channel, kind, payload):
            return struct.pack("<IBI", channel, kind, len(payload)) + payload

        class Input(io.BytesIO):
            def close(self):
                self.written = self.getvalue()
                super().close()

        class Child:
            def __init__(self):
                paint = "\x1b[H\x1b[2J\x1b[31m日本\x1b[0m".encode()
                reply = json.dumps({"id": 1, "result": {"channel": 7, "terminal": {"cols": 16, "rows": 4}}}).encode()
                self.stdin = Input()
                self.stdout = io.BytesIO(b'{"id":0,"result":{}}\n' + frame(0, 0, reply) +
                    frame(7, 0, paint[:18]) + frame(7, 0, paint[18:]))
                self.stderr = io.BytesIO()

            def wait(self, timeout=None):
                return 0

            def kill(self):
                raise AssertionError("Capture should finish before its watchdog")

        child = Child()
        with patch.object(screen.subprocess, "Popen", return_value=child) as popen:
            result = screen.capture("vps/t1")
        self.assertEqual(popen.call_args.args[0], ["hc", "connect"])
        self.assertEqual(text(result["rows"]), "日本")
        self.assertEqual(result["rows"][0][0]["style"], {"fg": 1})
        handshake, commands = child.stdin.written.split(b"\n", 1)
        self.assertTrue(json.loads(handshake)["params"]["framed"])
        sent = []
        while commands:
            channel, kind, size = struct.unpack("<IBI", commands[:9])
            sent.append((channel, kind, commands[9:9 + size]))
            commands = commands[9 + size:]
        request = json.loads(sent[0][2])
        self.assertEqual(request["method"], "terminal.attach")
        self.assertEqual(request["params"]["terminal"], "vps/t1")
        self.assertTrue(request["params"]["observe"])
        self.assertEqual(request["params"]["history"], 0)
        self.assertTrue(all(channel == 7 and kind in (1, 2) for channel, kind, _ in sent[1:]))

    def test_ansi_palette_and_style_reset(self):
        rows = screen.decode_screen("\x1b[1;36mcyan\x1b[0m plain")["rows"]
        self.assertEqual(rows[0][0], {"text": "cyan", "style": {"bold": True, "fg": 6}})
        self.assertEqual(rows[0][1], {"text": " plain", "style": {}})

    def test_indexed_and_truecolor_background(self):
        rows = screen.decode_screen("\x1b[38;5;196;48;2;10;20;30mcolor")["rows"]
        self.assertEqual(rows[0][0]["style"], {"fg": 196, "bg": "#0a141e"})

    def test_colon_sgr_and_reverse(self):
        rows = screen.decode_screen("\x1b[38:2::1:2:3;7;4mX")["rows"]
        self.assertEqual(rows[0][0]["style"], {"fg": "#010203", "reverse": True, "underline": True})

    def test_cursor_positions_overwrites_and_clear(self):
        rows = screen.decode_screen("old\x1b[H\x1b[2Jnew\x1b[2;4HX\x1b[1;2H!", 10, 4)["rows"]
        self.assertEqual(text(rows), "n!w\n   X")
        self.assertEqual(text(screen.decode_screen("abcdef\rXY\x1b[K")["rows"]), "XY")

    def test_unicode_wide_and_combining_characters(self):
        rows = screen.decode_screen("日本e\u0301\x1b[7GX", 12, 2)["rows"]
        self.assertEqual(text(rows), "日本e\u0301 X")

    def test_osc_titles_and_mode_controls_are_not_screen_text(self):
        rows = screen.decode_screen("\x1b]2;title\x1b\\\x1b[?2004h\x1b[9G\x1bH\x1b[Hprompt")["rows"]
        self.assertEqual(text(rows), "prompt")

    def test_background_spaces_are_kept_and_empty_bottom_rows_trimmed(self):
        rows = screen.decode_screen("\x1b[41m   \x1b[0m\r\n", 10, 4)["rows"]
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0][0], {"text": "   ", "style": {"bg": 1}})

    def test_screen_bounds_limit_output(self):
        result = screen.decode_screen("x" * 100000, 10000, 10000)
        self.assertEqual(result["cols"], 500)
        self.assertLessEqual(sum(len(run["text"]) for row in result["rows"] for run in row), 40000)


if __name__ == "__main__":
    unittest.main()
