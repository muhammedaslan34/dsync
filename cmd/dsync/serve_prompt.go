package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

type servePromptKind string

const (
	promptPair     servePromptKind = "pair"
	promptIncoming servePromptKind = "incoming"
	promptControl  servePromptKind = "control"
)

type servePrompt struct {
	kind   servePromptKind
	id     string
	text   string
	answer func(bool)
}

type servePromptEvent struct {
	add        *servePrompt
	closeKind  servePromptKind
	closeID    string
	noticeText string
	handled    chan struct{}
}

// servePrompter owns terminal questions for `dsync serve`. Node callbacks can
// arrive concurrently, but only the active prompt consumes the next stdin
// line. Closed or timed-out requests are removed from the queue.
type servePrompter struct {
	out    io.Writer
	events chan servePromptEvent
	done   chan struct{}
}

func newServePrompter(out io.Writer) *servePrompter {
	return &servePrompter{out: out, events: make(chan servePromptEvent, 16), done: make(chan struct{})}
}

func (p *servePrompter) ask(prompt servePrompt) {
	p.dispatch(servePromptEvent{add: &prompt})
}

func (p *servePrompter) close(kind servePromptKind, id string) {
	p.dispatch(servePromptEvent{closeKind: kind, closeID: id})
}

func (p *servePrompter) notice(text string) {
	p.dispatch(servePromptEvent{noticeText: text})

}

// dispatch waits until the queue has applied the event. Besides making output
// deterministic, this prevents a stdin line racing ahead of a close event and
// answering a request that has already timed out.
func (p *servePrompter) dispatch(event servePromptEvent) {
	event.handled = make(chan struct{})
	select {
	case p.events <- event:
	case <-p.done:
		return
	}
	select {
	case <-event.handled:
	case <-p.done:
	}
}

func (p *servePrompter) start(ctx context.Context, in io.Reader) {
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	go p.run(ctx, lines)
}

func (p *servePrompter) run(ctx context.Context, lines <-chan string) {
	defer close(p.done)
	var active *servePrompt
	var queued []servePrompt
	inputClosed := false

	showNext := func() {
		if active != nil || len(queued) == 0 {
			return
		}
		active = &queued[0]
		queued = queued[1:]
		fmt.Fprintf(p.out, "\n%s", active.text)
	}
	remove := func(kind servePromptKind, id string) {
		if active != nil && active.kind == kind && active.id == id {
			active = nil
			fmt.Fprintln(p.out, "\nRequest closed.")
		}
		kept := queued[:0]
		for _, prompt := range queued {
			if prompt.kind != kind || prompt.id != id {
				kept = append(kept, prompt)
			}
		}
		queued = kept
		showNext()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				lines = nil
				inputClosed = true
				if active != nil {
					active.answer(false)
					active = nil
				}
				for _, prompt := range queued {
					prompt.answer(false)
				}
				queued = nil
				fmt.Fprintln(p.out, "\nstdin closed; pending interactive requests were denied.")
				continue
			}
			if active == nil {
				continue
			}
			prompt := active
			active = nil
			prompt.answer(strings.EqualFold(strings.TrimSpace(line), "y"))
			showNext()
		case event := <-p.events:
			switch {
			case event.add != nil:
				prompt := *event.add
				if inputClosed {
					fmt.Fprintf(p.out, "\nCannot ask for %s approval because stdin is closed; denying the request.\n", prompt.kind)
					prompt.answer(false)
				} else {
					duplicate := active != nil && active.kind == prompt.kind && active.id == prompt.id
					for _, existing := range queued {
						duplicate = duplicate || existing.kind == prompt.kind && existing.id == prompt.id
					}
					if !duplicate {
						queued = append(queued, prompt)
						showNext()
					}
				}
			case event.closeID != "":
				remove(event.closeKind, event.closeID)
			case event.noticeText != "":
				fmt.Fprintf(p.out, "\n%s\n", event.noticeText)
				if active != nil {
					fmt.Fprint(p.out, active.text)
				}
			}
			close(event.handled)
		}
	}
}
