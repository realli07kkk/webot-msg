package app

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/realli07kkk/webot-msg/internal/api"
	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/console"
	"github.com/realli07kkk/webot-msg/internal/ilink"
)

type App struct {
	store  *config.Store
	client *ilink.Client

	activeMu  sync.Mutex
	activeBot string

	monitorMu       sync.Mutex
	runningMonitors map[string]struct{}
}

func New(configPath string, baseURL string) *App {
	return &App{
		store:           config.NewStore(configPath),
		client:          ilink.NewClient(baseURL),
		runningMonitors: make(map[string]struct{}),
	}
}

func (a *App) Run(port int) error {
	if err := a.store.EnsureDir(); err != nil {
		return fmt.Errorf("init config dir failed: %w", err)
	}
	if err := a.store.Load(); err != nil {
		return fmt.Errorf("load config failed: %w", err)
	}

	if a.store.Count() == 0 {
		fmt.Println("No login bots found. Starting QR Code login...")
		if err := a.Login(); err != nil {
			log.Printf("QR login failed: %v\n", err)
		}
	} else {
		fmt.Printf("Loaded %d bots.\n", a.store.Count())
	}

	if botID, ok := a.store.SingleBotID(); ok {
		a.setActiveBotID(botID)
		fmt.Printf("Auto selected single bot: %s\n", botID)
	}

	if _, err := a.store.EnsureAPITokens(config.GenerateToken); err != nil {
		return fmt.Errorf("ensure api tokens failed: %w", err)
	}

	a.handleShutdownSignal()

	for _, botID := range a.store.BotIDs() {
		a.startMonitor(botID)
	}

	apiServer := api.NewServer(a.store, a.client)
	go func() {
		if err := apiServer.Start(port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("API server stopped: %v", err)
		}
	}()

	if exitReason := console.Run(a); exitReason == console.ExitReasonCommand {
		fmt.Println("Exit requested. Saving config and exiting...")
		if err := a.store.Save(); err != nil {
			return fmt.Errorf("save config failed: %w", err)
		}
		return nil
	}

	fmt.Println("Console closed or not available. Running in background...")
	select {}
}

func (a *App) ActiveBotID() string {
	a.activeMu.Lock()
	defer a.activeMu.Unlock()
	return a.activeBot
}

func (a *App) Login() error {
	user, err := a.client.QRLogin()
	if err != nil {
		return err
	}
	if err := a.store.AddBot(*user); err != nil {
		return err
	}
	if a.store.Count() == 1 {
		a.setActiveBotID(user.BotID)
	}
	a.startMonitor(user.BotID)
	return nil
}

func (a *App) PrintBots() {
	fmt.Println("Logged in bots:")
	activeBotID := a.ActiveBotID()
	for _, entry := range a.store.ListBots() {
		mark := " "
		if entry.BotID == activeBotID {
			mark = "*"
		}
		fmt.Printf("  %d) [%s] BotID: %s  |  APIToken: %s\n", entry.Index, mark, entry.BotID, entry.User.APIToken)
	}
}

func (a *App) SelectBot(idx int) {
	for _, entry := range a.store.ListBots() {
		if entry.Index == idx {
			a.setActiveBotID(entry.BotID)
			fmt.Printf("Active bot changed to: %s\n", entry.BotID)
			return
		}
	}
	fmt.Println("Invalid bot index.")
}

func (a *App) DeleteBot(idx int) {
	botID, ok, err := a.store.DeleteBotByIndex(idx)
	if err != nil {
		fmt.Printf("Delete bot failed: %v\n", err)
		return
	}
	if !ok {
		fmt.Println("Invalid bot index.")
		return
	}

	if a.ActiveBotID() == botID {
		a.setActiveBotID("")
	}
	fmt.Printf("Bot deleted: %s\n", botID)
}

func (a *App) SendText(text string) error {
	activeBotID := a.ActiveBotID()
	user, exists := a.store.GetBot(activeBotID)
	if !exists {
		return errors.New("No active bot selected. Type '/bots' to select.")
	}

	if user.IlinkUserID == "" || user.ContextToken == "" {
		return errors.New("Active user has no message context to reply to. (Wait for one message or context is missing)")
	}

	if err := a.client.SendMessage(user, user.IlinkUserID, text, user.ContextToken); err != nil {
		return fmt.Errorf("Send failed: %w", err)
	}
	return nil
}

func (a *App) setActiveBotID(botID string) {
	a.activeMu.Lock()
	defer a.activeMu.Unlock()
	a.activeBot = botID
}

func (a *App) handleShutdownSignal() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nReceived shutdown signal. Saving config and exiting...")
		if err := a.store.Save(); err != nil {
			log.Printf("Save config failed: %v", err)
		}
		os.Exit(0)
	}()
}

func (a *App) startMonitor(botID string) {
	a.monitorMu.Lock()
	if _, exists := a.runningMonitors[botID]; exists {
		a.monitorMu.Unlock()
		return
	}
	a.runningMonitors[botID] = struct{}{}
	a.monitorMu.Unlock()

	go a.monitorWeixin(botID)
}

func (a *App) monitorWeixin(botID string) {
	defer func() {
		a.monitorMu.Lock()
		delete(a.runningMonitors, botID)
		a.monitorMu.Unlock()
	}()

	fmt.Printf("[Bot: %s] Started listening for messages...\n", botID)
	timeoutMs := 35000

	for {
		user, exists := a.store.GetBot(botID)
		if !exists {
			fmt.Printf("[Bot: %s] Stopped listening because bot was removed.\n", botID)
			return
		}

		updateRes, err := a.client.GetUpdates(user, time.Duration(timeoutMs+10000)*time.Millisecond)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if updateRes.LongpollingTimeoutMs > 0 {
			timeoutMs = updateRes.LongpollingTimeoutMs
		}

		a.persistUpdateState(botID, updateRes)
		a.printMessages(botID, updateRes.Msgs)
	}
}

func (a *App) persistUpdateState(botID string, updateRes *ilink.UpdatesResponse) {
	_, err := a.store.UpdateBot(botID, func(user *config.UserConfig) bool {
		changed := false
		if updateRes.GetUpdatesBuf != "" && user.GetUpdatesBuf != updateRes.GetUpdatesBuf {
			user.GetUpdatesBuf = updateRes.GetUpdatesBuf
			changed = true
		}

		for _, msg := range updateRes.Msgs {
			if msg.FromUserID == "" || msg.ContextToken == "" || user.ContextToken == msg.ContextToken {
				continue
			}
			user.ContextToken = msg.ContextToken
			changed = true
		}
		return changed
	})
	if err != nil {
		log.Printf("[Bot: %s] Save update state failed: %v", botID, err)
	}
}

func (a *App) printMessages(botID string, messages []ilink.WeixinMessage) {
	for _, msg := range messages {
		for _, item := range msg.ItemList {
			if item.Type == 1 && item.TextItem.Text != "" {
				fmt.Printf("\n[Bot: %s | Message from %s]: %s\n> ", botID, msg.FromUserID, item.TextItem.Text)
			} else {
				fmt.Printf("\n[Bot: %s | Message from %s]: <Media/Other type %d>\n> ", botID, msg.FromUserID, item.Type)
			}
		}
	}
}

var _ console.Controller = (*App)(nil)
