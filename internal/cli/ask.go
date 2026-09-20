package cli

// Asking a person something.
//
// The answers are read from standard input rather than from a terminal
// device, so the commands that ask work typed and piped alike, and running out
// of input ends them with a message rather than a wait. Nothing here is called
// inside withDaemon: the call deadline is there so a wedged daemon cannot hold
// a command forever, and a person reading a question is not a wedged daemon
// (see deadline_internal_test.go).

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxAnswer bounds what the questions read between them, so that a mistaken
// `owl project setup < some-huge-file` is refused rather than held in memory.
// It bounds a pasted token too, for the commands that ask for one after
// asking something else.
const maxAnswer = 4 << 10

// asker reads the answers. One reader serves every question, because a
// buffered one holds what it has read ahead and a second would lose it.
type asker struct{ in *bufio.Reader }

func newAsker(env Env) *asker {
	return &asker{in: bufio.NewReader(io.LimitReader(env.stdin(), maxAnswer))}
}

// line reads one answer, with its newline taken off. It reports whether that
// answer was the last thing there was to read, so a caller that has to ask
// again can say there is nothing left rather than waiting for it - and can say
// that without reading ahead, which at a terminal would mean waiting for the
// very answer it is about to refuse.
//
// An answer that never comes at all ends the command: a command nobody is
// answering should stop rather than wait.
//
// It asks nothing itself; the prompt is the caller's to write.
func (a *asker) line(what string) (answer string, last bool, err error) {
	text, err := a.in.ReadString('\n')
	// An answer typed without a newline, or piped in, ends at end of file,
	// which is an answer like any other. Nothing at all is not.
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, fmt.Errorf("reading %s: %w", what, err)
	}
	last = errors.Is(err, io.EOF)
	answer = strings.TrimSpace(text)
	if answer == "" && last {
		return "", true, fmt.Errorf("no answer for %s, and nothing left to read", what)
	}
	return answer, last, nil
}

// choose asks for one of choices by number and returns the one picked. An
// answer that is not one of them is said to be wrong and asked for again.
func (a *asker) choose(env Env, what string, choices []string) (string, error) {
	for {
		_, _ = fmt.Fprintf(env.Stdout, "Choose 1-%d: ", len(choices))
		answer, last, err := a.line("which " + what + " to use")
		if err != nil {
			return "", err
		}
		n, convErr := strconv.Atoi(answer)
		if convErr == nil && n >= 1 && n <= len(choices) {
			return choices[n-1], nil
		}
		_, _ = fmt.Fprintf(env.Stdout, "%q is not one of the choices; pick a number between 1 and %d.\n",
			terminalSafe(answer), len(choices))
		if last {
			// There was an answer; it was the wrong one. Saying none came
			// would send the reader looking for the wrong mistake.
			return "", fmt.Errorf("%q is not one of the choices for which %s to use, and there is nothing left to read",
				terminalSafe(answer), what)
		}
	}
}

// ask puts a prompt and reads an answer, taking whatever was typed. What makes
// an answer usable is the caller's to say, so a caller that has to refuse one
// asks again itself - and `last` tells it when asking again would be asking
// nobody.
func (a *asker) ask(env Env, prompt, what string) (answer string, last bool, err error) {
	_, _ = fmt.Fprint(env.Stdout, prompt)
	return a.line(what)
}

// confirm asks something with a yes or no answer, and takes anything else as
// no: the callers are offers to change the machine, and only yes is yes.
func (a *asker) confirm(env Env, prompt string) (bool, error) {
	answer, _, err := a.ask(env, prompt, "whether to go ahead")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}
