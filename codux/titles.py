from __future__ import annotations

CODEX_TITLE_TEMPLATE = "{codex}"
CODEX_TITLE_PENDING = "..."
IGNORED_CODEX_TITLES = {"", "CODEX", "NAV", "Codux Empty", "Codux Loading"}
TITLE_TEMPLATE_VARIABLES = (
    (
        CODEX_TITLE_TEMPLATE,
        "Live title from the Codex pane",
    ),
)


def title_uses_codex_placeholder(title: str) -> bool:
    return CODEX_TITLE_TEMPLATE in title


def is_transient_codex_title(title: str) -> bool:
    return (
        title.startswith(("Ready | ", "Starting | "))
        or " Ready | " in title
        or " Starting | " in title
        or title in {"Ready", "Starting"}
        or title.endswith((" Ready", " Starting"))
    )


def normalize_codex_title(raw_title: str | None) -> str | None:
    if raw_title is None:
        return None
    title = raw_title.strip()
    if title in IGNORED_CODEX_TITLES:
        return None
    if title.endswith(".local"):
        return None
    return title


def render_display_title(title: str, codex_title: str | None) -> str:
    if not title_uses_codex_placeholder(title):
        return title
    return title.replace(
        CODEX_TITLE_TEMPLATE,
        normalize_codex_title(codex_title) or CODEX_TITLE_PENDING,
    )


def recovered_tab_title(raw_title: str | None) -> str:
    title = (raw_title or "").strip()
    return title or CODEX_TITLE_TEMPLATE
