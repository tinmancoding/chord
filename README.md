# Chord 🎵
> **Multi-Repository Git Worktree Orchestrator** — "Git development in perfect harmony."

## Overview

Chord manages development environments where a single feature spans multiple Git repositories. It uses **Git worktrees** to create isolated workspaces ("Chords") where every repository is tuned to the correct branch.

### Terminology

| Term | Meaning |
|---|---|
| **Note** | A single Git repository |
| **Frequency** | The Git branch or commitish |
| **Chord** | A workspace (collection of worktrees) |
| **Composition** | The act of setting up the environment |

---

## Installation

```bash
go install github.com/landrasi/chord/cmd/chord@latest
```

---

## Configuration (`chord.yaml`)

```yaml
repositories:
  web-ui:
    url: "git@github.com:org/frontend.git"
    default_branch: "main"
  api-server:
    url: "git@github.com:org/backend.git"
    default_branch: "develop"

projects:
  fullstack:
    repos:
      - web-ui
      - api-server
```

---

## Commands

### `chord compose <project_id> <target_branch> [--start-at <commitish>]`

Creates a workspace directory and initialises Git worktrees for all repos in the project.

**Branch resolution (Tuning Logic):**
1. Fetch `--all --prune` from origin.
2. If `target_branch` is `"main"` → use the repo's configured `default_branch`.
3. Else if the local branch exists → use it.
4. Else if `origin/<target_branch>` exists → create a local tracking branch.
5. Else if `--start-at` is provided → create the branch from that commitish.
6. Else → create the branch from `default_branch`.

```bash
chord compose fullstack feature/payments
chord compose fullstack main
chord compose fullstack feature/new-auth --start-at v2.1.0
```

### `chord tune`

Checks the harmony of the current workspace. Run this from inside a workspace directory.

```
  Chord workspace: feature/payments

  Repo        Expected Branch    Current Branch     Harmony        Dirty
  ----------  -----------------  -----------------  -------------  -----
  web-ui      feature/payments   feature/payments   ♪ In Tune
  api-server  feature/payments   main               ♭ Dissonance   ✎ Yes
```

> **Sanity Rule:** When `target_branch` is `"main"`, the expected branch per repo is
> its `default_branch`. `chord tune` compares against this resolved value, so a
> `fullstack main` workspace with `api-server` on `develop` is correctly reported as
> *In Tune*.

### `chord mute <target_branch> [--force]`

Removes the workspace and cleans up Git worktree metadata from base clones.

```bash
chord mute feature/payments
chord mute feature/payments --force   # ignore uncommitted changes
```

---

## Architecture

Base clones are stored in `~/.cache/chord/repos/<repo-id>/`. Workspaces are
lightweight worktrees pointing back to these clones. A `.chord-state.yaml` file
at the workspace root tracks resolved branch state for `tune` and `mute`.

```
~/.cache/chord/repos/
  web-ui/        ← base clone
  api-server/    ← base clone

./feature-payments/          ← workspace root
  .chord-state.yaml
  web-ui/                    ← git worktree
  api-server/                ← git worktree
```
