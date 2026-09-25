package compositor

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Window places: Tyde remembers where each application's windows are, for
// each set of screens plugged in, and puts them back there. With the laptop
// alone the three windows may be maximized on it; with the external screen
// plugged in, one maximized on the laptop and two side by side on the
// external screen: plugging the screen in or out moves them there, and a
// window opening goes where the same one was.
//
// Places are learnt from what the user does, a moment after the windows
// settle, and not right after the screens changed: the windows moved then are
// not where the user wants them.
const (
	placesLearnDelay = 1500 * time.Millisecond // after the last change
	placesApplyDelay = 800 * time.Millisecond  // after the screens changed, once they settled
	placesHold       = 4 * time.Second         // no learning after the screens changed
	placesFile       = "window-places.json"
)

// windowPlace is where a window goes on a set of screens.
type windowPlace struct {
	Output string `json:"output"`         // the screen, by outputID
	Zone   string `json:"zone,omitempty"` // "max", a snap zone, or "" for a window left free
	// A free window: where its content is from the corner of the screen's
	// content area, and its size (without the decorations).
	X    int `json:"x,omitempty"`
	Y    int `json:"y,omitempty"`
	W    int `json:"w,omitempty"`
	H    int `json:"h,omitempty"`
	Desk int `json:"desk"`
}

// placeBook holds the places, by set of screens, then by application; the
// windows of an application are in the order they opened.
type placeBook struct {
	Setups map[string]map[string][]windowPlace `json:"setups"`
}

var zoneNames = map[snapZone]string{
	snapTop:         "max",
	snapLeft:        "left",
	snapRight:       "right",
	snapTopLeft:     "top-left",
	snapTopRight:    "top-right",
	snapBottomLeft:  "bottom-left",
	snapBottomRight: "bottom-right",
}

func zoneName(z snapZone) string {
	return zoneNames[z]
}

func zoneFromName(name string) snapZone {
	for z, n := range zoneNames {
		if n == name {
			return z
		}
	}
	return snapNone
}

// setupKey names a set of screens, whatever the order they came in.
func setupKey(ids []string) string {
	sorted := slices.Clone(ids)
	sort.Strings(sorted)
	return strings.Join(sorted, " + ")
}

// outputIdentity names a screen: its make, model and serial number, so that
// it is known on any port. Without a serial number, two screens of the same
// model are told apart by their port.
func outputIdentity(name, make, model, serial string) string {
	id := strings.TrimSpace(make + " " + model)
	switch {
	case serial != "":
		return id + " " + serial
	case id == "":
		return name
	default:
		return id + " @" + name
	}
}

// learn records the places of the windows of app, in their order; a nil
// place keeps what was known for that window (minimized, fullscreen). It
// reports whether anything changed.
func (b *placeBook) learn(setup, app string, places []*windowPlace) bool {
	if b.Setups == nil {
		b.Setups = map[string]map[string][]windowPlace{}
	}
	apps := b.Setups[setup]
	if apps == nil {
		apps = map[string][]windowPlace{}
		b.Setups[setup] = apps
	}
	known := apps[app]
	changed := false
	for i, p := range places {
		if p == nil {
			continue
		}
		if i < len(known) {
			if known[i] != *p {
				known[i] = *p
				changed = true
			}
			continue
		}
		if i == len(known) {
			known = append(known, *p)
			changed = true
		}
		// A place after an unknown one is not kept: the order would be lost.
	}
	apps[app] = known
	return changed
}

// lookup returns the place of the index-th window of app.
func (b *placeBook) lookup(setup, app string, index int) (windowPlace, bool) {
	known := b.Setups[setup][app]
	if index < 0 || index >= len(known) {
		return windowPlace{}, false
	}
	return known[index], true
}

// --- the compositor side ---

// placeable is a window whose place is remembered: a main window of an
// application.
type placeable struct {
	xdg  *xdgView
	xway *xwayView
}

func (p placeable) app() string {
	if p.xdg != nil {
		return getXdgToplevelAppID(p.xdg.xdgToplevel)
	}
	return getXwaylandSurfaceClass(p.xway.surface)
}

func (p placeable) seq() uint64 {
	if p.xdg != nil {
		return p.xdg.placeSeq
	}
	return p.xway.placeSeq
}

