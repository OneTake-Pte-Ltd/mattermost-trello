# Mattermost Trello Bot

A Mattermost plugin that lets AI-powered bots create and manage Trello cards directly from chat threads. Each bot maps to a specific Trello board and list, and is controlled entirely through `@mentions` — no slash commands to register, no separate interfaces to learn.

---

## Table of Contents

- [How it works](#how-it-works)
- [Deployment](#deployment)
- [Admin Configuration](#admin-configuration)
- [End User Guide](#end-user-guide)
- [Troubleshooting](#troubleshooting)

---

## How it works

1. A Mattermost administrator configures one or more bots, each linked to a Trello board and list.
2. Each bot is added as a member of the specific channels it should serve.
3. Users `@mention` a bot in a channel to create a Trello card from that message. Claude (via Anthropic API) generates a structured title, description, and task checklist automatically.
4. Subsequent replies in the same thread can update the card, mark tasks done, add comments, or generate a Linear issue — all by mentioning the bot again with a command.

---

## Deployment

### Requirements

| Requirement | Minimum version |
|---|---|
| Mattermost Server | 6.2.1 |
| Anthropic API key | Any (Claude Sonnet recommended) |
| Trello account | Power-Up API access |

### Installing the plugin

**Option A — Download a pre-built release (recommended)**

1. Go to the [Releases](../../releases) page of this repository.
2. Download the latest `.tar.gz` file from the **Latest build (master)** pre-release (or a versioned release tag).
   > Do **not** use the "Source code" zip/tar.gz — those contain only source files, not compiled binaries.
3. In Mattermost: **System Console → Plugin Management → Upload Plugin**, select the `.tar.gz`, and click **Upload**.

**Option B — Build from source**

Requirements: Go 1.21+, Node.js 20+, npm 8+.

```bash
git clone https://github.com/OneTake-Pte-Ltd/mattermost-trello.git
cd mattermost-trello
go run build/manifest/main.go apply   # generate webapp/src/manifest.ts
cd webapp && npm ci && npm run build  # build the admin UI bundle
cd ..
make dist                             # produces dist/com.mattermost.mattermost-trello-*.tar.gz
```

Upload the resulting `.tar.gz` via System Console as above.

### Enabling plugin uploads

If you see "Plugin uploads are disabled" in System Console, add this to your Mattermost `config.json` and restart:

```json
"PluginSettings": {
    "EnableUploads": true
}
```

---

## Admin Configuration

All configuration lives in **System Console → Plugins → Mattermost Trello Bot**.

### Global settings

| Setting | Description |
|---|---|
| **Anthropic API Key** | Your `sk-ant-...` key from [console.anthropic.com](https://console.anthropic.com). Required. |
| **Claude Model** | Model ID to use (default: `claude-sonnet-4-6`). Use `claude-opus-4-6` for the most capable model. |
| **Max Tokens** | Maximum tokens per Claude response (default: `1024`). Increase to `2048`+ for longer card descriptions. |
| **Global Context** | Optional free text injected into every Anthropic call across all bots. Use it for company-wide background: product name, terminology, preferred tone, etc. |

### Configuring bots

The **Bot Configurations** section shows a CRUD interface — one collapsible card per bot. Click **+ Add Bot** to create a new one.

Each bot has the following fields:

| Field | Required | Description |
|---|---|---|
| **Bot Username** | Yes | The Mattermost username for this bot (e.g. `trellobot`). Users tag it as `@trellobot`. Must be lowercase, no spaces. |
| **Display Name** | Yes | Human-readable name shown in Mattermost (e.g. `Trello Bot`). |
| **Trello API Key** | Yes | From [trello.com/power-ups/admin](https://trello.com/power-ups/admin) → your Power-Up → API Key. |
| **Trello API Token** | Yes | Generated from the same Power-Up admin page. Click "Token" next to your API key. |
| **Trello Board ID** | Yes | The ID of the board this bot creates cards on. Found in the board URL: `trello.com/b/{BOARD_ID}/...` |
| **Trello List ID** | Yes | The ID of the list where new cards will land. Use the Trello API or browser dev tools to find it. |
| **Bot Context** | No | Describe this bot's role, personality, or domain. Appended after Global Context in every Anthropic call. Example: *"This bot manages engineering bugs. Be concise and technical."* |
| **Allowed Users** | No | Comma-separated Mattermost usernames. When set, only these users can invoke the bot — everyone else gets a polite refusal. Example: `alice, bob, ceo`. Leading `@` is optional. |

After saving, the plugin registers each bot as a separate Mattermost user account. Check **System Console → Plugins → Mattermost Trello Bot → Bot Configurations** and look for any error messages.

### Adding bots to channels

Each bot only responds in channels where it has been **explicitly added as a member**. This is the primary security boundary — a bot configured for Team A's board cannot be invoked from Team B's channels.

Before a bot can join any channel it must first be a member of the team. This is a required two-step process:

**Step 1 — Add the bot to the team**

1. Go to the team where you want the bot to operate.
2. Open **Team Settings** (click the team name in the top-left) → **Members** → **Invite People**.
3. Search for the bot's username (e.g. `trellobot`) and add it as a team member.

Repeat this for every team the bot needs to serve.

**Step 2 — Add the bot to each channel**

1. Open the channel in Mattermost.
2. Click the channel name at the top → **Add Members**.
3. Search for the bot's username and add it.

> **Why this matters**: Without channel membership, the bot ignores all mentions, even if the username is typed correctly. This prevents users in one channel from using a bot that is scoped to a different team's Trello board.

### Multi-bot setup

You can configure as many bots as you need. Common patterns:

- **One bot per team** — Engineering bot linked to the Engineering board, Marketing bot linked to the Marketing board.
- **One bot per project** — Fine-grained control over which list new cards land in.
- **Restricted bot** — A bot used only by management, configured with `allowedUsers: ["ceo", "cto"]`.

Each bot is a fully independent Mattermost user account with its own Trello credentials. There is no cross-contamination between bots.

### Releasing a new version

Push a tag to trigger a versioned GitHub release:

```bash
git tag v1.2.0
git push origin v1.2.0
```

The CI workflow builds all server binaries and the webapp bundle, then attaches the `.tar.gz` to the release automatically. Every push to `master` also updates a rolling **Latest build** pre-release.

---

## End User Guide

### Creating a Trello card

Mention the bot in any channel where it is a member. Write your task description naturally — no special formatting required.

```
@trellobot Set up the new CI pipeline for the mobile app.
           We need GitHub Actions, Docker build, and automated test runs.
           Assign to @sarah.
```

The bot will:
- Generate a Trello card with a structured title, description, and task checklist (powered by Claude)
- Add the card to the configured Trello list
- Add `@sarah` as a Trello member on the card (if her Trello username matches her Mattermost username)
- Reply in the thread with the card link and the full task checklist

> **Tip:** The bot only responds to mentions in channels where it has been added as a member. If it doesn't reply, ask your admin to add it to the channel.

### Tagging team members as owners

Include `@username` mentions in your message when creating a card. The bot resolves each Mattermost username as a Trello username and adds them as card members.

```
@trellobot Redesign the onboarding flow. Assign to @alex and @priya.
```

> This assumes Trello usernames match Mattermost usernames. If a user's Trello username is different, the assignment will silently fail and the bot will log a warning.

### Updating a card

Once a card is linked to a thread, simply tag the bot with what changed — no `/update` required. Claude reads the current card state and your message, and rewrites the title, description, and checklist to reflect the new information.

```
@trellobot The deadline moved to end of month and we've added a new
           requirement for dark mode support.
```

Or use the explicit command:

```
@trellobot /update deadline moved to Friday, also add a step for load testing
```

### Adding a comment to a card

Use `/comment` to add a note to the Trello card without rewriting it:

```
@trellobot /comment Blocked on design review until Thursday.
```

The comment appears on the Trello card attributed to your Mattermost username.

### Marking tasks as done

```
@trellobot /done set up Docker and GitHub Actions
```

Claude identifies which checklist items match your description and marks them complete on the Trello card. You can mark multiple items at once.

### Checking progress

```
@trellobot /progress
```

The bot fetches the current checklist from Trello and posts a formatted summary showing which tasks are complete and which are still open.

### Generating a Linear issue

```
@trellobot /linear
```

Claude synthesises the Trello card content and the full Mattermost thread into a ready-to-paste Linear issue body (with title, description, acceptance criteria, and implementation notes). Copy it directly into Linear.

`/linear` also works **without a linked Trello card** — useful for converting a discussion thread into a Linear issue before a card has been created:

```
@trellobot /linear We discussed a new rate-limiting approach in this thread — turn it into a Linear issue
```

### Command reference

All commands are used by mentioning the bot in a thread that has a linked Trello card (except `/linear`, which works anywhere).

| Command | Description |
|---|---|
| *(no command)* — **new thread** | Creates a Trello card from your message |
| *(no command)* — **existing thread** | Updates the linked card (title, description, checklist) |
| `/update [instructions]` | Explicitly rewrite the card using the thread context and your instructions |
| `/comment <text>` | Add a comment to the Trello card without modifying the card itself |
| `/done <description>` | Mark matching checklist items as complete |
| `/progress` | Show current checklist progress |
| `/linear [notes]` | Generate a Linear issue body from the card and thread |
| `/freestyle` | Generate a rap about the card (for when things get too serious) |

### Tips

- **First message in a thread = card creation.** Every message after that = card update (or explicit `/comment`).
- **Checklist appears immediately** after card creation — no need to run `/progress` first.
- **`/linear` works anywhere**, even in threads with no Trello card. Just write a descriptive message.
- **Allowed users**: If your admin configured `allowedUsers` for a bot, only those users can invoke it. Others will receive a message saying the bot is restricted.
- **React feedback**: The bot adds 👀 while it is working and ✅ when done.

---

## Troubleshooting

### Bot doesn't respond

1. Confirm the bot has been **added as a channel member** (Admin: Channel Settings → Members).
2. Check that the bot username in the `@mention` exactly matches the configured `botUsername`.
3. Open **System Console → Plugins → Mattermost Trello Bot** and verify no error is shown next to the bot.
4. Call the status API to force re-registration: `GET /plugins/com.mattermost.mattermost-trello/api/v1/bots/status` (requires a logged-in session).

### "Unable to generate plugin webapp bundle"

This means the `.tar.gz` you uploaded was built without the webapp bundle — either from an old CI run or by building with `go build` directly. Make sure you:

- Download the `.tar.gz` from the [Releases](../../releases) page (not a "Source code" archive), **or**
- Build using `make dist` (not just `go build ./server`), which includes the webapp

### Card created but checklist is missing

The card was created successfully but the checklist step failed (non-fatal). This can happen if the Trello token doesn't have write permission on the board. Verify your token at [trello.com/power-ups/admin](https://trello.com/power-ups/admin).

### Member assignment silently skipped

`@username` assignments on card creation require the Mattermost username to match the user's Trello username exactly (case-insensitive). If they differ, the bot logs a warning and skips the assignment. The card is still created normally.

### "I'm only authorised to assist specific users"

The bot has been configured with `allowedUsers`. Contact your Mattermost admin to have your username added to the list.

### Only one bot account was created despite multiple configs

This is a known Mattermost server limitation with the old `EnsureBotUser` API. This plugin works around it using `CreateBot` directly. If you upgraded from an older version of this plugin and only one bot account exists, trigger re-registration via the status API endpoint above — it will create the missing bot accounts.
