// Package auth contains pure route validation and explicit state semantics.
package auth

import (
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var Reference = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
var ID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func Origin(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return errors.New("exact HTTPS origin required")
	}
	return nil
}
func Validate(c a.Configuration) error {
	if c.Version != a.Version || c.Kind != "AuthConfiguration" || !ID.MatchString(c.ConnectionID) || c.NativePrincipal == "" || c.TenantID == "" || len(c.Containers) == 0 || len(c.Containers) > 100 {
		return errors.New("exact connection, principal and scope required")
	}
	if e := Origin(c.Origin); e != nil {
		return e
	}
	if c.BasePath != "" && (path.Clean(c.BasePath) != c.BasePath || !strings.HasPrefix(c.BasePath, "/") || strings.ContainsAny(c.BasePath, "?#%\\")) {
		return errors.New("unsafe base path")
	}
	seen := map[string]bool{}
	for _, v := range c.Containers {
		if v == "" || seen[v] {
			return errors.New("duplicate or empty scope")
		}
		seen[v] = true
	}
	allowed := map[string]bool{"token": true, "username": true, "accessToken": true, "refreshToken": true, "clientSecret": true}
	refs := map[string]bool{}
	for k, v := range c.SecretReferences {
		if !allowed[k] || !Reference.MatchString(v) || refs[v] {
			return errors.New("invalid or shared secret reference")
		}
		refs[v] = true
	}
	switch c.Provider {
	case "github":
		if c.Mode == "gh-native" {
			u, _ := url.Parse(c.Origin)
			if u.Host != strings.ToLower(u.Host) || u.Port() != "" || c.BasePath != "" {
				return errors.New("native gh explicit ports/base paths unsupported; choose exact REST route")
			}
		}
		if c.Mode != "token" && c.Mode != "gh-native" {
			return errors.New("unsupported GitHub auth mode")
		}
	case "azure-devops":
		if c.BasePath != "" {
			return errors.New("Azure uses explicit organization lookup, not base path")
		}
		if !ID.MatchString(c.ProviderLookup) {
			return errors.New("exact Azure organization lookup required")
		}
		if c.Origin != "https://dev.azure.com" || c.Mode != "pat" && c.Mode != "entra" && c.Mode != "az-native" {
			return errors.New("unsupported Azure Services auth route")
		}
	case "jira-cloud":
		if c.Mode != "basic" && c.Mode != "oauth2" && c.Mode != "acli-native" {
			return errors.New("unsupported Jira Cloud auth mode")
		}
	case "jira-datacenter":
		if c.TLS == nil || c.TLS.ServerVersion == "" || len(c.TLS.CASHA256) != 64 || c.TLS.CAPath == "" || c.Mode != "token" && c.Mode != "basic" {
			return errors.New("instance, TLS and supported DC auth required")
		}
	case "linear":
		if c.Origin != "https://api.linear.app" || c.BasePath != "" || c.Mode != "api-key" && c.Mode != "oauth2" {
			return errors.New("unsupported Linear route")
		}
	default:
		return errors.New("unknown provider")
	}
	if c.Mode == "gh-native" || c.Mode == "az-native" || c.Mode == "acli-native" {
		if c.Tool == nil {
			return errors.New("native tool pin required")
		}
		return nil
	}
	if c.Mode == "oauth2" {
		if c.Provider == "jira-cloud" && c.BasePath != "" {
			return errors.New("Jira OAuth base path unsupported")
		}
		if c.OAuth == nil || c.OAuth.ClientID == "" || len(c.OAuth.Scopes) == 0 || c.Broker == nil {
			return errors.New("OAuth and protected broker required")
		}
		for _, ep := range []string{c.OAuth.AuthorizationURL, c.OAuth.TokenURL, c.OAuth.RevokeURL} {
			if ep == "" && c.Provider == "jira-cloud" {
				continue
			}
			u, e := url.Parse(ep)
			if e != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Fragment != "" || u.RawQuery != "" {
				return errors.New("invalid OAuth destination")
			}
		}
		u, e := url.Parse(c.OAuth.RedirectURI)
		if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return errors.New("exact loopback redirect required")
		}
		return nil
	}
	if c.SecretReferences["token"] == "" {
		return errors.New("token reference required")
	}
	if c.Mode == "basic" && c.SecretReferences["username"] == "" {
		return errors.New("username reference required")
	}
	return nil
}

type Route struct {
	Provider          string `json:"provider"`
	Mode              string `json:"mode"`
	Implementation    string `json:"implementation"`
	LiveQualification string `json:"liveQualification"`
	Limitation        string `json:"limitation"`
}

func Routes() []Route {
	return []Route{
		{"github", "token", "implemented", "pending", "explicit external token; no native store"},
		{"github", "gh-native", "implemented-driver", "pending", "secure native-store and implicit-refresh controls unqualified"},
		{"azure-devops", "pat/entra", "implemented", "pending", "explicit token; no CLI default mutation"},
		{"azure-devops", "az-native", "implemented-driver", "pending", "extension tree and isolated credential cache unqualified"},
		{"jira-cloud", "basic", "implemented", "pending", "explicit external references"},
		{"jira-cloud", "acli-native", "implemented-hybrid", "pending", "explicit jira-basic hybrid; signed opaque native status and inherited controls required"},
		{"linear", "api-key", "implemented", "pending", "explicit external reference"},
		{"linear", "oauth2", "implemented-protocol", "pending", "operator-registered app and protected secret broker required"},
		{"jira-cloud", "oauth2", "implemented-protocol", "pending", "registered confidential app, site identity and secure broker enrollment required"},
		{"jira-datacenter", "token/basic", "implemented", "pending", "exact server and external CA pin required"},
	}
}
