package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
	"fyshos.com/fynedesk/wm"
)

var sidebar *sidebarPanel

// ToggleSidebar opens or closes the slide-out sidebar.
func ToggleSidebar() {
	if sidebar != nil {
		sidebar.close()
		return
	}

	sb := newSidebar()
	sidebar = sb
	sb.show()
}

type sidebarPanel struct {
	mu  sync.Mutex
	win fyne.Window

	notifList *fyne.Container
	scroll    *container.Scroll
	clearBtn  *widget.Button
}

func newSidebar() *sidebarPanel {
	sb := &sidebarPanel{}

	d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
	if !ok {
		return sb
	}

	win := d.CreateSplashWindow()
	win.SetTitle("Sidebar " + SkipTaskbarHint)
	win.SetPadded(false)

	win.SetOnClosed(func() {
		if sidebar == sb {
			sidebar = nil
		}
	})

	sb.win = win
	sb.buildContent()
	return sb
}

func (sb *sidebarPanel) buildContent() {
	// --- Header: date & time ---
	now := time.Now()
	timeText := canvas.NewText(now.Format("15:04"), wmtheme.ToastTitle())
	timeText.TextStyle = fyne.TextStyle{Bold: true}
	timeText.TextSize = 28
	timeText.Alignment = fyne.TextAlignCenter

	dateText := canvas.NewText(now.Format("Monday, 2 January"), wmtheme.ToastBody())
	dateText.TextSize = 13
	dateText.Alignment = fyne.TextAlignCenter

	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		sb.close()
	})
	closeBtn.Importance = widget.LowImportance

	headerRow := container.NewBorder(nil, nil, nil, closeBtn,
		container.NewVBox(
			container.NewCenter(timeText),
			container.NewCenter(dateText),
		))

	// --- Quick settings ---
	quickLabel := sectionLabel(locale.T("sidebar.quickSettings"))

	// All widgets reading system state (volume, brightness, wifi, bluetooth,
	// audio devices, app streams) are built with placeholders here. Their
	// real values are read from external tools (wpctl, brightnessctl, nmcli,
	// rfkill, pw-dump) in a background goroutine and applied via fyne.Do so
	// that an unresponsive daemon cannot freeze the panel UI thread.

	// Volume
	volIcon := widget.NewIcon(wmtheme.SoundIcon)
	volSlider := newSliderBar(100, 5, func(v float64) {
		setVolume(int(v))
	})
	volRow := container.NewBorder(nil, nil, volIcon, nil, volSlider)

	// Brightness
	brightIcon := widget.NewIcon(wmtheme.BrightnessIcon)
	brightSlider := newSliderBar(100, 5, func(v float64) {
		setBrightness(int(v))
	})
	brightRow := container.NewBorder(nil, nil, brightIcon, nil, brightSlider)

	// Night light toggle
	nightLightCheck := widget.NewCheck(locale.T("sidebar.nightLight"), func(on bool) {
		s := fynedesk.Instance().Settings()
		if ds, ok := s.(*deskSettings); ok {
			ds.setNightLightEnabled(on)
		}
	})
	nightLightCheck.Checked = fynedesk.Instance().Settings().NightLightEnabled()
	nightLightRow := container.NewBorder(nil, nil, widget.NewIcon(theme.ColorPaletteIcon()), nil, nightLightCheck)

	// Wi-Fi toggle
	wifiCheck := widget.NewCheck("Wi-Fi", func(on bool) {
		go toggleWifi(on)
	})
	wifiIconHolder := container.NewStack(widget.NewIcon(wmtheme.WifiOffIcon))
	wifiRow := container.NewBorder(nil, nil, wifiIconHolder, nil, wifiCheck)

	// Bluetooth toggle
	btCheck := widget.NewCheck(locale.T("sidebar.bluetooth"), func(on bool) {
		go toggleBluetooth(on)
	})
	btRow := container.NewBorder(nil, nil, widget.NewIcon(theme.RadioButtonIcon()), nil, btCheck)

	// Do Not Disturb toggle
	dndCheck := widget.NewCheck(locale.T("sidebar.dnd"), func(on bool) {
		wm.SetDoNotDisturb(on)
	})
	dndCheck.Checked = wm.DoNotDisturb()
	dndRow := container.NewBorder(nil, nil, widget.NewIcon(wmtheme.NotificationsIcon), nil, dndCheck)

	// Audio device selectors and per-app mixer: build empty containers that
	// will be populated by the background refresh.
	audioOutput := container.NewVBox()
	audioInput := container.NewVBox()
	appMixer := container.NewVBox()

	quickSettings := container.NewVBox(quickLabel, volRow, audioOutput, audioInput, appMixer, brightRow, nightLightRow, wifiRow, btRow, dndRow)

	go sb.refreshSystemState(volSlider, brightSlider, wifiCheck, btCheck, wifiIconHolder, audioOutput, audioInput, appMixer)

	// --- Notifications ---
	notifLabel := sectionLabel(locale.T("sidebar.notifications"))

	sb.notifList = container.NewVBox()
	sb.clearBtn = widget.NewButton(locale.T("sidebar.clearAll"), func() {
		wm.ClearNotificationHistory()
		sb.refreshNotifications()
	})
	sb.clearBtn.Importance = widget.LowImportance
	sb.refreshNotifications()

	sb.scroll = container.NewVScroll(sb.notifList)
	sb.scroll.SetMinSize(fyne.NewSize(0, 150))

	notifSection := container.NewBorder(
		container.NewVBox(notifLabel),
		container.NewCenter(sb.clearBtn),
		nil, nil,
		sb.scroll,
	)

	// --- Compose all sections ---
	sep1 := canvas.NewRectangle(wmtheme.SidebarSeparator())
	sep1.SetMinSize(fyne.NewSize(0, 1))
	sep2 := canvas.NewRectangle(wmtheme.SidebarSeparator())
	sep2.SetMinSize(fyne.NewSize(0, 1))

	content := container.NewVBox(
		headerRow,
		sep1,
		quickSettings,
		sep2,
		notifSection,
	)

	// Dark background — green tint for Matrix theme
	var bg *canvas.Rectangle
	var borderLine *canvas.Rectangle
	if isMatrixTheme() {
		bg = canvas.NewRectangle(color.NRGBA{R: 0x00, G: 0x0A, B: 0x00, A: 0xF0})
		borderLine = canvas.NewRectangle(matrixGreen)
		borderLine.SetMinSize(fyne.NewSize(2, 0))
		// Tint header text green
		timeText.Color = matrixBrightGreen
		dateText.Color = matrixGreen
	} else {
		bg = canvas.NewRectangle(wmtheme.SidebarBackground())
		borderLine = canvas.NewRectangle(wmtheme.SidebarBorder())
		borderLine.SetMinSize(fyne.NewSize(1, 0))
	}

	padded := container.NewPadded(content)
	full := container.NewStack(bg, container.NewBorder(nil, nil, borderLine, nil, padded))

	sb.win.SetContent(full)
}

