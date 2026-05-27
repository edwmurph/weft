from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class Theme:
    active_color_256: str = "117"
    inactive_border_256: str = "244"
    active_tab_fg_256: str = "16"

    @property
    def reset(self) -> str:
        return "\033[0m"

    @property
    def underline(self) -> str:
        return "\033[4m"

    @property
    def end_underline(self) -> str:
        return "\033[24m"

    @property
    def active_border(self) -> str:
        return f"\033[38;5;{self.active_color_256}m"

    @property
    def inactive_border(self) -> str:
        return f"\033[38;5;{self.inactive_border_256}m"

    @property
    def active_tab(self) -> str:
        return f"\033[48;5;{self.active_color_256}m\033[38;5;{self.active_tab_fg_256}m"

    def paint_border(self, text: str, active: bool) -> str:
        color = self.active_border if active else self.inactive_border
        return f"{color}{text}{self.reset}"

    def paint_active_tab(self, text: str) -> str:
        return f"{self.active_tab}{text}{self.reset}"
