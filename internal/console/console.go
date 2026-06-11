package console

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type ExitReason int

const (
	ExitReasonInputClosed ExitReason = iota
	ExitReasonCommand
	ExitReasonInterrupt
)

type Controller interface {
	DefaultBotID() string
	Login(out io.Writer) (string, error)
	PrintBots(activeBotID string, out io.Writer)
	SelectBot(idx int, out io.Writer) (string, bool)
	DeleteBot(idx int, out io.Writer) (string, bool)
	EnableProtection(out io.Writer) error
	DisableProtection(out io.Writer) error
	PrintProtectionStatus(activeBotID string, out io.Writer)
	SendText(botID string, text string) error
}

func Run(controller Controller) ExitReason {
	if reader, ok := NewLocalTerminalLineReader(os.Stdin, os.Stdout); ok {
		return RunWithLineReader(controller, reader, reader)
	}
	return RunWithIO(controller, os.Stdin, os.Stdout)
}

func RunWithIO(controller Controller, in io.Reader, out io.Writer) ExitReason {
	return RunWithLineReader(controller, NewBufferedLineReader(in, out), out)
}

func RunWithLineReader(controller Controller, reader LineReader, out io.Writer) ExitReason {
	defer reader.Close()

	activeBotID := controller.DefaultBotID()

	fmt.Fprintln(out, "\nConsole commands:")
	for _, spec := range commandSpecs {
		fmt.Fprintf(out, "  %s\n", spec.Usage)
	}
	fmt.Fprintln(out, "  [Text]       - Send message using active user to themselves.")

	for {
		prompt := ""
		if activeBotID == "" {
			prompt = "[No Bot Selected] > "
		} else {
			prompt = fmt.Sprintf("[%s] > ", activeBotID)
		}

		text, err := reader.ReadLine(prompt)
		if err != nil {
			if errors.Is(err, ErrInterrupted) {
				return ExitReasonInterrupt
			}
			return ExitReasonInputClosed
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		if text == "/exit" || text == "/quit" {
			return ExitReasonCommand
		}

		if text == "/login" {
			botID, err := controller.Login(out)
			if err != nil {
				fmt.Fprintf(out, "QR login failed: %v\n", err)
			} else {
				activeBotID = botID
				fmt.Fprintf(out, "Active bot changed to: %s\n", botID)
			}
			continue
		}

		if text == "/bots" {
			controller.PrintBots(activeBotID, out)
			numStr, err := reader.ReadLine("Enter number to select (or enter to cancel): ")
			if err != nil {
				if errors.Is(err, ErrInterrupted) {
					return ExitReasonInterrupt
				}
				return ExitReasonInputClosed
			}
			numStr = strings.TrimSpace(numStr)
			if numStr != "" {
				if idx, err := strconv.Atoi(numStr); err == nil {
					if botID, ok := controller.SelectBot(idx, out); ok {
						activeBotID = botID
					}
				}
			}
			continue
		}

		if strings.HasPrefix(text, "/bot ") {
			parts := strings.Fields(text)
			if len(parts) > 1 {
				if idx, err := strconv.Atoi(parts[1]); err == nil {
					if botID, ok := controller.SelectBot(idx, out); ok {
						activeBotID = botID
					}
				}
			}
			continue
		}

		if strings.HasPrefix(text, "/del ") {
			parts := strings.Fields(text)
			if len(parts) > 1 {
				if idx, err := strconv.Atoi(parts[1]); err == nil {
					if botID, ok := controller.DeleteBot(idx, out); ok && botID == activeBotID {
						activeBotID = ""
					}
				}
			}
			continue
		}

		if text == "/protection" || strings.HasPrefix(text, "/protection ") {
			handleProtectionCommand(text, controller, activeBotID, out)
			continue
		}

		if strings.HasPrefix(text, "/") {
			fmt.Fprintln(out, "Command not recognized, treating as text msg...")
		}

		if err := controller.SendText(activeBotID, text); err != nil {
			fmt.Fprintln(out, err)
		} else {
			fmt.Fprintln(out, "Send success!")
		}
	}
}

func handleProtectionCommand(text string, controller Controller, activeBotID string, out io.Writer) {
	parts := strings.Fields(text)
	if len(parts) != 2 {
		fmt.Fprintln(out, protectionUsage())
		return
	}

	switch parts[1] {
	case "enable":
		if err := controller.EnableProtection(out); err != nil {
			fmt.Fprintf(out, "Enable protection failed: %v\n", err)
		}
	case "disable":
		if err := controller.DisableProtection(out); err != nil {
			fmt.Fprintf(out, "Disable protection failed: %v\n", err)
		}
	case "status":
		controller.PrintProtectionStatus(activeBotID, out)
	default:
		fmt.Fprintln(out, protectionUsage())
	}
}
