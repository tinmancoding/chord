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

**Via `go install`:**
```bash
go install github.com/tinmancoding/chord/cmd/chord@latest
```

**From source (using Make):**
```bash
make build          # builds ./chord in the repo root
make install        # builds and copies to ~/.local/bin
make install INSTALL_DIR=/usr/local/bin  # override install location
make clean          # removes the local binary
```

---

## Configuration

Chord looks for its config file at **`~/.config/chord/chord.yaml`** by default.

### Global flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | `-c` | `~/.config/chord/chord.yaml` | Path to the chord.yaml config file |
| `--base-dir` | `-b` | `~/chord` | Base directory for all workspaces (overrides `base_directory` in chord.yaml) |

```bash
chord -c /path/to/my-chord.yaml compose fullstack feature/payments
chord -b /tmp/workspaces compose fullstack feature/payments
```

### `~/.config/chord/chord.yaml`

```yaml
# Optional: where all workspaces are created.
# Defaults to ~/chord if omitted. Supports ~ expansion.
base_directory: "~/chord"

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

### `base_directory` precedence

The effective base directory is resolved in this order (highest wins):

1. `--base-dir` CLI flag
2. `base_directory` field in `chord.yaml`
3. Built-in default: `~/chord`

---

## Commands

### `chord compose <project_id> <target_branch> [--start-at <commitish>]`

Creates a workspace directory at `<base_directory>/<project_id>/<target_branch>` and
initialises Git worktrees for all repos in the project.

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

### `chord check`

Checks the harmony of the current workspace. Run this from inside a workspace directory.

```
  Chord workspace: feature/payments

  Repo        Expected Branch    Current Branch     Tracking Branch     Sync Status          Harmony        Dirty
  ----------  -----------------  -----------------  ------------------  -------------------  -------------  -----
  web-ui      feature/payments   feature/payments   origin/feature...   ✔ In Sync            ♪ In Tune
  api-server  feature/payments   main               origin/main         ↑ Ahead 2            ♭ Dissonance   ✎ Yes
```

The Sync Status column shows the relationship between your local branch and its remote tracking branch:
- **✔ In Sync**: Local and remote are at the same commit
- **↑ Ahead N**: Local has N commits not yet pushed to remote
- **↓ Behind N**: Remote has N commits not yet pulled to local
- **⇅ Diverged (↑N ↓M)**: Both ahead and behind (branches have diverged)
- **(no upstream)**: No remote tracking branch configured

> **Sanity Rule:** When `target_branch` is `"main"`, the expected branch per repo is
> its `default_branch`. `chord check` compares against this resolved value, so a
> `fullstack main` workspace with `api-server` on `develop` is correctly reported as
> *In Tune*.

### `chord tune [--yes] [--push]`

Synchronizes all repositories in the current workspace with their remote tracking branches. Run this from inside a workspace directory.

**Behavior:**

For repositories **without** an upstream tracking branch:
- With `--push`: Creates and pushes the upstream branch to origin (requires confirmation unless `--yes` is specified)
- Without `--push`: Skips the repository with a warning

For repositories **with** an upstream tracking branch:
- Fetches latest changes from origin
- **Only behind (no local commits)**: Fast-forwards to upstream
- **Ahead and behind (diverged)**: Rebases local commits onto upstream
- **Dirty working tree**: Automatically stashes changes before rebase (with `--yes`), then unstashes after

**Flags:**
- `--yes` / `-y`: Skip all confirmation prompts
- `--push`: Create upstream branches (if missing) and push all changes at the end for full synchronization

**Safety features:**
- Repositories in the middle of a rebase, merge, or other operation are skipped with a clear warning
- Failures in one repository don't stop the process; other repositories continue
- If stash pop fails after rebase, the user is notified to resolve conflicts manually

```bash
# Interactive mode: prompts for each operation
chord tune

# Automatic mode: perform all operations without prompts
chord tune --yes

# Full synchronization: push all changes to remote
chord tune --yes --push

# Just push changes (no local sync)
chord tune --push
```

### `chord mute <project_id> <target_branch> [--force]`

Removes the workspace at `<base_directory>/<project_id>/<target_branch>` and cleans
up Git worktree metadata from base clones.

```bash
chord mute fullstack feature/payments
chord mute fullstack feature/payments --force   # ignore uncommitted changes
```

---

## Limitations

**One workspace per project/branch combination.** Git does not allow the same
branch to be checked out in two worktrees simultaneously. Attempting to
`compose` a workspace for a project/branch that is already composed will fail
with an error. `mute` the existing workspace first before re-composing.

---

## Architecture

Base clones are stored in `~/.cache/chord/repos/<repo-id>/`. Workspaces are
lightweight worktrees pointing back to these clones, organised under the base
directory in a `<project>/<branch>` hierarchy. A `.chord-state.yaml` file at
the workspace root tracks resolved branch state for `check` and `mute`.

```
~/.cache/chord/repos/
  web-ui/              ← base clone
  api-server/          ← base clone

~/chord/                             ← base_directory
  fullstack/                         ← project
    feature-payments/                ← workspace root
      .chord-state.yaml
      web-ui/                        ← git worktree
      api-server/                    ← git worktree
    main/                            ← another workspace
      .chord-state.yaml
      web-ui/
      api-server/
```
