import importlib.util
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "native_terminal", ROOT / "scripts/native-loading/native_terminal.py")
terminal = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(terminal)


class NativeTerminalTests(unittest.TestCase):
    def test_cursor_position_preserves_login_and_footer_on_distinct_rows(self):
        rendered = terminal.render_screen(b"Login\x1b[2;1HLogin required", width=30, height=4)
        self.assertEqual(rendered, "Login\nLogin required")

    def test_vertical_absolute_position_keeps_column_and_clamps(self):
        screen = terminal.Screen(width=12, height=4)
        screen.feed(b"ABC\x1b[3dX\x1b[999dY")
        self.assertEqual(screen.render(), "ABC\n\n   X\n    Y")
        self.assertEqual(terminal.render_screen(b"A\x1b[2dB\x1b[dC", width=8, height=3), "A C\n B")

    def test_redraw_clear_and_overwrite_remove_stale_auth_prompt(self):
        cases = (
            b"Sign in required\x1b[2J\x1b[HWelcome\r\x1b[KREADY",
            b"Sign in required\x1b[2J\x1b[HREADY",
        )
        for raw in cases:
            with self.subTest(raw=raw):
                rendered = terminal.render_screen(raw, width=40, height=5)
                self.assertEqual(rendered, "READY")
                self.assertNotIn("Sign in", rendered)

    def test_osc_dcs_apc_and_pm_payloads_are_never_rendered(self):
        raw = (b"before\x1b]0;secret-window-title\x07"
               b"\x1b]52;c;clipboard-secret\x1b\\after"
               b"\x1bPprivate-dcs\x1b\\\x1b_private-apc\x1b\\"
               b"\x1b^private-pm\x1b\\")
        rendered = terminal.render_screen(raw)
        self.assertEqual(rendered, "beforeafter")
        for private in ("secret-window-title", "clipboard-secret", "private-dcs", "private-apc", "private-pm"):
            self.assertNotIn(private, rendered)

    def test_wide_and_combining_unicode_use_terminal_cell_widths(self):
        screen = terminal.Screen(width=6, height=2)
        screen.feed("A界e\u0301\x1b[6GZ".encode())
        self.assertEqual(screen.render(), "A界e\u0301 Z")

    def test_limits_reject_oversized_data_and_dimensions(self):
        with self.assertRaisesRegex(ValueError, "dimensions"):
            terminal.Screen(width=terminal.MAX_WIDTH + 1)
        with self.assertRaisesRegex(ValueError, "input exceeds"):
            terminal.render_screen(b"x" * (terminal.MAX_BYTES + 1))

    def test_extreme_csi_numbers_are_bounded_and_do_not_leak_parameters(self):
        raw = b"start\x1b[" + b"9" * 100000 + b"Hend"
        with self.assertRaisesRegex(terminal.TerminalAmbiguity, "unsupported control"):
            terminal.render_screen(raw, width=20, height=3)

    def test_insert_delete_and_erase_controls_update_screen_cells(self):
        self.assertEqual(terminal.render_screen(b"Login required\x1b[1;1H\x1b[14PREADY"), "READY")
        self.assertEqual(terminal.render_screen(b"Login\x1b[1;1H\x1b[5XREADY"), "READY")
        self.assertEqual(terminal.render_screen(b"Login\x1b[1;1H\x1b[5@"), "     Login")

    def test_line_and_screen_scroll_controls_remove_stale_rows(self):
        deleted = terminal.render_screen(b"one\r\nLogin\r\nthree\x1b[2;1H\x1b[M", width=20, height=3)
        self.assertEqual(deleted, "one\nthree")
        scrolled = terminal.render_screen(b"top\r\nmiddle\r\nbottom\r\nnew", width=20, height=3)
        self.assertEqual(scrolled, "middle\nbottom\nnew")

    def test_unhandled_mutating_controls_and_incomplete_strings_fail_closed(self):
        for raw in (
            b"Login\x1b[?1047h",
            b"Login\x1b[2;1;3H",
            b"Login\x1b]title-without-terminator",
            b"Login\x1b[!p",  # DECSTR is a screen-state mutation, not a query.
            b"Login\x1b[1$H",
            b"Login\x1b[?6n",
            b"Login\x1b]4;0;#ffffff\x07",
            b"Login\x1b]10;#000000\x07",
        ):
            with self.subTest(raw=raw), self.assertRaises(terminal.TerminalAmbiguity):
                terminal.render_screen(raw)
        self.assertEqual(terminal.render_screen(b"Ready\x1b[?25l\x1b[0m"), "Ready")
        self.assertEqual(terminal.render_screen(b"Ready\x1b[6n"), "Ready")
        self.assertEqual(terminal.render_screen(b"Ready\x1b]10;?\x07"), "Ready")
        self.assertEqual(terminal.render_screen(b"Ready\x1b]11;?\x07"), "Ready")

    def test_conceal_and_unknown_sgr_operations_fail_closed(self):
        for raw in (
            b"Login required\x1b[8mhidden prompt",
            b"Login required\x1b[0;8mhidden prompt",
            b"Login required\x1b[8:1mhidden prompt",
            b"Ready\x1b[999m",
        ):
            with self.subTest(raw=raw), self.assertRaises(terminal.TerminalAmbiguity):
                terminal.render_screen(raw)
        self.assertEqual(terminal.render_screen(b"Ready\x1b[1;32m"), "Ready")
        self.assertEqual(terminal.render_screen(b"Ready\x1b[38;5;42m"), "Ready")
        self.assertEqual(terminal.render_screen(b"Ready\x1b[48;2;1;2;3m"), "Ready")

    def test_keyboard_modifier_controls_do_not_paint_or_move_text(self):
        for raw in (b"\x1b[>4;2m", b"\x1b[>4m"):
            with self.subTest(raw=raw):
                self.assertEqual(terminal.render_screen(b"Ready" + raw), "Ready")

    def test_exact_kitty_pop_default_is_a_noop_but_other_values_fail_closed(self):
        baseline = terminal.Screen(width=24, height=4).feed(b"Ready\x1b[2;3H!")
        popped = terminal.Screen(width=24, height=4).feed(b"Ready\x1b[<1u\x1b[2;3H!")
        self.assertEqual(popped.render(), baseline.render())
        self.assertEqual((popped.row, popped.col), (baseline.row, baseline.col))
        for raw in (b"\x1b[<2u", b"\x1b[<1;1u", b"\x1b[?1u"):
            with self.subTest(raw=raw), self.assertRaises(terminal.TerminalAmbiguity):
                terminal.render_screen(b"Ready" + raw)

    def test_exact_zero_cursor_style_is_ignored_without_ignoring_other_q_controls(self):
        baseline = terminal.Screen(width=24, height=4).feed(b"Ready\x1b[2;3H!")
        styled = terminal.Screen(width=24, height=4).feed(b"Ready\x1b[0 q\x1b[2;3H!")
        self.assertEqual(styled.render(), baseline.render())
        self.assertEqual((styled.row, styled.col), (baseline.row, baseline.col))
        for raw in (b"\x1b[8 q", b"\x1b[0;q", b"\x1b[0\"q"):
            with self.subTest(raw=raw), self.assertRaises(terminal.TerminalAmbiguity):
                terminal.render_screen(b"Ready" + raw)

    def test_ambiguity_metadata_retains_numeric_control_only(self):
        for raw, expected in ((b"\x1b[?9W", "?9W"), (b"\x1b[99999999H", None)):
            with self.subTest(raw=raw):
                try:
                    terminal.render_screen(raw)
                except terminal.TerminalAmbiguity as error:
                    result = terminal.ambiguity_site(error)
                    self.assertEqual(result.get("numericCSIControl"), expected)
                    self.assertIn("line", result)
                    self.assertNotIn("text", result)
                else:
                    self.fail("expected unsupported control")

    def test_ambiguity_metadata_is_bounded_ascii_control_metadata_only(self):
        try:
            terminal.render_screen(b"\x1b[?12;34W")
        except terminal.TerminalAmbiguity as error:
            result = terminal.ambiguity_site(error)
        else:
            self.fail("expected unsupported control")
        self.assertEqual(result["csiParameterCodepoints"], [63, 49, 50, 59, 51, 52])
        self.assertEqual(result["csiFinalCodepoint"], ord("W"))
        self.assertEqual(result["csiNumericValues"], [12, 34])

        # A malformed/non-CSI local is not copied even if a future caller
        # raises while a frame happens to use the familiar local names.
        def _numbers():
            params = "private-token"
            final = "H"
            raise terminal.TerminalAmbiguity()

        try:
            _numbers()
        except terminal.TerminalAmbiguity as error:
            invalid = terminal.ambiguity_site(error)
        else:
            self.fail("expected malformed control")
        self.assertNotIn("csiParameterCodepoints", invalid)
        self.assertNotIn("numericCSIControl", invalid)
        self.assertNotIn("private-token", repr(invalid))
        self.assertNotIn("params", invalid)
        self.assertNotIn("text", invalid)

    def test_ambiguity_metadata_ignores_untrusted_nonterminal_locals(self):
        private_value = "account-secret.example/token-value"

        def unrelated_failure():
            params = private_value
            final = private_value
            raise terminal.TerminalAmbiguity()

        try:
            unrelated_failure()
        except terminal.TerminalAmbiguity as error:
            metadata = terminal.ambiguity_site(error)
        else:
            self.fail("expected terminal ambiguity")
        encoded = repr(metadata)
        self.assertNotIn(private_value, encoded)
        self.assertNotIn("params", metadata)
        self.assertNotIn("final", metadata)

    def test_empty_sgr_and_terminal_reset_restore_default_visibility(self):
        self.assertEqual(terminal.render_screen(b"\x1b[30mhidden\x1b[mVisible"), "      Visible")
        self.assertEqual(terminal.render_screen(b"\x1b[30mhidden\x1bcVisible"), "Visible")
        self.assertEqual(terminal.render_screen(b"Ready\x1b[?5W\x1b[>1u\x1b[?2031h"), "Ready")

    def test_equal_foreground_and_background_hide_text_preserving_width(self):
        for raw in (
            b"\x1b[30;40mhidden",
            b"\x1b[38;5;0;48;5;0mhidden",
            b"\x1b[38;2;1;2;3;48;2;1;2;3mhidden",
            b"\x1b[30;40;7mhidden",
        ):
            with self.subTest(raw=raw):
                rendered = terminal.render_screen(raw + b"\x1b[39;49mZ")
                self.assertEqual(rendered, "      Z")
        # Hidden wide glyphs preserve both terminal cells.
        self.assertEqual(
            terminal.render_screen(b"A\x1b[30;40m\xe7\x95\x8c\x1b[39;49m Z"),
            "A   Z",
        )

    def test_overwriting_wide_glyph_clears_its_other_cell(self):
        rendered = terminal.render_screen(
            b"A\xe7\x95\x8c\x1b[1;2H\x1b[30;40mX\x1b[39;49m Z"
        )
        self.assertNotIn("界", rendered)
        self.assertNotIn("X", rendered)
        self.assertIn("A  Z", rendered)

    def test_only_exact_known_inert_queries_are_ignored(self):
        for raw in (
            b"\x1b[c", b"\x1b[0c", b"\x1b[>c", b"\x1b[>0c",
            b"\x1b[6n", b"\x1b[>q", b"\x1b[?2026$p", b"\x1b[?1004$p", b"\x1b[?u",
        ):
            with self.subTest(raw=raw):
                self.assertEqual(terminal.render_screen(b"Ready" + raw), "Ready")
        for raw in (b"\x1b[1c", b"\x1b[?6n", b"\x1b[!p", b"\x1b[>1q"):
            with self.subTest(raw=raw), self.assertRaises(terminal.TerminalAmbiguity):
                terminal.render_screen(b"Ready" + raw)

    def test_alternate_screen_isolated_then_restores_main_screen(self):
        rendered = terminal.render_screen(b"Main\x1b[?1049hAlt\x1b[?1049l", width=20, height=3)
        self.assertEqual(rendered, "Main")

    def test_unclosed_synchronized_output_fails_but_complete_pair_renders(self):
        with self.assertRaises(terminal.TerminalAmbiguity):
            terminal.render_screen(b"Ready\x1b[?2026h")
        self.assertEqual(terminal.render_screen(b"Ready\x1b[?2026h\x1b[?2026l"), "Ready")


if __name__ == "__main__":
    unittest.main()
