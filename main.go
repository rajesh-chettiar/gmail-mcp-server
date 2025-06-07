package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	googleOption "google.golang.org/api/option"
)

type GmailServer struct {
	service *gmail.Service
	userID  string
}

func NewGmailServer() (*GmailServer, error) {
	ctx := context.Background()

	// Get credentials from environment variable
	credentialsJSON := os.Getenv("GMAIL_CREDENTIALS")
	if credentialsJSON == "" {
		return nil, fmt.Errorf("GMAIL_CREDENTIALS environment variable not set")
	}

	// Parse credentials
	config, err := google.ConfigFromJSON([]byte(credentialsJSON), gmail.GmailReadonlyScope, gmail.GmailComposeScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse client config: %v", err)
	}

	// Get token from file or perform OAuth flow
	token, err := getToken(config)
	if err != nil {
		return nil, fmt.Errorf("unable to get token: %v", err)
	}

	// Create Gmail service
	client := config.Client(ctx, token)
	service, err := gmail.NewService(ctx, googleOption.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Gmail service: %v", err)
	}

	return &GmailServer{
		service: service,
		userID:  "me",
	}, nil
}

// getToken retrieves a token from a local file or initiates OAuth flow
func getToken(config *oauth2.Config) (*oauth2.Token, error) {
	tokenFile := getAppFilePath("token.json")
	
	// Try to load existing token
	token, err := tokenFromFile(tokenFile)
	if err != nil {
		log.Printf("No valid token file found (%v), starting OAuth flow...", err)
		return performOAuthFlow(config, tokenFile)
	}

	// Validate the token by testing it with a simple Gmail API call
	log.Println("Validating existing token...")
	if !isTokenValid(token) {
		log.Println("Existing token is invalid or expired, starting OAuth flow...")
		return performOAuthFlow(config, tokenFile)
	}

	log.Println("✅ Using existing valid token")
	return token, nil
}

// isTokenValid tests if a token is valid by making a simple API call
func isTokenValid(token *oauth2.Token) bool {
	// Create a temporary client to test the token
	config := &oauth2.Config{
		ClientID:     "",
		ClientSecret: "",
		Endpoint:     google.Endpoint,
		Scopes:       []string{gmail.GmailReadonlyScope},
	}
	
	client := config.Client(context.Background(), token)
	service, err := gmail.NewService(context.Background(), googleOption.WithHTTPClient(client))
	if err != nil {
		return false
	}

	// Try a simple API call to verify the token works
	_, err = service.Users.GetProfile("me").Do()
	return err == nil
}

// performOAuthFlow handles the OAuth flow and saves the token
func performOAuthFlow(config *oauth2.Config, tokenFile string) (*oauth2.Token, error) {
	token, err := getTokenFromWeb(config)
	if err != nil {
		return nil, err
	}
	
	// Save token for next time
	saveToken(tokenFile, token)
	return token, nil
}

