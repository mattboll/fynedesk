package phone

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// KnownPhone is a phone paired once.
type KnownPhone struct {
	GUID  string `json:"guid"` // the name of its connect service
	Model string `json:"model,omitempty"`
}

// Known holds the paired phones, kept in a file.
type Known struct {
	path   string
	mu     sync.Mutex
	phones []KnownPhone
}

// LoadKnown reads the paired phones kept at path.
func LoadKnown(path string) *Known {
	k := &Known{path: path}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &k.phones)
	}
	return k
}

// GUIDs returns the guids of the paired phones.
func (k *Known) GUIDs() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	guids := make([]string, len(k.phones))
	for i, p := range k.phones {
		guids[i] = p.GUID
	}
	return guids
}

// Add records a paired phone, or its new model, and writes the file.
func (k *Known) Add(guid, model string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	i := slices.IndexFunc(k.phones, func(p KnownPhone) bool { return p.GUID == guid })
	if i < 0 {
		k.phones = append(k.phones, KnownPhone{GUID: guid, Model: model})
	} else if model != "" {
		k.phones[i].Model = model
	}
	data, err := json.MarshalIndent(k.phones, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return err
	}
	tmp := k.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, k.path)
}
