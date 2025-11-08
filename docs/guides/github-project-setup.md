# GitHub Project Board Setup Guide

This guide walks you through populating your Meltica GitHub project board with items from `docs/ROADMAP.md`.

## Prerequisites

1. **GitHub Personal Access Token** with the following scopes:
   - `project` (full project access)
   - `repo` (repository access)

2. **Existing Project Board** with three columns:
   - `Todo`
   - `In Progress`
   - `Done`

3. **Dependencies:**
   - `curl` (required)
   - `jq` (optional, but recommended for easier JSON parsing)

## Quick Start

### Step 1: Get Your GitHub Token

1. Visit: https://github.com/settings/tokens
2. Click **"Generate new token"** → **"Generate new token (classic)"**
3. Give it a name: `Meltica Project Board`
4. Select scopes:
   - ✅ `project`
   - ✅ `repo`
5. Click **"Generate token"**
6. **Copy the token immediately** (you won't be able to see it again)

### Step 2: Create Your Project Board (if you haven't already)

Choose one of these options:

**Option A: Repository Project (Classic)**
1. Go to: https://github.com/coachpo/meltica/projects
2. Click **"New project"** → **"Project (classic)"**
3. Name: `Meltica Roadmap`
4. Create three columns: `Todo`, `In Progress`, `Done`

**Option B: User Project (v2)**
1. Go to: https://github.com/users/coachpo/projects
2. Click **"New project"**
3. Choose template or start from scratch
4. Ensure you have columns: `Todo`, `In Progress`, `Done`

### Step 3: Run the Population Script

```bash
# Navigate to your project directory
cd /Users/liqing/Documents/PersonalProjects/meltica

# Make the script executable
chmod +x scripts/populate-project-board.sh

# Set your GitHub token
export GITHUB_TOKEN="ghp_your_token_here"

# Run the script
./scripts/populate-project-board.sh
```

### Step 4: Verify Results

The script will:
1. ✅ Create 8 color-coded labels for categorization
2. ✅ Create 16 GitHub issues from your roadmap
3. ✅ Add issues to the appropriate columns (Todo or Done)
4. ✅ Display the project URL when complete

## What Gets Created

### Labels

The script creates these labels for organization:

| Label | Color | Purpose |
|-------|-------|---------|
| `priority:urgent` | Red | Urgent items |
| `priority:important` | Green | Important items |
| `status:planning` | Blue | Items in planning phase |
| `status:postponed` | Yellow | Postponed items |
| `category:infrastructure` | Purple | Infrastructure work |
| `category:trading` | Dark Blue | Trading/execution features |
| `category:monitoring` | Light Blue | Observability/monitoring |
| `category:testing` | Light Green | Testing improvements |

### Issues in "Todo" Column

**Urgent and Important (PLANNING)**
1. Robust OMS/execution system

**Not Urgent and Important (PLANNING)**
2. Portfolio/accounting enhancements

**Not Urgent and Important (POSTPONED)**
3. Reliability & scalability improvements
4. Operations & monitoring automation
5. Multi-venue routing & failover
6. Expanded testing coverage

**Urgent and Important (POSTPONED)**
7. Persistent state and data backbone
8. Security of control surfaces & secrets

**Not Urgent and Not Important (POSTPONED)**
9. Advanced performance tuning
10. ML/optimization features

### Issues in "Done" Column

**Urgent and Important (Completed)**
11. ✅ Risk management and safety controls
12. ✅ Real exchange connectivity
13. ✅ Core persistence backbone

**Not Urgent and Important (Completed)**
14. ✅ Backtesting and historical replay
15. ✅ Durable in-process bus
16. ✅ Telemetry & dashboards

## Troubleshooting

### "Requires authentication" error

Make sure you've exported your token:
```bash
export GITHUB_TOKEN="ghp_your_token_here"
```

### "jq not installed" warning

The script works without `jq`, but you'll need to manually enter IDs when prompted.

Install `jq` for a smoother experience:
```bash
# macOS
brew install jq

# Linux
sudo apt-get install jq  # Debian/Ubuntu
sudo yum install jq      # RHEL/CentOS
```

### Cannot find project

Check that:
1. Your project board exists
2. The project name contains "Meltica"
3. Your token has the correct permissions
4. You're using the right project type (classic vs v2)

### Rate limiting

The script includes 1-second delays between API calls. If you hit rate limits, wait a few minutes and try again.

## Manual Alternative

If the script doesn't work for your setup, you can manually create issues and add them to your project board:

1. Go to: https://github.com/coachpo/meltica/issues/new
2. Copy issue titles and descriptions from `docs/ROADMAP.md`
3. Add appropriate labels
4. From the issue page, use the "Projects" sidebar to add it to your board
5. Drag issues to the correct columns

## Project Configuration

The script saves project metadata to `/tmp/meltica_project.env`:
```bash
PROJECT_ID=123456
PROJECT_NUMBER=1
TODO_COLUMN_ID=789012
IN_PROGRESS_COLUMN_ID=345678
DONE_COLUMN_ID=901234
```

You can reuse these IDs for future operations.

## GitHub API References

- [Projects API (Classic)](https://docs.github.com/en/rest/projects)
- [Issues API](https://docs.github.com/en/rest/issues)
- [Projects v2 API (GraphQL)](https://docs.github.com/en/issues/planning-and-tracking-with-projects/automating-your-project/using-the-api-to-manage-projects)

## Next Steps

After populating your board:

1. **Prioritize:** Drag items within columns to set priority order
2. **Start Working:** Move items from `Todo` to `In Progress` as you begin
3. **Track Progress:** Move items to `Done` when complete
4. **Link PRs:** Reference issue numbers in pull requests (e.g., `Closes #5`)
5. **Update Roadmap:** Keep `docs/ROADMAP.md` in sync with project board status

## Automation Ideas

Consider setting up GitHub Actions to:
- Auto-label issues based on title keywords
- Move issues to "In Progress" when a PR is opened
- Move issues to "Done" when a PR is merged
- Post comments on issues when work starts/completes

---

**Last updated:** 2025-11-09
**Script location:** `scripts/populate-project-board.sh`
**Roadmap source:** `docs/ROADMAP.md`
