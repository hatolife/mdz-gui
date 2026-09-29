package bundle

import "testing"

func TestNewSlideDeckDefaults(t *testing.T) {
	deck := NewSlideDeck("資料")
	if deck.Version != 1 || deck.Title != "資料" || deck.Theme != "light" || deck.Aspect != "16:9" {
		t.Fatalf("unexpected deck defaults: %+v", deck)
	}
	if deck.ContentMarginX != 60 || deck.ContentMarginY != 48 {
		t.Fatalf("unexpected margin defaults: %+v", deck)
	}
	if deck.FontFamily != "system" || deck.BodyFontSize != 20 {
		t.Fatalf("unexpected typography defaults: %+v", deck)
	}
	if deck.H1FontSize != 42 || deck.H2FontSize != 28 || deck.H3FontSize != 26 || deck.H4FontSize != 24 || deck.H5FontSize != 22 {
		t.Fatalf("unexpected heading defaults: %+v", deck)
	}
}
