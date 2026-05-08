// Command fynedesk-ctl is a CLI client for controlling the FyneDesk compositor
// via the UNIX socket IPC protocol.
//
// Usage:
//
//	fynedesk-ctl <command> [args...]
//
// Commands:
//
//	windows                     List all windows (JSON)
//	desktop                     Show current desktop state (JSON)
//	focus <id>                  Focus a window by ID
//	close <id>                  Close a window by ID
//	minimize <id>               Minimize a window
//	restore <id>                Restore a minimized window
//	maximize <id>               Maximize a window
//	unmaximize <id>             Unmaximize a window
//	fullscreen <id>             Fullscreen a window
//	unfullscreen <id>           Exit fullscreen
//	pin <id>                    Pin window to all desktops
//	unpin <id>                  Unpin window
//	move-to-desktop <id> <n>    Move window to desktop N
//	switch-desktop <n>          Switch to desktop N
//	lock                        Lock the screen
//	logout                      Logout / terminate compositor
//	restart                     Restart compositor
//	disable-output <name>       Disable an output (e.g. eDP-1)
//	subscribe [events...]       Subscribe and stream events (default: all)
//	config                      Show config file path and contents
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"

	"fyshos.com/fynedesk/wlipc"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// Handle commands that don't need a socket connection
	switch cmd {
	case "help", "--help", "-h":
		usage()
		return
	case "config":
		cmdConfig()
		return
	}

	client, err := wlipc.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: cannot connect to compositor: %v\n", err)
		fmt.Fprintf(os.Stderr, "Is the compositor running? Socket: %s\n", wlipc.SocketPath())
		os.Exit(1)
	}
	defer client.Close()

	switch cmd {
	case "windows", "list-windows":
		cmdListWindows(client, args)
	case "desktop", "get-desktop":
		cmdGetDesktop(client)
	case "focus":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "focus")
	case "close":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "close")
	case "minimize", "iconify":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "iconify")
	case "restore", "uniconify":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "uniconify")
	case "maximize":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "maximize")
	case "unmaximize":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "unmaximize")
	case "fullscreen":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "fullscreen")
	case "unfullscreen":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "unfullscreen")
	case "pin":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "pin")
	case "unpin":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "unpin")
	case "raise":
		requireArg(args, "window ID")
		cmdWindowAction(client, args[0], "raise")
	case "move-to-desktop":
		if len(args) < 2 {
			fatal("move-to-desktop requires <window-id> <desktop-number>")
		}
		desktop, err := strconv.Atoi(args[1])
		if err != nil {
			fatal("invalid desktop number: %s", args[1])
		}
		if desktop < 1 {
			fatal("desktop number must be >= 1")
		}
		cmdMoveToDesktop(client, args[0], desktop-1)
	case "switch-desktop":
		requireArg(args, "desktop number")
		desktop, err := strconv.Atoi(args[0])
		if err != nil {
			fatal("invalid desktop number: %s", args[0])
		}
		if desktop < 1 {
			fatal("desktop number must be >= 1")
		}
		cmdSwitchDesktop(client, desktop-1)
	case "lock":
		cmdSimple(client, wlipc.ReqLock)
	case "logout":
		cmdSimple(client, wlipc.ReqLogout)
	case "restart":
		cmdSimple(client, wlipc.ReqRestart)
	case "disable-output":
		requireArg(args, "output name")
		cmdDisableOutput(client, args[0])
	case "action":
		requireArg(args, "action name")
		cmdAction(client, args[0])
	case "subscribe":
		cmdSubscribe(client, args)
	default:
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: unknown command %q\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: fynedesk-ctl <command> [args...]

