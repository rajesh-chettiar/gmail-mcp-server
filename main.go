package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/joho/godotenv"
	"github.com/ledongthuc/pdf"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nguyenthenguyen/docx"
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
	token   *oauth2.Token
}

var (
	gmailServer     *GmailServer
	gmailAuthReady  bool
	oauthConfig     *oauth2.Config
	tokenFile       = getAppFilePath("token.json")
	styleGuideFile  = getAppFilePath("personal-email-style-guide.md")
)

func getAppDataDir() string {
	var appDataDir string
	if runtime.GOOS == "windows" {
		appDataDir = filepath.Join(os.Getenv("APPDATA"), "auto-gmail")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "."
		}
		appDataDir = filepath.Join(homeDir, ".auto-gmail")
	}
	os.MkdirAll(appDataDir, 0755)
	return appDataDir
}

func getAppFilePath(filename string) string {
	return filepath.Join(getAppDataDir(), filename)
}

func saveToken(path string, token *oauth2.Token) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Printf("Unable to cache oauth token: %v", err)
		return
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

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

func NewOAuthConfig() *oauth2.Config {
	clientID := os.Getenv("GMAIL_CLIENT_ID")
	clientSecret := os.Getenv("GMAIL_CLIENT_SECRET")
	redirectURL := os.Getenv("REDIRECT_URL")
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{gmail.GmailReadonlyScope, gmail.GmailComposeScope},
		Endpoint:     google.Endpoint,
	}
}

func NewGmailServer(token *oauth2.Token) (*GmailServer, error) {
	ctx := context.Background()
	client := oauthConfig.Client(ctx, token)
	service, err := gmail.NewService(ctx, googleOption.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Gmail service: %v", err)
	}
	return &GmailServer{
		service: service,
		userID:  "me",
		token:   token,
	}, nil
}

