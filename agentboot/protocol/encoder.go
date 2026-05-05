package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Encoder writes control responses (and other outbound messages) as
// newline-delimited JSON to a destination io.Writer. It is safe for
// concurrent use; concurrent Encode calls are serialized so that no two
// JSON values interleave on the wire.
type Encoder struct {
	mu  sync.Mutex
	dst io.Writer
}

// NewEncoder constructs an Encoder targeting w.
func NewEncoder(w io.Writer) *Encoder { return &Encoder{dst: w} }

// Encode marshals v to JSON and writes it to the destination, terminated
// by a newline.
func (e *Encoder) Encode(v any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	enc := json.NewEncoder(e.dst)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
}