Commands:
  windows                     List all windows
  windows --json              List all windows (JSON output)
  desktop                     Show current desktop state
  focus <id>                  Focus a window
  close <id>                  Close a window
  minimize <id>               Minimize a window
  restore <id>                Restore a minimized window
  maximize <id>               Maximize a window
  unmaximize <id>             Unmaximize a window
  fullscreen <id>             Fullscreen a window
  unfullscreen <id>           Exit fullscreen
  pin <id>                    Pin window to all desktops
  unpin <id>                  Unpin window
  raise <id>                  Raise window to top
  move-to-desktop <id> <n>    Move window to desktop N (1-based)
  switch-desktop <n>          Switch to desktop N (1-based)
  disable-output <name>       Disable an output (e.g. eDP-1)
  lock                        Lock the screen
  logout                      Logout / terminate compositor
  restart                     Restart compositor
  subscribe [events...]       Subscribe and stream events (JSON lines)
  config                      Show config file path

Window IDs have the form "xdg-N" or "xway-N" (see: fynedesk-ctl windows).

Config file: ~/.config/fynedesk/config.toml (TOML, source of truth)
Legacy prefs: ~/.config/fyne/com.fyshos.fynedesk/preferences.json

Event names for subscribe:
  windows-state, desktop-state, notification, screenshot,
  clipboard-history, keyboard-layout, volume-change,
  brightness-change, launcher-request, emoji-picker,
  context-menu, clipboard-show`)
}

func requireArg(args []string, name string) {
	if len(args) < 1 {
		fatal("missing required argument: %s", name)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "fynedesk-ctl: "+format+"\n", a...)
	os.Exit(1)
}

func cmdListWindows(client *wlipc.IPCClient, args []string) {
	resp, err := client.Request(wlipc.ReqListWindows, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if resp.Name == "error" {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: %s\n", string(resp.Data))
		os.Exit(1)
	}

	// Check for --json flag
	jsonOutput := false
	for _, a := range args {
		if a == "--json" || a == "-j" {
			jsonOutput = true
		}
	}

	if jsonOutput {
		// Pretty-print JSON
		var out json.RawMessage
		if err := json.Unmarshal(resp.Data, &out); err == nil {
			pretty, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(string(resp.Data))
		}
		return
	}

	// Parse and display as table
	var state struct {
		Windows []struct {
			ID           string  `json:"id"`
			Title        string  `json:"title"`
			AppID        string  `json:"app_id"`
			Desktop      int     `json:"desktop"`
			Focused      bool    `json:"focused"`
			Iconic       bool    `json:"iconic"`
			Maximized    bool    `json:"maximized"`
			Fullscreened bool    `json:"fullscreened"`
			Pinned       bool    `json:"pinned"`
			X            float32 `json:"x"`
			Y            float32 `json:"y"`
			Width        float32 `json:"w"`
			Height       float32 `json:"h"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(resp.Data, &state); err != nil {
		// Fallback to raw JSON
		fmt.Println(string(resp.Data))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tAPP\tTITLE\tDESK\tFLAGS\tGEOMETRY")
	for _, win := range state.Windows {
		flags := ""
		if win.Focused {
			flags += "*"
		}
		if win.Iconic {
			flags += "m"
		}
		if win.Maximized {
			flags += "M"
		}
		if win.Fullscreened {
			flags += "F"
		}
		if win.Pinned {
			flags += "P"
		}
		if flags == "" {
			flags = "-"
		}

		title := win.Title
		if len(title) > 40 {
			title = title[:37] + "..."
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%.0fx%.0f+%.0f+%.0f\n",
			win.ID, win.AppID, title, win.Desktop, flags,
			win.Width, win.Height, win.X, win.Y)
	}
	w.Flush()
}

