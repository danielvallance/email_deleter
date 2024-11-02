package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Stores access credentials for the Google Cloud project
type Credentials struct {
	Web struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	} `json:"web"`
}

// Global variables for OAuth callback server
var (
	authCode string
	authErr  error
	wg       sync.WaitGroup
)

func main() {

	// Read credentials file
	data, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read credentials file: %v\n", err)
	}

	// Store access credentials for Google Cloud project in struct
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		log.Fatalf("Unable to parse credentials: %v\n", err)
	}

	// This struct contains the OAuth settings which
	// will be used to get an authenticated client
	config := &oauth2.Config{
		ClientID:     creds.Web.ClientID,
		ClientSecret: creds.Web.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
		Scopes: []string{
			gmail.GmailModifyScope,
			gmail.GmailReadonlyScope,
		},
		RedirectURL: "http://localhost:8080/callback", // Must register as authorised redirect URI in Google Cloud project
	}

	// Get an authenticated client
	client, err := getClient(config)
	if err != nil {
		log.Fatalf("Could not get authenticated client: %v\n", err)
	}

	// Create a new Gmail service using the authenticated client
	srv, err := gmail.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to create Gmail service: %v\n", err)
	}

	// Get sender statistics
	senderStats, err := getSenderStats(srv)
	if err != nil {
		log.Fatalf("Unable to get sender statistics: %v\n", err)
	}

	// Process emails, get top senders and prompt user for which ones they would like to delete
	processEmails(srv, senderStats)
}

// Create HTTP server to handle the OAuth callback (authentication does not work if this is not called)
func startServer() *http.Server {
	// Start the server on localhost:8080, as this is an authorised redirect URI in the Google Cloud project
	srv := &http.Server{Addr: ":8080"}

	// Handles the /callback endpoint
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Get the authorisation code from the callback URL
		queryCode := r.URL.Query().Get("code")
		if queryCode == "" {
			authErr = fmt.Errorf("no code in callback")
			http.Error(w, "no code provided", http.StatusBadRequest)
			wg.Done()
			return
		}

		// Log that authorisation was successful
		authCode = queryCode
		fmt.Fprintf(w, "Authorisation successful.\n")
		wg.Done()
	})

	// Goroutine which runs the server above
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v\n", err)
		}
	}()

	return srv
}

// Get OAuth authenticated client
func getClient(config *oauth2.Config) (*http.Client, error) {
	// Try and find the token from token.json
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)

	// If that didn't work, then get one from the web
	if err != nil {
		tok, err = getTokenFromWeb(config)
		if err != nil {
			return nil, err
		}
		saveToken(tokFile, tok)
	}

	return config.Client(context.Background(), tok), nil
}

// Get OAuth token online to authenticate the client with
func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	// Start the HTTP server from which an OAuth token can be obtained
	wg.Add(1)
	srv := startServer()
	defer func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Printf("HTTP server shutdown error: %v\n", err)
		}
	}()

	// The user can visit this URL to get the authorisation token
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Please visit the following URL to authorize this application:\n%v\n", authURL)

	// Wait for the callback (which calls wg.Done())
	wg.Wait()

	if authErr != nil {
		fmt.Printf("Error getting authorisation code: %v\n", authErr)
		return nil, authErr
	}

	// Get token using authCode
	tok, err := config.Exchange(context.Background(), authCode)
	if err != nil {
		return nil, err
	}
	return tok, nil
}

// Try and read token from given file
func tokenFromFile(file string) (*oauth2.Token, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	tok := &oauth2.Token{}
	err = json.Unmarshal(data, tok)
	return tok, err
}

// Store token in given file
func saveToken(path string, token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	os.WriteFile(path, data, 0600)
	return nil
}

