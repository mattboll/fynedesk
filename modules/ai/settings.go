package ai

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/locale"
)

// SettingsContent builds the provider configuration panel for the AI assistant.
// Changes are persisted immediately to app preferences so they take effect the
// next time the assistant is opened. The panel is embedded both in the settings
// window's AI tab and in the first-run "Set up AI" step.
//
// Each provider needs different setup - a cloud key, or a local server URL - so
// the area below the provider selector is rebuilt for the chosen provider
// rather than relabelled in place (Fyne forms don't update item labels reliably
// on refresh).
func SettingsContent() fyne.CanvasObject {
	p := fyne.CurrentApp().Preferences()

	// The select shows translated names; the preference keeps the provider IDs.
	providers := []string{ProviderClaude, ProviderOpenAI, ProviderLocal}
	labels := make([]string, len(providers))
	for i, id := range providers {
		labels[i] = providerLabel(id)
	}
	provider := widget.NewSelect(labels, nil)
	provider.SetSelected(providerLabel(p.StringWithFallback(prefProvider, ProviderClaude)))
	selected := func() string {
		for i, l := range labels {
			if l == provider.Selected {
				return providers[i]
			}
		}
		return ProviderClaude
	}

	body := container.NewStack()

	var rebuild func()
	rebuild = func() {
		switch selected() {
		case ProviderOpenAI:
			body.Objects = []fyne.CanvasObject{cloudSettings(p, ProviderOpenAI, prefOpenAIKey)}
		case ProviderLocal:
			body.Objects = []fyne.CanvasObject{localSettings(p, rebuild)}
		default:
			body.Objects = []fyne.CanvasObject{cloudSettings(p, ProviderClaude, prefClaudeKey)}
		}
		body.Refresh()
	}
	rebuild()

	provider.OnChanged = func(string) {
		p.SetString(prefProvider, selected())
		if selected() == ProviderLocal {
			// Switching to Local while the module is loaded: get the managed
			// server coming up so it's ready by the time they ask something.
			serverMgr.ensure()
		}
		rebuild()
	}

	head := widget.NewForm(widget.NewFormItem(locale.T("ai.provider"), provider))
	return container.NewVBox(head, body)
}

// providerLabel is the name shown for a provider ID: the cloud providers are
// brand names, Local AI is translated.
func providerLabel(id string) string {
	if id == ProviderLocal {
		return locale.T("ai.localProvider")
	}
	return id
}

// cloudSettings builds the API-key + model form for a cloud provider. Both the
// key and the model are stored under that provider's own preference, so
// switching provider leaves the other one's setup untouched.
func cloudSettings(p fyne.Preferences, provider, keyPref string) fyne.CanvasObject {
	key := widget.NewPasswordEntry()
	key.SetPlaceHolder(locale.T("ai.keyPlaceholder"))
	key.SetText(loadKey(p, keyPref))
	key.OnChanged = func(s string) { saveKey(p, keyPref, s) }

	modelKey := modelPref(provider)
	model := widget.NewEntry()
	model.SetText(p.String(modelKey))
	model.SetPlaceHolder(defaultModel(provider))
	model.OnChanged = func(s string) { p.SetString(modelKey, s) }

	help := widget.NewLabel(locale.T("ai.cloudHelp"))
	help.Wrapping = fyne.TextWrapWord

	return container.NewVBox(widget.NewForm(
		widget.NewFormItem(locale.T("ai.apiKey"), key),
		widget.NewFormItem(locale.T("ai.model"), model),
	), help)
}

// localSettings builds the Local AI panel. rebuild swaps the panel between managed
// vs configuration when the managed option is toggled.
func localSettings(p fyne.Preferences, rebuild func()) fyne.CanvasObject {
	model := widget.NewEntry()
	model.SetText(p.String(prefLocalModel))
	model.SetPlaceHolder(defaultLocalModel())
	model.OnChanged = func(s string) { p.SetString(prefLocalModel, s) }

	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	managed := widget.NewCheck(managedLabel(), nil)
	managed.SetChecked(kronkManaged())
	managed.OnChanged = func(on bool) {
		p.SetBool(prefLocalManaged, on)
		if on {
			serverMgr.ensure()
		} else {
			// Hand the server back to the user: stop the one we started, as from
			// here on tyde issues no kronk commands.
			serverMgr.stop()
		}
		rebuild()
	}
	if !kronkAvailable() {
		managed.Disable() // nothing to manage with
	}

	reasoning := widget.NewCheck(locale.T("ai.reasoningDefault"), nil)
	reasoning.SetChecked(p.Bool(prefLocalThinking))
	reasoning.OnChanged = func(on bool) { p.SetBool(prefLocalThinking, on) }

	if kronkManaged() {
		// Managed server: tyde knows the address, so the user only picks a model.
		test := probeButton(func() string { return kronkEndpoint }, model, status)
		return container.NewVBox(
			managed,
			widget.NewForm(widget.NewFormItem(locale.T("ai.model"), model)),
			reasoning, test, status,
		)
	}

	// User-run server: needs an address.
	endpoint := widget.NewEntry()
	endpoint.SetText(p.StringWithFallback(prefLocalEndpoint, ollamaEndpoint))
	endpoint.SetPlaceHolder(ollamaEndpoint)
	endpoint.OnChanged = func(s string) { p.SetString(prefLocalEndpoint, s) }

	test := probeButton(func() string {
		if ep := strings.TrimSpace(endpoint.Text); ep != "" {
			return ep
		}
		return ollamaEndpoint
	}, model, status)

	return container.NewVBox(
		managed,
		widget.NewForm(
			widget.NewFormItem(locale.T("ai.ollamaURL"), endpoint),
			widget.NewFormItem(locale.T("ai.model"), model),
		),
		reasoning, test, status,
	)
}

// managedLabel offers tyde running the local server itself, when kronk is there
// to run it with.
func managedLabel() string { return locale.T("ai.managed") }

// Test-connection outcome marks. A pass needs no words, so it is just the tick
// beside the button; a failure adds the reason below.
const (
	passMark = "✅"
	failMark = "❌"
)

// probeButton makes a "Test connection" button that checks the server at
// endpointFn() and marks the result beside itself, filling in status only when
// something is wrong.
func probeButton(endpointFn func() string, model *widget.Entry, status *widget.Label) fyne.CanvasObject {
	mark := widget.NewLabel("")

	btn := widget.NewButton(locale.T("ai.testConnection"), func() {
		ep := endpointFn()
		want := strings.TrimSpace(model.Text)
		if want == "" {
			want = defaultLocalModel()
		}
		mark.SetText("")
		status.SetText(locale.T("ai.testing"))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			res := probeEndpoint(ctx, ep)
			problem := describeProbe(res, want)
			fyne.Do(func() {
				if problem == "" {
					mark.SetText(passMark)
				} else {
					mark.SetText(failMark)
				}
				status.SetText(problem)
			})
		}()
	})

	return container.NewHBox(btn, mark)
}

// describeProbe reduces a connection test to what went wrong, in one line a
// beginner can act on - or "" when the server and model are both ready, since
// the tick says that on its own.
func describeProbe(res probeResult, model string) string {
	if res.err != nil {
		// Unwrap the http.Client's "Get \"url\": …" wrapper: the reason is the
		// useful half and the URL is on screen already.
		err := res.err
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return err.Error()
	}
	if _, ok := res.has(model); ok {
		return ""
	}
	if len(res.models) == 0 {
		return locale.Tf("ai.noModels", model)
	}
	return locale.Tf("ai.modelUnavailable", model, strings.Join(res.models, ", "))
}
