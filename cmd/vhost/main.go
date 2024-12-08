package main

import (
	"fmt"
	"io"
	lnxconfig "ipstack_p1/pkg/Inxconfig"
	"ipstack_p1/pkg/ip"
	"ipstack_p1/pkg/tcp"
	"log"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/peterh/liner"
)

func main() {
	// The first argument should be '--config' followed by the config file
	if os.Args[1] != "--config" {
		fmt.Println("Usage: ./vhost --config <configfile>")
		os.Exit(1)
	}
	configFile := os.Args[2]

	// Determine whether to run as 'vhost' or 'vrouter' based on the executable name
	cmd := "vhost"
	execName := os.Args[0]
	if strings.Contains(execName, "vrouter") {
		cmd = "vrouter"
	}

	// Initialize the configuration
	if err := initializeConfig(configFile, cmd); err != nil {
		log.Println(err)
		os.Exit(1)
	}

	// log.Println("Configuration loaded. You can enter commands like 'li', 'ln', 'lr', 'down <ifname>', etc.")

	// Start the REPL to accept commands
	handleCommands()
}

func initializeConfig(fileName string, cmd string) error {
	// TODO: Process the .linx file here
	// You can parse the .linx file and initialize lnxConfig
	// For now, we'll leave it as a placeholder
	lnxConfig, err := lnxconfig.ParseConfig(fileName)
	if err != nil {
		log.Printf("Error in parsing Lnxconfig file: %v\n", err)
		return err
	}

	// Initialize the IP stack with the configuration
	ip.IPStack_Init(lnxConfig)
	tcp.TcpStack_Init()

	// Start the appropriate IP stack based on the command
	if cmd == "vhost" {
		ip.IPStack_Start_Host()
	} else if cmd == "vrouter" {
		ip.IPStack_Start_Router()
	}

	return nil
}

func handleCommands() {
	line := liner.NewLiner()
	defer line.Close()

	// Enable Ctrl+C to abort the prompt
	line.SetCtrlCAborts(true)

	// Set up auto-completion
	line.SetCompleter(func(input string) []string {
		commands := []string{
			// "li", "ln", "lr", "down", "up", "send", "quit", "exit",
		}
		var completions []string
		for _, cmd := range commands {
			if strings.HasPrefix(cmd, strings.ToLower(input)) {
				completions = append(completions, cmd)
			}
		}
		return completions
	})

	for {
		input, err := line.Prompt("> ")
		if err != nil {
			if err == io.EOF {
				log.Println("\nExiting.")
				break
			}
			if err == liner.ErrPromptAborted {
				log.Println("\nAborted")
				continue
			}
			log.Printf("Error reading line: %v\n", err)
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Execute the command
		if shouldExit := executeCommand(input); shouldExit {
			break
		}
	}
}

func executeCommand(line string) bool {
	tokens := strings.Fields(line)
	if len(tokens) == 0 {
		return false
	}
	cmd := tokens[0]
	args := tokens[1:]

	switch cmd {
	case "li":
		fmt.Println(ip.ListInterface_Call())
	case "ln":
		fmt.Println(ip.ListNeighbor_Call())
	case "lr":
		fmt.Println(ip.ListRouter_Call())
	case "ls":
		fmt.Println(tcp.ListSockets_Call())

	case "down":
		if len(args) != 1 {
			log.Println("Usage: down <ifname>")
			return false
		}
		ip.Down_Call(args[0])

	case "up":
		if len(args) != 1 {
			log.Println("Usage: up <ifname>")
			return false
		}
		ip.Up_Call(args[0])

	case "send":
		if len(args) < 2 {
			log.Println("Usage: send <addr> <message>")
			return false
		}
		ip.Send_TestPkg_Call(args[0], strings.Join(args[1:], " "))

	case "a":
		if len(args) != 1 {
			log.Println("Usage: a <port>")
			return false
		}
		num, err := strconv.Atoi(args[0])
		if err != nil {
			log.Println(err)
			return false
		}
		go tcp.ACommand_Call(uint16(num))

	case "c":
		if len(args) != 2 {
			log.Println("Usage: c <addr> <port>")
			return false
		}
		num, err := strconv.Atoi(args[1])
		if err != nil {
			log.Println(err)
			return false
		}
		go tcp.CCommand_Call(args[0], uint16(num))
	case "s":
		if len(args) < 2 {
			log.Println("Usage: s <socket ID> <bytes>")
			return false
		}
		socketID, err := strconv.Atoi(args[0])
		if err != nil {
			log.Println("Invalid socket ID")
			return false
		}
		data := args[1]

		tcp.SCommand_call(socketID, []byte(data))
	case "r":
		if len(args) != 2 {
			log.Println("Usage: r <socket ID> <numbytes>")
			return false
		}
		socketID, err := strconv.Atoi(args[0])
		if err != nil {
			log.Println("Invalid socket ID")
			return false
		}
		numBytes, err := strconv.Atoi(args[1])
		if err != nil {
			log.Println("Invalid number of bytes")
			return false
		}

		tcp.RCommand_call(socketID, numBytes)

	case "cl":
		if len(args) != 1 {
			log.Println("Usage: cl <socket ID>")
			return false
		}
		socketID, err := strconv.Atoi(args[0])
		if err != nil {
			log.Println("Invalid socket ID")
			return false
		}
		tcp.CLCommand_Call(socketID)

	case "sf":
		if len(args) < 3 {
			log.Println("Usage: sf <file path> <ip addr> <port>")
			return false
		}

		ipAddr, err := netip.ParseAddr(args[1])
		if err != nil {
			log.Printf("ERROR: Failed to parse IP address %v\n", err)
			return false
		}

		portNum, err := strconv.Atoi(args[2])
		if err != nil {
			log.Printf("ERROR: Failed to get port number %v\n", err)
			return false
		}

		filePath := args[0]
		tcp.SFCommand_Call(ipAddr, portNum, filePath)

	case "rf":
		if len(args) != 2 {
			log.Println("Usage: rf <dest file path> <port>")
			return false
		}

		portNum, err := strconv.Atoi(args[1])
		if err != nil {
			log.Printf("ERROR: Failed to get port number %v\n", err)
			return false
		}

		filePath := args[0]
		tcp.RFCommand_Call(portNum, filePath)

	case "p":
		if len(args) != 1 {
			log.Println("Usage: p <socket ID>")
			return false
		}

		socketID, err := strconv.Atoi(args[0])
		if err != nil {
			log.Println("Invalid socket ID")
			return false
		}

		tcp.PCommand_call(socketID)

	case "quit", "exit":
		// Exit the REPL
		log.Println("Exiting.")
		return true
	default:
		log.Printf("Unknown command: %s\n", cmd)
	}
	return false
}
