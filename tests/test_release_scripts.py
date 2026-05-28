from __future__ import annotations

import importlib.util
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def load_script(name: str):
    path = ROOT / "scripts" / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_next_version_infers_major_minor_patch() -> None:
    next_version = load_script("next_version")

    assert next_version.infer_bump("feat: add sessions command", []) == "minor"
    assert next_version.infer_bump("fix: repaint focused pane", ["codux/tmux.py"]) == "patch"
    assert next_version.infer_bump("refactor!: rename config keys", []) == "major"
    assert (
        next_version.infer_bump("docs: update README\n\nSemver-Bump: minor", ["README.md"])
        == "minor"
    )
    assert next_version.bump_version("1.2.3", "major") == "2.0.0"
    assert next_version.bump_version("1.2.3", "minor") == "1.3.0"
    assert next_version.bump_version("1.2.3", "patch") == "1.2.4"


def test_homebrew_formula_renders_runtime_resources() -> None:
    formula_script = load_script("render_homebrew_formula")
    lock_data = {
        "package": [
            {
                "name": "codux",
                "dependencies": [{"name": "rich"}, {"name": "typer"}],
            },
            {
                "name": "rich",
                "dependencies": [{"name": "markdown-it-py"}],
                "wheels": [
                    {
                        "url": "https://example.test/rich-py3-none-any.whl",
                        "hash": "sha256:richwheelhash",
                    }
                ],
                "sdist": {
                    "url": "https://example.test/rich.tar.gz",
                    "hash": "sha256:richhash",
                },
            },
            {
                "name": "markdown-it-py",
                "sdist": {
                    "url": "https://example.test/markdown.tar.gz",
                    "hash": "sha256:markdownhash",
                },
            },
            {
                "name": "typer",
                "dependencies": [{"name": "colorama", "marker": "sys_platform == 'win32'"}],
                "sdist": {
                    "url": "https://example.test/typer.tar.gz",
                    "hash": "sha256:typerhash",
                },
            },
            {
                "name": "colorama",
                "sdist": {
                    "url": "https://example.test/colorama.tar.gz",
                    "hash": "sha256:coloramahash",
                },
            },
        ]
    }

    formula = formula_script.render_formula(
        formula_name="codux",
        version_url="https://example.test/codux.tar.gz",
        sha256="coduxhash",
        wheel_url="https://example.test/codux-1.2.3-py3-none-any.whl",
        wheel_sha256="coduxwheelhash",
        python_formula="python@3.13",
        lock_data=lock_data,
    )

    assert "class Codux < Formula" in formula
    assert 'depends_on "python@3.13"' in formula
    assert 'depends_on "tmux"' in formula
    assert 'resource "codux-wheel"' in formula
    assert "coduxwheelhash" in formula
    assert "rich-py3-none-any.whl" in formula
    assert "rich.tar.gz" not in formula
    assert 'resource "markdown-it-py"' in formula
    assert 'resource "rich"' in formula
    assert 'resource "typer"' in formula
    assert "colorama" not in formula
    assert 'virtualenv_create(libexec, "python3.13")' in formula
    assert 'rm bin/"start"' in formula
