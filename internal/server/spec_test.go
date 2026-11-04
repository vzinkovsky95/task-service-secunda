package server

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/vzinkovsky95/task-service-secunda/api"
)

func specDocument(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(api.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return doc
}

func specOperations(doc *openapi3.T) []string {
	var operations []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			operations = append(operations, method+" "+path)
		}
	}
	sort.Strings(operations)
	return operations
}

func TestRoutesMatchTheOpenAPISpecInBothDirections(t *testing.T) {
	_, patterns := build(testDeps())
	routes := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		routes = append(routes, strings.ReplaceAll(pattern, "{$}", ""))
	}
	sort.Strings(routes)
	spec := specOperations(specDocument(t))

	for _, route := range routes {
		if !slices.Contains(spec, route) {
			t.Errorf("route %q is registered in router.go but missing from openapi.yaml", route)
		}
	}
	for _, operation := range spec {
		if !slices.Contains(routes, operation) {
			t.Errorf("operation %q is described in openapi.yaml but no such route is registered", operation)
		}
	}
	if len(routes) != len(slices.Compact(slices.Clone(routes))) {
		t.Error("a route is registered twice")
	}
}

var errorCodePattern = regexp.MustCompile(`(?:apperr\.New\(apperr\.\w+|WriteError\(w, r, [\w.]+), "([a-z_]+)"`)

func codesInSource(t *testing.T) []string {
	t.Helper()
	found := map[string]bool{}
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range errorCodePattern.FindAllStringSubmatch(string(source), -1) {
			found[match[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	codes := make([]string, 0, len(found))
	for code := range found {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func TestErrorCodesMatchTheOpenAPISpecInBothDirections(t *testing.T) {
	doc := specDocument(t)
	codeSchema := doc.Components.Schemas["Error"].Value.Properties["error"].Value.Properties["code"].Value

	var documented []string
	for _, value := range codeSchema.Enum {
		documented = append(documented, value.(string))
	}
	sort.Strings(documented)

	inSource := codesInSource(t)
	if len(inSource) < 15 {
		t.Fatalf("found only %d error codes in the source, the scanner is broken: %v", len(inSource), inSource)
	}
	for _, code := range inSource {
		if !slices.Contains(documented, code) {
			t.Errorf("error code %q is returned by the code but is not in the openapi.yaml enum", code)
		}
	}
	for _, code := range documented {
		if !slices.Contains(inSource, code) {
			t.Errorf("error code %q is in the openapi.yaml enum but nothing returns it", code)
		}
	}
}

func TestEveryDocumentedErrorCodeIsMentionedWhereItCanHappen(t *testing.T) {
	doc := specDocument(t)
	text := string(api.Spec)
	for _, value := range doc.Components.Schemas["Error"].Value.Properties["error"].Value.Properties["code"].Value.Enum {
		code := value.(string)
		if strings.Count(text, "`"+code+"`") == 0 {
			t.Errorf("error code %q is in the enum but never explained in a description", code)
		}
	}
}
