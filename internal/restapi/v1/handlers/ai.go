package handlers

import (
	"net/http"
)

// AIHandler adapts the shared AI tier (internal/handlers.AIHandler, which
// backs the cookie-auth /api/ai/* routes) to the bearer surface. The
// implementation is injected as function values so this package does not —
// and must not — import internal/handlers (import cycle: handlers imports
// restapi).
type AIHandler struct {
	ChatFn          func(w http.ResponseWriter, r *http.Request)
	DailyBriefingFn func(w http.ResponseWriter, r *http.Request)
}

// NewAIHandler wires the shared AI handler into the bearer surface.
func NewAIHandler(chat, dailyBriefing func(w http.ResponseWriter, r *http.Request)) *AIHandler {
	return &AIHandler{ChatFn: chat, DailyBriefingFn: dailyBriefing}
}

// Chat handles POST /rest/api/v1/ai/chat
//
// @Summary      Chat with the AI assistant
// @Description  Agentic chat: the assistant can query workspaces and items via tool calls. Conversation history persists server-side per session; the durable general session is used when no session_id is given. Requires the ai:chat scope; workspace-level data access stays within the token owner's permissions.
// @Tags         ai
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body AIChatRequest true "Chat message"
// @Success      200  {object}  AIChatResponse
// @Failure      400  {object}  ErrorResponse  "Invalid request body"
// @Failure      401  {object}  ErrorResponse
// @Failure      403  {object}  ErrorResponse  "Token lacks the ai:chat scope"
// @Failure      409  {object}  ErrorResponse  "Agent session already has an active turn"
// @Failure      429  {object}  ErrorResponse  "AI rate limit exceeded"
// @Failure      500  {object}  ErrorResponse
// @Router       /ai/chat [post]
func (h *AIHandler) Chat(w http.ResponseWriter, r *http.Request) {
	if h.ChatFn == nil {
		restapiNotAvailable(w)
		return
	}
	h.ChatFn(w, r)
}

// DailyBriefing handles GET /rest/api/v1/ai/daily-briefing
//
// @Summary      Get the daily briefing
// @Description  Returns the latest successful daily briefing generated for the token owner, with item-key references resolved. Empty content means no briefing is available today. Requires the ai:read scope.
// @Tags         ai
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  AIDailyBriefingResponse
// @Failure      401  {object}  ErrorResponse
// @Failure      403  {object}  ErrorResponse  "Token lacks the ai:read scope"
// @Failure      500  {object}  ErrorResponse
// @Router       /ai/daily-briefing [get]
func (h *AIHandler) DailyBriefing(w http.ResponseWriter, r *http.Request) {
	if h.DailyBriefingFn == nil {
		restapiNotAvailable(w)
		return
	}
	h.DailyBriefingFn(w, r)
}

func restapiNotAvailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`{"error":"ai_surface_unavailable"}`))
}

// AIChatRequest mirrors the session-surface chat request.
type AIChatRequest struct {
	Message   string `json:"message"`
	SessionID int    `json:"session_id,omitempty"`
}

// AIChatResponse mirrors the session-surface chat reply.
type AIChatResponse struct {
	UserMessageID int    `json:"user_message_id"`
	MessageID     int    `json:"message_id"`
	RunID         int    `json:"run_id"`
	Answer        string `json:"answer"`
	Iterations    int    `json:"iterations"`
	StopReason    string `json:"stop_reason"`
}

// AIDailyBriefingResponse mirrors the session-surface briefing reply.
type AIDailyBriefingResponse struct {
	ID          int            `json:"id"`
	Content     string         `json:"content"`
	Date        string         `json:"date"`
	GeneratedAt string         `json:"generated_at"`
	References  map[string]any `json:"references"`
}
