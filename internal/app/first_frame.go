package app

import (
	"sync"

	"github.com/amin-jalali/termocode/internal/bootgc"
)

// firstFrameOnce runs onFirstFrame for the first frame drawn at the
// window's size: startup is done then. onFirstFrame turns the garbage
// collector on (internal/bootgc held it off through startup); in a
// goroutine, so the frame goes out first.
var (
	firstFrameOnce = &sync.Once{}
	onFirstFrame   = func() { go bootgc.Release() }
)

// firstFrameDrawn is called after each frame drawn at the window's size.
func firstFrameDrawn() { firstFrameOnce.Do(onFirstFrame) }