// refreshSystemState reads all system state via external tools off the UI
// thread, then applies the values to the sidebar widgets via fyne.Do. This
// keeps the sidebar opening instant even when wpctl/nmcli/brightnessctl are
// unresponsive (e.g. after suspend/resume on flaky hardware) — the worst
// case becomes "values stay at their placeholder for 2s" instead of "panel
// freezes until reboot".
func (sb *sidebarPanel) refreshSystemState(
	volSlider, brightSlider *sliderBar,
	wifiCheck, btCheck *widget.Check,
	wifiIconHolder *fyne.Container,
	audioOutput, audioInput, appMixer *fyne.Container,
) {
	vol := readCurrentVolume()
	bright := readCurrentBrightness()
	wifi := isWifiEnabled()
	bt := isBluetoothEnabled()
	sinks, sinksDefault := listAudioDevices("sink")
	sources, sourcesDefault := listAudioDevices("source")
	streams := listAudioStreams()

	fyne.Do(func() {
		if sb.win == nil {
			return // sidebar closed before refresh finished
		}
		volSlider.Value = vol
		volSlider.Refresh()
		brightSlider.Value = bright
		brightSlider.Refresh()
		wifiCheck.Checked = wifi
		wifiCheck.Refresh()
		btCheck.Checked = bt
		btCheck.Refresh()

		wifiIconHolder.Objects = []fyne.CanvasObject{widget.NewIcon(wifiIconFor(wifi))}
		wifiIconHolder.Refresh()

		audioOutput.Objects = audioDeviceSelectorObjects("sink", sinks, sinksDefault)
		audioOutput.Refresh()
		audioInput.Objects = audioDeviceSelectorObjects("source", sources, sourcesDefault)
		audioInput.Refresh()
		appMixer.Objects = appMixerObjects(streams)
		appMixer.Refresh()
	})
}

