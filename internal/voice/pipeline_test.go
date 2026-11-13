package voice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// G-VOC: an offline pipeline: a recorder writes {wav}, then a local
// speech-to-text program reads it and prints the words.

func TestPipelineIsConfiguredAndShown(t *testing.T) {
	s := Settings{Record: []string{"arecord", "-d", "{seconds}", "{wav}"}, Transcribe: []string{"whisper-cli", "-l", "{lang}", "-f", "{wav}"}}
	if !s.Configured() {
		t.Fatal("record + transcribe is set up")
	}
	show := s.Show()
	if !strings.Contains(show, "arecord -d 60 <recording>.wav") || !strings.Contains(show, "whisper-cli -l auto -f <recording>.wav") {
		t.Fatalf("card text: %q", show)
	}
	if (Settings{Record: []string{"arecord"}}).Configured() {
		t.Fatal("a recorder alone is not set up")
	}
	if got := (Settings{Command: []string{"dict", "{seconds}"}}).Show(); got != "dict 60" {
		t.Fatalf("command card text: %q", got)
	}
}

func TestPipelineRecordsThenTranscribes(t *testing.T) {
	unixOnly(t)
	var wavSeen string
	s := Settings{
		Record:     []string{"sh", "-c", `echo audio > "$0"`, "{wav}"},
		Transcribe: []string{"sh", "-c", `cat "$0"; echo "[00:00:00.000 --> 00:00:01.000] words"`, "{wav}"},
	}
	sess, err := Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	wavSeen = sess.wav
	r := sess.Wait()
	if r.Err != nil || r.Text != "audio words" {
		t.Fatalf("result %+v", r)
	}
	if _, err := os.Stat(filepath.Dir(wavSeen)); !os.IsNotExist(err) {
		t.Fatalf("the recording must be deleted: %v", err)
	}
}

func TestPipelineStopEndsTheRecording(t *testing.T) {
	unixOnly(t)
	s := Settings{
		Record:     []string{"sh", "-c", `trap 'echo heard > "$0"; exit 0' INT; echo ready; while :; do sleep 0.05; done`, "{wav}"},
		Transcribe: []string{"sh", "-c", `cat "$0"`, "{wav}"},
	}
	sess, err := Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(sess.out.String(), "ready") })
	sess.Stop()
	r := sess.Wait()
	if r.Err != nil || r.Text != "heard" {
		t.Fatalf("result %+v", r)
	}
}

func TestPipelineRecorderSavedNothing(t *testing.T) {
	unixOnly(t)
	s := Settings{Record: []string{"sh", "-c", "echo no microphone >&2"}, Transcribe: []string{"cat", "{wav}"}}
	sess, err := Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	r := sess.Wait()
	if r.Err == nil || !strings.Contains(r.Err.Error(), "no microphone") {
		t.Fatalf("result %+v", r)
	}
}

func TestPipelineTranscriberFails(t *testing.T) {
	unixOnly(t)
	s := Settings{Record: []string{"sh", "-c", `echo a > "$0"`, "{wav}"}, Transcribe: []string{"sh", "-c", "echo model not found >&2; exit 2"}}
	sess, err := Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	r := sess.Wait()
	if r.Err == nil || !strings.Contains(r.Err.Error(), "model not found") {
		t.Fatalf("result %+v", r)
	}
}

func TestPipelineCancelWhileTranscribing(t *testing.T) {
	unixOnly(t)
	s := Settings{Record: []string{"sh", "-c", `echo a > "$0"`, "{wav}"}, Transcribe: []string{"sh", "-c", "echo secret; echo busy >&2; exec sleep 30"}}
	sess, err := Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return sess.transcribing() })
	sess.Cancel()
	r := sess.Wait()
	if !r.Canceled || r.Text != "" {
		t.Fatalf("result %+v", r)
	}
}
