package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(Spec)
	if err != nil {
		t.Fatalf("openapi.yaml does not load: %v", err)
	}
	return doc
}

func TestSpecIsAValidOpenAPIDocument(t *testing.T) {
	if err := loadSpec(t).Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml is not valid: %v", err)
	}
}

func TestEveryOperationIsDocumentedCompletely(t *testing.T) {
	doc := loadSpec(t)
	operationIDs := map[string]string{}

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			name := method + " " + path
			if op.OperationID == "" || op.Summary == "" || len(op.Tags) == 0 {
				t.Errorf("%s: operationId, summary and tags are required", name)
			}
			if other, dup := operationIDs[op.OperationID]; dup {
				t.Errorf("%s: operationId %q is already used by %s", name, op.OperationID, other)
			}
			operationIDs[op.OperationID] = name

			if !strings.HasPrefix(path, "/api/v1/") {
				continue
			}
			public := op.Security != nil && len(*op.Security) == 0
			for _, code := range []string{"429", "500"} {
				if op.Responses.Value(code) == nil {
					t.Errorf("%s: response %s is not documented", name, code)
				}
			}
			if !public && op.Responses.Value("401") == nil {
				t.Errorf("%s: requires a token but documents no 401", name)
			}
			if public && op.Responses.Value("401") != nil && path != "/api/v1/login" {
				t.Errorf("%s: public operation documents a 401", name)
			}
		}
	}
}

func TestBearerJWTIsTheOnlySecurityScheme(t *testing.T) {
	doc := loadSpec(t)
	schemes := doc.Components.SecuritySchemes
	if len(schemes) != 1 {
		t.Fatalf("security schemes = %d, want 1", len(schemes))
	}
	scheme := schemes["bearerAuth"].Value
	if scheme == nil || scheme.Type != "http" || scheme.Scheme != "bearer" || scheme.BearerFormat != "JWT" {
		t.Errorf("bearerAuth = %+v", scheme)
	}
	if doc.Security == nil || len(doc.Security) != 1 {
		t.Error("bearerAuth must apply to every operation by default")
	}
}

func TestSwaggerUIPageIsPinnedAndVerified(t *testing.T) {
	rec := httptest.NewRecorder()
	UIHandler()(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	page := rec.Body.String()

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
	if strings.Contains(page, "@latest") || strings.Contains(page, "swagger-ui-dist/") {
		t.Error("Swagger UI must be pinned to an exact version")
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(SwaggerUIVersion) {
		t.Errorf("version %q is not an exact semver", SwaggerUIVersion)
	}
	if got := strings.Count(page, "swagger-ui-dist@"+SwaggerUIVersion+"/"); got != 2 {
		t.Errorf("the pinned version appears %d times, want 2 (css and js)", got)
	}
	if got := strings.Count(page, `integrity="sha384-`); got != 2 {
		t.Errorf("integrity attributes = %d, want 2", got)
	}
	if !strings.Contains(page, `url: "./openapi.yaml"`) || !strings.Contains(page, "persistAuthorization: true") {
		t.Error("the page must load the served spec and keep the token across reloads")
	}
}

func TestSpecHandlerServesTheEmbeddedFile(t *testing.T) {
	rec := httptest.NewRecorder()
	SpecHandler()(rec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	if rec.Body.String() != string(Spec) {
		t.Error("served body differs from the embedded spec")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("content type = %q", ct)
	}
}

func TestPagesSiteUsesTheSamePinnedSwaggerUIAsTheService(t *testing.T) {
	site, err := os.ReadFile("../site/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(site)

	for _, attribute := range regexp.MustCompile(`integrity="sha384-[^"]+"`).FindAllString(uiPage, -1) {
		if !strings.Contains(page, attribute) {
			t.Errorf("site/index.html lacks %s used by the service page", attribute)
		}
	}
	if got := strings.Count(page, "swagger-ui-dist@"+SwaggerUIVersion+"/"); got != 2 {
		t.Errorf("site/index.html references the pinned version %d times, want 2", got)
	}
	if strings.Contains(page, "@latest") {
		t.Error("site/index.html must be pinned to an exact version")
	}
	if !strings.Contains(page, "supportedSubmitMethods: []") {
		t.Error("Try it out must be disabled on a static page without a backend")
	}
	if !strings.Contains(page, "не работающий стенд") || !strings.Contains(page, `url: "./openapi.yaml"`) {
		t.Error("the page must say it is documentation only and load the published spec")
	}
}
