// Command checkcoverage enforces a per-package statement coverage floor over
// a Go atomic coverage profile, for every package under a module path prefix.
// Defaults enforce the notification suite's 85% floor; -prefix and -min
// select another package tree and floor (spec 002: internal/textsafe, 95%).
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// notificationPrefix is the default package tree.
const notificationPrefix = "github.com/skaphos/oiax/v2/internal/notification"

type totals struct{ statements, covered int64 }

// gate is what one run enforces.
type gate struct {
	// prefix selects the packages: the prefix itself and everything below.
	prefix string
	// min is the floor, in percent, every selected package must meet.
	min float64
	// required packages must appear in the profile.
	required []string
	// excluded packages are test-only fixtures and are not counted.
	excluded []string
}

// gateFor returns the gate for a prefix and floor. The notification tree
// keeps its required production packages and fixture exclusion; any other
// tree requires only the prefix package itself.
func gateFor(prefix string, min float64) gate {
	if prefix == notificationPrefix {
		return gate{
			prefix:   prefix,
			min:      min,
			required: []string{prefix, prefix + "/delivery", prefix + "/store"},
			excluded: []string{prefix + "/notificationtest"},
		}
	}
	return gate{prefix: prefix, min: min, required: []string{prefix}}
}

func check(r io.Reader, w io.Writer, g gate) error {
	prefix := g.prefix
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
		if slices.Contains(g.excluded, pkg) {
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
	for _, pkg := range g.required {
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
		// Compare in hundredths of a percent, in integers, so 95.00% passes a
		// 95 floor exactly.
		if t.covered*10000 < t.statements*int64(math.Round(g.min*100)) {
			problems = append(problems, fmt.Errorf("%s: below %g%% coverage", name, g.min))
		}
	}
	return errors.Join(problems...)
}

func run(args []string, w io.Writer) error {
	flags := flag.NewFlagSet("checkcoverage", flag.ContinueOnError)
	flags.SetOutput(w)
	profile := flags.String("profile", "coverage-notifications.out", "atomic coverage profile")
	prefix := flags.String("prefix", notificationPrefix, "module path prefix of the packages to check")
	min := flags.Float64("min", 85, "minimum statement coverage per package, in percent")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *prefix == "" || *min < 0 || *min > 100 {
		return errors.New("-prefix must be set and -min must be between 0 and 100")
	}
	f, err := os.Open(*profile)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return check(f, w, gateFor(*prefix, *min))
}
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