// This function gets the emails the user has received, finds the accounts
// which have sent them the most emails, asks the users if they would like
// to delete all emails sent from that account, then handles the deletion API calls
func processEmails(srv *gmail.Service, senderStats []SenderStats) {
	// Sort implementation for senderStats
	sort.Slice(senderStats, func(i, j int) bool {
		return senderStats[i].Count > senderStats[j].Count
	})

	// Display top senders and prompt for deletion
	fmt.Printf("\nTop email senders:\n")
	for i := 0; i < len(senderStats); i++ {
		sender := senderStats[i]
		fmt.Printf("%d. %s (%d emails)\n", i+1, sender.Email, sender.Count)

		var response string
		fmt.Printf("Would you like to delete all emails from %s? (yes/no/quit):\n", sender.Email)
		fmt.Scanln(&response)

		if strings.ToLower(response) == "yes" {
			fmt.Printf("Deleting emails from %s...\n", sender.Email)
			err := deleteEmails(srv, sender.Ids)
			if err != nil {
				fmt.Printf("Error deleting emails: %v\n", err)
			} else {
				fmt.Printf("Successfully deleted %d emails from %s\n", sender.Count, sender.Email)
			}
		} else if strings.ToLower(response) == "no" {
			continue
		} else if strings.ToLower(response) == "quit" {
			fmt.Printf("Quitting\n")
			break
		} else {
			fmt.Printf("Please enter 'yes', 'no' or 'quit'. Retrying current sender.\n")
			i--
		}
	}
}

func getSenderStats(srv *gmail.Service) ([]SenderStats, error) {
	senderMap := make(map[string]*SenderStats)

	// Fetch the emails using the List method page by page
	pageToken := ""
	for {
		req := srv.Users.Messages.List("me")
		if pageToken != "" {
			req.PageToken(pageToken)
		}

		// Perform the request and handle errors
		r, err := req.Do()
		if err != nil {
			return nil, err
		}

		// Process each email in this "page"
		for _, msg := range r.Messages {
			message, err := srv.Users.Messages.Get("me", msg.Id).Format("metadata").Do()
			if err != nil {
				fmt.Printf("Could not get metadata for email ID %s, continuing\n", msg.Id)
				continue
			}

			// Use the From header to get the sender, and increment the
			// count of the number of emails they have sent
			for _, header := range message.Payload.Headers {
				if header.Name == "From" {
					email := extractEmail(header.Value)
					if stats, exists := senderMap[email]; exists {
						stats.Count++
						stats.Ids = append(stats.Ids, msg.Id)
					} else {
						senderMap[email] = &SenderStats{
							Email: email,
							Count: 1,
							Ids:   []string{msg.Id},
						}
					}
					break
				}
			}
		}

		// Check if there are more "pages" of emails
		if r.NextPageToken == "" {
			break
		}
		pageToken = r.NextPageToken
	}

	// Return sender stats as slice
	var stats []SenderStats
	for _, v := range senderMap {
		stats = append(stats, *v)
	}

	return stats, nil
}

// Stores the email IDs, and number of emails from a particular sender
type SenderStats struct {
	Email string
	Count int
	Ids   []string
}

// Gets email address from a From email header
func extractEmail(from string) string {

	// Have only come across emails by themselves, or within angle brackets. Is this accurate?
	start := strings.LastIndex(from, "<")
	end := strings.LastIndex(from, ">")
	if start >= 0 && end > start {
		return from[start+1 : end]
	}
	return from
}

// Moves the emails with the passed IDs to the Trash
func deleteEmails(srv *gmail.Service, ids []string) error {
	var deleteErrors []string
	successCount := 0

	// Loop through given emails
	for _, id := range ids {

		// Try and move email to trash
		email, err := srv.Users.Messages.Trash("me", id).Do()
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Sprintf("failed to delete message %s: %v", id, err))
			continue
		}

		// Check that the email is in trash
		isInTrash := false
		for _, label := range email.LabelIds {
			if label == "TRASH" {
				isInTrash = true
				break
			}
		}

		if !isInTrash {
			deleteErrors = append(deleteErrors, fmt.Sprintf("message %s was not moved to trash successfully", id))
		} else {
			successCount++
			// Print progress every 10 emails
			if successCount%10 == 0 {
				fmt.Printf("Successfully deleted %d emails...\n", successCount)
			}
		}

		// Delay to avoid rate limits (TODO: is there a better way to do this?)
		time.Sleep(100 * time.Millisecond)
	}

	// Print final summary
	fmt.Printf("\nDeletion Summary:\n")
	fmt.Printf("Successfully deleted: %d emails\n", successCount)

	if len(deleteErrors) > 0 {
		fmt.Printf("Failed to delete: %d emails\n", len(deleteErrors))
		fmt.Printf("Error details:\n")
		for _, errMsg := range deleteErrors {
			fmt.Printf("- %s\n", errMsg)
		}
		return fmt.Errorf("some deletions failed: %d errors occurred", len(deleteErrors))
	}

	return nil
}
