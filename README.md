# Chord 🎵
> **Multi-Repository Git Orchestrator** — "Git development in perfect harmony."

## Overview

Chord manages development environments for multi-repository projects with flexible, ad-hoc repository composition. Create workspaces by specifying repositories directly or using templates, with full Git clones for maximum flexibility.

### Terminology

| Term | Meaning |
|---|---|
| **Repository** | A single Git repository |
| **Chord** | A workspace containing one or more repositories |
| **Composition** | The act of setting up a chord workspace |
| **Template** | A frequently used repository group preset |
| **Namespace** | Organizational category for chords (supports hierarchical paths) |

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
chord -c /path/to/my-chord.yaml compose my-workspace --template fullstack --branch feature/payments
chord -b /tmp/workspaces compose my-workspace --repo web-ui@main
```

### `~/.config/chord/chord.yaml`

```yaml
# Optional: where all workspaces are created.
# Defaults to ~/chord if omitted. Supports ~ expansion.
base_directory: "~/chord"

# Optional: default namespace for workspaces.
# Defaults to "default" if omitted.
default_namespace: "default"

# Templates: frequently used repository groups with defaults
# These are just convenient presets - you can always compose ad-hoc
templates:
  fullstack:
    namespace: "work"  # Optional: default namespace for this template
    repos:
      - url: "git@github.com:org/frontend.git"
        default_branch: "main"  # Used for special "main" mapping
        name: "web-ui"  # Optional: directory name (defaults to repo name from URL)
      - url: "git@github.com:org/backend.git"
        default_branch: "develop"  # If --branch main is used, this repo uses "develop"
        name: "api-server"
  
  portfolio:
    namespace: "personal"
    repos:
      - url: "git@github.com:me/website.git"
        default_branch: "main"

# Optional: Repository aliases for convenience
# These allow shorter syntax: --repo web-ui@feature/branch
aliases:
  web-ui: "git@github.com:org/frontend.git"
  api-server: "git@github.com:org/backend.git"
  personal-site: "git@github.com:me/website.git"
```

### Namespace Resolution

Namespaces help organize workspaces into categories. They support hierarchical paths using slashes (e.g., `work/team-a/project-x`). 

**How namespaces affect directory structure:**
- Final path: `<base_directory>/<namespace>/<chord-name>/`
- Namespace can contain slashes: `work/team-a` → `~/chord/work/team-a/my-chord/`
- Chord names are unique within their namespace (different namespaces can have same chord name)

**Namespace resolution order (highest priority wins):**

1. `--namespace` CLI flag
2. Namespace prefix in chord name (e.g., `work/team-a/my-workspace`)
3. Template-specific namespace in config
4. `default_namespace` from config
5. Built-in default: `"default"`

**Examples:**
```bash
# Using namespace flag
chord compose my-workspace --namespace work/team-a
# Creates: ~/chord/work/team-a/my-workspace/

# Using namespace prefix in chord name
chord compose work/team-a/my-workspace
# Creates: ~/chord/work/team-a/my-workspace/

# Using template's namespace
chord compose my-workspace --template fullstack  # fullstack has namespace: "work"
# Creates: ~/chord/work/my-workspace/
```

### `base_directory` precedence

The effective base directory is resolved in this order (highest wins):

1. `--base-dir` CLI flag
2. `base_directory` field in `chord.yaml`
3. Built-in default: `~/chord`

---

## Commands

### `chord compose <chord-name> [options]`

Creates a workspace with flexible repository composition. You can use templates, specify repositories ad-hoc, or mix both.

**Repository specification format:**
- `<url@commitish>` — Full URL with commitish
- `<alias@commitish>` — Using aliases from config

Examples:
- `git@github.com:org/repo.git@feature/branch`
- `https://github.com/org/repo.git@v1.2.3`
- `web-ui@main` (using alias)

