package main

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const prefix = notificationPrefix

func TestCoverageGate(t *testing.T) {
	valid := "mode: atomic\n" + prefix + "/model.go:1.1,2.1 85 1\n" + prefix + "/model.go:3.1,4.1 15 0\n" + prefix + "/delivery/client.go:1.1,2.1 100 1\n" + prefix + "/store/store.go:1.1,2.1 100 1\n"
	for _, tc := range []struct {
		name, profile string
		pass          bool
	}{
		{"threshold", valid, true}, {"empty", "", false}, {"wrong mode", strings.Replace(valid, "atomic", "set", 1), false},
		{"weak package", strings.Replace(valid, "85 1", "84 1", 1), false}, {"missing package", strings.ReplaceAll(valid, prefix+"/store/", "unrelated/"), false},
		{"empty package", strings.Replace(valid, "100 1", "0 0", 1), false}, {"negative", strings.Replace(valid, "85 1", "-1 1", 1), false},
		{"duplicate", valid + prefix + "/model.go:1.1,2.1 85 1\n", false},
		{"new production package", valid + prefix + "/new/file.go:1.1,2.1 100 0\n", false},
		{"fixture exclusion", valid + prefix + "/notificationtest/file.go:1.1,2.1 100 0\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := check(strings.NewReader(tc.profile), io.Discard, gateFor(notificationPrefix, 85))
			if (err == nil) != tc.pass {
				t.Fatal(err)
			}
		})
	}
}

// -prefix and -min select another package tree and floor; the defaults keep
// the notification gate (spec 002 T010).
func TestCoverageGateFlags(t *testing.T) {
	const textsafe = "github.com/skaphos/oiax/v2/internal/textsafe"
	profile := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "cover.out")
		if err := os.WriteFile(p, []byte("mode: atomic\n"+body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	textsafeAt := func(covered int) string {
		return textsafe + "/textsafe.go:1.1,2.1 " + strconv.Itoa(covered) + " 1\n" +
			textsafe + "/textsafe.go:3.1,4.1 " + strconv.Itoa(1000-covered) + " 0\n"
	}
	notif := prefix + "/model.go:1.1,2.1 85 1\n" + prefix + "/model.go:3.1,4.1 15 0\n" +
		prefix + "/delivery/client.go:1.1,2.1 100 1\n" + prefix + "/store/store.go:1.1,2.1 100 1\n"
	for _, tc := range []struct {
		name string
		args []string
		body string
		pass bool
	}{
		{"defaults keep the notification gate", nil, notif, true},
		{"defaults reject a weak notification package", nil, strings.Replace(notif, "85 1", "84 1", 1), false},
		{"defaults ignore other packages", nil, notif + textsafeAt(0), true},
		{"textsafe at 94.9% fails", []string{"-prefix", textsafe, "-min", "95"}, textsafeAt(949), false},
		{"textsafe at 95.0% passes", []string{"-prefix", textsafe, "-min", "95"}, textsafeAt(950), true},
		{"textsafe gate ignores notification", []string{"-prefix", textsafe, "-min", "95"}, textsafeAt(950) + notif, true},
		{"textsafe gate requires the package", []string{"-prefix", textsafe, "-min", "95"}, notif, false},
		{"min out of range", []string{"-prefix", textsafe, "-min", "101"}, textsafeAt(950), false},
		{"empty prefix", []string{"-prefix", "", "-min", "95"}, textsafeAt(950), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"-profile", profile(t, tc.body)}, tc.args...)
			if err := run(args, io.Discard); (err == nil) != tc.pass {
				t.Fatalf("run(%v) = %v, want pass=%v", tc.args, err, tc.pass)
			}
		})
	}
}
