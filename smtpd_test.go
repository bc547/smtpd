//
// Let's see if I can write Go tests

package smtpd

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

// This should contain only things that are actually valid. Do not test
// error handling here.
var smtpValidTests = []struct {
	line string  // Input line
	cmd  Command // Output SMTP command
	arg  string  // Output argument
}{
	{"HELO localhost", HELO, "localhost"},
	{"HELO", HELO, ""},
	{"EHLO fred", EHLO, "fred"},
	{"EHLO", EHLO, ""},
	{"MAIL FROM:<>", MAILFROM, ""},
	{"MAIL FROM:<fred@example.com>", MAILFROM, "fred@example.com"},
	{"RCPT TO:<fred@example.com>", RCPTTO, "fred@example.com"},
	{"DATA", DATA, ""},
	{"QUIT", QUIT, ""},
	{"RSET", RSET, ""},
	{"NOOP", NOOP, ""},
	{"VRFY fred@example.org", VRFY, "fred@example.org"},
	{"EXPN fred@example.net", EXPN, "fred@example.net"},
	{"HELP barney", HELP, "barney"},
	{"HELP", HELP, ""},
	{"STARTTLS", STARTTLS, ""},
	{"AUTH PLAIN dGVzdAB0ZXN0ADEyMzQ=", AUTH, "PLAIN dGVzdAB0ZXN0ADEyMzQ="},

	// Torture cases.
	{"RCPT TO:<a>", RCPTTO, "a"}, // Minimal address
	{"HELO    ", HELO, ""},       // all blank optional argument
	{"HELO   a    ", HELO, "a"},  // whitespace in argument

	// Accepted as valid by ParseCmd even if they're wrong by the views
	// of higher layers.
	{"RCPT TO:<>", RCPTTO, ""},
	{"MAIL FROM:<<>>", MAILFROM, "<>"},
	{"MAIL FROM:<barney>", MAILFROM, "barney"},

	// Extended MAIL FROM and RCPT TO with additional arguments.
	{"MAIL FROM:<fred@example.mil> SIZE=10000", MAILFROM, "fred@example.mil"},
	{"RCPT TO:<fred@example.mil> SIZE=100", RCPTTO, "fred@example.mil"},

	// commands in lower case and mixed case, preserving argument case
	{"mail from:<FreD@Barney>", MAILFROM, "FreD@Barney"},
	{"Rcpt To:<joe@joe>", RCPTTO, "joe@joe"},
}

func TestGoodParses(t *testing.T) {
	var s ParsedLine
	for _, inp := range smtpValidTests {
		s = ParseCmd(inp.line)
		if s.cmd != inp.cmd {
			t.Fatalf("mismatched CMD result on '%s': got %v wanted %v", inp.line, s.cmd, inp.cmd)
		}
		if len(s.err) > 0 {
			t.Fatalf("command failed on '%s': error '%s'", inp.line, s.err)
		}
		if inp.arg != s.arg {
			t.Fatalf("mismatched arg results on '%s': got %v expected %v", inp.line, s.arg, inp.arg)
		}
	}
}

// We mostly don't match on the exact error text.
var smtpInvalidTests = []struct {
	line string  // Input line
	cmd  Command // Output SMTP command
	err  string  // Output err to check if non-empty
}{
	{"argble", BadCmd, ""},
	// UTF-8, and I want to test that this is specifically recognized
	// in an otherwise valid command
	{"MAIL FROM:<Å@fred.com>", BadCmd, "command contains non 7-bit ASCII"},

	// prefix validation
	{"VRFYFred", BadCmd, ""},
	{"MAIL FROMFred", BadCmd, ""},

	// malformed or missing addresses
	{"MAIL FROM <fred>", MAILFROM, ""},
	{"RCPT TO: <fred> ", RCPTTO, ""},
	{"MAIL FROM:", MAILFROM, ""},
	{"MAIL FROM:<", MAILFROM, ""},
	{"MAIL FROM:<fred@barney", MAILFROM, ""},

	// no space between > and param
	{"MAIL FROM:<fred@barney>SIZE=100", MAILFROM, ""},

	// No arguments
	{"VRFY", VRFY, ""},
	{"EXPN", EXPN, ""},
	{"AUTH", AUTH, ""},

	// Extra arguments on commands that don't take them.
	{"RSET fred", RSET, ""},
	{"NOOP fred", NOOP, ""},
	{"DATA fred", DATA, ""},
	{"QUIT fred", QUIT, ""},
}

