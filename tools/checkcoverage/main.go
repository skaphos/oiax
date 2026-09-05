// Command checkcoverage enforces independent notification package coverage.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

const prefix = "github.com/skaphos/oiax/internal/notification"

type totals struct{ statements, covered int64 }

func check(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() || scanner.Text() != "mode: atomic" {
		return errors.New("coverage must be nonempty atomic-mode Go coverage")
	}
	packages := map[string]totals{}
	blocks := map[string]bool{}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			return errors.New("malformed coverage record")
		}
		index := strings.LastIndex(fields[0], ":")
		if index < 0 {
			return errors.New("malformed coverage location")
		}
		path := fields[0][:index]
		slash := strings.LastIndex(path, "/")
		if slash < 0 {
			return errors.New("malformed package path")
		}
		pkg := path[:slash]
		statements, e1 := strconv.ParseInt(fields[1], 10, 32)
		count, e2 := strconv.ParseInt(fields[2], 10, 64)
		if e1 != nil || e2 != nil || statements < 0 || count < 0 {
			return errors.New("invalid coverage counts")
		}
		if pkg != prefix && !strings.HasPrefix(pkg, prefix+"/") {
			continue
		}
		if pkg == prefix+"/notificationtest" {
			continue
		} // explicitly test-only fixture support
		if blocks[fields[0]] {
			return errors.New("duplicate coverage block")
		}
		blocks[fields[0]] = true
		t := packages[pkg]
		t.statements += statements
		if count > 0 {
			t.covered += statements
		}
		packages[pkg] = t
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for _, pkg := range []string{prefix, prefix + "/delivery", prefix + "/store"} {
		if _, ok := packages[pkg]; !ok {
			return fmt.Errorf("missing required production package %s", pkg)
		}
	}
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	var problems []error
	for _, name := range names {
		t := packages[name]
		if t.statements == 0 {
			problems = append(problems, fmt.Errorf("%s: empty production suite", name))
			continue
		}
		if _, err := fmt.Fprintf(w, "%s: %.2f%% (%d/%d statements)\n", name, 100*float64(t.covered)/float64(t.statements), t.covered, t.statements); err != nil {
			return err
		}
		if t.covered*100 < t.statements*85 {
			problems = append(problems, fmt.Errorf("%s: below 85%% coverage", name))
		}
	}
	return errors.Join(problems...)
}

func run(args []string, w io.Writer) error {
	flags := flag.NewFlagSet("checkcoverage", flag.ContinueOnError)
	flags.SetOutput(w)
	profile := flags.String("profile", "coverage-notifications.out", "atomic coverage profile")
	if err := flags.Parse(args); err != nil {
		return err
	}
	f, err := os.Open(*profile)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return check(f, w)
}
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