func isTokenValid(token *oauth2.Token) bool {
	client := oauthConfig.Client(context.Background(), token)
	service, err := gmail.NewService(context.Background(), googleOption.WithHTTPClient(client))
	if err != nil {
		return false
	}
	_, err = service.Users.GetProfile("me").Do()
	return err == nil
}

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	authURL := oauthConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	html := fmt.Sprintf(`
		<html>
		<head><title>Authorize Gmail MCP Server</title></head>
		<body>
		<h1>Authorize Gmail MCP Server</h1>
		<p><a href="%s">Click here to authorize with Google</a></p>
		</body>
		</html>
	`, authURL)
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Authorization code not found", http.StatusBadRequest)
		return
	}
	token, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
		return
	}
	saveToken(tokenFile, token)
	server, err := NewGmailServer(token)
	if err != nil {
		http.Error(w, "Failed to create Gmail server: "+err.Error(), http.StatusInternalServerError)
		return
	}
	gmailServer = server
	gmailAuthReady = true
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<h1>✅ Gmail Authorization successful.</h1><p>You may close this window and use the API.</p>`))
}

// ---- Email/Attachment/Style Guide Utility Functions ----
// (All your extractEmailBody, extractFromParts, decodeEmailContent, etc. Place all those here, unchanged.)
// (You can copy these from your previous code.)

// Example: extractEmailBody, extractFromParts, decodeEmailContent, extractTextAndLinksFromHTML, etc.

// ---- MCP Tool Implementations ----
// (Copy your MCP tool implementations here, but ensure they use gmailServer global and check gmailAuthReady before calling Gmail APIs.)

// ExtractAttachmentByFilename safely extracts text content from an email attachment by filename
// This is more reliable than using attachment IDs which are unstable in Gmail API
func (g *GmailServer) ExtractAttachmentByFilename(ctx context.Context, messageID, filename string) (*mcp.CallToolResult, error) {
	// Get the message to find attachments
	message, err := g.service.Users.Messages.Get(g.userID, messageID).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get message: %v", err)), nil
	}
	
	// Find all attachments in the message
	allAttachments := extractAttachmentInfo(message)
	
	// Look for the attachment with matching filename
	var targetAttachment map[string]interface{}
	var attachmentPart *gmail.MessagePart
	
	for _, attachment := range allAttachments {
		if attachment["filename"] == filename {
			targetAttachment = attachment
			attachmentID := attachment["attachmentId"].(string)
			findAttachmentPart(message.Payload.Parts, attachmentID, &attachmentPart)
			break
		}
	}
	
	if targetAttachment == nil {
		availableFiles := make([]string, 0, len(allAttachments))
		for _, att := range allAttachments {
			availableFiles = append(availableFiles, att["filename"].(string))
		}
		return mcp.NewToolResultError(fmt.Sprintf("Attachment with filename '%s' not found. Available files: %v", filename, availableFiles)), nil
	}
	
	if attachmentPart == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Could not find attachment part for filename '%s'", filename)), nil
	}
	
	// Get the attachment data using the current attachment ID
	attachmentID := targetAttachment["attachmentId"].(string)
	attachment, err := g.service.Users.Messages.Attachments.Get(g.userID, messageID, attachmentID).Do()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get attachment data: %v", err)), nil
	}
	
	// Decode the attachment data
	data, err := base64.URLEncoding.DecodeString(attachment.Data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode attachment data: %v", err)), nil
	}
	
	// Extract text based on MIME type
	text, err := extractTextFromBytes(data, attachmentPart.MimeType, attachmentPart.Filename)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to extract text: %v", err)), nil
	}
	
	result := map[string]interface{}{
		"messageId":    messageID,
		"filename":     filename,
		"attachmentId": attachmentID,
		"mimeType":     attachmentPart.MimeType,
		"textContent":  text,
		"extractedAt":  time.Now().Format(time.RFC3339),
	}
	
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(resultJSON)), nil
}

// FetchEmailBodies fetches full email content for multiple threads
func (g *GmailServer) FetchEmailBodies(ctx context.Context, threadIDs []string) (*mcp.CallToolResult, error) {
	var results []map[string]interface{}
	
	for _, threadID := range threadIDs {
		// Get thread details directly from Gmail API
		threadDetail, err := g.service.Users.Threads.Get(g.userID, threadID).Do()
		if err != nil {
			log.Printf("Warning: Failed to get thread %s: %v", threadID, err)
			continue
		}

		if len(threadDetail.Messages) == 0 {
			continue
		}

		// Extract details from the first message
		firstMessage := threadDetail.Messages[0]
		var subject, from string

		// Extract headers
		for _, header := range firstMessage.Payload.Headers {
			switch header.Name {
			case "Subject":
				subject = header.Value
			case "From":
				from = header.Value
			}
		}

		// Extract full email body content with markdown formatting
		fullBody := extractEmailBody(firstMessage)
		
		// Limit full body to prevent overwhelming the context (8000 chars = ~2000 tokens)
		if len(fullBody) > 8000 {
			fullBody = fullBody[:8000] + "\n\n[Content truncated - email is longer than 8000 characters]"
		}

		// Collect attachment information from all messages in the thread
		var allAttachments []map[string]interface{}
		for _, message := range threadDetail.Messages {
			attachments := extractAttachmentInfo(message)
			for _, attachment := range attachments {
				// Add message ID to each attachment for reference
				attachment["messageId"] = message.Id
				allAttachments = append(allAttachments, attachment)
			}
		}

		// Get existing drafts for this thread
		existingDrafts, err := g.getThreadDrafts(threadID)
		if err != nil {
			log.Printf("Warning: Failed to get drafts for thread %s: %v", threadID, err)
			existingDrafts = []map[string]interface{}{}
		}

		threadResult := map[string]interface{}{
			"threadId":     threadID,
			"subject":      subject,
			"from":         from,
			"fullBody":     fullBody,
			"messageCount": len(threadDetail.Messages),
		}

		// Only include attachments if there are any
		if len(allAttachments) > 0 {
			threadResult["attachments"] = allAttachments
		}

		// Only include drafts if there are any
		if len(existingDrafts) > 0 {
			threadResult["drafts"] = existingDrafts
		}

		results = append(results, threadResult)
	}
	
	resultJSON, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal results: %v", err)), nil
	}
	
	return mcp.NewToolResultText(string(resultJSON)), nil
}

func main() {
	_ = godotenv.Load()
	log.Printf("📁 App data directory: %s", getAppDataDir())
	log.Printf("🔑 Token file: %s", tokenFile)
	log.Printf("📝 Style guide file: %s", styleGuideFile)

	oauthConfig = NewOAuthConfig()
	if oauthConfig.ClientID == "" || oauthConfig.ClientSecret == "" || oauthConfig.RedirectURL == "" {
		log.Fatal("Missing GMAIL_CLIENT_ID, GMAIL_CLIENT_SECRET or REDIRECT_URL env vars")
	}

	// Try loading token at startup (if present)
	if token, err := tokenFromFile(tokenFile); err == nil && isTokenValid(token) {
		gmailServer, _ = NewGmailServer(token)
		gmailAuthReady = true
		log.Println("✅ Gmail token loaded and valid.")
	} else {
		log.Println("🔑 Gmail token missing/invalid. Visit /authorize to start OAuth.")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// Health and status endpoints
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]interface{}{
			"status": "healthy",
			"gmail_authenticated": gmailAuthReady,
			"server": "Gmail MCP Server",
			"timestamp": time.Now().Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		tokenExists := "❌ Not found"
		if _, err := os.Stat(tokenFile); err == nil {
			tokenExists = "✅ Found"
		}
		toneExists := "❌ Not found"
		if _, err := os.Stat(styleGuideFile); err == nil {
			toneExists = "✅ Found"
		}
		statusMessage := fmt.Sprintf("📁 App Data Dir: %s\n🔑 Token: %s (%s)\n📝 Style Guide: %s (%s)\n",
			getAppDataDir(), tokenFile, tokenExists, styleGuideFile, toneExists)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(statusMessage))
	})

	// OAuth endpoints
	mux.HandleFunc("/authorize", handleAuthorize)
	mux.HandleFunc("/oauth2callback", handleOAuth2Callback)

	// Root endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body>
		<h1>Gmail MCP Server</h1>
		<p>Status: %v</p>
		<p><a href="/authorize">[Authorize]</a></p>
		<p><a href="/health">[Health]</a></p>
		<p><a href="/status">[Status]</a></p>
		</body></html>`, gmailAuthReady)
	})

	// MCP endpoint (only after auth)
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if !gmailAuthReady {
			http.Error(w, "Gmail not authorized. Visit /authorize.", http.StatusForbidden)
			return
		}
		// MCP server features here...
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"result": map[string]interface{}{
				"message": "MCP endpoint placeholder.",
			},
		})
	})

	log.Printf("🌐 Server starting on :%s ... Visit /authorize to connect Gmail.", port)
	httpServer := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}
	log.Fatal(httpServer.ListenAndServe())
}