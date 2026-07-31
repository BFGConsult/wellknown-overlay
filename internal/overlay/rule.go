package overlay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// Rule is an ordered gateway-only request rule. The first matching rule owns
// the request; requests not matched by any rule continue through the gateway's
// normal route and fallback handling.
type Rule struct {
	Match       RuleMatch     `json:"match"`
	File        string        `json:"file,omitempty"`
	ContentType string        `json:"content_type,omitempty"`
	Status      int           `json:"status,omitempty"`
	Redirect    *RuleRedirect `json:"redirect,omitempty"`
	Rewrite     *RuleRewrite  `json:"rewrite,omitempty"`
}

type RuleRedirect struct {
	Origin             string `json:"origin"`
	Status             int    `json:"status"`
	PreserveRequestURI bool   `json:"preserve_request_uri,omitempty"`
}

// RuleRewrite reserves the rewrite action name and JSON shape. Rewrites are
// deliberately rejected until their path semantics are designed and enabled.
type RuleRewrite struct {
	Path string `json:"path,omitempty"`
}

type RuleMatch struct {
	All        bool     `json:"-"`
	Hosts      []string `json:"hosts,omitempty"`
	Path       string   `json:"path,omitempty"`
	PathPrefix string   `json:"path_prefix,omitempty"`

	present       bool
	hostsSet      bool
	pathSet       bool
	pathPrefixSet bool
}

func (match *RuleMatch) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return errors.New("match is required")
	}

	match.present = true
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if value != "*" {
			return errors.New(`string match must be "*"`)
		}
		match.All = true
		return nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return errors.New(`match must be "*" or an object`)
	}
	for name, raw := range fields {
		switch name {
		case "hosts":
			match.hostsSet = true
			if err := json.Unmarshal(raw, &match.Hosts); err != nil {
				return fmt.Errorf("hosts: %w", err)
			}
		case "path":
			match.pathSet = true
			if err := json.Unmarshal(raw, &match.Path); err != nil {
				return fmt.Errorf("path: %w", err)
			}
		case "path_prefix":
			match.pathPrefixSet = true
			if err := json.Unmarshal(raw, &match.PathPrefix); err != nil {
				return fmt.Errorf("path_prefix: %w", err)
			}
		default:
			return fmt.Errorf("unknown match field %q", name)
		}
	}
	return nil
}

func (rule Rule) Validate() error {
	if err := rule.Match.validate(); err != nil {
		return err
	}

	actions := 0
	if rule.File != "" {
		actions++
	}
	if rule.Status != 0 {
		actions++
	}
	if rule.Redirect != nil {
		actions++
	}
	if rule.Rewrite != nil {
		actions++
	}
	if actions != 1 {
		return errors.New("must declare exactly one of file, status, redirect, or rewrite")
	}

	if rule.Rewrite != nil {
		return errors.New("rewrite action is reserved but not supported")
	}
	if rule.File != "" {
		if err := validateRuleFile(rule.File); err != nil {
			return fmt.Errorf("file: %w", err)
		}
	} else if rule.ContentType != "" {
		return errors.New("content_type requires file")
	}
	if rule.Status != 0 && (rule.Status < 300 || rule.Status > 599) {
		return errors.New("status must be an HTTP status from 300 through 599")
	}
	if rule.Redirect != nil {
		if err := rule.Redirect.validate(); err != nil {
			return fmt.Errorf("redirect: %w", err)
		}
	}
	return nil
}

func (match RuleMatch) validate() error {
	if !match.present {
		return errors.New("match is required")
	}
	if match.All {
		return nil
	}
	if !match.hostsSet && !match.pathSet && !match.pathPrefixSet {
		return errors.New(`empty match is invalid; use "match": "*" for an unconditional rule`)
	}
	if match.hostsSet {
		if len(match.Hosts) == 0 {
			return errors.New("match.hosts must not be empty")
		}
		seen := make(map[string]struct{}, len(match.Hosts))
		for _, host := range match.Hosts {
			if host == "*" {
				return errors.New(`match.hosts must contain exact hostnames; omit hosts to match all hosts`)
			}
			if strings.ToLower(host) != host {
				return fmt.Errorf("match host %q must be lowercase", host)
			}
			if err := validateSourceHost(host); err != nil {
				return fmt.Errorf("match host %q: %w", host, err)
			}
			if _, ok := seen[host]; ok {
				return fmt.Errorf("duplicate match host %q", host)
			}
			seen[host] = struct{}{}
		}
	}
	if match.pathSet && match.pathPrefixSet {
		return errors.New("match cannot declare both path and path_prefix")
	}
	if match.pathSet {
		if err := validateRulePath(match.Path); err != nil {
			return fmt.Errorf("match.path: %w", err)
		}
	}
	if match.pathPrefixSet {
		if err := validateRulePathPrefix(match.PathPrefix); err != nil {
			return fmt.Errorf("match.path_prefix: %w", err)
		}
	}
	return nil
}

func validateRulePath(value string) error {
	if value == "" || !strings.HasPrefix(value, "/") {
		return errors.New("must be an absolute path")
	}
	if path.Clean(value) != value {
		return errors.New("must be clean")
	}
	return nil
}

func validateRulePathPrefix(value string) error {
	if value == "" || !strings.HasPrefix(value, "/") {
		return errors.New("must be an absolute path")
	}
	clean := path.Clean(value)
	if value != clean && value != clean+"/" {
		return errors.New("must be clean")
	}
	return nil
}

func validateRuleFile(value string) error {
	if strings.HasPrefix(value, "/") {
		return errors.New("must be relative")
	}
	if path.Clean(value) != value || strings.HasPrefix(value, "../") || value == ".." {
		return errors.New("must stay within the root")
	}
	return nil
}

func (redirect RuleRedirect) validate() error {
	if redirect.Status != 301 && redirect.Status != 302 && redirect.Status != 303 && redirect.Status != 307 && redirect.Status != 308 {
		return errors.New("status must be 301, 302, 303, 307, or 308")
	}
	parsed, err := url.Parse(redirect.Origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("origin must be an absolute HTTP(S) origin")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("origin must not contain credentials, a path, query, or fragment")
	}
	if strings.ContainsAny(redirect.Origin, "\r\n") {
		return errors.New("origin must not contain line breaks")
	}
	return nil
}