func wifiIconFor(on bool) fyne.Resource {
	if on {
		return wmtheme.WifiIcon
	}
	return wmtheme.WifiOffIcon
}

func (sb *sidebarPanel) refreshNotifications() {
	sb.notifList.Objects = nil
	history := wm.NotificationHistory()

	if len(history) == 0 {
		sb.clearBtn.Hide()
		empty := widget.NewLabel(locale.T("sidebar.noNotifications"))
		empty.Alignment = fyne.TextAlignCenter
		sb.notifList.Add(container.NewCenter(empty))
	} else {
		sb.clearBtn.Show()
		for _, n := range history {
			n := n
			ts := n.Timestamp.Format("15:04")
			title := widget.NewLabel(fmt.Sprintf("%s  %s", ts, n.Title))
			title.TextStyle = fyne.TextStyle{Bold: true}
			title.Truncation = fyne.TextTruncateEllipsis

			removeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
				wm.RemoveNotification(n.ID)
				sb.refreshNotifications()
			})
			removeBtn.Importance = widget.LowImportance

			sb.notifList.Add(container.NewBorder(nil, nil, nil, removeBtn, title))
		}
	}
	sb.notifList.Refresh()
}

func (sb *sidebarPanel) show() {
	screen := fynedesk.Instance().Screens().Primary()
	screenW := float32(screen.Width) / screen.CanvasScale()
	screenH := float32(screen.Height) / screen.CanvasScale()

	sidebarW := float32(340)
	panelW := wmtheme.WidgetPanelWidth
	if fynedesk.Instance().Settings().NarrowWidgetPanel() {
		panelW = wmtheme.NarrowBarWidth
	}

	finalX := screenW - sidebarW - panelW
	startX := screenW // off-screen right

	sb.win.Resize(fyne.NewSize(sidebarW, screenH))
	wlipc.RequestOverlayPosition(sb.win.Title(), startX, 0, sidebarW, screenH)
	sb.win.Show()

	// Slide-in animation (300ms ease-out-cubic) — skip if reduce motion
	if fynedesk.Instance().Settings().ReduceMotion() {
		wlipc.RequestOverlayPosition(sb.win.Title(), finalX, 0, sidebarW, screenH)
	} else {
		go func() {
			title := sb.win.Title()
			dur := 300 * time.Millisecond
			start := time.Now()
			ticker := time.NewTicker(16 * time.Millisecond)
			defer ticker.Stop()

			for range ticker.C {
				t := float64(time.Since(start)) / float64(dur)
				if t >= 1 {
					wlipc.RequestOverlayPosition(title, finalX, 0, sidebarW, screenH)
					// Re-send final position after a delay to override any
					// compositor entrance animation that may have captured an
					// intermediate position during the window map race.
					time.AfterFunc(300*time.Millisecond, func() {
						wlipc.RequestOverlayPosition(title, finalX, 0, sidebarW, screenH)
					})
					return
				}
				x := startX + float32(easeOutCubic(t))*float32(finalX-startX)
				wlipc.RequestOverlayPosition(title, x, 0, sidebarW, screenH)
			}
		}()
	}
}

func (sb *sidebarPanel) close() {
	sb.mu.Lock()
	win := sb.win
	sb.mu.Unlock()

	if win == nil {
		return
	}

	screen := fynedesk.Instance().Screens().Primary()
	screenW := float32(screen.Width) / screen.CanvasScale()
	screenH := float32(screen.Height) / screen.CanvasScale()

	sidebarW := float32(340)
	panelW := wmtheme.WidgetPanelWidth
	if fynedesk.Instance().Settings().NarrowWidgetPanel() {
		panelW = wmtheme.NarrowBarWidth
	}

	finalX := screenW // off-screen right
	startX := screenW - sidebarW - panelW

	// Slide-out animation (250ms ease-in-cubic) — skip if reduce motion
	if fynedesk.Instance().Settings().ReduceMotion() {
		fyne.Do(func() { win.Close() })
	} else {
		go func() {
			dur := 250 * time.Millisecond
			start := time.Now()
			ticker := time.NewTicker(16 * time.Millisecond)
			defer ticker.Stop()

			for range ticker.C {
				t := float64(time.Since(start)) / float64(dur)
				if t >= 1 {
					fyne.Do(func() { win.Close() })
					return
				}
				x := startX + float32(easeInCubic(t))*float32(finalX-startX)
				wlipc.RequestOverlayPosition(win.Title(), x, 0, sidebarW, screenH)
			}
		}()
	}
}

