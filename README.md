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
- Email Style Guide: `personal-email-style-guide.md`

**Mac/Linux:** `~/.auto-gmail/`
- OAuth Token: `token.json`
- Email Style Guide: `personal-email-style-guide.md`

Use `/server-status` in your MCP client to see exact file paths anytime.

## 5. Available Commands

**Tools:**
- `search_threads` - Search Gmail with queries like "from:email@example.com" or "subject:meeting"
- `create_draft` - Create email drafts (AI will request style guide first)
- `get_personal_email_style_guide` - Get your email writing style guide (this is a temporary tool, created because most agents do not yet support fetching resources--once agents implement MCP resources better, then thsi tool can be removed)

**Resources:**
- `file://personal-email-style-guide` - Your personal email writing style (auto-generated or manual)

**Prompts:**
- `/generate-email-tone` - Analyze your sent emails to create personalized writing style
- `/server-status` - Show file locations and server status

## 6. Personal Email Style Guide

The server learns your writing style to help AI write emails that sound like you. It includes your background, specific language patterns, and unique characteristics.

**Automatic Generation (Recommended):**
1. Set `OPENAI_API_KEY` in your MCP config or `.env` file
2. **NEW:** The server auto-generates your style guide on first startup if it doesn't exist
3. It analyzes up to 25 of your most recent sent emails
4. Creates a focused style guide with 5 key sections:
   - Your background and professional context (inferred from emails)
   - Specific words/phrases you actually use
   - How you structure emails (greeting→body→closing)
   - Your actual communication tone with examples
   - Unique characteristics that make emails sound like you

**Manual Generation:**
- Run `/generate-email-tone` prompt in your MCP client anytime to regenerate
- The file is saved to your app data directory (see **File Storage Locations** above)

**Manual Creation/Editing:**
1. Use `/server-status` to find your style guide location
2. Create/edit the file with your preferences:

```markdown
# Personal Email Style Guide for Your Name <your@email.com>

## 1. USER BACKGROUND
- Your role, industry, expertise based on email patterns

## 2. WRITING PATTERNS  
- Specific words/phrases you use frequently
- Common expressions and verbal tics

## 3. STRUCTURE
- How you organize emails (greeting patterns, body structure, closings)

## 4. TONE
- Your communication style with concrete examples

## 5. SIGNATURE ELEMENTS
- Unique characteristics that make emails distinctly yours
```

**AI Integration:**
- AI always calls `get_personal_email_style_guide` tool before writing emails
- Ensures consistent personal style across all communications
- Resource also available at `file://personal-email-style-guide` for supporting clients

## First Run

On first run, the server opens a browser for Gmail OAuth authentication. Your token is saved to the app data directory. Make sure port 8080 is available during setup.
