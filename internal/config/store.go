package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const DefaultPath = "./config/auth.json"

const (
	authDirPerm  os.FileMode = 0700
	authFilePerm os.FileMode = 0600
)

type UserConfig struct {
	BotToken      string `json:"bot_token"`
	BotID         string `json:"bot_id"`
	GetUpdatesBuf string `json:"get_updates_buf"`
	IlinkUserID   string `json:"ilink_user_id"`
	ContextToken  string `json:"context_token"`
	APIToken      string `json:"api_token"`
}

type AppConfig struct {
	Bots map[string]*UserConfig `json:"bots"`
}

type BotEntry struct {
	Index int
	BotID string
	User  UserConfig
}

type Store struct {
	path string
	mu   sync.Mutex
	cfg  AppConfig
}

func NewStore(path string) *Store {
	return &Store{
		path: path,
		cfg: AppConfig{
			Bots: make(map[string]*UserConfig),
		},
	}
}

func GenerateToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Store) EnsureDir() error {
	return os.MkdirAll(filepath.Dir(s.path), authDirPerm)
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.ensureBotsLocked()
			return nil
		}
		return err
	}
	if err := json.Unmarshal(data, &s.cfg); err != nil {
		return err
	}
	s.ensureBotsLocked()
	return nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.validBotIDsLocked())
}

func (s *Store) SingleBotID() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.validBotIDsLocked()
	if len(ids) != 1 {
		return "", false
	}
	return ids[0], true
}

func (s *Store) BotIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validBotIDsLocked()
}

func (s *Store) ListBots() []BotEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.validBotIDsLocked()
	entries := make([]BotEntry, 0, len(ids))
	for i, botID := range ids {
		entries = append(entries, BotEntry{
			Index: i + 1,
			BotID: botID,
			User:  *s.cfg.Bots[botID],
		})
	}
	return entries
}

func (s *Store) GetBot(botID string) (UserConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.cfg.Bots[botID]
	if !ok || user == nil {
		return UserConfig{}, false
	}
	return *user, true
}

func (s *Store) AddBot(user UserConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureBotsLocked()
	userCopy := user
	s.cfg.Bots[user.BotID] = &userCopy
	return s.saveLocked()
}

func (s *Store) DeleteBotByIndex(idx int) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.validBotIDsLocked()
	if idx < 1 || idx > len(ids) {
		return "", false, nil
	}
	botID := ids[idx-1]
	delete(s.cfg.Bots, botID)
	return botID, true, s.saveLocked()
}

func (s *Store) EnsureAPITokens(generator func(int) (string, error)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, user := range s.cfg.Bots {
		if user == nil || user.APIToken != "" {
			continue
		}
		token, err := generator(16)
		if err != nil {
			return false, err
		}
		user.APIToken = token
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, s.saveLocked()
}

func (s *Store) UpdateBot(botID string, update func(*UserConfig) bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.cfg.Bots[botID]
	if !ok || user == nil {
		return false, nil
	}
	if !update(user) {
		return false, nil
	}
	return true, s.saveLocked()
}

func (s *Store) saveLocked() error {
	s.ensureBotsLocked()
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, authFilePerm); err != nil {
		return err
	}
	return os.Chmod(s.path, authFilePerm)
}

func (s *Store) ensureBotsLocked() {
	if s.cfg.Bots == nil {
		s.cfg.Bots = make(map[string]*UserConfig)
	}
}

func (s *Store) validBotIDsLocked() []string {
	s.ensureBotsLocked()
	ids := make([]string, 0, len(s.cfg.Bots))
	for botID, user := range s.cfg.Bots {
		if botID == "" || user == nil {
			continue
		}
		ids = append(ids, botID)
	}
	sort.Strings(ids)
	return ids
}