func sectionLabel(text string) fyne.CanvasObject {
	l := canvas.NewText(text, wmtheme.SectionLabelColor())
	l.TextStyle = fyne.TextStyle{Bold: true}
	l.TextSize = 11
	return container.NewHBox(l, layout.NewSpacer())
}

// readCurrentVolume returns the current volume (0-100) via wpctl.
func readCurrentVolume() float64 {
	out, err := wm.ExecOutput("wpctl", "get-volume", "@DEFAULT_AUDIO_SINK@")
	if err != nil {
		return 50
	}
	// Format: "Volume: 0.50" or "Volume: 0.50 [MUTED]"
	line := strings.TrimSpace(string(out))
	line = strings.TrimPrefix(line, "Volume: ")
	line = strings.TrimSuffix(line, " [MUTED]")
	vol, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
	if err != nil {
		return 50
	}
	return vol * 100
}

// setVolume sets the system volume to a specific percentage.
func setVolume(pct int) {
	volStr := fmt.Sprintf("%.2f", float64(pct)/100.0)
	go func() {
		if err := wm.ExecRun("wpctl", "set-volume", "@DEFAULT_AUDIO_SINK@", volStr); err != nil {
			fyne.LogError("Failed to run command", err)
		}
	}()
}

// readCurrentBrightness returns the current brightness (0-100) via brightnessctl.
func readCurrentBrightness() float64 {
	out, err := wm.ExecOutput("brightnessctl", "info", "-m")
	if err != nil {
		return 70
	}
	// Format: "device,class,current,max,percentage"
	// e.g. "intel_backlight,backlight,500,1000,50%"
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) < 5 {
		return 70
	}
	pctStr := strings.TrimSuffix(parts[len(parts)-1], "%")
	pct, err := strconv.ParseFloat(pctStr, 64)
	if err != nil {
		return 70
	}
	return pct
}

// setBrightness sets screen brightness to a specific percentage.
func setBrightness(pct int) {
	go func() {
		if err := wm.ExecRun("brightnessctl", "set", fmt.Sprintf("%d%%", pct)); err != nil {
			fyne.LogError("Failed to run command", err)
		}
	}()
}

// isWifiEnabled checks if Wi-Fi radio is enabled via nmcli.
func isWifiEnabled() bool {
	out, err := wm.ExecOutput("nmcli", "radio", "wifi")
	if err != nil {
		return true // assume enabled if nmcli unavailable
	}
	return strings.TrimSpace(string(out)) == "enabled"
}

// toggleWifi enables or disables Wi-Fi radio.
func toggleWifi(on bool) {
	state := "off"
	if on {
		state = "on"
	}
	if err := wm.ExecRun("nmcli", "radio", "wifi", state); err != nil {
		fyne.LogError("Failed to run command", err)
	}
}

// isBluetoothEnabled checks if Bluetooth is enabled via rfkill.
func isBluetoothEnabled() bool {
	out, err := wm.ExecOutput("rfkill", "list", "bluetooth")
	if err != nil {
		// Fallback: try bluetoothctl
		out2, err2 := wm.ExecOutput("bluetoothctl", "show")
		if err2 != nil {
			return false
		}
		return strings.Contains(string(out2), "Powered: yes")
	}
	return !strings.Contains(string(out), "Soft blocked: yes")
}

// toggleBluetooth enables or disables Bluetooth.
func toggleBluetooth(on bool) {
	if on {
		if err := wm.ExecRun("rfkill", "unblock", "bluetooth"); err != nil {
			fyne.LogError("Failed to run command", err)
		}
	} else {
		if err := wm.ExecRun("rfkill", "block", "bluetooth"); err != nil {
			fyne.LogError("Failed to run command", err)
		}
	}
}

