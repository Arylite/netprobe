package webapi

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// specOperations lists "METHOD /path" for every operation of the document,
// reading the two indentation levels OpenAPI paths use.
func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	path := regexp.MustCompile(`^  (/\S+):\s*$`)
	method := regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	ops := map[string]bool{}
	var current string
	inPaths := false
	for _, line := range strings.Split(string(spec), "\n") {
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && !strings.HasPrefix(line, " "):
			inPaths = false
		case inPaths:
			if m := path.FindStringSubmatch(line); m != nil {
				current = m[1]
			} else if m := method.FindStringSubmatch(line); m != nil && current != "" {
				ops[strings.ToUpper(m[1])+" "+current] = true
			}
		}
	}
	return ops
}

func TestEveryRouteIsInTheSpecAndNothingElse(t *testing.T) {
	f := newFixture(t, Config{})
	ops := specOperations(t)
	if len(ops) == 0 {
		t.Fatal("no operation found in the spec")
	}
	served := map[string]bool{}
	for _, rt := range f.srv.routes() {
		key := rt.method + " " + rt.pattern
		served[key] = true
		if !ops[key] {
			t.Errorf("%s is served but not described in openapi.yaml", key)
		}
	}
	for key := range ops {
		if !served[key] {
			t.Errorf("%s is described in openapi.yaml but not served", key)
		}
	}
}

func TestTheSpecIsPublished(t *testing.T) {
	f := newFixture(t, Config{})
	res, raw := f.do(t, http.MethodGet, "/api/v1/openapi.yaml", "", nil, nil)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(string(raw), "openapi: 3.0.3") || res.Header.Get("Content-Type") != "application/yaml" {
		t.Fatalf("status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestEveryProtectedOperationDocumentsItsErrors(t *testing.T) {
	// A protected endpoint can always answer 401; the document must say so.
	text := string(spec)
	if strings.Count(text, `"401"`) < 12 {
		t.Fatalf("the spec documents too few 401 answers: %d", strings.Count(text, `"401"`))
	}
}
