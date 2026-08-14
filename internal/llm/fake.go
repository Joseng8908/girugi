package llm

import "context"

// Fake is a test double. It returns Reply (or Err) and records the last call so
// tests can assert on what the handler sent to the LLM.
type Fake struct {
	Reply string
	Err   error

	// Recorded call arguments.
	System string
	Msgs   []Message
	Calls  int
}

func (f *Fake) Complete(_ context.Context, system string, msgs []Message) (string, error) {
	f.Calls++
	f.System = system
	f.Msgs = msgs
	if f.Err != nil {
		return "", f.Err
	}
	return f.Reply, nil
}
