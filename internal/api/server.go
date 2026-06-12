package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/ilink"
	"github.com/realli07kkk/webot-msg/internal/protection"
	"github.com/realli07kkk/webot-msg/internal/sender"
)

type messageClient interface {
	SendMessageContext(ctx context.Context, user config.UserConfig, to string, text string, contextToken string) error
	SendTypingContext(ctx context.Context, user config.UserConfig, status int) error
}

type Server struct {
	store        *config.Store
	client       messageClient
	guard        protection.Guard
	reminderText string
}

func NewServer(store *config.Store, client *ilink.Client, guard protection.Guard, reminderText string) *Server {
	return NewServerWithClient(store, client, guard, reminderText)
}

func NewServerWithClient(store *config.Store, client messageClient, guard protection.Guard, reminderText string) *Server {
	if guard == nil {
		guard = protection.NoopGuard{}
	}
	return &Server{
		store:        store,
		client:       client,
		guard:        guard,
		reminderText: reminderText,
	}
}

func (s *Server) Start(port int) error {
	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("API Server listening on http://0.0.0.0%s\n", addr)
	return http.ListenAndServe(addr, s.handler())
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/bots/", s.handleBotAction)
	return instrumentedHandler(mux)
}

type originalRequestKey struct{}

func instrumentedHandler(next http.Handler) http.Handler {
	traced := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original, ok := r.Context().Value(originalRequestKey{}).(*http.Request)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, original.Clone(r.Context()))
	}), "webot-msg.api")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sanitized := r.Clone(context.WithValue(r.Context(), originalRequestKey{}, r))
		urlCopy := *r.URL
		urlCopy.RawQuery = ""
		sanitized.URL = &urlCopy
		sanitized.RequestURI = urlCopy.RequestURI()
		traced.ServeHTTP(w, sanitized)
	})
}

func (s *Server) handleBotAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/bots/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{"code": 404, "error": "Not Found"})
		return
	}

	botID := parts[0]
	action := parts[1]

	jsonBody, ok := parseRequestParams(w, r)
	if !ok {
		return
	}

	token := ""
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		token = getReqParam(r, "token", jsonBody)
	}

	user, exists := s.store.GetBot(botID)
	if !exists {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{"code": 404, "error": "Bot not found"})
		return
	}
	if user.APIToken != token || token == "" {
		sendJSON(w, http.StatusUnauthorized, map[string]interface{}{"code": 401, "error": "Unauthorized"})
		return
	}

	switch action {
	case "messages":
		s.handleSendMessage(w, r, user, jsonBody)
	case "typing":
		s.handleTyping(w, r, user, jsonBody)
	default:
		sendJSON(w, http.StatusNotFound, map[string]interface{}{"code": 404, "error": "Unknown action"})
	}
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request, user config.UserConfig, jsonBody map[string]interface{}) {
	text := getReqParam(r, "text", jsonBody)
	if text == "" {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{"code": 400, "error": "Missing text"})
		return
	}
	if user.IlinkUserID == "" || user.ContextToken == "" {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{"code": 400, "error": "Context not ready"})
		return
	}

	if _, err := sender.SendProtectedText(r.Context(), s.client, s.guard, user, text, s.reminderText); err != nil {
		if protection.IsRejection(err) {
			reason := protection.RejectionReason(err)
			s.sendProtectionLocked(w, protection.RejectionMessage(reason), reason)
			return
		}
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{"code": 500, "error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"code": 200, "message": "OK"})
}

func (s *Server) sendProtectionLocked(w http.ResponseWriter, message string, reason string) {
	data := map[string]interface{}{"code": http.StatusTooManyRequests, "error": message}
	if reason != "" {
		data["reason"] = reason
	}
	sendJSON(w, http.StatusTooManyRequests, data)
}

func (s *Server) handleTyping(w http.ResponseWriter, r *http.Request, user config.UserConfig, jsonBody map[string]interface{}) {
	statusStr := getReqParam(r, "status", jsonBody)
	status, _ := strconv.Atoi(statusStr)
	if status == 0 {
		status = 1
	}
	if err := s.client.SendTypingContext(r.Context(), user, status); err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{"code": 500, "error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"code": 200, "message": "OK"})
}

func parseRequestParams(w http.ResponseWriter, r *http.Request) (map[string]interface{}, bool) {
	jsonBody := make(map[string]interface{})
	contentType := r.Header.Get("Content-Type")

	switch {
	case strings.Contains(contentType, "application/json"):
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"code": 400, "error": "Invalid request body"})
			return nil, false
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &jsonBody); err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]interface{}{"code": 400, "error": "Invalid JSON body"})
				return nil, false
			}
		}
	case strings.Contains(contentType, "multipart/form-data"):
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"code": 400, "error": "Invalid multipart form"})
			return nil, false
		}
	default:
		if err := r.ParseForm(); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"code": 400, "error": "Invalid form"})
			return nil, false
		}
	}

	return jsonBody, true
}

func getReqParam(r *http.Request, key string, jsonBody map[string]interface{}) string {
	if val, ok := jsonBody[key]; ok {
		return fmt.Sprint(val)
	}
	return r.FormValue(key)
}

func sendJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
