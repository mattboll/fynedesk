package launcher

import (
	_ "embed" // embed icon
	"os/exec"
	"strconv"
	"strings"

	"codeberg.org/sdassow/unyts/units"
	"fyne.io/fyne/v2"
	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
)

var unytsMeta = tyde.ModuleMetadata{
	Name:        "Launcher: Convert units",
	NewInstance: newUnyts,
}

//go:embed unyts.png
var resourceUnytsPngData []byte

var resourceUnyts = &fyne.StaticResource{
	StaticName:    "unyts.png",
	StaticContent: resourceUnytsPngData,
}

type unyts struct{}

func (u *unyts) Destroy() {
}

func (u *unyts) LaunchSuggestions(input string) []tyde.LaunchSuggestion {
	if u.isConversion(input) {
		return []tyde.LaunchSuggestion{&unytResult{input}}
	}

	return nil
}

func (u *unyts) Metadata() tyde.ModuleMetadata {
	return unytsMeta
}

func (u *unyts) isConversion(input string) bool {
	parts := strings.Split(input, " ")
	if len(parts) != 3 {
		return false
	}

	pivot := strings.ToLower(parts[1])
	if pivot != "in" && pivot != "as" && pivot != "to" {
		return false
	}

	// a matching string, is it a valid conversion
	_, _, err := units.ParseAmountAndUnit(parts[0])
	return err == nil
}

// newURLs creates a new module that will show URLs in the launcher suggestions
func newUnyts() tyde.Module {
	return &unyts{}
}

type unytResult struct {
	conversion string
}

func (r *unytResult) Icon() fyne.Resource {
	return resourceUnyts
}

func (r *unytResult) Title() string {
	parts := strings.Split(r.conversion, " ")
	src, unit, err := units.ParseAmountAndUnit(parts[0])
	if err != nil {
		return err.Error()
	}

	lookup := units.FuncMapForAbbr(unit.Abbr)
	conv, ok := lookup[parts[2]]
	if !ok {
		return locale.Tf("launcher.cannotConvert", unit.Abbr, parts[2])
	}

	return r.conversion + " = " + strconv.FormatFloat(conv(src), 'f', 1, 64)
}

func (r *unytResult) Launch() {
	cmd := exec.Command("unyts", "-c", r.conversion)
	if err := cmd.Start(); err != nil {
		fyne.LogError("Failed to launch unyts", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}
