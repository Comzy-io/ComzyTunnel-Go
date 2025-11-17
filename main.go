package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Colors for console output
const (
	ColorReset   = "\x1b[0m"
	ColorBright  = "\x1b[1m"
	ColorDim     = "\x1b[2m"
	ColorRed     = "\x1b[31m"
	ColorGreen   = "\x1b[32m"
	ColorYellow  = "\x1b[33m"
	ColorBlue    = "\x1b[34m"
	ColorMagenta = "\x1b[35m"
	ColorCyan    = "\x1b[36m"
	ColorWhite   = "\x1b[37m"
	ColorGray    = "\x1b[90m"
)

// Constants
const (
	LoginURL         = "https://portal.comzy.io"
	AnonymousTimeout = 60 * 60 * 1000 // 1 hour in milliseconds
	WSServerURL      = "wss://api.comzy.io:8191"
)

var (
	homeDir   string
	comzyDir  string
	userFile  string
)

func init() {
	var err error
	homeDir, err = os.UserHomeDir()
	if err != nil {
		logError(fmt.Sprintf("Failed to get home directory: %v", err))
		os.Exit(1)
	}
	comzyDir = filepath.Join(homeDir, ".comzy")
	userFile = filepath.Join(comzyDir, ".user")
}

// Logging utilities
func log(message, color string) {
	fmt.Printf("%s%s%s\n", color, message, ColorReset)
}

func logSuccess(message string) {
	log(message, ColorGreen)
}

func logError(message string) {
	log(message, ColorRed)
}

func logWarning(message string) {
	log(message, ColorYellow)
}

func logInfo(message string) {
	log(message, ColorCyan)
}

func logDim(message string) {
	log(message, ColorGray)
}

// Ensure .comzy folder exists
func ensureComzyDir() error {
	if _, err := os.Stat(comzyDir); os.IsNotExist(err) {
		return os.MkdirAll(comzyDir, 0755)
	}
	return nil
}

