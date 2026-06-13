package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/realli07kkk/webot-msg/internal/api"
	"github.com/realli07kkk/webot-msg/internal/audit"
	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/console"
	"github.com/realli07kkk/webot-msg/internal/control"
	"github.com/realli07kkk/webot-msg/internal/ilink"
	"github.com/realli07kkk/webot-msg/internal/protection"
	"github.com/realli07kkk/webot-msg/internal/sender"
	"golang.org/x/term"
)

type client interface {
	QRLoginWithWriter(out io.Writer) (*config.UserConfig, error)
	GetUpdates(user config.UserConfig, timeout time.Duration) (*ilink.UpdatesResponse, error)
	SendMessageContext(ctx context.Context, user config.UserConfig, to string, text string, contextToken string) error
	SendTyping(user config.UserConfig, status int) error
	SendTypingContext(ctx context.Context, user config.UserConfig, status int) error
}

type Options struct {
	AuthPath            string
	BaseURL             string
	ControlSocketPath   string
	Guard               protection.Guard
	ProtectionConfig    protection.EnableConfig
	ProtectionEnabled   bool
	ProtectionStatePath string
	Auditor             *audit.Recorder
	AuditConfig         audit.EnableConfig
	AuditStatePath      string
	IDGenerator         sender.IDGenerator
	ReminderText        string
	TimeCheckInterval   time.Duration
}

type App struct {
	store                *config.Store
	client               client
	controlSocketPath    string
	guard                protection.Guard
	runtimeGuard         *protection.RuntimeGuard
	protectionConfig     protection.EnableConfig
	protectionEnabled    bool
	protectionStateStore *protection.StateStore
	auditor              *audit.Recorder
	auditConfig          audit.EnableConfig
	auditStateStore      *audit.StateStore
	idGenerator          sender.IDGenerator
	reminderText         string
	timeCheckInterval    time.Duration

	monitorMu                 sync.Mutex
	runningMonitors           map[string]struct{}
	runningProtectionCheckers map[string]*protectionChecker

	consoleOutputMu     sync.Mutex
	consoleOutputs      map[int]io.Writer
	nextConsoleOutputID int
}

func New(opts Options) *App {
	guard := opts.Guard
	if guard == nil {
		guard = protection.NoopGuard{}
	}
	runtimeGuard, _ := guard.(*protection.RuntimeGuard)
	if opts.TimeCheckInterval <= 0 {
		opts.TimeCheckInterval = time.Minute
	}
	auditor := opts.Auditor
	if auditor == nil {
		auditor = audit.NewRecorder()
	}
	var protectionStateStore *protection.StateStore
	if opts.ProtectionStatePath != "" {
		protectionStateStore = protection.NewStateStore(opts.ProtectionStatePath)
	}
	var auditStateStore *audit.StateStore
	if opts.AuditStatePath != "" {
		auditStateStore = audit.NewStateStore(opts.AuditStatePath)
	}
	return &App{
		store:                     config.NewStore(opts.AuthPath),
		client:                    ilink.NewClient(opts.BaseURL),
		controlSocketPath:         opts.ControlSocketPath,
		guard:                     guard,
		runtimeGuard:              runtimeGuard,
		protectionConfig:          opts.ProtectionConfig,
		protectionEnabled:         opts.ProtectionEnabled,
		protectionStateStore:      protectionStateStore,
		auditor:                   auditor,
		auditConfig:               opts.AuditConfig,
		auditStateStore:           auditStateStore,
		idGenerator:               opts.IDGenerator,
		reminderText:              opts.ReminderText,
		timeCheckInterval:         opts.TimeCheckInterval,
		runningMonitors:           make(map[string]struct{}),
		runningProtectionCheckers: make(map[string]*protectionChecker),
		consoleOutputs:            make(map[int]io.Writer),
	}
}

