package app

import (
	"errors"
	"fmt"
	"io"
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
	"github.com/realli07kkk/webot-msg/internal/control"
	"github.com/realli07kkk/webot-msg/internal/ilink"
	"golang.org/x/term"
)

type App struct {
	store             *config.Store
	client            *ilink.Client
	controlSocketPath string

	monitorMu       sync.Mutex
	runningMonitors map[string]struct{}

	consoleOutputMu     sync.Mutex
	consoleOutputs      map[int]io.Writer
	nextConsoleOutputID int
}

func New(configPath string, baseURL string, controlSocketPath string) *App {
	return &App{
		store:             config.NewStore(configPath),
		client:            ilink.NewClient(baseURL),
		controlSocketPath: controlSocketPath,
		runningMonitors:   make(map[string]struct{}),
		consoleOutputs:    make(map[int]io.Writer),
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
		if isTerminal(os.Stdin) {
			fmt.Println("No login bots found. Starting QR Code login...")
			if _, err := a.Login(os.Stdout); err != nil {
				log.Printf("QR login failed: %v\n", err)
			}
		} else {
			fmt.Println("No login bots found. Use 'webot-msg console' to open a control console and run /login.")
		}
	} else {
		fmt.Printf("Loaded %d bots.\n", a.store.Count())
	}

	if botID, ok := a.store.SingleBotID(); ok {
		fmt.Printf("Auto selected single bot: %s\n", botID)
	}

	if _, err := a.store.EnsureAPITokens(config.GenerateToken); err != nil {
		return fmt.Errorf("ensure api tokens failed: %w", err)
	}

	a.handleShutdownSignal()

	controlServer := control.NewServer(a.controlSocketPath, a)
	if err := controlServer.Start(); err != nil {
		return fmt.Errorf("start control console failed: %w", err)
	}
	fmt.Printf("Control console listening on unix://%s\n", a.controlSocketPath)

	for _, botID := range a.store.BotIDs() {
		a.startMonitor(botID)
	}

	apiServer := api.NewServer(a.store, a.client)
	go func() {
		if err := apiServer.Start(port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("API server stopped: %v", err)
		}
	}()

	console.Run(a)

	fmt.Println("Console closed. Service continues running. Use systemd or Ctrl+C to stop the process.")
	select {}
}

func (a *App) DefaultBotID() string {
	botID, _ := a.store.SingleBotID()
	return botID
}

func (a *App) Login(out io.Writer) (string, error) {
	user, err := a.client.QRLoginWithWriter(out)
	if err != nil {
		return "", err
	}
	if err := a.store.AddBot(*user); err != nil {
		return "", err
	}
	a.startMonitor(user.BotID)
	return user.BotID, nil
}

func (a *App) PrintBots(activeBotID string, out io.Writer) {
	fmt.Fprintln(out, "Logged in bots:")
	for _, entry := range a.store.ListBots() {
		mark := " "
		if entry.BotID == activeBotID {
			mark = "*"
		}
		fmt.Fprintf(out, "  %d) [%s] BotID: %s  |  APIToken: %s\n", entry.Index, mark, entry.BotID, entry.User.APIToken)
	}
}

func (a *App) SelectBot(idx int, out io.Writer) (string, bool) {
	for _, entry := range a.store.ListBots() {
		if entry.Index == idx {
			fmt.Fprintf(out, "Active bot changed to: %s\n", entry.BotID)
			return entry.BotID, true
		}
	}
	fmt.Fprintln(out, "Invalid bot index.")
	return "", false
}

func (a *App) DeleteBot(idx int, out io.Writer) (string, bool) {
	botID, ok, err := a.store.DeleteBotByIndex(idx)
	if err != nil {
		fmt.Fprintf(out, "Delete bot failed: %v\n", err)
		return "", false
	}
	if !ok {
		fmt.Fprintln(out, "Invalid bot index.")
		return "", false
	}
	fmt.Fprintf(out, "Bot deleted: %s\n", botID)
	return botID, true
}

func (a *App) SendText(botID string, text string) error {
	user, exists := a.store.GetBot(botID)
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

func (a *App) AddConsoleOutput(out io.Writer) func() {
	if out == nil {
		return func() {}
	}

	a.consoleOutputMu.Lock()
	if a.consoleOutputs == nil {
		a.consoleOutputs = make(map[int]io.Writer)
	}
	id := a.nextConsoleOutputID
	a.nextConsoleOutputID++
	a.consoleOutputs[id] = out
	a.consoleOutputMu.Unlock()

	return func() {
		a.consoleOutputMu.Lock()
		delete(a.consoleOutputs, id)
		a.consoleOutputMu.Unlock()
	}
}

func (a *App) broadcastConsoleOutput(text string) {
	a.consoleOutputMu.Lock()
	outputs := make([]io.Writer, 0, len(a.consoleOutputs))
	for _, out := range a.consoleOutputs {
		outputs = append(outputs, out)
	}
	a.consoleOutputMu.Unlock()

	for _, out := range outputs {
		if _, err := io.WriteString(out, text); err != nil {
			log.Printf("Control console message broadcast failed: %v", err)
		}
	}
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
	lastUpdateErr := ""
	var lastUpdateErrAt time.Time

	for {
		user, exists := a.store.GetBot(botID)
		if !exists {
			fmt.Printf("[Bot: %s] Stopped listening because bot was removed.\n", botID)
			return
		}

		updateRes, err := a.client.GetUpdates(user, time.Duration(timeoutMs+10000)*time.Millisecond)
		if err != nil {
			errText := err.Error()
			if errText != lastUpdateErr || time.Since(lastUpdateErrAt) >= time.Minute {
				log.Printf("[Bot: %s] Get updates failed: %v", botID, err)
				lastUpdateErr = errText
				lastUpdateErrAt = time.Now()
			}
			time.Sleep(2 * time.Second)
			continue
		}
		lastUpdateErr = ""
		lastUpdateErrAt = time.Time{}

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
			if msg.FromUserID == "" || msg.ContextToken == "" {
				continue
			}
			if user.IlinkUserID != msg.FromUserID {
				user.IlinkUserID = msg.FromUserID
				changed = true
			}
			if user.ContextToken != msg.ContextToken {
				user.ContextToken = msg.ContextToken
				changed = true
			}
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
			var output string
			if item.Type == 1 && item.TextItem.Text != "" {
				output = fmt.Sprintf("\n[Bot: %s | Message from %s]: %s\n> ", botID, msg.FromUserID, item.TextItem.Text)
			} else {
				output = fmt.Sprintf("\n[Bot: %s | Message from %s]: <Media/Other type %d>\n> ", botID, msg.FromUserID, item.Type)
			}
			fmt.Print(output)
			a.broadcastConsoleOutput(output)
		}
	}
}

var _ console.Controller = (*App)(nil)

func isTerminal(file *os.File) bool {
	return term.IsTerminal(int(file.Fd()))
}