// audioDevice represents an audio sink (output) or source (input).
type audioDevice struct {
	id   string // wpctl object ID or pactl index
	name string // Human-readable description
}

// listAudioDevices queries wpctl for sinks or sources.
func listAudioDevices(kind string) (devices []audioDevice, defaultIdx int) {
	// Try wpctl first (PipeWire)
	out, err := wm.ExecOutput("wpctl", "status")
	if err != nil {
		return nil, 0
	}

	lines := strings.Split(string(out), "\n")
	var inSection bool
	sectionHeader := "Sinks:"
	if kind == "source" {
		sectionHeader = "Sources:"
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, sectionHeader) {
			inSection = true
			continue
		}
		if inSection && (trimmed == "" || (!strings.HasPrefix(trimmed, "│") && !strings.HasPrefix(trimmed, "*") && !strings.Contains(line, "."))) {
			// End of section: blank line or new section header
			if len(devices) > 0 {
				break
			}
			continue
		}
		if !inSection {
			continue
		}

		isDefault := strings.Contains(trimmed, "*")
		// Lines look like: " │  *  47. Built-in Audio Analog Stereo [vol: 0.50]"
		// or                " │     48. HDMI Audio [vol: 0.00]"
		cleaned := strings.ReplaceAll(trimmed, "│", "")
		cleaned = strings.ReplaceAll(cleaned, "*", "")
		cleaned = strings.TrimSpace(cleaned)

		// Extract ID and name: "47. Built-in Audio Analog Stereo [vol: 0.50]"
		dotIdx := strings.Index(cleaned, ".")
		if dotIdx < 1 {
			continue
		}
		id := strings.TrimSpace(cleaned[:dotIdx])
		rest := strings.TrimSpace(cleaned[dotIdx+1:])
		// Strip [vol: ...] suffix
		if bracketIdx := strings.Index(rest, "["); bracketIdx > 0 {
			rest = strings.TrimSpace(rest[:bracketIdx])
		}
		if rest == "" || id == "" {
			continue
		}

		if isDefault {
			defaultIdx = len(devices)
		}
		devices = append(devices, audioDevice{id: id, name: rest})
	}
	return devices, defaultIdx
}

// setDefaultAudioDevice sets the default sink or source via wpctl.
func setDefaultAudioDevice(id string) {
	go func() {
		if err := wm.ExecRun("wpctl", "set-default", id); err != nil {
			fyne.LogError("Failed to set default audio device", err)
		}
	}()
}

// audioDeviceSelectorObjects builds the child objects (label + dropdown)
// for an audio device selector, given pre-fetched device data. Returns nil
// when there's no meaningful choice to offer.
func audioDeviceSelectorObjects(kind string, devices []audioDevice, defaultIdx int) []fyne.CanvasObject {
	if len(devices) <= 1 {
		return nil
	}

	names := make([]string, len(devices))
	idMap := make(map[string]string)
	for i, d := range devices {
		names[i] = d.name
		idMap[d.name] = d.id
	}

	sel := widget.NewSelect(names, func(selected string) {
		if id, ok := idMap[selected]; ok {
			setDefaultAudioDevice(id)
		}
	})
	if defaultIdx < len(names) {
		sel.SetSelected(names[defaultIdx])
	}

	label := locale.T("sidebar.audioOutput")
	if kind == "source" {
		label = locale.T("sidebar.audioInput")
	}
	lbl := widget.NewLabel(label)
	lbl.TextStyle = fyne.TextStyle{Bold: true}

	return []fyne.CanvasObject{lbl, sel}
}

// audioStream represents an application audio stream.
type audioStream struct {
	index  string // PipeWire node ID or pactl sink input index
	name   string // Application name
	volume int    // Volume percentage (0-100)
}

// listAudioStreams queries PipeWire (pw-dump) for active output streams,
// falling back to pactl for PulseAudio-only setups.
func listAudioStreams() []audioStream {
	if streams := listAudioStreamsPW(); streams != nil {
		return streams
	}
	return listAudioStreamsPactl()
}

