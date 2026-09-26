package fyles

import (
	"image/color"
	"log"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"

	lib "github.com/FyshOS/fyles/pkg/fyles"
	"golang.org/x/sys/execabs"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wm"
)

var fylesMeta = tyde.ModuleMetadata{
	Name:        "Desktop Files",
	NewInstance: newFyles,
}

type fyles struct {
	icons *lib.Panel
}

func (f *fyles) Destroy() {
}

func (f *fyles) ScreenAreaWidget() fyne.CanvasObject {
	holder := container.NewPadded()
	win := tyde.Instance().Root()
	go func() {
		for win == nil {
			time.Sleep(10 * time.Millisecond)
			win = tyde.Instance().Root()
		}
		icons := lib.NewFylesPanel(f.tapped, win)
		icons.HideParent = true
		icons.Filter = filterHidden()
		list := desktopListing()
		f.icons = icons

		fyne.Do(func() {
			if list != nil {
				icons.SetListing(list)
			}

			holder.Objects = []fyne.CanvasObject{icons}
			holder.Refresh()
		})
	}()

	desk := tyde.Instance()
	var barPad fyne.CanvasObject
	if desk.Settings().BarPosition() == "left" {
		r := canvas.NewRectangle(color.Transparent)
		r.SetMinSize(fyne.NewSize(wmtheme.NarrowBarWidth, 1))
		barPad = r
	}

	rightIndent := wmtheme.WidgetPanelWidth
	if desk.Settings().NarrowWidgetPanel() {
		rightIndent = wmtheme.NarrowBarWidth
	}
	widgetPad := canvas.NewRectangle(color.Transparent)
	widgetPad.SetMinSize(fyne.NewSize(rightIndent, 1))

	return container.NewBorder(nil, nil, barPad, widgetPad, holder)
}

func (f *fyles) Metadata() tyde.ModuleMetadata {
	return fylesMeta
}

// desktopListing reads the desktop directory and returns the items to show,
// the shortcuts we always offer first. Call this from a goroutine as it can be slow.
// Returns nil if the directory could not be read.
func desktopListing() []fyne.URI {
	home, _ := os.UserHomeDir()
	u := storage.NewFileURI(filepath.Join(home, "Desktop"))
	homeDir := newCustomURI("file://"+home, locale.T("fyles.home"), theme.FolderIcon())
	settings := newCustomURI("settings://", locale.T("settings.settings"), theme.SettingsIcon())
	trash := newCustomURI("file://"+filepath.Join(home, ".local", "share", "Trash", "files"), locale.T("fyles.trash"), theme.DeleteIcon())

	list, err := storage.List(u)
	if err != nil {
		fyne.LogError("Could not read Desktop dir", err)
		return nil
	}

	return append([]fyne.URI{homeDir, trash, settings}, list...)
}

func (f *fyles) tapped(u fyne.URI) {
	go func() {
		time.Sleep(canvas.DurationShort)
		fyne.Do(f.icons.ClearSelection)
	}()

	if u.Scheme() == "settings" {
		tyde.Instance().ShowSettings("")
		return
	}
	// Folders open in Fyles when it is installed; anything else, and
	// folders without Fyles, with the default application (it used to give
	// up on everything without Fyles).
	if ok, _ := storage.CanList(u); ok {
		if p, err := execabs.LookPath("fyles"); err == nil {
			if err := wm.StartDetached(p, u.Path()); err == nil {
				return
			}
			log.Println("Error opening Fyles", err)
		}
	}
	if err := lib.Open(u); err != nil {
		log.Println("Error opening", u, err)
	}
}

// newFyles creates a new module that will manage desktop file icons.
func newFyles() tyde.Module {
	return &fyles{}
}

type filter struct{}

func (f *filter) Matches(u fyne.URI) bool {
	name := u.Name()
	return name != "" && name[0] != '.'
}

func filterHidden() storage.FileFilter {
	return &filter{}
}

type trashURI struct {
	fyne.URI

	name string
	icon fyne.Resource
}

func newCustomURI(str, name string, icon fyne.Resource) fyne.URI {
	u, _ := storage.ParseURI(str)
	return &trashURI{URI: u, name: name, icon: icon}
}

func (t *trashURI) Name() string {
	return t.name
}

func (t *trashURI) Icon() fyne.Resource {
	return t.icon
}
