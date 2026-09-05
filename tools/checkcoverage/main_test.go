package main

import (
	"io"
	"strings"
	"testing"
)

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
			err := check(strings.NewReader(tc.profile), io.Discard)
			if (err == nil) != tc.pass {
				t.Fatal(err)
			}
		})
	}
}