// placeableWindows returns the windows whose places are remembered, by
// application, in the order they opened.
func (s *server) placeableWindows() map[string][]placeable {
	byApp := map[string][]placeable{}
	add := func(p placeable) {
		if app := p.app(); app != "" {
			byApp[app] = append(byApp[app], p)
		}
	}
	for _, v := range s.xdgViews {
		if (v.mapped || v.minimized) && v.parent == nil && !isFixedSize(v) && v.placeSeq != 0 {
			add(placeable{xdg: v})
		}
	}
	for _, v := range s.xwayViews {
		if (v.mapped || v.minimized) && v.parent == nil && !v.isPanel && !v.isOverlay &&
			!v.overrideRedirect && v.placeSeq != 0 {
			add(placeable{xway: v})
		}
	}
	for _, list := range byApp {
		sort.Slice(list, func(i, j int) bool { return list[i].seq() < list[j].seq() })
	}
	return byApp
}

// outputID names an output for the places.
func outputID(out *outputState) string {
	o := out.output
	return outputIdentity(o.Name(), o.Make(), o.Model(), o.Serial())
}

// currentSetup names the screens of the desktop.
func (s *server) currentSetup() string {
	ids := make([]string, 0, len(s.outputs))
	for _, o := range s.outputs {
		ids = append(ids, outputID(o))
	}
	return setupKey(ids)
}

func (s *server) outputByID(id string) *outputState {
	for _, o := range s.outputs {
		if outputID(o) == id {
			return o
		}
	}
	return nil
}

// placeOf returns where a window is, or false when it is in no state to be
// remembered (minimized, fullscreen, on no screen).
func (s *server) placeOf(p placeable) (*windowPlace, bool) {
	var x, y float64
	var w, h, desk int
	var maximized, fullscreen, minimized bool
	var zone snapZone
	if v := p.xdg; v != nil {
		x, y = v.x, v.y
		if v.anim.active {
			x, y = v.anim.endX, v.anim.endY
		}
		w, h = xdgDecoSize(v)
		desk, maximized, fullscreen, minimized, zone = v.desk, v.maximized, v.fullscreen, v.minimized, v.snapped
	} else {
		v := p.xway
		x, y = v.x, v.y
		if v.anim.active {
			x, y = v.anim.endX, v.anim.endY
		}
		w, h = xwayDecoSize(v)
		desk, maximized, fullscreen, minimized, zone = v.desk, v.maximized, v.fullscreen, v.minimized, v.snapped
	}
	if fullscreen || minimized {
		return nil, false
	}
	out := s.getOutputForPosition(x+float64(w)/2, y+float64(h)/2)
	if out == nil {
		out = s.getOutputForPosition(x, y)
	}
	if out == nil {
		return nil, false
	}
	place := &windowPlace{Output: outputID(out), Desk: desk}
	if maximized {
		zone = snapTop
	}
	if zone != snapNone {
		place.Zone = zoneName(zone)
		return place, true
	}
	cx, cy, _, _ := s.contentBounds(s.getOutputGeometry(out))
	place.X, place.Y = int(x)-cx, int(y)-cy
	place.W, place.H = w, h
	return place, true
}

// windowsMoved is told that windows changed: their places are learnt a moment
// later.
func (s *server) windowsMoved() {
	if s.placesTimer != nil {
		s.placesTimer.Reset(placesLearnDelay)
		return
	}
	s.placesTimer = time.AfterFunc(placesLearnDelay, func() {
		s.enqueueAction(func() { //nolint:errcheck // learnt at the next change
			s.placesTimer = nil
			s.learnPlaces()
		})
	})
}

// learnPlaces records where the windows are.
func (s *server) learnPlaces() {
	if len(s.outputs) == 0 {
		return
	}
	if wait := time.Until(s.placesHoldUntil); wait > 0 {
		s.windowsMoved() // the screens just changed: later
		return
	}
	book := s.placeBook()
	setup := s.currentSetup()
	changed := false
	for app, list := range s.placeableWindows() {
		places := make([]*windowPlace, len(list))
		for i, p := range list {
			if place, ok := s.placeOf(p); ok {
				places[i] = place
			}
		}
		changed = book.learn(setup, app, places) || changed
	}
	if changed {
		s.savePlaces()
	}
}

// screensChanged is told that the screens of the desktop changed: the windows
// go to their places for the new screens, once these settled.
func (s *server) screensChanged() {
	s.placesHoldUntil = time.Now().Add(placesHold)
	time.AfterFunc(placesApplyDelay, func() {
		s.enqueueAction(s.applyPlaces) //nolint:errcheck // the windows stay where they are
	})
}

// applyPlaces puts the windows where they were with these screens.
func (s *server) applyPlaces() {
	if len(s.outputs) == 0 {
		return
	}
	book := s.placeBook()
	setup := s.currentSetup()
	log.Printf("[PLACES] screens %q: %d applications known", setup, len(book.Setups[setup]))
	for app, list := range s.placeableWindows() {
		for i, p := range list {
			if place, ok := book.lookup(setup, app, i); ok {
				s.putInPlace(p, place, true)
			}
		}
	}
	s.writeWindowsState()
}

