# Gmail MCP Server

An MCP server that lets AI agents search Gmail threads, understand your email writing style, and create draft emails.

## 1. Get Google Authentication

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a project and enable the Gmail API
3. Create OAuth2 credentials:
   - "APIs & Services" → "Credentials" → "Create Credentials" → "OAuth Client ID"
   - Choose "Desktop application"
   - Download the credentials JSON file

## 2. Set Up Environment Variables

Extract `client_id` and `client_secret` from your downloaded JSON and format them like this:

**For MCP Client Configs (Cursor/Claude Desktop):**
```json
"GMAIL_CREDENTIALS": "{\"installed\":{\"client_id\":\"YOUR_CLIENT_ID\",\"client_secret\":\"YOUR_CLIENT_SECRET\",\"redirect_uris\":[\"http://localhost:8080\"]}}"
```

**For Manual Testing:**
```bash
# .env file or environment variable
GMAIL_CREDENTIALS={"installed":{"client_id":"YOUR_CLIENT_ID","client_secret":"YOUR_CLIENT_SECRET","redirect_uris":["http://localhost:8080"]}}
OPENAI_API_KEY=your_openai_api_key_here
```

## 3. Add to MCP Clients

Build the server first: `go build`

Edit your MCP client config file:
- **Cursor:** `C:\Users\[User]\.cursor\mcp.json`
- **Claude Desktop:** `%APPDATA%\Claude\claude_desktop_config.json`

Add this configuration:
```json
{
  "mcpServers": {
    "gmail": {
      "command": "C:/path/to/your/auto-gmail.exe",
      "env": {
        "GMAIL_CREDENTIALS": "{\"installed\":{\"client_id\":\"YOUR_CLIENT_ID\",\"client_secret\":\"YOUR_CLIENT_SECRET\",\"redirect_uris\":[\"http://localhost:8080\"]}}",
        "OPENAI_API_KEY": "your_openai_api_key_here"
      }
    }
  }
}
```

Replace:
- `C:/path/to/your/auto-gmail.exe` with your actual executable path
- `YOUR_CLIENT_ID` and `YOUR_CLIENT_SECRET` with values from step 1
- `your_openai_api_key_here` with your OpenAI API key (optional)

## 4. File Storage Locations

The server stores files in standard application data directories:

**Windows:** `C:\Users\[YourUsername]\AppData\Roaming\auto-gmail\`
- OAuth Token: `token.json`
- Email Tone: `tone_personalization.md`

**Mac/Linux:** `~/.auto-gmail/`
- OAuth Token: `token.json`
- Email Tone: `tone_personalization.md`

Use `/server-status` in your MCP client to see exact file paths anytime.

## 5. Available Commands

**Tools:**
- `search_threads` - Search Gmail with queries like "from:email@example.com" or "subject:meeting"
- `create_draft` - Create email drafts (AI will request tone resource first)

**Resources:**
- `tone://email-style` - Your personal email writing style (auto-generated or manual)

**Prompts:**
- `/generate-email-tone` - Analyze your sent emails to create personalized writing style
- `/server-status` - Show file locations and server status

## 6. Email Tone Personalization

The server learns your writing style to help AI write emails that sound like you.

**Automatic (Recommended):**
1. Set `OPENAI_API_KEY` in your MCP config
2. When AI first requests `tone://email-style`, it auto-generates from your sent emails
3. Or manually run `/generate-email-tone` in your MCP client

**Manual Creation/Editing:**
1. Use `/server-status` to find your tone file location (see **File Storage Locations** above)
2. Create/edit the file with your preferences:

```markdown
# Email Writing Style Guide

## Greeting Style
- Use "Hi [Name]," for most emails
- Use "Hello [Name]," for formal emails

## Tone and Voice  
- Keep emails concise and direct
- Use friendly but professional tone

## Email Structure
- Start with context or purpose
- Use bullet points for lists
- End with clear next steps

## Closing Style
- Use "Best regards," for formal
- Use "Thanks," for quick responses
```

The AI always requests this resource before writing emails, ensuring consistent personal style.

## First Run

On first run, the server opens a browser for Gmail OAuth authentication. Your token is saved to the app data directory. Make sure port 8080 is available during setup.
