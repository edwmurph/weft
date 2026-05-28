from __future__ import annotations

import argparse
import re
import subprocess
from pathlib import Path


SEMVER_RE = re.compile(r"^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)$")
VERSION_LINE_RE = re.compile(r'(?m)^(version\s*=\s*")([^"]+)(")\s*$')
EMPTY_TREE = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
EXPLICIT_BUMP_RE = re.compile(r"(?im)^(?:semver[- ]bump|release[- ]bump):\s*(major|minor|patch)\b")
BREAKING_RE = re.compile(r"(?im)^(BREAKING CHANGE:|[a-z][a-z0-9_-]*(?:\([^)]*\))?!:)")
MINOR_RE = re.compile(
    r"(?im)^("
    r"feat(?:\([^)]*\))?:|"
    r"add(?:ed|s)?\b|"
    r"creat(?:e|ed|es)\b|"
    r"implement(?:ed|s)?\b|"
    r"introduc(?:e|ed|es)\b|"
    r"support(?:ed|s)?\b"
    r")"
)
PATCH_RE = re.compile(r"(?im)^(fix|docs?|chore|test|tests|refactor|update|remove|repair)\b")


def run_git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def read_version(pyproject_path: Path) -> str:
    match = VERSION_LINE_RE.search(pyproject_path.read_text())
    if not match:
        raise ValueError(f"could not find [project] version in {pyproject_path}")
    return match.group(2)


def bump_version(version: str, bump: str) -> str:
    match = SEMVER_RE.match(version)
    if not match:
        raise ValueError(f"unsupported version {version!r}; expected MAJOR.MINOR.PATCH")

    major = int(match.group("major"))
    minor = int(match.group("minor"))
    patch = int(match.group("patch"))

    if bump == "major":
        return f"{major + 1}.0.0"
    if bump == "minor":
        return f"{major}.{minor + 1}.0"
    if bump == "patch":
        return f"{major}.{minor}.{patch + 1}"
    raise ValueError(f"unsupported bump {bump!r}")


def infer_bump(commit_messages: str, changed_files: list[str]) -> str:
    explicit = EXPLICIT_BUMP_RE.search(commit_messages)
    if explicit:
        return explicit.group(1).lower()
    if BREAKING_RE.search(commit_messages):
        return "major"
    if MINOR_RE.search(commit_messages):
        return "minor"
    if PATCH_RE.search(commit_messages):
        return "patch"
    if any(path.startswith(".github/workflows/") for path in changed_files):
        return "minor"
    return "patch"


def write_version(pyproject_path: Path, version: str) -> None:
    text = pyproject_path.read_text()
    updated, count = VERSION_LINE_RE.subn(rf"\g<1>{version}\3", text, count=1)
    if count != 1:
        raise ValueError(f"could not update version in {pyproject_path}")
    pyproject_path.write_text(updated)


def fallback_base(head: str) -> str:
    try:
        return run_git("describe", "--tags", "--match", "v[0-9]*", "--abbrev=0", head)
    except subprocess.CalledProcessError:
        try:
            return run_git("rev-parse", f"{head}^")
        except subprocess.CalledProcessError:
            return EMPTY_TREE


def usable_base(base: str | None, head: str) -> str:
    if not base or set(base) == {"0"}:
        return fallback_base(head)
    try:
        run_git("cat-file", "-e", f"{base}^{{commit}}")
    except subprocess.CalledProcessError:
        return fallback_base(head)
    return base


def changed_files(base: str, head: str) -> list[str]:
    output = run_git("diff", "--name-only", "--diff-filter=ACMRT", base, head)
    return [line for line in output.splitlines() if line]


def commit_messages(base: str, head: str) -> str:
    if base == EMPTY_TREE:
        return run_git("log", "--format=%B%n---END-COMMIT---", head)
    return run_git("log", "--format=%B%n---END-COMMIT---", f"{base}..{head}")


def write_github_output(output_path: Path, values: dict[str, str]) -> None:
    with output_path.open("a") as output:
        for key, value in values.items():
            output.write(f"{key}={value}\n")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Infer and apply the next Codux release version.")
    parser.add_argument("--base", help="Base commit/ref for the release diff.")
    parser.add_argument("--head", default="HEAD", help="Head commit/ref for the release diff.")
    parser.add_argument("--bump", choices=["major", "minor", "patch"], help="Override bump type.")
    parser.add_argument("--pyproject", default="pyproject.toml", help="pyproject.toml path.")
    parser.add_argument(
        "--write", action="store_true", help="Write the next version to pyproject.toml."
    )
    parser.add_argument("--github-output", help="Path to a GitHub Actions output file.")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    pyproject_path = Path(args.pyproject)
    base = usable_base(args.base, args.head)
    files = changed_files(base, args.head)
    messages = commit_messages(base, args.head)
    bump = args.bump or infer_bump(messages, files)
    previous_version = read_version(pyproject_path)
    version = bump_version(previous_version, bump)

    if args.write:
        write_version(pyproject_path, version)

    values = {
        "base": base,
        "bump": bump,
        "previous_version": previous_version,
        "version": version,
        "tag": f"v{version}",
    }
    if args.github_output:
        write_github_output(Path(args.github_output), values)
    for key, value in values.items():
        print(f"{key}={value}")


if __name__ == "__main__":
    main()
