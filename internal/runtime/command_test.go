package runtime

import "testing"

func TestFormatCommand(t *testing.T) {
	got := FormatCommand("/opt/engine", []string{"-m", "/m/model.gguf", "--temp", "0.7", "-p", "it's here", ""})
	want := `/opt/engine -m /m/model.gguf --temp 0.7 -p 'it'\''s here' ''`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
