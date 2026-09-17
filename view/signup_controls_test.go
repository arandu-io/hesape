package view_test

import (
	"strings"
	"testing"
)

func TestMaskTokensExistBeforeEagerMaskedInputMounts(t *testing.T) {
	source := code(readUI(t))
	declaration := strings.Index(source, "var maskTokens = {")
	registration := strings.Index(source, "arandu.ui.define('mask'")
	if declaration < 0 || registration < 0 || declaration > registration {
		t.Fatal("masked input mounting can read its token table before initialization")
	}
	if strings.Count(source, "var maskTokens = {") != 1 {
		t.Fatal("mask token state must not be reset")
	}
}

func TestPasswordRuntimeOwnsPanelAndLiveSummary(t *testing.T) {
	source := code(readUI(t))
	for _, expected := range []string{"ctx.showPanel", "ctx.closePanel", "ctx.finish", "data-password-summary", "strength.textContent", "Array.from(value).length"} {
		if !strings.Contains(source, expected) {
			t.Errorf("password control lacks %s", expected)
		}
	}
	for _, expected := range []string{"removeEventListener('input', ctx.typed)", "removeEventListener('focus', ctx.showPanel)", "removeEventListener('click', ctx.finish)"} {
		if !strings.Contains(source, expected) {
			t.Errorf("password cleanup lacks %s", expected)
		}
	}
}