func cmdGetDesktop(client *wlipc.IPCClient) {
	resp, err := client.Request(wlipc.ReqGetDesktop, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if resp.Name == "error" {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: %s\n", string(resp.Data))
		os.Exit(1)
	}

	var state struct {
		Current  int `json:"current"`
		NumDesks int `json:"num_desks"`
	}
	if err := json.Unmarshal(resp.Data, &state); err != nil {
		fmt.Println(string(resp.Data))
		return
	}
	fmt.Printf("Desktop %d/%d\n", state.Current+1, state.NumDesks)
}

func cmdWindowAction(client *wlipc.IPCClient, windowID, action string) {
	req := struct {
		WindowID string `json:"window_id"`
		Action   string `json:"action"`
	}{WindowID: windowID, Action: action}

	resp, err := client.Request(wlipc.ReqWindowAction, req)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if resp.Name == "error" {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: %s\n", string(resp.Data))
		os.Exit(1)
	}
}

func cmdMoveToDesktop(client *wlipc.IPCClient, windowID string, desktop int) {
	req := struct {
		WindowID string `json:"window_id"`
		Action   string `json:"action"`
		Desktop  int    `json:"desktop"`
	}{WindowID: windowID, Action: "set_desktop", Desktop: desktop}

	resp, err := client.Request(wlipc.ReqWindowAction, req)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if resp.Name == "error" {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: %s\n", string(resp.Data))
		os.Exit(1)
	}
}

func cmdSwitchDesktop(client *wlipc.IPCClient, desktop int) {
	req := wlipc.DesktopSwitchRequest{Desktop: desktop}
	resp, err := client.Request(wlipc.ReqDesktopSwitch, req)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if resp.Name == "error" {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: %s\n", string(resp.Data))
		os.Exit(1)
	}
}

func cmdSimple(client *wlipc.IPCClient, reqName string) {
	resp, err := client.Request(reqName, struct{}{})
	if err != nil {
		// Connection may close immediately for logout/restart
		if reqName == wlipc.ReqLogout || reqName == wlipc.ReqRestart {
			return
		}
		fatal("request failed: %v", err)
	}
	if resp != nil && resp.Name == "error" {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: %s\n", string(resp.Data))
		os.Exit(1)
	}
}

func cmdConfig() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	configPath := filepath.Join(configDir, "fynedesk", "config.toml")

	if _, err := os.Stat(configPath); err != nil {
		fmt.Printf("Config file: %s (not yet created)\n", configPath)
		fmt.Println("The config file will be created automatically when settings are changed.")
		fmt.Println("You can also create it manually — see: fynedesk-ctl help")
		return
	}

	fmt.Printf("Config file: %s\n", configPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		fatal("could not read config: %v", err)
	}
	fmt.Println()
	fmt.Print(string(data))
}

func cmdDisableOutput(client *wlipc.IPCClient, name string) {
	req := struct {
		OutputName string `json:"output_name"`
		Position   string `json:"position"`
	}{OutputName: name, Position: "disable"}

	resp, err := client.Request(wlipc.ReqLayoutRequest, req)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if resp != nil && resp.Name == "error" {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: %s\n", string(resp.Data))
		os.Exit(1)
	}
	fmt.Printf("Output %q disabled\n", name)
}

func cmdAction(client *wlipc.IPCClient, action string) {
	req := struct {
		Action string `json:"action"`
	}{Action: action}
	resp, err := client.Request(wlipc.ReqCompositorAction, req)
	if err != nil {
		fatal("request failed: %v", err)
	}
	if resp != nil && resp.Name == "error" {
		fmt.Fprintf(os.Stderr, "fynedesk-ctl: %s\n", string(resp.Data))
		os.Exit(1)
	}
}

func cmdSubscribe(client *wlipc.IPCClient, events []string) {
	if len(events) == 0 {
		// Subscribe to all events
		events = []string{
			wlipc.EventWindowsState,
			wlipc.EventDesktopState,
			wlipc.EventNotification,
			wlipc.EventScreenshot,
			wlipc.EventClipboardHist,
			wlipc.EventKeyboardLayout,
			wlipc.EventVolumeChange,
			wlipc.EventBrightnessChange,
			wlipc.EventLauncherRequest,
			wlipc.EventEmojiPicker,
			wlipc.EventContextMenu,
			wlipc.EventClipboardShow,
			wlipc.EventCommandPalette,
		}
	}

	if err := client.Subscribe(events...); err != nil {
		fatal("subscribe failed: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Subscribed to: %v\n", events)
	fmt.Fprintf(os.Stderr, "Streaming events (Ctrl+C to stop)...\n")

	for {
		msg := client.ReadEvent()
		if msg == nil {
			fmt.Fprintln(os.Stderr, "Connection closed.")
			return
		}
		line, _ := json.Marshal(msg)
		fmt.Println(string(line))
	}
}
