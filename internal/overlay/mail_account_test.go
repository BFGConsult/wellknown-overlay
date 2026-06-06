package overlay

import "testing"

func TestMailAccountSelectProfileChoosesExactBeforeWildcard(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@example.org", "Wildcard"),
		testMailAccountProfile("bfg@example.org", "Exact"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfile("BFG@EXAMPLE.ORG")
	if profile.DisplayName != "Exact" {
		t.Fatalf("selected %q, want Exact", profile.DisplayName)
	}
}

func TestMailAccountSelectProfileChoosesMoreSpecificGlob(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@example.org", "Domain"),
		testMailAccountProfile("*+bfg@example.org", "Tagged"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfile("post+bfg@example.org")
	if profile.DisplayName != "Tagged" {
		t.Fatalf("selected %q, want Tagged", profile.DisplayName)
	}
}

func TestMailAccountSelectProfileUsesFirstMatchAsTieBreaker(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*+bfg@example.org", "First"),
		testMailAccountProfile("tag+*@example.org", "Second"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfile("tag+bfg@example.org")
	if profile.DisplayName != "First" {
		t.Fatalf("selected %q, want First", profile.DisplayName)
	}
}

func TestMailAccountSelectProfileFallsBackToDefault(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@example.org", "Domain"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfile("person@other.example")
	if profile.DisplayName != "Default" {
		t.Fatalf("selected %q, want Default", profile.DisplayName)
	}
}

func TestMailAccountSelectProfileForRequestUsesHostWhenEmailMissing(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@invest.example.org", "Invest"),
		testMailAccountProfile("*@consult.example.org", "Consult"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfileForRequest("", "consult.example.org")
	if profile.DisplayName != "Consult" {
		t.Fatalf("selected %q, want Consult", profile.DisplayName)
	}
}

func TestMailAccountSelectProfileForRequestStripsDiscoveryHostPrefix(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@example.org", "Root"),
		testMailAccountProfile("default", "Default"),
	}}

	for _, host := range []string{"autoconfig.example.org", "autodiscover.example.org"} {
		profile := account.SelectProfileForRequest("", host)
		if profile.DisplayName != "Root" {
			t.Fatalf("host %q selected %q, want Root", host, profile.DisplayName)
		}
	}
}

func TestMailAccountSelectProfileForRequestEmailBeatsHost(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@invest.example.org", "Invest"),
		testMailAccountProfile("*@consult.example.org", "Consult"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfileForRequest("person@invest.example.org", "consult.example.org")
	if profile.DisplayName != "Invest" {
		t.Fatalf("selected %q, want Invest", profile.DisplayName)
	}
}
