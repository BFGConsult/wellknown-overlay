package overlay

import "testing"

func TestMailAccountSelectProfileChoosesExactBeforeWildcard(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@efn.no", "Wildcard"),
		testMailAccountProfile("bfg@efn.no", "Exact"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfile("BFG@EFN.NO")
	if profile.DisplayName != "Exact" {
		t.Fatalf("selected %q, want Exact", profile.DisplayName)
	}
}

func TestMailAccountSelectProfileChoosesMoreSpecificGlob(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@efn.no", "Domain"),
		testMailAccountProfile("*+bfg@efn.no", "Tagged"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfile("post+bfg@efn.no")
	if profile.DisplayName != "Tagged" {
		t.Fatalf("selected %q, want Tagged", profile.DisplayName)
	}
}

func TestMailAccountSelectProfileUsesFirstMatchAsTieBreaker(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*+bfg@efn.no", "First"),
		testMailAccountProfile("tag+*@efn.no", "Second"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfile("tag+bfg@efn.no")
	if profile.DisplayName != "First" {
		t.Fatalf("selected %q, want First", profile.DisplayName)
	}
}

func TestMailAccountSelectProfileFallsBackToDefault(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@efn.no", "Domain"),
		testMailAccountProfile("default", "Default"),
	}}

	profile := account.SelectProfile("person@example.org")
	if profile.DisplayName != "Default" {
		t.Fatalf("selected %q, want Default", profile.DisplayName)
	}
}
