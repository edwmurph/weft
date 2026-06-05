# Repository Settings Checklist

These settings are applied in GitHub, not from files in this repository. They define the public contribution rails for Weft.

## Repository Features

- Enable Issues.
- Disable Discussions.
- Keep Wiki and Projects disabled unless the maintainer intentionally enables them later.

## Pull Requests

- Allow squash merge only.
- Disable merge commits.
- Disable rebase merges.
- Enable automatic branch deletion after merge if available.

## Actions

- Set workflow permissions to read-only by default.
- Require maintainer approval before running workflows from outside collaborators or fork pull requests.
- Do not enable Dependabot or Renovate in this pass.

## Main Branch Rules

- Target `main`.
- Allow bypass for `@edwmurph` and repository admins.
- Require pull requests before merge for non-bypass users.
- Require one approval.
- Require CODEOWNERS review.
- Dismiss stale approvals when new commits are pushed.
- Require conversation resolution.
- Require the `CI Gate` status check. That single gate fails when commit-subject validation or the required Ubuntu/macOS matrix fails, and it allows documentation or process-only changes to avoid expensive OS jobs.
- Require branches to be up to date before merging.
- Require linear history.
- Block force pushes.
- Block branch deletion.

## Access Model

- Keep `@edwmurph` as the only maintainer for now.
- Do not grant write access to outside contributors; they should contribute through forks and pull requests.

## Security

- Enable secret scanning and push protection if available.
- Enable private vulnerability reporting if available.
