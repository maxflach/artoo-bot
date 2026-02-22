package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
)

// WebChatTransport implements Transport using Server-Sent Events + HTTP POST.
// Browsers connect to GET /chat/sse for a real-time event stream, and POST
// to /chat/message to send messages. Access is gated behind the bot's API
// key mechanism. All web chat sessions run as the configured admin user.
type WebChatTransport struct {
	bot     *Bot
	handler func(IncomingMessage)
	clients sync.Map // sessionID → chan string
}

func newWebChatTransport(bot *Bot) *WebChatTransport {
	return &WebChatTransport{bot: bot}
}

func (wc *WebChatTransport) Name() string { return "wc" }

func (wc *WebChatTransport) Send(chatID string, text string) error {
	chVal, ok := wc.clients.Load(chatID)
	if !ok {
		return fmt.Errorf("webchat: no active session %q", chatID)
	}
	ch := chVal.(chan string)
	select {
	case ch <- text:
	default:
		log.Printf("webchat: client %q send buffer full, dropping message", chatID)
	}
	return nil
}

func (wc *WebChatTransport) SendFile(chatID string, filePath string) error {
	return wc.Send(chatID, fmt.Sprintf("📎 File created: %s", filePath))
}

func (wc *WebChatTransport) SendTyping(_ string) {} // no-op

func (wc *WebChatTransport) Start(handler func(IncomingMessage)) error {
	wc.handler = handler
	// Routes are registered via RegisterRoutes called from startAPIServer.
	// This goroutine just stays alive until the process exits.
	select {}
}

// RegisterRoutes mounts the web chat endpoints on the given mux.
// Must be called before Start().
func (wc *WebChatTransport) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/chat", wc.bot.requireAPIKey(wc.handleChatPage))
	mux.HandleFunc("/chat/sse", wc.handleSSE) // auth via query param
	mux.HandleFunc("/chat/message", wc.bot.requireAPIKey(wc.handleMessage))
}

const chatPageHTML = `<!DOCTYPE html>
<html>
<head>
  <title>%s</title>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
           background: #1a1a1a; color: #e0e0e0; height: 100vh; display: flex; flex-direction: column; }
    #messages { flex: 1; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 8px; }
    .msg { max-width: 80%%; padding: 8px 12px; border-radius: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-word; }
    .msg.user { background: #2b6cb0; align-self: flex-end; }
    .msg.bot  { background: #2d2d2d; align-self: flex-start; border: 1px solid #444; }
    #input-row { display: flex; gap: 8px; padding: 12px; border-top: 1px solid #333; }
    #input { flex: 1; padding: 10px 14px; border-radius: 8px; border: 1px solid #444;
             background: #2d2d2d; color: #e0e0e0; font-size: 15px; resize: none; }
    #send { padding: 10px 20px; background: #2b6cb0; color: white; border: none;
            border-radius: 8px; cursor: pointer; font-size: 15px; }
    #send:hover { background: #3182ce; }
    #send:disabled { background: #555; cursor: default; }
    #status { padding: 4px 12px; font-size: 12px; color: #666; background: #111; }
    #no-key { padding: 20px; color: #f66; }
  </style>
</head>
<body>
<div id="no-key" hidden></div>
<div id="messages"></div>
<div id="status">Connecting...</div>
<div id="input-row">
  <textarea id="input" rows="2" placeholder="Message %s..."></textarea>
  <button id="send" disabled>Send</button>
</div>
<script>
const token = new URLSearchParams(location.search).get('key') || localStorage.getItem('webchat_key');
if (!token) {
  const d = document.getElementById('no-key');
  d.removeAttribute('hidden');
  d.textContent = 'No API key. Visit /chat?key=YOUR_KEY';
  document.getElementById('input-row').style.display = 'none';
  document.getElementById('status').style.display = 'none';
} else {
  localStorage.setItem('webchat_key', token);
}

const msgs = document.getElementById('messages');
const input = document.getElementById('input');
const sendBtn = document.getElementById('send');
const status = document.getElementById('status');
let sessionID = '';

function appendMsg(role, text) {
  const d = document.createElement('div');
  d.className = 'msg ' + role;
  d.textContent = text;
  msgs.appendChild(d);
  msgs.scrollTop = msgs.scrollHeight;
}

const es = new EventSource('/chat/sse?key=' + encodeURIComponent(token));
es.onopen = () => { status.textContent = 'Connected'; sendBtn.disabled = false; };
es.onerror = () => { status.textContent = 'Disconnected — reload to reconnect'; sendBtn.disabled = true; };
es.addEventListener('session', e => { sessionID = e.data; });
es.addEventListener('message', e => { appendMsg('bot', e.data.replace(/\r/g, '\n')); });

async function sendMessage() {
  const text = input.value.trim();
  if (!text) return;
  appendMsg('user', text);
  input.value = '';
  sendBtn.disabled = true;
  status.textContent = 'Working...';
  try {
    await fetch('/chat/message', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token },
      body: JSON.stringify({ text, session_id: sessionID })
    });
  } catch(e) { appendMsg('bot', 'Error: ' + e.message); }
  sendBtn.disabled = false;
  status.textContent = 'Connected';
}

sendBtn.addEventListener('click', sendMessage);
input.addEventListener('keydown', e => {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage(); }
});
</script>
</body>
</html>
`

func (wc *WebChatTransport) handleChatPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, chatPageHTML, wc.bot.cfg.Persona.Name, wc.bot.cfg.Persona.Name)
}

// handleSSE opens a Server-Sent Events stream. Auth via ?key= query param so the
// browser's EventSource (which can't set headers) can authenticate.
func (wc *WebChatTransport) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Validate API key from query param.
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusUnauthorized)
		return
	}
	if !wc.bot.mem.lookupAPIKey(hashKey(key)) {
		http.Error(w, "invalid key", http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sessionID := generateConnID()
	ch := make(chan string, 16)
	wc.clients.Store(sessionID, ch)
	defer wc.clients.Delete(sessionID)

	// Tell the client its session ID for use in POST requests.
	fmt.Fprintf(w, "event: session\ndata: %s\n\n", sessionID)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			// Encode newlines as \r; client decodes back to \n.
			escaped := strings.ReplaceAll(msg, "\n", "\r")
			fmt.Fprintf(w, "data: %s\n\n", escaped)
			flusher.Flush()
		}
	}
}

// handleMessage receives a user message from the web chat client.
func (wc *WebChatTransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var body struct {
		Text      string `json:"text"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Text == "" {
		apiError(w, http.StatusBadRequest, "text is required")
		return
	}

	// Web chat runs as the admin user.
	userID := wc.bot.cfg.Telegram.AdminUserID
	if userID == 0 {
		wc.bot.sessionsMu.RLock()
		for id := range wc.bot.sessions {
			userID = id
			break
		}
		wc.bot.sessionsMu.RUnlock()
	}
	if userID == 0 {
		apiError(w, http.StatusInternalServerError, "no user configured")
		return
	}

	sessionID := body.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("user-%d", userID)
	}
	chatID := makeChatID("wc", sessionID)

	if wc.handler != nil {
		go wc.handler(IncomingMessage{
			UserID: userID,
			ChatID: chatID,
			Text:   body.Text,
		})
	}

	apiJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}