// Get stored token
func getStoredToken() string {
	data, err := os.ReadFile(userFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Save token
func saveToken(token string) error {
	if err := ensureComzyDir(); err != nil {
		return err
	}
	return os.WriteFile(userFile, []byte(strings.TrimSpace(token)), 0600)
}

// Remove token (logout)
func removeToken() {
	if _, err := os.Stat(userFile); err == nil {
		os.Remove(userFile)
		logSuccess("Logged out successfully")
	} else {
		logWarning("No active session found")
	}
}

// Handle login
func handleLogin() error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter your authentication token: ")
	token, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	token = strings.TrimSpace(token)
	if token != "" {
		if err := saveToken(token); err != nil {
			return err
		}
		logSuccess("Authentication successful")
	} else {
		logWarning("No token provided. Running in anonymous mode.")
		logInfo(fmt.Sprintf("To avoid connection timeout, login at: %s", LoginURL))
	}
	return nil
}

// Show help
func showHelp() {
	fmt.Println(`
Comzy - Secure tunnel to localhost

Usage:
  comzy [port]              Start tunnel on specified port (default: 3000)
  comzy login               Login with authentication token
  comzy logout              Logout and remove stored token
  comzy status              Show current authentication status
  comzy help                Show this help message

Examples:
  comzy 8080                Start tunnel on port 8080
  comzy                     Start tunnel on port 3000
  comzy login               Login with your token
  comzy logout              Logout from current session
`)
}

// Show status
func showStatus() {
	token := getStoredToken()
	if token != "" {
		logSuccess("Authenticated")
		if len(token) > 8 {
			logDim(fmt.Sprintf("Token: %s...", token[:8]))
		} else {
			logDim(fmt.Sprintf("Token: %s", token))
		}
	} else {
		logWarning("Not authenticated (anonymous mode)")
		logInfo(fmt.Sprintf("Login at: %s", LoginURL))
	}
}

// Request structures
type RegisterMessage struct {
	Type   string `json:"type"`
	UserID string `json:"userId"`
	Port   int    `json:"port"`
}

type IncomingRequest struct {
	ID      interface{}            `json:"id"` // Can be string or number
	Method  string                 `json:"method"`
	Path    string                 `json:"path"`
	Headers map[string]string      `json:"headers"`
	Body    interface{}            `json:"body"`
	Files   []FileUpload           `json:"files"`
	Type    string                 `json:"type"`
	Alias   string                 `json:"alias"`
}

type FileUpload struct {
	Fieldname    string     `json:"fieldname"`
	Originalname string     `json:"originalname"`
	Mimetype     string     `json:"mimetype"`
	Buffer       BufferData `json:"buffer"`
}

type BufferData struct {
	Data []byte `json:"data"`
}

type ResponseMessage struct {
	ID      interface{}       `json:"id"` // Can be string or number
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    interface{}       `json:"body"`
}

type BinaryResponse struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// Start tunnel
func startTunnel(localPort int) error {
	token := getStoredToken()
	isAnonymous := token == ""

	if isAnonymous {
		logWarning("Running in anonymous mode")
		logInfo(fmt.Sprintf("Login at: %s to avoid connection timeout", LoginURL))
		logDim("Use \"comzy login\" to authenticate\n")
	}

	fmt.Printf("%s%sStarting tunnel on localhost:%d%s\n", ColorBright, ColorWhite, localPort, ColorReset)

	var anonymousTimer *time.Timer
	var ws *websocket.Conn
	var pingTicker *time.Ticker

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println()
		logInfo("Shutting down tunnel...")
		if pingTicker != nil {
			pingTicker.Stop()
		}
		if anonymousTimer != nil {
			anonymousTimer.Stop()
		}
		if ws != nil {
			ws.Close()
		}
		os.Exit(0)
	}()

	connect := func() error {
		var err error
		ws, _, err = websocket.DefaultDialer.Dial(WSServerURL, nil)
		if err != nil {
			return fmt.Errorf("connection error: %v", err)
		}

		logSuccess("Connected to tunnel server")

		// Send register message
		registerMsg := RegisterMessage{
			Type:   "register",
			UserID: token,
			Port:   localPort,
		}
		if token == "" {
			registerMsg.UserID = "anonymous"
		}

		if err := ws.WriteJSON(registerMsg); err != nil {
			return fmt.Errorf("failed to register: %v", err)
		}

		// Set anonymous timeout
		if isAnonymous {
			anonymousTimer = time.AfterFunc(time.Hour, func() {
				fmt.Println()
				logWarning("Anonymous session expired (1 hour limit)")
				logInfo(fmt.Sprintf("Login at: %s for unlimited access", LoginURL))
				os.Exit(0)
			})
		}

		// Start ping ticker
		pingTicker = time.NewTicker(20 * time.Second)
		go func() {
			for range pingTicker.C {
				if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}()

		// Handle messages
		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				logWarning("Disconnected from tunnel server")
				if pingTicker != nil {
					pingTicker.Stop()
				}
				if anonymousTimer != nil {
					anonymousTimer.Stop()
				}
				return err
			}

			var request IncomingRequest
			if err := json.Unmarshal(message, &request); err != nil {
				logError(fmt.Sprintf("Failed to parse message: %v", err))
				continue
			}

			if request.Type == "registered" {
				generatedURL := fmt.Sprintf("https://%s.comzy.io", request.Alias)
				fmt.Println()
				logSuccess("Tunnel established")
				fmt.Printf("%sPublic URL:     %s%s%s\n", ColorBright, ColorCyan, generatedURL, ColorReset)
				fmt.Printf("%sForwarding to:  %shttp://localhost:%d%s\n", ColorBright, ColorCyan, localPort, ColorReset)

				if isAnonymous {
					logDim("Anonymous session will expire in 1 hour")
				}

				fmt.Println()
				logDim("Waiting for connections...")
				fmt.Println()
				continue
			}

			go handleRequest(ws, request, localPort)
		}
	}

	// Initial connection
	for {
		if err := connect(); err != nil {
			logError(err.Error())
			logInfo("Reconnecting in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}
}

// Handle incoming request
func handleRequest(ws *websocket.Conn, request IncomingRequest, localPort int) {
	defer func() {
		if r := recover(); r != nil {
			logError(fmt.Sprintf("Panic in handleRequest: %v", r))
		}
	}()

	logDim(fmt.Sprintf("%s %s -> localhost:%d", request.Method, request.Path, localPort))

	url := fmt.Sprintf("http://localhost:%d%s", localPort, request.Path)

	var reqBody io.Reader
	var contentType string

	// Handle multipart/form-data with files
	if strings.Contains(request.Headers["content-type"], "multipart/form-data") && len(request.Files) > 0 {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add form fields
		if bodyMap, ok := request.Body.(map[string]interface{}); ok {
			for key, value := range bodyMap {
				writer.WriteField(key, fmt.Sprintf("%v", value))
			}
		}

		// Add files
		for _, file := range request.Files {
			part, err := writer.CreateFormFile(file.Fieldname, file.Originalname)
			if err != nil {
				logError(fmt.Sprintf("Failed to create form file: %v", err))
				continue
			}
			part.Write(file.Buffer.Data)
		}

		writer.Close()
		reqBody = body
		contentType = writer.FormDataContentType()
	} else if request.Body != nil {
		// Handle regular body
		bodyBytes, _ := json.Marshal(request.Body)
		reqBody = bytes.NewReader(bodyBytes)
		contentType = request.Headers["content-type"]
	}

	// Create HTTP request
	httpReq, err := http.NewRequest(request.Method, url, reqBody)
	if err != nil {
		sendErrorResponse(ws, request.ID, err)
		return
	}

	// Set headers
	for key, value := range request.Headers {
		httpReq.Header.Set(key, value)
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}

	// Send request
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		sendErrorResponse(ws, request.ID, err)
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		sendErrorResponse(ws, request.ID, err)
		return
	}

	// Convert headers to map
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[strings.ToLower(key)] = values[0]
		}
	}

	// Prepare response body
	var responseBody interface{}
	respContentType := resp.Header.Get("Content-Type")

	// Check if binary data
	if strings.HasPrefix(respContentType, "image/") ||
		strings.HasPrefix(respContentType, "video/") ||
		strings.HasPrefix(respContentType, "audio/") ||
		strings.Contains(respContentType, "application/octet-stream") ||
		strings.Contains(respContentType, "application/pdf") {
		// Binary data - convert to base64
		responseBody = BinaryResponse{
			Type: "binary",
			Data: base64.StdEncoding.EncodeToString(respBody),
		}
	} else {
		// Text data
		if strings.Contains(respContentType, "application/json") {
			// Try to parse JSON
			var jsonBody interface{}
			if err := json.Unmarshal(respBody, &jsonBody); err == nil {
				responseBody = jsonBody
			} else {
				responseBody = string(respBody)
			}
		} else {
			responseBody = string(respBody)
		}
	}

	// Send response back through WebSocket
	response := ResponseMessage{
		ID:      request.ID,
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    responseBody,
	}

	if err := ws.WriteJSON(response); err != nil {
		logError(fmt.Sprintf("Failed to send response: %v", err))
	}
}

