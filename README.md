# Mattermost Langflow Plugin

Mattermost channels, threads, and DMs can trigger Langflow flows through dedicated Mattermost bots. Each configured bot maps to exactly one Langflow flow.

## MVP scope

- Langflow single-endpoint integration
- Agents-style System Console configuration with one custom `Config` screen for service settings, bot catalog, runtime policy, and status
- Multiple Mattermost bot accounts, each bound to one Langflow flow
- Bot mention and DM trigger through `@bot-username`
- Streaming bot replies by updating a single Mattermost post as Langflow tokens arrive
- Right-hand sidebar runner that lets users choose a configured bot
- Recent conversation context injection
- Threaded response posting through the selected bot account
- Per-user recent execution history
- Admin connection test and bot catalog preview

## Project structure

- `server/`: Go plugin server that handles Mattermost hooks, Langflow API requests, bot lifecycle, and plugin REST endpoints
- `webapp/`: Mattermost webapp bundle for the RHS runner and admin custom settings
- `plugin.json`: Mattermost manifest and System Console schema

## Config format

The System Console now stores one `Config` JSON document. The admin UI edits this automatically, but the persisted shape looks like this:

```json
{
  "service": {
    "base_url": "https://langflow.example.com",
    "auth_mode": "bearer",
    "auth_token": "YOUR_API_KEY",
    "allow_hosts": "langflow.example.com"
  },
  "runtime": {
    "default_timeout_seconds": 30,
    "enable_streaming": true,
    "streaming_update_ms": 350,
    "max_input_length": 4000,
    "max_output_length": 8000,
    "context_post_limit": 8,
    "enable_debug_logs": false,
    "enable_usage_logs": true
  },
  "bots": [
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
}
```

Each bot definition creates or updates one Mattermost bot account and binds it to one Langflow flow. The admin UI derives the internal bot identifier from `username`, so admins no longer need to enter a separate bot ID manually. Saving the System Console configuration applies those bot definitions immediately, so the plugin creates, updates, or deactivates its managed Mattermost bot accounts to match the catalog.

## Langflow request shape

Each bot calls the Langflow run API in this form when streaming replies are enabled:

```bash
curl -X POST "http://your-langflow-instance-url/api/v1/run/$FLOW_ID?stream=true" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "input_value": "Your input text here",
    "session_id": "mattermost:bot-id:thread-or-channel:user-id"
  }'
```

The plugin builds `input_value` from the user prompt, optional structured inputs, and optional recent Mattermost context. The `session_id` is derived from the Mattermost thread or channel so follow-up messages can stay correlated on the Langflow side.

## Bot usage

- Mention the bot in a channel or thread: `@thread-summary-bot Summarize this thread`
- Send the bot a direct message: `What are the action items?`
- Use the RHS panel to pick a configured bot and send a prompt without typing the mention manually
- When streaming is enabled, the bot creates one reply and that same reply is updated progressively until the Langflow run completes

## Notes

- Team, channel, and user access filters are supported per bot. Group-level policy is still a follow-up item.
- The plugin no longer relies on a slash command. Bot mention, DM, and RHS execution are the primary entry points.
- Mattermost may still log `no signature when persisting plugin to filestore` for custom uploads. Per Mattermost's plugin signing docs, that warning is expected until the release bundle is signed and the server trusts the signing key.
