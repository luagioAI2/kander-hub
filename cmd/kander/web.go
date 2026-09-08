package main

// The web package registers the `kander req serve` backend on init.
// The reqtui package registers the `kander req tui` backend on init.
// The launch package registers the `kander req decompose` backend on init.
import (
	_ "github.com/dualface/kander/internal/launch"
	_ "github.com/dualface/kander/internal/reqtui"
	_ "github.com/dualface/kander/internal/web"
)
