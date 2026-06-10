package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/ilink"
)

type Server struct {
	store  *config.Store
	client *ilink.Client
}

func NewServer(store *config.Store, client *ilink.Client) *Server {
	return &Server{
		store:  store,
		client: client,
	}
}

func (s *Server) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/bots/", s.handleBotAction)

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("API Server listening on http://0.0.0.0%s\n", addr)
	return http.ListenAndServe(addr, mux)
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
	if err := s.client.SendMessage(user, user.IlinkUserID, text, user.ContextToken); err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{"code": 500, "error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"code": 200, "message": "OK"})
}

func (s *Server) handleTyping(w http.ResponseWriter, r *http.Request, user config.UserConfig, jsonBody map[string]interface{}) {
	statusStr := getReqParam(r, "status", jsonBody)
	status, _ := strconv.Atoi(statusStr)
	if status == 0 {
		status = 1
	}
	if err := s.client.SendTyping(user, status); err != nil {
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
