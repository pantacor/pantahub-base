package oauth

import (
	"encoding/json"
	"testing"

	"golang.org/x/oauth2"
)

func TestProviderPayloadsExposeStableIDs(t *testing.T) {
	var google googlePayload
	if err := json.Unmarshal([]byte(`{"id":"google-123","email":"user@example.com","verified_email":true}`), &google); err != nil {
		t.Fatal(err)
	}
	if google.ID != "google-123" {
		t.Fatalf("Google ID = %q", google.ID)
	}

	var github githubPayload
	if err := json.Unmarshal([]byte(`{"id":456789,"login":"user"}`), &github); err != nil {
		t.Fatal(err)
	}
	if github.ID != 456789 {
		t.Fatalf("GitHub ID = %d", github.ID)
	}

	var gitlab gitlabPayload
	if err := json.Unmarshal([]byte(`{"id":987654,"username":"user","state":"active"}`), &gitlab); err != nil {
		t.Fatal(err)
	}
	if gitlab.ID != 987654 {
		t.Fatalf("GitLab ID = %d", gitlab.ID)
	}

	var entra entraidPayload
	if err := json.Unmarshal([]byte(`{"id":"entra-789","userPrincipalName":"user@example.com"}`), &entra); err != nil {
		t.Fatal(err)
	}
	if entra.ID != "entra-789" {
		t.Fatalf("Entra object ID = %q", entra.ID)
	}
	tenantScoped, err := entraTenantScopedID(&oauth2.Token{
		AccessToken: "eyJhbGciOiJub25lIn0.eyJ0aWQiOiJ0ZW5hbnQtMTIzIn0.signature",
	}, entra.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tenantScoped != "tenant-123:entra-789" {
		t.Fatalf("tenant-scoped Entra ID = %q", tenantScoped)
	}
}
