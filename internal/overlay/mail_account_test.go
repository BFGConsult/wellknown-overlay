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