// Send error response
func sendErrorResponse(ws *websocket.Conn, id interface{}, err error) {
	logError(fmt.Sprintf("Proxy error: %v", err))

	response := ResponseMessage{
		ID:     id,
		Status: 500,
		Headers: map[string]string{
			"content-type": "application/json",
		},
		Body: map[string]string{
			"error": "Internal server error",
		},
	}

	if err := ws.WriteJSON(response); err != nil {
		logError(fmt.Sprintf("Failed to send error response: %v", err))
	}
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		// Default: start tunnel on port 3000
		if err := startTunnel(3000); err != nil {
			logError(fmt.Sprintf("Fatal error: %v", err))
			os.Exit(1)
		}
		return
	}

	command := args[0]

	switch command {
	case "help", "--help", "-h":
		showHelp()
	case "login":
		if err := handleLogin(); err != nil {
			logError(fmt.Sprintf("Login failed: %v", err))
			os.Exit(1)
		}
	case "logout":
		removeToken()
	case "status":
		showStatus()
	default:
		// Try to parse as port number
		port, err := strconv.Atoi(command)
		if err != nil || port < 1 || port > 65535 {
			logError("Invalid port number. Use a port between 1-65535")
			os.Exit(1)
		}
		if err := startTunnel(port); err != nil {
			logError(fmt.Sprintf("Fatal error: %v", err))
			os.Exit(1)
		}
	}
}