**Flags:**
- `--template <name>` / `-t`: Use a template from config
- `--branch <branch>`: Branch to use for all template repos (respects default_branch mapping)
- `--from <commitish>`: When creating new branches, start from this commitish (tag, commit, or branch)
- `--repo <spec>` / `-r`: Add repository (can be used multiple times)
- `--namespace <ns>` / `-n`: Override namespace
- `--only <repo1,repo2>`: Partial composition (defers others)

**Chord naming:**
- Chord names can include namespace prefixes (e.g., `work/team-a/my-workspace`)
- The last `/` separates the chord name from the namespace path
- Chord names must be unique within their namespace

**Examples:**

```bash
# Using a template
chord compose my-workspace --template fullstack --branch feature/payments

# Using a template with namespace
chord compose work/my-workspace --template fullstack --branch feature/payments

# Ad-hoc composition with full URLs
chord compose feat-123 \
  --repo git@github.com:org/frontend.git@feature/ui \
  --repo git@github.com:org/backend.git@main \
  --repo git@github.com:org/shared.git@v1.2.3

# Using aliases
chord compose feat-456 \
  --repo web-ui@feature/new-design \
  --repo api-server@main

# Mixed: template + ad-hoc overrides
chord compose work/team-a/sprint-42 \
  --template fullstack \
  --branch feature/payments \
  --repo git@github.com:org/extra-lib.git@v2.0.0

# Single repository
chord compose my-experiment --repo web-ui@experimental/feature

# Partial composition (deferred repos)
chord compose feat-123 --template fullstack --branch feature/payments --only web-ui
```

**Chord naming vs. branch naming:**

The chord name (first argument) is the workspace directory name and can be different from the branch name:

```bash
# Chord name: "sprint-42", Branch: "feature/payments"
chord compose sprint-42 --template fullstack --branch feature/payments
# Creates: ~/chord/work/sprint-42/  (workspace directory)
# Each repo inside checked out to: feature/payments (git branch)

# Chord name can match branch name
chord compose feature/payments --template fullstack --branch feature/payments

# Or be completely different
chord compose my-experiment --template fullstack --branch main
```

**Branch Resolution Logic:**

When composing a chord, Chord intelligently handles branch availability:

1. **Branch exists remotely**: Creates local tracking branch from remote
2. **Branch exists locally (cache)**: Uses existing local branch
3. **Branch doesn't exist**: Creates new local branch from:
   - `--from` commitish (if specified), OR
   - `default_branch` from template (if available), OR
   - `"main"` as fallback

**Special case for `--branch main`:**
- If you specify `--branch main` and a repo has `default_branch: "develop"` in its template config, Chord uses "develop" instead
- This allows working with repos that use different default branch names