// placeNewWindow puts a window that opens where the same one was, with these
// screens. It is told whether a window rule already placed it.
func (s *server) placeNewWindow(p placeable, ruled bool) {
	s.placeSeqNext++
	if p.xdg != nil {
		p.xdg.placeSeq = s.placeSeqNext
	} else {
		p.xway.placeSeq = s.placeSeqNext
	}
	if ruled || len(s.outputs) == 0 {
		return
	}
	app := p.app()
	list := s.placeableWindows()[app]
	index := slices.IndexFunc(list, func(q placeable) bool { return q.seq() == p.seq() })
	if place, ok := s.placeBook().lookup(s.currentSetup(), app, index); ok {
		s.putInPlace(p, place, false)
	}
}

// putInPlace moves a window to its place; it slides there if animate.
func (s *server) putInPlace(p placeable, place windowPlace, animate bool) {
	out := s.outputByID(place.Output)
	if out == nil {
		return
	}
	if now, ok := s.placeOf(p); ok && *now == place {
		return // already there
	}
	if place.Desk >= 0 && place.Desk < s.numDesks {
		s.setViewDesk(p, place.Desk)
	}
	outGeo := s.getOutputGeometry(out)
	zone := zoneFromName(place.Zone)
	cx, cy, _, _ := s.contentBounds(outGeo)
	if v := p.xdg; v != nil {
		if v.fullscreen {
			return
		}
		if zone != snapNone {
			s.setXdgZone(v, outGeo, zone)
		} else {
			s.freeXdgAt(v, cx+place.X, cy+place.Y, place.W, place.H)
		}
		if !animate && v.anim.active {
			v.x, v.y = v.anim.endX, v.anim.endY
			v.anim.active = false
			setXdgScenePos(v)
		}
		return
	}
	v := p.xway
	if v.fullscreen {
		return
	}
	if zone != snapNone {
		s.setXwayZone(v, outGeo, zone)
	} else {
		s.freeXwayAt(v, cx+place.X, cy+place.Y, place.W, place.H)
	}
	if !animate && v.anim.active {
		v.x, v.y = v.anim.endX, v.anim.endY
		v.anim.active = false
		setXwayScenePos(v)
	}
}

// freeXdgAt gives a window its normal state, its content at (x, y) of the
// layout with the size w x h.
func (s *server) freeXdgAt(v *xdgView, x, y, w, h int) {
	oldX, oldY := v.x, v.y
	v.maximized = false
	v.snapped = snapNone
	v.xdgToplevel.SetMaximized(false)
	v.configuredW, v.configuredH = w, h
	v.xdgToplevel.SetSize(int32(w), int32(h))
	s.animateXdgPos(v, oldX, oldY, float64(x), float64(y))
}

// freeXwayAt gives an XWayland window its normal state, its content at (x,
// y) with the size w x h.
func (s *server) freeXwayAt(v *xwayView, x, y, w, h int) {
	oldX, oldY := v.x, v.y
	v.maximized = false
	v.snapped = snapNone
	v.surface.Configure(int16(x), int16(y), uint16(w), uint16(h))
	s.animateXwayPos(v, oldX, oldY, float64(x), float64(y))
}

// setViewDesk moves a window to a desktop, shown or hidden with it.
func (s *server) setViewDesk(p placeable, desk int) {
	if p.xdg != nil {
		v := p.xdg
		if v.desk == desk {
			return
		}
		v.desk = desk
		if v.mapped {
			setViewSceneEnabled(v.sceneTree, v.pinned || desk == s.currentDesk)
		}
		return
	}
	v := p.xway
	if v.desk == desk {
		return
	}
	v.desk = desk
	if v.mapped {
		setViewSceneEnabled(v.sceneTree, v.pinned || desk == s.currentDesk)
	}
}

// placeBook returns the places, read on first use.
func (s *server) placeBook() *placeBook {
	if s.places != nil {
		return s.places
	}
	s.places = &placeBook{}
	data, err := os.ReadFile(filepath.Join(s.getConfigDir(), placesFile))
	if err == nil {
		if err := json.Unmarshal(data, s.places); err != nil {
			log.Printf("[PLACES] %s: %v", placesFile, err)
			s.places = &placeBook{}
		}
	}
	return s.places
}

// savePlaces writes the places, off the main thread.
func (s *server) savePlaces() {
	data, err := json.MarshalIndent(s.places, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(s.getConfigDir(), placesFile)
	go writeAtomic(path, data)
}
