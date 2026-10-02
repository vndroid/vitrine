package server

import (
	"regexp"
	"strings"
	"testing"
)

// Every address of a frontend file in a page carries the version of the
// program, so a CDN that caches by address fetches the files again after an
// upgrade.
func TestAssetsCarryTheVersion(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	for _, target := range []string{"/", "/-/admin"} {
		body := do(s, "GET", target, "", nil).Body.String()
		for _, want := range []string{
			`href="/_vitrine/public/images/favicon/favicon-16-32.ico?v=test"`,
			`href="/_vitrine/public/images/favicon/favicon-152.png?v=test"`,
			`href="/_vitrine/public/css/styles.css?v=test"`,
			`<script src="/_vitrine/public/js/scripts.js?v=test"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s misses %s", target, want)
			}
		}
		// no address of a frontend file without the version
		re := regexp.MustCompile(`(?:src|href)="/_vitrine/public/[^"?]*"`)
		if bare := re.FindAllString(body, -1); len(bare) != 0 {
			t.Errorf("%s: addresses without the version: %v", target, bare)
		}
	}
}

func TestFallbackImagesCarryTheVersion(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	body := do(s, "GET", "/sub/", "", nil).Body.String()
	for _, want := range []string{
		`src="/_vitrine/public/images/fallback/folder-parent.png?v=test"`,
		`src="/_vitrine/public/images/fallback/file.png?v=test"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the listing misses %s", want)
		}
	}
	if regexp.MustCompile(`src="/_vitrine/public/images/fallback/[^"?]*"`).MatchString(body) {
		t.Error("a fallback image without the version")
	}
}

// The client builds the addresses of images from the setup.
func TestSetupHasTheAssetVersion(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	setup := post(t, s, `{"action":"get","setup":true}`)["setup"].(map[string]any)
	if setup["ASSET_VERSION"] != "test" {
		t.Errorf("ASSET_VERSION = %v", setup["ASSET_VERSION"])
	}
}

// The parameter does not change what is served: same file, same ETag.
func TestVersionedAssetIsServed(t *testing.T) {
	s, _ := newTestServer(t, fixtureOpts{})
	plain := do(s, "GET", "/_vitrine/public/js/scripts.js", "", nil)
	versioned := do(s, "GET", "/_vitrine/public/js/scripts.js?v=test", "", nil)
	other := do(s, "GET", "/_vitrine/public/js/scripts.js?v=0.0.1", "", nil)
	if versioned.Code != 200 || versioned.Body.String() != plain.Body.String() {
		t.Fatalf("versioned = %d, same body: %v", versioned.Code, versioned.Body.String() == plain.Body.String())
	}
	if versioned.Header().Get("ETag") == "" || versioned.Header().Get("ETag") != plain.Header().Get("ETag") || other.Header().Get("ETag") != plain.Header().Get("ETag") {
		t.Errorf("ETags differ: %q %q %q", plain.Header().Get("ETag"), versioned.Header().Get("ETag"), other.Header().Get("ETag"))
	}
	if rec := do(s, "GET", "/_vitrine/public/js/scripts.js?v=test", "", map[string]string{"If-None-Match": versioned.Header().Get("ETag")}); rec.Code != 304 {
		t.Errorf("If-None-Match = %d", rec.Code)
	}
	if cc := versioned.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q: the parameter must not change the revalidation", cc)
	}
}

func TestAssetVersionFallbackAndEscaping(t *testing.T) {
	for version, want := range map[string]string{"": "dev", "0.4.7": "0.4.7"} {
		s, _ := newTestServer(t, fixtureOpts{})
		s.version = version
		if got := s.assetVersion(); got != want {
			t.Errorf("assetVersion(%q) = %q, want %q", version, got, want)
		}
	}
	// a version with characters that need escaping stays one parameter
	s, _ := newTestServer(t, fixtureOpts{})
	s.version = `1.0.0-rc1+a&b"<c>`
	body := do(s, "GET", "/-/admin", "", nil).Body.String()
	if !strings.Contains(body, `styles.css?v=1.0.0-rc1%2ba%26b%22%3cc%3e"`) {
		t.Errorf("version not escaped: %s", regexp.MustCompile(`css/styles.css[^"]*"`).FindString(body))
	}
}
