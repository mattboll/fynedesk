package phone

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
)

// KDE Connect links a phone with its app (Android, iOS): its battery, its
// notifications (which kdeconnectd shows as notifications of the desktop,
// with their replies), files and the clipboard shared, the phone made to
// ring. Tyde talks to kdeconnectd on the session bus; the first call starts
// it.
const (
	kdeService   = "org.kde.kdeconnect"
	kdeDaemon    = "/modules/kdeconnect"
	kdeDevices   = "/modules/kdeconnect/devices/"
	kdeDaemonIf  = "org.kde.kdeconnect.daemon"
	kdeDeviceIf  = "org.kde.kdeconnect.device"
	kdeBatteryIf = "org.kde.kdeconnect.device.battery"
)

// KDEDevice is a device KDE Connect knows.
type KDEDevice struct {
	ID, Name, Type string
	Reachable      bool
	Paired         bool
	PairRequested  bool // by the phone: to accept or refuse
	PairPending    bool // by us: to accept on the phone
	Battery        int  // percent; -1 when unknown
	Charging       bool
}

// KDEConnect talks to kdeconnectd.
type KDEConnect struct {
	conn *dbus.Conn
}

// NewKDEConnect connects to the session bus.
func NewKDEConnect() (*KDEConnect, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	return &KDEConnect{conn: conn}, nil
}

// newKDEConnectOn talks to kdeconnectd over conn (tests).
func newKDEConnectOn(conn *dbus.Conn) *KDEConnect {
	return &KDEConnect{conn: conn}
}

// Close closes the connection.
func (k *KDEConnect) Close() error {
	return k.conn.Close()
}

func devicePath(id string) dbus.ObjectPath {
	return dbus.ObjectPath(kdeDevices + id)
}

func (k *KDEConnect) object(path dbus.ObjectPath) dbus.BusObject {
	return k.conn.Object(kdeService, path)
}

// Devices lists the devices KDE Connect knows: the paired ones, and those
// in reach that could be.
func (k *KDEConnect) Devices(ctx context.Context) ([]KDEDevice, error) {
	var ids []string
	if err := k.object(kdeDaemon).CallWithContext(ctx, kdeDaemonIf+".devices", 0, false, false).Store(&ids); err != nil {
		return nil, err
	}
	var devices []KDEDevice
	for _, id := range ids {
		d, err := k.device(ctx, id)
		if err != nil {
			continue // gone meanwhile
		}
		if d.Paired || d.Reachable {
			devices = append(devices, d)
		}
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Paired != devices[j].Paired {
			return devices[i].Paired
		}
		return devices[i].Name < devices[j].Name
	})
	return devices, nil
}

func (k *KDEConnect) device(ctx context.Context, id string) (KDEDevice, error) {
	var props map[string]dbus.Variant
	err := k.object(devicePath(id)).CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, kdeDeviceIf).Store(&props)
	if err != nil {
		return KDEDevice{}, err
	}
	d := KDEDevice{ID: id, Battery: -1}
	get := func(name string, into any) {
		if v, ok := props[name]; ok {
			_ = v.Store(into)
		}
	}
	get("name", &d.Name)
	get("type", &d.Type)
	get("isReachable", &d.Reachable)
	get("isPaired", &d.Paired)
	get("isPairRequestedByPeer", &d.PairRequested)
	get("isPairRequested", &d.PairPending)
	if d.Paired && d.Reachable {
		var battery map[string]dbus.Variant
		err := k.object(devicePath(id)+"/battery").CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, kdeBatteryIf).Store(&battery)
		if err == nil {
			if v, ok := battery["charge"]; ok {
				_ = v.Store(&d.Battery)
			}
			if v, ok := battery["isCharging"]; ok {
				_ = v.Store(&d.Charging)
			}
		}
	}
	return d, nil
}

func (k *KDEConnect) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) error {
	return k.object(path).CallWithContext(ctx, method, 0, args...).Err
}

// RequestPairing asks the phone to pair: it is accepted on the phone.
func (k *KDEConnect) RequestPairing(ctx context.Context, id string) error {
	return k.call(ctx, devicePath(id), kdeDeviceIf+".requestPairing")
}

// AcceptPairing accepts the pairing the phone asked for.
func (k *KDEConnect) AcceptPairing(ctx context.Context, id string) error {
	return k.call(ctx, devicePath(id), kdeDeviceIf+".acceptPairing")
}

// CancelPairing refuses or cancels a pairing.
func (k *KDEConnect) CancelPairing(ctx context.Context, id string) error {
	return k.call(ctx, devicePath(id), kdeDeviceIf+".cancelPairing")
}

// Unpair forgets a paired phone.
func (k *KDEConnect) Unpair(ctx context.Context, id string) error {
	return k.call(ctx, devicePath(id), kdeDeviceIf+".unpair")
}

// Ring makes the phone ring, to find it.
func (k *KDEConnect) Ring(ctx context.Context, id string) error {
	return k.call(ctx, devicePath(id)+"/findmyphone", "org.kde.kdeconnect.device.findmyphone.ring")
}

// ShareFiles sends files to the phone.
func (k *KDEConnect) ShareFiles(ctx context.Context, id string, paths []string) error {
	urls := make([]string, len(paths))
	for i, p := range paths {
		urls[i] = (&url.URL{Scheme: "file", Path: p}).String()
	}
	return k.call(ctx, devicePath(id)+"/share", "org.kde.kdeconnect.device.share.shareUrls", urls)
}

// SendClipboard sends the clipboard to the phone.
func (k *KDEConnect) SendClipboard(ctx context.Context, id string) error {
	return k.call(ctx, devicePath(id)+"/clipboard", "org.kde.kdeconnect.device.clipboard.sendClipboard")
}

// MountFiles mounts the files of the phone and returns where.
func (k *KDEConnect) MountFiles(ctx context.Context, id string) (string, error) {
	sftp := k.object(devicePath(id) + "/sftp")
	var ok bool
	if err := sftp.CallWithContext(ctx, "org.kde.kdeconnect.device.sftp.mountAndWait", 0).Store(&ok); err != nil {
		return "", err
	}
	if !ok {
		var reason string
		_ = sftp.CallWithContext(ctx, "org.kde.kdeconnect.device.sftp.getMountError", 0).Store(&reason)
		return "", fmt.Errorf("the files of the phone could not be mounted: %s", reason)
	}
	var point string
	err := sftp.CallWithContext(ctx, "org.kde.kdeconnect.device.sftp.mountPoint", 0).Store(&point)
	return point, err
}

// Watch calls changed whenever something of KDE Connect changes (a device,
// its battery, a pairing), until ctx is done.
func (k *KDEConnect) Watch(ctx context.Context, changed func()) error {
	match := []dbus.MatchOption{dbus.WithMatchSender(kdeService)}
	if err := k.conn.AddMatchSignalContext(ctx, match...); err != nil {
		return err
	}
	defer func() { _ = k.conn.RemoveMatchSignal(match...) }()
	signals := make(chan *dbus.Signal, 16)
	k.conn.Signal(signals)
	defer k.conn.RemoveSignal(signals)
	for {
		select {
		case <-ctx.Done():
			return nil
		case s, ok := <-signals:
			if !ok {
				return nil
			}
			if strings.HasPrefix(string(s.Path), kdeDaemon) {
				changed()
			}
		}
	}
}
