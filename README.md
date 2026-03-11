# Mattermost Langflow Plugin

Mattermost channels, threads, and DMs can trigger Langflow flows through dedicated Mattermost bots. Each configured bot maps to exactly one Langflow flow.

## MVP scope

- Langflow single-endpoint integration
- System Console configuration for the Langflow server, auth token, allowlist, limits, and bot catalog
- Multiple Mattermost bot accounts, each bound to one Langflow flow
- Bot mention and DM trigger through `@bot-username`
- Right-hand sidebar runner that lets users choose a configured bot
- Recent conversation context injection
- Threaded response posting through the selected bot account
- Per-user recent execution history
- Admin connection test and bot catalog preview

## Project structure

- `server/`: Go plugin server that handles Mattermost hooks, Langflow API requests, bot lifecycle, and plugin REST endpoints
- `webapp/`: Mattermost webapp bundle for the RHS runner and admin custom settings
- `plugin.json`: Mattermost manifest and System Console schema

## Bot catalog format

`BotDefinitions` in the System Console expects a JSON array like this:

```json
[
  {
    "id": "thread-summary-bot",
    "username": "thread-summary-bot",
    "display_name": "Thread Summary Bot",
    "description": "Summarize the current conversation thread.",
    "flow_id": "thread-summary",
    "include_context_by_default": true,
    "allowed_teams": ["engineering"],
    "allowed_channels": ["town-square"],
    "allowed_users": ["sysadmin"],
    "input_schema": [
      {
        "name": "tone",
        "label": "Tone",
        "type": "text",
        "placeholder": "concise",
        "default_value": "concise"
      }
    ]
  }
]
```

Each bot definition creates or updates one Mattermost bot account and binds it to one Langflow flow. Additional input fields are appended to the prompt before the plugin sends the request to Langflow.

## Langflow request shape

Each bot calls the Langflow run API in this form:

```bash
curl -X POST "http://your-langflow-instance-url/api/v1/run/$FLOW_ID" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "input_value": "Your input text here"
  }'
```

The plugin builds `input_value` from the user prompt, optional structured inputs, and optional recent Mattermost context.

## Bot usage

- Mention the bot in a channel or thread: `@thread-summary-bot Summarize this thread`
- Send the bot a direct message: `What are the action items?`
- Use the RHS panel to pick a configured bot and send a prompt without typing the mention manually

## Notes

- Team, channel, and user access filters are supported per bot. Group-level policy is still a follow-up item.
- The plugin no longer relies on a slash command. Bot mention, DM, and RHS execution are the primary entry points.
