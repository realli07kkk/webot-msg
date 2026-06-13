package console

type CommandSpec struct {
	Name        string
	Usage       string
	Subcommands []string
}

var commandSpecs = []CommandSpec{
	{
		Name:  "/login",
		Usage: "/login       - Scan QR code to add a new user/bot.",
	},
	{
		Name:  "/bots",
		Usage: "/bots        - List all logged-in bots and select active one.",
	},
	{
		Name:  "/bot",
		Usage: "/bot <num>   - Select bot by list index.",
	},
	{
		Name:  "/del",
		Usage: "/del <num>   - Delete bot by list index.",
	},
	{
		Name:        "/protection",
		Usage:       "/protection enable|disable|status - Control send protection.",
		Subcommands: []string{"enable", "disable", "status"},
	},
	{
		Name:        "/audit",
		Usage:       "/audit enable|disable|status - Control message audit.",
		Subcommands: []string{"enable", "disable", "status"},
	},
	{
		Name:  "/exit",
		Usage: "/exit        - Exit this console session.",
	},
	{
		Name:  "/quit",
		Usage: "/quit        - Exit this console session.",
	},
}

func findCommandSpec(name string) (CommandSpec, bool) {
	for _, spec := range commandSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return CommandSpec{}, false
}

func protectionUsage() string {
	if spec, ok := findCommandSpec("/protection"); ok {
		return "Usage: " + spec.Name + " " + joinWords(spec.Subcommands, "|")
	}
	return "Usage: /protection enable|disable|status"
}

func auditUsage() string {
	if spec, ok := findCommandSpec("/audit"); ok {
		return "Usage: " + spec.Name + " " + joinWords(spec.Subcommands, "|")
	}
	return "Usage: /audit enable|disable|status"
}

func joinWords(words []string, sep string) string {
	if len(words) == 0 {
		return ""
	}
	result := words[0]
	for _, word := range words[1:] {
		result += sep + word
	}
	return result
}