func TestBadParses(t *testing.T) {
	var s ParsedLine
	for _, inp := range smtpInvalidTests {
		s = ParseCmd(inp.line)
		if len(s.err) == 0 {
			t.Fatalf("'%s' not detected as error: cmd %v arg '%v'", inp.line, s.cmd, s.arg)
		}
		if inp.cmd != s.cmd {
			t.Fatalf("mismatched CMD on '%s': got %v expected %v", inp.line, s.cmd, inp.cmd)
		}

		if len(inp.err) > 0 && inp.err != s.err {
			t.Fatalf("wrong error string on '%s': got '%s' expected '%s'", inp.line, s.err, inp.err)
		}
	}
}

// This is a very quick test for basic functionality.
func TestParam(t *testing.T) {
	s := ParseCmd("MAIL FROM:<fred@barney.com> SIZE=1000")
	// We assume that basic parsing works and don't check.
	if s.params != "SIZE=1000" {
		t.Fatalf("MAIL FROM params failed: expected 'SIZE=1000', got '%s'", s.params)
	}
	s = ParseCmd("MAIL FROM:<fred@barney.com>")
	if len(s.params) > 0 {
		t.Fatalf("MAIL FROM w/o params got a parms value of: '%s'", s.params)
	}
}

//
// -------
// Current tests are crude because Server() API is not exactly settled.
// We're really testing the sequencing logic, both for accepting a good
// transaction and rejecting out of sequence things.
//
// TODO
// Testing literal text output is a losing approach. What we should do
// is mostly test that the response codes are what we expect. Possibly
// we should connect an instance of the Go SMTP client to the server and
// verify that that works and sees the right EHLO things, once we support
// EHLO things that is.
//

// returns expected server output \r\n'd, and the actual output.
// current approach cribbed from the net/smtp tests.
func runSmtpTest(serverStr, clientStr string) (string, string) {
	server := strings.Join(strings.Split(serverStr, "\n"), "\r\n")
	client := strings.Join(strings.Split(clientStr, "\n"), "\r\n")

	var outbuf bytes.Buffer
	writer := bufio.NewWriter(&outbuf)
	reader := strings.NewReader(client)

	Server(reader, writer)
	writer.Flush()
	return server, outbuf.String()
}
func TestBasicSmtpd(t *testing.T) {
	server, actualout := runSmtpTest(basicServer, basicClient)
	if actualout != server {
		t.Fatalf("Got:\n%s\nExpected:\n%s", actualout, server)
	}
}
// EHLO, send email, send email again, try what should be an out of
// sequence RCPT TO.
var basicClient = `EHLO localhost
MAIL FROM:<a@b.com>
RCPT TO:<c@d.org>
DATA
Subject: A test

Done.
.
MAIL FROM:<a1@b.com>
RCPT TO:<c1@d.org>
DATA
Subject: A test 2

Done. 2.
.
RCPT TO:<e@f.com>
QUIT
`
var basicServer =`220 Hello there
250 localhost Hello whoever you are
250 Okay, I'll believe you for now
250 Okay, I'll believe you for now
354 Send away
250 I've put it in a can
250 Okay, I'll believe you for now
250 Okay, I'll believe you for now
354 Send away
250 I've put it in a can
503 Out of sequence command
221 Goodbye
`

func TestSequenceErrors(t *testing.T) {
	server, actualout := runSmtpTest(sequenceServer, sequenceClient)
	if actualout != server {
		t.Fatalf("Got:\n%s\nExpected:\n%s", actualout, server)
	}
}
// A whole series of out of sequence commands, and finally an unrecognized
// one. We try a RSET to validate that it doesn't allow us to MAIL FROM
// without an EHLO.
var sequenceClient = `MAIL FROM:<a@b.com>
RSET
MAIL FROM:<a@b.com>
EHLO localhost
NOOP
RCPT TO:<c@d.com>
MAIL FROM:<a@b.com>
DATA
Subject: yadda yadda
`
var sequenceServer = `220 Hello there
503 Out of sequence command
250 Okay
503 Out of sequence command
250 localhost Hello whoever you are
250 Okay
503 Out of sequence command
250 Okay, I'll believe you for now
503 Out of sequence command
501 Bad: unrecognized command
`
