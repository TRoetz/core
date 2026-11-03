package handlers

// AI-assisted suggestions (WI-6.5): advisory sentiment and health rationale
// for one stakeholder, generated from the live register. Off by default — the
// call only succeeds once an LLM connection is configured and resolves for
// the "sms_assist" feature; nothing is written back to the register.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"windshift/internal/llm"
)

type smsAIAssistResult struct {
	SentimentSuggestion string  `json:"sentiment_suggestion"` // positive | neutral | negative
	Confidence          float64 `json:"confidence"`           // 0..1
	HealthRationale     string  `json:"health_rationale"`
}

// SetLLMManager wires the LLM connection manager for AI assist (WI-6.5).
// Without it, GET /sms/stakeholders/{id}/ai-assist answers 503.
func (h *SMSHandler) SetLLMManager(m llmFeatureResolver) { h.llmManager = m }

// AIAssist returns advisory sentiment + health suggestions for one stakeholder.
// Optional ?connection_id= overrides the feature's default connection when the
// feature is in default mode (see requireLLMClientForFeature).
func (h *SMSHandler) AIAssist(w http.ResponseWriter, r *http.Request) {
	user, ok := RequireAuth(w, r)
	if !ok {
		return
	}
	id, ok := requireIDParam(w, r, "id")
	if !ok {
		return
	}
	if h.llmManager == nil {
		respondServiceUnavailable(w, r, "AI assist is not configured.")
		return
	}
	client := requireLLMClientForFeature(w, r, h.llmManager, "sms_assist", parseConnectionIDParam(r))
	if client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), llm.DefaultRequestTimeout)
	defer cancel()
	extendWriteDeadline(w)

	s, err := h.smsGetStakeholder(ctx, id)
	if isNotFound(err) {
		respondNotFound(w, r, "stakeholder")
		return
	}
	if err != nil {
		respondInternalError(w, r, err)
		return
	}

	var commLines []string
	rows, err := h.db.QueryContext(ctx, `SELECT subject, sentiment FROM sms_communications WHERE stakeholder_id = ? ORDER BY created_at DESC LIMIT 5`, id)
	if err != nil {
		respondInternalError(w, r, err)
		return
	}
	for rows.Next() {
		var subject, sentiment string
		if err := rows.Scan(&subject, &sentiment); err == nil {
			line := "- " + strings.TrimSpace(subject)
			if sentiment != "" {
				line += fmt.Sprintf(" [recorded sentiment: %s]", sentiment)
			}
			commLines = append(commLines, line)
		}
	}
	rows.Close()

	var openIssues, openCommitments int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_issues WHERE stakeholder_id = ? AND status = 'open'`, id).Scan(&openIssues); err != nil {
		respondInternalError(w, r, err)
		return
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_commitments WHERE stakeholder_id = ? AND status = 'open'`, id).Scan(&openCommitments); err != nil {
		respondInternalError(w, r, err)
		return
	}

	systemPrompt := "You are a stakeholder relationship analyst. Based on the provided register data, suggest the current sentiment of this stakeholder and explain the health of the relationship. Respond with ONLY a JSON object: {\"sentiment_suggestion\": \"positive\" | \"neutral\" | \"negative\", \"confidence\": number between 0 and 1, \"health_rationale\": string of at most three sentences}."
	userPrompt := fmt.Sprintf(
		"Stakeholder profile:\n- name: %s\n- title / organization: %s / %s\n- relationship_status: %s\n- trust_level: %s\n- engagement_status: %s\n- open issues: %d\n- open commitments: %d\nRecent communications (newest first):\n%s",
		s.Name, s.Title, s.Organization, s.RelationshipStatus, orDash(s.TrustLevel), s.EngagementStatus,
		openIssues, openCommitments, joinOrNone(commLines))

	result, err := llm.CompleteStructured[smsAIAssistResult](ctx, client, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.2,
	})
	if err != nil {
		respondLLMError(w, r, err)
		return
	}

	smsAudit(h, r, user, "sms_ai_assist_generated", "sms_stakeholder", &id, s.Name, map[string]any{
		"sentiment": result.SentimentSuggestion,
	})
	respondJSONOK(w, map[string]any{
		"stakeholder_id":     id,
		"sentiment_suggestion": result.SentimentSuggestion,
		"confidence":         result.Confidence,
		"health_rationale":   result.HealthRationale,
		"generated_at":       time.Now().UTC().Format(time.RFC3339),
	})
}

func orDash(v *string) string {
	if v == nil || *v == "" {
		return "(unset)"
	}
	return *v
}

func joinOrNone(lines []string) string {
	if len(lines) == 0 {
		return "- (none recorded)"
	}
	return strings.Join(lines, "\n")
}
