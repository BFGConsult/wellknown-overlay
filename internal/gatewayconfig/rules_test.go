package gatewayconfig

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
)

func TestRenderOrderedRulesPreservesDeclarationOrder(t *testing.T) {
	cfg := orderedRuleTestConfig(t, `{
  "rules": [
    {"match":{"path":"/robots.txt"},"file":"robots-overlay.txt","content_type":"text/plain; charset=utf-8"},
    {"match":{"path":"/sitemap.xml"},"status":404},
    {"match":{"hosts":["efnu.no"]},"redirect":{"origin":"https://efn.no","status":308,"preserve_request_uri":true}},
    {"match":{"hosts":["www.efnu.no"]},"redirect":{"origin":"https://www.efn.no","status":308,"preserve_request_uri":true}}
  ]
}`)

	var ruleMap bytes.Buffer
	RenderOrderedRuleMap(&ruleMap, cfg)
	mapConfig := ruleMap.String()
	wantsInOrder := []string{
		`~^[^|]*\|\x2Frobots\x2Etxt$ 1;`,
		`~^[^|]*\|\x2Fsitemap\x2Exml$ 2;`,
		`~^(?:efnu\x2Eno)\|.*$ 3;`,
		`~^(?:www\x2Eefnu\x2Eno)\|.*$ 4;`,
	}
	last := -1
	for _, want := range wantsInOrder {
		at := strings.Index(mapConfig, want)
		if at < 0 {
			t.Fatalf("map does not contain %q:\n%s", want, mapConfig)
		}
		if at <= last {
			t.Fatalf("map entry %q is out of declaration order:\n%s", want, mapConfig)
		}
		last = at
	}

	var server bytes.Buffer
	RenderOrderedRuleServer(&server, cfg, "/srv/efnu")
	serverConfig := server.String()
	for _, want := range []string{
		`if ($wellknown_overlay_rule = 1)`,
		`alias "/srv/efnu/robots-overlay.txt"`,
		`return 404`,
		`return 308 "https://efn.no$request_uri"`,
		`return 308 "https://www.efn.no$request_uri"`,
	} {
		if !strings.Contains(serverConfig, want) {
			t.Fatalf("server config does not contain %q:\n%s", want, serverConfig)
		}
	}
}

func TestRenderOrderedRuleMapSupportsExplicitCatchAll(t *testing.T) {
	cfg := orderedRuleTestConfig(t, `{"rules":[{"match":"*","status":404}]}`)
	var out bytes.Buffer
	RenderOrderedRuleMap(&out, cfg)
	if !strings.Contains(out.String(), `~^.*$ 1;`) {
		t.Fatalf("map does not contain explicit catch-all:\n%s", out.String())
	}
}

func orderedRuleTestConfig(t *testing.T, body string) overlay.Config {
	t.Helper()
	dir := t.TempDir()
	configPath := dir + "/overlay.json"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := overlay.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
