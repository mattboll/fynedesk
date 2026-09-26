// Package phone connects Android phones for debugging over Wi-Fi, without
// Android Studio: the phone scans a QR code (Developer options > Wireless
// debugging > Pair device with QR code), it is found on the local network
// (mDNS), paired and connected with adb. Phones paired once are connected
// again whenever they show up on the network.
package phone

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// The services a phone announces while wireless debugging is on.
const (
	PairingService = "_adb-tls-pairing._tcp"
	ConnectService = "_adb-tls-connect._tcp"
)

// Pairing is what the QR code tells the phone: the name to announce itself
// with, and the code to pair with.
type Pairing struct {
	Name, Code string
}

// NewPairing makes a pairing with a random name and code.
func NewPairing() Pairing {
	return Pairing{Name: "tyde-" + randomString(8), Code: randomString(10)}
}

// QR returns the text of the QR code the phone scans.
func (p Pairing) QR() string {
	return "WIFI:T:ADB;S:" + p.Name + ";P:" + p.Code + ";;"
}

func randomString(n int) string {
	const letters = "abcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

// Service is a service a phone announces on the network.
type Service struct {
	Instance string // its name: the pairing name, or the phone's guid
	Addr     string // host:port
}

// Browser finds services announced on the local network.
type Browser interface {
	// Browse calls found for each service of the kind announced, until ctx
	// is done.
	Browse(ctx context.Context, service string, found func(Service)) error
}

// Device is a phone adb knows.
type Device struct {
	Serial string // ip:port for a phone connected over Wi-Fi
	State  string // "device" when usable
	Model  string
}

// Wireless reports whether the phone is connected over the network.
func (d Device) Wireless() bool {
	return strings.Contains(d.Serial, ":") || strings.Contains(d.Serial, "._adb-tls-connect.")
}

// ADB runs adb.
type ADB struct {
	Path string // "adb" in $PATH if empty
}

func (a ADB) run(ctx context.Context, args ...string) (string, error) {
	path := a.Path
	if path == "" {
		path = "adb"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

var pairedGUID = regexp.MustCompile(`Successfully paired to \S+ \[guid=([^\]]+)\]`)

// Pair pairs with the phone announcing a pairing service at addr; it returns
// the phone's guid, the name of its connect service.
func (a ADB) Pair(ctx context.Context, addr, code string) (string, error) {
	out, err := a.run(ctx, "pair", addr, code)
	if m := pairedGUID.FindStringSubmatch(out); m != nil {
		return m[1], nil
	}
	if err == nil {
		err = errors.New("adb pair failed")
	}
	return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(out))
}

// Connect connects to the phone at addr.
func (a ADB) Connect(ctx context.Context, addr string) error {
	out, err := a.run(ctx, "connect", addr)
	if strings.Contains(out, "connected to") && !strings.Contains(out, "failed") {
		return nil
	}
	if err == nil {
		err = errors.New("adb connect failed")
	}
	return fmt.Errorf("%w: %s", err, strings.TrimSpace(out))
}

// Disconnect disconnects the phone connected as serial.
func (a ADB) Disconnect(ctx context.Context, serial string) error {
	_, err := a.run(ctx, "disconnect", serial)
	return err
}

// Devices lists the phones adb knows.
func (a ADB) Devices(ctx context.Context) ([]Device, error) {
	out, err := a.run(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	return parseDevices(out), nil
}

// parseDevices reads the output of adb devices -l.
func parseDevices(out string) []Device {
	var devices []Device
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(line, "List of") || strings.HasPrefix(line, "*") {
			continue
		}
		d := Device{Serial: fields[0], State: fields[1]}
		for _, f := range fields[2:] {
			if model, ok := strings.CutPrefix(f, "model:"); ok {
				d.Model = strings.ReplaceAll(model, "_", " ")
			}
		}
		devices = append(devices, d)
	}
	return devices
}

// Step is how far a pairing went.
type Step int

const (
	StepWaiting    Step = iota // for the phone to scan the code
	StepPairing                // found: pairing
	StepConnecting             // paired: connecting
	StepConnected
)

// Pair waits for the phone that scanned the code of p, pairs with it and
// connects to it. It tells each step to progress and returns the phone's
// guid and the address it is connected at.
func Pair(ctx context.Context, adb ADB, b Browser, p Pairing, progress func(Step)) (guid, addr string, err error) {
	progress(StepWaiting)
	pairAt, err := waitFor(ctx, b, PairingService, func(s Service) bool { return s.Instance == p.Name })
	if err != nil {
		return "", "", err
	}
	progress(StepPairing)
	guid, err = adb.Pair(ctx, pairAt.Addr, p.Code)
	if err != nil {
		return "", "", err
	}
	progress(StepConnecting)
	at, err := waitFor(ctx, b, ConnectService, func(s Service) bool { return s.Instance == guid })
	if err != nil {
		return guid, "", err
	}
	if err := adb.Connect(ctx, at.Addr); err != nil {
		return guid, "", err
	}
	progress(StepConnected)
	return guid, at.Addr, nil
}

// waitFor returns the first service of the kind that matches.
func waitFor(ctx context.Context, b Browser, service string, match func(Service) bool) (Service, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	found := make(chan Service, 1)
	errc := make(chan error, 1)
	go func() {
		errc <- b.Browse(ctx, service, func(s Service) {
			if match(s) {
				select {
				case found <- s:
				default:
				}
				cancel()
			}
		})
	}()
	select {
	case s := <-found:
		return s, nil
	case <-ctx.Done():
		select {
		case s := <-found:
			return s, nil
		default:
		}
		return Service{}, ctx.Err()
	case err := <-errc:
		select {
		case s := <-found:
			return s, nil
		default:
		}
		if err == nil {
			err = errors.New("the search on the network stopped")
		}
		return Service{}, err
	}
}

// keepRetry is how long Keep first waits to search the network again after
// the search failed (doubling up to keepRetryMax): at login the network is
// often not up yet.
var keepRetry, keepRetryMax = 5 * time.Second, 2 * time.Minute

// Keep connects again the paired phones (their guids, from known) whenever
// they show up on the network, until ctx is done. A search that fails is
// started again.
func Keep(ctx context.Context, adb ADB, b Browser, known func() []string, connected func(guid, addr string)) error {
	last := map[string]time.Time{}
	wait := keepRetry
	for {
		err := b.Browse(ctx, ConnectService, func(s Service) {
			if !contains(known(), s.Instance) || time.Since(last[s.Instance]) < 30*time.Second {
				return
			}
			last[s.Instance] = time.Now()
			if err := adb.Connect(ctx, s.Addr); err == nil {
				connected(s.Instance, s.Addr)
			}
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			log.Printf("[phone] searching the network: %v (again in %s)", err, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, keepRetryMax)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
