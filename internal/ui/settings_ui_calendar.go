package ui

import (
	"context"
	"fmt"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/internal/calendar"
	"fyshos.com/fynedesk/internal/calendar/google"
	"fyshos.com/fynedesk/locale"
)

// loadCalendarScreen builds the Settings → Calendar tab. The tab works
// even when no calendar service is running: it then offers only the
// "Add account" affordances and explains the prerequisite.
func (d *settingsUI) loadCalendarScreen() fyne.CanvasObject {
	body := container.NewVBox()

	rebuild := func() {}
	rebuild = func() {
		body.Objects = d.buildCalendarSections(rebuild)
		body.Refresh()
	}
	rebuild()

	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(420, 360))
	return scroll
}

func (d *settingsUI) buildCalendarSections(rebuild func()) []fyne.CanvasObject {
	svc := calendar.Get()

	addCard := d.buildCalendarAddCard(rebuild)

	if svc == nil || svc.Store() == nil {
		return []fyne.CanvasObject{
			widget.NewLabelWithStyle(locale.T("cal.serviceUnavailable"),
				fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
			addCard,
		}
	}

	store := svc.Store()
	accounts := store.Accounts()

	var accountCards []fyne.CanvasObject
	for _, acc := range accounts {
		accountCards = append(accountCards, d.buildCalendarAccountCard(svc, acc, rebuild))
	}
	if len(accountCards) == 0 {
		accountCards = []fyne.CanvasObject{
			widget.NewLabel(locale.T("cal.noAccount")),
		}
	}

	out := []fyne.CanvasObject{}
	out = append(out, accountCards...)
	out = append(out, addCard)
	return out
}

func (d *settingsUI) buildCalendarAccountCard(svc *calendar.Service, acc calendar.Account, rebuild func()) fyne.CanvasObject {
	store := svc.Store()
	cals := store.CalendarsFor(acc.ID)
	sort.Slice(cals, func(i, j int) bool {
		if cals[i].Primary != cals[j].Primary {
			return cals[i].Primary
		}
		return cals[i].Name < cals[j].Name
	})

	var rows []fyne.CanvasObject
	for _, c := range cals {
		c := c
		prefs := acc.Calendars[c.ID]
		check := widget.NewCheck(c.Name, func(enabled bool) {
			updated, ok := store.AccountByID(acc.ID)
			if !ok {
				return
			}
			if updated.Calendars == nil {
				updated.Calendars = make(map[string]calendar.CalendarPrefs)
			}
			p := updated.Calendars[c.ID]
			p.Enabled = enabled
			updated.Calendars[c.ID] = p
			_ = store.PutAccount(updated)
			svc.Refresh(acc.ID)
		})
		check.Checked = prefs.Enabled
		rows = append(rows, check)
	}
	if len(rows) == 0 {
		rows = append(rows, widget.NewLabel(locale.T("cal.noCalendars")))
	}

	refreshBtn := widget.NewButtonWithIcon(locale.T("settings.refresh"), theme.ViewRefreshIcon(), func() {
		svc.Refresh(acc.ID)
	})
	refreshBtn.Importance = widget.LowImportance
	removeBtn := widget.NewButtonWithIcon(locale.T("settings.remove"), theme.DeleteIcon(), func() {
		dialog.ShowConfirm(locale.T("cal.removeConfirmTitle"),
			fmt.Sprintf(locale.T("cal.removeConfirmBody"), acc.Email),
			func(ok bool) {
				if !ok {
					return
				}
				_ = calendar.DeleteSecret(acc.ID)
				_ = store.RemoveAccount(acc.ID)
				rebuild()
			}, d.win)
	})
	removeBtn.Importance = widget.LowImportance

	footer := container.NewHBox(layout.NewSpacer(), refreshBtn, removeBtn)

	subtitle := fmt.Sprintf("%s · %s", acc.Email, sourceLabel(acc.Source))

	return widget.NewCard(displayName(acc), subtitle,
		container.NewVBox(append(rows, footer)...))
}

func (d *settingsUI) buildCalendarAddCard(rebuild func()) fyne.CanvasObject {
	addGOA := widget.NewButtonWithIcon(locale.T("cal.addGoogle"), theme.AccountIcon(), func() {
		d.startAddGOAFlow(rebuild)
	})
	addGOA.Importance = widget.MediumImportance

	addOAuth := widget.NewButtonWithIcon(locale.T("cal.addManual"), theme.LoginIcon(), func() {
		d.startAddOAuthFlow(rebuild)
	})

	hint := widget.NewLabel(locale.T("cal.addHint"))
	hint.Wrapping = fyne.TextWrapWord

	return widget.NewCard(locale.T("cal.addAccount"), "", container.NewVBox(
		hint,
		container.NewHBox(addGOA, addOAuth),
	))
}

// startAddGOAFlow detects GNOME Online Accounts and either adds the
// single Google account it knows about or shows a picker.
func (d *settingsUI) startAddGOAFlow(rebuild func()) {
	if !google.GOAAvailable() {
		dialog.ShowInformation(locale.T("cal.goaUnavailableTitle"),
			locale.T("cal.goaUnavailableBody"), d.win)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	candidates, err := google.ListGOAGoogleAccounts(ctx)
	if err != nil {
		dialog.ShowError(err, d.win)
		return
	}
	if len(candidates) == 0 {
		dialog.ShowInformation(locale.T("cal.goaNoAccountTitle"),
			locale.T("cal.goaNoAccountBody"), d.win)
		return
	}

	add := func(acc calendar.Account) {
		store := calendarStore()
		if store == nil {
			return
		}
		if err := store.PutAccount(acc); err != nil {
			dialog.ShowError(err, d.win)
			return
		}
		if svc := calendar.Get(); svc != nil {
			svc.Refresh(acc.ID)
		}
		rebuild()
	}

	if len(candidates) == 1 {
		add(candidates[0])
		return
	}

	options := make([]string, len(candidates))
	for i, c := range candidates {
		options[i] = c.Display
	}
	sel := widget.NewRadioGroup(options, nil)
	dialog.ShowCustomConfirm(locale.T("cal.pickAccountTitle"),
		locale.T("settings.apply"), locale.T("settings.cancel"),
		container.NewVBox(widget.NewLabel(locale.T("cal.pickAccountBody")), sel),
		func(ok bool) {
			if !ok || sel.Selected == "" {
				return
			}
			for i, label := range options {
				if label == sel.Selected {
					add(candidates[i])
					return
				}
			}
		}, d.win)
}

// startAddOAuthFlow walks the user through the manual OAuth client_id
// prompt and the consent flow. The HTTP loopback is ephemeral.
func (d *settingsUI) startAddOAuthFlow(rebuild func()) {
	clientIDEntry := widget.NewEntry()
	clientIDEntry.SetPlaceHolder("123-abc.apps.googleusercontent.com")
	clientSecretEntry := widget.NewPasswordEntry()
	clientSecretEntry.SetPlaceHolder(locale.T("cal.clientSecretPlaceholder"))

	form := container.NewVBox(
		widget.NewLabel(locale.T("cal.oauthInstructions")),
		widget.NewLabel("Client ID"), clientIDEntry,
		widget.NewLabel("Client Secret"), clientSecretEntry,
	)

	dialog.ShowCustomConfirm(locale.T("cal.addManual"),
		locale.T("cal.startAuth"), locale.T("settings.cancel"),
		form,
		func(ok bool) {
			if !ok || clientIDEntry.Text == "" {
				return
			}
			go d.runOAuthFlow(clientIDEntry.Text, clientSecretEntry.Text, rebuild)
		}, d.win)
}

func (d *settingsUI) runOAuthFlow(clientID, clientSecret string, rebuild func()) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	cfg := google.OAuthConfig{ClientID: clientID, ClientSecret: clientSecret}
	tok, email, err := google.AuthorizeOAuth(ctx, cfg, nil)
	if err != nil {
		fyne.Do(func() { dialog.ShowError(err, d.win) })
		return
	}
	if tok.RefreshToken == "" {
		fyne.Do(func() {
			dialog.ShowInformation(locale.T("cal.noRefreshTitle"),
				locale.T("cal.noRefreshBody"), d.win)
		})
		return
	}
	id := "google:" + email
	if email == "" {
		id = "google:oauth-" + time.Now().Format("20060102-150405")
	}
	display := email
	if display == "" {
		display = "Google account"
	}
	acc := calendar.Account{
		ID:       id,
		Provider: "google",
		Source:   calendar.SourceOAuth,
		Email:    email,
		Display:  display,
	}
	if err := calendar.SaveSecret(id, calendar.SecretBundle{
		RefreshToken: tok.RefreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}); err != nil {
		fyne.Do(func() { dialog.ShowError(err, d.win) })
		return
	}
	store := calendarStore()
	if store == nil {
		return
	}
	if err := store.PutAccount(acc); err != nil {
		fyne.Do(func() { dialog.ShowError(err, d.win) })
		return
	}
	if svc := calendar.Get(); svc != nil {
		svc.Refresh(id)
	}
	fyne.Do(rebuild)
}

// --- helpers ---

func calendarStore() *calendar.Store {
	svc := calendar.Get()
	if svc == nil {
		return nil
	}
	return svc.Store()
}

func displayName(acc calendar.Account) string {
	if acc.Display != "" {
		return acc.Display
	}
	if acc.Email != "" {
		return acc.Email
	}
	return acc.ID
}

func sourceLabel(s calendar.AccountSource) string {
	switch s {
	case calendar.SourceGOA:
		return "GNOME Online Accounts"
	case calendar.SourceOAuth:
		return "OAuth"
	default:
		return string(s)
	}
}