// listAudioStreamsPW uses pw-dump to list active audio output streams.
func listAudioStreamsPW() []audioStream {
	out, err := wm.ExecOutput("pw-dump")
	if err != nil {
		return nil
	}

	var nodes []json.RawMessage
	if err := json.Unmarshal(out, &nodes); err != nil {
		return nil
	}

	var streams []audioStream
	for _, raw := range nodes {
		var node struct {
			ID   int `json:"id"`
			Info struct {
				Props struct {
					MediaClass string `json:"media.class"`
					AppName    string `json:"application.name"`
				} `json:"props"`
				Params struct {
					Props []struct {
						ChannelVolumes []float64 `json:"channelVolumes"`
					} `json:"Props"`
				} `json:"params"`
			} `json:"info"`
		}
		if err := json.Unmarshal(raw, &node); err != nil {
			continue
		}
		if node.Info.Props.MediaClass != "Stream/Output/Audio" {
			continue
		}
		name := node.Info.Props.AppName
		if name == "" || name == "speech-dispatcher-dummy" {
			continue
		}

		vol := 100
		if len(node.Info.Params.Props) > 0 && len(node.Info.Params.Props[0].ChannelVolumes) > 0 {
			// PipeWire volumes are linear 0.0-1.0+; convert to percentage
			v := node.Info.Params.Props[0].ChannelVolumes[0]
			vol = int(math.Round(v * 100))
			if vol > 150 {
				vol = 150
			}
		}

		streams = append(streams, audioStream{
			index:  strconv.Itoa(node.ID),
			name:   name,
			volume: vol,
		})
	}
	return streams
}

// listAudioStreamsPactl uses pactl as fallback for PulseAudio-only systems.
func listAudioStreamsPactl() []audioStream {
	out, err := wm.ExecOutput("pactl", "list", "sink-inputs")
	if err != nil {
		return nil
	}

	var streams []audioStream
	var current audioStream
	inBlock := false

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Sink Input #") {
			if inBlock && current.name != "" {
				streams = append(streams, current)
			}
			current = audioStream{}
			current.index = strings.TrimPrefix(line, "Sink Input #")
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		if strings.HasPrefix(line, "Volume:") {
			if idx := strings.Index(line, "/"); idx >= 0 {
				rest := line[idx+1:]
				if idx2 := strings.Index(rest, "%"); idx2 >= 0 {
					pctStr := strings.TrimSpace(rest[:idx2])
					if v, err := strconv.Atoi(pctStr); err == nil {
						current.volume = v
					}
				}
			}
		}
		if strings.HasPrefix(line, "application.name = ") {
			current.name = strings.Trim(strings.TrimPrefix(line, "application.name = "), "\"")
		}
	}
	if inBlock && current.name != "" {
		streams = append(streams, current)
	}
	return streams
}

// setStreamVolume sets volume for a specific audio stream.
func setStreamVolume(index string, pct int) {
	go func() {
		// Try wpctl first (PipeWire), fallback to pactl
		vol := fmt.Sprintf("%.2f", float64(pct)/100.0)
		if err := wm.ExecRun("wpctl", "set-volume", index, vol); err != nil {
			if err2 := wm.ExecRun("pactl", "set-sink-input-volume", index, fmt.Sprintf("%d%%", pct)); err2 != nil {
				fyne.LogError("Failed to set stream volume", err2)
			}
		}
	}()
}

// appMixerObjects builds the per-app volume slider rows from a pre-fetched
// list of audio streams. Returns nil when there is nothing to show.
func appMixerObjects(streams []audioStream) []fyne.CanvasObject {
	if len(streams) == 0 {
		log.Printf("[MIXER] No audio streams found")
		return nil
	}
	log.Printf("[MIXER] Found %d audio stream(s)", len(streams))

	items := []fyne.CanvasObject{sectionLabel(locale.T("sidebar.appVolume"))}
	for _, s := range streams {
		stream := s // capture

		nameLabel := canvas.NewText(stream.name, nil)
		nameLabel.TextStyle = fyne.TextStyle{Italic: true}
		nameLabel.TextSize = 11

		slider := newSliderBar(150, 5, func(v float64) {
			setStreamVolume(stream.index, int(v))
		})
		slider.Value = float64(stream.volume)

		items = append(items, nameLabel, slider)
	}

	return items
}
