package main

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestServePrompterSerializesAnswers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	prompts := newServePrompter(io.Discard)
	lines := make(chan string)
	go prompts.run(ctx, lines)

	first := make(chan bool, 1)
	second := make(chan bool, 1)
	prompts.ask(servePrompt{kind: promptPair, id: "pair-1", text: "first? ", answer: func(ok bool) { first <- ok }})
	prompts.ask(servePrompt{kind: promptIncoming, id: "file-1", text: "second? ", answer: func(ok bool) { second <- ok }})

	lines <- "y"
	select {
	case got := <-first:
		if !got {
			t.Fatal("first prompt did not accept y")
		}
	case <-time.After(time.Second):
		t.Fatal("first prompt was not answered")
	}
	select {
	case <-second:
		t.Fatal("one stdin line answered two prompts")
	default:
	}

	lines <- "anything other than y"
	select {
	case got := <-second:
		if got {
			t.Fatal("second prompt accepted a non-y answer")
		}
	case <-time.After(time.Second):
		t.Fatal("second prompt was not answered")
	}
}

func TestServePrompterDeniesWhenStdinCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	prompts := newServePrompter(io.Discard)
	lines := make(chan string)
	go prompts.run(ctx, lines)

	answer := make(chan bool, 1)
	prompts.ask(servePrompt{kind: promptControl, id: "control-1", text: "control? ", answer: func(ok bool) { answer <- ok }})
	close(lines)
	select {
	case got := <-answer:
		if got {
			t.Fatal("closed stdin accepted an interactive request")
		}
	case <-time.After(time.Second):
		t.Fatal("closed stdin did not deny the request")
	}
}

func TestServePrompterRemovesClosedPromptBeforeNextAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	prompts := newServePrompter(io.Discard)
	lines := make(chan string)
	go prompts.run(ctx, lines)

	stale := make(chan bool, 1)
	next := make(chan bool, 1)
	prompts.ask(servePrompt{kind: promptIncoming, id: "expired", text: "old? ", answer: func(ok bool) { stale <- ok }})
	prompts.ask(servePrompt{kind: promptControl, id: "current", text: "new? ", answer: func(ok bool) { next <- ok }})
	prompts.close(promptIncoming, "expired")
	lines <- "y"

	select {
	case <-stale:
		t.Fatal("a closed prompt was answered")
	default:
	}
	select {
	case got := <-next:
		if !got {
			t.Fatal("answer was not delivered to the next prompt")
		}
	case <-time.After(time.Second):
		t.Fatal("next prompt was not answered")
	}
}
