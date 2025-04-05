package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mdp/qrterminal"
	_ "github.com/mattn/go-sqlite3"
	
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

// Message represents a chat message for our client
type Message struct {
	Time     time.Time
	Sender   string
	Content  string
	IsFromMe bool
	// Add image-related fields
	ImageURL     string
	ThumbnailURL string
	MediaType    string
}

// Database handler for storing message history
type MessageStore struct {
	db *sql.DB
}

// Initialize message store
func NewMessageStore() (*MessageStore, error) {
	// Create directory for database if it doesn't exist
	if err := os.MkdirAll("store", 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %v", err)
	}
	
	// Open SQLite database for messages
	db, err := sql.Open("sqlite3", "file:store/messages.db?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open message database: %v", err)
	}
	
	// Create tables if they don't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT,
			last_message_time TIMESTAMP
		);
		
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT,
			chat_jid TEXT,
			sender TEXT,
			content TEXT,
			timestamp TIMESTAMP,
			is_from_me BOOLEAN,
			image_url TEXT,
			thumbnail_url TEXT,
			media_type TEXT,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid)
		);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %v", err)
	}
	
	return &MessageStore{db: db}, nil
}

// Close the database connection
func (store *MessageStore) Close() error {
	return store.db.Close()
}

// Store a chat in the database
func (store *MessageStore) StoreChat(jid, name string, lastMessageTime time.Time) error {
	_, err := store.db.Exec(
		"INSERT OR REPLACE INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)",
		jid, name, lastMessageTime,
	)
	return err
}

// Store a message in the database
func (store *MessageStore) StoreMessage(id, chatJID, sender, content string, timestamp time.Time, isFromMe bool, imageURL, thumbnailURL, mediaType string) error {
	// Only store if there's actual content or media
	if content == "" && imageURL == "" {
		return nil
	}
	
	_, err := store.db.Exec(
		"INSERT OR REPLACE INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, image_url, thumbnail_url, media_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, chatJID, sender, content, timestamp, isFromMe, imageURL, thumbnailURL, mediaType,
	)
	return err
}

