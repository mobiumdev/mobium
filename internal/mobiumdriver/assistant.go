package mobiumdriver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Siri, asked in text through WebDriverAgent's /wda/siri/activate. Measured
// on the iPhone 15 Plus, iOS 26.6.2 and 27.0.1: the request answers in under
// a second, Siri's panel comes up over the app in SpringBoard's
// siri-interface-window, and "Open MobiumApp" brought the app forward in
// about a second (CHALLENGES 349). On a simulator, iOS 26.5, the first
// request after a boot drew nothing and left SpringBoard in front for good,
// and later ones raised nothing at all (CHALLENGES 351), so a simulator is
// refused rather than asked.

// SiriAsker is implemented by backends that can put a typed request to Siri.
type SiriAsker interface {
	AskSiri(ctx context.Context, request string) error
}

// AskSiri sends request to Siri as typed text.
func (w *WDA) AskSiri(ctx context.Context, request string) error {
	switch {
	case w.tv():
		return mobiumerr.New(mobiumerr.Unsupported, "an Apple TV's Siri listens through the remote's microphone, and "+
			"WebDriverAgent has no typed request for it")
	case w.phone == nil:
		return mobiumerr.New(mobiumerr.Unsupported, "Siri on a simulator answers nothing: the first typed request "+
			"after a boot draws no panel and leaves SpringBoard in front until Home is pressed, and later ones raise "+
			"nothing at all — ask on a real iPhone")
	}
	if err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/siri/activate"),
		map[string]interface{}{"text": request}, nil); err != nil {
		return fmt.Errorf("ask Siri: %w", err)
	}
	return nil
}
