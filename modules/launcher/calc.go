package launcher

import (
	"regexp"
	"strconv"

	"fyne.io/fyne/v2"

	"fyshos.com/fynedesk"
	wmTheme "fyshos.com/fynedesk/theme"

	"github.com/Knetic/govaluate"
)

var (
	// Restricted to genuine arithmetic — '<', '^', '>', ':' previously allowed
	// govaluate to evaluate XOR/comparison/ternary, which can produce huge
	// integer results (DoS via 2^99999999) or panic (integer divide by zero).
	exprRegex = regexp.MustCompile(`^[0-9.+\-*/()% ]+$`)
	numRegex  = regexp.MustCompile(`^[0-9.]+$`)
)

// maxExprLen caps the input length to avoid pathological evaluations from
// pasted text in the launcher entry box.
const maxExprLen = 64

var calcMeta = fynedesk.ModuleMetadata{
	Name:        "Launcher: Calculate",
	NewInstance: newCalcSuggest,
}

type calc struct{}

func (c *calc) Destroy() {
}

func (c *calc) LaunchSuggestions(input string) []fynedesk.LaunchSuggestion {
	if !c.isExpression(input) {
		return nil
	}

	result, err := c.eval(input)
	if err != nil {
		return nil
	}
	return []fynedesk.LaunchSuggestion{&calcItem{sum: input, result: result}}
}

func (c *calc) eval(sum string) (result string, err error) {
	// govaluate panics on integer division by zero (e.g. "5/0"); recover
	// so a bad expression in the launcher entry can't crash the panel.
	defer func() {
		if r := recover(); r != nil {
			result = ""
			err = errEvalPanic
		}
	}()

	expression, err := govaluate.NewEvaluableExpression(sum)
	if err != nil {
		return "", err
	}

	res, err := expression.Evaluate(nil)
	if err != nil {
		return "", err
	}

	if f, ok := res.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}

	return "", nil
}

var errEvalPanic = errEval("evaluator panicked")

type errEval string

func (e errEval) Error() string { return string(e) }

func (c *calc) Metadata() fynedesk.ModuleMetadata {
	return calcMeta
}

// isExpression will return true if input is a mathematical expression unless it just contains a number
func (c *calc) isExpression(input string) bool {
	if len(input) > maxExprLen {
		return false
	}
	return exprRegex.MatchString(input) && !numRegex.MatchString(input)
}

// newCalcSuggest creates a new module that will show calculations in the launcher suggestions
func newCalcSuggest() fynedesk.Module {
	return &calc{}
}

type calcItem struct {
	sum, result string
}

func (i *calcItem) Icon() fyne.Resource {
	return wmTheme.CalculateIcon
}

func (i *calcItem) Title() string {
	return i.sum + " = " + i.result
}

func (i *calcItem) Launch() {
	fyne.CurrentApp().Clipboard().SetContent(i.result)
}
