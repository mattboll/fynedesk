package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const permissionScreen = `● Bash(rm -rf build/)
  ⎿  Running…

────────────────────────────────────────
 Bash command

   rm -rf build/
   Remove the build directory

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands in /home/u/proj
   3. No, and tell Claude what to do differently (esc)

────────────────────────────────────────
  ⏵⏵ auto mode on · 2 shells
`

func TestChoices(t *testing.T) {
	got := Choices(permissionScreen)
	assert.Equal(t, []Choice{
		{"1", "Yes"},
		{"2", "Yes, and don't ask again for rm commands in /home/u/proj"},
		{"3", "No, and tell Claude what to do differently (esc)"},
	}, got)

	assert.Nil(t, Choices("Done! I renamed the files.\n❯ \n"), "no question")
	assert.Nil(t, Choices("Steps:\n 2. build\n 3. test\n"), "a list that does not start at 1 is not a question")
}

func TestPreview(t *testing.T) {
	screen := "Je peux aussi publier la revue.\n\n\n✻ Baked for 3m\n" +
		"──────────────────────\n❯ \n──────────────────────\n  ⏵⏵ auto mode on · 2 shells\n"
	assert.Equal(t, []string{"Je peux aussi publier la revue.", "", "✻ Baked for 3m"}, Preview(screen, 5))
	assert.Equal(t, []string{"✻ Baked for 3m"}, Preview(screen, 1))
}