func (a *App) Run(port int) error {
	if err := a.store.EnsureDir(); err != nil {
		return fmt.Errorf("init config dir failed: %w", err)
	}
	if err := a.store.Load(); err != nil {
		return fmt.Errorf("load config failed: %w", err)
	}
	var restoreWarnings io.Writer
	if isTerminal(os.Stderr) {
		restoreWarnings = os.Stderr
	}
	a.restoreProtectionState(restoreWarnings)
	a.restoreAuditState(restoreWarnings)

	if a.store.Count() == 0 {
		if isTerminal(os.Stdin) {
			fmt.Println("No login bots found. Starting QR Code login...")
			if _, err := a.Login(os.Stdout); err != nil {
				log.Printf("QR login failed: %v\n", err)
			}
		} else {
			fmt.Printf("No login bots found. Connect to unix://%s with socat or nc and run /login.\n", a.controlSocketPath)
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

	apiServer := api.NewServerWithClientOptions(a.store, a.client, a.guard, a.reminderText, sender.TextOptions{
		IDGenerator: a.idGenerator,
		Auditor:     a.auditor,
	})
	go func() {
		if err := apiServer.Start(port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("API server stopped: %v", err)
		}
	}()

	if console.Run(a) == console.ExitReasonInterrupt {
		fmt.Println("\nReceived interrupt. Saving config and exiting...")
		if err := a.store.Save(); err != nil {
			return fmt.Errorf("save config failed: %w", err)
		}
		return nil
	}

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
	a.stopProtectionChecker(botID)
	fmt.Fprintf(out, "Bot deleted: %s\n", botID)
	return botID, true
}

func (a *App) EnableProtection(out io.Writer) error {
	if a.runtimeGuard == nil {
		return errors.New("runtime protection guard is not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.runtimeGuard.Enable(ctx, a.protectionConfig); err != nil {
		return err
	}
	a.protectionEnabled = true
	for _, botID := range a.store.BotIDs() {
		a.startProtectionChecker(botID)
	}
	fmt.Fprintf(out, "Protection enabled. Redis key prefix: %s\n", a.protectionConfig.KeyPrefix)
	a.persistProtectionState(out, true)
	return nil
}

func (a *App) DisableProtection(out io.Writer) error {
	if a.runtimeGuard == nil {
		return errors.New("runtime protection guard is not available")
	}
	a.stopProtectionCheckers()
	a.runtimeGuard.Disable()
	a.protectionEnabled = false
	fmt.Fprintln(out, "Protection disabled.")
	a.persistProtectionState(out, false)
	return nil
}

func (a *App) persistProtectionState(out io.Writer, enabled bool) {
	if a.protectionStateStore == nil {
		return
	}
	if err := a.protectionStateStore.Save(protection.PersistedState{ProtectionEnabled: enabled}); err != nil {
		message := fmt.Sprintf("persist protection state failed: %v", err)
		log.Print(message)
		fmt.Fprintf(out, "Warning: %s\n", message)
	}
}

func (a *App) restoreProtectionState(warnOut io.Writer) {
	if a.protectionStateStore == nil {
		return
	}

	state, err := a.protectionStateStore.Load()
	if err != nil {
		a.warnProtectionRestore(warnOut, "protection state file is unreadable; starting with protection disabled: %v", err)
		return
	}
	if !state.ProtectionEnabled {
		return
	}
	if a.runtimeGuard == nil {
		a.warnProtectionRestore(warnOut, "protection auto-restore failed; runtime protection guard is not available")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.runtimeGuard.Enable(ctx, a.protectionConfig); err != nil {
		a.warnProtectionRestore(warnOut, "protection auto-restore failed; protection remains disabled. Run /protection enable after fixing Redis: %v", err)
		return
	}
	a.protectionEnabled = true
	log.Printf("Protection auto-restore succeeded. Redis key prefix: %s", a.protectionConfig.KeyPrefix)
}

func (a *App) warnProtectionRestore(out io.Writer, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	log.Print(message)
	if out != nil {
		fmt.Fprintf(out, "Warning: %s\n", message)
	}
}

func (a *App) PrintProtectionStatus(activeBotID string, out io.Writer) {
	if a.runtimeGuard == nil {
		fmt.Fprintln(out, "Protection status unavailable: runtime protection guard is not available")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if a.runtimeGuard.Enabled() && activeBotID == "" {
		a.printProtectionStatus(protection.Status{
			Enabled:         true,
			RedisConfigured: strings.TrimSpace(a.protectionConfig.RedisURL) != "",
		}, out)
		return
	}
	status, err := a.runtimeGuard.RuntimeStatus(ctx, activeBotID)
	if err != nil {
		fmt.Fprintf(out, "Protection status unavailable: %v\n", err)
		return
	}
	status.RedisConfigured = strings.TrimSpace(a.protectionConfig.RedisURL) != ""
	a.printProtectionStatus(status, out)
}

func (a *App) printProtectionStatus(status protection.Status, out io.Writer) {
	if !status.Enabled {
		fmt.Fprintln(out, "Protection disabled.")
		fmt.Fprintf(out, "Redis configured: %s\n", yesNo(status.RedisConfigured))
		return
	}

	fmt.Fprintln(out, "Protection enabled.")
	fmt.Fprintf(out, "Redis configured: %s\n", yesNo(status.RedisConfigured))
	if status.BotID == "" {
		fmt.Fprintln(out, "No active bot selected. Type '/bots' to select.")
		return
	}

	fmt.Fprintf(out, "Bot: %s\n", status.BotID)
	if status.Frozen {
		fmt.Fprintf(out, "Frozen: yes (%s)\n", status.Reason)
	} else {
		fmt.Fprintln(out, "Frozen: no")
	}
	if !status.ActiveWindowReady {
		fmt.Fprintln(out, "Active window: not ready; send a message from WeChat app before continuing.")
		return
	}
	fmt.Fprintf(out, "Messages before reminder: %d\n", status.MessagesBeforeReminder)
	fmt.Fprintf(out, "Active window remaining: %s\n", formatStatusDuration(status.ActiveWindowRemaining))
	fmt.Fprintf(out, "Time before warning: %s\n", formatStatusDuration(status.TimeBeforeWarning))
	if status.ReminderPending {
		fmt.Fprintln(out, "Reminder pending: yes")
	}
}

func (a *App) EnableAudit(out io.Writer) error {
	if a.auditor == nil {
		return errors.New("runtime audit recorder is not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.auditor.Enable(ctx, a.auditConfig); err != nil {
		return err
	}
	if err := a.persistAuditState(true); err != nil {
		message := fmt.Sprintf("audit enabled for current process, but persist audit state failed; restart will not restore audit automatically: %v", err)
		log.Print(message)
		return errors.New(message)
	}
	fmt.Fprintf(out, "Audit enabled. Redis key prefix: %s\n", a.auditConfig.KeyPrefix)
	return nil
}

func (a *App) DisableAudit(out io.Writer) error {
	if a.auditor == nil {
		return errors.New("runtime audit recorder is not available")
	}
	a.auditor.Disable()
	if err := a.persistAuditState(false); err != nil {
		message := fmt.Sprintf("audit disabled for current process, but persist audit state failed; restart may restore audit automatically: %v", err)
		log.Print(message)
		return errors.New(message)
	}
	fmt.Fprintln(out, "Audit disabled.")
	return nil
}

func (a *App) persistAuditState(enabled bool) error {
	if a.auditStateStore == nil {
		return nil
	}
	if err := a.auditStateStore.Save(audit.PersistedState{AuditEnabled: enabled}); err != nil {
		return err
	}
	return nil
}

func (a *App) restoreAuditState(warnOut io.Writer) {
	if a.auditStateStore == nil {
		return
	}

	state, err := a.auditStateStore.Load()
	if err != nil {
		a.warnAuditRestore(warnOut, "audit state file is unreadable; starting with audit disabled: %v", err)
		return
	}
	if !state.AuditEnabled {
		return
	}
	if a.auditor == nil {
		a.warnAuditRestore(warnOut, "audit auto-restore failed; runtime audit recorder is not available")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.auditor.Enable(ctx, a.auditConfig); err != nil {
		a.warnAuditRestore(warnOut, "audit auto-restore failed; audit remains disabled. Run /audit enable after fixing Redis: %v", err)
		return
	}
	log.Printf("Audit auto-restore succeeded. Redis key prefix: %s", a.auditConfig.KeyPrefix)
}

func (a *App) warnAuditRestore(out io.Writer, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	log.Print(message)
	if out != nil {
		fmt.Fprintf(out, "Warning: %s\n", message)
	}
}

func (a *App) PrintAuditStatus(out io.Writer) {
	if a.auditor == nil {
		fmt.Fprintln(out, "Audit status unavailable: runtime audit recorder is not available")
		return
	}
	if a.auditor.Enabled() {
		fmt.Fprintln(out, "Audit enabled.")
	} else {
		fmt.Fprintln(out, "Audit disabled.")
	}
	fmt.Fprintf(out, "Redis configured: %s\n", yesNo(strings.TrimSpace(a.auditConfig.RedisURL) != ""))
	fmt.Fprintf(out, "Time TTL: %s\n", formatStatusDuration(a.auditConfig.TimeTTL))
	fmt.Fprintf(out, "Body TTL: %s\n", formatStatusDuration(a.auditConfig.BodyTTL))
}

func (a *App) SendText(botID string, text string) error {
	user, exists := a.store.GetBot(botID)
	if !exists {
		return errors.New("No active bot selected. Type '/bots' to select.")
	}

	if user.IlinkUserID == "" || user.ContextToken == "" {
		return errors.New("Active user has no message context to reply to. (Wait for one message or context is missing)")
	}

	_, err := sender.SendProtectedTextWithOptions(context.Background(), a.client, a.protectionGuard(), user, text, a.reminderText, sender.TextOptions{
		IDGenerator: a.idGenerator,
		Auditor:     a.auditor,
	})
	if err != nil {
		if protection.IsRejection(err) {
			return errors.New(protection.RejectionMessage(protection.RejectionReason(err)))
		}
		return err
	}
	return nil
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func formatStatusDuration(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	return value.Round(time.Second).String()
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
		if err := console.RestoreActiveTerminal(); err != nil {
			log.Printf("Restore terminal failed: %v", err)
		}
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
	a.startProtectionChecker(botID)
}

func (a *App) startProtectionChecker(botID string) {
	if !a.protectionIsEnabled() {
		return
	}

	a.monitorMu.Lock()
	if a.runningProtectionCheckers == nil {
		a.runningProtectionCheckers = make(map[string]*protectionChecker)
	}
	if _, exists := a.runningProtectionCheckers[botID]; exists {
		a.monitorMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	checker := &protectionChecker{cancel: cancel}
	a.runningProtectionCheckers[botID] = checker
	a.monitorMu.Unlock()

	go a.monitorProtectionWindow(ctx, botID, checker)
}

func (a *App) monitorProtectionWindow(ctx context.Context, botID string, checker *protectionChecker) {
	defer func() {
		a.monitorMu.Lock()
		if a.runningProtectionCheckers[botID] == checker {
			delete(a.runningProtectionCheckers, botID)
		}
		a.monitorMu.Unlock()
	}()

	ticker := time.NewTicker(a.timeCheckInterval)
	defer ticker.Stop()

	for {
		if !a.checkProtectionTimeWindowOnce(botID) {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (a *App) stopProtectionChecker(botID string) {
	a.monitorMu.Lock()
	checker := a.runningProtectionCheckers[botID]
	delete(a.runningProtectionCheckers, botID)
	a.monitorMu.Unlock()
	if checker != nil && checker.cancel != nil {
		checker.cancel()
	}
}

func (a *App) stopProtectionCheckers() {
	a.monitorMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(a.runningProtectionCheckers))
	for botID, checker := range a.runningProtectionCheckers {
		if checker != nil && checker.cancel != nil {
			cancels = append(cancels, checker.cancel)
		}
		delete(a.runningProtectionCheckers, botID)
	}
	a.monitorMu.Unlock()

	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
}

type protectionChecker struct {
	cancel context.CancelFunc
}

func (a *App) protectionIsEnabled() bool {
	if a.runtimeGuard != nil {
		return a.runtimeGuard.Enabled()
	}
	return a.protectionEnabled
}

func (a *App) checkProtectionTimeWindowOnce(botID string) bool {
	user, exists := a.store.GetBot(botID)
	if !exists {
		return false
	}

	ctx := context.Background()
	operation := protection.BeginOperation(a.protectionGuard())
	defer operation.Done()

	decision, err := operation.CheckTimeWindow(ctx, botID)
	if err != nil {
		log.Printf("[Bot: %s] Protection time window check failed: %v", botID, err)
		return true
	}
	if decision.Kind == protection.DecisionSendReminderAndFreeze {
		if _, err := sender.SendProtectionReminder(ctx, a.client, operation, user, a.reminderText, decision.Reason); err != nil {
			log.Printf("[Bot: %s] Protection reminder record failed: %v", botID, err)
		}
	}
	return true
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
	activeConversation := false
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
			activeConversation = true
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
	if activeConversation {
		if err := a.protectionGuard().RecordActiveConversation(context.Background(), botID); err != nil {
			log.Printf("[Bot: %s] Protection active conversation reset failed: %v", botID, err)
		}
	}
}

func (a *App) protectionGuard() protection.Guard {
	if a.guard == nil {
		return protection.NoopGuard{}
	}
	return a.guard
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