// getTokenFromWeb requests a token from the web, then returns the retrieved token
func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	// Create a channel to receive the authorization code
	codeChan := make(chan string)
	errChan := make(chan error)

	// Start a temporary HTTP server to catch the OAuth callback
	server := &http.Server{Addr: ":8080"}
	
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errChan <- fmt.Errorf("no code in callback")
			return
		}

		// Send success page to user
		fmt.Fprint(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Gmail MCP Server - Authorization Complete</title>
    <style>
        body { font-family: Arial, sans-serif; text-align: center; margin-top: 50px; }
        .success { color: green; font-size: 18px; }
    </style>
</head>
<body>
    <h1>Authorization Successful!</h1>
    <p class="success">✅ You can now close this browser window and return to your terminal.</p>
    <p>Your Gmail MCP Server is now configured.</p>
</body>
</html>`)
		
		// Send the code back to the main flow
		codeChan <- code
	})

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("failed to start callback server: %v", err)
		}
	}()

	// Wait a moment for server to start
	time.Sleep(100 * time.Millisecond)

	// Update the redirect URI to point to our local server
	config.RedirectURL = "http://localhost:8080"
	
	// Generate the authorization URL
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	
	fmt.Println("Opening browser for authorization...")
	fmt.Printf("If browser doesn't open automatically, go to: %v\n", authURL)
	
	// Try to open browser automatically
	openBrowser(authURL)

	// Wait for either the code or an error
	var authCode string
	select {
	case authCode = <-codeChan:
		// Success! We got the code
	case err := <-errChan:
		return nil, fmt.Errorf("authorization failed: %v", err)
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authorization timed out after 5 minutes")
	}

	// Shutdown the temporary server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)

	// Exchange the code for a token
	token, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve token from web: %v", err)
	}
	
	fmt.Println("✅ Authorization successful! Token saved.")
	return token, nil
}

// openBrowser tries to open the URL in the default browser
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	
	if err != nil {
		fmt.Printf("Could not open browser automatically: %v\n", err)
	}
}

// tokenFromFile retrieves a token from a local file
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	
	token := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(token)
	return token, err
}

// saveToken saves a token to a file path
func saveToken(path string, token *oauth2.Token) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Printf("Unable to cache oauth token: %v", err)
		return
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// SearchThreads searches Gmail threads based on a query
func (g *GmailServer) SearchThreads(ctx context.Context, query string, maxResults int64) (*mcp.CallToolResult, error) {
	if maxResults <= 0 {
		maxResults = 10
	}

	threads, err := g.service.Users.Threads.List(g.userID).Q(query).MaxResults(maxResults).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to search threads: %v", err)), nil
	}

	var results []map[string]interface{}
	for _, thread := range threads.Threads {
		// Get thread details
		threadDetail, err := g.service.Users.Threads.Get(g.userID, thread.Id).Do()
		if err != nil {
			continue
		}

		if len(threadDetail.Messages) == 0 {
			continue
		}

		firstMessage := threadDetail.Messages[0]
		var subject, from, snippet string

		// Extract headers
		for _, header := range firstMessage.Payload.Headers {
			switch header.Name {
			case "Subject":
				subject = header.Value
			case "From":
				from = header.Value
			}
		}

		snippet = firstMessage.Snippet

		results = append(results, map[string]interface{}{
			"threadId":     thread.Id,
			"subject":      subject,
			"from":         from,
			"snippet":      snippet,
			"messageCount": len(threadDetail.Messages),
		})
	}

	resultJSON, _ := json.MarshalIndent(results, "", "  ")
	return mcp.NewToolResultText(string(resultJSON)), nil
}

// CreateDraft creates a Gmail draft
func (g *GmailServer) CreateDraft(ctx context.Context, to, subject, body string, threadID string) (*mcp.CallToolResult, error) {
	var message gmail.Message
	
	// Build the email message
	headers := fmt.Sprintf("To: %s\r\nSubject: %s\r\n", to, subject)
	if threadID != "" {
		// For replies, we need to set the In-Reply-To and References headers
		thread, err := g.service.Users.Threads.Get(g.userID, threadID).Do()
		if err == nil && len(thread.Messages) > 0 {
			lastMessage := thread.Messages[len(thread.Messages)-1]
			for _, header := range lastMessage.Payload.Headers {
				if header.Name == "Message-ID" {
					headers += fmt.Sprintf("In-Reply-To: %s\r\nReferences: %s\r\n", header.Value, header.Value)
					break
				}
			}
		}
	}
	
	rawMessage := headers + "\r\n" + body
	// Gmail API requires base64url-encoded raw message
	message.Raw = base64.URLEncoding.EncodeToString([]byte(rawMessage))

	draft := &gmail.Draft{
		Message: &message,
	}

	createdDraft, err := g.service.Users.Drafts.Create(g.userID, draft).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create draft: %v", err)), nil
	}

	result := map[string]interface{}{
		"draftId": createdDraft.Id,
		"message": "Draft created successfully",
		"to":      to,
		"subject": subject,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(resultJSON)), nil
}

// GenerateTonePersonalization analyzes sent emails and generates a tone personalization file
func GenerateTonePersonalization(gmailServer *GmailServer) error {
	log.Println("Generating tone personalization from sent emails...")

	// Get OpenAI API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	// Create OpenAI client
	client := openai.NewClient(option.WithAPIKey(apiKey))

	// Get sent emails
	log.Println("Fetching sent emails...")
	messages, err := gmailServer.service.Users.Messages.List(gmailServer.userID).Q("in:sent").MaxResults(50).Do()
	if err != nil {
		return fmt.Errorf("failed to fetch sent messages: %v", err)
	}

	var emailBodies []string
	for _, msg := range messages.Messages {
		// Get full message
		fullMsg, err := gmailServer.service.Users.Messages.Get(gmailServer.userID, msg.Id).Do()
		if err != nil {
			continue
		}

		// Extract email body
		body := extractEmailBody(fullMsg)
		if body != "" && len(body) > 50 { // Only include substantial emails
			emailBodies = append(emailBodies, body)
		}

		// Limit to avoid hitting token limits
		if len(emailBodies) >= 20 {
			break
		}
	}

	if len(emailBodies) == 0 {
		return fmt.Errorf("no sent emails found to analyze")
	}

	log.Printf("Analyzing %d sent emails...", len(emailBodies))

	// Prepare prompt for OpenAI
	samplesText := strings.Join(emailBodies, "\n\n---\n\n")
	prompt := fmt.Sprintf(`Analyze the following email samples that were written by a specific person. Based on these emails, create a comprehensive email writing style guide that captures their unique voice, tone, and preferences.

Here are the email samples:

%s

Based on these samples, create a detailed markdown style guide that includes:

1. **Greeting Style** - Stock phrases they use to start emails
2. **Tone and Voice** - Their overall communication style (informal, friendly, concise, verbose...)
3. **Email Structure** - How they organize their thoughts (long paragraphs, short sentences, bullet points)
4. **Closing Style** - Stock phrases they use to end emails
5. **Misc. Preferences** - Specific language patterns, commonly used words or phrases

Write this as if you're instructing an AI to write emails in this person's style. Be specific and actionable. Start with "# Email Writing Style Guide" and write it as natural language instructions, not as a JSON or structured data.

Focus on capturing the authentic voice and personality that comes through in these emails.`, samplesText)

	// Call OpenAI API
	log.Println("Generating style guide with OpenAI...")
	completion, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String(prompt),
					},
				},
			},
		},
		Model: shared.ChatModelGPT4o,
	})
	if err != nil {
		return fmt.Errorf("failed to generate tone guide: %v", err)
	}

	// Get the generated content
	if len(completion.Choices) == 0 {
		return fmt.Errorf("no response from OpenAI")
	}

	toneGuide := completion.Choices[0].Message.Content

	// Save to file
	toneFilePath := getAppFilePath("tone_personalization.md")
	err = os.WriteFile(toneFilePath, []byte(toneGuide), 0644)
	if err != nil {
		return fmt.Errorf("failed to write tone personalization file: %v", err)
	}

	log.Printf("Successfully generated tone_personalization.md at: %s", toneFilePath)
	return nil
}

// extractEmailBody extracts readable text from a Gmail message
func extractEmailBody(msg *gmail.Message) string {
	if msg.Payload == nil {
		return ""
	}

	// Try to get text from plain text part
	if msg.Payload.Body != nil && msg.Payload.Body.Data != "" {
		// This is a simple message with body data
		return msg.Payload.Body.Data
	}

	// For multipart messages, find the text/plain part
	return extractFromParts(msg.Payload.Parts)
}

// extractFromParts recursively extracts text from message parts
func extractFromParts(parts []*gmail.MessagePart) string {
	for _, part := range parts {
		if part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "" {
			return part.Body.Data
		}
		// Recursively check nested parts
		if len(part.Parts) > 0 {
			if text := extractFromParts(part.Parts); text != "" {
				return text
			}
		}
	}
	return ""
}

// getAppDataDir returns the application data directory
func getAppDataDir() string {
	var appDataDir string
	
	if runtime.GOOS == "windows" {
		// Windows: %APPDATA%\auto-gmail
		appDataDir = filepath.Join(os.Getenv("APPDATA"), "auto-gmail")
	} else {
		// Mac/Linux: ~/.auto-gmail
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Printf("Warning: Could not get home directory: %v", err)
			return "."
		}
		appDataDir = filepath.Join(homeDir, ".auto-gmail")
	}
	
	// Ensure the directory exists
	if err := os.MkdirAll(appDataDir, 0755); err != nil {
		log.Printf("Warning: Could not create app data directory: %v", err)
		return "."
	}
	
	return appDataDir
}

// getAppFilePath returns an absolute path in the app data directory
func getAppFilePath(filename string) string {
	return filepath.Join(getAppDataDir(), filename)
}

func main() {
	// Parse command line flags (none needed now)
	flag.Parse()

	// Load environment variables from .env file if it exists
	err := godotenv.Load()
	if err == nil {
		log.Printf("Loaded .env file")
	}

	// Show file locations early
	log.Printf("📁 App data directory: %s", getAppDataDir())
	log.Printf("🔑 Token file: %s", getAppFilePath("token.json"))
	log.Printf("📝 Tone file: %s", getAppFilePath("tone_personalization.md"))

	// Create Gmail server instance
	gmailServer, err := NewGmailServer()
	if err != nil {
		log.Fatalf("Failed to create Gmail server: %v", err)
	}

	// Normal MCP server operation
	// Create MCP server
	mcpServer := server.NewMCPServer(
		"Gmail MCP Server",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
	)

	// Add email tone resource
	toneResource := mcp.NewResource(
		"tone://email-style",
		"Email Tone and Style Guide",
		mcp.WithResourceDescription("Instructions on how to write emails in the user's personal style and tone"),
		mcp.WithMIMEType("text/markdown"),
	)

	mcpServer.AddResource(toneResource, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		// Try to read from tone_personalization.md file in app data directory
		toneFilePath := getAppFilePath("tone_personalization.md")
		content, err := os.ReadFile(toneFilePath)
		if err != nil {
			// If file doesn't exist, try to generate it automatically
			if os.IsNotExist(err) {
				apiKey := os.Getenv("OPENAI_API_KEY")
				if apiKey != "" {
					log.Println("📝 Tone file not found, auto-generating from your sent emails...")
					if genErr := GenerateTonePersonalization(gmailServer); genErr != nil {
						return nil, fmt.Errorf("tone personalization file not found at %s and auto-generation failed: %v. Please create the file manually or set OPENAI_API_KEY", toneFilePath, genErr)
					}
					// Try reading again after generation
					content, err = os.ReadFile(toneFilePath)
					if err != nil {
						return nil, fmt.Errorf("failed to read generated tone file: %v", err)
					}
					log.Println("✅ Tone personalization auto-generated successfully!")
				} else {
					return nil, fmt.Errorf("tone personalization file not found at %s and OPENAI_API_KEY not set. Please either set OPENAI_API_KEY for auto-generation or create the file manually", toneFilePath)
				}
			} else {
				return nil, fmt.Errorf("failed to read tone file at %s: %v", toneFilePath, err)
			}
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "tone://email-style",
				MIMEType: "text/markdown", 
				Text:     string(content),
			},
		}, nil
	})

	// Add administrative prompts
	generateTonePrompt := mcp.NewPrompt(
		"generate-email-tone",
		mcp.WithPromptDescription("Generate email tone personalization by analyzing your sent emails"),
	)

	mcpServer.AddPrompt(generateTonePrompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		// Check if OPENAI_API_KEY is available
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return &mcp.GetPromptResult{
				Messages: []mcp.PromptMessage{
					mcp.NewPromptMessage(
						mcp.RoleUser,
						mcp.NewTextContent("❌ Cannot generate tone: OPENAI_API_KEY environment variable not set"),
					),
				},
			}, nil
		}

		// Generate tone personalization
		err := GenerateTonePersonalization(gmailServer)
		if err != nil {
			return &mcp.GetPromptResult{
				Messages: []mcp.PromptMessage{
					mcp.NewPromptMessage(
						mcp.RoleUser,
						mcp.NewTextContent(fmt.Sprintf("❌ Failed to generate tone: %v", err)),
					),
				},
			}, nil
		}

		toneFilePath := getAppFilePath("tone_personalization.md")
		return &mcp.GetPromptResult{
			Messages: []mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent(fmt.Sprintf("✅ Successfully generated tone personalization file at: %s\n\nYou can now use the tone://email-style resource for personalized email writing.", toneFilePath)),
				),
			},
		}, nil
	})

	statusPrompt := mcp.NewPrompt(
		"server-status",
		mcp.WithPromptDescription("Show Gmail MCP server status and file locations"),
	)

	mcpServer.AddPrompt(statusPrompt, func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		// Check file statuses
		tokenPath := getAppFilePath("token.json")
		tonePath := getAppFilePath("tone_personalization.md")
		
		tokenExists := "❌ Not found"
		if _, err := os.Stat(tokenPath); err == nil {
			tokenExists = "✅ Found"
		}
		
		toneExists := "❌ Not found"
		if _, err := os.Stat(tonePath); err == nil {
			toneExists = "✅ Found"
		}

		statusMessage := fmt.Sprintf("📊 **Gmail MCP Server Status**\n\n📁 **App Data Directory:** %s\n\n🔑 **Token File:** %s\n   Status: %s\n\n📝 **Tone File:** %s\n   Status: %s\n\n🛠️ **Available Commands:**\n- Use /generate-email-tone to create email tone personalization\n- Use tools: search_threads, create_draft\n- Use resource: tone://email-style", 
			getAppDataDir(), tokenPath, tokenExists, tonePath, toneExists)

		return &mcp.GetPromptResult{
			Messages: []mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent(statusMessage),
				),
			},
		}, nil
	})

	// Add Search Threads tool
	searchThreadsTool := mcp.NewTool("search_threads",
		mcp.WithDescription(`Search Gmail threads using Gmail's powerful query syntax.

GMAIL SEARCH OPERATORS:
Basic Filters:
  from:amy@example.com           - Find emails from specific sender
  to:me                          - Find emails sent to specific recipient  
  cc:john@example.com            - Find emails with specific CC
  subject:"quarterly review"     - Find emails with specific subject text
  
Date/Time Filters:
  after:2025/06/01               - Emails after specific date
  before:2025/06/07              - Emails before specific date  
  older_than:7d                  - Older than 7 days (use d/m/y)
  newer_than:2m                  - Newer than 2 months
  
Content & Attachments:
  has:attachment                 - Has any attachment
  filename:pdf                   - Has PDF attachment
  filename:report.txt            - Has specific filename
  has:youtube                    - Contains YouTube videos
  has:drive                      - Contains Google Drive files
  
Labels & Categories:
  label:important                - Has specific label
  category:promotions            - In specific category
  is:unread                      - Unread messages
  is:starred                     - Starred messages
  is:important                   - Marked important
  in:sent                        - In sent folder
  in:trash                       - In trash
  in:anywhere                    - Search everywhere including spam/trash
  
Advanced Operators:
  "exact phrase"                 - Search for exact phrase
  (dinner movie)                 - Group terms together
  holiday AROUND 10 vacation     - Words within 10 words of each other
  from:amy OR from:bob           - Either condition (use OR or { })
  from:amy AND to:david          - Both conditions
  dinner -movie                  - Include dinner, exclude movie
  +unicorn                       - Match word exactly
  
Size & Technical:
  larger:10M                     - Larger than 10MB
  smaller:1M                     - Smaller than 1MB
  rfc822msgid:<id@example.com>   - Specific message ID
  list:info@example.com          - From mailing list
  deliveredto:user@example.com   - Delivered to specific address

EXAMPLE QUERIES:
  "is:unread"                    - All unread emails
  "from:support@github.com"      - All emails from GitHub
  "subject:invoice older_than:30d" - Old invoices
  "has:attachment filename:pdf"  - PDF attachments
  "from:boss@company.com is:unread" - Unread emails from boss
  "(urgent OR important) newer_than:1d" - Recent urgent/important emails`),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Gmail search query using the operators above (e.g., 'from:example@gmail.com', 'subject:meeting', 'is:unread')"),
		),
		mcp.WithNumber("max_results",
			mcp.Description("Maximum number of threads to return (default: 10)"),
		),
	)

	mcpServer.AddTool(searchThreadsTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError("query parameter is required and must be a string"), nil
		}

		maxResults := int64(10)
		args := req.GetArguments()
		if mr, ok := args["max_results"].(float64); ok {
			maxResults = int64(mr)
		}

		return gmailServer.SearchThreads(ctx, query, maxResults)
	})

	// Add Create Draft tool
	createDraftTool := mcp.NewTool("create_draft",
		mcp.WithDescription("Create a Gmail draft email. Important: Before writing any email, always request the tone://email-style resource to understand the user's writing style and preferences."),
		mcp.WithString("to",
			mcp.Required(),
			mcp.Description("Recipient email address"),
		),
		mcp.WithString("subject",
			mcp.Required(),
			mcp.Description("Email subject line"),
		),
		mcp.WithString("body",
			mcp.Required(),
			mcp.Description("Email body content"),
		),
		mcp.WithString("thread_id",
			mcp.Description("Thread ID if this is a reply (optional)"),
		),
	)

	mcpServer.AddTool(createDraftTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		to, err := req.RequireString("to")
		if err != nil {
			return mcp.NewToolResultError("to parameter is required and must be a string"), nil
		}

		subject, err := req.RequireString("subject")
		if err != nil {
			return mcp.NewToolResultError("subject parameter is required and must be a string"), nil
		}

		body, err := req.RequireString("body")
		if err != nil {
			return mcp.NewToolResultError("body parameter is required and must be a string"), nil
		}

		threadID := ""
		args := req.GetArguments()
		if tid, ok := args["thread_id"].(string); ok {
			threadID = tid
		}

		return gmailServer.CreateDraft(ctx, to, subject, body, threadID)
	})

	// Start the server
	log.Println("Starting Gmail MCP Server...")
	log.Println("✅ Server ready! Waiting for MCP client connections via stdio...")
	log.Println("   (Use Ctrl+C to stop the server)")
	
	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
