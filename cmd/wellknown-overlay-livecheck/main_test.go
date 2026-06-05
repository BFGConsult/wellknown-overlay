package main

import (
	"reflect"
	"testing"
)

func TestParseDKIMSelectorsUsesCLIValue(t *testing.T) {
	got := parseDKIMSelectors("mail2026, default ,mail2026", "envselector")
	want := []string{"mail2026", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectors = %#v, want %#v", got, want)
	}
}

func TestParseDKIMSelectorsFallsBackToEnvValue(t *testing.T) {
	got := parseDKIMSelectors("", "mail2026,default")
	want := []string{"mail2026", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectors = %#v, want %#v", got, want)
	}
}

func TestParseDKIMSelectorsReturnsNilWhenUnset(t *testing.T) {
	if got := parseDKIMSelectors("", ""); got != nil {
		t.Fatalf("selectors = %#v, want nil", got)
	}
}
