package console

import (
	"bufio"
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
	return RunWithIO(controller, os.Stdin, os.Stdout)
}

func RunWithIO(controller Controller, in io.Reader, out io.Writer) ExitReason {
	reader := bufio.NewReader(in)
	activeBotID := controller.DefaultBotID()

	fmt.Fprintln(out, "\nConsole commands:")
	fmt.Fprintln(out, "  /login       - Scan QR code to add a new user/bot.")
	fmt.Fprintln(out, "  /bots        - List all logged-in bots and select active one.")
	fmt.Fprintln(out, "  /bot <num>   - Select bot by list index.")
	fmt.Fprintln(out, "  /del <num>   - Delete bot by list index.")
	fmt.Fprintln(out, "  /protection enable|disable|status - Control send protection.")
	fmt.Fprintln(out, "  /exit        - Exit this console session.")
	fmt.Fprintln(out, "  /quit        - Exit this console session.")
	fmt.Fprintln(out, "  [Text]       - Send message using active user to themselves.")

	for {
		if activeBotID == "" {
			fmt.Fprint(out, "[No Bot Selected] > ")
		} else {
			fmt.Fprintf(out, "[%s] > ", activeBotID)
		}

		text, err := reader.ReadString('\n')
		if err != nil {
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
			fmt.Fprint(out, "Enter number to select (or enter to cancel): ")
			numStr, err := reader.ReadString('\n')
			if err != nil {
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
		fmt.Fprintln(out, "Usage: /protection enable|disable|status")
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
		fmt.Fprintln(out, "Usage: /protection enable|disable|status")
	}
}