// Get messages from a chat
func (store *MessageStore) GetMessages(chatJID string, limit int) ([]Message, error) {
	rows, err := store.db.Query(
		"SELECT sender, content, timestamp, is_from_me, image_url, thumbnail_url, media_type FROM messages WHERE chat_jid = ? ORDER BY timestamp DESC LIMIT ?",
		chatJID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var messages []Message
	for rows.Next() {
		var msg Message
		var timestamp time.Time
		err := rows.Scan(&msg.Sender, &msg.Content, &timestamp, &msg.IsFromMe, &msg.ImageURL, &msg.ThumbnailURL, &msg.MediaType)
		if err != nil {
			return nil, err
		}
		msg.Time = timestamp
		messages = append(messages, msg)
	}
	
	return messages, nil
}

// Get all chats
func (store *MessageStore) GetChats() (map[string]time.Time, error) {
	rows, err := store.db.Query("SELECT jid, last_message_time FROM chats ORDER BY last_message_time DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	chats := make(map[string]time.Time)
	for rows.Next() {
		var jid string
		var lastMessageTime time.Time
		err := rows.Scan(&jid, &lastMessageTime)
		if err != nil {
			return nil, err
		}
		chats[jid] = lastMessageTime
	}
	
	return chats, nil
}

// Extract text content from a message
func extractTextContent(msg *waProto.Message) string {
	if msg == nil {
		return ""
	}
	
	// Try to get text content
	if text := msg.GetConversation(); text != "" {
		return text
	} else if extendedText := msg.GetExtendedTextMessage(); extendedText != nil {
		return extendedText.GetText()
	}
	
	// Check for image caption
	if imageMsg := msg.GetImageMessage(); imageMsg != nil {
		return imageMsg.GetCaption()
	}
	
	return ""
}

// Extract media content from a message
func extractMediaContent(client *whatsmeow.Client, msg *waProto.Message, chatJID string, isHistorical bool, messageTimestamp time.Time) (string, string, string, error) {
	if msg == nil {
		return "", "", "", nil
	}

	// Only handle image messages
	if imageMsg := msg.GetImageMessage(); imageMsg != nil {
		// Skip old messages in non-historical context
		if !isHistorical {
			fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
			if messageTimestamp.Before(fiveMinutesAgo) {
				return "", "", "", nil
			}
		}

		// Download the image
		data, err := client.Download(imageMsg)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to download image: %v", err)
		}

		// Create media directory if it doesn't exist
		mediaDir := "store/media"
		if err := os.MkdirAll(mediaDir, 0755); err != nil {
			return "", "", "", fmt.Errorf("failed to create media directory: %v", err)
		}

		// Generate a filename based on timestamp
		filename := fmt.Sprintf("%s/img_%d.jpg", mediaDir, time.Now().UnixNano())
		
		// Save the image
		if err := os.WriteFile(filename, data, 0644); err != nil {
			return "", "", "", fmt.Errorf("failed to save image: %v", err)
		}

		return filename, string(imageMsg.GetJPEGThumbnail()), "image", nil
	}

	// Return empty values for non-image media types
	return "", "", "", nil
}

// SendMessageResponse represents the response for the send message API
type SendMessageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// SendMessageRequest represents the request body for the send message API
type SendMessageRequest struct {
	Phone   string `json:"phone"`
	Message string `json:"message"`
	MediaURL string `json:"media_url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Caption string `json:"caption,omitempty"`
}

// Function to verify and convert image
func verifyAndConvertImage(data []byte) ([]byte, int, int, error) {
	fmt.Printf("Processing image data: %d bytes\n", len(data))
	
	// Try to detect content type
	contentType := http.DetectContentType(data)
	fmt.Printf("Detected content type: %s\n", contentType)
	
	// Create a new reader for the image data
	reader := bytes.NewReader(data)
	
	// Decode image
	img, format, err := image.Decode(reader)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("Error decoding image: %v", err)
	}
	fmt.Printf("Successfully decoded image format: %s\n", format)
	
	// Get dimensions
	bounds := img.Bounds()
	width := bounds.Max.X
	height := bounds.Max.Y
	
	// Convert to RGBA if necessary
	var rgba *image.RGBA
	if rgbaImg, ok := img.(*image.RGBA); ok {
		rgba = rgbaImg
	} else {
		rgba = image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
	}
	
	// Create buffer for JPEG
	var jpegBuf bytes.Buffer
	
	// Encode as JPEG with high quality
	if err := jpeg.Encode(&jpegBuf, rgba, &jpeg.Options{Quality: 100}); err != nil {
		return nil, 0, 0, fmt.Errorf("Error encoding JPEG: %v", err)
	}
	
	jpegData := jpegBuf.Bytes()
	fmt.Printf("Successfully converted to JPEG: %d bytes\n", len(jpegData))
	
	return jpegData, width, height, nil
}

// Function to send a WhatsApp message
func sendWhatsAppMessage(client *whatsmeow.Client, phone, message string, mediaURL, mediaType, caption string) (bool, string) {
	// Validate client connection
	if !client.IsConnected() {
		return false, "Not connected to WhatsApp"
	}
	
	// Create JID for recipient
	var recipientJID types.JID
	if strings.HasSuffix(phone, "@g.us") {
		// Group chat
		recipientJID = types.JID{
			User:   strings.TrimSuffix(phone, "@g.us"),
			Server: "g.us",
		}
	} else {
		// Individual chat - add s.whatsapp.net if not present
		recipientJID = types.JID{
			User:   phone,
			Server: "s.whatsapp.net",
		}
	}
	
	// Create appropriate message based on type
	var msg *waProto.Message
	
	if mediaURL != "" && mediaType != "" {
		// Process media message
		mediaData, err := os.ReadFile(mediaURL)
		if err != nil {
			return false, fmt.Sprintf("Error reading media file: %v", err)
		}
		
		switch mediaType {
		case "image":
			// Process and send image
			jpegData, width, height, err := verifyAndConvertImage(mediaData)
			if err != nil {
				return false, fmt.Sprintf("Error processing image: %v", err)
			}
			
			// Upload the JPEG image to WhatsApp servers
			uploadedImage, err := client.Upload(context.Background(), jpegData, whatsmeow.MediaImage)
			if err != nil {
				return false, fmt.Sprintf("Error uploading image: %v", err)
			}
			
			msg = &waProto.Message{
				ImageMessage: &waProto.ImageMessage{
					URL:           proto.String(uploadedImage.URL),
					DirectPath:    proto.String(uploadedImage.DirectPath),
					MediaKey:      uploadedImage.MediaKey,
					FileEncSHA256: uploadedImage.FileEncSHA256,
					FileSHA256:    uploadedImage.FileSHA256,
					FileLength:    proto.Uint64(uploadedImage.FileLength),
					Caption:       proto.String(caption),
					Mimetype:      proto.String("image/jpeg"),
					Width:         proto.Uint32(uint32(width)),
					Height:        proto.Uint32(uint32(height)),
				},
			}

		case "video":
			// Upload the video to WhatsApp servers
			uploadedVideo, err := client.Upload(context.Background(), mediaData, whatsmeow.MediaVideo)
			if err != nil {
				return false, fmt.Sprintf("Error uploading video: %v", err)
			}
			
			msg = &waProto.Message{
				VideoMessage: &waProto.VideoMessage{
					URL:           proto.String(uploadedVideo.URL),
					DirectPath:    proto.String(uploadedVideo.DirectPath),
					MediaKey:      uploadedVideo.MediaKey,
					FileEncSHA256: uploadedVideo.FileEncSHA256,
					FileSHA256:    uploadedVideo.FileSHA256,
					FileLength:    proto.Uint64(uploadedVideo.FileLength),
					Caption:       proto.String(caption),
					Mimetype:      proto.String(http.DetectContentType(mediaData)),
				},
			}
		default:
			// Fallback to text message if media type is not supported
			msg = &waProto.Message{
				Conversation: proto.String(message),
			}
		}
	} else {
		// Simple text message
		msg = &waProto.Message{
			Conversation: proto.String(message),
		}
	}
	
	// Send the message
	sent, err := client.SendMessage(context.Background(), recipientJID, msg)
	
	if err != nil {
		return false, fmt.Sprintf("Error sending message: %v", err)
	}
	
	return true, fmt.Sprintf("Message sent to %s with ID: %s", phone, sent.ID)
}

// Start a REST API server to expose the WhatsApp client functionality
func startRESTServer(client *whatsmeow.Client, port int) {
	// Handler for sending messages
	http.HandleFunc("/api/send", func(w http.ResponseWriter, r *http.Request) {
		// Only allow POST requests
		fmt.Printf("[HTTP] Received %s request to /api/send from %s\n", r.Method, r.RemoteAddr)
		if r.Method != http.MethodPost {
			fmt.Printf("[ERROR] Method %s not allowed\n", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		
		// Parse the request body
		var req SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			fmt.Printf("[ERROR] Failed to parse request body: %v\n", err)
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}
		
		fmt.Printf("[DEBUG] Received message request: phone=%s, hasMedia=%v, mediaType=%s\n", 
			req.Phone, req.MediaURL != "", req.MediaType)
		
		// Validate request
		if req.Phone == "" || (req.Message == "" && req.MediaURL == "") {
			fmt.Printf("[ERROR] Invalid request: phone=%s, message=%s, mediaURL=%s\n", 
				req.Phone, req.Message, req.MediaURL)
			http.Error(w, "Phone and either message or media URL are required", http.StatusBadRequest)
			return
		}
		
		// Send the message
		success, message := sendWhatsAppMessage(client, req.Phone, req.Message, req.MediaURL, req.MediaType, req.Caption)
		fmt.Printf("[DEBUG] Message send result: success=%v, message=%s\n", success, message)
		
		// Set response headers
		w.Header().Set("Content-Type", "application/json")
		
		// Set appropriate status code
		if !success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		
		// Send response
		response := SendMessageResponse{
			Success: success,
			Message: message,
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			fmt.Printf("[ERROR] Failed to encode response: %v\n", err)
		}
	})
	
	// Start the server
	serverAddr := fmt.Sprintf(":%d", port)
	fmt.Printf("[SERVER] Starting REST API server on %s...\n", serverAddr)
	
	// Run server in a goroutine so it doesn't block
	go func() {
		if err := http.ListenAndServe(serverAddr, nil); err != nil {
			fmt.Printf("[ERROR] REST API server error: %v\n", err)
		}
	}()
}

// Config represents the application configuration
type Config struct {
	InputGroups  []string                     `json:"input_groups"`
	Destinations map[string]DestinationConfig `json:"destinations"`
	Media        MediaConfig                  `json:"media"`
}

type DestinationConfig struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

type MediaConfig struct {
	AllowedExtensions []string `json:"allowed_extensions"`
	StorePath         string   `json:"store_path"`
}

var appConfig Config

// isKindergartenGroup checks if the given chat JID belongs to a kindergarten group
func isKindergartenGroup(chatJID string) bool {
	for _, groupJID := range appConfig.InputGroups {
		if chatJID == groupJID {
			return true
		}
	}
	return false
}

// listGroups lists all groups the user is a member of
func listGroups(client *whatsmeow.Client) error {
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("client is not connected")
	}
	
	groups, err := client.GetJoinedGroups()
	if err != nil {
		return fmt.Errorf("failed to get groups: %v", err)
	}
	
	fmt.Println("\n=== WhatsApp Groups ===")
	fmt.Printf("Found %d groups:\n\n", len(groups))
	
	for i, group := range groups {
		fmt.Printf("%d. Name: %s\n   ID: %s\n\n", i+1, group.Name, group.JID)
	}
	
	fmt.Println("To use a group in your configuration, copy the ID (including @g.us) into your config.json file.")
	return nil
}

func main() {
	// Command line flags
	listGroupsFlag := flag.Bool("list-groups", false, "List all WhatsApp groups and exit")
	apiPort := flag.Int("port", 8080, "Port for the REST API server")
	flag.Parse()

	// Read configuration file
	configData, err := os.ReadFile("../config.json")
	if err != nil {
		fmt.Printf("Error reading config file: %v\n", err)
		return
	}

	// Parse configuration
	if err := json.Unmarshal(configData, &appConfig); err != nil {
		fmt.Printf("Error parsing config file: %v\n", err)
		return
	}

	// Set up logger with debug level
	logger := waLog.Stdout("Client", "INFO", true)
	logger.Infof("[STARTUP] Starting WhatsApp client...")

	// Create database connection for storing session data
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	
	// Create directory for database if it doesn't exist
	if err := os.MkdirAll("store", 0755); err != nil {
		logger.Errorf("[ERROR] Failed to create store directory: %v", err)
		return
	}
	
	container, err := sqlstore.New("sqlite3", "file:store/whatsapp.db?_foreign_keys=on", dbLog)
	if err != nil {
		logger.Errorf("[ERROR] Failed to connect to database: %v", err)
		return
	}

	// Get device store - This contains session information
	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		if err == sql.ErrNoRows {
			// No device exists, create one
			deviceStore = container.NewDevice()
			logger.Infof("[SETUP] Created new device")
		} else {
			logger.Errorf("[ERROR] Failed to get device: %v", err)
			return
		}
	}

	// Create client instance
	client := whatsmeow.NewClient(deviceStore, logger)
	if client == nil {
		logger.Errorf("[ERROR] Failed to create WhatsApp client")
		return
	}
	
	// Initialize message store
	messageStore, err := NewMessageStore()
	if err != nil {
		logger.Errorf("[ERROR] Failed to initialize message store: %v", err)
		return
	}
	defer messageStore.Close()
	
	// Setup event handling for messages and history sync
	client.AddEventHandler(func(evt interface{}) {
		logger.Infof("[EVENT] Received event type: %T", evt)
		
		switch v := evt.(type) {
		case *events.Message:
			logger.Infof("[MESSAGE] Processing incoming message event")
			handleMessage(client, messageStore, v, logger)
			
		case *events.HistorySync:
			logger.Infof("[SYNC] Processing history sync event")
			handleHistorySync(client, messageStore, v, logger)
			
		case *events.Connected:
			logger.Infof("[CONNECTION] Connected to WhatsApp")
			// List all groups when connected
			if groups, err := client.GetJoinedGroups(); err == nil {
				logger.Infof("[GROUPS] Found %d groups:", len(groups))
				for _, group := range groups {
					logger.Infof("[GROUP] Name: %s (JID: %s)", group.Name, group.JID)
				}
			}
			
			// If we're only listing groups, do it and exit
			if *listGroupsFlag {
				if err := listGroups(client); err != nil {
					logger.Errorf("Failed to list groups: %v", err)
				}
				client.Disconnect()
				os.Exit(0)
			}
			
		case *events.LoggedOut:
			logger.Warnf("[AUTH] Device logged out, please scan QR code to log in again")
			
		case *events.Disconnected:
			logger.Infof("[CONNECTION] Disconnected from WhatsApp")
		}
	})
	
	// Create channel to track connection success
	connected := make(chan bool, 1)
	
	// Connect to WhatsApp
	if client.Store.ID == nil {
		// No ID stored, this is a new client, need to pair with phone
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			logger.Errorf("Failed to connect: %v", err)
			return
		}

		// Print QR code for pairing with phone
		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("\nScan this QR code with your WhatsApp app:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			} else if evt.Event == "success" {
				connected <- true
				break
			}
		}
		
		// Wait for connection
		select {
		case <-connected:
			fmt.Println("\nSuccessfully connected and authenticated!")
		case <-time.After(3 * time.Minute):
			logger.Errorf("Timeout waiting for QR code scan")
			return
		}
	} else {
		// Already logged in, just connect
		err = client.Connect()
		if err != nil {
			logger.Errorf("Failed to connect: %v", err)
			return
		}
		connected <- true
	}

	// Wait a moment for connection to stabilize
	time.Sleep(2 * time.Second)
	
	if !client.IsConnected() {
		logger.Errorf("Failed to establish stable connection")
		return
	}
	
	fmt.Println("\n✓ Connected to WhatsApp! Type 'help' for commands.")
	
	// Start REST API server
	startRESTServer(client, *apiPort)
	
	// Create a channel to keep the main goroutine alive
	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGINT, syscall.SIGTERM)
	
	fmt.Printf("REST server is running on port %d. Press Ctrl+C to disconnect and exit.\n", *apiPort)
	
	// Wait for termination signal
	<-exitChan
	
	fmt.Println("Disconnecting...")
	// Disconnect client
	client.Disconnect()
}

// Handle regular incoming messages
func handleMessage(client *whatsmeow.Client, messageStore *MessageStore, msg *events.Message, logger waLog.Logger) {
	// Extract basic message information
	chatJID := msg.Info.Chat.String()
	sender := msg.Info.Sender.String()
	isFromMe := msg.Info.IsFromMe
	
	// Skip processing for non-monitored groups
	if msg.Info.IsGroup && !isKindergartenGroup(chatJID) {
		logger.Infof("Skipping message from non-monitored group: %s", chatJID)
		return
	}

	// Extract message content and media
	content := extractTextContent(msg.Message)
	imageURL, thumbnailURL, mediaType, err := extractMediaContent(client, msg.Message, chatJID, false, msg.Info.Timestamp)
	if err != nil {
		logger.Warnf("Failed to process media: %v", err)
	}

	// Skip empty messages (no text and no media)
	if content == "" && imageURL == "" {
		return
	}

	// Get chat name if possible
	name := msg.Info.Chat.User
	contact, err := client.Store.Contacts.GetContact(msg.Info.Chat)
	if err == nil && contact.FullName != "" {
		name = contact.FullName
	}

	// Store chat information
	if err := messageStore.StoreChat(chatJID, name, msg.Info.Timestamp); err != nil {
		logger.Warnf("Failed to store chat: %v", err)
	}

	// Store the message
	if err := messageStore.StoreMessage(
		msg.Info.ID,
		chatJID,
		sender,
		content,
		msg.Info.Timestamp,
		isFromMe,
		imageURL,
		thumbnailURL,
		mediaType,
	); err != nil {
		logger.Errorf("Failed to store message: %v", err)
		return
	}
	
	// Log successful message storage
	direction := "←"
	if isFromMe {
		direction = "→"
	}
	
	mediaInfo := ""
	if mediaType != "" {
		mediaInfo = fmt.Sprintf(" [%s: %s]", mediaType, imageURL)
	}
	
	logger.Infof("Stored message: [%s] %s %s: %s%s", 
		msg.Info.Timestamp.Format("2006-01-02 15:04:05"), 
		direction, sender, content, mediaInfo)
}

// Handle history sync events
func handleHistorySync(client *whatsmeow.Client, messageStore *MessageStore, historySync *events.HistorySync, logger waLog.Logger) {
	fmt.Printf("Received history sync event with %d conversations\n", len(historySync.Data.Conversations))
	
	syncedCount := 0
	for _, conversation := range historySync.Data.Conversations {
		// Parse JID from the conversation
		if conversation.ID == nil {
			continue
		}
		
		chatJID := *conversation.ID
		
		// Try to parse the JID
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			logger.Warnf("Failed to parse JID %s: %v", chatJID, err)
			continue
		}
		
		// Get contact name
		name := jid.User
		contact, err := client.Store.Contacts.GetContact(jid)
		if err == nil && contact.FullName != "" {
			name = contact.FullName
		}
		
		// Process messages
		messages := conversation.Messages
		if len(messages) > 0 {
			// Update chat with latest message timestamp
			latestMsg := messages[0]
			if latestMsg == nil || latestMsg.Message == nil {
				continue
			}
			
			// Get timestamp from message info
			timestamp := time.Time{}
			if ts := latestMsg.Message.GetMessageTimestamp(); ts != 0 {
				timestamp = time.Unix(int64(ts), 0)
			} else {
				continue
			}
			
			messageStore.StoreChat(chatJID, name, timestamp)
			
			// Store messages
			for _, msg := range messages {
				if msg == nil || msg.Message == nil {
					continue
				}
				
				// Extract text content
				var content string
				if msg.Message.Message != nil {
					content = extractTextContent(msg.Message.Message)
				}
				
				// Extract media content
				imageURL, thumbnailURL, mediaType := "", "", ""
				var downloadErr error
				if msg.Message.Message != nil {
					imageURL, thumbnailURL, mediaType, downloadErr = extractMediaContent(client, msg.Message.Message, chatJID, false, timestamp)
					if downloadErr != nil {
						logger.Warnf("Failed to process media: %v", downloadErr)
					}
				}
				
				// Skip empty messages (no text and no media)
				if content == "" && imageURL == "" {
					continue
				}
				
				// Determine sender
				var sender string
				isFromMe := false
				if msg.Message.Key != nil {
					if msg.Message.Key.FromMe != nil {
						isFromMe = *msg.Message.Key.FromMe
					}
					if !isFromMe && msg.Message.Key.Participant != nil && *msg.Message.Key.Participant != "" {
						sender = *msg.Message.Key.Participant
					} else if isFromMe {
						sender = client.Store.ID.User
					} else {
						sender = jid.User
					}
				} else {
					sender = jid.User
				}
				
				// Store message
				msgID := ""
				if msg.Message.Key != nil && msg.Message.Key.ID != nil {
					msgID = *msg.Message.Key.ID
				}
				
				// Get message timestamp
				timestamp := time.Time{}
				if ts := msg.Message.GetMessageTimestamp(); ts != 0 {
					timestamp = time.Unix(int64(ts), 0)
				} else {
					continue
				}
				
				err = messageStore.StoreMessage(
					msgID,
					chatJID,
					sender,
					content,
					timestamp,
					isFromMe,
					imageURL,
					thumbnailURL,
					mediaType,
				)
				if err != nil {
					logger.Warnf("Failed to store history message: %v", err)
				} else {
					syncedCount++
					// Log successful message storage
					logger.Infof("Stored message: [%s] %s -> %s: %s", timestamp.Format("2006-01-02 15:04:05"), sender, chatJID, content)
				}
			}
		}
	}
	
	fmt.Printf("History sync complete. Stored %d text messages.\n", syncedCount)
}

// Request history sync from the server
func requestHistorySync(client *whatsmeow.Client) {
	if client == nil {
		fmt.Println("Client is not initialized. Cannot request history sync.")
		return
	}

	if !client.IsConnected() {
		fmt.Println("Client is not connected. Please ensure you are connected to WhatsApp first.")
		return
	}

	if client.Store.ID == nil {
		fmt.Println("Client is not logged in. Please scan the QR code first.")
		return
	}

	// Build and send a history sync request
	historyMsg := client.BuildHistorySyncRequest(nil, 100)
	if historyMsg == nil {
		fmt.Println("Failed to build history sync request.")
		return
	}

	_, err := client.SendMessage(context.Background(), types.JID{
		Server: "s.whatsapp.net",
		User:   "status",
	}, historyMsg)
	
	if err != nil {
		fmt.Printf("Failed to request history sync: %v\n", err)
	} else {
		fmt.Println("History sync requested. Waiting for server response...")
	}
}
