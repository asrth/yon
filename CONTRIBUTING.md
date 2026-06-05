# Contributing to Yon

Thanks for your interest in **Yon** — a fast, offline, open-source desktop HTTP
API client built in Go + Fyne. Please read this before opening a pull request.
It's short on purpose.

## 1. Open an issue first — and wait for approval

Every change starts with a GitHub issue. **Do not open a pull request before an
issue for it exists and a maintainer has approved it.**

1. Open an issue describing the bug or feature: the problem, and (for a fix) how
   to reproduce it. Keep it focused.
2. Wait for a maintainer to triage and **approve** it (it gets labelled
   `approved` / assigned to you). This is when the work is agreed.
3. Only then start coding and open a PR that links the issue (e.g. `Closes #123`).

PRs without an approved issue may be closed without review — this keeps work
aligned before anyone spends time on it.

> Because this is a public repository, GitHub requires a maintainer to approve
> the CI runs on a contributor's pull request before they run. That's expected —
> your checks start once a maintainer approves the run.

## 2. How changes land

The default branch `main` is protected — nothing is committed to it directly.

```
approved issue
  → fork, create a branch:  <type>/<issue#>-short-slug   (e.g. feat/123-oauth2)
  → commit your work on that branch
  → open a pull request into main that links the issue
  → CI must be green (build & test on Linux and macOS)
  → a maintainer squash-merges it
```

A release is cut only **after** changes are merged to `main` — never from an
unmerged branch.

## 3. Code expectations

- `gofmt` clean, `go vet ./...` clean, and `go test ./... -race` passing.
- Match the style, naming, and comment density of the surrounding code.
- **Architecture rule:** only `main` and `internal/ui` may import Fyne. The
  engine (`internal/yonner`), data (`internal/model`), and storage
  (`internal/store`) stay UI-free. See `CONTEXT.md` / `docs/adr/` for the domain
  language and key decisions.
- Keep the diff scoped to the issue. Add a test for a fix — it should fail before
  the change and pass after.
- **No AI-assistant attribution** in commit messages (no `Co-Authored-By: …`
  assistant trailer). Repo-required disclosure in a PR body is fine; the commit
  message stays clean.

## 4. Security

Never put secrets (tokens, certificates, passwords) in the repo or in test data.
Secret variable values belong in the gitignored `.env`, never in a `.yon` file.
To report a security issue, open an issue marked *security* (or contact a
maintainer) rather than posting an exploit publicly.

## 5. Licence

Yon is MIT licensed. By contributing you agree your contribution is licensed
under the same terms.

Questions? Open an issue. Thanks for helping make Yon better.
