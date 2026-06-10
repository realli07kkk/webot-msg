package console

import (
	"bufio"
	"fmt"
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
	ActiveBotID() string
	Login() error
	PrintBots()
	SelectBot(idx int)
	DeleteBot(idx int)
	SendText(text string) error
}

func Run(controller Controller) ExitReason {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\nConsole commands:")
	fmt.Println("  /login       - Scan QR code to add a new user/bot.")
	fmt.Println("  /bots        - List all logged-in bots and select active one.")
	fmt.Println("  /bot <num>   - Select bot by list index.")
	fmt.Println("  /del <num>   - Delete bot by list index.")
	fmt.Println("  /exit        - Save config and exit.")
	fmt.Println("  /quit        - Save config and exit.")
	fmt.Println("  [Text]       - Send message using active user to themselves.")

	for {
		if activeBotID := controller.ActiveBotID(); activeBotID == "" {
			fmt.Print("[No Bot Selected] > ")
		} else {
			fmt.Printf("[%s] > ", activeBotID)
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
			if err := controller.Login(); err != nil {
				fmt.Printf("QR login failed: %v\n", err)
			}
			continue
		}

		if text == "/bots" {
			controller.PrintBots()
			fmt.Print("Enter number to select (or enter to cancel): ")
			numStr, err := reader.ReadString('\n')
			if err != nil {
				return ExitReasonInputClosed
			}
			numStr = strings.TrimSpace(numStr)
			if numStr != "" {
				if idx, err := strconv.Atoi(numStr); err == nil {
					controller.SelectBot(idx)
				}
			}
			continue
		}

		if strings.HasPrefix(text, "/bot ") {
			parts := strings.Fields(text)
			if len(parts) > 1 {
				if idx, err := strconv.Atoi(parts[1]); err == nil {
					controller.SelectBot(idx)
				}
			}
			continue
		}

		if strings.HasPrefix(text, "/del ") {
			parts := strings.Fields(text)
			if len(parts) > 1 {
				if idx, err := strconv.Atoi(parts[1]); err == nil {
					controller.DeleteBot(idx)
				}
			}
			continue
		}

		if strings.HasPrefix(text, "/") {
			fmt.Println("Command not recognized, treating as text msg...")
		}

		if err := controller.SendText(text); err != nil {
			fmt.Println(err)
		} else {
			fmt.Println("Send success!")
		}
	}
}