**Local-only branches:**
- Branches created locally (that don't exist on remote) work normally in your workspace
- Use `chord tune --push` to create remote branches when you're ready to share

**Starting point with `--from`:**
- Use `--from` to specify the starting point when creating new branches
- Only applies when the branch doesn't exist yet (ignored for existing branches)
- Accepts any valid commitish: branch name, tag, or commit SHA
- Common use cases:
  - Create feature branches from a specific release tag: `--from v1.2.0`
  - Start from a different branch: `--from develop`
  - Branch from a specific commit: `--from abc123`

**Examples:**
```bash
# Create workspace on feature/payments branch
# If branch doesn't exist in repos, creates from default_branch or "main"
chord compose sprint-42 --template fullstack --branch feature/payments

# Create feature branch starting from v1.2.0 release tag
chord compose hotfix-123 --template fullstack --branch hotfix/security-fix --from v1.2.0

# Start feature branch from develop instead of main
chord compose feat-456 --template fullstack --branch feature/new-ui --from develop

# --from is ignored if branch already exists
chord compose existing --template fullstack --branch main --from v1.0.0
# This checks out the existing 'main' branch, not v1.0.0
```

**Partial workspace composition with `--only`:**

The `--only` flag creates clones for specific repositories immediately, while deferring others until later. This is useful when:
- Working in CI workflows where some repository branches are created asynchronously
- Starting work on a subset of repositories before others are ready
- Testing changes in a specific repository without needing the full environment

Deferred repositories are tracked in the workspace state and can be created later when their remote branches become available.

**Complete workflow example:**

```bash
# Step 1: Create workspace with only web-ui (other repos deferred)
chord compose feat-123 --template fullstack --branch feature/payments --only web-ui

# Output:
#   ✔ [web-ui] Clone ready at ~/chord/work/feat-123/web-ui
#   
#   → Deferred repositories (use 'chord tune' to create later):
#     • api-server
#     • shared-lib

# Step 2: Work on web-ui...
cd ~/chord/work/feat-123/web-ui
# Make changes, commit, push...

# Step 3: Later, check if deferred repos are ready
cd ~/chord/work/feat-123
chord tune

# Output:
#   Checking for deferred repositories...
#     [api-server] Remote branch 'feature/payments' found
#       Create clone for api-server? [y/N]: y
#     [api-server] Clone created at ~/chord/work/feat-123/api-server
#     
#     [shared-lib] Remote branch 'feature/payments' not found yet
#       (will check again next time)

# Step 4: Check again later when all branches are ready
chord tune --yes  # Auto-creates all available deferred repos
```

### `chord check`

Checks the harmony of the current workspace. Run this from inside a workspace directory.

**Example output:**
```
  Namespace: work
  Chord: feat-123
  Template: fullstack
  Repos: 2

  ♫  Chord workspace: 

  ┌────────────┬─────────────────┬────────────────┬─────────────────┬───────────────────┬──────────────┬───────┐
  │    REPO    │ EXPECTED BRANCH │ CURRENT BRANCH │ TRACKING BRANCH │   SYNC STATUS     │   HARMONY    │ DIRTY │
  ├────────────┼─────────────────┼────────────────┼─────────────────┼───────────────────┼──────────────┼───────┤
  │ web-ui     │ feature/pay..   │ feature/pay..  │ origin/featu..  │ ✔ In Sync         │ ♪ In Tune    │       │
  │ api-server │ feature/pay..   │ main           │ origin/main     │ ↑ Ahead 2         │ ♭ Dissonance │ ✎ Yes │
  └────────────┴─────────────────┴────────────────┴─────────────────┴───────────────────┴──────────────┴───────┘

  → 1 repository out of tune
```

**Sync Status meanings:**
- **✔ In Sync**: Local and remote are at the same commit
- **↑ Ahead N**: Local has N commits not yet pushed to remote
- **↓ Behind N**: Remote has N commits not yet pulled to local
- **⇅ Diverged (↑N ↓M)**: Both ahead and behind (branches have diverged)
- **(no upstream)**: No remote tracking branch configured

If there are deferred repositories (from `--only` flag during compose), they will be listed after the main table with information about when they were last checked for remote branch availability.

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

For **deferred repositories** (from `--only` flag during compose):
- Checks if remote branches have become available
- Prompts to create clones for repos whose branches now exist on the remote (unless `--yes` is specified)
- Automatically tracks which repos have been checked and when

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

**CI Workflow Example:**

When working with repositories where branches are created by CI:

```bash
# Day 1: Start with just the web-ui repo
chord compose feat-123 --template fullstack --branch feature/payments --only web-ui
# Work on web-ui...

# Day 2: CI has created branches for other repos, check if they're ready
chord tune
# Output:
#   Checking for deferred repositories...
#     [api-server] Remote branch 'feature/payments' found
#       Create clone for api-server? [y/N]: y
#     [api-server] Clone created at ~/chord/work/feat-123/api-server
```

### `chord list [filter] [--namespace <ns>]`

Lists all workspaces. Alias: `chord ls`

**Flags:**
- `--namespace` / `-n`: Filter by namespace (e.g., `work`, `work/team-a`)
- `[filter]`: Optional positional filter string to match chord names (case-insensitive substring match)

```bash
# List all workspaces
chord list

# List workspaces in "work" namespace
chord list --namespace=work

# List workspaces in hierarchical namespace
chord list --namespace=work/team-a

# Filter by name
chord list payments

# Combine filters
chord list --namespace=work feature

# Using the alias
chord ls
```

**Example output:**
```
Namespace             Chord              Template    Repos    Path
--------------------  -----------------  ----------  -------  --------------------------------
work                  feat-123           fullstack   2        ~/chord/work/feat-123
work/team-a           sprint-42          fullstack   2        ~/chord/work/team-a/sprint-42
personal              blog-redesign      -           1        ~/chord/personal/blog-redesign
default               experiment         -           1        ~/chord/default/experiment

Total workspaces: 4
```

### `chord mute <chord-name> [--namespace <ns>] [--force] [--remote]`

Removes a workspace and cleans up Git metadata.

```bash
# Remove a workspace
chord mute feat-123

# Remove with namespace prefix
chord mute work/feat-123

# Remove with namespace flag
chord mute feat-123 --namespace work

# Ignore uncommitted changes
chord mute feat-123 --force

# Also delete remote branches
chord mute feat-123 --remote --yes
```

---

## Architecture

Chord uses a two-tier caching system for efficient repository management:

**Cache Layer** (`~/.cache/chord/repos/`):
- Bare Git clones shared across all namespaces and workspaces
- Each repository has a `.chord-cache-meta` file for URL validation
- Used as `--reference` when creating workspace clones for fast initialization

**Workspace Layer** (`~/chord/`):
- Flat structure: `<base>/<namespace>/<chord-name>/<repo-name>`
- Namespaces can contain slashes for hierarchical organization (e.g., `work/team-a/project-x`)
- The namespace path IS the directory path (slashes in namespace = subdirectories)
- Each chord has a `.chord-state.yaml` tracking repository state
- Full Git clones (not worktrees) for complete independence

```
~/.cache/chord/repos/                   ← shared cache (namespace-agnostic)
  org-frontend/                         ← bare clone
    .chord-cache-meta                   ← URL validation
  org-backend/                          ← bare clone
    .chord-cache-meta

~/chord/                                ← base_directory
  work/                                 ← namespace level 1
    feat-123/                           ← chord in "work" namespace
      .chord-state.yaml                 ← state tracking
      web-ui/                           ← full git clone
      api-server/                       ← full git clone
    feat-456/                           ← another chord in "work"
      web-ui/
      api-server/
    team-a/                             ← namespace level 2 (work/team-a)
      sprint-42/                        ← chord in "work/team-a" namespace
        .chord-state.yaml
        web-ui/
        api-server/
  personal/                             ← namespace level 1
    side-projects/                      ← namespace level 2 (personal/side-projects)
      blog-redesign/                    ← chord in "personal/side-projects"
        .chord-state.yaml
        website/
  default/                              ← default namespace
    experiment/                         ← chord in "default" namespace
      .chord-state.yaml
      test-repo/
```

**Why full clones instead of worktrees?**
- Allows the same branch to be checked out in multiple chords simultaneously
- Enables a repository to appear in multiple chords at the same time
- Each chord is completely independent
- Still fast to create thanks to `--reference` using the shared cache

---

## Troubleshooting

### Cache Issues

If you encounter issues with repository caches:

```bash
# Check cache location
ls -la ~/.cache/chord/repos/

# View cache metadata for a repo
cat ~/.cache/chord/repos/<repo-name>/.chord-cache-meta

# Clear cache for a specific repository
rm -rf ~/.cache/chord/repos/<repo-name>

# Clear all caches (will be rebuilt on next compose)
rm -rf ~/.cache/chord/repos/
```

### Common Issues

**"Cache conflict" error:**
- Occurs when a repository's URL has changed in your config
- Solution: Remove the cache directory for that repo or update config to match

**Branch not found errors:**
- Ensure the branch exists either locally in cache or on remote
- For new branches, use `--start-at` to specify where to create them from

**Permission denied when pushing:**
- Use `chord tune --push` to create remote branches (requires appropriate permissions)
- Ensure SSH keys or credentials are properly configured

---

## License

MIT

